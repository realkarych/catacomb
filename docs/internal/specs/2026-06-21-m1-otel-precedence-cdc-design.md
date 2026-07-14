# M1 — OTel enrichment + four-source precedence + CDC + passthrough exporter (design)

- **Status:** Approved (autonomous design — see [[autonomous-completion-mandate]])
- **Date:** 2026-06-21
- **Deciders:** @realkarych (delegated; decisions documented here)
- **Builds on:** M0.4 (merged): shard eviction + lazy reload + `/metrics` + operability
- **Implements (subsets of):** ADR-0002, ADR-0003, ADR-0004, ADR-0007, ADR-0009,
  ADR-0012, ADR-0013, ADR-0014, ADR-0015, ADR-0018, ADR-0019

## 1. Context & goal

M0.x delivered a single-source (hooks-only) live execution graph: the daemon receives
Claude Code hook events, reduces them into a graph, persists observations, and
recovers across restarts. The graph structure and payload come entirely from hooks.

M1 adds **OTel as the second live ingress**, alongside the existing hook receiver, and
builds the infrastructure that lets future ingress sources (JSONL tailer in M2,
stream-json in M2) participate in the same graph without conflict. Concretely:

- Claude Code emits OTLP spans when `CLAUDE_CODE_ENABLE_TELEMETRY=1` — spans carry
  token counts, latencies, model names, and span-level parent/child structure.
- These spans enrich but do not replace hook-derived structure (ADR-0002: hooks are the
  backbone; OTel is enrichment-only).
- The `tool_use_id` attribute on tool spans is the cross-source linchpin: an OTel
  `claude_code.tool` span and a hook `PreToolUse` event describing the same tool call
  carry the same `tool_use_id` and must collapse into one `tool_call` node.

M1 also delivers the **CDC delta bus** and **OTLP/OpenInference passthrough exporter**
needed to feed downstream observability pipelines (Jaeger, Arize Phoenix, LangSmith
etc.) from catacomb's enriched, de-duplicated execution graph.

## 2. Scope

**In scope:**

