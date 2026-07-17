package regress

import (
	"math"
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
		{"ten_six", 6, 10, 0.376953125},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.InDelta(t, tc.want, binomTailGE(tc.s, tc.m), 1e-12)
		})
	}
}

func TestBinomTailGELargeM(t *testing.T) {
	t.Parallel()
	assert.InDelta(t, 0.5, binomTailGE(1000, 2000), 0.01)
	assert.InDelta(t, 0.5089195055729272, binomTailGE(1000, 2000), 1e-9)
	assert.InDelta(t, 0.5, binomTailGE(538, 1075), 1e-9)
	assert.InDelta(t, 0.028443966820490395, binomTailGE(60, 100), 1e-9)
	assert.InEpsilon(t, math.Ldexp(1, -20), binomTailGE(20, 20), 1e-12)
	assert.Equal(t, 1.0, binomTailGE(0, 2000))
	assert.Equal(t, 0.0, binomTailGE(2001, 2000))
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
		{math.Ldexp(1, -999), 999},
		{math.Ldexp(1, -1000), 1000},
		{math.SmallestNonzeroFloat64, minUnanimousSearchCap},
		{0, minUnanimousSearchCap},
		{-1, minUnanimousSearchCap},
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
	durSel := func(ts aggregate.TaskStats) aggregate.MetricStats { return ts.DurationMS }
	pos, nonzero := signCounts(pairs, durSel, 3)
	assert.Equal(t, 1, pos)
	assert.Equal(t, 2, nonzero)

	pos, nonzero = signCounts(pairs, func(ts aggregate.TaskStats) aggregate.MetricStats { return ts.CostUSD }, 3)
	assert.Equal(t, 0, pos)
	assert.Equal(t, 0, nonzero)

	costPairs := []taskPair{
		{base: costTask("a", 1.0), cand: costTask("a", 1.2)},
		{base: costTask("b", 2.0), cand: costTask("b", 1.5)},
	}
	pos, nonzero = signCounts(costPairs, func(ts aggregate.TaskStats) aggregate.MetricStats { return ts.CostUSD }, 3)
	assert.Equal(t, 1, pos)
	assert.Equal(t, 2, nonzero)
}

func TestSignCountsSupportGuard(t *testing.T) {
	t.Parallel()
	durSel := func(ts aggregate.TaskStats) aggregate.MetricStats { return ts.DurationMS }
	noDur := metricTask("b", 5, 1100)
	noDur.DurationMS = aggregate.MetricStats{}
	lowDur := metricTask("c", 5, 900)
	lowDur.DurationMS = aggregate.MetricStats{N: 2, Median: 900}
	pairs := []taskPair{
		{base: metricTask("a", 5, 1000), cand: metricTask("a", 5, 1100)},
		{base: metricTask("b", 5, 1000), cand: noDur},
		{base: metricTask("c", 5, 1000), cand: lowDur},
	}
	pos, nonzero := signCounts(pairs, durSel, 3)
	assert.Equal(t, 1, pos)
	assert.Equal(t, 1, nonzero)

	pos, nonzero = signCounts(pairs[1:2], durSel, 0)
	assert.Equal(t, 0, pos)
	assert.Equal(t, 0, nonzero)

	pos, nonzero = signCounts(pairs[2:], durSel, 0)
	assert.Equal(t, 0, pos)
	assert.Equal(t, 1, nonzero)
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
		{"insufficient_singular", 1, 1, 1, VerdictInsufficient, "matched 1 task below paired min 5"},
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

func findMetricFinding(t *testing.T, fs []Finding, metric string) Finding {
	t.Helper()
	for _, f := range fs {
		if f.Metric == metric {
			return f
		}
	}
	t.Fatalf("no finding for metric %s", metric)
	return Finding{}
}

func TestPairedFindingsUnsupportedMetric(t *testing.T) {
	t.Parallel()
	th := DefaultThresholds()
	cases := []struct {
		name string
		zero func(*aggregate.TaskStats)
	}{
		{"candidate_no_duration_samples", func(ts *aggregate.TaskStats) { ts.DurationMS = aggregate.MetricStats{} }},
		{"baseline_no_duration_samples", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var b, c []aggregate.TaskStats
			for i := 0; i < 5; i++ {
				id := string(rune('a' + i))
				bt := metricTask(id, 5, 1000)
				ct := metricTask(id, 5, 1000)
				if tc.zero != nil {
					tc.zero(&ct)
				} else {
					bt.DurationMS = aggregate.MetricStats{}
				}
				b = append(b, bt)
				c = append(c, ct)
			}
			fs := pairedFindings(aggregate.Report{Tasks: b}, aggregate.Report{Tasks: c}, th)
			f := findMetricFinding(t, fs, "duration_ms")
			assert.NotEqual(t, VerdictRegression, f.Verdict)
			assert.NotEqual(t, VerdictImprovement, f.Verdict)
			assert.Equal(t, VerdictOK, f.Verdict)
			assert.Equal(t, "+0/0 tasks, p=1", f.Detail)
			cost := findMetricFinding(t, fs, "cost_usd")
			assert.Equal(t, VerdictOK, cost.Verdict)
		})
	}
}

