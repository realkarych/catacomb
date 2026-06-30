# Checkpoint-scoped subgraph diff — design

**Date:** 2026-06-30
**Status:** Approved (design), implementation pending
**Scope of this spec:** PR1 only (backend core + API + CLI). PR2/PR3 deferred (see Out of scope).

## Problem

Users want to mark **checkpoints (milestones)** at meaningful points during a task
execution and then **diff the execution subgraph delimited by those checkpoints** —
both across two runs ("same phase, baseline vs candidate") and within a single run
("between two checkpoints").

## What already exists (no new work needed here)

- **Checkpoint placement = Phase Markers.** Agents/harnesses already emit markers three
  ways: CLI `catacomb mark --session <id> --name <name> --boundary start|end
  [--occurrence N] [--state-ref S]`, HTTP `POST /v1/mark`, or a `mcp__catacomb__mark`
  tool call carried on the normal trace stream. A start/end pair is synthesized in
  `reduce/marker.go:buildMarker` into **one** `NodeMarker` node
  (`model.PhaseMarkerID(execID, name, occ)`) carrying `TStart`/`TEnd`,
  `PhaseKey = phasekey.Compute(enclosingStepKey, name, occ)`, and `Attrs["open"]=true`
  when the end marker is missing. It also creates `EdgeMarkerSpan` edges to every node
  in its window via `reduce/marker.go:addMarkerSpans`.
- **Diff engine.** `diff.DiffGraphs(an []*model.Node, ae []*model.Edge, bn []*model.Node,
  be []*model.Edge) DiffResult` aligns nodes by `step_key → content → LCS → position`
  and reports `Added/Removed/Changed/Unchanged` with per-node deltas
  (args/status/cost/duration/tokens_in/tokens_out). Exposed via `GET /v1/diff?a=&b=`
  (`daemon/diff.go:handleDiff`), `DiffView.svelte`, and CLI `catacomb diff A.jsonl
  B.jsonl` (`cmd/catacomb/diff.go`).
- **Subgraph-extraction precedent.** `GET /v1/sessions/{hash}/subagent/{agentId}`
  (`daemon/subagent.go`) already slices a subtree and emits it as SSE deltas, keeping
  an edge iff `included[e.Src] && included[e.Dst]`.

**The gap:** the diff operates on whole sessions only. There is no way to scope a diff
(or a view) to the subgraph delimited by a checkpoint/phase. `EdgeMarkerSpan` exists
only for visualization; there is no extraction/query API over it.

## Decisions

Recorded from the brainstorming dialogue:

1. **Placement = live instrumentation.** Checkpoints are emitted during the run via the
   existing marker mechanism. No post-hoc/UI placement and no rule-engine in scope.
2. **Both comparison modes** are wanted: cross-run same-phase, and within-run
   between-checkpoints. Both reduce to one primitive.
3. **Approach = server-side subgraph extraction + reuse `DiffGraphs` unchanged.** The
   diff engine and `DiffResult` shape are not touched.
4. **First cut = backend-first (PR1):** core + API + CLI, no frontend.
5. **Missing phase on one side → explicit error**, not diff-against-empty.

## Architecture

One new primitive, everything else is composition:

```
Subgraph(nodes, edges, window) -> (nodes, edges)
```

| Use case | Expression |
| --- | --- |
| Cross-run, same phase | `DiffGraphs( Subgraph(A, sel), Subgraph(B, sel) )` |
| Within-run, compare phases | `DiffGraphs( Subgraph(S, selA), Subgraph(S, selB) )` |
| Within-run, focus view | `Subgraph(S, sel)` (no diff; render) — PR2 |

Consequence: **zero changes to `DiffGraphs` / `DiffResult`** → existing `DiffView.svelte`
and CLI diff rendering work unchanged, just over a smaller node set.

### Components (each independently testable)

All three live in **one new package `subgraph`** that imports only `model` — so it is
shareable by both the daemon and the CLI, and creates no import cycle (`reduce` imports
`subgraph`, never the reverse):

