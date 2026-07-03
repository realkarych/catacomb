package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
)

func tooNewDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "g.db")
	s, err := openSQLite(sql.Open, path)
	require.NoError(t, err)
	require.NoError(t, s.UpsertRun(model.Run{ID: "r1", Status: model.StatusRunning}))
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
	require.NoError(t, db.QueryRow("SELECT count(*) FROM runs").Scan(&n))
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
	_, err = seed.Exec(schema)
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

func TestOpenStampsFreshDBToCurrent(t *testing.T) {
	s := fileStore(t)
	assert.Equal(t, currentSchemaVersion, userVersion(t, s.db))
}

func TestReopenAtCurrentDoesNotRemigrate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "g.db")
	s, err := openSQLite(sql.Open, path)
	require.NoError(t, err)
	require.NoError(t, s.UpsertRun(model.Run{ID: "r1", Status: model.StatusRunning}))
	require.NoError(t, s.Close())

	again, err := openSQLite(sql.Open, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = again.Close() })
	assert.Equal(t, currentSchemaVersion, userVersion(t, again.(*sqliteStore).db))
	runs, err := again.Runs()
	require.NoError(t, err)
	require.Len(t, runs, 1)
}

func TestOpenMigratesUnversionedDBPreservingData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "g.db")
	s, err := openSQLite(sql.Open, path)
	require.NoError(t, err)
	require.NoError(t, s.UpsertRun(model.Run{ID: "r1", Status: model.StatusRunning}))
	_, err = s.(*sqliteStore).db.Exec("PRAGMA user_version = 0")
	require.NoError(t, err)
	require.NoError(t, s.Close())

	migrated, err := openSQLite(sql.Open, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = migrated.Close() })
	assert.Equal(t, currentSchemaVersion, userVersion(t, migrated.(*sqliteStore).db))
	runs, err := migrated.Runs()
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, "r1", runs[0].ID)
}

func TestMigrateAppliesInOrder(t *testing.T) {
	db := rawDB(t)
	var order []int
	migs := []migration{
		{from: 0, to: 1, apply: func(*sql.Tx) error { order = append(order, 1); return nil }},
		{from: 1, to: 2, apply: func(*sql.Tx) error { order = append(order, 2); return nil }},
	}
	require.NoError(t, migrate(db, 0, migs))
	assert.Equal(t, []int{1, 2}, order)
	assert.Equal(t, 2, userVersion(t, db))
}

func TestMigrateFailingStepRollsBackWithSentinel(t *testing.T) {
	db := rawDB(t)
	migs := []migration{
		{from: 0, to: 1, apply: func(*sql.Tx) error { return nil }},
		{from: 1, to: 2, apply: func(*sql.Tx) error { return errors.New("boom") }},
	}
	err := migrate(db, 0, migs)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSchemaMigrationFailed)
	assert.Equal(t, 1, userVersion(t, db))
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
	require.Error(t, applyMigration(db, migration{from: 0, to: 1, apply: func(*sql.Tx) error { return nil }}))
}

func TestApplyMigrationApplyError(t *testing.T) {
	db := rawDB(t)
	err := applyMigration(db, migration{from: 0, to: 1, apply: func(*sql.Tx) error { return errors.New("boom") }})
	require.Error(t, err)
	assert.Equal(t, 0, userVersion(t, db))
}

func TestApplyMigrationStampError(t *testing.T) {
	db := rawDB(t)
	err := applyMigration(db, migration{from: 0, to: 1, apply: func(tx *sql.Tx) error { return tx.Rollback() }})
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
	m := migration{from: 0, to: 1, apply: func(tx *sql.Tx) error {
		if _, e := tx.Exec("CREATE TABLE probe(x)"); e != nil {
			return e
		}
		return errors.New("boom after ddl")
	}}
	err = applyMigration(db, m)
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
	_, err := db.Exec("CREATE TABLE observations(x)")
	require.NoError(t, err)
	tx, err := db.Begin()
	require.NoError(t, err)
	require.Error(t, applySchemaV1(tx))
	_ = tx.Rollback()
}

