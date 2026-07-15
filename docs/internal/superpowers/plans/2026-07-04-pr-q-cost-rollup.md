# PR-Q: Session Cost Double-Count Fix (V-1 Finding F7) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop double-counting session cost and tokens on the stream-json path. Today `ingest/streamjson/streamjson.go:135-146` maps the terminal `result` event (cumulative usage + `total_cost_usd`) to an `assistant_turn` partial; `reduce/reduce.go:67-72` keys it as `AssistantTurnID(execID, "")` (a `result` has no message id) and prices it `reported` (`pricing/pricing.go:49-50` short-circuit, `cost_source` stamped at `reduce/reduce.go:529`), while every real turn is priced `estimated`. Every rollup then sums all node `CostUSD`/`TokensIn`/`TokensOut`, so totals come out reported + Σ estimated ≈ 1.68× and tokens ≈ 2× (V-1: $0.7482 claude-reported → $1.2560 catacomb-recorded). After this PR: the `result`-derived node is tagged `session_total: true`; rollup cost is reported-when-present-else-Σ-estimated; token sums and phase folds exclude the session-total node; `catacomb runs` on a real run shows exactly the claude-reported cost.

**Architecture:** The tag is an additive node attr `session_total: true`, set in two places that are both pure functions of the observation (so live == `Recover()` == CLI replay, all deterministic): (1) at capture, the `ingest/streamjson` `result` partial carries `"session_total": true` in its attrs — protocol knowledge belongs in the parser; (2) the reducer copies the attr onto the node (the way `model` is copied at `reduce/reduce.go:79-84`) and additionally derives it for legacy observations (source `stream_json`, kind `assistant_turn`, empty `MessageID`, `cost_usd` attr present — a shape only the `result` event can produce: `assistantParts` never emits `cost_usd` and real turns always carry `msg.ID`). The legacy derivation makes existing DBs self-heal: every read path rebuilds graphs from stored observations (`daemon.Recover()` at `daemon/daemon.go:147-150`, CLI `storeGraphsWithIDs` at `cmd/catacomb/storeread.go:32-54`) — nothing persists node totals, so no migration. A `model.Node.SessionTotal()` helper is the single reader. Rollups change in exactly five places: `daemon/sessions.go` `summarizeGraphs` + `(*Daemon).summarizeSession` (shared `costAcc` fold, per-execution-graph precedence), `aggregate/aggregate.go` `runTotals` (per-RunGraph precedence), `foldRunPhases` (exclude session-total members), `foldRunSteps` (defensive guard — assistant turns are not stepkey-eligible per `stepkey/stepkey.go:27-34`, but aggregate consumes arbitrary persisted nodes). Everything else is verified unaffected: `daemon/subagent.go` rollups sum only `AgentID`-bearing inner nodes (the `result` event has no agent), diff builds items only from stepkey-bearing nodes (`diff/diff.go` `buildItems`), TUI/webui/exporters/grpc are per-node passthroughs (the attr rides `encodeAttrs`/`jsonMarshal(rn.Attrs)` automatically), regress/runs/inspect/TUI-sessions consume the fixed `aggregate.Report`/`SessionSummary` transitively. The pseudo-node keeps rendering in TUI/webui (per-node display of the session record is informative and unchanged). Regress verdicts were never wrong (both sides inflated equally); this PR fixes absolute reporting.

**Provenance:** `SessionSummary.CostSource` becomes the source of what was actually summed: `reported` when any session-total contributed, else the existing per-node max-rank over the estimated sum (behavior identical to today when no session-total exists). `aggregate.RunTotals` gains no source field — group medians can mix sources across runs; per-run provenance stays visible via `catacomb runs`. Deliberately deferred.

**Tech Stack:** Go 1.26, testify, table-driven tests. Repo rules: NO comments in Go (codepolicy), 100% coverage TDD-first, gofumpt via `make fmt`, `make lint` clean, deterministic tests (no wall-clock dependence in assertions), every task ends with a commit. PR-P will reshape the same `result` handling next — keep this tag orthogonal (attrs only, no signature changes in `build`).

## Global Constraints

