package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestImportCommandReachesStub(t *testing.T) {
	dir := t.TempDir()
	basket := writeImportBasket(t, dir)
	var stdout, stderr bytes.Buffer
	code := run([]string{
		"import", basket,
		"--task", "add-item", "--variant", "trunk", "--session-id", "s1",
		"--runs-dir", dir, "--projects-dir", dir,
	}, &stdout, &stderr)
	require.Equal(t, 0, code, stderr.String())
}
