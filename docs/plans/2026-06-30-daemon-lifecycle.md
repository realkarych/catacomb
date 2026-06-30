# Daemon Lifecycle (PR3) Implementation Plan

<!-- markdownlint-disable MD005 -->

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver the four daemon-lifecycle features of the config/sinks/sources/lifecycle workstream: (1) `status --json` + resolved-config display enriched from a secret-free summary in the discovery file; (2) `catacomb restart`; (3) `catacomb logs [-f]`; (4) `catacomb up --foreground`. `catacomb down` is already shipped in #75 — do not re-plan it. Register the two new commands in `root.go`.

**Architecture:** All four features key off the existing discovery file (`daemon.Discovery`) written by `runDaemonWith` in `cmd/catacomb/daemon.go`. Strategy: (1) Add five secret-free summary fields to `daemon.Discovery` (backend type, sink TYPE strings only, enabled source names, reaper-window string, max-shards int) and populate them in `runDaemonWith` before `WriteDiscovery`. (2) `status` reads them without any new endpoint or auth. (3) `restart` reuses `stopDaemon`/`waitGone` from `down.go` (same package `main`) then forks a fresh daemon via `buildStartDaemon`. (4) `logs` opens the file at `discoveryPath+".log"` (established in `up.go:109`) with an injectable opener and injected tick channel for follow mode. (5) `up --foreground` adds a `daemonDone <-chan error` to `upDeps` and starts `runDaemonWith` in a goroutine from `RunE`; `runUp` blocks on the channel after its normal hook/browser steps.

**Tech Stack:** Go 1.26, pure Go, no cgo. `modernc.org/sqlite`. `gopkg.in/yaml.v3`. cobra. testify. Module: `github.com/realkarych/catacomb`.

## Global Constraints

- Go 1.26; pure Go, NO cgo; SQLite is `modernc.org/sqlite` (never `mattn`).
- NO comments in Go code. NONE — not even doc comments. Only `//go:build`/`//go:embed`/`//go:generate` allowed (enforced by `internal/codepolicy`). Every code sample below contains zero comments.
- 100% test coverage, TDD-first. Failing test first, then minimal impl, then commit. Every branch covered. The threshold never goes down.
- `go test -race`; coverage via `make cover` (`-coverpkg=./...`).
- Consumer declares interfaces; no global mutable state beyond established injectable vars (`downSignal`, `downSleep`, `execCommand`, `startCmd`, `osExecutable`, etc.); no `init()` side effects; no constructors with hidden I/O; wire deps from `main`.
- Errors: sentinels checked with `errors.Is`; wrap `fmt.Errorf("pkg.Op: %w", err)`; **never log or serialize secrets** — sink DSNs/URIs/passwords/paths MUST NOT appear in `daemon.Discovery`, `status` output, or log files. Only sink TYPE strings (`"postgres"`, `"neo4j"`, `"otlp"`, `"jsonl"`) are safe to surface.
- gofumpt + goimports (local prefix `github.com/realkarych/catacomb`).
- No `time.Sleep` in tests (forbidigo); no `time.Sleep` in the `logs --follow` loop — use an injected tick channel.
- CROSS-PLATFORM (CI runs Windows): path assertions wrap expected in `filepath.FromSlash`; bad-path/error tests use a file-as-directory blocker (`os.WriteFile(filepath.Join(dir,"afile"),...) ; path = filepath.Join(dir,"afile","x.json")`), NOT `/nonexistent/...`.
- NO PLACEHOLDERS. Every code sample is complete, runnable Go.
- Worktree root: `/Users/karych/src/catacomb/.claude/worktrees/config-daemon-lifecycle`, branch `feat/daemon-lifecycle`. Commit footer: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`.

---

## File Structure

- `daemon/discovery.go` (Modify) — add five secret-free summary fields to `Discovery` struct.
- `daemon/discovery_test.go` (Modify) — add tests for round-trip serialization of new fields.
- `cmd/catacomb/daemon.go` (Modify) — add helpers `sinkTypeStrings`/`enabledSourceNames`; populate new fields in the `daemon.Discovery` literal inside `runDaemonWith`.
- `cmd/catacomb/daemon_test.go` (Modify) — add tests for new helpers and discovery population.
- `cmd/catacomb/status.go` (Modify) — add `asJSON bool` to `statusDeps`; add `statusReport` struct; enrich text output; add `--json` flag to `newStatusCmd`.
- `cmd/catacomb/status_test.go` (Modify) — add JSON and enriched-text tests; update existing tests to write new fields into the discovery fixture.
- `cmd/catacomb/restart.go` (Create) — new `restart` command; `restartDeps` struct; `runRestart`.
- `cmd/catacomb/restart_test.go` (Create) — 100% coverage of restart logic; reuses `swapSignal`/`swapSleepNoop` helpers from `down_test.go`.
- `cmd/catacomb/logs.go` (Create) — new `logs` command; `logsDeps` struct; `runLogs`.
- `cmd/catacomb/logs_test.go` (Create) — 100% coverage of logs logic; injected tick channel.
- `cmd/catacomb/up.go` (Modify) — add `daemonDone <-chan error` to `upDeps`; add `--foreground` flag; foreground setup in `RunE`; `<-deps.daemonDone` wait in `runUp`.
- `cmd/catacomb/up_test.go` (Modify) — add foreground tests.
- `cmd/catacomb/root.go` (Modify) — register `newRestartCmd()` and `newLogsCmd()` under `groupObserve`.
- `cmd/catacomb/root_test.go` (Modify) — assert `restart` and `logs` are registered.

---

## Task 1: Enrich `daemon.Discovery` with secret-free summary fields

**Files:**

- Modify: `daemon/discovery.go`
- Modify: `daemon/discovery_test.go`

**Interfaces:**

- `daemon.Discovery` gains five new fields (all JSON-serializable, all secret-free):

```go
StoreBackend    string   `json:"store_backend,omitempty"`
SinkTypes       []string `json:"sink_types,omitempty"`
SourcesEnabled  []string `json:"sources_enabled,omitempty"`
ReaperWindow    string   `json:"reaper_window,omitempty"`
MaxShards       int      `json:"max_shards,omitempty"`
```

- `StoreBackend`: one of `"sqlite"`, `"memory"`, `"postgres"` — the backend TYPE, never a DSN or path.
- `SinkTypes`: TYPE strings only (`"postgres"`, `"neo4j"`, `"otlp"`, `"jsonl"`). No DSN, URI, password, or path. An empty sinks list → `nil` (omitempty).
- `SourcesEnabled`: names of sources where the `Enabled` pointer is nil (nil = default-true) or `*Enabled == true`.
- `ReaperWindow`: `time.Duration.String()` output, e.g. `"30m0s"`.
- `MaxShards`: int, 0 is omitted by `omitempty` — set `json:"max_shards"` (no omitempty) so 0 is still round-tripped correctly. Use `json:"max_shards"` without omitempty.

Steps:

- [ ] **Write failing tests** in `daemon/discovery_test.go`. Add after existing tests:

```go
func TestDiscoveryNewFieldsRoundTrip(t *testing.T) {
	d := Discovery{
		Addr:           "127.0.0.1:1",
		Token:          "tok",
		StoreBackend:   "memory",
		SinkTypes:      []string{"otlp", "postgres"},
		SourcesEnabled: []string{"hooks", "otel"},
		ReaperWindow:   "30m0s",
		MaxShards:      4096,
	}
	tmp := filepath.Join(t.TempDir(), "d.json")
	require.NoError(t, WriteDiscovery(tmp, d))
	got, err := ReadDiscovery(tmp)
	require.NoError(t, err)
	assert.Equal(t, "memory", got.StoreBackend)
	assert.Equal(t, []string{"otlp", "postgres"}, got.SinkTypes)
	assert.Equal(t, []string{"hooks", "otel"}, got.SourcesEnabled)
	assert.Equal(t, "30m0s", got.ReaperWindow)
	assert.Equal(t, 4096, got.MaxShards)
}

