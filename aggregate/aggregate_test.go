package aggregate

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
)

func i64(v int64) *int64 { return &v }

func f64(v float64) *float64 { return &v }

func tp(t time.Time) *time.Time { return &t }

var fixtureBase = time.Unix(1_700_000_000, 0).UTC()

func fixtureGroup() []RunGraph {
	t0 := fixtureBase
	r1 := RunGraph{
		Run: model.Run{ID: "r1", Status: model.StatusOK, StartedAt: tp(t0), EndedAt: tp(t0.Add(10 * time.Second))},
		Nodes: []*model.Node{
			{ID: "r1-sess", RunID: "r1", Type: model.NodeSession, Status: model.StatusOK},
			{ID: "r1-s1b", RunID: "r1", Type: model.NodeToolCall, Name: "search2", Status: model.StatusOK, StepKey: "s1", CostUSD: f64(2), TokensIn: i64(20), TokensOut: i64(10), DurationMS: i64(200), Annotations: map[string]any{"eval.score": float64(0.25), "eval.latency": []byte("0.125")}},
			{ID: "r1-s1a", RunID: "r1", Type: model.NodeToolCall, Name: "search", Status: model.StatusOK, StepKey: "s1", CostUSD: f64(1), TokensIn: i64(10), TokensOut: i64(5), DurationMS: i64(100), Annotations: map[string]any{"eval.score": json.RawMessage("0.5"), "eval.note": json.RawMessage(`"good"`), "misc.ignored": json.RawMessage("1")}},
			{ID: "r1-s2", RunID: "r1", Type: model.NodeToolCall, Name: "build", Status: model.StatusOK, StepKey: "s2", CostUSD: f64(5), TokensIn: i64(50), TokensOut: i64(25), DurationMS: i64(500)},
			{ID: "r1-m1", RunID: "r1", Type: model.NodeMarker, Name: "phase-one", Status: model.StatusOK, PhaseKey: "p1", TStart: tp(t0), TEnd: tp(t0.Add(2000 * time.Millisecond))},
			{ID: "r1-mem1", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusOK, CostUSD: f64(10), TokensIn: i64(100), TokensOut: i64(50), DurationMS: i64(9999)},
			{ID: "r1-mem2", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusOK, CostUSD: f64(20), TokensIn: i64(200), TokensOut: i64(100)},
		},
		Edges: []*model.Edge{
			{Type: model.EdgeMarkerSpan, Src: "r1-m1", Dst: "r1-mem1"},
			{Type: model.EdgeMarkerSpan, Src: "r1-m1", Dst: "r1-mem2"},
			{Type: model.EdgeMarkerSpan, Src: "r1-m1", Dst: "r1-missing"},
		},
	}
	r2 := RunGraph{
		Run: model.Run{ID: "r2", Status: model.StatusOK, StartedAt: tp(t0), EndedAt: tp(t0.Add(20 * time.Second))},
		Nodes: []*model.Node{
			{ID: "r2-sess", RunID: "r2", Type: model.NodeSession, Status: model.StatusOK},
			{ID: "r2-s1", RunID: "r2", Type: model.NodeToolCall, Name: "search", Status: model.StatusError, StepKey: "s1", CostUSD: f64(4), TokensIn: i64(40), TokensOut: i64(20), DurationMS: i64(400), Annotations: map[string]any{"eval.score": json.RawMessage("0.5"), "eval.bad": true}},
			{ID: "r2-m1", RunID: "r2", Type: model.NodeMarker, Name: "phase-one-r2", Status: model.StatusError, PhaseKey: "p1", TStart: tp(t0), TEnd: tp(t0.Add(4000 * time.Millisecond))},
			{ID: "r2-m2", RunID: "r2", Type: model.NodeMarker, Name: "phase-one-r2b", Status: model.StatusOK, PhaseKey: "p1", TStart: tp(t0)},
			{ID: "r2-mem1", RunID: "r2", Type: model.NodeToolCall, Status: model.StatusOK, CostUSD: f64(30), TokensIn: i64(300), TokensOut: i64(150)},
		},
		Edges: []*model.Edge{
			{Type: model.EdgeMarkerSpan, Src: "r2-m1", Dst: "r2-mem1"},
		},
	}
	r3 := RunGraph{
		Run: model.Run{ID: "r3", Status: model.StatusError, StartedAt: tp(t0)},
		Nodes: []*model.Node{
			{ID: "r3-sess", RunID: "r3", Type: model.NodeSession, Status: model.StatusOK},
			{ID: "r3-s1", RunID: "r3", Type: model.NodeToolCall, Name: "search", Status: model.StatusOK, StepKey: "s1", TokensIn: i64(60), Annotations: map[string]any{"eval.score": json.RawMessage(`"n/a"`)}},
		},
	}
	return []RunGraph{r1, r2, r3}
}

