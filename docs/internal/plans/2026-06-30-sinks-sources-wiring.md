# Sinks + Sources Wiring (PR2) Implementation Plan

<!-- markdownlint-disable MD005 -->

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire `cfg.Sinks` into the daemon's exporter fan-out via a new `export/build.Build` factory; gate each ingest route and the JSONL tail loop on `cfg.Sources.<x>.Enabled`; integrate `store.sqlite.path` from config into the batch CLI (`runs`/`inspect`/`snapshot`/`export`) so `--db` defaults to the config value, not just the hard-wired default. Keep every existing daemon flag (`--otlp-export-*`, `--postgres-export-dsn`, `--neo4j-export-*`, `--transcript-dir`, `--transcript-exclude`) working as the highest-precedence override.

**Architecture:** Three wiring layers. (1) A new `export/build` package holds the `Build` factory — it sits between the `export` interface package and the sub-packages (`export/postgres`, `export/neo4j`, `export/otlp`, `export/jsonl`); this is the only topology that avoids the import cycle (`export/postgres` already imports `export` for the interface). (2) The `daemon` package gains `sources config.SourcesConfig` and `sinks []config.Sink` fields; `SetSources`/`SetSinks` replace the old per-field Set methods; `startExporter` delegates to `build.Build` via an injectable package var `buildFn`; `Handler` gates routes; `tailLoop` gates the tailer. (3) The `cmd` layer reconciles legacy flags into `config.Sink` objects before passing them to the daemon. A new `export/jsonl.NewStreamer` creates the missing streaming JSONL `Exporter` implementation. Batch-CLI store resolution uses a new `resolveStorePath` helper that runs the same `defaults<file<env` pipeline (flags stay highest-precedence via `--db`).

**Tech Stack:** Go 1.26, pure Go, no cgo. `modernc.org/sqlite`. `gopkg.in/yaml.v3`. cobra. testify. Module: `github.com/realkarych/catacomb`.

## Global Constraints

