package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/evidence"
	"github.com/realkarych/catacomb/store"
)

func packRunIDs(n int) []string {
	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		ids = append(ids, fmt.Sprintf("r-%02d", i))
	}
	return ids
}

func TestStrideSample(t *testing.T) {
	cases := []struct {
		name string
		ids  []string
		n    int
		want []string
	}{
		{"five take one", packRunIDs(5), 1, []string{"r-00"}},
		{"five take three", packRunIDs(5), 3, []string{"r-00", "r-01", "r-03"}},
		{"fifteen take one", packRunIDs(15), 1, []string{"r-00"}},
		{"fifteen take three", packRunIDs(15), 3, []string{"r-00", "r-05", "r-10"}},
		{"four take three", packRunIDs(4), 3, []string{"r-00", "r-01", "r-02"}},
		{"exact length", packRunIDs(5), 5, packRunIDs(5)},
		{"over length", packRunIDs(5), 9, packRunIDs(5)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, strideSample(tc.ids, tc.n))
		})
	}
}

func TestPackFlagValidationExit2(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		name string
		args []string
		want string
	}{
		{
			"missing runs-dir",
			[]string{"pack", "label:variant=base", "--out", filepath.Join(root, "o1")},
			"--runs-dir is required",
		},
		{
			"missing out",
			[]string{"pack", "label:variant=base", "--runs-dir", root},
			"--out is required",
		},
		{
			"zero sample",
			[]string{"pack", "label:variant=base", "--runs-dir", root, "--out", filepath.Join(root, "o2"), "--sample", "0"},
			"--sample must be > 0, got 0",
		},
		{
			"negative sample",
			[]string{"pack", "label:variant=base", "--runs-dir", root, "--out", filepath.Join(root, "o3"), "--sample", "-2"},
			"--sample must be > 0, got -2",
		},
		{
			"bad selector",
			[]string{"pack", "bogus", "--runs-dir", root, "--out", filepath.Join(root, "o4")},
			"invalid selector",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(tc.args, &stdout, &stderr)
			assert.Equal(t, 2, code)
			assert.Contains(t, stderr.String(), tc.want)
		})
	}
}

func TestPackMissingSelectorArgExit1(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"pack"}, &stdout, &stderr)
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr.String(), "accepts 1 arg")
}

func TestPackEmptySelectionExit2(t *testing.T) {
	root := evidenceRoot(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{"pack", "label:variant=none", "--runs-dir", root, "--out", filepath.Join(t.TempDir(), "pack")}, &stdout, &stderr)
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr.String(), `pack selector "label:variant=none": selector matched no runs`)
}

func TestPackExistingOutDirExit2(t *testing.T) {
	root := evidenceRoot(t)
	out := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run([]string{"pack", "label:variant=base", "--runs-dir", root, "--out", out}, &stdout, &stderr)
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr.String(), "already exists")
	entries, err := os.ReadDir(out)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestPackOutParentMissingExit2(t *testing.T) {
	root := evidenceRoot(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{"pack", "label:variant=base", "--runs-dir", root, "--out", filepath.Join(t.TempDir(), "a", "b")}, &stdout, &stderr)
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr.String(), "create out dir")
}

func TestPackManifestAndInstructions(t *testing.T) {
	root := t.TempDir()
	for _, id := range packRunIDs(5) {
		writeEvidenceRun(t, root, id, "base", "session.jsonl")
	}
	out := filepath.Join(t.TempDir(), "pack")
	var stdout, stderr bytes.Buffer
	code := run([]string{"pack", "label:variant=base", "--runs-dir", root, "--out", out, "--sample", "3"}, &stdout, &stderr)
	require.Equal(t, 0, code, stderr.String())
	assert.Equal(t, fmt.Sprintf("packed 3 of 5 runs into %s\n", out), stdout.String())

	raw, err := os.ReadFile(filepath.Join(out, "pack.json"))
	require.NoError(t, err)
	assert.Contains(t, string(raw), "\n  \"selector\"")
	for _, key := range []string{`"selector"`, `"runs_dir"`, `"sample_rule"`, `"requested"`, `"runs"`, `"created_at"`} {
		assert.Contains(t, string(raw), key)
	}
	var m PackManifest
	require.NoError(t, json.Unmarshal(raw, &m))
	assert.Equal(t, "label:variant=base", m.Selector)
	assert.Equal(t, root, m.RunsDir)
	assert.Equal(t, "runid-stride", m.SampleRule)
	assert.Equal(t, 3, m.Requested)
	assert.Equal(t, []string{"r-00", "r-01", "r-03"}, m.Runs)
	assert.False(t, m.CreatedAt.IsZero())

	instr, err := os.ReadFile(filepath.Join(out, "INSTRUCTIONS.md"))
	require.NoError(t, err)
	assert.Contains(t, string(instr),
		`{"key":"audit.clean","value":1,"run_id":"<run id>","tool":"<judge name>","tool_version":"<version>"}`)
	assert.Contains(t, string(instr), "regress --scores findings.jsonl --annotation audit.clean:higher-better")
	assert.Contains(t, string(instr), "catacomb-judge agreement")
	assert.Contains(t, string(instr), "catacomb-judge panel")
	assert.Contains(t, string(instr), "prompt_hash")
	assert.Contains(t, string(instr), "panel skips lines without tool")

	entries, err := os.ReadDir(out)
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	assert.ElementsMatch(t, []string{"pack.json", "INSTRUCTIONS.md", "r-00", "r-01", "r-03"}, names)
}

