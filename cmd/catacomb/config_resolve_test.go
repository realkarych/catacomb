package main

import (
	"errors"
	"io/fs"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/config"
)

func envLookup(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}

func TestResolveConfigDefaultsWhenNoFile(t *testing.T) {
	read := func(string) ([]byte, error) { return nil, fs.ErrNotExist }
	cfg, err := resolveConfig(daemonFlags{}, read, envLookup(nil), "/home/u")
	require.NoError(t, err)
	assert.Equal(t, config.BackendSQLite, cfg.Store.Backend)
	assert.Equal(t, "/home/u/.catacomb/catacomb.db", cfg.Store.SQLite.Path)
}

func TestResolveConfigFileThenEnvThenFlags(t *testing.T) {
	read := func(string) ([]byte, error) {
		return []byte("store:\n  backend: sqlite\n  sqlite:\n    path: /from-file.db\ndaemon:\n  max_shards: 11\n"), nil
	}
	env := envLookup(map[string]string{"CATACOMB_DB": "/from-env.db"})
	flags := daemonFlags{dbPath: "/from-flag.db", dbPathSet: true, maxShards: 22, maxShardsSet: true}
	cfg, err := resolveConfig(flags, read, env, "/home/u")
	require.NoError(t, err)
	assert.Equal(t, "/from-flag.db", cfg.Store.SQLite.Path)
	assert.Equal(t, 22, cfg.Daemon.MaxShards)
}

func TestResolveConfigEnvBeatsFile(t *testing.T) {
	read := func(string) ([]byte, error) {
		return []byte("store:\n  sqlite:\n    path: /from-file.db\n"), nil
	}
	cfg, err := resolveConfig(daemonFlags{}, read, envLookup(map[string]string{"CATACOMB_DB": "/from-env.db"}), "/home/u")
	require.NoError(t, err)
	assert.Equal(t, "/from-env.db", cfg.Store.SQLite.Path)
}

func TestResolveConfigFlagOverridesDaemonFields(t *testing.T) {
	read := func(string) ([]byte, error) { return nil, fs.ErrNotExist }
	flags := daemonFlags{
		discoveryPath: "/d.json", discoveryPathSet: true,
		reaperWindow: time.Minute, reaperWindowSet: true,
		allowPayloadAccess: true, allowPayloadAccessSet: true,
		allowAnnotations: true, allowAnnotationsSet: true,
	}
	cfg, err := resolveConfig(flags, read, envLookup(nil), "/home/u")
	require.NoError(t, err)
	assert.Equal(t, "/d.json", cfg.Daemon.Discovery)
	assert.Equal(t, config.Duration(time.Minute), cfg.Daemon.ReaperWindow)
	assert.True(t, cfg.Daemon.AllowPayloadAccess)
	assert.True(t, cfg.Daemon.AllowAnnotations)
}

func TestResolveConfigExpandsTilde(t *testing.T) {
	read := func(string) ([]byte, error) {
		return []byte("store:\n  sqlite:\n    path: ~/db/x.db\n"), nil
	}
	cfg, err := resolveConfig(daemonFlags{}, read, envLookup(nil), "/home/u")
	require.NoError(t, err)
	assert.Equal(t, "/home/u/db/x.db", cfg.Store.SQLite.Path)
}

func TestResolveConfigParseError(t *testing.T) {
	read := func(string) ([]byte, error) { return []byte("store:\n  nope: 1\n"), nil }
	_, err := resolveConfig(daemonFlags{}, read, envLookup(nil), "/home/u")
	require.Error(t, err)
}

func TestResolveConfigReadError(t *testing.T) {
	read := func(string) ([]byte, error) { return nil, errors.New("disk") }
	_, err := resolveConfig(daemonFlags{}, read, envLookup(nil), "/home/u")
	require.Error(t, err)
}

func TestResolveConfigValidateError(t *testing.T) {
	read := func(string) ([]byte, error) {
		return []byte("store:\n  backend: postgres\n  postgres:\n    dsn: x\n"), nil
	}
	_, err := resolveConfig(daemonFlags{}, read, envLookup(nil), "/home/u")
	assert.ErrorIs(t, err, config.ErrBackendNotImplemented)
}

func TestConfigFilePathPrecedence(t *testing.T) {
	assert.Equal(t, "/flag.yaml", configFilePath(daemonFlags{configPath: "/flag.yaml", configPathSet: true}, envLookup(map[string]string{"CATACOMB_CONFIG": "/env.yaml"}), "/home/u"))
	assert.Equal(t, "/env.yaml", configFilePath(daemonFlags{}, envLookup(map[string]string{"CATACOMB_CONFIG": "/env.yaml"}), "/home/u"))
	assert.Equal(t, "/home/u/.catacomb/config.yaml", configFilePath(daemonFlags{}, envLookup(nil), "/home/u"))
}

func TestConfigFilePathExpandsEnvVar(t *testing.T) {
	env := envLookup(map[string]string{"MYDIR": "/expanded"})
	got := configFilePath(daemonFlags{configPath: "${MYDIR}/config.yaml", configPathSet: true}, env, "/home/u")
	assert.Equal(t, "/expanded/config.yaml", got)
}

func TestResolveConfigExpandsEnvVar(t *testing.T) {
	read := func(string) ([]byte, error) { return nil, fs.ErrNotExist }
	env := envLookup(map[string]string{"DBDIR": "/expanded"})
	flags := daemonFlags{dbPath: "${DBDIR}/x.db", dbPathSet: true}
	cfg, err := resolveConfig(flags, read, env, "/home/u")
	require.NoError(t, err)
	assert.Equal(t, "/expanded/x.db", cfg.Store.SQLite.Path)
}

func TestResolveConfigHomeExpansion(t *testing.T) {
	read := func(string) ([]byte, error) { return nil, fs.ErrNotExist }
	cfg, err := resolveConfig(daemonFlags{}, read, envLookup(nil), "/home/other")
	require.NoError(t, err)
	assert.Equal(t, "/home/other/.catacomb/catacomb.db", cfg.Store.SQLite.Path)
}
