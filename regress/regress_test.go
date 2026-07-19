package regress

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/aggregate"
	"github.com/realkarych/catacomb/model"
)

func TestRecordStampsOmitzero(t *testing.T) {
	t.Parallel()
	raw, err := json.Marshal(Record{V: RecordVersion})
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "stamps")

	raw, err = json.Marshal(Record{V: RecordVersion, Stamps: model.Stamps{CatacombVersion: "v1", StepKeyScheme: "stepkey/v1"}})
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"stamps":{"catacomb_version":"v1","stepkey_scheme":"stepkey/v1"}`)
}

func TestRecordMarshalsTheVersionItWasStampedWith(t *testing.T) {
	t.Parallel()
	raw, err := json.Marshal(Record{V: RecordVersion, Project: "payments-api", CandidateSelector: "label:x=y"})
	require.NoError(t, err)
	var got Record
	require.NoError(t, json.Unmarshal(raw, &got))
	assert.Equal(t, RecordVersion, got.V)
	assert.Equal(t, 2, RecordVersion, "on-disk wire version: bumping this rejects records in older binaries, so change it deliberately")
	assert.Equal(t, "payments-api", got.Project)
	assert.Equal(t, "label:x=y", got.CandidateSelector)
}

func TestRecordProjectOmitEmpty(t *testing.T) {
	t.Parallel()
	raw, err := json.Marshal(Record{V: RecordVersion})
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "project")

	raw, err = json.Marshal(Record{V: RecordVersion, Project: "payments-api"})
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"project":"payments-api"`)
}

func TestRecordVersionOneWithoutProjectStillParses(t *testing.T) {
	t.Parallel()
	var rec Record
	require.NoError(t, json.Unmarshal([]byte(`{"v":1,"candidate_selector":"label:x=y"}`), &rec))
	assert.Equal(t, 1, rec.V)
	assert.Equal(t, "label:x=y", rec.CandidateSelector)
	assert.Empty(t, rec.Project)
}

func metric(n int, median, p25, p75 float64) aggregate.MetricStats {
	return aggregate.MetricStats{N: n, Median: median, P25: p25, P75: p75}
}

func presentRow(key, name string, present int) aggregate.Row {
	return aggregate.Row{
		Key:         key,
		Name:        name,
		Present:     present,
		StatusRates: map[model.Status]float64{},
		Occurrences: metric(present, 1, 1, 1),
		DurationMS:  metric(present, 1000, 900, 1100),
		CostUSD:     metric(present, 0.10, 0.09, 0.11),
		TokensIn:    metric(present, 2000, 1900, 2100),
		TokensOut:   metric(present, 800, 750, 850),
	}
}

func findFinding(fs []Finding, scope, key, metricName string) *Finding {
	for i := range fs {
		if fs[i].Scope == scope && fs[i].Key == key && fs[i].Metric == metricName {
			return &fs[i]
		}
	}
	return nil
}

func TestCompareCoverage(t *testing.T) {
	t.Parallel()
	in := Input{
		Baseline: aggregate.Report{
			Runs:   5,
			Steps:  []aggregate.Row{{Key: "s1"}, {Key: "s2"}},
			Phases: []aggregate.Row{{Key: "p1"}, {Key: "p2"}, {Key: "p3"}, {Key: "p4"}},
		},
		Candidate: aggregate.Report{
			Runs:   5,
			Steps:  []aggregate.Row{{Key: "s1"}},
			Phases: []aggregate.Row{{Key: "p1"}, {Key: "p2"}, {Key: "p3"}},
		},
	}
	r := Compare(in, DefaultThresholds())
	assert.InDelta(t, 0.5, r.Coverage.Steps, 1e-9)
	assert.InDelta(t, 0.75, r.Coverage.Phases, 1e-9)
	assert.False(t, r.StepsTrusted)
}

func TestCompareCoverageEmptyBaseline(t *testing.T) {
	t.Parallel()
	r := Compare(Input{
		Baseline:  aggregate.Report{Runs: 5},
		Candidate: aggregate.Report{Runs: 5},
	}, DefaultThresholds())
	assert.InDelta(t, 1.0, r.Coverage.Steps, 1e-9)
	assert.InDelta(t, 1.0, r.Coverage.Phases, 1e-9)
	assert.True(t, r.StepsTrusted)
}

