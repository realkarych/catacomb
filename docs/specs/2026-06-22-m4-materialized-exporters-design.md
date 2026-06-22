# M4 — Materialized graph exporters (postgres + neo4j) design

**Status:** approved (autonomous run, mandate-delegated)
**Date:** 2026-06-22
**Milestone:** M4 (spec §10 Exporters, §18 "M4 — neo4j + postgres exporters. Materialized upsert + CDC + snapshot.")
**Consumes:** the M1c CDC bus + the OTLP exporter pattern (`export/otlp`), the daemon's snapshot-then-attach consumer wiring, ADR-0007 (materialized upsert + CDC + snapshot), ADR-0015 (exporter correctness under failure).

## 1. Goal

Persist the live canonical graph into two external materialized stores —
**PostgreSQL** and **Neo4j** — as an idempotent, rev-guarded upsert of nodes and
edges, fed by the CDC bus (live) and seedable by a snapshot. Each is a `catacomb`
exporter selectable by a connection-string flag, runnable alongside the existing
OTLP passthrough exporter.

## 2. Materialized semantics vs. the OTLP exporter

The OTLP exporter buffers a run until a lifecycle close because OTLP **spans are
immutable**. Postgres and Neo4j are **mutable stores**, so the materialized
exporters do the opposite: **apply every delta live** as an idempotent upsert.

| Delta kind | Materialized action |
|---|---|
| `node_upsert`, `node_status` | upsert the node, rev-guarded (`WHERE excluded.rev > rev` / `MERGE … SET` guarded) |
| `edge_upsert` | upsert the edge, rev-guarded |
| `node_merge` | delete the old id, upsert the new id (id change) |
| `edge_delete` | delete the edge |
| `run_started` / `session_ended` / `run_ended` | upsert run-status metadata (best-effort); no buffering |

No buffer-until-terminal, no exactly-once span emission — the store always holds
the latest rev-guarded state, which is the correct convergent materialization
(ADR-0007). Provisional/heuristic statuses are written like any other; a later
higher-rev delta supersedes them (the rev-guard makes reordered/stale deltas
no-ops, ADR-0015).

## 3. The shared `Exporter` interface (extracted)

Today `export/otlp` is standalone and the daemon assumes its methods implicitly.
M4 extracts a shared contract so the daemon drives N exporters polymorphically.

```go
// package export
type Exporter interface {
    Name() string
    ApplyDelta(ctx context.Context, d cdc.GraphDelta) error
    SnapshotState(ctx context.Context, nodes []*model.Node, edges []*model.Edge) error
    FlushRun(ctx context.Context, runID string) error
    Shutdown(ctx context.Context) error
}
```

`export/otlp.Exporter` already satisfies this (add a `var _ export.Exporter =
(*Exporter)(nil)` assertion). `SnapshotState` is the per-graph snapshot the daemon
already calls; `FlushRun` stays in the interface for OTLP (a no-op for the
materialized exporters — they upsert live, nothing to flush, so they return nil).
(The spec §10 `Snapshot(ctx, filter)` is the daemon-side orchestration; the
exporter-facing seam is `SnapshotState(nodes, edges)`, matching the existing OTLP
shape — no behavior change to OTLP.)

## 4. Multi-exporter daemon wiring

`startExporter` currently builds one OTLP exporter and one consumer. M4
generalizes it to a list:

- The daemon holds an ordered list of *exporter configs* (each: a `name` + a
  connection string), set from flags. For each non-empty config, `startExporter`:
  1. constructs the exporter via its factory (a `newXFn` package-var seam),
  2. under `d.mu`: snapshots every graph into it (`SnapshotState`) + flushes
     already-ended runs (`FlushRun`), then `d.bus.Subscribe(exporterBufSize)`,
  3. launches one ctx-bound goroutine draining that consumer into `ApplyDelta`,
     `Unsubscribe` + `Shutdown` on ctx-done.
- `d.exporterConsumer` (singular) becomes `d.exporterConsumers []*cdc.Consumer`;
  `exporter_lag` sums `Dropped()` across them. The snapshot-then-attach race
  discipline (under `d.mu`, no gap/dup) is unchanged and per-exporter.
- Each exporter is independent on the bus: the fan-out + per-consumer
  coalesce/drop means a slow/failing postgres exporter cannot block neo4j, OTLP,
  or the SSE/gRPC subscribers (same property M3 relies on).

