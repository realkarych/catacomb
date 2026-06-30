# Checkpoint Subgraph Diff — PR2 (backend extensions) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Round out the phase-scoped subgraph feature with: a unified phase/range selector, a session phase-focus endpoint, `from/to` range scoping in `/v1/diff` and the CLI, a `catacomb subgraph` command, a shared `ErrPhaseNotFound`, a combined `phase` query param, and a focused API doc.

**Architecture:** Extends the merged PR1. The `subgraph` package gains a `Spec{Phase,From,To}` → `Parsed` → window resolver (`ParseSpec`, `RangeWindow`, `ScopeExecutionParsed`) and a shared `ErrPhaseNotFound`. The daemon `/v1/diff` and a new focus endpoint resolve a per-execution window from a `Spec`; the CLI `diff` and a new `subgraph` command do the same. The diff engine and PR1 semantics are unchanged.

**Tech Stack:** Go 1.26, `github.com/realkarych/catacomb`, cobra (CLI), testify, net/http + httptest.

## Global Constraints

- **No comments in Go code.** Only `//go:build`/`//go:embed`/`//go:generate`. Enforced by `go test ./internal/codepolicy`.
- **100% coverage** (file/package/total). Enforced by `make cover`. No unreachable code.
- **TDD:** failing test first, minimal code, commit.
- **Work in the worktree** `/Users/karych/src/catacomb/.claude/worktrees/checkpoint-phase2` (branch `worktree-checkpoint-phase2`). Never the shared checkout.
- Module prefix `github.com/realkarych/catacomb`. Markdown must pass `markdownlint-cli@0.49.0` (blank lines around lists and fenced code blocks).
- Backward compatibility: existing `/v1/diff`, `catacomb diff`, and the subagent endpoint behave unchanged when the new params are absent.

## File Structure

| File | Responsibility |
| --- | --- |
| `subgraph/spec.go` (new) | `Spec`, `Parsed`, `ErrPhaseNotFound`, `ParseSpec`, `RangeWindow`, `ScopeExecutionParsed`. |
| `subgraph/spec_test.go` (new) | Unit tests for the above. |
| `subgraph/resolve.go` (modify in Task 4) | Remove `ScopeExecution` (superseded by `ScopeExecutionParsed`) only AFTER its callers migrate; keep `PhaseWindow`, `ParseSelector`, `ErrInvalidSelector`. |
| `subgraph/resolve_test.go` (modify in Task 4) | Drop `TestScopeExecution` when `ScopeExecution` is removed. |
| `daemon/diff.go` (modify) | `scopedGraph` takes `subgraph.Spec`; `handleDiff` reads `phase`/`aPhase`/`bPhase`/`aFrom`/`aTo`/`bFrom`/`bTo`; `writeScopeErr` uses `subgraph` sentinels; remove daemon-local `ErrPhaseNotFound`. |
| `daemon/phase.go` (new) | `handlePhaseFocus` + `phaseFocusDeltas`. |
| `daemon/server.go` (modify) | Register `GET /v1/sessions/{hash}/phase/{phaseSel}`. |
| `daemon/diff_test.go`, `daemon/phase_test.go` (modify/new) | Tests. |
| `cmd/catacomb/diff.go` (modify) | `--a-from/--a-to/--b-from/--b-to`; `runDiff` builds `Spec`. |
| `cmd/catacomb/subgraph.go` (new) | `catacomb subgraph` command. |
| `cmd/catacomb/root.go` (modify) | Register `newSubgraphCmd()` in the advanced group. |
| `cmd/catacomb/*_test.go` (modify/new) | Tests. |
| `docs/api/phases.md` (new) | Focused API reference for phase diff params, focus endpoint, and `catacomb subgraph`. |

---

### Task 1: `subgraph` — `Spec`, `ParseSpec`, `RangeWindow`, `ScopeExecutionParsed`, `ErrPhaseNotFound`

**Files:**

- Create: `subgraph/spec.go`, `subgraph/spec_test.go`

> Do NOT remove `ScopeExecution` here — daemon (PR1) and the CLI still call it. It is removed in Task 4 once both callers have migrated. After this task `ScopeExecution` (old) and `ScopeExecutionParsed` (new) coexist; both are covered (the old by the existing `TestScopeExecution`, the new by `spec_test.go`).

**Interfaces:**

