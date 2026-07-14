# Graph Collapse + Node Inspection + CI Repair — Design

**Goal:** Make the catacomb web graph readable at session scale (800+ nodes) by collapsing the hierarchy into an expandable spine, enrich node inspection with the actual conversation text, and return GitHub CI to green.

**Architecture:** Three independent workstreams shipped in order — (1) CI repair, (2) collapsible graph, (3) conversation-text inspection. The graph work extends the existing pure-logic + thin-component split (vitest-tested pure modules feeding `GraphCanvas.svelte`). The inspection work extends the existing gated, redaction-aware payload path. No new services, endpoints, or dependencies.

**Tech Stack:** Go daemon (SQLite, SSE), Svelte 5 + Vite web UI, `@xyflow/svelte` + `@dagrejs/dagre`, vitest + Playwright, bubbletea TUI.

## Global Constraints

- No comments in Go code — only `//go:build`, `//go:embed`, `//go:generate` directives. Enforced by `internal/codepolicy`.
- Go: 100% test coverage (`make cover`, threshold never lowered), TDD-first, `golangci-lint` 0 issues.
- Frontend: vitest 100% on pure logic (reducer/stores/selectors/format/new graph modules); Svelte components not line-gated but covered by Playwright e2e.
- The committed `webui/dist/` must be rebuilt and checked in; CI verifies it is in sync.
- Design language: minimalist, functional, silence-when-healthy, no decorative elements. Light theme auto-follows OS, no toggle.
- Real CI is the gate: `make cover` + `golangci` + `npm run test` + Playwright + green GitHub Actions across ubuntu/macos/windows.

---

## Workstream 1 — CI Repair

