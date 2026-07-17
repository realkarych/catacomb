package main

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/store"
)

func importArgs(bundlePath, dbPath, runsDir string) []string {
	return []string{"baseline", "import", bundlePath, "--db", dbPath, "--runs-dir", runsDir}
}

func importTestBaseline(runIDs ...string) model.Baseline {
	return model.Baseline{
		Name:      "golden",
		RunIDs:    runIDs,
		Selector:  map[string]string{"variant": "base"},
		CreatedAt: time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC),
		Stamps:    currentStamps(),
	}
}

func importManifestEntry(t *testing.T, b model.Baseline, hashes map[string]string) bundleTarEntry {
	t.Helper()
	data, err := json.Marshal(bundleManifest{Version: bundleVersion, Baseline: b, Files: hashes})
	require.NoError(t, err)
	return bundleTarEntry{name: bundleManifestName, typeflag: tar.TypeReg, data: data}
}

func importHashes(files map[string][]byte) map[string]string {
	hashes := make(map[string]string, len(files))
	for p, data := range files {
		hashes[p] = bundleSHA(string(data))
	}
	return hashes
}

func importFileEntries(files map[string][]byte) []bundleTarEntry {
	entries := make([]bundleTarEntry, 0, len(files))
	for _, p := range slices.Sorted(maps.Keys(files)) {
		entries = append(entries, bundleTarEntry{name: p, typeflag: tar.TypeReg, data: files[p]})
	}
	return entries
}

func writeImportBundle(t *testing.T, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "bundle.tar.gz")
	require.NoError(t, os.WriteFile(p, data, 0o600))
	return p
}

func exportedGoldenBundle(t *testing.T) (string, string) {
	t.Helper()
	dbPath, runsDir := seedExportBaseline(t)
	out := filepath.Join(t.TempDir(), "golden.tar.gz")
	var stdout, stderr bytes.Buffer
	require.Equal(t, 0, run(baselineExportArgs(dbPath, runsDir, out), &stdout, &stderr), stderr.String())
	return out, runsDir
}

func importedGoldenTarget(t *testing.T) (string, string, string) {
	t.Helper()
	bundle, _ := exportedGoldenBundle(t)
	targetRuns := filepath.Join(t.TempDir(), "runs")
	targetDB := emptyStoreDB(t)
	var stdout, stderr bytes.Buffer
	require.Equal(t, 0, run(importArgs(bundle, targetDB, targetRuns), &stdout, &stderr), stderr.String())
	return bundle, targetRuns, targetDB
}

func listStoredBaselines(t *testing.T, dbPath string) []model.Baseline {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, runBaselineList(&buf, store.OpenSQLiteReadOnly, dbPath, true))
	var baselines []model.Baseline
	require.NoError(t, json.Unmarshal(buf.Bytes(), &baselines))
	return baselines
}

