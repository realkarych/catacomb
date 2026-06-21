# M1c-2 — OTLP/OpenInference passthrough exporter + daemon wiring Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the first real CDC consumer — a pure-Go `export/otlp` passthrough exporter that subscribes to the delta bus, maps catacomb graph nodes to OTLP spans carrying both OpenInference and OTel GenAI attributes, reconstructs the trace tree with deterministic span/trace IDs, and ships batches to a downstream OTLP sink (gRPC or HTTP by endpoint scheme) so any OTel backend (Langfuse, Phoenix, Honeycomb, Tempo, …) can ingest catacomb's reconstructed Claude Code trace tree. Then wire it into the daemon: snapshot-then-attach, a consumer goroutine under `Serve`'s context, an `exporter_lag` metric, and a `--otlp-export-endpoint` CLI flag. All exporter logic is tested to 100% line coverage under `-race` via a `spanExporter` seam (a fake recording exporter) with NO network.

**Architecture:** The exporter is a CDC consumer, not a `Tracer`. The OTel SDK `Tracer` generates its own IDs and cannot set arbitrary parent span IDs, so we never use a `Tracer`. Instead we build `go.opentelemetry.io/otel/sdk/trace/tracetest.SpanStub` values (`Name`, a `SpanContext` carrying our deterministic `trace.TraceID`/`trace.SpanID`, a `Parent` `SpanContext` carrying the parent node's deterministic `SpanID`, `StartTime`/`EndTime`, `Attributes`, `Status`), call `.Snapshot()` to get `sdktrace.ReadOnlySpan`, and hand the batch to the OTLP client's `ExportSpans(ctx, []ReadOnlySpan)`. IDs are deterministic so parent linkage works across separately-mapped nodes: `spanID(nodeID) = sha256(nodeID)[:8]`, `traceID(runID) = sha256(runID)[:16]`. A child's `Parent.SpanID = spanID(parentNodeID)` therefore always equals the parent node's own `spanID(parentNodeID)`. The exporter holds a `nodeMap[nodeID]*nodeState` (latest scalar/token/timing snapshot + a per-node latest `delta.Rev` guard) and a `parents[childNodeID] = parentNodeID` map (built from `edge_upsert` deltas / the snapshot's `parent_child` edges) grouped per run. Lifecycle deltas (`session_ended`/`run_ended`) flush the run's buffer to spans. The real OTLP client (`otlptracehttp.New` / `otlptracegrpc.New`, chosen by endpoint scheme) is constructed in `New`; both constructors are lazy (they do NOT dial), so `New` tests cover both branches without a network. A `spanExporter` interface (`ExportSpans` + `Shutdown`) is the testability seam: `New` builds the real client; an internal constructor `newWithExporter(...)` injects a fake recording exporter so the mapper and finalization are asserted in-process. The daemon, in `Serve`, when an endpoint is configured: builds the exporter (self-loop-guarded against both daemon listener addresses), pushes a `g.Snapshot()` of current state via `exporter.Snapshot(...)`, subscribes a consumer, and runs `for delta := range consumer.C { _ = exporter.ApplyDelta(ctx, delta) }` in a goroutine that exits on ctx cancel (Unsubscribe on exit). The daemon reads `delta.Node` COPIES off the bus channel — it never touches `d.mu`.

**Tech Stack:** Go 1.26, pure-Go (no cgo). New deps (M1c-2 only): the OTel SDK client stack at v1.44.0 (see Task 1). `testify` (`assert`/`require`). White-box tests (`package otlp`, `package daemon`, `package main`). Concurrency tests use channels / `require.Eventually` — never `time.Sleep` (forbidigo). The exporter never re-implements the OTLP wire protocol; the SDK client handles connection pooling, retry, and backpressure.

---

## Global Constraints

- Go 1.26 pure-Go (`modernc.org/sqlite`, no cgo). M1c-2 adds the OTel-SDK client deps (Task 1) and NOTHING else.
- **NO comments** in Go except `//go:build`, `//go:embed`, `//go:generate` (`internal/codepolicy`). Zero doc comments, zero inline comments.
- **100% line coverage** under `-race` (`make cover`) — exporter logic tested via the `spanExporter` seam (a fake recording exporter): self-loop guard (both-match AND no-match), scheme selection (both gRPC and HTTP branches), finalization (ready / lifecycle-flush / provisional-held / stale-after-close), mapper (ALL node kinds + token/cost present AND absent), rev-guard drop, parent linkage AND no-parent root.
- **NO dead code** — every exported and unexported symbol is reached by a genuine assertion-bearing test. No speculative options or fields.
- `golangci-lint v2` clean: gofumpt, goimports local-prefix `github.com/realkarych/catacomb`, govet shadow, **forbidigo bans `time.Sleep`** (the exporter consumer loop and ALL tests use channels / `require.Eventually`, never sleeps), errcheck, bodyclose, unparam, unparam.
- **NEVER `go mod tidy`** (binding repo rule). Add each new dependency with `go get <module>@v1.44.0` ONE AT A TIME; verify `go build ./...` after each.
- **Single-mutex daemon (`d.mu`).** The exporter goroutine reads delta COPIES off the bus channel — it NEVER touches `d.mu`. The bus already shallow-copies `Node`/`Edge` at the publish boundary (M1c-1).
- **Carry-forward 1 (race-safety):** the exporter reads ONLY scalar/token/timing fields from `delta.Node` (`Status`/`Rev`/`Type`/`Name`/`RunID`/`TStart`/`TEnd`/`TokensIn`/`TokensOut`/`CostUSD`). It NEVER reads `delta.Node.Attrs` / `delta.Node.Sources` / `delta.Node.Payload` — the publish-boundary shallow copy SHARES those reference fields, so reading them in the async exporter goroutine is a DATA RACE (`-race` flags it).
- **Carry-forward 2 (rev-guard):** the exporter's per-node "latest rev seen" guard uses `delta.Rev` (the triggering observation's seq), NOT `delta.Node.Rev` (which is stale on cascade `node_status` deltas, by design).
- **Carry-forward 3 (no-hang finalization):** finalization must NOT hang on a coalesced/quiescent-tail `session_ended`. Eventual-final-state is opportunistic; the backstop is snapshot-then-attach. Lifecycle deltas use non-coalescable `Kind:RunID` keys so `session_ended` is never swallowed by node churn; design finalization so it never deadlocks/leaks if a lifecycle close is delayed.
- **Carry-forward 4 (full-state upsert):** `node_upsert` and `node_status` share the `Node.ID` coalesce key, so a `node_status` may be the ONLY delta delivered for a node; its `delta.Node` carries full post-apply state. `ApplyDelta` updates `nodeMap[id]` from `delta.Node` for BOTH kinds (treat `node_status` as a full-state upsert, not a status-only patch).
- Cross-platform; gRPC + OTel SDK are cross-platform; no OS-specific syscall paths. All test files white-box (same package).
- **unparam gotcha:** vary args across call sites in tests (different endpoints, seqs, node IDs, bufSizes); never add a `.golangci.yml` exclusion.
- **markdownlint gotcha:** blank lines around every list and every fenced code block (MD031/MD032). Run `npx markdownlint-cli@0.49.0 --fix <file>` before committing this plan or any `.md`.

---

## Current code: exact signatures found (authoritative anchors)

These are the present-tree (branch `feat/m1c-exporter`, off master `dfd154f`) signatures the tasks below consume or modify. Cite them; do not guess.

- `cdc/cdc.go:22` — `type GraphDelta struct { Kind GraphDeltaKind; Rev uint64; Node *model.Node; Edge *model.Edge; OldID, NewID, RunID, ExecutionID string }`; 8 kinds (`DeltaNodeUpsert`/`DeltaEdgeUpsert`/`DeltaNodeStatus`/`DeltaNodeMerge`/`DeltaEdgeDelete`/`DeltaRunStarted`/`DeltaSessionEnded`/`DeltaRunEnded`).
- `cdc/cdc.go:50` — `func (b *Bus) Subscribe(bufSize int) *Consumer`; `cdc/cdc.go:64` — `func (b *Bus) Unsubscribe(c *Consumer)`; `cdc/cdc.go:33` — `type Consumer struct { C <-chan GraphDelta; Dropped func() int64; ... }`; `cdc/cdc.go:117` — `func (b *Bus) TotalDropped() int64`.
- `model/model.go:91` — `type Node struct { ID; RunID; Type NodeType; ParentID; ...; Name; Status Status; TStart, TEnd *time.Time; DurationMS *int64; TokensIn, TokensOut *int64; CostUSD *float64; Attrs map[string]any; ...; Rev uint64 }`.
- `model/model.go:17` — node types: `NodeSession`/`NodeUserPrompt`/`NodeAssistantTurn`/`NodeToolCall`/`NodeSubagent`/`NodeMCPCall`/`NodeHookEvent`/`NodeMarker`.
- `model/model.go:39` — statuses: `StatusPending`/`StatusRunning`/`StatusOK`/`StatusError`/`StatusBlocked`/`StatusCancelled`/`StatusUnknown`/`StatusSuperseded`/`StatusAbandoned`.
- `model/model.go:116` — `type Edge struct { ID; RunID; Type EdgeType; Src, Dst string; Attrs; Rev uint64 }`; `model/model.go:32` — `EdgeParentChild EdgeType = "parent_child"`.
- `export/jsonl/export.go:1` — package `jsonl`; the existing exporter pattern (package layout, pure functions, white-box tests). The new package mirrors this layout (`export/otlp/export.go` + `export/otlp/export_test.go`).
- `daemon/daemon.go:34` — `type Daemon struct { ... bus *cdc.Bus; ... }`; `daemon/daemon.go:51` — `func New(s store.Store) *Daemon`; `daemon/daemon.go:65` — `func (d *Daemon) Subscribe(bufSize int) *cdc.Consumer`.
- `daemon/daemon.go:254` — `type Metrics struct { UptimeSeconds; OpenRuns; Shards; MaxSeq; Quarantined; Evicted; StoreWriteErrors; DeltasDropped int64; ReaperWindowSeconds int64 }`. **Task 5 adds `ExporterLag int64 \`json:"exporter_lag"\``.**
- `daemon/daemon.go:266` — `func (d *Daemon) metricsSnapshot() Metrics` (under `d.mu`). **Task 5 sets `ExporterLag` from the exporter consumer's `Dropped()`.**
- `daemon/server.go:90` — `func (d *Daemon) Serve(ctx context.Context, httpLn, grpcLn net.Listener, token string) error`. **Task 5 adds the snapshot-then-attach exporter consumer loop here; signature gains the export endpoint (via a daemon field set before `Serve`, see Task 5).**
- `reduce/graph.go:72` — `func (g *Graph) Snapshot() ([]*model.Node, []*model.Edge)` (the daemon already calls this in `applyAndPersist`; Task 5 reuses it for snapshot-then-attach).
- `cmd/catacomb/daemon.go:17` — `func newDaemonCmd() *cobra.Command`; `cmd/catacomb/daemon.go:40` — `func runDaemonWith(ctx, open func(string)(store.Store,error), listen func()(net.Listener,error), listenGRPC func()(net.Listener,error), newToken func()(string,error), dbPath, discoveryPath string, reaperWindow time.Duration, maxShards int) error`. **Task 5 adds an `otlpEndpoint string` parameter + a `--otlp-export-endpoint` flag (default `""`).**
- `daemon/discovery.go:13` — `type Discovery struct { Addr; Token; GRPCAddr string }` (the daemon's two listener addresses are `Addr` (HTTP) and `GRPCAddr` (gRPC) — the self-loop guard compares the endpoint against both).

---

## Key design decision 1: arbitrary-ID span construction via `tracetest.SpanStub`

The OTel SDK `Tracer` mints its own random IDs and cannot set an arbitrary parent span ID, so it cannot reconstruct catacomb's existing trace tree. We bypass the `Tracer` entirely and build `ReadOnlySpan` values directly:

```go
stub := tracetest.SpanStub{
    Name:        n.Name,
    SpanContext: trace.NewSpanContext(trace.SpanContextConfig{TraceID: traceID(n.RunID), SpanID: spanID(n.ID), TraceFlags: trace.FlagsSampled}),
    Parent:      parentSpanContext,
    SpanKind:    trace.SpanKindInternal,
    StartTime:   start,
    EndTime:     end,
    Attributes:  attrs,
    Status:      spanStatus(n.Status),
}
ro := stub.Snapshot()
```

`stub.Snapshot()` returns `sdktrace.ReadOnlySpan`; a batch `[]sdktrace.ReadOnlySpan` goes to `client.ExportSpans(ctx, batch)`.

**Deterministic IDs** (the crux of cross-node parent linkage):

```go
func spanID(nodeID string) trace.SpanID {
    h := sha256.Sum256([]byte(nodeID))
    var id trace.SpanID
    copy(id[:], h[:8])
    return id
}

func traceID(runID string) trace.TraceID {
    h := sha256.Sum256([]byte(runID))
    var id trace.TraceID
    copy(id[:], h[:16])
    return id
}
```

Because `spanID` is a pure function of the node ID, a child's `Parent.SpanID = spanID(parentNodeID)` always equals the parent node's own span ID `spanID(parentNodeID)` — even though the two nodes are mapped independently and at different times. The parent node ID comes from the `parent_child` edge whose `Dst` is this node (the exporter tracks `parents[edge.Dst] = edge.Src` from `edge_upsert` deltas and the snapshot's edges). A root node (no parent edge) gets a zero/empty `Parent` `SpanContext` (`trace.SpanContext{}`), which the SDK treats as a trace root.

`trace.TraceID` is `[16]byte` and `trace.SpanID` is `[8]byte`; `sha256.Sum256` returns `[32]byte`, so the slices are in range. `traceID` keyed on `RunID` groups all of a run's spans into one trace.

## Key design decision 2: the `spanExporter` seam (100% coverage with NO network)

```go
type spanExporter interface {
    ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error
    Shutdown(ctx context.Context) error
}
```

`*otlptrace.Exporter` (returned by both `otlptracehttp.New` and `otlptracegrpc.New`) already implements exactly this interface (`ExportSpans(ctx, []sdktrace.ReadOnlySpan) error` + `Shutdown(ctx) error` — verified against v1.44.0), so the real client is assignable to the seam with no adapter. `Exporter` holds one `spanExporter`. Two constructors:

- `New(ctx, endpoint, daemonGRPCAddr, daemonHTTPAddr) (*Exporter, error)` — production. Runs the self-loop guard, then constructs the real client by endpoint scheme (`otlptracegrpc.New` for `grpc://` or bare `host:port`; `otlptracehttp.New` for `http://`/`https://`). BOTH constructors are LAZY — they do NOT dial — so `New` tests exercise both scheme branches AND the self-loop error WITHOUT a network.
- `newWithExporter(exp spanExporter, ...) *Exporter` — internal (white-box test) constructor that injects a fake recording exporter. Tests use this to assert the mapper + finalization in-process. The fake (`recordExporter`) appends every `ExportSpans` batch to a slice and records `Shutdown`; an `errExporter` variant returns an error so `ApplyDelta`/`Snapshot` error-propagation lines are covered.

This gives 100% line coverage: the lazy real-client branches are covered by `New` tests (no dial), and ALL mapper/finalization logic is covered by `newWithExporter` + fake tests (no network).

## Key design decision 3: self-loop guard (§7.2)

`New` normalizes `endpoint` and both daemon listener addresses to `host:port`: strip any `grpc://`/`http://`/`https://` scheme prefix, strip a trailing path, and canonicalize loopback aliases (`localhost`, `127.0.0.1`, `::1`, `[::1]`) to a single token so they compare equal. If the normalized endpoint equals the normalized `daemonGRPCAddr` OR `daemonHTTPAddr`, `New` returns an error and does NOT construct a client:

```text
otlp exporter: endpoint "<endpoint>" is the daemon's own receiver; refusing to create a self-loop (ADR-0019)
```

This prevents the ingest → export → ingest cycle at startup. Covered by a both-match test (endpoint == gRPC addr AND a second test endpoint == HTTP addr) and a no-match test (a distinct downstream endpoint constructs successfully).

## Key design decision 4: finalization (§7.3) — robust to a delayed lifecycle close

The exporter keeps, per run, a `nodeMap[nodeID]*nodeState` where `nodeState` holds the latest scalar/token/timing snapshot (the only fields the carry-forwards permit reading), the per-node latest `delta.Rev` (the rev-guard cursor), and a `ready bool` flag. `ApplyDelta`:

- `node_upsert` / `node_status` — **rev-guard on `delta.Rev`** (carry-forward 2): if `delta.Rev <= nodeMap[id].rev`, DROP (idempotent). Else update `nodeMap[id]` from `delta.Node`'s scalar/token/timing fields (carry-forwards 1 + 4: full-state upsert for BOTH kinds, scalars only) and set `rev = delta.Rev`. If `rank(status) == 3` (genuine terminal `ok`/`error`/`blocked`), set `ready = true`. NO export yet.
- `edge_upsert` — record `parents[delta.Edge.Dst] = delta.Edge.Src` when `delta.Edge.Type == parent_child` (also rev-guard the edge so a stale re-parent does not clobber a newer one). NO export.
- `session_ended` / `run_ended` — **flush the run**: map EVERY buffered node for that run to a span (ready ones AND remaining buffered nodes regardless of status — lifecycle close forces finalization even for unfinalized nodes), `ExportSpans` the batch, then DROP the run's buffer (free memory; a node arriving after the close is the documented stale-span case below).
- `run_started` / `node_merge` / `edge_delete` — no-op (M1 has no `node_merge`/`edge_delete` producer; `run_started` needs no exporter action — the run buffer is created lazily on first node).

**Provisional-status nodes** (`cancelled`/`unknown`/`superseded`) are NOT marked `ready` and are NOT emitted on their own; they are emitted only by a lifecycle flush. This prevents a superseded intermediate node from polluting the sink with a duplicate span before the run closes.

**No-hang guarantee (carry-forward 3):** finalization is event-driven (a lifecycle delta triggers the flush); there is no blocking wait for a terminal. If a `session_ended` is coalesced/delayed, the buffer simply persists in memory until the close arrives or the run is dropped — it never deadlocks or leaks unboundedly per delta. The backstop for a quiescent stream (a `session_ended` that the exporter never sees because it attached after the run ended) is snapshot-then-attach (Task 5): the daemon pushes `g.Snapshot()` through `exporter.Snapshot(...)`, which runs the SAME map+flush path for the snapshot's terminal runs.

**Documented stale-span case (§7.3 / ADR-0015):** a genuine terminal arriving AFTER a lifecycle-close flush (its run buffer already dropped) re-creates a one-node buffer for that run and is emitted on the NEXT lifecycle close for that run (or never, if none arrives). OTLP is the one immutable eventual-consistency-exempt sink, so a late span is acceptable. We do NOT attempt to retract or dedupe.

## Key design decision 5: node→span mapper (§7.4)

OpenInference span kind (`openinference.span.kind`):

| Node type | OpenInference kind |
|---|---|
| `subagent` | `AGENT` |
| `tool_call`, `mcp_call` | `TOOL` |
| `assistant_turn` | `LLM` |
| `marker`, `session`, `user_prompt`, `hook_event` | `CHAIN` |

Token and cost attributes — set in BOTH forms for maximum downstream compatibility:

| Node field | OpenInference attr | OTel GenAI attr |
|---|---|---|
| `TokensIn` (when non-nil) | `llm.token_count.prompt` | `gen_ai.usage.input_tokens` |
| `TokensOut` (when non-nil) | `llm.token_count.completion` | `gen_ai.usage.output_tokens` |
| `CostUSD` (when non-nil) | `llm.cost.total` | — |

Always also set `gen_ai.provider.name = "anthropic"` (catacomb only reconstructs Claude Code sessions). Timing: `TStart` → `StartTime`, `TEnd` → `EndTime` (zero `time.Time{}` when the pointer is nil — unfinalized at flush). Parent linkage via the deterministic span IDs (decision 1). Attributes use `go.opentelemetry.io/otel/attribute`; IDs use `go.opentelemetry.io/otel/trace`. Span status: `StatusError`/`StatusBlocked` → `sdktrace.Status{Code: codes.Error}`; `StatusOK` → `{Code: codes.Ok}`; everything else → `{Code: codes.Unset}` (`codes` = `go.opentelemetry.io/otel/codes`).

## Key design decision 6: daemon wiring (§7.6) + `exporter_lag`

In `Serve`, if `d.otlpEndpoint != ""`: build the exporter with `otlp.New(ctx, d.otlpEndpoint, grpcLn.Addr().String(), httpLn.Addr().String())`. On error (self-loop or client build failure) log and continue WITHOUT an exporter (ADR-0002: export is enrichment; its absence is not fatal to ingest). On success: take `g.Snapshot()` for each in-memory graph under `d.mu`, push via `exporter.Snapshot(ctx, ...)` (snapshot-then-attach), then `consumer := d.bus.Subscribe(exporterBufSize)`, store the consumer ref on the daemon (under `d.mu`) so `metricsSnapshot` can read its `Dropped()`, and run:

```go
go func() {
    defer d.bus.Unsubscribe(consumer)
    for {
        select {
        case <-ctx.Done():
            return
        case delta, ok := <-consumer.C:
            if !ok {
                return
            }
            _ = exporter.ApplyDelta(ctx, delta)
        }
    }
}()
```

**`exporter_lag` (concrete definition):** the exporter consumer's `Dropped()` count — the number of deltas the bus had to coalesce because the exporter goroutine fell behind (drained slower than ingest published). It is the simplest meaningful back-pressure gauge, reuses the existing per-`Consumer` `Dropped()` plumbing from M1c-1, and is zero when the exporter keeps up. `metricsSnapshot` reads it from the stored consumer ref (0 when no exporter is configured). This is distinct from `deltas_dropped` (the bus-wide total across ALL consumers); `exporter_lag` is the exporter consumer's own share.

**Why a daemon field, not a new `Serve` param:** `Serve`'s signature (`ctx, httpLn, grpcLn, token`) is consumed by several call sites; threading the endpoint through it would ripple widely and the endpoint is configuration, not a per-call argument. We add `d.otlpEndpoint string` set by a `SetOTLPEndpoint(string)` setter (mirroring the existing `SetReaperWindow`/`SetMaxShards` pattern) called from `runDaemonWith` before `Serve`. This keeps `Serve`'s signature stable and matches the established daemon-config idiom.

---

## Task decomposition (5 tasks, each independently testable + reviewable)

- **T1** — Add the OTel-SDK client deps (`go get` one at a time, commit `go.mod`/`go.sum`, `go build ./...` green). No code.
- **T2** — `export/otlp` skeleton: `Exporter` type, `New` + self-loop guard + scheme-based real-client construction, the `spanExporter` seam, `newWithExporter`, `Name`. Tested via `New` (both schemes + both self-loop matches + no-match) and the seam.
- **T3** — node→span mapper: deterministic `spanID`/`traceID`, OpenInference + GenAI attrs, span kind/status, parent linkage. Tested via a fake recording exporter — all node kinds, token/cost present + absent, parent + no-parent root.
- **T4** — finalization + `ApplyDelta` + `Snapshot`: `nodeMap` per run, rev-guard on `delta.Rev`, rank-3 ready, lifecycle flush, `node_status` full-state upsert, provisional-held, stale-after-close. Tested via the fake exporter.
- **T5** — daemon wiring: `d.otlpEndpoint` + `SetOTLPEndpoint`, snapshot-then-attach consumer loop in `Serve`, `exporter_lag` in `Metrics`/`metricsSnapshot`, `--otlp-export-endpoint` flag + `runDaemonWith` param. Update all `runDaemonWith` call sites.

---

## Task 1: add the OTel-SDK client dependencies

**Files:**

- Modify: `go.mod`, `go.sum`

**Interfaces:** none (deps only). This mirrors M1a's deps-first task.

- [ ] **Step 1: Add each module ONE AT A TIME** (never `go mod tidy`)

```bash
go get go.opentelemetry.io/otel@v1.44.0
go get go.opentelemetry.io/otel/trace@v1.44.0
go get go.opentelemetry.io/otel/sdk@v1.44.0
go get go.opentelemetry.io/otel/exporters/otlp/otlptrace@v1.44.0
go get go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp@v1.44.0
go get go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc@v1.44.0
```

`go.opentelemetry.io/otel/attribute`, `go.opentelemetry.io/otel/codes`, and `go.opentelemetry.io/otel/sdk/trace/tracetest` are sub-packages of the `otel` and `sdk` modules above — they need NO separate `go get`. `go.opentelemetry.io/otel/metric` arrives transitively (the SDK depends on it). The pins are v1.44.0 across the board (the current latest coherent OTel release set), consistent with the already-present `go.opentelemetry.io/proto/otlp v1.10.0`, `google.golang.org/grpc v1.81.1`, `google.golang.org/protobuf v1.36.11` (which the OTLP gRPC exporter reuses — no version conflict).

- [ ] **Step 2: Verify the build stays green** (no imports yet, so this only proves the module graph resolves)

```bash
go build ./...
```

Expected: success. The new modules appear in `go.mod` (likely `// indirect` until T2 imports them — that is fine; do NOT hand-edit the `// indirect` markers, do NOT `go mod tidy`).

- [ ] **Step 3: Verify `go.sum` is complete and the existing suite still passes**

```bash
go test ./... 2>&1 | tail -5
```

Expected: PASS (unchanged behavior; deps are not yet imported).

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "build(deps): add OTel SDK client stack (otlptrace http/grpc, sdk, trace) v1.44.0"
```

---

## Task 2: `export/otlp` skeleton — `New` + self-loop guard + `spanExporter` seam + scheme selection

**Files:**

- Create: `export/otlp/export.go`
- Test: `export/otlp/export_test.go`

**Interfaces:**

- Consumes: `cdc.GraphDelta` (later tasks); `otlptracehttp`/`otlptracegrpc`/`otlptrace`/`sdktrace`/`trace`.
- Produces:
  - `type Exporter struct { ... }` holding a `spanExporter` + per-run state maps (state maps fleshed out in T3/T4; T2 establishes the struct + client + seam).
  - `type spanExporter interface { ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error; Shutdown(ctx context.Context) error }`.
  - `func New(ctx context.Context, endpoint, daemonGRPCAddr, daemonHTTPAddr string) (*Exporter, error)`.
  - `func newWithExporter(exp spanExporter) *Exporter` (internal test seam).
  - `func (e *Exporter) Name() string` (returns `"otlp"`).
  - unexported helpers: `normalizeAddr(string) string`, `newClient(ctx context.Context, endpoint string) (spanExporter, error)`.

- [ ] **Step 1: Write the failing tests** in `export/otlp/export_test.go`

```go
package otlp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type recordExporter struct {
	batches  [][]sdktrace.ReadOnlySpan
	shutdown bool
}

func (r *recordExporter) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
	r.batches = append(r.batches, spans)
	return nil
}

func (r *recordExporter) Shutdown(_ context.Context) error {
	r.shutdown = true
	return nil
}

func TestNewRejectsSelfLoopGRPCAddr(t *testing.T) {
	_, err := New(context.Background(), "grpc://127.0.0.1:4317", "localhost:4317", "127.0.0.1:8080")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "self-loop")
}

func TestNewRejectsSelfLoopHTTPAddr(t *testing.T) {
	_, err := New(context.Background(), "http://localhost:8080", "127.0.0.1:4317", "127.0.0.1:8080")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "self-loop")
}

