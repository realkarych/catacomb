# Catacomb — Explicit Configuration (Sources / Store / Sinks) + Daemon Lifecycle Design

**Status:** Draft for review
**Date:** 2026-06-30
**License:** Apache-2.0
**Language:** Go

> *Two things in the CLI are implicit and expensive to get wrong. First, once hooks are wired, the daemon **always** persists to a SQLite file in the current working directory — there is no knob to send data elsewhere, to fan it out, or to not persist at all. Second, the daemon is an orphan process: started by a fork inside `up`, discoverable only through a JSON file, with no `down`, no `restart`, and a `status` that says little. This spec makes the data path explicit and configurable, and gives the daemon a real lifecycle.*

---

## 1. Summary

Today configuration is scattered across cobra flags, environment variables, and side-effecting edits to `~/.claude/settings.json`. The primary store (SQLite) is hard-wired and cannot be disabled or swapped. Downstream destinations exist (`export.Exporter`: postgres, neo4j, otlp, jsonl) but are configured through unrelated one-off flags. The daemon has no managed lifecycle beyond `kill <pid>`.

This change introduces a single declarative configuration file and reorganizes the data path into three explicit layers built on the **two interfaces that already exist** — `store.Store` (authoritative read/write) and `export.Exporter` (write-only fan-out):

- **Sources** — which ingest channels are active (hooks / otel / stream_json / jsonl).
- **Primary store** — exactly one authoritative backend (`sqlite` | `memory` | `postgres`), selectable.
- **Sinks** — zero or more write-only downstream destinations.

It also gives the daemon a managed lifecycle: `down`, `restart`, an enriched `status`, `logs`, and a foreground run mode — all keyed off the existing discovery file, keeping the plain-process model (no systemd/launchd).

## 2. Goals & Non-Goals

### Goals

- One declarative config file (`~/.catacomb/config.yaml`) makes every previously-implicit knob visible in one place.
- `store.backend: memory` is a first-class **ephemeral / live-only** mode — the honest answer to "don't write to the DB", with no `/dev/null` hacks.
- The primary store backend is selectable via the existing `store.Store` interface; `sqlite` and `memory` ship in this work.
- Ingest sources are individually enable/disable-able from config.
- Sinks are declared as a uniform list and built into `[]export.Exporter`.
- The daemon gains `down`, `restart`, enriched `status`, `logs`, and `up --foreground`.
- Existing flags continue to work as the highest-precedence override; existing behavior and tests are preserved (one documented default change: DB location).
- Default SQLite path moves from `./catacomb.db` to `~/.catacomb/catacomb.db`.

### Non-Goals

- **postgres-as-primary implementation.** The backend is made pluggable and `postgres` is a recognized value, but the full `store.Store` implementation on Postgres (observation-log read-back, recover, tail cursors, annotations, quarantine) is deferred to a follow-up. It returns a sentinel error if selected.
- **systemd / launchd unit generation.** The plain-process model stays; OS-supervisor integration is out of scope.
- **Hot-reload of config.** Changing config requires `restart`.
- **Teardown escalations** (`down --uninstall` / `--purge`). `down` means *stop the daemon*. Escalations remain compatible with the previously discussed teardown design but are a separate step.

## 3. Background — how it works today

- The daemon is started by a fork inside `catacomb up` (`os/exec`, detached), or run directly via `catacomb daemon`. It listens on a random loopback TCP port (HTTP + gRPC) and advertises itself through a discovery JSON file (`~/.catacomb/run/daemon.json`, or `$XDG_RUNTIME_DIR/...`, or `$CATACOMB_DISCOVERY`) carrying addr, token, PID, and DB path.
- Four ingest sources reconcile into one canonical graph: hooks (`POST /hook/{type}`), OTLP (`POST /v1/traces`), stream-json (`POST /v1/stream-json`), and JSONL transcript tailing. Sources are toggled only by side effects: editing `settings.json`, setting `OTEL_*` env, or passing `--transcript-dir`.
- Every observation is reduced and persisted to SQLite via `applyAndPersist`; there is no flag to disable this. SQLite (`store.Store`) is authoritative: the daemon recovers from it on start and serves UI/SSE reads from it.
- Downstream exporters (postgres/neo4j/otlp/jsonl) are constructed from individual daemon flags (`--postgres-export-dsn`, `--neo4j-export-*`, `--otlp-export-*`).
- There is no `catacomb down`/`restart`; the daemon is stopped with `kill <pid>` or a signal.

