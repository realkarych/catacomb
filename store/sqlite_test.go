package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
)

func fileStore(t *testing.T) *sqliteStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "g.db")
	s, err := openSQLite(sql.Open, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s.(*sqliteStore)
}

func TestOpenSQLiteWALEnabled(t *testing.T) {
	s := fileStore(t)
	var mode string
	require.NoError(t, s.db.QueryRow("PRAGMA journal_mode").Scan(&mode))
	assert.Equal(t, "wal", mode)
}

func TestOpenError(t *testing.T) {
	open := func(string, string) (*sql.DB, error) { return nil, errors.New("boom") }
	_, err := openSQLite(open, ":memory:")
	require.Error(t, err)
}

func TestWALError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ro.db")
	seed, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	_, err = seed.Exec(schema)
	require.NoError(t, err)
	require.NoError(t, seed.Close())
	open := func(driver, _ string) (*sql.DB, error) {
		return sql.Open(driver, "file:"+path+"?mode=ro")
	}
	_, err = openSQLite(open, path)
	require.Error(t, err)
}

func TestSchemaError(t *testing.T) {
	open := func(driver, dsn string) (*sql.DB, error) {
		db, err := sql.Open(driver, dsn)
		require.NoError(t, err)
		_, walErr := db.Exec("PRAGMA journal_mode=WAL")
		require.NoError(t, walErr)
		_, execErr := db.Exec("CREATE VIEW observations AS SELECT 1")
		require.NoError(t, execErr)
		return db, nil
	}
	_, err := openSQLite(open, ":memory:")
	require.Error(t, err)
}

func TestOpenSQLitePublicHappyPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "g.db")
	s, err := OpenSQLite(path)
	require.NoError(t, err)
	require.NoError(t, s.Close())
}

func TestOpenSQLiteCreatesParentDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "nested", "g.db")
	s, err := openSQLite(sql.Open, path)
	require.NoError(t, err)
	require.NoError(t, s.Close())
}

func TestOpenSQLiteMkdirError(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "nodir")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o600))
	_, err := OpenSQLite(filepath.Join(blocker, "g.db"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mkdir")
}

func TestMarshalVerbatimError(t *testing.T) {
	_, err := marshalVerbatim(make(chan int))
	require.Error(t, err)
}

type fakeRows struct {
	bodies  []string
	i       int
	scanErr error
	errErr  error
}

func (f *fakeRows) Next() bool { return f.i < len(f.bodies) }

func (f *fakeRows) Scan(dest ...any) error {
	if f.scanErr != nil {
		return f.scanErr
	}
	p, _ := dest[0].(*string)
	*p = f.bodies[f.i]
	f.i++
	return nil
}

func (f *fakeRows) Err() error { return f.errErr }

func (f *fakeRows) Close() error { return nil }

func TestOpenSQLiteReadOnlyReadsExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "g.db")
	s, err := OpenSQLite(path)
	require.NoError(t, err)
	require.NoError(t, s.UpsertBaseline(model.Baseline{Name: "golden", RunIDs: []string{"r1"}}))
	require.NoError(t, s.Close())

	ro, err := OpenSQLiteReadOnly(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ro.Close() })

	b, ok, err := ro.GetBaseline("golden")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, []string{"r1"}, b.RunIDs)
}

func TestOpenSQLiteReadOnlyOpenError(t *testing.T) {
	_, err := openSQLiteReadOnly(func(string, string) (*sql.DB, error) { return nil, assert.AnError }, "/any/path.db")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "store.OpenSQLiteReadOnly")
}

func TestOpenSQLiteReadOnlyPingError(t *testing.T) {
	_, err := openSQLiteReadOnly(sql.Open, "/nonexistent/dir/g.db")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ping")
}

func TestReadOnlyDSN(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "posix absolute path", path: "/tmp/g.db", want: "file:///tmp/g.db?mode=ro&_pragma=busy_timeout(5000)"},
		{name: "windows style path", path: "C:/Users/x.db", want: "file:///C:/Users/x.db?mode=ro&_pragma=busy_timeout(5000)"},
		{name: "path with space", path: "/tmp/a b/g.db", want: "file:///tmp/a%20b/g.db?mode=ro&_pragma=busy_timeout(5000)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, readOnlyDSN(tt.path))
		})
	}
}

