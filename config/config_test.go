package config

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestDefaults(t *testing.T) {
	c := Defaults()
	assert.Equal(t, BackendSQLite, c.Store.Backend)
	assert.Equal(t, DefaultSQLitePath, c.Store.SQLite.Path)
	assert.Equal(t, Duration(30*time.Minute), c.Daemon.ReaperWindow)
	assert.Equal(t, 4096, c.Daemon.MaxShards)
	assert.Equal(t, "", c.Daemon.Discovery)
	assert.False(t, c.Daemon.AllowPayloadAccess)
	assert.False(t, c.Daemon.AllowAnnotations)
	require.NotNil(t, c.Sources.Hooks.Enabled)
	assert.True(t, *c.Sources.Hooks.Enabled)
	require.NotNil(t, c.Sources.Otel.Enabled)
	assert.True(t, *c.Sources.Otel.Enabled)
	require.NotNil(t, c.Sources.StreamJSON.Enabled)
	assert.True(t, *c.Sources.StreamJSON.Enabled)
	require.NotNil(t, c.Sources.JSONL.Enabled)
	assert.False(t, *c.Sources.JSONL.Enabled)
	assert.Nil(t, c.Sinks)
}

func TestDefaultsTogglesAreDistinctPointers(t *testing.T) {
	c := Defaults()
	*c.Sources.Hooks.Enabled = false
	assert.True(t, *c.Sources.Otel.Enabled)
}

func TestDurationUnmarshalValid(t *testing.T) {
	var v struct {
		D Duration `yaml:"d"`
	}
	require.NoError(t, yaml.Unmarshal([]byte("d: 1h30m\n"), &v))
	assert.Equal(t, Duration(90*time.Minute), v.D)
}

func TestDurationUnmarshalNotScalar(t *testing.T) {
	var v struct {
		D Duration `yaml:"d"`
	}
	require.Error(t, yaml.Unmarshal([]byte("d: [1,2]\n"), &v))
}

func TestDurationUnmarshalBadValue(t *testing.T) {
	var v struct {
		D Duration `yaml:"d"`
	}
	require.Error(t, yaml.Unmarshal([]byte("d: nope\n"), &v))
}

func TestSentinelsDistinct(t *testing.T) {
	all := []error{
		ErrNoStoreBackend, ErrUnknownStoreBackend, ErrMissingSQLitePath,
		ErrBackendNotImplemented, ErrUnknownSink, ErrMissingSinkField,
		ErrDuplicateSink, ErrEmptyTranscriptDir,
	}
	for i := range all {
		for j := range all {
			if i != j {
				assert.False(t, errors.Is(all[i], all[j]))
			}
		}
	}
}