CI is red on `master` since the UX-overhaul merge (#31). Two jobs fail; one latent test-hygiene bug is folded in.

### 1.1 markdownlint — `MD032/blanks-around-lists`

`docs/plans/2026-06-24-m-a-web-views.md`, `…-m-b-deep-inspection.md`, `…-m-d-onboarding-polish.md` contain lists not surrounded by blank lines. Fix: insert the required blank lines so the plan docs pass `markdownlint-cli`. Do not disable the rule and do not add per-file ignores — the docs should be lint-clean like the rest.

### 1.2 Windows test — `TestBuildStartDaemonCreateRunDirError`

`cmd/catacomb/up_test.go:542` asserts a daemon-start error contains `"create run dir"`. On `windows-latest` the test fails because the daemon command path is the hardcoded unix string `/bin/catacomb`, so exec fails first with `executable file not found in %PATH%` before the run-dir logic runs. Fix: the test must drive the code path it claims to test on every OS — construct the unwritable/failing run-dir condition independently of the exec binary path, using `filepath`-built paths and `t.TempDir()`, so the asserted `"create run dir"` error surfaces on ubuntu, macos, and windows alike. If the run-dir failure cannot be provoked portably through the public seam, adjust the seam so it can — do not weaken the assertion.

### 1.3 Stray `.claude/` from install-hooks test (folded-in hygiene)

Confirm whether any `cmd/catacomb` test leaves a `.claude/` directory in the source tree after `make cover` (suspected around `installhooks_test.go`). If confirmed, the test must write strictly under `t.TempDir()` (explicit project path, not CWD-relative) and assert there. If not reproducible, record that it was checked and skip.

### 1.4 Exit criteria

`make cover` 100%, `golangci-lint` 0 issues, `npm run test` + e2e green, and a fresh GitHub Actions run green on all three OSes. No new coverage or lint exclusions introduced.

---

## Workstream 2 — Collapsible Graph

### 2.1 Problem

`GraphCanvas.svelte` maps every session node into one flat `@dagrejs/dagre` `LR` layout (`lib/layout.ts`). At 800+ nodes this is an unreadable sheet. There is no grouping, collapsing, or level-of-detail today — only filter-dimming and selection.

### 2.2 Collapse model

The collapse axis is the `parent_child` hierarchy plus node type. Every node that has `parent_child` children is collapsible. Within one session the graph is effectively one connected component (a tree rooted at `session`), so hierarchy — not connected-components — is the grouping that matters; genuinely disconnected nodes (orphan `hook_event`/`marker` with no `parent_child` link) are tucked into a single "other" affordance rather than scattered.

Node-type roles in the hierarchy:

- `session` — root.
- `user_prompt` — a turn group under the session.
- `assistant_turn` — under a prompt; parent of the turn's tool calls.
- `tool_call` / `mcp_call` — leaves under an assistant turn.
- `subagent` — a node whose entire `parent_child` subtree is one collapsible group, attached where it was spawned.
- `hook_event` / `marker` — usually leaves; orphans go to the "other" affordance.

**Default collapse policy:** on load, collapse all `subagent` nodes (hide their subtrees) and collapse all `assistant_turn` nodes (hide `tool_call`/`mcp_call` leaves). Visible by default: `session` → `user_prompt` → `assistant_turn` (collapsed) and top-level `subagent` (collapsed). The exact default depth is tuned live against the real 800-node session to land at ~20–40 visible nodes; the policy is expressed as data (a predicate over node type/depth) so tuning is a one-line change, not a refactor.

### 2.3 New pure modules (vitest 100%)

All under `webui/web/src/lib/graph/`:

- `hierarchy.ts`
  - `buildHierarchy(nodes: Node[], edges: Edge[]): Hierarchy` — builds parent→children and child→parent maps from `parent_child` edges (falling back to `node.parent_id` when an edge is absent), plus a root list and an orphan list.
  - `Hierarchy` exposes `childrenOf(id)`, `parentOf(id)`, `ancestorsOf(id)`, `descendantsOf(id)`, `roots`, `orphans`.
- `collapse.ts`
  - `defaultCollapsed(nodes: Node[], hierarchy: Hierarchy): Set<string>` — applies the default policy.
  - `visibleNodeIds(nodes: Node[], hierarchy: Hierarchy, collapsed: Set<string>): Set<string>` — a node is visible iff no ancestor is collapsed.
  - `toggle(collapsed: Set<string>, id: string): Set<string>` — pure, returns a new set.
  - `collapseAll` / `expandAll` helpers returning new sets.
- `aggregate.ts`
  - `aggregateOf(id: string, hierarchy: Hierarchy, byId: Record<string, Node>): Aggregate` — rolls up the subtree: `{ count, tokensIn, tokensOut, costUsd, status, hasError }`. Status rollup: `error` if any descendant errored, else `running` if any running, else `ok`. Missing numeric fields treated as 0.
- `lift-edges.ts`
  - `liftEdges(edges: Edge[], visible: Set<string>, hierarchy: Hierarchy): Edge[]` — for each edge, remap each endpoint that is hidden to its nearest visible ancestor; drop self-edges produced by lifting; dedupe by `(src,dst,type)`. Keeps a collapsed subagent connected to the spine and preserves cross-turn `data_dep`/`sequence` as turn-to-turn links.

Types (`Aggregate`, `Hierarchy`) live in `lib/graph/types.ts` and reuse the existing `Node`/`Edge` from `lib/types.ts`.

### 2.4 Component wiring

`GraphCanvas.svelte`:

- Holds `collapsed: Set<string>` state, seeded by `defaultCollapsed` when the session's topology first loads. User toggles mutate it via the pure `toggle`.
- Derives `visible = visibleNodeIds(...)`, feeds only visible nodes + `liftEdges(...)` into the existing dagre layout. The `toTopologyKey` memo is extended so layout recomputes when the collapsed set changes, not only when raw node/edge ids change.
- Renders each visible node with a collapse affordance when it has children: `▸` collapsed / `▾` expanded. A collapsed node shows the aggregate badge `{glyph} {label} · {count} · {tokens} · {cost}` with status color. The badge stat line uses existing `format` helpers.
- Selection (click body → drawer) stays; the `▸/▾` affordance is a separate hit target that toggles collapse without selecting.
- Toolbar gains `Collapse all` / `Expand all`. An "other (N)" chip reveals orphan nodes on demand.

### 2.5 Real-time SSE behavior

The reducer (`lib/reducer/reducer.ts`) is unchanged — it keeps the full graph. Collapse is a view concern. On each delta:

- Recompute hierarchy/visible incrementally; preserve the user's explicit collapse choices (never auto-expand a node the user expanded or collapsed).
- A node arriving under a collapsed ancestor stays hidden and only updates that ancestor's aggregate (count/tokens/cost/status climb live).
- A new top-level `user_prompt` appears in the spine; a new `subagent` arrives collapsed per default policy.
- No layout thrash: only recompute layout when the visible set or topology actually changes.

### 2.6 Re-layout stability

Expanding/collapsing must not "teleport" the view (the known dagre con). On toggle: keep the toggled node anchored (translate the new layout so the toggled node's screen position is preserved) and keep the current selection in view. Respect `prefers-reduced-motion` — no transition animation when set.

### 2.7 Accessibility

The existing window-level arrow traversal and reflow-safe focus-return are reused. Arrow navigation skips hidden nodes (operates over the visible set). `Enter`/`Space` on a focused collapsible node toggles it. Focus is retained on the toggled node across the re-layout.

### 2.8 TUI parity

`catacomb observe` already renders the graph as an expand/collapse tree. Align its default-collapse policy with the web (same predicate semantics) so both surfaces open at the same level of detail. No new TUI structure; only the default-state policy is unified.

### 2.9 Tests

- vitest 100% on `hierarchy.ts`, `collapse.ts`, `aggregate.ts`, `lift-edges.ts` — including orphans, missing fields, deep subagent subtrees, lifted-edge dedup, and status rollup precedence.
- Playwright e2e: load a multi-turn + subagent session; assert default view shows the spine (not all nodes); expand a turn reveals its tools; expand a subagent reveals its subtree; collapse-all/expand-all; aggregate badge text; a collapsed node still shows its spine edge; live SSE node under a collapsed parent bumps the badge without appearing.

---

## Workstream 3 — Conversation-Text Inspection

### 3.1 Current state

Per-node `tokens_in`/`tokens_out`/`cost_usd` are populated and shown in `NodeDrawer.svelte`. MCP/tool call parameters are stored in `Node.Payload.Input` and shown by `PayloadPanel.svelte` via `GET /v1/sessions/{hash}/nodes/{nodeId}/payload` when the daemon runs with `--allow-payload-access`. The only gap: user/assistant message **text** is extracted during ingestion (`decodeContent` in `ingest/jsonl/jsonl.go`) but never persisted — `userParts()`/`assistantParts()` build partials without a payload for the message text.

### 3.2 Storage via the existing Payload model (confirmed decision)

Reuse `model.Payload{Input, Output, Hash}`:

- `userParts()` attaches `Payload.Input` = the user prompt text (+ hash) to the `user_prompt` partial.
- `assistantParts()` attaches `Payload.Output` = the assistant response text (+ hash) to the `assistant_turn` partial, without disturbing the existing tool-use payloads on `mcp_call`/`tool_call` children.
- Mirror the same in `ingest/streamjson/streamjson.go`.
- Text is JSON-encoded as a string value so `redact.Redact()` scans it like any other payload. The reduce-side `mergePayload` (`reduce/reduce.go`) needs no change.
- v1 scope: visible user text + assistant response text. Thinking blocks are out of scope (the API may summarize/omit them) — noted as a possible future.

### 3.3 Endpoint & gating

No backend route changes. Text flows through the existing `GET …/payload` handler (`daemon/payload.go`): gated behind `--allow-payload-access` (default OFF) and run through redaction. Same trust boundary as tool payloads.

### 3.4 UI rendering

`PayloadPanel.svelte` branches on node type: for `user_prompt`/`assistant_turn` it renders `Input`/`Output` as conversation text (markdown), not raw JSON; for `tool_call`/`mcp_call` it keeps the JSON view. The disabled-state message ("start the daemon with `--allow-payload-access`") is unchanged.

### 3.5 Tests

- Go: ingestion tests assert `user_prompt`/`assistant_turn` partials carry the text payload + hash, that tool payloads are untouched, and that redaction applies; reduce/merge tests confirm no regression. `make cover` stays 100%.
- vitest: `PayloadPanel` text-vs-JSON branch logic (pure helper) covered.
- Playwright e2e: with payload access on, clicking an assistant turn shows its response text; clicking a tool shows JSON params.

---

## Data Flow

Transcript/stream ingest → reduce → SQLite + SSE deltas → web reducer (full graph) → **collapse view layer** (hierarchy/visible/lift/aggregate, new) → dagre → `GraphCanvas`. Selection → `NodeDrawer` → on demand `GET …/payload` (now including conversation text) → `PayloadPanel`.

## Error Handling

- Payload access OFF → existing 403 path and disabled-state message; collapse view unaffected.
- Missing/partial node fields → aggregates treat absent numerics as 0; nodes with no parent are roots/orphans, never dropped.
- Lifted edges that collapse to self-edges are dropped, never rendered.
- SSE reconnect/replay unchanged; collapse state is client-local and survives re-renders.

## Out of Scope (YAGNI)

Connected-component clustering as a separate mode; semantic-zoom container nodes; thinking-block capture; persisting collapse state across reloads; any new dependency; changes to pricing or the reducer's conflict resolution.

## Risks

- Default depth may need live tuning to hit ~20–40 visible — mitigated by expressing the policy as a swappable predicate.
- Anchored re-layout math is the fiddliest part — covered by e2e and manual live check on the real session.
- Storing conversation text enlarges the DB — acceptable for local self-observation and gated behind redaction + `--allow-payload-access`.