- Consumes: `Window`, `Subgraph`, `PhaseWindow`, `ParseSelector`, `ErrInvalidSelector` (existing).
- Produces:
  - `var ErrPhaseNotFound = errors.New("subgraph: phase not found")`
  - `type Spec struct { Phase, From, To string }` with `func (s Spec) Empty() bool`
  - `type Parsed struct { … }` (opaque; unexported fields)
  - `func ParseSpec(s Spec) (Parsed, error)` — validates: `From`/`To` both-or-neither (else `ErrInvalidSelector`); `Phase` and `From/To` mutually exclusive (else `ErrInvalidSelector`); parses selectors.
  - `func RangeWindow(nodes []*model.Node, execID, fromName string, fromOcc int, toName string, toOcc int) (Window, bool)` — window `[fromMarker.TStart, toMarker.TStart]`; `false` if either marker absent.
  - `func ScopeExecutionParsed(nodes []*model.Node, edges []*model.Edge, execID string, p Parsed) ([]*model.Node, []*model.Edge, bool)` — resolves a phase or range window then `Subgraph`; `false` if the referenced marker(s) absent.

- [ ] **Step 1: Write the failing test** (`subgraph/spec_test.go`)

```go
package subgraph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
)

func TestSpecEmpty(t *testing.T) {
	assert.True(t, Spec{}.Empty())
	assert.False(t, Spec{Phase: "p"}.Empty())
	assert.False(t, Spec{From: "a", To: "b"}.Empty())
}

func TestParseSpecValidation(t *testing.T) {
	_, err := ParseSpec(Spec{From: "a"})
	assert.ErrorIs(t, err, ErrInvalidSelector)

	_, err = ParseSpec(Spec{To: "b"})
	assert.ErrorIs(t, err, ErrInvalidSelector)

	_, err = ParseSpec(Spec{Phase: "p", From: "a", To: "b"})
	assert.ErrorIs(t, err, ErrInvalidSelector)

	_, err = ParseSpec(Spec{Phase: "p,x"})
	assert.ErrorIs(t, err, ErrInvalidSelector)

	_, err = ParseSpec(Spec{From: "a,x", To: "b"})
	assert.ErrorIs(t, err, ErrInvalidSelector)

	_, err = ParseSpec(Spec{From: "a", To: "b,x"})
	assert.ErrorIs(t, err, ErrInvalidSelector)
}

func TestRangeWindow(t *testing.T) {
	exec := "E"
	fromID := model.PhaseMarkerID(exec, "plan", 0)
	toID := model.PhaseMarkerID(exec, "impl", 0)
	nodes := []*model.Node{
		{ID: fromID, Type: model.NodeMarker, TStart: ts(100), TEnd: ts(150)},
		{ID: toID, Type: model.NodeMarker, TStart: ts(300), TEnd: ts(400)},
	}

	w, ok := RangeWindow(nodes, exec, "plan", 0, "impl", 0)
	require.True(t, ok)
	assert.Equal(t, ts(100).Unix(), w.Start.Unix())
	require.NotNil(t, w.End)
	assert.Equal(t, ts(300).Unix(), w.End.Unix())

	_, ok = RangeWindow(nodes, exec, "plan", 0, "missing", 0)
	assert.False(t, ok)
	_, ok = RangeWindow(nodes, exec, "missing", 0, "impl", 0)
	assert.False(t, ok)
}

func TestScopeExecutionParsedPhaseAndRange(t *testing.T) {
	exec := "E"
	plan := model.PhaseMarkerID(exec, "plan", 0)
	impl := model.PhaseMarkerID(exec, "impl", 0)
	nodes := []*model.Node{
		{ID: plan, Type: model.NodeMarker, TStart: ts(100), TEnd: ts(200)},
		{ID: impl, Type: model.NodeMarker, TStart: ts(300), TEnd: ts(400)},
		{ID: "t-in", Type: model.NodeToolCall, TStart: ts(150)},
		{ID: "t-mid", Type: model.NodeToolCall, TStart: ts(250)},
		{ID: "t-out", Type: model.NodeToolCall, TStart: ts(900)},
	}
	edges := []*model.Edge{}

	pPhase, err := ParseSpec(Spec{Phase: "plan"})
	require.NoError(t, err)
	sn, _, ok := ScopeExecutionParsed(nodes, edges, exec, pPhase)
	require.True(t, ok)
	assert.Equal(t, []string{"t-in"}, ids(sn))

	pRange, err := ParseSpec(Spec{From: "plan", To: "impl"})
	require.NoError(t, err)
	sn, _, ok = ScopeExecutionParsed(nodes, edges, exec, pRange)
	require.True(t, ok)
	assert.Equal(t, []string{"t-in", "t-mid"}, ids(sn))

	pMissing, err := ParseSpec(Spec{Phase: "ghost"})
	require.NoError(t, err)
	_, _, ok = ScopeExecutionParsed(nodes, edges, exec, pMissing)
	assert.False(t, ok)

	pBadRange, err := ParseSpec(Spec{From: "plan", To: "ghost"})
	require.NoError(t, err)
	_, _, ok = ScopeExecutionParsed(nodes, edges, exec, pBadRange)
	assert.False(t, ok)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./subgraph/ -run 'TestSpec|TestParseSpec|TestRangeWindow|TestScopeExecutionParsed' -v`