func TestApplySchemaV2Error(t *testing.T) {
	db := rawDB(t)
	tx, err := db.Begin()
	require.NoError(t, err)
	require.NoError(t, tx.Rollback())
	require.Error(t, applySchemaV2(tx))
}

func seedV1DB(t *testing.T, path string) {
	t.Helper()
	seed, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	_, err = seed.Exec(schema)
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

	runs, err := migrated.Runs()
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, "r1", runs[0].ID)

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
	_, err = seed.Exec(schema)
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

	runs, err := migrated.Runs()
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, "r1", runs[0].ID)

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
	require.Error(t, applySchemaV3(tx))
}

func seedV3Schema(t *testing.T, path string) *sql.DB {
	t.Helper()
	seed, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	for _, stmt := range []string{schema, schemaBaselines, schemaRegressResults} {
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

func seedV3DB(t *testing.T, path string) (secretObsBody, cleanObsBody, secretNodeBody string) {
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
	ob, err := json.Marshal(obs)
	require.NoError(t, err)
	cb, err := json.Marshal(clean)
	require.NoError(t, err)
	nb, err := json.Marshal(node)
	require.NoError(t, err)

	seed := seedV3Schema(t, path)
	_, err = seed.Exec(`INSERT INTO observations(obs_id, run_id, execution_id, seq, body) VALUES('o1','r1','e1',1,?),('o2','r1','e1',2,?)`, string(ob), string(cb))
	require.NoError(t, err)
	_, err = seed.Exec(`INSERT INTO nodes(id, run_id, body) VALUES('n1','r1',?)`, string(nb))
	require.NoError(t, err)
	stampAndClose(t, seed)
	return string(ob), string(cb), string(nb)
}

func TestOpenMigratesV3ToV4ScrubbingBodies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "g.db")
	secretBody, cleanBody, nodeSeed := seedV3DB(t, path)

	migrated, err := openSQLite(sql.Open, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = migrated.Close() })
	db := migrated.(*sqliteStore).db
	assert.Equal(t, currentSchemaVersion, userVersion(t, db))

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
}

func TestApplySchemaV4IsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "g.db")
	seedV3DB(t, path)
	migrated, err := openSQLite(sql.Open, path)
	require.NoError(t, err)
	db := migrated.(*sqliteStore).db
	tx, err := db.Begin()
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	obsChanged, err := scrubTable(tx, "SELECT obs_id, body FROM observations", "UPDATE observations SET body = ? WHERE obs_id = ?", scrubObservationBody)
	require.NoError(t, err)
	nodesChanged, err := scrubTable(tx, "SELECT id, body FROM nodes", "UPDATE nodes SET body = ? WHERE id = ?", scrubNodeBody)
	require.NoError(t, err)
	assert.Zero(t, obsChanged)
	assert.Zero(t, nodesChanged)
	require.NoError(t, tx.Rollback())
	require.NoError(t, migrated.Close())
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
	_, err := db.Exec(schema)
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

func TestScrubTableUpdateError(t *testing.T) {
	db := rawDB(t)
	_, err := db.Exec(schema)
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
	_, err := db.Exec(schema)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO nodes(id, run_id, body) VALUES('bad','r','not-json')`)
	require.NoError(t, err)
	tx, err := db.Begin()
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	require.Error(t, applySchemaV4(tx))
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

	migrated, err := openSQLite(sql.Open, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = migrated.Close() })
	db := migrated.(*sqliteStore).db

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

	migrated, err := openSQLite(sql.Open, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = migrated.Close() })
	db := migrated.(*sqliteStore).db

	var got string
	require.NoError(t, db.QueryRow("SELECT body FROM observations WHERE obs_id='o5'").Scan(&got))
	assert.Equal(t, string(ob), got)
	var o model.Observation
	require.NoError(t, json.Unmarshal([]byte(got), &o))
	assert.Equal(t, model.HashPayload(o.Payload), o.Payload.Hash)
}