func stepsCoverageInput(baselineSteps, matchedSteps int) Input {
	base := make([]aggregate.Row, 0, baselineSteps)
	cand := make([]aggregate.Row, 0, matchedSteps)
	for i := 0; i < baselineSteps; i++ {
		key := fmt.Sprintf("s%02d", i)
		row := presentRow(key, key, 5)
		base = append(base, row)
		if i < matchedSteps {
			slow := presentRow(key, key, 5)
			slow.DurationMS = metric(5, 1600, 1500, 1700)
			cand = append(cand, slow)
		}
	}
	return Input{
		Baseline:  aggregate.Report{Runs: 5, Steps: base},
		Candidate: aggregate.Report{Runs: 5, Steps: cand},
	}
}

func TestStepCoverageExactlyAtFloorStaysTrustedAndKeepsRegressions(t *testing.T) {
	t.Parallel()
	th := DefaultThresholds()
	require.InDelta(t, 0.7, th.CoverageFloor, 1e-12)

	atFloor := Compare(stepsCoverageInput(10, 7), th)
	require.InDelta(t, 0.7, atFloor.Coverage.Steps, 1e-12)
	assert.True(t, atFloor.StepsTrusted)
	f := findFinding(atFloor.Findings, "step", "s00", "duration_ms")
	require.NotNil(t, f)
	assert.Equal(t, VerdictRegression, f.Verdict)
	assert.Empty(t, f.Detail)

	justBelow := Compare(stepsCoverageInput(10, 6), th)
	require.InDelta(t, 0.6, justBelow.Coverage.Steps, 1e-12)
	assert.False(t, justBelow.StepsTrusted)
	below := findFinding(justBelow.Findings, "step", "s00", "duration_ms")
	require.NotNil(t, below)
	assert.Equal(t, VerdictNotable, below.Verdict)
}

func TestTotalErrorCountRoundsRateTimesRunsToTheNearestWholeRun(t *testing.T) {
	t.Parallel()
	th := DefaultThresholds()
	totals := func(runs int, errRate float64) aggregate.RunTotals {
		return aggregate.RunTotals{
			DurationMS: metric(runs, 1000, 900, 1100),
			CostUSD:    metric(runs, 0.10, 0.09, 0.11),
			TokensIn:   metric(runs, 2000, 1900, 2100),
			TokensOut:  metric(runs, 800, 750, 850),
			Nodes:      metric(runs, 12, 11, 13),
			ErrorRate:  errRate,
		}
	}
	in := Input{
		Baseline:  aggregate.Report{Runs: 3, Totals: totals(3, 0)},
		Candidate: aggregate.Report{Runs: 3, Totals: totals(3, 1)},
	}
	f := findFinding(Compare(in, th).Findings, "total", "", "error_rate")
	require.NotNil(t, f)
	assert.InDelta(t, 1.0, f.Candidate, 1e-12)
	assert.Equal(t, VerdictRegression, f.Verdict)
	assert.InDelta(t, wilsonUpperBoundAtZeroSuccesses(3, th.Z), f.BandHi, 1e-12)

	runs, errors := 22, 15
	rate := float64(errors) / float64(runs)
	require.NotEqual(t, errors, int(rate*float64(runs)),
		"fixture must exercise a rate whose product truncates below the true error count")
	lossy := Input{
		Baseline:  aggregate.Report{Runs: runs, Totals: totals(runs, 0)},
		Candidate: aggregate.Report{Runs: runs, Totals: totals(runs, rate)},
	}
	rounded := findFinding(Compare(lossy, th).Findings, "total", "", "error_rate")
	require.NotNil(t, rounded)
	assert.InDelta(t, rate, rounded.Candidate, 1e-12)
	assert.Equal(t, VerdictRegression, rounded.Verdict)

	mirrored := Input{
		Baseline:  aggregate.Report{Runs: runs, Totals: totals(runs, rate)},
		Candidate: aggregate.Report{Runs: runs, Totals: totals(runs, 0)},
	}
	improved := findFinding(Compare(mirrored, th).Findings, "total", "", "error_rate")
	require.NotNil(t, improved)
	assert.InDelta(t, rate, improved.Baseline, 1e-12)
	assert.InDelta(t, -rate, improved.Delta, 1e-12)
	assert.Equal(t, VerdictImprovement, improved.Verdict)
}

