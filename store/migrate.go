package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/redact"
)

const currentSchemaVersion = 5

const schemaBaselines = `CREATE TABLE IF NOT EXISTS baselines (name TEXT PRIMARY KEY, body TEXT NOT NULL);`

const schemaRegressResults = `CREATE TABLE IF NOT EXISTS regress_results (baseline TEXT NOT NULL, seq INTEGER NOT NULL, body TEXT NOT NULL, PRIMARY KEY (baseline, seq));`

var (
	ErrSchemaMigrationFailed = errors.New("store: schema migration failed")
	ErrSchemaTooNew          = errors.New("store: on-disk schema is newer than this catacomb binary; upgrade catacomb")
	ErrSchemaOutdated        = errors.New("store schema is older than this binary; run a write-path command like 'catacomb baseline set' to migrate")
)

type migration struct {
	from  int
	to    int
	apply func(*sql.Tx) (map[string]int, error)
}

var schemaMigrations = []migration{
	{from: 0, to: 1, apply: applySchemaV1},
	{from: 1, to: 2, apply: applySchemaV2},
	{from: 2, to: 3, apply: applySchemaV3},
	{from: 3, to: 4, apply: applySchemaV4},
	{from: 4, to: 5, apply: applySchemaV5},
}

var deadGraphTables = []string{"observations", "nodes", "edges", "runs", "quarantine", "tail_cursors", "annotations"}

func applySchemaV1(tx *sql.Tx) (map[string]int, error) {
	if _, err := tx.Exec(schema); err != nil {
		return nil, fmt.Errorf("store.applySchemaV1: %w", err)
	}
	return nil, nil
}

func applySchemaV2(tx *sql.Tx) (map[string]int, error) {
	if _, err := tx.Exec(schemaBaselines); err != nil {
		return nil, fmt.Errorf("store.applySchemaV2: %w", err)
	}
	return nil, nil
}

func applySchemaV3(tx *sql.Tx) (map[string]int, error) {
	if _, err := tx.Exec(schemaRegressResults); err != nil {
		return nil, fmt.Errorf("store.applySchemaV3: %w", err)
	}
	return nil, nil
}

type migrationConn interface {
	Query(query string, args ...any) (*sql.Rows, error)
	Exec(query string, args ...any) (sql.Result, error)
}

func applySchemaV4(tx *sql.Tx) (map[string]int, error) {
	obs, err := scrubExisting(tx, "observations", "SELECT obs_id, body FROM observations", "UPDATE observations SET body = ? WHERE obs_id = ?", scrubObservationBody)
	if err != nil {
		return nil, fmt.Errorf("store.applySchemaV4 observations: %w", err)
	}
	nodes, err := scrubExisting(tx, "nodes", "SELECT id, body FROM nodes", "UPDATE nodes SET body = ? WHERE id = ?", scrubNodeBody)
	if err != nil {
		return nil, fmt.Errorf("store.applySchemaV4 nodes: %w", err)
	}
	runs, err := scrubExisting(tx, "runs", "SELECT run_id, body FROM runs", "UPDATE runs SET body = ? WHERE run_id = ?", scrubRunBody)
	if err != nil {
		return nil, fmt.Errorf("store.applySchemaV4 runs: %w", err)
	}
	return map[string]int{"observations": obs, "nodes": nodes, "runs": runs}, nil
}

func scrubExisting(tx *sql.Tx, table, selectQ, updateQ string, rewrite func([]byte) ([]byte, error)) (int, error) {
	present, err := tableExists(tx, table)
	if err != nil {
		return 0, err
	}
	if !present {
		return 0, nil
	}
	return scrubTable(tx, selectQ, updateQ, rewrite)
}

func applySchemaV5(tx *sql.Tx) (map[string]int, error) {
	dropped, err := dropDeadTables(tx)
	if err != nil {
		return nil, fmt.Errorf("store.applySchemaV5: %w", err)
	}
	return map[string]int{"graph_tables": dropped}, nil
}

func dropDeadTables(tx *sql.Tx) (int, error) {
	dropped := 0
	for _, name := range deadGraphTables {
		present, err := tableExists(tx, name)
		if err != nil {
			return 0, err
		}
		if _, err := tx.Exec("DROP TABLE IF EXISTS " + name); err != nil {
			return 0, fmt.Errorf("store.dropDeadTables %s: %w", name, err)
		}
		if present {
			dropped++
		}
	}
	return dropped, nil
}

func tableExists(tx *sql.Tx, name string) (bool, error) {
	var n int
	if err := tx.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name = ?", name).Scan(&n); err != nil {
		return false, fmt.Errorf("store.tableExists %s: %w", name, err)
	}
	return n > 0, nil
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

func scrubRunBody(body []byte) ([]byte, error) {
	var r model.Run
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	return marshalVerbatim(redact.Run(r))
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

func applyMigration(db *sql.DB, m migration) (map[string]int, error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("store.applyMigration v%d->v%d begin: %w: %w", m.from, m.to, ErrSchemaMigrationFailed, err)
	}
	changed, err := m.apply(tx)
	if err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("store.applyMigration v%d->v%d apply: %w: %w", m.from, m.to, ErrSchemaMigrationFailed, err)
	}
	if err := setSchemaVersion(tx, m.to); err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("store.applyMigration v%d->v%d stamp: %w: %w", m.from, m.to, ErrSchemaMigrationFailed, err)
	}
	return changed, tx.Commit()
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

func migrate(db *sql.DB, version int, migrations []migration, logger *slog.Logger) (map[string]int, error) {
	pending := pendingMigrations(version, migrations)
	if len(pending) == 0 {
		return nil, nil
	}
	target := pending[len(pending)-1].to
	logger.Info("store: schema migration start", "from", version, "to", target)
	started := time.Now()
	changed := map[string]int{}
	for _, m := range pending {
		counts, err := applyMigration(db, m)
		if err != nil {
			return nil, err
		}
		for table, n := range counts {
			changed[table] += n
		}
	}
	logger.Info("store: schema migration finish",
		"from", version, "to", target,
		"changed_rows", changed,
		"duration_ms", time.Since(started).Milliseconds())
	return changed, nil
}

func pendingMigrations(version int, migrations []migration) []migration {
	var pending []migration
	for _, m := range migrations {
		if m.from < version {
			continue
		}
		pending = append(pending, m)
	}
	return pending
}

func shouldVacuumAfterMigration(changed map[string]int) bool {
	for _, n := range changed {
		if n > 0 {
			return true
		}
	}
	return false
}

func vacuumAfterScrub(conn migrationConn, logger *slog.Logger) {
	if _, err := conn.Exec("VACUUM"); err != nil {
		logger.Warn("store: post-migration vacuum failed; pre-scrub row images may linger in free pages", "err", err)
	}
	if _, err := conn.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		logger.Warn("store: post-migration wal checkpoint failed; pre-scrub row images may linger in the wal", "err", err)
	}
}
