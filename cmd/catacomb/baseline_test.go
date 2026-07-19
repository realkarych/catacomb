package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

func TestFormatSelector(t *testing.T) {
	cases := []struct {
		name string
		sel  map[string]string
		want string
	}{
		{"nil", nil, "-"},
		{"empty", map[string]string{}, "-"},
		{"single", map[string]string{"variant": "base"}, "variant=base"},
		{"keys sorted regardless of map order", map[string]string{"zone": "z", "arch": "a", "model": "m"}, "arch=a,model=m,zone=z"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, formatSelector(tc.sel))
		})
	}
}

func TestFormatSelectorIsStableAcrossRepeatedCalls(t *testing.T) {
	sel := map[string]string{"a": "1", "b": "2", "c": "3", "d": "4", "e": "5"}
	want := formatSelector(sel)
	for i := 0; i < 50; i++ {
		require.Equal(t, want, formatSelector(sel))
	}
	assert.Equal(t, "a=1,b=2,c=3,d=4,e=5", want)
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

func (f *fakeStore) Close() error { return nil }

func (f *fakeStore) UpsertBaseline(model.Baseline) error { return nil }

func (f *fakeStore) GetBaseline(string) (model.Baseline, bool, error) {
	return model.Baseline{}, false, nil
}

func (f *fakeStore) ListBaselines() ([]model.Baseline, error) { return nil, nil }

func (f *fakeStore) DeleteBaseline(string) error { return nil }

func (f *fakeStore) AppendRegressResult(string, json.RawMessage) (int, error) { return 0, nil }

func (f *fakeStore) RegressResultsFor(string) ([]model.RegressResult, error) { return nil, nil }

var errStoreBoom = errors.New("baseline test: store failed")

type upsertErrStore struct {
	fakeStore
}

func (u *upsertErrStore) UpsertBaseline(model.Baseline) error { return errStoreBoom }

func TestBaselineSetUpsertErrorIsWrappedNotSwallowed(t *testing.T) {
	err := runBaselineSet(io.Discard, openStore(&upsertErrStore{}), filepath.Join(t.TempDir(), "x.db"), "x", []string{"variant=base"}, evidenceRoot(t))
	require.ErrorIs(t, err, errStoreBoom)
	var opErr *operationalError
	require.ErrorAs(t, err, &opErr)
}

func baselineListNames(t *testing.T, out string) []string {
	t.Helper()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	require.NotEmpty(t, lines)
	require.True(t, strings.HasPrefix(lines[0], "NAME"), lines[0])
	names := make([]string, 0, len(lines)-1)
	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		require.NotEmpty(t, fields, line)
		names = append(names, fields[0])
	}
	return names
}

func TestBaselineListSortsNamesAscendingInBothFormats(t *testing.T) {
	dbPath := emptyStoreDB(t)
	for _, name := range []string{"zeta", "alpha", "mid"} {
		upsertBaselineRunsDir(t, dbPath, model.Baseline{Name: name, RunIDs: []string{"base-0"}, Selector: map[string]string{"variant": "base"}})
	}
	want := []string{"alpha", "mid", "zeta"}

	var table bytes.Buffer
	require.NoError(t, runBaselineList(&table, store.OpenSQLiteReadOnly, dbPath, false))
	assert.Equal(t, want, baselineListNames(t, table.String()))

	var asJSON bytes.Buffer
	require.NoError(t, runBaselineList(&asJSON, store.OpenSQLiteReadOnly, dbPath, true))
	var baselines []model.Baseline
	require.NoError(t, json.Unmarshal(asJSON.Bytes(), &baselines))
	got := make([]string, 0, len(baselines))
	for _, b := range baselines {
		got = append(got, b.Name)
	}
	assert.Equal(t, want, got)
}

type unsortedListStore struct {
	fakeStore
	baselines []model.Baseline
}

func (u *unsortedListStore) ListBaselines() ([]model.Baseline, error) { return u.baselines, nil }

func TestBaselineListSortsEvenWhenStoreReturnsUnordered(t *testing.T) {
	unsorted := &unsortedListStore{baselines: []model.Baseline{
		{Name: "zeta", RunIDs: []string{"r1"}},
		{Name: "alpha", RunIDs: []string{"r2"}},
		{Name: "mid", RunIDs: []string{"r3"}},
	}}
	f, err := os.CreateTemp(t.TempDir(), "*.db")
	require.NoError(t, err)
	_ = f.Close()
	want := []string{"alpha", "mid", "zeta"}

	var table bytes.Buffer
	require.NoError(t, runBaselineList(&table, openStore(unsorted), f.Name(), false))
	assert.Equal(t, want, baselineListNames(t, table.String()))

	var asJSON bytes.Buffer
	require.NoError(t, runBaselineList(&asJSON, openStore(unsorted), f.Name(), true))
	var baselines []model.Baseline
	require.NoError(t, json.Unmarshal(asJSON.Bytes(), &baselines))
	got := make([]string, 0, len(baselines))
	for _, b := range baselines {
		got = append(got, b.Name)
	}
	assert.Equal(t, want, got)
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

func (l *listErrStore) ListBaselines() ([]model.Baseline, error) { return nil, errStoreBoom }

func TestBaselineListErrorIsWrappedNotSwallowed(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "*.db")
	require.NoError(t, err)
	_ = f.Close()
	var buf bytes.Buffer
	err = runBaselineList(&buf, openStore(&listErrStore{}), f.Name(), false)
	require.ErrorIs(t, err, errStoreBoom)
	var opErr *operationalError
	require.ErrorAs(t, err, &opErr)
	assert.Empty(t, buf.String())
}

func TestBaselineRmStoreMissing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.db")
	err := runBaselineRm(io.Discard, store.OpenSQLite, missing, "x")
	require.ErrorIs(t, err, ErrStoreNotFound)
}

type deleteErrStore struct {
	fakeStore
}

func (d *deleteErrStore) DeleteBaseline(string) error { return errStoreBoom }

func TestBaselineRmErrorIsWrappedAndReportsNoRemoval(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "*.db")
	require.NoError(t, err)
	_ = f.Close()
	var buf bytes.Buffer
	err = runBaselineRm(&buf, openStore(&deleteErrStore{}), f.Name(), "x")
	require.ErrorIs(t, err, errStoreBoom)
	var opErr *operationalError
	require.ErrorAs(t, err, &opErr)
	assert.Empty(t, buf.String())
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
	failing := func(string) (store.Store, error) { return nil, errStoreBoom }
	err := runBaselineSet(io.Discard, failing, filepath.Join(t.TempDir(), "x.db"), "golden", []string{"variant=base"}, runsDir)
	require.ErrorIs(t, err, errStoreBoom)
	var opErr *operationalError
	require.ErrorAs(t, err, &opErr)
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

func subcommandNames(cmd *cobra.Command) []string {
	names := make([]string, 0, len(cmd.Commands()))
	for _, sub := range cmd.Commands() {
		names = append(names, sub.Name())
	}
	sort.Strings(names)
	return names
}

func TestBaselineCmdExposesEverySubcommand(t *testing.T) {
	root := newRootCmd()
	assert.Contains(t, subcommandNames(root), "baseline")

	var baseline *cobra.Command
	for _, sub := range root.Commands() {
		if sub.Name() == "baseline" {
			baseline = sub
		}
	}
	require.NotNil(t, baseline)
	assert.Equal(t, []string{"export", "import", "list", "rm", "set"}, subcommandNames(baseline))
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
