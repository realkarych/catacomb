package store

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"

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
CREATE TABLE IF NOT EXISTS annotations (
    execution_id TEXT NOT NULL,
    source_key   TEXT NOT NULL,
    step_key     TEXT,
    owner        TEXT NOT NULL,
    key          TEXT NOT NULL,
    value        TEXT NOT NULL,
    write_seq    INTEGER NOT NULL,
    PRIMARY KEY (execution_id, source_key, owner, key)
);
CREATE INDEX IF NOT EXISTS idx_annotations_exec ON annotations(execution_id);
CREATE INDEX IF NOT EXISTS idx_annotations_exec_step ON annotations(execution_id, step_key);
`

const (
	upsertBaseline       = `INSERT INTO baselines(name, body) VALUES(?,?) ON CONFLICT(name) DO UPDATE SET body=excluded.body`
	selectBaseline       = `SELECT body FROM baselines WHERE name = ?`
	selectBaselines      = `SELECT body FROM baselines ORDER BY name`
	deleteBaseline       = `DELETE FROM baselines WHERE name = ?`
	insertRegressResult  = `INSERT INTO regress_results(baseline, seq, body) SELECT ?, COALESCE(MAX(seq),0)+1, ? FROM regress_results WHERE baseline = ? RETURNING seq`
	selectRegressResults = `SELECT seq, body FROM regress_results WHERE baseline = ? ORDER BY seq`
)

func marshalVerbatim(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("store.marshalVerbatim: %w", err)
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

func OpenSQLite(path string) (Store, error) {
	return openSQLite(sql.Open, path)
}

func OpenSQLiteReadOnly(path string) (Store, error) {
	return openSQLiteReadOnly(sql.Open, path)
}

var absFn = filepath.Abs

func openSQLiteReadOnly(open func(driver, dsn string) (*sql.DB, error), path string) (Store, error) {
	abs, err := absFn(path)
	if err != nil {
		return nil, fmt.Errorf("store.OpenSQLiteReadOnly abs: %w", err)
	}
	db, err := open("sqlite", readOnlyDSN(abs))
	if err != nil {
		return nil, fmt.Errorf("store.OpenSQLiteReadOnly: %w", err)
	}
	if pingErr := db.Ping(); pingErr != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store.OpenSQLiteReadOnly ping: %w", pingErr)
	}
	version, err := schemaVersionGuard(db, currentSchemaVersion)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store.OpenSQLiteReadOnly schema: %w", err)
	}
	if version < currentSchemaVersion {
		_ = db.Close()
		return nil, fmt.Errorf("store.OpenSQLiteReadOnly schema: %w", ErrSchemaOutdated)
	}
	return &sqliteStore{db: db, marshal: marshalVerbatim}, nil
}

func readOnlyDSN(path string) string {
	p := filepath.ToSlash(path)
	if len(p) == 0 || p[0] != '/' {
		p = "/" + p
	}
	return (&url.URL{Scheme: "file", Path: p, RawQuery: "mode=ro"}).String()
}

func openSQLite(open func(driver, dsn string) (*sql.DB, error), path string) (Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("store.OpenSQLite mkdir: %w", err)
	}
	db, err := open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("store.OpenSQLite: %w", err)
	}
	version, err := schemaVersionGuard(db, currentSchemaVersion)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store.OpenSQLite schema: %w", err)
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store.OpenSQLite wal: %w", err)
	}
	logger := slog.Default()
	changed, migErr := migrate(db, version, schemaMigrations, logger)
	if migErr != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store.OpenSQLite migrate: %w", migErr)
	}
	if shouldVacuumAfterMigration(changed) {
		vacuumAfterScrub(db, logger)
	}
	return &sqliteStore{db: db, marshal: marshalVerbatim}, nil
}

type rowScanner interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
	Close() error
}

func (s *sqliteStore) UpsertBaseline(b model.Baseline) error {
	body, err := s.marshal(b)
	if err != nil {
		return fmt.Errorf("store.UpsertBaseline marshal: %w", err)
	}
	if _, err := s.db.Exec(upsertBaseline, b.Name, string(body)); err != nil {
		return fmt.Errorf("store.UpsertBaseline: %w", err)
	}
	return nil
}

func (s *sqliteStore) GetBaseline(name string) (model.Baseline, bool, error) {
	var body string
	err := s.db.QueryRow(selectBaseline, name).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Baseline{}, false, nil
	}
	if err != nil {
		if isMissingTable(err, "baselines") {
			return model.Baseline{}, false, fmt.Errorf("store.GetBaseline: %w", ErrSchemaOutdated)
		}
		return model.Baseline{}, false, fmt.Errorf("store.GetBaseline: %w", err)
	}
	var b model.Baseline
	if err := json.Unmarshal([]byte(body), &b); err != nil {
		return model.Baseline{}, false, fmt.Errorf("store.GetBaseline decode: %w", err)
	}
	return b, true, nil
}

func (s *sqliteStore) ListBaselines() ([]model.Baseline, error) {
	rows, err := s.db.Query(selectBaselines)
	if err != nil {
		if isMissingTable(err, "baselines") {
			return nil, fmt.Errorf("store.ListBaselines: %w", ErrSchemaOutdated)
		}
		return nil, fmt.Errorf("store.ListBaselines: %w", err)
	}
	out, err := scanBaselines(rows)
	if err != nil {
		return nil, err
	}
	return out, rows.Err()
}

func scanBaselines(rows rowScanner) ([]model.Baseline, error) {
	defer func() { _ = rows.Close() }()
	var out []model.Baseline
	for rows.Next() {
		var body string
		if err := rows.Scan(&body); err != nil {
			return nil, fmt.Errorf("store.ListBaselines scan: %w", err)
		}
		var b model.Baseline
		if err := json.Unmarshal([]byte(body), &b); err != nil {
			return nil, fmt.Errorf("store.ListBaselines decode: %w", err)
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store.ListBaselines rows: %w", err)
	}
	return out, nil
}

func (s *sqliteStore) DeleteBaseline(name string) error {
	if _, err := s.db.Exec(deleteBaseline, name); err != nil {
		return fmt.Errorf("store.DeleteBaseline: %w", err)
	}
	return nil
}

func (s *sqliteStore) AppendRegressResult(baseline string, body json.RawMessage) (int, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("store.AppendRegressResult begin: %w", err)
	}
	var seq int
	if err := tx.QueryRow(insertRegressResult, baseline, string(body), baseline).Scan(&seq); err != nil {
		_ = tx.Rollback()
		return 0, fmt.Errorf("store.AppendRegressResult insert: %w", err)
	}
	return seq, tx.Commit()
}

func (s *sqliteStore) RegressResultsFor(baseline string) ([]model.RegressResult, error) {
	rows, err := s.db.Query(selectRegressResults, baseline)
	if err != nil {
		if isMissingTable(err, "regress_results") {
			return nil, fmt.Errorf("store.RegressResultsFor: %w", ErrSchemaOutdated)
		}
		return nil, fmt.Errorf("store.RegressResultsFor: %w", err)
	}
	out, err := scanRegressResults(baseline, rows)
	if err != nil {
		return nil, err
	}
	return out, rows.Err()
}

func scanRegressResults(baseline string, rows rowScanner) ([]model.RegressResult, error) {
	defer func() { _ = rows.Close() }()
	var out []model.RegressResult
	for rows.Next() {
		var seq int
		var body string
		if err := rows.Scan(&seq, &body); err != nil {
			return nil, fmt.Errorf("store.RegressResultsFor scan: %w", err)
		}
		out = append(out, model.RegressResult{Baseline: baseline, Seq: seq, Body: json.RawMessage(body)})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store.RegressResultsFor rows: %w", err)
	}
	return out, nil
}

func isMissingTable(err error, table string) bool {
	return err != nil && strings.Contains(err.Error(), "no such table: "+table)
}

func (s *sqliteStore) Close() error { return s.db.Close() }
