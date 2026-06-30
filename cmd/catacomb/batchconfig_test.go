package main

import (
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolveStorePathDefaultsWhenNoFile(t *testing.T) {
	read := func(string) ([]byte, error) { return nil, fs.ErrNotExist }
	got := resolveStorePath(read, envLookup(nil), "/home/u")
	assert.Equal(t, filepath.FromSlash("/home/u/.catacomb/catacomb.db"), got)
}

func TestResolveStorePathFromFile(t *testing.T) {
	read := func(string) ([]byte, error) {
		return []byte("store:\n  backend: sqlite\n  sqlite:\n    path: /custom.db\n"), nil
	}
	got := resolveStorePath(read, envLookup(nil), "/home/u")
	assert.Equal(t, "/custom.db", got)
}

func TestResolveStorePathEnvBeatsFile(t *testing.T) {
	read := func(string) ([]byte, error) {
		return []byte("store:\n  sqlite:\n    path: /from-file.db\n"), nil
	}
	got := resolveStorePath(read, envLookup(map[string]string{"CATACOMB_DB": "/from-env.db"}), "/home/u")
	assert.Equal(t, "/from-env.db", got)
}

func TestResolveStorePathExpandsTilde(t *testing.T) {
	read := func(string) ([]byte, error) {
		return []byte("store:\n  sqlite:\n    path: ~/sub/db.db\n"), nil
	}
	got := resolveStorePath(read, envLookup(nil), "/home/u")
	assert.Equal(t, filepath.FromSlash("/home/u/sub/db.db"), got)
}

func TestResolveStorePathFallsBackOnBadConfig(t *testing.T) {
	read := func(string) ([]byte, error) {
		return []byte("store:\n  nope_key: 1\n"), nil
	}
	got := resolveStorePath(read, envLookup(nil), "/home/u")
	assert.Equal(t, filepath.FromSlash("/home/u/.catacomb/catacomb.db"), got)
}

func TestResolveStorePathFallsBackOnReadError(t *testing.T) {
	read := func(string) ([]byte, error) { return nil, fs.ErrPermission }
	got := resolveStorePath(read, envLookup(nil), "/home/u")
	assert.Equal(t, filepath.FromSlash("/home/u/.catacomb/catacomb.db"), got)
}

func TestResolveStorePathExpandsEnvVar(t *testing.T) {
	read := func(string) ([]byte, error) { return nil, fs.ErrNotExist }
	env := envLookup(map[string]string{"DBDIR": "/expanded"})
	got := resolveStorePath(read, env, "/home/u")
	assert.Equal(t, filepath.FromSlash("/home/u/.catacomb/catacomb.db"), got)
	readWithVar := func(string) ([]byte, error) {
		return []byte("store:\n  sqlite:\n    path: ${DBDIR}/x.db\n"), nil
	}
	got2 := resolveStorePath(readWithVar, env, "/home/u")
	assert.Equal(t, "/expanded/x.db", got2)
}

func TestDefaultBatchDBPathConsistentWithDefaultDBPath(t *testing.T) {
	orig := osUserHomeDir
	t.Cleanup(func() { osUserHomeDir = orig })
	osUserHomeDir = func() (string, error) { return "/home/u", nil }
	assert.Equal(t, defaultDBPath(), defaultBatchDBPath())
}

func TestBatchCommandsUseDefaultBatchDBPath(t *testing.T) {
	orig := osUserHomeDir
	t.Cleanup(func() { osUserHomeDir = orig })
	osUserHomeDir = func() (string, error) { return "/home/u", nil }
	expected := defaultBatchDBPath()
	for _, tc := range []struct {
		name string
		got  string
	}{
		{"runs", newRunsCmd().Flags().Lookup("db").DefValue},
		{"inspect", newInspectCmd().Flags().Lookup("db").DefValue},
		{"snapshot", newSnapshotCmd().Flags().Lookup("db").DefValue},
		{"export", newExportCmd().Flags().Lookup("db").DefValue},
	} {
		assert.Equal(t, expected, tc.got, "command %s", tc.name)
	}
}
