# Config + Pluggable Store (PR1) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the primary store a selectable backend driven by a declarative config file: `store.backend: memory` runs live-only (nothing persisted, UI/SSE still served), `sqlite` is the default and moves to `~/.catacomb/catacomb.db`. Introduce a pure `config` package whose full schema (`Daemon`/`Store`/`Sources`/`Sinks`) is stable from day one, but where PR1 consumes only `Store` + `Daemon`; `Sources`/`Sinks` are parsed and validated but not yet wired.

**Architecture:** A new pure `config` package (no I/O: `Defaults`, `Parse`, `Merge`, `Validate`, `FromEnv`, `ExpandPath/ExpandPaths`, sentinels). A new `store/memory` package implementing the full `store.Store` interface in memory. A `store/storetest` package holding ONE shared contract suite exercised against both `sqlite` and `memory` to guarantee parity. A `store.Open(config.StoreConfig)` factory (`store` imports `config` and `store/memory`; no cycle because `config` imports neither). The `daemon` command loads config with precedence `defaults < file < env < flags` (flags applied only when `cobra Flag.Changed`), builds the store via `store.Open`, and `runDaemonWith` is refactored from an 18-parameter function into `(deps daemonDeps, params daemonParams)`. Exporter/transcript behavior is unchanged: those still map from the existing flags straight into `daemonParams`; only the store construction changes.

**Tech Stack:** Go 1.26, pure Go, no cgo. SQLite via `modernc.org/sqlite`. YAML via `gopkg.in/yaml.v3` (already in the module graph; promoted from indirect). cobra for the CLI. testify for tests. Module: `github.com/realkarych/catacomb`.

## Global Constraints

