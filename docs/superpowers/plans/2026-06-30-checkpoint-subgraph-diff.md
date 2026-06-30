# Checkpoint-scoped Subgraph Diff — Implementation Plan (PR1)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let users diff the execution subgraph delimited by a checkpoint/phase — both cross-run (same phase in two runs) and within-run (`a=b`, two phases) — by scoping the existing `DiffGraphs` to a phase window, with zero change to the diff engine.

**Architecture:** A new pure package `subgraph` provides a phase `Window`, the membership predicate `InWindow` (factored out of `reduce`'s `addMarkerSpans` so behavior cannot drift), a `Subgraph` slicer, a `PhaseWindow` resolver, and a `ParseSelector`/`ScopeExecution` pair. The daemon `/v1/diff` handler and the CLI `diff` command resolve a phase per execution and run each side through `Subgraph` before calling `DiffGraphs`. No frontend (PR3) and no focus endpoint (PR2) in this plan.

**Tech Stack:** Go 1.26, `github.com/realkarych/catacomb` module, `spf13/cobra` (CLI), `stretchr/testify` (tests), stdlib `net/http` + `net/http/httptest` (daemon).

## Global Constraints

- **No comments in Go code.** None — not even doc comments. Only `//go:build`, `//go:embed`, `//go:generate` are allowed. Enforced by `go test ./internal/codepolicy`.
- **100% coverage** (file, package, total). Enforced by `make cover` → `.testcoverage.yml`. Every branch of every new function must be exercised. Do not write unreachable code.
- **TDD:** failing test first, minimal code to pass, then commit.
- **Always work in the worktree** `/Users/karych/src/catacomb/.claude/worktrees/checkpoint-subgraph-diff` (branch `worktree-checkpoint-subgraph-diff`). Never edit the shared checkout.
- Module import prefix: `github.com/realkarych/catacomb`.
- Run tests with `go test -race ./...`; the full gate is `make cover`.

## File Structure

| File | Responsibility |
| --- | --- |
| `subgraph/subgraph.go` (new) | `Window`, `InWindow`, `Subgraph` — pure, imports only `model`. |
| `subgraph/resolve.go` (new) | `PhaseWindow`, `ParseSelector`, `ScopeExecution`, `ErrInvalidSelector`. |
| `subgraph/subgraph_test.go`, `subgraph/resolve_test.go` (new) | Unit tests for the above. |
| `reduce/marker.go` (modify) | `addMarkerSpans` delegates its time check to `subgraph.InWindow`. |
| `daemon/diff.go` (modify) | `scopedGraph` + phase query params + `ErrPhaseNotFound` + `writeScopeErr`. |
| `daemon/diff_test.go` (modify) | Tests for phase scoping + errors. |
| `cmd/catacomb/diff.go` (modify) | `--phase`/`--a-phase`/`--b-phase` flags; per-side scoping in `runDiff`. |
| `cmd/catacomb/diff_test.go` (modify) | Tests for CLI scoping + errors. |

---

### Task 1: `subgraph` core — `Window`, `InWindow`, `Subgraph`

**Files:**
- Create: `subgraph/subgraph.go`
- Test: `subgraph/subgraph_test.go`

**Interfaces:**
- Produces:
  - `type Window struct { Start time.Time; End *time.Time }`
  - `func InWindow(n *model.Node, w Window) bool` — `true` iff `n.TStart != nil` and `n.TStart` is in the **closed** interval `[w.Start, w.End]` (`w.End == nil` ⇒ unbounded right).
  - `func Subgraph(nodes []*model.Node, edges []*model.Edge, w Window) ([]*model.Node, []*model.Edge)` — keeps non-marker nodes with `InWindow` true, preserving input order, plus edges whose **both** endpoints are kept.

- [ ] **Step 1: Write the failing test**

```go
package subgraph

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/realkarych/catacomb/model"
)

func ts(sec int64) *time.Time {
	t := time.Unix(sec, 0).UTC()
	return &t
}

func node(id string, typ model.NodeType, start *time.Time) *model.Node {
	return &model.Node{ID: id, Type: typ, TStart: start}
}

func TestInWindowClosedInterval(t *testing.T) {
	w := Window{Start: time.Unix(100, 0).UTC(), End: ts(200)}

	assert.False(t, InWindow(node("x", model.NodeToolCall, nil), w))
	assert.False(t, InWindow(node("x", model.NodeToolCall, ts(99)), w))
	assert.True(t, InWindow(node("x", model.NodeToolCall, ts(100)), w))
	assert.True(t, InWindow(node("x", model.NodeToolCall, ts(150)), w))
	assert.True(t, InWindow(node("x", model.NodeToolCall, ts(200)), w))
	assert.False(t, InWindow(node("x", model.NodeToolCall, ts(201)), w))
}

func TestInWindowOpenEnd(t *testing.T) {
	w := Window{Start: time.Unix(100, 0).UTC(), End: nil}
	assert.True(t, InWindow(node("x", model.NodeToolCall, ts(10_000)), w))
	assert.False(t, InWindow(node("x", model.NodeToolCall, ts(99)), w))
}

func TestSubgraphFiltersNodesMarkersAndInducesEdges(t *testing.T) {
	w := Window{Start: time.Unix(100, 0).UTC(), End: ts(200)}
	nodes := []*model.Node{
		node("in1", model.NodeToolCall, ts(110)),
		node("in2", model.NodeToolCall, ts(190)),
		node("out", model.NodeToolCall, ts(500)),
		node("mark", model.NodeMarker, ts(120)),
	}
	edges := []*model.Edge{
		{ID: "e1", Src: "in1", Dst: "in2"},
		{ID: "e2", Src: "in1", Dst: "out"},
		{ID: "e3", Src: "mark", Dst: "in1"},
	}

	sn, se := Subgraph(nodes, edges, w)

	assert.Equal(t, []string{"in1", "in2"}, ids(sn))
	assert.Len(t, se, 1)
	assert.Equal(t, "e1", se[0].ID)
}

func ids(ns []*model.Node) []string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = n.ID
	}
	return out
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./subgraph/ -run TestInWindow -v`
Expected: FAIL — `undefined: Window` / `InWindow` / `Subgraph`.

- [ ] **Step 3: Write minimal implementation**

```go
package subgraph

import (
	"time"

	"github.com/realkarych/catacomb/model"
)

type Window struct {
	Start time.Time
	End   *time.Time
}

func InWindow(n *model.Node, w Window) bool {
	if n.TStart == nil {
		return false
	}
	if n.TStart.Before(w.Start) {
		return false
	}
	if w.End != nil && n.TStart.After(*w.End) {
		return false
	}
	return true
}

func Subgraph(nodes []*model.Node, edges []*model.Edge, w Window) ([]*model.Node, []*model.Edge) {
	included := make(map[string]bool, len(nodes))
	var sn []*model.Node
	for _, n := range nodes {
		if n.Type == model.NodeMarker {
			continue
		}
		if !InWindow(n, w) {
			continue
		}
		included[n.ID] = true
		sn = append(sn, n)
	}
	var se []*model.Edge
	for _, e := range edges {
		if included[e.Src] && included[e.Dst] {
			se = append(se, e)
		}
	}
	return sn, se
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./subgraph/ -v`
Expected: PASS (all three tests).

- [ ] **Step 5: Commit**

```bash
git add subgraph/subgraph.go subgraph/subgraph_test.go
git commit -m "feat(subgraph): phase Window, InWindow predicate, Subgraph slicer

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 2: `subgraph` resolver — `PhaseWindow`, `ParseSelector`, `ScopeExecution`

**Files:**
- Create: `subgraph/resolve.go`
- Test: `subgraph/resolve_test.go`

**Interfaces:**
- Consumes: `Window`, `Subgraph` (Task 1); `model.PhaseMarkerID(executionID, name string, occ int) string`.
- Produces:
  - `var ErrInvalidSelector = errors.New("subgraph: invalid phase selector")`
  - `func ParseSelector(val string) (name string, occ int, err error)` — splits on the first comma; no comma ⇒ `occ = 0`; bad integer ⇒ wraps `ErrInvalidSelector`.
  - `func PhaseWindow(nodes []*model.Node, execID, name string, occ int) (Window, bool)` — finds the `NodeMarker` whose ID is `model.PhaseMarkerID(execID, name, occ)`; returns its `[TStart, TEnd]` window. `false` if absent or its `TStart` is nil.
  - `func ScopeExecution(nodes []*model.Node, edges []*model.Edge, execID, name string, occ int) ([]*model.Node, []*model.Edge, bool)` — `PhaseWindow` then `Subgraph`; `false` if the phase is absent.

- [ ] **Step 1: Write the failing test**

```go
package subgraph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
)

