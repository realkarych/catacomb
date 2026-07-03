package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/redact"
)

const currentSchemaVersion = 4

const schemaBaselines = `CREATE TABLE IF NOT EXISTS baselines (name TEXT PRIMARY KEY, body TEXT NOT NULL);`

const schemaRegressResults = `CREATE TABLE IF NOT EXISTS regress_results (baseline TEXT NOT NULL, seq INTEGER NOT NULL, body TEXT NOT NULL, PRIMARY KEY (baseline, seq));`

var (
	ErrSchemaMigrationFailed = errors.New("store: schema migration failed")
	ErrSchemaTooNew          = errors.New("store: on-disk schema is newer than this catacomb binary; upgrade catacomb")
	ErrSchemaOutdated        = errors.New("store schema is older than this binary; run a write-path command (catacomb up or baseline set) to migrate")
)

type migration struct {
	from  int
	to    int
	apply func(*sql.Tx) error
}

var schemaMigrations = []migration{
	{from: 0, to: 1, apply: applySchemaV1},
	{from: 1, to: 2, apply: applySchemaV2},
	{from: 2, to: 3, apply: applySchemaV3},
	{from: 3, to: 4, apply: applySchemaV4},
}

func applySchemaV1(tx *sql.Tx) error {
	if _, err := tx.Exec(schema); err != nil {
		return fmt.Errorf("store.applySchemaV1: %w", err)
	}
	return nil
}

func applySchemaV2(tx *sql.Tx) error {
	if _, err := tx.Exec(schemaBaselines); err != nil {
		return fmt.Errorf("store.applySchemaV2: %w", err)
	}
	return nil
}

func applySchemaV3(tx *sql.Tx) error {
	if _, err := tx.Exec(schemaRegressResults); err != nil {
		return fmt.Errorf("store.applySchemaV3: %w", err)
	}
	return nil
}

type migrationConn interface {
	Query(query string, args ...any) (*sql.Rows, error)
	Exec(query string, args ...any) (sql.Result, error)
}

func applySchemaV4(tx *sql.Tx) error {
	if _, err := scrubTable(tx, "SELECT obs_id, body FROM observations", "UPDATE observations SET body = ? WHERE obs_id = ?", scrubObservationBody); err != nil {
		return fmt.Errorf("store.applySchemaV4 observations: %w", err)
	}
	if _, err := scrubTable(tx, "SELECT id, body FROM nodes", "UPDATE nodes SET body = ? WHERE id = ?", scrubNodeBody); err != nil {
		return fmt.Errorf("store.applySchemaV4 nodes: %w", err)
	}
	return nil
}

type scrubbedRow struct {
	id   string
	body string
}

func scrubTable(conn migrationConn, selectQ, updateQ string, rewrite func([]byte) ([]byte, error)) (int, error) {
	rows, err := conn.Query(selectQ)
	if err != nil {
		return 0, fmt.Errorf("store.scrubTable select: %w", err)
	}
	changed, err := collectScrubbed(rows, rewrite)
	if err != nil {
		return 0, err
	}
	for _, r := range changed {
		if _, err := conn.Exec(updateQ, r.body, r.id); err != nil {
			return 0, fmt.Errorf("store.scrubTable update: %w", err)
		}
	}
	return len(changed), rows.Err()
}

func collectScrubbed(rows rowScanner, rewrite func([]byte) ([]byte, error)) ([]scrubbedRow, error) {
	defer func() { _ = rows.Close() }()
	var out []scrubbedRow
	for rows.Next() {
		var id, body string
		if err := rows.Scan(&id, &body); err != nil {
			return nil, fmt.Errorf("store.collectScrubbed scan: %w", err)
		}
		next, err := rewrite([]byte(body))
		if err != nil {
			return nil, fmt.Errorf("store.collectScrubbed rewrite: %w", err)
		}
		if string(next) != body {
			out = append(out, scrubbedRow{id: id, body: string(next)})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store.collectScrubbed rows: %w", err)
	}
	return out, nil
}

func scrubObservationBody(body []byte) ([]byte, error) {
	var o model.Observation
	if err := json.Unmarshal(body, &o); err != nil {
		return nil, err
	}
	return marshalVerbatim(redact.Observation(o))
}

func scrubNodeBody(body []byte) ([]byte, error) {
	var n model.Node
	if err := json.Unmarshal(body, &n); err != nil {
		return nil, err
	}
	return marshalVerbatim(redact.Node(&n))
}

func readSchemaVersion(db *sql.DB) (int, error) {
	var v int
	if err := db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		return 0, fmt.Errorf("store.readSchemaVersion: %w", err)
	}
	return v, nil
}

func setSchemaVersion(tx *sql.Tx, v int) error {
	if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", v)); err != nil {
		return fmt.Errorf("store.setSchemaVersion: %w", err)
	}
	return nil
}

func applyMigration(db *sql.DB, m migration) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("store.applyMigration v%d->v%d begin: %w: %w", m.from, m.to, ErrSchemaMigrationFailed, err)
	}
	if err := m.apply(tx); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("store.applyMigration v%d->v%d apply: %w: %w", m.from, m.to, ErrSchemaMigrationFailed, err)
	}
	if err := setSchemaVersion(tx, m.to); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("store.applyMigration v%d->v%d stamp: %w: %w", m.from, m.to, ErrSchemaMigrationFailed, err)
	}
	return tx.Commit()
}

func schemaVersionGuard(db *sql.DB, current int) (int, error) {
	version, err := readSchemaVersion(db)
	if err != nil {
		return 0, err
	}
	if version > current {
		return 0, fmt.Errorf("store.schemaVersionGuard: on-disk=%d supported=%d: %w", version, current, ErrSchemaTooNew)
	}
	return version, nil
}

func migrate(db *sql.DB, version int, migrations []migration) error {
	for _, m := range migrations {
		if m.from < version {
			continue
		}
		if err := applyMigration(db, m); err != nil {
			return err
		}
		version = m.to
	}
	return nil
}