Expected: FAIL — `undefined: Spec` / `ParseSpec` / `RangeWindow` / `ScopeExecutionParsed` / `ErrPhaseNotFound`.

- [ ] **Step 3: Write `subgraph/spec.go`**

```go
package subgraph

import (
	"errors"
	"fmt"

	"github.com/realkarych/catacomb/model"
)

var ErrPhaseNotFound = errors.New("subgraph: phase not found")

type Spec struct {
	Phase string
	From  string
	To    string
}

func (s Spec) Empty() bool {
	return s.Phase == "" && s.From == "" && s.To == ""
}

type Parsed struct {
	isRange  bool
	name     string
	occ      int
	fromName string
	fromOcc  int
	toName   string
	toOcc    int
}

func ParseSpec(s Spec) (Parsed, error) {
	hasFrom := s.From != ""
	hasTo := s.To != ""
	if hasFrom != hasTo {
		return Parsed{}, fmt.Errorf("%w: from and to must both be set", ErrInvalidSelector)
	}
	if hasFrom {
		if s.Phase != "" {
			return Parsed{}, fmt.Errorf("%w: phase and from/to are mutually exclusive", ErrInvalidSelector)
		}
		fn, fo, err := ParseSelector(s.From)
		if err != nil {
			return Parsed{}, err
		}
		tn, to, err := ParseSelector(s.To)
		if err != nil {
			return Parsed{}, err
		}
		return Parsed{isRange: true, fromName: fn, fromOcc: fo, toName: tn, toOcc: to}, nil
	}
	n, o, err := ParseSelector(s.Phase)
	if err != nil {
		return Parsed{}, err
	}
	return Parsed{name: n, occ: o}, nil
}

func RangeWindow(nodes []*model.Node, execID, fromName string, fromOcc int, toName string, toOcc int) (Window, bool) {
	from, ok := PhaseWindow(nodes, execID, fromName, fromOcc)
	if !ok {
		return Window{}, false
	}
	to, ok := PhaseWindow(nodes, execID, toName, toOcc)
	if !ok {
		return Window{}, false
	}
	end := to.Start
	return Window{Start: from.Start, End: &end}, true
}

func ScopeExecutionParsed(nodes []*model.Node, edges []*model.Edge, execID string, p Parsed) ([]*model.Node, []*model.Edge, bool) {
	var w Window
	var ok bool
	if p.isRange {
		w, ok = RangeWindow(nodes, execID, p.fromName, p.fromOcc, p.toName, p.toOcc)
	} else {
		w, ok = PhaseWindow(nodes, execID, p.name, p.occ)
	}
	if !ok {
		return nil, nil, false
	}
	sn, se := Subgraph(nodes, edges, w)
	return sn, se, true
}
```

- [ ] **Step 4: Run tests + coverage**

Run: `go test ./subgraph/ -v` then `go vet ./subgraph/` then `go test ./internal/codepolicy`
Expected: PASS (existing tests incl. `TestScopeExecution` still green; new `spec_test.go` passes), no policy violations. `go build ./...` must still succeed (daemon + CLI still call the retained `ScopeExecution`).

- [ ] **Step 5: Commit**

```bash
git add subgraph/spec.go subgraph/spec_test.go
git commit -m "feat(subgraph): Spec/Parsed selector, RangeWindow, shared ErrPhaseNotFound

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 2: daemon `/v1/diff` — combined `phase`, per-side overrides, `from/to` range

**Files:**

- Modify: `daemon/diff.go`, `daemon/diff_test.go`

**Interfaces:**

- Consumes: `subgraph.Spec`, `subgraph.ParseSpec`, `subgraph.ScopeExecutionParsed`, `subgraph.ErrPhaseNotFound`, `subgraph.ErrInvalidSelector` (Task 1).
- Produces: `scopedGraph(hash string, spec subgraph.Spec)`; `/v1/diff` reads `phase`, `aPhase`, `bPhase`, `aFrom`, `aTo`, `bFrom`, `bTo`. Per side: `Phase = sidePhase or phase`; `From`/`To` from `a*/b*`. `writeScopeErr` keyed on `subgraph` sentinels. Remove daemon-local `ErrPhaseNotFound`.

- [ ] **Step 1: Write the failing tests** (append to `daemon/diff_test.go`)

```go
func TestHandleDiff_CombinedPhaseParam(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	advancingClock(t)
	phaseSession(t, d)
	srv := httptest.NewServer(d.Handler("testtoken"))
	t.Cleanup(srv.Close)

	resp := getDiff(t, srv, "a=s1&b=s1&phase=phase1")
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var result diff.DiffResult
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Empty(t, result.Added)
	assert.Empty(t, result.Removed)
	assert.Empty(t, result.Changed)
}

