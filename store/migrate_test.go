package store

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
)

const legacySchema = `
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

func tooNewDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "g.db")
	s, err := openSQLite(sql.Open, path)
	require.NoError(t, err)
	seedBaselineRow(t, s.(*sqliteStore).db, "b1")
	_, err = s.(*sqliteStore).db.Exec(fmt.Sprintf("PRAGMA user_version = %d", currentSchemaVersion+1))
	require.NoError(t, err)
	require.NoError(t, s.Close())
	return path
}

func TestOpenSQLiteRefusesNewerSchema(t *testing.T) {
	path := tooNewDB(t)
	_, err := openSQLite(sql.Open, path)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSchemaTooNew)

	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	assert.Equal(t, currentSchemaVersion+1, userVersion(t, db))
	var n int
	require.NoError(t, db.QueryRow("SELECT count(*) FROM baselines").Scan(&n))
	assert.Equal(t, 1, n)
}

func TestOpenSQLiteReadOnlyRefusesNewerSchema(t *testing.T) {
	path := tooNewDB(t)
	_, err := openSQLiteReadOnly(sql.Open, path)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSchemaTooNew)
}

func TestOpenSQLiteRefusesNewerSchemaWithoutWriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "g.db")
	seed, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	_, err = seed.Exec(legacySchema)
	require.NoError(t, err)
	_, err = seed.Exec(`INSERT INTO runs(run_id, status, body) VALUES('r1','running','{"id":"r1","status":"running"}')`)
	require.NoError(t, err)
	_, err = seed.Exec(fmt.Sprintf("PRAGMA user_version = %d", currentSchemaVersion+1))
	require.NoError(t, err)
	require.NoError(t, seed.Close())

	_, err = openSQLite(sql.Open, path)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSchemaTooNew)

	check, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = check.Close() })
	var mode string
	require.NoError(t, check.QueryRow("PRAGMA journal_mode").Scan(&mode))
	assert.NotEqual(t, "wal", mode)
	assert.Equal(t, currentSchemaVersion+1, userVersion(t, check))
	var n int
	require.NoError(t, check.QueryRow("SELECT count(*) FROM runs").Scan(&n))
	assert.Equal(t, 1, n)
}

func rawDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "raw.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func userVersion(t *testing.T, db *sql.DB) int {
	t.Helper()
	var v int
	require.NoError(t, db.QueryRow("PRAGMA user_version").Scan(&v))
	return v
}

func seedBaselineRow(t *testing.T, db *sql.DB, name string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO baselines(name, body) VALUES(?, ?)`, name, fmt.Sprintf(`{"name":%q}`, name))
	require.NoError(t, err)
}

func baselineNames(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query("SELECT name FROM baselines ORDER BY name")
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var names []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		names = append(names, name)
	}
	require.NoError(t, rows.Err())
	return names
}

func TestOpenStampsFreshDBToCurrent(t *testing.T) {
	s := fileStore(t)
	assert.Equal(t, currentSchemaVersion, userVersion(t, s.db))
}

func TestReopenAtCurrentDoesNotRemigrate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "g.db")
	s, err := openSQLite(sql.Open, path)
	require.NoError(t, err)
	seedBaselineRow(t, s.(*sqliteStore).db, "b1")
	require.NoError(t, s.Close())

	again, err := openSQLite(sql.Open, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = again.Close() })
	assert.Equal(t, currentSchemaVersion, userVersion(t, again.(*sqliteStore).db))
	assert.Equal(t, []string{"b1"}, baselineNames(t, again.(*sqliteStore).db))
}

func TestOpenMigratesUnversionedDBPreservingData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "g.db")
	s, err := openSQLite(sql.Open, path)
	require.NoError(t, err)
	seedBaselineRow(t, s.(*sqliteStore).db, "b1")
	_, err = s.(*sqliteStore).db.Exec("PRAGMA user_version = 0")
	require.NoError(t, err)
	require.NoError(t, s.Close())

	migrated, err := openSQLite(sql.Open, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = migrated.Close() })
	assert.Equal(t, currentSchemaVersion, userVersion(t, migrated.(*sqliteStore).db))
	assert.Equal(t, []string{"b1"}, baselineNames(t, migrated.(*sqliteStore).db))
}