func TestDiscoveryNewFieldsOmitEmpty(t *testing.T) {
	d := Discovery{Addr: "127.0.0.1:1", Token: "tok"}
	tmp := filepath.Join(t.TempDir(), "d.json")
	require.NoError(t, WriteDiscovery(tmp, d))
	b, err := os.ReadFile(tmp)
	require.NoError(t, err)
	assert.NotContains(t, string(b), "store_backend")
	assert.NotContains(t, string(b), "sink_types")
	assert.NotContains(t, string(b), "sources_enabled")
	assert.NotContains(t, string(b), "reaper_window")
}
```

- [ ] **Run** `go test ./daemon/ -run TestDiscoveryNewFields` — expect FAIL (fields undefined).

- [ ] **Minimal impl** — modify `daemon/discovery.go`, adding the five fields after `AllowAnnotations`:

```go
type Discovery struct {
	Addr               string   `json:"addr"`
	Token              string   `json:"token"`
	GRPCAddr           string   `json:"grpc_addr,omitempty"`
	Pid                int      `json:"pid,omitempty"`
	StartedAt          string   `json:"started_at,omitempty"`
	TranscriptDir      string   `json:"transcript_dir,omitempty"`
	DBPath             string   `json:"db_path,omitempty"`
	AllowPayloadAccess bool     `json:"allow_payload_access,omitempty"`
	AllowAnnotations   bool     `json:"allow_annotations,omitempty"`
	StoreBackend       string   `json:"store_backend,omitempty"`
	SinkTypes          []string `json:"sink_types,omitempty"`
	SourcesEnabled     []string `json:"sources_enabled,omitempty"`
	ReaperWindow       string   `json:"reaper_window,omitempty"`
	MaxShards          int      `json:"max_shards"`
}
```

- [ ] **Run** `go test -race ./daemon/` — expect PASS.
- [ ] **Run** `go build ./...` — expect clean compile.
- [ ] **Commit:** `git add daemon/discovery.go daemon/discovery_test.go && git commit -m "feat(daemon/discovery): add secret-free summary fields (store/sinks/sources/reaper)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"`

---

## Task 2: Populate summary fields in `runDaemonWith`

**Files:**

- Modify: `cmd/catacomb/daemon.go`
- Modify: `cmd/catacomb/daemon_test.go`

**Interfaces:**

- Two new package-level helpers in `cmd/catacomb/daemon.go` (no comments):

```go
func sinkTypeStrings(sinks []config.Sink) []string {
	if len(sinks) == 0 {
		return nil
	}
	out := make([]string, len(sinks))
	for i, s := range sinks {
		out[i] = s.Type
	}
	return out
}

func enabledSourceNames(s config.SourcesConfig) []string {
	enabled := func(b *bool) bool { return b == nil || *b }
	var names []string
	if enabled(s.Hooks.Enabled) {
		names = append(names, "hooks")
	}
	if enabled(s.Otel.Enabled) {
		names = append(names, "otel")
	}
	if enabled(s.StreamJSON.Enabled) {
		names = append(names, "stream_json")
	}
	if enabled(s.JSONL.Enabled) {
		names = append(names, "jsonl")
	}
	return names
}
```

- In `runDaemonWith`, the `daemon.Discovery` literal (currently lines ~223-231) gains five new fields populated from `p` and the resolved `sinks`/`sources` locals that already exist in scope:

```go
disc := daemon.Discovery{
	Addr:               ln.Addr().String(),
	Token:              token,
	GRPCAddr:           grpcLn.Addr().String(),
	TranscriptDir:      sources.JSONL.TranscriptDir,
	DBPath:             dbPath,
	AllowPayloadAccess: p.allowPayloadAccess,
	AllowAnnotations:   p.allowAnnotations,
	StoreBackend:       p.store.Backend,
	SinkTypes:          sinkTypeStrings(sinks),
	SourcesEnabled:     enabledSourceNames(sources),
	ReaperWindow:       p.reaperWindow.String(),
	MaxShards:          p.maxShards,
}
```

Steps:

- [ ] **Write failing tests** in `cmd/catacomb/daemon_test.go`. Add after the existing `boolPtr` helper:

```go
func TestSinkTypeStrings(t *testing.T) {
	assert.Nil(t, sinkTypeStrings(nil))
	assert.Nil(t, sinkTypeStrings([]config.Sink{}))
	got := sinkTypeStrings([]config.Sink{
		{Type: config.SinkOTLP, Endpoint: "grpc://host:4317"},
		{Type: config.SinkPostgres, DSN: "postgres://secret"},
	})
	assert.Equal(t, []string{config.SinkOTLP, config.SinkPostgres}, got)
}

func TestEnabledSourceNames(t *testing.T) {
	tr, fa := boolPtr(true), boolPtr(false)
	got := enabledSourceNames(config.SourcesConfig{
		Hooks:      config.SourceToggle{Enabled: tr},
		Otel:       config.SourceToggle{Enabled: fa},
		StreamJSON: config.SourceToggle{Enabled: nil},
		JSONL:      config.JSONLSource{Enabled: tr},
	})
	assert.Equal(t, []string{"hooks", "stream_json", "jsonl"}, got)
}

func TestEnabledSourceNamesAllDefault(t *testing.T) {
	got := enabledSourceNames(config.SourcesConfig{})
	assert.Equal(t, []string{"hooks", "otel", "stream_json", "jsonl"}, got)
}

