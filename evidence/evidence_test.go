package evidence_test

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/evidence"
	"github.com/realkarych/catacomb/redact"
)

func sampleMeta(runID, variant string) evidence.Meta {
	return evidence.Meta{
		RunID: runID, Task: "t1", Variant: variant, Rep: 1,
		SessionID: "sess-1", Labels: map[string]string{"basket": "b", "variant": variant},
		ExitCode: 0, BasketHash: "h", MarkerName: "task:t1",
		MarkerStart: time.Unix(100, 0).UTC(), MarkerEnd: time.Unix(200, 0).UTC(),
		FinishedAt: time.Unix(201, 0).UTC(),
	}
}

func TestWriteReadRoundtrip(t *testing.T) {
	src := filepath.Join(t.TempDir(), "in.jsonl")
	require.NoError(t, os.WriteFile(src, []byte("{\"a\":1}\n{\"b\":2}\n"), 0o600))
	dir := filepath.Join(t.TempDir(), "run-1")
	m := sampleMeta("run-1", "base")
	require.NoError(t, evidence.Write(dir, m, []evidence.SourceFile{{Src: src, Rel: "session.jsonl"}}))
	got, err := evidence.ReadMeta(dir)
	require.NoError(t, err)
	require.Equal(t, m, got)
	copied, err := os.ReadFile(filepath.Join(dir, "session.jsonl"))
	require.NoError(t, err)
	require.Equal(t, string(redact.Redact([]byte("{\"a\":1}")).Data)+"\n"+string(redact.Redact([]byte("{\"b\":2}")).Data)+"\n", string(copied))
}

func TestWriteNestedRelAndErrors(t *testing.T) {
	src := filepath.Join(t.TempDir(), "agent.jsonl")
	require.NoError(t, os.WriteFile(src, []byte("{}\n"), 0o600))
	dir := filepath.Join(t.TempDir(), "run-2")
	require.NoError(t, evidence.Write(dir, sampleMeta("run-2", "base"), []evidence.SourceFile{{Src: src, Rel: filepath.Join("subagents", "agent-1.jsonl")}}))
	_, err := os.Stat(filepath.Join(dir, "subagents", "agent-1.jsonl"))
	require.NoError(t, err)
	require.Error(t, evidence.Write(filepath.Join(t.TempDir(), "run-3"), sampleMeta("run-3", "base"), []evidence.SourceFile{{Src: filepath.Join(t.TempDir(), "missing.jsonl"), Rel: "session.jsonl"}}))
}

func TestWriteRejectsNonLocalRel(t *testing.T) {
	src := filepath.Join(t.TempDir(), "in.jsonl")
	require.NoError(t, os.WriteFile(src, []byte("{}\n"), 0o600))
	dir := filepath.Join(t.TempDir(), "run-x")
	sentinel := filepath.Join(dir, "keep.txt")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(sentinel, []byte("x"), 0o600))
	abs := filepath.Join(t.TempDir(), "abs.jsonl")
	for _, rel := range []string{filepath.Join("..", "x"), abs} {
		err := evidence.Write(dir, sampleMeta("run-x", "base"), []evidence.SourceFile{{Src: src, Rel: rel}})
		require.Error(t, err)
		require.Contains(t, err.Error(), "rel")
		require.Contains(t, err.Error(), strconv.Quote(rel))
		_, serr := os.Stat(sentinel)
		require.NoError(t, serr, "target dir must stay untouched")
	}
}

func TestWriteRemovesStaleFiles(t *testing.T) {
	src := filepath.Join(t.TempDir(), "in.jsonl")
	require.NoError(t, os.WriteFile(src, []byte("{}\n"), 0o600))
	dir := filepath.Join(t.TempDir(), "run-1")
	require.NoError(t, evidence.Write(dir, sampleMeta("run-1", "base"), []evidence.SourceFile{
		{Src: src, Rel: "session.jsonl"},
		{Src: src, Rel: filepath.Join("subagents", "agent-1.jsonl")},
	}))
	require.NoError(t, evidence.Write(dir, sampleMeta("run-1", "base"), []evidence.SourceFile{
		{Src: src, Rel: "session.jsonl"},
	}))
	_, err := os.Stat(filepath.Join(dir, "subagents", "agent-1.jsonl"))
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(filepath.Join(dir, "session.jsonl"))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(dir, "meta.json"))
	require.NoError(t, err)
}