func TestParseSelector(t *testing.T) {
	n, occ, err := ParseSelector("plan")
	require.NoError(t, err)
	assert.Equal(t, "plan", n)
	assert.Equal(t, 0, occ)

	n, occ, err = ParseSelector("plan,2")
	require.NoError(t, err)
	assert.Equal(t, "plan", n)
	assert.Equal(t, 2, occ)

	_, _, err = ParseSelector("plan,x")
	assert.ErrorIs(t, err, ErrInvalidSelector)
}

func TestPhaseWindowResolvesMarker(t *testing.T) {
	exec := "E"
	markerID := model.PhaseMarkerID(exec, "plan", 0)
	nodes := []*model.Node{
		{ID: markerID, Type: model.NodeMarker, TStart: ts(100), TEnd: ts(200)},
		{ID: "other", Type: model.NodeToolCall, TStart: ts(150)},
	}

	w, ok := PhaseWindow(nodes, exec, "plan", 0)
	require.True(t, ok)
	assert.Equal(t, ts(100).Unix(), w.Start.Unix())
	require.NotNil(t, w.End)
	assert.Equal(t, ts(200).Unix(), w.End.Unix())

	_, ok = PhaseWindow(nodes, exec, "missing", 0)
	assert.False(t, ok)
}

func TestPhaseWindowOpenPhaseAndNilStart(t *testing.T) {
	exec := "E"
	open := model.PhaseMarkerID(exec, "open", 0)
	bad := model.PhaseMarkerID(exec, "bad", 0)
	nodes := []*model.Node{
		{ID: open, Type: model.NodeMarker, TStart: ts(100), TEnd: nil},
		{ID: bad, Type: model.NodeMarker, TStart: nil},
	}

	w, ok := PhaseWindow(nodes, exec, "open", 0)
	require.True(t, ok)
	assert.Nil(t, w.End)

	_, ok = PhaseWindow(nodes, exec, "bad", 0)
	assert.False(t, ok)
}