func TestNewGRPCSchemeConstructsClient(t *testing.T) {
	e, err := New(context.Background(), "grpc://collector.example:4317", "127.0.0.1:4317", "127.0.0.1:8080")
	require.NoError(t, err)
	assert.Equal(t, "otlp", e.Name())
	assert.NotNil(t, e.client)
}

func TestNewBareHostPortUsesGRPC(t *testing.T) {
	e, err := New(context.Background(), "collector.example:4317", "127.0.0.1:4318", "127.0.0.1:8081")
	require.NoError(t, err)
	assert.NotNil(t, e.client)
}

func TestNewHTTPSchemeConstructsClient(t *testing.T) {
	e, err := New(context.Background(), "https://collector.example:443", "127.0.0.1:4319", "127.0.0.1:8082")
	require.NoError(t, err)
	assert.NotNil(t, e.client)
}

func TestNewWithExporterUsesInjectedSeam(t *testing.T) {
	rec := &recordExporter{}
	e := newWithExporter(rec)
	assert.Equal(t, "otlp", e.Name())
	assert.Same(t, spanExporter(rec), e.client)
}

func TestNormalizeAddrCanonicalisesLoopback(t *testing.T) {
	assert.Equal(t, normalizeAddr("grpc://localhost:4317"), normalizeAddr("127.0.0.1:4317"))
	assert.Equal(t, normalizeAddr("http://[::1]:8080"), normalizeAddr("localhost:8080"))
	assert.NotEqual(t, normalizeAddr("collector:4317"), normalizeAddr("127.0.0.1:4317"))
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
go test ./export/otlp/ -run 'TestNew|TestNormalizeAddr' -v
```

Expected: FAIL — package `otlp` / symbols undefined.

- [ ] **Step 3: Write `export/otlp/export.go`**

```go
package otlp

import (
	"context"
	"fmt"
	"net"
	"strings"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type spanExporter interface {
	ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error
	Shutdown(ctx context.Context) error
}

type Exporter struct {
	client  spanExporter
	runs    map[string]map[string]*nodeState
	parents map[string]string
	edgeRev map[string]uint64
}

type nodeState struct {
	node  *model.Node
	rev   uint64
	ready bool
}

func New(ctx context.Context, endpoint, daemonGRPCAddr, daemonHTTPAddr string) (*Exporter, error) {
	norm := normalizeAddr(endpoint)
	if norm == normalizeAddr(daemonGRPCAddr) || norm == normalizeAddr(daemonHTTPAddr) {
		return nil, fmt.Errorf("otlp exporter: endpoint %q is the daemon's own receiver; refusing to create a self-loop (ADR-0019)", endpoint)
	}
	client, err := newClient(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	return newWithExporter(client), nil
}

func newWithExporter(exp spanExporter) *Exporter {
	return &Exporter{
		client:  exp,
		runs:    map[string]map[string]*nodeState{},
		parents: map[string]string{},
		edgeRev: map[string]uint64{},
	}
}

func (e *Exporter) Name() string { return "otlp" }

func newClient(ctx context.Context, endpoint string) (spanExporter, error) {
	if strings.HasPrefix(endpoint, "http://") || strings.HasPrefix(endpoint, "https://") {
		return otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpoint))
	}
	host := strings.TrimPrefix(endpoint, "grpc://")
	return otlptracegrpc.New(ctx, otlptracegrpc.WithEndpoint(host), otlptracegrpc.WithInsecure())
}

func normalizeAddr(addr string) string {
	a := addr
	for _, p := range []string{"grpc://", "http://", "https://"} {
		a = strings.TrimPrefix(a, p)
	}
	if i := strings.IndexByte(a, '/'); i >= 0 {
		a = a[:i]
	}
	host, port, err := net.SplitHostPort(a)
	if err != nil {
		return a
	}
	switch host {
	case "localhost", "127.0.0.1", "::1", "[::1]", "":
		host = "loopback"
	}
	return host + ":" + port
}
```

Add the import `"github.com/realkarych/catacomb/model"` (used by `nodeState.node`). The `runs`/`parents`/`edgeRev`/`nodeState` members are introduced now (T4 populates them) — but a field that is only assigned in a later task would be uncovered in THIS task. To keep T2 at 100% with no dead code, **introduce `nodeState` and the maps in T4, not T2**: in T2 the struct is `type Exporter struct { client spanExporter }` only, and `newWithExporter` sets just `client`. T4 widens the struct + `newWithExporter`. (The block above shows the FINAL shape for reference; implement the T2-minimal shape first.) The T2 minimal form:

```go
type Exporter struct {
	client spanExporter
}

func newWithExporter(exp spanExporter) *Exporter {
	return &Exporter{client: exp}
}
```

- [ ] **Step 4: Run to verify it passes**

```bash
go test ./export/otlp/ -run 'TestNew|TestNormalizeAddr' -v
```

Expected: PASS. Both `otlptracehttp.New` and `otlptracegrpc.New` are lazy (no dial), so `TestNewGRPCScheme*`/`TestNewHTTPScheme*`/`TestNewBareHostPort` construct real clients without a network.

- [ ] **Step 5: Full gate**

```bash
make cover && make lint
```

Expected: 100% for `export/otlp` (both `newClient` scheme branches via the gRPC + bare + HTTP tests; both self-loop matches; the no-match success path; every `normalizeAddr` branch incl. the `SplitHostPort` error fallback via a scheme-only/no-port input — add a `normalizeAddr("grpc://")`-style assertion if the error arm is otherwise uncovered). lint 0. unparam: `New`'s args vary across tests (different endpoints/addrs).

- [ ] **Step 6: Commit**

```bash
git add export/otlp/export.go export/otlp/export_test.go go.mod go.sum
git commit -m "feat(export/otlp): Exporter skeleton + self-loop guard + spanExporter seam + scheme selection"
```

---

## Task 3: node→span mapper (deterministic IDs, OpenInference + GenAI attrs, parent linkage)

**Files:**

- Modify: `export/otlp/export.go` (add the mapper + ID helpers)
- Test: `export/otlp/export_test.go`

**Interfaces:**

- Consumes: `model.Node`, `model.NodeType`, `model.Status`; `attribute`, `codes`, `trace`, `tracetest`, `sdktrace`.
- Produces:
  - `func spanID(nodeID string) trace.SpanID`; `func traceID(runID string) trace.TraceID`.
  - `func (e *Exporter) nodeToSpan(n *model.Node, parentNodeID string) sdktrace.ReadOnlySpan`.
  - `func openInferenceKind(t model.NodeType) string`.
  - `func spanStatus(s model.Status) sdktrace.Status`.

- [ ] **Step 1: Write the failing tests** in `export/otlp/export_test.go`

```go
func attrMap(ro sdktrace.ReadOnlySpan) map[string]string {
	m := map[string]string{}
	for _, kv := range ro.Attributes() {
		m[string(kv.Key)] = kv.Value.Emit()
	}
	return m
}

func i64(v int64) *int64       { return &v }
func f64(v float64) *float64   { return &v }

func TestSpanIDDeterministicAndParentMatches(t *testing.T) {
	a := spanID("node-A")
	b := spanID("node-A")
	assert.Equal(t, a, b)
	assert.NotEqual(t, spanID("node-A"), spanID("node-B"))
	assert.True(t, a.IsValid())
}

func TestTraceIDGroupsByRun(t *testing.T) {
	assert.Equal(t, traceID("run-1"), traceID("run-1"))
	assert.NotEqual(t, traceID("run-1"), traceID("run-2"))
	assert.True(t, traceID("run-1").IsValid())
}

func TestNodeToSpanLLMKindAndTokensAndCost(t *testing.T) {
	e := newWithExporter(&recordExporter{})
	n := &model.Node{ID: "n1", RunID: "r1", Type: model.NodeAssistantTurn, Name: "turn", Status: model.StatusOK, TokensIn: i64(11), TokensOut: i64(22), CostUSD: f64(0.5)}
	ro := e.nodeToSpan(n, "")
	m := attrMap(ro)
	assert.Equal(t, "LLM", m["openinference.span.kind"])
	assert.Equal(t, "anthropic", m["gen_ai.provider.name"])
	assert.Equal(t, "11", m["llm.token_count.prompt"])
	assert.Equal(t, "11", m["gen_ai.usage.input_tokens"])
	assert.Equal(t, "22", m["llm.token_count.completion"])
	assert.Equal(t, "22", m["gen_ai.usage.output_tokens"])
	assert.Equal(t, "0.5", m["llm.cost.total"])
	assert.Equal(t, traceID("r1"), ro.SpanContext().TraceID())
	assert.Equal(t, spanID("n1"), ro.SpanContext().SpanID())
	assert.False(t, ro.Parent().HasSpanID())
}

func TestNodeToSpanOmitsAbsentTokensAndCost(t *testing.T) {
	e := newWithExporter(&recordExporter{})
	n := &model.Node{ID: "n2", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusRunning}
	m := attrMap(e.nodeToSpan(n, ""))
	assert.Equal(t, "TOOL", m["openinference.span.kind"])
	_, hasIn := m["llm.token_count.prompt"]
	_, hasOut := m["gen_ai.usage.output_tokens"]
	_, hasCost := m["llm.cost.total"]
	assert.False(t, hasIn)
	assert.False(t, hasOut)
	assert.False(t, hasCost)
}

func TestNodeToSpanParentLinkage(t *testing.T) {
	e := newWithExporter(&recordExporter{})
	n := &model.Node{ID: "child", RunID: "r1", Type: model.NodeMCPCall, Status: model.StatusOK}
	ro := e.nodeToSpan(n, "parent")
	assert.True(t, ro.Parent().HasSpanID())
	assert.Equal(t, spanID("parent"), ro.Parent().SpanID())
	assert.Equal(t, traceID("r1"), ro.Parent().TraceID())
}

func TestNodeToSpanTiming(t *testing.T) {
	e := newWithExporter(&recordExporter{})
	start := time.Date(2026, 6, 21, 10, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Second)
	n := &model.Node{ID: "n3", RunID: "r1", Type: model.NodeSubagent, Status: model.StatusOK, TStart: &start, TEnd: &end}
	ro := e.nodeToSpan(n, "")
	assert.Equal(t, start, ro.StartTime())
	assert.Equal(t, end, ro.EndTime())
}

func TestNodeToSpanUnfinalizedTimingIsZero(t *testing.T) {
	e := newWithExporter(&recordExporter{})
	n := &model.Node{ID: "n4", RunID: "r1", Type: model.NodeUserPrompt, Status: model.StatusRunning}
	ro := e.nodeToSpan(n, "")
	assert.True(t, ro.EndTime().IsZero())
}

func TestOpenInferenceKindTable(t *testing.T) {
	assert.Equal(t, "AGENT", openInferenceKind(model.NodeSubagent))
	assert.Equal(t, "TOOL", openInferenceKind(model.NodeToolCall))
	assert.Equal(t, "TOOL", openInferenceKind(model.NodeMCPCall))
	assert.Equal(t, "LLM", openInferenceKind(model.NodeAssistantTurn))
	assert.Equal(t, "CHAIN", openInferenceKind(model.NodeMarker))
	assert.Equal(t, "CHAIN", openInferenceKind(model.NodeSession))
	assert.Equal(t, "CHAIN", openInferenceKind(model.NodeUserPrompt))
	assert.Equal(t, "CHAIN", openInferenceKind(model.NodeHookEvent))
}

func TestSpanStatusMapping(t *testing.T) {
	assert.Equal(t, codes.Error, spanStatus(model.StatusError).Code)
	assert.Equal(t, codes.Error, spanStatus(model.StatusBlocked).Code)
	assert.Equal(t, codes.Ok, spanStatus(model.StatusOK).Code)
	assert.Equal(t, codes.Unset, spanStatus(model.StatusRunning).Code)
}
```

Add to the test import block: `"time"`, `"go.opentelemetry.io/otel/codes"`, `"github.com/realkarych/catacomb/model"`.

- [ ] **Step 2: Run to verify it fails**

```bash
go test ./export/otlp/ -run 'TestSpanID|TestTraceID|TestNodeToSpan|TestOpenInferenceKind|TestSpanStatus' -v
```

Expected: FAIL — `spanID`/`traceID`/`nodeToSpan`/`openInferenceKind`/`spanStatus` undefined.

- [ ] **Step 3: Add the mapper + helpers** to `export/otlp/export.go`

Add imports: `"crypto/sha256"`, `"strconv"`, `"go.opentelemetry.io/otel/attribute"`, `"go.opentelemetry.io/otel/codes"`, `"go.opentelemetry.io/otel/trace"`, `"go.opentelemetry.io/otel/sdk/trace/tracetest"`.

```go
func spanID(nodeID string) trace.SpanID {
	h := sha256.Sum256([]byte(nodeID))
	var id trace.SpanID
	copy(id[:], h[:8])
	return id
}

func traceID(runID string) trace.TraceID {
	h := sha256.Sum256([]byte(runID))
	var id trace.TraceID
	copy(id[:], h[:16])
	return id
}

func openInferenceKind(t model.NodeType) string {
	switch t {
	case model.NodeSubagent:
		return "AGENT"
	case model.NodeToolCall, model.NodeMCPCall:
		return "TOOL"
	case model.NodeAssistantTurn:
		return "LLM"
	default:
		return "CHAIN"
	}
}

func spanStatus(s model.Status) sdktrace.Status {
	switch s {
	case model.StatusError, model.StatusBlocked:
		return sdktrace.Status{Code: codes.Error}
	case model.StatusOK:
		return sdktrace.Status{Code: codes.Ok}
	default:
		return sdktrace.Status{Code: codes.Unset}
	}
}

func (e *Exporter) nodeToSpan(n *model.Node, parentNodeID string) sdktrace.ReadOnlySpan {
	attrs := []attribute.KeyValue{
		attribute.String("openinference.span.kind", openInferenceKind(n.Type)),
		attribute.String("gen_ai.provider.name", "anthropic"),
	}
	if n.TokensIn != nil {
		attrs = append(attrs,
			attribute.Int64("llm.token_count.prompt", *n.TokensIn),
			attribute.Int64("gen_ai.usage.input_tokens", *n.TokensIn),
		)
	}
	if n.TokensOut != nil {
		attrs = append(attrs,
			attribute.Int64("llm.token_count.completion", *n.TokensOut),
			attribute.Int64("gen_ai.usage.output_tokens", *n.TokensOut),
		)
	}
	if n.CostUSD != nil {
		attrs = append(attrs, attribute.String("llm.cost.total", strconv.FormatFloat(*n.CostUSD, 'g', -1, 64)))
	}
	var parent trace.SpanContext
	if parentNodeID != "" {
		parent = trace.NewSpanContext(trace.SpanContextConfig{TraceID: traceID(n.RunID), SpanID: spanID(parentNodeID), TraceFlags: trace.FlagsSampled})
	}
	var start, end time.Time
	if n.TStart != nil {
		start = *n.TStart
	}
	if n.TEnd != nil {
		end = *n.TEnd
	}
	stub := tracetest.SpanStub{
		Name:        n.Name,
		SpanContext: trace.NewSpanContext(trace.SpanContextConfig{TraceID: traceID(n.RunID), SpanID: spanID(n.ID), TraceFlags: trace.FlagsSampled}),
		Parent:      parent,
		SpanKind:    trace.SpanKindInternal,
		StartTime:   start,
		EndTime:     end,
		Attributes:  attrs,
		Status:      spanStatus(n.Status),
	}
	return stub.Snapshot()
}
```

Add `"time"` to the production import block.

- [ ] **Step 4: Run to verify it passes**

```bash
go test ./export/otlp/ -run 'TestSpanID|TestTraceID|TestNodeToSpan|TestOpenInferenceKind|TestSpanStatus' -v
```

Expected: PASS.

- [ ] **Step 5: Full gate**

```bash
make cover && make lint
```

Expected: 100% for `export/otlp`. Coverage musts: every `openInferenceKind` arm (all 8 node types via `TestOpenInferenceKindTable`), every `spanStatus` arm, the token/cost present (`TestNodeToSpanLLM...`) AND absent (`TestNodeToSpanOmits...`) branches, parent (`TestNodeToSpanParentLinkage`) AND no-parent (`TestNodeToSpanLLM...` asserts `!HasSpanID`) branches, `TStart`/`TEnd` set (`TestNodeToSpanTiming`) AND nil (`TestNodeToSpanUnfinalizedTimingIsZero`). lint 0.

- [ ] **Step 6: Commit**

```bash
git add export/otlp/export.go export/otlp/export_test.go
git commit -m "feat(export/otlp): node->span mapper (deterministic IDs, OpenInference+GenAI attrs, parent linkage)"
```

---

## Task 4: finalization + `ApplyDelta` + `Snapshot`

**Files:**

- Modify: `export/otlp/export.go` (widen `Exporter` struct; add `ApplyDelta`, `Snapshot`, flush, rank)
- Test: `export/otlp/export_test.go`

**Interfaces:**

- Consumes: `cdc.GraphDelta`, `cdc.Delta*` kinds; `model.Node`/`model.Edge`.
- Produces:
  - widened `type Exporter struct { client spanExporter; runs map[string]map[string]*nodeState; parents map[string]string; edgeRev map[string]uint64 }` + `type nodeState struct { node *model.Node; rev uint64; ready bool }`.
  - `func (e *Exporter) ApplyDelta(ctx context.Context, delta cdc.GraphDelta) error`.
  - `func (e *Exporter) Snapshot(ctx context.Context, filter RunFilter) error`; `type RunFilter struct { RunID string }` (empty `RunID` = all runs).
  - `func (e *Exporter) flushRun(ctx context.Context, runID string) error`.
  - `func rank(s model.Status) int` (3 for `ok`/`error`/`blocked`; 0 otherwise — only the rank-3 boundary matters here).
  - `func (e *Exporter) upsertNode(d cdc.GraphDelta)` (the rev-guarded full-state map update shared by `node_upsert`/`node_status`).

- [ ] **Step 1: Write the failing tests** in `export/otlp/export_test.go`

```go
func TestApplyDeltaBuffersUntilLifecycleFlush(t *testing.T) {
	rec := &recordExporter{}
	e := newWithExporter(rec)
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaNodeUpsert, Rev: 1, RunID: "r1", Node: &model.Node{ID: "n1", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusOK}}))
	assert.Empty(t, rec.batches)
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaSessionEnded, Rev: 2, RunID: "r1"}))
	require.Len(t, rec.batches, 1)
	require.Len(t, rec.batches[0], 1)
	assert.Equal(t, spanID("n1"), rec.batches[0][0].SpanContext().SpanID())
}

