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

func TestRenderHumanAnnotationDetail(t *testing.T) {
	t.Parallel()
	rep := Report{
		OverallVerdict: VerdictRegression,
		Findings: []Finding{{
			Scope:     "total",
			Metric:    "ann:verifier.pass",
			Verdict:   VerdictRegression,
			Baseline:  1.0,
			Candidate: 0.4,
			BandLo:    0.6,
			BandHi:    1.0,
			Detail:    "ones 5/5 -> 2/5",
		}},
	}
	var buf bytes.Buffer
	RenderHuman(rep, &buf)
	out := buf.String()
	assert.Contains(t, out, "DETAIL")
	assert.Contains(t, out, "ann:verifier.pass")
	assert.Contains(t, out, "ones 5/5 -> 2/5")
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
	require.Contains(t, buf.String(), "sensitivity: gate cannot fire at this support (full flip needs k>=4 presence, full flip needs k>=4 error_rate)")
}

func TestRenderHumanSensitivityOnlyUnreachableAxes(t *testing.T) {
	t.Parallel()
	rep := Report{
		BaselineRuns: 5, CandidateRuns: 5, OverallVerdict: VerdictOK,
		Sensitivity: &Sensitivity{
			Presence:   RateSensitivity{Reachable: true, MinFullFlipRuns: 3},
			ErrorRate:  RateSensitivity{Reachable: true, MinFullFlipRuns: 3},
			Annotation: &RateSensitivity{Reachable: true, MinFullFlipRuns: 3},
			Paired:     &PairedSensitivity{Reachable: false, MinTasks: 5},
		},
	}
	var buf bytes.Buffer
	RenderHuman(rep, &buf)
	out := buf.String()
	assert.Contains(t, out, "sensitivity: gate cannot fire at this support (paired gate needs k>=5 tasks)\n")
	assert.NotContains(t, out, "presence")
	assert.NotContains(t, out, "error_rate")
	assert.NotContains(t, out, "annotation")
}

