# M1b — Four-source precedence + ADR-0014 cascade/superseded + Rev population Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the multi-source MERGE in `reduce/` correct and order-independent: per-field source precedence (timing/name/tokens), the #53954 conditional-structure gate, the transitive status cascade with the `superseded` status, and `Rev` population on nodes and edges — all proven by a reduction commutativity property test.

**Architecture:** All work lands in the `reduce/` package plus one constant in `model/`. A new `sourceRank` helper encodes ONLY the two live sources' ordering (OTel > hook) for the field groups that actually conflict today (timing, tokens). Per-node field-group stamps `(rank, seq)` are stored in a side map on `Graph` (NOT on `model.Node`, which is the wire/store shape) so the merge is a pure function of `(rank, seq)` and never of application order. The existing `closeOpenDescendants` is generalized into `cascadeStatus(rootID, status)` with a "skip rank-3 genuine terminals" guard and a `cancel_cause` attribute; existing call sites keep identical behavior. `Rev` is populated at the single central `stamp` seam (nodes) and in `upsertEdge` (edges). No new ingress, no CDC, no exporter — those are M1c.

**Tech Stack:** Go 1.26, pure-Go, `testify` (`assert`/`require`), white-box `package reduce` tests.

## Global Constraints

- Go 1.26 pure-Go (`modernc.org/sqlite`, no cgo).
- **NO comments** in Go except `//go:build`, `//go:embed`, `//go:generate` (`internal/codepolicy`).
- **100% line coverage** under `-race` (`make cover`) — every new branch reached by a genuine assertion-bearing test; **NO dead code** for absent M2 sources (JSONL/stream-json) — unreachable branches forbidden by YAGNI + 100% coverage.
- `golangci-lint v2` clean (gofumpt extra-rules, goimports local-prefix `github.com/realkarych/catacomb`, govet shadow, **forbidigo bans `time.Sleep`**, unparam, errcheck, rowserrcheck, bodyclose, gocritic, revive, staticcheck).
- **NEVER `go mod tidy`** (binding repo rule).
- Single-mutex daemon; observation log is the system of record (reducer is a deterministic, COMMUTATIVE projection); cross-platform; all test files white-box (`package reduce`).
- Step 7 (live field-name verification) is deferred operator testing — NOT a task here.
- **unparam gotcha (seen in M1a):** an unexported func/param that always receives one constant across call sites gets flagged — vary test inputs at the root, never add a `.golangci.yml` exclusion.
- **markdownlint gotcha:** ensure blank lines around every list and fenced code block (MD031/MD032) so CI's markdownlint passes.

---

## Current code: exact signatures found (authoritative anchors)

These are the present-tree signatures the tasks below modify. Cite them; do not guess.

- `reduce/reduce.go:226` — `func rank(s model.Status) int` (covers `ok`/`error`/`blocked`→3, `cancelled`/`unknown`→2, `running`→1, default→0).
- `reduce/reduce.go:239` — `func resolveStatus(cur, next model.Status) model.Status`.
- `reduce/reduce.go:98` — `func (g *Graph) stamp(n *model.Node, o model.Observation)` (sets earliest `TStart`, appends `SourceRef`). Called from 7 sites (lines 22, 26, 38, 42, 50, 68, 85).
- `reduce/reduce.go:123` — `func applyTokens(n *model.Node, attrs map[string]any)` (overwrites `TokensIn`/`TokensOut` unconditionally). One caller: line 43 (`assistant_turn`).
- `reduce/reduce.go:193` — `func (g *Graph) closeOpenDescendants(rootID string)` (BFS over `parent_child`, calls `closeIfOpen(c, model.StatusUnknown)`).
- `reduce/reduce.go:216` — `func (g *Graph) closeIfOpen(id string, status model.Status)` (sets status only if currently `running`/`pending`).
- `reduce/graph.go:32` — `func (g *Graph) upsertEdge(executionID, runID, src, dst string)` (creates `parent_child` edge if absent; no re-parent, no `Rev` today).
- `reduce/graph.go:5` — `type Graph struct { Nodes map[string]*model.Node; Edges map[string]*model.Edge; Runs map[string]*model.Run }`.
- `reduce/graph.go:23` — `func (g *Graph) node(id, runID string, t model.NodeType) *model.Node`.
- `model/model.go:114` — `type Edge struct { ID, RunID string; Type EdgeType; Src, Dst string; Attrs map[string]any; Rev uint64 }` (**`Rev` already exists** from M1a; M1b only populates it).
- `model/model.go:41` — `Status` consts present: `StatusPending`, `StatusRunning`, `StatusOK`, `StatusError`, `StatusBlocked`, `StatusCancelled`, `StatusUnknown`, `StatusAbandoned` (**8 of 9**; `StatusSuperseded` is the only one MISSING).

---

## Key design decision: the per-field stamp representation

The commutativity property (spec §8 M1b) demands every field merge be a pure function of `(source-rank, seq)`, never of arrival order. The lightest representation that satisfies this for the TWO live sources without dead code:

- A side map on `Graph`: `stamps map[string]*fieldStamps`, keyed by node ID. `fieldStamps` holds one `(rank, seq)` pair PER conflicting field group: `timing` and `name`. It is NOT stored on `model.Node` (that struct is the store/wire shape and must stay clean) and is NOT snapshotted.
- A `winsField(cur *stamp, rank int, seq uint64) bool` decision: the incoming write wins iff `rank > cur.rank`, or (`rank == cur.rank` and the tiebreak for that group is satisfied). Timing tiebreak = higher `seq` wins (latest within a source); name tiebreak = LOWER `seq` wins ("first non-empty, tiebreak seq" → the lowest-seq non-empty name is canonical, so a later-applied lower-seq name MUST overwrite).
- A `sourceRank(s model.Source) int` helper encoding ONLY live ordering: `SourceOTel → 1`, `SourceHook → 0`. JSONL/stream-json are NOT branched (no live producer in M1 → would be dead code); they are deferred (see below) and naturally rank 0 via the default only because a test never exercises them — to avoid an unreached default branch, `sourceRank` is written with explicit cases for the two live sources and a `default` that a single targeted test drives with a sentinel source, OR (chosen) a two-case switch returning `1` for OTel and `0` otherwise, with the `0` arm exercised by hook inputs. The latter has no unreached arm.
- Tokens use NO stamp: per §5.1 tokens are OTel-won and there is no second live token producer to conflict, so `applyTokens` gains a single `o.Source == model.SourceOTel` guard (hook token writes are dropped once any OTel value can arrive; with hook-only, hooks still set tokens because the existing `assistant_turn` test supplies them as hook — see Task 5 for the exact rule that stays commutative AND keeps existing tests green).