func TestRunDaemonWithSummaryFields(t *testing.T) {
	discFile := filepath.Join(t.TempDir(), "d.json")
	ctx, cancel := context.WithCancel(context.Background())
	deps := testDaemonDeps()
	deps.newToken = func() (string, error) {
		cancel()
		return "tok", nil
	}
	p := testDaemonParams(t)
	p.discoveryPath = discFile
	p.reaperWindow = 15 * time.Minute
	p.maxShards = 512
	p.sinks = []config.Sink{
		{Type: config.SinkOTLP, Endpoint: "grpc://host:4317"},
	}
	enabled := true
	p.sources = config.SourcesConfig{
		Hooks: config.SourceToggle{Enabled: &enabled},
		Otel:  config.SourceToggle{Enabled: boolPtr(false)},
	}
	_ = runDaemonWith(ctx, deps, p)
	disc, err := daemon.ReadDiscovery(discFile)
	require.NoError(t, err)
	assert.Equal(t, config.BackendSQLite, disc.StoreBackend)
	assert.Equal(t, []string{config.SinkOTLP}, disc.SinkTypes)
	assert.Contains(t, disc.SourcesEnabled, "hooks")
	assert.NotContains(t, disc.SourcesEnabled, "otel")
	assert.Equal(t, "15m0s", disc.ReaperWindow)
	assert.Equal(t, 512, disc.MaxShards)
}
```

- [ ] **Run** `go test ./cmd/catacomb/ -run 'TestSinkTypeStrings|TestEnabledSourceNames|TestRunDaemonWithSummaryFields'` — expect FAIL.

- [ ] **Minimal impl** — add `sinkTypeStrings` and `enabledSourceNames` to `daemon.go` and update the `disc` literal in `runDaemonWith` as shown above. No other lines change.

- [ ] **Run** `go test -race ./cmd/catacomb/` — expect PASS.
- [ ] **Commit:** `git add cmd/catacomb/daemon.go cmd/catacomb/daemon_test.go && git commit -m "feat(cmd/daemon): populate secret-free summary in discovery (backend/sinks/sources/reaper)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"`

---

## Task 3: `status --json` + enriched text display

**Files:**

- Modify: `cmd/catacomb/status.go`
- Modify: `cmd/catacomb/status_test.go`

**Interfaces:**

- `statusDeps` gains `asJSON bool`.
- New struct `statusReport` (JSON output shape):

```go
type statusReport struct {
	Addr           string   `json:"addr"`
	Pid            int      `json:"pid"`
	Uptime         string   `json:"uptime"`
	TokenAge       string   `json:"token_age"`
	ObservingDir   string   `json:"observing_dir,omitempty"`
	StoreBackend   string   `json:"store_backend,omitempty"`
	SinkTypes      []string `json:"sink_types,omitempty"`
	SourcesEnabled []string `json:"sources_enabled,omitempty"`
	ReaperWindow   string   `json:"reaper_window,omitempty"`
	MaxShards      int      `json:"max_shards,omitempty"`
	Sessions       int      `json:"sessions"`
	Nodes          int      `json:"nodes"`
	Healthy        bool     `json:"healthy"`
}
```

- `runStatus` produces `statusReport`, then either JSON-encodes it or writes the tabwriter table.
- The tabwriter table adds new rows (after existing rows) when the fields are non-empty: `store`, `sinks`, `sources`, `reaper`, `shards`.
- `newStatusCmd` binds `--json` / `-j` flag → `deps.asJSON`.

Steps:

- [ ] **Write failing tests** — add to `cmd/catacomb/status_test.go`:

```go
func discWithSummary(t *testing.T, addr string) daemon.Discovery {
	t.Helper()
	return daemon.Discovery{
		Addr:           addr,
		Token:          "tok",
		Pid:            42,
		StartedAt:      time.Now().Add(-5 * time.Minute).UTC().Format(time.RFC3339),
		StoreBackend:   config.BackendMemory,
		SinkTypes:      []string{config.SinkOTLP},
		SourcesEnabled: []string{"hooks", "otel", "stream_json"},
		ReaperWindow:   "30m0s",
		MaxShards:      4096,
	}
}

func TestRunStatusJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"session":"s1","node_count":10}]`))
	}))
	t.Cleanup(srv.Close)

	disc := discWithSummary(t, strings.TrimPrefix(srv.URL, "http://"))
	discPath := filepath.Join(t.TempDir(), "d.json")
	require.NoError(t, daemon.WriteDiscovery(discPath, disc))

	var out bytes.Buffer
	deps := statusDeps{
		readDiscovery: daemon.ReadDiscovery,
		discoveryPath: discPath,
		httpClient:    srv.Client(),
		now:           time.Now,
		asJSON:        true,
	}
	require.NoError(t, runStatus(context.Background(), &out, deps))

	var rep statusReport
	require.NoError(t, json.Unmarshal(out.Bytes(), &rep))
	assert.Equal(t, 42, rep.Pid)
	assert.Equal(t, config.BackendMemory, rep.StoreBackend)
	assert.Equal(t, []string{config.SinkOTLP}, rep.SinkTypes)
	assert.Contains(t, rep.SourcesEnabled, "hooks")
	assert.Equal(t, "30m0s", rep.ReaperWindow)
	assert.Equal(t, 4096, rep.MaxShards)
	assert.Equal(t, 1, rep.Sessions)
	assert.Equal(t, 10, rep.Nodes)
	assert.True(t, rep.Healthy)
}

func TestRunStatusJSONUnhealthy(t *testing.T) {
	discPath := filepath.Join(t.TempDir(), "d.json")
	require.NoError(t, daemon.WriteDiscovery(discPath, daemon.Discovery{
		Addr:  "127.0.0.1:1",
		Token: "tok",
		Pid:   99,
	}))
	var out bytes.Buffer
	deps := statusDeps{
		readDiscovery: daemon.ReadDiscovery,
		discoveryPath: discPath,
		httpClient:    http.DefaultClient,
		now:           time.Now,
		asJSON:        true,
	}
	require.NoError(t, runStatus(context.Background(), &out, deps))
	var rep statusReport
	require.NoError(t, json.Unmarshal(out.Bytes(), &rep))
	assert.Equal(t, 99, rep.Pid)
	assert.False(t, rep.Healthy)
}

func TestRunStatusJSONNoDaemon(t *testing.T) {
	deps := statusDeps{
		readDiscovery: daemon.ReadDiscovery,
		discoveryPath: filepath.Join(t.TempDir(), "missing.json"),
		httpClient:    http.DefaultClient,
		now:           time.Now,
		asJSON:        true,
	}
	err := runStatus(context.Background(), io.Discard, deps)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoDaemon)
}

func TestRunStatusTextEnriched(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)
	disc := discWithSummary(t, strings.TrimPrefix(srv.URL, "http://"))
	discPath := filepath.Join(t.TempDir(), "d.json")
	require.NoError(t, daemon.WriteDiscovery(discPath, disc))

	var out bytes.Buffer
	deps := statusDeps{
		readDiscovery: daemon.ReadDiscovery,
		discoveryPath: discPath,
		httpClient:    srv.Client(),
		now:           time.Now,
		asJSON:        false,
	}
	require.NoError(t, runStatus(context.Background(), &out, deps))
	text := out.String()
	assert.Contains(t, text, "memory")
	assert.Contains(t, text, "otlp")
	assert.Contains(t, text, "hooks")
	assert.Contains(t, text, "30m0s")
	assert.Contains(t, text, "4096")
}

func TestStatusCmdJSONFlag(t *testing.T) {
	cmd := newStatusCmd()
	f := cmd.Flags().Lookup("json")
	require.NotNil(t, f)
	assert.Equal(t, "false", f.DefValue)
}
```

- [ ] Also update `TestRunStatusHealthy` to use `discWithSummary` or simply ensure the discovery fixture in that test includes at least empty summary fields (which is fine — the struct already passes `omitempty`). Add `"encoding/json"`, `"path/filepath"`, `"github.com/realkarych/catacomb/config"` to `status_test.go` imports.

- [ ] **Run** `go test ./cmd/catacomb/ -run 'TestRunStatusJSON|TestRunStatusTextEnriched|TestStatusCmdJSONFlag'` — expect FAIL.

- [ ] **Minimal impl** — `cmd/catacomb/status.go`:

  - Add `asJSON bool` to `statusDeps`.
  - Add `statusReport` struct.
  - Refactor `runStatus` to build a `statusReport` and then either JSON-encode or tabwriter-format it:

```go
type statusDeps struct {
	readDiscovery func(string) (daemon.Discovery, error)
	discoveryPath string
	httpClient    *http.Client
	now           func() time.Time
	asJSON        bool
}

type statusReport struct {
	Addr           string   `json:"addr"`
	Pid            int      `json:"pid"`
	Uptime         string   `json:"uptime"`
	TokenAge       string   `json:"token_age"`
	ObservingDir   string   `json:"observing_dir,omitempty"`
	StoreBackend   string   `json:"store_backend,omitempty"`
	SinkTypes      []string `json:"sink_types,omitempty"`
	SourcesEnabled []string `json:"sources_enabled,omitempty"`
	ReaperWindow   string   `json:"reaper_window,omitempty"`
	MaxShards      int      `json:"max_shards,omitempty"`
	Sessions       int      `json:"sessions"`
	Nodes          int      `json:"nodes"`
	Healthy        bool     `json:"healthy"`
}

func runStatus(ctx context.Context, out io.Writer, deps statusDeps) error {
	disc, err := deps.readDiscovery(deps.discoveryPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNoDaemon
		}
		return err
	}

	now := deps.now()
	uptime := "unknown"
	tokenAge := "unknown"
	if disc.StartedAt != "" {
		if t, parseErr := time.Parse(time.RFC3339, disc.StartedAt); parseErr == nil {
			d := now.Sub(t).Truncate(time.Second)
			uptime = d.String()
			tokenAge = d.String()
		}
	}

	sessions, nodes, fetchErr := fetchSessionCounts(ctx, disc, deps.httpClient)
	healthy := fetchErr == nil

	rep := statusReport{
		Addr:           disc.Addr,
		Pid:            disc.Pid,
		Uptime:         uptime,
		TokenAge:       tokenAge,
		ObservingDir:   disc.TranscriptDir,
		StoreBackend:   disc.StoreBackend,
		SinkTypes:      disc.SinkTypes,
		SourcesEnabled: disc.SourcesEnabled,
		ReaperWindow:   disc.ReaperWindow,
		MaxShards:      disc.MaxShards,
		Sessions:       sessions,
		Nodes:          nodes,
		Healthy:        healthy,
	}

	if deps.asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(rep)
	}

	if fetchErr != nil && errors.Is(fetchErr, ErrDaemonRestarted) {
		return ErrDaemonRestarted
	}

	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintf(w, "addr\t%s\n", rep.Addr)
	_, _ = fmt.Fprintf(w, "pid\t%d\n", rep.Pid)
	_, _ = fmt.Fprintf(w, "uptime\t%s\n", rep.Uptime)
	_, _ = fmt.Fprintf(w, "token age\t%s\n", rep.TokenAge)
	_, _ = fmt.Fprintf(w, "observing\t%s\n", observingLabel(disc.TranscriptDir))
	if rep.StoreBackend != "" {
		_, _ = fmt.Fprintf(w, "store\t%s\n", rep.StoreBackend)
	}
	if len(rep.SinkTypes) > 0 {
		_, _ = fmt.Fprintf(w, "sinks\t%s\n", strings.Join(rep.SinkTypes, " "))
	}
	if len(rep.SourcesEnabled) > 0 {
		_, _ = fmt.Fprintf(w, "sources\t%s\n", strings.Join(rep.SourcesEnabled, " "))
	}
	if rep.ReaperWindow != "" {
		_, _ = fmt.Fprintf(w, "reaper\t%s\n", rep.ReaperWindow)
	}
	if rep.MaxShards > 0 {
		_, _ = fmt.Fprintf(w, "shards\t%d\n", rep.MaxShards)
	}
	if fetchErr != nil {
		_, _ = fmt.Fprintf(w, "sessions\tunavailable\n")
		_, _ = fmt.Fprintf(w, "nodes\tunavailable\n")
		return w.Flush()
	}
	_, _ = fmt.Fprintf(w, "sessions\t%d\n", rep.Sessions)
	_, _ = fmt.Fprintf(w, "nodes\t%d\n", rep.Nodes)
	return w.Flush()
}
```

  - `newStatusCmd` gains `--json` / `-j`:

```go
func newStatusCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Print daemon addr, pid, uptime, and session/node counts",
		RunE: func(cmd *cobra.Command, _ []string) error {
			deps := statusDeps{
				readDiscovery: daemon.ReadDiscovery,
				discoveryPath: daemon.DiscoveryPath(),
				httpClient:    statusHTTPClient,
				now:           statusNowFn,
				asJSON:        asJSON,
			}
			return runStatus(cmd.Context(), cmd.OutOrStdout(), deps)
		},
	}
	cmd.Flags().BoolVarP(&asJSON, "json", "j", false, "output machine-readable JSON")
	return cmd
}
```

  - Add `"encoding/json"` and `"strings"` to `status.go` imports (if not already present).

- [ ] **Run** `go test -race ./cmd/catacomb/` — expect PASS.
- [ ] **Run** `make cover` — expect 100% threshold still met.
- [ ] **Commit:** `git add cmd/catacomb/status.go cmd/catacomb/status_test.go && git commit -m "feat(cmd/status): --json flag + enriched text display (backend/sinks/sources/reaper)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"`

---

## Task 4: `catacomb restart` command

**Files:**

- Create: `cmd/catacomb/restart.go`
- Create: `cmd/catacomb/restart_test.go`

**Interfaces:**

- `restartDeps` struct (all fields injectable for testing):

```go
type restartDeps struct {
	readDiscovery func(string) (daemon.Discovery, error)
	discoveryPath string
	stopFn        func(pid int, force bool) (bool, error)
	waitGoneFn    func(pid int) bool
	removeDisc    func(string) error
	startDaemon   func() error
	pollHealthz   func(ctx context.Context, addr string) error
	after         func(time.Duration) <-chan time.Time
	force         bool
	asJSON        bool
	waitSeconds   int
}
```

- `runRestart(ctx context.Context, out io.Writer, deps restartDeps) error` — logic:
  1. `readDiscovery` → if `os.ErrNotExist`, skip stop (daemon not running, proceed to start).
  2. If disc found: `stopFn(disc.Pid, deps.force)` → if not stopped and !force, return error. If stopped, `removeDisc(deps.discoveryPath)`.
  3. `startDaemon()` → start fresh daemon.
  4. Poll `readDiscovery` + `pollHealthz` with `after` tick up to `waitSeconds` retries.
  5. If JSON: emit `restartReport{Stopped: bool, Started: bool, Addr: string}`. Else print text lines.

- `restartReport` struct:

```go
type restartReport struct {
	Stopped bool   `json:"stopped"`
	Started bool   `json:"started"`
	Addr    string `json:"addr,omitempty"`
}
```

- `newRestartCmd() *cobra.Command` — flags `--force`, `--json`.

- `stopFn` in production = `stopDaemon` (same `package main`).
- `waitGoneFn` in production = `waitGone` (same `package main`).
- `startDaemon` in production = `buildStartDaemon(discPath, "")()` (same pattern as `up.go:buildStartDaemon`).
- `pollHealthz` in production = `upPollHealthz` (already exported as package-level var in `up.go`).

Steps:

- [ ] **Write failing test** `cmd/catacomb/restart_test.go`:

```go
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/daemon"
)

func fakeRestartDeps(discPath string) restartDeps {
	return restartDeps{
		readDiscovery: daemon.ReadDiscovery,
		discoveryPath: discPath,
		stopFn:        func(_ int, _ bool) (bool, error) { return true, nil },
		waitGoneFn:    func(_ int) bool { return true },
		removeDisc:    func(_ string) error { return nil },
		startDaemon:   func() error { return nil },
		pollHealthz:   func(_ context.Context, _ string) error { return nil },
		after:         func(_ time.Duration) <-chan time.Time { ch := make(chan time.Time, 1); ch <- time.Now(); return ch },
		waitSeconds:   1,
	}
}

func TestRunRestartNoDaemonStartsNew(t *testing.T) {
	disc := filepath.Join(t.TempDir(), "d.json")
	deps := fakeRestartDeps(disc)
	started := false
	deps.startDaemon = func() error { started = true; return nil }
	require.NoError(t, runRestart(context.Background(), io.Discard, deps))
	assert.True(t, started)
}

func TestRunRestartStopsAndRestarts(t *testing.T) {
	dir := t.TempDir()
	disc := filepath.Join(dir, "d.json")
	require.NoError(t, daemon.WriteDiscovery(disc, daemon.Discovery{Addr: "127.0.0.1:1", Token: "t", Pid: 99}))

	stopped, started := false, false
	deps := fakeRestartDeps(disc)
	deps.stopFn = func(_ int, _ bool) (bool, error) { stopped = true; return true, nil }
	deps.startDaemon = func() error { started = true; return nil }

	require.NoError(t, runRestart(context.Background(), io.Discard, deps))
	assert.True(t, stopped)
	assert.True(t, started)
}

func TestRunRestartForce(t *testing.T) {
	dir := t.TempDir()
	disc := filepath.Join(dir, "d.json")
	require.NoError(t, daemon.WriteDiscovery(disc, daemon.Discovery{Addr: "127.0.0.1:1", Token: "t", Pid: 99}))

	var gotForce bool
	deps := fakeRestartDeps(disc)
	deps.force = true
	deps.stopFn = func(_ int, force bool) (bool, error) { gotForce = force; return true, nil }
	require.NoError(t, runRestart(context.Background(), io.Discard, deps))
	assert.True(t, gotForce)
}

func TestRunRestartStopError(t *testing.T) {
	dir := t.TempDir()
	disc := filepath.Join(dir, "d.json")
	require.NoError(t, daemon.WriteDiscovery(disc, daemon.Discovery{Addr: "127.0.0.1:1", Token: "t", Pid: 99}))
	deps := fakeRestartDeps(disc)
	deps.stopFn = func(_ int, _ bool) (bool, error) { return false, ErrDaemonStop }
	err := runRestart(context.Background(), io.Discard, deps)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDaemonStop)
}

func TestRunRestartStartError(t *testing.T) {
	disc := filepath.Join(t.TempDir(), "d.json")
	deps := fakeRestartDeps(disc)
	deps.startDaemon = func() error { return errors.New("exec failed") }
	err := runRestart(context.Background(), io.Discard, deps)
	require.Error(t, err)
}

func TestRunRestartJSONOutput(t *testing.T) {
	dir := t.TempDir()
	disc := filepath.Join(dir, "d.json")
	newDisc := filepath.Join(dir, "d.json")
	deps := fakeRestartDeps(disc)
	deps.asJSON = true
	deps.readDiscovery = func(path string) (daemon.Discovery, error) {
		d, err := daemon.ReadDiscovery(path)
		if err != nil {
			return daemon.Discovery{Addr: "127.0.0.1:9999", Token: "new"}, nil
		}
		return d, nil
	}
	_ = daemon.WriteDiscovery(newDisc, daemon.Discovery{Addr: "127.0.0.1:9999", Token: "new", Pid: 1})
	var out bytes.Buffer
	require.NoError(t, runRestart(context.Background(), &out, deps))
	var rep restartReport
	require.NoError(t, json.Unmarshal(out.Bytes(), &rep))
	assert.True(t, rep.Started)
}

func TestRunRestartTextOutput(t *testing.T) {
	disc := filepath.Join(t.TempDir(), "d.json")
	deps := fakeRestartDeps(disc)
	var out bytes.Buffer
	require.NoError(t, runRestart(context.Background(), &out, deps))
	assert.Contains(t, strings.ToLower(out.String()), "restart")
}

func TestRunRestartDiscoveryReadError(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "afile"), []byte("x"), 0o600))
	deps := fakeRestartDeps(filepath.Join(dir, "afile"))
	deps.readDiscovery = func(_ string) (daemon.Discovery, error) {
		return daemon.Discovery{}, errors.New("bad json")
	}
	err := runRestart(context.Background(), io.Discard, deps)
	require.Error(t, err)
}

func TestRestartCmdRegistered(t *testing.T) {
	root := newRootCmd()
	found := false
	for _, sub := range root.Commands() {
		if sub.Use == "restart" {
			found = true
		}
	}
	assert.True(t, found, "restart subcommand must be registered")
}

func TestRestartCmdForceFlag(t *testing.T) {
	cmd := newRestartCmd()
	f := cmd.Flags().Lookup("force")
	require.NotNil(t, f)
}

func TestRestartCmdJSONFlag(t *testing.T) {
	cmd := newRestartCmd()
	f := cmd.Flags().Lookup("json")
	require.NotNil(t, f)
}

func TestRestartUsesDownSignalForStop(t *testing.T) {
	swapSleepNoop(t)
	n := 0
	swapSignal(t, func(_ int, _ syscall.Signal) error {
		n++
		if n >= 3 {
			return errors.New("gone")
		}
		return nil
	})
	dir := t.TempDir()
	disc := filepath.Join(dir, "d.json")
	require.NoError(t, daemon.WriteDiscovery(disc, daemon.Discovery{Addr: "127.0.0.1:1", Token: "t", Pid: 99}))
	deps := fakeRestartDeps(disc)
	deps.stopFn = stopDaemon
	deps.startDaemon = func() error { return nil }
	require.NoError(t, runRestart(context.Background(), io.Discard, deps))
	assert.GreaterOrEqual(t, n, 3)
}
```

- [ ] **Run** `go test ./cmd/catacomb/ -run 'TestRunRestart|TestRestartCmd'` — expect FAIL (package `main` missing `runRestart`, `restartDeps`, `newRestartCmd`).

- [ ] **Minimal impl** `cmd/catacomb/restart.go`:

```go
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/realkarych/catacomb/daemon"
)

type restartDeps struct {
	readDiscovery func(string) (daemon.Discovery, error)
	discoveryPath string
	stopFn        func(pid int, force bool) (bool, error)
	waitGoneFn    func(pid int) bool
	removeDisc    func(string) error
	startDaemon   func() error
	pollHealthz   func(ctx context.Context, addr string) error
	after         func(time.Duration) <-chan time.Time
	force         bool
	asJSON        bool
	waitSeconds   int
}

type restartReport struct {
	Stopped bool   `json:"stopped"`
	Started bool   `json:"started"`
	Addr    string `json:"addr,omitempty"`
}

func newRestartCmd() *cobra.Command {
	var force, asJSON bool
	cmd := &cobra.Command{
		Use:   "restart",
		Short: "Stop the running daemon and start a fresh one",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			discPath := daemon.DiscoveryPath()
			deps := restartDeps{
				readDiscovery: daemon.ReadDiscovery,
				discoveryPath: discPath,
				stopFn:        stopDaemon,
				waitGoneFn:    waitGone,
				removeDisc:    os.Remove,
				startDaemon:   buildStartDaemon(discPath, ""),
				pollHealthz:   prodPollHealthz,
				after:         time.After,
				force:         force,
				asJSON:        asJSON,
				waitSeconds:   5,
			}
			return runRestart(cmd.Context(), cmd.OutOrStdout(), deps)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "escalate a stuck daemon stop to SIGKILL")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output machine-readable JSON")
	return cmd
}

func runRestart(ctx context.Context, out io.Writer, deps restartDeps) error {
	rep := restartReport{}

	disc, derr := deps.readDiscovery(deps.discoveryPath)
	if derr != nil && !errors.Is(derr, os.ErrNotExist) {
		return fmt.Errorf("restart: read discovery: %w", derr)
	}

	if derr == nil {
		stopped, serr := deps.stopFn(disc.Pid, deps.force)
		if serr != nil {
			return serr
		}
		if !stopped {
			return ErrDaemonStop
		}
		rep.Stopped = true
		if err := deps.removeDisc(deps.discoveryPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("restart: remove discovery: %w", err)
		}
	}

	if err := deps.startDaemon(); err != nil {
		return fmt.Errorf("restart: start daemon: %w", err)
	}

	ready := false
	var newDisc daemon.Discovery
	for attempt := 0; attempt < deps.waitSeconds; attempt++ {
		d, err := deps.readDiscovery(deps.discoveryPath)
		if err == nil {
			if hzErr := deps.pollHealthz(ctx, d.Addr); hzErr == nil {
				newDisc = d
				ready = true
				break
			}
		}
		<-deps.after(time.Second)
	}
	rep.Started = ready
	if ready {
		rep.Addr = newDisc.Addr
	}

	if deps.asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(rep)
	}

	if rep.Stopped {
		_, _ = fmt.Fprintln(out, "daemon stopped")
	}
	if rep.Started {
		_, _ = fmt.Fprintf(out, "daemon restarted (%s)\n", rep.Addr)
	} else {
		_, _ = fmt.Fprintln(out, "daemon did not start in time")
	}
	return nil
}
```

