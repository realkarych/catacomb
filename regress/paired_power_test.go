package regress

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/aggregate"
)

func pairedDurationInput(deltas []float64) Input {
	b := make([]aggregate.TaskStats, 0, len(deltas))
	c := make([]aggregate.TaskStats, 0, len(deltas))
	for i, d := range deltas {
		id := fmt.Sprintf("t%02d", i)
		b = append(b, metricTask(id, 5, 1000))
		c = append(c, metricTask(id, 5, 1000+d))
	}
	return Input{
		Baseline:  aggregate.Report{Runs: 5 * len(deltas), Tasks: b},
		Candidate: aggregate.Report{Runs: 5 * len(deltas), Tasks: c},
	}
}

func repeatDeltas(pos, neg, zero int) []float64 {
	out := make([]float64, 0, pos+neg+zero)
	for i := 0; i < pos; i++ {
		out = append(out, 100)
	}
	for i := 0; i < neg; i++ {
		out = append(out, -100)
	}
	for i := 0; i < zero; i++ {
		out = append(out, 0)
	}
	return out
}

func TestPairedGatePower(t *testing.T) {
	t.Parallel()
	th := DefaultThresholds()
	cases := []struct {
		name           string
		pos, neg, zero int
		wantVerdict    Verdict
		wantDetail     string
		wantGates      bool
	}{
		{"unanimous_5_gates", 5, 0, 0, VerdictRegression, "+5/5 tasks, p=0.03125", true},
		{"eight_seven_gates", 7, 1, 0, VerdictRegression, "+7/8 tasks, p=0.03516", true},
		{"eight_six_holds", 6, 2, 0, VerdictOK, "+6/8 tasks, p=0.1445", false},
		{"zero_delta_dropped", 5, 0, 1, VerdictRegression, "+5/5 tasks, p=0.03125", true},
		{"improvement_5", 0, 5, 0, VerdictImprovement, "-5/5 tasks, p=0.03125", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rep := Compare(pairedDurationInput(repeatDeltas(tc.pos, tc.neg, tc.zero)), th)
			f := findFinding(rep.Findings, "paired", "", "duration_ms")
			if tc.wantVerdict == VerdictOK {
				require.Nil(t, f)
			} else {
				require.NotNil(t, f)
				assert.Equal(t, tc.wantVerdict, f.Verdict)
				assert.Equal(t, tc.wantDetail, f.Detail)
			}
			assert.Equal(t, tc.wantGates, rep.OverallVerdict == VerdictRegression)
		})
	}
}

func TestPairedInsufficientBelowMinTasks(t *testing.T) {
	t.Parallel()
	rep := Compare(pairedDurationInput(repeatDeltas(3, 0, 0)), DefaultThresholds())
	f := findFinding(rep.Findings, "paired", "", "duration_ms")
	require.NotNil(t, f)
	assert.Equal(t, VerdictInsufficient, f.Verdict)
	assert.Contains(t, f.Detail, "matched 3 tasks")
	assert.Contains(t, f.Detail, "paired min 5")
}

func TestPairedDormantNoTasks(t *testing.T) {
	t.Parallel()
	rep := Compare(Input{Baseline: aggregate.Report{Runs: 10}, Candidate: aggregate.Report{Runs: 10}}, DefaultThresholds())
	assert.Nil(t, findFinding(rep.Findings, "paired", "", "duration_ms"))
	if rep.Sensitivity != nil {
		assert.Nil(t, rep.Sensitivity.Paired)
	}
}

func TestPairedScopeOrder(t *testing.T) {
	t.Parallel()
	assert.Equal(t, scopeOrder["total"]+1, scopeOrder["paired"])
	assert.Less(t, scopeOrder["paired"], scopeOrder["phase"])
	assert.Less(t, scopeOrder["phase"], scopeOrder["step"])

	rep := Compare(pairedDurationInput(repeatDeltas(5, 0, 0)), DefaultThresholds())
	totalIdx, pairedIdx := -1, -1
	for i, f := range rep.Findings {
		if f.Scope == "total" && totalIdx == -1 {
			totalIdx = i
		}
		if f.Scope == "paired" && pairedIdx == -1 {
			pairedIdx = i
		}
	}
	require.NotEqual(t, -1, totalIdx)
	require.NotEqual(t, -1, pairedIdx)
	assert.Less(t, totalIdx, pairedIdx)
}

func TestPairedDisclosureFires(t *testing.T) {
	t.Parallel()
	for _, nTasks := range []int{1, 2, 3, 4} {
		t.Run(fmt.Sprintf("tasks_%d", nTasks), func(t *testing.T) {
			t.Parallel()
			rep := Compare(pairedDurationInput(repeatDeltas(nTasks, 0, 0)), DefaultThresholds())
			f := findFinding(rep.Findings, "paired", "", "duration_ms")
			require.NotNil(t, f)
			assert.Equal(t, VerdictInsufficient, f.Verdict)
			assert.Equal(t, fmt.Sprintf("matched %d tasks below paired min 5", nTasks), f.Detail)
			require.NotNil(t, rep.Sensitivity)
			require.NotNil(t, rep.Sensitivity.Paired)
			assert.False(t, rep.Sensitivity.Paired.Reachable)
			assert.Equal(t, 5, rep.Sensitivity.Paired.MinFullFlipRuns)

			var buf bytes.Buffer
			RenderHuman(rep, &buf)
			assert.Contains(t, buf.String(), "paired gate needs k>=5 tasks")
		})
	}
}

func TestPairedDisclosureAlphaUnreachable(t *testing.T) {
	t.Parallel()
	th := DefaultThresholds()
	th.PairedAlpha = 0.01
	rep := Compare(pairedDurationInput(repeatDeltas(5, 0, 0)), th)
	assert.Nil(t, findFinding(rep.Findings, "paired", "", "duration_ms"))
	assert.NotEqual(t, VerdictRegression, rep.OverallVerdict)
	require.NotNil(t, rep.Sensitivity)
	require.NotNil(t, rep.Sensitivity.Paired)
	assert.False(t, rep.Sensitivity.Paired.Reachable)
	assert.Equal(t, 7, rep.Sensitivity.Paired.MinFullFlipRuns)

	var buf bytes.Buffer
	RenderHuman(rep, &buf)
	assert.Contains(t, buf.String(), "paired gate needs k>=7 tasks")
}

func TestPairedDisclosureReachableOmitted(t *testing.T) {
	t.Parallel()
	rep := Compare(pairedDurationInput(repeatDeltas(5, 0, 0)), DefaultThresholds())
	require.Equal(t, VerdictRegression, rep.OverallVerdict)
	assert.Nil(t, rep.Sensitivity)
}

func TestPairedSensitivityRoundTrip(t *testing.T) {
	t.Parallel()
	rep := Report{
		OverallVerdict: VerdictOK,
		Sensitivity: &Sensitivity{
			Presence:  RateSensitivity{Reachable: true, MinFullFlipRuns: 3},
			ErrorRate: RateSensitivity{Reachable: true, MinFullFlipRuns: 3},
			Paired:    &RateSensitivity{Reachable: false, MinFullFlipRuns: 5},
		},
	}
	var buf bytes.Buffer
	require.NoError(t, RenderJSON(rep, &buf))
	require.Contains(t, buf.String(), `"paired"`)
	var got Report
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	require.NotNil(t, got.Sensitivity)
	require.NotNil(t, got.Sensitivity.Paired)
	assert.Equal(t, rep.Sensitivity.Paired, got.Sensitivity.Paired)
}