- OTLP/HTTP and OTLP/gRPC receivers (the second ingress alongside hooks).
- `ingest/otel` adapter: `ResourceSpans` → `[]model.Observation`.
- OTel GenAI semconv and OpenInference attribute extraction.
- `tool_use_id` cross-source merge (OTel + hook collapse to one node).
- Full four-source per-field precedence table (ADR-0003).
- ADR-0014 conditional structure rule (the #53954 gate).
- Status lattice extension: `superseded` + `abandoned` ranks; full nine-status `rank()`.
- Transitive status cascade over `parent_child` closure (`cancelled`/`superseded`).
- `Rev uint64` field on `model.Node` (populate) and `model.Edge` (add + populate).
- CDC delta bus (`cdc` package): `GraphDelta`, `Bus`, `Consumer`.
- Delta emission from the reducer/daemon.
- OTLP/OpenInference passthrough exporter (`export/otlp`): CDC subscriber, node→span
  mapper, self-loop guard.
- `catacomb env` CLI verb: prints the five env vars to aim Claude Code at the daemon.
- `listen.otlp_grpc`, `listen.otlp_http`, `exporters.otlp.endpoint` config keys.
- Metrics additions: `deltas_dropped`, `exporter_lag` per consumer.

**Out of scope → later milestones:**

- JSONL tailer and stream-json ingress → M2.
- WebSocket/gRPC streaming graph surfaces → M3.
- neo4j/postgres stores → M4.
- Per-execution OTel-completeness latch (ADR-0014 optional optimization) → post-M1.
- Selective recover (rebuild only non-terminal runs) → post-M1.

## 3. OTLP receiver

### 3.1 Two listeners, one daemon

The daemon gains two new listen addresses alongside the existing hook HTTP server.

**OTLP/HTTP** is served on the existing daemon HTTP mux as a new route:
`POST /v1/traces` accepts `Content-Type: application/x-protobuf` with a body of
`ExportTraceServiceRequest`. The route is registered behind the existing `authed`
middleware (bearer token). A per-request `recover()` converts any panic to a
quarantined poison observation (ADR-0019 §1); the response is HTTP 200 with an empty
`ExportTraceServiceResponse` on success and HTTP 400/500 on parse failure.

**OTLP/gRPC** runs on a second loopback listener: a `grpc.Server` that registers
`collectorv1.TraceServiceServer`. Bearer-token authentication is enforced by a unary
server interceptor that reads the `authorization` gRPC metadata key. The gRPC server
runs in its own goroutine, under the daemon's `Serve` context; a supervisor goroutine
with `recover()` + exponential-backoff restart wraps the `grpc.Server.Serve` call
(ADR-0019 §1).

Both receivers call the same `Daemon.IngestOTLP(req *ExportTraceServiceRequest) error`
method, which mirrors the existing `Daemon.Ingest(hookType, payload)` path.

### 3.2 Discovery file

The daemon's discovery file gains a new field `otlp_grpc_addr` alongside the existing
`hook_addr`. On startup, after binding the gRPC listener, the daemon writes this field.
The `catacomb env` CLI verb reads the discovery file and emits:

```
CLAUDE_CODE_ENABLE_TELEMETRY=1
CLAUDE_CODE_ENHANCED_TELEMETRY_BETA=1
OTEL_TRACES_EXPORTER=otlp
OTEL_EXPORTER_OTLP_PROTOCOL=grpc
OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:<otlp_grpc_port>
OTEL_EXPORTER_OTLP_HEADERS=authorization=Bearer <token>
```

### 3.3 Security and isolation

**Token gate:** the bearer token used for the hook receiver is reused for both OTLP
endpoints. The token is installed once (via `install-hooks` or `catacomb env`) and
stored in the user's environment. `OTEL_EXPORTER_OTLP_HEADERS` wires it into the Go
OTel SDK's OTLP exporter.

**Panic isolation (ADR-0019 §1):** a single malformed span batch never crashes the
daemon. The HTTP handler and the gRPC `Export()` implementation each wrap their body in
`recover()`. A recovered panic produces a quarantine log entry with the execution ID
and the panic value; the daemon increments `quarantined` and returns HTTP 500 / gRPC
status `Internal` to the sender. The gRPC supervisor restarts after backoff if
`grpc.Server.Serve` itself returns an error.

**Schema stability (spec §17 risk):** Claude Code's OTel span names and attributes are
beta and may change. All attribute lookups go through a versioned attribute map in
`ingest/otel`. Unknown span names fall back to a generic `hook_event`-equivalent
observation (`attrs.identity = "heuristic"`) rather than a parse error.

## 4. OTel→model mapping (`ingest/otel`)

### 4.1 Package contract

New package `ingest/otel` with a single exported entry point:

```go
func Parse(
    req *collectorv1.ExportTraceServiceRequest,
    execID string,
    nextSeq func() uint64,
) ([]model.Observation, error)
```

This mirrors `ingest/hook.Parse`. The function is pure (no I/O, no side effects). Each
`Span` in the request becomes one `model.Observation` with `Source: model.SourceOTel`.
The reducer (unchanged interface) processes these observations through the same
`applyAndPersist` path as hook observations.

### 4.2 Span→observation pipeline

```
ResourceSpans
  └── ScopeSpans
        └── Span
              ├── SpanId          → Observation.Correlation.SpanID (hex)
              ├── ParentSpanId    → Observation.Correlation.ParentSpanID (hex)
              ├── Name            → Observation.Kind (verbatim; mapper selects NodeType)
              ├── StartTimeUnixNano → Observation.EventTime (t_start)
              ├── EndTimeUnixNano → drives t_end when non-zero and span is complete
              └── Attributes      → see §4.3
```

Resource attributes (once per `ResourceSpans`, applied to all child spans):

- `session.id` → `Correlation.SessionID` and `Observation.RunID`
- `service.name` = `"claude_code"` → validation (non-fatal if absent; allows
  non-Claude-Code OTel sources)
- `user.id`, `app.version` → stored in `Observation.Attrs`

### 4.3 Attribute extraction

The mapper checks for both OTel GenAI semconv attrs and the parallel Claude Code
native attrs; when both are present, GenAI semconv wins (higher provenance specificity
for Claude Code sources). OpenInference attrs are checked as a third tier when neither
GenAI nor Claude Code attrs are present.

**Identity and correlation:**

| Attribute(s) checked | Correlation field | Notes |
|---|---|---|
| `tool_use_id`, `gen_ai.tool.call.id` | `Correlation.ToolUseID` | Linchpin; both names checked |
| `agent_id` | `Correlation.AgentID` | |
| `parent_agent_id` | `Correlation.ParentAgentID` | |
| `session.id`, `session_id`, `gen_ai.conversation.id` | `Correlation.SessionID` | |

**Node fields:**

| Attribute(s) checked | Node field | Notes |
|---|---|---|
| `gen_ai.usage.input_tokens`, `input_tokens`, `llm.token_count.prompt` | `Node.TokensIn` | GenAI name preferred |
| `gen_ai.usage.output_tokens`, `output_tokens`, `llm.token_count.completion` | `Node.TokensOut` | GenAI name preferred |
| `gen_ai.usage.cache_read.input_tokens`, `cache_read_tokens` | `Attrs["cache_read_tokens"]` | |
| `gen_ai.usage.cache_creation.input_tokens`, `cache_creation_tokens` | `Attrs["cache_creation_tokens"]` | |
| `gen_ai.request.model`, `gen_ai.response.model`, `model`, `llm.model_name` | `Attrs["model"]` | |
| `gen_ai.tool.name`, `tool_name`, `tool.name` | `Node.Name` | for tool spans |
| `gen_ai.agent.name` | `Node.Name` | for agent spans |
| `mcp_server_name`, `mcp_tool_name` | `Attrs["mcp_server_name"]` etc. | triggers `NodeMCPCall` |
| `subagent_type` | `Node.SubagentType` | |
| `ttft_ms`, `duration_ms` | `Attrs["ttft_ms"]` etc. | |

### 4.4 Span→node type mapping

| Span name | Node type | Key used | Notes |
|---|---|---|---|
| `claude_code.interaction` | `assistant_turn` | `message.id` attr if present, else `span_id` | Root interaction span |
| `claude_code.llm_request` | `assistant_turn` | `span_id` | Token/cost enrichment; see #53954 note |
| `claude_code.tool`, `claude_code.tool.execution`, `execute_tool {name}` | `tool_call` or `mcp_call` | `tool_use_id` linchpin | MCP tool if `mcp_server_name` present |
| `claude_code.hook` | `hook_event` | `span_id` | Supplementary detail tier |
| `invoke_agent {name}`, `chat {model}` | `subagent` or `assistant_turn` | `gen_ai.agent.id`, `span_id` | GenAI semconv agent/chat span |
| OpenInference `LLM` kind | `assistant_turn` | `span_id` | OpenInference fallback |
| OpenInference `TOOL` kind | `tool_call` | `tool_call.id` → `ToolUseID` | OpenInference fallback |
| OpenInference `AGENT` kind | `subagent` | `span_id` | OpenInference fallback |
| OpenInference `CHAIN` kind | `marker` | `span_id` | |
| unknown | `hook_event` (generic) | `span_id` | `attrs.identity = "heuristic"` |

The `openinference.span.kind` attribute is checked after Claude Code span names: a span
with both a recognized Claude Code name and an OpenInference kind uses the Claude Code
classification.

### 4.5 The `tool_use_id` cross-source linchpin

When an OTel span carries `tool_use_id` (or its GenAI-semconv equivalent
`gen_ai.tool.call.id`), the mapper sets `Correlation.ToolUseID`. The reducer calls
`model.ToolCallID(executionID, toolUseID)` to derive the canonical node ID — the same
function used when processing a hook `PreToolUse` observation. The two observations
converge on one `tool_call` node: hook contributes payload and provisional timing; OTel
contributes authoritative timing, token counts, and model name.

### 4.6 The `parent_span_id` edge and the #53954 gate

The mapper records `ParentSpanID` in `Correlation.ParentSpanID` but does not create a
`parent_child` edge itself. The reducer creates that edge conditionally (§5.3). On the
Agent SDK streaming path (Claude Code driven via `query()` / `--output-format
stream-json`), only `claude_code.llm_request` spans fire — no interaction, tool, or
hook spans. These spans have no OTel children and no `tool_use_id`. They arrive as flat
enrichment observations that add token/cost data to nodes the hook-derived structure
already placed in the graph. They do not generate `parent_child` edges.

## 5. Four-source precedence and status

### 5.1 Per-field precedence table

When multiple sources provide a value for the same field, the reducer applies
source-ranked wins. The table below governs every field-level merge (ADR-0003/0014).

| Field group | Source precedence (high → low) | Tiebreak |
|---|---|---|
| Structure: `parent_id`, edge type | Conditional: JSONL → OTel (if span has children or `tool_use_id`) → OTel → stream-json `parent_tool_use_id` → hook heuristics | `seq` (ADR-0018) |
| Timing: `t_start`, `t_end`, `duration_ms` | OTel span times → hooks (pre/post `event_time`) → JSONL → stream-json | `seq` |
| Cost/tokens: `tokens_in`, `tokens_out`, `cost_usd` | OTel GenAI attrs → stream-json `result.usage` → JSONL | `seq` |
| Payload: `payload.input`, `payload.output` | hooks (`tool_input`/`tool_response`) / JSONL (full) → stream-json deltas | `seq` |
| MCP attrs: `mcp_server_name`, `mcp_tool_name` | OTel `mcp_*` attrs → hooks mcp fields → name-pattern parse (`mcp__<srv>__<tool>`) | `seq` |
| Name (`name` field) | First non-empty value from any source | `seq` |
| Status | Not in this table — status lattice governs (§5.2) | — |

The reducer's field-merge path gains a source-tagged `stamp` check: before overwriting
a field, compare the ranked source of the incoming observation against the ranked source
that last set the field. A lower-ranked source never overwrites a higher-ranked value.
The existing `applyTokens` function (which currently overwrites unconditionally) is
updated to check `Source == SourceOTel` before winning the tokens fields.

### 5.2 Status lattice

Nine statuses across three rank tiers (ADR-0014 §2 + ADR-0012):

```
Rank 3 — genuine terminals (irreversible; never downgraded):
    ok | error | blocked

Rank 2 — provisional (superseded by any genuine terminal):
    cancelled | unknown | superseded | abandoned

Rank 1:
    running

Rank 0:
    pending
```

`resolveStatus(cur, next)` returns:

- `cur` if `rank(cur) == 3` and `rank(next) < 3` (genuine terminal is permanent).
- `next` if `rank(next) >= rank(cur)` (higher rank wins; equal rank → `next` wins,
  i.e. the latest observation takes the status).
- `cur` otherwise.

The existing `rank()` function in `reduce/reduce.go` covers `ok`/`error`/`blocked` →
3, `cancelled`/`unknown` → 2, `running` → 1, default → 0. M1b adds `superseded` → 2
and `abandoned` → 2.

**`superseded`** applies to nodes on a regeneration/edit branch (ADR-0009/0014). The
JSONL tailer will be the primary producer in M2; M1 only adds `superseded` to `rank()`
so the reducer accepts it correctly in the lattice if it arrives from any source.

**`abandoned`** applies to a `Run`, not individual nodes. Individual nodes under a
run-ended event continue to receive `unknown` (consistent with existing
`closeIfOpen`/`closeOpenDescendants` behavior). The `abandoned` rank-2 entry ensures
that if a future source marks individual nodes abandoned, the lattice handles it without
a later upgrade.

### 5.3 ADR-0014 conditional structure rule (the #53954 gate)

An OTel `parent_child` edge is accepted only when the child span satisfies either of:

- **(a)** at least one OTel child span has been observed for that span ID, **or**
- **(b)** the observation carries `Correlation.ToolUseID` (tool spans always carry it).

Otherwise the JSONL/hook-derived edge stands.

**Implementation:** `Graph` gains a per-execution field
`spanChildren map[string]bool` (key = parent `span_id`; value = has-been-observed-as-a
parent). As each OTel observation arrives with a non-empty `ParentSpanID`, the reducer
records `spanChildren[ParentSpanID] = true` for that execution. Before promoting an OTel
`parent_child` edge, the reducer checks:

```go
if obs.Source == SourceOTel && obs.Correlation.ParentSpanID != "" {
    if !g.spanChildren[obs.Correlation.SpanID] && obs.Correlation.ToolUseID == "" {
        // condition not met — skip OTel edge, let JSONL/hook structure stand
        return
    }
    // condition met — upsert the parent_child edge
}
```

No version detection of the Claude Code SDK is needed: the rule is correct by
construction for all SDK versions (a collapsed OTel tree has only flat `llm_request`
spans; an expanded tree has child spans and/or `tool_use_id`-bearing tool spans).

The per-execution OTel-completeness latch described in ADR-0014 (a monotonic flag that,
once set, stops consulting `spanChildren`) is an optional optimization deferred to
post-M1.

### 5.4 Transitive status cascade

**Trigger:** any node transitions to `cancelled` or `superseded` via any source.

**Action:**

1. BFS/DFS from that node over `parent_child` edges in the current graph.
2. For each reachable descendant: if `rank(descendant.Status) < 3` (not a genuine
   terminal), set `descendant.Status = <triggering status>` and
   `descendant.Attrs["cancel_cause"] = <triggering node ID>`.
3. Emit `DeltaNodeStatus` for each affected descendant (once CDC bus is wired in M1c).

This generalizes the existing `closeOpenDescendants` (which BFS-walks descendants and
sets `StatusUnknown`) by parameterizing the target status and adding a "skip genuine
terminals" guard. The existing call sites (`session_end` → `closeOpenDescendants` sets
`StatusUnknown`; `run_ended` → same) remain unchanged in behavior; only the function
signature is generalized.

