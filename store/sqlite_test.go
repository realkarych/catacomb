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

	"github.com/realkarych/catacomb/cdc"
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

func countWhere(t *testing.T, s *sqliteStore, table, id string) int {
	t.Helper()
	var n int
	require.NoError(t, s.db.QueryRow("SELECT count(*) FROM "+table+" WHERE id = ?", id).Scan(&n))
	return n
}

func nodeStatus(t *testing.T, s *sqliteStore, id string) string {
	t.Helper()
	var body string
	require.NoError(t, s.db.QueryRow("SELECT body FROM nodes WHERE id = ?", id).Scan(&body))
	var n model.Node
	require.NoError(t, json.Unmarshal([]byte(body), &n))
	return string(n.Status)
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

func TestOpenSQLiteCreatesParentDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "nested", "g.db")
	s, err := openSQLite(sql.Open, path)
	require.NoError(t, err)
	require.NoError(t, s.Close())
}

func TestAppendDeltasInsertsObservation(t *testing.T) {
	s := fileStore(t)
	o := model.Observation{ObsID: "o1", RunID: "s1", ExecutionID: "exec1", Seq: 1}
	require.NoError(t, s.AppendDeltas(o, nil))
	assert.Equal(t, 1, count(t, s, "observations"))
	assert.Equal(t, 0, count(t, s, "nodes"))
	assert.Equal(t, 0, count(t, s, "edges"))
}

func TestAppendDeltasUpsertsNode(t *testing.T) {
	s := fileStore(t)
	o := model.Observation{ObsID: "o1", RunID: "s1", ExecutionID: "exec1", Seq: 1}
	n := &model.Node{ID: "n1", RunID: "s1", Type: model.NodeSession}
	require.NoError(t, s.AppendDeltas(o, []cdc.GraphDelta{{Kind: cdc.DeltaNodeUpsert, Node: n}}))
	assert.Equal(t, 1, count(t, s, "nodes"))

	o2 := model.Observation{ObsID: "o2", RunID: "s1", ExecutionID: "exec1", Seq: 2}
	n2 := &model.Node{ID: "n1", RunID: "s1", Type: model.NodeSession, Status: model.StatusOK}
	require.NoError(t, s.AppendDeltas(o2, []cdc.GraphDelta{{Kind: cdc.DeltaNodeStatus, Node: n2}}))
	assert.Equal(t, 1, count(t, s, "nodes"))
	assert.Equal(t, string(model.StatusOK), nodeStatus(t, s, "n1"))
}

func TestAppendDeltasUpsertsEdge(t *testing.T) {
	s := fileStore(t)
	o := model.Observation{ObsID: "o1", RunID: "s1", ExecutionID: "exec1", Seq: 1}
	e := &model.Edge{ID: "e1", RunID: "s1", Type: model.EdgeParentChild, Src: "n1", Dst: "n2"}
	require.NoError(t, s.AppendDeltas(o, []cdc.GraphDelta{{Kind: cdc.DeltaEdgeUpsert, Edge: e}}))
	assert.Equal(t, 1, count(t, s, "edges"))
}

func TestAppendDeltasDeletesEdge(t *testing.T) {
	s := fileStore(t)
	e := &model.Edge{ID: "e1", RunID: "s1", Type: model.EdgeParentChild, Src: "n1", Dst: "n2"}
	require.NoError(t, s.AppendDeltas(
		model.Observation{ObsID: "o1", RunID: "s1", ExecutionID: "exec1", Seq: 1},
		[]cdc.GraphDelta{{Kind: cdc.DeltaEdgeUpsert, Edge: e}},
	))
	assert.Equal(t, 1, count(t, s, "edges"))

	require.NoError(t, s.AppendDeltas(
		model.Observation{ObsID: "o2", RunID: "s1", ExecutionID: "exec1", Seq: 2},
		[]cdc.GraphDelta{{Kind: cdc.DeltaEdgeDelete, Edge: e}},
	))
	assert.Equal(t, 0, count(t, s, "edges"))
}

