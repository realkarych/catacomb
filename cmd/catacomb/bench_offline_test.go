package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/bench"
	"github.com/realkarych/catacomb/evidence"
)

func stubBenchChild(t *testing.T, env ...string) {
	t.Helper()
	t.Setenv("GO_HELPER_BENCH", "1")
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		t.Setenv(k, v)
	}
	orig := execCommand
	execCommand = func(_ string, _ ...string) *exec.Cmd {
		return exec.Command(os.Args[0], "-test.run=TestHelperBenchChild")
	}
	t.Cleanup(func() { execCommand = orig })
}

func offlineCell(runID string, task bench.Task, variant bench.Variant) bench.Cell {
	return bench.Cell{
		Task:    task,
		Variant: variant,
		Rep:     1,
		RunID:   runID,
		Labels:  map[string]string{"basket": "b", "task": task.ID, "variant": variant.ID, "rep": "1"},
	}
}

func fixturePath(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("testdata", "session_marked.jsonl"))
	require.NoError(t, err)
	return p
}

func TestBenchOfflineEndToEnd(t *testing.T) {
	projects := t.TempDir()
	runs := t.TempDir()
	stubBenchChild(t, "HELPER_SESSION=sess-a", "HELPER_PROJECTS="+projects, "HELPER_FIXTURE="+fixturePath(t))

	basket := filepath.Join(t.TempDir(), "b.yaml")
	require.NoError(t, os.WriteFile(basket, []byte(
		"basket: bx\nreps: 1\ntasks:\n  - id: t1\n    cmd: [\"sess-a\"]\n    checkpoints:\n      - missing.cp\nvariants:\n  - id: base\n"), 0o600))
	manifestPath := filepath.Join(t.TempDir(), "m.jsonl")

	var out, errb bytes.Buffer
	err := runBench(context.Background(), &out, &errb, "", basket, benchFlags{
		offline: true, projectsDir: projects, runsDir: runs, manifest: manifestPath,
	})
	require.NoError(t, err)

	entries, err := bench.Manifest{Path: manifestPath}.Completed()
	require.NoError(t, err)
	require.Len(t, entries, 1)
	entry := entries["bench-bx-t1-base-r1"]
	require.Equal(t, []string{"missing.cp"}, entry.MissingCheckpoints)
	require.True(t, entry.Marked)
	require.NotNil(t, entry.CostUSD)
	require.NotEmpty(t, entry.EvidenceDir)
	require.Contains(t, errb.String(), "missing checkpoints: missing.cp")

	got, err := evidence.ScanRuns(runs)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "bench-bx-t1-base-r1", got[0].Meta.RunID)
	_, statErr := os.Stat(filepath.Join(got[0].Dir, "session.jsonl"))
	require.NoError(t, statErr)
}

func TestBenchOfflineCheckpointHitEndToEnd(t *testing.T) {
	projects := t.TempDir()
	runs := t.TempDir()
	stubBenchChild(t, "HELPER_SESSION=sess-cp", "HELPER_PROJECTS="+projects, "HELPER_FIXTURE="+fixturePath(t))

	basket := filepath.Join(t.TempDir(), "b.yaml")
	require.NoError(t, os.WriteFile(basket, []byte(
		"basket: bx\nreps: 1\ntasks:\n  - id: t1\n    cmd: [\"sess-cp\"]\n    checkpoints:\n      - plan\nvariants:\n  - id: base\n"), 0o600))
	manifestPath := filepath.Join(t.TempDir(), "m.jsonl")

	var out, errb bytes.Buffer
	err := runBench(context.Background(), &out, &errb, "", basket, benchFlags{
		offline: true, projectsDir: projects, runsDir: runs, manifest: manifestPath,
	})
	require.NoError(t, err)

	entries, err := bench.Manifest{Path: manifestPath}.Completed()
	require.NoError(t, err)
	require.Len(t, entries, 1)
	entry := entries["bench-bx-t1-base-r1"]
	assert.Empty(t, entry.MissingCheckpoints)
	assert.True(t, entry.Marked)
	assert.Contains(t, out.String(), "checkpoints[t1]: plan 1/1")
	assert.NotContains(t, errb.String(), "missing checkpoints")
}

func TestBenchOfflineNoSessionNote(t *testing.T) {
	stubBenchChild(t)
	cell := offlineCell("r1", bench.Task{ID: "t1", Cmd: []string{"claude"}}, bench.Variant{ID: "base"})
	entry, failed, verified := runBenchCellOffline(io.Discard, io.Discard, cell, "h", nil,
		offlineOpts{projectsDir: t.TempDir(), runsDir: t.TempDir()})
	assert.Equal(t, "no session id observed", entry.Note)
	assert.Empty(t, entry.SessionID)
	assert.False(t, failed)
	assert.False(t, verified)
}