func TestRowErrorCountRoundsStatusRateTimesPresentToTheNearestWholeRun(t *testing.T) {
	t.Parallel()
	present, errors := 22, 15
	rate := float64(errors) / float64(present)
	require.NotEqual(t, errors, int(rate*float64(present)),
		"fixture must exercise a rate whose product truncates below the true error count")
	base := presentRow("p1", "one", present)
	cand := presentRow("p1", "one", present)
	cand.StatusRates = map[model.Status]float64{model.StatusError: rate}
	in := Input{
		Baseline:  aggregate.Report{Runs: present, Phases: []aggregate.Row{base}},
		Candidate: aggregate.Report{Runs: present, Phases: []aggregate.Row{cand}},
	}
	f := findFinding(Compare(in, DefaultThresholds()).Findings, "phase", "p1", "error_rate")
	require.NotNil(t, f)
	assert.InDelta(t, rate, f.Candidate, 1e-12)
	assert.InDelta(t, rate, f.Delta, 1e-12)
}

func TestCompareTotalsCostRegression(t *testing.T) {
	t.Parallel()
	totals := func(cost float64) aggregate.RunTotals {
		return aggregate.RunTotals{
			DurationMS: metric(5, 1000, 900, 1100),
			CostUSD:    metric(5, cost, cost*0.9, cost*1.1),
			TokensIn:   metric(5, 2000, 1900, 2100),
			TokensOut:  metric(5, 800, 750, 850),
			Nodes:      metric(5, 12, 11, 13),
		}
	}
	in := Input{
		Baseline:  aggregate.Report{Runs: 5, Totals: totals(0.10)},
		Candidate: aggregate.Report{Runs: 5, Totals: totals(0.20)},
	}
	r := Compare(in, DefaultThresholds())
	cost := findFinding(r.Findings, "total", "", "cost_usd")
	require.NotNil(t, cost)
	assert.Equal(t, VerdictRegression, cost.Verdict)
	assert.Equal(t, 1, r.Regressions)
	assert.Equal(t, VerdictRegression, r.OverallVerdict)

	dur := findFinding(r.Findings, "total", "", "duration_ms")
	require.NotNil(t, dur)
	assert.Equal(t, VerdictOK, dur.Verdict)
}

func TestComparePhasePresenceRegression(t *testing.T) {
	t.Parallel()
	in := Input{
		Baseline:  aggregate.Report{Runs: 10, Phases: []aggregate.Row{presentRow("p1", "one", 10)}},
		Candidate: aggregate.Report{Runs: 10, Phases: []aggregate.Row{presentRow("p1", "one", 4)}},
	}
	r := Compare(in, DefaultThresholds())
	pf := findFinding(r.Findings, "phase", "p1", "presence")
	require.NotNil(t, pf)
	assert.Equal(t, VerdictRegression, pf.Verdict)
	assert.Equal(t, "present 10/10 -> 4/10", pf.Detail)
	assert.Equal(t, VerdictRegression, r.OverallVerdict)
}

func TestComparePhasePresenceInsufficientKeepsDetail(t *testing.T) {
	t.Parallel()
	in := Input{
		Baseline:  aggregate.Report{Runs: 2, Phases: []aggregate.Row{presentRow("p1", "one", 2)}},
		Candidate: aggregate.Report{Runs: 2, Phases: []aggregate.Row{presentRow("p1", "one", 1)}},
	}
	r := Compare(in, DefaultThresholds())
	pf := findFinding(r.Findings, "phase", "p1", "presence")
	require.NotNil(t, pf)
	assert.Equal(t, VerdictInsufficient, pf.Verdict)
	assert.Contains(t, pf.Detail, "below min support")
	assert.Contains(t, pf.Detail, "present 2/2 -> 1/2")
}

