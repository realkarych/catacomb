package store

import (
	"fmt"

	"github.com/realkarych/catacomb/config"
	"github.com/realkarych/catacomb/store/memory"
)

func Open(cfg config.StoreConfig) (Store, error) {
	switch cfg.Backend {
	case config.BackendSQLite:
		return OpenSQLite(cfg.SQLite.Path)
	case config.BackendMemory:
		return memory.New(), nil
	case config.BackendPostgres:
		return nil, fmt.Errorf("store.Open: %w", config.ErrBackendNotImplemented)
	default:
		return nil, fmt.Errorf("store.Open: %w", config.ErrUnknownStoreBackend)
	}
}
