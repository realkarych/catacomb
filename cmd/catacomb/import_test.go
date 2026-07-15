package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/evidence"
)

func writeImportBasket(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "basket.yaml")
	require.NoError(t, os.WriteFile(p, []byte(`basket: checkout
reps: 1
tasks:
  - id: add-item
    cmd: ["claude", "-p", "add an item", "--output-format", "stream-json"]
    checkpoints: ["phase:cart"]
variants:
  - id: trunk
  - id: patched
`), 0o600))
	return p
}

func TestImportRequiresSessionXorTranscript(t *testing.T) {
	dir := t.TempDir()
	basket := writeImportBasket(t, dir)
	var out, errb bytes.Buffer
	err := runImport(context.Background(), &out, &errb, basket, importFlags{
		task: "add-item", variant: "trunk", rep: 1, runsDir: dir, projectsDir: dir,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "session-id")
}

func TestImportRejectsBothInputs(t *testing.T) {
	dir := t.TempDir()
	basket := writeImportBasket(t, dir)
	var out, errb bytes.Buffer
	err := runImport(context.Background(), &out, &errb, basket, importFlags{
		task: "add-item", variant: "trunk", rep: 1, sessionID: "s1", transcript: "x.jsonl", runsDir: dir, projectsDir: dir,
	})
	require.Error(t, err)
}

func TestImportUnknownTask(t *testing.T) {
	dir := t.TempDir()
	basket := writeImportBasket(t, dir)
	var out, errb bytes.Buffer
	err := runImport(context.Background(), &out, &errb, basket, importFlags{
		task: "nope", variant: "trunk", rep: 1, sessionID: "s1", runsDir: dir, projectsDir: dir,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "task")
}

func TestImportUnknownVariant(t *testing.T) {
	dir := t.TempDir()
	basket := writeImportBasket(t, dir)
	var out, errb bytes.Buffer
	err := runImport(context.Background(), &out, &errb, basket, importFlags{
		task: "add-item", variant: "nope", rep: 1, sessionID: "s1", runsDir: dir, projectsDir: dir,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "variant")
}

func TestImportBadBasket(t *testing.T) {
	dir := t.TempDir()
	var out, errb bytes.Buffer
	err := runImport(context.Background(), &out, &errb, filepath.Join(dir, "missing.yaml"), importFlags{
		task: "add-item", variant: "trunk", rep: 1, sessionID: "s1", runsDir: dir, projectsDir: dir,
	})
	require.Error(t, err)
}

func TestImportCommandSessionNotFound(t *testing.T) {
	dir := t.TempDir()
	basket := writeImportBasket(t, dir)
	var stdout, stderr bytes.Buffer
	code := run([]string{
		"import", basket,
		"--task", "add-item", "--variant", "trunk", "--session-id", "s1",
		"--runs-dir", dir, "--projects-dir", dir,
	}, &stdout, &stderr)
	require.Equal(t, 2, code, stderr.String())
	assert.Contains(t, stderr.String(), "no transcript for session s1")
}

func stageTranscript(t *testing.T, projects, sid string) {
	t.Helper()
	dst := filepath.Join(projects, "proj", sid+".jsonl")
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
	data, err := os.ReadFile("testdata/session.jsonl")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(dst, data, 0o600))
}

func TestImportTranscriptsBySessionID(t *testing.T) {
	projects := t.TempDir()
	stageTranscript(t, projects, "sess-123")
	ts, sid, err := importTranscripts(importFlags{sessionID: "sess-123", projectsDir: projects})
	require.NoError(t, err)
	assert.Equal(t, "sess-123", sid)
	assert.Contains(t, ts.Main, "sess-123.jsonl")
}

func TestImportTranscriptsBySessionIDNotFound(t *testing.T) {
	projects := t.TempDir()
	_, _, err := importTranscripts(importFlags{sessionID: "missing", projectsDir: projects})
	require.Error(t, err)
}

func TestImportTranscriptsByPath(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "sess-abc.jsonl")
	data, err := os.ReadFile("testdata/session.jsonl")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(main, data, 0o600))
	ts, sid, err := importTranscripts(importFlags{transcript: main})
	require.NoError(t, err)
	assert.Equal(t, "sess-abc", sid)
	assert.Equal(t, main, ts.Main)
}

func TestImportTranscriptsByPathMissing(t *testing.T) {
	_, _, err := importTranscripts(importFlags{transcript: filepath.Join(t.TempDir(), "nope.jsonl")})
	require.Error(t, err)
}

func TestImportTranscriptsByPathBadSubagentGlob(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "bad[.jsonl")
	require.NoError(t, os.WriteFile(main, []byte("{}\n"), 0o600))
	_, _, err := importTranscripts(importFlags{transcript: main})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "subagents")
}

