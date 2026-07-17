package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/evidence"
	"github.com/realkarych/catacomb/regress"
)

func TestOfflineParityFixtureCarriesMarkers(t *testing.T) {
	g, err := loadGraphOffline(filepath.Join("testdata", "session_marked.jsonl"), nil, "exec-parity", nil, nil)
	require.NoError(t, err)
	names := graphMarkerNames(g)
	require.NotEmpty(t, names)
	sorted := make([]string, 0, len(names))
	for name := range names {
		sorted = append(sorted, name)
	}
	sort.Strings(sorted)
	t.Logf("markers in session_marked.jsonl: %v", sorted)
}

func writeParityEvidence(t *testing.T, root, id, variant, fixture string, rep int) {
	t.Helper()
	m := evidence.Meta{
		RunID:       id,
		Task:        "t1",
		Variant:     variant,
		Rep:         rep,
		SessionID:   "s1",
		Labels:      map[string]string{"variant": variant},
		MarkerName:  "task:t1",
		MarkerStart: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
		MarkerEnd:   time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC),
		FinishedAt:  time.Date(2026, 6, 20, 10, 1, 0, 0, time.UTC),
	}
	src := filepath.Join("testdata", fixture)
	require.NoError(t, evidence.Write(filepath.Join(root, id), m, []evidence.SourceFile{{Src: src, Rel: "session.jsonl"}}))
}

func parityRunsDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for i := 0; i < 3; i++ {
		writeParityEvidence(t, root, fmt.Sprintf("base-%d", i), "base", "session_marked.jsonl", i+1)
		writeParityEvidence(t, root, fmt.Sprintf("cand-%d", i), "cand", "session.jsonl", i+1)
		writeParityEvidence(t, root, fmt.Sprintf("ctrl-%d", i), "ctrl", "session_marked.jsonl", i+1)
	}
	return root
}

func runParityRegressJSON(t *testing.T, runsDir, baseline, candidate string) (regress.Report, int) {
	t.Helper()
	var out, errBuf bytes.Buffer
	code := run([]string{
		"regress", "--runs-dir", runsDir,
		"--db", filepath.Join(t.TempDir(), "absent.db"),
		"--baseline", baseline, "--candidate", candidate,
		"--format", "json",
	}, &out, &errBuf)
	require.Empty(t, errBuf.String())
	var rep regress.Report
	require.NoError(t, json.Unmarshal(out.Bytes(), &rep), out.String())
	return rep, code
}

func regressionFindings(rep regress.Report) []regress.Finding {
	var out []regress.Finding
	for _, f := range rep.Findings {
		if f.Verdict == regress.VerdictRegression {
			out = append(out, f)
		}
	}
	return out
}

func TestOfflineParityGate(t *testing.T) {
	root := parityRunsDir(t)

	degraded, degradedCode := runParityRegressJSON(t, root, "label:variant=base", "label:variant=cand")
	assert.Equal(t, 1, degradedCode)
	assert.Equal(t, regress.VerdictRegression, degraded.OverallVerdict)
	assert.Equal(t, 3, degraded.BaselineRuns)
	assert.Equal(t, 3, degraded.CandidateRuns)
	assert.NotEmpty(t, regressionFindings(degraded))

	control, controlCode := runParityRegressJSON(t, root, "label:variant=base", "label:variant=ctrl")
	assert.Equal(t, 0, controlCode)
	assert.Equal(t, regress.VerdictOK, control.OverallVerdict)
	assert.Equal(t, 3, control.BaselineRuns)
	assert.Equal(t, 3, control.CandidateRuns)
	assert.Empty(t, regressionFindings(control))
	assert.Zero(t, control.Regressions)
}

func TestOfflineParityGateErrorIsRegressionDetected(t *testing.T) {
	root := parityRunsDir(t)
	f := regressFlags{
		runsDir:    root,
		baseline:   "label:variant=base",
		candidate:  "label:variant=cand",
		thresholds: regress.DefaultThresholds(),
	}
	err := runRegress(io.Discard, io.Discard, openStore(nil), newPricer, f)
	require.ErrorIs(t, err, errRegressionDetected)

	f.candidate = "label:variant=ctrl"
	require.NoError(t, runRegress(io.Discard, io.Discard, openStore(nil), newPricer, f))
}