func TestScopeExecution(t *testing.T) {
	exec := "E"
	markerID := model.PhaseMarkerID(exec, "plan", 0)
	nodes := []*model.Node{
		{ID: markerID, Type: model.NodeMarker, TStart: ts(100), TEnd: ts(200)},
		{ID: "in", Type: model.NodeToolCall, TStart: ts(150)},
		{ID: "out", Type: model.NodeToolCall, TStart: ts(900)},
	}
	edges := []*model.Edge{{ID: "e", Src: "in", Dst: "out"}}

	sn, se, ok := ScopeExecution(nodes, edges, exec, "plan", 0)
	require.True(t, ok)
	assert.Equal(t, []string{"in"}, ids(sn))
	assert.Empty(t, se)

	_, _, ok = ScopeExecution(nodes, edges, exec, "missing", 0)
	assert.False(t, ok)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./subgraph/ -run 'TestParseSelector|TestPhaseWindow|TestScopeExecution' -v`
Expected: FAIL — `undefined: ParseSelector` / `PhaseWindow` / `ScopeExecution` / `ErrInvalidSelector`.

- [ ] **Step 3: Write minimal implementation**

```go
package subgraph

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/realkarych/catacomb/model"
)

var ErrInvalidSelector = errors.New("subgraph: invalid phase selector")

func ParseSelector(val string) (string, int, error) {
	name, occStr, hasOcc := strings.Cut(val, ",")
	if !hasOcc {
		return name, 0, nil
	}
	occ, err := strconv.Atoi(occStr)
	if err != nil {
		return "", 0, fmt.Errorf("%w: %q", ErrInvalidSelector, val)
	}
	return name, occ, nil
}