## 4. Architecture — three explicit layers

| Layer | Interface | Cardinality | Meaning |
|-------|-----------|-------------|---------|
| **Sources** | ingest (new registry) | 0..N enabled | where events come from: hooks / otel / stream_json / jsonl |
| **Primary store** | `store.Store` (exists) | exactly 1 | authoritative write + recover + reads for UI/SSE |
| **Sinks** | `export.Exporter` (exists) | 0..N | write-only fan-out: postgres / neo4j / otlp / jsonl |

The key distinction the layers make explicit: the **primary store is read-back-authoritative** (the daemon recovers from it and serves the UI from it), whereas **sinks are one-directional**. "Don't write to the DB" therefore maps cleanly to `store.backend: memory` — the in-memory reduce graph still serves UI/SSE live, nothing is persisted, and `Recover()` is a no-op (state is lost on restart). It does **not** mean "no store object": the daemon always has exactly one `store.Store`; `memory` is just a non-durable one.

### 4.1 Mapping the request onto the layers

- **"write / don't write to DB"** → `store.backend: sqlite | memory`.
- **"configurable sinks"** → the `sinks` list (replaces scattered exporter flags).
- **"other sources"** → the `sources` registry (explicit per-channel enable).
- **"pluggable primary backend"** → `store.backend` selection over `store.Store`.

## 5. Configuration system

### 5.1 File, format, location

- Format: **YAML** (`gopkg.in/yaml.v3`, already present transitively in the module graph — no new dependency tree).
- Default path: `~/.catacomb/config.yaml`. Overridable via `--config <path>` flag or `$CATACOMB_CONFIG`.
- A missing file is not an error — built-in defaults apply (no surprises, parity with today).
- **Strict decoding** (`yaml.Decoder.KnownFields(true)`): an unknown key is a hard error pointing at the offending field. This is deliberate — silent typos are exactly the "implicitness" this spec removes.

### 5.2 Schema

```yaml
daemon:
  discovery: ~/.catacomb/run/daemon.json
  reaper_window: 30m
  max_shards: 4096
  allow_payload_access: false
  allow_annotations: false

store:                      # PRIMARY — exactly one, authoritative
  backend: sqlite           # sqlite | memory | postgres
  sqlite:
    path: ~/.catacomb/catacomb.db
  # postgres:               # recognized but not implemented yet (sentinel error)
  #   dsn: postgres://user:pass@host/db

sources:                    # ingest — each independently toggleable
  hooks:       { enabled: true }
  otel:        { enabled: true }
  stream_json: { enabled: true }
  jsonl:
    enabled: true
    transcript_dir: ~/.claude/projects
    exclude: ["**/scratch/**"]

sinks:                      # SECONDARY — write-only fan-out, 0..N
  - { type: postgres, dsn: "postgres://user:pass@host/db" }
  - { type: neo4j, uri: "bolt://host:7687", user: neo4j, password: "secret" }
  - { type: otlp, endpoint: "http://localhost:4317", project: catacomb }
  - { type: jsonl, path: "./catacomb-export.jsonl" }
```

### 5.3 `config` package (pure)

- New package `config`, **no I/O of its own** (AGENTS: wire dependencies from `main`, no global state, no constructors with hidden I/O).
- Types: `Config{ Daemon DaemonConfig; Store StoreConfig; Sources SourcesConfig; Sinks []Sink }`.
- Pure functions:
  - `Parse(data []byte) (Config, error)` — strict YAML decode into a partial config.
  - `Defaults() Config` — built-in defaults.
  - `Merge(base, override Config) Config` — field-wise overlay (override wins where set).
  - `Validate(Config) error` — invariants (see §5.5).
- The cmd layer performs I/O: read file bytes, read `os.Environ`, then apply flag overrides. The pure package is trivially testable to 100%.

