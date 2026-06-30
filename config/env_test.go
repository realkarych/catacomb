package config

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func lookupMap(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}

func getenvMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestFromEnv(t *testing.T) {
	c := FromEnv(lookupMap(map[string]string{"CATACOMB_DB": "/e.db", "CATACOMB_DISCOVERY": "/e.json"}))
	assert.Equal(t, "/e.db", c.Store.SQLite.Path)
	assert.Equal(t, "/e.json", c.Daemon.Discovery)
}

func TestFromEnvEmpty(t *testing.T) {
	c := FromEnv(lookupMap(map[string]string{}))
	assert.Equal(t, Config{}, c)
}

func TestExpandPath(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"tilde only", "~", "/home/u"},
		{"tilde slash", "~/.catacomb/x.db", filepath.FromSlash("/home/u/.catacomb/x.db")},
		{"env var", "$ROOT/x", "/r/x"},
		{"braced env", "${ROOT}/x", "/r/x"},
		{"absolute untouched", "/abs/x", "/abs/x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExpandPath(tt.in, "/home/u", getenvMap(map[string]string{"ROOT": "/r"}))
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExpandPaths(t *testing.T) {
	c := Config{
		Store:   StoreConfig{SQLite: SQLiteConfig{Path: "~/.catacomb/x.db"}},
		Daemon:  DaemonConfig{Discovery: "~/run/d.json"},
		Sources: SourcesConfig{JSONL: JSONLSource{TranscriptDir: "~/proj"}},
		Sinks:   []Sink{{Type: SinkJSONL, Path: "~/out.jsonl"}},
	}
	got := ExpandPaths(c, "/home/u", getenvMap(nil))
	assert.Equal(t, filepath.FromSlash("/home/u/.catacomb/x.db"), got.Store.SQLite.Path)
	assert.Equal(t, filepath.FromSlash("/home/u/run/d.json"), got.Daemon.Discovery)
	assert.Equal(t, filepath.FromSlash("/home/u/proj"), got.Sources.JSONL.TranscriptDir)
	assert.Equal(t, filepath.FromSlash("/home/u/out.jsonl"), got.Sinks[0].Path)
	assert.Equal(t, "~/out.jsonl", c.Sinks[0].Path)
}