func TestAppendDeltasIgnoresNilAndRunKinds(t *testing.T) {
	s := fileStore(t)
	o := model.Observation{ObsID: "o1", RunID: "s1", ExecutionID: "exec1", Seq: 1}
	deltas := []cdc.GraphDelta{
		{Kind: cdc.DeltaNodeUpsert},
		{Kind: cdc.DeltaNodeMerge},
		{Kind: cdc.DeltaEdgeUpsert},
		{Kind: cdc.DeltaEdgeDelete},
		{Kind: cdc.DeltaRunStarted, RunID: "s1"},
		{Kind: cdc.DeltaSessionEnded, RunID: "s1"},
		{Kind: cdc.DeltaRunEnded, RunID: "s1"},
	}
	require.NoError(t, s.AppendDeltas(o, deltas))
	assert.Equal(t, 1, count(t, s, "observations"))
	assert.Equal(t, 0, count(t, s, "nodes"))
	assert.Equal(t, 0, count(t, s, "edges"))
}

func TestAppendDeltasBeginError(t *testing.T) {
	s := fileStore(t)
	require.NoError(t, s.db.Close())
	require.Error(t, s.AppendDeltas(model.Observation{ObsID: "x"}, nil))
}

func TestAppendDeltasObsError(t *testing.T) {
	s := fileStore(t)
	require.NoError(t, s.AppendDeltas(model.Observation{ObsID: "dup"}, nil))
	require.Error(t, s.AppendDeltas(model.Observation{ObsID: "dup"}, nil))
}

func TestAppendDeltasNodeMarshalErrorRollsBack(t *testing.T) {
	s := fileStore(t)
	s.marshal = func(v any) ([]byte, error) {
		if _, ok := v.(*model.Node); ok {
			return nil, errors.New("boom")
		}
		return json.Marshal(v)
	}
	err := s.AppendDeltas(model.Observation{ObsID: "o"}, []cdc.GraphDelta{{Kind: cdc.DeltaNodeUpsert, Node: &model.Node{ID: "n"}}})
	require.Error(t, err)
	assert.Equal(t, 0, count(t, s, "observations"))
}

func TestAppendDeltasEdgeMarshalErrorRollsBack(t *testing.T) {
	s := fileStore(t)
	s.marshal = func(v any) ([]byte, error) {
		if _, ok := v.(*model.Edge); ok {
			return nil, errors.New("boom")
		}
		return json.Marshal(v)
	}
	err := s.AppendDeltas(model.Observation{ObsID: "o"}, []cdc.GraphDelta{{Kind: cdc.DeltaEdgeUpsert, Edge: &model.Edge{ID: "e"}}})
	require.Error(t, err)
	assert.Equal(t, 0, count(t, s, "observations"))
}

func TestAppendDeltasEdgeDeleteExecErrorRollsBack(t *testing.T) {
	s := fileStore(t)
	_, err := s.db.Exec("DROP TABLE edges")
	require.NoError(t, err)
	err = s.AppendDeltas(model.Observation{ObsID: "o"}, []cdc.GraphDelta{{Kind: cdc.DeltaEdgeDelete, Edge: &model.Edge{ID: "e"}}})
	require.Error(t, err)
	assert.Equal(t, 0, count(t, s, "observations"))
}

func TestAppendDeltasNodeMergeDeletesOldRow(t *testing.T) {
	s := fileStore(t)
	old := &model.Node{ID: "old", RunID: "s1", Type: model.NodeToolCall}
	require.NoError(t, s.AppendDeltas(
		model.Observation{ObsID: "o1", RunID: "s1", ExecutionID: "exec1", Seq: 1},
		[]cdc.GraphDelta{{Kind: cdc.DeltaNodeUpsert, Node: old}},
	))
	assert.Equal(t, 1, count(t, s, "nodes"))

	merged := &model.Node{ID: "new", RunID: "s1", Type: model.NodeToolCall}
	require.NoError(t, s.AppendDeltas(
		model.Observation{ObsID: "o2", RunID: "s1", ExecutionID: "exec1", Seq: 2},
		[]cdc.GraphDelta{{Kind: cdc.DeltaNodeMerge, OldID: "old", NewID: "new", Node: merged}},
	))
	assert.Equal(t, 1, count(t, s, "nodes"))
	assert.Equal(t, 0, countWhere(t, s, "nodes", "old"))
	assert.Equal(t, 1, countWhere(t, s, "nodes", "new"))
}

