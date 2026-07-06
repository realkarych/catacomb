package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/bench"
)

func writeBasket(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "basket.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func readManifest(t *testing.T, path string) []bench.ManifestEntry {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var out []bench.ManifestEntry
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		var e bench.ManifestEntry
		require.NoError(t, json.Unmarshal(line, &e))
		out = append(out, e)
	}
	return out
}

func loadBasket(t *testing.T, content string) (bench.Basket, string, []bench.Cell) {
	t.Helper()
	b, hash, err := bench.Load(writeBasket(t, content))
	require.NoError(t, err)
	return b, hash, b.Cells()
}

func markedCellFn(hash string) cellRunner {
	return func(cell bench.Cell, _ map[string]string) (bench.ManifestEntry, bool, bool) {
		return manifestFor(cell, hash, true), false, false
	}
}

func manifestFor(cell bench.Cell, hash string, marked bool) bench.ManifestEntry {
	return bench.ManifestEntry{
		RunID:      cell.RunID,
		Task:       cell.Task.ID,
		Variant:    cell.Variant.ID,
		Rep:        cell.Rep,
		BasketHash: hash,
		Marked:     marked,
		SessionID:  "s-" + cell.RunID,
	}
}

const twoVariantBasket = "basket: bord\nreps: 1\ntasks:\n  - id: t1\n    cmd: [\"claude\"]\nvariants:\n  - id: v1\n  - id: v2\n"

func TestRunBenchCellsRecordsMarkedAndEpilogue(t *testing.T) {
	basket, hash, cells := loadBasket(t, twoVariantBasket)
	manifest := filepath.Join(t.TempDir(), "m.jsonl")
	f := benchFlags{manifest: manifest, runsDir: "/runs"}

	var out bytes.Buffer
	require.NoError(t, runBenchCells(&out, "b.yaml", basket, cells, hash, f, markedCellFn(hash)))

	entries := readManifest(t, manifest)
	require.Len(t, entries, 2)
	assert.Equal(t, "bench-bord-t1-v1-r1", entries[0].RunID)
	assert.Equal(t, "bench-bord-t1-v2-r1", entries[1].RunID)
	assert.Contains(t, out.String(), "marked 2/2 cells")
	assert.Contains(t, out.String(), "catacomb regress --runs-dir /runs --baseline label:basket=bord,variant=v1 --candidate label:basket=bord,variant=v2")
}

func TestRunBenchCellsDefaultManifestPath(t *testing.T) {
	basket, hash, cells := loadBasket(t, "basket: bdef\nreps: 1\ntasks:\n  - id: t1\n    cmd: [\"claude\"]\nvariants:\n  - id: v1\n")
	basketPath := filepath.Join(t.TempDir(), "basket.yaml")

	require.NoError(t, runBenchCells(io.Discard, basketPath, basket, cells, hash, benchFlags{}, markedCellFn(hash)))
	entries := readManifest(t, basketPath+".manifest.jsonl")
	require.Len(t, entries, 1)
	assert.Equal(t, "bench-bdef-t1-v1-r1", entries[0].RunID)
}

func TestRunBenchCellsRerunWithoutResumeRefused(t *testing.T) {
	basket, hash, cells := loadBasket(t, twoVariantBasket)
	manifest := filepath.Join(t.TempDir(), "m.jsonl")
	require.NoError(t, runBenchCells(io.Discard, "b.yaml", basket, cells, hash, benchFlags{manifest: manifest}, markedCellFn(hash)))

	err := runBenchCells(io.Discard, "b.yaml", basket, cells, hash, benchFlags{manifest: manifest}, markedCellFn(hash))
	require.ErrorIs(t, err, errBenchRerun)
}

func TestRunBenchCellsResumeSkipsCompleted(t *testing.T) {
	basket, hash, cells := loadBasket(t, twoVariantBasket)
	manifest := filepath.Join(t.TempDir(), "m.jsonl")
	done := bench.ManifestEntry{RunID: "bench-bord-t1-v1-r1", Task: "t1", Variant: "v1", Rep: 1, BasketHash: hash}
	raw, _ := json.Marshal(done)
	require.NoError(t, os.WriteFile(manifest, append(raw, '\n'), 0o600))

	var out bytes.Buffer
	require.NoError(t, runBenchCells(&out, "b.yaml", basket, cells, hash, benchFlags{manifest: manifest, resume: true}, markedCellFn(hash)))
	assert.Contains(t, out.String(), "skip bench-bord-t1-v1-r1 (already completed)")
	entries := readManifest(t, manifest)
	require.Len(t, entries, 2)
	assert.Equal(t, "bench-bord-t1-v2-r1", entries[1].RunID)
}