func TestComparePhaseAbsentInCandidate(t *testing.T) {
	t.Parallel()
	in := Input{
		Baseline:  aggregate.Report{Runs: 5, Phases: []aggregate.Row{presentRow("p1", "one", 5)}},
		Candidate: aggregate.Report{Runs: 5},
	}
	r := Compare(in, DefaultThresholds())

	pf := findFinding(r.Findings, "phase", "p1", "presence")
	require.NotNil(t, pf)
	assert.Equal(t, VerdictRegression, pf.Verdict)
	assert.Equal(t, "present 5/5 -> 0/5", pf.Detail)

	mf := findFinding(r.Findings, "phase", "p1", "metrics")
	require.NotNil(t, mf)
	assert.Equal(t, VerdictInsufficient, mf.Verdict)
	assert.Equal(t, "absent in candidate", mf.Detail)

	assert.Nil(t, findFinding(r.Findings, "phase", "p1", "duration_ms"))
	assert.Nil(t, findFinding(r.Findings, "phase", "p1", "error_rate"))
}

func TestComparePhaseAbsentInBaseline(t *testing.T) {
	t.Parallel()
	in := Input{
		Baseline:  aggregate.Report{Runs: 5},
		Candidate: aggregate.Report{Runs: 5, Phases: []aggregate.Row{presentRow("p1", "one", 5)}},
	}
	r := Compare(in, DefaultThresholds())

	pf := findFinding(r.Findings, "phase", "p1", "presence")
	require.NotNil(t, pf)
	assert.Equal(t, VerdictImprovement, pf.Verdict)
	assert.Equal(t, "present 0/5 -> 5/5", pf.Detail)

	mf := findFinding(r.Findings, "phase", "p1", "metrics")
	require.NotNil(t, mf)
	assert.Equal(t, VerdictInsufficient, mf.Verdict)
	assert.Equal(t, "absent in baseline", mf.Detail)
}

func TestComparePhaseErrorRateNotable(t *testing.T) {
	t.Parallel()
	base := presentRow("p1", "one", 5)
	cand := presentRow("p1", "one", 5)
	cand.StatusRates = map[model.Status]float64{model.StatusError: 0.6}
	in := Input{
		Baseline:  aggregate.Report{Runs: 5, Phases: []aggregate.Row{base}},
		Candidate: aggregate.Report{Runs: 5, Phases: []aggregate.Row{cand}},
	}
	r := Compare(in, DefaultThresholds())
	ef := findFinding(r.Findings, "phase", "p1", "error_rate")
	require.NotNil(t, ef)
	assert.Equal(t, VerdictNotable, ef.Verdict)
	assert.InDelta(t, 0.0, ef.Baseline, 1e-9)
	assert.InDelta(t, 0.6, ef.Candidate, 1e-9)
}

func TestComparePhaseMetricImprovement(t *testing.T) {
	t.Parallel()
	base := presentRow("p1", "one", 5)
	cand := presentRow("p1", "one", 5)
	cand.DurationMS = metric(5, 600, 500, 700)
	in := Input{
		Baseline:  aggregate.Report{Runs: 5, Phases: []aggregate.Row{base}},
		Candidate: aggregate.Report{Runs: 5, Phases: []aggregate.Row{cand}},
	}
	r := Compare(in, DefaultThresholds())
	df := findFinding(r.Findings, "phase", "p1", "duration_ms")
	require.NotNil(t, df)
	assert.Equal(t, VerdictImprovement, df.Verdict)
}

func TestCompareStepRegressionDowngrade(t *testing.T) {
	t.Parallel()
	s1b := presentRow("s1", "one", 5)
	s1c := presentRow("s1", "one", 5)
	s1c.DurationMS = metric(5, 1600, 1500, 1700)
	in := Input{
		Baseline:  aggregate.Report{Runs: 5, Steps: []aggregate.Row{s1b, presentRow("s2", "two", 5)}},
		Candidate: aggregate.Report{Runs: 5, Steps: []aggregate.Row{s1c}},
	}
	r := Compare(in, DefaultThresholds())
	assert.False(t, r.StepsTrusted)

	s1 := findFinding(r.Findings, "step", "s1", "duration_ms")
	require.NotNil(t, s1)
	assert.Equal(t, VerdictNotable, s1.Verdict)
	assert.Equal(t, "step alignment coverage 0.50 below floor 0.70", s1.Detail)

	s2 := findFinding(r.Findings, "step", "s2", "presence")
	require.NotNil(t, s2)
	assert.Equal(t, VerdictNotable, s2.Verdict)
	assert.Equal(t, "present 5/5 -> 0/5; step alignment coverage 0.50 below floor 0.70", s2.Detail)

	assert.Equal(t, 0, r.Regressions)
}