func TestAggregateFixture(t *testing.T) {
	want := Report{
		Runs: 3,
		Steps: []Row{
			{
				Key: "s1", Name: "search", Present: 3, PresenceRate: 1,
				StatusRates: map[model.Status]float64{model.StatusOK: 2.0 / 3, model.StatusError: 1.0 / 3},
				Occurrences: MetricStats{N: 3, Median: 1, P25: 1, P75: 2, P90: 2},
				DurationMS:  MetricStats{N: 2, Median: 300, P25: 300, P75: 400, P90: 400},
				CostUSD:     MetricStats{N: 3, Median: 3, P25: 0, P75: 4, P90: 4},
				TokensIn:    MetricStats{N: 3, Median: 40, P25: 30, P75: 60, P90: 60},
				TokensOut:   MetricStats{N: 3, Median: 15, P25: 0, P75: 20, P90: 20},
			},
			{
				Key: "s2", Name: "build", Present: 1, PresenceRate: 1.0 / 3,
				StatusRates: map[model.Status]float64{model.StatusOK: 1},
				Occurrences: MetricStats{N: 1, Median: 1, P25: 1, P75: 1, P90: 1},
				DurationMS:  MetricStats{N: 1, Median: 500, P25: 500, P75: 500, P90: 500},
				CostUSD:     MetricStats{N: 1, Median: 5, P25: 5, P75: 5, P90: 5},
				TokensIn:    MetricStats{N: 1, Median: 50, P25: 50, P75: 50, P90: 50},
				TokensOut:   MetricStats{N: 1, Median: 25, P25: 25, P75: 25, P90: 25},
			},
		},
		Phases: []Row{
			{
				Key: "p1", Name: "phase-one", Present: 2, PresenceRate: 2.0 / 3,
				StatusRates: map[model.Status]float64{model.StatusOK: 0.5, model.StatusError: 0.5},
				Occurrences: MetricStats{N: 2, Median: 1, P25: 1, P75: 2, P90: 2},
				DurationMS:  MetricStats{N: 2, Median: 2000, P25: 2000, P75: 4000, P90: 4000},
				CostUSD:     MetricStats{N: 2, Median: 30, P25: 30, P75: 30, P90: 30},
				TokensIn:    MetricStats{N: 2, Median: 300, P25: 300, P75: 300, P90: 300},
				TokensOut:   MetricStats{N: 2, Median: 150, P25: 150, P75: 150, P90: 150},
			},
		},
		Totals: RunTotals{
			DurationMS: MetricStats{N: 2, Median: 10000, P25: 10000, P75: 20000, P90: 20000},
			CostUSD:    MetricStats{N: 3, Median: 34, P25: 0, P75: 38, P90: 38},
			TokensIn:   MetricStats{N: 3, Median: 340, P25: 60, P75: 380, P90: 380},
			TokensOut:  MetricStats{N: 3, Median: 170, P25: 0, P75: 190, P90: 190},
			Nodes:      MetricStats{N: 3, Median: 5, P25: 2, P75: 7, P90: 7},
			ErrorRate:  2.0 / 3,
		},
	}
	got := Aggregate(fixtureGroup(), Options{})
	require.Equal(t, want, got)
}