func TestRenderHumanSensitivityAllReachableSilent(t *testing.T) {
	t.Parallel()
	rep := Report{
		BaselineRuns: 5, CandidateRuns: 5, OverallVerdict: VerdictOK,
		Sensitivity: &Sensitivity{
			Presence:  RateSensitivity{Reachable: true, MinFullFlipRuns: 3},
			ErrorRate: RateSensitivity{Reachable: true, MinFullFlipRuns: 3},
			Paired:    &PairedSensitivity{Reachable: true, MinTasks: 5},
		},
	}
	var buf bytes.Buffer
	RenderHuman(rep, &buf)
	assert.NotContains(t, buf.String(), "sensitivity:")
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

func TestRenderHumanSensitivityAnnotation(t *testing.T) {
	t.Parallel()
	withAnn := Report{
		BaselineRuns: 3, CandidateRuns: 3, OverallVerdict: VerdictOK,
		Sensitivity: &Sensitivity{
			Presence:   RateSensitivity{Reachable: false, MinFullFlipRuns: 4},
			ErrorRate:  RateSensitivity{Reachable: false, MinFullFlipRuns: 4},
			Annotation: &RateSensitivity{Reachable: false, MinFullFlipRuns: 6},
		},
	}
	var buf bytes.Buffer
	RenderHuman(withAnn, &buf)
	require.Contains(t, buf.String(), "sensitivity: gate cannot fire at this support (full flip needs k>=4 presence, full flip needs k>=4 error_rate, full flip needs k>=6 annotation)")

	noAnn := Report{
		BaselineRuns: 3, CandidateRuns: 3, OverallVerdict: VerdictOK,
		Sensitivity: &Sensitivity{
			Presence:  RateSensitivity{Reachable: false, MinFullFlipRuns: 4},
			ErrorRate: RateSensitivity{Reachable: false, MinFullFlipRuns: 4},
		},
	}
	buf.Reset()
	RenderHuman(noAnn, &buf)
	out := buf.String()
	require.Contains(t, out, "sensitivity:")
	assert.NotContains(t, out, "annotation")
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

func TestRenderMarkdownGolden(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	RenderMarkdown(sampleReport(), &buf)
	golden, err := os.ReadFile("testdata/golden_report.md")
	require.NoError(t, err)
	assert.Equal(t, string(golden), buf.String())
}

func TestRenderMarkdownHeadline(t *testing.T) {
	t.Parallel()
	cases := []struct {
		verdict Verdict
		want    string
	}{
		{VerdictRegression, "**Verdict: ❌ regression**"},
		{VerdictOK, "**Verdict: ✅ ok**"},
		{VerdictImprovement, "**Verdict: ✅ improvement**"},
		{VerdictInsufficient, "**Verdict: ⚠️ insufficient**"},
		{VerdictNotable, "**Verdict: ⚠️ notable**"},
	}
	for _, tc := range cases {
		t.Run(string(tc.verdict), func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			RenderMarkdown(Report{OverallVerdict: tc.verdict}, &buf)
			assert.True(t, strings.HasPrefix(buf.String(), tc.want+"\n\n"), buf.String())
		})
	}
}

func TestRenderMarkdownRunsAndCoverageLine(t *testing.T) {
	t.Parallel()
	rep := Report{BaselineRuns: 3, CandidateRuns: 7, Coverage: Coverage{Steps: 0.5, Phases: 0.75}, OverallVerdict: VerdictOK}
	var buf bytes.Buffer
	RenderMarkdown(rep, &buf)
	assert.Contains(t, buf.String(), "baseline 3 runs · candidate 7 runs · coverage steps 0.50 phases 0.75\n")
}

func TestRenderMarkdownSensitivityBlockquote(t *testing.T) {
	t.Parallel()
	rep := Report{
		BaselineRuns: 3, CandidateRuns: 3, OverallVerdict: VerdictOK,
		Sensitivity: &Sensitivity{
			Presence:   RateSensitivity{Reachable: false, MinFullFlipRuns: 4},
			ErrorRate:  RateSensitivity{Reachable: false, MinFullFlipRuns: 0},
			Annotation: &RateSensitivity{Reachable: false, MinFullFlipRuns: 6},
			Paired:     &PairedSensitivity{Reachable: false, MinTasks: 5},
		},
	}
	var buf bytes.Buffer
	RenderMarkdown(rep, &buf)
	assert.Contains(t, buf.String(),
		"> ⚠️ gate cannot fire at this support: full flip needs k>=4 presence, full flip unreachable error_rate, full flip needs k>=6 annotation, paired gate needs k>=5 tasks\n")
}

func TestRenderMarkdownSensitivityReachableSilent(t *testing.T) {
	t.Parallel()
	rep := Report{
		OverallVerdict: VerdictOK,
		Sensitivity: &Sensitivity{
			Presence:   RateSensitivity{Reachable: true, MinFullFlipRuns: 3},
			ErrorRate:  RateSensitivity{Reachable: true, MinFullFlipRuns: 3},
			Annotation: &RateSensitivity{Reachable: true, MinFullFlipRuns: 3},
			Paired:     &PairedSensitivity{Reachable: true, MinTasks: 5},
		},
	}
	var buf bytes.Buffer
	RenderMarkdown(rep, &buf)
	assert.NotContains(t, buf.String(), "gate cannot fire")
	buf.Reset()
	RenderMarkdown(Report{OverallVerdict: VerdictOK}, &buf)
	assert.NotContains(t, buf.String(), "gate cannot fire")
}

func TestRenderMarkdownFindingRowPresenceNormalized(t *testing.T) {
	t.Parallel()
	rep := Report{
		OverallVerdict: VerdictRegression,
		Findings: []Finding{{
			Scope: "phase", Key: "pa", Name: "alpha", Metric: "presence",
			Verdict: VerdictRegression, Baseline: 0.0, Candidate: 0.667, BandLo: 0, BandHi: 0.20,
		}},
	}
	var buf bytes.Buffer
	RenderMarkdown(rep, &buf)
	assert.Contains(t, buf.String(), "| regression | phase | pa | alpha | presence | 1.00 | 0.33 | [0.80, 1.00] | - |\n")
}

func TestRenderMarkdownPipeEscaped(t *testing.T) {
	t.Parallel()
	rep := Report{
		OverallVerdict: VerdictRegression,
		Findings: []Finding{{
			Scope: "total", Key: "k|1", Name: "n|2", Metric: "ann:a|b",
			Verdict: VerdictRegression, Baseline: 1, Candidate: 0,
			Detail: "ones 5/5 -> 2/5 | flaky",
		}},
	}
	var buf bytes.Buffer
	RenderMarkdown(rep, &buf)
	assert.Contains(t, buf.String(), `| regression | total | k\|1 | n\|2 | ann:a\|b | 1.00 | 0.00 | - | ones 5/5 -> 2/5 \| flaky |`)
}

func TestRenderMarkdownEmptyFindingsHeaderOnly(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	RenderMarkdown(Report{OverallVerdict: VerdictOK}, &buf)
	want := "**Verdict: ✅ ok**\n\n" +
		"baseline 0 runs · candidate 0 runs · coverage steps 0.00 phases 0.00\n\n" +
		"| Verdict | Scope | Key | Name | Metric | Baseline | Candidate | Band | Detail |\n" +
		"|---|---|---|---|---|---|---|---|---|\n"
	assert.Equal(t, want, buf.String())
}

func TestRenderMarkdownDetailsBlock(t *testing.T) {
	t.Parallel()
	rep := Report{
		OverallVerdict: VerdictOK,
		Reliability: &Reliability{
			Baseline:  GroupReliability{Tasks: []TaskReliability{{Task: "a", N: 1, Ones: 1, PassK: []float64{1}}}, KMax: 1, Mean: []float64{1}},
			Candidate: GroupReliability{Tasks: []TaskReliability{{Task: "a", N: 1, Ones: 0, PassK: []float64{0}}}, KMax: 1, Mean: []float64{0}},
		},
		Audit: &Audit{
			Baseline:  []CellFlag{{RunID: "r1", Task: "t1", Metric: "cost_usd", Value: 5, Median: 2, Band: 1}},
			Candidate: []CellFlag{{RunID: "r2", Metric: "duration_ms", Value: 9000, Median: 1000, Band: 500}},
		},
	}
	var buf bytes.Buffer
	RenderMarkdown(rep, &buf)
	out := buf.String()
	assert.Contains(t, out, "\n<details>\n<summary>reliability &amp; audit</summary>\n\n")
	assert.Contains(t, out, "reliability (baseline): pass^1 1.00 (1 task)\n")
	assert.Contains(t, out, "reliability (candidate): pass^1 0.00 (1 task)\n")
	assert.Contains(t, out, "audit: baseline run r1 (task t1) cost_usd 5 vs group median 2 (band 1)\n")
	assert.Contains(t, out, "audit: candidate run r2 duration_ms 9000 vs group median 1000 (band 500)\n")
	assert.True(t, strings.HasSuffix(out, "\n</details>\n"), out)
}

func TestRenderMarkdownDetailsAuditOnly(t *testing.T) {
	t.Parallel()
	rep := Report{
		OverallVerdict: VerdictOK,
		Audit:          &Audit{Baseline: []CellFlag{{RunID: "r1", Metric: "cost_usd", Value: 5, Median: 2, Band: 1}}},
	}
	var buf bytes.Buffer
	RenderMarkdown(rep, &buf)
	out := buf.String()
	assert.Contains(t, out, "<details>")
	assert.Contains(t, out, "audit: baseline run r1 cost_usd 5 vs group median 2 (band 1)\n")
	assert.NotContains(t, out, "reliability (")
}

func TestRenderMarkdownOmitsDetailsWhenBothNil(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	RenderMarkdown(sampleReport(), &buf)
	assert.NotContains(t, buf.String(), "<details>")
}

func TestRenderMarkdownDeterministic(t *testing.T) {
	t.Parallel()
	var first, second bytes.Buffer
	RenderMarkdown(sampleReport(), &first)
	RenderMarkdown(sampleReport(), &second)
	assert.Equal(t, first.Bytes(), second.Bytes())
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