func TestCompareStepRegressionTrusted(t *testing.T) {
	t.Parallel()
	s1b := presentRow("s1", "one", 5)
	s1c := presentRow("s1", "one", 5)
	s1c.DurationMS = metric(5, 1600, 1500, 1700)
	in := Input{
		Baseline:  aggregate.Report{Runs: 5, Steps: []aggregate.Row{s1b}},
		Candidate: aggregate.Report{Runs: 5, Steps: []aggregate.Row{s1c}},
	}
	r := Compare(in, DefaultThresholds())
	assert.True(t, r.StepsTrusted)
	s1 := findFinding(r.Findings, "step", "s1", "duration_ms")
	require.NotNil(t, s1)
	assert.Equal(t, VerdictRegression, s1.Verdict)
	assert.Empty(t, s1.Detail)
	assert.Equal(t, VerdictRegression, r.OverallVerdict)
}

func TestCompareOKFiltering(t *testing.T) {
	t.Parallel()
	in := Input{
		Baseline:  aggregate.Report{Runs: 5, Phases: []aggregate.Row{presentRow("p1", "one", 5)}},
		Candidate: aggregate.Report{Runs: 5, Phases: []aggregate.Row{presentRow("p1", "one", 5)}},
	}
	r := Compare(in, DefaultThresholds())
	assert.Nil(t, findFinding(r.Findings, "phase", "p1", "presence"))
	assert.Nil(t, findFinding(r.Findings, "phase", "p1", "duration_ms"))
	assert.NotNil(t, findFinding(r.Findings, "total", "", "error_rate"))
	for _, f := range r.Findings {
		assert.Equal(t, "total", f.Scope)
	}
}

func TestCompareOverallInsufficient(t *testing.T) {
	t.Parallel()
	in := Input{
		Baseline:  aggregate.Report{Runs: 2, Phases: []aggregate.Row{presentRow("p1", "one", 2)}},
		Candidate: aggregate.Report{Runs: 2, Phases: []aggregate.Row{presentRow("p1", "one", 2)}},
	}
	r := Compare(in, DefaultThresholds())
	assert.Positive(t, r.Insufficient)
	assert.Equal(t, 0, r.Regressions)
	assert.Equal(t, VerdictInsufficient, r.OverallVerdict)
}

func TestCompareOverallOKWithInsufficientAndNotable(t *testing.T) {
	t.Parallel()
	base := presentRow("p1", "one", 5)
	cand := presentRow("p1", "one", 5)
	cand.StatusRates = map[model.Status]float64{model.StatusError: 0.6}
	in := Input{
		Baseline: aggregate.Report{
			Runs:   5,
			Phases: []aggregate.Row{base},
			Steps:  []aggregate.Row{presentRow("s1", "one", 2)},
		},
		Candidate: aggregate.Report{
			Runs:   5,
			Phases: []aggregate.Row{cand},
			Steps:  []aggregate.Row{presentRow("s1", "one", 2)},
		},
	}
	r := Compare(in, DefaultThresholds())
	assert.Positive(t, r.Insufficient)
	assert.Equal(t, 0, r.Regressions)
	notable := findFinding(r.Findings, "phase", "p1", "error_rate")
	require.NotNil(t, notable)
	assert.Equal(t, VerdictNotable, notable.Verdict)
	assert.Equal(t, VerdictOK, r.OverallVerdict)
}

func inputWithFullPresenceFlipK3(t *testing.T) Input {
	t.Helper()
	return Input{
		Baseline:  aggregate.Report{Runs: 3, Phases: []aggregate.Row{presentRow("px", "x", 3)}},
		Candidate: aggregate.Report{Runs: 3},
	}
}

func TestFailOnNotableEscalatesOverallVerdict(t *testing.T) {
	t.Parallel()
	th := DefaultThresholds()
	th.Z = 1.96
	in := inputWithFullPresenceFlipK3(t)
	rep := Compare(in, th)
	require.Equal(t, VerdictOK, rep.OverallVerdict)
	require.Positive(t, rep.Notables)

	th.FailOnNotable = true
	rep = Compare(in, th)
	require.Equal(t, VerdictRegression, rep.OverallVerdict)
}

func annStep(score aggregate.MetricStats) aggregate.Row {
	r := presentRow("s1", "step-one", 5)
	r.Annotations = map[string]aggregate.MetricStats{"eval.score": score}
	return r
}