- Go 1.26; pure Go, NO cgo; SQLite is `modernc.org/sqlite` (never `mattn`).
- NO comments in Go code. NONE — not even doc comments. Only `//go:build`/`//go:embed`/`//go:generate` allowed (enforced by `internal/codepolicy`). Every code sample below contains zero comments.
- 100% test coverage, TDD-first. Failing test first, then minimal impl. Every branch covered. The threshold never goes down.
- `go test -race`; coverage via `make cover` (`-coverpkg=./...` aggregates cross-package execution).
- Consumer declares interfaces; no global mutable state; no `init()` side effects; no constructors with hidden I/O; wire deps from `main`. No `any` as a data type (`map[string]any` open bags OK).
- Errors: sentinels checked with `errors.Is`; wrap `fmt.Errorf("pkg.Op: %w", err)`; `log/slog` JSON; never log secrets.
- gofumpt + goimports (local prefix `github.com/realkarych/catacomb`).
- No `time.Sleep` in tests (forbidigo); use channels/`require.Eventually`/deadlines; table-driven by default; `testify/require` for fatal, `testify/assert` otherwise.
- Merge "set" rule (explicit & consistent): `Merge(base, override)` overlays each `override` field that is set. A string/int/`Duration` is set when non-zero; a slice is set when non-nil; a `*bool` is set when non-nil. The only `*bool` fields are the source toggles (`Sources.*.Enabled`), whose default is `true` and therefore need a tri-state. The daemon booleans (`AllowPayloadAccess`, `AllowAnnotations`) default to `false` so they are plain `bool` with "true overrides"; explicit-`false` precedence is delivered by the flag layer via `Flag.Changed`, not by `Merge`.
- Path expansion is a pure func `config.ExpandPath(path, home string, getenv func(string) string) string`; the cmd layer injects real `os.UserHomeDir()` + `os.Getenv`. The `config` package performs zero I/O.
- Default sqlite path: `~/.catacomb/catacomb.db`. Default config path: `~/.catacomb/config.yaml`. New env: `CATACOMB_CONFIG`; PR1 also reads `CATACOMB_DB` and `CATACOMB_DISCOVERY` in the env tier.
- All work happens in the worktree on branch `worktree-config-sinks-lifecycle`. Run from the worktree root: `/Users/karych/src/catacomb/.claude/worktrees/config-sinks-lifecycle`. No `--no-verify`. Squash PR. Commit message footer: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`.

---

## File Structure

- `config/config.go` (Create) — schema types (`Config`, `DaemonConfig`, `StoreConfig`, `SQLiteConfig`, `PostgresConfig`, `SourcesConfig`, `SourceToggle`, `JSONLSource`, `Sink`), backend/sink constants, `Duration` type + `UnmarshalYAML`, sentinels, `Defaults()`.
- `config/parse.go` (Create) — `Parse([]byte) (Config, error)` strict decode.
- `config/merge.go` (Create) — `Merge(base, override Config) Config` and helpers.
- `config/validate.go` (Create) — `Validate(Config) error` + invariants.
- `config/env.go` (Create) — `FromEnv(lookup func(string)(string,bool)) Config`, `ExpandPath`, `ExpandPaths`.
- `config/*_test.go` (Create) — table-driven tests for every function/branch.
- `store/memory/memory.go` (Create) — `Store` struct + `New()` implementing all 15 `store.Store` methods in memory.
- `store/storetest/storetest.go` (Create) — `RunStoreContract(t *testing.T, newStore func(*testing.T) store.Store)` shared parity suite.
- `store/contract_test.go` (Create, package `store_test`) — runs the contract suite against `OpenSQLite`.
- `store/memory/memory_test.go` (Create, package `memory_test`) — runs the contract suite against `memory.New`.
- `store/open.go` (Create) — `Open(config.StoreConfig) (Store, error)` factory.
- `store/open_test.go` (Create) — factory wiring tests.
- `cmd/catacomb/config_resolve.go` (Create) — `daemonFlags`, `resolveConfig`, `configFilePath`, `applyDaemonFlags`.
- `cmd/catacomb/config_resolve_test.go` (Create) — precedence tests with injected `readFile`/`lookupEnv`/`home`.
- `cmd/catacomb/daemon.go` (Modify) — `daemonDeps`, `daemonParams`, refactored `runDaemonWith`, `storeDBPath`, `resolveDiscovery`, `defaultDaemonDeps`, new `--config` flag, `--db` default move, RunE wiring.
- `cmd/catacomb/daemon_test.go` (Modify) — rewrite all `runDaemonWith` call sites to the struct API; add memory-backend + default-path tests.
- `cmd/catacomb/storepath.go` (Create) — `defaultDBPath()` for the batch CLI.
- `cmd/catacomb/storepath_test.go` (Create) — `defaultDBPath` happy/error path.
- `cmd/catacomb/{runs,inspect,snapshot,export,replay}.go` (Modify) — `--db` default → `defaultDBPath()`.
- `go.mod` / `go.sum` (Modify) — `gopkg.in/yaml.v3` promoted to a direct dependency by `go mod tidy`.

---

## Task 1: config package — types, sentinels, Duration, Defaults

**Files:**

- Create: `config/config.go`
- Create: `config/config_test.go`
- Modify: `go.mod`, `go.sum` (via `go mod tidy`)

**Interfaces:**

- Produces:
  - `type Config struct { Daemon DaemonConfig; Store StoreConfig; Sources SourcesConfig; Sinks []Sink }`
  - `type DaemonConfig struct { Discovery string; ReaperWindow Duration; MaxShards int; AllowPayloadAccess bool; AllowAnnotations bool }`
  - `type StoreConfig struct { Backend string; SQLite SQLiteConfig; Postgres PostgresConfig }`
  - `type SQLiteConfig struct { Path string }`, `type PostgresConfig struct { DSN string }`
  - `type SourcesConfig struct { Hooks SourceToggle; Otel SourceToggle; StreamJSON SourceToggle; JSONL JSONLSource }`
  - `type SourceToggle struct { Enabled *bool }`, `type JSONLSource struct { Enabled *bool; TranscriptDir string; Exclude []string }`
  - `type Sink struct { Type, DSN, URI, User, Password, Endpoint, Project, Path string }`
  - `type Duration time.Duration` with `func (d *Duration) UnmarshalYAML(node *yaml.Node) error`
  - Constants: `BackendSQLite="sqlite"`, `BackendMemory="memory"`, `BackendPostgres="postgres"`, `SinkPostgres="postgres"`, `SinkNeo4j="neo4j"`, `SinkOTLP="otlp"`, `SinkJSONL="jsonl"`, `DefaultSQLitePath="~/.catacomb/catacomb.db"`, `DefaultConfigPath="~/.catacomb/config.yaml"`.
  - Sentinels: `ErrNoStoreBackend`, `ErrUnknownStoreBackend`, `ErrMissingSQLitePath`, `ErrBackendNotImplemented`, `ErrUnknownSink`, `ErrMissingSinkField`, `ErrDuplicateSink`, `ErrEmptyTranscriptDir`.
  - `func Defaults() Config`
- Consumes: `gopkg.in/yaml.v3`, stdlib `time`, `errors`.

Steps:

- [ ] **Write failing test** `config/config_test.go`:

```go
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
```

- [ ] **Run** `go test ./config/` — expect FAIL (`undefined: Defaults`, `undefined: Duration`, etc.; package does not compile).
- [ ] **Minimal impl** `config/config.go`:

```go
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
```

- [ ] **Run** `go test ./config/` — expect PASS.
- [ ] **Run** `go mod tidy` then `go build ./...` — `gopkg.in/yaml.v3` becomes a direct require; expect clean build.
- [ ] **Commit:** `git add config/config.go config/config_test.go go.mod go.sum && git commit -m "feat(config): schema types, sentinels, Duration, Defaults"`

---

## Task 2: config.Parse (strict YAML decode)

**Files:**

- Create: `config/parse.go`
- Create: `config/parse_test.go`

**Interfaces:**

- Produces: `func Parse(data []byte) (Config, error)` — strict decode (`yaml.Decoder` + `KnownFields(true)`); empty input returns the zero `Config` with nil error; unknown keys and bad scalars are errors.
- Consumes: Task 1 types.

Steps:

- [ ] **Write failing test** `config/parse_test.go`:

```go
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
```

- [ ] **Run** `go test ./config/ -run TestParse` — expect FAIL (`undefined: Parse`).
- [ ] **Minimal impl** `config/parse.go`:

```go
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"gopkg.in/yaml.v3"
)

func Parse(data []byte) (Config, error) {
	var c Config
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		if errors.Is(err, io.EOF) {
			return Config{}, nil
		}
		return Config{}, fmt.Errorf("config.Parse: %w", err)
	}
	return c, nil
}
```

- [ ] **Run** `go test ./config/` — expect PASS.
- [ ] **Commit:** `git add config/parse.go config/parse_test.go && git commit -m "feat(config): strict YAML Parse"`

---

## Task 3: config.Merge (field-wise overlay)

**Files:**

- Create: `config/merge.go`
- Create: `config/merge_test.go`

**Interfaces:**

- Produces: `func Merge(base, override Config) Config` (override wins where set, per the Global Constraints "set" rule). Unexported helpers: `mergeDaemon`, `mergeStore`, `mergeSources`, `mergeToggle`, `mergeJSONL`.
- Consumes: Task 1 types.

Steps:

- [ ] **Write failing test** `config/merge_test.go`:

```go
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

func TestMergeSinksReplace(t *testing.T) {
	base := Config{Sinks: []Sink{{Type: SinkJSONL, Path: "/a"}}}
	got := Merge(base, Config{Sinks: []Sink{{Type: SinkOTLP, Endpoint: "e"}}})
	require.Len(t, got.Sinks, 1)
	assert.Equal(t, SinkOTLP, got.Sinks[0].Type)
	keep := Merge(base, Config{})
	require.Len(t, keep.Sinks, 1)
	assert.Equal(t, SinkJSONL, keep.Sinks[0].Type)
}
```

- [ ] **Run** `go test ./config/ -run TestMerge` — expect FAIL (`undefined: Merge`).
- [ ] **Minimal impl** `config/merge.go`:

```go
package config

func Merge(base, override Config) Config {
	out := base
	out.Daemon = mergeDaemon(base.Daemon, override.Daemon)
	out.Store = mergeStore(base.Store, override.Store)
	out.Sources = mergeSources(base.Sources, override.Sources)
	if override.Sinks != nil {
		out.Sinks = override.Sinks
	}
	return out
}

func mergeDaemon(base, o DaemonConfig) DaemonConfig {
	if o.Discovery != "" {
		base.Discovery = o.Discovery
	}
	if o.ReaperWindow != 0 {
		base.ReaperWindow = o.ReaperWindow
	}
	if o.MaxShards != 0 {
		base.MaxShards = o.MaxShards
	}
	if o.AllowPayloadAccess {
		base.AllowPayloadAccess = true
	}
	if o.AllowAnnotations {
		base.AllowAnnotations = true
	}
	return base
}

func mergeStore(base, o StoreConfig) StoreConfig {
	if o.Backend != "" {
		base.Backend = o.Backend
	}
	if o.SQLite.Path != "" {
		base.SQLite.Path = o.SQLite.Path
	}
	if o.Postgres.DSN != "" {
		base.Postgres.DSN = o.Postgres.DSN
	}
	return base
}

func mergeSources(base, o SourcesConfig) SourcesConfig {
	base.Hooks = mergeToggle(base.Hooks, o.Hooks)
	base.Otel = mergeToggle(base.Otel, o.Otel)
	base.StreamJSON = mergeToggle(base.StreamJSON, o.StreamJSON)
	base.JSONL = mergeJSONL(base.JSONL, o.JSONL)
	return base
}

func mergeToggle(base, o SourceToggle) SourceToggle {
	if o.Enabled != nil {
		base.Enabled = o.Enabled
	}
	return base
}

func mergeJSONL(base, o JSONLSource) JSONLSource {
	if o.Enabled != nil {
		base.Enabled = o.Enabled
	}
	if o.TranscriptDir != "" {
		base.TranscriptDir = o.TranscriptDir
	}
	if o.Exclude != nil {
		base.Exclude = o.Exclude
	}
	return base
}
```

- [ ] **Run** `go test ./config/` — expect PASS.
- [ ] **Commit:** `git add config/merge.go config/merge_test.go && git commit -m "feat(config): field-wise Merge overlay"`

---

## Task 4: config.Validate (store/sources/sinks invariants)

**Files:**

- Create: `config/validate.go`
- Create: `config/validate_test.go`

**Interfaces:**

- Produces: `func Validate(c Config) error`. Unexported: `validateStore`, `validateSources`, `validateSinks`, `sinkTarget`.
- Consumes: Task 1 types + sentinels.

Steps:

- [ ] **Write failing test** `config/validate_test.go`:

```go
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
```

- [ ] **Run** `go test ./config/ -run TestValidate` — expect FAIL (`undefined: Validate`).
- [ ] **Minimal impl** `config/validate.go`:

```go
package config

import "fmt"

func Validate(c Config) error {
	if err := validateStore(c.Store); err != nil {
		return err
	}
	if err := validateSources(c.Sources); err != nil {
		return err
	}
	return validateSinks(c.Sinks)
}

func validateStore(s StoreConfig) error {
	switch s.Backend {
	case BackendSQLite:
		if s.SQLite.Path == "" {
			return fmt.Errorf("config.Validate: %w", ErrMissingSQLitePath)
		}
		return nil
	case BackendMemory:
		return nil
	case BackendPostgres:
		return fmt.Errorf("config.Validate: %w", ErrBackendNotImplemented)
	case "":
		return fmt.Errorf("config.Validate: %w", ErrNoStoreBackend)
	default:
		return fmt.Errorf("config.Validate: %w", ErrUnknownStoreBackend)
	}
}

func validateSources(s SourcesConfig) error {
	if s.JSONL.Enabled != nil && *s.JSONL.Enabled && s.JSONL.TranscriptDir == "" {
		return fmt.Errorf("config.Validate: %w", ErrEmptyTranscriptDir)
	}
	return nil
}

func validateSinks(sinks []Sink) error {
	seen := map[string]struct{}{}
	for _, sk := range sinks {
		target, err := sinkTarget(sk)
		if err != nil {
			return err
		}
		key := sk.Type + "|" + target
		if _, dup := seen[key]; dup {
			return fmt.Errorf("config.Validate: %w", ErrDuplicateSink)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func sinkTarget(sk Sink) (string, error) {
	switch sk.Type {
	case SinkPostgres:
		if sk.DSN == "" {
			return "", fmt.Errorf("config.Validate: %w", ErrMissingSinkField)
		}
		return sk.DSN, nil
	case SinkNeo4j:
		if sk.URI == "" || sk.User == "" || sk.Password == "" {
			return "", fmt.Errorf("config.Validate: %w", ErrMissingSinkField)
		}
		return sk.URI, nil
	case SinkOTLP:
		if sk.Endpoint == "" {
			return "", fmt.Errorf("config.Validate: %w", ErrMissingSinkField)
		}
		return sk.Endpoint, nil
	case SinkJSONL:
		if sk.Path == "" {
			return "", fmt.Errorf("config.Validate: %w", ErrMissingSinkField)
		}
		return sk.Path, nil
	default:
		return "", fmt.Errorf("config.Validate: %w", ErrUnknownSink)
	}
}
```

- [ ] **Run** `go test ./config/` — expect PASS.
- [ ] **Commit:** `git add config/validate.go config/validate_test.go && git commit -m "feat(config): Validate store/sources/sinks invariants"`

---

## Task 5: config.FromEnv + ExpandPath/ExpandPaths

**Files:**

- Create: `config/env.go`
- Create: `config/env_test.go`

**Interfaces:**

- Produces:
  - `func FromEnv(lookup func(string) (string, bool)) Config` — fixed env set: `CATACOMB_DB`→`Store.SQLite.Path`, `CATACOMB_DISCOVERY`→`Daemon.Discovery`.
  - `func ExpandPath(path, home string, getenv func(string) string) string` — `~`/`~/...` → home, then `$VAR`/`${VAR}` via `os.Expand(_, getenv)`.
  - `func ExpandPaths(c Config, home string, getenv func(string) string) Config` — expands `Store.SQLite.Path`, `Daemon.Discovery`, `Sources.JSONL.TranscriptDir`, and each `Sinks[i].Path`.
- Consumes: Task 1 types.

Steps:

- [ ] **Write failing test** `config/env_test.go`:

```go
package config

import (
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
		{"tilde slash", "~/.catacomb/x.db", "/home/u/.catacomb/x.db"},
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
	assert.Equal(t, "/home/u/.catacomb/x.db", got.Store.SQLite.Path)
	assert.Equal(t, "/home/u/run/d.json", got.Daemon.Discovery)
	assert.Equal(t, "/home/u/proj", got.Sources.JSONL.TranscriptDir)
	assert.Equal(t, "/home/u/out.jsonl", got.Sinks[0].Path)
}
```

- [ ] **Run** `go test ./config/ -run 'TestFromEnv|TestExpand'` — expect FAIL (`undefined: FromEnv`).
- [ ] **Minimal impl** `config/env.go`:

```go
package config

import (
	"os"
	"path/filepath"
	"strings"
)

func FromEnv(lookup func(string) (string, bool)) Config {
	var c Config
	if v, ok := lookup("CATACOMB_DB"); ok {
		c.Store.SQLite.Path = v
	}
	if v, ok := lookup("CATACOMB_DISCOVERY"); ok {
		c.Daemon.Discovery = v
	}
	return c
}

func ExpandPath(path, home string, getenv func(string) string) string {
	if path == "" {
		return ""
	}
	switch {
	case path == "~":
		path = home
	case strings.HasPrefix(path, "~/"):
		path = filepath.Join(home, path[2:])
	}
	return os.Expand(path, getenv)
}

func ExpandPaths(c Config, home string, getenv func(string) string) Config {
	c.Store.SQLite.Path = ExpandPath(c.Store.SQLite.Path, home, getenv)
	c.Daemon.Discovery = ExpandPath(c.Daemon.Discovery, home, getenv)
	c.Sources.JSONL.TranscriptDir = ExpandPath(c.Sources.JSONL.TranscriptDir, home, getenv)
	for i := range c.Sinks {
		c.Sinks[i].Path = ExpandPath(c.Sinks[i].Path, home, getenv)
	}
	return c
}
```

- [ ] **Run** `go test ./config/` — expect PASS. (`os.Expand` and `filepath.Join` are pure string transforms — no I/O — so the package stays pure.)
- [ ] **Run** `make cover` scoped to config: `go test -race -coverpkg=./config/... -coverprofile=cover.out ./config/ && go tool cover -func=cover.out` — expect every `config/*` line covered.
- [ ] **Commit:** `git add config/env.go config/env_test.go && git commit -m "feat(config): FromEnv + ExpandPath/ExpandPaths"`

---

## Task 6: store/memory — in-memory store.Store

**Files:**

- Create: `store/memory/memory.go`

**Interfaces:**

- Produces: `package memory`; `type Store struct {...}`; `func New() *Store`; methods matching `store.Store` exactly (structural satisfaction — `memory` does NOT import `store`, avoiding a cycle):
  - `Persist([]model.Observation, []*model.Node, []*model.Edge) error`
  - `AppendDeltas(model.Observation, []cdc.GraphDelta) error`
  - `MaxSeq() (uint64, error)`
  - `ObservationsSince(uint64) ([]model.Observation, error)`
  - `ObservationsForExecution(string) ([]model.Observation, error)`
  - `UpsertRun(model.Run) error`, `ListOpenRuns() ([]model.Run, error)`, `Runs() ([]model.Run, error)`
  - `Quarantine(model.QuarantineRecord) error`, `QuarantineCount() (int64, error)`
  - `UpsertTailCursor(model.TailCursor) error`, `LoadTailCursors() ([]model.TailCursor, error)`
  - `UpsertAnnotation(model.Annotation) error`, `AnnotationsForExecution(string) ([]model.Annotation, error)`, `MoveAnnotations(string, string, string) error`
  - `Close() error`
- Consumes: `github.com/realkarych/catacomb/model`, `github.com/realkarych/catacomb/cdc`, stdlib `cmp`, `slices`, `sync`.

Notes baked into the implementation (parity with `store/sqlite.go`):

- Observations are appended in arrival order; readers sort by `Seq` (matches sqlite `ORDER BY seq`). `ObservationsSince(seq)` returns `Seq > seq`.
- Annotation LWW key is `(ExecutionID, SourceKey, Owner, Key)`; write applies when `new.WriteSeq >= existing.WriteSeq` (mirrors the sqlite `excluded.write_seq>=annotations.write_seq` clause — equal seq overwrites).
- `AnnotationsForExecution` orders by `(SourceKey, Owner, Key)`; `Runs`/`ListOpenRuns` order by `ID`; `LoadTailCursors` orders by `Path`.
- `ListOpenRuns` filters `Status == model.StatusRunning`.
- Nodes/edges are stored (write-only via this interface; no read method exists) so the type is a complete drop-in; `applyDelta` mirrors the sqlite switch including nil-guard and run-kind/default no-ops.
- Memory is non-durable and append-only; it does NOT enforce obs-id uniqueness or move-target conflicts (backend-specific error/rollback paths stay in `store/sqlite_test.go`). The shared contract suite (Task 7) asserts only the semantics both backends share.

Steps:

- [ ] **Write failing test:** memory is covered entirely by the shared contract suite (Task 7). For Task 6, add a temporary smoke test `store/memory/smoke_test.go` so the package compiles and the basic round-trip is proven before the suite lands; it is replaced by `memory_test.go` in Task 7.

```go
package memory

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
)

func TestNewRoundTrip(t *testing.T) {
	s := New()
	require.NoError(t, s.Persist([]model.Observation{{ObsID: "o1", ExecutionID: "e", Seq: 1}}, nil, nil))
	got, err := s.ObservationsSince(0)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, uint64(1), got[0].Seq)
	require.NoError(t, s.Close())
}
```

- [ ] **Run** `go test ./store/memory/` — expect FAIL (`undefined: New`).
- [ ] **Minimal impl** `store/memory/memory.go`:

```go
package memory

import (
	"cmp"
	"slices"
	"sync"

	"github.com/realkarych/catacomb/cdc"
	"github.com/realkarych/catacomb/model"
)

type annKey struct {
	exec   string
	source string
	owner  string
	key    string
}

type Store struct {
	mu          sync.Mutex
	obs         []model.Observation
	nodes       map[string]*model.Node
	edges       map[string]*model.Edge
	runs        map[string]model.Run
	annotations map[annKey]model.Annotation
	cursors     map[string]model.TailCursor
	quarantine  int64
}

func New() *Store {
	return &Store{
		nodes:       map[string]*model.Node{},
		edges:       map[string]*model.Edge{},
		runs:        map[string]model.Run{},
		annotations: map[annKey]model.Annotation{},
		cursors:     map[string]model.TailCursor{},
	}
}

func (s *Store) Persist(obs []model.Observation, nodes []*model.Node, edges []*model.Edge) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.obs = append(s.obs, obs...)
	for _, n := range nodes {
		s.nodes[n.ID] = n
	}
	for _, e := range edges {
		s.edges[e.ID] = e
	}
	return nil
}

func (s *Store) AppendDeltas(o model.Observation, deltas []cdc.GraphDelta) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.obs = append(s.obs, o)
	for _, d := range deltas {
		s.applyDelta(d)
	}
	return nil
}

func (s *Store) applyDelta(d cdc.GraphDelta) {
	switch d.Kind {
	case cdc.DeltaNodeUpsert, cdc.DeltaNodeStatus:
		if d.Node != nil {
			s.nodes[d.Node.ID] = d.Node
		}
	case cdc.DeltaNodeMerge:
		if d.Node != nil {
			if d.OldID != "" {
				delete(s.nodes, d.OldID)
			}
			s.nodes[d.Node.ID] = d.Node
		}
	case cdc.DeltaEdgeUpsert:
		if d.Edge != nil {
			s.edges[d.Edge.ID] = d.Edge
		}
	case cdc.DeltaEdgeDelete:
		if d.Edge != nil {
			delete(s.edges, d.Edge.ID)
		}
	}
}

func (s *Store) MaxSeq() (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var maxSeq uint64
	for _, o := range s.obs {
		if o.Seq > maxSeq {
			maxSeq = o.Seq
		}
	}
	return maxSeq, nil
}

func (s *Store) ObservationsSince(seq uint64) ([]model.Observation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []model.Observation
	for _, o := range s.obs {
		if o.Seq > seq {
			out = append(out, o)
		}
	}
	sortObservations(out)
	return out, nil
}

func (s *Store) ObservationsForExecution(executionID string) ([]model.Observation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []model.Observation
	for _, o := range s.obs {
		if o.ExecutionID == executionID {
			out = append(out, o)
		}
	}
	sortObservations(out)
	return out, nil
}

func sortObservations(o []model.Observation) {
	slices.SortFunc(o, func(a, b model.Observation) int { return cmp.Compare(a.Seq, b.Seq) })
}

func (s *Store) UpsertRun(r model.Run) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs[r.ID] = r
	return nil
}

func (s *Store) ListOpenRuns() ([]model.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []model.Run
	for _, r := range s.runs {
		if r.Status == model.StatusRunning {
			out = append(out, r)
		}
	}
	sortRuns(out)
	return out, nil
}

func (s *Store) Runs() ([]model.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]model.Run, 0, len(s.runs))
	for _, r := range s.runs {
		out = append(out, r)
	}
	sortRuns(out)
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func sortRuns(r []model.Run) {
	slices.SortFunc(r, func(a, b model.Run) int { return cmp.Compare(a.ID, b.ID) })
}

func (s *Store) Quarantine(model.QuarantineRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.quarantine++
	return nil
}

func (s *Store) QuarantineCount() (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.quarantine, nil
}

func (s *Store) UpsertTailCursor(c model.TailCursor) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cursors[c.Path] = c
	return nil
}

func (s *Store) LoadTailCursors() ([]model.TailCursor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]model.TailCursor, 0, len(s.cursors))
	for _, c := range s.cursors {
		out = append(out, c)
	}
	slices.SortFunc(out, func(a, b model.TailCursor) int { return cmp.Compare(a.Path, b.Path) })
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func (s *Store) UpsertAnnotation(a model.Annotation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := annKey{exec: a.ExecutionID, source: a.SourceKey, owner: a.Owner, key: a.Key}
	existing, ok := s.annotations[k]
	if !ok || a.WriteSeq >= existing.WriteSeq {
		s.annotations[k] = a
	}
	return nil
}

func (s *Store) AnnotationsForExecution(executionID string) ([]model.Annotation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []model.Annotation
	for k, a := range s.annotations {
		if k.exec == executionID {
			out = append(out, a)
		}
	}
	slices.SortFunc(out, func(a, b model.Annotation) int {
		return cmp.Or(cmp.Compare(a.SourceKey, b.SourceKey), cmp.Compare(a.Owner, b.Owner), cmp.Compare(a.Key, b.Key))
	})
	return out, nil
}

func (s *Store) MoveAnnotations(executionID, fromKey, toKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	moved := map[annKey]model.Annotation{}
	for k, a := range s.annotations {
		if k.exec == executionID && k.source == fromKey {
			a.SourceKey = toKey
			moved[annKey{exec: executionID, source: toKey, owner: k.owner, key: k.key}] = a
			delete(s.annotations, k)
		}
	}
	for k, a := range moved {
		s.annotations[k] = a
	}
	return nil
}

func (s *Store) Close() error { return nil }
```

- [ ] **Run** `go test ./store/memory/` — expect PASS (smoke).
- [ ] **Commit:** `git add store/memory/memory.go store/memory/smoke_test.go && git commit -m "feat(store/memory): in-memory store.Store implementation"`

---

## Task 7: store/storetest — shared contract suite + sqlite & memory parity

**Files:**

- Create: `store/storetest/storetest.go`
- Create: `store/contract_test.go` (package `store_test`)
- Replace: `store/memory/smoke_test.go` → `store/memory/memory_test.go` (package `memory_test`)

**Interfaces:**

- Produces: `package storetest`; `func RunStoreContract(t *testing.T, newStore func(t *testing.T) store.Store)`. Imports `store` (for the interface), `model`, `cdc`. No cycle: `store` does not import `storetest`.
- Consumes: `store.Store`, `model`, `cdc`.

Notes: keep the suite branch-free (linear subtests that all run for every backend) so `-coverpkg=./...` records 100% of `storetest.go` and 100% of `store/memory/memory.go` on each backend run. Use distinct `Seq`/keys so ordering is deterministic.

Steps:

- [ ] **Write the suite** `store/storetest/storetest.go`:

```go
package storetest

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/cdc"
	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/store"
)

func RunStoreContract(t *testing.T, newStore func(t *testing.T) store.Store) {
	t.Helper()
	t.Run("observations seq and exec filters", func(t *testing.T) {
		s := newStore(t)
		max0, err := s.MaxSeq()
		require.NoError(t, err)
		assert.Equal(t, uint64(0), max0)
		require.NoError(t, s.Persist([]model.Observation{
			{ObsID: "o1", RunID: "r1", ExecutionID: "e1", Seq: 1, Kind: "a"},
			{ObsID: "o3", RunID: "r1", ExecutionID: "e1", Seq: 3, Kind: "c"},
		}, nil, nil))
		require.NoError(t, s.AppendDeltas(model.Observation{ObsID: "o2", RunID: "r1", ExecutionID: "e2", Seq: 2, Kind: "b"}, nil))
		maxN, err := s.MaxSeq()
		require.NoError(t, err)
		assert.Equal(t, uint64(3), maxN)
		since, err := s.ObservationsSince(1)
		require.NoError(t, err)
		require.Len(t, since, 2)
		assert.Equal(t, uint64(2), since[0].Seq)
		assert.Equal(t, uint64(3), since[1].Seq)
		exec, err := s.ObservationsForExecution("e1")
		require.NoError(t, err)
		require.Len(t, exec, 2)
		assert.Equal(t, uint64(1), exec[0].Seq)
		assert.Equal(t, uint64(3), exec[1].Seq)
	})

	t.Run("delta kinds", func(t *testing.T) {
		s := newStore(t)
		require.NoError(t, s.AppendDeltas(model.Observation{ObsID: "d1", RunID: "r1", ExecutionID: "e1", Seq: 1}, []cdc.GraphDelta{
			{Kind: cdc.DeltaNodeUpsert, Node: &model.Node{ID: "n1", RunID: "r1", Type: model.NodeSession}},
			{Kind: cdc.DeltaNodeStatus, Node: &model.Node{ID: "n1", RunID: "r1", Type: model.NodeSession, Status: model.StatusOK}},
			{Kind: cdc.DeltaEdgeUpsert, Edge: &model.Edge{ID: "x1", RunID: "r1", Src: "n1", Dst: "n2"}},
			{Kind: cdc.DeltaEdgeDelete, Edge: &model.Edge{ID: "x1"}},
			{Kind: cdc.DeltaNodeMerge, OldID: "n1", NewID: "n2", Node: &model.Node{ID: "n2", RunID: "r1", Type: model.NodeSession}},
		}))
		require.NoError(t, s.AppendDeltas(model.Observation{ObsID: "d2", RunID: "r1", ExecutionID: "e1", Seq: 2}, []cdc.GraphDelta{
			{Kind: cdc.DeltaNodeUpsert},
			{Kind: cdc.DeltaNodeMerge},
			{Kind: cdc.DeltaEdgeUpsert},
			{Kind: cdc.DeltaEdgeDelete},
			{Kind: cdc.DeltaNodeMerge, Node: &model.Node{ID: "n3", RunID: "r1", Type: model.NodeToolCall}},
			{Kind: cdc.DeltaRunStarted, RunID: "r1"},
		}))
		obs, err := s.ObservationsSince(0)
		require.NoError(t, err)
		assert.Len(t, obs, 2)
	})

	t.Run("runs upsert filter order", func(t *testing.T) {
		s := newStore(t)
		empty, err := s.Runs()
		require.NoError(t, err)
		assert.Empty(t, empty)
		require.NoError(t, s.UpsertRun(model.Run{ID: "b", Status: model.StatusRunning, LastSeq: 1}))
		require.NoError(t, s.UpsertRun(model.Run{ID: "a", Status: model.StatusOK}))
		require.NoError(t, s.UpsertRun(model.Run{ID: "b", Status: model.StatusOK, LastSeq: 9}))
		all, err := s.Runs()
		require.NoError(t, err)
		require.Len(t, all, 2)
		assert.Equal(t, "a", all[0].ID)
		assert.Equal(t, model.StatusOK, all[1].Status)
		assert.Equal(t, uint64(9), all[1].LastSeq)
		require.NoError(t, s.UpsertRun(model.Run{ID: "c", Status: model.StatusRunning}))
		open, err := s.ListOpenRuns()
		require.NoError(t, err)
		require.Len(t, open, 1)
		assert.Equal(t, "c", open[0].ID)
	})

	t.Run("quarantine counter", func(t *testing.T) {
		s := newStore(t)
		require.NoError(t, s.Quarantine(model.QuarantineRecord{HookType: "PreToolUse"}))
		require.NoError(t, s.Quarantine(model.QuarantineRecord{HookType: "Stop"}))
		n, err := s.QuarantineCount()
		require.NoError(t, err)
		assert.Equal(t, int64(2), n)
	})

	t.Run("tail cursors upsert order", func(t *testing.T) {
		s := newStore(t)
		none, err := s.LoadTailCursors()
		require.NoError(t, err)
		assert.Empty(t, none)
		require.NoError(t, s.UpsertTailCursor(model.TailCursor{Path: "/b", Offset: 1}))
		require.NoError(t, s.UpsertTailCursor(model.TailCursor{Path: "/a", Offset: 2}))
		require.NoError(t, s.UpsertTailCursor(model.TailCursor{Path: "/a", Offset: 5, Fingerprint: "f"}))
		got, err := s.LoadTailCursors()
		require.NoError(t, err)
		require.Len(t, got, 2)
		assert.Equal(t, "/a", got[0].Path)
		assert.Equal(t, int64(5), got[0].Offset)
		assert.Equal(t, "f", got[0].Fingerprint)
		assert.Equal(t, "/b", got[1].Path)
	})

	t.Run("annotations lww order step move", func(t *testing.T) {
		s := newStore(t)
		require.NoError(t, s.UpsertAnnotation(model.Annotation{ExecutionID: "e1", SourceKey: "k", StepKey: "st", Owner: "eval", Key: "score", Value: json.RawMessage(`5`), WriteSeq: 5}))
		require.NoError(t, s.UpsertAnnotation(model.Annotation{ExecutionID: "e1", SourceKey: "k", Owner: "eval", Key: "score", Value: json.RawMessage(`9`), WriteSeq: 7}))
		require.NoError(t, s.UpsertAnnotation(model.Annotation{ExecutionID: "e1", SourceKey: "k", Owner: "eval", Key: "score", Value: json.RawMessage(`1`), WriteSeq: 4}))
		require.NoError(t, s.UpsertAnnotation(model.Annotation{ExecutionID: "e1", SourceKey: "k", Owner: "aaa", Key: "note", Value: json.RawMessage(`2`), WriteSeq: 2}))
		anns, err := s.AnnotationsForExecution("e1")
		require.NoError(t, err)
		require.Len(t, anns, 2)
		assert.Equal(t, "aaa", anns[0].Owner)
		assert.Equal(t, "eval", anns[1].Owner)
		assert.Equal(t, "st", anns[1].StepKey)
		assert.Equal(t, "9", string(anns[1].Value))
		require.NoError(t, s.MoveAnnotations("e1", "k", "k2"))
		moved, err := s.AnnotationsForExecution("e1")
		require.NoError(t, err)
		require.Len(t, moved, 2)
		for _, a := range moved {
			assert.Equal(t, "k2", a.SourceKey)
		}
	})

	t.Run("close", func(t *testing.T) {
		require.NoError(t, newStore(t).Close())
	})
}
```

- [ ] **Wire sqlite** `store/contract_test.go`:

```go
package store_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/store"
	"github.com/realkarych/catacomb/store/storetest"
)

func TestSQLiteContract(t *testing.T) {
	storetest.RunStoreContract(t, func(t *testing.T) store.Store {
		s, err := store.OpenSQLite(filepath.Join(t.TempDir(), "g.db"))
		require.NoError(t, err)
		t.Cleanup(func() { _ = s.Close() })
		return s
	})
}
```

- [ ] **Wire memory** `store/memory/memory_test.go` (delete `smoke_test.go`):

```go
package memory_test

import (
	"testing"

	"github.com/realkarych/catacomb/store"
	"github.com/realkarych/catacomb/store/memory"
	"github.com/realkarych/catacomb/store/storetest"
)

func TestMemoryContract(t *testing.T) {
	storetest.RunStoreContract(t, func(t *testing.T) store.Store {
		return memory.New()
	})
}
```

- [ ] **Run** `go test -race ./store/...` — expect PASS for both `TestSQLiteContract` and `TestMemoryContract`. (`return memory.New()` in a `func(*testing.T) store.Store` is the compile-time proof that `*memory.Store` satisfies `store.Store`.)
- [ ] **Run** `go test -race -coverpkg=./... -coverprofile=cover.out ./store/... && go tool cover -func=cover.out | grep -E 'store/memory|store/storetest'` — expect 100% for `store/memory/memory.go` and `store/storetest/storetest.go`.
- [ ] **Commit:** `git add store/storetest/storetest.go store/contract_test.go store/memory/memory_test.go && git rm store/memory/smoke_test.go && git commit -m "test(store): shared contract suite, sqlite+memory parity"`

---

## Task 8: store.Open factory

**Files:**

- Create: `store/open.go`
- Create: `store/open_test.go` (package `store_test`)

**Interfaces:**

- Produces: `func Open(cfg config.StoreConfig) (Store, error)` in package `store`. `sqlite`→`OpenSQLite(cfg.SQLite.Path)`; `memory`→`memory.New()`; `postgres`→`config.ErrBackendNotImplemented`; unknown→`config.ErrUnknownStoreBackend`. `store` imports `config` and `store/memory` (no cycle: neither imports `store`).
- Consumes: `config.StoreConfig`, `config.Backend*` constants, sentinels, `store/memory`.

Steps:

- [ ] **Write failing test** `store/open_test.go`:

```go
package store_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/config"
	"github.com/realkarych/catacomb/store"
)

func TestOpenSQLite(t *testing.T) {
	s, err := store.Open(config.StoreConfig{Backend: config.BackendSQLite, SQLite: config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "g.db")}})
	require.NoError(t, err)
	require.NotNil(t, s)
	t.Cleanup(func() { _ = s.Close() })
	runs, err := s.Runs()
	require.NoError(t, err)
	assert.Empty(t, runs)
}

func TestOpenSQLiteError(t *testing.T) {
	_, err := store.Open(config.StoreConfig{Backend: config.BackendSQLite, SQLite: config.SQLiteConfig{Path: "/nonexistent/dir/g.db"}})
	require.Error(t, err)
}

func TestOpenMemory(t *testing.T) {
	s, err := store.Open(config.StoreConfig{Backend: config.BackendMemory})
	require.NoError(t, err)
	require.NotNil(t, s)
	max, err := s.MaxSeq()
	require.NoError(t, err)
	assert.Equal(t, uint64(0), max)
}

func TestOpenPostgresNotImplemented(t *testing.T) {
	_, err := store.Open(config.StoreConfig{Backend: config.BackendPostgres, Postgres: config.PostgresConfig{DSN: "x"}})
	assert.ErrorIs(t, err, config.ErrBackendNotImplemented)
}

func TestOpenUnknownBackend(t *testing.T) {
	_, err := store.Open(config.StoreConfig{Backend: "redis"})
	assert.ErrorIs(t, err, config.ErrUnknownStoreBackend)
}
```

- [ ] **Run** `go test ./store/ -run TestOpen` — expect FAIL (`undefined: store.Open`).
- [ ] **Minimal impl** `store/open.go`:

```go
package store

import (
	"fmt"

	"github.com/realkarych/catacomb/config"
	"github.com/realkarych/catacomb/store/memory"
)

func Open(cfg config.StoreConfig) (Store, error) {
	switch cfg.Backend {
	case config.BackendSQLite:
		return OpenSQLite(cfg.SQLite.Path)
	case config.BackendMemory:
		return memory.New(), nil
	case config.BackendPostgres:
		return nil, fmt.Errorf("store.Open: %w", config.ErrBackendNotImplemented)
	default:
		return nil, fmt.Errorf("store.Open: %w", config.ErrUnknownStoreBackend)
	}
}
```

- [ ] **Run** `go test ./store/...` — expect PASS.
- [ ] **Commit:** `git add store/open.go store/open_test.go && git commit -m "feat(store): Open factory over config.StoreConfig"`

---

## Task 9: daemon cmd — resolveConfig precedence pipeline

**Files:**

- Create: `cmd/catacomb/config_resolve.go`
- Create: `cmd/catacomb/config_resolve_test.go`

**Interfaces:**

- Produces (package `main`):
  - `type daemonFlags struct { configPath string; configPathSet bool; dbPath string; dbPathSet bool; discoveryPath string; discoveryPathSet bool; reaperWindow time.Duration; reaperWindowSet bool; maxShards int; maxShardsSet bool; allowPayloadAccess bool; allowPayloadAccessSet bool; allowAnnotations bool; allowAnnotationsSet bool }`
  - `func resolveConfig(f daemonFlags, readFile func(string) ([]byte, error), lookupEnv func(string) (string, bool), home string) (config.Config, error)`
  - `func configFilePath(f daemonFlags, lookupEnv func(string) (string, bool), home string) string`
  - `func applyDaemonFlags(cfg config.Config, f daemonFlags) config.Config`
  - Precedence: `defaults < file < env < flags`; expand paths; `Validate`. Missing file (`os.ErrNotExist`) → defaults; other read error → wrapped error.
- Consumes: `config` package; injected `readFile`/`lookupEnv`/`home` keep the function pure and table-testable.

Steps:

- [ ] **Write failing test** `cmd/catacomb/config_resolve_test.go`:

```go
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
	read := func(string) ([]byte, error) { return []byte("store:\n  backend: postgres\n  postgres:\n    dsn: x\n"), nil }
	_, err := resolveConfig(daemonFlags{}, read, envLookup(nil), "/home/u")
	assert.ErrorIs(t, err, config.ErrBackendNotImplemented)
}

func TestConfigFilePathPrecedence(t *testing.T) {
	assert.Equal(t, "/flag.yaml", configFilePath(daemonFlags{configPath: "/flag.yaml", configPathSet: true}, envLookup(map[string]string{"CATACOMB_CONFIG": "/env.yaml"}), "/home/u"))
	assert.Equal(t, "/env.yaml", configFilePath(daemonFlags{}, envLookup(map[string]string{"CATACOMB_CONFIG": "/env.yaml"}), "/home/u"))
	assert.Equal(t, "/home/u/.catacomb/config.yaml", configFilePath(daemonFlags{}, envLookup(nil), "/home/u"))
}
```

- [ ] **Run** `go test ./cmd/catacomb/ -run 'TestResolveConfig|TestConfigFilePath'` — expect FAIL (`undefined: resolveConfig`).
- [ ] **Minimal impl** `cmd/catacomb/config_resolve.go`:

```go
package main

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/realkarych/catacomb/config"
)

type daemonFlags struct {
	configPath            string
	configPathSet         bool
	dbPath                string
	dbPathSet             bool
	discoveryPath         string
	discoveryPathSet      bool
	reaperWindow          time.Duration
	reaperWindowSet       bool
	maxShards             int
	maxShardsSet          bool
	allowPayloadAccess    bool
	allowPayloadAccessSet bool
	allowAnnotations      bool
	allowAnnotationsSet   bool
}

func configFilePath(f daemonFlags, lookupEnv func(string) (string, bool), home string) string {
	getenv := func(k string) string { v, _ := lookupEnv(k); return v }
	if f.configPathSet {
		return config.ExpandPath(f.configPath, home, getenv)
	}
	if v, ok := lookupEnv("CATACOMB_CONFIG"); ok {
		return config.ExpandPath(v, home, getenv)
	}
	return config.ExpandPath(config.DefaultConfigPath, home, getenv)
}

func resolveConfig(f daemonFlags, readFile func(string) ([]byte, error), lookupEnv func(string) (string, bool), home string) (config.Config, error) {
	getenv := func(k string) string { v, _ := lookupEnv(k); return v }
	cfg := config.Defaults()
	data, err := readFile(configFilePath(f, lookupEnv, home))
	switch {
	case err == nil:
		fileCfg, perr := config.Parse(data)
		if perr != nil {
			return config.Config{}, fmt.Errorf("daemon.resolveConfig: %w", perr)
		}
		cfg = config.Merge(cfg, fileCfg)
	case errors.Is(err, os.ErrNotExist):
	default:
		return config.Config{}, fmt.Errorf("daemon.resolveConfig read: %w", err)
	}
	cfg = config.Merge(cfg, config.FromEnv(lookupEnv))
	cfg = applyDaemonFlags(cfg, f)
	cfg = config.ExpandPaths(cfg, home, getenv)
	if err := config.Validate(cfg); err != nil {
		return config.Config{}, fmt.Errorf("daemon.resolveConfig: %w", err)
	}
	return cfg, nil
}

func applyDaemonFlags(cfg config.Config, f daemonFlags) config.Config {
	if f.dbPathSet {
		cfg.Store.SQLite.Path = f.dbPath
	}
	if f.discoveryPathSet {
		cfg.Daemon.Discovery = f.discoveryPath
	}
	if f.reaperWindowSet {
		cfg.Daemon.ReaperWindow = config.Duration(f.reaperWindow)
	}
	if f.maxShardsSet {
		cfg.Daemon.MaxShards = f.maxShards
	}
	if f.allowPayloadAccessSet {
		cfg.Daemon.AllowPayloadAccess = f.allowPayloadAccess
	}
	if f.allowAnnotationsSet {
		cfg.Daemon.AllowAnnotations = f.allowAnnotations
	}
	return cfg
}
```

- [ ] **Run** `go test ./cmd/catacomb/ -run 'TestResolveConfig|TestConfigFilePath'` — expect PASS. (`resolveConfig`/`configFilePath`/`applyDaemonFlags` are referenced by these tests, so the `unused` linter treats them as used until Task 10 wires `RunE`.)
- [ ] **Commit:** `git add cmd/catacomb/config_resolve.go cmd/catacomb/config_resolve_test.go && git commit -m "feat(cmd): resolveConfig precedence pipeline (defaults<file<env<flags)"`

---

## Task 10: daemon cmd — runDaemonWith struct refactor + RunE wiring

**Files:**

- Modify: `cmd/catacomb/daemon.go` (the whole file: `newDaemonCmd` flags + RunE; replace `runDaemonWith` at lines 67-145)
- Modify: `cmd/catacomb/daemon_test.go` (rewrite every `runDaemonWith` call site; update full-command tests; add memory + default-path tests)

**Interfaces:**

- Produces (package `main`):
  - `type daemonDeps struct { openStore func(config.StoreConfig) (store.Store, error); listen func() (net.Listener, error); listenGRPC func() (net.Listener, error); newToken func() (string, error) }`
  - `type daemonParams struct { store config.StoreConfig; discoveryPath string; reaperWindow time.Duration; maxShards int; otlpEndpoint string; otlpProject string; postgresDSN string; neo4jURI string; neo4jUser string; neo4jPassword string; transcriptDir string; transcriptExclude []string; allowPayloadAccess bool; allowAnnotations bool }`
  - `func runDaemonWith(ctx context.Context, deps daemonDeps, p daemonParams) error`
  - `func defaultDaemonDeps() daemonDeps`
  - `func storeDBPath(c config.StoreConfig) string` (sqlite→`SQLite.Path`, else `""`)
  - `func resolveDiscovery(s string) string` (empty→`daemon.DiscoveryPath()`)
  - New flag `--config` (default `""`); `--db` default changed from `"catacomb.db"` to `""`.
- Consumes: Task 8 `store.Open`, Task 9 `resolveConfig`/`daemonFlags`, `config`, `daemon`, `repro`.

Steps:

- [ ] **Write the failing tests / rewrite call sites** in `cmd/catacomb/daemon_test.go`. Update the import block to drop nothing essential and add `"github.com/realkarych/catacomb/config"`. Add the two test helpers at the top of the file (after the `failSinceStore` block):

```go
func testDaemonDeps() daemonDeps {
	return daemonDeps{
		openStore:  store.Open,
		listen:     daemon.ListenLoopback,
		listenGRPC: daemon.ListenLoopback,
		newToken:   daemon.NewToken,
	}
}

func testDaemonParams(t *testing.T) daemonParams {
	t.Helper()
	return daemonParams{
		store:         config.StoreConfig{Backend: config.BackendSQLite, SQLite: config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "g.db")}},
		discoveryPath: filepath.Join(t.TempDir(), "d.json"),
		reaperWindow:  30 * time.Minute,
		maxShards:     4096,
	}
}
```

  Then replace each `runDaemonWith(...)` call with the struct API. The exact rewrites (each is the full replacement body of the named test's daemon call):

  - `TestRunDaemonOpenError`:

    ```go
    deps := testDaemonDeps()
    deps.openStore = func(config.StoreConfig) (store.Store, error) { return nil, errors.New("open") }
    err := runDaemonWith(context.Background(), deps, testDaemonParams(t))
    require.Error(t, err)
    ```

  - `TestRunDaemonListenError`:

    ```go
    deps := testDaemonDeps()
    deps.listen = func() (net.Listener, error) { return nil, errors.New("listen") }
    err := runDaemonWith(context.Background(), deps, testDaemonParams(t))
    require.Error(t, err)
    ```

  - `TestRunDaemonDiscoveryError`:

    ```go
    dir := t.TempDir()
    require.NoError(t, os.WriteFile(filepath.Join(dir, "afile"), []byte("x"), 0o600))
    p := testDaemonParams(t)
    p.discoveryPath = filepath.Join(dir, "afile", "d.json")
    err := runDaemonWith(context.Background(), testDaemonDeps(), p)
    require.Error(t, err)
    ```

  - `TestRunDaemonRecoverError`:

    ```go
    deps := testDaemonDeps()
    deps.openStore = openFailSince
    err := runDaemonWith(context.Background(), deps, testDaemonParams(t))
    require.Error(t, err)
    ```

    and change `openFailSince` to the new factory shape:

    ```go
    func openFailSince(config.StoreConfig) (store.Store, error) { return &failSinceStore{}, nil }
    ```

  - `TestRunDaemonNewTokenError`:

    ```go
    deps := testDaemonDeps()
    deps.newToken = func() (string, error) { return "", errors.New("token") }
    err := runDaemonWith(context.Background(), deps, testDaemonParams(t))
    require.Error(t, err)
    ```

  - `TestRunDaemonWithGRPCListenError`:

    ```go
    deps := testDaemonDeps()
    deps.listenGRPC = func() (net.Listener, error) { return nil, errors.New("grpc listen") }
    err := runDaemonWith(context.Background(), deps, testDaemonParams(t))
    require.Error(t, err)
    ```

  - `TestRunDaemonDiscoveryHasGRPCAddr`: keep the eventually-loop; change the goroutine to

    ```go
    p := testDaemonParams(t)
    p.discoveryPath = discovery
    go func() { errc <- runDaemonWith(ctx, testDaemonDeps(), p) }()
    ```

    (set `discovery := filepath.Join(t.TempDir(), "d.json")` and read it in the loop).
  - `TestRunDaemonWithOTLPEndpoint`:

    ```go
    discovery := filepath.Join(t.TempDir(), "d.json")
    p := testDaemonParams(t)
    p.discoveryPath = discovery
    p.otlpEndpoint = "grpc://collector.example:4317"
    p.otlpProject = "phoenix-demo"
    go func() { errc <- runDaemonWith(ctx, testDaemonDeps(), p) }()
    awaitHealthz(t, readAddr(t, discovery))
    cancel()
    require.NoError(t, <-errc)
    ```

  - `TestRunDaemonWithTranscriptDir`:

    ```go
    disc := filepath.Join(t.TempDir(), "d.json")
    p := testDaemonParams(t)
    p.discoveryPath = disc
    p.reaperWindow = time.Minute
    p.maxShards = 16
    p.transcriptDir = t.TempDir()
    p.transcriptExclude = []string{"x-*.jsonl"}
    go func() { errc <- runDaemonWith(ctx, testDaemonDeps(), p) }()
    ```

    (keep the `os.Stat(disc)` eventually-loop, `cancel()`, `<-errc`).
  - `TestRunDaemonWithNeo4jURISet`:

    ```go
    discovery := filepath.Join(t.TempDir(), "d.json")
    p := testDaemonParams(t)
    p.discoveryPath = discovery
    p.neo4jURI = "bolt://localhost:7687"
    p.neo4jUser = "neo4j"
    p.neo4jPassword = "pw"
    go func() { errc <- runDaemonWith(ctx, testDaemonDeps(), p) }()
    awaitHealthz(t, readAddr(t, discovery))
    cancel()
    require.NoError(t, <-errc)
    ```

  - `TestRunDaemonWithAllowPayloadAccessTrue`:

    ```go
    discovery := filepath.Join(t.TempDir(), "d.json")
    p := testDaemonParams(t)
    p.discoveryPath = discovery
    p.allowPayloadAccess = true
    go func() { errc <- runDaemonWith(ctx, testDaemonDeps(), p) }()
    awaitHealthz(t, readAddr(t, discovery))
    cancel()
    require.NoError(t, <-errc)
    ```

  - `TestRunDaemonDiscoveryHasScope`: assert the resolved sqlite path is echoed in discovery:

    ```go
    db := filepath.Join(t.TempDir(), "g.db")
    transcripts := t.TempDir()
    discovery := filepath.Join(t.TempDir(), "d.json")
    p := testDaemonParams(t)
    p.store = config.StoreConfig{Backend: config.BackendSQLite, SQLite: config.SQLiteConfig{Path: db}}
    p.discoveryPath = discovery
    p.transcriptDir = transcripts
    p.allowPayloadAccess = true
    go func() { errc <- runDaemonWith(ctx, testDaemonDeps(), p) }()
    ```

    (keep eventually-loop; assert `d.TranscriptDir == transcripts`, `d.DBPath == db`, `d.AllowPayloadAccess`).
  - `TestRunDaemonDiscoveryHasPidAndStartedAt`:

    ```go
    discovery := filepath.Join(t.TempDir(), "d.json")
    p := testDaemonParams(t)
    p.discoveryPath = discovery
    go func() { errc <- runDaemonWith(ctx, testDaemonDeps(), p) }()
    ```

    (keep the rest of the assertions).
  - `TestDaemonEndToEnd`: replace its `runDaemonWith(ctx, store.OpenSQLite, ...)` with

    ```go
    p := testDaemonParams(t)
    p.store = config.StoreConfig{Backend: config.BackendSQLite, SQLite: config.SQLiteConfig{Path: dbPath}}
    p.discoveryPath = discovery
    go func() { errc <- runDaemonWith(ctx, testDaemonDeps(), p) }()
    ```

    (keep `dbPath`/`discovery` locals and downstream assertions).

<!-- markdownlint-disable-next-line MD005 -->
- [ ] **Add new tests** (memory backend serves + nil DBPath; default path resolution; `--config` flag exists):

```go
func TestRunDaemonMemoryBackendServesAndDiscoveryDBEmpty(t *testing.T) {
	discovery := filepath.Join(t.TempDir(), "d.json")
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	p := daemonParams{store: config.StoreConfig{Backend: config.BackendMemory}, discoveryPath: discovery, reaperWindow: 30 * time.Minute, maxShards: 4096}
	go func() { errc <- runDaemonWith(ctx, testDaemonDeps(), p) }()
	awaitHealthz(t, readAddr(t, discovery))
	d, err := daemon.ReadDiscovery(discovery)
	require.NoError(t, err)
	assert.Equal(t, "", d.DBPath)
	cancel()
	require.NoError(t, <-errc)
}

func TestStoreDBPath(t *testing.T) {
	assert.Equal(t, "/x.db", storeDBPath(config.StoreConfig{Backend: config.BackendSQLite, SQLite: config.SQLiteConfig{Path: "/x.db"}}))
	assert.Equal(t, "", storeDBPath(config.StoreConfig{Backend: config.BackendMemory}))
}

func TestDaemonConfigFlagRegistered(t *testing.T) {
	cmd := newDaemonCmd()
	f := cmd.Flags().Lookup("config")
	require.NotNil(t, f)
	assert.Equal(t, "", f.DefValue)
}

func TestDaemonDBFlagDefaultEmpty(t *testing.T) {
	cmd := newDaemonCmd()
	f := cmd.Flags().Lookup("db")
	require.NotNil(t, f)
	assert.Equal(t, "", f.DefValue)
}
```

- [ ] **Make full-command tests hermetic.** In `TestDaemonCommandWiring`, `TestDaemonCommandDefaultDiscovery`, `TestDaemonCommandReaperWindowFlag`, `TestDaemonCommandMaxShardsFlag`, add at the top (so RunE's config read never touches the real `~/.catacomb/config.yaml`):

  ```go
  t.Setenv("CATACOMB_CONFIG", filepath.Join(t.TempDir(), "none.yaml"))
  ```

  These tests keep passing `--db <temp>` / `--discovery`, which are highest precedence.

- [ ] **Run** `go test ./cmd/catacomb/ -run 'TestRunDaemon|TestDaemonCommand|TestStoreDBPath|TestDaemonConfig|TestDaemonDBFlag|TestDaemonEndToEnd'` — expect FAIL to compile (`runDaemonWith` still has the old signature; `daemonParams`/`daemonDeps` undefined).
- [ ] **Minimal impl** — rewrite `cmd/catacomb/daemon.go`:

```go
package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/realkarych/catacomb/config"
	"github.com/realkarych/catacomb/daemon"
	"github.com/realkarych/catacomb/repro"
	"github.com/realkarych/catacomb/store"
)

type daemonDeps struct {
	openStore  func(config.StoreConfig) (store.Store, error)
	listen     func() (net.Listener, error)
	listenGRPC func() (net.Listener, error)
	newToken   func() (string, error)
}

type daemonParams struct {
	store              config.StoreConfig
	discoveryPath      string
	reaperWindow       time.Duration
	maxShards          int
	otlpEndpoint       string
	otlpProject        string
	postgresDSN        string
	neo4jURI           string
	neo4jUser          string
	neo4jPassword      string
	transcriptDir      string
	transcriptExclude  []string
	allowPayloadAccess bool
	allowAnnotations   bool
}

func defaultDaemonDeps() daemonDeps {
	return daemonDeps{
		openStore:  store.Open,
		listen:     daemon.ListenLoopback,
		listenGRPC: daemon.ListenLoopback,
		newToken:   daemon.NewToken,
	}
}

func storeDBPath(c config.StoreConfig) string {
	if c.Backend == config.BackendSQLite {
		return c.SQLite.Path
	}
	return ""
}

func resolveDiscovery(s string) string {
	if s != "" {
		return s
	}
	return daemon.DiscoveryPath()
}

func newDaemonCmd() *cobra.Command {
	var configPath, dbPath, discoveryPath, otlpEndpoint, otlpProject, postgresDSN string
	var neo4jURI, neo4jUser, neo4jPassword string
	var reaperWindow time.Duration
	var maxShards int
	var transcriptDir string
	var transcriptExclude []string
	var allowPayloadAccess bool
	var allowAnnotations bool
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Run the catacomb daemon (receives hook events, builds the live graph)",
		Long: `Run the catacomb daemon: it receives hook events, builds the live graph,
persists it to the configured primary store, and serves the web UI and gRPC feed.

Configuration is loaded from ~/.catacomb/config.yaml (override with --config or
$CATACOMB_CONFIG). Set store.backend: memory for a live-only daemon that persists
nothing. The default SQLite database is ~/.catacomb/catacomb.db. Existing flags
remain the highest-precedence override.`,
		Example: `  # live only
  catacomb daemon

  # backfill and tail every past + live session
  catacomb daemon --transcript-dir ~/.claude/projects --allow-payload-access`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := osUserHomeDir()
			if err != nil {
				return fmt.Errorf("daemon: resolve home: %w", err)
			}
			flags := daemonFlags{
				configPath: configPath, configPathSet: cmd.Flags().Changed("config"),
				dbPath: dbPath, dbPathSet: cmd.Flags().Changed("db"),
				discoveryPath: discoveryPath, discoveryPathSet: cmd.Flags().Changed("discovery"),
				reaperWindow: reaperWindow, reaperWindowSet: cmd.Flags().Changed("reaper-window"),
				maxShards: maxShards, maxShardsSet: cmd.Flags().Changed("max-shards"),
				allowPayloadAccess: allowPayloadAccess, allowPayloadAccessSet: cmd.Flags().Changed("allow-payload-access"),
				allowAnnotations: allowAnnotations, allowAnnotationsSet: cmd.Flags().Changed("allow-annotations"),
			}
			cfg, err := resolveConfig(flags, os.ReadFile, os.LookupEnv, home)
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			params := daemonParams{
				store:              cfg.Store,
				discoveryPath:      resolveDiscovery(cfg.Daemon.Discovery),
				reaperWindow:       time.Duration(cfg.Daemon.ReaperWindow),
				maxShards:          cfg.Daemon.MaxShards,
				otlpEndpoint:       otlpEndpoint,
				otlpProject:        otlpProject,
				postgresDSN:        postgresDSN,
				neo4jURI:           neo4jURI,
				neo4jUser:          neo4jUser,
				neo4jPassword:      neo4jPassword,
				transcriptDir:      transcriptDir,
				transcriptExclude:  transcriptExclude,
				allowPayloadAccess: cfg.Daemon.AllowPayloadAccess,
				allowAnnotations:   cfg.Daemon.AllowAnnotations,
			}
			return runDaemonWith(ctx, defaultDaemonDeps(), params)
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "config file path (default: ~/.catacomb/config.yaml; or $CATACOMB_CONFIG)")
	cmd.Flags().StringVar(&dbPath, "db", "", "SQLite database path (default: ~/.catacomb/catacomb.db; maps to store.sqlite.path)")
	cmd.Flags().StringVar(&discoveryPath, "discovery", "", "discovery file path (default: resolved CATACOMB_DISCOVERY)")
	cmd.Flags().DurationVar(&reaperWindow, "reaper-window", 30*time.Minute, "idle window before a run is marked abandoned")
	cmd.Flags().IntVar(&maxShards, "max-shards", 4096, "soft cap on in-memory execution shards")
	cmd.Flags().StringVar(&otlpEndpoint, "otlp-export-endpoint", "", "downstream OTLP endpoint to export the reconstructed trace tree (empty = disabled)")
	cmd.Flags().StringVar(&otlpProject, "otlp-export-project", "catacomb", "OpenInference project name (resource attribute openinference.project.name)")
	cmd.Flags().StringVar(&postgresDSN, "postgres-export-dsn", "", "PostgreSQL DSN to export the materialized graph (empty = disabled)")
	cmd.Flags().StringVar(&neo4jURI, "neo4j-export-uri", "", "Neo4j Bolt URI to export the materialized graph (empty = disabled)")
	cmd.Flags().StringVar(&neo4jUser, "neo4j-export-user", "", "Neo4j username for materialized graph export")
	cmd.Flags().StringVar(&neo4jPassword, "neo4j-export-password", "", "Neo4j password for materialized graph export")
	cmd.Flags().StringVar(&transcriptDir, "transcript-dir", "", "Claude Code transcript dir to tail (empty = disabled; recommended: ~/.claude/projects)")
	cmd.Flags().StringArrayVar(&transcriptExclude, "transcript-exclude", nil, "glob(s) of transcript paths to never tail (repeatable; the daemon db + cwd are always excluded)")
	cmd.Flags().BoolVar(&allowPayloadAccess, "allow-payload-access", false, "enable the node payload content endpoint (default off)")
	cmd.Flags().BoolVar(&allowAnnotations, "allow-annotations", false, "enable the node annotation write endpoint (default off)")
	return cmd
}

func runDaemonWith(ctx context.Context, deps daemonDeps, p daemonParams) error {
	s, err := deps.openStore(p.store)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()

	dbPath := storeDBPath(p.store)
	d := daemon.New(s)
	d.SetReaperWindow(p.reaperWindow)
	d.SetMaxShards(p.maxShards)
	d.SetOTLPEndpoint(p.otlpEndpoint)
	d.SetOTLPProject(p.otlpProject)
	d.SetPostgresDSN(p.postgresDSN)
	d.SetNeo4j(p.neo4jURI, p.neo4jUser, p.neo4jPassword)
	d.SetDBPath(dbPath)
	d.SetTranscriptDir(p.transcriptDir)
	d.SetTranscriptExclude(p.transcriptExclude)
	d.SetAllowPayloadAccess(p.allowPayloadAccess)
	d.SetAllowAnnotations(p.allowAnnotations)
	d.SetReproConfig(repro.Config{
		OTLPEndpoint:  p.otlpEndpoint,
		OTLPProject:   p.otlpProject,
		TranscriptDir: p.transcriptDir,
	})
	if err := d.Recover(); err != nil {
		return err
	}
	token, err := deps.newToken()
	if err != nil {
		return err
	}
	ln, err := deps.listen()
	if err != nil {
		return err
	}
	defer func() { _ = ln.Close() }()

	grpcLn, err := deps.listenGRPC()
	if err != nil {
		return err
	}
	defer func() { _ = grpcLn.Close() }()

	disc := daemon.Discovery{
		Addr:               ln.Addr().String(),
		Token:              token,
		GRPCAddr:           grpcLn.Addr().String(),
		TranscriptDir:      p.transcriptDir,
		DBPath:             dbPath,
		AllowPayloadAccess: p.allowPayloadAccess,
		AllowAnnotations:   p.allowAnnotations,
	}
	disc.Pid = os.Getpid()
	disc.StartedAt = time.Now().UTC().Format(time.RFC3339)
	if err := daemon.WriteDiscovery(p.discoveryPath, disc); err != nil {
		return err
	}
	return d.Serve(ctx, ln, grpcLn, token)
}
```

- [ ] **Run** `go test -race ./cmd/catacomb/` — expect PASS (all rewritten + new tests). `resolveDiscovery`'s empty branch is covered by `TestDaemonCommandDefaultDiscovery` (which sets `CATACOMB_DISCOVERY`); the non-empty branch by every test that sets a discovery path.
- [ ] **Run** `make cover` — expect the gate to pass for `cmd/catacomb`, `config`, `store`, `store/memory`, `store/storetest`.
- [ ] **Commit:** `git add cmd/catacomb/daemon.go cmd/catacomb/daemon_test.go && git commit -m "refactor(cmd): config-driven daemon, store.Open, struct runDaemonWith, --config"`

---

## Task 11: batch CLI default db path + up.go consistency

**Files:**

- Create: `cmd/catacomb/storepath.go`
- Create: `cmd/catacomb/storepath_test.go`
- Modify: `cmd/catacomb/runs.go:27`, `cmd/catacomb/inspect.go:27`, `cmd/catacomb/snapshot.go:25`, `cmd/catacomb/export.go:74`, `cmd/catacomb/replay.go:45` (`--db` default `"catacomb.db"` → `defaultDBPath()`)

**Interfaces:**

- Produces: `func defaultDBPath() string` (package `main`) — `~/.catacomb/catacomb.db` via `osUserHomeDir()`; falls back to `"catacomb.db"` if home is unresolvable.
- Consumes: existing `osUserHomeDir` package var (`cmd/catacomb/installhooks.go:22`).

Notes: `up.go` forks `catacomb daemon` with no `--db`, so it inherits the daemon's new default automatically; `restartCommand` prints `--db` from `disc.DBPath`, which is now the resolved absolute path — no change required there. PR1 keeps batch-CLI config integration out of scope: the batch commands adopt the new default path but do not read `config.yaml` (a custom `store.sqlite.path` still needs an explicit `--db` for `runs`/`inspect`/`snapshot`/`export`). Listed under Deferred.

Steps:

- [ ] **Write failing test** `cmd/catacomb/storepath_test.go`:

```go
package main

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultDBPathFromHome(t *testing.T) {
	orig := osUserHomeDir
	t.Cleanup(func() { osUserHomeDir = orig })
	osUserHomeDir = func() (string, error) { return "/home/u", nil }
	assert.Equal(t, filepath.Join("/home/u", ".catacomb", "catacomb.db"), defaultDBPath())
}

func TestDefaultDBPathFallback(t *testing.T) {
	orig := osUserHomeDir
	t.Cleanup(func() { osUserHomeDir = orig })
	osUserHomeDir = func() (string, error) { return "", errors.New("no home") }
	assert.Equal(t, "catacomb.db", defaultDBPath())
}

func TestBatchCommandsUseDefaultDBPath(t *testing.T) {
	for _, cmd := range []*struct {
		name string
		def  string
	}{
		{"runs", newRunsCmd().Flags().Lookup("db").DefValue},
		{"inspect", newInspectCmd().Flags().Lookup("db").DefValue},
		{"snapshot", newSnapshotCmd().Flags().Lookup("db").DefValue},
		{"export", newExportCmd().Flags().Lookup("db").DefValue},
		{"replay", newReplayCmd().Flags().Lookup("db").DefValue},
	} {
		assert.Equal(t, defaultDBPath(), cmd.def, "command %s --db default", cmd.name)
	}
}
```

- [ ] **Run** `go test ./cmd/catacomb/ -run 'TestDefaultDBPath|TestBatchCommands'` — expect FAIL (`undefined: defaultDBPath`; batch defaults still `"catacomb.db"`).
- [ ] **Minimal impl** `cmd/catacomb/storepath.go`:

```go
package main

import "path/filepath"

func defaultDBPath() string {
	home, err := osUserHomeDir()
	if err != nil {
		return "catacomb.db"
	}
	return filepath.Join(home, ".catacomb", "catacomb.db")
}
```

- [ ] **Edit** each batch command's `--db` registration, e.g. in `cmd/catacomb/runs.go:27` change
  `cmd.Flags().StringVar(&dbPath, "db", "catacomb.db", "SQLite database path")` →
  `cmd.Flags().StringVar(&dbPath, "db", defaultDBPath(), "SQLite database path (default: ~/.catacomb/catacomb.db)")`.
  Apply the identical change in `inspect.go:27`, `snapshot.go:25`, `export.go:74`, `replay.go:45` — note the receiver variable differs (`runs.go`/`inspect.go`/`snapshot.go` use `&dbPath`, `export.go` uses `&a.dbPath`, `replay.go` uses `&args.dbPath`); only the default literal `"catacomb.db"` → `defaultDBPath()` and the usage string change, leave each var as-is.
- [ ] **Run** `go test -race ./cmd/catacomb/` — expect PASS. Grep for any test asserting the old literal: `grep -rn '"catacomb.db"' cmd/catacomb/*_test.go` — if any remain, update to `defaultDBPath()`.
- [ ] **Run** `make cover && make lint` — expect both green across the whole module.
- [ ] **Build + manual live-verify (mandatory per spec §12):**
  - `make build`
  - sqlite default: `./bin/catacomb daemon --discovery /tmp/cat/d.json &` then confirm `~/.catacomb/catacomb.db` is created and `curl http://<addr>/healthz` 200; `kill` it.
  - memory mode: write `/tmp/cat/config.yaml` with `store:\n  backend: memory\n`, run `./bin/catacomb daemon --config /tmp/cat/config.yaml --discovery /tmp/cat/d.json &`; confirm UI/healthz serve, discovery `db_path` is empty, and NO db file is created; `kill` it.
  - unknown key: a config with a typo'd key fails fast with a YAML position; `store.backend: postgres` fails with `ErrBackendNotImplemented`.
- [ ] **Commit:** `git add cmd/catacomb/storepath.go cmd/catacomb/storepath_test.go cmd/catacomb/runs.go cmd/catacomb/inspect.go cmd/catacomb/snapshot.go cmd/catacomb/export.go cmd/catacomb/replay.go && git commit -m "feat(cmd): move batch CLI default db to ~/.catacomb/catacomb.db"`

---

## Deferred to PR2/PR3

These are recognized in the schema (parsed + validated in PR1) but intentionally NOT wired here:

- **Sinks from config** (`export.Build(sinks []config.Sink) ([]export.Exporter, error)`, replacing `--otlp-export-*`/`--postgres-export-dsn`/`--neo4j-export-*` field-by-field construction); deprecation marking of those flags. → PR2.
- **Sources enable/disable gating** in the daemon (route mounting for `hooks`/`otel`/`stream_json`, tailer start for `jsonl`); reconciling `Defaults().Sources.JSONL` with the `--transcript-dir` flag. → PR2.
- **Batch-CLI config integration** (`runs`/`inspect`/`snapshot`/`export` reading `store.sqlite.path` from `config.yaml` instead of only the default path). → PR2.
- **Consuming `daemon.discovery` from config beyond the empty-fallback** (full reconciliation with `daemon.DiscoveryPath()`'s XDG/`CATACOMB_DISCOVERY` logic). → PR3 (discovery becomes the lifecycle handle).
- **Lifecycle commands:** `down`, `restart`, enriched `status` (+ `--json` resolved config), `logs [-f]`, `up --foreground`. → PR3.

---

## Self-review against the spec (PR1 scope)

- §5.3 pure `config` pkg: `Parse`/`Defaults`/`Merge`/`Validate` (+ `FromEnv`/`ExpandPath`) — Tasks 1-5; no I/O in `config`. ✔
- §5.1 strict decode (`KnownFields(true)`), missing file = defaults — Task 2 + Task 9. ✔
- §5.2 full schema stable day one (Daemon/Store/Sources/Sinks) — Task 1; only Store+Daemon consumed (Tasks 9-10), Sources/Sinks parsed+validated only. ✔
- §5.4 precedence `defaults < file < env < flags`, flags only when `Flag.Changed`; `~`/`$VAR` expanded by cmd layer — Tasks 9-10. ✔
- §5.5 validation invariants + sentinels (`ErrNoStoreBackend`, `ErrUnknownStoreBackend`, `ErrBackendNotImplemented`, `ErrUnknownSink`, …) — Task 4. ✔
- §6 `store.Open` factory; `store/memory` full impl; default sqlite path `~/.catacomb/catacomb.db`; batch CLI adopts it — Tasks 6, 8, 11. ✔
- §12 shared store-contract suite across sqlite+memory; factory wiring tests incl. postgres sentinel; live-verify — Tasks 7, 8, 11. ✔
- §10 backward compat: `--db`→`store.sqlite.path`, all existing flags remain top-precedence overrides, one documented default change (db path) — Task 10. ✔
- Type/signature consistency: `config.StoreConfig`, `daemonDeps.openStore func(config.StoreConfig)(store.Store,error)`, `store.Open`, `daemonParams`, `daemonFlags`, `config.Duration` are introduced once and used identically across Tasks 6-11. ✔
- No placeholders; every step shows real Go (zero comments). ✔
