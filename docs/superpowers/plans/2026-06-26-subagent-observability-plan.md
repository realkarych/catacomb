# Subagent observability — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: use
> superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** make each subagent a single collapsible node nested under the turn
that spawned it, containing its full inner work
(`turn → Agent tool_call → subagent → inner prompt/turns/tools`), with no setup
change and no CPU regression.

**Architecture:** the tailer discovers sibling `subagents/agent-*.jsonl` and
reads `agent-<id>.meta.json` for the spawn link; jsonl threads `AgentID`; reduce
nests agent-scoped and near-linear; one frontend label polish. Design:
`docs/superpowers/specs/2026-06-26-subagent-observability-design.md`.

**Tech stack:** Go 1.x (daemon, ingest, reduce), Svelte 5 + Vitest + Playwright
(webui).

## Global Constraints

- **No comments in Go code.** Only `//go:build|embed|generate` directives are
  allowed. Enforced by `internal/codepolicy`.
- **100% Go test coverage**, TDD-first: `make cover` must stay at 100% (it never
  goes down). `golangci-lint` 0 issues.
- **Frontend:** `cd webui && npm run test` (vitest + coverage gate) green;
  `npm run check` (typecheck) clean. Do NOT commit `webui/dist` (a later
  finalize step rebuilds it once).
- **Redaction/gating preserved:** content only via the gated, redaction-aware
  `/payload`; never `{@html}` / raw HTML injection.
- Conventional commits ending with
  `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`.
- Behavior preservation: the **main session** (nodes with `AgentID==""`) must
  keep its current hierarchy and edge/tombstone semantics exactly.

---

## Task 1: Tailer steady-state early-out

**Files:**

- Modify: `ingest/tail/tail.go` (`pollFile`)
- Test: `ingest/tail/tail_test.go`

**Problem:** `pollFile` calls `persistFingerprint` every tick for every file
even when nothing changed (`size == cursor.Offset`), doing a 512-byte read +
sha256 + a `UpsertTailCursor` DB write. `TailCursor.Size` and `TailCursor.Mtime`
are persisted (`persistFingerprint`) but never read. At ~435 idle files × 2 Hz
this is a constant tax.

**Behavior:** at the top of `pollFile`, after `stat`, if the file is already
tracked AND `info.Size() == st.cursor.Size` AND
`info.ModTime().UnixNano() == st.cursor.Mtime`, return `nil` immediately — no
read, no fingerprint, no DB write. This is safe: truncation (`size < offset`)
and content rewrite (head-fingerprint mismatch) are still detected on the next
poll where size or mtime differs, and a same-size+same-mtime file has no new
bytes to ingest.

**Interfaces:** none changed (internal to `pollFile`).

**Tests (TDD):**

- A tracked, unchanged file (same size + mtime as the persisted cursor) triggers
  NO `openFile` call and NO `UpsertTailCursor` write within a poll — assert via
  the existing `openFile`/store seams (count calls).
- A file whose mtime changed but size did not is still polled (not skipped).
- A grown file is still ingested (regression).
- A truncated file (`size < offset`) still resets + marks lossy (regression).
- Keep all existing tail tests green; 100% coverage of the new branch.

**Acceptance:** idle files cost a `stat` only; all tail behaviors preserved.

---

## Task 2: Subagent discovery + meta.json + AgentID threading

**Files:**

- Modify: `ingest/tail/tail.go` (discovery, meta read, `Sink` interface)
- Modify: `ingest/jsonl/jsonl.go` (`decodeLine` base correlation)
- Modify: `daemon/daemon.go` (implement the new `Sink` method)
- Modify: `model/model.go` (add `SubagentMeta` type)
- Test: `ingest/tail/tail_test.go`, `ingest/jsonl/jsonl_test.go`,
  `daemon/*_test.go`

**Interfaces (produce):**

- `model.SubagentMeta{ SessionID, AgentID, ToolUseID, AgentType, Description string }`.
- `tail.Sink` gains:
  `IngestSubagentMeta(m model.SubagentMeta) error`.
- The daemon implements `IngestSubagentMeta`: resolve `execID` from
  `m.SessionID` (same `execBySession` map / `newExecID` path as
  `IngestTranscript`), build one `model.Observation{ Kind:"subagent_stop",
  Correlation:{ AgentID:m.AgentID, ParentToolUseID:m.ToolUseID,
  SessionID:m.SessionID }, Attrs:{ "subagent_type":m.AgentType,
  "description":m.Description } }` with a fresh `ObsID`/`Seq`, and run it through
  `applyAndPersist` (same persistence path as transcript observations). Reducer
  consumption is Task 3.