func TestRunBenchCellsResumeAllSkippedOmitsMarked(t *testing.T) {
	basket, hash, cells := loadBasket(t, "basket: ball\nreps: 1\ntasks:\n  - id: t1\n    cmd: [\"claude\"]\n    checkpoints: [plan]\nvariants:\n  - id: v1\n")
	manifest := filepath.Join(t.TempDir(), "m.jsonl")
	done := bench.ManifestEntry{RunID: "bench-ball-t1-v1-r1", Task: "t1", Variant: "v1", Rep: 1, BasketHash: hash}
	raw, _ := json.Marshal(done)
	require.NoError(t, os.WriteFile(manifest, append(raw, '\n'), 0o600))

	var out bytes.Buffer
	require.NoError(t, runBenchCells(&out, "b.yaml", basket, cells, hash, benchFlags{manifest: manifest, resume: true}, markedCellFn(hash)))
	assert.Contains(t, out.String(), "skip bench-ball-t1-v1-r1")
	assert.NotContains(t, out.String(), "marked")
	assert.NotContains(t, out.String(), "checkpoints[")
}

func TestRunBenchCellsResumeHashMismatch(t *testing.T) {
	basket, hash, cells := loadBasket(t, twoVariantBasket)
	manifest := filepath.Join(t.TempDir(), "m.jsonl")
	done := bench.ManifestEntry{RunID: "bench-bord-t1-v1-r1", Task: "t1", Variant: "v1", Rep: 1, BasketHash: "deadbeef"}
	raw, _ := json.Marshal(done)
	require.NoError(t, os.WriteFile(manifest, append(raw, '\n'), 0o600))

	err := runBenchCells(io.Discard, "b.yaml", basket, cells, hash, benchFlags{manifest: manifest, resume: true}, markedCellFn(hash))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "delete the manifest")
}

func TestRunBenchCellsFailFastStops(t *testing.T) {
	basket, hash, cells := loadBasket(t, twoVariantBasket)
	manifest := filepath.Join(t.TempDir(), "m.jsonl")
	failFn := func(cell bench.Cell, _ map[string]string) (bench.ManifestEntry, bool, bool) {
		e := manifestFor(cell, hash, false)
		e.ExitCode = 5
		return e, true, false
	}
	err := runBenchCells(io.Discard, "b.yaml", basket, cells, hash, benchFlags{manifest: manifest, failFast: true}, failFn)
	require.ErrorIs(t, err, errBenchFailFast)
	entries := readManifest(t, manifest)
	require.Len(t, entries, 1)
	assert.Equal(t, "bench-bord-t1-v1-r1", entries[0].RunID)
}

func TestRunBenchCellsCompletedReadError(t *testing.T) {
	basket, hash, cells := loadBasket(t, twoVariantBasket)
	manifestDir := filepath.Join(t.TempDir(), "manifest-is-a-dir")
	require.NoError(t, os.Mkdir(manifestDir, 0o755))

	err := runBenchCells(io.Discard, "b.yaml", basket, cells, hash, benchFlags{manifest: manifestDir, resume: true}, markedCellFn(hash))
	require.Error(t, err)
	var opErr *operationalError
	require.ErrorAs(t, err, &opErr)
}

func TestRunBenchCellsManifestAppendError(t *testing.T) {
	basket, hash, cells := loadBasket(t, twoVariantBasket)
	manifest := filepath.Join(t.TempDir(), "no-such-dir", "m.jsonl")

	err := runBenchCells(io.Discard, "b.yaml", basket, cells, hash, benchFlags{manifest: manifest}, markedCellFn(hash))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "manifest")
}

func TestRunBenchCellsCheckpointSummary(t *testing.T) {
	basket, hash, cells := loadBasket(t, "basket: bcp\nreps: 1\ntasks:\n  - id: t1\n    cmd: [\"claude\"]\n    checkpoints: [plan, tests.pass]\n  - id: t2\n    cmd: [\"claude\"]\nvariants:\n  - id: v1\n")
	manifest := filepath.Join(t.TempDir(), "m.jsonl")
	cpFn := func(cell bench.Cell, _ map[string]string) (bench.ManifestEntry, bool, bool) {
		e := manifestFor(cell, hash, true)
		if cell.Task.ID == "t1" {
			e.MissingCheckpoints = []string{"tests.pass"}
		}
		return e, false, true
	}
	var out bytes.Buffer
	require.NoError(t, runBenchCells(&out, "b.yaml", basket, cells, hash, benchFlags{manifest: manifest}, cpFn))
	assert.Contains(t, out.String(), "checkpoints[t1]: plan 1/1")
	assert.Contains(t, out.String(), "checkpoints[t1]: tests.pass 0/1")
	assert.NotContains(t, out.String(), "checkpoints[t2]")
}