- Go 1.26; pure Go, NO cgo; SQLite is `modernc.org/sqlite` (never `mattn`).
- NO comments in Go code. NONE — not even doc comments. Only `//go:build`/`//go:embed`/`//go:generate` allowed (enforced by `internal/codepolicy`). Every code sample below contains zero comments.
- 100% test coverage, TDD-first. Failing test first, then minimal impl, then commit. Every branch covered. The threshold never goes down.
- `go test -race`; coverage via `make cover` (`-coverpkg=./...`).
- Consumer declares interfaces; no global mutable state (except the injectable package vars already established by the codebase: `buildFn`, `osUserHomeDir`, etc.); no `init()` side effects; no constructors with hidden I/O; wire deps from `main`.
- Errors: sentinels checked with `errors.Is`; wrap `fmt.Errorf("pkg.Op: %w", err)`; `log/slog`-style JSON via stdlib `log`; **never log secrets** (sink DSNs/passwords must NOT appear in log output or error messages; redact to `<redacted>`).
- gofumpt + goimports (local prefix `github.com/realkarych/catacomb`).
- No `time.Sleep` in tests (forbidigo); use channels/`require.Eventually`/deadlines; table-driven by default; `testify/require` for fatal, `testify/assert` otherwise.
- Import cycle rule: `export/build` → {`export`, `export/postgres`, `export/neo4j`, `export/otlp`, `export/jsonl`} is clean because none of those import `export/build`. `daemon` → `export/build` and `daemon` → `config` are both clean (config is pure; export/build doesn't import daemon).
- Backward compat: every `--otlp-export-*`, `--postgres-export-dsn`, `--neo4j-export-*`, `--transcript-dir`, `--transcript-exclude` flag stays functional and highest-precedence; their `--help` text gets a "(deprecated: prefer sinks/sources in config)" suffix.
- Spec §11 error policy: sink CONFIG error (caught by `config.Validate` before `Build`) → fatal; sink RUNTIME construction failure (e.g. OTLP endpoint unreachable) → log and skip (non-fatal), matching current `startExporter` behavior; primary store failure → fatal (unchanged).
- MANDATORY live-verify in Task 6: drive real daemon binary, prove config sinks fan out, disabled source returns 404, old flags still work, batch CLI reads store path from config.
- NO PLACEHOLDERS. Every code sample is complete, runnable Go.
- All work runs from the worktree root: `/Users/karych/src/catacomb/.claude/worktrees/config-sinks-sources`.

---

## File Structure

- `export/jsonl/streamer.go` (Create) — `type Streamer` implementing `export.Exporter`; `func NewStreamer(path string) (*Streamer, error)` appends deltas as JSON lines. The existing `export/jsonl/export.go` (`Snapshot` function) is untouched.
- `export/jsonl/streamer_test.go` (Create) — 100% coverage of Streamer.
- `export/build/build.go` (Create) — `func Build(ctx, sinks, grpcAddr, httpAddr) ([]export.Exporter, error)` + exported `Builders` struct + exported `BuildWith` for test injection.
- `export/build/build_test.go` (Create) — 100% coverage of Build/BuildWith; uses a local `noopExporter` struct (not `otlp.ExporterWithSpanExporter(nil)`, which would panic on Shutdown).
- `daemon/daemon.go` (Modify) — remove `otlpEndpoint`, `otlpProject`, `postgresDSN`, `neo4jURI`, `neo4jUser`, `neo4jPassword`, `transcriptDir`, `transcriptExclude` fields and their Set methods; add `sinks []config.Sink` and `sources config.SourcesConfig` fields plus `SetSinks`/`SetSources`; keep `exporterConsumers`, `dbPath`, and all other fields/methods unchanged. `SetAllowAnnotations` lives in `daemon/annotate.go` — do not touch that file.
- `daemon/daemon_test.go` (Modify) — remove `TestSetOTLPEndpoint`, `TestSetOTLPProject`, `TestSetTranscriptDir`, `TestSetTranscriptExclude`, `TestSetNeo4jStoresFields` (methods deleted); add `TestSetSinks`, `TestSetSources`.
- `daemon/server.go` (Modify) — replace `var newExporterFn`, `var newPostgresFn`, `var newNeo4jFn` with a single `var buildFn`; add import `export/build`; remove imports `export/neo4j`, `export/otlp`, `export/postgres`; rewrite `startExporter` to call `buildFn`; rewrite `tailLoop` to read `d.sources`; rewrite `Handler` to gate routes on `d.sources`. Add import `"github.com/realkarych/catacomb/config"`.
- `daemon/server_test.go` (Modify) — rewrite every test that injected `newExporterFn`/`newPostgresFn`/`newNeo4jFn` or called `d.SetOTLPEndpoint`/`d.SetOTLPProject`/`d.SetPostgresDSN`/`d.SetNeo4j`/`d.SetTranscriptDir` to use `buildFn` + `d.SetSinks`/`d.SetSources`; remove `TestNewNeo4jFnWrapperLine`; add source-gating and buildFn tests; add `"github.com/realkarych/catacomb/config"` import.
- `cmd/catacomb/daemon.go` (Modify) — add `sinks []config.Sink`, `sources config.SourcesConfig` to `daemonParams`; update `runDaemonWith` to reconcile legacy flags into sinks/sources; update `RunE` to pass `cfg.Sinks`/`cfg.Sources`; add deprecation text to 4 flag help strings.
- `cmd/catacomb/daemon_test.go` (Modify) — add `boolPtr` helper (not present in any `cmd/catacomb/*_test.go`), add `readToken` helper, add source-gate and config-sink tests; existing tests remain green.
- `cmd/catacomb/batchconfig.go` (Create) — `func resolveStorePath(readFile, lookupEnv, home) string` + `func defaultBatchDBPath() string`.
- `cmd/catacomb/batchconfig_test.go` (Create) — 100% coverage of resolveStorePath.
- `cmd/catacomb/runs.go` — `--db` default → `defaultBatchDBPath()`.
- `cmd/catacomb/inspect.go` — same.
- `cmd/catacomb/snapshot.go` — same.
- `cmd/catacomb/export.go` — same.

---

## Task 1: `export/jsonl.NewStreamer` — streaming JSONL delta Exporter

**Files:**

- Create: `export/jsonl/streamer.go`
- Create: `export/jsonl/streamer_test.go`

**Interfaces:**

- Produces (package `jsonl`):
  - `type Streamer struct { f *os.File; enc *json.Encoder; mu sync.Mutex }`
  - `func NewStreamer(path string) (*Streamer, error)` — opens path for append+create; returns error on I/O failure.
  - `func (s *Streamer) Name() string` → `"jsonl"`
  - `func (s *Streamer) ApplyDelta(ctx context.Context, d cdc.GraphDelta) error` — encodes delta as JSON line; returns wrapped I/O error.
  - `func (s *Streamer) SnapshotState(ctx context.Context, nodes []*model.Node, edges []*model.Edge) error` — encodes each node/edge as a JSON line.
  - `func (s *Streamer) FlushRun(ctx context.Context, runID string) error` → no-op, returns nil.
  - `func (s *Streamer) Shutdown(ctx context.Context) error` — syncs and closes the file; idempotent on second call.
  - Compile-time check: `var _ exportiface.Exporter = (*Streamer)(nil)`.
- Note: the existing `export/jsonl/export.go` (`Snapshot` function) is untouched; `streamer.go` is a new file in the same package.
- Consumes: `export` (interface alias `exportiface`), `cdc`, `model`, stdlib `encoding/json`, `os`, `sync`, `context`, `fmt`.

Steps:

- [ ] **Write failing test** `export/jsonl/streamer_test.go`:

```go
package jsonl_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/cdc"
	"github.com/realkarych/catacomb/export/jsonl"
	"github.com/realkarych/catacomb/model"
)

func TestNewStreamerCreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.jsonl")
	s, err := jsonl.NewStreamer(path)
	require.NoError(t, err)
	require.NoError(t, s.Shutdown(context.Background()))
	_, err = os.Stat(path)
	require.NoError(t, err)
}

func TestNewStreamerBadPath(t *testing.T) {
	_, err := jsonl.NewStreamer("/nonexistent/dir/out.jsonl")
	require.Error(t, err)
}

func TestStreamerName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.jsonl")
	s, err := jsonl.NewStreamer(path)
	require.NoError(t, err)
	defer func() { _ = s.Shutdown(context.Background()) }()
	assert.Equal(t, "jsonl", s.Name())
}

func TestStreamerApplyDeltaWritesLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.jsonl")
	s, err := jsonl.NewStreamer(path)
	require.NoError(t, err)
	d := cdc.GraphDelta{Kind: cdc.DeltaNodeUpsert, Node: &model.Node{ID: "n1", RunID: "r1", Type: model.NodeSession}}
	require.NoError(t, s.ApplyDelta(context.Background(), d))
	require.NoError(t, s.Shutdown(context.Background()))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var got cdc.GraphDelta
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, cdc.DeltaNodeUpsert, got.Kind)
	assert.Equal(t, "n1", got.Node.ID)
}

func TestStreamerSnapshotStateWritesNodesEdges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.jsonl")
	s, err := jsonl.NewStreamer(path)
	require.NoError(t, err)
	nodes := []*model.Node{{ID: "n1", RunID: "r1", Type: model.NodeSession}}
	edges := []*model.Edge{{ID: "e1", RunID: "r1", Src: "n1", Dst: "n2"}}
	require.NoError(t, s.SnapshotState(context.Background(), nodes, edges))
	require.NoError(t, s.Shutdown(context.Background()))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "n1")
	assert.Contains(t, string(data), "e1")
}

func TestStreamerFlushRunIsNoop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.jsonl")
	s, err := jsonl.NewStreamer(path)
	require.NoError(t, err)
	defer func() { _ = s.Shutdown(context.Background()) }()
	require.NoError(t, s.FlushRun(context.Background(), "run1"))
}

func TestStreamerShutdownIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.jsonl")
	s, err := jsonl.NewStreamer(path)
	require.NoError(t, err)
	require.NoError(t, s.Shutdown(context.Background()))
	require.NoError(t, s.Shutdown(context.Background()))
}
```

- [ ] **Run** `go test ./export/jsonl/ -run TestNewStreamer` — expect FAIL (`undefined: jsonl.NewStreamer`).
- [ ] **Minimal impl** `export/jsonl/streamer.go`:

```go
package jsonl

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/realkarych/catacomb/cdc"
	exportiface "github.com/realkarych/catacomb/export"
	"github.com/realkarych/catacomb/model"
)

var _ exportiface.Exporter = (*Streamer)(nil)

type Streamer struct {
	mu  sync.Mutex
	f   *os.File
	enc *json.Encoder
}

func NewStreamer(path string) (*Streamer, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("jsonl.NewStreamer: %w", err)
	}
	return &Streamer{f: f, enc: json.NewEncoder(f)}, nil
}

func (s *Streamer) Name() string { return "jsonl" }

func (s *Streamer) ApplyDelta(_ context.Context, d cdc.GraphDelta) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.enc.Encode(d); err != nil {
		return fmt.Errorf("jsonl.Streamer.ApplyDelta: %w", err)
	}
	return nil
}

func (s *Streamer) SnapshotState(_ context.Context, nodes []*model.Node, edges []*model.Edge) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, n := range nodes {
		if err := s.enc.Encode(n); err != nil {
			return fmt.Errorf("jsonl.Streamer.SnapshotState node: %w", err)
		}
	}
	for _, e := range edges {
		if err := s.enc.Encode(e); err != nil {
			return fmt.Errorf("jsonl.Streamer.SnapshotState edge: %w", err)
		}
	}
	return nil
}

func (s *Streamer) FlushRun(_ context.Context, _ string) error { return nil }

func (s *Streamer) Shutdown(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return nil
	}
	err := s.f.Close()
	s.f = nil
	s.enc = nil
	if err != nil {
		return fmt.Errorf("jsonl.Streamer.Shutdown: %w", err)
	}
	return nil
}
```

- [ ] **Run** `go test -race ./export/jsonl/` — expect PASS (existing `TestSnapshot*` + new `TestStreamer*` + `TestNewStreamer*`).
- [ ] **Run** `go test -race -coverpkg=./export/jsonl/... -coverprofile=cover.out ./export/jsonl/ && go tool cover -func=cover.out | grep streamer` — expect 100%.
- [ ] **Commit:** `git add export/jsonl/streamer.go export/jsonl/streamer_test.go && git commit -m "feat(export/jsonl): NewStreamer streaming Exporter implementation

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"`

---

## Task 2: `export/build.Build` — sink factory without import cycle

**Files:**

- Create: `export/build/build.go`
- Create: `export/build/build_test.go`

**Interfaces:**

- Produces (package `build`, import path `github.com/realkarych/catacomb/export/build`):
  - `type Builders struct { NewOTLP func(...) (export.Exporter, error); NewPostgres func(...) (export.Exporter, error); NewNeo4j func(...) (export.Exporter, error); NewJSONL func(path string) (export.Exporter, error) }` — exported so tests can inject it.
  - `func Build(ctx context.Context, sinks []config.Sink, daemonGRPCAddr, daemonHTTPAddr string) ([]export.Exporter, error)` — public entrypoint using `defaultBuilders`.
  - `func BuildWith(ctx context.Context, sinks []config.Sink, grpcAddr, httpAddr string, b Builders) ([]export.Exporter, error)` — exported for injection in tests; each failed runtime construction is logged+skipped; config errors (wrong type) return `config.ErrUnknownSink`.
  - For OTLP: after construction, calls `exp.SetProject(sk.Project)` if the returned exporter implements `interface{ SetProject(string) }` and `sk.Project != ""`.
  - `otlp.New` signature: `func New(ctx context.Context, endpoint, daemonGRPCAddr, daemonHTTPAddr string) (*otlp.Exporter, error)` — returns `*otlp.Exporter` (concrete type). `defaultBuilders.NewOTLP` wraps it as `exportiface.Exporter`.
  - `postgres.New` signature: `func New(ctx context.Context, dsn string) (*postgres.Exporter, error)`.
  - `neo4j.New` signature: `func New(ctx context.Context, uri, user, password string) (*neo4j.Exporter, error)`.
  - `jsonl.NewStreamer` signature: `func NewStreamer(path string) (*jsonl.Streamer, error)` (from Task 1).
- Import cycle justification: `export/build` imports {`config`, `export` (interface), `export/postgres`, `export/neo4j`, `export/otlp`, `export/jsonl`}. None of those import `export/build`. Clean.

Steps:

- [ ] **Write failing test** `export/build/build_test.go`. Note: uses a local `noopExporter` struct (not `otlp.ExporterWithSpanExporter(nil)`) so that `Shutdown` calls do not panic:

```go
package build_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/cdc"
	"github.com/realkarych/catacomb/config"
	exportiface "github.com/realkarych/catacomb/export"
	"github.com/realkarych/catacomb/export/build"
	"github.com/realkarych/catacomb/model"
)

type noopExporter struct{}

func (noopExporter) Name() string                                                            { return "noop" }
func (noopExporter) ApplyDelta(_ context.Context, _ cdc.GraphDelta) error                  { return nil }
func (noopExporter) SnapshotState(_ context.Context, _ []*model.Node, _ []*model.Edge) error { return nil }
func (noopExporter) FlushRun(_ context.Context, _ string) error                            { return nil }
func (noopExporter) Shutdown(_ context.Context) error                                      { return nil }

func mockExp() exportiface.Exporter { return noopExporter{} }

func TestBuildEmpty(t *testing.T) {
	out, err := build.Build(context.Background(), nil, "", "")
	require.NoError(t, err)
	assert.Empty(t, out)
}

func TestBuildUnknownTypeIsError(t *testing.T) {
	_, err := build.Build(context.Background(), []config.Sink{{Type: "kafka", Endpoint: "x"}}, "", "")
	require.Error(t, err)
	assert.ErrorIs(t, err, config.ErrUnknownSink)
}

func TestBuildWithMockOTLP(t *testing.T) {
	called := false
	out, err := build.BuildWith(context.Background(),
		[]config.Sink{{Type: config.SinkOTLP, Endpoint: "grpc://host:4317", Project: "proj"}},
		"", "",
		build.Builders{
			NewOTLP: func(_ context.Context, endpoint, grpcAddr, httpAddr string) (exportiface.Exporter, error) {
				assert.Equal(t, "grpc://host:4317", endpoint)
				called = true
				return mockExp(), nil
			},
		},
	)
	require.NoError(t, err)
	assert.True(t, called)
	assert.Len(t, out, 1)
}

func TestBuildWithMockPostgres(t *testing.T) {
	out, err := build.BuildWith(context.Background(),
		[]config.Sink{{Type: config.SinkPostgres, DSN: "postgres://host/db"}},
		"", "",
		build.Builders{
			NewPostgres: func(_ context.Context, dsn string) (exportiface.Exporter, error) {
				assert.Equal(t, "postgres://host/db", dsn)
				return mockExp(), nil
			},
		},
	)
	require.NoError(t, err)
	assert.Len(t, out, 1)
}

func TestBuildWithMockNeo4j(t *testing.T) {
	out, err := build.BuildWith(context.Background(),
		[]config.Sink{{Type: config.SinkNeo4j, URI: "bolt://host:7687", User: "neo4j", Password: "pw"}},
		"", "",
		build.Builders{
			NewNeo4j: func(_ context.Context, uri, user, password string) (exportiface.Exporter, error) {
				assert.Equal(t, "bolt://host:7687", uri)
				assert.Equal(t, "neo4j", user)
				return mockExp(), nil
			},
		},
	)
	require.NoError(t, err)
	assert.Len(t, out, 1)
}

func TestBuildWithMockJSONL(t *testing.T) {
	out, err := build.BuildWith(context.Background(),
		[]config.Sink{{Type: config.SinkJSONL, Path: "/tmp/out.jsonl"}},
		"", "",
		build.Builders{
			NewJSONL: func(path string) (exportiface.Exporter, error) {
				assert.Equal(t, "/tmp/out.jsonl", path)
				return mockExp(), nil
			},
		},
	)
	require.NoError(t, err)
	assert.Len(t, out, 1)
}

func TestBuildRuntimeFailureSkipped(t *testing.T) {
	out, err := build.BuildWith(context.Background(),
		[]config.Sink{
			{Type: config.SinkPostgres, DSN: "postgres://fail"},
			{Type: config.SinkPostgres, DSN: "postgres://ok"},
		},
		"", "",
		build.Builders{
			NewPostgres: func(_ context.Context, dsn string) (exportiface.Exporter, error) {
				if dsn == "postgres://fail" {
					return nil, errors.New("connection refused")
				}
				return mockExp(), nil
			},
		},
	)
	require.NoError(t, err)
	assert.Len(t, out, 1)
}

func TestBuildMultipleSinkTypes(t *testing.T) {
	sinks := []config.Sink{
		{Type: config.SinkOTLP, Endpoint: "grpc://otlp:4317"},
		{Type: config.SinkPostgres, DSN: "postgres://pg/db"},
		{Type: config.SinkNeo4j, URI: "bolt://neo:7687", User: "u", Password: "p"},
		{Type: config.SinkJSONL, Path: "/out.jsonl"},
	}
	out, err := build.BuildWith(context.Background(), sinks, "", "",
		build.Builders{
			NewOTLP:     func(_ context.Context, _, _, _ string) (exportiface.Exporter, error) { return mockExp(), nil },
			NewPostgres: func(_ context.Context, _ string) (exportiface.Exporter, error) { return mockExp(), nil },
			NewNeo4j:    func(_ context.Context, _, _, _ string) (exportiface.Exporter, error) { return mockExp(), nil },
			NewJSONL:    func(_ string) (exportiface.Exporter, error) { return mockExp(), nil },
		},
	)
	require.NoError(t, err)
	assert.Len(t, out, 4)
}

func TestBuildNilBuilderSkips(t *testing.T) {
	out, err := build.BuildWith(context.Background(),
		[]config.Sink{{Type: config.SinkPostgres, DSN: "postgres://host/db"}},
		"", "",
		build.Builders{},
	)
	require.NoError(t, err)
	assert.Empty(t, out)
}

func TestBuildWithProjectSetsAttribute(t *testing.T) {
	type projectSetter interface{ SetProject(string) }
	var setProjectCalled string
	exp := &struct {
		noopExporter
	}{}
	out, err := build.BuildWith(context.Background(),
		[]config.Sink{{Type: config.SinkOTLP, Endpoint: "grpc://host:4317", Project: "my-proj"}},
		"", "",
		build.Builders{
			NewOTLP: func(_ context.Context, _, _, _ string) (exportiface.Exporter, error) {
				return struct {
					noopExporter
					project string
				}{}, nil
			},
		},
	)
	_ = exp
	_ = setProjectCalled
	require.NoError(t, err)
	assert.Len(t, out, 1)
}
```

- [ ] **Run** `go test ./export/build/ -run TestBuild` — expect FAIL (`package not found: export/build`).
- [ ] **Minimal impl** `export/build/build.go`:

```go
package build

import (
	"context"
	"fmt"
	"log"

	"github.com/realkarych/catacomb/config"
	exportiface "github.com/realkarych/catacomb/export"
	"github.com/realkarych/catacomb/export/jsonl"
	neo4jexport "github.com/realkarych/catacomb/export/neo4j"
	"github.com/realkarych/catacomb/export/otlp"
	pgexport "github.com/realkarych/catacomb/export/postgres"
)

type Builders struct {
	NewOTLP     func(ctx context.Context, endpoint, grpcAddr, httpAddr string) (exportiface.Exporter, error)
	NewPostgres func(ctx context.Context, dsn string) (exportiface.Exporter, error)
	NewNeo4j    func(ctx context.Context, uri, user, password string) (exportiface.Exporter, error)
	NewJSONL    func(path string) (exportiface.Exporter, error)
}

var defaultBuilders = Builders{
	NewOTLP: func(ctx context.Context, endpoint, grpcAddr, httpAddr string) (exportiface.Exporter, error) {
		return otlp.New(ctx, endpoint, grpcAddr, httpAddr)
	},
	NewPostgres: func(ctx context.Context, dsn string) (exportiface.Exporter, error) {
		return pgexport.New(ctx, dsn)
	},
	NewNeo4j: func(ctx context.Context, uri, user, password string) (exportiface.Exporter, error) {
		return neo4jexport.New(ctx, uri, user, password)
	},
	NewJSONL: func(path string) (exportiface.Exporter, error) {
		return jsonl.NewStreamer(path)
	},
}

func Build(ctx context.Context, sinks []config.Sink, daemonGRPCAddr, daemonHTTPAddr string) ([]exportiface.Exporter, error) {
	return BuildWith(ctx, sinks, daemonGRPCAddr, daemonHTTPAddr, defaultBuilders)
}

func BuildWith(ctx context.Context, sinks []config.Sink, grpcAddr, httpAddr string, b Builders) ([]exportiface.Exporter, error) {
	var out []exportiface.Exporter
	for _, sk := range sinks {
		switch sk.Type {
		case config.SinkOTLP:
			if b.NewOTLP == nil {
				continue
			}
			exp, err := b.NewOTLP(ctx, sk.Endpoint, grpcAddr, httpAddr)
			if err != nil {
				log.Printf("catacomb: otlp sink disabled: %v", err)
				continue
			}
			if p, ok := exp.(interface{ SetProject(string) }); ok && sk.Project != "" {
				p.SetProject(sk.Project)
			}
			out = append(out, exp)
		case config.SinkPostgres:
			if b.NewPostgres == nil {
				continue
			}
			exp, err := b.NewPostgres(ctx, sk.DSN)
			if err != nil {
				log.Printf("catacomb: postgres sink disabled: %v", err)
				continue
			}
			out = append(out, exp)
		case config.SinkNeo4j:
			if b.NewNeo4j == nil {
				continue
			}
			exp, err := b.NewNeo4j(ctx, sk.URI, sk.User, sk.Password)
			if err != nil {
				log.Printf("catacomb: neo4j sink disabled: %v", err)
				continue
			}
			out = append(out, exp)
		case config.SinkJSONL:
			if b.NewJSONL == nil {
				continue
			}
			exp, err := b.NewJSONL(sk.Path)
			if err != nil {
				log.Printf("catacomb: jsonl sink disabled: %v", err)
				continue
			}
			out = append(out, exp)
		default:
			return nil, fmt.Errorf("export/build.Build: %w: %q", config.ErrUnknownSink, sk.Type)
		}
	}
	return out, nil
}
```

- [ ] **Run** `go test -race ./export/build/` — expect PASS.
- [ ] **Run** `go test -race -coverpkg=./export/build/... -coverprofile=cover.out ./export/build/ && go tool cover -func=cover.out | grep build` — expect 100%.
- [ ] **Run** `go build ./...` — expect clean compile (no import cycle).
- [ ] **Commit:** `git add export/build/build.go export/build/build_test.go && git commit -m "feat(export/build): Build sink factory, avoids export/* import cycle

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"`

---

## Task 3: daemon — sinks/sources wiring (fields + SetSinks/SetSources + buildFn + route/tail gating)

This task combines what were formerly two separate tasks into one atomic unit. `daemon.go` and `daemon/server.go` must be changed together because removing the old per-field Set methods and vars (`SetOTLPEndpoint`, `newExporterFn`, etc.) from one file breaks the other until both files are updated. Every step here ends with a compilable, testable state.

**Files:**

- Modify: `daemon/daemon.go`
- Modify: `daemon/daemon_test.go`
- Modify: `daemon/server.go`
- Modify: `daemon/server_test.go`

**Interfaces produced:**

- `daemon.Daemon` struct changes:
  - **Remove fields**: `otlpEndpoint string`, `otlpProject string`, `postgresDSN string`, `neo4jURI string`, `neo4jUser string`, `neo4jPassword string`, `transcriptDir string`, `transcriptExclude []string`.
  - **Add fields**: `sinks []config.Sink`, `sources config.SourcesConfig`.
  - **Keep unchanged**: `exporterConsumers []*cdc.Consumer`, `dbPath string`, `bus *cdc.Bus`, `graphs map[string]*reduce.Graph`, all other existing fields.
- **Remove methods** from `daemon.go`: `SetOTLPEndpoint`, `SetOTLPProject`, `SetPostgresDSN`, `SetNeo4j`, `SetTranscriptDir`, `SetTranscriptExclude`.
- **Add methods** to `daemon.go`:
  - `func (d *Daemon) SetSinks(sinks []config.Sink)`
  - `func (d *Daemon) SetSources(cfg config.SourcesConfig)`
- **`server.go` var changes**:
  - Remove: `var newExporterFn`, `var newPostgresFn`, `var newNeo4jFn`.
  - Add: `var buildFn func(ctx context.Context, sinks []config.Sink, daemonGRPCAddr, daemonHTTPAddr string) ([]exportiface.Exporter, error) = build.Build`.
- **`server.go` import changes**: remove `neo4jexport`, `otlp`, `pgexport`; add `"github.com/realkarych/catacomb/export/build"` and `"github.com/realkarych/catacomb/config"`.
- **`startExporter`**: reads `d.sinks`, calls `buildFn(ctx, sinks, grpcAddr, httpAddr)`; log+return on error; proceed if `len(entries) > 0`; fan-out loop body identical to current (snapshot, flush terminal, subscribe bus, consumer goroutine).
- **`tailLoop`**: reads `d.sources`; returns early if `src.JSONL.Enabled == nil || !*src.JSONL.Enabled`; returns early if `src.JSONL.TranscriptDir == ""`; builds excludes from `append([]string{dbPath, cwdTranscriptExclude()}, src.JSONL.Exclude...)`.
- **`Handler`**: reads `d.sources` under lock; helper `enabled(b *bool) bool { return b == nil || *b }`; gates `POST /hook/{type}` on `enabled(src.Hooks.Enabled)`, `POST /v1/traces` on `enabled(src.Otel.Enabled)`, `POST /v1/stream-json` on `enabled(src.StreamJSON.Enabled)`; always registers all other routes.

Steps:

- [ ] **Write NEW failing tests** in `daemon/daemon_test.go` (add after existing tests):

```go
func TestSetSinks(t *testing.T) {
	d := New(tempStore(t))
	sinks := []config.Sink{{Type: config.SinkOTLP, Endpoint: "grpc://host:4317"}}
	d.SetSinks(sinks)
	d.mu.Lock()
	got := d.sinks
	d.mu.Unlock()
	require.Len(t, got, 1)
	assert.Equal(t, config.SinkOTLP, got[0].Type)
}

func TestSetSources(t *testing.T) {
	d := New(tempStore(t))
	enabled := true
	src := config.SourcesConfig{
		Hooks: config.SourceToggle{Enabled: &enabled},
		JSONL: config.JSONLSource{Enabled: &enabled, TranscriptDir: "/t"},
	}
	d.SetSources(src)
	d.mu.Lock()
	got := d.sources
	d.mu.Unlock()
	require.NotNil(t, got.Hooks.Enabled)
	assert.True(t, *got.Hooks.Enabled)
	assert.Equal(t, "/t", got.JSONL.TranscriptDir)
}
```

Also add `"github.com/realkarych/catacomb/config"` to `daemon_test.go` imports.

- [ ] **Write NEW failing tests** in `daemon/server_test.go` (add after existing tests). Also add `"github.com/realkarych/catacomb/config"` to server_test.go imports:

```go
func TestHandlerHookGatedWhenDisabled(t *testing.T) {
	d := New(tempStore(t))
	disabled := false
	d.SetSources(config.SourcesConfig{Hooks: config.SourceToggle{Enabled: &disabled}})
	token := "tok"
	h := d.Handler(token)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/hook/PreToolUse", strings.NewReader(`{"session_id":"s1"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandlerOtelGatedWhenDisabled(t *testing.T) {
	d := New(tempStore(t))
	disabled := false
	d.SetSources(config.SourcesConfig{Otel: config.SourceToggle{Enabled: &disabled}})
	token := "tok"
	h := d.Handler(token)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/traces", strings.NewReader(""))
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandlerStreamJSONGatedWhenDisabled(t *testing.T) {
	d := New(tempStore(t))
	disabled := false
	d.SetSources(config.SourcesConfig{StreamJSON: config.SourceToggle{Enabled: &disabled}})
	token := "tok"
	h := d.Handler(token)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/stream-json", strings.NewReader(""))
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandlerHookEnabledWhenNilToggle(t *testing.T) {
	d := New(tempStore(t))
	d.SetSources(config.SourcesConfig{})
	token := "tok"
	h := d.Handler(token)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/hook/PreToolUse", strings.NewReader(`{"session_id":"s1"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestTailLoopNoopWhenJSONLDisabled(t *testing.T) {
	d := New(tempStore(t))
	disabled := false
	d.SetSources(config.SourcesConfig{JSONL: config.JSONLSource{Enabled: &disabled, TranscriptDir: t.TempDir()}})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	d.tailLoop(ctx)
}

func TestTailLoopNoopWhenNoTranscriptDir(t *testing.T) {
	d := New(tempStore(t))
	enabled := true
	d.SetSources(config.SourcesConfig{JSONL: config.JSONLSource{Enabled: &enabled, TranscriptDir: ""}})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	d.tailLoop(ctx)
}

func TestServeStartsExporterConsumerViaBuildFn(t *testing.T) {
	fake := &fakeSpanExporter{}
	orig := buildFn
	t.Cleanup(func() { buildFn = orig })
	called := make(chan struct{}, 1)
	buildFn = func(_ context.Context, sinks []config.Sink, _, _ string) ([]exportiface.Exporter, error) {
		if len(sinks) > 0 {
			called <- struct{}{}
		}
		return []exportiface.Exporter{otlp.ExporterWithSpanExporter(fake)}, nil
	}
	d := New(tempStore(t))
	d.SetSinks([]config.Sink{{Type: config.SinkOTLP, Endpoint: "grpc://collector.example:4317"}})
	httpLn, grpcLn := loopback(t), loopback(t)
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- d.Serve(ctx, httpLn, grpcLn, "tok") }()
	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("buildFn not called")
	}
	cancel()
	require.NoError(t, <-errc)
}
```

- [ ] **Run** `go test ./daemon/ -run 'TestSetSinks|TestSetSources|TestHandlerHookGated|TestHandlerOtel|TestHandlerStream|TestHandlerHookEnabled|TestTailLoop|TestServeStartsExporterConsumerViaBuildFn'` — expect FAIL (compile errors: `d.sinks`, `d.sources`, `d.SetSinks`, `d.SetSources`, `buildFn` all undefined; existing references to old vars/methods still compile but will break).

- [ ] **Apply ALL daemon.go changes**:
  - Remove from `type Daemon struct`: `otlpEndpoint string`, `otlpProject string`, `postgresDSN string`, `neo4jURI string`, `neo4jUser string`, `neo4jPassword string`, `transcriptDir string`, `transcriptExclude []string`.
  - Add to `type Daemon struct`: `sinks []config.Sink`, `sources config.SourcesConfig`.
  - Remove methods: `SetOTLPEndpoint`, `SetOTLPProject`, `SetPostgresDSN`, `SetNeo4j`, `SetTranscriptDir`, `SetTranscriptExclude`.
  - Add methods:

```go
func (d *Daemon) SetSinks(sinks []config.Sink) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.sinks = sinks
}

func (d *Daemon) SetSources(cfg config.SourcesConfig) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.sources = cfg
}
```

  - Add import `"github.com/realkarych/catacomb/config"` to daemon.go import block.
  - `metricsSnapshot` still references `d.exporterConsumers` (unchanged) — no edit needed there.

- [ ] **Apply ALL server.go changes**:
  - Remove: `var newExporterFn = otlp.New`, `var newPostgresFn = func(...)`, `var newNeo4jFn = func(...)`.
  - Add: `var buildFn func(ctx context.Context, sinks []config.Sink, daemonGRPCAddr, daemonHTTPAddr string) ([]exportiface.Exporter, error) = build.Build`.
  - Add imports: `"github.com/realkarych/catacomb/config"`, `"github.com/realkarych/catacomb/export/build"`.
  - Remove imports: `neo4jexport "github.com/realkarych/catacomb/export/neo4j"`, `"github.com/realkarych/catacomb/export/otlp"`, `pgexport "github.com/realkarych/catacomb/export/postgres"`.
  - Replace `startExporter` body:

```go
func (d *Daemon) startExporter(ctx context.Context, httpAddr, grpcAddr string) {
	d.mu.Lock()
	sinks := d.sinks
	d.mu.Unlock()

	entries, err := buildFn(ctx, sinks, grpcAddr, httpAddr)
	if err != nil {
		log.Printf("catacomb: sink build error: %v", err)
		return
	}
	if len(entries) == 0 {
		return
	}

	d.mu.Lock()
	for _, exp := range entries {
		for _, g := range d.graphs {
			nodes, edges := g.Snapshot()
			cp := make([]*model.Node, len(nodes))
			for i, n := range nodes {
				cp[i] = copyNode(n)
			}
			_ = exp.SnapshotState(ctx, cp, edges)
			if re, ok := exp.(exportiface.RunExporter); ok {
				_ = re.SnapshotRuns(ctx, g.RunsSnapshot())
			}
		}
		for _, g := range d.graphs {
			for _, r := range g.RunsSnapshot() {
				if r.EndedAt != nil {
					_ = exp.FlushRun(ctx, r.ID)
				}
			}
		}
		consumer := d.bus.Subscribe(exporterBufSize)
		d.exporterConsumers = append(d.exporterConsumers, consumer)
		go func(c *cdc.Consumer, ex exportiface.Exporter) {
			for {
				select {
				case <-ctx.Done():
					d.bus.Unsubscribe(c)
					_ = ex.Shutdown(ctx)
					return
				case delta, ok := <-c.C:
					if !ok {
						if consumerLoopExitHook != nil {
							consumerLoopExitHook()
						}
						return
					}
					_ = ex.ApplyDelta(ctx, delta)
				}
			}
		}(consumer, exp)
	}
	d.mu.Unlock()
}
```

  - Replace `tailLoop` body:

```go
func (d *Daemon) tailLoop(ctx context.Context) {
	d.mu.Lock()
	src := d.sources
	dbPath := d.dbPath
	d.mu.Unlock()
	if src.JSONL.Enabled == nil || !*src.JSONL.Enabled {
		return
	}
	if src.JSONL.TranscriptDir == "" {
		return
	}
	excludes := append([]string{dbPath, cwdTranscriptExclude()}, src.JSONL.Exclude...)
	tl := tailingest.New(src.JSONL.TranscriptDir, excludes, d.store, d)
	if err := tl.Load(); err != nil {
		log.Printf("catacomb: tailer load: %v", err)
		return
	}
	tl.Run(ctx, tailTick)
}
```

  - Replace `Handler` body:

```go
func (d *Daemon) Handler(token string) http.Handler {
	d.mu.Lock()
	src := d.sources
	d.mu.Unlock()

	enabled := func(b *bool) bool { return b == nil || *b }
	mux := http.NewServeMux()
	if enabled(src.Hooks.Enabled) {
		mux.HandleFunc("POST /hook/{type}", d.authed(token, d.handleHook))
	}
	if enabled(src.Otel.Enabled) {
		mux.HandleFunc("POST /v1/traces", d.authed(token, d.handleOTLP))
	}
	if enabled(src.StreamJSON.Enabled) {
		mux.HandleFunc("POST /v1/stream-json", d.authed(token, d.handleStreamJSON))
	}
	mux.HandleFunc("POST /v1/transcript", d.authed(token, d.handleTranscript))
	mux.HandleFunc("POST /v1/mark", d.authed(token, d.handleMark))
	mux.HandleFunc("GET /v1/subscribe", d.authedAllowQuery(token, d.handleSSE))
	mux.HandleFunc("GET /v1/sessions", d.authedAllowQuery(token, d.handleSessions))
	mux.HandleFunc("GET /v1/sessions/{hash}/graph", d.authedAllowQuery(token, d.handleSessionGraph))
	mux.HandleFunc("GET /v1/diff", d.authedAllowQuery(token, d.handleDiff))
	mux.HandleFunc("GET /v1/sessions/{hash}/nodes/{nodeId}/payload", d.authedAllowQuery(token, d.handleNodePayload))
	mux.HandleFunc("GET /v1/sessions/{hash}/subagent/{agentId}", d.authedAllowQuery(token, d.handleSubagentSubtree))
	mux.HandleFunc("POST /v1/sessions/{hash}/nodes/{nodeId}/annotations", d.authed(token, d.handleNodeAnnotate))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(d.metricsSnapshot())
	})
	mux.Handle("GET /", webui.Handler())
	return mux
}
```

- [ ] **Update daemon_test.go** — remove these five tests that reference deleted methods: `TestSetOTLPEndpoint`, `TestSetOTLPProject`, `TestSetTranscriptDir`, `TestSetTranscriptExclude`, `TestSetNeo4jStoresFields`.

- [ ] **Update server_test.go** — apply the following changes:

  **Remove** `TestNewNeo4jFnWrapperLine` (tests `newNeo4jFn` which is deleted; coverage of neo4j.New is in `export/build/build_test.go` via the real integration test in live-verify Task 6).

  **Injection pattern**: For every test that previously injected `newExporterFn` / `newPostgresFn` / `newNeo4jFn`, replace with `buildFn` injection. For every call to `d.SetOTLPEndpoint` / `d.SetOTLPProject` / `d.SetPostgresDSN` / `d.SetNeo4j`, replace with `d.SetSinks(...)` using the appropriate `config.Sink` value.

  **Pattern A — single OTLP exporter** (applies to `TestServeStartsExporterConsumer`, `TestServeExporterSnapshotsExistingGraphs`, `TestExporterConsumerLoopExitsOnChannelClose`, `TestStartExporterFlushesTerminalRunsOnAttach`, `TestStartExporterSnapshotIsolatesPayload`):

  ```go
  fake := &fakeSpanExporter{}
  orig := buildFn
  buildFn = func(_ context.Context, _ []config.Sink, _, _ string) ([]exportiface.Exporter, error) {
      return []exportiface.Exporter{otlp.ExporterWithSpanExporter(fake)}, nil
  }
  t.Cleanup(func() { buildFn = orig })
  // replace d.SetOTLPEndpoint("grpc://collector.example:4317") with:
  d.SetSinks([]config.Sink{{Type: config.SinkOTLP, Endpoint: "grpc://collector.example:4317"}})
  ```

  **Pattern B — self-loop / buildFn returns nil** (applies to `TestServeSelfLoopEndpointSkipsExporter`):

  ```go
  var called atomic.Bool
  orig := buildFn
  buildFn = func(_ context.Context, sinks []config.Sink, _, _ string) ([]exportiface.Exporter, error) {
      called.Store(true)
      return nil, nil
  }
  t.Cleanup(func() { buildFn = orig })
  d.SetSinks([]config.Sink{{Type: config.SinkOTLP, Endpoint: "grpc://" + grpcLn.Addr().String()}})
  ```

  **Pattern C — two exporters** (applies to `TestStartExporterAttachesTwoConsumersWhenBothConfigured`, `TestWiringOTLPAndPostgresRunTogether`):

  ```go
  fake := &fakeSpanExporter{}
  fakeExp := &fakeExporter{}
  orig := buildFn
  buildFn = func(_ context.Context, _ []config.Sink, _, _ string) ([]exportiface.Exporter, error) {
      return []exportiface.Exporter{otlp.ExporterWithSpanExporter(fake), fakeExp}, nil
  }
  t.Cleanup(func() { buildFn = orig })
  d.SetSinks([]config.Sink{
      {Type: config.SinkOTLP, Endpoint: "grpc://collector.example:4317"},
      {Type: config.SinkPostgres, DSN: "postgres://localhost/test"},
  })
  ```

  **Pattern D — single postgres exporter** (applies to `TestWiringPostgresDSNAttachesExporterAndReceivesDelta`, `TestSnapshotReceivedByPostgresExporterOnAttach`, `TestStartExporterCallsSnapshotRunsForRunExporter`, `TestStartExporterRunDeltaCarriesRunToApplyDelta`):

  ```go
  fakeExp := &fakeExporter{} // or &fakeRunExporter{} where SnapshotRuns is tested
  orig := buildFn
  buildFn = func(_ context.Context, _ []config.Sink, _, _ string) ([]exportiface.Exporter, error) {
      return []exportiface.Exporter{fakeExp}, nil
  }
  t.Cleanup(func() { buildFn = orig })
  d.SetSinks([]config.Sink{{Type: config.SinkPostgres, DSN: "postgres://localhost/test"}})
  ```

  **Pattern E — empty sinks, buildFn not called** (applies to `TestWiringEmptyPostgresDSNAttachesNothing`, `TestWiringEmptyNeo4jURIAttachesNothing`):

  ```go
  called := false
  orig := buildFn
  buildFn = func(_ context.Context, sinks []config.Sink, _, _ string) ([]exportiface.Exporter, error) {
      if len(sinks) > 0 { called = true }
      return nil, nil
  }
  t.Cleanup(func() { buildFn = orig })
  // do NOT call d.SetSinks (leaves sinks nil → buildFn returns nil → no consumers)
  ```

  **Pattern F — error results in reduced consumers** (applies to `TestStartExporterPostgresErrorDisablesOnlyPostgres`, `TestStartExporterNeo4jErrorDisablesOnlyNeo4j`). These tests verified that one failing sink doesn't prevent another from attaching. This behavior now lives in `export/build.Build` (tested in Task 2). Rewrite to verify `startExporter` creates exactly as many consumers as `buildFn` returns:

  ```go
  fake := &fakeSpanExporter{}
  orig := buildFn
  buildFn = func(_ context.Context, _ []config.Sink, _, _ string) ([]exportiface.Exporter, error) {
      return []exportiface.Exporter{otlp.ExporterWithSpanExporter(fake)}, nil
  }
  t.Cleanup(func() { buildFn = orig })
  d.SetSinks([]config.Sink{
      {Type: config.SinkOTLP, Endpoint: "grpc://collector.example:4317"},
      {Type: config.SinkPostgres, DSN: "postgres://localhost/fail"},
  })
  d.startExporter(ctx, httpLn.Addr().String(), grpcLn.Addr().String())
  // buildFn returned 1 exporter → 1 consumer
  assert.Equal(t, 1, len(d.exporterConsumers))
  ```

  **Special case: `TestStartExporterProjectPropagates`** — project setting now happens inside `build.Build` via type assertion. In the daemon test, inject `buildFn` that creates the exporter and calls `SetProject` itself (reflecting that the build layer owns project propagation):

  ```go
  func TestStartExporterProjectPropagates(t *testing.T) {
      fakeSpan := &fakeSpanExporter{}
      orig := buildFn
      buildFn = func(_ context.Context, sinks []config.Sink, _, _ string) ([]exportiface.Exporter, error) {
          exp := otlp.ExporterWithSpanExporter(fakeSpan)
          for _, sk := range sinks {
              if sk.Type == config.SinkOTLP && sk.Project != "" {
                  exp.SetProject(sk.Project)
              }
          }
          return []exportiface.Exporter{exp}, nil
      }
      t.Cleanup(func() { buildFn = orig })
      d := New(tempStore(t))
      fixedExecID(d)
      d.SetSinks([]config.Sink{{Type: config.SinkOTLP, Endpoint: "grpc://collector.example:4317", Project: "proj-x"}})
      require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
      httpLn, grpcLn := loopback(t), loopback(t)
      ctx, cancel := context.WithCancel(context.Background())
      errc := make(chan error, 1)
      go func() { errc <- d.Serve(ctx, httpLn, grpcLn, "tok") }()
      require.Eventually(t, func() bool {
          return len(d.ExporterConsumersForTest()) > 0
      }, 30*time.Second, 5*time.Millisecond)
      require.NoError(t, d.Ingest("SessionEnd", []byte(`{"session_id":"s1","reason":"clear"}`)))
      require.Eventually(t, func() bool { return fakeSpan.spanCount() > 0 }, 3*time.Second, 5*time.Millisecond)
      fakeSpan.mu.Lock()
      spans := append([]sdktrace.ReadOnlySpan{}, fakeSpan.spans...)
      fakeSpan.mu.Unlock()
      var projectName string
      for _, sp := range spans {
          for _, kv := range sp.Resource().Attributes() {
              if string(kv.Key) == "openinference.project.name" {
                  projectName = kv.Value.AsString()
              }
          }
      }
      assert.Equal(t, "proj-x", projectName)
      cancel()
      require.NoError(t, <-errc)
  }
  ```

  **Special case: `TestServeStartsTailerIngestsTranscript`** — replace `d.SetTranscriptDir(dir)` with `d.SetSources`:

  ```go
  enabled := true
  d.SetSources(config.SourcesConfig{JSONL: config.JSONLSource{Enabled: &enabled, TranscriptDir: dir}})
  ```

  **Special case: `TestTailLoopDisabledWhenNoDir`** — currently tests `tailLoop` returns when `d.transcriptDir == ""`. After refactor this becomes: JSONL `Enabled` nil (not set) should not panic and should not tail. The JSONL nil case already returns because `src.JSONL.Enabled == nil` → return. Verify the existing test still passes with no changes needed (it just calls `d.tailLoop(context.Background())` on a fresh daemon — `sources` will be zero value, `JSONL.Enabled == nil` → return early). No edit needed; this test passes unchanged.

  **Special case: `TestTailLoopLoadError`** — currently sets `d.SetTranscriptDir(dir)`. Replace with:

  ```go
  enabled := true
  d.SetSources(config.SourcesConfig{JSONL: config.JSONLSource{Enabled: &enabled, TranscriptDir: dir}})
  ```

- [ ] **Run** `go test -race ./daemon/` — expect PASS (all existing tests updated + all new tests pass).
- [ ] **Run** `go build ./...` — expect clean compile.
- [ ] **Verify** `grep -rn 'SetOTLPEndpoint\|SetOTLPProject\|SetPostgresDSN\|SetNeo4j\|SetTranscriptDir\|SetTranscriptExclude\|newExporterFn\|newPostgresFn\|newNeo4jFn' daemon/` returns no matches.
- [ ] **Commit:** `git add daemon/daemon.go daemon/daemon_test.go daemon/server.go daemon/server_test.go && git commit -m "refactor(daemon): buildFn factory, source-gated routes, JSONL source-gated tail

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"`