func TestPackStrideOverFifteenRuns(t *testing.T) {
	root := t.TempDir()
	for _, id := range packRunIDs(15) {
		writeEvidenceRun(t, root, id, "base", "session.jsonl")
	}
	cases := []struct {
		name   string
		sample string
		want   []string
	}{
		{"take three", "3", []string{"r-00", "r-05", "r-10"}},
		{"take one", "1", []string{"r-00"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "pack")
			var stdout, stderr bytes.Buffer
			code := run([]string{"pack", "label:variant=base", "--runs-dir", root, "--out", out, "--sample", tc.sample}, &stdout, &stderr)
			require.Equal(t, 0, code, stderr.String())
			var m PackManifest
			raw, err := os.ReadFile(filepath.Join(out, "pack.json"))
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(raw, &m))
			assert.Equal(t, tc.want, m.Runs)
		})
	}
}

func TestPackNameSelector(t *testing.T) {
	root := evidenceRoot(t)
	dbPath := filepath.Join(t.TempDir(), "b.db")
	require.NoError(t, runBaselineSet(io.Discard, store.OpenSQLite, dbPath, "golden", []string{"variant=base"}, root))
	out := filepath.Join(t.TempDir(), "pack")
	var stdout, stderr bytes.Buffer
	code := run([]string{"pack", "name:golden", "--runs-dir", root, "--db", dbPath, "--out", out}, &stdout, &stderr)
	require.Equal(t, 0, code, stderr.String())
	var m PackManifest
	raw, err := os.ReadFile(filepath.Join(out, "pack.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &m))
	assert.Equal(t, "name:golden", m.Selector)
	assert.Equal(t, 3, m.Requested)
	assert.Equal(t, []string{"base-0", "base-1"}, m.Runs)
}

func writePackEvidenceRun(t *testing.T, root, id, variant string) {
	t.Helper()
	scores := filepath.Join(t.TempDir(), "scores.jsonl")
	require.NoError(t, os.WriteFile(scores, []byte(fmt.Sprintf(`{"key":"verifier.pass","value":1,"run_id":%q}`+"\n", id)), 0o600))
	verify := filepath.Join(t.TempDir(), "verify.json")
	require.NoError(t, os.WriteFile(verify, []byte(`{"cmd":["true"],"exit_code":0}`+"\n"), 0o600))
	artifact := filepath.Join(t.TempDir(), "report.txt")
	require.NoError(t, os.WriteFile(artifact, []byte("all good\n"), 0o600))
	m := evidence.Meta{
		RunID:       id,
		Task:        "t1",
		Variant:     variant,
		Rep:         1,
		SessionID:   "s1",
		Labels:      map[string]string{"variant": variant},
		MarkerName:  "task:t1",
		MarkerStart: time.Unix(100, 0).UTC(),
		MarkerEnd:   time.Unix(200, 0).UTC(),
		FinishedAt:  time.Unix(201, 0).UTC(),
	}
	src := filepath.Join("testdata", "session.jsonl")
	require.NoError(t, evidence.Write(filepath.Join(root, id), m, []evidence.SourceFile{
		{Src: src, Rel: "session.jsonl"},
		{Src: src, Rel: "subagents/agent-001.jsonl"},
		{Src: scores, Rel: "scores.jsonl"},
		{Src: verify, Rel: "verify.json"},
		{Src: artifact, Rel: "artifacts/report.txt"},
	}))
}

func dirFileMap(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	require.NoError(t, filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		require.NoError(t, err)
		if d.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		require.NoError(t, rerr)
		data, derr := os.ReadFile(path)
		require.NoError(t, derr)
		files[filepath.ToSlash(rel)] = string(data)
		return nil
	}))
	return files
}