func TestAppendDeltasNodeMergeNoOldIDJustUpserts(t *testing.T) {
	s := fileStore(t)
	n := &model.Node{ID: "n", RunID: "s1", Type: model.NodeToolCall}
	require.NoError(t, s.AppendDeltas(
		model.Observation{ObsID: "o1", RunID: "s1", ExecutionID: "exec1", Seq: 1},
		[]cdc.GraphDelta{{Kind: cdc.DeltaNodeMerge, Node: n}},
	))
	assert.Equal(t, 1, countWhere(t, s, "nodes", "n"))
}

func TestAppendDeltasNodeMergeDeleteExecErrorRollsBack(t *testing.T) {
	s := fileStore(t)
	_, err := s.db.Exec("DROP TABLE nodes")
	require.NoError(t, err)
	err = s.AppendDeltas(
		model.Observation{ObsID: "o", RunID: "s1", ExecutionID: "exec1", Seq: 1},
		[]cdc.GraphDelta{{Kind: cdc.DeltaNodeMerge, OldID: "old", Node: &model.Node{ID: "new"}}},
	)
	require.Error(t, err)
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
	require.NoError(t, s.AppendDeltas(model.Observation{ObsID: "a", Seq: 3}, nil))
	require.NoError(t, s.AppendDeltas(model.Observation{ObsID: "b", Seq: 7}, nil))
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
	require.NoError(t, s.AppendDeltas(model.Observation{ObsID: "a", RunID: "s1", ExecutionID: "e", Seq: 1, Kind: "session_start"}, nil))
	require.NoError(t, s.AppendDeltas(model.Observation{ObsID: "b", RunID: "s1", ExecutionID: "e", Seq: 2, Kind: "user_prompt"}, nil))
	require.NoError(t, s.AppendDeltas(model.Observation{ObsID: "c", RunID: "s1", ExecutionID: "e", Seq: 3, Kind: "session_end"}, nil))

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

func TestUpsertRunLabelsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "labels.db")
	s, err := openSQLite(sql.Open, path)
	require.NoError(t, err)
	labels := map[string]string{"basket": "checkout", "rep": "1"}
	require.NoError(t, s.UpsertRun(model.Run{ID: "r1", Status: model.StatusOK, Labels: labels}))
	require.NoError(t, s.Close())

	reopened, err := openSQLite(sql.Open, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = reopened.Close() })
	runs, err := reopened.Runs()
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, labels, runs[0].Labels)
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

func TestObservationsForExecution(t *testing.T) {
	s := fileStore(t)
	require.NoError(t, s.AppendDeltas(model.Observation{ObsID: "a", RunID: "s1", ExecutionID: "e1", Seq: 1, Kind: "session_start"}, nil))
	require.NoError(t, s.AppendDeltas(model.Observation{ObsID: "b", RunID: "s2", ExecutionID: "e2", Seq: 2, Kind: "session_start"}, nil))
	require.NoError(t, s.AppendDeltas(model.Observation{ObsID: "c", RunID: "s1", ExecutionID: "e1", Seq: 3, Kind: "session_end"}, nil))
	obs, err := s.ObservationsForExecution("e1")
	require.NoError(t, err)
	require.Len(t, obs, 2)
	assert.Equal(t, uint64(1), obs[0].Seq)
	assert.Equal(t, uint64(3), obs[1].Seq)
}

func TestObservationsForExecutionQueryError(t *testing.T) {
	s := fileStore(t)
	require.NoError(t, s.db.Close())
	_, err := s.ObservationsForExecution("e1")
	assert.Error(t, err)
}

func TestObservationsForExecutionDecodeError(t *testing.T) {
	s := fileStore(t)
	_, err := s.db.Exec(`INSERT INTO observations(obs_id, run_id, execution_id, seq, body) VALUES('bad','s1','e1',1,'{not json}')`)
	require.NoError(t, err)
	_, err = s.ObservationsForExecution("e1")
	assert.Error(t, err)
}