func runDirNames(t *testing.T, runsDir string) []string {
	t.Helper()
	entries, err := os.ReadDir(runsDir)
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func goldenRunFiles() []string {
	return []string{
		filepath.Join("base-0", "meta.json"),
		filepath.Join("base-0", "session.jsonl"),
		filepath.Join("base-1", "meta.json"),
		filepath.Join("base-1", "session.jsonl"),
	}
}

func goldenModTimes(t *testing.T, runsDir string) map[string]time.Time {
	t.Helper()
	times := map[string]time.Time{}
	for _, rel := range goldenRunFiles() {
		info, err := os.Stat(filepath.Join(runsDir, rel))
		require.NoError(t, err)
		times[rel] = info.ModTime()
	}
	return times
}

func assertNoImportResidue(t *testing.T, runsDir, dbPath string) {
	t.Helper()
	entries, err := os.ReadDir(runsDir)
	if !errors.Is(err, fs.ErrNotExist) {
		require.NoError(t, err)
		assert.Empty(t, entries)
	}
	assert.Empty(t, listStoredBaselines(t, dbPath))
}

func TestBaselineImportRoundTripListRegress(t *testing.T) {
	ts := pinBaselineNow(t)
	bundle, srcRunsDir := exportedGoldenBundle(t)
	targetRuns := filepath.Join(t.TempDir(), "runs")
	targetDB := filepath.Join(t.TempDir(), "fresh", "catacomb.db")

	var stdout, stderr bytes.Buffer
	code := run(importArgs(bundle, targetDB, targetRuns), &stdout, &stderr)
	require.Equal(t, 0, code, stderr.String())
	assert.Equal(t, fmt.Sprintf("imported baseline golden: 2 runs into %s\n", targetRuns), stdout.String())
	assert.Empty(t, stderr.String())

	for _, rel := range goldenRunFiles() {
		want, err := os.ReadFile(filepath.Join(srcRunsDir, rel))
		require.NoError(t, err)
		got, err := os.ReadFile(filepath.Join(targetRuns, rel))
		require.NoError(t, err)
		assert.Equal(t, want, got, rel)
	}
	assert.ElementsMatch(t, []string{"base-0", "base-1"}, runDirNames(t, targetRuns))

	baselines := listStoredBaselines(t, targetDB)
	require.Len(t, baselines, 1)
	b := baselines[0]
	assert.Equal(t, "golden", b.Name)
	assert.Equal(t, []string{"base-0", "base-1"}, b.RunIDs)
	assert.Equal(t, map[string]string{"variant": "base"}, b.Selector)
	assert.Equal(t, targetRuns, b.RunsDir)
	assert.Equal(t, model.Stamps{CatacombVersion: "dev", StepKeyScheme: "stepkey/v1"}, b.Stamps)
	assert.True(t, b.CreatedAt.Equal(ts))

	writeEvidenceRun(t, targetRuns, "cand-0", "cand", "session.jsonl")
	writeEvidenceRun(t, targetRuns, "cand-1", "cand", "session.jsonl")
	stdout.Reset()
	stderr.Reset()
	code = run([]string{
		"regress", "--runs-dir", targetRuns, "--db", targetDB,
		"--baseline", "name:golden", "--candidate", "label:variant=cand",
	}, &stdout, &stderr)
	assert.Equal(t, 0, code, stderr.String())
	assert.Contains(t, stdout.String(), "baseline runs 2")
	assert.NotContains(t, stderr.String(), "recorded runs-dir")
	assert.Empty(t, stderr.String())
}

func TestBaselineImportIdempotentReimport(t *testing.T) {
	bundle, targetRuns, targetDB := importedGoldenTarget(t)
	before := goldenModTimes(t, targetRuns)

	var stdout, stderr bytes.Buffer
	code := run(importArgs(bundle, targetDB, targetRuns), &stdout, &stderr)
	require.Equal(t, 0, code, stderr.String())
	assert.Equal(t, fmt.Sprintf("imported baseline golden: 2 runs into %s\n", targetRuns), stdout.String())

	after := goldenModTimes(t, targetRuns)
	assert.Equal(t, before, after)
	assert.ElementsMatch(t, []string{"base-0", "base-1"}, runDirNames(t, targetRuns))
	assert.Len(t, listStoredBaselines(t, targetDB), 1)
}

func TestBaselineImportRestoresMissingRun(t *testing.T) {
	bundle, targetRuns, targetDB := importedGoldenTarget(t)
	base0Session, err := os.ReadFile(filepath.Join(targetRuns, "base-0", "session.jsonl"))
	require.NoError(t, err)
	base1Session, err := os.ReadFile(filepath.Join(targetRuns, "base-1", "session.jsonl"))
	require.NoError(t, err)
	require.NoError(t, os.RemoveAll(filepath.Join(targetRuns, "base-1")))

	var stdout, stderr bytes.Buffer
	code := run(importArgs(bundle, targetDB, targetRuns), &stdout, &stderr)
	require.Equal(t, 0, code, stderr.String())

	restored, err := os.ReadFile(filepath.Join(targetRuns, "base-1", "session.jsonl"))
	require.NoError(t, err)
	assert.Equal(t, base1Session, restored)
	kept, err := os.ReadFile(filepath.Join(targetRuns, "base-0", "session.jsonl"))
	require.NoError(t, err)
	assert.Equal(t, base0Session, kept)
	assert.ElementsMatch(t, []string{"base-0", "base-1"}, runDirNames(t, targetRuns))
}

func TestBaselineImportCollisions(t *testing.T) {
	cases := []struct {
		name    string
		corrupt func(t *testing.T, runsDir string)
		wants   []string
	}{
		{
			"different content",
			func(t *testing.T, runsDir string) {
				t.Helper()
				p := filepath.Join(runsDir, "base-0", "session.jsonl")
				require.NoError(t, os.WriteFile(p, []byte("tampered\n"), 0o600))
			},
			[]string{"different content", "base-0"},
		},
		{
			"missing file",
			func(t *testing.T, runsDir string) {
				t.Helper()
				require.NoError(t, os.Remove(filepath.Join(runsDir, "base-1", "meta.json")))
			},
			[]string{"base-1", "meta.json"},
		},
		{
			"extra file",
			func(t *testing.T, runsDir string) {
				t.Helper()
				p := filepath.Join(runsDir, "base-0", "extra.txt")
				require.NoError(t, os.WriteFile(p, []byte("x"), 0o600))
			},
			[]string{"different content", "extra.txt"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bundle, targetRuns, targetDB := importedGoldenTarget(t)
			tc.corrupt(t, targetRuns)

			var stdout, stderr bytes.Buffer
			code := run(importArgs(bundle, targetDB, targetRuns), &stdout, &stderr)
			assert.Equal(t, 2, code)
			for _, want := range tc.wants {
				assert.Contains(t, stderr.String(), want)
			}
			assert.ElementsMatch(t, []string{"base-0", "base-1"}, runDirNames(t, targetRuns))
		})
	}
}

func TestBaselineImportCollisionLeavesDiskUntouched(t *testing.T) {
	bundle, targetRuns, targetDB := importedGoldenTarget(t)
	p := filepath.Join(targetRuns, "base-0", "session.jsonl")
	require.NoError(t, os.WriteFile(p, []byte("tampered\n"), 0o600))

	var stdout, stderr bytes.Buffer
	code := run(importArgs(bundle, targetDB, targetRuns), &stdout, &stderr)
	assert.Equal(t, 2, code)

	data, err := os.ReadFile(p)
	require.NoError(t, err)
	assert.Equal(t, []byte("tampered\n"), data)
}

func TestBaselineImportCollisionSymlinkSameContent(t *testing.T) {
	bundle, targetRuns, targetDB := importedGoldenTarget(t)
	p := filepath.Join(targetRuns, "base-0", "session.jsonl")
	data, err := os.ReadFile(p)
	require.NoError(t, err)
	copyPath := filepath.Join(targetRuns, "base-0", ".session-copy")
	require.NoError(t, os.Remove(p))
	require.NoError(t, os.WriteFile(copyPath, data, 0o600))
	if err := os.Symlink(copyPath, p); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run(importArgs(bundle, targetDB, targetRuns), &stdout, &stderr)
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr.String(), "different content")
}

