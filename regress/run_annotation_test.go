package regress

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/aggregate"
)

func onesStats(n int) aggregate.MetricStats {
	return aggregate.MetricStats{N: n, Median: 1, P25: 1, P75: 1, P90: 1}
}

func zerosStats(n int) aggregate.MetricStats {
	return aggregate.MetricStats{N: n, Median: 0, P25: 0, P75: 0, P90: 0}
}

func reportPlainTotals(k int) aggregate.Report {
	return aggregate.Report{
		Runs: k,
		Totals: aggregate.RunTotals{
			DurationMS: metric(k, 1000, 900, 1100),
			CostUSD:    metric(k, 0.10, 0.09, 0.11),
			TokensIn:   metric(k, 2000, 1900, 2100),
			TokensOut:  metric(k, 800, 750, 850),
			Nodes:      metric(k, 12, 11, 13),
		},
	}
}

func reportWithAnnKey(k int, key string, ann aggregate.AnnotationTotals) aggregate.Report {
	r := reportPlainTotals(k)
	r.Totals.Annotations = map[string]aggregate.AnnotationTotals{key: ann}
	return r
}

func reportWithAnn(k int, ann aggregate.AnnotationTotals) aggregate.Report {
	return reportWithAnnKey(k, VerifierOutcomeKey, ann)
}

func findingByMetric(t *testing.T, fs []Finding, metricName string) Finding {
	t.Helper()
	f := findFinding(fs, "total", "", metricName)
	require.NotNil(t, f)
	return *f
}

func TestCompareRunAnnotationPassFullFlip(t *testing.T) {
	t.Parallel()
	b := reportWithAnn(5, aggregate.AnnotationTotals{N: 5, Ones: 5, Binary: true, Stats: onesStats(5)})
	c := reportWithAnn(5, aggregate.AnnotationTotals{N: 5, Ones: 0, Binary: true, Stats: zerosStats(5)})
	rep := Compare(Input{Baseline: b, Candidate: c}, DefaultThresholds())
	f := findingByMetric(t, rep.Findings, "ann:verifier.pass")
	assert.Equal(t, VerdictRegression, f.Verdict)
	assert.Equal(t, VerdictRegression, rep.OverallVerdict)
	assert.Equal(t, "ones 5/5 -> 0/5", f.Detail)
}

func TestCompareRunAnnotationPassAvsA(t *testing.T) {
	t.Parallel()
	b := reportWithAnn(5, aggregate.AnnotationTotals{N: 5, Ones: 5, Binary: true, Stats: onesStats(5)})
	rep := Compare(Input{Baseline: b, Candidate: b}, DefaultThresholds())
	f := findingByMetric(t, rep.Findings, "ann:verifier.pass")
	assert.Equal(t, VerdictOK, f.Verdict)
	assert.Equal(t, VerdictOK, rep.OverallVerdict)
	assert.Equal(t, f.Baseline, f.Candidate)
	assert.InDelta(t, 1.0, f.Baseline, 1e-9)
}

func TestCompareRunAnnotationHigherBetterPassSpaceNumbers(t *testing.T) {
	t.Parallel()
	b := reportWithAnn(5, aggregate.AnnotationTotals{N: 5, Ones: 5, Binary: true, Stats: onesStats(5)})
	c := reportWithAnn(5, aggregate.AnnotationTotals{N: 5, Ones: 2, Binary: true, Stats: metric(5, 0.4, 0, 1)})
	rep := Compare(Input{Baseline: b, Candidate: c}, DefaultThresholds())
	f := findingByMetric(t, rep.Findings, "ann:verifier.pass")
	assert.Equal(t, VerdictNotable, f.Verdict)
	assert.InDelta(t, 1.0, f.Baseline, 1e-9)
	assert.InDelta(t, 0.4, f.Candidate, 1e-9)
	assert.InDelta(t, -0.6, f.Delta, 1e-9)
	assert.InDelta(t, 1.0, f.BandHi, 1e-9)
	assert.InDelta(t, 1-0.3511570491920283, f.BandLo, 1e-9)
	assert.Equal(t, "ones 5/5 -> 2/5", f.Detail)
}

func TestCompareRunAnnotationContinuousUsesBand(t *testing.T) {
	t.Parallel()
	b := reportWithAnn(5, aggregate.AnnotationTotals{N: 5, Binary: false, Stats: metric(5, 0.8, 0.75, 0.85)})
	c := reportWithAnn(5, aggregate.AnnotationTotals{N: 5, Binary: false, Stats: aggregate.MetricStats{N: 5, Median: 0.4}})
	rep := Compare(Input{Baseline: b, Candidate: c}, DefaultThresholds())
	f := findingByMetric(t, rep.Findings, "ann:verifier.pass")
	assert.Equal(t, VerdictRegression, f.Verdict)
	assert.InDelta(t, 0.8, f.Baseline, 1e-9)
	assert.InDelta(t, 0.6, f.BandLo, 1e-9)
	assert.InDelta(t, 1.0, f.BandHi, 1e-9)
	assert.NotContains(t, f.Detail, "ones")
}

