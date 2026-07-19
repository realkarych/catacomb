package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/store"
)

var errOpenBoom = errors.New("storeread test: open failed")

func failingOpener(string) (store.Store, error) { return nil, errOpenBoom }

func TestOpenReadStoreMissingDBIsNotFoundNotAnOpenAttempt(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.db")
	opened := false
	tracking := func(p string) (store.Store, error) {
		opened = true
		return store.OpenSQLiteReadOnly(p)
	}
	s, err := openReadStore(tracking, missing)
	require.ErrorIs(t, err, ErrStoreNotFound)
	assert.Nil(t, s)
	assert.False(t, opened, "a missing db must short-circuit before opening")
	_, statErr := os.Stat(missing)
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestOpenReadStoreWrapsOpenerError(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "*.db")
	require.NoError(t, err)
	_ = f.Close()
	s, err := openReadStore(failingOpener, f.Name())
	require.ErrorIs(t, err, errOpenBoom)
	assert.NotErrorIs(t, err, ErrStoreNotFound)
	assert.Nil(t, s)
}

func TestOpenReadStoreReturnsAUsableStore(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "g.db")
	w, err := store.OpenSQLite(dbPath)
	require.NoError(t, err)
	require.NoError(t, w.UpsertBaseline(model.Baseline{Name: "golden", RunIDs: []string{"r1"}}))
	require.NoError(t, w.Close())

	s, err := openReadStore(store.OpenSQLiteReadOnly, dbPath)
	require.NoError(t, err)
	require.NotNil(t, s)
	t.Cleanup(func() { _ = s.Close() })

	b, ok, err := s.GetBaseline("golden")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, []string{"r1"}, b.RunIDs)
}

func TestOpenWriteStoreOpensAMissingDBAndWrapsFailures(t *testing.T) {
	fresh := filepath.Join(t.TempDir(), "fresh", "catacomb.db")
	_, statErr := os.Stat(fresh)
	require.ErrorIs(t, statErr, os.ErrNotExist)

	s, err := openWriteStore(store.OpenSQLite, fresh)
	require.NoError(t, err)
	require.NotNil(t, s)
	require.NoError(t, s.UpsertBaseline(model.Baseline{Name: "golden", RunIDs: []string{"r1"}}))
	require.NoError(t, s.Close())
	require.FileExists(t, fresh)

	failed, err := openWriteStore(failingOpener, fresh)
	require.ErrorIs(t, err, errOpenBoom)
	assert.Nil(t, failed)
}
