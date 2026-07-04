package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeOverrideWinsWhereSet(t *testing.T) {
	base := Defaults()
	over := Config{
		Store:  StoreConfig{Backend: BackendMemory, SQLite: SQLiteConfig{Path: "/p.db"}, Postgres: PostgresConfig{DSN: "dsn"}},
		Daemon: DaemonConfig{Discovery: "/d.json", ReaperWindow: Duration(time.Minute), MaxShards: 9, AllowPayloadAccess: true, AllowAnnotations: true},
	}
	got := Merge(base, over)
	assert.Equal(t, BackendMemory, got.Store.Backend)
	assert.Equal(t, "/p.db", got.Store.SQLite.Path)
	assert.Equal(t, "dsn", got.Store.Postgres.DSN)
	assert.Equal(t, "/d.json", got.Daemon.Discovery)
	assert.Equal(t, Duration(time.Minute), got.Daemon.ReaperWindow)
	assert.Equal(t, 9, got.Daemon.MaxShards)
	assert.True(t, got.Daemon.AllowPayloadAccess)
	assert.True(t, got.Daemon.AllowAnnotations)
}

func TestMergeUnsetKeepsBase(t *testing.T) {
	base := Defaults()
	got := Merge(base, Config{})
	assert.Equal(t, BackendSQLite, got.Store.Backend)
	assert.Equal(t, DefaultSQLitePath, got.Store.SQLite.Path)
	assert.Equal(t, 4096, got.Daemon.MaxShards)
	assert.Equal(t, Duration(30*time.Minute), got.Daemon.ReaperWindow)
	require.NotNil(t, got.Sources.JSONL.Enabled)
	assert.False(t, *got.Sources.JSONL.Enabled)
}

func TestMergeToggleAndJSONL(t *testing.T) {
	base := Defaults()
	over := Config{Sources: SourcesConfig{
		Hooks: SourceToggle{Enabled: boolPtr(false)},
		JSONL: JSONLSource{Enabled: boolPtr(true), TranscriptDir: "/t", Exclude: []string{"x"}},
	}}
	got := Merge(base, over)
	require.NotNil(t, got.Sources.Hooks.Enabled)
	assert.False(t, *got.Sources.Hooks.Enabled)
	require.NotNil(t, got.Sources.JSONL.Enabled)
	assert.True(t, *got.Sources.JSONL.Enabled)
	assert.Equal(t, "/t", got.Sources.JSONL.TranscriptDir)
	assert.Equal(t, []string{"x"}, got.Sources.JSONL.Exclude)
	require.NotNil(t, got.Sources.Otel.Enabled)
	assert.True(t, *got.Sources.Otel.Enabled)
}

func TestMergePayloads(t *testing.T) {
	base := Defaults()
	out := Merge(base, Config{Payloads: PayloadsConfig{Mode: PayloadModeAll}})
	assert.Equal(t, PayloadModeAll, out.Payloads.Mode)
	assert.Equal(t, DefaultPayloadMaxBytes, out.Payloads.MaxBytes)

	out = Merge(base, Config{Payloads: PayloadsConfig{MaxBytes: 4096}})
	assert.Equal(t, PayloadModeRedact, out.Payloads.Mode)
	assert.Equal(t, 4096, out.Payloads.MaxBytes)

	out = Merge(base, Config{})
	assert.Equal(t, base.Payloads, out.Payloads)
}

func TestMergeSinksReplace(t *testing.T) {
	base := Config{Sinks: []Sink{{Type: SinkJSONL, Path: "/a"}}}
	got := Merge(base, Config{Sinks: []Sink{{Type: SinkOTLP, Endpoint: "e"}}})
	require.Len(t, got.Sinks, 1)
	assert.Equal(t, SinkOTLP, got.Sinks[0].Type)
	keep := Merge(base, Config{})
	require.Len(t, keep.Sinks, 1)
	assert.Equal(t, SinkJSONL, keep.Sinks[0].Type)
}