**Behavior — tailer discovery:**

- In addition to the current `glob()` results, for each MAIN transcript path
  (the `*.jsonl` and `*/*.jsonl` matches), resolve symlinks
  (`filepath.EvalSymlinks`), and from the resolved path derive the sibling
  subagents glob: `<dir>/<base-without-.jsonl>/subagents/agent-*.jsonl`. Add
  those agent transcripts to the poll set (deduplicated). This makes the flat
  tail-scope expose subagents without a setup change. Keep the existing
  `*/*/subagents/agent-*.jsonl` glob too.
- For each agent transcript file, derive `agentID` from the filename
  (`agent-<id>.jsonl` → `<id>`) and read the sibling `agent-<id>.meta.json`
  (`{agentType, description, toolUseId}`). Emit `IngestSubagentMeta` ONCE per
  file (track an emitted flag on the file state; meta is static). If the
  `.meta.json` is missing or unparseable, skip the meta emit (the JSONL
  `subagent_stop` still creates the node as a session-parented fallback) — do not
  error the poll.
- Agent transcript lines are ingested through the existing `IngestTranscript`
  path (offset-based, like main transcripts).

**Behavior — jsonl:**

- In `decodeLine`, set `base.AgentID = ln.AgentID` so the `user_prompt`,
  `assistant_turn`, and `assistant_tool_use` partials all carry `AgentID` for
  sub-transcript lines. (Main-transcript lines have `ln.AgentID==""`, so this is
  a no-op for them — preserve their behavior.)

**Tests (TDD):**

- `decodeLine`: a sidechain line yields partials whose `Correlation.AgentID` is
  set; a main line yields `AgentID==""` (regression).
- tailer: given a temp dir with a main transcript symlink whose resolved sibling
  `<session>/subagents/agent-X.jsonl` + `agent-X.meta.json` exist, the poll (1)
  ingests the agent transcript lines and (2) calls `IngestSubagentMeta` once with
  the parsed `{AgentID:"X", ToolUseID, AgentType, Description}`; a second poll
  does NOT re-emit meta. Missing `.meta.json` → no meta emit, no error, lines
  still ingested. Use the existing `openFile`/`statFn`/`Sink` seams (extend the
  test `Sink` with the new method).
- daemon: `IngestSubagentMeta` creates, updates, and persists the subagent node
  through the reducer (assert via the store/graph as existing daemon tests do).
- 100% coverage on all new branches.

**Acceptance:** with a flat tail-scope, subagent transcripts are tailed and each
subagent's `{toolUseId, agentType, description}` reaches the reducer exactly
once; sub-transcript nodes carry `AgentID`.

---

## Task 3: Reduce — agent-scoped nesting + de-quadratic parenting

**Files:**

- Modify: `reduce/reduce.go` (`applyUser`, `applySubagent`, `precedingPromptID`,
  `parentTurn`, `recordPrompt`, execState prompt/turn bookkeeping)
- Test: `reduce/reduce_test.go`

**Consumes (from Task 2):** observations whose `Correlation.AgentID` is set for
sub-transcript prompts/turns/tools, and a `subagent_stop` observation carrying
`Correlation.ParentToolUseID = <spawn toolUseId>` plus
`Attrs["subagent_type"]` and `Attrs["description"]`.

**Behavior:**

1. **Inner-root prompt → subagent.** In `applyUser`, when
   `o.Correlation.AgentID != ""`, parent the prompt under
   `SubagentID(o.ExecutionID, o.Correlation.AgentID)` instead of
   `SessionNodeID`. When `AgentID==""` keep the session edge (unchanged).
2. **Subagent → Agent tool_call (reparent-safe).** In `applySubagent`, give the
   subagent node ONE structural parent chosen by rank: the Agent tool_call
   `ToolCallID(o.ExecutionID, o.Correlation.ParentToolUseID)` when
   `ParentToolUseID != ""` (higher rank), else `SessionNodeID` (lower rank).
   Use the existing reparent-aware mechanism so an arriving meta observation
   upgrades a previously session-parented marker and the stale edge is deleted
   with `Rev = max(o.Seq, old.Rev)` — order-independent across the
   hook/JSONL/meta producers. Also set `n.SubagentType` from
   `Attrs["subagent_type"]` (keep current "first non-empty wins") and
   `n.Name` from `Attrs["description"]` when present and `n.Name==""`.
