package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/bench"
	"github.com/realkarych/catacomb/evidence"
)

const verifyBasketYAML = "basket: bk\nreps: 1\ntasks:\n  - id: t1\n    cmd: [\"agent\"]\n    env: { CATACOMB_FROMTASK: ft }\n    verify:\n      cmd: [\"verify\"]\n  - id: t2\n    cmd: [\"agent\"]\nvariants:\n  - id: base\n    env: { CATACOMB_FROMVARIANT: fv }\n  - id: bad\n"

func writeVerifyEvidence(t *testing.T, root, runID, task, variant, hash string, exit int) string {
	t.Helper()
	return writeVerifyEvidenceBasket(t, root, runID, "bk", task, variant, hash, exit)
}

func writeVerifyEvidenceBasket(t *testing.T, root, runID, basket, task, variant, hash string, exit int) string {
	t.Helper()
	dir := filepath.Join(root, runID)
	meta := evidence.Meta{
		RunID:      runID,
		Task:       task,
		Variant:    variant,
		Rep:        1,
		Labels:     map[string]string{"basket": basket, "task": task, "variant": variant, "rep": "1"},
		ExitCode:   exit,
		BasketHash: hash,
		MarkerName: "task:" + task,
	}
	require.NoError(t, evidence.Write(dir, meta, nil))
	return dir
}

func verifyBasket(t *testing.T) (string, string) {
	t.Helper()
	path := writeBasket(t, verifyBasketYAML)
	_, hash, err := bench.Load(path)
	require.NoError(t, err)
	return path, hash
}

func TestRunVerifyHappyPathTwoDirs(t *testing.T) {
	stubVerify(t)
	basketPath, hash := verifyBasket(t)
	runs := t.TempDir()
	dirA := writeVerifyEvidence(t, runs, "bench-bk-t1-base-r1", "t1", "base", hash, 0)
	writeVerifyEvidence(t, runs, "bench-bk-t1-base-r2", "t1", "base", hash, 0)
	writeVerifyEvidence(t, runs, "bench-bk-t2-base-r1", "t2", "base", hash, 0)

	var out, errb bytes.Buffer
	require.NoError(t, runVerify(t.Context(), &out, &errb, basketPath, verifyFlags{runsDir: runs}))

	assert.Contains(t, out.String(), "verify bench-bk-t1-base-r1: ok")
	assert.Contains(t, out.String(), "verify bench-bk-t1-base-r2: ok")
	assert.NotContains(t, out.String(), "bench-bk-t2-base-r1")
	assert.Empty(t, errb.String())

	entries, err := loadEvidenceScores(dirA, "bench-bk-t1-base-r1")
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "verifier.pass", entries[0].Key)
	assert.Equal(t, "bench-bk-t1-base-r1", entries[0].RunID)

	rec, ok, err := evidence.ReadVerify(dirA)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "offline", rec.Mode)
	assert.Empty(t, rec.Error)
}

func TestOfflineVerifyResolvesBasketRelativeScriptFromAnyCwd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("verifier is a #!/bin/sh script; not executable on Windows")
	}
	base := t.TempDir()
	script := filepath.Join(base, "verify.sh")
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\necho '{\"key\":\"verifier.pass\",\"value\":1}'\n"), 0o755))
	basketPath := filepath.Join(base, "basket.yaml")
	require.NoError(t, os.WriteFile(basketPath, []byte(
		"basket: bk\nreps: 1\ntasks:\n  - id: t1\n    cmd: [\"agent\"]\n    verify:\n      cmd: [\"./verify.sh\"]\nvariants:\n  - id: base\n"), 0o600))
	_, hash, err := bench.LoadOffline(basketPath)
	require.NoError(t, err)

	runs := t.TempDir()
	dir := writeVerifyEvidence(t, runs, "bench-bk-t1-base-r1", "t1", "base", hash, 0)

	t.Chdir(t.TempDir())

	var out, errb bytes.Buffer
	require.NoError(t, runVerify(t.Context(), &out, &errb, basketPath, verifyFlags{runsDir: runs}))
	assert.Contains(t, out.String(), "verify bench-bk-t1-base-r1: ok")
	assert.Empty(t, errb.String())

	entries, err := loadEvidenceScores(dir, "bench-bk-t1-base-r1")
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "verifier.pass", entries[0].Key)
	assert.InDelta(t, 1.0, entries[0].Value, 1e-9)

	rec, ok, err := evidence.ReadVerify(dir)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "offline", rec.Mode)
	assert.Equal(t, 0, rec.ExitCode)
	assert.Empty(t, rec.Error)
}