func TestMigrateAppliesInOrder(t *testing.T) {
	db := rawDB(t)
	var order []int
	migs := []migration{
		{from: 0, to: 1, apply: func(*sql.Tx) (map[string]int, error) { order = append(order, 1); return nil, nil }},
		{from: 1, to: 2, apply: func(*sql.Tx) (map[string]int, error) { order = append(order, 2); return nil, nil }},
	}
	_, err := migrate(db, 0, migs, discardLogger())
	require.NoError(t, err)
	assert.Equal(t, []int{1, 2}, order)
	assert.Equal(t, 2, userVersion(t, db))
}

func TestMigrateFailingStepRollsBackWithSentinel(t *testing.T) {
	db := rawDB(t)
	migs := []migration{
		{from: 0, to: 1, apply: func(*sql.Tx) (map[string]int, error) { return nil, nil }},
		{from: 1, to: 2, apply: func(*sql.Tx) (map[string]int, error) { return nil, errors.New("boom") }},
	}
	_, err := migrate(db, 0, migs, discardLogger())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSchemaMigrationFailed)
	assert.Equal(t, 1, userVersion(t, db))
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestMigrateLogsStartAndFinishWithChangedRowCounts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "g.db")
	seedV3DB(t, path)
	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	_, err = migrate(db, 3, schemaMigrations, logger)
	require.NoError(t, err)

	logs := buf.String()
	assert.Contains(t, logs, "schema migration start")
	assert.Contains(t, logs, "schema migration finish")
	assert.Contains(t, logs, `"from":3`)
	assert.Contains(t, logs, `"to":5`)
	assert.Contains(t, logs, `"observations":1`)
	assert.Contains(t, logs, `"nodes":1`)
	assert.Contains(t, logs, `"runs":1`)
	assert.Contains(t, logs, `"graph_tables":7`)
	assert.Contains(t, logs, "duration_ms")
}

func TestMigrateAtCurrentVersionLogsNothing(t *testing.T) {
	db := rawDB(t)
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	_, err := migrate(db, currentSchemaVersion, schemaMigrations, logger)
	require.NoError(t, err)
	assert.Empty(t, buf.String())
}

func TestShouldVacuumAfterMigration(t *testing.T) {
	assert.False(t, shouldVacuumAfterMigration(nil), "no migration scrubbed any row")
	assert.False(t, shouldVacuumAfterMigration(map[string]int{"runs": 0}), "scrub touched no rows")
	assert.True(t, shouldVacuumAfterMigration(map[string]int{"runs": 1}))
	assert.True(t, shouldVacuumAfterMigration(map[string]int{"observations": 0, "nodes": 2}))
}

type execErrConn struct{}

func (execErrConn) Query(string, ...any) (*sql.Rows, error) { return nil, errors.New("query boom") }
func (execErrConn) Exec(string, ...any) (sql.Result, error) { return nil, errors.New("exec boom") }

func TestVacuumAfterScrubLogsErrors(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	vacuumAfterScrub(execErrConn{}, logger)
	logs := buf.String()
	assert.Contains(t, logs, "vacuum failed")
	assert.Contains(t, logs, "wal checkpoint failed")
	assert.Contains(t, logs, "exec boom")
}

func TestReadSchemaVersionError(t *testing.T) {
	db := rawDB(t)
	require.NoError(t, db.Close())
	_, err := readSchemaVersion(db)
	require.Error(t, err)
}

func TestSetSchemaVersionError(t *testing.T) {
	db := rawDB(t)
	tx, err := db.Begin()
	require.NoError(t, err)
	require.NoError(t, tx.Rollback())
	require.Error(t, setSchemaVersion(tx, 1))
}

func TestApplyMigrationBeginError(t *testing.T) {
	db := rawDB(t)
	require.NoError(t, db.Close())
	_, err := applyMigration(db, migration{from: 0, to: 1, apply: func(*sql.Tx) (map[string]int, error) { return nil, nil }})
	require.Error(t, err)
}

func TestApplyMigrationApplyError(t *testing.T) {
	db := rawDB(t)
	_, err := applyMigration(db, migration{from: 0, to: 1, apply: func(*sql.Tx) (map[string]int, error) { return nil, errors.New("boom") }})
	require.Error(t, err)
	assert.Equal(t, 0, userVersion(t, db))
}

