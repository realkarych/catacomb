package config

import "errors"

const (
	BackendSQLite   = "sqlite"
	BackendMemory   = "memory"
	BackendPostgres = "postgres"
)

var (
	ErrUnknownStoreBackend   = errors.New("config: unknown store backend")
	ErrBackendNotImplemented = errors.New("config: store backend not implemented")
)

type StoreConfig struct {
	Backend  string
	SQLite   SQLiteConfig
	Postgres PostgresConfig
}

type SQLiteConfig struct {
	Path string
}

type PostgresConfig struct {
	DSN string
}
