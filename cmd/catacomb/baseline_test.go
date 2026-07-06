package main

import (
	"bytes"
	"database/sql"
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

	"github.com/realkarych/catacomb/cdc"
	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/store"
)

func pinBaselineNow(t *testing.T) time.Time {
	t.Helper()
	ts := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	orig := nowFn
	nowFn = func() time.Time { return ts }
	t.Cleanup(func() { nowFn = orig })
	return ts
}

func emptyStoreDB(t *testing.T) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "b.db")
	s, err := store.OpenSQLite(dbPath)
	require.NoError(t, err)
	require.NoError(t, s.Close())
	return dbPath
}

func TestBaselineSetListRmRoundTrip(t *testing.T) {
	runsDir := evidenceRoot(t)
	dbPath := emptyStoreDB(t)
	ts := pinBaselineNow(t)

	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"baseline", "set", "golden", "--db", dbPath, "--label", "variant=base", "--runs-dir", runsDir})
	require.NoError(t, cmd.Execute())
	assert.Contains(t, buf.String(), `baseline "golden" set: 2 runs`)

	cmd = newRootCmd()
	buf.Reset()
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"baseline", "list", "--db", dbPath})
	require.NoError(t, cmd.Execute())
	out := buf.String()
	assert.Contains(t, out, "NAME")
	assert.Contains(t, out, "SELECTOR")
	assert.Contains(t, out, "golden")
	assert.Contains(t, out, "variant=base")
	assert.Contains(t, out, ts.Format(time.RFC3339))

	cmd = newRootCmd()
	buf.Reset()
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"baseline", "list", "--db", dbPath, "--json"})
	require.NoError(t, cmd.Execute())
	var baselines []model.Baseline
	require.NoError(t, json.Unmarshal(buf.Bytes(), &baselines))
	require.Len(t, baselines, 1)
	assert.Equal(t, "golden", baselines[0].Name)
	assert.Equal(t, []string{"base-0", "base-1"}, baselines[0].RunIDs)
	assert.Equal(t, map[string]string{"variant": "base"}, baselines[0].Selector)
	assert.Equal(t, runsDir, baselines[0].RunsDir)
	assert.Equal(t, model.Stamps{CatacombVersion: "dev", StepKeyScheme: "stepkey/v1"}, baselines[0].Stamps)

	cmd = newRootCmd()
	buf.Reset()
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"baseline", "rm", "golden", "--db", dbPath})
	require.NoError(t, cmd.Execute())
	assert.Contains(t, buf.String(), `baseline "golden" removed`)

	cmd = newRootCmd()
	buf.Reset()
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"baseline", "list", "--db", dbPath})
	require.NoError(t, cmd.Execute())
	assert.NotContains(t, buf.String(), "golden")
}

func TestBaselineSetRequiresLabel(t *testing.T) {
	err := runBaselineSet(io.Discard, store.OpenSQLite, emptyStoreDB(t), "all", nil, evidenceRoot(t))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one --label")
}

