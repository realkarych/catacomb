package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePartial(t *testing.T) {
	data := []byte("store:\n  backend: memory\ndaemon:\n  reaper_window: 5m\n  max_shards: 8\n")
	c, err := Parse(data)
	require.NoError(t, err)
	assert.Equal(t, BackendMemory, c.Store.Backend)
	assert.Equal(t, Duration(5*time.Minute), c.Daemon.ReaperWindow)
	assert.Equal(t, 8, c.Daemon.MaxShards)
}

func TestParseSinksAndSources(t *testing.T) {
	data := []byte("sources:\n  jsonl:\n    enabled: true\n    transcript_dir: /t\nsinks:\n  - { type: jsonl, path: /x.jsonl }\n")
	c, err := Parse(data)
	require.NoError(t, err)
	require.NotNil(t, c.Sources.JSONL.Enabled)
	assert.True(t, *c.Sources.JSONL.Enabled)
	assert.Equal(t, "/t", c.Sources.JSONL.TranscriptDir)
	require.Len(t, c.Sinks, 1)
	assert.Equal(t, SinkJSONL, c.Sinks[0].Type)
	assert.Equal(t, "/x.jsonl", c.Sinks[0].Path)
}

func TestParsePayloadsSection(t *testing.T) {
	c, err := Parse([]byte("payloads:\n  mode: refs\n  max_bytes: 1024\n"))
	require.NoError(t, err)
	assert.Equal(t, PayloadModeRefs, c.Payloads.Mode)
	assert.Equal(t, 1024, c.Payloads.MaxBytes)
}

func TestParseEmptyIsZero(t *testing.T) {
	c, err := Parse(nil)
	require.NoError(t, err)
	assert.Equal(t, Config{}, c)
}

func TestParseUnknownKeyRejected(t *testing.T) {
	_, err := Parse([]byte("store:\n  nope: 1\n"))
	require.Error(t, err)
}

func TestParseBadDurationRejected(t *testing.T) {
	_, err := Parse([]byte("daemon:\n  reaper_window: notaduration\n"))
	require.Error(t, err)
}