func TestTailCursorUpsertAndLoad(t *testing.T) {
	s := fileStore(t)
	require.NoError(t, s.UpsertTailCursor(model.TailCursor{Path: "/p/a.jsonl", Offset: 10, Fingerprint: "f1", Size: 10, Mtime: 1}))
	require.NoError(t, s.UpsertTailCursor(model.TailCursor{Path: "/p/b.jsonl", Offset: 20, Fingerprint: "f2", Size: 20, Mtime: 2}))
	require.NoError(t, s.UpsertTailCursor(model.TailCursor{Path: "/p/a.jsonl", Offset: 30, Fingerprint: "f3", Size: 30, Mtime: 3}))

	got, err := s.LoadTailCursors()
	require.NoError(t, err)
	require.Len(t, got, 2)
	byPath := map[string]model.TailCursor{}
	for _, c := range got {
		byPath[c.Path] = c
	}
	assert.Equal(t, int64(30), byPath["/p/a.jsonl"].Offset)
	assert.Equal(t, "f3", byPath["/p/a.jsonl"].Fingerprint)
	assert.Equal(t, int64(20), byPath["/p/b.jsonl"].Offset)
}

func TestUpsertTailCursorExecError(t *testing.T) {
	s := fileStore(t)
	require.NoError(t, s.db.Close())
	assert.Error(t, s.UpsertTailCursor(model.TailCursor{Path: "/p/x.jsonl", Offset: 1}))
}

func TestLoadTailCursorsQueryError(t *testing.T) {
	s := fileStore(t)
	require.NoError(t, s.db.Close())
	_, err := s.LoadTailCursors()
	assert.Error(t, err)
}

func TestLoadTailCursorsScanError(t *testing.T) {
	s := fileStore(t)
	_, err := s.db.Exec("DROP TABLE tail_cursors")
	require.NoError(t, err)
	_, err = s.db.Exec("CREATE TABLE tail_cursors (path TEXT PRIMARY KEY, offset TEXT, fingerprint TEXT, size INTEGER, mtime INTEGER)")
	require.NoError(t, err)
	_, err = s.db.Exec(`INSERT INTO tail_cursors VALUES('p','not-an-int','f',1,1)`)
	require.NoError(t, err)
	_, err = s.LoadTailCursors()
	assert.Error(t, err)
}

func TestScanTailCursorsIterError(t *testing.T) {
	_, err := scanTailCursors(&fakeRows{errErr: errors.New("iter")})
	assert.Error(t, err)
}

func TestOpenSQLiteReadOnlyReadsExisting(t *testing.T) {
	tests := []struct {
		name  string
		runID string
	}{
		{name: "single run round-trip", runID: "r1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "g.db")
			s, err := OpenSQLite(path)
			require.NoError(t, err)
			require.NoError(t, s.UpsertRun(model.Run{ID: tt.runID, Status: model.StatusRunning}))
			require.NoError(t, s.Close())

			ro, err := OpenSQLiteReadOnly(path)
			require.NoError(t, err)
			t.Cleanup(func() { _ = ro.Close() })

			runs, err := ro.Runs()
			require.NoError(t, err)
			require.Len(t, runs, 1)
			assert.Equal(t, tt.runID, runs[0].ID)
		})
	}
}

func TestOpenSQLiteReadOnlyOpenError(t *testing.T) {
	tests := []struct {
		name    string
		open    func(string, string) (*sql.DB, error)
		path    string
		wantMsg string
	}{
		{
			name:    "open returns error",
			open:    func(string, string) (*sql.DB, error) { return nil, assert.AnError },
			path:    "/any/path.db",
			wantMsg: "store.OpenSQLiteReadOnly",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := openSQLiteReadOnly(tt.open, tt.path)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantMsg)
		})
	}
}

func TestOpenSQLiteReadOnlyPingError(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantMsg string
	}{
		{name: "nonexistent directory", path: "/nonexistent/dir/g.db", wantMsg: "ping"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := openSQLiteReadOnly(sql.Open, tt.path)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantMsg)
		})
	}
}

func TestReadOnlyDSN(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "posix absolute path", path: "/tmp/g.db", want: "file:///tmp/g.db?mode=ro"},
		{name: "windows style path", path: "C:/Users/x.db", want: "file:///C:/Users/x.db?mode=ro"},
		{name: "path with space", path: "/tmp/a b/g.db", want: "file:///tmp/a%20b/g.db?mode=ro"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, readOnlyDSN(tt.path))
		})
	}
}

