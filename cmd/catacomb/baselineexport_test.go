package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/store"
)

func seedExportBaseline(t *testing.T) (string, string) {
	t.Helper()
	runsDir := evidenceRoot(t)
	dbPath := emptyStoreDB(t)
	require.NoError(t, runBaselineSet(io.Discard, store.OpenSQLite, dbPath, "golden", []string{"variant=base"}, runsDir))
	return dbPath, runsDir
}

func baselineExportArgs(dbPath, runsDir, out string) []string {
	return []string{"baseline", "export", "golden", "--db", dbPath, "--runs-dir", runsDir, "--out", out}
}

func TestBaselineExportHappyPath(t *testing.T) {
	dbPath, runsDir := seedExportBaseline(t)
	out := filepath.Join(t.TempDir(), "golden.catacomb-bundle.tar.gz")

	var stdout, stderr bytes.Buffer
	code := run(baselineExportArgs(dbPath, runsDir, out), &stdout, &stderr)
	require.Equal(t, 0, code, stderr.String())
	assert.Equal(t, fmt.Sprintf("exported baseline golden: %s (2 runs, 4 files)\n", out), stdout.String())

	f, err := os.Open(out)
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })
	got := map[string][]byte{}
	m, err := readBundle(f, collectBundleContents(got))
	require.NoError(t, err)
	assert.Equal(t, bundleVersion, m.Version)
	assert.Equal(t, "golden", m.Baseline.Name)
	assert.Equal(t, []string{"base-0", "base-1"}, m.Baseline.RunIDs)
	wantPaths := []string{
		"runs/base-0/meta.json",
		"runs/base-0/session.jsonl",
		"runs/base-1/meta.json",
		"runs/base-1/session.jsonl",
	}
	require.Len(t, m.Files, len(wantPaths))
	for _, p := range wantPaths {
		assert.Contains(t, m.Files, p)
		assert.Contains(t, got, p)
	}
}

func TestBaselineExportDeterministic(t *testing.T) {
	dbPath, runsDir := seedExportBaseline(t)
	dir := t.TempDir()
	first := filepath.Join(dir, "first.tar.gz")
	second := filepath.Join(dir, "second.tar.gz")

	var stdout, stderr bytes.Buffer
	require.Equal(t, 0, run(baselineExportArgs(dbPath, runsDir, first), &stdout, &stderr), stderr.String())
	require.Equal(t, 0, run(baselineExportArgs(dbPath, runsDir, second), &stdout, &stderr), stderr.String())

	firstBytes, err := os.ReadFile(first)
	require.NoError(t, err)
	secondBytes, err := os.ReadFile(second)
	require.NoError(t, err)
	assert.Equal(t, firstBytes, secondBytes)
}

func TestBaselineExportUnknownBaseline(t *testing.T) {
	dbPath := emptyStoreDB(t)
	out := filepath.Join(t.TempDir(), "ghost.tar.gz")

	err := runBaselineExport(io.Discard, store.OpenSQLiteReadOnly, dbPath, "ghost", t.TempDir(), out)
	require.ErrorIs(t, err, ErrBaselineNotFound)
	var opErr *operationalError
	require.ErrorAs(t, err, &opErr)

	var stdout, stderr bytes.Buffer
	code := run([]string{"baseline", "export", "ghost", "--db", dbPath, "--runs-dir", t.TempDir(), "--out", out}, &stdout, &stderr)
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr.String(), `baseline not found: "ghost"`)
}