### 5.5 `Rev` field on `Node` and `Edge`

`model.Node` already has a documented `Rev uint64` field but it is not yet populated by
the reducer. `model.Edge` has no `Rev` field today.

M1b adds:

- Population of `n.Rev` in every reducer mutation path:
  `n.Rev = max(n.Rev, o.Seq)` (latest `seq` that touched this node wins; never
  rolls back). This is the "latest wins" rule for non-status, non-structure fields.
- `Rev uint64` on `model.Edge` (added to `model/model.go`).
- Population of `e.Rev` in `graph.upsertEdge`: `e.Rev = o.Seq` on create;
  `e.Rev = max(e.Rev, o.Seq)` on re-parent.

These `Rev` values flow into `GraphDelta` (§6) and allow consumers to apply
rev-guarded idempotent upserts (ADR-0015 §1).

## 6. CDC delta bus (`cdc` package)

### 6.1 `GraphDelta` type

```go
type GraphDeltaKind string

const (
    DeltaNodeUpsert   GraphDeltaKind = "node_upsert"
    DeltaEdgeUpsert   GraphDeltaKind = "edge_upsert"
    DeltaNodeStatus   GraphDeltaKind = "node_status"
    DeltaNodeMerge    GraphDeltaKind = "node_merge"
    DeltaEdgeDelete   GraphDeltaKind = "edge_delete"
    DeltaRunStarted   GraphDeltaKind = "run_started"
    DeltaSessionEnded GraphDeltaKind = "session_ended"
    DeltaRunEnded     GraphDeltaKind = "run_ended"
)

type GraphDelta struct {
    Kind        GraphDeltaKind
    Rev         uint64
    Node        *model.Node
    Edge        *model.Edge
    OldID       string
    NewID       string
    RunID       string
    ExecutionID string
}
```