func TestOpenSQLiteReadOnlyPathWithSpace(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a b")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, "my db.db")
	s, err := OpenSQLite(path)
	require.NoError(t, err)
	require.NoError(t, s.UpsertRun(model.Run{ID: "r1", Status: model.StatusRunning}))
	require.NoError(t, s.Close())
	ro, err := OpenSQLiteReadOnly(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ro.Close() })
	runs, err := ro.Runs()
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, "r1", runs[0].ID)
}

func TestAnnotationsRoundTripAndLWW(t *testing.T) {
	s := fileStore(t)
	v5 := json.RawMessage(`5`)
	v7 := json.RawMessage(`9`)
	v4 := json.RawMessage(`1`)
	require.NoError(t, s.UpsertAnnotation(model.Annotation{ExecutionID: "e1", SourceKey: "k1", Owner: "eval", Key: "score", Value: v5, WriteSeq: 5}))
	require.NoError(t, s.UpsertAnnotation(model.Annotation{ExecutionID: "e1", SourceKey: "k1", Owner: "eval", Key: "score", Value: v7, WriteSeq: 7}))
	require.NoError(t, s.UpsertAnnotation(model.Annotation{ExecutionID: "e1", SourceKey: "k1", Owner: "eval", Key: "score", Value: v4, WriteSeq: 4}))
	require.NoError(t, s.UpsertAnnotation(model.Annotation{ExecutionID: "e1", SourceKey: "k1", Owner: "other", Key: "score", Value: json.RawMessage(`2`), WriteSeq: 2}))
	anns, err := s.AnnotationsForExecution("e1")
	require.NoError(t, err)
	require.Len(t, anns, 2)
	evalAnn := anns[0]
	if evalAnn.Owner != "eval" {
		evalAnn = anns[1]
	}
	assert.Equal(t, "eval", evalAnn.Owner)
	assert.Equal(t, "score", evalAnn.Key)
	assert.Equal(t, string(v7), string(evalAnn.Value))
}

func TestMoveAnnotations(t *testing.T) {
	s := fileStore(t)
	require.NoError(t, s.UpsertAnnotation(model.Annotation{ExecutionID: "e1", SourceKey: "from", Owner: "eval", Key: "score", Value: json.RawMessage(`9`), WriteSeq: 1}))
	require.NoError(t, s.MoveAnnotations("e1", "from", "to"))
	anns, err := s.AnnotationsForExecution("e1")
	require.NoError(t, err)
	require.Len(t, anns, 1)
	assert.Equal(t, "to", anns[0].SourceKey)
	assert.Equal(t, "eval", anns[0].Owner)
}

func TestMoveAnnotationsBeginError(t *testing.T) {
	s := fileStore(t)
	require.NoError(t, s.Close())
	err := s.MoveAnnotations("e1", "from", "to")
	require.Error(t, err)
}

func TestUpsertAnnotationError(t *testing.T) {
	s := fileStore(t)
	require.NoError(t, s.db.Close())
	err := s.UpsertAnnotation(model.Annotation{ExecutionID: "e1", SourceKey: "k1", Owner: "eval", Key: "score", Value: json.RawMessage(`9`), WriteSeq: 1})
	require.Error(t, err)
}

func TestAnnotationsForExecutionError(t *testing.T) {
	s := fileStore(t)
	require.NoError(t, s.db.Close())
	_, err := s.AnnotationsForExecution("e1")
	require.Error(t, err)
}

func TestAnnotationsStepKeyScanned(t *testing.T) {
	s := fileStore(t)
	require.NoError(t, s.UpsertAnnotation(model.Annotation{
		ExecutionID: "e1",
		SourceKey:   "k1",
		StepKey:     "step-abc",
		Owner:       "eval",
		Key:         "score",
		Value:       json.RawMessage(`9`),
		WriteSeq:    1,
	}))
	anns, err := s.AnnotationsForExecution("e1")
	require.NoError(t, err)
	require.Len(t, anns, 1)
	assert.Equal(t, "step-abc", anns[0].StepKey)
}

func TestScanAnnotationsScanError(t *testing.T) {
	_, err := scanAnnotations(&fakeRows{bodies: []string{"x"}, scanErr: errors.New("scan")})
	require.Error(t, err)
}

