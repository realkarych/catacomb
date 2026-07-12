package regress

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/aggregate"
)

func TestBinomTailGE(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		s, m int
		want float64
	}{
		{"s_le_zero", 0, 5, 1},
		{"s_negative", -1, 5, 1},
		{"s_gt_m", 6, 5, 0},
		{"unanimous_5", 5, 5, 0.03125},
		{"eight_seven", 7, 8, 9.0 / 256},
		{"eight_six", 6, 8, 37.0 / 256},
		{"seven_six", 6, 7, 0.0625},
		{"half_of_two", 1, 2, 0.75},
		{"m_zero", 0, 0, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.InDelta(t, tc.want, binomTailGE(tc.s, tc.m), 1e-12)
		})
	}
}

func TestMinUnanimousTasks(t *testing.T) {
	t.Parallel()
	cases := []struct {
		alpha float64
		want  int
	}{
		{0.9, 1},
		{0.5, 1},
		{0.49, 2},
		{0.0625, 4},
		{0.05, 5},
		{0.03125, 5},
		{0.01, 7},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, minUnanimousTasks(tc.alpha), "alpha=%g", tc.alpha)
	}
}

func TestSmallestFiringTasks(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 5, smallestFiringTasks(Thresholds{PairedAlpha: 0.05, PairedMinTasks: 5}))
	assert.Equal(t, 7, smallestFiringTasks(Thresholds{PairedAlpha: 0.01, PairedMinTasks: 5}))
	assert.Equal(t, 8, smallestFiringTasks(Thresholds{PairedAlpha: 0.05, PairedMinTasks: 8}))
}

func metricTask(task string, runs int, dur float64) aggregate.TaskStats {
	return aggregate.TaskStats{
		Task:       task,
		Runs:       runs,
		DurationMS: aggregate.MetricStats{N: runs, Median: dur},
		CostUSD:    aggregate.MetricStats{N: runs, Median: 1},
		TokensIn:   aggregate.MetricStats{N: runs, Median: 100},
		TokensOut:  aggregate.MetricStats{N: runs, Median: 100},
	}
}

func costTask(task string, cost float64) aggregate.TaskStats {
	ts := metricTask(task, 5, 1000)
	ts.CostUSD = aggregate.MetricStats{N: 5, Median: cost}
	return ts
}

func TestPairedTasksMatching(t *testing.T) {
	t.Parallel()
	b := []aggregate.TaskStats{
		metricTask("a", 5, 1000),
		metricTask("b", 2, 1000),
		metricTask("c", 5, 1000),
		metricTask("e", 5, 1000),
	}
	c := []aggregate.TaskStats{
		metricTask("a", 5, 1100),
		metricTask("b", 5, 1100),
		metricTask("d", 5, 1100),
		metricTask("e", 2, 1100),
	}
	pairs := pairedTasks(b, c, 3)
	require.Len(t, pairs, 1)
	assert.Equal(t, "a", pairs[0].base.Task)
	assert.Equal(t, "a", pairs[0].cand.Task)
}

func TestSignCounts(t *testing.T) {
	t.Parallel()
	pairs := []taskPair{
		{base: metricTask("a", 5, 1000), cand: metricTask("a", 5, 1100)},
		{base: metricTask("b", 5, 1000), cand: metricTask("b", 5, 900)},
		{base: metricTask("c", 5, 1000), cand: metricTask("c", 5, 1000)},
	}
	pos, nonzero := signCounts(pairs, func(ts aggregate.TaskStats) aggregate.MetricStats { return ts.DurationMS })
	assert.Equal(t, 1, pos)
	assert.Equal(t, 2, nonzero)

	pos, nonzero = signCounts(pairs, func(ts aggregate.TaskStats) aggregate.MetricStats { return ts.CostUSD })
	assert.Equal(t, 0, pos)
	assert.Equal(t, 0, nonzero)

	costPairs := []taskPair{
		{base: costTask("a", 1.0), cand: costTask("a", 1.2)},
		{base: costTask("b", 2.0), cand: costTask("b", 1.5)},
	}
	pos, nonzero = signCounts(costPairs, func(ts aggregate.TaskStats) aggregate.MetricStats { return ts.CostUSD })
	assert.Equal(t, 1, pos)
	assert.Equal(t, 2, nonzero)
}

