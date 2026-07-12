package regress

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/aggregate"
)

func outcomeTask(task string, n, ones int) aggregate.TaskStats {
	return aggregate.TaskStats{Task: task, Runs: n, Outcome: &aggregate.TaskOutcome{N: n, Ones: ones}}
}

func plainTask(task string) aggregate.TaskStats {
	return aggregate.TaskStats{Task: task, Runs: 1}
}

func TestPassK(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		c, n, k int
		want    float64
	}{
		{"tau_bench_k1", 3, 4, 1, 0.75},
		{"tau_bench_k2", 3, 4, 2, 0.5},
		{"tau_bench_k3", 3, 4, 3, 0.25},
		{"tau_bench_k4", 3, 4, 4, 0},
		{"all_ones_k1", 5, 5, 1, 1},
		{"all_ones_k5", 5, 5, 5, 1},
		{"zero_c", 0, 5, 1, 0},
		{"c_lt_k", 2, 5, 3, 0},
		{"single", 1, 1, 1, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.InDelta(t, tc.want, passK(tc.c, tc.n, tc.k), 1e-9)
		})
	}
}

func TestGroupReliabilityUnequalNs(t *testing.T) {
	t.Parallel()
	gr, ok := groupReliability([]aggregate.TaskStats{
		outcomeTask("a", 4, 3),
		outcomeTask("b", 3, 3),
	})
	require.True(t, ok)
	assert.Equal(t, 3, gr.KMax)
	require.Len(t, gr.Tasks, 2)
	assert.Equal(t, "a", gr.Tasks[0].Task)
	assert.Equal(t, 4, gr.Tasks[0].N)
	assert.Equal(t, 3, gr.Tasks[0].Ones)
	assert.InDeltaSlice(t, []float64{0.75, 0.5, 0.25}, gr.Tasks[0].PassK, 1e-9)
	assert.InDeltaSlice(t, []float64{1, 1, 1}, gr.Tasks[1].PassK, 1e-9)
	assert.InDeltaSlice(t, []float64{0.875, 0.75, 0.625}, gr.Mean, 1e-9)
}

func TestGroupReliabilityBoundaries(t *testing.T) {
	t.Parallel()
	allOnes, ok := groupReliability([]aggregate.TaskStats{outcomeTask("a", 3, 3)})
	require.True(t, ok)
	assert.InDeltaSlice(t, []float64{1, 1, 1}, allOnes.Mean, 1e-9)

	allZero, ok := groupReliability([]aggregate.TaskStats{outcomeTask("a", 3, 0)})
	require.True(t, ok)
	assert.InDeltaSlice(t, []float64{0, 0, 0}, allZero.Mean, 1e-9)
}

func TestGroupReliabilityMixedFiltersPlain(t *testing.T) {
	t.Parallel()
	gr, ok := groupReliability([]aggregate.TaskStats{outcomeTask("a", 2, 1), plainTask("z")})
	require.True(t, ok)
	require.Len(t, gr.Tasks, 1)
	assert.Equal(t, "a", gr.Tasks[0].Task)
	assert.Equal(t, 2, gr.KMax)
}

func TestGroupReliabilityNoOutcomes(t *testing.T) {
	t.Parallel()
	_, ok := groupReliability([]aggregate.TaskStats{plainTask("a"), plainTask("b")})
	assert.False(t, ok)
	_, ok = groupReliability(nil)
	assert.False(t, ok)
}

func TestComputeReliability(t *testing.T) {
	t.Parallel()
	with := aggregate.Report{Tasks: []aggregate.TaskStats{outcomeTask("a", 2, 1)}}
	without := aggregate.Report{Tasks: []aggregate.TaskStats{plainTask("a")}}
	empty := aggregate.Report{}
	cases := []struct {
		name    string
		b, c    aggregate.Report
		wantNil bool
	}{
		{"both_have", with, with, false},
		{"baseline_only", with, without, true},
		{"candidate_only", without, with, true},
		{"neither", empty, empty, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := computeReliability(tc.b, tc.c)
			if tc.wantNil {
				assert.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			assert.Equal(t, 2, got.Baseline.KMax)
			assert.Equal(t, 2, got.Candidate.KMax)
		})
	}
}

