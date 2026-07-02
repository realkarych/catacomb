package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/ingest/hook"
	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/store"
)

func labeledRuns() []seedRun {
	return []seedRun{
		{session: "base-0", labels: "variant=base", tools: 1, durationMS: 1000},
		{session: "base-1", labels: "variant=base", tools: 1, durationMS: 1000},
		{session: "base-2", labels: "variant=base", tools: 1, durationMS: 1000},
		{session: "other-0", labels: "variant=other", tools: 1, durationMS: 1000},
	}
}

func pinBaselineNow(t *testing.T) time.Time {
	t.Helper()
	ts := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	orig := nowFn
	nowFn = func() time.Time { return ts }
	t.Cleanup(func() { nowFn = orig })
	return ts
}

func TestBaselineSetListRmRoundTrip(t *testing.T) {
	dbPath := seedRegressDB(t, labeledRuns())
	ts := pinBaselineNow(t)

	root := newRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"baseline", "set", "golden", "--db", dbPath, "--label", "variant=base"})
	require.NoError(t, root.Execute())
	assert.Contains(t, buf.String(), `baseline "golden" set: 3 runs`)

	root = newRootCmd()
	buf.Reset()
	root.SetOut(&buf)
	root.SetArgs([]string{"baseline", "list", "--db", dbPath})
	require.NoError(t, root.Execute())
	out := buf.String()
	assert.Contains(t, out, "NAME")
	assert.Contains(t, out, "SELECTOR")
	assert.Contains(t, out, "golden")
	assert.Contains(t, out, "variant=base")
	assert.Contains(t, out, ts.Format(time.RFC3339))

	root = newRootCmd()
	buf.Reset()
	root.SetOut(&buf)
	root.SetArgs([]string{"baseline", "list", "--db", dbPath, "--json"})
	require.NoError(t, root.Execute())
	var baselines []model.Baseline
	require.NoError(t, json.Unmarshal(buf.Bytes(), &baselines))
	require.Len(t, baselines, 1)
	assert.Equal(t, "golden", baselines[0].Name)
	assert.Equal(t, []string{"base-0", "base-1", "base-2"}, baselines[0].RunIDs)
	assert.Equal(t, map[string]string{"variant": "base"}, baselines[0].Selector)

	root = newRootCmd()
	buf.Reset()
	root.SetOut(&buf)
	root.SetArgs([]string{"baseline", "rm", "golden", "--db", dbPath})
	require.NoError(t, root.Execute())
	assert.Contains(t, buf.String(), `baseline "golden" removed`)

	root = newRootCmd()
	buf.Reset()
	root.SetOut(&buf)
	root.SetArgs([]string{"baseline", "list", "--db", dbPath})
	require.NoError(t, root.Execute())
	assert.NotContains(t, buf.String(), "golden")
}

func TestBaselineSetNoLabelsSelectorDash(t *testing.T) {
	dbPath := seedRegressDB(t, labeledRuns())
	pinBaselineNow(t)
	require.NoError(t, runBaselineSet(io.Discard, store.OpenSQLite, newPricer, dbPath, "all", nil))

	var buf bytes.Buffer
	require.NoError(t, runBaselineList(&buf, store.OpenSQLiteReadOnly, dbPath, false))
	out := buf.String()
	assert.Contains(t, out, "all")
	assert.Contains(t, out, "-")
}

func TestBaselineSetZeroMatchError(t *testing.T) {
	dbPath := seedRegressDB(t, labeledRuns())
	err := runBaselineSet(io.Discard, store.OpenSQLite, newPricer, dbPath, "none", []string{"variant=ghost"})
	require.ErrorIs(t, err, ErrEmptyGroup)
}

