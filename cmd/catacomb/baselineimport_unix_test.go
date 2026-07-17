//go:build !windows

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBaselineImportTempDirCreateError(t *testing.T) {
	bundle := writeImportBundle(t, []byte("never read"))
	targetDB := emptyStoreDB(t)
	targetRuns := t.TempDir()
	require.NoError(t, os.Chmod(targetRuns, 0o500))
	t.Cleanup(func() { _ = os.Chmod(targetRuns, 0o700) })

	var stdout, stderr bytes.Buffer
	code := run(importArgs(bundle, targetDB, targetRuns), &stdout, &stderr)
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr.String(), "baseline import")
	assert.Empty(t, listStoredBaselines(t, targetDB))
}

func TestBaselineImportExistingRunWalkError(t *testing.T) {
	bundle, targetRuns, targetDB := importedGoldenTarget(t)
	locked := filepath.Join(targetRuns, "base-0", "locked")
	require.NoError(t, os.Mkdir(locked, 0o700))
	require.NoError(t, os.Chmod(locked, 0o000))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })

	var stdout, stderr bytes.Buffer
	code := run(importArgs(bundle, targetDB, targetRuns), &stdout, &stderr)
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr.String(), "baseline import")
}

func TestBaselineImportEvidencePerms(t *testing.T) {
	files := map[string][]byte{
		"runs/r1/meta.json":                 []byte(`{"run_id":"r1"}` + "\n"),
		"runs/r1/session.jsonl":             []byte("line\n"),
		"runs/r1/subagents/agent-001.jsonl": []byte("sub\n"),
	}
	entries := append([]bundleTarEntry{importManifestEntry(t, importTestBaseline("r1"), importHashes(files))}, importFileEntries(files)...)
	bundle := writeImportBundle(t, gzipTarBundle(t, entries...))
	targetRuns := filepath.Join(t.TempDir(), "runs")
	targetDB := emptyStoreDB(t)

	var stdout, stderr bytes.Buffer
	code := run(importArgs(bundle, targetDB, targetRuns), &stdout, &stderr)
	require.Equal(t, 0, code, stderr.String())

	for _, dir := range []string{targetRuns, filepath.Join(targetRuns, "r1"), filepath.Join(targetRuns, "r1", "subagents")} {
		info, err := os.Stat(dir)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o700), info.Mode().Perm(), dir)
	}
	for _, rel := range []string{"meta.json", "session.jsonl", filepath.Join("subagents", "agent-001.jsonl")} {
		info, err := os.Stat(filepath.Join(targetRuns, "r1", rel))
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), rel)
	}
}