3. **Agent-scoped turn/prompt parenting.** A turn parents under the preceding
   prompt **of the same `AgentID`**. Track `AgentID` on the recorded prompt/turn
   refs (or keep per-`AgentID` groups). `precedingPromptID` returns the prompt
   with the greatest `seq` strictly less than the target `seq` **within the same
   `AgentID` group** (fall back to the session node id when none — same as
   today). The `AgentID==""` group reproduces today's main-session behavior
   exactly.
4. **Near-linear.** Keep each `AgentID` group's prompts sorted by `seq` so the
   preceding prompt is a binary search, and on a new prompt re-parent only the
   turns in that group whose preceding prompt actually changes (stop once a
   turn's parent is unchanged). Eliminate the current full `s.turns` rescan in
   `recordPrompt`. Preserve the exact invariant: a turn's parent is the prompt
   with the greatest `seq` strictly less than the turn's (min-observed) `seq` in
   its group, else the session/subagent root; preserve `setTurnParent`
   tombstone semantics (`Rev = max(o.Seq, old.Rev)`).

**Tests (TDD):**

- Inner-root prompt with `AgentID` set parents under the subagent node, not the
  session.
- A `subagent_stop` with `ParentToolUseID` set parents the subagent under the
  Agent tool_call; a later/earlier session-parented `subagent_stop` for the same
  agent does NOT leave a duplicate session edge (reparent + tombstone asserted);
  `Name` = description, `SubagentType` = type.
- Agent isolation: interleaved main + two-subagent observations (shared exec,
  interleaved `seq`) — each subagent's turns parent only under its own prompts;
  main turns parent only under main prompts. Build the exact expected
  `parent_child` edge set and assert it.
- Main-session regression: an existing main-only sequence produces byte-identical
  edges/tombstones to before (no behavior change for `AgentID==""`).
- A correctness case with many interleaved prompts/turns (e.g. 200+) asserting
  the parent invariant holds for every turn (guards the near-linear rewrite).
- 100% coverage.

**Acceptance:** `turn → Agent tool_call → subagent → inner prompt/turns/tools`
holds; main session unchanged; no `O(P²·T)` rescan remains.

---

## Task 4: Frontend — subagent label from description

**Files:**

- Modify: `webui/web/src/lib/graph/outline.ts` (`outlineLabel`)
- Test: `webui/web/src/lib/graph/outline.test.ts` (or the existing outline spec)

**Behavior:** for a `subagent` node, `outlineLabel` returns
`{ primary: node.name || 'subagent', secondary: node.subagent_type }`. When
`node.name` is the meta `description` it becomes the primary label; with no name
it falls back to `'subagent'` (today's behavior) with the type as secondary.
Keep the existing return shape and all other node-type labels unchanged.

**Tests (TDD):** a subagent node with `name` set → primary is the name; without
`name` → primary `'subagent'`, secondary the type. vitest 100% on `outlineLabel`.

**Acceptance:** subagents render with their task description as the row label;
no other label regressions.

---

## Task 5: Incremental persist — kill the O(N²) full-graph snapshot (live-found)

**Files:**

- Modify: `daemon/daemon.go` (`applyAndPersist`)
- Modify: `store/sqlite.go` (new `AppendDeltas`, `deleteEdge`; drop dead `AppendAndApply`)
- Test: `daemon/*_test.go`, `store/*_test.go`

**Problem (found during live-verify):** `applyAndPersist` wrote the whole graph
(`g.Snapshot()`) via `store.AppendAndApply` on every observation — O(N²) over a
run; subagent ingestion pegged a core for minutes. The `nodes`/`edges` tables
are a write-only cache (restart replays the `observations` table), so persisting
the per-observation delta is correct and linear.

**Behavior:** add `store.AppendDeltas(o, deltas)` that, in one transaction,
inserts the observation and applies `g.DrainDeltas()`: upsert nodes
(NodeUpsert/NodeStatus/NodeMerge), upsert edges (EdgeUpsert), and DELETE edges
(EdgeDelete — the snapshot path never deleted, leaving stale reparent rows).
`applyAndPersist` drains once and uses the same slice for persist + publish;
`UpsertRun` and the `storeWriteErrors` error path are preserved. Remove the now
dead `AppendAndApply` (keep `Persist`/`applyGraph` — still used by
`cmd/catacomb/replay.go`).

**Tests:** store delta-apply (incl. edge-delete removes the row, rollback);
restart/replay yields the correct graph with a reparent tombstone absent; an
O(N²) regression guard that asserts only delta-sized writes per observation and
provably FAILS if reverted to `g.Snapshot()`; 100% coverage.

**Acceptance:** persistence linear in deltas; frozen-scope full-session ingest
settles in ~20 s with CPU returning to idle.

---

## Task 6: Lazy-load — filtered snapshot/SSE + subagent aggregate (backend)

**Files:** `daemon/sessions.go` (`sessionGraphDeltas`), `daemon/subscribe.go`
(`matchDelta`/`matchNode`), a small aggregate helper; tests in
`daemon/sessions_test.go`, `daemon/subscribe_test.go`.

**Behavior:** define "inner" node = `node.AgentID != "" && node.ID !=
model.SubagentID(execID, node.AgentID)`. The `/graph` snapshot and the SSE matcher
must OMIT inner nodes and any edge incident to an inner node. Keep the subagent
node and the top-level spine. On each subagent node in the snapshot/stream, set
`Attrs["descendant_count"]` = number of its inner nodes and token/cost totals
(summed from inner nodes), computed from the in-memory `reduce.Graph` (deep-copy
the node before mutating Attrs — `copyNode` already deep-copies Attrs). Aggregate
must update as inner nodes arrive (recompute from current graph state).

**Tests:** snapshot excludes inner nodes + their edges, keeps spine + subagent
nodes; subagent node carries a correct `descendant_count`/token totals; SSE does
not stream an inner-node delta but does stream spine deltas + subagent updates;
100% coverage.

**Acceptance:** the big session's `/graph` payload drops from ~24 MB to the spine;
collapsed subagents show a live count.

---

## Task 7: Lazy-load — subagent subtree endpoint (backend)

**Files:** `daemon/server.go` (route), a handler (e.g. `daemon/sessions.go` or a new
`daemon/subagent.go`); tests.

**Behavior:** `GET /v1/sessions/{hash}/subagent/{agentId}` (clone the
`handleNodePayload` routing/auth pattern, `daemon/payload.go` + `server.go:50`)
returns, for the session's execution, the inner nodes (`AgentID == agentId`,
excluding the subagent node itself) + every edge among them + the subagent→inner-root
edge, serialized as the same `[]sseEvent`/delta shape the snapshot uses. Unknown
session/agent → 404; same auth/token gate as the other endpoints.

**Tests:** returns exactly the agent's inner nodes+edges (not other agents', not the
spine); 404 on unknown; auth enforced; 100% coverage.