func TestScanAnnotationsRowsErr(t *testing.T) {
	_, err := scanAnnotations(&fakeRows{errErr: errors.New("iter")})
	require.Error(t, err)
}

func TestMoveAnnotationsQueryError(t *testing.T) {
	s := fileStore(t)
	_, err := s.db.Exec("DROP TABLE annotations")
	require.NoError(t, err)
	err = s.MoveAnnotations("e1", "from", "to")
	require.Error(t, err)
}

func TestScanMoveAnnotationRowsScanError(t *testing.T) {
	_, err := scanMoveAnnotationRows(&fakeRows{bodies: []string{"x"}, scanErr: errors.New("scan")})
	require.Error(t, err)
}

func TestScanMoveAnnotationRowsRowsErr(t *testing.T) {
	_, err := scanMoveAnnotationRows(&fakeRows{errErr: errors.New("iter")})
	require.Error(t, err)
}

func TestMoveAnnotationsScanError(t *testing.T) {
	s := fileStore(t)
	_, err := s.db.Exec("DROP TABLE annotations")
	require.NoError(t, err)
	_, err = s.db.Exec(`CREATE TABLE annotations (execution_id TEXT NOT NULL, source_key TEXT NOT NULL, step_key TEXT, owner TEXT NOT NULL, key TEXT NOT NULL, value TEXT NOT NULL, write_seq TEXT NOT NULL, PRIMARY KEY (execution_id, source_key, owner, key))`)
	require.NoError(t, err)
	_, err = s.db.Exec(`INSERT INTO annotations VALUES('e1','from',NULL,'eval','score','9','not-an-int')`)
	require.NoError(t, err)
	err = s.MoveAnnotations("e1", "from", "to")
	require.Error(t, err)
}

func TestMoveAnnotationsDeleteError(t *testing.T) {
	s := fileStore(t)
	require.NoError(t, s.UpsertAnnotation(model.Annotation{ExecutionID: "e1", SourceKey: "from", Owner: "eval", Key: "score", Value: json.RawMessage(`9`), WriteSeq: 1}))
	_, err := s.db.Exec(`CREATE TRIGGER block_del BEFORE DELETE ON annotations BEGIN SELECT RAISE(ABORT, 'blocked'); END`)
	require.NoError(t, err)
	err = s.MoveAnnotations("e1", "from", "to")
	require.Error(t, err)
}

func TestMoveAnnotationsInsertError(t *testing.T) {
	s := fileStore(t)
	require.NoError(t, s.UpsertAnnotation(model.Annotation{ExecutionID: "e1", SourceKey: "from", Owner: "eval", Key: "score", Value: json.RawMessage(`9`), WriteSeq: 1}))
	_, err := s.db.Exec(`CREATE TRIGGER block_ins BEFORE INSERT ON annotations BEGIN SELECT RAISE(ABORT, 'blocked'); END`)
	require.NoError(t, err)
	err = s.MoveAnnotations("e1", "from", "to")
	require.Error(t, err)
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

func TestGetBaselineOnV1StoreReportsOutdated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v1.db")
	seedV1DB(t, path)
	s, err := openSQLiteReadOnly(sql.Open, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	_, _, err = s.GetBaseline("x")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSchemaOutdated)
}

func TestListBaselinesOnV1StoreReportsOutdated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v1.db")
	seedV1DB(t, path)
	s, err := openSQLiteReadOnly(sql.Open, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	_, err = s.ListBaselines()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSchemaOutdated)
}

func TestOpenSQLiteReadOnlyRelativePath(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	s, err := OpenSQLite(filepath.Join(dir, "g.db"))
	require.NoError(t, err)
	require.NoError(t, s.UpsertRun(model.Run{ID: "r1", Status: model.StatusRunning}))
	require.NoError(t, s.Close())

	t.Chdir(dir)
	ro, err := OpenSQLiteReadOnly("./g.db")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ro.Close() })
	runs, err := ro.Runs()
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, "r1", runs[0].ID)
}

func TestOpenSQLiteReadOnlyAbsError(t *testing.T) {
	orig := absFn
	absFn = func(string) (string, error) { return "", errors.New("boom") }
	t.Cleanup(func() { absFn = orig })
	_, err := OpenSQLiteReadOnly("x.db")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "abs")
}