func TestApplyDeltaRevGuardDropsStale(t *testing.T) {
	rec := &recordExporter{}
	e := newWithExporter(rec)
	apply := func(rev uint64, st model.Status) {
		require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaNodeStatus, Rev: rev, RunID: "r1", Node: &model.Node{ID: "n1", RunID: "r1", Type: model.NodeToolCall, Status: st}}))
	}
	apply(5, model.StatusError)
	apply(3, model.StatusRunning)
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaRunEnded, Rev: 9, RunID: "r1"}))
	require.Len(t, rec.batches, 1)
	require.Len(t, rec.batches[0], 1)
	assert.Equal(t, codes.Error, rec.batches[0][0].Status().Code)
}

func TestApplyDeltaNodeStatusIsFullStateUpsert(t *testing.T) {
	rec := &recordExporter{}
	e := newWithExporter(rec)
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaNodeStatus, Rev: 1, RunID: "r1", Node: &model.Node{ID: "only", RunID: "r1", Type: model.NodeAssistantTurn, Name: "t", Status: model.StatusOK, TokensIn: i64(7)}}))
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaSessionEnded, Rev: 2, RunID: "r1"}))
	require.Len(t, rec.batches, 1)
	m := attrMap(rec.batches[0][0])
	assert.Equal(t, "7", m["gen_ai.usage.input_tokens"])
}