func TestBaselineImportRejectsBadBundles(t *testing.T) {
	sessionPath := "runs/r1/session.jsonl"
	metaPath := "runs/r1/meta.json"
	baseFiles := func() map[string][]byte {
		return map[string][]byte{
			metaPath:    []byte(`{"run_id":"r1"}` + "\n"),
			sessionPath: []byte("payload-v1\n"),
		}
	}
	cases := []struct {
		name   string
		bundle func(t *testing.T) []byte
		want   string
	}{
		{
			"tampered file content",
			func(t *testing.T) []byte {
				t.Helper()
				files := baseFiles()
				hashes := importHashes(files)
				files[sessionPath][0] ^= 0x01
				entries := append([]bundleTarEntry{importManifestEntry(t, importTestBaseline("r1"), hashes)}, importFileEntries(files)...)
				return gzipTarBundle(t, entries...)
			},
			"hash mismatch",
		},
		{
			"tar file not in manifest",
			func(t *testing.T) []byte {
				t.Helper()
				files := baseFiles()
				hashes := importHashes(files)
				delete(hashes, sessionPath)
				entries := append([]bundleTarEntry{importManifestEntry(t, importTestBaseline("r1"), hashes)}, importFileEntries(files)...)
				return gzipTarBundle(t, entries...)
			},
			"not in the manifest",
		},
		{
			"manifest file missing from archive",
			func(t *testing.T) []byte {
				t.Helper()
				files := baseFiles()
				hashes := importHashes(files)
				hashes["runs/r1/ghost.jsonl"] = bundleSHA("ghost\n")
				entries := append([]bundleTarEntry{importManifestEntry(t, importTestBaseline("r1"), hashes)}, importFileEntries(files)...)
				return gzipTarBundle(t, entries...)
			},
			"missing from the archive",
		},
		{
			"duplicate archive entry",
			func(t *testing.T) []byte {
				t.Helper()
				files := baseFiles()
				entry := bundleTarEntry{name: sessionPath, typeflag: tar.TypeReg, data: files[sessionPath]}
				return gzipTarBundle(t, importManifestEntry(t, importTestBaseline("r1"), importHashes(files)), entry, entry)
			},
			"duplicate archive entry",
		},
		{
			"version too new",
			func(t *testing.T) []byte {
				t.Helper()
				return gzipTarBundle(t, bundleManifestEntry(t, 2))
			},
			"newer than this catacomb supports",
		},
		{
			"hostile symlink entry",
			func(t *testing.T) []byte {
				t.Helper()
				link := bundleTarEntry{name: "runs/r1/link", typeflag: tar.TypeSymlink, linkname: "../../etc/passwd"}
				return gzipTarBundle(t, importManifestEntry(t, importTestBaseline("r1"), map[string]string{}), link)
			},
			"not a regular file",
		},
		{
			"run id path traversal",
			func(t *testing.T) []byte {
				t.Helper()
				return gzipTarBundle(t, importManifestEntry(t, importTestBaseline("../evil"), map[string]string{}))
			},
			"run id is not a clean local name",
		},
		{
			"run id embedded slash",
			func(t *testing.T) []byte {
				t.Helper()
				return gzipTarBundle(t, importManifestEntry(t, importTestBaseline("nested/run"), map[string]string{}))
			},
			"run id is not a clean local name",
		},
		{
			"run id embedded backslash",
			func(t *testing.T) []byte {
				t.Helper()
				return gzipTarBundle(t, importManifestEntry(t, importTestBaseline(`nested\run`), map[string]string{}))
			},
			"run id is not a clean local name",
		},
		{
			"invalid baseline name",
			func(t *testing.T) []byte {
				t.Helper()
				files := baseFiles()
				b := importTestBaseline("r1")
				b.Name = " x"
				entries := append([]bundleTarEntry{importManifestEntry(t, b, importHashes(files))}, importFileEntries(files)...)
				return gzipTarBundle(t, entries...)
			},
			"whitespace",
		},
		{
			"file then nested under file",
			func(t *testing.T) []byte {
				t.Helper()
				files := map[string][]byte{"runs/r1/a": []byte("x\n"), "runs/r1/a/b": []byte("y\n")}
				entries := append([]bundleTarEntry{importManifestEntry(t, importTestBaseline("r1"), importHashes(files))},
					bundleTarEntry{name: "runs/r1/a", typeflag: tar.TypeReg, data: files["runs/r1/a"]},
					bundleTarEntry{name: "runs/r1/a/b", typeflag: tar.TypeReg, data: files["runs/r1/a/b"]})
				return gzipTarBundle(t, entries...)
			},
			"baseline import",
		},
		{
			"file over staged dir",
			func(t *testing.T) []byte {
				t.Helper()
				files := map[string][]byte{"runs/r1/a": []byte("x\n"), "runs/r1/a/b": []byte("y\n")}
				entries := append([]bundleTarEntry{importManifestEntry(t, importTestBaseline("r1"), importHashes(files))},
					bundleTarEntry{name: "runs/r1/a/b", typeflag: tar.TypeReg, data: files["runs/r1/a/b"]},
					bundleTarEntry{name: "runs/r1/a", typeflag: tar.TypeReg, data: files["runs/r1/a"]})
				return gzipTarBundle(t, entries...)
			},
			"baseline import",
		},
		{
			"truncated file payload",
			func(t *testing.T) []byte {
				t.Helper()
				files := map[string][]byte{sessionPath: bytes.Repeat([]byte("x"), 600)}
				entries := append([]bundleTarEntry{importManifestEntry(t, importTestBaseline("r1"), importHashes(files))}, importFileEntries(files)...)
				raw := gunzipBundle(t, gzipTarBundle(t, entries...))
				return gzipBytes(t, raw[:len(raw)-1724])
			},
			"unexpected EOF",
		},
		{
			"garbage gzip",
			func(t *testing.T) []byte {
				t.Helper()
				return []byte("not a gzip stream")
			},
			"open gzip",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bundle := writeImportBundle(t, tc.bundle(t))
			targetRuns := filepath.Join(t.TempDir(), "runs")
			targetDB := emptyStoreDB(t)

			var stdout, stderr bytes.Buffer
			code := run(importArgs(bundle, targetDB, targetRuns), &stdout, &stderr)
			assert.Equal(t, 2, code, stderr.String())
			assert.Contains(t, stderr.String(), tc.want)
			assertNoImportResidue(t, targetRuns, targetDB)
		})
	}
}

