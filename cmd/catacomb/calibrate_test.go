package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/aggregate"
	"github.com/realkarych/catacomb/calibrate"
	"github.com/realkarych/catacomb/evidence"
	"github.com/realkarych/catacomb/model"
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

func writeTimedEvidenceRun(t *testing.T, root, id, variant string, extra map[string]string, start time.Time) {
	t.Helper()
	labels := map[string]string{"variant": variant}
	for k, v := range extra {
		labels[k] = v
	}
	m := evidence.Meta{
		RunID:       id,
		Task:        "t1",
		Variant:     variant,
		Rep:         1,
		SessionID:   "s1",
		Labels:      labels,
		MarkerName:  "task:t1",
		MarkerStart: start,
		MarkerEnd:   start.Add(100 * time.Second),
		FinishedAt:  start.Add(101 * time.Second),
	}
	src := filepath.Join("testdata", "session_marked.jsonl")
	require.NoError(t, evidence.Write(filepath.Join(root, id), m, []evidence.SourceFile{{Src: src, Rel: "session.jsonl"}}))
}

func runIDsOf(runs []aggregate.RunGraph) []string {
	ids := make([]string, 0, len(runs))
	for _, rg := range runs {
		ids = append(ids, rg.Run.ID)
	}
	return ids
}

func timedRunGraph(id string, started, ended *time.Time) aggregate.RunGraph {
	return aggregate.RunGraph{Run: model.Run{ID: id, StartedAt: started, EndedAt: ended}}
}

func TestOrderRunsByTimeWallClockBeatsLexicalRunID(t *testing.T) {
	t0 := time.Unix(10_000, 0).UTC()
	mk := func(id string, offset time.Duration) aggregate.RunGraph {
		s := t0.Add(offset)
		e := s.Add(time.Minute)
		return timedRunGraph(id, &s, &e)
	}
	runs := []aggregate.RunGraph{mk("r1", 0), mk("r10", 2*time.Hour), mk("r2", time.Hour)}
	got := orderRunsByTime(runs)
	assert.Equal(t, []string{"r1", "r2", "r10"}, runIDsOf(got))
	assert.Equal(t, []string{"r1", "r10", "r2"}, runIDsOf(runs))
}

func TestOrderRunsByTimeTieBreaksEndedAtThenID(t *testing.T) {
	t0 := time.Unix(10_000, 0).UTC()
	e1 := t0.Add(time.Minute)
	e2 := t0.Add(2 * time.Minute)
	runs := []aggregate.RunGraph{
		timedRunGraph("b", &t0, &e2),
		timedRunGraph("d", &t0, &e1),
		timedRunGraph("a", &t0, &e1),
		timedRunGraph("c", &t0, &e1),
		timedRunGraph("z", nil, nil),
	}
	got := orderRunsByTime(runs)
	assert.Equal(t, []string{"z", "a", "c", "d", "b"}, runIDsOf(got))
}

func TestOrderRunsByTimeAllUntimedKeepsIncomingOrder(t *testing.T) {
	runs := []aggregate.RunGraph{
		{Run: model.Run{ID: "b"}},
		{Run: model.Run{ID: "a"}},
	}
	got := orderRunsByTime(runs)
	assert.Equal(t, []string{"b", "a"}, runIDsOf(got))
}

func TestCalibrateOrdersRunsByWallClockNotRunID(t *testing.T) {
	root := t.TempDir()
	t0 := time.Unix(10_000, 0).UTC()
	for i, id := range []string{"aa-1", "aa-2", "aa-3", "aa-4", "aa-5", "aa-10"} {
		writeTimedEvidenceRun(t, root, id, "aa", nil, t0.Add(time.Duration(i)*time.Hour))
	}
	var out, errBuf bytes.Buffer
	code := run([]string{"calibrate", "--runs-dir", root, "--group", "label:variant=aa", "--format", "json"}, &out, &errBuf)
	require.Equal(t, 0, code, errBuf.String())
	var rep calibrate.CalibrateReport
	require.NoError(t, json.Unmarshal(out.Bytes(), &rep))
	assert.Equal(t, []string{"aa-1", "aa-2", "aa-3", "aa-4", "aa-5", "aa-10"}, rep.RunIDs)
}

func TestCalibrateMultiTaskGroupWarnsOnce(t *testing.T) {
	root := t.TempDir()
	t0 := time.Unix(10_000, 0).UTC()
	for i := range 6 {
		task := "alpha"
		if i >= 3 {
			task = "beta"
		}
		writeTimedEvidenceRun(t, root, fmt.Sprintf("mt-%d", i), "mt", map[string]string{"task": task}, t0.Add(time.Duration(i)*time.Hour))
	}
	var out, errBuf bytes.Buffer
	code := run([]string{"calibrate", "--runs-dir", root, "--group", "label:variant=mt"}, &out, &errBuf)
	assert.Equal(t, 0, code)
	assert.Equal(t,
		"warning: calibrate group spans 2 tasks; drift may reflect task composition — prefer a per-task selector (label:...,task=<id>)\n",
		errBuf.String())
	assert.Contains(t, out.String(), "self-check: sufficient · runs 6 · min-support 3\n")
}

func TestCalibrateSingleTaskGroupNoWarning(t *testing.T) {
	root := t.TempDir()
	t0 := time.Unix(10_000, 0).UTC()
	for i := range 6 {
		writeTimedEvidenceRun(t, root, fmt.Sprintf("st-%d", i), "st", map[string]string{"task": "alpha"}, t0.Add(time.Duration(i)*time.Hour))
	}
	var out, errBuf bytes.Buffer
	code := run([]string{"calibrate", "--runs-dir", root, "--group", "label:variant=st"}, &out, &errBuf)
	assert.Equal(t, 0, code)
	assert.Empty(t, errBuf.String())
}

func TestCalibrateFailOnNotableHelpNamesExitZero(t *testing.T) {
	flag := newCalibrateCmd().Flags().Lookup("fail-on-notable")
	require.NotNil(t, flag)
	assert.Contains(t, flag.Usage, "always exits 0")
	regressFlag := newRegressCmd().Flags().Lookup("fail-on-notable")
	require.NotNil(t, regressFlag)
	assert.NotContains(t, regressFlag.Usage, "always exits 0")
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
	assert.Contains(t, errBuf.String(), `calibrate selector "label:variant=none": selector matched no runs`)
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