func TestApplyDeltaProvisionalHeldThenFlushedOnClose(t *testing.T) {
	rec := &recordExporter{}
	e := newWithExporter(rec)
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaNodeStatus, Rev: 4, RunID: "r1", Node: &model.Node{ID: "c1", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusCancelled}}))
	assert.Empty(t, rec.batches)
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaRunEnded, Rev: 6, RunID: "r1"}))
	require.Len(t, rec.batches, 1)
	require.Len(t, rec.batches[0], 1)
}

func TestApplyDeltaEdgeUpsertLinksParent(t *testing.T) {
	rec := &recordExporter{}
	e := newWithExporter(rec)
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaNodeUpsert, Rev: 1, RunID: "r1", Node: &model.Node{ID: "p", RunID: "r1", Type: model.NodeAssistantTurn, Status: model.StatusOK}}))
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaNodeUpsert, Rev: 2, RunID: "r1", Node: &model.Node{ID: "ch", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusOK}}))
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaEdgeUpsert, Rev: 2, RunID: "r1", Edge: &model.Edge{ID: "e1", RunID: "r1", Type: model.EdgeParentChild, Src: "p", Dst: "ch", Rev: 2}}))
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaSessionEnded, Rev: 3, RunID: "r1"}))
	require.Len(t, rec.batches, 1)
	byID := map[trace.SpanID]sdktrace.ReadOnlySpan{}
	for _, ro := range rec.batches[0] {
		byID[ro.SpanContext().SpanID()] = ro
	}
	child := byID[spanID("ch")]
	require.NotNil(t, child)
	assert.Equal(t, spanID("p"), child.Parent().SpanID())
}