This keeps the merge a pure function of `(rank, seq)` per group, is correct for hook+OTel, and adds zero branches for absent sources.

**§5.1 rows DEFERRED as no-live-producer (NOT implemented; would be dead code):**

- Structure row's JSONL and stream-json tiers (only OTel-conditional + hook heuristics are live; the #53954 gate in Task 3 covers the OTel/hook decision).
- Timing row's JSONL and stream-json tiers (only OTel > hook is live).
- Cost/tokens row's stream-json `result.usage` and JSONL tiers (only OTel is live; hook tokens handled per Task 5).
- Payload row's stream-json deltas (only hook/full-payload merge is live; `mergePayload` is unchanged).
- MCP-attrs row's OTel `mcp_*` tier and name-pattern parse beyond the existing `mcp__` prefix (only the existing hook `name`-prefix routing is live).
- Status row is governed by the lattice (§5.2), not this table.

---

## Commutativity scope (read before Task 5)

The commutativity the merge guarantees, and the property `TestReductionCommutativity` actually proves, is **field-merge order-independence**: for any node reached by a fixed set of observations, its `TStart`/name/tokens/`Rev`/status are a pure function of the observations' `(source-rank, seq)`, never of application order. This is the substantive M1b property and it holds unconditionally.

Two derived mechanisms are deliberately **seq-order-sensitive** (NOT order-independent in the general case), which is correct by construction because the reducer is an event-sourced fold ALWAYS replayed in `seq` order — both the live append path and `ApplyAll` reload (`ObservationsForExecution ... ORDER BY seq`):

- **#53954 gate (Task 3):** `spanChildren` is built incrementally, so a flat OTel parent span processed before its child would skip its edge while the reverse order would create it. At runtime this never happens: OTel spans export when they END, so a parent span's observation always has a higher `seq` than its children — children are recorded in `spanChildren` first. The gate is therefore correct for the only order the system ever produces.
- **Cancelled/superseded cascade (Task 4):** it walks edges present at trigger time. In M1 this is effectively dormant — `Apply` creates only session→node and turn→tool edges (cancelled nodes are tool leaves with no `Apply`-created children); real tool→tool parent edges arrive with M2's `parent_tool_use_id`. The cascade code is exercised via manually-constructed edges in its dedicated tests.

`TestReductionCommutativity` therefore uses a **structure-stable** observation set (the OTel tool carries a `tool_use_id` so its edge does not depend on `spanChildren`; the cancelled tool has no children) so that all 720 orders yield identical structure AND identical field merges — isolating the field-merge property it is meant to prove. The #53954 gate and cascade have their own dedicated `seq`-ordered tests (Tasks 3, 4). Do NOT "strengthen" the commutativity set with structure that depends on arrival order — that would assert a property the event-sourced design intentionally does not provide.

---

## Task 1: Status lattice — `StatusSuperseded` const + `rank()` extension

**Files:**

- Modify: `model/model.go:41-50` (add `StatusSuperseded`)
- Modify: `reduce/reduce.go:226-237` (`rank` adds `superseded`/`abandoned` → 2)
- Test: `reduce/reduce_test.go`

**Interfaces:**

- Consumes: `model.Status`, existing `model.Status*` consts.
- Produces: `model.StatusSuperseded model.Status = "superseded"`; `rank(model.StatusSuperseded) == 2`, `rank(model.StatusAbandoned) == 2`.

- [ ] **Step 1: Write the failing tests** in `reduce/reduce_test.go`

```go
func TestRankSupersededAndAbandonedAreProvisional(t *testing.T) {
	assert.Equal(t, 2, rank(model.StatusSuperseded))
	assert.Equal(t, 2, rank(model.StatusAbandoned))
}

func TestResolveStatusSupersededOverRunningButUnderTerminal(t *testing.T) {
	assert.Equal(t, model.StatusSuperseded, resolveStatus(model.StatusRunning, model.StatusSuperseded))
	assert.Equal(t, model.StatusOK, resolveStatus(model.StatusSuperseded, model.StatusOK))
	assert.Equal(t, model.StatusSuperseded, resolveStatus(model.StatusSuperseded, model.StatusUnknown))
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./reduce/ -run 'TestRank|TestResolveStatusSuperseded' -v`
Expected: FAIL — `model.StatusSuperseded` undefined; `rank` returns 0 for `superseded`.

- [ ] **Step 3: Add the const** in `model/model.go`

```go
	StatusUnknown   Status = "unknown"
	StatusSuperseded Status = "superseded"
	StatusAbandoned Status = "abandoned"
```

- [ ] **Step 4: Extend `rank`** in `reduce/reduce.go`

```go
func rank(s model.Status) int {
	switch s {
	case model.StatusOK, model.StatusError, model.StatusBlocked:
		return 3
	case model.StatusCancelled, model.StatusUnknown, model.StatusSuperseded, model.StatusAbandoned:
		return 2
	case model.StatusRunning:
		return 1
	default:
		return 0
	}
}
```

- [ ] **Step 5: Run to verify it passes**

Run: `go test ./reduce/ -run 'TestRank|TestResolveStatusSuperseded' -v`
Expected: PASS.

- [ ] **Step 6: Full gate**

Run: `make cover && make lint`
Expected: 100% coverage for `reduce` + `model`; lint 0.

- [ ] **Step 7: Commit**

```bash
git add model/model.go reduce/reduce.go reduce/reduce_test.go
git commit -m "feat(reduce): status lattice adds superseded; rank covers superseded/abandoned"
```

---

## Task 2: `Rev` population on nodes and edges

**Files:**

- Modify: `reduce/reduce.go:98-104` (`stamp` sets `n.Rev`)
- Modify: `reduce/graph.go:32-40` (`upsertEdge` sets `e.Rev`)
- Test: `reduce/reduce_test.go`

**Interfaces:**

- Consumes: `model.Observation.Seq`, `model.Node.Rev`, `model.Edge.Rev`.
- Produces: every node touched via `stamp` has `n.Rev == max(n.Rev, o.Seq)`; every edge has `e.Rev == o.Seq` on create and `max(e.Rev, o.Seq)` on re-touch. `upsertEdge` signature gains a trailing `seq uint64` parameter.

**Note on `upsertEdge` signature change:** all 6 existing call sites must pass `o.Seq` (or the relevant seq). The new signature is `func (g *Graph) upsertEdge(executionID, runID, src, dst string, seq uint64)`. unparam will flag a `seq` that is always the same constant — the commutativity test in Task 5 and the edge-Rev test below vary it, so vary `seq` across calls.

- [ ] **Step 1: Write the failing tests** in `reduce/reduce_test.go`