func TestHandleDiff_RangeFromTo(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	advancingClock(t)
	phaseSession(t, d)
	srv := httptest.NewServer(d.Handler("testtoken"))
	t.Cleanup(srv.Close)

	resp := getDiff(t, srv, "a=s1&b=s1&aFrom=phase1&aTo=phase2&bFrom=phase1&bTo=phase2")
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var result diff.DiffResult
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Empty(t, result.Added)
}

func TestHandleDiff_RangeRequiresBoth(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	advancingClock(t)
	phaseSession(t, d)
	srv := httptest.NewServer(d.Handler("testtoken"))
	t.Cleanup(srv.Close)

	resp := getDiff(t, srv, "a=s1&b=s1&aFrom=phase1")
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}
```

> Note: `phaseSession` (added in PR1) ingests phase1 and phase2 markers into session `s1`. `getDiff`, `advancingClock`, `fixedExecID`, `tempStore` already exist in the test package.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./daemon/ -run 'TestHandleDiff_CombinedPhaseParam|TestHandleDiff_RangeFromTo|TestHandleDiff_RangeRequiresBoth' -v`
Expected: FAIL — `phase`/`aFrom`/`aTo` ignored today, so range/combined produce wrong status or whole-run diff.

- [ ] **Step 3: Rewrite `daemon/diff.go` scoping**

Replace `var ErrPhaseNotFound = …` (delete it), `scopedGraph`, `writeScopeErr`, and the `handleDiff` query parsing with:

```go
func (d *Daemon) scopedGraph(hash string, spec subgraph.Spec) ([]*model.Node, []*model.Edge, error) {
	if spec.Empty() {
		return d.sessionGraphNodes(hash)
	}
	parsed, err := subgraph.ParseSpec(spec)
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
		sn, se, ok := subgraph.ScopeExecutionParsed(n, e, execID, parsed)
		if !ok {
			continue
		}
		nodes = append(nodes, sn...)
		edges = append(edges, se...)
		found = true
	}
	if !found {
		return nil, nil, subgraph.ErrPhaseNotFound
	}
	return nodes, edges, nil
}

func writeScopeErr(w http.ResponseWriter, hash string, err error) {
	switch {
	case errors.Is(err, ErrSessionNotFound):
		w.WriteHeader(http.StatusNotFound)
	case errors.Is(err, subgraph.ErrPhaseNotFound):
		http.Error(w, fmt.Sprintf("phase not found in session %q", hash), http.StatusBadRequest)
	default:
		http.Error(w, fmt.Sprintf("invalid phase selector for session %q", hash), http.StatusBadRequest)
	}
}

func sideSpec(q url.Values, side string) subgraph.Spec {
	phase := q.Get("phase")
	sidePhase := q.Get(side + "Phase")
	if sidePhase == "" {
		sidePhase = phase
	}
	return subgraph.Spec{
		Phase: sidePhase,
		From:  q.Get(side + "From"),
		To:    q.Get(side + "To"),
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
	d.mu.Lock()
	aN, aE, err := d.scopedGraph(a, sideSpec(q, "a"))
	if err != nil {
		d.mu.Unlock()
		writeScopeErr(w, a, err)
		return
	}
	bN, bE, err := d.scopedGraph(b, sideSpec(q, "b"))
	if err != nil {
		d.mu.Unlock()
		writeScopeErr(w, b, err)
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

Add `"net/url"` to the import block. The existing PR1 tests (`TestHandleDiff_PhaseScopeOK`, `_PhaseNotFound`, `_InvalidSelector`, `_SessionNotFoundWithPhase`, `_PhaseUnionAcrossExecutions`, `_WithinRunPhases`) still pass because `sideSpec` maps `aPhase`/`bPhase` exactly as before and `writeScopeErr`'s arms map the same statuses (`ErrSessionNotFound`→404, phase-not-found→400, invalid→400). The `_SessionNotFoundWithPhase` test passes `aPhase=phase1` for session `nope`: `sideSpec` yields `Spec{Phase:"phase1"}` (non-empty) → `scopedGraph` reaches `executionsForSession`→ empty → `ErrSessionNotFound`→404.

- [ ] **Step 4: Run the full daemon suite**

Run: `go test -race ./daemon/`
Expected: PASS (PR1 diff tests + 3 new).

- [ ] **Step 5: Commit**

```bash
git add daemon/diff.go daemon/diff_test.go
git commit -m "feat(daemon): /v1/diff combined phase param + from/to range scoping

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 3: daemon phase-focus endpoint