func TestBaselineSetInvalidLabel(t *testing.T) {
	dbPath := seedRegressDB(t, labeledRuns())
	err := runBaselineSet(io.Discard, store.OpenSQLite, newPricer, dbPath, "bad", []string{"BAD=x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid --label")
}

func TestBaselineSetStoreMissing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.db")
	err := runBaselineSet(io.Discard, store.OpenSQLite, newPricer, missing, "x", []string{"variant=base"})
	require.ErrorIs(t, err, ErrStoreNotFound)
}

func TestBaselineSetLoadError(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "*.db")
	require.NoError(t, err)
	_ = f.Close()
	err = runBaselineSet(io.Discard, openStore(&obsErrStore{}), newPricer, f.Name(), "x", []string{"variant=base"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "store read")
}

type upsertErrStore struct {
	fakeStore
	obs []model.Observation
}

func (u *upsertErrStore) ObservationsSince(uint64) ([]model.Observation, error) { return u.obs, nil }
func (u *upsertErrStore) UpsertBaseline(model.Baseline) error                   { return errors.New("boom") }

func TestBaselineSetUpsertError(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "*.db")
	require.NoError(t, err)
	_ = f.Close()
	obs, err := hook.Parse("SessionStart", []byte(`{"session_id":"r1"}`), "e1", func() uint64 { return 1 })
	require.NoError(t, err)
	err = runBaselineSet(io.Discard, openStore(&upsertErrStore{obs: obs}), newPricer, f.Name(), "x", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "baseline set")
}

func TestBaselineListSortsMultiple(t *testing.T) {
	dbPath := seedRegressDB(t, labeledRuns())
	pinBaselineNow(t)
	require.NoError(t, runBaselineSet(io.Discard, store.OpenSQLite, newPricer, dbPath, "zeta", []string{"variant=base"}))
	require.NoError(t, runBaselineSet(io.Discard, store.OpenSQLite, newPricer, dbPath, "alpha", []string{"variant=other"}))

	var buf bytes.Buffer
	require.NoError(t, runBaselineList(&buf, store.OpenSQLiteReadOnly, dbPath, false))
	out := buf.String()
	assert.Less(t, strings.Index(out, "alpha"), strings.Index(out, "zeta"))
}

func TestBaselineListJSONEmpty(t *testing.T) {
	dbPath := seedRegressDB(t, labeledRuns())
	var buf bytes.Buffer
	require.NoError(t, runBaselineList(&buf, store.OpenSQLiteReadOnly, dbPath, true))
	var baselines []model.Baseline
	require.NoError(t, json.Unmarshal(buf.Bytes(), &baselines))
	assert.Empty(t, baselines)
}

func TestBaselineListStoreMissing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.db")
	err := runBaselineList(io.Discard, store.OpenSQLiteReadOnly, missing, false)
	require.ErrorIs(t, err, ErrStoreNotFound)
}

type listErrStore struct {
	fakeStore
}

func (l *listErrStore) ListBaselines() ([]model.Baseline, error) { return nil, errors.New("boom") }

func TestBaselineListError(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "*.db")
	require.NoError(t, err)
	_ = f.Close()
	err = runBaselineList(io.Discard, openStore(&listErrStore{}), f.Name(), false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "baseline list")
}

func TestBaselineRmStoreMissing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.db")
	err := runBaselineRm(io.Discard, store.OpenSQLite, missing, "x")
	require.ErrorIs(t, err, ErrStoreNotFound)
}

type deleteErrStore struct {
	fakeStore
}

func (d *deleteErrStore) DeleteBaseline(string) error { return errors.New("boom") }

func TestBaselineRmError(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "*.db")
	require.NoError(t, err)
	_ = f.Close()
	err = runBaselineRm(io.Discard, openStore(&deleteErrStore{}), f.Name(), "x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "baseline rm")
}

func TestBaselineCmdWiredAndGrouped(t *testing.T) {
	root := newRootCmd()
	groups := make(map[string]string)
	for _, sub := range root.Commands() {
		groups[sub.Name()] = sub.GroupID
	}
	assert.Equal(t, "advanced", groups["baseline"])
}