- [ ] **Run** `go test -race ./cmd/catacomb/` — expect PASS.
- [ ] **Run** `make cover` — expect threshold still met.
- [ ] **Commit:** `git add cmd/catacomb/restart.go cmd/catacomb/restart_test.go && git commit -m "feat(cmd/restart): new restart command (stop + start via down/up primitives)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"`

---

## Task 5: `catacomb logs [-f]` command

**Files:**

- Create: `cmd/catacomb/logs.go`
- Create: `cmd/catacomb/logs_test.go`

**Interfaces:**

- `logsDeps` struct:

```go
type logsDeps struct {
	logPath string
	openLog func(string) (io.ReadCloser, error)
	follow  bool
	tick    <-chan time.Time
}
```

- `runLogs(ctx context.Context, out io.Writer, deps logsDeps) error`:
  1. `deps.openLog(deps.logPath)` — if `os.ErrNotExist`, print `"(no log file yet)"` and return nil.
  2. `io.Copy(out, r)` — print all existing content.
  3. If `!deps.follow`, close and return.
  4. Follow loop: `select { case <-ctx.Done(): return nil; case _, ok := <-deps.tick: if !ok { return nil }; io.Copy(out, r) }`.
  5. Close `r` on any exit path.

- `newLogsCmd() *cobra.Command` — flags `--follow` / `-f`. In production creates a real `time.NewTicker(200ms)` when `--follow` is set, passes `.C` as `tick`. Never calls `time.Sleep`.

- `logPath` in production = `daemon.DiscoveryPath() + ".log"`. No discovery read needed — the path is deterministic.

Steps:

- [ ] **Write failing test** `cmd/catacomb/logs_test.go`:

```go
package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openFileRC(path string) (io.ReadCloser, error) { return os.Open(path) }

func TestRunLogsNoFile(t *testing.T) {
	var out bytes.Buffer
	deps := logsDeps{
		logPath: filepath.Join(t.TempDir(), "missing.log"),
		openLog: openFileRC,
		follow:  false,
	}
	require.NoError(t, runLogs(context.Background(), &out, deps))
	assert.Contains(t, out.String(), "no log file yet")
}

func TestRunLogsReadsExistingContent(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "d.log")
	require.NoError(t, os.WriteFile(p, []byte("hello from daemon\n"), 0o600))
	var out bytes.Buffer
	deps := logsDeps{logPath: p, openLog: openFileRC, follow: false}
	require.NoError(t, runLogs(context.Background(), &out, deps))
	assert.Contains(t, out.String(), "hello from daemon")
}

func TestRunLogsOpenError(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "afile"), []byte("x"), 0o600))
	badPath := filepath.Join(dir, "afile")
	deps := logsDeps{
		logPath: badPath,
		openLog: func(_ string) (io.ReadCloser, error) { return nil, os.ErrPermission },
		follow:  false,
	}
	err := runLogs(context.Background(), io.Discard, deps)
	require.Error(t, err)
}

func TestRunLogsFollowPicksUpNewContent(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "d.log")
	require.NoError(t, os.WriteFile(p, []byte("line1\n"), 0o600))

	tick := make(chan time.Time, 2)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	var mu bytes.Buffer
	deps := logsDeps{logPath: p, openLog: openFileRC, follow: true, tick: tick}

	errc := make(chan error, 1)
	go func() { errc <- runLogs(ctx, &mu, deps) }()

	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, err = f.WriteString("line2\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	tick <- time.Now()

	require.Eventually(t, func() bool {
		return strings.Contains(mu.String(), "line2")
	}, 2*time.Second, 5*time.Millisecond)

	cancel()
	require.NoError(t, <-errc)
	assert.Contains(t, mu.String(), "line1")
}

func TestRunLogsFollowExitsOnContextCancel(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "d.log")
	require.NoError(t, os.WriteFile(p, []byte("x\n"), 0o600))
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	tick := make(chan time.Time)
	deps := logsDeps{logPath: p, openLog: openFileRC, follow: true, tick: tick}
	require.NoError(t, runLogs(ctx, io.Discard, deps))
}

func TestRunLogsFollowExitsOnClosedTick(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "d.log")
	require.NoError(t, os.WriteFile(p, []byte("x\n"), 0o600))
	tick := make(chan time.Time)
	close(tick)
	deps := logsDeps{logPath: p, openLog: openFileRC, follow: true, tick: tick}
	require.NoError(t, runLogs(context.Background(), io.Discard, deps))
}

func TestLogsCmdRegistered(t *testing.T) {
	root := newRootCmd()
	found := false
	for _, sub := range root.Commands() {
		if sub.Use == "logs" {
			found = true
		}
	}
	assert.True(t, found, "logs subcommand must be registered")
}

func TestLogsCmdFollowFlag(t *testing.T) {
	cmd := newLogsCmd()
	f := cmd.Flags().Lookup("follow")
	require.NotNil(t, f)
	assert.Equal(t, "false", f.DefValue)
}
```

- [ ] **Run** `go test ./cmd/catacomb/ -run 'TestRunLogs|TestLogsCmdRegistered|TestLogsCmdFollowFlag'` — expect FAIL.

- [ ] **Minimal impl** `cmd/catacomb/logs.go`:

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/realkarych/catacomb/daemon"
)

type logsDeps struct {
	logPath string
	openLog func(string) (io.ReadCloser, error)
	follow  bool
	tick    <-chan time.Time
}

func newLogsCmd() *cobra.Command {
	var follow bool
	return &cobra.Command{
		Use:   "logs",
		Short: "Print the daemon log (use -f to follow)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var tick <-chan time.Time
			if follow {
				t := time.NewTicker(200 * time.Millisecond)
				defer t.Stop()
				tick = t.C
			}
			deps := logsDeps{
				logPath: daemon.DiscoveryPath() + ".log",
				openLog: func(p string) (io.ReadCloser, error) { return os.Open(p) },
				follow:  follow,
				tick:    tick,
			}
			return runLogs(cmd.Context(), cmd.OutOrStdout(), deps)
		},
	}
}

