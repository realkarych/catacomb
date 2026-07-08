package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func captureDriftOut(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	orig := driftOut
	driftOut = &buf
	t.Cleanup(func() { driftOut = orig })
	return &buf
}

func writeDriftyCopy(t *testing.T, src string) string {
	t.Helper()
	data, err := os.ReadFile(src)
	require.NoError(t, err)
	content := strings.TrimRight(string(data), "\n") + "\n" + `{"type":"checkpoint_v9","sessionId":"s1"}` + "\n"
	path := filepath.Join(t.TempDir(), "drifty.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestParseTranscriptsRenumbersSeq(t *testing.T) {
	main := filepath.Join("testdata", "session.jsonl")
	sub := filepath.Join(t.TempDir(), "agent-a.jsonl")
	data, err := os.ReadFile(main)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(sub, data, 0o600))
	obs, err := parseTranscripts(main, []string{sub}, "exec-1")
	require.NoError(t, err)
	require.NotEmpty(t, obs)
	for i, o := range obs {
		require.Equal(t, uint64(i+1), o.Seq)
		require.Equal(t, "exec-1", o.ExecutionID)
	}
	_, err = parseTranscripts(filepath.Join(t.TempDir(), "absent.jsonl"), nil, "exec-1")
	require.Error(t, err)
}

func TestParseTranscriptsMalformedLine(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "bad.jsonl")
	require.NoError(t, os.WriteFile(bad, []byte(`{"type":`), 0o600))
	_, err := parseTranscripts(bad, nil, "exec-1")
	require.Error(t, err)
}

func TestBoundaryObservationsShape(t *testing.T) {
	start, end := time.Unix(10, 0), time.Unix(20, 0)
	obs := boundaryObservations("sess-9", "task:t1", start, end)
	require.Len(t, obs, 2)
	require.Equal(t, "marker", obs[0].Kind)
	require.Equal(t, "task:t1", obs[0].Attrs["name"])
	require.Equal(t, "start", obs[0].Attrs["boundary"])
	require.Equal(t, "end", obs[1].Attrs["boundary"])
	require.Equal(t, "sess-9", obs[0].Correlation.SessionID)
	require.True(t, obs[0].EventTime.Equal(start.UTC()))
}

func TestLoadGraphOfflineInjectsMarkers(t *testing.T) {
	main := filepath.Join("testdata", "session.jsonl")
	boundary := boundaryObservations("s", "task:demo", time.Unix(1, 0), time.Unix(2, 0))
	g, err := loadGraphOffline(main, nil, "exec-2", nil, boundary)
	require.NoError(t, err)
	names := graphMarkerNames(g)
	_, ok := names["task:demo"]
	require.True(t, ok)
	require.Equal(t, "exec-2", boundary[0].ExecutionID)
	require.Equal(t, boundary[0].Seq+1, boundary[1].Seq)
	g2, err := loadGraphOffline(main, nil, "exec-2", nil, boundaryObservations("s", "task:demo", time.Unix(1, 0), time.Unix(2, 0)))
	require.NoError(t, err)
	n1, e1 := g.Snapshot()
	n2, e2 := g2.Snapshot()
	require.Equal(t, len(n1), len(n2))
	require.Equal(t, len(e1), len(e2))
}

func TestLoadGraphOfflineWithPricer(t *testing.T) {
	main := filepath.Join("testdata", "session.jsonl")
	sub := filepath.Join(t.TempDir(), "agent-a.jsonl")
	data, err := os.ReadFile(main)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(sub, data, 0o600))
	g, err := loadGraphOffline(main, []string{sub}, "exec-3", newPricer(), nil)
	require.NoError(t, err)
	nodes, _ := g.Snapshot()
	require.NotEmpty(t, nodes)
	_, err = loadGraphOffline(filepath.Join(t.TempDir(), "absent.jsonl"), nil, "exec-3", newPricer(), nil)
	require.Error(t, err)
}

func TestParseTranscriptsWarnsOnUnknownRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	content := `{"type":"user","uuid":"u1","sessionId":"s1","timestamp":"2026-06-20T10:00:00Z","message":{"role":"user","content":"hi"}}` + "\n" +
		`{"type":"checkpoint_v9","sessionId":"s1"}` + "\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	buf := captureDriftOut(t)

	obs, err := parseTranscripts(path, nil, "exec-w")
	require.NoError(t, err)
	require.NotEmpty(t, obs)
	assert.Contains(t, buf.String(), "unrecognized transcript record")
	assert.Contains(t, buf.String(), "unknown_record_type=1")
}

func TestParseTranscriptsNoWarnOnCleanTranscript(t *testing.T) {
	buf := captureDriftOut(t)

	_, err := parseTranscripts(filepath.Join("testdata", "session.jsonl"), nil, "exec-c")
	require.NoError(t, err)
	assert.Empty(t, buf.String())
}
