package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/calibrate"
	"github.com/realkarych/catacomb/regress"
)

func calibrateEvidenceRoot(t *testing.T, n int) string {
	t.Helper()
	root := t.TempDir()
	for i := range n {
		writeEvidenceRun(t, root, fmt.Sprintf("aa-%d", i), "aa", "session_marked.jsonl")
	}
	return root
}

func TestCalibrateSixRunGroupHeadlineAndAA(t *testing.T) {
	root := calibrateEvidenceRoot(t, 6)
	var out, errBuf bytes.Buffer
	code := run([]string{"calibrate", "--runs-dir", root, "--group", "label:variant=aa"}, &out, &errBuf)
	assert.Equal(t, 0, code, errBuf.String())
	assert.Contains(t, out.String(), "self-check: sufficient · runs 6 · min-support 3\n")
	assert.Contains(t, out.String(), "A/A ok (first 3 vs second 3)\n")
	assert.Contains(t, out.String(), "influence: leave-one-out needs k>=7 runs (have 6)\n")
	assert.Empty(t, errBuf.String())
}

func TestCalibrateFormatJSONParsesToReport(t *testing.T) {
	root := calibrateEvidenceRoot(t, 6)
	var out, errBuf bytes.Buffer
	code := run([]string{"calibrate", "--runs-dir", root, "--group", "label:variant=aa", "--format", "json"}, &out, &errBuf)
	require.Equal(t, 0, code, errBuf.String())
	var rep calibrate.CalibrateReport
	require.NoError(t, json.Unmarshal(out.Bytes(), &rep))
	assert.Equal(t, 6, rep.Runs)
	assert.Equal(t, 3, rep.MinSupport)
	assert.True(t, rep.Sufficient)
	require.NotNil(t, rep.Split)
	assert.Equal(t, regress.VerdictOK, rep.Split.Verdict)
}

func TestCalibrateUnknownGroupExitTwo(t *testing.T) {
	root := calibrateEvidenceRoot(t, 6)
	var out, errBuf bytes.Buffer
	code := run([]string{"calibrate", "--runs-dir", root, "--group", "label:variant=none"}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), "matched no runs")
}

func TestCalibrateEmptyGroupSelectorExitTwo(t *testing.T) {
	root := calibrateEvidenceRoot(t, 6)
	var out, errBuf bytes.Buffer
	code := run([]string{"calibrate", "--runs-dir", root}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), "invalid selector")
}

func TestCalibrateBogusFormatExitTwo(t *testing.T) {
	root := calibrateEvidenceRoot(t, 6)
	var out, errBuf bytes.Buffer
	code := run([]string{"calibrate", "--runs-dir", root, "--group", "label:variant=aa", "--format", "bogus"}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), `unknown --format "bogus"`)
	assert.Contains(t, errBuf.String(), "human")
	assert.Contains(t, errBuf.String(), "json")
}

func TestCalibrateSeededDriftSurfacesDriftLine(t *testing.T) {
	root := t.TempDir()
	for i := range 3 {
		writeTokenEvidenceRun(t, root, fmt.Sprintf("dr-%d", i), "dr", 10)
	}
	for i := 3; i < 6; i++ {
		writeTokenEvidenceRun(t, root, fmt.Sprintf("dr-%d", i), "dr", 5000)
	}
	var out, errBuf bytes.Buffer
	code := run([]string{"calibrate", "--runs-dir", root, "--group", "label:variant=dr"}, &out, &errBuf)
	assert.Equal(t, 0, code, errBuf.String())
	assert.Contains(t, out.String(), "A/A regression (first 3 vs second 3)\n")
	assert.Contains(t, out.String(), "drift: total tokens_in regression 10.00 -> 5000.00\n")
}

func TestCalibrateInsufficientGroupHeadline(t *testing.T) {
	root := calibrateEvidenceRoot(t, 4)
	var out, errBuf bytes.Buffer
	code := run([]string{"calibrate", "--runs-dir", root, "--group", "label:variant=aa"}, &out, &errBuf)
	assert.Equal(t, 0, code, errBuf.String())
	assert.Contains(t, out.String(), "self-check: insufficient · runs 4 · min-support 3\n")
	assert.Contains(t, out.String(), "self-check needs k>=6 runs (have 4)\n")
	assert.NotContains(t, out.String(), "A/A")
}

func TestCalibrateMinSupportGuardExitTwo(t *testing.T) {
	root := calibrateEvidenceRoot(t, 6)
	var out, errBuf bytes.Buffer
	code := run([]string{"calibrate", "--runs-dir", root, "--group", "label:variant=aa", "--min-support", "0"}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), "calibrate: --min-support must be >= 1")
}

func TestCalibrateThresholdFlagsShared(t *testing.T) {
	cmd := newCalibrateCmd()
	for _, flag := range []string{
		"min-support", "presence-delta", "error-delta", "metric-rel-delta", "iqr-factor",
		"coverage-floor", "z", "annotation-rate-delta", "paired-alpha", "paired-min-tasks",
		"audit-iqr-factor", "audit-rel-delta", "fail-on-notable",
	} {
		assert.NotNil(t, cmd.Flags().Lookup(flag), "missing threshold flag %q", flag)
	}
}

func TestRunCalibrateRequiresRunsDir(t *testing.T) {
	f := calibrateFlags{group: "label:variant=aa", format: "human", thresholds: regress.DefaultThresholds()}
	err := runCalibrate(io.Discard, io.Discard, newPricer, f)
	require.ErrorIs(t, err, errCalibrateNoRunsDir)
	var opErr *operationalError
	require.ErrorAs(t, err, &opErr)
}

func TestRunCalibrateJSONRenderErrorOperational(t *testing.T) {
	root := calibrateEvidenceRoot(t, 1)
	f := calibrateFlags{
		group: "label:variant=aa", runsDir: root, format: "json",
		thresholds: regress.DefaultThresholds(),
	}
	err := runCalibrate(failWriter{}, io.Discard, newPricer, f)
	require.Error(t, err)
	var opErr *operationalError
	require.ErrorAs(t, err, &opErr)
}

func TestCalibrateHelpListsCommand(t *testing.T) {
	var out, errBuf bytes.Buffer
	code := run([]string{"--help"}, &out, &errBuf)
	assert.Equal(t, 0, code, errBuf.String())
	assert.Contains(t, out.String(), "calibrate")
}

func TestCalibrateOwnHelpClean(t *testing.T) {
	var out, errBuf bytes.Buffer
	code := run([]string{"calibrate", "--help"}, &out, &errBuf)
	assert.Equal(t, 0, code, errBuf.String())
	assert.Contains(t, out.String(), "--group")
	assert.Contains(t, out.String(), "--format")
	assert.Contains(t, out.String(), "--min-support")
	assert.Empty(t, errBuf.String())
}
