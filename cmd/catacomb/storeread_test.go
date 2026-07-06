package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/store"
)

func TestOpenReadStoreMissing(t *testing.T) {
	_, err := openReadStore(store.OpenSQLiteReadOnly, filepath.Join(t.TempDir(), "nope.db"))
	assert.True(t, errors.Is(err, ErrStoreNotFound))
}

func TestOpenReadStoreOpenError(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "*.db")
	require.NoError(t, err)
	_ = f.Close()
	badOpen := func(string) (store.Store, error) { return nil, errors.New("boom") }
	_, err = openReadStore(badOpen, f.Name())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "store open")
}

func TestOpenReadStoreSuccess(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "g.db")
	w, err := store.OpenSQLite(dbPath)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	s, err := openReadStore(store.OpenSQLiteReadOnly, dbPath)
	require.NoError(t, err)
	require.NotNil(t, s)
	_ = s.Close()
}
