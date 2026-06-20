package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

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