- Work in this worktree only; never touch the shared checkout.
- No comments in Go code — none, not even doc comments (`internal/codepolicy` fails the build otherwise).
- 100% test coverage (`make cover`); TDD: failing test first, minimal implementation. Do not add unreachable branches — they cannot be covered (this is why `costAcc.fold` returns the label, not a rank needing a defaulted decode).
- `make lint` must pass; `make fmt` before committing.
- The tag must ride the observation: both the capture attr and the legacy derivation are pure functions of `model.Observation` — no post-hoc daemon state, so live ingest, `Recover()`, and CLI replay converge on the same graph.
- The attr value is a JSON scalar (`true`) — required by `redact/scalarattrs_guard_test.go`, which fails the build on non-scalar parser attrs.
- Cost equality assertions use `assert.InDelta(…, 1e-12)`: the reported value must pass through unmodified, not approximately.
- Existing tests must keep passing unmodified except where a step below says otherwise; `aggregate` fixture `fixtureGroup()` and its golden `want` are NOT touched — new tests build their own fixtures.
- Line numbers cited are current master (post-#114) anchors; match on code shape if they have drifted.

---

### Task 1: The `session_total` tag — model helper, capture attr, reducer copy + legacy derivation

**Files:**

- Modify: `model/model.go`, `ingest/streamjson/streamjson.go`, `reduce/reduce.go`
- Test: `model/model_test.go`, `ingest/streamjson/streamjson_test.go`, `reduce/reduce_test.go`

**Interfaces:** Produces `func (n *Node) SessionTotal() bool` on `model.Node` and the attr key literal `"session_total"`. Tasks 2–4 depend on the helper.

- [ ] **Step 1: Write failing tests**

Append to `model/model_test.go`:

```go
func TestNodeSessionTotal(t *testing.T) {
	assert.False(t, (&Node{}).SessionTotal())
	assert.False(t, (&Node{Attrs: map[string]any{"session_total": false}}).SessionTotal())
	assert.False(t, (&Node{Attrs: map[string]any{"session_total": "true"}}).SessionTotal())
	assert.True(t, (&Node{Attrs: map[string]any{"session_total": true}}).SessionTotal())
}
```

Append to `ingest/streamjson/streamjson_test.go`:

```go
func TestParseResultTaggedSessionTotal(t *testing.T) {
	fixedNow(time.Now())
	obs, _, err := Parse([]byte(`{"type":"result","session_id":"s","usage":{"input_tokens":1,"output_tokens":2},"total_cost_usd":0.5}`), "e", seq())
	require.NoError(t, err)
	require.Len(t, obs, 1)
	assert.Equal(t, true, obs[0].Attrs["session_total"])

	obs, _, err = Parse([]byte(`{"type":"result","session_id":"s"}`), "e", seq())
	require.NoError(t, err)
	require.Len(t, obs, 1)
	assert.Equal(t, true, obs[0].Attrs["session_total"])

	obs, _, err = Parse([]byte(`{"type":"assistant","session_id":"s","message":{"id":"m1","model":"m","usage":{"input_tokens":1,"output_tokens":1},"content":[{"type":"text","text":"x"}]}}`), "e", seq())
	require.NoError(t, err)
	require.Len(t, obs, 1)
	_, has := obs[0].Attrs["session_total"]
	assert.False(t, has)
}
```

Append to `reduce/reduce_test.go` (helpers `ob`, `execID` already exist; `encoding/json`, `streamjson`, `pricing` already imported):

```go
func TestResultObservationTagsSessionTotalNode(t *testing.T) {
	o := ob("assistant_turn", "", time.Unix(0, 0).UTC())
	o.Source = model.SourceStreamJSON
	o.Attrs = map[string]any{"session_total": true, "model": "m", "tokens_in": int64(7), "tokens_out": int64(9), "cost_usd": 0.5}

	g := NewGraph()
	g.Apply(o)

	n := g.Nodes[model.AssistantTurnID(execID, "")]
	require.NotNil(t, n)
	assert.True(t, n.SessionTotal())
}

func TestLegacyResultObservationTagsSessionTotalNode(t *testing.T) {
	o := ob("assistant_turn", "", time.Unix(0, 0).UTC())
	o.Source = model.SourceStreamJSON
	o.Attrs = map[string]any{"tokens_in": int64(7), "cost_usd": 0.5}

	g := NewGraph()
	g.Apply(o)

	assert.True(t, g.Nodes[model.AssistantTurnID(execID, "")].SessionTotal())
}

func TestMessageTurnWithReportedCostNotSessionTotal(t *testing.T) {
	o := ob("assistant_turn", "", time.Unix(0, 0).UTC())
	o.Source = model.SourceStreamJSON
	o.Correlation.MessageID = "m1"
	o.Attrs = map[string]any{"cost_usd": 0.5}

	g := NewGraph()
	g.Apply(o)

	assert.False(t, g.Nodes[model.AssistantTurnID(execID, "m1")].SessionTotal())
}

func TestNonStreamTurnWithCostNotSessionTotal(t *testing.T) {
	o := ob("assistant_turn", "", time.Unix(0, 0).UTC())
	o.Attrs = map[string]any{"cost_usd": 0.5}

	g := NewGraph()
	g.Apply(o)

	assert.False(t, g.Nodes[model.AssistantTurnID(execID, "")].SessionTotal())
}

func TestSessionTotalTagSurvivesStoreRoundTrip(t *testing.T) {
	o := ob("assistant_turn", "", time.Unix(0, 0).UTC())
	o.Source = model.SourceStreamJSON
	o.Attrs = map[string]any{"session_total": true, "cost_usd": 0.5}

	raw, err := json.Marshal(o)
	require.NoError(t, err)
	var rt model.Observation
	require.NoError(t, json.Unmarshal(raw, &rt))

	g := NewGraph()
	g.Apply(rt)

	assert.True(t, g.Nodes[model.AssistantTurnID(execID, "")].SessionTotal())
}
```

`TestNonStreamTurnWithCostNotSessionTotal` relies on `ob` defaulting `Source` to `model.SourceJSONL`.

- [ ] **Step 2: Run tests, verify they fail**

Run: `go test ./model/ ./ingest/streamjson/ ./reduce/`
Expected: FAIL — `SessionTotal` undefined; missing attr assertions.

- [ ] **Step 3: Implement**

`model/model.go` — add below the `Node` struct:

```go
func (n *Node) SessionTotal() bool {
	v, ok := n.Attrs["session_total"].(bool)
	return ok && v
}
```

`ingest/streamjson/streamjson.go` — in `build`, `case "result":` change the attrs initializer (line 136):

```go
	case "result":
		attrs := map[string]any{"session_total": true}
```

`reduce/reduce.go` — in `Apply`, `case "assistant_turn":`, insert after the `model` attr copy block (after line 84, before `g.mergePayload`):

```go
		if sessionTotalObservation(o) {
			if n.Attrs == nil {
				n.Attrs = map[string]any{}
			}
			n.Attrs["session_total"] = true
		}
```

and add the helper next to `applyCost`:

```go
func sessionTotalObservation(o model.Observation) bool {
	if v, ok := o.Attrs["session_total"].(bool); ok && v {
		return true
	}
	if o.Source != model.SourceStreamJSON || o.Correlation.MessageID != "" {
		return false
	}
	_, ok := o.Attrs["cost_usd"]
	return ok
}
```

Note `TestResultObservationTagsSessionTotalNode` uses `NewGraph()` (nil pricer → `applyCost` no-ops → `n.Attrs` may be nil at tag time), covering the nil-map branch; the reduce test with a pricer already existing (`TestAssistantTurnReportedCostWinsOverCacheEstimate`) keeps its behavior — its turn has a `MessageID`, so it is NOT tagged.

- [ ] **Step 4: Run tests, gates, commit**

Run: `go test ./model/ ./ingest/streamjson/ ./reduce/ ./redact/ && make fmt && make lint && make cover`
Expected: all green, coverage 100% (the redact scalar-attrs guard confirms the new attr is a scalar).

```bash
git add model/ ingest/streamjson/ reduce/
git commit -m "feat(reduce): tag stream-json result node session_total (F7)"
```

---

### Task 2: Daemon rollups — reported-over-estimated cost fold, token exclusion

**Files:**

- Modify: `daemon/sessions.go` (both `summarizeGraphs` and `(*Daemon).summarizeSession`)
- Test: `daemon/sessions_test.go`

**Interfaces:** Produces unexported `costAcc` with `add(*model.Node)` and `fold() (float64, string, int, bool)` — (total, source label, source rank, has-cost). Precedence unit is one execution graph: within a graph's matched nodes, reported (session-total) replaces Σ estimated; across graphs contributions add (a resumed/mixed session sums each execution's best value).