func TestRunVerifyOfflineSpecEnv(t *testing.T) {
	dump := filepath.Join(t.TempDir(), "env.txt")
	stubVerify(t, "VERIFY_ENV_OUT="+dump)
	basketPath, hash := verifyBasket(t)
	runs := t.TempDir()
	writeVerifyEvidence(t, runs, "bench-bk-t1-base-r1", "t1", "base", hash, 7)

	var out, errb bytes.Buffer
	require.NoError(t, runVerify(t.Context(), &out, &errb, basketPath, verifyFlags{runsDir: runs}))

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
	workdir, present := got["CATACOMB_WORKDIR"]
	assert.True(t, present)
	assert.Empty(t, workdir)
	assert.Equal(t, "7", got["CATACOMB_AGENT_EXIT_CODE"])
	assert.Equal(t, "bench-bk-t1-base-r1", got["CATACOMB_RUN_ID"])
	assert.Equal(t, "bk", got["CATACOMB_BASKET"])
	assert.Equal(t, "t1", got["CATACOMB_TASK"])
	assert.Equal(t, "base", got["CATACOMB_VARIANT"])
	assert.Equal(t, "ft", got["CATACOMB_FROMTASK"])
	assert.Equal(t, "fv", got["CATACOMB_FROMVARIANT"])
}

func TestRunVerifyWorkspacePatchFileAbsent(t *testing.T) {
	stubVerify(t)
	basketPath := writeBasket(t, "basket: wsbk\nreps: 1\ntasks:\n  - id: t1\n    cmd: [\"agent\"]\n    workspace:\n      cmd: [\"materialize\"]\n      patch: gone.patch\n    verify:\n      cmd: [\"verify\"]\nvariants:\n  - id: base\n")
	_, hash, err := bench.LoadOffline(basketPath)
	require.NoError(t, err)
	runs := t.TempDir()
	writeVerifyEvidenceBasket(t, runs, "bench-wsbk-t1-base-r1", "wsbk", "t1", "base", hash, 0)

	var out, errb bytes.Buffer
	require.NoError(t, runVerify(t.Context(), &out, &errb, basketPath, verifyFlags{runsDir: runs}))
	assert.Contains(t, out.String(), "verify bench-wsbk-t1-base-r1: ok")
	assert.Empty(t, errb.String())
}

func TestRunVerifyFailingVerifier(t *testing.T) {
	stubVerify(t)
	basketPath, hash := verifyBasket(t)
	runs := t.TempDir()
	writeVerifyEvidence(t, runs, "bench-bk-t1-bad-r1", "t1", "bad", hash, 0)

	var out, errb bytes.Buffer
	err := runVerify(t.Context(), &out, &errb, basketPath, verifyFlags{runsDir: runs})
	require.ErrorIs(t, err, errVerifyFailed)
	assert.Contains(t, out.String(), "verify bench-bk-t1-bad-r1: error (")
	assert.Contains(t, out.String(), "exit status 3")
}

func TestRunVerifyUnknownVariant(t *testing.T) {
	stubVerify(t)
	basketPath, hash := verifyBasket(t)
	runs := t.TempDir()
	writeVerifyEvidence(t, runs, "bench-bk-t1-ghost-r1", "t1", "ghost", hash, 0)

	var out, errb bytes.Buffer
	err := runVerify(t.Context(), &out, &errb, basketPath, verifyFlags{runsDir: runs})
	require.ErrorIs(t, err, errVerifyFailed)
	assert.Contains(t, out.String(), `verify bench-bk-t1-ghost-r1: error (unknown variant "ghost")`)
}

func TestRunVerifyLabelFilter(t *testing.T) {
	stubVerify(t)
	basketPath, hash := verifyBasket(t)
	runs := t.TempDir()
	writeVerifyEvidence(t, runs, "bench-bk-t1-base-r1", "t1", "base", hash, 0)
	writeVerifyEvidence(t, runs, "bench-bk-t1-bad-r1", "t1", "bad", hash, 0)

	var out, errb bytes.Buffer
	require.NoError(t, runVerify(t.Context(), &out, &errb, basketPath, verifyFlags{runsDir: runs, labels: "variant=base"}))
	assert.Contains(t, out.String(), "verify bench-bk-t1-base-r1: ok")
	assert.NotContains(t, out.String(), "bench-bk-t1-bad-r1")
}

