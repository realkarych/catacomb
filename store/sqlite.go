package store

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/realkarych/catacomb/cdc"
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
	insertObservation            = `INSERT INTO observations(obs_id, run_id, execution_id, seq, body) VALUES(?,?,?,?,?)`
	upsertNode                   = `INSERT INTO nodes(id, run_id, body) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body`
	upsertEdge                   = `INSERT INTO edges(id, run_id, body) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body`
	deleteEdge                   = `DELETE FROM edges WHERE id = ?`
	deleteNode                   = `DELETE FROM nodes WHERE id = ?`
	upsertRun                    = `INSERT INTO runs(run_id, status, body) VALUES(?,?,?) ON CONFLICT(run_id) DO UPDATE SET status=excluded.status, body=excluded.body`
	insertQuarantine             = `INSERT INTO quarantine(body) VALUES(?)`
	upsertTailCursor             = `INSERT INTO tail_cursors(path, offset, fingerprint, size, mtime) VALUES(?,?,?,?,?) ON CONFLICT(path) DO UPDATE SET offset=excluded.offset, fingerprint=excluded.fingerprint, size=excluded.size, mtime=excluded.mtime`
	selectTailCursors            = `SELECT path, offset, fingerprint, size, mtime FROM tail_cursors ORDER BY path`
	upsertAnnotation             = `INSERT INTO annotations(execution_id,source_key,step_key,owner,key,value,write_seq) VALUES(?,?,?,?,?,?,?) ON CONFLICT(execution_id,source_key,owner,key) DO UPDATE SET value=excluded.value,step_key=COALESCE(excluded.step_key,annotations.step_key),write_seq=excluded.write_seq WHERE excluded.write_seq>=annotations.write_seq`
	selectAnnotations            = `SELECT execution_id,source_key,step_key,owner,key,value FROM annotations WHERE execution_id=? ORDER BY source_key,owner,key`
	selectAnnotationsBySourceKey = `SELECT execution_id,source_key,step_key,owner,key,value,write_seq FROM annotations WHERE execution_id=? AND source_key=?`
	deleteAnnotationsBySourceKey = `DELETE FROM annotations WHERE execution_id=? AND source_key=?`
	insertAnnotation             = `INSERT INTO annotations(execution_id,source_key,step_key,owner,key,value,write_seq) VALUES(?,?,?,?,?,?,?)`
	upsertBaseline               = `INSERT INTO baselines(name, body) VALUES(?,?) ON CONFLICT(name) DO UPDATE SET body=excluded.body`
	selectBaseline               = `SELECT body FROM baselines WHERE name = ?`
	selectBaselines              = `SELECT body FROM baselines ORDER BY name`
	deleteBaseline               = `DELETE FROM baselines WHERE name = ?`
	insertRegressResult          = `INSERT INTO regress_results(baseline, seq, body) SELECT ?, COALESCE(MAX(seq),0)+1, ? FROM regress_results WHERE baseline = ? RETURNING seq`
	selectRegressResults         = `SELECT seq, body FROM regress_results WHERE baseline = ? ORDER BY seq`
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
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store.OpenSQLiteReadOnly ping: %w", err)
	}
	if _, err := schemaVersionGuard(db, currentSchemaVersion); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store.OpenSQLiteReadOnly schema: %w", err)
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
	if err := migrate(db, version, schemaMigrations); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store.OpenSQLite migrate: %w", err)
	}
	return &sqliteStore{db: db, marshal: marshalVerbatim}, nil
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