- [ ] **Step 1: Write failing tests**

Append to `daemon/sessions_test.go` (mirrors `TestSummarizeGraphsCoverage` style):

```go
func sessionTotalGraph(withResult bool, resultCost *float64) *reduce.Graph {
	g := reduce.NewGraph()
	g.Runs["s1"] = &model.Run{ID: "s1", Status: model.StatusOK, SessionIDs: []string{"s1"}}
	est1, est2 := 0.30, 0.40
	ti1, to1 := int64(100), int64(50)
	ti2, to2 := int64(200), int64(70)
	g.Nodes["e:turn:m1"] = &model.Node{ID: "e:turn:m1", RunID: "s1", Type: model.NodeAssistantTurn, CostUSD: &est1, TokensIn: &ti1, TokensOut: &to1, Attrs: map[string]any{"cost_source": "estimated"}}
	g.Nodes["e:turn:m2"] = &model.Node{ID: "e:turn:m2", RunID: "s1", Type: model.NodeAssistantTurn, CostUSD: &est2, TokensIn: &ti2, TokensOut: &to2, Attrs: map[string]any{"cost_source": "estimated"}}
	if withResult {
		tit, tot := int64(999), int64(999)
		n := &model.Node{ID: "e:turn:", RunID: "s1", Type: model.NodeAssistantTurn, TokensIn: &tit, TokensOut: &tot, Attrs: map[string]any{"session_total": true}}
		if resultCost != nil {
			n.CostUSD = resultCost
			n.Attrs["cost_source"] = "reported"
		}
		g.Nodes["e:turn:"] = n
	}
	return g
}

func TestSummarizeRunReportedSessionTotalReplacesEstimates(t *testing.T) {
	rep := 0.50
	sum := SummarizeRun("s1", []*reduce.Graph{sessionTotalGraph(true, &rep)})

	require.NotNil(t, sum.CostUSD)
	assert.InDelta(t, 0.50, *sum.CostUSD, 1e-12)
	assert.Equal(t, "reported", sum.CostSource)
	assert.Equal(t, int64(300), sum.TokensIn)
	assert.Equal(t, int64(120), sum.TokensOut)
}

func TestSummarizeRunFallsBackToEstimatesWithoutResult(t *testing.T) {
	sum := SummarizeRun("s1", []*reduce.Graph{sessionTotalGraph(false, nil)})

	require.NotNil(t, sum.CostUSD)
	assert.InDelta(t, 0.70, *sum.CostUSD, 1e-12)
	assert.Equal(t, "estimated", sum.CostSource)
	assert.Equal(t, int64(300), sum.TokensIn)
	assert.Equal(t, int64(120), sum.TokensOut)
}

func TestSummarizeRunSessionTotalWithoutCostStillExcludedFromTokens(t *testing.T) {
	sum := SummarizeRun("s1", []*reduce.Graph{sessionTotalGraph(true, nil)})

	require.NotNil(t, sum.CostUSD)
	assert.InDelta(t, 0.70, *sum.CostUSD, 1e-12)
	assert.Equal(t, "estimated", sum.CostSource)
	assert.Equal(t, int64(300), sum.TokensIn)
	assert.Equal(t, int64(120), sum.TokensOut)
}

func TestSummarizeRunMixedGraphsSumPerGraphBest(t *testing.T) {
	rep := 0.50
	g1 := sessionTotalGraph(true, &rep)
	g2 := reduce.NewGraph()
	g2.Runs["s1"] = &model.Run{ID: "s1", Status: model.StatusOK, SessionIDs: []string{"s1"}}
	est := 0.20
	g2.Nodes["e2:turn:m9"] = &model.Node{ID: "e2:turn:m9", RunID: "s1", Type: model.NodeAssistantTurn, CostUSD: &est, Attrs: map[string]any{"cost_source": "estimated"}}

	sum := SummarizeRun("s1", []*reduce.Graph{g1, g2})

	require.NotNil(t, sum.CostUSD)
	assert.InDelta(t, 0.70, *sum.CostUSD, 1e-12)
	assert.Equal(t, "reported", sum.CostSource)
}

func TestSessionSummariesReportedSessionTotal(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))

	d.mu.Lock()
	g := d.graphs["exec1"]
	est, rep := 0.30, 0.50
	ti, to := int64(100), int64(40)
	tit, tot := int64(999), int64(999)
	g.Nodes["exec1:turn:m1"] = &model.Node{ID: "exec1:turn:m1", RunID: "s1", Type: model.NodeAssistantTurn, CostUSD: &est, TokensIn: &ti, TokensOut: &to, Attrs: map[string]any{"cost_source": "estimated"}}
	g.Nodes["exec1:turn:"] = &model.Node{ID: "exec1:turn:", RunID: "s1", Type: model.NodeAssistantTurn, CostUSD: &rep, TokensIn: &tit, TokensOut: &tot, Attrs: map[string]any{"cost_source": "reported", "session_total": true}}
	sums := d.sessionSummaries()
	d.mu.Unlock()

	require.Len(t, sums, 1)
	require.NotNil(t, sums[0].CostUSD)
	assert.InDelta(t, 0.50, *sums[0].CostUSD, 1e-12)
	assert.Equal(t, "reported", sums[0].CostSource)
	assert.Equal(t, int64(100), sums[0].TokensIn)
	assert.Equal(t, int64(40), sums[0].TokensOut)
}
```

