package store

import (
	"database/sql"
	"encoding/json"
	"fmt"

	_ "modernc.org/sqlite"

	"github.com/realkarych/catacomb/model"
)

type sqliteStore struct {
	db      *sql.DB
	marshal func(any) ([]byte, error)
}

const schema = `
CREATE TABLE IF NOT EXISTS observations (obs_id TEXT PRIMARY KEY, run_id TEXT, execution_id TEXT, seq INTEGER, body TEXT);
CREATE TABLE IF NOT EXISTS nodes (id TEXT PRIMARY KEY, run_id TEXT, body TEXT);
CREATE TABLE IF NOT EXISTS edges (id TEXT PRIMARY KEY, run_id TEXT, body TEXT);
CREATE INDEX IF NOT EXISTS idx_observations_run_seq ON observations(run_id, seq);
CREATE INDEX IF NOT EXISTS idx_nodes_run ON nodes(run_id);
CREATE INDEX IF NOT EXISTS idx_edges_run ON edges(run_id);
`

const (
	insertObservation = `INSERT INTO observations(obs_id, run_id, execution_id, seq, body) VALUES(?,?,?,?,?)`
	upsertNode        = `INSERT INTO nodes(id, run_id, body) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body`
	upsertEdge        = `INSERT INTO edges(id, run_id, body) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body`
)

func OpenSQLite(path string) (Store, error) {
	return openSQLite(sql.Open, path)
}

func openSQLite(open func(driver, dsn string) (*sql.DB, error), path string) (Store, error) {
	db, err := open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("store.OpenSQLite: %w", err)
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return nil, fmt.Errorf("store.OpenSQLite wal: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("store.OpenSQLite schema: %w", err)
	}
	return &sqliteStore{db: db, marshal: json.Marshal}, nil
}

func (s *sqliteStore) Persist(obs []model.Observation, nodes []*model.Node, edges []*model.Edge) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("store.Persist begin: %w", err)
	}
	for _, o := range obs {
		if err := s.write(tx, insertObservation, o, o.ObsID, o.RunID, o.ExecutionID, o.Seq); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	for _, n := range nodes {
		if err := s.write(tx, upsertNode, n, n.ID, n.RunID); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	for _, e := range edges {
		if err := s.write(tx, upsertEdge, e, e.ID, e.RunID); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (s *sqliteStore) write(tx *sql.Tx, query string, value any, keys ...any) error {
	body, err := s.marshal(value)
	if err != nil {
		return fmt.Errorf("store.Persist marshal: %w", err)
	}
	args := make([]any, 0, len(keys)+1)
	args = append(args, keys...)
	args = append(args, string(body))
	if _, err := tx.Exec(query, args...); err != nil {
		return fmt.Errorf("store.Persist exec: %w", err)
	}
	return nil
}

func (s *sqliteStore) Close() error { return s.db.Close() }