### 5.4 Precedence

`defaults < config file < environment < flags`.

Flags override only when **explicitly set** (cobra `Flag.Changed`), so an unset flag never clobbers a file value. Environment sits between file and flags, mapped through a **fixed, small set** of variables for predictability (`CATACOMB_CONFIG`, `CATACOMB_DISCOVERY`, the SQLite store path, plus the established `OTEL_*` vars where they already apply) — not a general `CATACOMB_*`→key reflection scheme. `~` and `$VAR` in path values are expanded by the cmd layer before validation.

### 5.5 Validation invariants

- `store.backend` ∈ {`sqlite`, `memory`, `postgres`}; required sub-fields present for the chosen backend (`sqlite.path` for sqlite; `postgres.dsn` for postgres).
- `postgres` backend → `ErrBackendNotImplemented` (clear, actionable; recognized but deferred).
- Each `sinks[].type` ∈ {`postgres`, `neo4j`, `otlp`, `jsonl`} with its required fields; unknown type → error.
- `sources.jsonl.enabled: true` with empty `transcript_dir` → error (nothing to tail).
- Duplicate sink of the same type+target is rejected (avoids double fan-out).

## 6. Store layer (pluggable primary)

- `store.Open(cfg config.StoreConfig) (store.Store, error)` — factory, switches on `backend`:
  - `sqlite` → existing `OpenSQLite` (unchanged behavior; path from config).
  - `memory` → new `store/memory` implementation.
  - `postgres` → `ErrBackendNotImplemented`.
- New package **`store/memory`** implementing the full `store.Store` interface in memory: observations in a seq-ordered slice; runs / annotations / tail-cursors as maps; quarantine counter; guarded by a mutex (the daemon calls the store from multiple goroutines). `MaxSeq`/`ObservationsSince`/`ObservationsForExecution` filter the slice; `Recover` reads back as usual (empty after a fresh start).
- Bonus: `store/memory` is a real, shared implementation that can replace bespoke in-test fakes, which helps maintain 100% coverage rather than fighting it.
- Default sqlite path becomes `~/.catacomb/catacomb.db`. The batch CLI (`runs`/`inspect`/`snapshot`/`export`) adopts the same default so it reads the same database without `--db`.

## 7. Sources registry

- The daemon receives `SourcesConfig` and mounts a channel **only when enabled**:
  - `hooks` → routes `POST /hook/{type}`
  - `otel` → route `POST /v1/traces`
  - `stream_json` → route `POST /v1/stream-json`
  - `jsonl` → transcript tail loop (started only if enabled and `transcript_dir` is set)
- A disabled push source means its route is not registered (requests get 404). A disabled pull source means the tailer is not started.
- **Documented seam:** disabling the `hooks` source at the daemon does **not** remove the hook forwarders from `settings.json`. Wiring/unwiring `settings.json` remains the job of `install-hooks` (and a future `down --uninstall`). The daemon-side toggle controls acceptance, not the client-side forwarders.

## 8. Sinks (fan-out)

- `export.Build(sinks []config.Sink) ([]export.Exporter, error)` — factory that constructs the exporter list from config, replacing the daemon's current field-by-field construction (`otlpEndpoint`, `postgresDSN`, `neo4j*`).
- Each `type` maps to an existing exporter implementation; the daemon's existing fan-out path (`publishDelta` → exporters) is unchanged and simply consumes the built list.
- **Error policy:**
  - Primary store unavailable → fatal (the daemon cannot do its job).
  - Sink **config** error (unknown type, missing field) → fatal at startup (predictable, explicit).
  - Sink **runtime** error (e.g., Postgres connection drops mid-run) → counted and logged, ingest continues — matching how exporters behave today; a flaky downstream must not stop observation.

## 9. Daemon lifecycle (a)

The discovery file (carrying PID, addr, token, started-at) becomes the explicit lifecycle handle. The process model is unchanged — a plain detached process.

