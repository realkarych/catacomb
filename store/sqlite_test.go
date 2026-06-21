package store

import (
	"database/sql"
	"encoding/json"
	"errors"
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

func count(t *testing.T, s *sqliteStore, table string) int {
	t.Helper()
	var n int
	require.NoError(t, s.db.QueryRow("SELECT count(*) FROM "+table).Scan(&n))
	return n
}

func TestPersistRoundTripCounts(t *testing.T) {
	s := fileStore(t)
	obs := []model.Observation{{ObsID: "obs-0", RunID: "s1", ExecutionID: "exec1", Seq: 0}}
	nodes := []*model.Node{{ID: "n1", RunID: "s1", Type: model.NodeSession}}
	edges := []*model.Edge{{ID: "e1", RunID: "s1", Type: model.EdgeParentChild, Src: "n1", Dst: "n2"}}
	require.NoError(t, s.Persist(obs, nodes, edges))
	require.NoError(t, s.Persist(nil, []*model.Node{{ID: "n1", RunID: "s1", Type: model.NodeSession, Status: model.StatusOK}}, nil))

	assert.Equal(t, 1, count(t, s, "observations"))
	assert.Equal(t, 1, count(t, s, "nodes"))
	assert.Equal(t, 1, count(t, s, "edges"))
}

func TestPersistWALEnabled(t *testing.T) {
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
	open := func(driver, dsn string) (*sql.DB, error) {
		db, err := sql.Open(driver, dsn)
		require.NoError(t, err)
		require.NoError(t, db.Close())
		return db, nil
	}
	_, err := openSQLite(open, ":memory:")
	require.Error(t, err)
}

func TestSchemaError(t *testing.T) {
	open := func(driver, dsn string) (*sql.DB, error) {
		db, err := sql.Open(driver, dsn)
		require.NoError(t, err)
		_, walErr := db.Exec("PRAGMA journal_mode=WAL")
		require.NoError(t, walErr)
		_, execErr := db.Exec("CREATE TABLE observations(x)")
		require.NoError(t, execErr)
		return db, nil
	}
	_, err := openSQLite(open, ":memory:")
	require.Error(t, err)
}

func TestPersistBeginError(t *testing.T) {
	s := fileStore(t)
	require.NoError(t, s.db.Close())
	require.Error(t, s.Persist([]model.Observation{{ObsID: "x"}}, nil, nil))
}

func TestPersistExecError(t *testing.T) {
	s := fileStore(t)
	dup := []model.Observation{{ObsID: "same"}, {ObsID: "same"}}
	require.Error(t, s.Persist(dup, nil, nil))
	assert.Equal(t, 0, count(t, s, "observations"))
}

func TestPersistNodeMarshalError(t *testing.T) {
	s := fileStore(t)
	s.marshal = func(v any) ([]byte, error) {
		if _, ok := v.(*model.Node); ok {
			return nil, errors.New("boom")
		}
		return json.Marshal(v)
	}
	require.Error(t, s.Persist(nil, []*model.Node{{ID: "n"}}, nil))
}

func TestPersistEdgeMarshalError(t *testing.T) {
	s := fileStore(t)
	s.marshal = func(v any) ([]byte, error) {
		if _, ok := v.(*model.Edge); ok {
			return nil, errors.New("boom")
		}
		return json.Marshal(v)
	}
	require.Error(t, s.Persist(nil, nil, []*model.Edge{{ID: "e"}}))
}

func TestOpenSQLitePublicHappyPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "g.db")
	s, err := OpenSQLite(path)
	require.NoError(t, err)
	require.NoError(t, s.Close())
}

func TestAppendAndApplyRoundTrip(t *testing.T) {
	s := fileStore(t)
	o := model.Observation{ObsID: "o1", RunID: "s1", ExecutionID: "exec1", Seq: 1}
	nodes := []*model.Node{{ID: "n1", RunID: "s1", Type: model.NodeSession}}
	edges := []*model.Edge{{ID: "e1", RunID: "s1", Type: model.EdgeParentChild, Src: "n1", Dst: "n2"}}
	require.NoError(t, s.AppendAndApply(o, nodes, edges))

	assert.Equal(t, 1, count(t, s, "observations"))
	assert.Equal(t, 1, count(t, s, "nodes"))
	assert.Equal(t, 1, count(t, s, "edges"))
}