func TestPairedFindingVerdicts(t *testing.T) {
	t.Parallel()
	th := DefaultThresholds()
	cases := []struct {
		name              string
		nonzero, positive int
		matched           int
		wantVerdict       Verdict
		wantDetail        string
	}{
		{"insufficient", 1, 1, 3, VerdictInsufficient, "matched 3 tasks below paired min 5"},
		{"regression_unanimous", 5, 5, 5, VerdictRegression, "+5/5 tasks, p=0.03125"},
		{"regression_eight_seven", 8, 7, 8, VerdictRegression, "+7/8 tasks, p=0.03516"},
		{"ok_eight_six", 8, 6, 8, VerdictOK, "+6/8 tasks, p=0.1445"},
		{"improvement", 5, 0, 5, VerdictImprovement, "-5/5 tasks, p=0.03125"},
		{"ok_all_zero", 0, 0, 6, VerdictOK, "+0/0 tasks, p=1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := pairedFinding("duration_ms", tc.nonzero, tc.positive, tc.matched, th)
			assert.Equal(t, "paired", f.Scope)
			assert.Equal(t, "duration_ms", f.Metric)
			assert.Equal(t, tc.wantVerdict, f.Verdict)
			assert.Equal(t, tc.wantDetail, f.Detail)
		})
	}
}

func TestPairedFindingsDormant(t *testing.T) {
	t.Parallel()
	with := aggregate.Report{Tasks: []aggregate.TaskStats{metricTask("a", 5, 1000)}}
	assert.Nil(t, pairedFindings(aggregate.Report{}, with, DefaultThresholds()))
	assert.Nil(t, pairedFindings(with, aggregate.Report{}, DefaultThresholds()))
}

func TestPairedFindingsPerMetric(t *testing.T) {
	t.Parallel()
	th := DefaultThresholds()
	b := aggregate.Report{Tasks: []aggregate.TaskStats{metricTask("a", 5, 1000)}}
	c := aggregate.Report{Tasks: []aggregate.TaskStats{metricTask("a", 5, 1100)}}
	fs := pairedFindings(b, c, th)
	require.Len(t, fs, 4)
	names := map[string]bool{}
	for _, f := range fs {
		names[f.Metric] = true
		assert.Equal(t, VerdictInsufficient, f.Verdict)
	}
	assert.Equal(t, map[string]bool{"duration_ms": true, "cost_usd": true, "tokens_in": true, "tokens_out": true}, names)
}

func TestPairedSensitivity(t *testing.T) {
	t.Parallel()
	th := DefaultThresholds()
	assert.Nil(t, pairedSensitivity(aggregate.Report{}, aggregate.Report{Tasks: []aggregate.TaskStats{metricTask("a", 5, 1)}}, th))

	var b, c []aggregate.TaskStats
	for i := 0; i < 5; i++ {
		id := string(rune('a' + i))
		b = append(b, metricTask(id, 5, 1000))
		c = append(c, metricTask(id, 5, 1100))
	}
	reach := pairedSensitivity(aggregate.Report{Tasks: b}, aggregate.Report{Tasks: c}, th)
	require.NotNil(t, reach)
	assert.True(t, reach.Reachable)
	assert.Equal(t, 5, reach.MinFullFlipRuns)

	unreach := pairedSensitivity(aggregate.Report{Tasks: b[:4]}, aggregate.Report{Tasks: c[:4]}, th)
	require.NotNil(t, unreach)
	assert.False(t, unreach.Reachable)
	assert.Equal(t, 5, unreach.MinFullFlipRuns)
}