Semantics per kind:

- **`node_upsert`** — node created or any non-status field changed. `Node` carries the
  full current state.
- **`edge_upsert`** — edge created or re-parented. `Edge` carries the full current
  state.
- **`node_status`** — status-only change (optimization; consumers may treat as
  `node_upsert`). Reduces fan-out volume during cascade passes.
- **`node_merge`** — provisional id rewritten to canonical id. `OldID` = provisional;
  `NewID` = canonical. Consumers must update references, move edges, and move
  annotations.
- **`edge_delete`** — tombstone for an edge that was deleted as part of a re-parent.
  Rev-guarded: consumers apply only if `delta.Rev > e.Rev` in the sink.
- **`run_started`** — lifecycle open.
- **`session_ended`** — lifecycle close; OTLP exporter keys finalization on this.
- **`run_ended`** — lifecycle close.

`Rev` is set to the originating `seq` of the observation that caused the mutation.
Consumers implement conditional upsert: apply delta only if `delta.Rev > current_rev`.

### 6.2 `Bus` and `Consumer`

```go
type Bus struct { ... }

func NewBus() *Bus
func (b *Bus) Subscribe(bufSize int) *Consumer
func (b *Bus) Unsubscribe(c *Consumer)
func (b *Bus) Publish(d GraphDelta)

type Consumer struct {
    C       <-chan GraphDelta
    Dropped func() int64
}
```