func TestApplyDeltaEdgeRevGuardKeepsNewerParent(t *testing.T) {
	rec := &recordExporter{}
	e := newWithExporter(rec)
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaEdgeUpsert, Rev: 9, RunID: "r1", Edge: &model.Edge{ID: "e1", RunID: "r1", Type: model.EdgeParentChild, Src: "newp", Dst: "ch", Rev: 9}}))
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaEdgeUpsert, Rev: 4, RunID: "r1", Edge: &model.Edge{ID: "e1", RunID: "r1", Type: model.EdgeParentChild, Src: "oldp", Dst: "ch", Rev: 4}}))
	assert.Equal(t, "newp", e.parents["ch"])
}

func TestApplyDeltaIgnoresNonParentEdge(t *testing.T) {
	rec := &recordExporter{}
	e := newWithExporter(rec)
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaEdgeUpsert, Rev: 1, RunID: "r1", Edge: &model.Edge{ID: "e2", RunID: "r1", Type: model.EdgeSequence, Src: "a", Dst: "b", Rev: 1}}))
	_, ok := e.parents["b"]
	assert.False(t, ok)
}

func TestApplyDeltaLifecycleNoopsAndUnknownKind(t *testing.T) {
	rec := &recordExporter{}
	e := newWithExporter(rec)
	for _, k := range []cdc.GraphDeltaKind{cdc.DeltaRunStarted, cdc.DeltaNodeMerge, cdc.DeltaEdgeDelete} {
		require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: k, Rev: 1, RunID: "r1"}))
	}
	assert.Empty(t, rec.batches)
}

func TestFlushUnknownRunIsNoop(t *testing.T) {
	rec := &recordExporter{}
	e := newWithExporter(rec)
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaSessionEnded, Rev: 1, RunID: "ghost"}))
	assert.Empty(t, rec.batches)
}

func TestFlushPropagatesExportError(t *testing.T) {
	e := newWithExporter(&errExporter{})
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaNodeUpsert, Rev: 1, RunID: "r1", Node: &model.Node{ID: "n1", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusOK}}))
	err := e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaSessionEnded, Rev: 2, RunID: "r1"})
	require.Error(t, err)
}

func TestStaleSpanAfterCloseReEmittedOnNextClose(t *testing.T) {
	rec := &recordExporter{}
	e := newWithExporter(rec)
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaNodeUpsert, Rev: 1, RunID: "r1", Node: &model.Node{ID: "n1", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusOK}}))
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaSessionEnded, Rev: 2, RunID: "r1"}))
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaNodeStatus, Rev: 3, RunID: "r1", Node: &model.Node{ID: "late", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusError}}))
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaRunEnded, Rev: 4, RunID: "r1"}))
	require.Len(t, rec.batches, 2)
	assert.Equal(t, spanID("late"), rec.batches[1][0].SpanContext().SpanID())
}

