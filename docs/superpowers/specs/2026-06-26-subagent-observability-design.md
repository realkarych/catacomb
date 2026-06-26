# Subagent observability — design

## Problem

Subagent rows pile up at the top of the outline: empty (zero children),
uncollapsible, parented flat under the session. Root cause: the daemon records
a `subagent` lifecycle marker (from the `SubagentStop` hook and from every
sidechain line in JSONL) parented flat to the session, while a subagent's real
work lives in untailed `…/<session>/subagents/agent-<id>.jsonl` sub-transcripts
(435 for session `157a2d02`). Even when those sub-transcripts are tailed, their
inner work attaches flat under the main session, because:

- the spawn link is **not in the JSONL** — `parent_tool_use_id` is null on every
  sub-transcript line; the only `agentId → tool_use_id` mapping lives in the
  sidecar `agent-<id>.meta.json` (`toolUseId`), which the tailer never reads;
- two performance bugs make ingesting the extra files peg a core (a missing
  steady-state early-out in the tailer, and an `O(P²·T)` reduce).

## Goal

Each subagent becomes one collapsible node nested under the turn that spawned
it, containing its full inner work:

```text
turn → Agent tool_call → subagent → inner prompt → inner turns → inner tools
```

The user's flat tail-scope keeps working with no setup change, and there is no
CPU regression.

## Design

### Ingestion (tailer + jsonl + daemon)

- The tailer discovers, for each tailed main transcript, its sibling
  `<session>/subagents/agent-*.jsonl` resolved through the symlink, so the flat
  tail-scope picks them up automatically. The existing
  `*/*/subagents/agent-*.jsonl` glob (project-dir scopes) is retained.
- For each agent transcript the tailer reads the sidecar
  `agent-<id>.meta.json` once → `{toolUseId, agentType, description}` and emits a
  single subagent-meta observation through a new `Sink.IngestSubagentMeta`. The
  daemon maps it onto the main session's execution graph (sub-transcripts
  already carry the main `sessionId`).
- `jsonl.decodeLine` sets `base.AgentID = ln.AgentID`, so every node parsed from
  a sub-transcript line (prompt / turn / tool) carries its `AgentID`.

### Reduce (nesting + correctness + perf)

- The subagent-meta observation reuses `Correlation.ParentToolUseID` to carry
  the meta `toolUseId`. `applySubagent` gives the subagent node ONE reparent-safe
  structural parent: the `Agent` tool_call (`ToolCallID(exec, toolUseId)`) when
  known, else the session — order-independent, deleting the stale edge on
  upgrade. It also sets `SubagentType` (from `agentType`) and `Name` (from
  `description`).
- `applyUser` parents an inner-root prompt (`Correlation.AgentID != ""`) under
  `SubagentID(exec, AgentID)` instead of the session.
- Turn / prompt parenting becomes **agent-scoped**: a turn parents under the
  preceding prompt *of the same `AgentID`* (the main session is the `AgentID==""`
  group), so a subagent's turns never attach to a foreign prompt. This is what
  nests inner turns under the inner prompt under the subagent.
- The same rewrite makes parenting **near-linear**: prompts are grouped by
  `AgentID` and kept sorted by `seq`, so the preceding prompt is found by binary
  search and a new prompt re-parents only the affected turns in its own group.
  Today `recordPrompt` rescans every turn on every prompt (`O(P²·T)`); with
  subagents inflating `P` and `T` this pegs a core.

### Perf (tailer steady-state)

- `pollFile` early-outs when `size` and `mtime` match the persisted cursor
  (fields already stored, currently write-only), so the ~435 idle
  sub-transcripts cost one `stat` per tick instead of read + sha256 (×2) + a DB
  write. Truncation and rotation stay detected via the existing
  `size < offset` and head-fingerprint checks.

### Frontend

None required for nesting — the outline is a generic, virtualized,
auto-collapsing indented tree driven purely by `parent_child` edges. One small
polish: `outlineLabel` for a `subagent` shows its `Name` (the meta
`description`, e.g. "Review PR1 reparent") when present, falling back to
`subagent_type` — directly answering the "zero context" complaint.

## Redaction / safety

Unchanged. Inner content is served only through the existing gated,
redaction-aware `/payload`. The meta `description` is a short task label authored
by the parent agent; it flows through normal node fields with no `{@html}`.

## Testing

- Go: 100% via `make cover`, TDD-first. New unit tests for the tailer skip,
  sibling-discovery + meta read, jsonl `AgentID` threading, and the
  agent-scoped / near-linear reduce parenting (a large interleaved-input
  correctness case plus a main-session-unchanged regression). No Go comments.
- Frontend: vitest 100% on `outlineLabel`; Playwright live-verify on session
  `157a2d02` (subagents nested, collapsible, labelled; CPU returns to idle after
  ingest).