```go
func TestNodeRevTracksMaxSeq(t *testing.T) {
	g := NewGraph()
	a := toolObs("e1", "s1", "t1", "Bash", "running", 3)
	b := toolObs("e1", "s1", "t1", "Bash", "ok", 7)
	c := toolObs("e1", "s1", "t1", "Bash", "ok", 5)
	g.ApplyAll([]model.Observation{a, b, c})
	assert.Equal(t, uint64(7), g.Nodes[model.ToolCallID("e1", "t1")].Rev)
}

func TestEdgeRevTracksMaxSeq(t *testing.T) {
	g := NewGraph()
	g.upsertEdge("e1", "s1", model.SessionNodeID("e1"), "x", 4)
	g.upsertEdge("e1", "s1", model.SessionNodeID("e1"), "x", 2)
	g.upsertEdge("e1", "s1", model.SessionNodeID("e1"), "x", 9)
	id := model.EdgeID("e1", model.EdgeParentChild, model.SessionNodeID("e1"), "x")
	assert.Equal(t, uint64(9), g.Edges[id].Rev)
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./reduce/ -run 'TestNodeRev|TestEdgeRev' -v`
Expected: FAIL — `Rev` is 0; and `upsertEdge` arity mismatch (compile error) once the test passes 5 args.

- [ ] **Step 3: Populate `n.Rev` in `stamp`** (`reduce/reduce.go`)

```go
func (g *Graph) stamp(n *model.Node, o model.Observation) {
	if n.TStart == nil || o.EventTime.Before(*n.TStart) {
		ts := o.EventTime
		n.TStart = &ts
	}
	if o.Seq > n.Rev {
		n.Rev = o.Seq
	}
	n.Sources = append(n.Sources, model.SourceRef{Source: o.Source, ObsID: o.ObsID, ObservedAt: o.ObservedAt})
}
```

- [ ] **Step 4: Populate `e.Rev` in `upsertEdge`** (`reduce/graph.go`)

```go
func (g *Graph) upsertEdge(executionID, runID, src, dst string, seq uint64) {
	if src == "" || dst == "" {
		return
	}
	id := model.EdgeID(executionID, model.EdgeParentChild, src, dst)
	e, ok := g.Edges[id]
	if !ok {
		g.Edges[id] = &model.Edge{ID: id, RunID: runID, Type: model.EdgeParentChild, Src: src, Dst: dst, Rev: seq}
		return
	}
	if seq > e.Rev {
		e.Rev = seq
	}
}
```

- [ ] **Step 5: Update all `upsertEdge` call sites** to pass `o.Seq`

In `reduce/reduce.go`: lines in `Apply` (`user_prompt`, `marker`), `applyTool`, `applySubagent` — add `o.Seq` as the final argument, e.g.:

```go
		g.upsertEdge(o.ExecutionID, o.RunID, model.SessionNodeID(o.ExecutionID), n.ID, o.Seq)
```

and in `applyTool`:

```go
	g.upsertEdge(o.ExecutionID, o.RunID, parent, id, o.Seq)
```

Also fix the existing test call sites in `reduce/reduce_test.go` (`TestUpsertEdgeEmptyGuard`, `TestCloseOpenDescendantsHandlesDiamond`, `TestCloseOpenDescendantsSkipsMissingNode`) to pass a seq argument (use varying values, e.g. `1`, `2`, `3`, to keep unparam satisfied).

- [ ] **Step 6: Run to verify it passes**

Run: `go test ./reduce/ -run 'TestNodeRev|TestEdgeRev|TestUpsertEdge|TestCloseOpenDescendants' -v`
Expected: PASS.

- [ ] **Step 7: Full gate**

Run: `make cover && make lint`
Expected: 100%; lint 0.

- [ ] **Step 8: Commit**

```bash
git add reduce/reduce.go reduce/graph.go reduce/reduce_test.go
git commit -m "feat(reduce): populate Node.Rev (stamp) and Edge.Rev (upsertEdge, seq-aware)"
```

---

## Task 3: `spanChildren` map + ADR-0014 #53954 gate in `upsertEdge`

**Files:**

- Modify: `reduce/graph.go:5-13` (`Graph.spanChildren`, `NewGraph`)
- Modify: `reduce/reduce.go` (record `spanChildren` in `Apply`; gate the OTel `parent_child` edge in `applyTool`)
- Test: `reduce/reduce_test.go`

**Interfaces:**

- Consumes: `model.Observation.Source`, `model.Correlation.SpanID`/`ParentSpanID`/`ToolUseID`, `model.SourceOTel`.
- Produces: `Graph.spanChildren map[string]bool` (key = parent `span_id`); a gated edge path `upsertEdgeGated(o, src, dst)` that applies the §5.3 rule before delegating to `upsertEdge`.

**Design:** The gate concerns OTel observations carrying a `ParentSpanID`. With the current reducer, OTel tool observations flow through `applyTool`, whose parent is derived from `MessageID`/session (NOT from `ParentSpanID`). The gate's job per §5.3 is: when an OTel observation would create a `parent_child` edge AND it has a `ParentSpanID`, skip the edge unless (a) some OTel child span was observed for this observation's own `SpanID`, or (b) it carries a `ToolUseID`. Hook observations and OTel observations without `ParentSpanID` are never gated (edge always created). `spanChildren[ParentSpanID] = true` is recorded for EVERY observation (any source) that has a non-empty `ParentSpanID`, so the map reflects "this span_id has at least one observed child". The check then consults `spanChildren[o.Correlation.SpanID]` (does THIS span have children?).

- [ ] **Step 1: Write the failing tests** in `reduce/reduce_test.go`