func TestBenchOfflineResolveTimeoutNote(t *testing.T) {
	restore := sleepFn
	sleepFn = func(time.Duration) {}
	t.Cleanup(func() { sleepFn = restore })
	stubBenchChild(t, "HELPER_SESSION=ghost")
	cell := offlineCell("r1", bench.Task{ID: "t1", Cmd: []string{"claude"}}, bench.Variant{ID: "base"})
	entry, _, verified := runBenchCellOffline(io.Discard, io.Discard, cell, "h", nil,
		offlineOpts{projectsDir: t.TempDir(), runsDir: t.TempDir()})
	assert.Contains(t, entry.Note, "transcripts not found")
	assert.False(t, verified)
	assert.Empty(t, entry.EvidenceDir)
}

func TestBenchOfflineGraphLoadFailureNote(t *testing.T) {
	projects := t.TempDir()
	stubBenchChild(t, "HELPER_SESSION=bad", "HELPER_PROJECTS="+projects, "HELPER_BODY={\"type\":")
	cell := offlineCell("r1", bench.Task{ID: "t1", Cmd: []string{"claude"}}, bench.Variant{ID: "base"})
	entry, _, verified := runBenchCellOffline(io.Discard, io.Discard, cell, "h", nil,
		offlineOpts{projectsDir: projects, runsDir: t.TempDir()})
	assert.Contains(t, entry.Note, "graph:")
	assert.False(t, verified)
	assert.Empty(t, entry.EvidenceDir)
}

func TestBenchOfflineEvidenceWriteFailureNote(t *testing.T) {
	projects := t.TempDir()
	stubBenchChild(t, "HELPER_SESSION=ok1", "HELPER_PROJECTS="+projects, "HELPER_FIXTURE="+fixturePath(t))
	runsFile := filepath.Join(t.TempDir(), "runs-is-a-file")
	require.NoError(t, os.WriteFile(runsFile, []byte("x"), 0o600))
	cell := offlineCell("r1", bench.Task{ID: "t1", Cmd: []string{"claude"}}, bench.Variant{ID: "base"})
	entry, _, _ := runBenchCellOffline(io.Discard, io.Discard, cell, "h", nil,
		offlineOpts{projectsDir: projects, runsDir: runsFile})
	assert.Contains(t, entry.Note, "evidence write")
	assert.Empty(t, entry.EvidenceDir)
}

func TestBenchOfflineSetupFailureNote(t *testing.T) {
	stubBenchChild(t, "HELPER_EXIT1=1")
	cell := offlineCell("r1", bench.Task{ID: "t1", Cmd: []string{"claude"}}, bench.Variant{ID: "base", Setup: []string{"boom"}})
	entry, failed, verified := runBenchCellOffline(io.Discard, io.Discard, cell, "h", nil,
		offlineOpts{projectsDir: t.TempDir(), runsDir: t.TempDir()})
	assert.Equal(t, "setup failed", entry.Note)
	assert.Equal(t, 1, entry.ExitCode)
	assert.True(t, failed)
	assert.False(t, verified)
}

func TestBenchOfflineSpawnFailureNote(t *testing.T) {
	orig := execCommand
	execCommand = func(_ string, _ ...string) *exec.Cmd {
		return exec.Command(filepath.Join(t.TempDir(), "does-not-exist-binary"))
	}
	t.Cleanup(func() { execCommand = orig })
	cell := offlineCell("r1", bench.Task{ID: "t1", Cmd: []string{"nope"}}, bench.Variant{ID: "base"})
	var errb bytes.Buffer
	entry, failed, verified := runBenchCellOffline(io.Discard, &errb, cell, "h", nil,
		offlineOpts{projectsDir: t.TempDir(), runsDir: t.TempDir()})
	assert.Contains(t, entry.Note, "spawn failed")
	assert.True(t, failed)
	assert.False(t, verified)
	assert.Contains(t, errb.String(), "bench r1: spawn failed")
}

func TestBenchOfflineNoCheckpointsWritesEvidence(t *testing.T) {
	projects := t.TempDir()
	runs := t.TempDir()
	stubBenchChild(t, "HELPER_SESSION=ok2", "HELPER_PROJECTS="+projects, "HELPER_FIXTURE="+fixturePath(t))
	cell := offlineCell("bench-b-t1-base-r1", bench.Task{ID: "t1", Cmd: []string{"claude"}}, bench.Variant{ID: "base"})
	entry, failed, verified := runBenchCellOffline(io.Discard, io.Discard, cell, "hash-x",
		map[string]string{"env": "ci"}, offlineOpts{projectsDir: projects, runsDir: runs})
	assert.False(t, failed)
	assert.False(t, verified)
	assert.True(t, entry.Marked)
	assert.Empty(t, entry.MissingCheckpoints)
	require.NotEmpty(t, entry.EvidenceDir)
	require.NotNil(t, entry.CostUSD)

	meta, err := evidence.ReadMeta(entry.EvidenceDir)
	require.NoError(t, err)
	assert.Equal(t, "task:t1", meta.MarkerName)
	assert.Equal(t, "hash-x", meta.BasketHash)
	assert.Equal(t, "ci", meta.Labels["env"])
	assert.Equal(t, "base", meta.Labels["variant"])
	require.NotNil(t, meta.CostUSD)
}