- `subgraph.Subgraph(nodes []*model.Node, edges []*model.Edge, window Window)
  ([]*model.Node, []*model.Edge)` — pure function.
- **Shared membership predicate** `subgraph.InWindow(n *model.Node, window Window) bool`,
  factored out of `reduce/marker.go:addMarkerSpans` so both `EdgeMarkerSpan` creation and
  `Subgraph` use one source of truth. `reduce/marker.go` imports and calls it.
- **Resolver** `subgraph.PhaseWindow(nodes []*model.Node, execID, name string, occ int)
  (Window, bool)` — pure over a node slice + `execID`; scans for the synthesized marker
  node `model.PhaseMarkerID(execID, name, occ)` (Type `NodeMarker`) and reads its
  `TStart`/`TEnd`. No re-pairing of start/end bounds — `buildMarker` already collapsed
  them. Pure-over-slice (not over `reduce.Graph`) is what lets the CLI reuse it.

Thin daemon handler + CLI wiring call resolver → `Subgraph` → `DiffGraphs`.

`Window` is `{ Start time.Time; End *time.Time }` (`End == nil` ⇒ unbounded right edge,
i.e. an open phase).

## Selector model

- Primary: `name + occurrence`. Resolves to a single marker node per execution → its
  window. Wire format of the query/flag value is `name[,occurrence]`; **`occurrence`
  defaults to `0`** when omitted.
- Alternative (PR2): explicit `from` / `to` markers, for "between two *different*
  checkpoints" within a run. Window `= [fromMarker.TStart, toMarker.TStart]`.

**Cross-run alignment is by `name`, not by `phase_key`.** `phase_key = hash(
enclosingStepKey, name, occ)`; if runs diverge *before* the phase, `enclosingStepKey`
differs → `phase_key` differs, breaking the match exactly where the diff matters most.
So each side selects its phase independently by human-readable `name+occurrence`;
`phase_key` stays an internal marker id (and the handle for the PR2 focus view).
Alignment *inside* the subgraphs is still `DiffGraphs`'s `step_key → …` ladder.

## Membership semantics

Verbatim to the existing `addMarkerSpans` predicate, so behavior cannot drift:

```
n.TStart != nil && !n.TStart.Before(window.Start) &&
  (window.End == nil || !n.TStart.After(*window.End))
```

This is a **closed interval `[Start, End]`** (inclusive on both ends), keyed on `TStart`.
Derived rules:

- **Long node straddling the end** (`TStart` inside, `TEnd` after `End`): **included** —
  membership is by `TStart`, matching `EdgeMarkerSpan`.
- **`TStart == nil`**: excluded.
- **`NodeMarker` nodes and the boundary marker itself**: excluded from the content
  subgraph (they are boundaries, not content).
- **Open phase** (`window.End == nil`): everything from `Start` onward.
- **Nested / overlapping phases**: a node may fall in several windows; `Subgraph` simply
  takes the requested one. No special handling.
- **Induced edges**: keep an edge iff **both** endpoints are in the selected set
  (mirrors `subagent.go`). Dangling edges dropped.

### Invariants

- **`step_key` / `phase_key` are computed in `Snapshot()` from the full-run context, and
  slicing happens *after* `Snapshot()`.** Therefore alignment keys do not shift due to
  extraction — the critical correctness property for cross-run diff.
- **Per-execution resolution is mandatory for correctness.** `executionsForSession(hash)`
  may return multiple executions, and today's `sessionGraphNodes` *concatenates* them into
  flat slices. We must **not** slice the concatenated blob by a single time window: two
  executions can overlap in wall-clock, so a node from execA could fall inside execB's
  window. Therefore the daemon needs a per-execution accessor (e.g.
  `sessionGraphNodesByExecution(hash) -> []{execID, nodes, edges}`); for each execution we
  `PhaseWindow(execNodes, execID, …)` then `Subgraph(execNodes, execEdges, window)`, then
  union the results. "Phase not found" only if the phase is absent in **every** execution.
  (The CLI path is naturally single-execution: `loadGraph` mints one `execID` per input
  file, so each side is one execution — no per-exec fan-out needed there.)