- **`catacomb down`** — read discovery; if absent or stale (PID not alive) report "not running" and exit success (idempotent), cleaning up a stale discovery file. If running: send `SIGTERM` to the PID, poll liveness until exit or `--timeout` (default 10s), then `SIGKILL`; remove the discovery file on confirmed exit. Flags: `--timeout`, `--json`, `--yes`. PID-reuse is acknowledged: liveness is a best-effort `signal 0` check; the caveat is documented.
- **`catacomb restart`** — `down` then `up`, re-reading the same config/flags.
- **`catacomb status`** (enrich existing) — daemon pid/addr/uptime, primary store backend, active sources, active sinks, counts (store-write errors, quarantine count), health. Adds `--json`, which also emits the **resolved** config (post-precedence) so an agent can introspect the live wiring.
- **`catacomb logs [-f]`** — print (and optionally follow) the daemon log file (`<discovery>.log`).
- **`catacomb up --foreground`** — run the daemon attached in the current terminal (no fork; logs to stdout; Ctrl-C stops). Reuses `runDaemonWith`. Intended for debugging.
- All process control (signal send, liveness check, clock, filesystem) sits behind **consumer-declared interfaces** (AGENTS dependency-inversion) so the commands are unit-testable without spawning real processes. A **live-verify on a real daemon is mandatory** before completion.

## 10. Backward compatibility & migration

- Every existing flag keeps working as a top-precedence override, mapped onto the new config:
  - `--db` → `store.sqlite.path`
  - `--postgres-export-dsn` → a `postgres` sink
  - `--neo4j-export-uri/-user/-password` → a `neo4j` sink
  - `--otlp-export-endpoint/-project` → an `otlp` sink
  - `--transcript-dir` / `--transcript-exclude` → `sources.jsonl`
  - `--reaper-window` / `--max-shards` / `--allow-payload-access` / `--allow-annotations` → `daemon.*`
- Old flags are marked deprecated in `--help` but remain functional, so the existing (100%-tested) surface keeps passing.
- **One behavior change:** the default SQLite path moves to `~/.catacomb/catacomb.db`. `--db ./catacomb.db` (or the config equivalent) reproduces the old location. This is called out in `--help` and the README; the batch CLI default moves in lockstep.

## 11. Error handling summary

- Config parse/validate failures → fail fast at startup with a precise message (YAML position for decode errors). Sentinels: `ErrNoStoreBackend`, `ErrUnknownSink`, `ErrBackendNotImplemented`, checked with `errors.Is`.
- Missing config file → defaults (not an error). Malformed file → error.
- Primary store open failure → fatal. Sink config failure → fatal. Sink runtime failure → counted/logged, non-fatal.
- `down` on a not-running daemon → idempotent success; stale discovery is cleaned.

## 12. Testing strategy (AGENTS: TDD, 100% coverage, live-verify)

- `config`: table-driven tests for parse / defaults / merge / validate / precedence and strict-decode rejection of unknown keys.
- `store/memory`: a **shared store-contract test suite** exercised against both `memory` and `sqlite`, guaranteeing behavioral parity.
- `store.Open` / `export.Build`: factory wiring with injected dependencies; `postgres` primary returns the sentinel.
- Lifecycle commands: inject fs / clock / signal-sender / liveness-checker; cover idempotent `down`, stale-discovery cleanup, timeout→SIGKILL, `restart`, enriched `status` (`--json`), `logs -f`, `up --foreground`.
- **Live-verify on a real daemon is required** (the project's hard-won, repeatedly-reconfirmed lesson: green 100% coverage has missed every severe real bug). Minimum live scenarios: memory mode persists nothing yet serves the UI; a daemon with a postgres sink fans out; `down`/`restart`/`status`/`logs`; `up --foreground`.

## 13. Rollout / PR breakdown (indicative)

Likely three logical PRs (finalized in the implementation plan):

1. **`config` package + pluggable store** — config schema/parse/merge/validate/precedence, `store.Open`, `store/memory`, default-path move, flag→config mapping. No behavior change beyond default path.
2. **Sources + sinks from config** — `export.Build`, source-registry gating in the daemon, deprecation of the old exporter/source flags (kept functional).
3. **Daemon lifecycle** — `down`, `restart`, enriched `status`, `logs`, `up --foreground`.
