package main

import (
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

func stubVerify(t *testing.T, env ...string) {
	t.Helper()
	t.Setenv("GO_HELPER_VERIFY", "1")
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		t.Setenv(k, v)
	}
	orig := execCommandContext
	execCommandContext = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, os.Args[0], "-test.run=TestHelperVerify")
	}
	t.Cleanup(func() { execCommandContext = orig })
}

func benchSpec(dir string) verifySpec {
	return verifySpec{
		EvidenceDir: dir, Workdir: dir, RunID: "r1", Basket: "bk",
		Task: "t1", Variant: "base", Rep: 2, AgentExit: 7, Mode: "bench",
	}
}

func offlineSpec(dir string) verifySpec {
	s := benchSpec(dir)
	s.Mode = "offline"
	return s
}

func TestRunVerifyCellSuccess(t *testing.T) {
	stubVerify(t)
	dir := t.TempDir()
	rec := runVerifyCell(t.Context(), io.Discard, bench.Verify{Cmd: []string{"verify"}}, benchSpec(dir))
	assert.Empty(t, rec.Error)
	assert.Equal(t, 0, rec.ExitCode)
	assert.Equal(t, "bench", rec.Mode)
	assert.NotEmpty(t, rec.SHA256)

	entries, err := loadEvidenceScores(dir, "r1")
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "verifier.pass", entries[0].Key)
	assert.InDelta(t, 1.0, entries[0].Value, 1e-9)
	assert.Equal(t, "r1", entries[0].RunID)

	got, ok, err := evidence.ReadVerify(dir)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Empty(t, got.Error)
}

func TestRunVerifyCellPreservesProvenance(t *testing.T) {
	stubVerify(t, "VERIFY_PROVENANCE=1")
	dir := t.TempDir()
	rec := runVerifyCell(t.Context(), io.Discard, bench.Verify{Cmd: []string{"verify"}}, benchSpec(dir))
	require.Empty(t, rec.Error)

	data, err := os.ReadFile(filepath.Join(dir, "scores.jsonl"))
	require.NoError(t, err)
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	require.Len(t, lines, 2)

	assert.JSONEq(t, `{"key":"judge.groundedness","value":0.8,"run_id":"explicit","tool":"deepeval","tool_version":"3.1","prompt_hash":"abc"}`, lines[0])
	assert.JSONEq(t, `{"key":"verifier.pass","value":1,"run_id":"r1","tool":"deepeval","tool_version":"3.1","prompt_hash":"abc"}`, lines[1])
}

func TestRunVerifyCellZeroTimeout(t *testing.T) {
	stubVerify(t)
	dir := t.TempDir()
	rec := runVerifyCell(t.Context(), io.Discard, bench.Verify{Cmd: []string{"verify"}, Timeout: "0s"}, benchSpec(dir))
	assert.Empty(t, rec.Error)
}

func TestRunVerifyCellFailingExit(t *testing.T) {
	stubVerify(t, "VERIFY_EXIT3=1")
	dir := t.TempDir()
	rec := runVerifyCell(t.Context(), io.Discard, bench.Verify{Cmd: []string{"verify"}}, offlineSpec(dir))
	assert.Equal(t, 3, rec.ExitCode)
	assert.Contains(t, rec.Error, "verifier failed")
	assert.Contains(t, rec.Error, "exit status 3")
	_, statErr := os.Stat(filepath.Join(dir, "scores.jsonl"))
	assert.ErrorIs(t, statErr, os.ErrNotExist)
	got, ok, err := evidence.ReadVerify(dir)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, 3, got.ExitCode)
	assert.NotEmpty(t, got.Error)
}