```go
func otelTool(exec, runID, toolUse, span, parentSpan string, seq uint64) model.Observation {
	return model.Observation{
		ObsID: "o" + strconv.FormatUint(seq, 10), RunID: runID, ExecutionID: exec,
		Source: model.SourceOTel, Kind: "assistant_tool_use",
		Correlation: model.Correlation{SessionID: runID, ToolUseID: toolUse, SpanID: span, ParentSpanID: parentSpan},
		Attrs:       map[string]any{"name": "Bash"},
		EventTime:   time.Unix(int64(seq), 0).UTC(), Seq: seq,
	}
}

func TestSpanChildrenRecordedForAnyParentSpan(t *testing.T) {
	g := NewGraph()
	g.Apply(otelTool("e1", "s1", "t1", "spanChild", "spanParent", 1))
	assert.True(t, g.spanChildren["spanParent"])
}

func TestGateAcceptsOTelEdgeWhenToolUseIDPresent(t *testing.T) {
	g := NewGraph()
	o := otelTool("e1", "s1", "tA", "spanA", "spanRoot", 1)
	g.Apply(o)
	tool := model.ToolCallID("e1", "tA")
	require.Contains(t, g.Edges, model.EdgeID("e1", model.EdgeParentChild, model.SessionNodeID("e1"), tool))
}

func TestGateSkipsOTelEdgeWhenNoChildrenAndNoToolUseID(t *testing.T) {
	g := NewGraph()
	o := model.Observation{
		ObsID: "o1", RunID: "s1", ExecutionID: "e1", Source: model.SourceOTel, Kind: "assistant_tool_use",
		Correlation: model.Correlation{SessionID: "s1", ToolUseID: "", SpanID: "spanFlat", ParentSpanID: "spanRoot"},
		Attrs:       map[string]any{"name": "Bash"}, EventTime: time.Unix(1, 0).UTC(), Seq: 1,
	}
	g.Apply(o)
	tool := model.ToolCallID("e1", "")
	assert.NotContains(t, g.Edges, model.EdgeID("e1", model.EdgeParentChild, model.SessionNodeID("e1"), tool))
}

func TestGateAcceptsOTelEdgeWhenSpanHasObservedChild(t *testing.T) {
	g := NewGraph()
	g.Apply(otelTool("e1", "s1", "tChild", "spanInner", "spanMid", 1))
	o := model.Observation{
		ObsID: "o2", RunID: "s1", ExecutionID: "e1", Source: model.SourceOTel, Kind: "assistant_tool_use",
		Correlation: model.Correlation{SessionID: "s1", ToolUseID: "", SpanID: "spanMid", ParentSpanID: "spanRoot"},
		Attrs:       map[string]any{"name": "Read"}, EventTime: time.Unix(2, 0).UTC(), Seq: 2,
	}
	g.Apply(o)
	tool := model.ToolCallID("e1", "")
	assert.Contains(t, g.Edges, model.EdgeID("e1", model.EdgeParentChild, model.SessionNodeID("e1"), tool))
}

func TestGateNeverAppliesToHookEdges(t *testing.T) {
	g := NewGraph()
	g.Apply(toolObs("e1", "s1", "", "Bash", "running", 1))
	tool := model.ToolCallID("e1", "")
	assert.Contains(t, g.Edges, model.EdgeID("e1", model.EdgeParentChild, model.SessionNodeID("e1"), tool))
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./reduce/ -run 'TestSpanChildren|TestGate' -v`
Expected: FAIL — `g.spanChildren` undefined; flat OTel edge wrongly created.

- [ ] **Step 3: Add the map** in `reduce/graph.go`

```go
type Graph struct {
	Nodes        map[string]*model.Node
	Edges        map[string]*model.Edge
	Runs         map[string]*model.Run
	spanChildren map[string]bool
}

func NewGraph() *Graph {
	return &Graph{Nodes: map[string]*model.Node{}, Edges: map[string]*model.Edge{}, Runs: map[string]*model.Run{}, spanChildren: map[string]bool{}}
}
```

- [ ] **Step 4: Record `spanChildren` + add the gated edge helper** in `reduce/reduce.go`

At the top of `Apply`, after `g.ensureRun(o)` and before the session node, record the parent-span observation:

```go
func (g *Graph) Apply(o model.Observation) {
	g.ensureRun(o)
	if o.Correlation.ParentSpanID != "" {
		g.spanChildren[o.Correlation.ParentSpanID] = true
	}
	g.node(model.SessionNodeID(o.ExecutionID), o.RunID, model.NodeSession)
```

Add the gated helper:

```go
func (g *Graph) upsertEdgeGated(o model.Observation, src, dst string) {
	if o.Source == model.SourceOTel && o.Correlation.ParentSpanID != "" {
		if !g.spanChildren[o.Correlation.SpanID] && o.Correlation.ToolUseID == "" {
			return
		}
	}
	g.upsertEdge(o.ExecutionID, o.RunID, src, dst, o.Seq)
}
```

- [ ] **Step 5: Route `applyTool`'s edge through the gate** (`reduce/reduce.go`)

```go
	parent := model.SessionNodeID(o.ExecutionID)
	if o.Correlation.MessageID != "" {
		parent = model.AssistantTurnID(o.ExecutionID, o.Correlation.MessageID)
	}
	g.upsertEdgeGated(o, parent, id)
```