func TestApplyMigrationStampError(t *testing.T) {
	db := rawDB(t)
	_, err := applyMigration(db, migration{from: 0, to: 1, apply: func(tx *sql.Tx) (map[string]int, error) { return nil, tx.Rollback() }})
	require.Error(t, err)
	assert.Equal(t, 0, userVersion(t, db))
}

func TestSchemaVersionGuardReadError(t *testing.T) {
	db := rawDB(t)
	require.NoError(t, db.Close())
	_, err := schemaVersionGuard(db, currentSchemaVersion)
	require.Error(t, err)
}

func TestApplyMigrationRollsBackPartialDDL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ddl.db")
	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	m := migration{from: 0, to: 1, apply: func(tx *sql.Tx) (map[string]int, error) {
		if _, e := tx.Exec("CREATE TABLE probe(x)"); e != nil {
			return nil, e
		}
		return nil, errors.New("boom after ddl")
	}}
	_, err = applyMigration(db, m)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSchemaMigrationFailed)
	require.NoError(t, db.Close())

	fresh, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = fresh.Close() })
	var tables int
	require.NoError(t, fresh.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='probe'").Scan(&tables))
	assert.Equal(t, 0, tables)
	assert.Equal(t, 0, userVersion(t, fresh))
}

func TestApplySchemaV1Error(t *testing.T) {
	db := rawDB(t)
	tx, err := db.Begin()
	require.NoError(t, err)
	require.NoError(t, tx.Rollback())
	_, errV1 := applySchemaV1(tx)
	require.Error(t, errV1)
}

func TestApplySchemaV2Error(t *testing.T) {
	db := rawDB(t)
	tx, err := db.Begin()
	require.NoError(t, err)
	require.NoError(t, tx.Rollback())
	_, errV2 := applySchemaV2(tx)
	require.Error(t, errV2)
}

func seedV1DB(t *testing.T, path string) {
	t.Helper()
	seed, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	_, err = seed.Exec(legacySchema)
	require.NoError(t, err)
	_, err = seed.Exec(`INSERT INTO runs(run_id, status, body) VALUES('r1','running','{"id":"r1","status":"running"}')`)
	require.NoError(t, err)
	_, err = seed.Exec("PRAGMA user_version = 1")
	require.NoError(t, err)
	require.NoError(t, seed.Close())
}

func TestOpenMigratesV1ToV2CreatingBaselines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "g.db")
	seedV1DB(t, path)

	migrated, err := openSQLite(sql.Open, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = migrated.Close() })
	assert.Equal(t, currentSchemaVersion, userVersion(t, migrated.(*sqliteStore).db))

	assert.Equal(t, []string{"baselines", "regress_results"}, tableNames(t, migrated.(*sqliteStore).db))

	require.NoError(t, migrated.UpsertBaseline(model.Baseline{Name: "base", RunIDs: []string{"r1"}}))
	got, ok, err := migrated.GetBaseline("base")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, []string{"r1"}, got.RunIDs)
}

func tableNames(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var names []string
	for rows.Next() {
		var n string
		require.NoError(t, rows.Scan(&n))
		names = append(names, n)
	}
	require.NoError(t, rows.Err())
	return names
}

func schemaDDL(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query("SELECT name, sql FROM sqlite_master WHERE sql IS NOT NULL AND name NOT LIKE 'sqlite_%' ORDER BY name")
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var ddl []string
	for rows.Next() {
		var name, sql string
		require.NoError(t, rows.Scan(&name, &sql))
		ddl = append(ddl, name+"\x00"+sql)
	}
	require.NoError(t, rows.Err())
	return ddl
}

func TestFreshAndV1UpgradeConvergeOnSchema(t *testing.T) {
	fresh := fileStore(t)

	path := filepath.Join(t.TempDir(), "v1.db")
	seedV1DB(t, path)
	upgraded, err := openSQLite(sql.Open, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = upgraded.Close() })

	assert.Equal(t, schemaDDL(t, fresh.db), schemaDDL(t, upgraded.(*sqliteStore).db))
	assert.Contains(t, tableNames(t, fresh.db), "baselines")
}