func TestPackBundleCompleteness(t *testing.T) {
	root := t.TempDir()
	writePackEvidenceRun(t, root, "run-a", "base")
	writePackEvidenceRun(t, root, "run-b", "base")
	out := filepath.Join(t.TempDir(), "pack")
	var stdout, stderr bytes.Buffer
	code := run([]string{"pack", "label:variant=base", "--runs-dir", root, "--out", out, "--sample", "5"}, &stdout, &stderr)
	require.Equal(t, 0, code, stderr.String())
	assert.Equal(t, fmt.Sprintf("packed 2 of 2 runs into %s\n", out), stdout.String())
	for _, id := range []string{"run-a", "run-b"} {
		src := dirFileMap(t, filepath.Join(root, id))
		require.Len(t, src, 6)
		require.Contains(t, src, "subagents/agent-001.jsonl")
		require.Contains(t, src, "artifacts/report.txt")
		assert.Equal(t, src, dirFileMap(t, filepath.Join(out, id)))
	}
}

func TestPackSymlinkRefusedExit2(t *testing.T) {
	root := t.TempDir()
	writeEvidenceRun(t, root, "run-a", "base", "session.jsonl")
	link := filepath.Join(root, "run-a", "loop.jsonl")
	if err := os.Symlink(filepath.Join(root, "run-a", "session.jsonl"), link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"pack", "label:variant=base", "--runs-dir", root, "--out", filepath.Join(t.TempDir(), "pack")}, &stdout, &stderr)
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr.String(), "symlink")
}

func TestPackRunDirNameMismatchExit2(t *testing.T) {
	root := t.TempDir()
	m := evidence.Meta{
		RunID:       "other",
		SessionID:   "s1",
		Labels:      map[string]string{"variant": "base"},
		MarkerName:  "task:t1",
		MarkerStart: time.Unix(100, 0).UTC(),
		MarkerEnd:   time.Unix(200, 0).UTC(),
	}
	src := filepath.Join("testdata", "session.jsonl")
	require.NoError(t, evidence.Write(filepath.Join(root, "dir-a"), m, []evidence.SourceFile{{Src: src, Rel: "session.jsonl"}}))
	var stdout, stderr bytes.Buffer
	code := run([]string{"pack", "label:variant=base", "--runs-dir", root, "--out", filepath.Join(t.TempDir(), "pack")}, &stdout, &stderr)
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr.String(), `copy run "other"`)
}

func TestPackRunIDEscapesExit2(t *testing.T) {
	root := t.TempDir()
	m := evidence.Meta{
		RunID:       "../esc",
		SessionID:   "s1",
		Labels:      map[string]string{"variant": "base"},
		MarkerName:  "task:t1",
		MarkerStart: time.Unix(100, 0).UTC(),
		MarkerEnd:   time.Unix(200, 0).UTC(),
	}
	src := filepath.Join("testdata", "session.jsonl")
	require.NoError(t, evidence.Write(filepath.Join(root, "esc"), m, []evidence.SourceFile{{Src: src, Rel: "session.jsonl"}}))
	var stdout, stderr bytes.Buffer
	code := run([]string{"pack", "label:variant=base", "--runs-dir", root, "--out", filepath.Join(t.TempDir(), "pack")}, &stdout, &stderr)
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr.String(), "escapes the bundle dir")
}

func TestPackReservedRunIDCollisionsExit2(t *testing.T) {
	cases := []struct {
		id   string
		want string
	}{
		{"pack.json", "write pack.json"},
		{"INSTRUCTIONS.md", "write INSTRUCTIONS.md"},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			root := t.TempDir()
			writeEvidenceRun(t, root, tc.id, "base", "session.jsonl")
			var stdout, stderr bytes.Buffer
			code := run([]string{"pack", "label:variant=base", "--runs-dir", root, "--out", filepath.Join(t.TempDir(), "pack")}, &stdout, &stderr)
			assert.Equal(t, 2, code)
			assert.Contains(t, stderr.String(), tc.want)
		})
	}
}

func TestWritePackManifestMarshalError(t *testing.T) {
	err := writePackManifest(t.TempDir(), PackManifest{CreatedAt: time.Date(10001, 1, 1, 0, 0, 0, 0, time.UTC)})
	require.Error(t, err)
}

func TestWritePackManifestWriteError(t *testing.T) {
	err := writePackManifest(filepath.Join(t.TempDir(), "missing"), PackManifest{CreatedAt: time.Unix(1, 0).UTC()})
	require.Error(t, err)
}

func TestCopyFileErrors(t *testing.T) {
	tmp := t.TempDir()
	require.Error(t, copyFile(filepath.Join(tmp, "absent"), filepath.Join(tmp, "dst")))
	src := filepath.Join(tmp, "src")
	require.NoError(t, os.WriteFile(src, []byte("x"), 0o600))
	require.Error(t, copyFile(src, filepath.Join(tmp, "missing", "dst")))
}

func TestCopyEvidenceEntryEscapes(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "evil.txt"), []byte("x"), 0o600))
	entries, err := os.ReadDir(tmp)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	err = copyEvidenceEntry(filepath.Join(tmp, "sub"), t.TempDir(), filepath.Join(tmp, "evil.txt"), entries[0])
	require.Error(t, err)
	assert.Contains(t, err.Error(), "escapes evidence dir")
}