func TestSnapshotMapsAndFlushesTerminalRuns(t *testing.T) {
	rec := &recordExporter{}
	e := newWithExporter(rec)
	nodes := []*model.Node{
		{ID: "p", RunID: "r1", Type: model.NodeSession, Status: model.StatusOK},
		{ID: "ch", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusOK},
	}
	edges := []*model.Edge{{ID: "e1", RunID: "r1", Type: model.EdgeParentChild, Src: "p", Dst: "ch", Rev: 1}}
	require.NoError(t, e.SnapshotState(context.Background(), nodes, edges))
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaRunEnded, Rev: 9, RunID: "r1"}))
	require.Len(t, rec.batches, 1)
	require.Len(t, rec.batches[0], 2)
}

func TestSnapshotFilterByRunID(t *testing.T) {
	rec := &recordExporter{}
	e := newWithExporter(rec)
	nodes := []*model.Node{
		{ID: "a", RunID: "r1", Type: model.NodeToolCall, Status: model.StatusOK},
		{ID: "b", RunID: "r2", Type: model.NodeToolCall, Status: model.StatusOK},
	}
	require.NoError(t, e.Snapshot(context.Background(), RunFilter{RunID: "r1"}))
	_ = nodes
	require.NoError(t, e.SnapshotState(context.Background(), nodes, edgesNil()))
	require.NoError(t, e.ApplyDelta(context.Background(), cdc.GraphDelta{Kind: cdc.DeltaRunEnded, Rev: 5, RunID: "r2"}))
	require.Len(t, rec.batches, 1)
	require.Len(t, rec.batches[0], 1)
	assert.Equal(t, spanID("b"), rec.batches[0][0].SpanContext().SpanID())
}

func edgesNil() []*model.Edge { return nil }

type errExporter struct{}

func (errExporter) ExportSpans(context.Context, []sdktrace.ReadOnlySpan) error {
	return assert.AnError
}
func (errExporter) Shutdown(context.Context) error { return nil }
```

Add `"github.com/realkarych/catacomb/cdc"` to the test import block.

**Note on `Snapshot` vs `SnapshotState`.** The spec §7.1 signature is `Snapshot(ctx, filter RunFilter)`. But the daemon has the materialized `[]*model.Node`/`[]*model.Edge` from `g.Snapshot()`, not a graph handle the exporter can query. To honor the spec signature AND give the daemon a usable entry point, define BOTH: `Snapshot(ctx, filter RunFilter) error` is the spec-shaped entry (it ingests no nodes itself — it only records `e.filter` for a subsequent `SnapshotState`, OR is a thin convenience that returns nil for an empty exporter), and `SnapshotState(ctx, nodes, edges) error` is the concrete loader the daemon calls. **Resolve this cleanly: make `Snapshot(ctx, filter)` store the filter on the exporter and make `SnapshotState` apply it.** This keeps the spec's exported name while giving the daemon a real loader. (If a reviewer prefers a single method, fold them — but then the daemon must pass nodes/edges, which the spec signature cannot carry; the two-method split is the honest resolution and is documented here as the spec/impl reconciliation.)

- [ ] **Step 2: Run to verify it fails**

```bash
go test ./export/otlp/ -run 'TestApplyDelta|TestFlush|TestStale|TestSnapshot' -v
```

Expected: FAIL — `ApplyDelta`/`Snapshot`/`SnapshotState`/`RunFilter`/`rank`/`e.parents` undefined.

- [ ] **Step 3: Widen the struct + add the state members** in `export/otlp/export.go`

```go
type Exporter struct {
	client  spanExporter
	runs    map[string]map[string]*nodeState
	parents map[string]string
	edgeRev map[string]uint64
	filter  RunFilter
}

type nodeState struct {
	node  *model.Node
	rev   uint64
	ready bool
}

type RunFilter struct {
	RunID string
}

func newWithExporter(exp spanExporter) *Exporter {
	return &Exporter{
		client:  exp,
		runs:    map[string]map[string]*nodeState{},
		parents: map[string]string{},
		edgeRev: map[string]uint64{},
	}
}
```

Update `New` to call this widened `newWithExporter` (no change to its body — it already calls `newWithExporter(client)`).

- [ ] **Step 4: Add `ApplyDelta`, `upsertNode`, `flushRun`, `rank`** in `export/otlp/export.go`

```go
func rank(s model.Status) int {
	switch s {
	case model.StatusOK, model.StatusError, model.StatusBlocked:
		return 3
	default:
		return 0
	}
}

func (e *Exporter) ApplyDelta(ctx context.Context, d cdc.GraphDelta) error {
	switch d.Kind {
	case cdc.DeltaNodeUpsert, cdc.DeltaNodeStatus:
		e.upsertNode(d)
		return nil
	case cdc.DeltaEdgeUpsert:
		if d.Edge != nil && d.Edge.Type == model.EdgeParentChild && d.Rev >= e.edgeRev[d.Edge.Dst] {
			e.edgeRev[d.Edge.Dst] = d.Rev
			e.parents[d.Edge.Dst] = d.Edge.Src
		}
		return nil
	case cdc.DeltaSessionEnded, cdc.DeltaRunEnded:
		return e.flushRun(ctx, d.RunID)
	default:
		return nil
	}
}

func (e *Exporter) upsertNode(d cdc.GraphDelta) {
	if d.Node == nil {
		return
	}
	run, ok := e.runs[d.RunID]
	if !ok {
		run = map[string]*nodeState{}
		e.runs[d.RunID] = run
	}
	st, ok := run[d.Node.ID]
	if ok && d.Rev <= st.rev {
		return
	}
	if !ok {
		st = &nodeState{}
		run[d.Node.ID] = st
	}
	st.node = d.Node
	st.rev = d.Rev
	if rank(d.Node.Status) == 3 {
		st.ready = true
	}
}

func (e *Exporter) flushRun(ctx context.Context, runID string) error {
	run, ok := e.runs[runID]
	if !ok || len(run) == 0 {
		return nil
	}
	spans := make([]sdktrace.ReadOnlySpan, 0, len(run))
	for id, st := range run {
		spans = append(spans, e.nodeToSpan(st.node, e.parents[id]))
	}
	delete(e.runs, runID)
	return e.client.ExportSpans(ctx, spans)
}
```

The `nodeState.ready` flag is set but the lifecycle flush emits ALL buffered nodes (ready and not), so `ready` is currently only ever READ if a future per-node early-emit path is added. To avoid a write-only field (lint `unused`/dead state), **drop `ready` from `nodeState` in this implementation** — finalization is lifecycle-driven only (provisional-held until close), so a per-node `ready` flag has no reader in M1c-2. Keep `nodeState` as `{ node *model.Node; rev uint64 }`. (The rank-3 check still runs inside `upsertNode` only if it has an effect; since there is no reader, REMOVE the `if rank(...) == 3 { st.ready = true }` block too, and instead keep `rank` used by `spanStatus`? No — `rank` then has no caller. Resolve cleanly below.)

**Resolution (no dead code):** finalization in M1c-2 is purely lifecycle-driven — provisional and terminal nodes alike are buffered and flushed on `session_ended`/`run_ended`. There is therefore NO need for a per-node `ready` flag or a `rank` helper in the exporter. So:

- `nodeState` is `{ node *model.Node; rev uint64 }` (no `ready`).
- Do NOT add a `rank` function. Drop the `if rank(...) == 3` block from `upsertNode`.
- Remove `TestApplyDeltaProvisionalHeldThenFlushedOnClose`'s reliance on rank (it already only asserts buffer-until-close, which holds without `ready`).
- Span terminal-vs-provisional distinction is carried by `spanStatus` (the span's `Status.Code`), NOT by a buffering decision.

This is the honest minimal design: §7.3's "mark ready on rank-3" is an OPTIMIZATION for early emit that M1c-2 does not implement (it buffers everything until the lifecycle close, which is simpler and still correct — the §7.3 "ready_to_export" path would only matter if we emitted before the close, which we deliberately do not, to avoid superseded-duplicate spans). Document this deviation in the Self-Review.

- [ ] **Step 5: Add `Snapshot` + `SnapshotState`** in `export/otlp/export.go`

```go
func (e *Exporter) Snapshot(_ context.Context, filter RunFilter) error {
	e.filter = filter
	return nil
}

func (e *Exporter) SnapshotState(_ context.Context, nodes []*model.Node, edges []*model.Edge) error {
	for _, edge := range edges {
		if edge.Type != model.EdgeParentChild {
			continue
		}
		if edge.Rev >= e.edgeRev[edge.Dst] {
			e.edgeRev[edge.Dst] = edge.Rev
			e.parents[edge.Dst] = edge.Src
		}
	}
	for _, n := range nodes {
		if e.filter.RunID != "" && n.RunID != e.filter.RunID {
			continue
		}
		run, ok := e.runs[n.RunID]
		if !ok {
			run = map[string]*nodeState{}
			e.runs[n.RunID] = run
		}
		st, ok := run[n.ID]
		if ok && n.Rev <= st.rev {
			continue
		}
		if !ok {
			st = &nodeState{}
			run[n.ID] = st
		}
		st.node = n
		st.rev = n.Rev
	}
	return nil
}
```

`SnapshotState` uses `n.Rev` (the node's own rev) as the seed cursor — this is the ONE place reading `Node.Rev` is correct, because the snapshot is taken under `d.mu` (not async) and represents the authoritative post-apply state; the rev-guard cursor is seeded so subsequent live `node_status` deltas (which carry the triggering `delta.Rev`) win correctly. Document this in the Self-Review (it does not contradict carry-forward 2, which governs the ASYNC `ApplyDelta` path).

- [ ] **Step 6: Run to verify it passes**

```bash
go test ./export/otlp/ -run 'TestApplyDelta|TestFlush|TestStale|TestSnapshot' -v
```

Expected: PASS. Adjust the tests that referenced `ready`/`rank` per Step 4's resolution (none assert `ready` directly; `TestApplyDeltaProvisionalHeldThenFlushedOnClose` asserts buffer-until-close, which holds).

- [ ] **Step 7: Run the WHOLE `export/otlp` suite under race**

```bash
go test ./export/otlp/ -race -count=2 -v
```

Expected: PASS, stable.

- [ ] **Step 8: Full gate**

```bash
make cover && make lint
```

Expected: 100% for `export/otlp`. Coverage musts: both `node_upsert`/`node_status` arms (full-state upsert), the rev-guard drop (`TestApplyDeltaRevGuardDropsStale`), `edge_upsert` parent-record + non-parent skip + edge rev-guard, both lifecycle arms flushing, the unknown-run flush no-op, the unknown-kind/`run_started`/`node_merge`/`edge_delete` default no-op, the export-error propagation (`errExporter`), the stale-after-close re-emit, `Snapshot` filter set + `SnapshotState` map/flush + filter-by-run. lint 0. unparam: vary revs/run IDs/node IDs across tests.

- [ ] **Step 9: Commit**

```bash
git add export/otlp/export.go export/otlp/export_test.go
git commit -m "feat(export/otlp): finalization + ApplyDelta (rev-guard, full-state upsert, lifecycle flush) + Snapshot"
```

---

## Task 5: daemon wiring — snapshot-then-attach consumer loop + `exporter_lag` + `--otlp-export-endpoint`

**Files:**

- Modify: `daemon/daemon.go` (`d.otlpEndpoint`; `SetOTLPEndpoint`; `d.exporterConsumer`; `Metrics.ExporterLag`; `metricsSnapshot`)
- Modify: `daemon/server.go` (`Serve` builds the exporter + snapshot-then-attach + consumer goroutine)
- Modify: `cmd/catacomb/daemon.go` (`--otlp-export-endpoint` flag + `runDaemonWith` param + call site)
- Test: `daemon/server_test.go` / `daemon/daemon_test.go` (exporter wiring, lag metric); `cmd/catacomb/daemon_test.go` (flag + call-site)

**Interfaces:**

- Consumes: `export/otlp.New`/`*otlp.Exporter`/`ApplyDelta`/`SnapshotState`/`Snapshot`; `cdc.Consumer`.
- Produces:
  - `Daemon.otlpEndpoint string`; `func (d *Daemon) SetOTLPEndpoint(s string)`.
  - `Daemon.exporterConsumer *cdc.Consumer` (stored under `d.mu` so `metricsSnapshot` reads its `Dropped()`).
  - `Serve`: when `d.otlpEndpoint != ""`, build the exporter (guarded against `grpcLn.Addr()` and `httpLn.Addr()`), `SnapshotState` the current graphs, `Subscribe`, run the consumer goroutine.
  - `Metrics.ExporterLag int64 \`json:"exporter_lag"\``;`metricsSnapshot` sets it from `d.exporterConsumer.Dropped()` (0 when nil).
  - `runDaemonWith(... , otlpEndpoint string)`; `newDaemonCmd` `--otlp-export-endpoint` flag.