func runLogs(ctx context.Context, out io.Writer, deps logsDeps) error {
	r, err := deps.openLog(deps.logPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			_, _ = fmt.Fprintln(out, "(no log file yet)")
			return nil
		}
		return fmt.Errorf("logs: open: %w", err)
	}
	defer func() { _ = r.Close() }()

	if _, err := io.Copy(out, r); err != nil {
		return fmt.Errorf("logs: read: %w", err)
	}
	if !deps.follow {
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case _, ok := <-deps.tick:
			if !ok {
				return nil
			}
			if _, err := io.Copy(out, r); err != nil {
				return fmt.Errorf("logs: follow read: %w", err)
			}
		}
	}
}
```

- [ ] **Run** `go test -race ./cmd/catacomb/` — expect PASS.
- [ ] **Run** `make cover` — expect threshold still met.
- [ ] **Commit:** `git add cmd/catacomb/logs.go cmd/catacomb/logs_test.go && git commit -m "feat(cmd/logs): new logs command with -f follow via injected ticker

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"`

---

## Task 6: `catacomb up --foreground`

**Files:**

- Modify: `cmd/catacomb/up.go`
- Modify: `cmd/catacomb/up_test.go`

**Interfaces:**

- `upDeps` gains one new field:

```go
daemonDone <-chan error
```

- `runUp` gains one block at the `if deps.noDemo` exit point:

```go
if deps.noDemo {
	if deps.daemonDone != nil {
		return <-deps.daemonDone
	}
	return nil
}
```

- `newUpCmd` gains `--foreground` / `-F` flag. When set, `RunE`:
  1. Resolves config via `resolveConfig(daemonFlags{}, os.ReadFile, os.LookupEnv, home)`.
  2. Applies `transcriptDir` to `cfg.Sources.JSONL` when `history` is also set.
  3. Builds `daemonParams` from cfg.
  4. Sets up a signal context via `signal.NotifyContext`.
  5. Starts `runDaemonWith(fgCtx, defaultDaemonDeps(), params)` in a goroutine that sends to a buffered `done` channel.
  6. Sets `deps.startDaemon = func() error { return nil }` (daemon is already running in goroutine).
  7. Sets `deps.daemonDone = done`, `deps.noDemo = true`.
  8. `deps.discoveryPath = params.discoveryPath` (from config, may differ from default path).
  9. Calls `runUp(fgCtx, cmd.OutOrStdout(), deps)` with the signal-aware context.

Steps:

- [ ] **Write failing tests** — add to `cmd/catacomb/up_test.go`:

```go
func TestRunUpForegroundBlocksOnDone(t *testing.T) {
	disc := daemon.Discovery{Addr: "127.0.0.1:12345", Token: "tok"}
	deps := fakeDepsWithDisc(t, disc)
	done := make(chan error, 1)
	done <- nil
	deps.daemonDone = done
	deps.noDemo = true
	require.NoError(t, runUp(context.Background(), io.Discard, deps))
}

func TestRunUpForegroundPropagatesDaemonError(t *testing.T) {
	disc := daemon.Discovery{Addr: "127.0.0.1:12345", Token: "tok"}
	deps := fakeDepsWithDisc(t, disc)
	done := make(chan error, 1)
	done <- errors.New("daemon exited unexpectedly")
	deps.daemonDone = done
	deps.noDemo = true
	err := runUp(context.Background(), io.Discard, deps)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "daemon exited")
}

func TestRunUpNoDaemonDoneIsNil(t *testing.T) {
	disc := daemon.Discovery{Addr: "127.0.0.1:12345", Token: "tok"}
	deps := fakeDepsWithDisc(t, disc)
	deps.daemonDone = nil
	deps.noDemo = true
	require.NoError(t, runUp(context.Background(), io.Discard, deps))
}

func TestUpCmdForegroundFlag(t *testing.T) {
	cmd := newUpCmd()
	f := cmd.Flags().Lookup("foreground")
	require.NotNil(t, f, "up must have --foreground flag")
	assert.Equal(t, "false", f.DefValue)
}
```

- [ ] **Run** `go test ./cmd/catacomb/ -run 'TestRunUpForeground|TestUpCmdForegroundFlag'` — expect FAIL.

- [ ] **Minimal impl** — modify `cmd/catacomb/up.go`:

  Add `daemonDone <-chan error` to `upDeps` (insert after the last existing field, `projectsDir string`):

```go
type upDeps struct {
	readDiscovery func(string) (daemon.Discovery, error)
	startDaemon   func() error
	installHooks  func() error
	pollHealthz   func(ctx context.Context, addr string) error
	sessionCount  func(ctx context.Context, disc daemon.Discovery) (int, error)
	openBrowser   func(string) error
	replayDemo    func(ctx context.Context, disc daemon.Discovery) error
	after         func(time.Duration) <-chan time.Time
	discoveryPath string
	waitSeconds   int
	noOpen        bool
	noDemo        bool
	history       bool
	projectsDir   string
	daemonDone    <-chan error
}
```

  In `runUp`, replace the `if deps.noDemo { return nil }` block with:

```go
if deps.noDemo {
	if deps.daemonDone != nil {
		return <-deps.daemonDone
	}
	return nil
}
```

  In `newUpCmd`, add the flag declaration and foreground setup in `RunE`. Insert this after `var noOpen, noDemo, global, history bool`:

```go
var foreground bool
```

  Add flag binding in `newUpCmd` return block:

```go
cmd.Flags().BoolVarP(&foreground, "foreground", "F", false, "run the daemon attached in the current process (no fork; Ctrl-C stops; for debugging)")
```

  In `RunE`, insert the foreground block BEFORE the `deps := upDeps{...}` literal construction:

```go
var daemonDone <-chan error
if foreground {
	home, herr := osUserHomeDir()
	if herr != nil {
		return fmt.Errorf("up: resolve home: %w", herr)
	}
	cfg, cerr := resolveConfig(daemonFlags{}, os.ReadFile, os.LookupEnv, home)
	if cerr != nil {
		return cerr
	}
	var transcriptDirFg string
	if history {
		projects, perr := claudeProjectsDir()
		if perr != nil {
			return perr
		}
		transcriptDirFg = projects
	}
	if transcriptDirFg != "" {
		enabled := true
		cfg.Sources.JSONL.Enabled = &enabled
		cfg.Sources.JSONL.TranscriptDir = transcriptDirFg
	}
	fgParams := daemonParams{
		store:              cfg.Store,
		sinks:              cfg.Sinks,
		sources:            cfg.Sources,
		discoveryPath:      resolveDiscovery(cfg.Daemon.Discovery),
		reaperWindow:       time.Duration(cfg.Daemon.ReaperWindow),
		maxShards:          cfg.Daemon.MaxShards,
		allowPayloadAccess: cfg.Daemon.AllowPayloadAccess,
		allowAnnotations:   cfg.Daemon.AllowAnnotations,
	}
	fgCtx, fgStop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer fgStop()
	done := make(chan error, 1)
	go func() { done <- runDaemonWith(fgCtx, defaultDaemonDeps(), fgParams) }()
	daemonDone = done
	discPath = fgParams.discoveryPath
	noDemo = true
}
```

  Add required imports `"os/signal"` and `"syscall"` to `up.go` if not already present.

  Add `daemonDone: daemonDone` to the `deps := upDeps{...}` literal.

  Change the `RunE` call from `return runUp(cmd.Context(), ...)` to use `fgCtx` when foreground (pass `cmd.Context()` in the non-foreground case, `fgCtx` in the foreground case). Simplest: always pass `cmd.Context()` — for non-foreground, this is the same context; for foreground, `fgCtx` is the signal-aware context derived from `cmd.Context()`. Since we declare `fgCtx` inside the if-block, instead pass ctx at top:

```go
runCtx := cmd.Context()
if foreground {
	// ... fgCtx setup ...
	runCtx = fgCtx
}
// ... deps construction ...
return runUp(runCtx, cmd.OutOrStdout(), deps)
```