func TestCompareRunAnnotationAbsentOneSideInsufficient(t *testing.T) {
	t.Parallel()
	b := reportWithAnn(5, aggregate.AnnotationTotals{N: 5, Ones: 5, Binary: true, Stats: onesStats(5)})
	c := reportPlainTotals(5)
	rep := Compare(Input{Baseline: b, Candidate: c}, DefaultThresholds())
	f := findingByMetric(t, rep.Findings, "ann:verifier.pass")
	assert.Equal(t, VerdictInsufficient, f.Verdict)
	assert.Equal(t, "annotation absent in candidate", f.Detail)
}

func TestCompareRunAnnotationLowerBetterSpec(t *testing.T) {
	t.Parallel()
	b := reportWithAnnKey(5, "verifier.row_diff", aggregate.AnnotationTotals{N: 5, Ones: 0, Binary: true, Stats: zerosStats(5)})
	c := reportWithAnnKey(5, "verifier.row_diff", aggregate.AnnotationTotals{N: 5, Ones: 5, Binary: true, Stats: onesStats(5)})
	rep := Compare(Input{
		Baseline:    b,
		Candidate:   c,
		Annotations: []AnnotationSpec{{Key: "verifier.row_diff", HigherBetter: false}},
	}, DefaultThresholds())
	f := findingByMetric(t, rep.Findings, "ann:verifier.row_diff")
	assert.Equal(t, VerdictRegression, f.Verdict)
	assert.Equal(t, "ones 0/5 -> 5/5", f.Detail)
	assert.Equal(t, VerdictRegression, rep.OverallVerdict)
	assert.InDelta(t, 0.0, f.Baseline, 1e-9)
	assert.InDelta(t, 1.0, f.Candidate, 1e-9)
	assert.InDelta(t, 1.0, f.Delta, 1e-9)
	assert.InDelta(t, 0.0, f.BandLo, 1e-9)
	assert.InDelta(t, 0.3511570491920283, f.BandHi, 1e-9)
}

func TestCompareRunAnnotationImprovementOnRisingPass(t *testing.T) {
	t.Parallel()
	b := reportWithAnn(5, aggregate.AnnotationTotals{N: 5, Ones: 0, Binary: true, Stats: zerosStats(5)})
	c := reportWithAnn(5, aggregate.AnnotationTotals{N: 5, Ones: 5, Binary: true, Stats: onesStats(5)})
	rep := Compare(Input{Baseline: b, Candidate: c}, DefaultThresholds())
	f := findingByMetric(t, rep.Findings, "ann:verifier.pass")
	assert.Equal(t, VerdictImprovement, f.Verdict)
	assert.Equal(t, "ones 0/5 -> 5/5", f.Detail)
}

func TestCompareRunAnnotationExplicitSpecOverridesDefault(t *testing.T) {
	t.Parallel()
	b := reportWithAnn(5, aggregate.AnnotationTotals{N: 5, Ones: 5, Binary: true, Stats: onesStats(5)})
	c := reportWithAnn(5, aggregate.AnnotationTotals{N: 5, Ones: 0, Binary: true, Stats: zerosStats(5)})
	rep := Compare(Input{
		Baseline:    b,
		Candidate:   c,
		Annotations: []AnnotationSpec{{Key: VerifierOutcomeKey, HigherBetter: false}},
	}, DefaultThresholds())
	f := findingByMetric(t, rep.Findings, "ann:verifier.pass")
	assert.Equal(t, VerdictImprovement, f.Verdict)
}

func TestSensitivityAnnotationAxisDisclosed(t *testing.T) {
	t.Parallel()
	b := reportWithAnn(2, aggregate.AnnotationTotals{N: 2, Ones: 2, Binary: true, Stats: onesStats(2)})
	c := reportWithAnn(2, aggregate.AnnotationTotals{N: 2, Ones: 0, Binary: true, Stats: zerosStats(2)})
	rep := Compare(Input{Baseline: b, Candidate: c}, DefaultThresholds())
	require.NotNil(t, rep.Sensitivity)
	require.NotNil(t, rep.Sensitivity.Annotation)
	assert.False(t, rep.Sensitivity.Annotation.Reachable)
	assert.Equal(t, 3, rep.Sensitivity.Annotation.MinFullFlipRuns)
}

func TestSensitivityContinuousAnnotationNoAxis(t *testing.T) {
	t.Parallel()
	b := reportWithAnn(2, aggregate.AnnotationTotals{N: 2, Binary: false, Stats: metric(2, 0.8, 0.75, 0.85)})
	c := reportWithAnn(2, aggregate.AnnotationTotals{N: 2, Binary: false, Stats: aggregate.MetricStats{N: 2, Median: 0.4}})
	rep := Compare(Input{Baseline: b, Candidate: c}, DefaultThresholds())
	require.NotNil(t, rep.Sensitivity)
	assert.Nil(t, rep.Sensitivity.Annotation)
}