- [ ] **Step 1: Write the failing tests**

In `daemon/daemon_test.go` (metric + setter):

```go
func TestExporterLagZeroWhenNoExporter(t *testing.T) {
	d := New(tempStore(t))
	m := d.metricsSnapshot()
	assert.Equal(t, int64(0), m.ExporterLag)
}

func TestExporterLagReflectsConsumerDropped(t *testing.T) {
	d := New(tempStore(t))
	c := d.Subscribe(0)
	d.mu.Lock()
	d.exporterConsumer = c
	d.mu.Unlock()
	d.bus.Publish(cdc.GraphDelta{Kind: cdc.DeltaNodeUpsert, Rev: 1, Node: &model.Node{ID: "x"}})
	d.bus.Publish(cdc.GraphDelta{Kind: cdc.DeltaNodeUpsert, Rev: 2, Node: &model.Node{ID: "y"}})
	m := d.metricsSnapshot()
	assert.Positive(t, m.ExporterLag)
}

func TestSetOTLPEndpoint(t *testing.T) {
	d := New(tempStore(t))
	d.SetOTLPEndpoint("grpc://collector:4317")
	d.mu.Lock()
	got := d.otlpEndpoint
	d.mu.Unlock()
	assert.Equal(t, "grpc://collector:4317", got)
}
```

In `daemon/server_test.go` (Serve builds exporter + delivers spans to a mock sink). Use a real downstream OTLP/gRPC sink (a minimal in-process `collectorv1.TraceServiceServer` on a SECOND loopback listener, distinct from the daemon's own listeners so the self-loop guard passes) OR, simpler and network-free, assert via the consumer/metric path that the exporter attached. Concretely, a deterministic test that does not depend on the SDK dialing:

```go
func TestServeStartsExporterConsumer(t *testing.T) {
	sink := newMockOTLPSink(t)
	defer sink.stop()
	d := New(tempStore(t))
	fixedExecID(d)
	d.SetOTLPEndpoint("grpc://" + sink.addr)
	httpLn, grpcLn := loopback(t), loopback(t)
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- d.Serve(ctx, httpLn, grpcLn, "tok") }()
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_input":{}}`)))
	require.NoError(t, d.Ingest("SessionEnd", []byte(`{"session_id":"s1","reason":"clear"}`)))
	require.Eventually(t, func() bool { return sink.spanCount() > 0 }, 3*time.Second, 5*time.Millisecond)
	cancel()
	require.NoError(t, <-errc)
}