**Fan-out:** `Publish` sends `d` to every subscribed consumer's channel.

**Drop semantics (coalesce-to-latest):** when a consumer's channel is full on
`Publish`, the bus marks that node dirty (stores its current materialized state),
skips the enqueue, increments the consumer's `Dropped` counter, and schedules a
re-emit of the full materialized node state once the channel drains. A consumer
always eventually receives the final state; no state is permanently lost.

**Metrics:** the daemon's `metricsSnapshot()` aggregates `Dropped()` counts across all
consumers and exposes them as `deltas_dropped` and `exporter_lag` in `GET /metrics`.

### 6.3 Snapshot-then-attach

When a new consumer connects (or reconnects after a restart):

1. The caller takes `g.Snapshot()` (current materialized nodes and edges under
   `d.mu`).
2. Calls `exporter.Snapshot(ctx, filter)` to push full state to the downstream sink.
3. Calls `b.Subscribe(bufSize)` to receive live deltas.
4. Stores a per-consumer `last_applied_seq` cursor. On reconnect, if the cursor is
   behind the bus's retention window, falls back to a full snapshot.

### 6.4 Delta emission seam

**Open decision (to settle in the M1c plan):** there are two viable approaches for how
the reducer/daemon produces deltas:

- **(a) In-`Graph` delta buffer (recommended):** `Graph.Apply` collects
  `[]GraphDelta` in a field during the apply pass; `Daemon.applyAndPersist` drains that
  slice after `Apply` returns and calls `bus.Publish(d)` for each delta. This requires
  a modest change to `Graph.Apply` (add a `deltas []GraphDelta` field flushed at
  the end of each apply call) but avoids any before/after diffing overhead and keeps
  all mutation logic inside the reducer.