- [ ] **Step 2: Run tests, verify they fail**

Run: `go test ./daemon/ -run 'SessionTotal|SummarizeRunReported|SummarizeRunFalls|SummarizeRunMixed|SessionSummariesReported'`
Expected: FAIL — pre-fix code sums reported + estimated (cost 1.20 instead of 0.50) and includes the session-total tokens (1299/1119 instead of 300/120).

- [ ] **Step 3: Implement**

Add to `daemon/sessions.go`:

```go
type costAcc struct {
	reported     float64
	hasReported  bool
	estimated    float64
	hasEstimated bool
	srcRank      int
	src          string
}

func (a *costAcc) add(n *model.Node) {
	if n.CostUSD == nil {
		return
	}
	if n.SessionTotal() {
		a.hasReported = true
		a.reported += *n.CostUSD
		return
	}
	a.hasEstimated = true
	a.estimated += *n.CostUSD
	src, _ := n.Attrs["cost_source"].(string)
	var rank int
	switch src {
	case "reported":
		rank = 2
	case "estimated":
		rank = 1
	}
	if rank > a.srcRank {
		a.srcRank = rank
		a.src = src
	}
}

func (a *costAcc) fold() (float64, string, int, bool) {
	if a.hasReported {
		return a.reported, "reported", 2, true
	}
	if a.hasEstimated {
		return a.estimated, a.src, a.srcRank, true
	}
	return 0, "", 0, false
}
```