func TestBaselineImportHostileRunIDSentinel(t *testing.T) {
	bundle := writeImportBundle(t, gzipTarBundle(t, importManifestEntry(t, importTestBaseline("../evil"), map[string]string{})))

	err := runBaselineImport(io.Discard, store.OpenSQLite, emptyStoreDB(t), bundle, filepath.Join(t.TempDir(), "runs"))
	require.ErrorIs(t, err, errBundleRunID)
	var opErr *operationalError
	require.ErrorAs(t, err, &opErr)
}

func TestBaselineImportMissingDiskFileIsReadErrorNotCollision(t *testing.T) {
	bundle, targetRuns, targetDB := importedGoldenTarget(t)
	require.NoError(t, os.Remove(filepath.Join(targetRuns, "base-1", "meta.json")))

	err := runBaselineImport(io.Discard, store.OpenSQLite, targetDB, bundle, targetRuns)
	require.ErrorIs(t, err, fs.ErrNotExist)
	assert.NotErrorIs(t, err, errBundleCollision)
	var opErr *operationalError
	require.ErrorAs(t, err, &opErr)
}

func TestBaselineImportEmptyRunsBundle(t *testing.T) {
	bundle := writeImportBundle(t, gzipTarBundle(t, importManifestEntry(t, importTestBaseline(), map[string]string{})))
	targetRuns := filepath.Join(t.TempDir(), "runs")
	targetDB := emptyStoreDB(t)

	var stdout, stderr bytes.Buffer
	code := run(importArgs(bundle, targetDB, targetRuns), &stdout, &stderr)
	require.Equal(t, 0, code, stderr.String())
	assert.Equal(t, fmt.Sprintf("imported baseline golden: 0 runs into %s\n", targetRuns), stdout.String())

	baselines := listStoredBaselines(t, targetDB)
	require.Len(t, baselines, 1)
	assert.Equal(t, "golden", baselines[0].Name)
	assert.Empty(t, baselines[0].RunIDs)
	assert.Equal(t, targetRuns, baselines[0].RunsDir)
}