func wilcoxonThresholds() Thresholds {
	th := DefaultThresholds()
	th.PairedTest = PairedTestWilcoxon
	return th
}

func TestWilcoxonPValues(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		deltas     []float64
		wantWPlus  float64
		wantWTotal float64
		wantPReg   float64
		wantPImp   float64
	}{
		{"empty", nil, 0, 0, 1, 1},
		{"single_positive", []float64{3}, 1, 1, 0.5, 1},
		{"n6_rank1_discordant", []float64{-1, 2, 3, 4, 5, 6}, 20, 21, 0.03125, 0.984375},
		{"n6_rank2_discordant", []float64{1, -2, 3, 4, 5, 6}, 19, 21, 0.046875, 0.96875},
		{"n5_unanimous_positive", []float64{1, 2, 3, 4, 5}, 15, 15, 0.03125, 1},
		{"n5_unanimous_negative", []float64{-1, -2, -3, -4, -5}, 0, 15, 1, 0.03125},
		{"n6_midrank_tie_at_smallest", []float64{-5, 5, 10, 20, 30, 40}, 19.5, 21, 0.046875, 0.984375},
		{"n4_midrank_tie", []float64{5, 5, -3, 8}, 9, 10, 0.125, 0.9375},
		{"n3_all_tied_magnitudes", []float64{3, 3, -3}, 4, 6, 0.5, 0.875},
		{"n6_mixed", []float64{1, -2, 3, -4, 5, -6}, 9, 21, 0.65625, 0.421875},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			wPlus, wTotal, pReg, pImp := wilcoxonPValues(tc.deltas)
			assert.InDelta(t, tc.wantWPlus, wPlus, 1e-12)
			assert.InDelta(t, tc.wantWTotal, wTotal, 1e-12)
			assert.InDelta(t, tc.wantPReg, pReg, 1e-12)
			assert.InDelta(t, tc.wantPImp, pImp, 1e-12)
		})
	}
}

func TestWilcoxonFindingVerdicts(t *testing.T) {
	t.Parallel()
	oneTask := DefaultThresholds()
	oneTask.PairedMinTasks = 1
	cases := []struct {
		name        string
		deltas      []float64
		matched     int
		th          Thresholds
		wantVerdict Verdict
		wantDetail  string
	}{
		{"insufficient", []float64{1, 2, 3}, 3, DefaultThresholds(), VerdictInsufficient, "matched 3 tasks below paired min 5"},
		{"insufficient_singular", []float64{1}, 1, DefaultThresholds(), VerdictInsufficient, "matched 1 task below paired min 5"},
		{"regression_lone_small_discordant", []float64{-1, 2, 3, 4, 5, 6}, 6, DefaultThresholds(), VerdictRegression, "W+ 20/21 over 6 tasks, p=0.03125"},
		{"regression_rank2_discordant", []float64{1, -2, 3, 4, 5, 6}, 6, DefaultThresholds(), VerdictRegression, "W+ 19/21 over 6 tasks, p=0.04688"},
		{"regression_midrank_tie", []float64{-5, 5, 10, 20, 30, 40}, 6, DefaultThresholds(), VerdictRegression, "W+ 19.5/21 over 6 tasks, p=0.04688"},
		{"regression_unanimous", []float64{1, 2, 3, 4, 5}, 5, DefaultThresholds(), VerdictRegression, "W+ 15/15 over 5 tasks, p=0.03125"},
		{"improvement_unanimous", []float64{-1, -2, -3, -4, -5}, 5, DefaultThresholds(), VerdictImprovement, "W+ 0/15 over 5 tasks, p=0.03125"},
		{"ok_mixed", []float64{1, -2, 3, -4, 5, -6}, 6, DefaultThresholds(), VerdictOK, "W+ 9/21 over 6 tasks, p=0.6562"},
		{"ok_zeros_discarded", []float64{1, 2, 3, 4}, 5, DefaultThresholds(), VerdictOK, "W+ 10/10 over 4 tasks, p=0.0625"},
		{"ok_no_nonzero_deltas", nil, 6, DefaultThresholds(), VerdictOK, "W+ 0/0 over 0 tasks, p=1"},
		{"ok_single_task", []float64{3}, 1, oneTask, VerdictOK, "W+ 1/1 over 1 task, p=0.5"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := wilcoxonFinding("duration_ms", tc.deltas, tc.matched, tc.th)
			assert.Equal(t, "paired", f.Scope)
			assert.Equal(t, "duration_ms", f.Metric)
			assert.Equal(t, tc.wantVerdict, f.Verdict)
			assert.Equal(t, tc.wantDetail, f.Detail)
		})
	}
}