func (s *sqliteStore) AppendDeltas(o model.Observation, deltas []cdc.GraphDelta) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("store.AppendDeltas begin: %w", err)
	}
	if err := s.write(tx, insertObservation, o, o.ObsID, o.RunID, o.ExecutionID, o.Seq); err != nil {
		_ = tx.Rollback()
		return err
	}
	for _, d := range deltas {
		if err := s.applyDelta(tx, d); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (s *sqliteStore) applyDelta(tx *sql.Tx, d cdc.GraphDelta) error {
	switch d.Kind {
	case cdc.DeltaNodeUpsert, cdc.DeltaNodeStatus:
		if d.Node == nil {
			return nil
		}
		return s.write(tx, upsertNode, d.Node, d.Node.ID, d.Node.RunID)
	case cdc.DeltaNodeMerge:
		if d.Node == nil {
			return nil
		}
		if d.OldID != "" {
			if _, err := tx.Exec(deleteNode, d.OldID); err != nil {
				return fmt.Errorf("store.AppendDeltas delete merged node: %w", err)
			}
		}
		return s.write(tx, upsertNode, d.Node, d.Node.ID, d.Node.RunID)
	case cdc.DeltaEdgeUpsert:
		if d.Edge == nil {
			return nil
		}
		return s.write(tx, upsertEdge, d.Edge, d.Edge.ID, d.Edge.RunID)
	case cdc.DeltaEdgeDelete:
		if d.Edge == nil {
			return nil
		}
		if _, err := tx.Exec(deleteEdge, d.Edge.ID); err != nil {
			return fmt.Errorf("store.AppendDeltas delete edge: %w", err)
		}
		return nil
	default:
		return nil
	}
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

func (s *sqliteStore) UpsertAnnotation(a model.Annotation) error {
	stepKey := sql.NullString{String: a.StepKey, Valid: a.StepKey != ""}
	if _, err := s.db.Exec(upsertAnnotation, a.ExecutionID, a.SourceKey, stepKey, a.Owner, a.Key, string(a.Value), a.WriteSeq); err != nil {
		return fmt.Errorf("store.UpsertAnnotation: %w", err)
	}
	return nil
}

func scanAnnotations(rows rowScanner) ([]model.Annotation, error) {
	defer func() { _ = rows.Close() }()
	var out []model.Annotation
	for rows.Next() {
		var a model.Annotation
		var stepKey sql.NullString
		var value string
		if err := rows.Scan(&a.ExecutionID, &a.SourceKey, &stepKey, &a.Owner, &a.Key, &value); err != nil {
			return nil, fmt.Errorf("store.AnnotationsForExecution scan: %w", err)
		}
		if stepKey.Valid {
			a.StepKey = stepKey.String
		}
		a.Value = json.RawMessage(value)
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store.AnnotationsForExecution rows: %w", err)
	}
	return out, nil
}

func (s *sqliteStore) AnnotationsForExecution(executionID string) ([]model.Annotation, error) {
	rows, err := s.db.Query(selectAnnotations, executionID)
	if err != nil {
		return nil, fmt.Errorf("store.AnnotationsForExecution: %w", err)
	}
	out, err := scanAnnotations(rows)
	_ = rows.Err()
	return out, err
}

type moveAnnotationRow struct {
	execID, srcKey, owner, key, value string
	stepKey                           sql.NullString
	writeSeq                          uint64
}

func scanMoveAnnotationRows(rows rowScanner) ([]moveAnnotationRow, error) {
	defer func() { _ = rows.Close() }()
	var out []moveAnnotationRow
	for rows.Next() {
		var r moveAnnotationRow
		if err := rows.Scan(&r.execID, &r.srcKey, &r.stepKey, &r.owner, &r.key, &r.value, &r.writeSeq); err != nil {
			return nil, fmt.Errorf("store.MoveAnnotations scan: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store.MoveAnnotations rows: %w", err)
	}
	return out, nil
}

func (s *sqliteStore) MoveAnnotations(executionID, fromKey, toKey string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("store.MoveAnnotations begin: %w", err)
	}
	rows, err := tx.Query(selectAnnotationsBySourceKey, executionID, fromKey)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("store.MoveAnnotations select: %w", err)
	}
	anns, err := scanMoveAnnotationRows(rows)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	_ = rows.Err()
	if _, err := tx.Exec(deleteAnnotationsBySourceKey, executionID, fromKey); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("store.MoveAnnotations delete: %w", err)
	}
	for _, r := range anns {
		if _, err := tx.Exec(insertAnnotation, executionID, toKey, r.stepKey, r.owner, r.key, r.value, r.writeSeq); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("store.MoveAnnotations insert: %w", err)
		}
	}
	return tx.Commit()
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