func TestBaselineImportRelativeRunsDirStoredAbsolute(t *testing.T) {
	bundle, _ := exportedGoldenBundle(t)
	targetDB := emptyStoreDB(t)
	t.Chdir(t.TempDir())
	abs, err := filepath.Abs("rel-runs")
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	code := run(importArgs(bundle, targetDB, "rel-runs"), &stdout, &stderr)
	require.Equal(t, 0, code, stderr.String())
	assert.Equal(t, fmt.Sprintf("imported baseline golden: 2 runs into %s\n", abs), stdout.String())

	baselines := listStoredBaselines(t, targetDB)
	require.Len(t, baselines, 1)
	assert.Equal(t, abs, baselines[0].RunsDir)
	assert.ElementsMatch(t, []string{"base-0", "base-1"}, runDirNames(t, abs))
}

func TestBaselineImportMissingBundle(t *testing.T) {
	targetRuns := filepath.Join(t.TempDir(), "runs")
	targetDB := emptyStoreDB(t)

	var stdout, stderr bytes.Buffer
	code := run(importArgs(filepath.Join(t.TempDir(), "ghost.tar.gz"), targetDB, targetRuns), &stdout, &stderr)
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr.String(), "baseline import")
	assertNoImportResidue(t, targetRuns, targetDB)
}