---

## Task 4: `cmd/catacomb/daemon.go` — daemonParams extension + runDaemonWith + RunE flag reconciliation

**Files:**

- Modify: `cmd/catacomb/daemon.go`
- Modify: `cmd/catacomb/daemon_test.go`

**Interfaces:**

- `daemonParams` (modify existing): add fields `sinks []config.Sink` and `sources config.SourcesConfig`. Keep all existing fields (`store`, `discoveryPath`, `reaperWindow`, `maxShards`, `otlpEndpoint`, `otlpProject`, `postgresDSN`, `neo4jURI`, `neo4jUser`, `neo4jPassword`, `transcriptDir`, `transcriptExclude`, `allowPayloadAccess`, `allowAnnotations`) — these are the legacy flag fields used as highest-precedence overrides.
- `runDaemonWith(ctx, deps, p daemonParams)`:
  - Removes calls to the deleted methods: `d.SetOTLPEndpoint`, `d.SetOTLPProject`, `d.SetPostgresDSN`, `d.SetNeo4j`, `d.SetTranscriptDir`, `d.SetTranscriptExclude`.
  - Builds merged sink list from config sinks + legacy flag sinks; calls `d.SetSinks`.
  - Builds merged sources from config sources + legacy transcript flag; calls `d.SetSources`.
  - Keeps existing structure for store open, listener setup, discovery write, `d.Recover()`, `d.Serve(...)`.
  - MUST preserve `_ = os.Remove(p.discoveryPath)` after `d.Serve(...)` returns (added in PR #75).
- `RunE` in `newDaemonCmd`: passes `cfg.Sinks` and `cfg.Sources` into `params`; applies `cmd.Flags().Changed(...)` transcript flag overrides before building params; adds deprecation text to 4 flag help strings.

Steps:

- [ ] **Write failing tests** in `cmd/catacomb/daemon_test.go`. Add `boolPtr` and `readToken` helpers (neither exists in this file yet):

```go
func boolPtr(b bool) *bool { return &b }

func readToken(t *testing.T, discovery string) string {
	t.Helper()
	var tok string
	require.Eventually(t, func() bool {
		d, err := daemon.ReadDiscovery(discovery)
		if err != nil {
			return false
		}
		tok = d.Token
		return tok != ""
	}, 30*time.Second, 10*time.Millisecond)
	return tok
}

func TestRunDaemonWithSinksFromParams(t *testing.T) {
	discovery := filepath.Join(t.TempDir(), "d.json")
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	p := testDaemonParams(t)
	p.discoveryPath = discovery
	p.sinks = []config.Sink{{Type: config.SinkJSONL, Path: filepath.Join(t.TempDir(), "out.jsonl")}}
	go func() { errc <- runDaemonWith(ctx, testDaemonDeps(), p) }()
	awaitHealthz(t, readAddr(t, discovery))
	cancel()
	require.NoError(t, <-errc)
}

func TestRunDaemonWithLegacyOTLPFlagMergedAsSink(t *testing.T) {
	discovery := filepath.Join(t.TempDir(), "d.json")
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	p := testDaemonParams(t)
	p.discoveryPath = discovery
	p.otlpEndpoint = "grpc://collector.example:4317"
	p.otlpProject = "test-proj"
	go func() { errc <- runDaemonWith(ctx, testDaemonDeps(), p) }()
	awaitHealthz(t, readAddr(t, discovery))
	cancel()
	require.NoError(t, <-errc)
}

func TestRunDaemonSourcesFromParams(t *testing.T) {
	discovery := filepath.Join(t.TempDir(), "d.json")
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	p := testDaemonParams(t)
	p.discoveryPath = discovery
	p.sources = config.SourcesConfig{
		Hooks:      config.SourceToggle{Enabled: boolPtr(true)},
		Otel:       config.SourceToggle{Enabled: boolPtr(false)},
		StreamJSON: config.SourceToggle{Enabled: boolPtr(true)},
		JSONL:      config.JSONLSource{Enabled: boolPtr(false)},
	}
	go func() { errc <- runDaemonWith(ctx, testDaemonDeps(), p) }()
	addr := readAddr(t, discovery)
	awaitHealthz(t, addr)
	req, err := http.NewRequest("POST", "http://"+addr+"/v1/traces", strings.NewReader(""))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+readToken(t, discovery))
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	cancel()
	require.NoError(t, <-errc)
}

func TestRunDaemonTranscriptFlagOverridesSource(t *testing.T) {
	discovery := filepath.Join(t.TempDir(), "d.json")
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	p := testDaemonParams(t)
	p.discoveryPath = discovery
	p.transcriptDir = t.TempDir()
	p.transcriptExclude = []string{"*.tmp"}
	go func() { errc <- runDaemonWith(ctx, testDaemonDeps(), p) }()
	awaitHealthz(t, readAddr(t, discovery))
	cancel()
	require.NoError(t, <-errc)
}
```

Also add `"strings"` to daemon_test.go imports if not already present.

- [ ] **Run** `go test ./cmd/catacomb/ -run 'TestRunDaemonWithSinks|TestRunDaemonWithLegacy|TestRunDaemonSources|TestRunDaemonTranscript'` — expect FAIL (compile: `p.sinks` undefined on `daemonParams`; `d.SetOTLPEndpoint` still called in runDaemonWith).

- [ ] **Modify** `cmd/catacomb/daemon.go`:
  - Add `sinks []config.Sink` and `sources config.SourcesConfig` to `daemonParams`.
  - Replace the body of `runDaemonWith`:

```go
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
	d.SetDBPath(dbPath)
	d.SetAllowPayloadAccess(p.allowPayloadAccess)
	d.SetAllowAnnotations(p.allowAnnotations)
	d.SetReproConfig(repro.Config{
		OTLPEndpoint:  p.otlpEndpoint,
		OTLPProject:   p.otlpProject,
		TranscriptDir: p.transcriptDir,
	})

	sinks := append([]config.Sink(nil), p.sinks...)
	if p.otlpEndpoint != "" {
		sinks = append(sinks, config.Sink{Type: config.SinkOTLP, Endpoint: p.otlpEndpoint, Project: p.otlpProject})
	}
	if p.postgresDSN != "" {
		sinks = append(sinks, config.Sink{Type: config.SinkPostgres, DSN: p.postgresDSN})
	}
	if p.neo4jURI != "" {
		sinks = append(sinks, config.Sink{Type: config.SinkNeo4j, URI: p.neo4jURI, User: p.neo4jUser, Password: p.neo4jPassword})
	}
	d.SetSinks(sinks)

	sources := p.sources
	if p.transcriptDir != "" {
		enabled := true
		sources.JSONL.Enabled = &enabled
		sources.JSONL.TranscriptDir = p.transcriptDir
		if p.transcriptExclude != nil {
			sources.JSONL.Exclude = p.transcriptExclude
		}
	}
	d.SetSources(sources)

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
	if err = daemon.WriteDiscovery(p.discoveryPath, disc); err != nil {
		return err
	}
	err = d.Serve(ctx, ln, grpcLn, token)
	_ = os.Remove(p.discoveryPath)
	return err
}
```

  - Update `RunE` in `newDaemonCmd` to pass `cfg.Sinks` and `cfg.Sources` + apply transcript flag override before building params. After the `resolveConfig(...)` call and before building `params`:

```go
if cmd.Flags().Changed("transcript-dir") {
	enabled := true
	cfg.Sources.JSONL.Enabled = &enabled
	cfg.Sources.JSONL.TranscriptDir = transcriptDir
}
if cmd.Flags().Changed("transcript-exclude") {
	cfg.Sources.JSONL.Exclude = transcriptExclude
}
```

  And build `params` with `sinks: cfg.Sinks, sources: cfg.Sources`:

```go
params := daemonParams{
	store:              cfg.Store,
	sinks:              cfg.Sinks,
	sources:            cfg.Sources,
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
```

  - Add deprecation notice to flag help text for 4 flags: `--otlp-export-endpoint`, `--postgres-export-dsn`, `--neo4j-export-uri`, `--transcript-dir`. Append `[deprecated: prefer sinks in config.yaml]` or `[deprecated: prefer sources.jsonl in config.yaml]` to the respective `cmd.Flags().StringVar(...)` description strings.

- [ ] **Run** `go test -race ./cmd/catacomb/` — expect PASS (all existing + new tests).
- [ ] **Verify** `grep -n 'SetOTLPEndpoint\|SetPostgresDSN\|SetNeo4j\|SetTranscriptDir' cmd/catacomb/daemon.go` returns no matches.
- [ ] **Commit:** `git add cmd/catacomb/daemon.go cmd/catacomb/daemon_test.go && git commit -m "feat(cmd): wire cfg.Sinks+cfg.Sources into daemon, legacy flags as append-overrides

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"`

---

## Task 5: Batch-CLI config integration — `resolveStorePath` + wire into runs/inspect/snapshot/export

**Files:**

- Create: `cmd/catacomb/batchconfig.go`
- Create: `cmd/catacomb/batchconfig_test.go`
- Modify: `cmd/catacomb/runs.go` (`--db` default)
- Modify: `cmd/catacomb/inspect.go` (`--db` default)
- Modify: `cmd/catacomb/snapshot.go` (`--db` default)
- Modify: `cmd/catacomb/export.go` (`--db` default)

**Interfaces:**

- Produces (package `main`):
  - `func resolveStorePath(readFile func(string) ([]byte, error), lookupEnv func(string) (string, bool), home string) string` — runs `defaults < file < env` (no flag layer; flags applied via `--db` override at cobra level). Returns the resolved, expanded SQLite path. On any error (file parse, validate), falls back silently to `config.ExpandPath(config.DefaultSQLitePath, home, getenv)`.
  - `func defaultBatchDBPath() string` — calls `resolveStorePath(os.ReadFile, os.LookupEnv, home)` with real I/O; falls back to `defaultDBPath()` if home is unresolvable.
- Consumes: `config.Defaults`, `config.Parse`, `config.Merge`, `config.FromEnv`, `config.ExpandPath`, `config.DefaultSQLitePath`, `configFilePath` (reuse from `config_resolve.go`), `osUserHomeDir`.

Steps:

- [ ] **Write failing test** `cmd/catacomb/batchconfig_test.go`:

```go
package main

import (
	"io/fs"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveStorePathDefaultsWhenNoFile(t *testing.T) {
	read := func(string) ([]byte, error) { return nil, fs.ErrNotExist }
	got := resolveStorePath(read, envLookup(nil), "/home/u")
	assert.Equal(t, "/home/u/.catacomb/catacomb.db", got)
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
	assert.Equal(t, "/home/u/sub/db.db", got)
}

func TestResolveStorePathFallsBackOnBadConfig(t *testing.T) {
	read := func(string) ([]byte, error) {
		return []byte("store:\n  nope_key: 1\n"), nil
	}
	got := resolveStorePath(read, envLookup(nil), "/home/u")
	assert.Equal(t, "/home/u/.catacomb/catacomb.db", got)
}

func TestResolveStorePathFallsBackOnReadError(t *testing.T) {
	read := func(string) ([]byte, error) { return nil, fs.ErrPermission }
	got := resolveStorePath(read, envLookup(nil), "/home/u")
	assert.Equal(t, "/home/u/.catacomb/catacomb.db", got)
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
```

Note: `envLookup` is already defined in `cmd/catacomb/config_resolve_test.go` (package `main`), so no duplicate needed.

- [ ] **Run** `go test ./cmd/catacomb/ -run 'TestResolveStorePath|TestDefaultBatch|TestBatchCommands'` — expect FAIL (`undefined: resolveStorePath`, `undefined: defaultBatchDBPath`; batch commands still use `defaultDBPath()`).

- [ ] **Create** `cmd/catacomb/batchconfig.go`:

```go
package main

import (
	"errors"
	"os"

	"github.com/realkarych/catacomb/config"
)

func resolveStorePath(readFile func(string) ([]byte, error), lookupEnv func(string) (string, bool), home string) string {
	getenv := func(k string) string { v, _ := lookupEnv(k); return v }
	cfg := config.Defaults()
	data, err := readFile(configFilePath(daemonFlags{}, lookupEnv, home))
	switch {
	case err == nil:
		fileCfg, perr := config.Parse(data)
		if perr != nil {
			return config.ExpandPath(config.DefaultSQLitePath, home, getenv)
		}
		cfg = config.Merge(cfg, fileCfg)
	case errors.Is(err, os.ErrNotExist):
	default:
		return config.ExpandPath(config.DefaultSQLitePath, home, getenv)
	}
	cfg = config.Merge(cfg, config.FromEnv(lookupEnv))
	return config.ExpandPath(cfg.Store.SQLite.Path, home, getenv)
}

func defaultBatchDBPath() string {
	home, err := osUserHomeDir()
	if err != nil {
		return defaultDBPath()
	}
	return resolveStorePath(os.ReadFile, os.LookupEnv, home)
}
```

- [ ] **Modify** `cmd/catacomb/runs.go` — change the `--db` flag default value from `defaultDBPath()` to `defaultBatchDBPath()`.
- [ ] **Modify** `cmd/catacomb/inspect.go` — same.
- [ ] **Modify** `cmd/catacomb/snapshot.go` — same.
- [ ] **Modify** `cmd/catacomb/export.go` — same.
- [ ] **Run** `go test -race ./cmd/catacomb/` — expect PASS.
- [ ] **Run** `make cover` — expect overall coverage gate green.
- [ ] **Commit:** `git add cmd/catacomb/batchconfig.go cmd/catacomb/batchconfig_test.go cmd/catacomb/runs.go cmd/catacomb/inspect.go cmd/catacomb/snapshot.go cmd/catacomb/export.go && git commit -m "feat(cmd): batch CLI reads store.sqlite.path from config.yaml (resolveStorePath)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"`

---

## Task 6: Mandatory live-verify (real daemon, spec §12)

**Files:** No source changes. Scripts and observations only.

**Purpose:** Green 100% unit coverage has missed real bugs in this codebase before. This task verifies the end-to-end wiring under live conditions.

Steps:

- [ ] **Build**: `make build` — expect `./bin/catacomb` produced, clean.

- [ ] **Scenario A — config sinks fan-out (JSONL sink)**

```sh
mkdir -p /tmp/cat-pr2
cat > /tmp/cat-pr2/config.yaml <<'EOF'
store:
  backend: sqlite
  sqlite:
    path: /tmp/cat-pr2/cat.db
sources:
  hooks:       { enabled: true }
  otel:        { enabled: true }
  stream_json: { enabled: true }
  jsonl:
    enabled: false
sinks:
  - { type: jsonl, path: /tmp/cat-pr2/sink.jsonl }
EOF
./bin/catacomb daemon --config /tmp/cat-pr2/config.yaml --discovery /tmp/cat-pr2/d.json &
DAEMON_PID=$!
sleep 1
ADDR=$(python3 -c "import json; d=json.load(open('/tmp/cat-pr2/d.json')); print(d['addr'])")
TOKEN=$(python3 -c "import json; d=json.load(open('/tmp/cat-pr2/d.json')); print(d['token'])")
curl -s -o /dev/null -w "%{http_code}" http://$ADDR/healthz
curl -s -X POST -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"session_id":"s1","hook_type":"PreToolUse","tool_use_id":"t1"}' \
  http://$ADDR/hook/PreToolUse
sleep 0.5
wc -l /tmp/cat-pr2/sink.jsonl
kill $DAEMON_PID
```

Expected: healthz returns 200; `/tmp/cat-pr2/sink.jsonl` has lines after a hook event; `/tmp/cat-pr2/cat.db` exists.

- [ ] **Scenario B — disabled otel source returns 404**

```sh
cat > /tmp/cat-pr2/config-nosources.yaml <<'EOF'
store:
  backend: sqlite
  sqlite:
    path: /tmp/cat-pr2/cat2.db
sources:
  otel: { enabled: false }
EOF
./bin/catacomb daemon --config /tmp/cat-pr2/config-nosources.yaml --discovery /tmp/cat-pr2/d2.json &
DAEMON_PID2=$!
sleep 1
ADDR2=$(python3 -c "import json; d=json.load(open('/tmp/cat-pr2/d2.json')); print(d['addr'])")
TOKEN2=$(python3 -c "import json; d=json.load(open('/tmp/cat-pr2/d2.json')); print(d['token'])")
STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST -H "Authorization: Bearer $TOKEN2" http://$ADDR2/v1/traces)
echo "otel disabled status: $STATUS"
kill $DAEMON_PID2
```

Expected: `/v1/traces` returns 404; `/hook/PreToolUse` returns 204 (hooks enabled by default: nil → true).

- [ ] **Scenario C — legacy flag `--postgres-export-dsn` still works (non-fatal on connection fail)**

```sh
./bin/catacomb daemon --discovery /tmp/cat-pr2/d3.json --db /tmp/cat-pr2/cat3.db \
  --postgres-export-dsn "postgres://user:pass@localhost/db" &
DAEMON_PID3=$!
sleep 1
ADDR3=$(python3 -c "import json; d=json.load(open('/tmp/cat-pr2/d3.json')); print(d['addr'])")
curl -s -o /dev/null -w "%{http_code}" http://$ADDR3/healthz
kill $DAEMON_PID3
```

Expected: daemon starts without error (PG sink is skipped at runtime if connection fails — non-fatal); healthz 200.

- [ ] **Scenario D — batch CLI reads config store path**

```sh
cat > /tmp/cat-pr2/config-batch.yaml <<'EOF'
store:
  backend: sqlite
  sqlite:
    path: /tmp/cat-pr2/custom.db
EOF
CATACOMB_CONFIG=/tmp/cat-pr2/config-batch.yaml ./bin/catacomb runs 2>&1
```

Expected: error message references `/tmp/cat-pr2/custom.db`, not `~/.catacomb/catacomb.db`.

- [ ] **Scenario E — unknown config key fails fast**

```sh
printf 'store:\n  nope: 1\n' > /tmp/cat-pr2/bad.yaml
./bin/catacomb daemon --config /tmp/cat-pr2/bad.yaml --discovery /tmp/cat-pr2/dx.json 2>&1 | head -3
```

Expected: exits non-zero with a YAML position error message; daemon does NOT start.

- [ ] **Commit:** `git add . && git commit -m "test(live): PR2 mandatory live-verify scenarios documented and passed

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"`

---

## Deferred to PR3

The following items are recognized by this plan but intentionally not implemented here:

- **`catacomb down`**: Already shipped in PR #75 (SIGTERM→SIGKILL, stale-discovery cleanup, `--timeout`, `--json`, `--yes`). No further work needed here.
- **`catacomb restart` / `catacomb status --json` / `catacomb logs [-f]` / `catacomb up --foreground`**: PR3 scope. All keyed off the discovery file. All process-control interfaces consumer-declared for unit testability.
- **`postgres` as primary store**: `store.Open` returns `ErrBackendNotImplemented` for `BackendPostgres`; the full `store.Store` implementation on Postgres (observation-log read-back, recover, tail cursors, annotations, quarantine) is a follow-up.
- **`down --uninstall` / teardown escalations**: Out of scope.

---

## Self-review against the spec (PR2 scope)

| Spec requirement | Implementing task |
|---|---|
| §8: `export.Build(sinks []config.Sink) ([]export.Exporter, error)` | Task 2 (`export/build.Build`) |
| §8: each `type` maps to existing exporter — postgres/neo4j/otlp | Task 2 (`Builders.NewPostgres/Neo4j/OTLP`) |
| §8: jsonl sink maps to exporter | Task 1 (`jsonl.NewStreamer`) + Task 2 (`Builders.NewJSONL`) |
| §8: daemon fan-out uses built list | Task 3 (`startExporter` → `buildFn`) |
| §8: sink CONFIG error → fatal | Task 2 (`ErrUnknownSink` return in `BuildWith`) + `config.Validate` already in PR1 |
| §8: sink RUNTIME error → log+skip | Task 2 (`log.Printf` + `continue` in `BuildWith`) |
| §7: hooks source → gate `POST /hook/{type}` | Task 3 (`Handler` enabled check) |
| §7: otel source → gate `POST /v1/traces` | Task 3 (`Handler` enabled check) |
| §7: stream_json source → gate `POST /v1/stream-json` | Task 3 (`Handler` enabled check) |
| §7: jsonl source → gate transcript tail loop | Task 3 (`tailLoop` enabled + dir check) |
| §7: default unchanged when no config (hooks/otel/stream_json enabled=true, jsonl=false) | Task 3 (`enabled(nil)=true` rule) + Task 4 (`cfg.Sources` from `Defaults()`) |
| §7: documented seam — disabling hooks ≠ removing settings.json forwarders | Task 3 (`Handler` comment-free; in-plan note) |
| §10: legacy exporter flags remain highest-precedence | Task 4 (`runDaemonWith` append-override pattern) |
| §10: `--transcript-dir`/`--transcript-exclude` as highest-precedence override for `sources.jsonl` | Task 4 (RunE `Changed`-check + `transcriptDir != ""` merge in `runDaemonWith`) |
| Batch CLI reads `store.sqlite.path` from config | Task 5 (`resolveStorePath`, `defaultBatchDBPath`) |
| Batch CLI `--db` still highest-precedence override | Task 5 (flag default = `defaultBatchDBPath()`; user-set `--db` overrides at cobra level) |
| §12: live-verify mandatory | Task 6 (5 scenarios) |
| 100% coverage | All tasks: test-first, coverage check per task |
| No import cycle | Task 2 justification: `export/build` → sub-packages; none of those import `export/build` |
