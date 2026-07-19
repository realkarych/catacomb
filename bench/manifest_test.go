package bench_test

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
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

func TestManifestEntryOfflineFieldsJSON(t *testing.T) {
	cost := 0.42
	with := bench.ManifestEntry{RunID: "r", CostUSD: &cost, EvidenceDir: "/runs/r"}
	data, err := json.Marshal(with)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"cost_usd":0.42`)
	assert.Contains(t, string(data), `"evidence_dir":"/runs/r"`)

	var back bench.ManifestEntry
	require.NoError(t, json.Unmarshal(data, &back))
	require.NotNil(t, back.CostUSD)
	assert.InDelta(t, 0.42, *back.CostUSD, 1e-9)
	assert.Equal(t, "/runs/r", back.EvidenceDir)

	without, err := json.Marshal(bench.ManifestEntry{RunID: "r"})
	require.NoError(t, err)
	assert.NotContains(t, string(without), "cost_usd")
	assert.NotContains(t, string(without), "evidence_dir")
}

func TestManifestOfflineRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.jsonl")
	m := bench.Manifest{Path: path}
	cost := 1.25
	e := bench.ManifestEntry{
		RunID: "bench-b-t1-base-r1", Task: "t1", Variant: "base", Rep: 1,
		CostUSD: &cost, EvidenceDir: "/runs/bench-b-t1-base-r1", BasketHash: "h",
		FinishedAt: time.Date(2026, 7, 5, 10, 0, 0, 0, time.UTC),
	}
	require.NoError(t, m.Append(e))
	done, err := m.Completed()
	require.NoError(t, err)
	got := done["bench-b-t1-base-r1"]
	require.NotNil(t, got.CostUSD)
	assert.InDelta(t, 1.25, *got.CostUSD, 1e-9)
	assert.Equal(t, "/runs/bench-b-t1-base-r1", got.EvidenceDir)
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

func TestManifestCompletedSkipsBlankAndWhitespaceOnlyLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.jsonl")
	body := "{\"run_id\":\"a\",\"exit_code\":3}\n\n   \n{\"run_id\":\"b\"}\n"
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	done, err := bench.Manifest{Path: path}.Completed()
	require.NoError(t, err)

	ids := make([]string, 0, len(done))
	for id := range done {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	assert.Equal(t, []string{"a", "b"}, ids, "blank lines must be skipped, not turned into empty entries")
	assert.Equal(t, 3, done["a"].ExitCode)
}

func TestManifestCompletedRejectsTheWholeFileOnAMalformedLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.jsonl")
	require.NoError(t, os.WriteFile(path, []byte("{\"run_id\":\"a\"}\n{not-json\n"), 0o600))

	done, err := bench.Manifest{Path: path}.Completed()

	require.Error(t, err)
	var syntaxErr *json.SyntaxError
	assert.ErrorAs(t, err, &syntaxErr)
	assert.Nil(t, done, "a partially parsed manifest must not be handed back as if it were complete")
}

func TestManifestCompletedReadErrorIsReportedNotSwallowedAsAbsent(t *testing.T) {
	done, err := bench.Manifest{Path: t.TempDir()}.Completed()

	require.Error(t, err)
	assert.NotErrorIs(t, err, os.ErrNotExist,
		"only a missing file may be treated as an empty manifest")
	assert.Nil(t, done)
}

func TestManifestAppendOpenError(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o600))

	err := bench.Manifest{Path: filepath.Join(blocker, "manifest.jsonl")}.Append(bench.ManifestEntry{RunID: "r"})

	require.Error(t, err)
	var pathErr *fs.PathError
	assert.ErrorAs(t, err, &pathErr)
}