func stubVerifyCancellingAfterFirstCell(t *testing.T, cancel context.CancelFunc) {
	t.Helper()
	t.Setenv("GO_HELPER_VERIFY", "1")
	orig := execCommandContext
	calls := 0
	execCommandContext = func(_ context.Context, _ string, _ ...string) *exec.Cmd {
		calls++
		if calls == 1 {
			cancel()
		}
		return exec.Command(os.Args[0], "-test.run=TestHelperVerify")
	}
	t.Cleanup(func() { execCommandContext = orig })
}

func TestRunVerifyStopsOnContextCancel(t *testing.T) {
	basketPath, hash := verifyBasket(t)
	runs := t.TempDir()
	first := writeVerifyEvidence(t, runs, "bench-bk-t1-base-r1", "t1", "base", hash, 0)
	second := writeVerifyEvidence(t, runs, "bench-bk-t1-base-r2", "t1", "base", hash, 0)
	third := writeVerifyEvidence(t, runs, "bench-bk-t1-base-r3", "t1", "base", hash, 0)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	stubVerifyCancellingAfterFirstCell(t, cancel)

	var out, errb bytes.Buffer
	err := runVerify(ctx, &out, &errb, basketPath, verifyFlags{runsDir: runs})
	require.ErrorIs(t, err, errVerifyInterrupted)
	var opErr *operationalError
	require.ErrorAs(t, err, &opErr)
	assert.NotErrorIs(t, err, errVerifyFailed)

	_, ok, rerr := evidence.ReadVerify(first)
	require.NoError(t, rerr)
	assert.True(t, ok)
	for _, dir := range []string{second, third} {
		_, ok, rerr := evidence.ReadVerify(dir)
		require.NoError(t, rerr)
		assert.False(t, ok, "untouched dir %s must keep its original verify.json", dir)
	}
}

func TestRunVerifyPreCancelledContextTouchesNothing(t *testing.T) {
	stubVerify(t)
	basketPath, hash := verifyBasket(t)
	runs := t.TempDir()
	dir := writeVerifyEvidence(t, runs, "bench-bk-t1-base-r1", "t1", "base", hash, 0)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	var out, errb bytes.Buffer
	err := runVerify(ctx, &out, &errb, basketPath, verifyFlags{runsDir: runs})
	require.ErrorIs(t, err, errVerifyInterrupted)
	assert.NotErrorIs(t, err, ErrEmptyGroup)

	_, ok, rerr := evidence.ReadVerify(dir)
	require.NoError(t, rerr)
	assert.False(t, ok)
}

func TestRunVerifyRejectsMalformedLabelTerms(t *testing.T) {
	tests := []struct {
		name   string
		labels string
	}{
		{name: "uppercase key", labels: "Variant=base"},
		{name: "missing separator", labels: "variant"},
		{name: "empty term in list", labels: "variant=base,"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubVerify(t)
			basketPath, hash := verifyBasket(t)
			runs := t.TempDir()
			dir := writeVerifyEvidence(t, runs, "bench-bk-t1-base-r1", "t1", "base", hash, 0)

			var out, errb bytes.Buffer
			err := runVerify(t.Context(), &out, &errb, basketPath, verifyFlags{runsDir: runs, labels: tt.labels})
			require.Error(t, err)
			var opErr *operationalError
			require.ErrorAs(t, err, &opErr)
			assert.Contains(t, err.Error(), "invalid --label")
			assert.Empty(t, out.String())

			_, ok, rerr := evidence.ReadVerify(dir)
			require.NoError(t, rerr)
			assert.False(t, ok)
		})
	}
}

func TestRunVerifyHashMismatchWarnsOnce(t *testing.T) {
	stubVerify(t)
	basketPath, _ := verifyBasket(t)
	runs := t.TempDir()
	writeVerifyEvidence(t, runs, "bench-bk-t1-base-r1", "t1", "base", "deadbeef", 0)
	writeVerifyEvidence(t, runs, "bench-bk-t1-base-r2", "t1", "base", "deadbeef", 0)

	var out, errb bytes.Buffer
	require.NoError(t, runVerify(t.Context(), &out, &errb, basketPath, verifyFlags{runsDir: runs}))
	assert.Equal(t, 1, strings.Count(errb.String(), verifyHashWarning))
}

