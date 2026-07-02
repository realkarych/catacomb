package regress

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/aggregate"
)

func TestDefaultThresholds(t *testing.T) {
	t.Parallel()
	th := DefaultThresholds()
	require.Equal(t, Thresholds{
		PresenceDelta:  0.2,
		ErrorRateDelta: 0.1,
		MetricRelDelta: 0.25,
		IQRFactor:      1.5,
		MinSupport:     3,
		CoverageFloor:  0.7,
	}, th)
}

func TestCompareRate(t *testing.T) {
	t.Parallel()
	th := DefaultThresholds()
	cases := []struct {
		name        string
		bSucc, bN   int
		cSucc, cN   int
		delta       float64
		wantVerdict Verdict
	}{
		{"regression_disjoint", 0, 5, 5, 5, th.ErrorRateDelta, VerdictRegression},
		{"notable_overlap", 0, 3, 3, 3, th.ErrorRateDelta, VerdictNotable},
		{"improvement_disjoint", 5, 5, 0, 5, th.ErrorRateDelta, VerdictImprovement},
		{"ok_equal", 2, 5, 2, 5, th.ErrorRateDelta, VerdictOK},
		{"ok_delta_not_exceeded", 0, 3, 3, 3, 1.0, VerdictOK},
		{"insufficient_baseline", 2, 2, 3, 5, th.ErrorRateDelta, VerdictInsufficient},
		{"insufficient_candidate", 3, 5, 1, 2, th.ErrorRateDelta, VerdictInsufficient},
		{"insufficient_both", 1, 2, 1, 2, th.ErrorRateDelta, VerdictInsufficient},
		{"insufficient_zero_n", 0, 0, 3, 5, th.ErrorRateDelta, VerdictInsufficient},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := compareRate("step", "s1", "search", "error_rate", tc.bSucc, tc.bN, tc.cSucc, tc.cN, tc.delta, th)
			assert.Equal(t, tc.wantVerdict, f.Verdict)
			assert.Equal(t, "step", f.Scope)
			assert.Equal(t, "s1", f.Key)
			assert.Equal(t, "search", f.Name)
			assert.Equal(t, "error_rate", f.Metric)
			assert.InDelta(t, f.Candidate-f.Baseline, f.Delta, 1e-9)
		})
	}
}

func TestCompareRateFields(t *testing.T) {
	t.Parallel()
	th := DefaultThresholds()

	reg := compareRate("phase", "p1", "phase-one", "presence", 0, 5, 5, 5, th.ErrorRateDelta, th)
	assert.InDelta(t, 0.0, reg.Baseline, 1e-9)
	assert.InDelta(t, 1.0, reg.Candidate, 1e-9)
	assert.InDelta(t, 0.0, reg.BandLo, 1e-3)
	assert.InDelta(t, 0.4345, reg.BandHi, 1e-3)
	assert.Empty(t, reg.Detail)

	ins := compareRate("step", "s2", "build", "error_rate", 0, 2, 3, 5, th.ErrorRateDelta, th)
	assert.Equal(t, "baseline n=2 below min support 3", ins.Detail)
	assert.InDelta(t, 0.0, ins.BandLo, 1e-9)
	assert.InDelta(t, 0.0, ins.BandHi, 1e-9)

	insC := compareRate("phase", "p2", "deploy", "presence", 3, 5, 1, 2, th.ErrorRateDelta, th)
	assert.Equal(t, "candidate n=2 below min support 3", insC.Detail)

	insBoth := compareRate("step", "s3", "test", "error_rate", 1, 2, 1, 2, th.ErrorRateDelta, th)
	assert.Equal(t, "baseline n=2 and candidate n=2 below min support 3", insBoth.Detail)
}

func TestCompareMetric(t *testing.T) {
	t.Parallel()
	th := DefaultThresholds()
	base := aggregate.MetricStats{N: 5, Median: 1000, P25: 900, P75: 1100, P90: 1200}
	cases := []struct {
		name        string
		baseline    aggregate.MetricStats
		candidate   aggregate.MetricStats
		wantVerdict Verdict
		wantBandLo  float64
		wantBandHi  float64
	}{
		{"regression", base, aggregate.MetricStats{N: 5, Median: 1400}, VerdictRegression, 700, 1300},
		{"improvement", base, aggregate.MetricStats{N: 5, Median: 600}, VerdictImprovement, 700, 1300},
		{"ok", base, aggregate.MetricStats{N: 5, Median: 1200}, VerdictOK, 700, 1300},
		{
			"zero_iqr_rel_band",
			aggregate.MetricStats{N: 5, Median: 1000, P25: 1000, P75: 1000},
			aggregate.MetricStats{N: 5, Median: 1400},
			VerdictRegression, 750, 1250,
		},
		{
			"neg_median_regression",
			aggregate.MetricStats{N: 5, Median: -100, P25: -110, P75: -90},
			aggregate.MetricStats{N: 5, Median: -60},
			VerdictRegression, -130, -70,
		},
		{
			"neg_median_ok",
			aggregate.MetricStats{N: 5, Median: -100, P25: -110, P75: -90},
			aggregate.MetricStats{N: 5, Median: -100},
			VerdictOK, -130, -70,
		},
		{
			"neg_median_abs_rel_band_dominates",
			aggregate.MetricStats{N: 5, Median: -100, P25: -102, P75: -98},
			aggregate.MetricStats{N: 5, Median: -80},
			VerdictOK, -125, -75,
		},
		{
			"insufficient_baseline",
			aggregate.MetricStats{N: 2, Median: 1000, P25: 900, P75: 1100},
			aggregate.MetricStats{N: 5, Median: 1400},
			VerdictInsufficient, 0, 0,
		},
		{
			"insufficient_candidate",
			base,
			aggregate.MetricStats{N: 1, Median: 1400},
			VerdictInsufficient, 0, 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := compareMetric("step", "s1", "build", "duration_ms", tc.baseline, tc.candidate, th)
			assert.Equal(t, tc.wantVerdict, f.Verdict)
			assert.InDelta(t, tc.wantBandLo, f.BandLo, 1e-9)
			assert.InDelta(t, tc.wantBandHi, f.BandHi, 1e-9)
			assert.InDelta(t, tc.candidate.Median-tc.baseline.Median, f.Delta, 1e-9)
			assert.Equal(t, "duration_ms", f.Metric)
		})
	}
}

func TestCompareMetricInsufficientDetail(t *testing.T) {
	t.Parallel()
	th := DefaultThresholds()
	f := compareMetric("phase", "p1", "phase-one", "cost_usd",
		aggregate.MetricStats{N: 2}, aggregate.MetricStats{N: 5, Median: 100}, th)
	assert.Equal(t, "baseline n=2 below min support 3", f.Detail)
}