Flags (`cmd/catacomb/daemon.go`, threaded via `runDaemonWith` → setters, mirroring
`--otlp-export-endpoint`): `--postgres-export-dsn` (empty = disabled),
`--neo4j-export-uri` + `--neo4j-export-user` + `--neo4j-export-password` (empty
uri = disabled). All default empty (opt-in), like OTLP.

## 5. Testability seam recipe (100% coverage, no DB)

Mirror `export/otlp` exactly. Each materialized exporter:

- defines a **narrow private interface** for the store operations it needs
  (postgres: an `execer` with `Exec`/`Query`/`Begin`/`Close`; neo4j: a
  `runner`/`session` with `Run`/`Close`), implemented in production by a thin
  adapter over the real driver;
- a public `New(ctx, …)` → `newFull(ctx, …, factory)` with the factory injectable;
  plus an `ExporterWithClient(client)` test constructor (like
  `ExporterWithSpanExporter`);
- a `record*` fake (records the SQL/Cypher + params) for unit tests of
  `ApplyDelta`/`SnapshotState` — **no network**;
- the **lazy factory** is line-covered by a "construct" test with a valid
  connection string and **no live DB**: `pgxpool.New(ctx, dsn)` validates config
  without dialing (lazy pool), and `neo4j.NewDriverWithContext(uri, auth)` does not
  connect until first use — so constructing the real client succeeds DB-free and
  covers the factory body. Real query execution is never hit in unit tests (it's
  behind the seam).

Result: **no `.testcoverage.yml` exclusion** for the new packages — 100% via seam
injection, exactly as `export/otlp` does.

## 6. Postgres exporter (`export/postgres`)

- **Schema** (created idempotently on first use via `CREATE TABLE IF NOT EXISTS`):
  - `nodes(id TEXT PRIMARY KEY, run_id TEXT, type TEXT, name TEXT, status TEXT,
    tier TEXT, parent_id TEXT, agent_id TEXT, t_start TIMESTAMPTZ, t_end
    TIMESTAMPTZ, duration_ms BIGINT, tokens_in BIGINT, tokens_out BIGINT, cost_usd
    DOUBLE PRECISION, payload_hash TEXT, attrs JSONB, annotations JSONB, rev
    BIGINT)`
  - `edges(id TEXT PRIMARY KEY, run_id TEXT, type TEXT, src TEXT, dst TEXT, attrs
    JSONB, rev BIGINT)`
  - **Payload is NOT a column** (ADR-0020 redaction; only `payload_hash`).
- **Upsert (rev-guard):** `INSERT … ON CONFLICT (id) DO UPDATE SET … WHERE
  excluded.rev > nodes.rev` — a reordered/stale delta is a no-op. `node_merge` =
  `DELETE … WHERE id=$old` then upsert `$new`. `edge_delete` = `DELETE … WHERE
  id=$id`.
- **Snapshot:** one transaction, batched upserts of all nodes then edges.
- **Seam:** `execer { Exec(ctx, sql, args…) (pgconn.CommandTag, error); Begin(ctx)
  (pgx.Tx, error); Close() }` (or the minimal subset actually used); production
  wraps a `*pgxpool.Pool`; tests use a `recordExecer`.
- **Dep:** `github.com/jackc/pgx/v5` (pure-Go).

## 7. Neo4j exporter (`export/neo4j`)

- **Nodes** as labeled nodes by `NodeType` (`:Session`, `:UserPrompt`,
  `:AssistantTurn`, `:ToolCall`, `:Subagent`, `:McpCall`, `:HookEvent`,
  `:Marker`), keyed by canonical `id`; **edges** as relationships by `EdgeType`
  (`PARENT_OF`, `NEXT`, `IN_PHASE`, `DATA_DEP`).
- **Upsert (rev-guard):** `MERGE (n {id:$id}) … SET n += $props` guarded by
  `WHERE coalesce(n.rev,-1) < $rev` (Cypher); relationships likewise. `node_merge`
  = detach-delete old + merge new. `edge_delete` = match-delete the relationship.
- **Snapshot:** batched `MERGE` (UNWIND a parameter list) in one session.
- **Seam:** a `runner { Run(ctx, cypher, params) error; Close(ctx) error }`
  abstraction over a Bolt session/driver; production wraps the neo4j driver; tests
  use a `recordRunner`.
- **Dep:** `github.com/neo4j/neo4j-go-driver/v5` (pure-Go).
- Property values are scalars + JSON-encoded `attrs` (Neo4j has no nested-map
  property type), mirroring the gRPC/SSE attr encoding.