func TestBaselineSetNameValidation(t *testing.T) {
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
			err := runBaselineSet(io.Discard, store.OpenSQLite, emptyStoreDB(t), tc.arg, []string{"variant=base"}, evidenceRoot(t))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestFormatSelectorEmpty(t *testing.T) {
	assert.Equal(t, "-", formatSelector(nil))
}

func TestBaselineSetInvalidLabel(t *testing.T) {
	err := runBaselineSet(io.Discard, store.OpenSQLite, emptyStoreDB(t), "bad", []string{"BAD=x"}, evidenceRoot(t))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid --label")
}

func TestBaselineSetRequiresRunsDir(t *testing.T) {
	err := runBaselineSet(io.Discard, store.OpenSQLite, emptyStoreDB(t), "x", []string{"variant=base"}, "")
	require.ErrorIs(t, err, errBaselineNoRunsDir)
	var opErr *operationalError
	require.ErrorAs(t, err, &opErr)
}

type fakeStore struct{}

func (f *fakeStore) Persist([]model.Observation, []*model.Node, []*model.Edge) error { return nil }

func (f *fakeStore) Close() error { return nil }

func (f *fakeStore) AppendDeltas(model.Observation, []cdc.GraphDelta) error { return nil }

func (f *fakeStore) MaxSeq() (uint64, error) { return 0, nil }

func (f *fakeStore) ObservationsSince(uint64) ([]model.Observation, error) { return nil, nil }

func (f *fakeStore) ObservationsForExecution(string) ([]model.Observation, error) { return nil, nil }

func (f *fakeStore) UpsertRun(model.Run) error { return nil }

func (f *fakeStore) ListOpenRuns() ([]model.Run, error) { return nil, nil }

func (f *fakeStore) Runs() ([]model.Run, error) { return nil, nil }

func (f *fakeStore) Quarantine(model.QuarantineRecord) error { return nil }

func (f *fakeStore) QuarantineCount() (int64, error) { return 0, nil }

func (f *fakeStore) UpsertTailCursor(model.TailCursor) error { return nil }

func (f *fakeStore) LoadTailCursors() ([]model.TailCursor, error) { return nil, nil }

func (f *fakeStore) UpsertAnnotation(model.Annotation) error { return nil }

func (f *fakeStore) AnnotationsForExecution(string) ([]model.Annotation, error) { return nil, nil }

func (f *fakeStore) MoveAnnotations(string, string, string) error { return nil }

func (f *fakeStore) UpsertBaseline(model.Baseline) error { return nil }

func (f *fakeStore) GetBaseline(string) (model.Baseline, bool, error) {
	return model.Baseline{}, false, nil
}

func (f *fakeStore) ListBaselines() ([]model.Baseline, error) { return nil, nil }

func (f *fakeStore) DeleteBaseline(string) error { return nil }

func (f *fakeStore) AppendRegressResult(string, json.RawMessage) (int, error) { return 0, nil }

func (f *fakeStore) RegressResultsFor(string) ([]model.RegressResult, error) { return nil, nil }

type upsertErrStore struct {
	fakeStore
}

func (u *upsertErrStore) UpsertBaseline(model.Baseline) error { return errors.New("boom") }

func TestBaselineSetUpsertError(t *testing.T) {
	err := runBaselineSet(io.Discard, openStore(&upsertErrStore{}), filepath.Join(t.TempDir(), "x.db"), "x", []string{"variant=base"}, evidenceRoot(t))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "baseline set")
}

func TestBaselineListSortsMultiple(t *testing.T) {
	dbPath := emptyStoreDB(t)
	upsertBaselineRunsDir(t, dbPath, model.Baseline{Name: "zeta", RunIDs: []string{"base-0"}, Selector: map[string]string{"variant": "base"}})
	upsertBaselineRunsDir(t, dbPath, model.Baseline{Name: "alpha", RunIDs: []string{"base-1"}, Selector: map[string]string{"variant": "other"}})

	var buf bytes.Buffer
	require.NoError(t, runBaselineList(&buf, store.OpenSQLiteReadOnly, dbPath, false))
	out := buf.String()
	assert.Less(t, strings.Index(out, "alpha"), strings.Index(out, "zeta"))
}

func TestBaselineListJSONEmpty(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, runBaselineList(&buf, store.OpenSQLiteReadOnly, emptyStoreDB(t), true))
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

func TestBaselineSetRunsDirPersistsOffline(t *testing.T) {
	runsDir := evidenceRoot(t)
	dbPath := emptyStoreDB(t)
	ts := pinBaselineNow(t)

	root := newRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"baseline", "set", "golden", "--db", dbPath, "--label", "variant=base", "--runs-dir", runsDir})
	require.NoError(t, root.Execute())
	assert.Contains(t, buf.String(), `baseline "golden" set: 2 runs`)

	s, err := store.OpenSQLiteReadOnly(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	b, ok, err := s.GetBaseline("golden")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, []string{"base-0", "base-1"}, b.RunIDs)
	assert.Equal(t, map[string]string{"variant": "base"}, b.Selector)
	assert.Equal(t, runsDir, b.RunsDir)
	assert.Equal(t, model.Stamps{CatacombVersion: "dev", StepKeyScheme: "stepkey/v1"}, b.Stamps)
	assert.True(t, b.CreatedAt.Equal(ts))
	assert.Empty(t, b.Repro)
}

