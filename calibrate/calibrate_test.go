package calibrate

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/aggregate"
	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/regress"
)

func i64(v int64) *int64 { return &v }

func f64(v float64) *float64 { return &v }

func fixtureRun(idx int, durationMS int64) aggregate.RunGraph {
	start := time.Unix(1_700_000_000, 0).UTC().Add(time.Duration(idx) * time.Hour)
	end := start.Add(time.Duration(durationMS) * time.Millisecond)
	id := fmt.Sprintf("r%02d", idx)
	return aggregate.RunGraph{
		Run: model.Run{ID: id, Status: model.StatusOK, StartedAt: &start, EndedAt: &end},
		Nodes: []*model.Node{
			{
				ID: id + "-s1", RunID: id, Type: model.NodeToolCall, Name: "search",
				Status: model.StatusOK, StepKey: "s1",
				CostUSD: f64(1), TokensIn: i64(10), TokensOut: i64(5), DurationMS: i64(100),
			},
		},
	}
}

func fixtureGroup(durationsMS ...int64) []aggregate.RunGraph {
	group := make([]aggregate.RunGraph, 0, len(durationsMS))
	for i, d := range durationsMS {
		group = append(group, fixtureRun(i, d))
	}
	return group
}

func withTaskLabels(group []aggregate.RunGraph, task string) []aggregate.RunGraph {
	for i := range group {
		group[i].Run.Labels = map[string]string{"task": task}
	}
	return group
}

func TestCalibrateInsufficientRuns(t *testing.T) {
	got := Calibrate(fixtureGroup(10000, 10000, 10000, 10000), regress.DefaultThresholds())
	want := CalibrateReport{
		Runs:       4,
		MinSupport: 3,
		RunIDs:     []string{"r00", "r01", "r02", "r03"},
		Thresholds: regress.DefaultThresholds(),
		Sufficient: false,
		Detail:     "self-check needs k>=6 runs (have 4)",
	}
	require.Equal(t, want, got)
}

func TestCalibrateReportEchoesRunIDsAndThresholds(t *testing.T) {
	th := regress.DefaultThresholds()
	got := Calibrate(fixtureGroup(10000, 10000, 10000, 10000, 10000, 10000), th)
	assert.Equal(t, []string{"r00", "r01", "r02", "r03", "r04", "r05"}, got.RunIDs)
	assert.Equal(t, th, got.Thresholds)
}

func TestCalibrateIdenticalRunsClean(t *testing.T) {
	got := Calibrate(fixtureGroup(10000, 10000, 10000, 10000, 10000, 10000), regress.DefaultThresholds())
	require.True(t, got.Sufficient)
	assert.Equal(t, 6, got.Runs)
	assert.Equal(t, 3, got.MinSupport)
	assert.Empty(t, got.Detail)
	require.NotNil(t, got.Split)
	assert.Equal(t, 3, got.Split.FirstN)
	assert.Equal(t, 3, got.Split.SecondN)
	assert.Equal(t, regress.VerdictOK, got.Split.Verdict)
	assert.Empty(t, got.Split.Drift)
	require.NotNil(t, got.Influence)
	assert.False(t, got.Influence.Evaluated)
	assert.Equal(t, "leave-one-out needs k>=7 runs (have 6)", got.Influence.Detail)
	assert.Empty(t, got.Influence.FlippingRuns)
}

func TestCalibrateSecondHalfDriftGates(t *testing.T) {
	got := Calibrate(fixtureGroup(10000, 10000, 10000, 14000, 14000, 14000), regress.DefaultThresholds())
	require.True(t, got.Sufficient)
	require.NotNil(t, got.Split)
	assert.Equal(t, regress.VerdictRegression, got.Split.Verdict)
	require.Len(t, got.Split.Drift, 1)
	assert.Equal(t, DriftFinding{
		Scope:     "total",
		Metric:    "duration_ms",
		Verdict:   regress.VerdictRegression,
		Baseline:  10000,
		Candidate: 14000,
	}, got.Split.Drift[0])
	require.NotNil(t, got.Influence)
	assert.False(t, got.Influence.Evaluated)
	assert.Equal(t, "leave-one-out needs k>=7 runs (have 6)", got.Influence.Detail)
}

func TestCalibrateLeaveOneOutNamesFlippingRun(t *testing.T) {
	got := Calibrate(fixtureGroup(10000, 10000, 30000, 10000, 14000, 14000, 14000), regress.DefaultThresholds())
	require.True(t, got.Sufficient)
	require.NotNil(t, got.Split)
	assert.Equal(t, 3, got.Split.FirstN)
	assert.Equal(t, 4, got.Split.SecondN)
	assert.Equal(t, regress.VerdictOK, got.Split.Verdict)
	assert.Empty(t, got.Split.Drift)
	require.NotNil(t, got.Influence)
	require.True(t, got.Influence.Evaluated)
	assert.Empty(t, got.Influence.Detail)
	require.Equal(t, []FlipFinding{
		{DroppedIndex: 2, RunID: "r02", From: regress.VerdictOK, To: regress.VerdictRegression},
	}, got.Influence.FlippingRuns)
}

func TestCalibrateSingleTaskInsufficientSplitCarriesNotes(t *testing.T) {
	group := withTaskLabels(fixtureGroup(10000, 10000, 10000, 10000, 10000, 10000), "sql")
	got := Calibrate(group, regress.DefaultThresholds())
	require.True(t, got.Sufficient)
	require.NotNil(t, got.Split)
	assert.Equal(t, regress.VerdictInsufficient, got.Split.Verdict)
	assert.Equal(t, []string{"matched 1 task below paired min 5"}, got.Split.Notes)
}

func TestCalibrateNonInsufficientSplitHasNoNotes(t *testing.T) {
	got := Calibrate(fixtureGroup(10000, 10000, 10000, 14000, 14000, 14000), regress.DefaultThresholds())
	require.NotNil(t, got.Split)
	assert.Equal(t, regress.VerdictRegression, got.Split.Verdict)
	assert.Nil(t, got.Split.Notes)
}

func TestInsufficientNotesDedupesAndSkipsEmptyDetail(t *testing.T) {
	notes := insufficientNotes([]regress.Finding{
		{Verdict: regress.VerdictInsufficient, Detail: "baseline n=2 below min support 3"},
		{Verdict: regress.VerdictInsufficient, Detail: "baseline n=2 below min support 3"},
		{Verdict: regress.VerdictInsufficient},
		{Verdict: regress.VerdictOK, Detail: "not a note"},
		{Verdict: regress.VerdictInsufficient, Detail: "matched 1 task below paired min 5"},
	})
	assert.Equal(t, []string{"baseline n=2 below min support 3", "matched 1 task below paired min 5"}, notes)
}

func TestCalibrateDeterministic(t *testing.T) {
	group := fixtureGroup(10000, 10000, 30000, 10000, 14000, 14000, 14000)
	first := Calibrate(group, regress.DefaultThresholds())
	second := Calibrate(group, regress.DefaultThresholds())
	require.Equal(t, first, second)
}