func TestAppendAndApplyBeginError(t *testing.T) {
	s := fileStore(t)
	require.NoError(t, s.db.Close())
	require.Error(t, s.AppendAndApply(model.Observation{ObsID: "x"}, nil, nil))
}

func TestAppendAndApplyObsError(t *testing.T) {
	s := fileStore(t)
	require.NoError(t, s.AppendAndApply(model.Observation{ObsID: "dup"}, nil, nil))
	require.Error(t, s.AppendAndApply(model.Observation{ObsID: "dup"}, nil, nil))
}

func TestAppendAndApplyNodeMarshalError(t *testing.T) {
	s := fileStore(t)
	s.marshal = func(v any) ([]byte, error) {
		if _, ok := v.(*model.Node); ok {
			return nil, errors.New("boom")
		}
		return json.Marshal(v)
	}
	require.Error(t, s.AppendAndApply(model.Observation{ObsID: "o"}, []*model.Node{{ID: "n"}}, nil))
	assert.Equal(t, 0, count(t, s, "observations"))
}

func TestMaxSeqEmpty(t *testing.T) {
	s := fileStore(t)
	v, err := s.MaxSeq()
	require.NoError(t, err)
	assert.Equal(t, uint64(0), v)
}

func TestMaxSeqAfterAppend(t *testing.T) {
	s := fileStore(t)
	require.NoError(t, s.AppendAndApply(model.Observation{ObsID: "a", Seq: 3}, nil, nil))
	require.NoError(t, s.AppendAndApply(model.Observation{ObsID: "b", Seq: 7}, nil, nil))
	v, err := s.MaxSeq()
	require.NoError(t, err)
	assert.Equal(t, uint64(7), v)
}

func TestMaxSeqQueryError(t *testing.T) {
	s := fileStore(t)
	require.NoError(t, s.db.Close())
	_, err := s.MaxSeq()
	require.Error(t, err)
}

func TestObservationsSince(t *testing.T) {
	s := fileStore(t)
	require.NoError(t, s.AppendAndApply(model.Observation{ObsID: "a", RunID: "s1", ExecutionID: "e", Seq: 1, Kind: "session_start"}, nil, nil))
	require.NoError(t, s.AppendAndApply(model.Observation{ObsID: "b", RunID: "s1", ExecutionID: "e", Seq: 2, Kind: "user_prompt"}, nil, nil))
	require.NoError(t, s.AppendAndApply(model.Observation{ObsID: "c", RunID: "s1", ExecutionID: "e", Seq: 3, Kind: "session_end"}, nil, nil))

	got, err := s.ObservationsSince(1)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "user_prompt", got[0].Kind)
	assert.Equal(t, "session_end", got[1].Kind)
}

func TestObservationsSinceQueryError(t *testing.T) {
	s := fileStore(t)
	require.NoError(t, s.db.Close())
	_, err := s.ObservationsSince(0)
	require.Error(t, err)
}

func TestObservationsSinceDecodeError(t *testing.T) {
	s := fileStore(t)
	_, err := s.db.Exec(`INSERT INTO observations(obs_id, run_id, execution_id, seq, body) VALUES('bad','s1','e',1,'{not json}')`)
	require.NoError(t, err)
	_, err = s.ObservationsSince(0)
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

func TestScanObservationsScanError(t *testing.T) {
	_, err := scanObservations(&fakeRows{bodies: []string{"x"}, scanErr: errors.New("scan")})
	require.Error(t, err)
}

func TestScanObservationsRowsErr(t *testing.T) {
	_, err := scanObservations(&fakeRows{errErr: errors.New("iter")})
	require.Error(t, err)
}

func TestUpsertRunRoundTrip(t *testing.T) {
	s := fileStore(t)
	require.NoError(t, s.UpsertRun(model.Run{ID: "r1", Status: model.StatusRunning, LastSeq: 4}))
	require.NoError(t, s.UpsertRun(model.Run{ID: "r1", Status: model.StatusOK, LastSeq: 9}))
	runs, err := s.Runs()
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, model.StatusOK, runs[0].Status)
	assert.Equal(t, uint64(9), runs[0].LastSeq)
}