func TestBaselineImportRunsDirIsFile(t *testing.T) {
	bundle, _ := exportedGoldenBundle(t)
	targetDB := emptyStoreDB(t)
	flat := filepath.Join(t.TempDir(), "flat")
	require.NoError(t, os.WriteFile(flat, []byte("x"), 0o600))

	var stdout, stderr bytes.Buffer
	code := run(importArgs(bundle, targetDB, flat), &stdout, &stderr)
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr.String(), "baseline import")
	assert.Empty(t, listStoredBaselines(t, targetDB))
}

func TestBaselineImportRequiresRunsDir(t *testing.T) {
	err := runBaselineImport(io.Discard, store.OpenSQLite, emptyStoreDB(t), "x.tar.gz", "")
	require.ErrorIs(t, err, errBaselineImportNoRunsDir)
	var opErr *operationalError
	require.ErrorAs(t, err, &opErr)
}

func TestBaselineImportAbsError(t *testing.T) {
	orig := absFn
	absFn = func(string) (string, error) { return "", errors.New("boom-abs") }
	t.Cleanup(func() { absFn = orig })

	err := runBaselineImport(io.Discard, store.OpenSQLite, emptyStoreDB(t), "x.tar.gz", t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom-abs")
	var opErr *operationalError
	require.ErrorAs(t, err, &opErr)
}

func TestBaselineImportOpenStoreError(t *testing.T) {
	bundle := writeImportBundle(t, gzipTarBundle(t, importManifestEntry(t, importTestBaseline(), map[string]string{})))
	failing := func(string) (store.Store, error) { return nil, errors.New("boom-open") }

	err := runBaselineImport(io.Discard, failing, filepath.Join(t.TempDir(), "x.db"), bundle, filepath.Join(t.TempDir(), "runs"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom-open")
	var opErr *operationalError
	require.ErrorAs(t, err, &opErr)
}

func TestBaselineImportUpsertError(t *testing.T) {
	bundle := writeImportBundle(t, gzipTarBundle(t, importManifestEntry(t, importTestBaseline(), map[string]string{})))

	err := runBaselineImport(io.Discard, openStore(&upsertErrStore{}), filepath.Join(t.TempDir(), "x.db"), bundle, filepath.Join(t.TempDir(), "runs"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "baseline import")
	var opErr *operationalError
	require.ErrorAs(t, err, &opErr)
}
