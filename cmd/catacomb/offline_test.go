package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/ingest/drift"
	"github.com/realkarych/catacomb/model"
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

func writeVersionedCopy(t *testing.T, src, version string) string {
	t.Helper()
	data, err := os.ReadFile(src)
	require.NoError(t, err)
	extra := fmt.Sprintf(`{"type":"user","uuid":"uv","sessionId":"s1","timestamp":"2026-06-20T10:00:09Z","version":%q,"message":{"role":"user","content":"ping"}}`, version)
	content := strings.TrimRight(string(data), "\n") + "\n" + extra + "\n"
	path := filepath.Join(t.TempDir(), "versioned.jsonl")
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
	require.Empty(t, boundary[0].ExecutionID)
	require.Empty(t, boundary[1].ExecutionID)
	require.Zero(t, boundary[0].Seq)
	require.Zero(t, boundary[1].Seq)
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

func TestMaxObservedVersion(t *testing.T) {
	obs := []model.Observation{
		{Attrs: nil},
		{Attrs: map[string]any{"claude_code_version": 123}},
		{Attrs: map[string]any{"claude_code_version": "1.2.3"}},
		{Attrs: map[string]any{"claude_code_version": "9.9.9"}},
		{Attrs: map[string]any{"claude_code_version": "2.0.0"}},
	}
	assert.Equal(t, "9.9.9", maxObservedVersion(obs))
	assert.Equal(t, "", maxObservedVersion(nil))
}

func TestWarnVersionFiresAndStaysSilent(t *testing.T) {
	resetDriftWarnings()
	buf := captureDriftOut(t)
	warnVersion("9.9.9")
	out := buf.String()
	assert.Contains(t, out, "9.9.9")
	assert.Contains(t, out, drift.TestedClaudeCodeVersion)
	assert.Contains(t, out, "newer than tested")

	buf.Reset()
	warnVersion(drift.TestedClaudeCodeVersion)
	warnVersion("")
	warnVersion("1.0.0")
	assert.Empty(t, buf.String())
}

func TestWarnVersionDedupes(t *testing.T) {
	resetDriftWarnings()
	var buf bytes.Buffer
	old := driftOut
	driftOut = &buf
	defer func() { driftOut = old }()

	high := "999.0.0"
	warnVersion(high)
	warnVersion(high)
	assert.Equal(t, 1, strings.Count(buf.String(), "newer than tested"))
}

func TestParseTranscriptsWarnsOnNewerVersion(t *testing.T) {
	resetDriftWarnings()
	buf := captureDriftOut(t)
	path := writeVersionedCopy(t, filepath.Join("testdata", "session.jsonl"), "9.9.9")
	_, err := parseTranscripts(path, nil, "exec-v")
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "9.9.9")
	assert.Contains(t, buf.String(), "newer than tested")
}

func TestParseTranscriptsNoVersionWarnAtCeiling(t *testing.T) {
	buf := captureDriftOut(t)
	path := writeVersionedCopy(t, filepath.Join("testdata", "session.jsonl"), drift.TestedClaudeCodeVersion)
	_, err := parseTranscripts(path, nil, "exec-v2")
	require.NoError(t, err)
	assert.NotContains(t, buf.String(), "newer than tested")
}

func TestParseTranscriptsWarnsDriftAndVersionTogether(t *testing.T) {
	resetDriftWarnings()
	buf := captureDriftOut(t)
	drifty := writeDriftyCopy(t, filepath.Join("testdata", "session.jsonl"))
	path := writeVersionedCopy(t, drifty, "9.9.9")
	_, err := parseTranscripts(path, nil, "exec-dv")
	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, "unrecognized transcript record")
	assert.Contains(t, out, "newer than tested")
	assert.Contains(t, out, "9.9.9")
}

func TestGraphFromObservationsAppliesExtraAndPricer(t *testing.T) {
	obs, err := parseTranscripts("testdata/session.jsonl", nil, "exec-1")
	require.NoError(t, err)
	extra := boundaryObservations("s1", "task:t1", time.Unix(0, 0).UTC(), time.Unix(10, 0).UTC())
	g := graphFromObservations(obs, "exec-1", newPricer(), extra)
	require.NotNil(t, g)
	marks := graphMarkerNames(g)
	_, ok := marks["task:t1"]
	assert.True(t, ok)
}

func TestTranscriptTimeBounds(t *testing.T) {
	obs, err := parseTranscripts("testdata/session.jsonl", nil, "exec-1")
	require.NoError(t, err)
	start, end, ok := transcriptTimeBounds(obs)
	require.True(t, ok)
	assert.False(t, start.After(end))
}

func TestTranscriptTimeBoundsEmpty(t *testing.T) {
	_, _, ok := transcriptTimeBounds(nil)
	assert.False(t, ok)
}

func TestTranscriptTimeBoundsSkipsZeroAndOutOfOrder(t *testing.T) {
	obs := []model.Observation{
		{EventTime: time.Unix(100, 0).UTC()},
		{},
		{EventTime: time.Unix(50, 0).UTC()},
		{EventTime: time.Unix(200, 0).UTC()},
	}
	start, end, ok := transcriptTimeBounds(obs)
	require.True(t, ok)
	assert.True(t, start.Equal(time.Unix(50, 0).UTC()))
	assert.True(t, end.Equal(time.Unix(200, 0).UTC()))
}

func TestLoadGraphOfflineStillWorks(t *testing.T) {
	g, err := loadGraphOffline("testdata/session.jsonl", nil, "exec-1", newPricer(), nil)
	require.NoError(t, err)
	require.NotNil(t, g)
}
