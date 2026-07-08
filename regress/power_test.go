package regress

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/aggregate"
)

var powerKs = []int{3, 5, 10, 20, 30}

func powerTotals(k int, tokensOutMedian, tokensOutP25, tokensOutP75, errRate float64) aggregate.RunTotals {
	return aggregate.RunTotals{
		DurationMS: metric(k, 1000, 900, 1100),
		CostUSD:    metric(k, 0.10, 0.09, 0.11),
		TokensIn:   metric(k, 2000, 1900, 2100),
		TokensOut:  aggregate.MetricStats{N: k, Median: tokensOutMedian, P25: tokensOutP25, P75: tokensOutP75},
		Nodes:      metric(k, 12, 11, 13),
		ErrorRate:  errRate,
	}
}

func presencePair(k, present int) Input {
	in := Input{
		Baseline:  aggregate.Report{Runs: k, Phases: []aggregate.Row{presentRow("p1", "one", k)}},
		Candidate: aggregate.Report{Runs: k},
	}
	if present > 0 {
		in.Candidate.Phases = []aggregate.Row{presentRow("p1", "one", present)}
	}
	return in
}

func continuousPair(k int, m, iqrFrac, e float64) Input {
	iqr := iqrFrac * m
	p25 := m - iqr/2
	p75 := m + iqr/2
	return Input{
		Baseline:  aggregate.Report{Runs: k, Totals: powerTotals(k, m, p25, p75, 0)},
		Candidate: aggregate.Report{Runs: k, Totals: powerTotals(k, m*(1+e), p25, p75, 0)},
	}
}

func errorPair(k, q int) Input {
	return Input{
		Baseline:  aggregate.Report{Runs: k, Totals: powerTotals(k, 2000, 2000, 2000, 0)},
		Candidate: aggregate.Report{Runs: k, Totals: powerTotals(k, 2000, 2000, 2000, float64(q)/float64(k))},
	}
}

func gates(rep Report) bool { return rep.OverallVerdict == VerdictRegression }

func TestGatePowerPresence(t *testing.T) {
	t.Parallel()
	th := DefaultThresholds()
	require.Equal(t, 3, minFullFlipRuns(th, th.PresenceDelta))

	cases := []struct {
		k                  int
		wantLargestGatingP int
		wantMinDropCount   int
		wantMinDropPct     float64
	}{
		{3, 0, 3, 100.0},
		{5, 1, 4, 80.0},
		{10, 5, 5, 50.0},
		{20, 15, 5, 25.0},
		{30, 23, 7, 23.333},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("k=%d", tc.k), func(t *testing.T) {
			t.Parallel()
			largest := -1
			for present := 0; present <= tc.k; present++ {
				if gates(Compare(presencePair(tc.k, present), th)) && present > largest {
					largest = present
				}
			}
			require.Equal(t, tc.wantLargestGatingP, largest)
			require.Equal(t, tc.wantMinDropCount, tc.k-largest)
			require.InDelta(t, tc.wantMinDropPct, 100*float64(tc.k-largest)/float64(tc.k), 0.01)

			for present := 0; present <= tc.k; present++ {
				rep := Compare(presencePair(tc.k, present), th)
				require.Equalf(t, present <= largest, gates(rep), "present=%d gating", present)
			}

			full := Compare(presencePair(tc.k, 0), th)
			require.True(t, gates(full))
			pf := findFinding(full.Findings, "phase", "p1", "presence")
			require.NotNil(t, pf)
			require.Equal(t, VerdictRegression, pf.Verdict)
			require.Nil(t, full.Sensitivity)
		})
	}
}

func TestGatePowerContinuousKInvariant(t *testing.T) {
	t.Parallel()
	th := DefaultThresholds()
	const m = 2000.0

	cases := []struct {
		name       string
		iqrFrac    float64
		wantMinE   float64
		wantEdgeE  float64
		wantBandLo float64
		wantBandHi float64
	}{
		{"rel_band_iqr_0", 0, 0.30, 0.25, 1500, 2500},
		{"rel_band_iqr_0.05", 0.05, 0.30, 0.25, 1500, 2500},
		{"rel_band_iqr_0.15", 0.15, 0.30, 0.25, 1500, 2500},
		{"iqr_dominated_0.30", 0.30, 0.50, 0.45, 1100, 2900},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			minEByK := map[int]float64{}
			for _, k := range powerKs {
				var minE float64
				for i := 1; i <= 20; i++ {
					e := float64(5*i) / 100
					if gates(Compare(continuousPair(k, m, tc.iqrFrac, e), th)) {
						minE = e
						break
					}
				}
				minEByK[k] = minE
				require.InDelta(t, tc.wantMinE, minE, 1e-9)

				band := findFinding(Compare(continuousPair(k, m, tc.iqrFrac, tc.wantMinE), th).Findings, "total", "", "tokens_out")
				require.NotNil(t, band)
				require.InDelta(t, tc.wantBandLo, band.BandLo, 1e-9)
				require.InDelta(t, tc.wantBandHi, band.BandHi, 1e-9)
			}
			require.InDelta(t, minEByK[3], minEByK[30], 1e-9)
			for _, k := range powerKs {
				require.InDelta(t, minEByK[3], minEByK[k], 1e-9)
			}

			edge := Compare(continuousPair(3, m, tc.iqrFrac, tc.wantEdgeE), th)
			require.False(t, gates(edge))
			ef := findFinding(edge.Findings, "total", "", "tokens_out")
			require.NotNil(t, ef)
			require.Equal(t, VerdictOK, ef.Verdict)
		})
	}
}

func TestGatePowerErrorRate(t *testing.T) {
	t.Parallel()
	th := DefaultThresholds()
	require.Equal(t, 3, minFullFlipRuns(th, th.ErrorRateDelta))

	cases := []struct {
		k           int
		wantQMin    int
		wantMinRate float64
	}{
		{3, 3, 100.0},
		{5, 4, 80.0},
		{10, 5, 50.0},
		{20, 5, 25.0},
		{30, 5, 16.667},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("k=%d", tc.k), func(t *testing.T) {
			t.Parallel()
			qMin := 0
			for q := 1; q <= tc.k; q++ {
				if gates(Compare(errorPair(tc.k, q), th)) {
					qMin = q
					break
				}
			}
			require.Equal(t, tc.wantQMin, qMin)
			require.InDelta(t, tc.wantMinRate, 100*float64(qMin)/float64(tc.k), 0.01)

			for q := 1; q <= tc.k; q++ {
				rep := Compare(errorPair(tc.k, q), th)
				require.Equalf(t, q >= qMin, gates(rep), "q=%d gating", q)
			}

			gate := Compare(errorPair(tc.k, qMin), th)
			ef := findFinding(gate.Findings, "total", "", "error_rate")
			require.NotNil(t, ef)
			require.Equal(t, VerdictRegression, ef.Verdict)
		})
	}
}