- **(b) Before/after diff:** the daemon snapshots the node map before `Apply` and diffs
  after. Simpler to wire but O(N) per observation in a large graph.

**Recommendation:** (a) — the in-`Graph` buffer. The reducer already knows exactly
what it changed; emitting from the mutation site is authoritative and cheap. The M1c
plan must decide and implement.

## 7. Passthrough exporter (`export/otlp`)

### 7.1 Package contract

New package `export/otlp` following the pattern of `export/jsonl/export.go`:

```go
type Exporter struct { ... }

func New(
    ctx context.Context,
    endpoint string,
    daemonGRPCAddr string,
    daemonHTTPAddr string,
) (*Exporter, error)

func (e *Exporter) Name() string
func (e *Exporter) ApplyDelta(ctx context.Context, delta cdc.GraphDelta) error
func (e *Exporter) Snapshot(ctx context.Context, filter RunFilter) error
```

`New` returns an error (and never starts) if `endpoint` resolves to either daemon
receiver address (§7.2).

### 7.2 Self-loop guard (ADR-0013/0019)

At initialization, `New` normalizes `endpoint` and both daemon listener addresses:
strip scheme, resolve hostname to loopback where applicable, compare `host:port`. If
the endpoint matches `daemonGRPCAddr` or `daemonHTTPAddr`:

```
otlp exporter: endpoint "<endpoint>" is the daemon's own receiver;
refusing to create a self-loop (ADR-0019)
```

This prevents the ingestion → export → ingestion cycle at startup, not at runtime.

### 7.3 Finalization logic

The exporter buffers nodes per run and emits spans in batches:

- `DeltaNodeUpsert` / `DeltaNodeStatus` — update the exporter's internal
  `nodeMap[node.ID]` with the latest materialized state. No export yet.
- Check `delta.Node.Status` rank: if rank 3 (genuine terminal), mark the node
  `ready_to_export`.
- `DeltaSessionEnded` / `DeltaRunEnded` — flush all `ready_to_export` nodes in the
  run, plus any remaining buffered nodes regardless of status (lifecycle close forces
  finalization even for nodes in provisional status).

Nodes in provisional status (`cancelled`, `unknown`, `superseded`) are not emitted
until a lifecycle close event arrives, ensuring that a superseded intermediate node does
not pollute the downstream OTLP sink with duplicate spans.

A genuine terminal arriving after a lifecycle-close span is a documented stale-span
case; OTLP is the one immutable eventual-consistency-exempt sink per ADR-0015.

### 7.4 Node→span mapper

Exported spans carry both OTel GenAI semconv and OpenInference attributes for maximum
downstream compatibility.

**OpenInference span kind** (`openinference.span.kind`):

| Node type | OpenInference kind |
|---|---|
| `subagent` | `AGENT` |
| `tool_call`, `mcp_call` | `TOOL` |
| `assistant_turn` | `LLM` |
| `marker` | `CHAIN` |
| `session`, `user_prompt`, `hook_event` | `CHAIN` |

**Token and cost attributes** (set on each exported span):

| Node field | OpenInference attr | OTel GenAI attr |
|---|---|---|
| `Node.TokensIn` | `llm.token_count.prompt` | `gen_ai.usage.input_tokens` |
| `Node.TokensOut` | `llm.token_count.completion` | `gen_ai.usage.output_tokens` |
| `Node.CostUSD` | `llm.cost.total` | — |

Also set `gen_ai.provider.name = "anthropic"` when the session is known to be a Claude
Code session.

Span timing: `Node.TStart` → `StartTimeUnixNano`; `Node.TEnd` → `EndTimeUnixNano`
(zero if not yet finalized at export time — lifecycle close may export unfinished nodes).

