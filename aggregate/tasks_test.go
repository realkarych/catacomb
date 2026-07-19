package aggregate

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
)

func taskRun(id, task string, durMS int64, cost float64, tokensIn, tokensOut int64, pass *float64) RunGraph {
	t0 := fixtureBase
	run := model.Run{ID: id, Status: model.StatusOK, Labels: map[string]string{"task": task}}
	if durMS >= 0 {
		run.StartedAt = tp(t0)
		run.EndedAt = tp(t0.Add(time.Duration(durMS) * time.Millisecond))
	}
	rg := RunGraph{
		Run: run,
		Nodes: []*model.Node{
			{ID: id + "-sess", RunID: id, Type: model.NodeSession, Status: model.StatusOK},
			{ID: id + "-n1", RunID: id, Type: model.NodeToolCall, Status: model.StatusOK, CostUSD: f64(cost), TokensIn: i64(tokensIn), TokensOut: i64(tokensOut)},
		},
	}
	if pass != nil {
		rg.Annotations = map[string]float64{"verifier.pass": *pass}
	}
	return rg
}

func taskGroup() []RunGraph {
	one, zero := f64(1), f64(0)
	beta1 := RunGraph{
		Run: model.Run{ID: "b1", Status: model.StatusOK, Labels: map[string]string{"task": "beta"}, StartedAt: tp(fixtureBase), EndedAt: tp(fixtureBase.Add(4000 * time.Millisecond))},
		Nodes: []*model.Node{
			{ID: "b1-a", RunID: "b1", Type: model.NodeToolCall, Status: model.StatusOK, CostUSD: f64(4), TokensIn: i64(40), TokensOut: i64(20)},
			{ID: "b1-b", RunID: "b1", Type: model.NodeToolCall, Status: model.StatusOK, CostUSD: f64(6), TokensIn: i64(60), TokensOut: i64(30)},
		},
		Annotations: map[string]float64{"verifier.pass": 1},
	}
	unlabeled := RunGraph{
		Run:         model.Run{ID: "u1", Status: model.StatusOK},
		Nodes:       []*model.Node{{ID: "u1-n", RunID: "u1", Type: model.NodeToolCall, Status: model.StatusOK, CostUSD: f64(999), TokensIn: i64(999), TokensOut: i64(999)}},
		Annotations: map[string]float64{"verifier.pass": 1},
	}
	emptyTask := RunGraph{
		Run:   model.Run{ID: "u2", Status: model.StatusOK, Labels: map[string]string{"task": ""}},
		Nodes: []*model.Node{{ID: "u2-n", RunID: "u2", Type: model.NodeToolCall, Status: model.StatusOK, CostUSD: f64(888)}},
	}
	return []RunGraph{
		emptyTask,
		unlabeled,
		taskRun("g1", "gamma", 500, 7, 70, 35, nil),
		taskRun("b2", "beta", 6000, 20, 200, 100, one),
		beta1,
		taskRun("a3", "alpha", -1, 2, 20, 10, nil),
		taskRun("a2", "alpha", 3000, 3, 30, 15, zero),
		taskRun("a1", "alpha", 1000, 1, 10, 5, one),
	}
}

func TestTaskStats(t *testing.T) {
	want := []TaskStats{
		{
			Task: "alpha", Runs: 3,
			Outcome:    &TaskOutcome{N: 2, Ones: 1},
			DurationMS: MetricStats{N: 2, Median: 1000, P25: 1000, P75: 3000, P90: 3000},
			CostUSD:    MetricStats{N: 3, Median: 2, P25: 1, P75: 3, P90: 3},
			TokensIn:   MetricStats{N: 3, Median: 20, P25: 10, P75: 30, P90: 30},
			TokensOut:  MetricStats{N: 3, Median: 10, P25: 5, P75: 15, P90: 15},
		},
		{
			Task: "beta", Runs: 2,
			Outcome:    &TaskOutcome{N: 2, Ones: 2},
			DurationMS: MetricStats{N: 2, Median: 4000, P25: 4000, P75: 6000, P90: 6000},
			CostUSD:    MetricStats{N: 2, Median: 10, P25: 10, P75: 20, P90: 20},
			TokensIn:   MetricStats{N: 2, Median: 100, P25: 100, P75: 200, P90: 200},
			TokensOut:  MetricStats{N: 2, Median: 50, P25: 50, P75: 100, P90: 100},
		},
		{
			Task: "gamma", Runs: 1,
			Outcome:    nil,
			DurationMS: MetricStats{N: 1, Median: 500, P25: 500, P75: 500, P90: 500},
			CostUSD:    MetricStats{N: 1, Median: 7, P25: 7, P75: 7, P90: 7},
			TokensIn:   MetricStats{N: 1, Median: 70, P25: 70, P75: 70, P90: 70},
			TokensOut:  MetricStats{N: 1, Median: 35, P25: 35, P75: 35, P90: 35},
		},
	}
	got := Aggregate(taskGroup(), Options{}).Tasks
	require.Equal(t, want, got)
}

