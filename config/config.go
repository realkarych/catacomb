package config

import (
	"errors"
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	BackendSQLite   = "sqlite"
	BackendMemory   = "memory"
	BackendPostgres = "postgres"

	SinkPostgres = "postgres"
	SinkNeo4j    = "neo4j"
	SinkOTLP     = "otlp"
	SinkJSONL    = "jsonl"

	DefaultSQLitePath = "~/.catacomb/catacomb.db"
	DefaultConfigPath = "~/.catacomb/config.yaml"
)

var (
	ErrNoStoreBackend        = errors.New("config: no store backend")
	ErrUnknownStoreBackend   = errors.New("config: unknown store backend")
	ErrMissingSQLitePath     = errors.New("config: sqlite backend requires store.sqlite.path")
	ErrBackendNotImplemented = errors.New("config: store backend not implemented")
	ErrUnknownSink           = errors.New("config: unknown sink type")
	ErrMissingSinkField      = errors.New("config: sink missing required field")
	ErrDuplicateSink         = errors.New("config: duplicate sink")
	ErrEmptyTranscriptDir    = errors.New("config: jsonl source enabled with empty transcript_dir")
)

type Duration time.Duration

func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	var s string
	if err := node.Decode(&s); err != nil {
		return fmt.Errorf("config.Duration: %w", err)
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("config.Duration: %w", err)
	}
	*d = Duration(v)
	return nil
}

type Config struct {
	Daemon  DaemonConfig  `yaml:"daemon"`
	Store   StoreConfig   `yaml:"store"`
	Sources SourcesConfig `yaml:"sources"`
	Sinks   []Sink        `yaml:"sinks"`
}

type DaemonConfig struct {
	Discovery          string   `yaml:"discovery,omitempty"`
	ReaperWindow       Duration `yaml:"reaper_window,omitempty"`
	MaxShards          int      `yaml:"max_shards,omitempty"`
	AllowPayloadAccess bool     `yaml:"allow_payload_access,omitempty"`
	AllowAnnotations   bool     `yaml:"allow_annotations,omitempty"`
}

type StoreConfig struct {
	Backend  string         `yaml:"backend,omitempty"`
	SQLite   SQLiteConfig   `yaml:"sqlite"`
	Postgres PostgresConfig `yaml:"postgres"`
}

type SQLiteConfig struct {
	Path string `yaml:"path,omitempty"`
}

type PostgresConfig struct {
	DSN string `yaml:"dsn,omitempty"`
}

type SourcesConfig struct {
	Hooks      SourceToggle `yaml:"hooks"`
	Otel       SourceToggle `yaml:"otel"`
	StreamJSON SourceToggle `yaml:"stream_json"`
	JSONL      JSONLSource  `yaml:"jsonl"`
}

type SourceToggle struct {
	Enabled *bool `yaml:"enabled,omitempty"`
}

type JSONLSource struct {
	Enabled       *bool    `yaml:"enabled,omitempty"`
	TranscriptDir string   `yaml:"transcript_dir,omitempty"`
	Exclude       []string `yaml:"exclude,omitempty"`
}

type Sink struct {
	Type     string `yaml:"type"`
	DSN      string `yaml:"dsn,omitempty"`
	URI      string `yaml:"uri,omitempty"`
	User     string `yaml:"user,omitempty"`
	Password string `yaml:"password,omitempty"`
	Endpoint string `yaml:"endpoint,omitempty"`
	Project  string `yaml:"project,omitempty"`
	Path     string `yaml:"path,omitempty"`
}

func boolPtr(b bool) *bool { return &b }

func Defaults() Config {
	return Config{
		Daemon: DaemonConfig{
			ReaperWindow: Duration(30 * time.Minute),
			MaxShards:    4096,
		},
		Store: StoreConfig{
			Backend: BackendSQLite,
			SQLite:  SQLiteConfig{Path: DefaultSQLitePath},
		},
		Sources: SourcesConfig{
			Hooks:      SourceToggle{Enabled: boolPtr(true)},
			Otel:       SourceToggle{Enabled: boolPtr(true)},
			StreamJSON: SourceToggle{Enabled: boolPtr(true)},
			JSONL:      JSONLSource{Enabled: boolPtr(false)},
		},
	}
}