Span parent linkage: if the node has a `parent_child` edge to a parent node, use the
parent node's span ID as `ParentSpanId` in the exported span. The exporter reconstructs
the OTel trace tree from catacomb's graph edges.

### 7.5 Transport

The exporter uses the Go OTel SDK client packages — `otlptracegrpc.New` or
`otlptracehttp.New` — as the OTLP client. These handle connection pooling, retry, and
backpressure. The exporter does not re-implement the OTLP wire protocol.

Transport is configured by `exporters.otlp.endpoint` in `catacomb.yaml`. If the
endpoint uses a `grpc://` or bare `host:port` scheme it uses the gRPC transport;
`http://` or `https://` uses the HTTP transport.

### 7.6 Wiring in the daemon

After the bus is created and the first snapshot is taken, the daemon starts the
exporter consumer loop:

```go
consumer := d.bus.Subscribe(exporterBufSize)
go func() {
    for delta := range consumer.C {
        _ = otlpExporter.ApplyDelta(ctx, delta)
    }
}()
```

`exporterBufSize` is a tunable (default 1024) exposed in config. The exporter goroutine
runs under the daemon's `Serve` context and exits when the context is cancelled.

## 8. Decomposition

Three sub-milestones, each independently testable with unit/fixture tests before the
next begins.

### M1a — OTLP receiver + `ingest/otel` adapter + `Rev` on Edge

**Deliverables:**

- `ingest/otel` package: `Parse()` converting `ExportTraceServiceRequest` →
  `[]model.Observation`. Unit-tested with fixture span payloads covering Claude Code
  span names, OTel GenAI semconv, and OpenInference.
- OTLP gRPC receiver: `grpc.Server` + `TraceServiceServer` calling `d.IngestOTLP`.
  Supervisor goroutine with `recover()` + backoff.
- OTLP HTTP receiver: `POST /v1/traces` handler on the existing mux behind `authed`.
- `Daemon.IngestOTLP()`: extracts `session_id` from resource attrs, calls
  `ingest/otel.Parse`, routes to `applyAndPersist`.
- `catacomb env` CLI verb (five env vars + bearer token header).
- `otlp_grpc_addr` in the discovery file.
- `Rev uint64` on `model.Edge`.

**Independence test:** send a fixture `ExportTraceServiceRequest` with a
`claude_code.tool` span carrying `tool_use_id = "toolu_abc"`, plus a hook `PreToolUse`
with the same `tool_use_id`, via the in-process path. Assert: exactly one `tool_call`
node exists; it has OTel timing and hook payload merged; `Correlation.ToolUseID` is
set. Verifies the cross-source linchpin without requiring CDC or precedence changes.

### M1b — Four-source precedence + ADR-0014 cascade/superseded

**Deliverables:**

- `rank()` in `reduce/reduce.go` updated: `superseded` → 2, `abandoned` → 2.
- Per-field source-ranked `stamp` check in the reducer field-merge path.
- `Graph.spanChildren map[string]bool` in `graph.go`, populated by OTel observations.
- ADR-0014 #53954 gate in `upsertEdge`: checks `spanChildren` before promoting an OTel
  parent edge.
- `cascadeStatus(rootID string, status model.Status)` generalizing
  `closeOpenDescendants`: BFS with "skip genuine terminals" guard, sets
  `Attrs["cancel_cause"]`.
- `Rev` population: `n.Rev = max(n.Rev, o.Seq)` in all reducer mutation paths;
  `e.Rev` on edge upserts.

**Independence test:** reduction commutativity property tests (same four-source
observation set applied in all orders; assert identical final graph). Specific test for
issue 53954: OTel-only `llm_request` + hook `PreToolUse/PostToolUse` → no OTel
`parent_child` edge (span has no children and no `tool_use_id`). Cascade test: cancel
a parent `tool_call` → assert non-terminal descendants become `cancelled` with
`cancel_cause` set; genuine-terminal descendants unchanged.

### M1c — CDC bus + OTLP/OpenInference passthrough exporter

**Deliverables:**

- `cdc` package: `Bus`, `Consumer`, `GraphDelta` types with all eight kinds.
  Coalesce-to-latest drop semantics. `Dropped` counter per consumer.
- Delta emission: `Graph.Apply` collects `[]GraphDelta` (per the in-`Graph` buffer
  recommendation in §6.4); `Daemon.applyAndPersist` publishes to `bus` after each
  apply.
