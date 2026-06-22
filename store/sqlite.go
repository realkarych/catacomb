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
CREATE TABLE IF NOT EXISTS runs (run_id TEXT PRIMARY KEY, status TEXT, body TEXT);
CREATE TABLE IF NOT EXISTS quarantine (id INTEGER PRIMARY KEY AUTOINCREMENT, body TEXT);
CREATE TABLE IF NOT EXISTS tail_cursors (path TEXT PRIMARY KEY, offset INTEGER, fingerprint TEXT, size INTEGER, mtime INTEGER);
CREATE INDEX IF NOT EXISTS idx_observations_run_seq ON observations(run_id, seq);
CREATE INDEX IF NOT EXISTS idx_observations_exec_seq ON observations(execution_id, seq);
CREATE INDEX IF NOT EXISTS idx_nodes_run ON nodes(run_id);
CREATE INDEX IF NOT EXISTS idx_edges_run ON edges(run_id);
CREATE INDEX IF NOT EXISTS idx_runs_status ON runs(status);
`

const (
	insertObservation = `INSERT INTO observations(obs_id, run_id, execution_id, seq, body) VALUES(?,?,?,?,?)`
	upsertNode        = `INSERT INTO nodes(id, run_id, body) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body`
	upsertEdge        = `INSERT INTO edges(id, run_id, body) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body`
	upsertRun         = `INSERT INTO runs(run_id, status, body) VALUES(?,?,?) ON CONFLICT(run_id) DO UPDATE SET status=excluded.status, body=excluded.body`
	insertQuarantine  = `INSERT INTO quarantine(body) VALUES(?)`
	upsertTailCursor  = `INSERT INTO tail_cursors(path, offset, fingerprint, size, mtime) VALUES(?,?,?,?,?) ON CONFLICT(path) DO UPDATE SET offset=excluded.offset, fingerprint=excluded.fingerprint, size=excluded.size, mtime=excluded.mtime`
	selectTailCursors = `SELECT path, offset, fingerprint, size, mtime FROM tail_cursors ORDER BY path`
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

func (s *sqliteStore) applyGraph(tx *sql.Tx, nodes []*model.Node, edges []*model.Edge) error {
	for _, n := range nodes {
		if err := s.write(tx, upsertNode, n, n.ID, n.RunID); err != nil {
			return err
		}
	}
	for _, e := range edges {
		if err := s.write(tx, upsertEdge, e, e.ID, e.RunID); err != nil {
			return err
		}
	}
	return nil
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
	if err := s.applyGraph(tx, nodes, edges); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *sqliteStore) AppendAndApply(o model.Observation, nodes []*model.Node, edges []*model.Edge) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("store.AppendAndApply begin: %w", err)
	}
	if err := s.write(tx, insertObservation, o, o.ObsID, o.RunID, o.ExecutionID, o.Seq); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := s.applyGraph(tx, nodes, edges); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *sqliteStore) MaxSeq() (uint64, error) {
	var v sql.NullInt64
	if err := s.db.QueryRow("SELECT MAX(seq) FROM observations").Scan(&v); err != nil {
		return 0, fmt.Errorf("store.MaxSeq: %w", err)
	}
	if !v.Valid {
		return 0, nil
	}
	return uint64(v.Int64), nil
}

type rowScanner interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
	Close() error
}

func (s *sqliteStore) ObservationsSince(seq uint64) ([]model.Observation, error) {
	rows, err := s.db.Query("SELECT body FROM observations WHERE seq > ? ORDER BY seq", seq)
	if err != nil {
		return nil, fmt.Errorf("store.ObservationsSince: %w", err)
	}
	out, err := scanObservations(rows)
	if err != nil {
		return nil, err
	}
	return out, rows.Err()
}

func (s *sqliteStore) ObservationsForExecution(executionID string) ([]model.Observation, error) {
	rows, err := s.db.Query("SELECT body FROM observations WHERE execution_id = ? ORDER BY seq", executionID)
	if err != nil {
		return nil, fmt.Errorf("store.ObservationsForExecution: %w", err)
	}
	out, err := scanObservations(rows)
	if err != nil {
		return nil, err
	}
	return out, rows.Err()
}

func scanObservations(rows rowScanner) ([]model.Observation, error) {
	defer func() { _ = rows.Close() }()
	var out []model.Observation
	for rows.Next() {
		var body string
		if err := rows.Scan(&body); err != nil {
			return nil, fmt.Errorf("store.ObservationsSince scan: %w", err)
		}
		var o model.Observation
		if err := json.Unmarshal([]byte(body), &o); err != nil {
			return nil, fmt.Errorf("store.ObservationsSince decode: %w", err)
		}
		out = append(out, o)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store.ObservationsSince rows: %w", err)
	}
	return out, nil
}

func (s *sqliteStore) UpsertRun(r model.Run) error {
	body, err := s.marshal(r)
	if err != nil {
		return fmt.Errorf("store.UpsertRun marshal: %w", err)
	}
	if _, err := s.db.Exec(upsertRun, r.ID, string(r.Status), string(body)); err != nil {
		return fmt.Errorf("store.UpsertRun: %w", err)
	}
	return nil
}

func (s *sqliteStore) ListOpenRuns() ([]model.Run, error) {
	rows, err := s.db.Query("SELECT body FROM runs WHERE status = ? ORDER BY run_id", string(model.StatusRunning))
	if err != nil {
		return nil, fmt.Errorf("store.ListOpenRuns: %w", err)
	}
	out, err := scanRuns(rows)
	if err != nil {
		return nil, err
	}
	return out, rows.Err()
}

func (s *sqliteStore) Runs() ([]model.Run, error) {
	rows, err := s.db.Query("SELECT body FROM runs ORDER BY run_id")
	if err != nil {
		return nil, fmt.Errorf("store.Runs: %w", err)
	}
	out, err := scanRuns(rows)
	if err != nil {
		return nil, err
	}
	return out, rows.Err()
}

func scanRuns(rows rowScanner) ([]model.Run, error) {
	defer func() { _ = rows.Close() }()
	var out []model.Run
	for rows.Next() {
		var body string
		if err := rows.Scan(&body); err != nil {
			return nil, fmt.Errorf("store.scanRuns scan: %w", err)
		}
		var r model.Run
		if err := json.Unmarshal([]byte(body), &r); err != nil {
			return nil, fmt.Errorf("store.scanRuns decode: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store.scanRuns rows: %w", err)
	}
	return out, nil
}

func (s *sqliteStore) UpsertTailCursor(c model.TailCursor) error {
	if _, err := s.db.Exec(upsertTailCursor, c.Path, c.Offset, c.Fingerprint, c.Size, c.Mtime); err != nil {
		return fmt.Errorf("store.UpsertTailCursor: %w", err)
	}
	return nil
}

func (s *sqliteStore) LoadTailCursors() ([]model.TailCursor, error) {
	rows, err := s.db.Query(selectTailCursors)
	if err != nil {
		return nil, fmt.Errorf("store.LoadTailCursors: %w", err)
	}
	out, err := scanTailCursors(rows)
	if err != nil {
		return nil, err
	}
	return out, rows.Err()
}

func scanTailCursors(rows rowScanner) ([]model.TailCursor, error) {
	defer func() { _ = rows.Close() }()
	var out []model.TailCursor
	for rows.Next() {
		var c model.TailCursor
		if err := rows.Scan(&c.Path, &c.Offset, &c.Fingerprint, &c.Size, &c.Mtime); err != nil {
			return nil, fmt.Errorf("store.LoadTailCursors scan: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store.LoadTailCursors rows: %w", err)
	}
	return out, nil
}

func (s *sqliteStore) write(tx *sql.Tx, query string, value any, keys ...any) error {
	body, err := s.marshal(value)
	if err != nil {
		return fmt.Errorf("store.write marshal: %w", err)
	}
	args := make([]any, 0, len(keys)+1)
	args = append(args, keys...)
	args = append(args, string(body))
	if _, err := tx.Exec(query, args...); err != nil {
		return fmt.Errorf("store.write exec: %w", err)
	}
	return nil
}

func (s *sqliteStore) Quarantine(rec model.QuarantineRecord) error {
	body, err := s.marshal(rec)
	if err != nil {
		return fmt.Errorf("store.Quarantine marshal: %w", err)
	}
	if _, err := s.db.Exec(insertQuarantine, string(body)); err != nil {
		return fmt.Errorf("store.Quarantine: %w", err)
	}
	return nil
}

func (s *sqliteStore) QuarantineCount() (int64, error) {
	var n int64
	if err := s.db.QueryRow("SELECT COUNT(*) FROM quarantine").Scan(&n); err != nil {
		return 0, fmt.Errorf("store.QuarantineCount: %w", err)
	}
	return n, nil
}

func (s *sqliteStore) Close() error { return s.db.Close() }
