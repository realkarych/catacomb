package store_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/store"
	"github.com/realkarych/catacomb/store/storetest"
)

func TestSQLiteContract(t *testing.T) {
	storetest.RunStoreContract(t, func(t *testing.T) store.Store {
		s, err := store.OpenSQLite(filepath.Join(t.TempDir(), "g.db"))
		require.NoError(t, err)
		t.Cleanup(func() { _ = s.Close() })
		return s
	})
}