func TestRunVerifyNoMatchingRuns(t *testing.T) {
	stubVerify(t)
	basketPath, hash := verifyBasket(t)
	runs := t.TempDir()
	writeVerifyEvidenceBasket(t, runs, "bench-other-t1-base-r1", "other", "t1", "base", hash, 0)

	err := runVerify(t.Context(), &bytes.Buffer{}, &bytes.Buffer{}, basketPath, verifyFlags{runsDir: runs})
	require.ErrorIs(t, err, ErrEmptyGroup)
	var opErr *operationalError
	require.ErrorAs(t, err, &opErr)
}

func TestRunVerifyEmptyRunsDirFlag(t *testing.T) {
	basketPath, _ := verifyBasket(t)
	err := runVerify(t.Context(), &bytes.Buffer{}, &bytes.Buffer{}, basketPath, verifyFlags{runsDir: ""})
	require.ErrorIs(t, err, errVerifyNoRunsDir)
	var opErr *operationalError
	require.ErrorAs(t, err, &opErr)
}

func TestRunVerifyBadBasketIsOperational(t *testing.T) {
	err := runVerify(t.Context(), &bytes.Buffer{}, &bytes.Buffer{}, filepath.Join(t.TempDir(), "missing.yaml"), verifyFlags{runsDir: t.TempDir()})
	require.Error(t, err)
	var opErr *operationalError
	require.ErrorAs(t, err, &opErr)
}

func TestRunVerifyScanRunsError(t *testing.T) {
	basketPath, _ := verifyBasket(t)
	runsFile := writeBasket(t, "x")
	err := runVerify(t.Context(), &bytes.Buffer{}, &bytes.Buffer{}, basketPath, verifyFlags{runsDir: runsFile})
	require.Error(t, err)
	var opErr *operationalError
	require.ErrorAs(t, err, &opErr)
}

func TestVerifyUnknownTaskSkipped(t *testing.T) {
	stubVerify(t)
	basketPath, hash := verifyBasket(t)
	runs := t.TempDir()
	writeVerifyEvidence(t, runs, "bench-bk-t1-base-r1", "t1", "base", hash, 0)
	writeVerifyEvidence(t, runs, "bench-bk-gone-base-r1", "gone", "base", hash, 0)

	var out, errb bytes.Buffer
	require.NoError(t, runVerify(t.Context(), &out, &errb, basketPath, verifyFlags{runsDir: runs}))
	assert.Contains(t, out.String(), "verify bench-bk-t1-base-r1: ok")
	assert.NotContains(t, out.String(), "bench-bk-gone-base-r1")
}

func TestVerifyCLIExitCodes(t *testing.T) {
	basketPath, hash := verifyBasket(t)

	t.Run("clean exit 0", func(t *testing.T) {
		stubVerify(t)
		runs := t.TempDir()
		writeVerifyEvidence(t, runs, "bench-bk-t1-base-r1", "t1", "base", hash, 0)
		var out, errb bytes.Buffer
		code := run([]string{"verify", basketPath, "--runs-dir", runs}, &out, &errb)
		require.Equal(t, 0, code, errb.String())
		assert.Contains(t, out.String(), "verify bench-bk-t1-base-r1: ok")
	})

	t.Run("verifier failure exit 1", func(t *testing.T) {
		stubVerify(t)
		runs := t.TempDir()
		writeVerifyEvidence(t, runs, "bench-bk-t1-bad-r1", "t1", "bad", hash, 0)
		var out, errb bytes.Buffer
		code := run([]string{"verify", basketPath, "--runs-dir", runs}, &out, &errb)
		assert.Equal(t, 1, code)
		assert.Contains(t, out.String(), "error (")
	})

	t.Run("no runs exit 2", func(t *testing.T) {
		stubVerify(t)
		var out, errb bytes.Buffer
		code := run([]string{"verify", basketPath, "--runs-dir", t.TempDir()}, &out, &errb)
		assert.Equal(t, 2, code)
		assert.Contains(t, errb.String(), "matched no runs")
	})
}

func TestVerifyCmdWiredAndFlags(t *testing.T) {
	root := newRootCmd()
	names := map[string]bool{}
	for _, sub := range root.Commands() {
		names[sub.Name()] = true
	}
	assert.True(t, names["verify"])

	cmd := newVerifyCmd()
	rd := cmd.Flags().Lookup("runs-dir")
	require.NotNil(t, rd)
	assert.True(t, strings.HasSuffix(rd.DefValue, filepath.Join(".catacomb", "runs")) || rd.DefValue == "")
	require.NotNil(t, cmd.Flags().Lookup("label"))
}