**Acceptance:** a single subagent's inner work is fetchable on demand.

---

## Task 8: Lazy-load — client fetch-on-expand + aggregate fallback (frontend)

**Files:** `webui/web/src/lib/api.ts` (`fetchSubagentSubtree`),
`webui/web/src/components/Outline.svelte` (expand hook + chevron),
`webui/web/src/lib/graph/aggregate.ts` (backend-count fallback); vitest beside each.

**Behavior:**

- `fetchSubagentSubtree(hash, agentId)` mirrors `fetchSessionGraph`; its events flow
  through the existing `handleEvent`/`applyDelta` (idempotent merge).
- In `Outline.svelte`, a `subagent` row shows the chevron when
  `attrs.descendant_count > 0` (NOT when it has client-side children). Expanding it
  (chevron click + ArrowRight) first calls `fetchSubagentSubtree` if its agentId is
  not in a `loadedAgents` set, then toggles collapse; show a transient loading state;
  guard against double-fetch.
- `aggregateOf`: when a collapsed node has no client-side descendants but is a
  subagent with `attrs.descendant_count`, build the `Aggregate` from the backend
  attrs (count + tokens/cost) so the row reads "N steps", not "0".

**Tests (vitest):** `fetchSubagentSubtree` shape; aggregate fallback uses backend
attrs when descendants absent and real descendants once loaded; chevron shown for a
childless subagent with `descendant_count>0`; expand triggers exactly one fetch then
renders inner rows. 100% on the touched lib logic.

**Acceptance:** the big live session loads fast and stays responsive; expanding a
subagent fetches + shows its inner work; collapsed rows show live counts.

---

## Execution notes

- Order: Task 1 → Task 2 → Task 3 → Task 4 (Task 1/2 share `tail.go`; Task 3
  consumes Task 2; Task 4 is independent frontend). One implementer at a time.
- After each task: `scripts/review-package BASE HEAD` → task reviewer (spec +
  quality); fix Critical/Important; mark the ledger.
- Final: whole-branch opus review + fix wave; rebuild `webui/dist` once; rebuild
  the daemon; Playwright live-verify on `157a2d02`; `make cover` 100%,
  `golangci-lint` 0, markdownlint clean; green CI; merge.
