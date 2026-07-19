package regress

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/aggregate"
)

func tokensOutCells(ids []string, values []float64) []aggregate.Cell {
	cells := make([]aggregate.Cell, len(ids))
	for i, id := range ids {
		cells[i] = aggregate.Cell{RunID: id, TokensOut: values[i]}
	}
	return cells
}

func TestGroupFlagsCalibration(t *testing.T) {
	t.Parallel()
	th := DefaultThresholds()
	cases := []struct {
		name   string
		ids    []string
		values []float64
		want   []CellFlag
	}{
		{
			"rel_floor_exact_at_band_no_flag",
			[]string{"r1", "r2", "r3", "r4", "r5"},
			[]float64{100, 100, 100, 100, 150},
			nil,
		},
		{
			"rel_floor_epsilon_past_band_flags",
			[]string{"r1", "r2", "r3", "r4", "r5"},
			[]float64{100, 100, 100, 100, 151},
			[]CellFlag{{RunID: "r5", Metric: "tokens_out", Value: 151, Median: 100, Band: 50}},
		},
		{
			"iqr_band_exact_at_band_no_flag",
			[]string{"r1", "r2", "r3", "r4", "r5"},
			[]float64{10, 20, 30, 40, 90},
			nil,
		},
		{
			"iqr_band_epsilon_past_band_flags",
			[]string{"r1", "r2", "r3", "r4", "r5"},
			[]float64{10, 20, 30, 40, 91},
			[]CellFlag{{RunID: "r5", Metric: "tokens_out", Value: 91, Median: 30, Band: 60}},
		},
		{
			"two_cells_never_flag",
			[]string{"r1", "r2"},
			[]float64{1, 1000},
			nil,
		},
		{
			"one_cell_never_flags",
			[]string{"r1"},
			[]float64{1000},
			nil,
		},
		{
			"empty_group",
			nil,
			nil,
			nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := groupFlags(tokensOutCells(tc.ids, tc.values), th)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestGroupFlagsNeverFlagsGroupsSmallerThanThreeCellsHoweverExtreme(t *testing.T) {
	t.Parallel()
	th := DefaultThresholds()
	valueSets := [][]float64{
		{},
		{0},
		{1e9},
		{-1e9},
		{0, 1e9},
		{1, 1000},
		{-500, 500},
		{100, 100},
		{0, 0},
	}
	for _, values := range valueSets {
		ids := make([]string, len(values))
		for i := range values {
			ids[i] = fmt.Sprintf("r%d", i)
		}
		require.Nil(t, groupFlags(tokensOutCells(ids, values), th), "values %v", values)
	}
}

func TestGroupFlagsSkipsTwoCellGroupsEvenWhenALoweredIQRFactorWouldOtherwiseFlagThem(t *testing.T) {
	t.Parallel()
	th := DefaultThresholds()
	th.AuditIQRFactor = 0.5
	th.AuditRelDelta = 0
	cells := tokensOutCells([]string{"r0", "r1"}, []float64{0, 1e9})
	assert.Nil(t, groupFlags(cells, th))
	assert.NotNil(t, groupFlags(tokensOutCells([]string{"r0", "r1", "r2"}, []float64{0, 0, 1e9}), th))
}

func TestGroupFlagsMinimumOfThreeCellsHoldsWhenLoweredIQRFactorWouldOtherwiseFlagTwoCells(t *testing.T) {
	t.Parallel()
	th := DefaultThresholds()
	th.AuditIQRFactor = 0.5
	twoCells := groupFlags(tokensOutCells([]string{"r1", "r2"}, []float64{100, 1000}), th)
	assert.Nil(t, twoCells)
	threeCells := groupFlags(tokensOutCells([]string{"r1", "r2", "r3"}, []float64{100, 100, 1000}), th)
	require.Equal(t, []CellFlag{{RunID: "r3", Metric: "tokens_out", Value: 1000, Median: 100, Band: 450}}, threeCells)
}

func TestGroupFlagsIQRZeroNeedsRelFloor(t *testing.T) {
	t.Parallel()
	th := DefaultThresholds()
	got := groupFlags(tokensOutCells([]string{"r1", "r2", "r3", "r4"}, []float64{100, 100, 100, 100}), th)
	assert.Nil(t, got)
	got = groupFlags(tokensOutCells([]string{"r1", "r2", "r3", "r4"}, []float64{100, 100, 100, 149}), th)
	assert.Nil(t, got)
}

func TestGroupFlagsMultiMetricFixedOrder(t *testing.T) {
	t.Parallel()
	th := DefaultThresholds()
	base := aggregate.Cell{DurationMS: 100, CostUSD: 1, TokensIn: 50, TokensOut: 200, Turns: 5}
	cells := []aggregate.Cell{}
	for _, id := range []string{"r1", "r2", "r3"} {
		c := base
		c.RunID = id
		cells = append(cells, c)
	}
	cells = append(cells, aggregate.Cell{RunID: "r4", DurationMS: 1000, CostUSD: 1, TokensIn: 50, TokensOut: 2000, Turns: 50})
	got := groupFlags(cells, th)
	require.Equal(t, []CellFlag{
		{RunID: "r4", Metric: "duration_ms", Value: 1000, Median: 100, Band: 50},
		{RunID: "r4", Metric: "tokens_out", Value: 2000, Median: 200, Band: 100},
		{RunID: "r4", Metric: "turns", Value: 50, Median: 5, Band: 2.5},
	}, got)
}

func TestGroupFlagsSortedByRunIDAcrossMetrics(t *testing.T) {
	t.Parallel()
	th := DefaultThresholds()
	cells := []aggregate.Cell{
		{RunID: "z", DurationMS: 1000, Turns: 5},
		{RunID: "b", DurationMS: 100, Turns: 5},
		{RunID: "c", DurationMS: 100, Turns: 5},
		{RunID: "d", DurationMS: 100, Turns: 5},
		{RunID: "a", DurationMS: 100, Turns: 50},
	}
	got := groupFlags(cells, th)
	require.Equal(t, []CellFlag{
		{RunID: "a", Metric: "turns", Value: 50, Median: 5, Band: 2.5},
		{RunID: "z", Metric: "duration_ms", Value: 1000, Median: 100, Band: 50},
	}, got)
}

func TestGroupFlagsTaskLabel(t *testing.T) {
	t.Parallel()
	th := DefaultThresholds()
	cells := tokensOutCells([]string{"r1", "r2", "r3", "r4"}, []float64{100, 100, 100, 1000})
	cells[3].Labels = map[string]string{"task": "sql"}
	got := groupFlags(cells, th)
	require.Len(t, got, 1)
	assert.Equal(t, CellFlag{RunID: "r4", Task: "sql", Metric: "tokens_out", Value: 1000, Median: 100, Band: 50}, got[0])
}

func TestComputeAuditBothGroupsIndependent(t *testing.T) {
	t.Parallel()
	th := DefaultThresholds()
	baseline := tokensOutCells([]string{"b1", "b2", "b3", "b4"}, []float64{100, 100, 100, 1000})
	candidate := tokensOutCells([]string{"c1", "c2", "c3", "c4"}, []float64{100, 100, 100, 300})
	got := computeAudit(baseline, candidate, th)
	require.NotNil(t, got)
	assert.Equal(t, []CellFlag{{RunID: "b4", Metric: "tokens_out", Value: 1000, Median: 100, Band: 50}}, got.Baseline)
	assert.Equal(t, []CellFlag{{RunID: "c4", Metric: "tokens_out", Value: 300, Median: 100, Band: 50}}, got.Candidate)
}

func TestComputeAuditSingleGroupFlagged(t *testing.T) {
	t.Parallel()
	th := DefaultThresholds()
	clean := tokensOutCells([]string{"c1", "c2", "c3"}, []float64{100, 100, 100})
	flagged := tokensOutCells([]string{"b1", "b2", "b3", "b4"}, []float64{100, 100, 100, 1000})
	got := computeAudit(flagged, clean, th)
	require.NotNil(t, got)
	assert.Len(t, got.Baseline, 1)
	assert.Empty(t, got.Candidate)
}

func TestComputeAuditNil(t *testing.T) {
	t.Parallel()
	th := DefaultThresholds()
	clean := tokensOutCells([]string{"r1", "r2", "r3"}, []float64{100, 100, 100})
	assert.Nil(t, computeAudit(nil, nil, th))
	assert.Nil(t, computeAudit(clean, clean, th))
}

func auditInput(withCells bool) Input {
	in := Input{
		Baseline: aggregate.Report{
			Runs:   4,
			Totals: aggregate.RunTotals{DurationMS: aggregate.MetricStats{N: 4, Median: 1000, P25: 900, P75: 1100}},
		},
		Candidate: aggregate.Report{
			Runs:   4,
			Totals: aggregate.RunTotals{DurationMS: aggregate.MetricStats{N: 4, Median: 1400, P25: 1300, P75: 1500}},
		},
	}
	if withCells {
		in.BaselineCells = tokensOutCells([]string{"b1", "b2", "b3", "b4"}, []float64{100, 100, 100, 100})
		in.CandidateCells = tokensOutCells([]string{"c1", "c2", "c3", "c4"}, []float64{100, 100, 100, 1000})
	}
	return in
}

func TestCompareAuditWired(t *testing.T) {
	t.Parallel()
	rep := Compare(auditInput(true), DefaultThresholds())
	require.NotNil(t, rep.Audit)
	assert.Empty(t, rep.Audit.Baseline)
	assert.Equal(t, []CellFlag{{RunID: "c4", Metric: "tokens_out", Value: 1000, Median: 100, Band: 50}}, rep.Audit.Candidate)
}

func TestCompareAuditNonGating(t *testing.T) {
	t.Parallel()
	th := DefaultThresholds()
	with := Compare(auditInput(true), th)
	without := Compare(auditInput(false), th)
	require.NotNil(t, with.Audit)
	require.Nil(t, without.Audit)
	assert.Equal(t, VerdictRegression, with.OverallVerdict)
	assert.Equal(t, without.OverallVerdict, with.OverallVerdict)
	assert.Equal(t, without.Findings, with.Findings)
	with.Audit = nil
	assert.Equal(t, without, with)
}

func TestCompareNilCellsJSONDormant(t *testing.T) {
	t.Parallel()
	rep := Compare(auditInput(false), DefaultThresholds())
	var buf bytes.Buffer
	require.NoError(t, RenderJSON(rep, &buf))
	assert.NotContains(t, buf.String(), `"audit"`)
}

func TestRenderHumanAuditLines(t *testing.T) {
	t.Parallel()
	rep := Report{
		OverallVerdict: VerdictOK,
		Reliability: &Reliability{
			Baseline:  GroupReliability{Tasks: []TaskReliability{{Task: "a", N: 1, Ones: 1, PassK: []float64{1}}}, KMax: 1, Mean: []float64{1}},
			Candidate: GroupReliability{Tasks: []TaskReliability{{Task: "a", N: 1, Ones: 1, PassK: []float64{1}}}, KMax: 1, Mean: []float64{1}},
		},
		Audit: &Audit{
			Baseline:  []CellFlag{{RunID: "b1", Metric: "cost_usd", Value: 3.5, Median: 1, Band: 0.5}},
			Candidate: []CellFlag{{RunID: "r07", Task: "sql", Metric: "tokens_out", Value: 1932, Median: 243, Band: 121.5}},
		},
	}
	var buf bytes.Buffer
	RenderHuman(rep, &buf)
	out := buf.String()
	assert.Contains(t, out, "audit: baseline run b1 cost_usd 3.5 vs group median 1 (band 0.5)\n")
	assert.Contains(t, out, "audit: candidate run r07 (task sql) tokens_out 1932 vs group median 243 (band 121.5)\n")
	assert.Less(t, strings.Index(out, "reliability"), strings.Index(out, "audit:"))
}

func TestRenderHumanAuditAbsent(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	RenderHuman(Report{OverallVerdict: VerdictOK}, &buf)
	assert.NotContains(t, buf.String(), "audit")
}

func TestAuditJSONRoundTrip(t *testing.T) {
	t.Parallel()
	rep := Report{
		OverallVerdict: VerdictOK,
		Audit: &Audit{
			Baseline:  []CellFlag{{RunID: "b4", Task: "sql", Metric: "tokens_out", Value: 1000, Median: 100, Band: 50}},
			Candidate: []CellFlag{{RunID: "c4", Metric: "duration_ms", Value: 900, Median: 100, Band: 50}},
		},
	}
	var buf bytes.Buffer
	require.NoError(t, RenderJSON(rep, &buf))
	raw := buf.String()
	assert.Contains(t, raw, `"audit"`)
	assert.Contains(t, raw, `"run_id"`)
	assert.Contains(t, raw, `"metric"`)
	assert.Contains(t, raw, `"median"`)
	assert.Contains(t, raw, `"band"`)
	assert.Equal(t, 1, strings.Count(raw, `"task"`))
	var got Report
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	require.NotNil(t, got.Audit)
	assert.Equal(t, rep.Audit, got.Audit)
}

func TestDefaultThresholdsAudit(t *testing.T) {
	t.Parallel()
	th := DefaultThresholds()
	assert.InDelta(t, 3.0, th.AuditIQRFactor, 1e-9)
	assert.InDelta(t, 0.5, th.AuditRelDelta, 1e-9)
}