func TestBenchOfflineWritesSubagentEvidence(t *testing.T) {
	projects := t.TempDir()
	runs := t.TempDir()
	subDir := filepath.Join(projects, "-tmp-proj", "subs1", "subagents")
	require.NoError(t, os.MkdirAll(subDir, 0o700))
	data, err := os.ReadFile(fixturePath(t))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(subDir, "agent-1.jsonl"), data, 0o600))

	stubBenchChild(t, "HELPER_SESSION=subs1", "HELPER_PROJECTS="+projects, "HELPER_FIXTURE="+fixturePath(t))
	cell := offlineCell("bench-b-t1-base-r1", bench.Task{ID: "t1", Cmd: []string{"claude"}}, bench.Variant{ID: "base"})
	entry, _, _ := runBenchCellOffline(io.Discard, io.Discard, cell, "h", nil,
		offlineOpts{projectsDir: projects, runsDir: runs})
	require.NotEmpty(t, entry.EvidenceDir)
	_, err = os.Stat(filepath.Join(entry.EvidenceDir, "session.jsonl"))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(entry.EvidenceDir, "subagents", "agent-1.jsonl"))
	require.NoError(t, err)
}

func TestBenchOfflineMissingDirsIsOperational(t *testing.T) {
	basket := writeBasket(t, "basket: b\nreps: 1\ntasks:\n  - id: t1\n    cmd: [\"x\"]\nvariants:\n  - id: v1\n")
	var out, errb bytes.Buffer
	err := runBench(context.Background(), &out, &errb, "", basket,
		benchFlags{offline: true, projectsDir: "", runsDir: t.TempDir()})
	require.ErrorIs(t, err, errBenchOfflineDirs)
	var opErr *operationalError
	require.ErrorAs(t, err, &opErr)
}

func TestBenchDefaultDir(t *testing.T) {
	assert.Empty(t, benchDefaultDir(""))
	assert.Empty(t, benchDefaultDir("", "a", "b"))
	assert.Equal(t, filepath.Join("/home", "a", "b"), benchDefaultDir("/home", "a", "b"))
}

func TestPrintOfflineEpilogue(t *testing.T) {
	var single bytes.Buffer
	printOfflineEpilogue(&single, bench.Basket{Name: "b", Reps: 1, Variants: []bench.Variant{{ID: "v1"}}}, "/runs")
	assert.Contains(t, single.String(), "Next steps:")
	assert.NotContains(t, single.String(), "catacomb regress")
	assert.Contains(t, single.String(), "reps=1 limits")

	var multi bytes.Buffer
	printOfflineEpilogue(&multi, bench.Basket{Name: "b", Reps: 5, Variants: []bench.Variant{{ID: "v1"}, {ID: "v2"}}}, "/runs")
	assert.Contains(t, multi.String(),
		"catacomb regress --runs-dir /runs --baseline label:basket=b,variant=v1 --candidate label:basket=b,variant=v2")
	assert.NotContains(t, multi.String(), "limits rate-gate")
}

func TestHelperBenchChild(t *testing.T) {
	t.Helper()
	if os.Getenv("GO_HELPER_BENCH") != "1" {
		return
	}
	if os.Getenv("HELPER_EXIT1") == "1" {
		os.Exit(1)
	}
	sid := os.Getenv("HELPER_SESSION")
	writeHelperTranscript(sid)
	if sid != "" {
		fmt.Printf("{\"type\":\"system\",\"session_id\":%q}\n", sid)
	}
	fmt.Println(`{"type":"result","total_cost_usd":0.01}`)
	os.Exit(0)
}

func writeHelperTranscript(sid string) {
	proj := os.Getenv("HELPER_PROJECTS")
	if proj == "" {
		return
	}
	dir := filepath.Join(proj, "-tmp-proj")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		os.Exit(1)
	}
	body := []byte(os.Getenv("HELPER_BODY"))
	if fx := os.Getenv("HELPER_FIXTURE"); fx != "" {
		data, err := os.ReadFile(fx)
		if err != nil {
			os.Exit(1)
		}
		body = data
	}
	if err := os.WriteFile(filepath.Join(dir, sid+".jsonl"), body, 0o600); err != nil {
		os.Exit(1)
	}
}