func TestWriteDSN(t *testing.T) {
	assert.Equal(t, "/tmp/g.db?_pragma=busy_timeout(5000)", writeDSN("/tmp/g.db"))
}

func busyTimeoutMillis(t *testing.T, db *sql.DB) int {
	t.Helper()
	var ms int
	require.NoError(t, db.QueryRow("PRAGMA busy_timeout").Scan(&ms))
	return ms
}

func TestOpenSQLiteSetsBusyTimeout(t *testing.T) {
	s := fileStore(t)
	assert.Equal(t, 5000, busyTimeoutMillis(t, s.db))
}

func TestOpenSQLiteReadOnlySetsBusyTimeout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "g.db")
	s, err := OpenSQLite(path)
	require.NoError(t, err)
	require.NoError(t, s.Close())
	ro, err := OpenSQLiteReadOnly(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ro.Close() })
	assert.Equal(t, 5000, busyTimeoutMillis(t, ro.(*sqliteStore).db))
}

func TestSecondWriterWaitsWhileFirstHoldsWriteLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "g.db")
	first, err := OpenSQLite(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = first.Close() })
	second, err := OpenSQLite(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = second.Close() })

	tx, err := first.(*sqliteStore).db.Begin()
	require.NoError(t, err)
	_, err = tx.Exec(`INSERT INTO baselines(name, body) VALUES('held','{"name":"held"}')`)
	require.NoError(t, err)

	release := time.AfterFunc(100*time.Millisecond, func() { _ = tx.Commit() })
	t.Cleanup(func() { release.Stop() })

	seq, err := second.AppendRegressResult("contended", json.RawMessage(`{"n":1}`))
	require.NoError(t, err)
	assert.Equal(t, 1, seq)

	require.NoError(t, second.UpsertBaseline(model.Baseline{Name: "after-wait"}))
	held, ok, err := second.GetBaseline("held")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "held", held.Name)
}

func TestOpenSQLiteReadOnlyPathWithSpace(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a b")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, "my db.db")
	s, err := OpenSQLite(path)
	require.NoError(t, err)
	require.NoError(t, s.UpsertBaseline(model.Baseline{Name: "golden"}))
	require.NoError(t, s.Close())
	ro, err := OpenSQLiteReadOnly(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ro.Close() })
	b, ok, err := ro.GetBaseline("golden")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "golden", b.Name)
}

func TestOpenSQLiteReadOnlyRelativePath(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	s, err := OpenSQLite(filepath.Join(dir, "g.db"))
	require.NoError(t, err)
	require.NoError(t, s.UpsertBaseline(model.Baseline{Name: "golden"}))
	require.NoError(t, s.Close())

	t.Chdir(dir)
	ro, err := OpenSQLiteReadOnly("./g.db")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ro.Close() })
	b, ok, err := ro.GetBaseline("golden")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "golden", b.Name)
}

func TestOpenSQLiteReadOnlyAbsError(t *testing.T) {
	orig := absFn
	absFn = func(string) (string, error) { return "", errors.New("boom") }
	t.Cleanup(func() { absFn = orig })
	_, err := OpenSQLiteReadOnly("x.db")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "abs")
}

func TestUpsertBaselineMarshalError(t *testing.T) {
	s := fileStore(t)
	s.marshal = func(any) ([]byte, error) { return nil, errors.New("marshal") }
	assert.Error(t, s.UpsertBaseline(model.Baseline{Name: "b"}))
}

func TestUpsertBaselineExecError(t *testing.T) {
	s := fileStore(t)
	require.NoError(t, s.db.Close())
	assert.Error(t, s.UpsertBaseline(model.Baseline{Name: "b"}))
}

func TestGetBaselineQueryError(t *testing.T) {
	s := fileStore(t)
	require.NoError(t, s.db.Close())
	_, _, err := s.GetBaseline("b")
	assert.Error(t, err)
}

func TestGetBaselineDecodeError(t *testing.T) {
	s := fileStore(t)
	_, err := s.db.Exec(`INSERT INTO baselines(name, body) VALUES('b','{not json}')`)
	require.NoError(t, err)
	_, _, err = s.GetBaseline("b")
	assert.Error(t, err)
}