## 8. Decomposition

### 8.1 M4a — shared interface + multi-exporter wiring + postgres (this plan)

1. `export.Exporter` interface + `var _ export.Exporter = (*otlp.Exporter)(nil)`
   conformance (no behavior change; `FlushRun` already exists on OTLP).
2. Daemon multi-exporter wiring: `d.exporterConsumers []*cdc.Consumer`,
   `startExporter` loops over configured exporters (OTLP path preserved
   byte-for-byte in behavior), `exporter_lag` sums consumers; a per-exporter
   config list set from flags.
3. `export/postgres` exporter (schema, rev-guarded upsert, node_merge/edge_delete,
   snapshot, `execer` seam + `recordExecer` fake + lazy-pool construct test).
4. Wire `--postgres-export-dsn` flag through `runDaemonWith` → setter →
   `startExporter`; e2e wiring test (a fake exporter via the `newPostgresFn` seam
   observes snapshot + a live delta).

### 8.2 M4b — neo4j (next plan)

1. `export/neo4j` exporter (labels/rels, MERGE rev-guard, node_merge/edge_delete,
   snapshot, `runner` seam + `recordRunner` fake + lazy-driver construct test).
2. Wire `--neo4j-export-uri`/`-user`/`-password` through the multi-exporter
   machinery from M4a; e2e wiring test.

### 8.3 Independence

M4a delivers the shared interface, the multi-exporter daemon, and a complete
postgres exporter — independently mergeable. M4b adds neo4j on top of M4a's
machinery. (M4a must land first; M4b depends on the interface + wiring.)

## 9. Constraints (inherited, binding)

Go 1.26 pure-Go (no cgo; `pgx/v5` + `neo4j-go-driver/v5` are pure-Go); **NO
comments except `//go:build|//go:embed|//go:generate`** (`internal/codepolicy`);
**100% line coverage under `-race`** (`make cover`) achieved via the seam recipe
— **no DB in tests, no new coverage exclusions**; golangci-lint v2 clean (gofumpt,
goimports local-prefix `github.com/realkarych/catacomb`, govet shadow, **forbidigo
bans `time.Sleep`**, unparam, errcheck, rowserrcheck, bodyclose); **never `go mod
tidy`** — add `pgx`/`neo4j` via `go get`; single-mutex daemon (`d.mu`) — exporter
construction + snapshot-then-attach hold it, per-consumer drain loops do NOT;
cross-platform (`GOOS=windows go build ./...` clean — both drivers are
cross-platform pure-Go); loopback + bearer unaffected (exporters are outbound);
commit per task; never commit to master mid-plan.

## 10. Testing strategy (M4-specific)

- **Exporter logic (no DB):** `ApplyDelta` for each kind (upsert rev-guard:
  higher-rev applies, lower/equal-rev no-op; node_merge delete+insert; edge_delete;
  lifecycle best-effort) asserted against the `record*` fake (inspect emitted
  SQL/Cypher + params); `SnapshotState` batches all nodes+edges; payload never
  emitted (no payload column/property); attrs JSON-encoding.
- **Factory construct test (no DB):** `New(ctx, dsn/uri)` builds the real
  lazy client (pgxpool/neo4j driver) without connecting → covers the factory; an
  error path (malformed dsn/uri) covers the error branch.
- **Daemon multi-exporter wiring:** with the `newPostgresFn` seam returning a fake
  exporter, assert it receives the snapshot at attach + a live delta after an
  ingest, and that disabling (empty flag) attaches nothing; `exporter_lag`
  aggregates; OTLP + postgres can run together (two consumers).
- All `-race`; host + windows build.

## 11. Deferred → M4+ / M5 / Step 7

- **Per-exporter `seq`-cursor resume** (ADR-0015 resume-from-cursor): the daemon
  snapshots-on-attach (covers restart correctness); a durable per-exporter cursor
  to skip re-snapshot is a later optimization.
- **Adjacency views / `pg_notify`** (postgres) and **batched-UNWIND tuning**
  (neo4j) — beyond the core materialization.
- **Payload export opt-in** (currently always omitted, ADR-0020).
- **`catacomb export --to postgres|neo4j` one-shot snapshot CLI** (the daemon
  streaming path is the milestone; a one-shot verb can reuse `SnapshotState`
  later).
- **Connection retry/backoff** on transient DB outage (the drain loop currently
  swallows `ApplyDelta` errors like OTLP; a retry/backoff policy is a follow-up).
