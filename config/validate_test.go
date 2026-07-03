package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func okStore() StoreConfig {
	return StoreConfig{Backend: BackendSQLite, SQLite: SQLiteConfig{Path: "/p.db"}}
}

func TestValidateOK(t *testing.T) {
	require.NoError(t, Validate(Config{Store: okStore()}))
	require.NoError(t, Validate(Config{Store: StoreConfig{Backend: BackendMemory}}))
}

func TestValidateStoreBackends(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want error
	}{
		{"empty backend", Config{Store: StoreConfig{}}, ErrNoStoreBackend},
		{"unknown backend", Config{Store: StoreConfig{Backend: "redis"}}, ErrUnknownStoreBackend},
		{"sqlite no path", Config{Store: StoreConfig{Backend: BackendSQLite}}, ErrMissingSQLitePath},
		{"postgres deferred", Config{Store: StoreConfig{Backend: BackendPostgres, Postgres: PostgresConfig{DSN: "x"}}}, ErrBackendNotImplemented},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.ErrorIs(t, Validate(tt.cfg), tt.want)
		})
	}
}

func TestValidateSources(t *testing.T) {
	bad := Config{Store: okStore(), Sources: SourcesConfig{JSONL: JSONLSource{Enabled: boolPtr(true)}}}
	assert.ErrorIs(t, Validate(bad), ErrEmptyTranscriptDir)
	good := Config{Store: okStore(), Sources: SourcesConfig{JSONL: JSONLSource{Enabled: boolPtr(true), TranscriptDir: "/t"}}}
	require.NoError(t, Validate(good))
	off := Config{Store: okStore(), Sources: SourcesConfig{JSONL: JSONLSource{Enabled: boolPtr(false)}}}
	require.NoError(t, Validate(off))
	nilEnabled := Config{Store: okStore(), Sources: SourcesConfig{JSONL: JSONLSource{}}}
	require.NoError(t, Validate(nilEnabled))
}

func TestValidatePayloadsModeAndMaxBytes(t *testing.T) {
	valid := Defaults()
	require.NoError(t, Validate(valid))

	bad := Defaults()
	bad.Payloads.Mode = "everything"
	err := Validate(bad)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnknownPayloadMode)

	neg := Defaults()
	neg.Payloads.MaxBytes = -1
	err = Validate(neg)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidPayloadMaxBytes)

	unset := Defaults()
	unset.Payloads = PayloadsConfig{}
	assert.NoError(t, Validate(unset))
}

func TestSinkKey(t *testing.T) {
	tests := []struct {
		name string
		sink Sink
		want string
	}{
		{"postgres", Sink{Type: SinkPostgres, DSN: "postgres://host/db"}, "postgres|postgres://host/db"},
		{"neo4j", Sink{Type: SinkNeo4j, URI: "bolt://localhost:7687"}, "neo4j|bolt://localhost:7687"},
		{"otlp", Sink{Type: SinkOTLP, Endpoint: "grpc://host:4317"}, "otlp|grpc://host:4317"},
		{"jsonl", Sink{Type: SinkJSONL, Path: "/var/log/out.jsonl"}, "jsonl|/var/log/out.jsonl"},
		{"unknown", Sink{Type: "kafka"}, "kafka"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, SinkKey(tt.sink))
		})
	}
}

func TestValidateSinks(t *testing.T) {
	tests := []struct {
		name  string
		sinks []Sink
		want  error
	}{
		{"postgres ok", []Sink{{Type: SinkPostgres, DSN: "d"}}, nil},
		{"neo4j ok", []Sink{{Type: SinkNeo4j, URI: "u", User: "n", Password: "p"}}, nil},
		{"otlp ok", []Sink{{Type: SinkOTLP, Endpoint: "e"}}, nil},
		{"jsonl ok", []Sink{{Type: SinkJSONL, Path: "/x"}}, nil},
		{"postgres missing dsn", []Sink{{Type: SinkPostgres}}, ErrMissingSinkField},
		{"neo4j missing field", []Sink{{Type: SinkNeo4j, URI: "u"}}, ErrMissingSinkField},
		{"otlp missing endpoint", []Sink{{Type: SinkOTLP}}, ErrMissingSinkField},
		{"jsonl missing path", []Sink{{Type: SinkJSONL}}, ErrMissingSinkField},
		{"unknown type", []Sink{{Type: "kafka"}}, ErrUnknownSink},
		{"duplicate", []Sink{{Type: SinkJSONL, Path: "/x"}, {Type: SinkJSONL, Path: "/x"}}, ErrDuplicateSink},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(Config{Store: okStore(), Sinks: tt.sinks})
			if tt.want == nil {
				require.NoError(t, err)
				return
			}
			assert.ErrorIs(t, err, tt.want)
		})
	}
}
