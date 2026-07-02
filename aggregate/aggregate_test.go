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
			{ID: "r1-s1b", RunID: "r1", Type: model.NodeToolCall, Name: "search2", Status: model.StatusOK, StepKey: "s1", CostUSD: f64(2), TokensIn: i64(20), TokensOut: i64(10), DurationMS: i64(200)},
			{ID: "r1-s1a", RunID: "r1", Type: model.NodeToolCall, Name: "search", Status: model.StatusOK, StepKey: "s1", CostUSD: f64(1), TokensIn: i64(10), TokensOut: i64(5), DurationMS: i64(100)},
			{ID: "r1-sup", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusSuperseded, StepKey: "s1", CostUSD: f64(100), TokensIn: i64(1000), TokensOut: i64(1000), DurationMS: i64(9999)},
			{ID: "r1-s2", RunID: "r1", Type: model.NodeToolCall, Name: "build", Status: model.StatusBlocked, StepKey: "s2", CostUSD: f64(5), TokensIn: i64(50), TokensOut: i64(25), DurationMS: i64(500)},
			{ID: "r1-m1", RunID: "r1", Type: model.NodeMarker, Name: "phase-one", Status: model.StatusOK, PhaseKey: "p1", TStart: tp(t0), TEnd: tp(t0.Add(2000 * time.Millisecond))},
			{ID: "r1-mem1", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusOK, CostUSD: f64(10), TokensIn: i64(100), TokensOut: i64(50), DurationMS: i64(9999)},
			{ID: "r1-mem2", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusOK, CostUSD: f64(20), TokensIn: i64(200), TokensOut: i64(100)},
			{ID: "r1-mem-sup", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusSuperseded, CostUSD: f64(999), TokensIn: i64(999), TokensOut: i64(999)},
		},
		Edges: []*model.Edge{
			{Type: model.EdgeMarkerSpan, Src: "r1-m1", Dst: "r1-mem1"},
			{Type: model.EdgeMarkerSpan, Src: "r1-m1", Dst: "r1-mem2"},
			{Type: model.EdgeMarkerSpan, Src: "r1-m1", Dst: "r1-mem-sup"},
			{Type: model.EdgeMarkerSpan, Src: "r1-m1", Dst: "r1-missing"},
		},
	}
	r2 := RunGraph{
		Run: model.Run{ID: "r2", Status: model.StatusOK, StartedAt: tp(t0), EndedAt: tp(t0.Add(20 * time.Second))},
		Nodes: []*model.Node{
			{ID: "r2-sess", RunID: "r2", Type: model.NodeSession, Status: model.StatusOK},
			{ID: "r2-s1", RunID: "r2", Type: model.NodeToolCall, Name: "search", Status: model.StatusError, StepKey: "s1", CostUSD: f64(4), TokensIn: i64(40), TokensOut: i64(20), DurationMS: i64(400)},
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
			{ID: "r3-s1", RunID: "r3", Type: model.NodeToolCall, Name: "search", Status: model.StatusOK, StepKey: "s1", TokensIn: i64(60)},
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
				Occurrences: MetricStats{N: 3, Median: 1, P90: 2},
				DurationMS:  MetricStats{N: 3, Median: 300, P90: 400},
				CostUSD:     MetricStats{N: 3, Median: 3, P90: 4},
				TokensIn:    MetricStats{N: 3, Median: 40, P90: 60},
				TokensOut:   MetricStats{N: 3, Median: 15, P90: 20},
			},
			{
				Key: "s2", Name: "build", Present: 1, PresenceRate: 1.0 / 3,
				StatusRates: map[model.Status]float64{model.StatusBlocked: 1},
				Occurrences: MetricStats{N: 1, Median: 1, P90: 1},
				DurationMS:  MetricStats{N: 1, Median: 500, P90: 500},
				CostUSD:     MetricStats{N: 1, Median: 5, P90: 5},
				TokensIn:    MetricStats{N: 1, Median: 50, P90: 50},
				TokensOut:   MetricStats{N: 1, Median: 25, P90: 25},
			},
		},
		Phases: []Row{
			{
				Key: "p1", Name: "phase-one", Present: 2, PresenceRate: 2.0 / 3,
				StatusRates: map[model.Status]float64{model.StatusOK: 0.5, model.StatusError: 0.5},
				Occurrences: MetricStats{N: 2, Median: 1, P90: 2},
				DurationMS:  MetricStats{N: 2, Median: 2000, P90: 4000},
				CostUSD:     MetricStats{N: 2, Median: 30, P90: 30},
				TokensIn:    MetricStats{N: 2, Median: 300, P90: 300},
				TokensOut:   MetricStats{N: 2, Median: 150, P90: 150},
			},
		},
		Totals: RunTotals{
			DurationMS: MetricStats{N: 3, Median: 10000, P90: 20000},
			CostUSD:    MetricStats{N: 3, Median: 34, P90: 38},
			TokensIn:   MetricStats{N: 3, Median: 340, P90: 380},
			TokensOut:  MetricStats{N: 3, Median: 170, P90: 190},
			Nodes:      MetricStats{N: 3, Median: 5, P90: 7},
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
	perms := permute3(fixtureGroup())
	first, err := json.Marshal(Aggregate(perms[0], Options{}))
	require.NoError(t, err)
	for i := 1; i < len(perms); i++ {
		got, err := json.Marshal(Aggregate(perms[i], Options{}))
		require.NoError(t, err)
		assert.Equal(t, string(first), string(got), "permutation %d differs", i)
	}
}