func TestListOpenRunsFiltersByStatus(t *testing.T) {
	s := fileStore(t)
	require.NoError(t, s.UpsertRun(model.Run{ID: "open", Status: model.StatusRunning}))
	require.NoError(t, s.UpsertRun(model.Run{ID: "done", Status: model.StatusOK}))
	open, err := s.ListOpenRuns()
	require.NoError(t, err)
	require.Len(t, open, 1)
	assert.Equal(t, "open", open[0].ID)
}

func TestUpsertRunMarshalError(t *testing.T) {
	s := fileStore(t)
	s.marshal = func(any) ([]byte, error) { return nil, errors.New("marshal") }
	assert.Error(t, s.UpsertRun(model.Run{ID: "r1"}))
}

func TestUpsertRunExecError(t *testing.T) {
	s := fileStore(t)
	require.NoError(t, s.db.Close())
	assert.Error(t, s.UpsertRun(model.Run{ID: "r1"}))
}

func TestListOpenRunsQueryError(t *testing.T) {
	s := fileStore(t)
	require.NoError(t, s.db.Close())
	_, err := s.ListOpenRuns()
	assert.Error(t, err)
}

func TestRunsQueryError(t *testing.T) {
	s := fileStore(t)
	require.NoError(t, s.db.Close())
	_, err := s.Runs()
	assert.Error(t, err)
}

func TestScanRunsScanError(t *testing.T) {
	_, err := scanRuns(&fakeRows{bodies: []string{"x"}, scanErr: errors.New("scan")})
	assert.Error(t, err)
}

func TestScanRunsDecodeError(t *testing.T) {
	_, err := scanRuns(&fakeRows{bodies: []string{"not-json"}})
	assert.Error(t, err)
}

func TestScanRunsIterError(t *testing.T) {
	_, err := scanRuns(&fakeRows{errErr: errors.New("iter")})
	assert.Error(t, err)
}

func TestListOpenRunsDecodeError(t *testing.T) {
	s := fileStore(t)
	_, err := s.db.Exec(`INSERT INTO runs(run_id, status, body) VALUES('bad','running','{not json}')`)
	require.NoError(t, err)
	_, err = s.ListOpenRuns()
	assert.Error(t, err)
}

func TestRunsDecodeError(t *testing.T) {
	s := fileStore(t)
	_, err := s.db.Exec(`INSERT INTO runs(run_id, status, body) VALUES('bad','running','{not json}')`)
	require.NoError(t, err)
	_, err = s.Runs()
	assert.Error(t, err)
}

func TestQuarantineRoundTrip(t *testing.T) {
	s := fileStore(t)
	require.NoError(t, s.Quarantine(model.QuarantineRecord{Raw: []byte("{bad"), HookType: "PreToolUse", Err: "boom", At: time.Unix(1, 0).UTC()}))
	require.NoError(t, s.Quarantine(model.QuarantineRecord{Raw: []byte("x"), HookType: "Stop", Err: "panic", At: time.Unix(2, 0).UTC()}))
	n, err := s.QuarantineCount()
	require.NoError(t, err)
	assert.Equal(t, int64(2), n)
}

func TestQuarantineMarshalError(t *testing.T) {
	s := fileStore(t)
	s.marshal = func(any) ([]byte, error) { return nil, errors.New("marshal") }
	assert.Error(t, s.Quarantine(model.QuarantineRecord{}))
}

func TestQuarantineExecError(t *testing.T) {
	s := fileStore(t)
	require.NoError(t, s.db.Close())
	assert.Error(t, s.Quarantine(model.QuarantineRecord{}))
}

func TestQuarantineCountQueryError(t *testing.T) {
	s := fileStore(t)
	require.NoError(t, s.db.Close())
	_, err := s.QuarantineCount()
	assert.Error(t, err)
}