func seedV2DB(t *testing.T, path string) {
	t.Helper()
	seed, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	_, err = seed.Exec(legacySchema)
	require.NoError(t, err)
	_, err = seed.Exec(schemaBaselines)
	require.NoError(t, err)
	_, err = seed.Exec(`INSERT INTO runs(run_id, status, body) VALUES('r1','running','{"id":"r1","status":"running"}')`)
	require.NoError(t, err)
	_, err = seed.Exec("PRAGMA user_version = 2")
	require.NoError(t, err)
	require.NoError(t, seed.Close())
}

func TestOpenMigratesV2ToV3CreatingRegressResults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "g.db")
	seedV2DB(t, path)

	migrated, err := openSQLite(sql.Open, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = migrated.Close() })
	assert.Equal(t, currentSchemaVersion, userVersion(t, migrated.(*sqliteStore).db))

	assert.Equal(t, []string{"baselines", "regress_results"}, tableNames(t, migrated.(*sqliteStore).db))

	seq, err := migrated.AppendRegressResult("base", json.RawMessage(`{"ok":true}`))
	require.NoError(t, err)
	assert.Equal(t, 1, seq)
	got, err := migrated.RegressResultsFor("base")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, 1, got[0].Seq)
}

func TestFreshAndV2UpgradeConvergeOnSchema(t *testing.T) {
	fresh := fileStore(t)

	path := filepath.Join(t.TempDir(), "v2.db")
	seedV2DB(t, path)
	upgraded, err := openSQLite(sql.Open, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = upgraded.Close() })

	assert.Equal(t, schemaDDL(t, fresh.db), schemaDDL(t, upgraded.(*sqliteStore).db))
	assert.Contains(t, tableNames(t, fresh.db), "regress_results")
}

func TestApplySchemaV3Error(t *testing.T) {
	db := rawDB(t)
	tx, err := db.Begin()
	require.NoError(t, err)
	require.NoError(t, tx.Rollback())
	_, errV3 := applySchemaV3(tx)
	require.Error(t, errV3)
}

func seedV3Schema(t *testing.T, path string) *sql.DB {
	t.Helper()
	seed, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	for _, stmt := range []string{legacySchema, schemaBaselines, schemaRegressResults} {
		_, err = seed.Exec(stmt)
		require.NoError(t, err)
	}
	return seed
}

func stampAndClose(t *testing.T, seed *sql.DB) {
	t.Helper()
	_, err := seed.Exec("PRAGMA user_version = 3")
	require.NoError(t, err)
	require.NoError(t, seed.Close())
}

const (
	seedRunCwdSecret   = "AKIARUNCWD0EXAMPLE00"
	seedRunLabelSecret = "postgres://runner:run_label_password@db.internal/labels"
)

func seedV3DB(t *testing.T, path string) (secretObsBody, cleanObsBody, secretNodeBody, secretRunBody string) {
	t.Helper()
	obs := model.Observation{
		ObsID: "o1", RunID: "r1", ExecutionID: "e1", Source: model.SourceHook, Kind: "assistant_tool_use", Seq: 1,
		Attrs: map[string]any{"prompt": "use AKIAIOSFODNN7EXAMPLE"},
		Payload: &model.Payload{
			Input: json.RawMessage(`{"command":"psql postgres://kesha:kesha_dev_password@localhost/appdb"}`),
			Hash:  "deadbeef",
		},
	}
	clean := model.Observation{ObsID: "o2", RunID: "r1", ExecutionID: "e1", Source: model.SourceHook, Kind: "stop", Seq: 2}
	node := model.Node{
		ID: "n1", RunID: "r1", Type: model.NodeToolCall, Name: "Bash",
		Payload: &model.Payload{
			Input: json.RawMessage(`{"command":"export TOKEN=AKIAIOSFODNN7EXAMPLE"}`),
			Hash:  "deadbeef",
		},
		PayloadHash: "deadbeef",
	}
	run := model.Run{
		ID: "r1", Status: model.StatusRunning,
		Labels: map[string]string{"team": "core", "note": seedRunLabelSecret},
		Repro: &model.ReproMeta{
			Cwd:         "/deploy/" + seedRunCwdSecret + "/build",
			PromptsHash: strings.Repeat("ab", 32),
		},
	}
	ob, err := json.Marshal(obs)
	require.NoError(t, err)
	cb, err := json.Marshal(clean)
	require.NoError(t, err)
	nb, err := json.Marshal(node)
	require.NoError(t, err)
	rb, err := json.Marshal(run)
	require.NoError(t, err)

	seed := seedV3Schema(t, path)
	_, err = seed.Exec(`INSERT INTO observations(obs_id, run_id, execution_id, seq, body) VALUES('o1','r1','e1',1,?),('o2','r1','e1',2,?)`, string(ob), string(cb))
	require.NoError(t, err)
	_, err = seed.Exec(`INSERT INTO nodes(id, run_id, body) VALUES('n1','r1',?)`, string(nb))
	require.NoError(t, err)
	_, err = seed.Exec(`INSERT INTO runs(run_id, status, body) VALUES('r1','running',?)`, string(rb))
	require.NoError(t, err)
	stampAndClose(t, seed)
	return string(ob), string(cb), string(nb), string(rb)
}

