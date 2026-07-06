package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
)

func TestRunReplayBuildsGraph(t *testing.T) {
	dir := t.TempDir()
	g, err := runReplay(replayArgs{
		input:      "testdata/session.jsonl",
		exportPath: filepath.Join(dir, "g.jsonl"),
	})
	require.NoError(t, err)
	assert.NotEmpty(t, g.Nodes)
	assert.NotEmpty(t, g.Edges)
	assert.FileExists(t, filepath.Join(dir, "g.jsonl"))
}

func TestRunReplayNoExport(t *testing.T) {
	g, err := runReplay(replayArgs{input: "testdata/session.jsonl"})
	require.NoError(t, err)
	assert.NotEmpty(t, g.Nodes)
}

func TestRunReplayMissingInput(t *testing.T) {
	_, err := runReplay(replayArgs{input: filepath.Join(t.TempDir(), "nope.jsonl")})
	require.Error(t, err)
}

func TestRunReplayMalformedInput(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "bad.jsonl")
	require.NoError(t, os.WriteFile(bad, []byte("{not json}\n"), 0o644))
	_, err := runReplay(replayArgs{input: bad})
	require.Error(t, err)
}

func TestRunReplayBadExportPath(t *testing.T) {
	_, err := runReplay(replayArgs{
		input:      "testdata/session.jsonl",
		exportPath: filepath.Join(t.TempDir(), "nodir", "g.jsonl"),
	})
	require.Error(t, err)
}

func TestReplayScrubsSecrets(t *testing.T) {
	dir := t.TempDir()
	transcript := filepath.Join(dir, "s.jsonl")
	line := `{"type":"assistant","sessionId":"replay-s","message":{"id":"m1","model":"m","role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"psql postgres://kesha:kesha_dev_password@localhost/appdb"}}]}}`
	require.NoError(t, os.WriteFile(transcript, []byte(line+"\n"), 0o600))
	exportPath := filepath.Join(dir, "g.jsonl")

	g, err := runReplay(replayArgs{input: transcript, exportPath: exportPath})
	require.NoError(t, err)

	var sawPayload bool
	for _, n := range g.Nodes {
		if n.Payload != nil && len(n.Payload.Input) > 0 {
			sawPayload = true
			assert.NotContains(t, string(n.Payload.Input), "kesha_dev_password")
			assert.Contains(t, string(n.Payload.Input), "‹redacted:connection-string›")
			assert.Equal(t, model.HashPayload(n.Payload), n.PayloadHash)
		}
	}
	assert.True(t, sawPayload)

	blob, err := os.ReadFile(exportPath)
	require.NoError(t, err)
	assert.NotContains(t, string(blob), "kesha_dev_password")
}

func TestReplayCommandWiring(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"replay", "testdata/session.jsonl"})
	var buf strings.Builder
	root.SetOut(&buf)
	require.NoError(t, root.Execute())
	assert.Contains(t, buf.String(), "replayed testdata/session.jsonl")
}

func TestReplayCommandError(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"replay", filepath.Join(t.TempDir(), "nope.jsonl")})
	require.Error(t, root.Execute())
}