func TestBaselineSetRunsDirEmptyMatch(t *testing.T) {
	err := runBaselineSet(io.Discard, store.OpenSQLite, emptyStoreDB(t), "none", []string{"variant=ghost"}, evidenceRoot(t))
	require.ErrorIs(t, err, ErrEmptyGroup)
	assert.Contains(t, err.Error(), "variant=ghost")
}

func TestBaselineSetRunsDirScanError(t *testing.T) {
	err := runBaselineSet(io.Discard, store.OpenSQLite, emptyStoreDB(t), "x", []string{"variant=base"}, filepath.Join(t.TempDir(), "absent"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "runs-dir")
}

func TestBaselineSetRunsDirCreatesStoreOnFreshMachine(t *testing.T) {
	runsDir := evidenceRoot(t)
	dbPath := filepath.Join(t.TempDir(), "fresh", "catacomb.db")

	var buf bytes.Buffer
	require.NoError(t, runBaselineSet(&buf, store.OpenSQLite, dbPath, "golden", []string{"variant=base"}, runsDir))
	assert.Contains(t, buf.String(), `baseline "golden" set: 2 runs`)

	_, statErr := os.Stat(dbPath)
	require.NoError(t, statErr)

	s, err := store.OpenSQLiteReadOnly(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	b, ok, err := s.GetBaseline("golden")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, []string{"base-0", "base-1"}, b.RunIDs)
}

func TestBaselineSetRunsDirOpenError(t *testing.T) {
	runsDir := evidenceRoot(t)
	failing := func(string) (store.Store, error) { return nil, errors.New("boom-open") }
	err := runBaselineSet(io.Discard, failing, filepath.Join(t.TempDir(), "x.db"), "golden", []string{"variant=base"}, runsDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom-open")
}

func TestBaselineListLegacyRowWithoutStamps(t *testing.T) {
	dbPath := emptyStoreDB(t)
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO baselines(name, body) VALUES ('legacy', '{"name":"legacy","run_ids":["r1"],"selector":{"variant":"base"},"created_at":"2026-01-02T03:04:05Z"}')`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	var buf bytes.Buffer
	require.NoError(t, runBaselineList(&buf, store.OpenSQLiteReadOnly, dbPath, true))
	var baselines []model.Baseline
	require.NoError(t, json.Unmarshal(buf.Bytes(), &baselines))
	require.Len(t, baselines, 1)
	assert.Equal(t, "legacy", baselines[0].Name)
	assert.Equal(t, []string{"r1"}, baselines[0].RunIDs)
	assert.True(t, baselines[0].Stamps.Zero())
	assert.Empty(t, baselines[0].RunsDir)

	buf.Reset()
	require.NoError(t, runBaselineList(&buf, store.OpenSQLiteReadOnly, dbPath, false))
	assert.Contains(t, buf.String(), "legacy")
}

func TestBaselineCmdWired(t *testing.T) {
	root := newRootCmd()
	names := make(map[string]bool)
	for _, sub := range root.Commands() {
		names[sub.Name()] = true
	}
	assert.True(t, names["baseline"])
}

func TestBaselineExitCodesOperational(t *testing.T) {
	dbPath := emptyStoreDB(t)
	missing := filepath.Join(t.TempDir(), "nope.db")
	root := evidenceRoot(t)
	cases := []struct {
		name string
		args []string
	}{
		{"set invalid label", []string{"baseline", "set", "x", "--db", dbPath, "--label", "BAD=x", "--runs-dir", root}},
		{"set runs-dir zero match", []string{"baseline", "set", "x", "--db", dbPath, "--label", "variant=ghost", "--runs-dir", root}},
		{"set runs-dir scan error", []string{"baseline", "set", "x", "--db", dbPath, "--label", "variant=base", "--runs-dir", filepath.Join(t.TempDir(), "absent")}},
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