- **Payload stripping** (`!allowPayloadAccess`) is preserved exactly as today.

## Diff integration (PR1)

### API

`daemon/diff.go:handleDiff` today:

```go
aN, aE, err := d.sessionGraphNodes(a)
bN, bE, err := d.sessionGraphNodes(b)
...
result := diff.DiffGraphs(aN, aE, bN, bE)
```

Change: read optional `aPhase` / `bPhase` (`name[,occurrence]`) query params; when
present, run each side's `(nodes, edges)` through the per-execution resolver + `Subgraph`
**before** `DiffGraphs`. Empty params ⇒ current whole-run behavior, byte-for-byte
(backward compatible).

- **Within-run phase compare** needs no new endpoint: `GET /v1/diff?a=H&b=H&aPhase=X&bPhase=Y`.
- **Missing phase** on a requested side ⇒ HTTP 4xx + clear message
  (`phase "X" not found in session B`). A future `--allow-empty` may relax this.

### CLI

`cmd/catacomb/diff.go`: add flags `--phase` (shorthand: both sides equal), `--a-phase`,
`--b-phase`. Same resolver + `Subgraph` units as the API. `runDiff` slices `an/ae` and
`bn/be` after `Snapshot()` before calling `DiffGraphs`. Error text mirrors the API.

## Surfaces & PR decomposition

- **PR1 (this spec):** `Subgraph` + factored predicate (with parity test) + resolver +
  `/v1/diff` phase params + CLI flags. Delivers cross-run same-phase diff **and**
  within-run phase-vs-phase diff (`a=b`) end-to-end, programmatically.
- **PR2 (deferred):** `GET /v1/sessions/{hash}/phase/{name}[?occurrence=]` focus endpoint
  (SSE deltas, mirroring the subagent endpoint); `from`/`to` explicit-marker selection;
  optional `catacomb subgraph <input> --phase X`.
- **PR3 (deferred):** Web UI — phase pickers in `DiffView.svelte`, focus rendering reusing
  the subagent-subtree pattern, optional phase filter in the outline.

## Testing (TDD-first, 100% coverage)

- `Subgraph`: in-window / out-of-window / both boundaries (inclusive) / `nil TStart` /
  `nil End` (open) / dangling-edge drop / overlapping phases.
- **Parity test:** factored predicate yields a byte-identical `EdgeMarkerSpan` set vs the
  current implementation, on existing `reduce/marker_test.go` fixtures.
- Resolver: found / not-found / open phase / occurrence selection / multi-execution union.
- `/v1/diff` handlers: cross-run, within-run (`a=b`), missing phase → error, no phase →
  unchanged behavior.
- CLI: flag parsing, both modes, error text.

## Out of scope

- Post-hoc / UI checkpoint placement; rule-engine auto-checkpoints.
- Any change to `DiffGraphs` / `DiffResult`.
- PR2/PR3 surfaces above.
- The separate, larger deliverable: **comprehensive setup+usage docs for the whole
  catacomb system** (tracked separately; sequenced after this feature).

## Code-touch map (PR1)

| File | Change |
| --- | --- |
| new pkg `subgraph` | `Window`, `InWindow`, `Subgraph`, `PhaseWindow` (imports only `model`). |
| `reduce/marker.go` | `addMarkerSpans` calls `subgraph.InWindow` (parity-preserving refactor). |
| `daemon/diff.go` | Read `aPhase`/`bPhase`; per-exec resolve + slice before `DiffGraphs`; missing-phase error. |
| `daemon/` (accessor) | `sessionGraphNodesByExecution(hash)` returning per-exec `{execID, nodes, edges}`. |
| `cmd/catacomb/diff.go` | `--phase`/`--a-phase`/`--b-phase` flags; resolve + slice before `DiffGraphs`. |
| tests alongside each | Per the Testing section. |