func permute3[T any](s []T) [][]T {
	a, b, c := s[0], s[1], s[2]
	return [][]T{
		{a, b, c},
		{a, c, b},
		{b, a, c},
		{b, c, a},
		{c, a, b},
		{c, b, a},
	}
}

func TestAggregatePermutationInvariant(t *testing.T) {
	opts := Options{AnnotationKeys: []string{"eval.score", "eval.latency"}}
	perms := permute3(fixtureGroup())
	first, err := json.Marshal(Aggregate(perms[0], opts))
	require.NoError(t, err)
	for i := 1; i < len(perms); i++ {
		got, err := json.Marshal(Aggregate(perms[i], opts))
		require.NoError(t, err)
		assert.Equal(t, string(first), string(got), "permutation %d differs", i)
	}
}

func TestAggregateAnnotations(t *testing.T) {
	got := Aggregate(fixtureGroup(), Options{AnnotationKeys: []string{"eval.score", "eval.note", "eval.latency", "eval.bad"}})
	require.Len(t, got.Steps, 2)
	s1 := got.Steps[0]
	require.Equal(t, "s1", s1.Key)
	require.Equal(t, 3, s1.Present)
	require.NotNil(t, s1.Annotations)
	assert.Len(t, s1.Annotations, 2)
	assert.Equal(t, MetricStats{N: 2, Median: 0.5, P25: 0.5, P75: 0.75, P90: 0.75}, s1.Annotations["eval.score"])
	assert.Equal(t, MetricStats{N: 1, Median: 0.125, P25: 0.125, P75: 0.125, P90: 0.125}, s1.Annotations["eval.latency"])
	_, hasNote := s1.Annotations["eval.note"]
	assert.False(t, hasNote)
	_, hasBad := s1.Annotations["eval.bad"]
	assert.False(t, hasBad)
	_, hasIgnored := s1.Annotations["misc.ignored"]
	assert.False(t, hasIgnored)
	s2 := got.Steps[1]
	require.Equal(t, "s2", s2.Key)
	assert.Nil(t, s2.Annotations)
	for _, p := range got.Phases {
		assert.Nil(t, p.Annotations)
	}
}

func TestAggregateAnnotationsDisabled(t *testing.T) {
	for _, keys := range [][]string{nil, {}} {
		got := Aggregate(fixtureGroup(), Options{AnnotationKeys: keys})
		for _, s := range got.Steps {
			assert.Nil(t, s.Annotations)
		}
	}
}

func TestAggregatePhaseDurationSumsMarkers(t *testing.T) {
	t0 := fixtureBase
	rg := RunGraph{
		Run: model.Run{ID: "r1", Status: model.StatusOK, StartedAt: tp(t0), EndedAt: tp(t0.Add(time.Second))},
		Nodes: []*model.Node{
			{ID: "m-a", RunID: "r1", Type: model.NodeMarker, Name: "phase", Status: model.StatusOK, PhaseKey: "p1", TStart: tp(t0), TEnd: tp(t0.Add(3000 * time.Millisecond))},
			{ID: "m-b", RunID: "r1", Type: model.NodeMarker, Name: "phase2", Status: model.StatusOK, PhaseKey: "p1", TStart: tp(t0), TEnd: tp(t0.Add(5000 * time.Millisecond))},
		},
	}
	got := Aggregate([]RunGraph{rg}, Options{})
	require.Len(t, got.Phases, 1)
	assert.Equal(t, MetricStats{N: 1, Median: 8000, P25: 8000, P75: 8000, P90: 8000}, got.Phases[0].DurationMS)
}