**Files:**

- Create: `daemon/phase.go`, `daemon/phase_test.go`
- Modify: `daemon/server.go` (route)

**Interfaces:**

- Consumes: `subgraph.Spec`/`ParseSpec`/`ScopeExecutionParsed`/`ErrPhaseNotFound`/`ErrInvalidSelector`, `deltaToSSE`, `cdc.GraphDelta`, `copyNode`, `copyEdge`, `executionsForSession`.
- Produces: `GET /v1/sessions/{hash}/phase/{phaseSel}` → `[]sseEvent` (node + edge upserts of the phase subgraph). Errors: session not found → 404; phase not found → 404; invalid selector → 400.

- [ ] **Step 1: Write the failing test** (`daemon/phase_test.go`)

```go
package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func getPhase(t *testing.T, srv *httptest.Server, hash, phaseSel string) *http.Response {
	t.Helper()
	resp, err := http.Get(srv.URL + "/v1/sessions/" + hash + "/phase/" + phaseSel + "?token=testtoken")
	require.NoError(t, err)
	return resp
}

func TestHandlePhaseFocus_OK(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	advancingClock(t)
	phaseSession(t, d)
	srv := httptest.NewServer(d.Handler("testtoken"))
	t.Cleanup(srv.Close)

	resp := getPhase(t, srv, "s1", "phase1")
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var evs []sseEvent
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&evs))
}

func TestHandlePhaseFocus_PhaseNotFound(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	advancingClock(t)
	phaseSession(t, d)
	srv := httptest.NewServer(d.Handler("testtoken"))
	t.Cleanup(srv.Close)

	resp := getPhase(t, srv, "s1", "ghost")
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestHandlePhaseFocus_SessionNotFound(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	srv := httptest.NewServer(d.Handler("testtoken"))
	t.Cleanup(srv.Close)

	resp := getPhase(t, srv, "nope", "phase1")
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestHandlePhaseFocus_InvalidSelector(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	advancingClock(t)
	phaseSession(t, d)
	srv := httptest.NewServer(d.Handler("testtoken"))
	t.Cleanup(srv.Close)

	resp := getPhase(t, srv, "s1", "phase1,x")
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./daemon/ -run TestHandlePhaseFocus -v`
Expected: FAIL — route not registered (404 for all, so OK/InvalidSelector assertions fail).

- [ ] **Step 3: Write `daemon/phase.go`**

```go
package daemon

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/realkarych/catacomb/cdc"
	"github.com/realkarych/catacomb/subgraph"
)

func (d *Daemon) phaseFocusDeltas(hash, phaseSel string) ([]sseEvent, error) {
	parsed, err := subgraph.ParseSpec(subgraph.Spec{Phase: phaseSel})
	if err != nil {
		return nil, err
	}
	execs := d.executionsForSession(hash)
	if len(execs) == 0 {
		return nil, ErrSessionNotFound
	}
	out := []sseEvent{}
	found := false
	for _, execID := range execs {
		n, e := d.graphs[execID].Snapshot()
		sn, se, ok := subgraph.ScopeExecutionParsed(n, e, execID, parsed)
		if !ok {
			continue
		}
		found = true
		for _, node := range sn {
			out = append(out, deltaToSSE(cdc.GraphDelta{
				Kind:        cdc.DeltaNodeUpsert,
				Rev:         node.Rev,
				Node:        copyNode(node),
				RunID:       node.RunID,
				ExecutionID: execID,
			}))
		}
		for _, edge := range se {
			out = append(out, deltaToSSE(cdc.GraphDelta{
				Kind:        cdc.DeltaEdgeUpsert,
				Rev:         edge.Rev,
				Edge:        copyEdge(edge),
				RunID:       edge.RunID,
				ExecutionID: execID,
			}))
		}
	}
	if !found {
		return nil, subgraph.ErrPhaseNotFound
	}
	return out, nil
}

func (d *Daemon) handlePhaseFocus(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	phaseSel := r.PathValue("phaseSel")
	d.mu.Lock()
	evs, err := d.phaseFocusDeltas(hash, phaseSel)
	d.mu.Unlock()
	switch {
	case errors.Is(err, ErrSessionNotFound), errors.Is(err, subgraph.ErrPhaseNotFound):
		w.WriteHeader(http.StatusNotFound)
		return
	case errors.Is(err, subgraph.ErrInvalidSelector):
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(evs)
}
```