In `summarizeGraphs` (and identically in `(*Daemon).summarizeSession`): declare `var acc costAcc` immediately before the `for _, n := range g.Nodes` loop; inside the loop replace the token sums and the whole `if n.CostUSD != nil { … }` block with:

```go
			if !n.SessionTotal() {
				if n.TokensIn != nil {
					tokensIn += *n.TokensIn
				}
				if n.TokensOut != nil {
					tokensOut += *n.TokensOut
				}
			}
			acc.add(n)
```

and immediately after the node loop (still inside the graphs/executions loop) add:

```go
		if c, src, rank, ok := acc.fold(); ok {
			hasCost = true
			totalCost += c
			if rank > srcRank {
				srcRank = rank
				sum.CostSource = src
			}
		}
```

Behavior for graphs without session-total nodes is bit-identical to today (same rank logic, same `""` source for cost-without-attr nodes), so `TestSessionSummaryWithCost`, `TestSessionSummaryWithEstimatedCost`, and `TestSummarizeGraphsCoverage` pass unmodified. `NodeCount`/`CountsByType` still include the pseudo-node (it is a real graph node).

- [ ] **Step 4: Run tests, gates, commit**

Run: `go test ./daemon/ && make fmt && make lint && make cover`

```bash
git add daemon/
git commit -m "fix(daemon): session/run cost is reported-else-estimated; tokens exclude session_total (F7)"
```

---

### Task 3: Aggregate rollups — run totals precedence, phase/step exclusion

**Files:**

- Modify: `aggregate/aggregate.go` (`runTotals`, `foldRunPhases`, `foldRunSteps`)
- Test: `aggregate/aggregate_test.go`

- [ ] **Step 1: Write failing tests**

Append to `aggregate/aggregate_test.go` (do NOT touch `fixtureGroup`):

