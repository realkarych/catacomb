# M3 — Realtime subscription surfaces (SSE + gRPC stream) design

**Status:** approved (autonomous run, mandate-delegated)
**Date:** 2026-06-22
**Milestone:** M3 (spec §9 Realtime Surfaces, §18 milestone "M3 — Realtime surfaces. WS/SSE + gRPC stream.")
**Consumes:** the M1c CDC bus (`cdc.Bus`/`Consumer`/`GraphDelta`), the live `publishDelta` path, the snapshot-then-attach discipline proven by the OTLP exporter, the loopback+bearer trust boundary (ADR-0013).

## 1. Goal

Expose the live canonical graph to external consumers in real time: a consumer
subscribes (optionally filtered to a run / node-type / tier), receives an
**initial consistent snapshot**, then a **live `GraphDelta` stream** as the graph
evolves. Two transports:

- **SSE** (`text/event-stream` over the existing loopback HTTP server) — zero new
  dependency; browser-native (EventSource) and trivially consumable by any HTTP
  client. **M3a.**
- **gRPC server-streaming** (`GraphService.Subscribe(SubscribeRequest) returns
  (stream GraphDelta)`) — typed, programmatic; protobuf mirrors the canonical
  model. Requires a new protobuf toolchain. **M3b.**

Both are gated by the per-daemon bearer token (ADR-0013: "bearer token for any
TCP/realtime surface + gated all-runs subscription").

## 2. Why SSE + gRPC, and why WebSocket is deferred

The realtime contract is **server → client push** of a snapshot followed by a
delta stream. SSE realizes exactly this with the standard library
(`http.Flusher` + `text/event-stream`), auto-reconnects in browsers, and needs no
dependency. gRPC server-streaming gives the typed surface programmatic consumers
want. Together they cover the browser and the programmatic client.

**WebSocket is deferred** (possible M3c). WS adds a third-party dependency and a
bidirectional framing layer the delta push does not use (the client sends only the
initial filter, expressible as SSE query params or a gRPC request message). The
M5 embedded web UI can consume SSE via `EventSource`; if a concrete UI need for
WS emerges there, M3c adds it with full context. This is the YAGNI- and
ethos-aligned choice (pure-Go, minimal deps).

## 3. Architecture: one subscription core, two transports

```
                          ┌────────────────────────────────────────┐
ingest (4 sources) ─▶ applyAndPersist ─▶ publishDelta ─▶ cdc.Bus    │  (all under d.mu)
                          │                                  │ fan-out (N consumers)
                          │                          ┌───────┴───────┐
                          │                          ▼               ▼
                          │                   exporterConsumer   subscription consumers
                          │                                          │
                  d.Subscribe(filter)  ── snapshot-then-attach ──────┤
                  (under d.mu: collect filtered snapshot from         │
                   d.graphs, THEN d.bus.Subscribe — no gap)           │
                                                                      ▼
                                              ┌───────────────────────┴───────────┐
                                              ▼                                    ▼
                                       SSE handler (M3a)                  gRPC graphServer (M3b)
                                       text/event-stream                  stream GraphDelta
```

### 3.1 The subscription core (`daemon`, shared by both transports)

A single daemon method does the race-free handshake; the transports only marshal.

```go
type SubFilter struct {
    RunID     string   // "" / "all" = every run; else exact model.Node.RunID / GraphDelta.RunID
    NodeTypes []string // empty = all node types (model.NodeType values)
    Tiers     []string // empty = all tiers (model.Node.Tier; today every node is "core")
}

type Subscription struct {
    Snapshot []cdc.GraphDelta // synthesized node_upsert/edge_upsert deltas (the stream head)
    Consumer *cdc.Consumer    // live deltas after the snapshot point
}

func (d *Daemon) Subscribe(f SubFilter, bufSize int) *Subscription
func (d *Daemon) Unsubscribe(s *Subscription)
```

`Subscribe` runs **entirely under `d.mu`**: it iterates `d.graphs`, collects the
filtered nodes/edges as copy-safe `node_upsert`/`edge_upsert` deltas, then calls
`d.bus.Subscribe(bufSize)`. Because `publishDelta` also runs under `d.mu`, **no
delta can be published between snapshot collection and consumer registration** — the
snapshot reflects every delta up to seq *S*, and the consumer receives every delta
from *S+1* onward. No gap, no duplication. (This is the exact invariant the OTLP
exporter relies on.)

**Copy-safety (foundational).** Today `publishDelta` does a *shallow* struct copy
of `Node`/`Edge`: the `Attrs`/`Annotations` maps and `Sources` slice stay shared
with the live graph. That is safe only because the OTLP exporter reads scalar fields
only — but the reducer mutates those ref fields in place on already-published nodes
(`cascadeStatus` sets `Attrs["cancel_cause"]`; `applySource` appends to `Sources`),
so a realtime consumer that marshals `attrs`/`sources` off the bus concurrently with
ingest would data-race. M3 therefore replaces the shallow copy with a shared
`copyNode`/`copyEdge` helper that also clones the `Attrs`/`Annotations` maps (one
level — the reducer only ever stores scalar attr values) and the `Sources` slice.
Both `publishDelta` (live deltas) and `Subscribe` (snapshot) use it, under `d.mu`,
so every bus delta is fully read-safe. Proven by a `-race` test: a consumer
marshalling a node's `Attrs` while ingest drives `cancel_cause`/source-append churn.

The snapshot is emitted as `GraphDelta`s of kind `node_upsert`/`edge_upsert` so the
**wire format is uniform**: a client applies snapshot events and live deltas with
the identical code path, and the per-entity `Rev` lets a client de-dup idempotently
(belt-and-suspenders for the bus's coalesce-drop re-emit, §3.3).

### 3.2 Filtering

`SubFilter` is applied to **both** the snapshot (during collection) and the live
stream (each `Consumer.C` delta before send). Predicates:

- **RunID** — `f.RunID` empty or `"all"` matches every delta; else
  `delta.RunID == f.RunID`. (Runs span executions; `d.graphs` is execID-keyed, so
  the snapshot scans all shards and filters by `RunID`.)
- **NodeTypes** — applies to deltas carrying a node (`node_upsert`/`node_status`)
  and to snapshot nodes; lifecycle deltas (`run_started`/`run_ended`/
  `session_ended`) and edge deltas pass the type filter (they carry no node type)
  but still honor the RunID filter. Edge deltas additionally pass only if **both**
  endpoints would pass — deferred: M3 filters edges by RunID only (endpoint-type
  filtering needs node-type lookup; YAGNI for v1, documented).
- **Tiers** — same shape as NodeTypes against `model.Node.Tier`.

### 3.3 Backpressure & lifecycle

The bus already coalesces per consumer (latest-state-per-key) and counts drops; a
slow subscriber never blocks the daemon — it receives coalesced latest state and a
rising `Dropped()`. Each subscription uses a bounded buffer (`subBufSize`, e.g.
256). The transport handler is **ctx-bound**: on client disconnect
(`r.Context().Done()` for SSE, the stream context for gRPC) it calls
`Unsubscribe` (which removes the consumer from the bus and closes its channel) and
returns. No goroutine leak: each subscription is one handler goroutine that owns its
consumer for the connection's lifetime.

## 4. M3a — SSE surface

- **Route:** `GET /v1/subscribe` on the existing loopback HTTP mux, **gated by the
  existing `authed` bearer wrapper** (Authorization header). Query params:
  `run`, `type` (repeatable / comma-list), `tier` (repeatable / comma-list).
- **Response:** `Content-Type: text/event-stream`, `Cache-Control: no-cache`,
  `Connection: keep-alive`. Each event is `data: <GraphDelta-as-JSON>\n\n`; the
  JSON envelope mirrors the export node/edge schema (`model.Node`/`model.Edge` json
  tags + a `kind` + `rev`). The optional SSE `id:` field carries `Rev`.
- **Flow:** require `http.Flusher` (else 500); write headers; `sub := d.Subscribe`;
  `defer d.Unsubscribe`; write each snapshot event then `Flush`; then loop:
  `select { <-ctx.Done() → return; ev := <-sub.Consumer.C → if matchDelta: write +
  Flush }`. A keep-alive comment (`: ping\n\n`) on a ticker keeps idle proxies open
  (ticker, **never `time.Sleep`** — forbidigo).
- **Reconnect:** snapshot-on-connect; a reconnecting client gets a fresh consistent
  snapshot (no `Last-Event-ID` replay buffer in M3a — correct by construction).
- **Browser auth note:** `EventSource` cannot set headers, so a browser needs a
  `?token=` query param. M3a keeps the **header-only** trust boundary uniform with
  every other route; the query-param variant (with its token-in-logs caveat) is
  **deferred to M5** when the embedded UI consumes the feed.
- **CLI (optional, M3a):** `catacomb watch [--run X] [--type ...]` — a thin SSE
  client that prints deltas; dogfooding + an integration-test driver. Reuses the
  discovery file for addr+token. (Include only if it stays small; else defer.)

## 5. M3b — gRPC streaming surface

### 5.1 Protobuf toolchain (new infrastructure)

No `.proto`/buf/codegen exists today (M1 reuses only the vendored OTLP protos).
M3b introduces:

- `proto/catacomb/v1/graph.proto` — `package catacomb.v1; option go_package
  ".../gen/catacombv1";` defining:
  - `service GraphService { rpc Subscribe(SubscribeRequest) returns (stream GraphDelta); }`
  - `SubscribeRequest { string run_id = 1; repeated string node_types = 2;
    repeated string tiers = 3; }`
  - `GraphDelta { string kind = 1; uint64 rev = 2; Node node = 3; Edge edge = 4;
    string old_id = 5; string new_id = 6; string run_id = 7; string execution_id = 8; }`
  - `Node` / `Edge` messages mirroring the `model` fields the wire needs
    (scalars + timing + tokens + status + tier + attrs-as-`map<string,string>` of
    JSON-encoded values, to avoid `Struct` bloat; payloads excluded by default).
- **buf** (`buf.yaml`, `buf.gen.yaml`) driving `protoc-gen-go` + `protoc-gen-go-grpc`;
  a `make proto` target regenerates into `gen/catacombv1/`. Generated `*.pb.go`
  carry the standard `// Code generated … DO NOT EDIT.` header.
- **Gates:** generated files are excluded from coverage (`.testcoverage.yml`
  exclude for `gen/`), skipped by golangci-lint (generated-header default) and by
  `internal/codepolicy` (already skips the generated header). The **hand-written**
  `graphServer` + interceptor + delta-mapping are 100% covered.

### 5.2 Server

- `streamBearerInterceptor(token) grpc.StreamServerInterceptor` mirrors the unary
  one (constant-time, reads `authorization` from stream metadata); added via
  `grpc.StreamInterceptor(...)` in `newGRPCServer` alongside the unary one.
- `graphServer{d *Daemon}` implements `Subscribe`: parse `SubscribeRequest` →
  `SubFilter`; `sub := d.Subscribe`; `defer d.Unsubscribe`; send each snapshot delta
  (mapped cdc→proto); loop on `sub.Consumer.C` filtered by `matchDelta`, sending
  until `stream.Context().Done()` or a send error. A `toProtoDelta(cdc.GraphDelta)
  *catacombv1.GraphDelta` mapping is the only marshalling code.
- Registered on the existing second loopback gRPC listener; `Discovery.GRPCAddr`
  already advertises it.

## 6. Auth & trust boundary (ADR-0013)

Both surfaces require the bearer token (SSE: `authed` header wrapper; gRPC: stream
interceptor). "Subscribe all runs" (`run` empty/`all`) is allowed only with a valid
token — same gate as a single-run subscription; no separate privilege tier in M3
(documented: all authenticated local callers are equally trusted, per the
loopback+same-user model). Listeners stay loopback-only.

## 7. Wire envelope

The SSE JSON and the gRPC `GraphDelta`/`Node`/`Edge` both **mirror the existing
export schema** (`export/jsonl` embeds `*model.Node`/`*model.Edge` with their json
tags + a `kind`). The SSE envelope is `{"kind": "...", "rev": N, "run_id": "...",
"execution_id": "...", "node": {…model.Node…}}` (or `"edge"`, or `old_id`/`new_id`
for `node_merge`, or none for lifecycle kinds). Payloads are omitted by default
(redaction surface, ADR-0020); `attrs`/`annotations` included.

## 8. Decomposition

### 8.1 M3a — SSE (this plan)

1. Copy-safety foundation: shared `copyNode`/`copyEdge` (clone Attrs/Annotations
   maps + Sources slice); `publishDelta` uses it instead of the shallow copy;
   `-race` test (consumer marshals Attrs during cascade/source-append ingest).
2. Subscription core in `daemon` (`SubFilter`, `Subscribe`/`Unsubscribe`,
   `matchNode`/`matchEdge`/`matchDelta`, snapshot via `copyNode`/`copyEdge`) — pure,
   unit-tested (snapshot reflects filter; race-free handshake; Unsubscribe detaches).
3. SSE handler + route (`GET /v1/subscribe`, `authed`-gated, Flusher, snapshot →
   live loop, keep-alive ticker, ctx-bound Unsubscribe) + filter parsing; end-to-end
   test with a raw HTTP client (snapshot + live delta + filter drop + 401 + disconnect).
4. (Optional) `catacomb watch` SSE client CLI (reuses discovery addr+token) +
   its test — include only if small, else defer to M5.

### 8.2 M3b — gRPC stream (next plan)

1. Protobuf toolchain: `graph.proto` + buf + `make proto` + gates exclusions.
2. `graphServer.Subscribe` + `toProtoDelta` mapping + stream bearer interceptor +
   registration; bufconn in-memory streaming test.
3. (Optional) `catacomb watch --protocol grpc` parity.

### 8.3 Independence

M3a ships a complete, usable realtime surface on its own (SSE). M3b adds the typed
gRPC surface and is independently mergeable. WS (M3c) is deferred.

## 9. Constraints (inherited, binding)

Go 1.26 pure-Go (no cgo); **NO comments except `//go:build|//go:embed|//go:generate`**
and the generated-file header (`internal/codepolicy`); **100% line coverage under
`-race`** (`make cover`) with generated `*.pb.go` excluded from the gate;
golangci-lint v2 clean (gofumpt, goimports local-prefix
`github.com/realkarych/catacomb`, govet shadow, **forbidigo bans `time.Sleep`** —
use tickers, `unparam`, `errcheck`, `rowserrcheck`, `bodyclose`); **never
`go mod tidy`** (add gRPC/proto deps, if any beyond those present, via `go get`);
single-mutex daemon (`d.mu`) — the subscription handshake holds it, the per-connection
stream loop does NOT; cross-platform (`GOOS=windows go build ./...` clean, no
unix-only syscalls; tests use `require.Eventually`, close listeners before temp-dir
cleanup); loopback + bearer trust boundary; commit per task; never commit to master
mid-plan.

## 10. Testing strategy (M3-specific)

- **Subscription core:** snapshot reflects current graph under filter; deep-copy
  isolation (mutating a returned snapshot node does not touch the live graph);
  `matchNode`/`matchDelta` table tests (run/type/tier, empty=all, `all`);
  Subscribe-then-Unsubscribe detaches from the bus.
- **Race-free handshake:** an ingest concurrent with Subscribe yields the delta
  exactly once (snapshot xor stream) — assert no missing/duplicate node by `Rev`.
- **SSE:** `httptest` server (or the real loopback) + a client reading the stream:
  401 without token; snapshot events then a live delta after an ingest; filter
  drops a non-matching run; client disconnect unsubscribes (bus consumer count
  returns to baseline via `require.Eventually`); Flusher-absent → 500.
- **gRPC:** `bufconn` in-memory; Unauthenticated without token (stream
  interceptor); snapshot + live delta over the stream; `toProtoDelta` mapping table
  test; stream context cancel unsubscribes.
- All `-race`, host + windows build.

## 11. Deferred → M3c / M4 / M5 / Step 7

- **WebSocket** (M3c, only if M5's UI needs it).
- **Query-param token** for browser `EventSource` (M5, with the token-in-logs
  caveat).
- **`Last-Event-ID` / seq-cursor replay** on reconnect (snapshot-on-connect
  suffices now; replay would read the obs log from a cursor).
- **Edge filtering by endpoint node type** (M3 filters edges by RunID only).
- **Per-subscription `Dropped`/lag metric** surfaced in `/metrics` (the bus already
  counts; wiring is small — fold in if cheap, else defer).