func TestCompareAnnotationRegressionTrusted(t *testing.T) {
	t.Parallel()
	in := Input{
		Baseline:    aggregate.Report{Runs: 5, Steps: []aggregate.Row{annStep(metric(5, 0.8, 0.75, 0.85))}},
		Candidate:   aggregate.Report{Runs: 5, Steps: []aggregate.Row{annStep(metric(5, 0.4, 0.35, 0.45))}},
		Annotations: []AnnotationSpec{{Key: "eval.score", HigherBetter: true}},
	}
	r := Compare(in, DefaultThresholds())
	require.True(t, r.StepsTrusted)
	f := findFinding(r.Findings, "step", "s1", "ann:eval.score")
	require.NotNil(t, f)
	assert.Equal(t, VerdictRegression, f.Verdict)
	assert.Empty(t, f.Detail)
	assert.Equal(t, 1, r.Regressions)
	assert.Equal(t, VerdictRegression, r.OverallVerdict)
}

func TestCompareAnnotationRegressionDowngraded(t *testing.T) {
	t.Parallel()
	in := Input{
		Baseline:    aggregate.Report{Runs: 5, Steps: []aggregate.Row{annStep(metric(5, 0.8, 0.75, 0.85)), presentRow("s2", "two", 5)}},
		Candidate:   aggregate.Report{Runs: 5, Steps: []aggregate.Row{annStep(metric(5, 0.4, 0.35, 0.45))}},
		Annotations: []AnnotationSpec{{Key: "eval.score", HigherBetter: true}},
	}
	r := Compare(in, DefaultThresholds())
	require.False(t, r.StepsTrusted)
	f := findFinding(r.Findings, "step", "s1", "ann:eval.score")
	require.NotNil(t, f)
	assert.Equal(t, VerdictNotable, f.Verdict)
	assert.Equal(t, "step alignment coverage 0.50 below floor 0.70", f.Detail)
	assert.Equal(t, 0, r.Regressions)
}

func TestCompareAnnotationImprovementTrusted(t *testing.T) {
	t.Parallel()
	in := Input{
		Baseline:    aggregate.Report{Runs: 5, Steps: []aggregate.Row{annStep(metric(5, 0.8, 0.75, 0.85))}},
		Candidate:   aggregate.Report{Runs: 5, Steps: []aggregate.Row{annStep(metric(5, 1.5, 1.4, 1.6))}},
		Annotations: []AnnotationSpec{{Key: "eval.score", HigherBetter: true}},
	}
	r := Compare(in, DefaultThresholds())
	f := findFinding(r.Findings, "step", "s1", "ann:eval.score")
	require.NotNil(t, f)
	assert.Equal(t, VerdictImprovement, f.Verdict)
}

func TestCompareAnnotationAbsentOneSide(t *testing.T) {
	t.Parallel()
	spec := []AnnotationSpec{{Key: "eval.score", HigherBetter: true}}
	withAnn := annStep(metric(5, 0.8, 0.75, 0.85))
	plain := presentRow("s1", "step-one", 5)

	cases := []struct {
		name       string
		base, cand aggregate.Row
		wantDetail string
	}{
		{"candidate_absent", withAnn, plain, "annotation absent in candidate"},
		{"baseline_absent", plain, withAnn, "annotation absent in baseline"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := Input{
				Baseline:    aggregate.Report{Runs: 5, Steps: []aggregate.Row{tc.base}},
				Candidate:   aggregate.Report{Runs: 5, Steps: []aggregate.Row{tc.cand}},
				Annotations: spec,
			}
			r := Compare(in, DefaultThresholds())
			f := findFinding(r.Findings, "step", "s1", "ann:eval.score")
			require.NotNil(t, f)
			assert.Equal(t, VerdictInsufficient, f.Verdict)
			assert.Equal(t, tc.wantDetail, f.Detail)
		})
	}
}

func TestCompareAnnotationAbsentBoth(t *testing.T) {
	t.Parallel()
	in := Input{
		Baseline:    aggregate.Report{Runs: 5, Steps: []aggregate.Row{presentRow("s1", "step-one", 5)}},
		Candidate:   aggregate.Report{Runs: 5, Steps: []aggregate.Row{presentRow("s1", "step-one", 5)}},
		Annotations: []AnnotationSpec{{Key: "eval.score", HigherBetter: true}},
	}
	r := Compare(in, DefaultThresholds())
	assert.Nil(t, findFinding(r.Findings, "step", "s1", "ann:eval.score"))
}