func TestListBaselinesQueryError(t *testing.T) {
	s := fileStore(t)
	require.NoError(t, s.db.Close())
	_, err := s.ListBaselines()
	assert.Error(t, err)
}

func TestListBaselinesDecodeError(t *testing.T) {
	s := fileStore(t)
	_, err := s.db.Exec(`INSERT INTO baselines(name, body) VALUES('b','{not json}')`)
	require.NoError(t, err)
	_, err = s.ListBaselines()
	assert.Error(t, err)
}

func TestScanBaselinesScanError(t *testing.T) {
	_, err := scanBaselines(&fakeRows{bodies: []string{"x"}, scanErr: errors.New("scan")})
	assert.Error(t, err)
}

func TestScanBaselinesIterError(t *testing.T) {
	_, err := scanBaselines(&fakeRows{errErr: errors.New("iter")})
	assert.Error(t, err)
}

func TestDeleteBaselineExecError(t *testing.T) {
	s := fileStore(t)
	require.NoError(t, s.db.Close())
	assert.Error(t, s.DeleteBaseline("b"))
}

func currentStoreMissingTable(t *testing.T, table string) Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "g.db")
	s, err := openSQLite(sql.Open, path)
	require.NoError(t, err)
	require.NoError(t, s.Close())
	raw, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	_, err = raw.Exec("DROP TABLE " + table)
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	ro, err := openSQLiteReadOnly(sql.Open, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ro.Close() })
	return ro
}

func TestGetBaselineMissingTableReportsOutdated(t *testing.T) {
	s := currentStoreMissingTable(t, "baselines")
	_, _, err := s.GetBaseline("x")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSchemaOutdated)
}

func TestListBaselinesMissingTableReportsOutdated(t *testing.T) {
	s := currentStoreMissingTable(t, "baselines")
	_, err := s.ListBaselines()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSchemaOutdated)
}

func TestRegressResultsForMissingTableReportsOutdated(t *testing.T) {
	s := currentStoreMissingTable(t, "regress_results")
	_, err := s.RegressResultsFor("golden")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSchemaOutdated)
}

func TestAppendRegressResultBeginError(t *testing.T) {
	s := fileStore(t)
	require.NoError(t, s.db.Close())
	_, err := s.AppendRegressResult("base", json.RawMessage(`{}`))
	require.Error(t, err)
}

func TestAppendRegressResultMissingTableError(t *testing.T) {
	s := fileStore(t)
	_, err := s.db.Exec("DROP TABLE regress_results")
	require.NoError(t, err)
	_, err = s.AppendRegressResult("base", json.RawMessage(`{}`))
	require.Error(t, err)
}

func TestAppendRegressResultInsertError(t *testing.T) {
	s := fileStore(t)
	_, err := s.db.Exec(`CREATE TRIGGER block_regress BEFORE INSERT ON regress_results BEGIN SELECT RAISE(ABORT, 'blocked'); END`)
	require.NoError(t, err)
	_, err = s.AppendRegressResult("base", json.RawMessage(`{}`))
	require.Error(t, err)
}

func TestRegressResultsForQueryError(t *testing.T) {
	s := fileStore(t)
	require.NoError(t, s.db.Close())
	_, err := s.RegressResultsFor("base")
	require.Error(t, err)
}

func TestRegressResultsForScanError(t *testing.T) {
	s := fileStore(t)
	_, err := s.db.Exec("DROP TABLE regress_results")
	require.NoError(t, err)
	_, err = s.db.Exec("CREATE TABLE regress_results (baseline TEXT NOT NULL, seq TEXT NOT NULL, body TEXT NOT NULL, PRIMARY KEY (baseline, seq))")
	require.NoError(t, err)
	_, err = s.db.Exec(`INSERT INTO regress_results VALUES('base','not-an-int','{}')`)
	require.NoError(t, err)
	_, err = s.RegressResultsFor("base")
	require.Error(t, err)
}

func TestScanRegressResultsScanError(t *testing.T) {
	_, err := scanRegressResults("base", &fakeRows{bodies: []string{"x"}, scanErr: errors.New("scan")})
	require.Error(t, err)
}

func TestScanRegressResultsIterError(t *testing.T) {
	_, err := scanRegressResults("base", &fakeRows{errErr: errors.New("iter")})
	require.Error(t, err)
}