- [ ] **Run** `go test -race ./cmd/catacomb/` — expect PASS (all existing up tests still green).
- [ ] **Run** `make cover` — expect threshold still met.
- [ ] **Commit:** `git add cmd/catacomb/up.go cmd/catacomb/up_test.go && git commit -m "feat(cmd/up): --foreground flag runs daemon attached in current process

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"`

---

## Task 7: Register `restart` and `logs` in `root.go`

**Files:**

- Modify: `cmd/catacomb/root.go`
- Modify: `cmd/catacomb/root_test.go`

**Interfaces:**

- Two new `observe(...)` entries in `newRootCmd`:
  - `root.AddCommand(observe(newRestartCmd()))` — after `newDownCmd()`.
  - `root.AddCommand(observe(newLogsCmd()))` — after `newRestartCmd()`.

Steps:

- [ ] **Write failing tests** — add to `cmd/catacomb/root_test.go`:

```go
func TestRestartAndLogsCmdsRegistered(t *testing.T) {
	root := newRootCmd()
	names := make(map[string]bool)
	for _, sub := range root.Commands() {
		names[sub.Use] = true
	}
	assert.True(t, names["restart"], "restart must be registered")
	assert.True(t, names["logs"], "logs must be registered")
}
```

- [ ] **Run** `go test ./cmd/catacomb/ -run TestRestartAndLogsCmdsRegistered` — expect FAIL.

- [ ] **Minimal impl** — modify `cmd/catacomb/root.go`. Add after `root.AddCommand(observe(newDownCmd()))`:

```go
root.AddCommand(observe(newRestartCmd()))
root.AddCommand(observe(newLogsCmd()))
```

- [ ] **Run** `go test -race ./cmd/catacomb/` — expect PASS.
- [ ] **Commit:** `git add cmd/catacomb/root.go cmd/catacomb/root_test.go && git commit -m "feat(cmd/root): register restart and logs commands

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"`

---

## Task 8 (Optional): Minor deferred fixes

These are clean-up items from PR2 that fit naturally here. Include if the scope allows; otherwise add a `FIXME` comment-free code note (a renamed sentinel or a TODO test assertion) and defer to a follow-up.

### M1 — `repro.Config` uses legacy fields at `daemon.go:174`

The current `d.SetReproConfig(repro.Config{OTLPEndpoint: p.otlpEndpoint, ...})` only picks up the OTLP endpoint when it was passed via the deprecated `--otlp-export-endpoint` flag; it ignores endpoints declared via `sinks` in config. Fix: after building the merged `sinks` slice, scan for the first OTLP sink and use its `Endpoint`/`Project`.

```go
var reproOTLPEndpoint, reproOTLPProject string
for _, sk := range sinks {
	if sk.Type == config.SinkOTLP {
		reproOTLPEndpoint = sk.Endpoint
		reproOTLPProject = sk.Project
		break
	}
}
if p.otlpEndpoint != "" {
	reproOTLPEndpoint = p.otlpEndpoint
	reproOTLPProject = p.otlpProject
}
d.SetReproConfig(repro.Config{
	OTLPEndpoint:  reproOTLPEndpoint,
	OTLPProject:   reproOTLPProject,
	TranscriptDir: sources.JSONL.TranscriptDir,
})
```

Test: `TestRunDaemonWithReproConfigFromSink` — build `daemonParams` with a sink of type `SinkOTLP`, no legacy `otlpEndpoint`, assert `SetReproConfig` receives the sink's endpoint. Requires reading the `Daemon.reproConfig` field in tests (expose via `d.ReproConfigForTest()` or check indirectly via a mock `SetReproConfig` injected through `daemonDeps`).

**Files:** `cmd/catacomb/daemon.go`, `cmd/catacomb/daemon_test.go`.

### M2 — Duplicate-sink re-validation after flag merge

After the legacy flags are merged into `sinks`, `config.Validate` is not re-run on the combined slice, so a user could specify the same type+target via both config.yaml and `--postgres-export-dsn`. The gap is documented; the fix (re-run `config.ValidateSinks` on the merged slice) is straightforward but requires a `config.ValidateSinks([]Sink) error` public function (extract from `Validate`). Defer to a follow-up PR if adding that function would extend scope.

---

## Task 9: Live verify on a real daemon (MANDATORY)

No task in this plan is complete until this task passes. 100% coverage is necessary but not sufficient.

**Steps:**

- [ ] Build: `make build` — expect clean.
- [ ] **Scenario A — enriched status:**
  - `catacomb up`
  - `catacomb status` — verify text output shows `store`, `sources`, `reaper`, `shards` rows.
  - `catacomb status --json` — pipe through `jq` or `python3 -m json.tool`; verify `store_backend`, `sink_types`, `sources_enabled`, `reaper_window`, `max_shards` fields are present and correct; verify NO DSN/URI/password in the output.
  - `catacomb down`

- [ ] **Scenario B — restart:**
  - `catacomb up`
  - Note PID from `catacomb status`
  - `catacomb restart`
  - `catacomb status` — verify PID changed; daemon is healthy.
  - `catacomb restart --json` — verify `{"stopped": true, "started": true, ...}`.
  - `catacomb down`

- [ ] **Scenario C — logs:**
  - `catacomb up`
  - `catacomb logs` — verify daemon startup lines appear.
  - `catacomb logs -f` in a background terminal — trigger a hook event (`catacomb hook PreToolUse '{"session_id":"s1"}'`); verify new log lines appear in the `logs -f` terminal within ~1 second.
  - `catacomb down`

- [ ] **Scenario D — up --foreground:**
  - `catacomb up --foreground` — verify process stays attached in the terminal, URL is printed, browser opens (or `--no-open` suppresses it), logs appear on stdout.
  - Press Ctrl-C — verify process exits cleanly.

- [ ] **Scenario E — backward compatibility:**
  - `catacomb up` still works (no regression from `daemonDone` field addition).
  - `catacomb status` without `--json` still produces the tabwriter table.

- [ ] **Commit final verification:** `git add -p` (any trivial fixups from live run) and commit, then `make cover` must still show 100%.

---

## Self-review mapping

| Feature | Task(s) | Secret-free? | Verified symbols |
|---------|---------|--------------|-----------------|
| Discovery summary fields | T1 | Yes — TYPE strings only, no DSN/URI/path | `daemon.Discovery` (discovery.go:13), `WriteDiscovery`/`ReadDiscovery` (discovery.go:62,76) |
| Populate summary in runDaemonWith | T2 | Yes | `runDaemonWith` (daemon.go:160), `daemonParams` (daemon.go:27), `config.Sink.Type` (config.go:97) |
| `status --json` + enriched text | T3 | Yes — reads from Discovery, no new endpoint | `statusDeps` (status.go:25), `runStatus` (status.go:48), `fetchSessionCounts` (status.go:98) |
| `restart` command | T4 | N/A | `stopDaemon` (down.go:354), `waitGone` (down.go:37), `buildStartDaemon` (up.go:103), `prodPollHealthz` (up.go:161) |
| `logs [-f]` command | T5 | N/A | log path = `discPath+".log"` (up.go:109), `daemon.DiscoveryPath()` (discovery.go:32) |
| `up --foreground` | T6 | N/A | `runDaemonWith` (daemon.go:160), `daemonDeps` (daemon.go:20), `resolveConfig` (config_resolve.go:40), `upDeps` (up.go:21), `runUp` (up.go:180) |
| Register commands | T7 | N/A | `newRootCmd` (root.go:11), `groupObserve` (root.go:6) |
| Live verify | T9 | Must confirm no secret leaks | Full binary test |