func TestAggregateSingleRun(t *testing.T) {
	got := Aggregate(fixtureGroup()[:1], Options{})
	require.Equal(t, 1, got.Runs)
	require.Len(t, got.Steps, 2)
	s1 := got.Steps[0]
	assert.Equal(t, "s1", s1.Key)
	assert.Equal(t, 1, s1.Present)
	assert.Equal(t, float64(1), s1.PresenceRate)
	assert.Equal(t, MetricStats{N: 1, Median: 2, P90: 2}, s1.Occurrences)
	assert.Equal(t, MetricStats{N: 1, Median: 3, P90: 3}, s1.CostUSD)
	assert.Equal(t, map[model.Status]float64{model.StatusOK: 1}, s1.StatusRates)
	require.Len(t, got.Phases, 1)
	assert.Equal(t, MetricStats{N: 1, Median: 2000, P90: 2000}, got.Phases[0].DurationMS)
	assert.Equal(t, MetricStats{N: 1, Median: 10000, P90: 10000}, got.Totals.DurationMS)
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
	assert.JSONEq(t, `{"runs":0,"steps":[],"phases":[],"totals":{"duration_ms":{"n":0,"median":0,"p90":0},"cost_usd":{"n":0,"median":0,"p90":0},"tokens_in":{"n":0,"median":0,"p90":0},"tokens_out":{"n":0,"median":0,"p90":0},"nodes":{"n":0,"median":0,"p90":0},"error_rate":0}}`, string(b))
}

func TestSeverityOrdering(t *testing.T) {
	order := []model.Status{
		model.StatusError,
		model.StatusBlocked,
		model.StatusCancelled,
		model.StatusUnknown,
		model.StatusRunning,
		model.StatusPending,
		model.StatusOK,
	}
	for i := 0; i+1 < len(order); i++ {
		assert.Greater(t, severity(order[i]), severity(order[i+1]), "%s should outrank %s", order[i], order[i+1])
	}
	assert.Equal(t, 0, severity(model.StatusSuperseded))
}

func TestWorse(t *testing.T) {
	assert.Equal(t, model.StatusError, worse(model.StatusOK, model.StatusError))
	assert.Equal(t, model.StatusError, worse(model.StatusError, model.StatusOK))
	assert.Equal(t, model.StatusOK, worse(model.StatusOK, model.StatusOK))
}
