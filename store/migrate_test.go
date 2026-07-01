package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
)

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
	require.NoError(t, migrate(db, migs))
	assert.Equal(t, []int{1, 2}, order)
	assert.Equal(t, 2, userVersion(t, db))
}

func TestMigrateFailingStepRollsBackWithSentinel(t *testing.T) {
	db := rawDB(t)
	migs := []migration{
		{from: 0, to: 1, apply: func(*sql.Tx) error { return nil }},
		{from: 1, to: 2, apply: func(*sql.Tx) error { return errors.New("boom") }},
	}
	err := migrate(db, migs)
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

func TestMigrateReadVersionError(t *testing.T) {
	db := rawDB(t)
	require.NoError(t, db.Close())
	require.Error(t, migrate(db, schemaMigrations))
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
