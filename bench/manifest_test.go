package bench_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/bench"
)

func TestManifestAppendCompletedRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.jsonl")
	m := bench.Manifest{Path: path}

	e1 := bench.ManifestEntry{
		RunID:      "bench-c-t-v-r1",
		Task:       "t",
		Variant:    "v",
		Rep:        1,
		ExitCode:   0,
		SessionID:  "sess-1",
		Marked:     true,
		BasketHash: "abc",
		FinishedAt: time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC),
		Note:       "ok",
	}
	e2 := bench.ManifestEntry{
		RunID:      "bench-c-t-v-r2",
		Task:       "t",
		Variant:    "v",
		Rep:        2,
		ExitCode:   1,
		BasketHash: "abc",
		FinishedAt: time.Date(2026, 7, 2, 10, 1, 0, 0, time.UTC),
	}
	require.NoError(t, m.Append(e1))
	require.NoError(t, m.Append(e2))

	done, err := m.Completed()
	require.NoError(t, err)
	require.Len(t, done, 2)

	got1 := done["bench-c-t-v-r1"]
	assert.Equal(t, e1.RunID, got1.RunID)
	assert.Equal(t, e1.ExitCode, got1.ExitCode)
	assert.Equal(t, e1.SessionID, got1.SessionID)
	assert.True(t, got1.Marked)
	assert.Equal(t, e1.Note, got1.Note)
	assert.True(t, e1.FinishedAt.Equal(got1.FinishedAt))
	assert.Equal(t, 1, done["bench-c-t-v-r2"].ExitCode)
}

func TestManifestEntryMissingCheckpointsJSON(t *testing.T) {
	withField := bench.ManifestEntry{RunID: "r", MissingCheckpoints: []string{"compiled", "task:link"}}
	data, err := json.Marshal(withField)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"missing_checkpoints":["compiled","task:link"]`)

	var back bench.ManifestEntry
	require.NoError(t, json.Unmarshal(data, &back))
	assert.Equal(t, withField.MissingCheckpoints, back.MissingCheckpoints)

	without, err := json.Marshal(bench.ManifestEntry{RunID: "r"})
	require.NoError(t, err)
	assert.NotContains(t, string(without), "missing_checkpoints")
}

func TestManifestCompletedLastWins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.jsonl")
	m := bench.Manifest{Path: path}
	require.NoError(t, m.Append(bench.ManifestEntry{RunID: "r", ExitCode: 1, Note: "first"}))
	require.NoError(t, m.Append(bench.ManifestEntry{RunID: "r", ExitCode: 0, Note: "second"}))

	done, err := m.Completed()
	require.NoError(t, err)
	require.Len(t, done, 1)
	assert.Equal(t, 0, done["r"].ExitCode)
	assert.Equal(t, "second", done["r"].Note)
}

func TestManifestCompletedAbsentFile(t *testing.T) {
	m := bench.Manifest{Path: filepath.Join(t.TempDir(), "missing.jsonl")}
	done, err := m.Completed()
	require.NoError(t, err)
	assert.Empty(t, done)
}

func TestManifestCompletedSkipsBlankLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.jsonl")
	body := "{\"run_id\":\"a\"}\n\n   \n{\"run_id\":\"b\"}\n"
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	done, err := bench.Manifest{Path: path}.Completed()
	require.NoError(t, err)
	assert.Len(t, done, 2)
}

func TestManifestCompletedMalformedLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.jsonl")
	require.NoError(t, os.WriteFile(path, []byte("{\"run_id\":\"a\"}\n{not-json\n"), 0o600))
	_, err := bench.Manifest{Path: path}.Completed()
	require.Error(t, err)
}

func TestManifestCompletedReadError(t *testing.T) {
	_, err := bench.Manifest{Path: t.TempDir()}.Completed()
	require.Error(t, err)
}

func TestManifestAppendOpenError(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o600))
	err := bench.Manifest{Path: filepath.Join(blocker, "manifest.jsonl")}.Append(bench.ManifestEntry{RunID: "r"})
	require.Error(t, err)
}