func TestCompareAnnotationNoSpecsNoFindings(t *testing.T) {
	t.Parallel()
	in := Input{
		Baseline:  aggregate.Report{Runs: 5, Steps: []aggregate.Row{annStep(metric(5, 0.8, 0.75, 0.85))}},
		Candidate: aggregate.Report{Runs: 5, Steps: []aggregate.Row{annStep(metric(5, 0.4, 0.35, 0.45))}},
	}
	r := Compare(in, DefaultThresholds())
	for _, f := range r.Findings {
		assert.NotEqual(t, "ann:eval.score", f.Metric)
	}
}

type findingSlot struct {
	scope  string
	key    string
	metric string
}

func findingSlots(fs []Finding) []findingSlot {
	out := make([]findingSlot, 0, len(fs))
	for _, f := range fs {
		out = append(out, findingSlot{scope: f.Scope, key: f.Key, metric: f.Metric})
	}
	return out
}

func multiScopeInput(phases, steps []aggregate.Row, tasks []aggregate.TaskStats) Input {
	slowStep := presentRow("s1", "step-one", 5)
	slowStep.DurationMS = metric(5, 1600, 1500, 1700)
	slowPhase := presentRow("pa", "alpha", 5)
	slowPhase.DurationMS = metric(5, 1600, 1500, 1700)
	candTasks := make([]aggregate.TaskStats, 0, len(tasks))
	for _, ts := range tasks {
		candTasks = append(candTasks, metricTask(ts.Task, 5, 1100))
	}
	return Input{
		Baseline:  aggregate.Report{Runs: 5, Phases: phases, Steps: steps, Tasks: tasks},
		Candidate: aggregate.Report{Runs: 5, Phases: []aggregate.Row{slowPhase}, Steps: []aggregate.Row{slowStep}, Tasks: candTasks},
	}
}

func multiScopeBaselineParts() ([]aggregate.Row, []aggregate.Row, []aggregate.TaskStats) {
	phases := []aggregate.Row{presentRow("pa", "alpha", 5), presentRow("pb", "beta", 5)}
	steps := []aggregate.Row{presentRow("s1", "step-one", 5)}
	tasks := []aggregate.TaskStats{
		metricTask("t1", 5, 1000), metricTask("t2", 5, 1000), metricTask("t3", 5, 1000),
		metricTask("t4", 5, 1000), metricTask("t5", 5, 1000),
	}
	return phases, steps, tasks
}

func TestCompareFindingsFollowTotalPairedPhaseStepThenKeyThenMetric(t *testing.T) {
	t.Parallel()
	phases, steps, tasks := multiScopeBaselineParts()
	got := Compare(multiScopeInput(phases, steps, tasks), DefaultThresholds())
	require.Equal(t, []findingSlot{
		{"total", "", "cost_usd"},
		{"total", "", "duration_ms"},
		{"total", "", "error_rate"},
		{"total", "", "nodes"},
		{"total", "", "tokens_in"},
		{"total", "", "tokens_out"},
		{"paired", "", "duration_ms"},
		{"phase", "pa", "duration_ms"},
		{"phase", "pb", "metrics"},
		{"phase", "pb", "presence"},
		{"step", "s1", "duration_ms"},
	}, findingSlots(got.Findings))
}

func TestCompareFindingOrderIsIndependentOfInputRowAndTaskOrder(t *testing.T) {
	t.Parallel()
	phases, steps, tasks := multiScopeBaselineParts()
	want := Compare(multiScopeInput(phases, steps, tasks), DefaultThresholds()).Findings

	reversedPhases := []aggregate.Row{phases[1], phases[0]}
	reversedTasks := make([]aggregate.TaskStats, 0, len(tasks))
	for i := len(tasks) - 1; i >= 0; i-- {
		reversedTasks = append(reversedTasks, tasks[i])
	}
	got := Compare(multiScopeInput(reversedPhases, steps, reversedTasks), DefaultThresholds()).Findings

	require.Equal(t, want, got)
}
