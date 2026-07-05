package regress

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/aggregate"
	"github.com/realkarych/catacomb/model"
)

func sampleReport() Report {
	totals := func(cost float64) aggregate.RunTotals {
		return aggregate.RunTotals{
			DurationMS: metric(5, 1000, 900, 1100),
			CostUSD:    metric(5, cost, cost*0.9, cost*1.1),
			TokensIn:   metric(5, 2000, 1900, 2100),
			TokensOut:  metric(5, 800, 750, 850),
			Nodes:      metric(5, 12, 11, 13),
		}
	}

	paBase := presentRow("pa", "alpha", 5)
	paCand := presentRow("pa", "alpha", 5)
	paCand.StatusRates = map[model.Status]float64{model.StatusError: 0.6}
	paCand.DurationMS = metric(5, 600, 500, 700)

	s1Base := presentRow("s1", "step-one", 5)
	s1Cand := presentRow("s1", "step-one", 5)
	s1Cand.DurationMS = metric(5, 1600, 1500, 1700)

	in := Input{
		Baseline: aggregate.Report{
			Runs:   5,
			Totals: totals(0.10),
			Phases: []aggregate.Row{paBase, presentRow("pb", "beta", 5)},
			Steps:  []aggregate.Row{s1Base, presentRow("s2", "step-two", 5)},
		},
		Candidate: aggregate.Report{
			Runs:   5,
			Totals: totals(0.20),
			Phases: []aggregate.Row{paCand, presentRow("pd", "delta", 5)},
			Steps:  []aggregate.Row{s1Cand},
		},
	}
	return Compare(in, DefaultThresholds())
}

func TestRenderHumanGolden(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	RenderHuman(sampleReport(), &buf)
	golden, err := os.ReadFile("testdata/golden_report.txt")
	require.NoError(t, err)
	assert.Equal(t, string(golden), buf.String())
}

func TestRenderJSONGolden(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	require.NoError(t, RenderJSON(sampleReport(), &buf))
	golden, err := os.ReadFile("testdata/golden_report.json")
	require.NoError(t, err)
	assert.Equal(t, string(golden), buf.String())
}

func TestRenderHumanNameColumn(t *testing.T) {
	t.Parallel()
	rep := Report{
		OverallVerdict: VerdictOK,
		Findings: []Finding{
			{Scope: "total", Key: "", Name: "", Metric: "cost_usd", Verdict: VerdictOK, Baseline: 0.10, Candidate: 0.10, BandLo: 0.07, BandHi: 0.13},
			{Scope: "phase", Key: "pa", Name: "alpha", Metric: "duration_ms", Verdict: VerdictOK, Baseline: 1000, Candidate: 1000, BandLo: 700, BandHi: 1300},
			{Scope: "step", Key: "s1", Name: "step-one", Metric: "duration_ms", Verdict: VerdictOK, Baseline: 1000, Candidate: 1000, BandLo: 700, BandHi: 1300},
		},
	}
	var buf bytes.Buffer
	RenderHuman(rep, &buf)
	rows := map[string][]string{}
	for _, line := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 5 {
			rows[fields[2]] = fields
		}
	}
	require.Contains(t, rows, "KEY")
	assert.Equal(t, "NAME", rows["KEY"][3])
	assert.Equal(t, "alpha", rows["pa"][3])
	assert.Equal(t, "step-one", rows["s1"][3])
	assert.Equal(t, "-", rows["-"][3])
}

func TestRenderHumanPresenceNormalized(t *testing.T) {
	t.Parallel()
	rep := Report{
		OverallVerdict: VerdictRegression,
		Findings: []Finding{{
			Scope:     "phase",
			Key:       "pa",
			Metric:    "presence",
			Verdict:   VerdictRegression,
			Baseline:  0.0,
			Candidate: 0.667,
			BandLo:    0,
			BandHi:    0.20,
		}},
	}
	var buf bytes.Buffer
	RenderHuman(rep, &buf)
	out := buf.String()
	assert.Contains(t, out, "1.00")
	assert.Contains(t, out, "0.33")
	assert.Contains(t, out, "regression")
	assert.NotContains(t, out, "0.67")
	assert.Contains(t, out, "[0.80, 1.00]")
}

func TestRenderHumanSensitivityNote(t *testing.T) {
	t.Parallel()
	rep := Report{
		BaselineRuns: 3, CandidateRuns: 3, OverallVerdict: VerdictOK,
		Sensitivity: &Sensitivity{
			Presence:  RateSensitivity{Reachable: false, MinFullFlipRuns: 4},
			ErrorRate: RateSensitivity{Reachable: false, MinFullFlipRuns: 4},
		},
	}
	var buf bytes.Buffer
	RenderHuman(rep, &buf)
	require.Contains(t, buf.String(), "sensitivity: rate gate cannot fire at this support (full flip needs k>=4 presence, full flip needs k>=4 error_rate)")
}

func TestRenderHumanSensitivityUnreachable(t *testing.T) {
	t.Parallel()
	rep := Report{
		BaselineRuns: 3, CandidateRuns: 3, OverallVerdict: VerdictOK,
		Sensitivity: &Sensitivity{
			Presence:  RateSensitivity{Reachable: false, MinFullFlipRuns: 0},
			ErrorRate: RateSensitivity{Reachable: false, MinFullFlipRuns: 5},
		},
	}
	var buf bytes.Buffer
	RenderHuman(rep, &buf)
	out := buf.String()
	assert.Contains(t, out, "full flip unreachable presence")
	assert.NotContains(t, out, "k>=never")
}

func TestRenderHumanNoSensitivityNote(t *testing.T) {
	t.Parallel()
	rep := Report{BaselineRuns: 3, CandidateRuns: 3, OverallVerdict: VerdictOK}
	var buf bytes.Buffer
	RenderHuman(rep, &buf)
	assert.NotContains(t, buf.String(), "sensitivity:")
}

func TestRenderJSONSensitivityOmitted(t *testing.T) {
	t.Parallel()
	rep := Report{BaselineRuns: 3, CandidateRuns: 3, OverallVerdict: VerdictOK}
	var buf bytes.Buffer
	require.NoError(t, RenderJSON(rep, &buf))
	assert.NotContains(t, buf.String(), "sensitivity")
}

func TestRenderJSONSensitivityRoundTrip(t *testing.T) {
	t.Parallel()
	rep := Report{
		BaselineRuns: 3, CandidateRuns: 3, OverallVerdict: VerdictOK,
		Sensitivity: &Sensitivity{
			Presence:  RateSensitivity{Reachable: false, MinFullFlipRuns: 4},
			ErrorRate: RateSensitivity{Reachable: true, MinFullFlipRuns: 2},
		},
	}
	var buf bytes.Buffer
	require.NoError(t, RenderJSON(rep, &buf))
	require.Contains(t, buf.String(), "sensitivity")
	var got Report
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	require.NotNil(t, got.Sensitivity)
	assert.Equal(t, rep.Sensitivity.Presence, got.Sensitivity.Presence)
	assert.Equal(t, rep.Sensitivity.ErrorRate, got.Sensitivity.ErrorRate)
}

type errWriter struct{}

func (errWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestRenderJSONError(t *testing.T) {
	t.Parallel()
	err := RenderJSON(sampleReport(), errWriter{})
	require.Error(t, err)
}
