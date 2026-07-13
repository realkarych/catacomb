package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/bench"
	"github.com/realkarych/catacomb/evidence"
	"github.com/realkarych/catacomb/model"
)

func stubBenchChild(t *testing.T, env ...string) {
	t.Helper()
	t.Setenv("GO_HELPER_BENCH", "1")
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		t.Setenv(k, v)
	}
	orig := execCommandContext
	execCommandContext = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, os.Args[0], "-test.run=TestHelperBenchChild")
	}
	t.Cleanup(func() { execCommandContext = orig })
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

func TestOfflineEnvCarriesLabelsAndRunID(t *testing.T) {
	cell := offlineCell("run-42", bench.Task{ID: "t1", Env: map[string]string{"FOO": "bar"}}, bench.Variant{ID: "base"})
	env := offlineEnv(cell, map[string]string{"basket": "b", "variant": "base"})
	assert.Contains(t, env, "FOO=bar")
	assert.Contains(t, env, "CATACOMB_LABELS=basket=b,variant=base")
	assert.Contains(t, env, "CATACOMB_RUN_ID=run-42")
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
	err := runBench(t.Context(), &out, &errb, basket, benchFlags{
		projectsDir: projects, runsDir: runs, manifest: manifestPath,
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
	err := runBench(t.Context(), &out, &errb, basket, benchFlags{
		projectsDir: projects, runsDir: runs, manifest: manifestPath,
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

func TestBenchOfflineTimeoutDeadline(t *testing.T) {
	tests := []struct {
		name         string
		timeout      string
		wantDeadline bool
	}{
		{"timeout set", "5s", true},
		{"timeout unset", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("GO_HELPER_BENCH", "1")
			var hasDeadline bool
			orig := execCommandContext
			execCommandContext = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
				_, hasDeadline = ctx.Deadline()
				return exec.CommandContext(ctx, os.Args[0], "-test.run=TestHelperBenchChild")
			}
			t.Cleanup(func() { execCommandContext = orig })
			cell := offlineCell("r1", bench.Task{ID: "t1", Cmd: []string{"claude"}, Timeout: tt.timeout}, bench.Variant{ID: "base"})
			entry, failed, _ := runBenchCellOffline(t.Context(), io.Discard, io.Discard, cell, "h", nil,
				offlineOpts{projectsDir: t.TempDir(), runsDir: t.TempDir()})
			assert.Equal(t, tt.wantDeadline, hasDeadline)
			assert.False(t, failed)
			assert.Equal(t, "no session id observed", entry.Note)
		})
	}
}

func TestBenchOfflineSetupUnderTimeout(t *testing.T) {
	t.Setenv("GO_HELPER_BENCH", "1")
	var deadlines []bool
	orig := execCommandContext
	execCommandContext = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		_, has := ctx.Deadline()
		deadlines = append(deadlines, has)
		return exec.CommandContext(ctx, os.Args[0], "-test.run=TestHelperBenchChild")
	}
	t.Cleanup(func() { execCommandContext = orig })
	cell := offlineCell("r1", bench.Task{ID: "t1", Cmd: []string{"claude"}, Timeout: "5s"},
		bench.Variant{ID: "base", Setup: []string{"prep"}})
	entry, failed, _ := runBenchCellOffline(t.Context(), io.Discard, io.Discard, cell, "h", nil,
		offlineOpts{projectsDir: t.TempDir(), runsDir: t.TempDir()})
	assert.Equal(t, []bool{true, true}, deadlines)
	assert.False(t, failed)
	assert.Equal(t, "no session id observed", entry.Note)
}

func TestBenchOfflineSetupCancelledContext(t *testing.T) {
	stubBenchChild(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	cell := offlineCell("r1", bench.Task{ID: "t1", Cmd: []string{"claude"}},
		bench.Variant{ID: "base", Setup: []string{"prep"}})
	entry, failed, verified := runBenchCellOffline(ctx, io.Discard, io.Discard, cell, "h", nil,
		offlineOpts{projectsDir: t.TempDir(), runsDir: t.TempDir()})
	assert.Equal(t, "setup failed; cancelled", entry.Note)
	assert.Equal(t, -1, entry.ExitCode)
	assert.True(t, failed)
	assert.False(t, verified)
}

func TestBenchOfflineSetupTimedOutNote(t *testing.T) {
	stubBenchChild(t)
	ctx, cancel := context.WithTimeout(t.Context(), -time.Second)
	defer cancel()
	cell := offlineCell("r1", bench.Task{ID: "t1", Cmd: []string{"claude"}},
		bench.Variant{ID: "base", Setup: []string{"prep"}})
	entry, failed, verified := runBenchCellOffline(ctx, io.Discard, io.Discard, cell, "h", nil,
		offlineOpts{projectsDir: t.TempDir(), runsDir: t.TempDir()})
	assert.Equal(t, "setup failed; timed out", entry.Note)
	assert.True(t, failed)
	assert.False(t, verified)
}

func TestBenchOfflineTimeoutCancelledContext(t *testing.T) {
	stubBenchChild(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	cell := offlineCell("r1", bench.Task{ID: "t1", Cmd: []string{"claude"}, Timeout: "1ms"}, bench.Variant{ID: "base"})
	var errb bytes.Buffer
	entry, failed, verified := runBenchCellOffline(ctx, io.Discard, &errb, cell, "h", nil,
		offlineOpts{projectsDir: t.TempDir(), runsDir: t.TempDir()})
	assert.Contains(t, entry.Note, "cancelled")
	assert.Contains(t, entry.Note, "spawn failed: context canceled")
	assert.True(t, failed)
	assert.False(t, verified)
	assert.Contains(t, errb.String(), "context canceled")
}

func TestBenchOfflineTimedOutNote(t *testing.T) {
	stubBenchChild(t)
	ctx, cancel := context.WithTimeout(t.Context(), -time.Second)
	defer cancel()
	cell := offlineCell("r1", bench.Task{ID: "t1", Cmd: []string{"claude"}, Timeout: "5s"}, bench.Variant{ID: "base"})
	entry, failed, verified := runBenchCellOffline(ctx, io.Discard, io.Discard, cell, "h", nil,
		offlineOpts{projectsDir: t.TempDir(), runsDir: t.TempDir()})
	assert.Contains(t, entry.Note, "timed out")
	assert.True(t, failed)
	assert.False(t, verified)
}

func TestBenchOfflineNoSessionNote(t *testing.T) {
	stubBenchChild(t)
	cell := offlineCell("r1", bench.Task{ID: "t1", Cmd: []string{"claude"}}, bench.Variant{ID: "base"})
	entry, failed, verified := runBenchCellOffline(t.Context(), io.Discard, io.Discard, cell, "h", nil,
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
	entry, _, verified := runBenchCellOffline(t.Context(), io.Discard, io.Discard, cell, "h", nil,
		offlineOpts{projectsDir: t.TempDir(), runsDir: t.TempDir()})
	assert.Contains(t, entry.Note, "transcripts not found")
	assert.False(t, verified)
	assert.Empty(t, entry.EvidenceDir)
}

func TestBenchOfflineGraphLoadFailureNote(t *testing.T) {
	projects := t.TempDir()
	stubBenchChild(t, "HELPER_SESSION=bad", "HELPER_PROJECTS="+projects, "HELPER_BODY={\"type\":")
	cell := offlineCell("r1", bench.Task{ID: "t1", Cmd: []string{"claude"}}, bench.Variant{ID: "base"})
	entry, _, verified := runBenchCellOffline(t.Context(), io.Discard, io.Discard, cell, "h", nil,
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
	entry, _, _ := runBenchCellOffline(t.Context(), io.Discard, io.Discard, cell, "h", nil,
		offlineOpts{projectsDir: projects, runsDir: runsFile})
	assert.Contains(t, entry.Note, "evidence write")
	assert.Empty(t, entry.EvidenceDir)
}

func TestBenchOfflineSetupFailureNote(t *testing.T) {
	stubBenchChild(t, "HELPER_EXIT1=1")
	cell := offlineCell("r1", bench.Task{ID: "t1", Cmd: []string{"claude"}}, bench.Variant{ID: "base", Setup: []string{"boom"}})
	entry, failed, verified := runBenchCellOffline(t.Context(), io.Discard, io.Discard, cell, "h", nil,
		offlineOpts{projectsDir: t.TempDir(), runsDir: t.TempDir()})
	assert.Equal(t, "setup failed", entry.Note)
	assert.Equal(t, 1, entry.ExitCode)
	assert.True(t, failed)
	assert.False(t, verified)
}

func TestBenchOfflineSpawnFailureNote(t *testing.T) {
	orig := execCommandContext
	execCommandContext = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, filepath.Join(t.TempDir(), "does-not-exist-binary"))
	}
	t.Cleanup(func() { execCommandContext = orig })
	cell := offlineCell("r1", bench.Task{ID: "t1", Cmd: []string{"nope"}}, bench.Variant{ID: "base"})
	var errb bytes.Buffer
	entry, failed, verified := runBenchCellOffline(t.Context(), io.Discard, &errb, cell, "h", nil,
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
	entry, failed, verified := runBenchCellOffline(t.Context(), io.Discard, io.Discard, cell, "hash-x",
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

func TestBenchEnvStamps(t *testing.T) {
	tests := []struct {
		name      string
		runs      []model.Run
		wantModel string
		wantCCV   string
	}{
		{
			name: "model and repro from matching run",
			runs: []model.Run{
				{ID: "other", ModelID: "skip-me", Repro: &model.ReproMeta{ClaudeCodeVersion: "9.9.9"}},
				{ID: "sess", ModelID: "m-1", Repro: &model.ReproMeta{ClaudeCodeVersion: "2.1.50"}},
			},
			wantModel: "m-1",
			wantCCV:   "2.1.50",
		},
		{
			name:      "matching run without repro",
			runs:      []model.Run{{ID: "sess", ModelID: "m-2"}},
			wantModel: "m-2",
		},
		{
			name: "no matching run",
			runs: []model.Run{{ID: "other", ModelID: "m-3"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := benchEnvStamps(tt.runs, "sess", nil)
			require.NotNil(t, env)
			assert.Equal(t, Version, env.CatacombVersion)
			assert.Equal(t, runtime.GOOS, env.Resources.OS)
			assert.Equal(t, runtime.GOARCH, env.Resources.Arch)
			assert.Equal(t, runtime.NumCPU(), env.Resources.CPUs)
			assert.Equal(t, tt.wantModel, env.ModelID)
			assert.Equal(t, tt.wantCCV, env.ClaudeCodeVersion)
		})
	}
}

func TestBenchOfflineEnvStampsInMeta(t *testing.T) {
	tests := []struct {
		name      string
		session   string
		fixture   bool
		body      string
		wantModel string
		wantCCV   string
	}{
		{
			name:      "transcript with model and claude code version",
			session:   "envsess",
			fixture:   true,
			wantModel: "claude-opus-4-8",
			wantCCV:   "2.1.100",
		},
		{
			name:    "transcript without model or claude code version",
			session: "noenvsess",
			body:    `{"type":"user","uuid":"u1","sessionId":"noenvsess","timestamp":"2026-06-20T10:00:00Z","message":{"role":"user","content":"go"}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projects := t.TempDir()
			runs := t.TempDir()
			helperEnv := []string{"HELPER_SESSION=" + tt.session, "HELPER_PROJECTS=" + projects}
			if tt.fixture {
				fx, err := filepath.Abs(filepath.Join("testdata", "session_envstamps.jsonl"))
				require.NoError(t, err)
				helperEnv = append(helperEnv, "HELPER_FIXTURE="+fx)
			} else {
				helperEnv = append(helperEnv, "HELPER_BODY="+tt.body)
			}
			stubBenchChild(t, helperEnv...)
			cell := offlineCell("bench-b-t1-base-r1", bench.Task{ID: "t1", Cmd: []string{"claude"}}, bench.Variant{ID: "base"})
			entry, failed, _ := runBenchCellOffline(t.Context(), io.Discard, io.Discard, cell, "h", nil,
				offlineOpts{projectsDir: projects, runsDir: runs})
			require.False(t, failed)
			require.NotEmpty(t, entry.EvidenceDir)

			meta, err := evidence.ReadMeta(entry.EvidenceDir)
			require.NoError(t, err)
			require.NotNil(t, meta.Env)
			assert.Equal(t, Version, meta.Env.CatacombVersion)
			assert.Equal(t, runtime.GOOS, meta.Env.Resources.OS)
			assert.Equal(t, runtime.GOARCH, meta.Env.Resources.Arch)
			assert.GreaterOrEqual(t, meta.Env.Resources.CPUs, 1)
			assert.Equal(t, tt.wantModel, meta.Env.ModelID)
			assert.Equal(t, tt.wantCCV, meta.Env.ClaudeCodeVersion)

			data, err := os.ReadFile(filepath.Join(entry.EvidenceDir, "meta.json"))
			require.NoError(t, err)
			var raw struct {
				Env map[string]any `json:"env"`
			}
			require.NoError(t, json.Unmarshal(data, &raw))
			require.NotNil(t, raw.Env)
			_, hasModel := raw.Env["model_id"]
			assert.Equal(t, tt.wantModel != "", hasModel)
			_, hasCCV := raw.Env["claude_code_version"]
			assert.Equal(t, tt.wantCCV != "", hasCCV)
		})
	}
}

func TestBenchOfflineVariantEnvAndEmptySetup(t *testing.T) {
	projects := t.TempDir()
	runs := t.TempDir()
	stubBenchChild(t, "HELPER_SESSION=ve1", "HELPER_PROJECTS="+projects, "HELPER_FIXTURE="+fixturePath(t))
	cell := offlineCell("bench-b-t1-base-r1",
		bench.Task{ID: "t1", Cmd: []string{"claude"}, Env: map[string]string{"TASKENV": "a"}},
		bench.Variant{ID: "base", Env: map[string]string{"VARENV": "b"}, Setup: []string{""}})
	entry, failed, _ := runBenchCellOffline(t.Context(), io.Discard, io.Discard, cell, "h", nil,
		offlineOpts{projectsDir: projects, runsDir: runs})
	assert.False(t, failed)
	assert.True(t, entry.Marked)
	require.NotEmpty(t, entry.EvidenceDir)
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
	entry, _, _ := runBenchCellOffline(t.Context(), io.Discard, io.Discard, cell, "h", nil,
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
	err := runBench(t.Context(), &out, &errb, basket,
		benchFlags{projectsDir: "", runsDir: t.TempDir()})
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

func stubBenchAndVerify(t *testing.T, env ...string) {
	t.Helper()
	t.Setenv("GO_HELPER_BENCH", "1")
	t.Setenv("GO_HELPER_VERIFY", "1")
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		t.Setenv(k, v)
	}
	orig := execCommandContext
	execCommandContext = func(ctx context.Context, name string, _ ...string) *exec.Cmd {
		run := "TestHelperBenchChild"
		if name == "verify" {
			run = "TestHelperVerify"
		}
		return exec.CommandContext(ctx, os.Args[0], "-test.run="+run)
	}
	t.Cleanup(func() { execCommandContext = orig })
}

func TestBenchOfflineWithArtifactsAndVerify(t *testing.T) {
	projects := t.TempDir()
	runs := t.TempDir()
	work := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(work, "out"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(work, "out", "result.csv"), []byte("a,b\n1,2\n"), 0o600))
	stubBenchAndVerify(t, "HELPER_SESSION=vsess", "HELPER_PROJECTS="+projects, "HELPER_FIXTURE="+fixturePath(t))

	basket := filepath.Join(t.TempDir(), "b.yaml")
	yaml := fmt.Sprintf("basket: bx\nreps: 1\ntasks:\n  - id: t1\n    cmd: [\"agent\"]\n    dir: %q\n    artifacts:\n      - \"out/*.csv\"\n    verify:\n      cmd: [\"verify\"]\nvariants:\n  - id: base\n  - id: bad\n", work)
	require.NoError(t, os.WriteFile(basket, []byte(yaml), 0o600))
	manifestPath := filepath.Join(t.TempDir(), "m.jsonl")

	var out, errb bytes.Buffer
	require.NoError(t, runBench(t.Context(), &out, &errb, basket, benchFlags{projectsDir: projects, runsDir: runs, manifest: manifestPath}))

	entries, err := bench.Manifest{Path: manifestPath}.Completed()
	require.NoError(t, err)
	base := entries["bench-bx-t1-base-r1"]
	bad := entries["bench-bx-t1-bad-r1"]

	assert.True(t, base.Verified)
	assert.Empty(t, base.VerifyError)
	_, statErr := os.Stat(filepath.Join(base.EvidenceDir, "artifacts", "out", "result.csv"))
	require.NoError(t, statErr)
	_, statErr = os.Stat(filepath.Join(base.EvidenceDir, "scores.jsonl"))
	require.NoError(t, statErr)
	rec, ok, verr := evidence.ReadVerify(base.EvidenceDir)
	require.NoError(t, verr)
	require.True(t, ok)
	assert.Equal(t, "bench", rec.Mode)
	meta, err := evidence.ReadMeta(base.EvidenceDir)
	require.NoError(t, err)
	require.Len(t, meta.Artifacts, 1)
	assert.Equal(t, filepath.Join("out", "result.csv"), meta.Artifacts[0].Rel)

	assert.False(t, bad.Verified)
	assert.NotEmpty(t, bad.VerifyError)
	assert.Contains(t, errb.String(), "bench bench-bx-t1-bad-r1: verify failed:")
	assert.Contains(t, out.String(), "verify[t1]: pass 1/1")
}

func TestCaptureArtifactsOfflineCaptureError(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, evidence.Write(dir, evidence.Meta{RunID: "r", Task: "t", MarkerName: "task:t"}, nil))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "artifacts"), []byte("x"), 0o600))
	work := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(work, "f.txt"), []byte("hi\n"), 0o600))
	entry := &bench.ManifestEntry{}
	cell := offlineCell("r", bench.Task{ID: "t", Dir: work, Artifacts: []string{"f.txt"}}, bench.Variant{ID: "base"})
	var errb bytes.Buffer
	captureArtifactsOffline(&errb, cell, dir, work, entry)
	assert.Contains(t, entry.Note, "artifacts:")
	assert.Contains(t, errb.String(), "bench r: artifacts:")
}

func TestCaptureArtifactsOfflineStampError(t *testing.T) {
	dir := t.TempDir()
	work := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(work, "f.txt"), []byte("hi\n"), 0o600))
	entry := &bench.ManifestEntry{}
	cell := offlineCell("r", bench.Task{ID: "t", Dir: work, Artifacts: []string{"f.txt"}}, bench.Variant{ID: "base"})
	var errb bytes.Buffer
	captureArtifactsOffline(&errb, cell, dir, work, entry)
	assert.Contains(t, entry.Note, "artifacts stamp:")
	assert.Contains(t, errb.String(), "bench r: artifacts stamp:")
}

func TestVerifierPassed(t *testing.T) {
	pass := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(pass, "scores.jsonl"),
		[]byte(`{"key":"verifier.pass","value":1,"run_id":"r"}`+"\n"), 0o600))
	assert.True(t, verifierPassed(pass, "r"))

	fail := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(fail, "scores.jsonl"),
		[]byte(`{"key":"verifier.pass","value":0,"run_id":"r"}`+"\n"), 0o600))
	assert.False(t, verifierPassed(fail, "r"))

	assert.False(t, verifierPassed(t.TempDir(), "r"))

	bad := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(bad, "scores.jsonl"), 0o700))
	assert.False(t, verifierPassed(bad, "r"))
}

func stubBenchExecRouted(t *testing.T, childCmd string, exits map[string]int, env ...string) *execCapture {
	t.Helper()
	t.Setenv("GO_HELPER_BENCH", "1")
	t.Setenv("GO_HELPER_WS", "1")
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		t.Setenv(k, v)
	}
	cap := &execCapture{}
	orig := execCommandContext
	execCommandContext = func(ctx context.Context, name string, _ ...string) *exec.Cmd {
		var c *exec.Cmd
		if name == childCmd {
			c = exec.CommandContext(ctx, os.Args[0], "-test.run=TestHelperBenchChild")
		} else {
			c = exec.CommandContext(ctx, os.Args[0], "-test.run=TestHelperWorkspaceExit", "--", strconv.Itoa(exits[name]))
		}
		cap.names = append(cap.names, name)
		cap.cmds = append(cap.cmds, c)
		return c
	}
	t.Cleanup(func() { execCommandContext = orig })
	return cap
}

func TestBenchWorkspaceCellOrderingAndWorkdirThreading(t *testing.T) {
	projects, runs, base := t.TempDir(), t.TempDir(), t.TempDir()
	cap := stubBenchExecRouted(t, "sess-a", nil,
		"HELPER_SESSION=sess-a", "HELPER_PROJECTS="+projects, "HELPER_FIXTURE="+fixturePath(t))
	cell := offlineCell("bench-b-t1-v-r1",
		bench.Task{ID: "t1", Cmd: []string{"sess-a"}, Workspace: &bench.Workspace{Cmd: []string{"ws-cmd"}, Teardown: []string{"td-cmd"}}},
		bench.Variant{ID: "v", Setup: []string{"setup-cmd"}})
	o := offlineOpts{projectsDir: projects, runsDir: runs, pricer: newPricer(), workspace: workspaceOpts{baseDir: base}}
	entry, failed, _ := runBenchCellOffline(t.Context(), io.Discard, io.Discard, cell, "h", nil, o)
	require.False(t, failed)
	require.Equal(t, []string{"ws-cmd", "setup-cmd", "sess-a", "td-cmd"}, cap.names)
	wsDir := cap.cmds[0].Dir
	require.Equal(t, base, filepath.Dir(wsDir))
	require.Equal(t, wsDir, cap.cmds[1].Dir)
	require.Equal(t, wsDir, cap.cmds[2].Dir)
	require.Equal(t, wsDir, cap.cmds[3].Dir)
	require.NoDirExists(t, wsDir)
	require.NotEmpty(t, entry.EvidenceDir)
	require.NotEmpty(t, cap.cmds[2].Env)
	for _, kv := range cap.cmds[2].Env {
		require.False(t, strings.HasPrefix(kv, "CATACOMB_PATCH="))
	}
}

func TestBenchWorkspaceStampsInMeta(t *testing.T) {
	projects, runs, base := t.TempDir(), t.TempDir(), t.TempDir()
	stubBenchExecRouted(t, "sess-a", nil,
		"HELPER_SESSION=sess-a", "HELPER_PROJECTS="+projects, "HELPER_FIXTURE="+fixturePath(t))
	ws := &bench.Workspace{Cmd: []string{"ws-cmd"}, Rev: "r42", PatchSHA256: "ab34"}
	cell := offlineCell("bench-b-t1-v-r1", bench.Task{ID: "t1", Cmd: []string{"sess-a"}, Workspace: ws}, bench.Variant{ID: "v"})
	o := offlineOpts{projectsDir: projects, runsDir: runs, pricer: newPricer(), workspace: workspaceOpts{baseDir: base}}
	entry, failed, _ := runBenchCellOffline(t.Context(), io.Discard, io.Discard, cell, "h", nil, o)
	require.False(t, failed)
	got, err := evidence.ScanRuns(runs)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.NotNil(t, got[0].Meta.Env)
	require.NotNil(t, got[0].Meta.Env.Workspace)
	require.Equal(t, "r42", got[0].Meta.Env.Workspace.Rev)
	require.Equal(t, "ab34", got[0].Meta.Env.Workspace.PatchSHA256)
	require.NotEmpty(t, entry.EvidenceDir)
}

func TestBenchWorkspaceFailureNoteAndNoEvidence(t *testing.T) {
	projects, runs, base := t.TempDir(), t.TempDir(), t.TempDir()
	cap := stubBenchExecRouted(t, "sess-a", map[string]int{"ws-cmd": 3},
		"HELPER_SESSION=sess-a", "HELPER_PROJECTS="+projects, "HELPER_FIXTURE="+fixturePath(t))
	cell := offlineCell("bench-b-t1-v-r1",
		bench.Task{ID: "t1", Cmd: []string{"sess-a"}, Workspace: &bench.Workspace{Cmd: []string{"ws-cmd"}, Teardown: []string{"td-cmd"}}},
		bench.Variant{ID: "v"})
	o := offlineOpts{projectsDir: projects, runsDir: runs, pricer: newPricer(), workspace: workspaceOpts{baseDir: base}}
	entry, failed, verified := runBenchCellOffline(t.Context(), io.Discard, io.Discard, cell, "h", nil, o)
	require.True(t, failed)
	require.False(t, verified)
	require.Equal(t, 3, entry.ExitCode)
	require.Contains(t, entry.Note, "workspace failed")
	require.Empty(t, entry.EvidenceDir)
	require.Equal(t, []string{"ws-cmd", "td-cmd"}, cap.names)
	require.False(t, entry.FinishedAt.IsZero())
}

func TestBenchWorkspaceSetupCancelledNote(t *testing.T) {
	base := t.TempDir()
	cap := stubBenchExecRouted(t, "sess-a", nil)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	cell := offlineCell("bench-b-t1-v-r1",
		bench.Task{ID: "t1", Cmd: []string{"sess-a"}, Workspace: &bench.Workspace{Cmd: []string{"ws-cmd"}, Teardown: []string{"td-cmd"}}},
		bench.Variant{ID: "v"})
	o := offlineOpts{projectsDir: t.TempDir(), runsDir: t.TempDir(), pricer: newPricer(), workspace: workspaceOpts{baseDir: base}}
	entry, failed, verified := runBenchCellOffline(ctx, io.Discard, io.Discard, cell, "h", nil, o)
	require.True(t, failed)
	require.False(t, verified)
	require.Equal(t, "workspace failed; cancelled", entry.Note)
	require.Empty(t, entry.EvidenceDir)
	require.Contains(t, cap.names, "td-cmd")
}

func TestBenchWorkspaceTeardownFailureNoteMerged(t *testing.T) {
	projects, runs, base := t.TempDir(), t.TempDir(), t.TempDir()
	stubBenchExecRouted(t, "sess-a", map[string]int{"td-cmd": 5},
		"HELPER_SESSION=sess-a", "HELPER_PROJECTS="+projects, "HELPER_FIXTURE="+fixturePath(t))
	cell := offlineCell("bench-b-t1-v-r1",
		bench.Task{ID: "t1", Cmd: []string{"sess-a"}, Workspace: &bench.Workspace{Cmd: []string{"ws-cmd"}, Teardown: []string{"td-cmd"}}},
		bench.Variant{ID: "v"})
	o := offlineOpts{projectsDir: projects, runsDir: runs, pricer: newPricer(), workspace: workspaceOpts{baseDir: base}}
	entry, failed, _ := runBenchCellOffline(t.Context(), io.Discard, io.Discard, cell, "h", nil, o)
	require.False(t, failed)
	require.Contains(t, entry.Note, "workspace teardown")
	require.NotEmpty(t, entry.EvidenceDir)
}

func TestBenchWorkspaceKeepFlag(t *testing.T) {
	projects, runs, base := t.TempDir(), t.TempDir(), t.TempDir()
	cap := stubBenchExecRouted(t, "sess-a", nil,
		"HELPER_SESSION=sess-a", "HELPER_PROJECTS="+projects, "HELPER_FIXTURE="+fixturePath(t))
	cell := offlineCell("bench-b-t1-v-r1", bench.Task{ID: "t1", Cmd: []string{"sess-a"}, Workspace: &bench.Workspace{Cmd: []string{"ws-cmd"}}}, bench.Variant{ID: "v"})
	o := offlineOpts{projectsDir: projects, runsDir: runs, pricer: newPricer(), workspace: workspaceOpts{baseDir: base, keep: true}}
	var errb bytes.Buffer
	_, failed, _ := runBenchCellOffline(t.Context(), io.Discard, &errb, cell, "h", nil, o)
	require.False(t, failed)
	require.DirExists(t, cap.cmds[0].Dir)
	require.Contains(t, errb.String(), "workspace kept: "+cap.cmds[0].Dir)
}

func TestBenchWorkspaceDormancy(t *testing.T) {
	projects, runs := t.TempDir(), t.TempDir()
	cap := stubBenchExecRouted(t, "sess-a", nil,
		"HELPER_SESSION=sess-a", "HELPER_PROJECTS="+projects, "HELPER_FIXTURE="+fixturePath(t))
	cell := offlineCell("bench-b-t1-v-r1", bench.Task{ID: "t1", Cmd: []string{"sess-a"}}, bench.Variant{ID: "v"})
	o := offlineOpts{projectsDir: projects, runsDir: runs, pricer: newPricer()}
	entry, failed, _ := runBenchCellOffline(t.Context(), io.Discard, io.Discard, cell, "h", nil, o)
	require.False(t, failed)
	require.Equal(t, []string{"sess-a"}, cap.names)
	got, err := evidence.ScanRuns(runs)
	require.NoError(t, err)
	require.Nil(t, got[0].Meta.Env.Workspace)
	require.NotEmpty(t, entry.EvidenceDir)
}

func TestBenchWorkspaceFailFastAtRunLevel(t *testing.T) {
	projects, runs, base := t.TempDir(), t.TempDir(), t.TempDir()
	stubBenchExecRouted(t, "sess-ff", map[string]int{"ws-cmd": 7},
		"HELPER_SESSION=sess-ff", "HELPER_PROJECTS="+projects, "HELPER_FIXTURE="+fixturePath(t))
	basket := writeBasket(t, "basket: bws\nreps: 2\ntasks:\n  - id: t1\n    cmd: [\"sess-ff\"]\n    workspace:\n      cmd: [\"ws-cmd\"]\nvariants:\n  - id: base\n")
	manifest := filepath.Join(t.TempDir(), "m.jsonl")
	err := runBench(t.Context(), io.Discard, io.Discard, basket, benchFlags{
		projectsDir: projects, runsDir: runs, manifest: manifest,
		failFast: true, workspacesDir: base,
	})
	require.ErrorIs(t, err, errBenchFailFast)
	entries, merr := bench.Manifest{Path: manifest}.Completed()
	require.NoError(t, merr)
	require.Len(t, entries, 1)
	entry := entries["bench-bws-t1-base-r1"]
	require.Contains(t, entry.Note, "workspace failed")
	require.Equal(t, 7, entry.ExitCode)
}

func TestVerifyStatsAndSummary(t *testing.T) {
	s := newVerifyStats()
	tk := bench.Task{ID: "t1", Verify: &bench.Verify{Cmd: []string{"v"}}}
	s.record(tk, true)
	s.record(tk, false)
	var b bytes.Buffer
	printVerifySummary(&b, bench.Basket{Tasks: []bench.Task{tk, {ID: "t2"}}}, s)
	assert.Contains(t, b.String(), "verify[t1]: pass 1/2")
	assert.NotContains(t, b.String(), "t2")

	var none bytes.Buffer
	printVerifySummary(&none, bench.Basket{Tasks: []bench.Task{{ID: "t2"}}}, newVerifyStats())
	assert.Empty(t, none.String())
}