- [ ] **Step 4: Register the route in `daemon/server.go`**

After the subagent route line, add:

```go
	mux.HandleFunc("GET /v1/sessions/{hash}/phase/{phaseSel}", d.authedAllowQuery(token, d.handlePhaseFocus))
```

- [ ] **Step 5: Run + commit**

Run: `go test -race ./daemon/ -run TestHandlePhaseFocus` then `go test ./internal/codepolicy`
Expected: PASS.

```bash
git add daemon/phase.go daemon/phase_test.go daemon/server.go
git commit -m "feat(daemon): GET /v1/sessions/{hash}/phase/{phaseSel} focus endpoint

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 4: CLI — `diff` from/to flags + `catacomb subgraph` command

**Files:**

- Modify: `cmd/catacomb/diff.go`, `cmd/catacomb/diff_test.go`, `cmd/catacomb/root.go`
- Create: `cmd/catacomb/subgraph.go`, `cmd/catacomb/subgraph_test.go`

**Interfaces:**

- Consumes: `subgraph.Spec`/`ParseSpec`/`ScopeExecutionParsed`/`ErrPhaseNotFound`, `loadGraph`, `newExecutionID`, `model`.
- Produces: diff gains `--a-from/--a-to/--b-from/--b-to`; `runDiff` builds a per-side `subgraph.Spec`. New `catacomb subgraph <input.jsonl> [--phase X | --from Y --to Z] [--json]`.

- [ ] **Step 1: Write the failing tests** (append to `cmd/catacomb/diff_test.go`; create `cmd/catacomb/subgraph_test.go`)

Append to `diff_test.go` (the degenerate `plan→plan` self-diff exercises the from/to wiring without needing a second phase in the fixture; the range *semantics* are unit-tested in Task 1):

```go
func TestRunDiffRange(t *testing.T) {
	result, err := runDiff(diffArgs{
		a: "testdata/session_marked.jsonl", b: "testdata/session_marked.jsonl",
		aFrom: "plan", aTo: "plan", bFrom: "plan", bTo: "plan",
	})
	require.NoError(t, err)
	assert.Empty(t, result.Added)
	assert.Empty(t, result.Removed)
	assert.Empty(t, result.Changed)
}

func TestRunDiffRangeRequiresBoth(t *testing.T) {
	_, err := runDiff(diffArgs{
		a: "testdata/session_marked.jsonl", b: "testdata/session_marked.jsonl",
		aFrom: "plan",
	})
	assert.ErrorIs(t, err, subgraph.ErrInvalidSelector)
}
```

Create `subgraph_test.go`. The `plan` phase window `[10:00:03,10:00:07]` of `testdata/session_marked.jsonl` contains the `pwd` tool (`toolu_2`, at `10:00:05`) but not `ls` (`toolu_1`, before) or `whoami` (`toolu_3`, after). Asserting on these tool-use IDs proves filtering robustly without depending on how many turn/other nodes share the window:

```go
package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/subgraph"
)

func TestSubgraphCommandJSON(t *testing.T) {
	root := newRootCmd()
	var sb strings.Builder
	root.SetOut(&sb)
	root.SetArgs([]string{"subgraph", "--phase", "plan", "--json", "testdata/session_marked.jsonl"})
	require.NoError(t, root.Execute())
	out := sb.String()
	assert.Contains(t, out, "toolu_2")
	assert.NotContains(t, out, "toolu_1")
	assert.NotContains(t, out, "toolu_3")
}

func TestSubgraphCommandHuman(t *testing.T) {
	root := newRootCmd()
	var sb strings.Builder
	root.SetOut(&sb)
	root.SetArgs([]string{"subgraph", "--phase", "plan", "testdata/session_marked.jsonl"})
	require.NoError(t, root.Execute())
	out := sb.String()
	assert.Contains(t, out, "nodes:")
	assert.Contains(t, out, "toolu_2")
	assert.NotContains(t, out, "toolu_1")
}

func TestSubgraphCommandPhaseNotFound(t *testing.T) {
	root := newRootCmd()
	root.SetOut(&strings.Builder{})
	root.SetArgs([]string{"subgraph", "--phase", "ghost", "testdata/session_marked.jsonl"})
	err := root.Execute()
	assert.ErrorIs(t, err, subgraph.ErrPhaseNotFound)
}

