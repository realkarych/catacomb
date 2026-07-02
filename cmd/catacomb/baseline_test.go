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

func TestBaselineSetRequiresLabel(t *testing.T) {
	dbPath := seedRegressDB(t, labeledRuns())
	err := runBaselineSet(io.Discard, store.OpenSQLite, newPricer, dbPath, "all", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one --label")
}

func TestBaselineSetNameValidation(t *testing.T) {
	dbPath := seedRegressDB(t, labeledRuns())
	cases := []struct {
		name string
		arg  string
		want string
	}{
		{"empty", "", "must not be empty"},
		{"too long", strings.Repeat("a", 129), "128 bytes"},
		{"leading space", " x", "whitespace"},
		{"trailing space", "x ", "whitespace"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := runBaselineSet(io.Discard, store.OpenSQLite, newPricer, dbPath, tc.arg, []string{"variant=base"})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestFormatSelectorEmpty(t *testing.T) {
	assert.Equal(t, "-", formatSelector(nil))
}

func TestBaselineSetGetRoundTripsRepro(t *testing.T) {
	runs := []seedRun{
		{session: "base-0", labels: "variant=base", tools: 1, durationMS: 1000, cwd: "/repo/a"},
		{session: "base-1", labels: "variant=base", tools: 1, durationMS: 1000, cwd: "/repo/b"},
	}
	dbPath := seedRegressDB(t, runs)
	require.NoError(t, runBaselineSet(io.Discard, store.OpenSQLite, newPricer, dbPath, "golden", []string{"variant=base"}))

	s, err := store.OpenSQLiteReadOnly(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	b, ok, err := s.GetBaseline("golden")
	require.NoError(t, err)
	require.True(t, ok)
	require.Len(t, b.Repro, 2)
	require.NotNil(t, b.Repro["base-0"])
	assert.Equal(t, "/repo/a", b.Repro["base-0"].Cwd)
	assert.Equal(t, "/repo/b", b.Repro["base-1"].Cwd)
}

func TestBaselineSetNoReproWhenRunsLackRepro(t *testing.T) {
	dbPath := seedRegressDB(t, labeledRuns())
	require.NoError(t, runBaselineSet(io.Discard, store.OpenSQLite, newPricer, dbPath, "golden", []string{"variant=base"}))

	s, err := store.OpenSQLiteReadOnly(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	b, ok, err := s.GetBaseline("golden")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Empty(t, b.Repro)
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
	for i := range obs {
		if obs[i].Attrs == nil {
			obs[i].Attrs = map[string]any{}
		}
		obs[i].Attrs["catacomb.labels"] = "variant=base"
	}
	err = runBaselineSet(io.Discard, openStore(&upsertErrStore{obs: obs}), newPricer, f.Name(), "x", []string{"variant=base"})
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

func TestBaselineListV1StoreHint(t *testing.T) {
	dbPath := seedV1RegressDB(t)
	var out, errBuf bytes.Buffer
	code := run([]string{"baseline", "list", "--db", dbPath}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), "older than this binary")
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

func TestBaselineExitCodesOperational(t *testing.T) {
	dbPath := seedRegressDB(t, labeledRuns())
	missing := filepath.Join(t.TempDir(), "nope.db")
	cases := []struct {
		name string
		args []string
	}{
		{"set missing store", []string{"baseline", "set", "x", "--db", missing, "--label", "variant=base"}},
		{"set zero match", []string{"baseline", "set", "x", "--db", dbPath, "--label", "variant=ghost"}},
		{"set invalid label", []string{"baseline", "set", "x", "--db", dbPath, "--label", "BAD=x"}},
		{"list missing store", []string{"baseline", "list", "--db", missing}},
		{"rm missing store", []string{"baseline", "rm", "x", "--db", missing}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out, errBuf bytes.Buffer
			code := run(tc.args, &out, &errBuf)
			assert.Equal(t, 2, code, errBuf.String())
		})
	}
}