func TestImportWritesEvidence(t *testing.T) {
	dir := t.TempDir()
	basket := writeImportBasket(t, dir)
	projects := filepath.Join(dir, "projects")
	stageTranscript(t, projects, "sess-xyz")
	runs := filepath.Join(dir, "runs")
	var out, errb bytes.Buffer
	err := runImport(context.Background(), &out, &errb, basket, importFlags{
		task: "add-item", variant: "trunk", rep: 1, sessionID: "sess-xyz",
		projectsDir: projects, runsDir: runs,
	})
	require.NoError(t, err)
	metaPath := filepath.Join(runs, "import-checkout-add-item-trunk-r1", "meta.json")
	require.FileExists(t, metaPath)
	require.FileExists(t, filepath.Join(runs, "import-checkout-add-item-trunk-r1", "session.jsonl"))
}

func TestImportMetaShape(t *testing.T) {
	dir := t.TempDir()
	basket := writeImportBasket(t, dir)
	projects := filepath.Join(dir, "projects")
	stageTranscript(t, projects, "sess-xyz")
	runs := filepath.Join(dir, "runs")
	var out, errb bytes.Buffer
	require.NoError(t, runImport(context.Background(), &out, &errb, basket, importFlags{
		task: "add-item", variant: "patched", rep: 2, sessionID: "sess-xyz",
		projectsDir: projects, runsDir: runs,
	}))
	m, err := evidence.ReadMeta(filepath.Join(runs, "import-checkout-add-item-patched-r2"))
	require.NoError(t, err)
	assert.Equal(t, "task:add-item", m.MarkerName)
	assert.Equal(t, "patched", m.Labels["variant"])
	assert.Equal(t, "checkout", m.Labels["basket"])
	assert.Equal(t, "2", m.Labels["rep"])
	assert.Nil(t, m.CostUSD)
	assert.False(t, m.MarkerStart.After(m.MarkerEnd))
}

func TestImportRunIDOverride(t *testing.T) {
	dir := t.TempDir()
	basket := writeImportBasket(t, dir)
	projects := filepath.Join(dir, "projects")
	stageTranscript(t, projects, "sess-xyz")
	runs := filepath.Join(dir, "runs")
	var out, errb bytes.Buffer
	require.NoError(t, runImport(context.Background(), &out, &errb, basket, importFlags{
		task: "add-item", variant: "trunk", rep: 1, sessionID: "sess-xyz", runID: "manual-1",
		projectsDir: projects, runsDir: runs,
	}))
	require.FileExists(t, filepath.Join(runs, "manual-1", "meta.json"))
}

func TestImportWarnsMissingCheckpoint(t *testing.T) {
	dir := t.TempDir()
	basket := writeImportBasket(t, dir)
	projects := filepath.Join(dir, "projects")
	stageTranscript(t, projects, "sess-xyz")
	runs := filepath.Join(dir, "runs")
	var out, errb bytes.Buffer
	require.NoError(t, runImport(context.Background(), &out, &errb, basket, importFlags{
		task: "add-item", variant: "trunk", rep: 1, sessionID: "sess-xyz",
		projectsDir: projects, runsDir: runs,
	}))
	assert.Contains(t, errb.String(), "missing checkpoints")
	assert.Contains(t, errb.String(), "phase:cart")
}

func TestImportLabelsMergeAmbient(t *testing.T) {
	got := importLabels(importFlags{task: "t", variant: "v", rep: 3, labels: "env=ci,variant=SHOULD_LOSE"}, "b")
	assert.Equal(t, "v", got["variant"])
	assert.Equal(t, "ci", got["env"])
	assert.Equal(t, "3", got["rep"])
	assert.Equal(t, "b", got["basket"])
}

func TestImportParseError(t *testing.T) {
	dir := t.TempDir()
	basket := writeImportBasket(t, dir)
	notFile := filepath.Join(dir, "adir.jsonl")
	require.NoError(t, os.MkdirAll(notFile, 0o755))
	var out, errb bytes.Buffer
	err := runImport(context.Background(), &out, &errb, basket, importFlags{
		task: "add-item", variant: "trunk", rep: 1, transcript: notFile,
		runsDir: filepath.Join(dir, "runs"),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "import")
}

func TestImportNoTimestamps(t *testing.T) {
	dir := t.TempDir()
	basket := writeImportBasket(t, dir)
	main := filepath.Join(dir, "empty.jsonl")
	require.NoError(t, os.WriteFile(main, nil, 0o600))
	var out, errb bytes.Buffer
	err := runImport(context.Background(), &out, &errb, basket, importFlags{
		task: "add-item", variant: "trunk", rep: 1, transcript: main,
		runsDir: filepath.Join(dir, "runs"),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no timestamped records")
}

func TestImportEvidenceWriteError(t *testing.T) {
	dir := t.TempDir()
	basket := writeImportBasket(t, dir)
	projects := filepath.Join(dir, "projects")
	stageTranscript(t, projects, "sess-xyz")
	blocker := filepath.Join(dir, "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o600))
	var out, errb bytes.Buffer
	err := runImport(context.Background(), &out, &errb, basket, importFlags{
		task: "add-item", variant: "trunk", rep: 1, sessionID: "sess-xyz",
		projectsDir: projects, runsDir: filepath.Join(blocker, "runs"),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "evidence write")
}