```go
func sessionTotalRun(withResult bool, resultCost *float64) RunGraph {
	nodes := []*model.Node{
		{ID: "e:sess", RunID: "r1", Type: model.NodeSession, Status: model.StatusOK},
		{ID: "e:turn:m1", RunID: "r1", Type: model.NodeAssistantTurn, Status: model.StatusOK, CostUSD: f64(0.30), TokensIn: i64(100), TokensOut: i64(50)},
		{ID: "e:turn:m2", RunID: "r1", Type: model.NodeAssistantTurn, Status: model.StatusOK, CostUSD: f64(0.40), TokensIn: i64(200), TokensOut: i64(70)},
	}
	if withResult {
		n := &model.Node{ID: "e:turn:", RunID: "r1", Type: model.NodeAssistantTurn, Status: model.StatusOK, TokensIn: i64(999), TokensOut: i64(999), Attrs: map[string]any{"session_total": true}}
		n.CostUSD = resultCost
		nodes = append(nodes, n)
	}
	return RunGraph{Run: model.Run{ID: "r1", Status: model.StatusOK}, Nodes: nodes}
}

func TestRunTotalsPreferReportedSessionTotal(t *testing.T) {
	r := runTotals([]RunGraph{sessionTotalRun(true, f64(0.50))})

	assert.InDelta(t, 0.50, r.CostUSD.Median, 1e-12)
	assert.Equal(t, float64(300), r.TokensIn.Median)
	assert.Equal(t, float64(120), r.TokensOut.Median)
	assert.Equal(t, float64(4), r.Nodes.Median)
}

func TestRunTotalsFallBackToEstimates(t *testing.T) {
	r := runTotals([]RunGraph{sessionTotalRun(false, nil)})

	assert.InDelta(t, 0.70, r.CostUSD.Median, 1e-12)
	assert.Equal(t, float64(300), r.TokensIn.Median)
	assert.Equal(t, float64(120), r.TokensOut.Median)
}

func TestRunTotalsSessionTotalWithoutCostFallsBack(t *testing.T) {
	r := runTotals([]RunGraph{sessionTotalRun(true, nil)})

	assert.InDelta(t, 0.70, r.CostUSD.Median, 1e-12)
	assert.Equal(t, float64(300), r.TokensIn.Median)
}

func TestRunTotalsSupersededSessionTotalIgnored(t *testing.T) {
	rg := sessionTotalRun(true, f64(0.50))
	rg.Nodes[3].Status = model.StatusSuperseded

	r := runTotals([]RunGraph{rg})

	assert.InDelta(t, 0.70, r.CostUSD.Median, 1e-12)
	assert.Equal(t, float64(3), r.Nodes.Median)
}

func TestPhaseFoldExcludesSessionTotalMember(t *testing.T) {
	t0 := fixtureBase
	group := []RunGraph{{
		Run: model.Run{ID: "r1", Status: model.StatusOK},
		Nodes: []*model.Node{
			{ID: "m1", RunID: "r1", Type: model.NodeMarker, Name: "phase", Status: model.StatusOK, PhaseKey: "p1", TStart: tp(t0), TEnd: tp(t0.Add(time.Second))},
			{ID: "in-window", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusOK, CostUSD: f64(1), TokensIn: i64(10), TokensOut: i64(5)},
			{ID: "e:turn:", RunID: "r1", Type: model.NodeAssistantTurn, Status: model.StatusOK, CostUSD: f64(9), TokensIn: i64(900), TokensOut: i64(900), Attrs: map[string]any{"session_total": true}},
		},
		Edges: []*model.Edge{
			{Type: model.EdgeMarkerSpan, Src: "m1", Dst: "in-window"},
			{Type: model.EdgeMarkerSpan, Src: "m1", Dst: "e:turn:"},
		},
	}}

	rep := Aggregate(group, Options{})

	require.Len(t, rep.Phases, 1)
	assert.Equal(t, float64(1), rep.Phases[0].CostUSD.Median)
	assert.Equal(t, float64(10), rep.Phases[0].TokensIn.Median)
	assert.Equal(t, float64(5), rep.Phases[0].TokensOut.Median)
}

func TestStepFoldExcludesSessionTotal(t *testing.T) {
	group := []RunGraph{{
		Run: model.Run{ID: "r1", Status: model.StatusOK},
		Nodes: []*model.Node{
			{ID: "a", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusOK, StepKey: "s1", CostUSD: f64(1)},
			{ID: "b", RunID: "r1", Type: model.NodeAssistantTurn, Status: model.StatusOK, StepKey: "s1", CostUSD: f64(9), Attrs: map[string]any{"session_total": true}},
		},
	}}

	rep := Aggregate(group, Options{})

	require.Len(t, rep.Steps, 1)
	assert.Equal(t, float64(1), rep.Steps[0].CostUSD.Median)
	assert.Equal(t, float64(1), rep.Steps[0].Occurrences.Median)
}
```

(`TestStepFoldExcludesSessionTotal` is the reachable cover for the defensive guard: real assistant turns never earn a `StepKey` — `stepkey.go:27-34` — but aggregate operates on arbitrary persisted nodes.)

- [ ] **Step 2: Run tests, verify they fail**

Run: `go test ./aggregate/`

- [ ] **Step 3: Implement**

`foldRunSteps` — extend the skip condition:

```go
		if n.StepKey == "" || !included(n) || n.SessionTotal() {
			continue
		}
```

`foldRunPhases` — extend the member skip:

```go
			for _, mid := range members[n.ID] {
				m := byID[mid]
				if m == nil || !included(m) || m.SessionTotal() {
					continue
				}
```

`runTotals` — replace the per-node accumulation loop body:

```go
		var reported float64
		hasReported := false
		for _, n := range rg.Nodes {
			if !included(n) {
				continue
			}
			count++
			if n.Status == model.StatusError {
				hasError = true
			}
			if n.SessionTotal() {
				if n.CostUSD != nil {
					hasReported = true
					reported += *n.CostUSD
				}
				continue
			}
			sums.cost += derefF(n.CostUSD)
			sums.tokensIn += derefI(n.TokensIn)
			sums.tokensOut += derefI(n.TokensOut)
		}
		if hasReported {
			sums.cost = reported
		}
```