func TestCompareReliabilityWired(t *testing.T) {
	t.Parallel()
	in := Input{
		Baseline:  aggregate.Report{Runs: 2, Tasks: []aggregate.TaskStats{outcomeTask("a", 2, 2)}},
		Candidate: aggregate.Report{Runs: 2, Tasks: []aggregate.TaskStats{outcomeTask("a", 2, 1)}},
	}
	rep := Compare(in, DefaultThresholds())
	require.NotNil(t, rep.Reliability)
	assert.Equal(t, 2, rep.Reliability.Baseline.KMax)
	assert.InDeltaSlice(t, []float64{1, 1}, rep.Reliability.Baseline.Mean, 1e-9)
	assert.InDeltaSlice(t, []float64{0.5, 0}, rep.Reliability.Candidate.Mean, 1e-9)
}

func TestCompareReliabilityDormant(t *testing.T) {
	t.Parallel()
	rep := Compare(Input{Baseline: aggregate.Report{Runs: 2}, Candidate: aggregate.Report{Runs: 2}}, DefaultThresholds())
	assert.Nil(t, rep.Reliability)
}

func TestRenderHumanReliabilityPresent(t *testing.T) {
	t.Parallel()
	rep := Report{
		OverallVerdict: VerdictOK,
		Reliability: &Reliability{
			Baseline: GroupReliability{
				Tasks: []TaskReliability{
					{Task: "a", N: 2, Ones: 1, PassK: []float64{0.5, 0}},
					{Task: "b", N: 2, Ones: 2, PassK: []float64{1, 1}},
				},
				KMax: 2,
				Mean: []float64{0.75, 0.5},
			},
			Candidate: GroupReliability{
				Tasks: []TaskReliability{{Task: "a", N: 2, Ones: 0, PassK: []float64{0, 0}}},
				KMax:  2,
				Mean:  []float64{0, 0},
			},
		},
	}
	var buf bytes.Buffer
	RenderHuman(rep, &buf)
	out := buf.String()
	assert.Contains(t, out, "reliability (baseline): pass^1 0.75 -> pass^2 0.50 (2 tasks)")
	assert.Contains(t, out, "reliability (candidate): pass^1 0.00 -> pass^2 0.00 (1 tasks)")
}

func TestRenderHumanReliabilitySinglePoint(t *testing.T) {
	t.Parallel()
	rep := Report{
		OverallVerdict: VerdictOK,
		Reliability: &Reliability{
			Baseline:  GroupReliability{Tasks: []TaskReliability{{Task: "a", N: 1, Ones: 1, PassK: []float64{1}}}, KMax: 1, Mean: []float64{1}},
			Candidate: GroupReliability{Tasks: []TaskReliability{{Task: "a", N: 1, Ones: 1, PassK: []float64{1}}}, KMax: 1, Mean: []float64{1}},
		},
	}
	var buf bytes.Buffer
	RenderHuman(rep, &buf)
	out := buf.String()
	assert.Contains(t, out, "reliability (baseline): pass^1 1.00 (1 tasks)")
	assert.NotContains(t, out, "->")
}

func TestRenderHumanReliabilityAbsent(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	RenderHuman(Report{OverallVerdict: VerdictOK}, &buf)
	assert.NotContains(t, buf.String(), "reliability")
}

func TestRenderJSONReliabilityRoundTrip(t *testing.T) {
	t.Parallel()
	rep := Report{
		OverallVerdict: VerdictOK,
		Reliability: &Reliability{
			Baseline:  GroupReliability{Tasks: []TaskReliability{{Task: "a", N: 4, Ones: 3, PassK: []float64{0.75, 0.5, 0.25}}}, KMax: 3, Mean: []float64{0.75, 0.5, 0.25}},
			Candidate: GroupReliability{Tasks: []TaskReliability{{Task: "a", N: 4, Ones: 4, PassK: []float64{1, 1, 1}}}, KMax: 3, Mean: []float64{1, 1, 1}},
		},
	}
	var buf bytes.Buffer
	require.NoError(t, RenderJSON(rep, &buf))
	require.Contains(t, buf.String(), "reliability")
	require.Contains(t, buf.String(), "pass_k")
	var got Report
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	require.NotNil(t, got.Reliability)
	assert.Equal(t, rep.Reliability.Baseline, got.Reliability.Baseline)
	assert.Equal(t, rep.Reliability.Candidate, got.Reliability.Candidate)
}

func TestRenderJSONReliabilityOmitted(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	require.NoError(t, RenderJSON(Report{OverallVerdict: VerdictOK}, &buf))
	assert.NotContains(t, buf.String(), "reliability")
}