func TestReadMetaErrors(t *testing.T) {
	_, err := evidence.ReadMeta(t.TempDir())
	require.Error(t, err)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "meta.json"), []byte("{"), 0o600))
	_, err = evidence.ReadMeta(dir)
	require.Error(t, err)
}

func TestScanRunsAndMatchLabels(t *testing.T) {
	root := t.TempDir()
	for _, id := range []string{"run-b", "run-a"} {
		require.NoError(t, evidence.Write(filepath.Join(root, id), sampleMeta(id, "base"), nil))
	}
	require.NoError(t, os.MkdirAll(filepath.Join(root, "junk"), 0o700))
	runs, err := evidence.ScanRuns(root)
	require.NoError(t, err)
	require.Len(t, runs, 2)
	require.Equal(t, "run-a", runs[0].Meta.RunID)
	require.True(t, evidence.MatchLabels(map[string]string{"a": "1", "b": "2"}, map[string]string{"a": "1"}))
	require.False(t, evidence.MatchLabels(map[string]string{"a": "1"}, map[string]string{"a": "2"}))
	_, err = evidence.ScanRuns(filepath.Join(root, "absent"))
	require.Error(t, err)
}

func TestScanRunsSkipsPlainFiles(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "stray.txt"), []byte("x"), 0o600))
	runs, err := evidence.ScanRuns(root)
	require.NoError(t, err)
	require.Empty(t, runs)
}

func TestWriteReplaceDirError(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o600))
	require.Error(t, evidence.Write(filepath.Join(blocker, "run-x"), sampleMeta("run-x", "base"), nil))
}

func TestWriteMarshalError(t *testing.T) {
	m := sampleMeta("run-x", "base")
	m.FinishedAt = time.Date(10001, 1, 1, 0, 0, 0, 0, time.UTC)
	require.Error(t, evidence.Write(filepath.Join(t.TempDir(), "run-x"), m, nil))
}

func TestWriteMetaFileError(t *testing.T) {
	src := filepath.Join(t.TempDir(), "in.jsonl")
	require.NoError(t, os.WriteFile(src, []byte("{}\n"), 0o600))
	dir := filepath.Join(t.TempDir(), "run-x")
	require.Error(t, evidence.Write(dir, sampleMeta("run-x", "alt"), []evidence.SourceFile{{Src: src, Rel: filepath.Join("meta.json", "x.jsonl")}}))
}

func TestWriteCopyDestErrors(t *testing.T) {
	src := filepath.Join(t.TempDir(), "in.jsonl")
	require.NoError(t, os.WriteFile(src, []byte("{}\n"), 0o600))
	dir := filepath.Join(t.TempDir(), "run-x")
	require.Error(t, evidence.Write(dir, sampleMeta("run-x", "base"), []evidence.SourceFile{
		{Src: src, Rel: "sub"},
		{Src: src, Rel: filepath.Join("sub", "a.jsonl")},
	}))
	require.Error(t, evidence.Write(dir, sampleMeta("run-x", "base"), []evidence.SourceFile{
		{Src: src, Rel: filepath.Join("taken", "x.jsonl")},
		{Src: src, Rel: "taken"},
	}))
}

func TestWriteSrcReadError(t *testing.T) {
	srcDir := t.TempDir()
	dir := filepath.Join(t.TempDir(), "run-x")
	require.Error(t, evidence.Write(dir, sampleMeta("run-x", "base"), []evidence.SourceFile{{Src: srcDir, Rel: "session.jsonl"}}))
}
