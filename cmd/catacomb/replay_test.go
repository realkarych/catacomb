package main

import (
	"bytes"
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

func TestRunReplayExportIsDeterministicallySorted(t *testing.T) {
	dir := t.TempDir()
	transcript := filepath.Join(dir, "two.jsonl")
	lines := `{"type":"user","uuid":"u1","sessionId":"sB","timestamp":"2026-06-20T10:00:00Z","message":{"role":"user","content":"hi"}}
{"type":"assistant","uuid":"a1","parentUuid":"u1","sessionId":"sB","timestamp":"2026-06-20T10:00:01Z","message":{"role":"assistant","id":"msg_b","model":"m","content":[{"type":"tool_use","id":"tb","name":"Bash","input":{"command":"ls"}}]}}
{"type":"user","uuid":"u2","sessionId":"sA","timestamp":"2026-06-20T10:00:02Z","message":{"role":"user","content":"hi"}}
{"type":"assistant","uuid":"a2","parentUuid":"u2","sessionId":"sA","timestamp":"2026-06-20T10:00:03Z","message":{"role":"assistant","id":"msg_a","model":"m","content":[{"type":"tool_use","id":"ta","name":"Bash","input":{"command":"ls"}}]}}
`
	require.NoError(t, os.WriteFile(transcript, []byte(lines), 0o600))
	exportPath := filepath.Join(dir, "g.jsonl")

	_, err := runReplay(replayArgs{input: transcript, exportPath: exportPath})
	require.NoError(t, err)

	blob, err := os.ReadFile(exportPath)
	require.NoError(t, err)

	var nodeIDs, edgeIDs, runIDs []string
	for _, l := range decodeSnapshotLines(t, string(blob)) {
		id, _ := l["id"].(string)
		switch l["kind"] {
		case "node":
			nodeIDs = append(nodeIDs, id)
		case "edge":
			edgeIDs = append(edgeIDs, id)
		case "run":
			runIDs = append(runIDs, id)
		}
	}
	require.Greater(t, len(nodeIDs), 1)
	require.Greater(t, len(edgeIDs), 1)
	assert.Equal(t, []string{"sA", "sB"}, runIDs)
	assert.IsIncreasing(t, nodeIDs)
	assert.IsIncreasing(t, edgeIDs)
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

func TestReplayWarnsOnUnknownRecords(t *testing.T) {
	buf := captureDriftOut(t)
	drifty := writeDriftyCopy(t, filepath.Join("testdata", "session.jsonl"))
	root := newRootCmd()
	root.SetOut(&strings.Builder{})
	root.SetArgs([]string{"replay", drifty})
	require.NoError(t, root.Execute())
	assert.Contains(t, buf.String(), "unrecognized transcript record")
	assert.Contains(t, buf.String(), "unknown_record_type=1")
}

func TestReplayWarnsOnNewerVersion(t *testing.T) {
	resetDriftWarnings()
	buf := captureDriftOut(t)
	versioned := writeVersionedCopy(t, filepath.Join("testdata", "session.jsonl"), "9.9.9")
	root := newRootCmd()
	root.SetOut(&strings.Builder{})
	root.SetArgs([]string{"replay", versioned})
	require.NoError(t, root.Execute())
	assert.Contains(t, buf.String(), "9.9.9")
	assert.Contains(t, buf.String(), "newer than tested")
}

func TestReplayMissingInputIsOperational(t *testing.T) {
	var out, errBuf bytes.Buffer
	code := run([]string{"replay", filepath.Join(t.TempDir(), "nope.jsonl")}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), "open")
}

func TestReplayMalformedInputIsOperational(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "bad.jsonl")
	require.NoError(t, os.WriteFile(bad, []byte("{not json}\n"), 0o600))
	var out, errBuf bytes.Buffer
	code := run([]string{"replay", bad}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.NotEmpty(t, errBuf.String())
}