- `export/otlp` package: `Exporter` with `ApplyDelta`, `Snapshot`, self-loop guard,
  finalization logic, OpenInference attribute mapping.
- Exporter wiring in `Daemon.Serve`: snapshot-then-attach consumer loop.
- `deltas_dropped` and `exporter_lag` in `GET /metrics` JSON.
- Config: `listen.otlp_grpc`, `listen.otlp_http`, `exporters.otlp.endpoint` in
  `catacomb.yaml`.

**Depends on:** M1a (for `Rev` on edges) and M1b (for correct status/cascade in
emitted deltas). The `cdc` and `export/otlp` package skeletons can be authored in
parallel with M1b work.

**Independence test:** end-to-end test with a mock OTLP sink (a minimal in-process
`TraceServiceServer` recording received spans). Run a fixture through the reducer; wait
for CDC to deliver `session_ended`; assert the mock sink received spans with correct
OpenInference kind and attribute values. Self-loop guard test: configure exporter
endpoint = `localhost:4317` (same as daemon gRPC receiver) → `New()` returns error.

## 9. Dependencies

All packages are pure-Go (no cgo). Add with `go get` one at a time in the order
below — **never `go mod tidy`** (binding project rule, AGENTS.md).

**For the OTLP receiver (server role):**

```
go get go.opentelemetry.io/proto/otlp@latest
go get google.golang.org/grpc@latest
go get google.golang.org/protobuf@latest
```

Sub-packages used from `go.opentelemetry.io/proto/otlp`:

- `collector/trace/v1` — `TraceServiceServer`, `ExportTraceServiceRequest`,
  `RegisterTraceServiceServer`
- `trace/v1` — `ResourceSpans`, `ScopeSpans`, `Span`
- `common/v1` — `KeyValue`, `AnyValue`

`google.golang.org/protobuf` is likely already a transitive dependency; verify after
`go get` to avoid redundant pinning.

**For the OTLP passthrough exporter (client role):**

```
go get go.opentelemetry.io/otel@latest
go get go.opentelemetry.io/otel/sdk@latest
go get go.opentelemetry.io/otel/exporters/otlp/otlptrace@latest
go get go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc@latest
go get go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp@latest
```

Estimated new direct deps in `go.mod`: 5–7 `require` lines for the receiver/exporter
stack, plus 10–20 transitive entries (grpc, protobuf runtime, OTel SDK, net/http2).

## 10. Constraints (inherited, binding)

- **Go 1.26 pure-Go.** All new packages (receiver, `cdc`, `export/otlp`) are pure-Go;
  no cgo. `modernc.org/sqlite` remains the only SQLite binding.
- **No comments in Go code** (`internal/codepolicy`). Zero doc comments, zero inline
  comments; only `//go:build`, `//go:embed`, `//go:generate` directives allowed.
- **100% line coverage under `-race`** (threshold never goes down). Every new package
  (`ingest/otel`, `cdc`, `export/otlp`) must reach 100% line coverage; new branches in
  `reduce/` and `model/` must be covered.
- **golangci-lint v2 clean.** No lint warnings introduced.
- **Never `go mod tidy`.** Use `go get <pkg>@latest` for each new dependency; verify
  imports compile after each addition.
- **Single-mutex daemon discipline.** All `d.graphs`, `d.lastSeen`, `d.execBySession`,
  bus subscription list, and metrics counters accessed under `d.mu`. The CDC bus
  `Publish` call happens inside the existing `applyAndPersist` path, which already runs
  under `d.mu`.
- **`nowFn` injection.** All time references in new code use the injected `nowFn`;
  no `time.Now()` calls in non-test code.
- **Cross-platform.** gRPC and OTel SDK packages are cross-platform; no OS-specific
  syscall paths introduced.
- **Observation log is the system of record (ADR-0012).** The CDC bus is non-durable;
  it carries derived graph state. No observation is deleted. Durability is the
  `observations` table; the bus is a live fan-out layer on top of it.
- **ADR-0019 quarantine.** Every new goroutine or handler that can receive external
  input (OTLP gRPC, OTLP HTTP) wraps its body in `recover()`. Panics are quarantined,
  not propagated. The gRPC supervisor restarts with backoff. One bad span batch never
  crashes the daemon or affects other runs.
- **ADR-0002 enrichment-only.** OTel absence is not an error. The hook-derived graph
  is complete without OTel; OTel adds enrichment. If no OTLP spans arrive for an
  execution, the reducer produces the same graph it would have without M1.
