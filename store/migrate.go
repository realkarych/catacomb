package store

import (
	"database/sql"
	"errors"
	"fmt"
)

const currentSchemaVersion = 1

var ErrSchemaMigrationFailed = errors.New("store: schema migration failed")

type migration struct {
	from  int
	to    int
	apply func(*sql.Tx) error
}

var schemaMigrations = []migration{
	{from: 0, to: 1, apply: applySchemaV1},
}

func applySchemaV1(tx *sql.Tx) error {
	if _, err := tx.Exec(schema); err != nil {
		return fmt.Errorf("store.applySchemaV1: %w", err)
	}
	return nil
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

func migrate(db *sql.DB, migrations []migration) error {
	version, err := readSchemaVersion(db)
	if err != nil {
		return err
	}
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