func TestAppendNote(t *testing.T) {
	assert.Equal(t, "first", appendNote("", "first"))
	assert.Equal(t, "first; second", appendNote("first", "second"))
}

func TestSpawnFailure(t *testing.T) {
	assert.Empty(t, spawnFailure(nil))
	cmd := execCommand(os.Args[0], "-test.run=TestSpawnFailureExitHelper")
	t.Setenv("GO_SPAWN_FAILURE_EXIT", "1")
	assert.Empty(t, spawnFailure(cmd.Run()))
	assert.Contains(t, spawnFailure(assertErr("boom")), "spawn failed: boom")
}

type assertErr string

func (e assertErr) Error() string { return string(e) }

func TestSpawnFailureExitHelper(t *testing.T) {
	t.Helper()
	if os.Getenv("GO_SPAWN_FAILURE_EXIT") == "1" {
		os.Exit(2)
	}
}

func TestRunBenchDryRunPrintsTable(t *testing.T) {
	basket := writeBasket(t, "basket: bdry\nreps: 1\ntasks:\n  - id: t1\n    cmd: [\"claude\"]\nvariants:\n  - id: v1\n")
	var out, errBuf bytes.Buffer
	require.NoError(t, runBench(&out, &errBuf, basket, benchFlags{dryRun: true}))
	for _, want := range []string{"RUN_ID", "TASK", "VARIANT", "REP", "bench-bdry-t1-v1-r1"} {
		assert.Contains(t, out.String(), want)
	}
	_, statErr := os.Stat(basket + ".manifest.jsonl")
	assert.True(t, os.IsNotExist(statErr))
}

func TestRunBenchBadBasketIsOperational(t *testing.T) {
	var out, errBuf bytes.Buffer
	code := run([]string{"bench", filepath.Join(t.TempDir(), "missing.yaml")}, &out, &errBuf)
	assert.Equal(t, 2, code)
}

func TestBenchCLIRunsOfflineByDefault(t *testing.T) {
	projects := t.TempDir()
	runs := t.TempDir()
	stubBenchChild(t, "HELPER_SESSION=sess-cli", "HELPER_PROJECTS="+projects, "HELPER_FIXTURE="+fixturePath(t))
	basket := writeBasket(t, "basket: bcli\nreps: 1\ntasks:\n  - id: t1\n    cmd: [\"sess-cli\"]\nvariants:\n  - id: base\n")
	manifest := filepath.Join(t.TempDir(), "m.jsonl")

	var out, errBuf bytes.Buffer
	code := run([]string{"bench", basket, "--projects-dir", projects, "--runs-dir", runs, "--manifest", manifest}, &out, &errBuf)
	require.Equal(t, 0, code, errBuf.String())
	entries := readManifest(t, manifest)
	require.Len(t, entries, 1)
	assert.Equal(t, "bench-bcli-t1-base-r1", entries[0].RunID)
	assert.NotEmpty(t, entries[0].EvidenceDir)
}

func TestBenchRejectsRemovedOfflineFlag(t *testing.T) {
	basket := writeBasket(t, "basket: b\nreps: 1\ntasks:\n  - id: t1\n    cmd: [\"x\"]\nvariants:\n  - id: v1\n")
	var out, errBuf bytes.Buffer
	code := run([]string{"bench", basket, "--offline"}, &out, &errBuf)
	assert.Equal(t, 1, code)
	assert.Contains(t, errBuf.String(), "unknown flag")
}

func TestBenchCmdWired(t *testing.T) {
	root := newRootCmd()
	names := make(map[string]bool)
	for _, sub := range root.Commands() {
		names[sub.Name()] = true
	}
	assert.True(t, names["bench"])
}

func TestBenchNoOfflineFlag(t *testing.T) {
	cmd := newBenchCmd()
	assert.Nil(t, cmd.Flags().Lookup("offline"))
	rd := cmd.Flags().Lookup("runs-dir")
	require.NotNil(t, rd)
	assert.True(t, strings.HasSuffix(rd.DefValue, filepath.Join(".catacomb", "runs")) || rd.DefValue == "")
}