func TestSubgraphCommandInvalidSelector(t *testing.T) {
	root := newRootCmd()
	root.SetOut(&strings.Builder{})
	root.SetArgs([]string{"subgraph", "--from", "plan", "testdata/session_marked.jsonl"})
	err := root.Execute()
	assert.ErrorIs(t, err, subgraph.ErrInvalidSelector)
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./cmd/catacomb/ -run 'TestRunDiffRange|TestSubgraphCommand' -v`
Expected: FAIL — `diffArgs` has no `aFrom`/`aTo`; `subgraph` command undefined.

- [ ] **Step 3: Extend `cmd/catacomb/diff.go`**

Replace `diffArgs`, `aSel`/`bSel`, `scopeCLISide`, and the flag block with:

```go
type diffArgs struct {
	a      string
	b      string
	json   bool
	phase  string
	aPhase string
	bPhase string
	aFrom  string
	aTo    string
	bFrom  string
	bTo    string
}

func (a diffArgs) spec(side string) subgraph.Spec {
	phase := a.aPhase
	from, to := a.aFrom, a.aTo
	if side == "b" {
		phase, from, to = a.bPhase, a.bFrom, a.bTo
	}
	if phase == "" {
		phase = a.phase
	}
	return subgraph.Spec{Phase: phase, From: from, To: to}
}

func scopeCLISide(nodes []*model.Node, edges []*model.Edge, execID string, spec subgraph.Spec) ([]*model.Node, []*model.Edge, error) {
	if spec.Empty() {
		return nodes, edges, nil
	}
	parsed, err := subgraph.ParseSpec(spec)
	if err != nil {
		return nil, nil, err
	}
	sn, se, ok := subgraph.ScopeExecutionParsed(nodes, edges, execID, parsed)
	if !ok {
		return nil, nil, fmt.Errorf("diff: phase not found: %w", subgraph.ErrPhaseNotFound)
	}
	return sn, se, nil
}
```

In `runDiff`, change the two `scopeCLISide` call sites (the rest of `runDiff` is unchanged from PR1):

```go
	an, ae, err = scopeCLISide(an, ae, aExec, args.spec("a"))
	if err != nil {
		return catdiff.DiffResult{}, err
	}
	bn, be, err = scopeCLISide(bn, be, bExec, args.spec("b"))
	if err != nil {
		return catdiff.DiffResult{}, err
	}
```

The old `aSel()`/`bSel()` methods are replaced by `spec(side)` above — delete them. Add flags in `newDiffCmd`:

```go
	cmd.Flags().StringVar(&args.aFrom, "a-from", "", "scope side A from this checkpoint name[,occurrence]")
	cmd.Flags().StringVar(&args.aTo, "a-to", "", "scope side A to this checkpoint name[,occurrence]")
	cmd.Flags().StringVar(&args.bFrom, "b-from", "", "scope side B from this checkpoint name[,occurrence]")
	cmd.Flags().StringVar(&args.bTo, "b-to", "", "scope side B to this checkpoint name[,occurrence]")
```

(The existing PR1 tests still pass: `spec("a")` with only `phase` set yields `Spec{Phase: …}`, and an empty spec yields whole-run.)

- [ ] **Step 4: Create `cmd/catacomb/subgraph.go`**

```go
package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/subgraph"
)

type subgraphArgs struct {
	input string
	phase string
	from  string
	to    string
	json  bool
}