(`var reported`/`hasReported` go inside the `for _, rg := range group` loop, next to `var sums metricSums`.) The existing fixture tests pass unchanged — `fixtureGroup` has no session-total nodes. Regress (`regress/regress.go:104-106`, `:220-222`) consumes `aggregate.Report` and is fixed transitively; persisted baselines will show absolute cost/token deltas against new candidates — expected and correct, verdicts compare like-with-like within one invocation.

- [ ] **Step 4: Run tests, gates, commit**

Run: `go test ./aggregate/ ./regress/ && make fmt && make lint && make cover`

```bash
git add aggregate/
git commit -m "fix(aggregate): run totals prefer reported session total; folds exclude it (F7)"
```

---

### Task 4: End-to-end F7 regression fixture — ingest → reduce → summary, live == Recover

**Files:**

- Test: `daemon/server_test.go` (stream-json HTTP fixture, mirrors `TestStreamJSONHTTPThreadsSessionAcrossLines`)

- [ ] **Step 1: Write the tests (they must pass already — this task pins the F7 scenario end-to-end)**

```go
const f7Stream = `{"type":"system","subtype":"init","session_id":"s1","model":"claude-haiku-4-5"}
{"type":"assistant","session_id":"s1","message":{"id":"m1","model":"claude-haiku-4-5","usage":{"input_tokens":1000,"output_tokens":500},"content":[{"type":"text","text":"a"}]}}
{"type":"assistant","session_id":"s1","message":{"id":"m2","model":"claude-haiku-4-5","usage":{"input_tokens":2000,"output_tokens":700},"content":[{"type":"text","text":"b"}]}}
`

const f7Result = `{"type":"result","session_id":"s1","usage":{"input_tokens":3000,"output_tokens":1200},"total_cost_usd":0.0421}
`

func ingestF7(t *testing.T, d *Daemon, body string) {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/v1/stream-json", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer tok")
	rec := httptest.NewRecorder()
	d.Handler("tok").ServeHTTP(rec, r)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Eventually(t, func() bool { return len(d.GraphsForTest()) == 1 }, time.Second, 10*time.Millisecond)
}

func TestStreamJSONSessionCostNotDoubleCounted(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	ingestF7(t, d, f7Stream+f7Result)

	d.mu.Lock()
	sums := d.sessionSummaries()
	d.mu.Unlock()

	require.Len(t, sums, 1)
	require.NotNil(t, sums[0].CostUSD)
	assert.InDelta(t, 0.0421, *sums[0].CostUSD, 1e-12)
	assert.Equal(t, "reported", sums[0].CostSource)
	assert.Equal(t, int64(3000), sums[0].TokensIn)
	assert.Equal(t, int64(1200), sums[0].TokensOut)
}

func TestStreamJSONNoResultFallsBackToEstimates(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	ingestF7(t, d, f7Stream)

	d.mu.Lock()
	sums := d.sessionSummaries()
	d.mu.Unlock()

	require.Len(t, sums, 1)
	require.NotNil(t, sums[0].CostUSD)
	assert.InDelta(t, 0.009, *sums[0].CostUSD, 1e-12)
	assert.Equal(t, "estimated", sums[0].CostSource)
	assert.Equal(t, int64(3000), sums[0].TokensIn)
	assert.Equal(t, int64(1200), sums[0].TokensOut)
}

func TestStreamJSONCostRecoverMatchesLive(t *testing.T) {
	st := tempStore(t)
	d1 := New(st)
	fixedExecID(d1)
	ingestF7(t, d1, f7Stream+f7Result)

	d1.mu.Lock()
	live := d1.sessionSummaries()
	d1.mu.Unlock()

	d2 := New(st)
	require.NoError(t, d2.Recover())
	d2.mu.Lock()
	recovered := d2.sessionSummaries()
	d2.mu.Unlock()

	require.Len(t, recovered, 1)
	require.NotNil(t, recovered[0].CostUSD)
	assert.InDelta(t, *live[0].CostUSD, *recovered[0].CostUSD, 1e-12)
	assert.Equal(t, live[0].CostSource, recovered[0].CostSource)
	assert.Equal(t, live[0].TokensIn, recovered[0].TokensIn)
	assert.Equal(t, live[0].TokensOut, recovered[0].TokensOut)
}
```