func migrateToV4(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = migrate(db, 3, schemaMigrations[:4], discardLogger())
	require.NoError(t, err)
	return db
}

func TestOpenMigratesV3ToV4ScrubbingBodies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "g.db")
	secretBody, cleanBody, nodeSeed, runSeed := seedV3DB(t, path)

	db := migrateToV4(t, path)
	assert.Equal(t, 4, userVersion(t, db))

	var obsBody string
	require.NoError(t, db.QueryRow("SELECT body FROM observations WHERE obs_id='o1'").Scan(&obsBody))
	assert.NotEqual(t, secretBody, obsBody)
	assert.NotContains(t, obsBody, "kesha_dev_password")
	assert.NotContains(t, obsBody, "AKIAIOSFODNN7EXAMPLE")
	assert.Contains(t, obsBody, "‹redacted:connection-string›")
	assert.Contains(t, obsBody, "‹redacted:aws-key›")
	var o model.Observation
	require.NoError(t, json.Unmarshal([]byte(obsBody), &o))
	assert.Equal(t, model.HashPayload(o.Payload), o.Payload.Hash)
	assert.NotEqual(t, "deadbeef", o.Payload.Hash)

	var cleanGot string
	require.NoError(t, db.QueryRow("SELECT body FROM observations WHERE obs_id='o2'").Scan(&cleanGot))
	assert.Equal(t, cleanBody, cleanGot)

	var nodeBody string
	require.NoError(t, db.QueryRow("SELECT body FROM nodes WHERE id='n1'").Scan(&nodeBody))
	assert.NotEqual(t, nodeSeed, nodeBody)
	assert.NotContains(t, nodeBody, "AKIAIOSFODNN7EXAMPLE")
	var n model.Node
	require.NoError(t, json.Unmarshal([]byte(nodeBody), &n))
	assert.Equal(t, model.HashPayload(n.Payload), n.PayloadHash)
	assert.Equal(t, n.Payload.Hash, n.PayloadHash)

	var runBody string
	require.NoError(t, db.QueryRow("SELECT body FROM runs WHERE run_id='r1'").Scan(&runBody))
	assert.NotEqual(t, runSeed, runBody)
	assert.NotContains(t, runBody, seedRunCwdSecret)
	assert.NotContains(t, runBody, "run_label_password")
	var r model.Run
	require.NoError(t, json.Unmarshal([]byte(runBody), &r))
	assert.Equal(t, "/deploy/‹redacted:aws-key›/build", r.Repro.Cwd)
	assert.Equal(t, "‹redacted:connection-string›", r.Labels["note"])
	assert.Equal(t, "core", r.Labels["team"])
	assert.Equal(t, strings.Repeat("ab", 32), r.Repro.PromptsHash)
}

func TestApplySchemaV4IsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "g.db")
	seedV3DB(t, path)
	db := migrateToV4(t, path)
	tx, err := db.Begin()
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	obsChanged, err := scrubTable(tx, "SELECT obs_id, body FROM observations", "UPDATE observations SET body = ? WHERE obs_id = ?", scrubObservationBody)
	require.NoError(t, err)
	nodesChanged, err := scrubTable(tx, "SELECT id, body FROM nodes", "UPDATE nodes SET body = ? WHERE id = ?", scrubNodeBody)
	require.NoError(t, err)
	runsChanged, err := scrubTable(tx, "SELECT run_id, body FROM runs", "UPDATE runs SET body = ? WHERE run_id = ?", scrubRunBody)
	require.NoError(t, err)
	assert.Zero(t, obsChanged)
	assert.Zero(t, nodesChanged)
	assert.Zero(t, runsChanged)
	require.NoError(t, tx.Rollback())
}

func TestMigrationLeavesNoSecretBytesInFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "g.db")
	seedV3DB(t, path)
	s, err := openSQLite(sql.Open, path)
	require.NoError(t, err)
	require.NoError(t, s.Close())
	for _, f := range []string{path, path + "-wal"} {
		blob, rerr := os.ReadFile(f)
		if rerr != nil {
			continue
		}
		assert.NotContains(t, string(blob), "kesha_dev_password", f)
		assert.NotContains(t, string(blob), "AKIAIOSFODNN7EXAMPLE", f)
		assert.NotContains(t, string(blob), seedRunCwdSecret, f)
		assert.NotContains(t, string(blob), "run_label_password", f)
	}
}

func TestOpenVacuumsUnversionedV0DBWithScrubbedData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "g.db")
	run := model.Run{
		ID:     "r1",
		Status: model.StatusRunning,
		Labels: map[string]string{
			"filler": strings.Repeat("benign low entropy padding words ", 400),
			"note":   seedRunLabelSecret,
		},
	}
	rb, err := json.Marshal(run)
	require.NoError(t, err)
	seed := seedV3Schema(t, path)
	_, err = seed.Exec(`INSERT INTO runs(run_id, status, body) VALUES('r1','running',?)`, string(rb))
	require.NoError(t, err)
	_, err = seed.Exec("PRAGMA user_version = 0")
	require.NoError(t, err)
	require.NoError(t, seed.Close())

	s, err := openSQLite(sql.Open, path)
	require.NoError(t, err)
	require.NoError(t, s.Close())

	for _, f := range []string{path, path + "-wal"} {
		blob, rerr := os.ReadFile(f)
		if rerr != nil {
			continue
		}
		assert.NotContains(t, string(blob), "run_label_password", f)
	}
}

func TestOpenSQLiteReadOnlyRefusesOutdatedSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "g.db")
	seedV3DB(t, path)
	_, err := openSQLiteReadOnly(sql.Open, path)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSchemaOutdated)
}

func TestOpenSQLiteReadOnlyAcceptsCurrentSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "g.db")
	seedV3DB(t, path)
	s, err := openSQLite(sql.Open, path)
	require.NoError(t, err)
	require.NoError(t, s.Close())
	ro, err := openSQLiteReadOnly(sql.Open, path)
	require.NoError(t, err)
	require.NoError(t, ro.Close())
}

func TestScrubTableSelectError(t *testing.T) {
	db := rawDB(t)
	tx, err := db.Begin()
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	_, err = scrubTable(tx, "SELECT obs_id, body FROM observations", "", scrubObservationBody)
	require.Error(t, err)
}

func TestScrubTableRewriteError(t *testing.T) {
	db := rawDB(t)
	_, err := db.Exec(legacySchema)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO observations(obs_id, run_id, execution_id, seq, body) VALUES('bad','r','e',1,'not-json')`)
	require.NoError(t, err)
	tx, err := db.Begin()
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	_, err = scrubTable(tx, "SELECT obs_id, body FROM observations", "UPDATE observations SET body = ? WHERE obs_id = ?", scrubObservationBody)
	require.Error(t, err)
}

func TestScrubNodeBodyRejectsInvalidJSON(t *testing.T) {
	_, err := scrubNodeBody([]byte("not-json"))
	require.Error(t, err)
}

func TestScrubRunBodyRejectsInvalidJSON(t *testing.T) {
	_, err := scrubRunBody([]byte("not-json"))
	require.Error(t, err)
}

func TestApplySchemaV4RunErrorPropagates(t *testing.T) {
	db := rawDB(t)
	_, err := db.Exec(legacySchema)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO runs(run_id, status, body) VALUES('bad','running','not-json')`)
	require.NoError(t, err)
	tx, err := db.Begin()
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	_, err = applySchemaV4(tx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "runs")
}