func TestServeSelfLoopEndpointSkipsExporter(t *testing.T) {
	d := New(tempStore(t))
	httpLn, grpcLn := loopback(t), loopback(t)
	d.SetOTLPEndpoint("grpc://" + grpcLn.Addr().String())
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- d.Serve(ctx, httpLn, grpcLn, "tok2") }()
	require.Eventually(t, func() bool {
		d.mu.Lock()
		defer d.mu.Unlock()
		return d.exporterConsumer == nil
	}, time.Second, 5*time.Millisecond)
	cancel()
	require.NoError(t, <-errc)
}
```

`newMockOTLPSink` is a minimal helper (a `grpc.Server` registering a `collectorv1.TraceServiceServer` whose `Export` counts spans under a mutex) on its own loopback listener — reuse the M1a-grpc receiver test patterns (grep `daemon/*_test.go` for an existing `TraceServiceServer` test double; if one exists, reuse it). `loopback(t)` wraps `daemon.ListenLoopback`. This is the ONE place a real SDK dial happens — it is a genuine end-to-end assertion (span lands in a sink), uses `require.Eventually` (no sleep), and exercises the gRPC scheme branch end-to-end.

**Critical: use `SessionEnd`, NOT `Stop`, to trigger the flush.** Per the M0.4 Stop-fix (`ingest/hook/hook.go:76`), `Stop` maps to kind `"stop"` (a turn boundary), which is NOT a session terminal and emits NO `session_ended` delta. Only `SessionEnd` (kind `session_end`, `hook.go:74`) reaches `applyRunEnded`/the `session_end` case and emits `DeltaSessionEnded`, which is what `flushRun` keys on. A test that ingests `Stop` would buffer the tool span forever and the sink would stay empty. The same applies to any future end-to-end exporter test.

In `cmd/catacomb/daemon_test.go` (flag + call-site), add an assertion that `runDaemonWith` accepts the new `otlpEndpoint` arg and that the flag is registered; vary the endpoint across call sites for unparam. Update EVERY existing `runDaemonWith(...)` caller to pass an `otlpEndpoint` (use `""` for the existing ones, a non-empty value for at least one new test so the param genuinely varies).

- [ ] **Step 2: Run to verify it fails**

```bash
go test ./daemon/ ./cmd/catacomb/ -run 'TestExporterLag|TestSetOTLP|TestServeStartsExporter|TestServeSelfLoop|TestDaemonOTLPFlag' -v
```

Expected: FAIL — `ExporterLag`/`SetOTLPEndpoint`/`exporterConsumer`/the flag/the param undefined.

- [ ] **Step 3: Add `otlpEndpoint` + `exporterConsumer` + `SetOTLPEndpoint`** in `daemon/daemon.go`

Add the two fields to `Daemon` and the setter (mirroring `SetReaperWindow`):

```go
func (d *Daemon) SetOTLPEndpoint(s string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.otlpEndpoint = s
}
```

- [ ] **Step 4: Add `ExporterLag` to `Metrics` + `metricsSnapshot`** in `daemon/daemon.go`

```go
type Metrics struct {
	UptimeSeconds       int64  `json:"uptime_seconds"`
	OpenRuns            int    `json:"open_runs"`
	Shards              int    `json:"shards"`
	MaxSeq              uint64 `json:"max_seq"`
	Quarantined         int64  `json:"quarantined"`
	Evicted             int64  `json:"evicted"`
	StoreWriteErrors    int64  `json:"store_write_errors"`
	DeltasDropped       int64  `json:"deltas_dropped"`
	ExporterLag         int64  `json:"exporter_lag"`
	ReaperWindowSeconds int64  `json:"reaper_window_seconds"`
}
```

In `metricsSnapshot`, compute the lag under `d.mu`:

```go
	var lag int64
	if d.exporterConsumer != nil {
		lag = d.exporterConsumer.Dropped()
	}
	return Metrics{
		...
		DeltasDropped:       d.bus.TotalDropped(),
		ExporterLag:         lag,
		ReaperWindowSeconds: int64(d.reaperWindow.Seconds()),
	}
```

`Consumer.Dropped()` takes the bus's own mutex (leaf-only), so calling it under `d.mu` cannot deadlock (same lock-order argument as `TotalDropped` in M1c-1).

- [ ] **Step 5: Wire the exporter in `Serve`** (`daemon/server.go`)

Add the import `"github.com/realkarych/catacomb/export/otlp"`. At the top of `Serve` (after `ctx, cancel := context.WithCancel(ctx)`), start the exporter if configured:

```go
func (d *Daemon) Serve(ctx context.Context, httpLn, grpcLn net.Listener, token string) error {
	srv := &http.Server{Handler: d.Handler(token)}
	grpcSrv := d.newGRPCServer(token)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	d.startExporter(ctx, httpLn.Addr().String(), grpcLn.Addr().String())
	go d.reapLoop(ctx)
	...
}

func (d *Daemon) startExporter(ctx context.Context, httpAddr, grpcAddr string) {
	d.mu.Lock()
	endpoint := d.otlpEndpoint
	d.mu.Unlock()
	if endpoint == "" {
		return
	}
	exp, err := otlp.New(ctx, endpoint, grpcAddr, httpAddr)
	if err != nil {
		log.Printf("catacomb: otlp exporter disabled: %v", err)
		return
	}
	d.mu.Lock()
	for _, g := range d.graphs {
		nodes, edges := g.Snapshot()
		_ = exp.SnapshotState(ctx, nodes, edges)
	}
	consumer := d.bus.Subscribe(exporterBufSize)
	d.exporterConsumer = consumer
	d.mu.Unlock()
	go func() {
		defer d.bus.Unsubscribe(consumer)
		for {
			select {
			case <-ctx.Done():
				return
			case delta, ok := <-consumer.C:
				if !ok {
					return
				}
				_ = exp.ApplyDelta(ctx, delta)
			}
		}
	}()
}
```

Add the const `exporterBufSize = 1024` near the other daemon consts. The snapshot loop runs under `d.mu` (graphs are not mutated concurrently while we hold it); `SnapshotState` is in-process (no dial). `Subscribe` happens under `d.mu` AFTER the snapshot so no live delta is missed between snapshot and attach (any delta published after `Subscribe` is queued on the consumer channel; deltas during the snapshot are folded into the snapshot state, and the rev-guard makes a re-delivered node idempotent). The goroutine exits on ctx cancel (and on channel close via `Unsubscribe`).

- [ ] **Step 6: Add the flag + `runDaemonWith` param + call site** (`cmd/catacomb/daemon.go`)

```go
func newDaemonCmd() *cobra.Command {
	var dbPath, discoveryPath, otlpEndpoint string
	var reaperWindow time.Duration
	var maxShards int
	cmd := &cobra.Command{
		...
		RunE: func(cmd *cobra.Command, _ []string) error {
			...
			return runDaemonWith(ctx, store.OpenSQLite, daemon.ListenLoopback, daemon.ListenLoopback, daemon.NewToken, dbPath, discoveryPath, reaperWindow, maxShards, otlpEndpoint)
		},
	}
	...
	cmd.Flags().StringVar(&otlpEndpoint, "otlp-export-endpoint", "", "downstream OTLP endpoint to export the reconstructed trace tree (empty = disabled)")
	return cmd
}

func runDaemonWith(
	ctx context.Context,
	open func(string) (store.Store, error),
	listen func() (net.Listener, error),
	listenGRPC func() (net.Listener, error),
	newToken func() (string, error),
	dbPath, discoveryPath string,
	reaperWindow time.Duration,
	maxShards int,
	otlpEndpoint string,
) error {
	...
	d := daemon.New(s)
	d.SetReaperWindow(reaperWindow)
	d.SetMaxShards(maxShards)
	d.SetOTLPEndpoint(otlpEndpoint)
	...
}
```

Update EVERY existing `runDaemonWith(...)` call site (grep `cmd/catacomb/*_test.go`) to pass a trailing `otlpEndpoint` — use `""` for existing call sites and a non-empty value for at least one test so unparam sees the param vary.

- [ ] **Step 7: Run to verify it passes**

```bash
go test ./daemon/ ./cmd/catacomb/ -run 'TestExporterLag|TestSetOTLP|TestServeStartsExporter|TestServeSelfLoop|TestDaemonOTLPFlag' -v
```

Expected: PASS.

- [ ] **Step 8: Run the WHOLE daemon + cmd suites under race**

```bash
go test ./daemon/ ./cmd/catacomb/ -race -count=2 -v
```

Expected: PASS — existing `Serve`/metrics/ingest tests still green; `exporter_lag` is additive in the JSON (zero when no exporter). The exporter goroutine is race-clean: it reads delta COPIES (the bus shallow-copies at publish) and never touches `d.mu`.

- [ ] **Step 9: Full gate (host + windows build)**

```bash
make cover && make lint && GOOS=windows go build ./...
```

Expected: 100% for `daemon` + `cmd/catacomb` (the `startExporter` configured path via `TestServeStartsExporter`, the self-loop-skip path via `TestServeSelfLoop`, the no-endpoint early-return via existing `Serve` tests that set no endpoint, the `ExporterLag` nil + non-nil branches, the flag). lint 0; windows build clean.

- [ ] **Step 10: Commit**

```bash
git add daemon/daemon.go daemon/server.go cmd/catacomb/daemon.go daemon/daemon_test.go daemon/server_test.go cmd/catacomb/daemon_test.go
git commit -m "feat(daemon): OTLP exporter snapshot-then-attach consumer loop + exporter_lag + --otlp-export-endpoint"
```

---

## Deferred (documented — NOT implemented in M1c-2)

- **Per-node early emit on rank-3 (`ready_to_export`, §7.3 optimization)** — M1c-2 buffers ALL nodes until the lifecycle close (simpler; avoids superseded-duplicate spans). The §7.3 "mark ready" optimization (emit a terminal node before its run closes) is deliberately NOT implemented; it would only matter for very long-lived runs and risks emitting a node that is later superseded. Revisit if downstream latency on long runs becomes a concern.
- **`last_applied_seq` reconnect cursor (spec §6.3)** — the exporter attaches once per `Serve`; a reconnecting-after-restart cursor with retention-window fallback is post-M1 (the daemon restart re-runs snapshot-then-attach from scratch, which is correct).
- **`node_merge` / `edge_delete` handling** — no live producer in M1 (M1c-1 key decision 4); `ApplyDelta` no-ops them.
- **`catacomb.yaml` config keys** (`exporters.otlp.endpoint`, `exporterBufSize`) — prefer the CLI flag per the ledger rule; `exporterBufSize` is a fixed const (1024) for now.
- **OTLP resource attributes / `service.name` on exported spans** — §7.4 covers per-span GenAI/OpenInference attrs; a shared `resource.Resource` (service.name = "catacomb") on the batch is a nice-to-have deferred (the SpanStub `Resource` field is left nil; downstream still ingests the spans).

## Self-Review

- **Spec §7 coverage:**
  - §7.1 contract (`New`/`Name`/`ApplyDelta`/`Snapshot`) — T2 (`New`/`Name`) + T4 (`ApplyDelta`/`Snapshot`). The `Snapshot(ctx, RunFilter)` exported name is honored; `SnapshotState(ctx, nodes, edges)` is the concrete loader the daemon calls (spec/impl reconciliation documented in T4 Step 1). ✓
  - §7.2 self-loop guard — T2 (`normalizeAddr` + both-match + no-match tests; refuses BEFORE constructing a client). ✓
  - §7.3 finalization — T4 (buffer-until-lifecycle-close, provisional-held, lifecycle flush of ALL buffered nodes, stale-after-close re-emit). The "mark ready on rank-3" optimization is deliberately NOT implemented (deferred above) — finalization is lifecycle-driven; this is the honest deviation, documented. ✓
  - §7.4 node→span mapper — T3 (OpenInference kind table all 8 node types; token/cost in BOTH OpenInference + GenAI forms, present + absent; `gen_ai.provider.name=anthropic`; timing finalized + unfinalized; parent linkage + root). ✓
  - §7.5 transport — T2 (`newClient` scheme selection: `grpc://`/bare → gRPC, `http(s)://` → HTTP; SDK client handles pooling/retry; both lazy so covered without dial). ✓
  - §7.6 daemon wiring — T5 (snapshot-then-attach, consumer goroutine under ctx with Unsubscribe on exit, `exporter_lag`, `--otlp-export-endpoint`). ✓
- **Carry-forward 1 (read only scalars):** `nodeToSpan` and `upsertNode`/`SnapshotState` read ONLY `Status`/`Type`/`Name`/`RunID`/`TStart`/`TEnd`/`TokensIn`/`TokensOut`/`CostUSD` (+`Rev` per CF2). NO read of `Node.Attrs`/`Sources`/`Payload` anywhere. Stated in Global Constraints; enforced by the mapper code. ✓
- **Carry-forward 2 (rev-guard on `delta.Rev`):** `upsertNode` guards `d.Rev <= st.rev` using the DELTA's `Rev` (triggering seq), not `delta.Node.Rev`; asserted by `TestApplyDeltaRevGuardDropsStale` (rev 3 after rev 5 is dropped; the terminal status from rev 5 survives the flush). `SnapshotState` seeds the cursor from `n.Rev` — the ONE correct `Node.Rev` read, justified because the snapshot is synchronous under `d.mu` (documented T4 Step 5; does not contradict CF2 which governs async `ApplyDelta`). ✓
- **Carry-forward 3 (no-hang finalization):** `ApplyDelta` is event-driven; a lifecycle delta triggers `flushRun`. No blocking wait for a terminal; a delayed/coalesced `session_ended` leaves the buffer in memory until close (bounded per run) — never deadlocks. Backstop = snapshot-then-attach (`SnapshotState` runs the same map+flush path for terminal runs). Lifecycle deltas are non-coalescable (`Kind:RunID` key, M1c-1) so node churn cannot swallow `session_ended`. ✓
- **Carry-forward 4 (node_status full-state upsert):** `ApplyDelta` routes BOTH `DeltaNodeUpsert` and `DeltaNodeStatus` through `upsertNode`, which replaces `st.node` wholesale from `d.Node`; asserted by `TestApplyDeltaNodeStatusIsFullStateUpsert` (a `node_status`-only node carries full tokens into its span). ✓
- **Deps-first:** T1 adds the OTel-SDK stack (each `go get` one at a time, no tidy, build green) BEFORE any import in T2 — mirrors M1a's deps-first task. Exact set + versions listed (v1.44.0). ✓
- **Seam-for-coverage:** the `spanExporter` interface + `newWithExporter` fake (`recordExporter`/`errExporter`) give 100% of the mapper + finalization with NO network; the real-client construction (both schemes) is covered by lazy `New` tests; the one real dial is the genuine end-to-end `TestServeStartsExporter` against an in-process mock sink (uses `require.Eventually`, no sleep). ✓
- **Placeholder scan:** every code step is complete Go. The two design forks (T2 minimal-then-widened struct; T4 `ready`/`rank` removal) are RESOLVED inline with the final code form shown and a one-line rationale; no TBD/TODO/"similar to". ✓
- **Type consistency:** `spanExporter` defined T2, implemented by `*otlptrace.Exporter` (real) + `recordExporter`/`errExporter` (fake); `Exporter` struct widened T2→T4 consistently; `RunFilter`/`Snapshot`/`SnapshotState`/`ApplyDelta` signatures stable; `Metrics.ExporterLag`/`SetOTLPEndpoint`/`exporterConsumer`/`runDaemonWith(+otlpEndpoint)` defined and used consistently with ALL call sites updated. ✓
- **No-dead-code / 100% genuine coverage:** `ready`/`rank` removed (no reader) per T4 Step 4 resolution; every method/branch has an asserting test; the `normalizeAddr` `SplitHostPort` error fallback covered; both lazy client schemes covered. ✓
- **Concurrency / no `time.Sleep`:** the consumer goroutine uses `select` on `ctx.Done()` + channel; all tests use synchronous calls or `require.Eventually` (forbidigo-safe). The exporter goroutine never touches `d.mu`; the bus mutex is leaf-only → no lock-order deadlock with `d.mu` (same argument as M1c-1). ✓