func newSubgraphCmd() *cobra.Command {
	a := subgraphArgs{}
	cmd := &cobra.Command{
		Use:   "subgraph <session.jsonl>",
		Short: "Extract the execution subgraph of a checkpoint phase",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, positional []string) error {
			a.input = positional[0]
			nodes, edges, err := runSubgraph(a)
			if err != nil {
				return err
			}
			if a.json {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(map[string]any{"nodes": nodes, "edges": edges})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "nodes: %d  edges: %d\n", len(nodes), len(edges))
			for _, n := range nodes {
				fmt.Fprintf(cmd.OutOrStdout(), "%s %s %s\n", n.Type, n.Name, n.ID)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&a.phase, "phase", "", "phase name[,occurrence]")
	cmd.Flags().StringVar(&a.from, "from", "", "range start checkpoint name[,occurrence]")
	cmd.Flags().StringVar(&a.to, "to", "", "range end checkpoint name[,occurrence]")
	cmd.Flags().BoolVar(&a.json, "json", false, "output as JSON")
	return cmd
}

func runSubgraph(a subgraphArgs) ([]*model.Node, []*model.Edge, error) {
	exec := newExecutionID()
	g, _, err := loadGraph(a.input, exec)
	if err != nil {
		return nil, nil, fmt.Errorf("subgraph: %s: %w (%w)", a.input, err, ErrDiffInput)
	}
	nodes, edges := g.Snapshot()
	spec := subgraph.Spec{Phase: a.phase, From: a.from, To: a.to}
	if spec.Empty() {
		return nil, nil, fmt.Errorf("subgraph: %w: provide --phase or --from/--to", subgraph.ErrInvalidSelector)
	}
	parsed, err := subgraph.ParseSpec(spec)
	if err != nil {
		return nil, nil, err
	}
	sn, se, ok := subgraph.ScopeExecutionParsed(nodes, edges, exec, parsed)
	if !ok {
		return nil, nil, fmt.Errorf("subgraph: phase not found: %w", subgraph.ErrPhaseNotFound)
	}
	return sn, se, nil
}
```

- [ ] **Step 5: Register in `cmd/catacomb/root.go`**

Add to the advanced group (next to `newDiffCmd()`):

```go
	root.AddCommand(advanced(newSubgraphCmd()))
```

- [ ] **Step 6: Remove the now-unused `ScopeExecution`**

At this point daemon (Task 2) and the CLI (this task) both use `ScopeExecutionParsed`, so the old `subgraph.ScopeExecution` has no remaining callers. In `subgraph/resolve.go` delete the `ScopeExecution` function; in `subgraph/resolve_test.go` delete `TestScopeExecution`. Verify nothing else references it:

Run: `grep -rn "ScopeExecution\b" --include=*.go . | grep -v ScopeExecutionParsed`
Expected: no matches outside comments. Then `go build ./...` must succeed.

- [ ] **Step 7: Run + commit**

Run: `go test -race ./cmd/catacomb/ ./subgraph/` then `go test ./internal/codepolicy`
Expected: PASS (existing diff tests + new; subgraph coverage still 100% via `ScopeExecutionParsed`).

```bash
git add cmd/catacomb/diff.go cmd/catacomb/diff_test.go cmd/catacomb/subgraph.go cmd/catacomb/subgraph_test.go cmd/catacomb/root.go subgraph/resolve.go subgraph/resolve_test.go
git commit -m "feat(cli): diff range flags + catacomb subgraph; drop superseded ScopeExecution

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 5: API doc

**Files:**

- Create: `docs/api/phases.md`

**Interfaces:** none (docs only).

- [ ] **Step 1: Write `docs/api/phases.md`**

```markdown
# Phase-scoped diff & subgraph API

Checkpoints (phase markers) are placed with `catacomb mark` / `mcp__catacomb__mark` /
`POST /v1/mark`. The endpoints below scope a diff or a graph view to the subgraph a
checkpoint delimits.

## Selector syntax

A selector is `name` or `name,occurrence` (occurrence defaults to `0`).

## `GET /v1/diff`

Diff two sessions, optionally scoped per side.

| Query param | Meaning |
| --- | --- |
| `a`, `b` | session hashes (required) |
| `phase` | scope BOTH sides to this phase |
| `aPhase`, `bPhase` | per-side phase (overrides `phase` for that side) |
| `aFrom`/`aTo`, `bFrom`/`bTo` | per-side range: subgraph between two checkpoints `[from, to)` |

`from`/`to` must be set together and are mutually exclusive with a phase selector on the
same side. Missing phase → `400`; invalid selector → `400`; unknown session → `404`.
With no selector params the response is byte-for-byte the unscoped diff.

## `GET /v1/sessions/{hash}/phase/{name}`

Returns the subgraph of one phase as a JSON array of SSE-style node/edge upsert events
(the same shape as `GET /v1/sessions/{hash}/subagent/{agentId}`). `{name}` may be
`name,occurrence`. Unknown session or phase → `404`; invalid selector → `400`.

## CLI

- `catacomb diff A.jsonl B.jsonl --phase plan` — diff a phase across two runs.
- `catacomb diff A.jsonl B.jsonl --a-from plan --a-to impl --b-from plan --b-to impl` — range.
- `catacomb subgraph session.jsonl --phase plan [--json]` — print one phase's subgraph.
- `catacomb subgraph session.jsonl --from plan --to impl` — print a range subgraph.
```

- [ ] **Step 2: Verify markdownlint + commit**

Run: `npx --yes markdownlint-cli@0.49.0 docs/api/phases.md`
Expected: no errors.

```bash
git add docs/api/phases.md
git commit -m "docs(api): phase-scoped diff, focus endpoint, subgraph command reference

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Final gate (after Task 5)

- [ ] Run `go test -race ./... && go test ./internal/codepolicy && make cover` → all pass, 100%.
- [ ] Run `npx --yes markdownlint-cli@0.49.0 '**/*.md' --ignore node_modules` → exit 0.
- [ ] Run `go build ./... && go vet ./...` → clean.