func sixTaskReports() (aggregate.Report, aggregate.Report) {
	var b, c []aggregate.TaskStats
	for i, delta := range []float64{-1, 2, 3, 4, 5, 6} {
		id := string(rune('a' + i))
		b = append(b, metricTask(id, 5, 1000))
		c = append(c, metricTask(id, 5, 1000+delta))
	}
	return aggregate.Report{Tasks: b}, aggregate.Report{Tasks: c}
}

func TestWilcoxonOutpowersSignAtSixTasks(t *testing.T) {
	t.Parallel()
	b, c := sixTaskReports()

	signDur := findMetricFinding(t, pairedFindings(b, c, DefaultThresholds()), "duration_ms")
	assert.Equal(t, VerdictOK, signDur.Verdict)
	assert.Equal(t, "+5/6 tasks, p=0.1094", signDur.Detail)

	fs := pairedFindings(b, c, wilcoxonThresholds())
	require.Len(t, fs, 4)
	dur := findMetricFinding(t, fs, "duration_ms")
	assert.Equal(t, VerdictRegression, dur.Verdict)
	assert.Equal(t, "W+ 20/21 over 6 tasks, p=0.03125", dur.Detail)
	cost := findMetricFinding(t, fs, "cost_usd")
	assert.Equal(t, VerdictOK, cost.Verdict)
	assert.Equal(t, "W+ 0/0 over 0 tasks, p=1", cost.Detail)
}

func TestPairedFindingsTestSelection(t *testing.T) {
	t.Parallel()
	var b, c []aggregate.TaskStats
	for i := 0; i < 5; i++ {
		id := string(rune('a' + i))
		b = append(b, metricTask(id, 5, 1000))
		c = append(c, metricTask(id, 5, 1100))
	}
	br := aggregate.Report{Tasks: b}
	cr := aggregate.Report{Tasks: c}

	for _, name := range []string{"", PairedTestSign} {
		th := DefaultThresholds()
		th.PairedTest = name
		f := findMetricFinding(t, pairedFindings(br, cr, th), "duration_ms")
		assert.Equal(t, VerdictRegression, f.Verdict, "test %q", name)
		assert.Equal(t, "+5/5 tasks, p=0.03125", f.Detail, "test %q", name)
	}

	f := findMetricFinding(t, pairedFindings(br, cr, wilcoxonThresholds()), "duration_ms")
	assert.Equal(t, VerdictRegression, f.Verdict)
	assert.Equal(t, "W+ 15/15 over 5 tasks, p=0.03125", f.Detail)
}

func TestPairedFindingsWilcoxonDormant(t *testing.T) {
	t.Parallel()
	with := aggregate.Report{Tasks: []aggregate.TaskStats{metricTask("a", 5, 1000)}}
	assert.Nil(t, pairedFindings(aggregate.Report{}, with, wilcoxonThresholds()))
	assert.Nil(t, pairedFindings(with, aggregate.Report{}, wilcoxonThresholds()))
}

func TestPairedFindingsWilcoxonDeterministic(t *testing.T) {
	t.Parallel()
	b, c := sixTaskReports()
	first := pairedFindings(b, c, wilcoxonThresholds())
	second := pairedFindings(b, c, wilcoxonThresholds())
	assert.Equal(t, first, second)
}

func TestPairedSensitivityUnchangedUnderWilcoxon(t *testing.T) {
	t.Parallel()
	b, c := sixTaskReports()
	sign := pairedSensitivity(b, c, DefaultThresholds())
	wilcoxon := pairedSensitivity(b, c, wilcoxonThresholds())
	require.NotNil(t, sign)
	require.NotNil(t, wilcoxon)
	assert.Equal(t, sign, wilcoxon)
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
	assert.Equal(t, 5, reach.MinTasks)

	unreach := pairedSensitivity(aggregate.Report{Tasks: b[:4]}, aggregate.Report{Tasks: c[:4]}, th)
	require.NotNil(t, unreach)
	assert.False(t, unreach.Reachable)
	assert.Equal(t, 5, unreach.MinTasks)
}