func TestScrubTableUpdateError(t *testing.T) {
	db := rawDB(t)
	_, err := db.Exec(legacySchema)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TRIGGER obs_frozen BEFORE UPDATE ON observations BEGIN SELECT RAISE(ABORT, 'frozen'); END`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO observations(obs_id, run_id, execution_id, seq, body) VALUES('o1','r','e',1,'{"obs_id":"o1","attrs":{"prompt":"AKIAIOSFODNN7EXAMPLE"},"event_time":"0001-01-01T00:00:00Z","observed_at":"0001-01-01T00:00:00Z","run_id":"r","execution_id":"e","source":"hook","kind":"k","correlation":{},"seq":1}')`)
	require.NoError(t, err)
	tx, err := db.Begin()
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	_, err = scrubTable(tx, "SELECT obs_id, body FROM observations", "UPDATE observations SET body = ? WHERE obs_id = ?", scrubObservationBody)
	require.Error(t, err)
}

func TestApplySchemaV4NodeErrorPropagates(t *testing.T) {
	db := rawDB(t)
	_, err := db.Exec(legacySchema)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO nodes(id, run_id, body) VALUES('bad','r','not-json')`)
	require.NoError(t, err)
	tx, err := db.Begin()
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	_, errV4 := applySchemaV4(tx)
	require.Error(t, errV4)
}

type failingScanner struct {
	next    int
	scanErr error
	rowsErr error
}

func (f *failingScanner) Next() bool        { f.next++; return f.next == 1 && f.scanErr != nil }
func (f *failingScanner) Close() error      { return nil }
func (f *failingScanner) Err() error        { return f.rowsErr }
func (f *failingScanner) Scan(...any) error { return f.scanErr }

func TestCollectScrubbedScanAndRowsErrors(t *testing.T) {
	_, err := collectScrubbed(&failingScanner{scanErr: errors.New("scan boom")}, scrubObservationBody)
	require.Error(t, err)
	_, err = collectScrubbed(&failingScanner{rowsErr: errors.New("rows boom")}, scrubObservationBody)
	require.Error(t, err)
}

func TestOpenMigratesV3FailureRollsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "g.db")
	seed := seedV3Schema(t, path)
	_, err := seed.Exec(`INSERT INTO observations(obs_id, run_id, execution_id, seq, body) VALUES('bad','r','e',1,'not-json')`)
	require.NoError(t, err)
	stampAndClose(t, seed)

	_, err = openSQLite(sql.Open, path)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSchemaMigrationFailed)

	check, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = check.Close() })
	assert.Equal(t, 3, userVersion(t, check))
	var body string
	require.NoError(t, check.QueryRow("SELECT body FROM observations WHERE obs_id='bad'").Scan(&body))
	assert.Equal(t, "not-json", body)
}

func TestFreshAndV3UpgradeConvergeOnSchema(t *testing.T) {
	fresh := fileStore(t)
	path := filepath.Join(t.TempDir(), "v3.db")
	seedV3DB(t, path)
	upgraded, err := openSQLite(sql.Open, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = upgraded.Close() })
	assert.Equal(t, schemaDDL(t, fresh.db), schemaDDL(t, upgraded.(*sqliteStore).db))
}

func TestMigrationPreservesTypedRefPayloadHash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "g.db")
	obs := model.Observation{
		ObsID: "o3", RunID: "r1", ExecutionID: "e1", Source: model.SourceHook, Kind: "assistant_tool_use", Seq: 3,
		Payload: &model.Payload{
			Input: json.RawMessage(`"‹ref:64,00112233aabbccdd›"`),
			Hash:  "content-hash-by-design",
		},
	}
	node := model.Node{
		ID: "n2", RunID: "r1", Type: model.NodeToolCall, Name: "Bash",
		Payload: &model.Payload{
			Input: json.RawMessage(`"‹binary:1048576,0123456789abcdef›"`),
			Hash:  "content-hash-by-design",
		},
		PayloadHash: "content-hash-by-design",
	}
	ob, err := json.Marshal(obs)
	require.NoError(t, err)
	nb, err := json.Marshal(node)
	require.NoError(t, err)
	seed := seedV3Schema(t, path)
	_, err = seed.Exec(`INSERT INTO observations(obs_id, run_id, execution_id, seq, body) VALUES('o3','r1','e1',3,?)`, string(ob))
	require.NoError(t, err)
	_, err = seed.Exec(`INSERT INTO nodes(id, run_id, body) VALUES('n2','r1',?)`, string(nb))
	require.NoError(t, err)
	stampAndClose(t, seed)

	db := migrateToV4(t, path)

	var gotObs, gotNode string
	require.NoError(t, db.QueryRow("SELECT body FROM observations WHERE obs_id='o3'").Scan(&gotObs))
	require.NoError(t, db.QueryRow("SELECT body FROM nodes WHERE id='n2'").Scan(&gotNode))
	assert.Equal(t, string(ob), gotObs)
	assert.Equal(t, string(nb), gotNode)
}

func TestMigrationKeepsVerbatimBytesOfCleanRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "g.db")
	input := json.RawMessage(`{"command":"go build && go test > out.log"}`)
	obs := model.Observation{
		ObsID: "o5", RunID: "r1", ExecutionID: "e1", Source: model.SourceHook, Kind: "assistant_tool_use", Seq: 5,
		Payload: &model.Payload{
			Input: input,
			Hash:  model.HashPayload(&model.Payload{Input: input}),
		},
	}
	ob, err := marshalVerbatim(obs)
	require.NoError(t, err)
	seed := seedV3Schema(t, path)
	_, err = seed.Exec(`INSERT INTO observations(obs_id, run_id, execution_id, seq, body) VALUES('o5','r1','e1',5,?)`, string(ob))
	require.NoError(t, err)
	stampAndClose(t, seed)

	db := migrateToV4(t, path)

	var got string
	require.NoError(t, db.QueryRow("SELECT body FROM observations WHERE obs_id='o5'").Scan(&got))
	assert.Equal(t, string(ob), got)
	var o model.Observation
	require.NoError(t, json.Unmarshal([]byte(got), &o))
	assert.Equal(t, model.HashPayload(o.Payload), o.Payload.Hash)
}

func TestFreshDBHasExactlyOfflineTables(t *testing.T) {
	s := fileStore(t)
	assert.Equal(t, []string{"baselines", "regress_results"}, tableNames(t, s.db))
}

func TestOpenMigratesVOldToV5DroppingGraphTables(t *testing.T) {
	path := filepath.Join(t.TempDir(), "g.db")
	seedV3DB(t, path)
	raw, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	seedBaselineRow(t, raw, "kept")
	require.NoError(t, raw.Close())

	migrated, err := openSQLite(sql.Open, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = migrated.Close() })
	db := migrated.(*sqliteStore).db

	assert.Equal(t, currentSchemaVersion, userVersion(t, db))
	assert.Equal(t, []string{"baselines", "regress_results"}, tableNames(t, db))
	assert.Equal(t, []string{"kept"}, baselineNames(t, db))
}

func TestApplySchemaV5DropsDeadTablesIdempotently(t *testing.T) {
	db := rawDB(t)
	for _, stmt := range []string{legacySchema, schemaBaselines, schemaRegressResults} {
		_, err := db.Exec(stmt)
		require.NoError(t, err)
	}

	tx, err := db.Begin()
	require.NoError(t, err)
	counts, err := applySchemaV5(tx)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	assert.Equal(t, map[string]int{"graph_tables": len(deadGraphTables)}, counts)
	assert.Equal(t, []string{"baselines", "regress_results"}, tableNames(t, db))

	again, err := db.Begin()
	require.NoError(t, err)
	countsAgain, err := applySchemaV5(again)
	require.NoError(t, err)
	require.NoError(t, again.Commit())
	assert.Equal(t, map[string]int{"graph_tables": 0}, countsAgain)
}

func TestApplySchemaV5Error(t *testing.T) {
	db := rawDB(t)
	tx, err := db.Begin()
	require.NoError(t, err)
	require.NoError(t, tx.Rollback())
	_, err = applySchemaV5(tx)
	require.Error(t, err)
}

func TestScrubExistingErrorPropagates(t *testing.T) {
	db := rawDB(t)
	tx, err := db.Begin()
	require.NoError(t, err)
	require.NoError(t, tx.Rollback())
	_, err = scrubExisting(tx, "observations", "SELECT obs_id, body FROM observations", "UPDATE observations SET body = ? WHERE obs_id = ?", scrubObservationBody)
	require.Error(t, err)
}