func PhaseWindow(nodes []*model.Node, execID, name string, occ int) (Window, bool) {
	id := model.PhaseMarkerID(execID, name, occ)
	for _, n := range nodes {
		if n.ID != id {
			continue
		}
		if n.TStart == nil {
			return Window{}, false
		}
		return Window{Start: *n.TStart, End: n.TEnd}, true
	}
	return Window{}, false
}

func ScopeExecution(nodes []*model.Node, edges []*model.Edge, execID, name string, occ int) ([]*model.Node, []*model.Edge, bool) {
	w, ok := PhaseWindow(nodes, execID, name, occ)
	if !ok {
		return nil, nil, false
	}
	sn, se := Subgraph(nodes, edges, w)
	return sn, se, true
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./subgraph/ -v`
Expected: PASS (all tests, Tasks 1 + 2).

- [ ] **Step 5: Commit**

```bash
git add subgraph/resolve.go subgraph/resolve_test.go
git commit -m "feat(subgraph): phase selector parsing and per-execution resolver

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 3: `reduce` parity refactor — `addMarkerSpans` uses `subgraph.InWindow`

**Files:**
- Modify: `reduce/marker.go` (`addMarkerSpans`, lines 226-253; add `subgraph` import)
- Test: existing `reduce/marker_test.go` is the parity oracle (must stay green); add one focused assertion.

**Interfaces:**
- Consumes: `subgraph.Window`, `subgraph.InWindow` (Task 1).
- Produces: no API change. Behavior of `EdgeMarkerSpan` synthesis is byte-identical.

**Why:** single source of truth for the time-window predicate. The marker-specific exclusions (`nodeID == markerID`, `n.Type == NodeMarker`) stay in `addMarkerSpans`; only the three time checks move to `subgraph.InWindow`.

- [ ] **Step 1: Run the existing parity oracle first (baseline green)**

Run: `go test ./reduce/ -run Marker -v`
Expected: PASS. This is the behavior we must preserve.

- [ ] **Step 2: Refactor `addMarkerSpans`**

Replace the body of `addMarkerSpans` (keep the signature) with:

```go
func (g *Graph) addMarkerSpans(execID, runID, markerID string, tStart time.Time, tEnd *time.Time) {
	w := subgraph.Window{Start: tStart, End: tEnd}
	for nodeID, n := range g.Nodes {
		if nodeID == markerID {
			continue
		}
		if n.Type == model.NodeMarker {
			continue
		}
		if !subgraph.InWindow(n, w) {
			continue
		}
		edgeID := model.EdgeID(execID, model.EdgeMarkerSpan, markerID, nodeID)
		g.Edges[edgeID] = &model.Edge{
			ID:    edgeID,
			RunID: runID,
			Type:  model.EdgeMarkerSpan,
			Src:   markerID,
			Dst:   nodeID,
		}
		g.synthMarkerEdges[edgeID] = true
	}
}
```

Add `"github.com/realkarych/catacomb/subgraph"` to the import block in `reduce/marker.go`.

- [ ] **Step 3: Run the parity oracle + full reduce package**

Run: `go test -race ./reduce/ ./subgraph/`
Expected: PASS, unchanged counts. If any marker test fails, the predicate is not equivalent — revert and reconcile before continuing.

- [ ] **Step 4: Guard against an import cycle**

Run: `go build ./...`
Expected: build succeeds (no `import cycle` error — `subgraph` imports only `model`, never `reduce`).

- [ ] **Step 5: Commit**

```bash
git add reduce/marker.go
git commit -m "refactor(reduce): addMarkerSpans delegates time check to subgraph.InWindow

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 4: daemon — phase-scoped `/v1/diff`

**Files:**
- Modify: `daemon/diff.go` (add `ErrPhaseNotFound`, `scopedGraph`, `writeScopeErr`; rewrite `handleDiff`; add `fmt` + `subgraph` imports)
- Test: `daemon/diff_test.go`

**Interfaces:**
- Consumes: `subgraph.ParseSelector`, `subgraph.ScopeExecution`, `subgraph.ErrInvalidSelector` (Task 2); existing `d.sessionGraphNodes`, `d.executionsForSession`, `d.graphs[execID].Snapshot()`, `ErrSessionNotFound`, `nodesWithoutPayload`.
- Produces: `GET /v1/diff` now accepts optional `aPhase` / `bPhase` query params (`name[,occurrence]`). Empty ⇒ unchanged whole-run behavior. Missing phase ⇒ 400; invalid selector ⇒ 400; session not found ⇒ 404.

- [ ] **Step 1: Write the failing tests**

Append to `daemon/diff_test.go` (these reuse existing helpers `tempStore`, `fixedExecID`, and the package var `nowFn`):

```go
func advancingClock(t *testing.T) {
	base := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	var tick int64
	nowFn = func() time.Time {
		tick++
		return base.Add(time.Duration(tick) * time.Second)
	}
	t.Cleanup(func() { nowFn = time.Now })
}

func phaseSession(t *testing.T, d *Daemon) {
	t.Helper()
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.IngestMark(MarkInput{SessionID: "s1", Name: "phase1", Boundary: "start"}))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_input":{}}`)))
	require.NoError(t, d.IngestMark(MarkInput{SessionID: "s1", Name: "phase1", Boundary: "end"}))
	require.NoError(t, d.IngestMark(MarkInput{SessionID: "s1", Name: "phase2", Boundary: "start"}))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t2","tool_input":{}}`)))
	require.NoError(t, d.IngestMark(MarkInput{SessionID: "s1", Name: "phase2", Boundary: "end"}))
}