func TestBaselineExportMissingRunDir(t *testing.T) {
	dbPath, runsDir := seedExportBaseline(t)
	require.NoError(t, os.RemoveAll(filepath.Join(runsDir, "base-1")))
	outDir := t.TempDir()
	out := filepath.Join(outDir, "golden.tar.gz")

	var stdout, stderr bytes.Buffer
	code := run(baselineExportArgs(dbPath, runsDir, out), &stdout, &stderr)
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr.String(), `"base-1"`)

	_, statErr := os.Stat(out)
	require.ErrorIs(t, statErr, fs.ErrNotExist)
	entries, err := os.ReadDir(outDir)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestBaselineExportRunDirIsAFile(t *testing.T) {
	dbPath, runsDir := seedExportBaseline(t)
	require.NoError(t, os.RemoveAll(filepath.Join(runsDir, "base-0")))
	require.NoError(t, os.WriteFile(filepath.Join(runsDir, "base-0"), []byte("x"), 0o600))

	err := runBaselineExport(io.Discard, store.OpenSQLiteReadOnly, dbPath, "golden", runsDir, filepath.Join(t.TempDir(), "x.tar.gz"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"base-0"`)
	var opErr *operationalError
	require.ErrorAs(t, err, &opErr)
}

func TestBaselineExportOutExists(t *testing.T) {
	dbPath, runsDir := seedExportBaseline(t)
	out := filepath.Join(t.TempDir(), "golden.tar.gz")
	require.NoError(t, os.WriteFile(out, []byte("keep"), 0o600))

	var stdout, stderr bytes.Buffer
	code := run(baselineExportArgs(dbPath, runsDir, out), &stdout, &stderr)
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr.String(), "already exists")

	data, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Equal(t, "keep", string(data))
}

func TestBaselineExportNoPartialOutOnWriteError(t *testing.T) {
	dbPath, runsDir := seedExportBaseline(t)
	link := filepath.Join(runsDir, "base-0", "loop.jsonl")
	if err := os.Symlink(filepath.Join(runsDir, "base-0", "session.jsonl"), link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	outDir := t.TempDir()
	out := filepath.Join(outDir, "golden.tar.gz")

	var stdout, stderr bytes.Buffer
	code := run(baselineExportArgs(dbPath, runsDir, out), &stdout, &stderr)
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr.String(), "symlink")

	entries, err := os.ReadDir(outDir)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestBaselineExportTempCreateError(t *testing.T) {
	dbPath, runsDir := seedExportBaseline(t)
	out := filepath.Join(t.TempDir(), "missing-parent", "golden.tar.gz")

	err := runBaselineExport(io.Discard, store.OpenSQLiteReadOnly, dbPath, "golden", runsDir, out)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "baseline export")
	var opErr *operationalError
	require.ErrorAs(t, err, &opErr)
}

func TestBaselineExportMissingStore(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.db")
	err := runBaselineExport(io.Discard, store.OpenSQLiteReadOnly, missing, "golden", t.TempDir(), filepath.Join(t.TempDir(), "x.tar.gz"))
	require.ErrorIs(t, err, ErrStoreNotFound)
}

type getErrStore struct {
	fakeStore
}

func (g *getErrStore) GetBaseline(string) (model.Baseline, bool, error) {
	return model.Baseline{}, false, errors.New("boom")
}

func TestBaselineExportGetBaselineError(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "*.db")
	require.NoError(t, err)
	_ = f.Close()
	err = runBaselineExport(io.Discard, openStore(&getErrStore{}), f.Name(), "golden", t.TempDir(), filepath.Join(t.TempDir(), "x.tar.gz"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "baseline export")
}

func TestBaselineExportRequiresOut(t *testing.T) {
	dbPath, runsDir := seedExportBaseline(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{"baseline", "export", "golden", "--db", dbPath, "--runs-dir", runsDir}, &stdout, &stderr)
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr.String(), "--out is required")
}

func TestBaselineExportRequiresRunsDir(t *testing.T) {
	err := runBaselineExport(io.Discard, store.OpenSQLiteReadOnly, emptyStoreDB(t), "golden", "", filepath.Join(t.TempDir(), "x.tar.gz"))
	require.ErrorIs(t, err, errBaselineExportNoRunsDir)
	var opErr *operationalError
	require.ErrorAs(t, err, &opErr)
}