func TestAggregatePhaseAllOpenMarkersExcludeDuration(t *testing.T) {
	t0 := fixtureBase
	rg := RunGraph{
		Run: model.Run{ID: "r1", Status: model.StatusOK, StartedAt: tp(t0), EndedAt: tp(t0.Add(time.Second))},
		Nodes: []*model.Node{
			{ID: "m-open", RunID: "r1", Type: model.NodeMarker, Name: "phase", Status: model.StatusOK, PhaseKey: "p1", TStart: tp(t0)},
			{ID: "m-open2", RunID: "r1", Type: model.NodeMarker, Name: "phase2", Status: model.StatusOK, PhaseKey: "p1", TEnd: tp(t0.Add(time.Second))},
		},
	}
	got := Aggregate([]RunGraph{rg}, Options{})
	require.Len(t, got.Phases, 1)
	p := got.Phases[0]
	assert.Equal(t, 1, p.Present)
	assert.Equal(t, MetricStats{N: 1, Median: 2, P25: 2, P75: 2, P90: 2}, p.Occurrences)
	assert.Equal(t, MetricStats{}, p.DurationMS)
}

func TestAggregateSingleRun(t *testing.T) {
	got := Aggregate(fixtureGroup()[:1], Options{})
	require.Equal(t, 1, got.Runs)
	require.Len(t, got.Steps, 2)
	s1 := got.Steps[0]
	assert.Equal(t, "s1", s1.Key)
	assert.Equal(t, 1, s1.Present)
	assert.Equal(t, float64(1), s1.PresenceRate)
	assert.Equal(t, MetricStats{N: 1, Median: 2, P25: 2, P75: 2, P90: 2}, s1.Occurrences)
	assert.Equal(t, MetricStats{N: 1, Median: 3, P25: 3, P75: 3, P90: 3}, s1.CostUSD)
	assert.Equal(t, map[model.Status]float64{model.StatusOK: 1}, s1.StatusRates)
	require.Len(t, got.Phases, 1)
	assert.Equal(t, MetricStats{N: 1, Median: 2000, P25: 2000, P75: 2000, P90: 2000}, got.Phases[0].DurationMS)
	assert.Equal(t, MetricStats{N: 1, Median: 10000, P25: 10000, P75: 10000, P90: 10000}, got.Totals.DurationMS)
	assert.Equal(t, float64(0), got.Totals.ErrorRate)
}

func TestAggregateEmptyGroup(t *testing.T) {
	got := Aggregate(nil, Options{})
	assert.Equal(t, 0, got.Runs)
	assert.Empty(t, got.Steps)
	assert.Empty(t, got.Phases)
	assert.Equal(t, RunTotals{}, got.Totals)
	b, err := json.Marshal(got)
	require.NoError(t, err)
	assert.JSONEq(t, `{"runs":0,"steps":[],"phases":[],"totals":{"duration_ms":{"n":0,"median":0,"p25":0,"p75":0,"p90":0},"cost_usd":{"n":0,"median":0,"p25":0,"p75":0,"p90":0},"tokens_in":{"n":0,"median":0,"p25":0,"p75":0,"p90":0},"tokens_out":{"n":0,"median":0,"p25":0,"p75":0,"p90":0},"nodes":{"n":0,"median":0,"p25":0,"p75":0,"p90":0},"error_rate":0}}`, string(b))
}

func TestSeverityOrdering(t *testing.T) {
	order := []model.Status{
		model.StatusError,
		model.StatusRunning,
		model.StatusPending,
		model.StatusOK,
	}
	for i := 0; i+1 < len(order); i++ {
		assert.Greater(t, severity(order[i]), severity(order[i+1]), "%s should outrank %s", order[i], order[i+1])
	}
	assert.Equal(t, 0, severity(model.Status("")))
}

func TestWorse(t *testing.T) {
	assert.Equal(t, model.StatusError, worse(model.StatusOK, model.StatusError))
	assert.Equal(t, model.StatusError, worse(model.StatusError, model.StatusOK))
	assert.Equal(t, model.StatusOK, worse(model.StatusOK, model.StatusOK))
}

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