func getDiff(t *testing.T, srv *httptest.Server, query string) *http.Response {
	t.Helper()
	resp, err := http.Get(srv.URL + "/v1/diff?token=testtoken&" + query)
	require.NoError(t, err)
	return resp
}

func TestHandleDiff_PhaseScopeOK(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	advancingClock(t)
	phaseSession(t, d)
	srv := httptest.NewServer(d.Handler("testtoken"))
	t.Cleanup(srv.Close)

	resp := getDiff(t, srv, "a=s1&b=s1&aPhase=phase1&bPhase=phase1")
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var result diff.DiffResult
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
}

func TestHandleDiff_WithinRunPhases(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	advancingClock(t)
	phaseSession(t, d)
	srv := httptest.NewServer(d.Handler("testtoken"))
	t.Cleanup(srv.Close)

	resp := getDiff(t, srv, "a=s1&b=s1&aPhase=phase1&bPhase=phase2")
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var result diff.DiffResult
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
}

func TestHandleDiff_PhaseNotFound(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	advancingClock(t)
	phaseSession(t, d)
	srv := httptest.NewServer(d.Handler("testtoken"))
	t.Cleanup(srv.Close)

	resp := getDiff(t, srv, "a=s1&b=s1&aPhase=ghost")
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestHandleDiff_InvalidSelector(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	advancingClock(t)
	phaseSession(t, d)
	srv := httptest.NewServer(d.Handler("testtoken"))
	t.Cleanup(srv.Close)

	resp := getDiff(t, srv, "a=s1&b=s1&bPhase=phase1,x")
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestHandleDiff_SessionNotFoundWithPhase(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	advancingClock(t)
	phaseSession(t, d)
	srv := httptest.NewServer(d.Handler("testtoken"))
	t.Cleanup(srv.Close)

	resp := getDiff(t, srv, "a=nope&b=s1&aPhase=phase1")
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}
```

If `daemon/diff_test.go` does not already import `net/http/httptest` and `time`, add them. (The existing tests already import `net/http`, `encoding/json`, testify, and `diff`.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./daemon/ -run 'TestHandleDiff_Phase|TestHandleDiff_WithinRun|TestHandleDiff_InvalidSelector|TestHandleDiff_SessionNotFoundWithPhase' -v`
Expected: FAIL — `aPhase` is ignored today, so `TestHandleDiff_PhaseNotFound`/`InvalidSelector` return 200 instead of 400 (compile passes, assertions fail).

- [ ] **Step 3: Implement the scoping in `daemon/diff.go`**

Add to the import block: `"fmt"` and `"github.com/realkarych/catacomb/subgraph"`.

Add the sentinel and helpers, and replace `handleDiff`:

```go
var ErrPhaseNotFound = errors.New("daemon: phase not found")

func (d *Daemon) scopedGraph(hash, phaseSel string) ([]*model.Node, []*model.Edge, error) {
	if phaseSel == "" {
		return d.sessionGraphNodes(hash)
	}
	name, occ, err := subgraph.ParseSelector(phaseSel)
	if err != nil {
		return nil, nil, err
	}
	execs := d.executionsForSession(hash)
	if len(execs) == 0 {
		return nil, nil, ErrSessionNotFound
	}
	var nodes []*model.Node
	var edges []*model.Edge
	found := false
	for _, execID := range execs {
		n, e := d.graphs[execID].Snapshot()
		sn, se, ok := subgraph.ScopeExecution(n, e, execID, name, occ)
		if !ok {
			continue
		}
		nodes = append(nodes, sn...)
		edges = append(edges, se...)
		found = true
	}
	if !found {
		return nil, nil, ErrPhaseNotFound
	}
	return nodes, edges, nil
}

func writeScopeErr(w http.ResponseWriter, hash, phase string, err error) {
	switch {
	case errors.Is(err, ErrSessionNotFound):
		w.WriteHeader(http.StatusNotFound)
	case errors.Is(err, ErrPhaseNotFound):
		http.Error(w, fmt.Sprintf("phase %q not found in session %q", phase, hash), http.StatusBadRequest)
	default:
		http.Error(w, fmt.Sprintf("invalid phase selector %q for session %q", phase, hash), http.StatusBadRequest)
	}
}

func (d *Daemon) handleDiff(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	a := q.Get("a")
	b := q.Get("b")
	if a == "" || b == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	aPhase := q.Get("aPhase")
	bPhase := q.Get("bPhase")
	d.mu.Lock()
	aN, aE, err := d.scopedGraph(a, aPhase)
	if err != nil {
		d.mu.Unlock()
		writeScopeErr(w, a, aPhase, err)
		return
	}
	bN, bE, err := d.scopedGraph(b, bPhase)
	if err != nil {
		d.mu.Unlock()
		writeScopeErr(w, b, bPhase, err)
		return
	}
	if !d.allowPayloadAccess {
		aN = nodesWithoutPayload(aN)
		bN = nodesWithoutPayload(bN)
	}
	result := diff.DiffGraphs(aN, aE, bN, bE)
	d.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}
```

The `default` branch of `writeScopeErr` is the invalid-selector case (reachable via `subgraph.ErrInvalidSelector` from `ParseSelector`); there is no unreachable arm.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race ./daemon/ -run TestHandleDiff`
Expected: PASS (existing whole-run diff tests + the five new phase tests).

- [ ] **Step 5: Commit**

```bash
git add daemon/diff.go daemon/diff_test.go
git commit -m "feat(daemon): phase-scoped /v1/diff via aPhase/bPhase query params

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 5: CLI — `--phase` / `--a-phase` / `--b-phase` on `catacomb diff`

**Files:**
- Create: `cmd/catacomb/testdata/session_marked.jsonl`
- Modify: `cmd/catacomb/diff.go` (`diffArgs` fields, flags, `aSel`/`bSel`, `scopeCLISide`, `runDiff`; add `model` + `subgraph` imports)
- Test: `cmd/catacomb/diff_test.go`

**Interfaces:**
- Consumes: `subgraph.ParseSelector`, `subgraph.ScopeExecution`, `subgraph.ErrInvalidSelector` (Task 2); existing `loadGraph`, `newExecutionID`, `catdiff.DiffGraphs`.
- Produces: `diffArgs{ a, b string; json bool; phase, aPhase, bPhase string }`; flags `--phase`, `--a-phase`, `--b-phase`. Per-side effective selector = side flag if set, else `--phase`.

- [ ] **Step 1: Create the deterministic fixture**

Create `cmd/catacomb/testdata/session_marked.jsonl` with these exact 11 lines (timestamps make the `plan` phase window `[10:00:03, 10:00:07]`, so only `pwd` is in-phase; `ls` is before, `whoami` is after; the two `mcp__catacomb__mark` tool-uses are suppressed):

```jsonl
{"type":"user","uuid":"u1","sessionId":"s1","timestamp":"2026-06-20T10:00:00Z","message":{"role":"user","content":"go"}}
{"type":"assistant","uuid":"a1","parentUuid":"u1","sessionId":"s1","timestamp":"2026-06-20T10:00:01Z","message":{"role":"assistant","id":"msg_1","model":"claude-opus-4-8","content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"ls"}}]}}
{"type":"user","uuid":"u2","parentUuid":"a1","sessionId":"s1","timestamp":"2026-06-20T10:00:02Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"ok","is_error":false}]}}
{"type":"assistant","uuid":"a2","parentUuid":"u2","sessionId":"s1","timestamp":"2026-06-20T10:00:03Z","message":{"role":"assistant","id":"msg_2","model":"claude-opus-4-8","content":[{"type":"tool_use","id":"toolu_mark1","name":"mcp__catacomb__mark","input":{"name":"plan","boundary":"start"}}]}}
{"type":"user","uuid":"u3","parentUuid":"a2","sessionId":"s1","timestamp":"2026-06-20T10:00:04Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_mark1","content":"","is_error":false}]}}
{"type":"assistant","uuid":"a3","parentUuid":"u3","sessionId":"s1","timestamp":"2026-06-20T10:00:05Z","message":{"role":"assistant","id":"msg_3","model":"claude-opus-4-8","content":[{"type":"tool_use","id":"toolu_2","name":"Bash","input":{"command":"pwd"}}]}}
{"type":"user","uuid":"u4","parentUuid":"a3","sessionId":"s1","timestamp":"2026-06-20T10:00:06Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_2","content":"/tmp","is_error":false}]}}
{"type":"assistant","uuid":"a4","parentUuid":"u4","sessionId":"s1","timestamp":"2026-06-20T10:00:07Z","message":{"role":"assistant","id":"msg_4","model":"claude-opus-4-8","content":[{"type":"tool_use","id":"toolu_mark2","name":"mcp__catacomb__mark","input":{"name":"plan","boundary":"end"}}]}}
{"type":"user","uuid":"u5","parentUuid":"a4","sessionId":"s1","timestamp":"2026-06-20T10:00:08Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_mark2","content":"","is_error":false}]}}
{"type":"assistant","uuid":"a5","parentUuid":"u5","sessionId":"s1","timestamp":"2026-06-20T10:00:09Z","message":{"role":"assistant","id":"msg_5","model":"claude-opus-4-8","content":[{"type":"tool_use","id":"toolu_3","name":"Bash","input":{"command":"whoami"}}]}}
{"type":"user","uuid":"u6","parentUuid":"a5","sessionId":"s1","timestamp":"2026-06-20T10:00:10Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_3","content":"me","is_error":false}]}}
```

- [ ] **Step 2: Write the failing tests**

Append to `cmd/catacomb/diff_test.go` (add `"github.com/realkarych/catacomb/subgraph"` to its imports):

```go
func TestRunDiffPhaseScopeReducesSet(t *testing.T) {
	whole, err := runDiff(diffArgs{a: "testdata/session_marked.jsonl", b: "testdata/session_marked.jsonl"})
	require.NoError(t, err)
	scoped, err := runDiff(diffArgs{a: "testdata/session_marked.jsonl", b: "testdata/session_marked.jsonl", phase: "plan"})
	require.NoError(t, err)
	assert.Len(t, whole.Unchanged, 3)
	assert.Len(t, scoped.Unchanged, 1)
}

func TestRunDiffAPhaseOnly(t *testing.T) {
	result, err := runDiff(diffArgs{a: "testdata/session_marked.jsonl", b: "testdata/session_marked.jsonl", aPhase: "plan"})
	require.NoError(t, err)
	assert.Len(t, result.Unchanged, 1)
	assert.Len(t, result.Added, 2)
}

func TestRunDiffBPhaseNotFound(t *testing.T) {
	_, err := runDiff(diffArgs{a: "testdata/session_marked.jsonl", b: "testdata/session_marked.jsonl", bPhase: "ghost"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestRunDiffInvalidSelector(t *testing.T) {
	_, err := runDiff(diffArgs{a: "testdata/session_marked.jsonl", b: "testdata/session_marked.jsonl", phase: "plan,x"})
	assert.ErrorIs(t, err, subgraph.ErrInvalidSelector)
}

func TestDiffCommandPhaseFlag(t *testing.T) {
	root := newRootCmd()
	var sb strings.Builder
	root.SetOut(&sb)
	root.SetArgs([]string{"diff", "--phase", "plan", "testdata/session_marked.jsonl", "testdata/session_marked.jsonl"})
	require.NoError(t, root.Execute())
	assert.Contains(t, sb.String(), "unchanged: 1")
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./cmd/catacomb/ -run 'TestRunDiffPhase|TestRunDiffAPhase|TestRunDiffBPhase|TestRunDiffInvalidSelector|TestDiffCommandPhaseFlag' -v`
Expected: FAIL — `diffArgs` has no `phase`/`aPhase`/`bPhase` fields (compile error), then assertion failures.

- [ ] **Step 4: Implement the scoping in `cmd/catacomb/diff.go`**

Add to the import block: `"github.com/realkarych/catacomb/model"` and `"github.com/realkarych/catacomb/subgraph"`.

Extend `diffArgs` and the flags, add `aSel`/`bSel`/`scopeCLISide`, and rewrite `runDiff`:

```go
type diffArgs struct {
	a      string
	b      string
	json   bool
	phase  string
	aPhase string
	bPhase string
}

func (a diffArgs) aSel() string {
	if a.aPhase != "" {
		return a.aPhase
	}
	return a.phase
}

func (a diffArgs) bSel() string {
	if a.bPhase != "" {
		return a.bPhase
	}
	return a.phase
}

func scopeCLISide(nodes []*model.Node, edges []*model.Edge, execID, sel string) ([]*model.Node, []*model.Edge, error) {
	if sel == "" {
		return nodes, edges, nil
	}
	name, occ, err := subgraph.ParseSelector(sel)
	if err != nil {
		return nil, nil, err
	}
	sn, se, ok := subgraph.ScopeExecution(nodes, edges, execID, name, occ)
	if !ok {
		return nil, nil, fmt.Errorf("diff: phase %q not found", sel)
	}
	return sn, se, nil
}

func runDiff(args diffArgs) (catdiff.DiffResult, error) {
	aExec := newExecutionID()
	ag, _, err := loadGraph(args.a, aExec)
	if err != nil {
		return catdiff.DiffResult{}, fmt.Errorf("diff: %s: %w (%w)", args.a, err, ErrDiffInput)
	}
	bExec := newExecutionID()
	bg, _, err := loadGraph(args.b, bExec)
	if err != nil {
		return catdiff.DiffResult{}, fmt.Errorf("diff: %s: %w (%w)", args.b, err, ErrDiffInput)
	}
	an, ae := ag.Snapshot()
	bn, be := bg.Snapshot()
	an, ae, err = scopeCLISide(an, ae, aExec, args.aSel())
	if err != nil {
		return catdiff.DiffResult{}, err
	}
	bn, be, err = scopeCLISide(bn, be, bExec, args.bSel())
	if err != nil {
		return catdiff.DiffResult{}, err
	}
	return catdiff.DiffGraphs(an, ae, bn, be), nil
}
```

In `newDiffCmd`, register the flags next to the existing `--json` flag:

```go
	cmd.Flags().BoolVar(&args.json, "json", false, "output as JSON")
	cmd.Flags().StringVar(&args.phase, "phase", "", "scope both sides to phase name[,occurrence]")
	cmd.Flags().StringVar(&args.aPhase, "a-phase", "", "scope side A to phase name[,occurrence]")
	cmd.Flags().StringVar(&args.bPhase, "b-phase", "", "scope side B to phase name[,occurrence]")
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test -race ./cmd/catacomb/ -run Diff`
Expected: PASS (existing diff tests + the five new phase tests).

- [ ] **Step 6: Commit**

```bash
git add cmd/catacomb/diff.go cmd/catacomb/diff_test.go cmd/catacomb/testdata/session_marked.jsonl
git commit -m "feat(cli): --phase/--a-phase/--b-phase scope catacomb diff to a checkpoint

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Final gate (after Task 5)

- [ ] **Run the full suite + coverage + codepolicy**

```bash
go test -race ./... && go test ./internal/codepolicy && make cover
```
Expected: all tests pass; `make cover` reports 100% (file/package/total); codepolicy finds no disallowed comments. If `make cover` flags an uncovered line, add a test for that branch before declaring done — do not lower the threshold.

- [ ] **Confirm no import cycle and clean build**

```bash
go build ./... && go vet ./...
```
Expected: clean.