Arithmetic: haiku tier (`pricing/pricing.go:103`) prices m1 at 1000/1e6·$1 + 500/1e6·$5 = $0.0035 and m2 at $0.0055 → Σ estimated $0.009; the `result` reports $0.0421 ≠ Σ (that inequality is the point — pre-fix code returns 0.0511). The result usage (3000/1200) intentionally equals Σ per-turn usage, as it does live (cumulative), so `TokensIn == 3000` proves the session-total node was excluded (pre-fix: 6000). If `tempStore` cannot be shared across two `New` calls, open the second store handle on the same path instead — the point is `Recover()` replays the same stored observations.

The recover test is also the old-DB self-heal proof in miniature: stored observations drive everything; a DB written by pre-fix binaries replays through `sessionTotalObservation`'s legacy branch (stream-json turn, empty message id, `cost_usd` present) and produces the corrected totals with no migration.

- [ ] **Step 2: Run, gates, commit**

Run: `go test ./daemon/ -run 'F7|StreamJSONSession|StreamJSONNoResult|StreamJSONCostRecover' && make fmt && make lint && make cover`

```bash
git add daemon/
git commit -m "test(daemon): F7 end-to-end fixture — reported total, no token doubling, recover parity"
```

---

### Task 5: Docs, full gates, live verify

**Files:**

- Modify: `docs/guide/cli.md` (runs section, ~line 461)

- [ ] **Step 1: Docs**

In `docs/guide/cli.md` `### runs`, extend the output paragraph ("Outputs a table (or JSON) of runs with status, start time, tool counts, token counts, and cost.") with:

```markdown
Cost prefers the session total reported by Claude Code itself (the stream-json
`result` event; `cost_source: reported`) and falls back to summing per-turn
estimates when no reported total exists. Token totals always come from per-turn
usage — the cumulative `result` record is never double-counted. Databases
written by older catacomb versions are re-summarized correctly on read; no
migration is needed.
```

Run: `npx -y markdownlint-cli@0.49.0 'docs/**/*.md'` — 0 errors.

- [ ] **Step 2: Full gates**

Run: `make fmt && make lint && make cover`
Expected: fmt clean, lint 0 issues, coverage 100% everywhere.

- [ ] **Step 3: Live verify (real `claude -p` through a temp daemon)**

```bash
make build
TMP=$(mktemp -d)
bin/catacomb daemon --db "$TMP/cat.db" --discovery "$TMP/disc.json" > "$TMP/daemon.log" 2>&1 &
DPID=$!
until [ -f "$TMP/disc.json" ]; do sleep 0.2; done
CATACOMB_DISCOVERY="$TMP/disc.json" bin/catacomb run -- \
  claude -p 'Reply with exactly: ok' --model claude-haiku-4-5 \
  --output-format stream-json --verbose > "$TMP/stream.ndjson"
sleep 2
REPORTED=$(python3 -c "import json;print([json.loads(l)['total_cost_usd'] for l in open('$TMP/stream.ndjson') if l.strip() and json.loads(l).get('type')=='result'][0])")
bin/catacomb runs --db "$TMP/cat.db" --json > "$TMP/runs.json"
python3 - "$REPORTED" "$TMP/runs.json" <<'EOF'
import json, sys
reported = float(sys.argv[1])
runs = json.load(open(sys.argv[2]))
[r] = runs
assert abs(r["cost_usd"] - reported) < 1e-9, (r["cost_usd"], reported)
assert r["cost_source"] == "reported"
print("live-verify ok: recorded", r["cost_usd"], "== reported", reported)
EOF
kill $DPID
```

Expected: the assertion passes — catacomb-recorded cost equals claude-reported `total_cost_usd` exactly (pre-fix this was ≈1.68×). Adapt the JSON field access if the `runs --json` envelope differs; the invariant is exact equality plus `cost_source == "reported"`. Capture the `live-verify ok` line for the PR description.

- [ ] **Step 4: Commit docs**

```bash
git add docs/guide/cli.md
git commit -m "docs(guide): runs cost semantics — reported total preferred, no double count (F7)"
```

---

## Verification checklist

- `catacomb runs` on the V-1 dogfood DB (if available) now shows ≈$0.7482-consistent per-run costs instead of ≈$1.2560 aggregates — old DBs self-heal on read because all totals are recomputed from observations (`daemon/daemon.go:147-150`, `cmd/catacomb/storeread.go:32-54`); nothing stores baked totals.
- Non-changes held: subagent descendant rollups (`daemon/subagent.go` — pseudo-node has no `AgentID`), diff (`diff/diff.go` `buildItems` — no `StepKey`), per-node display in TUI/webui (unchanged, pseudo-node still renders with its `reported` provenance), exporters/grpc (attr rides existing attr serialization).
- Determinism: the tag derives from observation content only; live ingest, `Recover()`, and CLI replay agree (Task 4 recover test).
