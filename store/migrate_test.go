package store

import (
	"database/sql"
	"errors"
	"fmt"
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