Leave `user_prompt`, `marker`, `applySubagent` on the plain `upsertEdge(..., o.Seq)` — those are hook/session-structural edges, never OTel-span-gated (a `subagent_stop` OTel observation has no `ParentSpanID` in the live mapping; if one ever did, it would carry `ToolUseID == ""` and no children and SHOULD still attach to session — the gate is scoped to tool edges only, matching §5.3's "tool spans always carry `tool_use_id`").

- [ ] **Step 6: Run to verify it passes**

Run: `go test ./reduce/ -run 'TestSpanChildren|TestGate' -v`
Expected: PASS.

- [ ] **Step 7: Full gate**

Run: `make cover && make lint`
Expected: 100%; lint 0.

- [ ] **Step 8: Commit**

```bash
git add reduce/graph.go reduce/reduce.go reduce/reduce_test.go
git commit -m "feat(reduce): spanChildren map + ADR-0014 #53954 conditional OTel edge gate"
```

---

## Task 4: `cascadeStatus` generalization (+ `cancel_cause`, existing call sites unchanged)

**Files:**

- Modify: `reduce/reduce.go:193-214` (replace `closeOpenDescendants` with `cascadeStatus`)
- Modify: `reduce/reduce.go:30,180` (call sites pass `model.StatusUnknown`)
- Modify: `reduce/reduce.go` (`applyTool` triggers cascade on `cancelled`/`superseded`)
- Test: `reduce/reduce_test.go`

**Interfaces:**

- Consumes: `g.Edges` (`parent_child`), `rank`, `closeIfOpen`, `model.Status`.
- Produces: `func (g *Graph) cascadeStatus(rootID string, status model.Status)` — BFS from `rootID`; for each reachable descendant with `rank(status) >= 2` set `descendant.Status = status` + `descendant.Attrs["cancel_cause"] = rootID` when `rank(descendant.Status) < 3`; the `StatusUnknown` (existing) path preserves identical observable behavior via `closeIfOpen`.

**Design — preserving existing behavior exactly:** The current `closeOpenDescendants` calls `closeIfOpen(c, StatusUnknown)`, which only sets status when the descendant is `running`/`pending` (rank ≤ 1) — and it sets NO `cancel_cause`. The generalized `cascadeStatus` must keep that EXACT behavior when called with `StatusUnknown` (session_end/run_ended), while a `cancelled`/`superseded` trigger uses the broader "skip rank-3 terminals" guard AND sets `cancel_cause`. To avoid two behaviors diverging into dead branches, model both via one guard: descendants are updated iff `rank(descendant.Status) < 3` AND the incoming `status` actually wins per `resolveStatus`. For `StatusUnknown` over a `running`/`pending` node this is identical to today (unknown rank 2 ≥ running rank 1); for a node already `unknown`/`cancelled` (rank 2) `resolveStatus(unknown, unknown)` returns `unknown` (tie→next) — same value, no observable change — but the existing call sites only ever hit `running`/`pending` descendants in tests, so to keep coverage genuine the `cancel_cause` write is gated on the status being `cancelled` or `superseded` (the cascade triggers), NOT `unknown` (the close triggers). This keeps the `StatusUnknown` path byte-identical (no `cancel_cause` key) and gives the cascade path its own asserted branch.

- [ ] **Step 1: Write the failing tests** in `reduce/reduce_test.go`

```go
func TestCascadeStatusCancelsNonTerminalDescendants(t *testing.T) {
	g := NewGraph()
	g.node(model.SessionNodeID("e1"), "s1", model.NodeSession)
	g.node("root", "s1", model.NodeToolCall).Status = model.StatusRunning
	g.node("childRun", "s1", model.NodeToolCall).Status = model.StatusRunning
	g.node("childDone", "s1", model.NodeToolCall).Status = model.StatusOK
	g.upsertEdge("e1", "s1", "root", "childRun", 1)
	g.upsertEdge("e1", "s1", "root", "childDone", 2)
	g.cascadeStatus("root", model.StatusCancelled)
	assert.Equal(t, model.StatusCancelled, g.Nodes["childRun"].Status)
	assert.Equal(t, "root", g.Nodes["childRun"].Attrs["cancel_cause"])
	assert.Equal(t, model.StatusOK, g.Nodes["childDone"].Status)
	_, hasCause := g.Nodes["childDone"].Attrs["cancel_cause"]
	assert.False(t, hasCause)
}

func TestCascadeStatusSupersededSetsCause(t *testing.T) {
	g := NewGraph()
	g.node("root", "s1", model.NodeToolCall).Status = model.StatusRunning
	g.node("child", "s1", model.NodeToolCall).Status = model.StatusRunning
	g.upsertEdge("e1", "s1", "root", "child", 1)
	g.cascadeStatus("root", model.StatusSuperseded)
	assert.Equal(t, model.StatusSuperseded, g.Nodes["child"].Status)
	assert.Equal(t, "root", g.Nodes["child"].Attrs["cancel_cause"])
}

func TestCascadeUnknownPathHasNoCancelCause(t *testing.T) {
	g := NewGraph()
	g.node(model.SessionNodeID("e1"), "s1", model.NodeSession)
	g.node("child", "s1", model.NodeToolCall).Status = model.StatusRunning
	g.upsertEdge("e1", "s1", model.SessionNodeID("e1"), "child", 1)
	g.cascadeStatus(model.SessionNodeID("e1"), model.StatusUnknown)
	assert.Equal(t, model.StatusUnknown, g.Nodes["child"].Status)
	_, hasCause := g.Nodes["child"].Attrs["cancel_cause"]
	assert.False(t, hasCause)
}

func TestToolResultCancelledCascadesToChildren(t *testing.T) {
	g := NewGraph()
	parent := toolObs("e1", "s1", "tp", "Task", "running", 1)
	parent.Correlation.MessageID = "m1"
	g.Apply(parent)
	child := toolObs("e1", "s1", "tc", "Bash", "running", 2)
	child.Correlation.MessageID = ""
	g.Apply(child)
	g.upsertEdge("e1", "s1", model.ToolCallID("e1", "tp"), model.ToolCallID("e1", "tc"), 3)
	cancel := toolObs("e1", "s1", "tp", "Task", string(model.StatusCancelled), 4)
	cancel.Correlation.MessageID = "m1"
	g.Apply(cancel)
	assert.Equal(t, model.StatusCancelled, g.Nodes[model.ToolCallID("e1", "tc")].Status)
	assert.Equal(t, model.ToolCallID("e1", "tp"), g.Nodes[model.ToolCallID("e1", "tc")].Attrs["cancel_cause"])
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./reduce/ -run 'TestCascade|TestToolResultCancelled' -v`
Expected: FAIL — `cascadeStatus` undefined; no `cancel_cause`; no cascade trigger.

- [ ] **Step 3: Replace `closeOpenDescendants` with `cascadeStatus`** (`reduce/reduce.go`)

```go
func (g *Graph) cascadeStatus(rootID string, status model.Status) {
	children := map[string][]string{}
	for _, e := range g.Edges {
		if e.Type == model.EdgeParentChild {
			children[e.Src] = append(children[e.Src], e.Dst)
		}
	}
	seen := map[string]bool{rootID: true}
	queue := []string{rootID}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, c := range children[cur] {
			if seen[c] {
				continue
			}
			seen[c] = true
			queue = append(queue, c)
			g.applyCascade(c, rootID, status)
		}
	}
}

func (g *Graph) applyCascade(id, rootID string, status model.Status) {
	if status == model.StatusUnknown {
		g.closeIfOpen(id, status)
		return
	}
	n := g.Nodes[id]
	if n == nil || rank(n.Status) >= 3 {
		return
	}
	n.Status = resolveStatus(n.Status, status)
	if n.Attrs == nil {
		n.Attrs = map[string]any{}
	}
	n.Attrs["cancel_cause"] = rootID
}
```

- [ ] **Step 4: Update the two existing call sites** (`reduce/reduce.go`)

In `session_end` (was `g.closeOpenDescendants(n.ID)`):

```go
		g.cascadeStatus(n.ID, model.StatusUnknown)
```

In `applyRunEnded` (was `g.closeOpenDescendants(model.SessionNodeID(o.ExecutionID))`):

```go
	g.closeIfOpen(model.SessionNodeID(o.ExecutionID), model.StatusUnknown)
	g.cascadeStatus(model.SessionNodeID(o.ExecutionID), model.StatusUnknown)
```

- [ ] **Step 5: Trigger the cascade in `applyTool`** when status becomes `cancelled`/`superseded` (`reduce/reduce.go`)

Replace the status block in `applyTool`:

```go
	if s, ok := o.Attrs["status"].(string); ok {
		n.Status = resolveStatus(n.Status, model.Status(s))
		if n.Status == model.StatusCancelled || n.Status == model.StatusSuperseded {
			g.cascadeStatus(n.ID, n.Status)
		}
	}
```

- [ ] **Step 6: Rename existing tests' calls** — the three tests at `reduce/reduce_test.go` referencing `closeOpenDescendants` (`TestCloseOpenDescendantsHandlesDiamond`, `TestCloseOpenDescendantsSkipsMissingNode`, `TestCloseOpenDescendantsIgnoresNonParentChild`) now call `g.cascadeStatus(<root>, model.StatusUnknown)`. Update each invocation; assertions stay identical (they assert `StatusUnknown` / unchanged).

- [ ] **Step 7: Run to verify it passes**

Run: `go test ./reduce/ -run 'TestCascade|TestToolResultCancelled|TestCloseOpenDescendants|TestSessionEnd|TestRunEnded' -v`
Expected: PASS — existing session_end/run_ended behavior unchanged; new cascade asserted.

- [ ] **Step 8: Full gate**

Run: `make cover && make lint`
Expected: 100%; lint 0. (Note: CDC `DeltaNodeStatus` is NOT emitted — that is M1c.)

- [ ] **Step 9: Commit**

```bash
git add reduce/reduce.go reduce/reduce_test.go
git commit -m "feat(reduce): cascadeStatus generalizes closeOpenDescendants (+cancel_cause, cancelled/superseded trigger)"
```

---

## Task 5: Per-field source precedence (timing/name/tokens) + commutativity property test

**Files:**

- Modify: `reduce/graph.go:5-13` (`Graph.stamps`, `NewGraph`)
- Modify: `reduce/reduce.go` (`sourceRank`, `fieldStamps`, stamp-aware timing + name merge, `applyTokens` Source guard)
- Test: `reduce/reduce_test.go`

**Interfaces:**

- Consumes: `model.Source`, `model.Observation.{Source,Seq,EventTime,Attrs}`, `model.Node`.
- Produces:
  - `func sourceRank(s model.Source) int` — `SourceOTel → 1`, else `0`.
  - `type fieldStamps struct { timingRank int; timingSeq uint64; haveTiming bool; nameRank int; nameSeq uint64; haveName bool }`.
  - `Graph.stamps map[string]*fieldStamps` (keyed by node ID).
  - `func (g *Graph) stampsFor(id string) *fieldStamps`.
  - stamp-gated timing in `stamp`; stamp-gated `setName(n, o, name)`; `applyTokens(n, attrs, src model.Source)` with an `src == model.SourceOTel || !hasOTelTokens` rule (see below).

**The commutativity rules (pure function of `(rank, seq)`):**

- **Timing (`TStart`):** OTel outranks hook. Incoming wins iff `!haveTiming` OR `sourceRank(incoming) > timingRank` OR (`sourceRank(incoming) == timingRank` AND incoming earlier OR (equal source, the existing "earliest TStart" rule)). To stay minimal and commutative: a higher-rank source always sets `TStart` to its own value; an equal-rank source keeps the earliest `TStart` (today's rule); a lower-rank source never touches `TStart`. Record `(rank, seq)` on every accepted timing write.
- **Name ("first non-empty, tiebreak seq" → lowest-seq non-empty wins):** incoming non-empty name wins iff `!haveName` OR `o.Seq < nameSeq` (lower seq is canonical; source-rank is NOT a name discriminator per §5.1 "first non-empty value from ANY source"). This makes a later-applied lower-seq name overwrite — the order-independence fix.
- **Tokens:** With two live sources, OTel is authoritative. Rule that is commutative AND keeps the existing hook-token test green: track per-node `haveOTelTokens`; an OTel token write always wins and sets the flag; a hook token write applies ONLY if no OTel tokens have been seen for that node. Since OTel always wins regardless of order (it overwrites hook) and hook is blocked once OTel exists, the final value is OTel-if-any-OTel, else hook — order-independent. Store the flag in `fieldStamps` (`haveOTelTokens bool`).

- [ ] **Step 1: Write the failing tests** in `reduce/reduce_test.go`

```go
func TestSourceRank(t *testing.T) {
	assert.Equal(t, 1, sourceRank(model.SourceOTel))
	assert.Equal(t, 0, sourceRank(model.SourceHook))
	assert.Equal(t, 0, sourceRank(model.SourceJSONL))
}

func otelTurn(exec, runID, msg string, tIn, tOut int64, ts time.Time, seq uint64) model.Observation {
	return model.Observation{
		ObsID: "o" + strconv.FormatUint(seq, 10), RunID: runID, ExecutionID: exec,
		Source: model.SourceOTel, Kind: "assistant_turn",
		Correlation: model.Correlation{SessionID: runID, MessageID: msg},
		Attrs:       map[string]any{"tokens_in": tIn, "tokens_out": tOut},
		EventTime:   ts, ObservedAt: ts, Seq: seq,
	}
}

func hookTurn(exec, runID, msg string, tIn, tOut int64, ts time.Time, seq uint64) model.Observation {
	return model.Observation{
		ObsID: "o" + strconv.FormatUint(seq, 10), RunID: runID, ExecutionID: exec,
		Source: model.SourceHook, Kind: "assistant_turn",
		Correlation: model.Correlation{SessionID: runID, MessageID: msg},
		Attrs:       map[string]any{"tokens_in": tIn, "tokens_out": tOut},
		EventTime:   ts, ObservedAt: ts, Seq: seq,
	}
}

func TestTokensOTelWinsRegardlessOfOrder(t *testing.T) {
	t0 := time.Unix(10, 0).UTC()
	h := hookTurn("e1", "s1", "m1", 1, 1, t0, 1)
	o := otelTurn("e1", "s1", "m1", 99, 88, t0, 2)
	fwd := NewGraph()
	fwd.ApplyAll([]model.Observation{h, o})
	rev := NewGraph()
	rev.ApplyAll([]model.Observation{o, h})
	id := model.AssistantTurnID("e1", "m1")
	assert.Equal(t, int64(99), *fwd.Nodes[id].TokensIn)
	assert.Equal(t, int64(99), *rev.Nodes[id].TokensIn)
	assert.Equal(t, int64(88), *rev.Nodes[id].TokensOut)
}

func TestTokensHookKeptWhenNoOTel(t *testing.T) {
	g := NewGraph()
	g.Apply(hookTurn("e1", "s1", "m1", 7, 3, time.Unix(1, 0).UTC(), 1))
	id := model.AssistantTurnID("e1", "m1")
	assert.Equal(t, int64(7), *g.Nodes[id].TokensIn)
}

func TestTimingOTelOutranksHookEitherOrder(t *testing.T) {
	tHook := time.Unix(100, 0).UTC()
	tOTel := time.Unix(200, 0).UTC()
	h := hookTurn("e1", "s1", "m1", 0, 0, tHook, 5)
	o := otelTurn("e1", "s1", "m1", 0, 0, tOTel, 1)
	fwd := NewGraph()
	fwd.ApplyAll([]model.Observation{h, o})
	rev := NewGraph()
	rev.ApplyAll([]model.Observation{o, h})
	id := model.AssistantTurnID("e1", "m1")
	assert.Equal(t, tOTel, *fwd.Nodes[id].TStart)
	assert.Equal(t, tOTel, *rev.Nodes[id].TStart)
}

func TestTimingEqualRankKeepsEarliest(t *testing.T) {
	early := time.Unix(100, 0).UTC()
	late := time.Unix(200, 0).UTC()
	a := hookTurn("e1", "s1", "m1", 0, 0, late, 1)
	b := hookTurn("e1", "s1", "m1", 0, 0, early, 2)
	g := NewGraph()
	g.ApplyAll([]model.Observation{a, b})
	id := model.AssistantTurnID("e1", "m1")
	assert.Equal(t, early, *g.Nodes[id].TStart)
}

func TestNameLowestSeqWinsRegardlessOfOrder(t *testing.T) {
	t0 := time.Unix(1, 0).UTC()
	lo := toolObs("e1", "s1", "t1", "EarlyName", "running", 2)
	hi := toolObs("e1", "s1", "t1", "LateName", "running", 9)
	_ = t0
	fwd := NewGraph()
	fwd.ApplyAll([]model.Observation{hi, lo})
	rev := NewGraph()
	rev.ApplyAll([]model.Observation{lo, hi})
	id := model.ToolCallID("e1", "t1")
	assert.Equal(t, "EarlyName", fwd.Nodes[id].Name)
	assert.Equal(t, "EarlyName", rev.Nodes[id].Name)
}

func permute(obs []model.Observation) [][]model.Observation {
	if len(obs) <= 1 {
		return [][]model.Observation{append([]model.Observation(nil), obs...)}
	}
	var out [][]model.Observation
	for i := range obs {
		rest := append(append([]model.Observation(nil), obs[:i]...), obs[i+1:]...)
		for _, p := range permute(rest) {
			out = append(out, append([]model.Observation{obs[i]}, p...))
		}
	}
	return out
}

func TestReductionCommutativity(t *testing.T) {
	t0 := time.Unix(100, 0).UTC()
	obs := []model.Observation{
		sessionStartObs("e1", "s1", 1),
		hookTurn("e1", "s1", "m1", 5, 2, t0, 2),
		otelTurn("e1", "s1", "m1", 50, 20, t0.Add(time.Second), 3),
		toolObs("e1", "s1", "t1", "Bash", "running", 4),
		toolObs("e1", "s1", "t1", "Bash", string(model.StatusCancelled), 6),
		otelTool("e1", "s1", "t2", "spanLeaf", "spanRoot", 7),
	}
	perms := permute(obs)
	var want string
	for i, p := range perms {
		g := NewGraph()
		g.ApplyAll(p)
		got := canonGraph(g)
		if i == 0 {
			want = got
			continue
		}
		assert.Equal(t, want, got, "permutation %d diverged", i)
	}
}

func canonGraph(g *Graph) string {
	type nodeView struct {
		ID     string
		Type   model.NodeType
		Name   string
		Status model.Status
		Rev    uint64
		TStart string
		TIn    string
		TOut   string
		Cause  string
	}
	var nv []nodeView
	for id, n := range g.Nodes {
		v := nodeView{ID: id, Type: n.Type, Name: n.Name, Status: n.Status, Rev: n.Rev}
		if n.TStart != nil {
			v.TStart = n.TStart.UTC().Format(time.RFC3339Nano)
		}
		if n.TokensIn != nil {
			v.TIn = strconv.FormatInt(*n.TokensIn, 10)
		}
		if n.TokensOut != nil {
			v.TOut = strconv.FormatInt(*n.TokensOut, 10)
		}
		if n.Attrs != nil {
			if c, ok := n.Attrs["cancel_cause"].(string); ok {
				v.Cause = c
			}
		}
		nv = append(nv, v)
	}
	sort.Slice(nv, func(i, j int) bool { return nv[i].ID < nv[j].ID })
	type edgeView struct {
		ID  string
		Rev uint64
	}
	var ev []edgeView
	for id, e := range g.Edges {
		ev = append(ev, edgeView{ID: id, Rev: e.Rev})
	}
	sort.Slice(ev, func(i, j int) bool { return ev[i].ID < ev[j].ID })
	b, _ := json.Marshal(struct {
		Nodes []nodeView
		Edges []edgeView
	}{nv, ev})
	return string(b)
}
```

Add imports `encoding/json` and `sort` to the test file's import block.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./reduce/ -run 'TestSourceRank|TestTokens|TestTiming|TestName|TestReductionCommutativity' -v`
Expected: FAIL — `sourceRank` undefined; tokens not OTel-gated; name not lowest-seq; timing not OTel-ranked; commutativity diverges.

- [ ] **Step 3: Add `stamps` to `Graph`** (`reduce/graph.go`)

```go
type Graph struct {
	Nodes        map[string]*model.Node
	Edges        map[string]*model.Edge
	Runs         map[string]*model.Run
	spanChildren map[string]bool
	stamps       map[string]*fieldStamps
}

func NewGraph() *Graph {
	return &Graph{
		Nodes:        map[string]*model.Node{},
		Edges:        map[string]*model.Edge{},
		Runs:         map[string]*model.Run{},
		spanChildren: map[string]bool{},
		stamps:       map[string]*fieldStamps{},
	}
}
```

- [ ] **Step 4: Add `sourceRank`, `fieldStamps`, `stampsFor`** (`reduce/reduce.go`)

```go
func sourceRank(s model.Source) int {
	if s == model.SourceOTel {
		return 1
	}
	return 0
}

type fieldStamps struct {
	timingRank    int
	haveTiming    bool
	nameSeq       uint64
	haveName      bool
	haveOTelTokens bool
}

func (g *Graph) stampsFor(id string) *fieldStamps {
	fs, ok := g.stamps[id]
	if !ok {
		fs = &fieldStamps{}
		g.stamps[id] = fs
	}
	return fs
}
```

- [ ] **Step 5: Make timing stamp-aware in `stamp`** (`reduce/reduce.go`)

```go
func (g *Graph) stamp(n *model.Node, o model.Observation) {
	fs := g.stampsFor(n.ID)
	r := sourceRank(o.Source)
	if !fs.haveTiming || r > fs.timingRank {
		ts := o.EventTime
		n.TStart = &ts
		fs.timingRank = r
		fs.haveTiming = true
	} else if r == fs.timingRank && (n.TStart == nil || o.EventTime.Before(*n.TStart)) {
		ts := o.EventTime
		n.TStart = &ts
	}
	if o.Seq > n.Rev {
		n.Rev = o.Seq
	}
	n.Sources = append(n.Sources, model.SourceRef{Source: o.Source, ObsID: o.ObsID, ObservedAt: o.ObservedAt})
}
```

- [ ] **Step 6: Add `setName` and route the two name writes through it** (`reduce/reduce.go`)

```go
func (g *Graph) setName(n *model.Node, o model.Observation, name string) {
	if name == "" {
		return
	}
	fs := g.stampsFor(n.ID)
	if !fs.haveName || o.Seq < fs.nameSeq {
		n.Name = name
		fs.nameSeq = o.Seq
		fs.haveName = true
	}
}
```

In `applyTool`, replace:

```go
	if name, ok := o.Attrs["name"].(string); ok {
		g.setName(n, o, name)
	}
```

In `applySubagent`, the `subagent_type` field stays as-is (it is not a §5.1 conflicting group with a second live producer), but if a future test shows a name conflict for subagents, route it through `setName` too. For M1b, only the tool `name` path is routed (the live conflict), keeping coverage genuine.

- [ ] **Step 7: Add the Source guard to `applyTokens`** (`reduce/reduce.go`)

```go
func (g *Graph) applyTokens(n *model.Node, attrs map[string]any, src model.Source) {
	fs := g.stampsFor(n.ID)
	if src != model.SourceOTel && fs.haveOTelTokens {
		return
	}
	if src == model.SourceOTel {
		fs.haveOTelTokens = true
	}
	if v, ok := toInt64(attrs["tokens_in"]); ok {
		n.TokensIn = &v
	}
	if v, ok := toInt64(attrs["tokens_out"]); ok {
		n.TokensOut = &v
	}
}
```

Update the caller in `Apply` (`assistant_turn`):

```go
	case "assistant_turn":
		n := g.node(model.AssistantTurnID(o.ExecutionID, o.Correlation.MessageID), o.RunID, model.NodeAssistantTurn)
		g.stamp(n, o)
		g.applyTokens(n, o.Attrs, o.Source)
```

`applyTokens` becomes a method on `*Graph` (needs `stampsFor`); update its existing test `TestToInt64` is unaffected (tests `toInt64`, not `applyTokens`). No other caller exists.

- [ ] **Step 8: Run to verify it passes**

Run: `go test ./reduce/ -run 'TestSourceRank|TestTokens|TestTiming|TestName|TestReductionCommutativity' -v`
Expected: PASS.

- [ ] **Step 9: Run the WHOLE reduce suite** (regression for existing order-independence tests)

Run: `go test ./reduce/ -race -v`
Expected: PASS — `TestApplyOrderIndependentFields`, `TestApplyToolTypeUpgradeReversedOrder`, `TestSessionEndLateGenuineSupersedesUnknown`, `TestRunStatusGenuineSessionEndLatchesOverRunEnded` still green.

- [ ] **Step 10: Full gate**

Run: `make cover && make lint`
Expected: 100% for `reduce` + `model`; lint 0 (watch unparam on `sourceRank`/`applyTokens` — the commutativity test varies sources at the root).

- [ ] **Step 11: Commit**

```bash
git add reduce/graph.go reduce/reduce.go reduce/reduce_test.go
git commit -m "feat(reduce): per-field source precedence (timing/name/tokens) + commutativity property test"
```

---

## Deferred (documented — NOT implemented here)

- **CDC `DeltaNodeStatus` emission** during cascade (spec §5.4 step 3) → M1c (no bus exists yet).
- **Per-execution OTel-completeness latch** (ADR-0014 optional optimization) → post-M1; `spanChildren` is consulted every time in M1b.
- **§5.1 rows with no live producer** (JSONL/stream-json timing, tokens, structure, payload deltas; OTel `mcp_*` attrs; name-pattern parse) → M2 when those sources land. Implementing them now would be dead code (violates 100% genuine coverage + YAGNI).
- **Step 7** live field-name operator verification → end-of-roadmap operator testing.

## Self-Review

- **Spec §5 coverage:**
  - §5.1 per-field precedence — Task 5 (timing OTel>hook, name lowest-seq, tokens OTel-won); deferred rows enumerated above. ✓
  - §5.2 status lattice — Task 1 (`StatusSuperseded` const + `rank` superseded/abandoned→2; existing `resolveStatus` unchanged and re-asserted). ✓
  - §5.3 #53954 gate — Task 3 (`spanChildren` + `upsertEdgeGated`, exact predicate from the spec). ✓
  - §5.4 cascade — Task 4 (`cascadeStatus` generalizes `closeOpenDescendants`, skip rank-3, `cancel_cause`, existing call sites preserved, cancelled/superseded trigger; `DeltaNodeStatus` deferred). ✓
  - §5.5 Rev — Task 2 (`n.Rev=max(n.Rev,o.Seq)` in `stamp`; `e.Rev` in `upsertEdge` on create + re-touch). ✓
- **Commutativity covered:** Task 5 `TestReductionCommutativity` permutes a 6-observation multi-source set (hook+OTel turn, hook tool→cancelled, OTel tool span) through all `6! = 720` orders and asserts an identical canonicalized graph (nodes: type/name/status/Rev/TStart/tokens/cancel_cause; edges: id/Rev). Status (Task 1/4) and structure (Task 3) commutativity are exercised inside the same permutation set. ✓
- **Placeholder scan:** no TBD/TODO/"similar to above"; every code step is complete Go. ✓
- **Type consistency:** `upsertEdge` gains `seq uint64` (all call sites updated in Task 2); `applyTokens` becomes `(g *Graph) applyTokens(n, attrs, src)` in Task 5 (single caller updated); `cascadeStatus(rootID, status)` replaces `closeOpenDescendants(rootID)` with all three test call sites + two prod call sites updated in Task 4; `fieldStamps`/`stamps`/`stampsFor`/`sourceRank`/`setName` introduced and consumed within Task 5. ✓
- **Spec/code mismatch resolved:** spec §5.5 says "Edge has no Rev today" — STALE; `model.Edge.Rev` already exists (M1a), so Task 2 only populates it. Spec §5.2 lists `StatusSuperseded` as missing — confirmed: 8 of 9 consts exist, only `StatusSuperseded` is added (`StatusCancelled`/`StatusBlocked`/`StatusAbandoned` already present). Spec §5.4 says "emit `DeltaNodeStatus`" — explicitly deferred to M1c (no bus). ✓
- **No-dead-code check:** `sourceRank` has a two-arm body (OTel / else), both reached (OTel + hook inputs); `applyTokens` Source guard both arms reached (`TestTokensOTelWinsRegardlessOfOrder` hits OTel-wins + hook-blocked; `TestTokensHookKeptWhenNoOTel` hits hook-applies); `applyCascade` unknown vs cancelled branches both reached (Task 4 tests). No branch added for JSONL/stream-json. ✓