func TestRunVerifyCellInvalidStdout(t *testing.T) {
	stubVerify(t, "VERIFY_BADLINE=1")
	dir := t.TempDir()
	rec := runVerifyCell(t.Context(), io.Discard, bench.Verify{Cmd: []string{"verify"}}, offlineSpec(dir))
	assert.Contains(t, rec.Error, "line 1")
	_, statErr := os.Stat(filepath.Join(dir, "scores.jsonl"))
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestRunVerifyCellOverCap(t *testing.T) {
	stubVerify(t, "VERIFY_BIG=1")
	dir := t.TempDir()
	rec := runVerifyCell(t.Context(), io.Discard, bench.Verify{Cmd: []string{"verify"}}, offlineSpec(dir))
	assert.Contains(t, rec.Error, "exceeded")
	_, statErr := os.Stat(filepath.Join(dir, "scores.jsonl"))
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestRunVerifyCellTimeout(t *testing.T) {
	stubVerify(t, "VERIFY_BLOCK=1")
	dir := t.TempDir()
	rec := runVerifyCell(t.Context(), io.Discard, bench.Verify{Cmd: []string{"verify"}, Timeout: "50ms"}, offlineSpec(dir))
	assert.Equal(t, "timed out", rec.Error)
	_, statErr := os.Stat(filepath.Join(dir, "scores.jsonl"))
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestRunVerifyCellWriteFailures(t *testing.T) {
	stubVerify(t)
	gone := filepath.Join(t.TempDir(), "gone")
	spec := benchSpec(gone)
	spec.Workdir = t.TempDir()
	rec := runVerifyCell(t.Context(), io.Discard, bench.Verify{Cmd: []string{"verify"}}, spec)
	assert.Contains(t, rec.Error, "scores")
	assert.Contains(t, rec.Error, "verify.json")
}

func TestRunVerifyCellEnvContract(t *testing.T) {
	dir := t.TempDir()
	dump := filepath.Join(t.TempDir(), "env.txt")
	stubVerify(t, "VERIFY_ENV_OUT="+dump)
	spec := offlineSpec(dir)
	spec.ExtraEnv = []string{"CATACOMB_FROMVARIANT=vv"}
	rec := runVerifyCell(t.Context(), io.Discard, bench.Verify{Cmd: []string{"verify"}, Env: map[string]string{"CATACOMB_FROMVERIFY": "fv"}}, spec)
	require.Empty(t, rec.Error)

	data, err := os.ReadFile(dump)
	require.NoError(t, err)
	got := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		k, v, _ := strings.Cut(line, "=")
		got[k] = v
	}
	assert.Equal(t, dir, got["CATACOMB_EVIDENCE_DIR"])
	workdir, present := got["CATACOMB_WORKDIR"]
	assert.True(t, present)
	assert.Empty(t, workdir)
	assert.Equal(t, "r1", got["CATACOMB_RUN_ID"])
	assert.Equal(t, "bk", got["CATACOMB_BASKET"])
	assert.Equal(t, "t1", got["CATACOMB_TASK"])
	assert.Equal(t, "base", got["CATACOMB_VARIANT"])
	assert.Equal(t, "2", got["CATACOMB_REP"])
	assert.Equal(t, "7", got["CATACOMB_AGENT_EXIT_CODE"])
	assert.Equal(t, "vv", got["CATACOMB_FROMVARIANT"])
	assert.Equal(t, "fv", got["CATACOMB_FROMVERIFY"])
}

func TestParseVerifierScores(t *testing.T) {
	lines, err := parseVerifierScores([]byte("\n{\"key\":\"verifier.pass\",\"value\":1}\n{\"key\":\"a.b\",\"value\":2,\"run_id\":\"other\"}\n"), "fill-me")
	require.NoError(t, err)
	require.Len(t, lines, 2)
	assert.JSONEq(t, `{"key":"verifier.pass","value":1,"run_id":"fill-me"}`, string(lines[0]))
	assert.JSONEq(t, `{"key":"a.b","value":2,"run_id":"other"}`, string(lines[1]))

	_, err = parseVerifierScores([]byte("not-json\n"), "r")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "line 1")
}

func TestCanonicalEnv(t *testing.T) {
	assert.Equal(t, map[string]string{}, canonicalEnv(nil))
	m := map[string]string{"A": "1"}
	assert.Equal(t, m, canonicalEnv(m))
}

func TestCapWriter(t *testing.T) {
	w := &capWriter{limit: 4}
	n, err := w.Write([]byte("ab"))
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	assert.False(t, w.over)
	n, err = w.Write([]byte("cde"))
	require.NoError(t, err)
	assert.Equal(t, 3, n)
	assert.True(t, w.over)
	n, err = w.Write([]byte("fg"))
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	assert.Equal(t, "ab", w.buf.String())
}

func TestHelperVerify(t *testing.T) {
	t.Helper()
	if os.Getenv("GO_HELPER_VERIFY") != "1" {
		return
	}
	if out := os.Getenv("VERIFY_ENV_OUT"); out != "" {
		var b strings.Builder
		for _, e := range os.Environ() {
			if strings.HasPrefix(e, "CATACOMB_") {
				b.WriteString(e)
				b.WriteByte('\n')
			}
		}
		_ = os.WriteFile(out, []byte(b.String()), 0o600)
		os.Exit(0)
	}
	if os.Getenv("VERIFY_BLOCK") == "1" {
		<-time.After(10 * time.Second)
	}
	if os.Getenv("VERIFY_BIG") == "1" {
		fmt.Print(strings.Repeat("x", 2<<20))
		os.Exit(0)
	}
	if os.Getenv("VERIFY_BADLINE") == "1" {
		fmt.Println("not-json")
		os.Exit(0)
	}
	if os.Getenv("VERIFY_PROVENANCE") == "1" {
		fmt.Println(`{"key":"judge.groundedness","value":0.8,"run_id":"explicit","tool":"deepeval","tool_version":"3.1","prompt_hash":"abc"}`)
		fmt.Println(`{"key":"verifier.pass","value":1,"tool":"deepeval","tool_version":"3.1","prompt_hash":"abc"}`)
		os.Exit(0)
	}
	if os.Getenv("VERIFY_EXIT3") == "1" || os.Getenv("CATACOMB_VARIANT") == "bad" {
		os.Exit(3)
	}
	fmt.Printf("{\"key\":\"verifier.pass\",\"value\":1,\"run_id\":%q}\n", os.Getenv("CATACOMB_RUN_ID"))
	os.Exit(0)
}
