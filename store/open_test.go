package store_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/config"
	"github.com/realkarych/catacomb/store"
)

func TestOpenSQLite(t *testing.T) {
	s, err := store.Open(config.StoreConfig{Backend: config.BackendSQLite, SQLite: config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "g.db")}})
	require.NoError(t, err)
	require.NotNil(t, s)
	t.Cleanup(func() { _ = s.Close() })
	runs, err := s.Runs()
	require.NoError(t, err)
	assert.Empty(t, runs)
}

func TestOpenSQLiteError(t *testing.T) {
	_, err := store.Open(config.StoreConfig{Backend: config.BackendSQLite, SQLite: config.SQLiteConfig{Path: "/nonexistent/dir/g.db"}})
	require.Error(t, err)
}

func TestOpenMemory(t *testing.T) {
	s, err := store.Open(config.StoreConfig{Backend: config.BackendMemory})
	require.NoError(t, err)
	require.NotNil(t, s)
	max, err := s.MaxSeq()
	require.NoError(t, err)
	assert.Equal(t, uint64(0), max)
}

func TestOpenPostgresNotImplemented(t *testing.T) {
	_, err := store.Open(config.StoreConfig{Backend: config.BackendPostgres, Postgres: config.PostgresConfig{DSN: "x"}})
	assert.ErrorIs(t, err, config.ErrBackendNotImplemented)
}

func TestOpenUnknownBackend(t *testing.T) {
	_, err := store.Open(config.StoreConfig{Backend: "redis"})
	assert.ErrorIs(t, err, config.ErrUnknownStoreBackend)
}