func TestTaskStatsCountsOnlyMeasuredSamplesPerMetric(t *testing.T) {
	tasks := Aggregate(taskGroup(), Options{}).Tasks
	require.Len(t, tasks, 3)
	alpha := tasks[0]
	require.Equal(t, "alpha", alpha.Task)
	assert.Equal(t, 3, alpha.Runs)
	assert.Equal(t, MetricStats{N: 2, Median: 1000, P25: 1000, P75: 3000, P90: 3000}, alpha.DurationMS)
	assert.Equal(t, MetricStats{N: 3, Median: 2, P25: 1, P75: 3, P90: 3}, alpha.CostUSD)
	require.NotNil(t, alpha.Outcome)
	assert.Equal(t, &TaskOutcome{N: 2, Ones: 1}, alpha.Outcome)
	assert.Less(t, alpha.DurationMS.N, alpha.CostUSD.N)
	assert.Less(t, alpha.Outcome.N, alpha.Runs)
}

func TestTaskOutcomeCountsOnlyExactlyOneAsAPassAndEveryPresentValueAsATrial(t *testing.T) {
	values := []float64{1, 0, 0.5, 2, -1, 1}
	group := make([]RunGraph, 0, len(values)+1)
	for i, v := range values {
		score := v
		group = append(group, taskRun(fmt.Sprintf("r%d", i), "alpha", 1000, 1, 10, 5, &score))
	}
	group = append(group, taskRun("r-noann", "alpha", 1000, 1, 10, 5, nil))

	tasks := Aggregate(group, Options{}).Tasks
	require.Len(t, tasks, 1)
	require.NotNil(t, tasks[0].Outcome)
	assert.Equal(t, &TaskOutcome{N: 6, Ones: 2}, tasks[0].Outcome)
	assert.Equal(t, 7, tasks[0].Runs)
}

func TestTaskStatsJSONShape(t *testing.T) {
	b, err := json.Marshal(Aggregate(taskGroup(), Options{}).Tasks)
	require.NoError(t, err)
	assert.JSONEq(t, `[
		{"task":"alpha","runs":3,"outcome":{"n":2,"ones":1},
		 "duration_ms":{"n":2,"median":1000,"p25":1000,"p75":3000,"p90":3000},
		 "cost_usd":{"n":3,"median":2,"p25":1,"p75":3,"p90":3},
		 "tokens_in":{"n":3,"median":20,"p25":10,"p75":30,"p90":30},
		 "tokens_out":{"n":3,"median":10,"p25":5,"p75":15,"p90":15}},
		{"task":"beta","runs":2,"outcome":{"n":2,"ones":2},
		 "duration_ms":{"n":2,"median":4000,"p25":4000,"p75":6000,"p90":6000},
		 "cost_usd":{"n":2,"median":10,"p25":10,"p75":20,"p90":20},
		 "tokens_in":{"n":2,"median":100,"p25":100,"p75":200,"p90":200},
		 "tokens_out":{"n":2,"median":50,"p25":50,"p75":100,"p90":100}},
		{"task":"gamma","runs":1,
		 "duration_ms":{"n":1,"median":500,"p25":500,"p75":500,"p90":500},
		 "cost_usd":{"n":1,"median":7,"p25":7,"p75":7,"p90":7},
		 "tokens_in":{"n":1,"median":70,"p25":70,"p75":70,"p90":70},
		 "tokens_out":{"n":1,"median":35,"p25":35,"p75":35,"p90":35}}
	]`, string(b))
}

func TestTaskStatsDormantWhenNoLabels(t *testing.T) {
	rep := Aggregate(fixtureGroup(), Options{})
	assert.Nil(t, rep.Tasks)
}

func TestTaskStatsAllExcludedIsNil(t *testing.T) {
	group := []RunGraph{
		{Run: model.Run{ID: "x1", Status: model.StatusOK}},
		{Run: model.Run{ID: "x2", Status: model.StatusOK, Labels: map[string]string{"basket": "b"}}},
	}
	assert.Nil(t, Aggregate(group, Options{}).Tasks)
}
