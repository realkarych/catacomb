# Catacomb — Design Spec

**Status:** Draft for review
**Date:** 2026-06-20
**License:** Apache-2.0
**Language:** Go

> *A catacomb is a network of passages where what mattered is preserved. Catacomb is a sidecar that wraps a live Claude Code instance, observes everything it does — hooks, subagent allocation, tool calls, MCP calls — and assembles it, in real time, into one queryable execution graph.*

---

## 1. Summary

Catacomb is a universal, open-source observability tool that builds a **real-time execution graph of agentic pipelines running on Claude Code**. It runs as a **daemon (sidecar)** next to one or more Claude Code instances, ingests four raw signal sources, reconciles them into a single **canonical action graph**, persists it, and exposes it live (WebSocket/SSE, gRPC, embedded web UI) and via streaming/snapshot **exporters** to `jsonl`, `neo4j`, `postgres`, and an OTLP/OpenInference passthrough.

Catacomb is **domain-agnostic** and **evaluation-agnostic**: it does not score, judge, or optimize anything. It produces a faithful, queryable graph with a generic per-node annotation slot so downstream systems (e.g. a step-level evaluation layer) can attach their own metadata without Catacomb knowing about it.

## 2. Goals & Non-Goals

### Goals

- Real-time construction of an execution graph from a **live** Claude Code run, with low overhead.
- Work across **both** ways of driving Claude Code: the **Claude Agent SDK** (programmatic) and **interactive CLI**.
- Capture, as first-class graph elements: **hook events, subagent allocation + parent→child orchestration, tool calls, MCP calls**, LLM requests, and user-defined phase markers.
- Reconcile **overlapping observations** of the same action (from up to four sources) into **one canonical node**.
- Persist a **forest of runs** that is queryable offline and survives restarts.
- Stream the graph live and export it to `jsonl` / `neo4j` / `postgres` / **OTLP (OpenInference passthrough)** as a **materialized graph** (idempotent upsert + change-data-capture), plus on-demand snapshots.
- Be a clean **reusable Go library** with the daemon and CLI as thin frontends.
- Conform to **OpenTelemetry GenAI semantic conventions** and **OpenInference** on import and export boundaries.

### Non-Goals (v1)

- **No evaluation / scoring / optimality** (MC-value, rollouts, reward models, improvement reports). Out of scope; supported only via a generic annotation slot for downstream consumers.
- **No pipeline orchestration or control** of Claude Code. Catacomb only observes.
- **No multi-tenant SaaS, auth, RBAC.** Single-operator local/self-hosted tool.
- **No support for non–Claude-Code runtimes** in v1 (the data model is kept runtime-neutral to allow it later).
- **No bit-exact replay or model determinism guarantees** (Claude Code does not expose temperature/seed; see §15).

## 3. Background & Constraints

Claude Code emits observable signal through four channels, each partial:

| Source | Strength | Weakness |
|---|---|---|
| **Hooks** (`PreToolUse`, `PostToolUse`, `UserPromptSubmit`, `Stop`, `SubagentStop`, `SessionStart`, `SessionEnd`, `PreCompact`, `Notification`) | Fire on **both** SDK and CLI paths; carry `session_id`, `tool_name`, `tool_input`, `tool_response`, `transcript_path`, MCP fields | Flat events; parent→child must be reconstructed |
| **Native OpenTelemetry traces** (beta: `CLAUDE_CODE_ENABLE_TELEMETRY=1` + `CLAUDE_CODE_ENHANCED_TELEMETRY_BETA=1` + `OTEL_TRACES_EXPORTER`) | Genuine parent→child span tree (`claude_code.interaction` → `llm_request`/`hook`/`tool`; subagent spans nest under the dispatching `tool` span), per-node `agent_id`/`parent_agent_id`/`subagent_type`, cost/token, dedicated MCP signals | **Beta**: span names/attrs may change; on the **Agent SDK streaming path only `llm_request` spans fire** (issue #53954) → hierarchy can collapse on the SDK path; subagent nesting is a recent fix (~v2.1.145) |
| **`stream-json`** (`claude -p --output-format stream-json`) | NDJSON: `system`(init→`session_id`, mcp_servers, model), `assistant`, `user`(tool_result), `stream_event`(carries `parent_tool_use_id`, `uuid`, `session_id`), `result`(usage) | CLI envelope schema officially **undocumented** (issues #24612/#24596) |
| **Transcript JSONL** (`~/.claude/projects/<encoded-cwd>/<sessionId>.jsonl`, subagents under `…/<sessionId>/subagents/agent-<agentId>.jsonl` + `agent-<agentId>.meta.json`) | **Source of truth for the conversation tree and the subagent tree**; carries `uuid`/`parentUuid` (threading), `promptId`, `leafUuid`, `agentId`, `isSidechain`, `interruptedMessageId`; pairs `tool_use`↔`tool_result` by `tool_use_id`; offline replay | Written to disk (slight lag); on-disk shape is largely undocumented and version-fragile |

The hook set above is the known-stable core; the exact event taxonomy is version-dependent (e.g. permission decisions surface through `PreToolUse` output, and tool failures through `PostToolUse` with an error `tool_response`, rather than as dedicated events). Hooks also carry per-event subagent attribution (`agent_id`, `agent_type`, and on `SubagentStop` an `agent_transcript_path`) on recent versions. The hook receiver isolates event-name and field knowledge behind the versioned parser of §17.

**On-disk transcript vs SDK message surface.** The conversation **thread is a tree** keyed by `uuid`/`parentUuid` *on disk*; the Agent SDK's `getSessionMessages()` exposes only `{type, uuid, session_id, message, parent_tool_use_id}` and does **not** surface `parentUuid`/`isSidechain`/`leafUuid`. Therefore the JSONL adapter reconstructs threading from the **files**, and `parent_tool_use_id` is used only for the subagent boundary, never as the conversation thread. A transcript also contains many **non-conversational meta-records** (`system`/`compact_boundary`, `isCompactSummary`, `file-history-snapshot`, `attachment`, `permission-mode`, `mode`, `last-prompt`, `ai-title`, `queue-operation`, `pr-link`, `isMeta`) that must not be promoted to prompts/turns. The interpretation of all of this — threading, interruption, regeneration branches, subagent parentage, meta-records, and the `Agent`↔`Task` / inline-↔-separate-sidechain version duality — is specified in §5.8 and decided in ADR-0009.

**Design consequence:** because the user drives Claude Code via **both the Agent SDK and interactive CLI**, **hooks are the backbone** (they fire on every path). Native OTel is **enrichment** where it is whole (interactive CLI / `claude -p`). JSONL is the **backfill** for the subagent tree when OTel collapses on the SDK path (#53954). `stream-json` is an additional structural signal (notably `parent_tool_use_id`).

**Cross-source anchor:** `session_id` is present in all four sources (hook payload/transcript, OTel resource/span attributes, `stream-json` `system.init`, and the JSONL filename). It is the join key that groups one run across sources. `run_id := session_id` (with an optional wrapper mapping for pipelines that span multiple sessions; see §5.4).

## 4. Architecture

```
            ┌─ ingestion adapters ─────────┐    ┌─ core (library) ─────────────┐    ┌─ fan-out ──────────────┐
 hooks ───▶ │ hook receiver (HTTP, local)  │    │                              │ ─▶ │ WebSocket / SSE        │
 OTel  ───▶ │ OTLP receiver (gRPC+HTTP)    │ ─▶ │  normalizer / correlator     │    │ gRPC stream            │
 -p    ───▶ │ stream-json reader           │    │      │                       │ ─▶ │ embedded web UI        │
 jsonl ───▶ │ JSONL tailer (fsnotify)      │    │  observation log (append)    │    └────────────────────────┘
            └──────────────────────────────┘    │      │                       │    ┌─ exporters ────────────┐
                                                 │  canonical graph (in-mem)    │ ─▶ │ jsonl   (CDC + snapshot)│
                                                 │      │                       │    │ otlp    (OpenInference) │
                                                 │  durable store (SQLite/DuckDB)│   │ neo4j/pg (upsert + CDC) │
                                                 └──────────────────────────────┘    └────────────────────────┘

  package layout:  /catacombcore (lib)  ·  /cmd/catacomb (daemon+CLI)  ·  thin frontends over the lib
```

**Data flow:** adapter → raw **Observation** → append-only observation log → **reducer** (canonical-entity merge with precedence) updates the **in-memory canonical graph** and the **durable store** → state transitions emit **CDC deltas** → deltas fan out to live subscribers and streaming exporters. Snapshots and offline replay read the durable store / observation log.

**Process model:** one long-running daemon. Hooks invoke a tiny forwarder (`catacomb hook <type>`, same binary) that POSTs the event to the daemon. Everything else (OTLP receiver, JSONL tailer, stream-json reader, surfaces, exporters) lives in the daemon. The core graph engine is an importable library (`catacombcore`) with no daemon/CLI dependencies.

## 5. Data Model

### 5.1 Observation (raw, append-only — the system of record)

```
Observation {
  obs_id        string         ULID, monotonic per daemon
  run_id        string         = session_id (see §5.4)
  source        enum{hook, otel, stream_json, jsonl}
  kind          string         source-native event type (e.g. "PostToolUse", "claude_code.tool")
  correlation   {              any subset present
    session_id, tool_use_id, parent_tool_use_id,
    span_id, parent_span_id, agent_id, parent_agent_id,
    message_id, uuid
  }
  attrs         map[string]any source-native fields
  payload       Payload?       tool input/output, prompt text (subject to redaction; §11)
  observed_at   time           when the daemon received it
  seq           uint64         global receive order
}
```

Observations are never mutated. The canonical graph is a **deterministic reduction** of the observation log, so the graph can always be rebuilt from the log (and tests assert this; §16).

### 5.2 Node (canonical, materialized)

```
Node {
  id            string         canonical id, stable across sources (§5.5)
  run_id        string
  type          enum{session, user_prompt, assistant_turn, tool_call, subagent, mcp_call, hook_event, marker}
  parent_id     string?        structural/causal parent
  agent_id      string?        subagent/teammate that issued this; null on main
  parent_agent_id string?
  subagent_type string?
  name          string         tool name / subagent type / mcp tool / marker name
  status        enum{pending, running, ok, error, blocked, cancelled, superseded}
  t_start, t_end time?
  duration_ms   int?
  tokens_in, tokens_out int?
  cost_usd      float?
  attrs         map[string]any model, cwd, span_id, transcript_path, mcp_server_name, mcp_tool_name, permission_decision, ...
  payload       Payload?       redacted per policy
  payload_hash  string?        sha256 of pre-redaction payload (dedup/integrity)
  sources       []SourceRef    {source, obs_id, observed_at} that contributed (provenance)
  tier          enum{core, detail}  core nodes always emitted; detail nodes (assistant_turn, hook_event) emitted only when graph.granularity = rich (§12)
  annotations   map[string]any RESERVED for downstream consumers; Catacomb never reads/writes these
}
```

The `tier` field is the per-node half of the single granularity axis: the `graph.granularity` config (§12) is `lean` or `rich`; `lean` materializes only `core`-tier nodes, `rich` materializes `core` + `detail`.

### 5.3 Edge (canonical, materialized)

```
Edge {
  id     string                deterministic: hash(run_id, type, src, dst)
  run_id string
  type   enum{parent_child, sequence, marker_span, data_dep}
  src    string                node id
  dst    string                node id
  attrs  map[string]any
}
```

- `parent_child` — delegation/spawn and structural nesting (orchestrator→subagent, agent→tool_call, tool_call→mcp_call).
- `sequence` — temporal order within one agent's stream (optional; detail-tier, emitted only when `graph.granularity = rich`).
- `marker_span` — groups all nodes of a phase under a `marker` node (see §5.6).
- `data_dep` — reserved/future; not populated in v1.

### 5.4 Run identity

`run_id := session_id`. A `Run` is the connected subgraph sharing a `run_id`, plus run-level metadata captured for reproducibility (§15): pinned `model_id` (snapshot, not alias), and version hashes of prompts / skills / subagent definitions / Catacomb config. For pipelines spanning multiple Claude Code sessions, a **wrapper run id** groups N session_ids into one logical run. Primary mechanism: env **`CATACOMB_RUN_ID`** set by the orchestrator and inherited by every child session (SDK and CLI); `catacomb run --run-id <id>` is sugar that sets this env for the wrapped process. Absent the env, `run_id` falls back to `session_id`. Sessions attach as child subgraphs under the wrapper run. (Marker-driven grouping deferred.)

### 5.5 Canonical id derivation (so all sources collapse to one node)

The linchpin is `tool_use_id`, which is shared across hooks, JSONL, and stream-json; OTel tool spans carry it as an attribute when present.

| Node type | Canonical id | Fallback |
|---|---|---|
| `session` | `session_id` | — |
| `user_prompt` | `run_id : uuid` | `run_id : t_start` |
| `assistant_turn` | `run_id : message_id` | `run_id : span_id` |
| `tool_call` / `mcp_call` | `run_id : tool_use_id` | `run_id : span_id` (OTel-only); last-resort heuristic = `run_id : agent_id : name : t_start±ε` |
| `subagent` | `run_id : agentId` (the `agent-<agentId>.jsonl` file id) | spawning tool_use id from `agent-<agentId>.meta.json` `toolUseId` → parent `Agent`/`Task` `tool_call`; live: hook `agent_id` / SDK `parent_tool_use_id`; old layout: inline `isSidechain:true` threaded by `parentUuid` |
| `hook_event` | `run_id : obs_id` | — |
| `marker` | `run_id : marker_seq` | — |

The last-resort heuristic is **conservative**: an OTel-only tool observation lacking `tool_use_id` becomes a *provisional* node tagged `attrs.identity=heuristic`, and is merged into a canonical node only if a strong key (`tool_use_id`) arrives later — Catacomb prefers a rare duplicate over a wrong merge.

`mcp_call` is a `tool_call` whose name matches `mcp__<server>__<tool>` (or whose attrs carry `mcp_server_name`/`mcp_tool_name`); it is typed `mcp_call` and keyed identically.

### 5.6 Markers (phase boundaries)

Catacomb is phase-agnostic but supports **user-defined markers** to delimit logical phases. The orchestrator emits an explicit marker; Catacomb pairs `start`/`end` into a `marker` node and links the phase's nodes via `marker_span` edges.

```
marker { run_id, name, boundary: start|end, state_ref?: <git-sha|snapshot-id>, ts, attrs }
```

Emission channels (any; most-robust first): a no-op MCP call `catacomb__mark` (reliable on the SDK path), a sentinel `UserPromptSubmit`/log line convention, or a direct POST to the daemon. (`state_ref` is an opaque string Catacomb stores but does not interpret — e.g. a downstream eval layer may use it to snapshot deterministic state.)

### 5.7 OpenTelemetry / OpenInference mapping

The canonical model is **graph-native**. Bidirectional mappers live at the boundaries:

- **Import:** OTLP spans → Observations (span kind/attrs → node type/fields; `parent_span_id` → `parent_child`).
- **Export:** nodes/edges → OpenInference span kinds (`AGENT` for subagent, `TOOL` for tool/mcp, `LLM` for assistant_turn, `CHAIN` for marker spans) for OTel/OpenInference backends.

This keeps DAG-shaped edges and non-OTel sources expressible while remaining interoperable.

### 5.8 Conversation threading, interruption & branches

Field-level rules for interpreting one transcript into structure (decided in ADR-0009):

- **Threading is a tree, not a chain.** `parent_child` edges within a run follow the on-disk `parentUuid` tree, read from the transcript files (the SDK surface omits `parentUuid`). An `assistant_turn` and its `tool_call`s attach to the nearest ancestor `user_prompt` found by walking `parentUuid` upward; `promptId` (present on user-side records) corroborates the attribution.
- **Multiple prompts.** Every real user message is a `user_prompt` child of the session; a session holds as many prompt subtrees as the user sent. Sibling order comes from `t_start` (optionally `sequence` edges).
- **Interruption.** Detected from the transcript, not hooks (the `Stop` hook does not fire on user interrupt). The interrupting message is an ordinary new `user_prompt`; the assistant turn named by the following user record's `interruptedMessageId` — and any in-flight `tool_call` of it lacking a `tool_result` — transitions to status `cancelled`. Cancellation is a status change, never a re-parenting.
- **Regeneration / edit branches.** A `parentUuid` may have several children (edit/retry/regenerate); all branches are kept. The active branch ends at the current leaf (`leafUuid` / the latest `last-prompt`); nodes off that path get status `superseded` (marked, never deleted). An SDK session-level fork (new `sessionId`) is a separate session grouped by the wrapper run-id (§5.4), not an in-file branch.
- **Meta-records.** Non-conversational records (the §3 list) are classified and never promoted to `user_prompt`/`assistant_turn`; compaction boundaries (`compact_boundary`, `isCompactSummary`) are segment markers, not prompts.
- **Version duality.** Tool name (`Agent`|`Task`), sidechain layout (inline `isSidechain:true` | separate `subagents/` files), and the presence of recent attribution fields are resolved behind the versioned parser (§17); unknown record/event types are recorded generically, not dropped.
- **Milestone.** M0.1's single-pass reducer captures only a subset (it does not yet thread `parentUuid` or model interruption/branches); full threading lands with the JSONL tailer in M0.2+.

## 6. Ingestion Adapters

All adapters normalize to **Observation** and push to the log. Adapters are a Go interface so the community can add more:

```go
type Adapter interface {
    Name() string
    Start(ctx context.Context, sink ObservationSink) error
}
```

1. **Hook receiver (backbone).** `catacomb install-hooks` writes `~/.claude/settings.json` (or project settings) entries that run `catacomb hook <type>`; the forwarder reads the hook JSON on stdin and POSTs to the daemon over loopback (`127.0.0.1`, configurable port / unix socket). Captures every hook type listed in §3. Forwarder is dependency-light and fails open (never blocks the agent; on daemon-down it drops with a local warning).
2. **OTLP receiver (enrichment).** Daemon exposes OTLP/gRPC + OTLP/HTTP endpoints. `catacomb env` prints the env to enable native traces (`CLAUDE_CODE_ENABLE_TELEMETRY=1`, `CLAUDE_CODE_ENHANCED_TELEMETRY_BETA=1`, `OTEL_TRACES_EXPORTER=otlp`, `OTEL_EXPORTER_OTLP_ENDPOINT=...`). Maps spans/metrics/logs → Observations. Tolerant to beta schema drift via a versioned span-attribute map (whose drift risk is tracked in §17).
3. **stream-json reader.** `catacomb run -- claude -p ... --output-format stream-json [--verbose --include-partial-messages]` spawns and tees the child's NDJSON to the daemon while passing it through to the user; also `catacomb ingest stream-json < file` for piped input. Parses `system/assistant/user/stream_event/result`; extracts `parent_tool_use_id` for subagent linkage. Envelope parsing is isolated behind a community-shared schema module (whose undocumented-schema risk is tracked in §17).
4. **JSONL tailer (subagent-tree truth + offline).** Watches the projects dir via fsnotify for `*.jsonl` and `**/subagents/agent-*.jsonl`; pairs `tool_use`↔`tool_result` by `tool_use_id`; reconstructs the subagent parent→child tree. Also drives `catacomb replay <path>` for fully offline graph construction from past transcripts (no live Claude Code).

## 7. Reconciliation & Node Lifecycle

**Strategy: canonical entity + merge with per-field precedence.** Each real action is one node; sources are observations merged by canonical id.

- **Lifecycle:** first observation for an id creates a `pending`/`running` node; subsequent observations (start→end, enrichment, late/out-of-order) **idempotently upsert** fields. Out-of-order is handled because merge is commutative per field given precedence + timestamps.
- **Per-field precedence** (higher wins; ties broken by latest `observed_at`):

  | Field group | Precedence (high → low) |
  |---|---|
  | Structure (parent/child, nesting) | OTel span tree → JSONL tree → stream-json `parent_tool_use_id` → hook heuristics |
  | Timing (`t_start`/`t_end`/`duration`) | OTel spans → hooks (pre/post) → JSONL → stream-json |
  | Cost / tokens | OTel metrics/usage → stream-json `result.usage` → JSONL |
  | Payload (input/output) | hooks (`tool_input`/`tool_response`) / JSONL (full) → stream-json deltas |
  | MCP attrs | OTel `mcp_*` → hooks mcp fields → name-pattern parse |

- **Provenance:** every contributing observation is recorded in `Node.sources`, so any field is traceable to its origin and the merge is auditable.
- **CDC:** each create/field-change/edge-add emits a typed **GraphDelta** (`node_upsert`, `edge_upsert`, `node_status`, `run_started`, `run_ended`) to the fan-out bus. Subscribers and streaming exporters consume the same delta stream.

## 8. Persistence

- **Durable layer:** embedded DB, **SQLite by default** (ubiquitous, zero-config), **DuckDB optional** (columnar, better for cross-run analytical queries). Behind a `Store` interface.
- **Tables:** `observations` (append-only log), `nodes`, `edges`, `runs`, `markers`. Nodes/edges hold the materialized graph; observations allow full rebuild.
- **In-memory graph** serves realtime reads/subscriptions; the durable store is written through (batched) and is the recovery source.
- **Crash recovery:** on start, rebuild the in-memory graph by replaying `observations` (authoritative) or loading materialized `nodes`/`edges` (fast path) and reconciling.
- **Retention:** append-only by default; optional per-run TTL / max-runs eviction (config). No retention enforcement in the hot path.

## 9. Realtime Surfaces

- **WebSocket / SSE:** subscribe to a run (or all runs); receive an initial graph snapshot then a live **GraphDelta** stream. JSON envelope mirrors the export node/edge schema. Filters: run_id, node type, tier.
- **gRPC stream:** typed `Subscribe(run_id, filter) → stream GraphDelta` for programmatic consumers; protobuf mirrors the canonical model.
- **Embedded web UI:** static assets embedded via Go `embed`, served by the daemon; talks to the SSE/WS feed. Views (v1, all four):
  1. **Live graph/DAG** — dagre/force layout (e.g. Cytoscape.js), nodes/edges update live, colored by type/status.
  2. **Timeline / waterfall** — per-agent swim-lanes, durations, parallelism.
  3. **Node inspector** — payload in/out, cost/tokens, attrs, contributing sources.
  4. **Run list / compare** — browse the run forest; filter; diff two runs.

## 10. Exporters

Pluggable interface; **materialized graph with idempotent upsert + CDC** is the default semantics across all targets; jsonl additionally supports an event-log mode.

```go
type Exporter interface {
    Name() string
    ApplyDelta(ctx context.Context, delta GraphDelta) error
    Snapshot(ctx context.Context, filter RunFilter) error
}
```

- **jsonl** — default: one JSON object per node and per edge (materialized), re-emitted on change with a `rev` counter; **event-log mode** (`--mode=events`) streams raw observations/deltas instead. Files split per run; append-only.
- **otlp (OpenInference passthrough)** — re-emits the graph as OpenInference spans to any OTLP endpoint (Arize Phoenix, Tempo, Honeycomb, …) via the §5.7 export mapper; CDC emits spans as nodes finalize, snapshot replays a run. Near-zero added code; gives free trajectory visualization in external backends.
- **neo4j** — nodes as labeled nodes (`:Session`, `:ToolCall`, `:Subagent`, `:McpCall`, `:Marker`, …), edges as relationships (`PARENT_OF`, `NEXT`, `IN_PHASE`). `MERGE` on canonical id for idempotent upsert; CDC applied via Bolt. Snapshot = batched `MERGE`.
- **postgres** — `nodes` / `edges` tables keyed by canonical id (`INSERT … ON CONFLICT DO UPDATE`), JSONB for `attrs`/`payload`/`annotations`; optional adjacency views and `pg_notify` for downstream CDC. Snapshot = upsert in a transaction.

Both streaming (continuous CDC as the graph grows) and snapshot (`catacomb export --to jsonl|otlp|neo4j|postgres [--run <id>]`) are supported for every target.

## 11. Payload Handling & Privacy

Configurable; default **store payload + hash + redaction**.

- Store tool inputs/outputs and prompt text by default, **plus `sha256` of the pre-redaction payload** (dedup/integrity), with configurable **redaction rules**: regex patterns and key-path globs (e.g. `*.api_key`, `authorization`), applied before persistence/export. Redacted spans replaced by `‹redacted:reason›` with the hash retained.
- Modes: `full+hash+redact` (default) · `refs-only` (store only `transcript_path` refs + hashes, no payload) · `all` (no redaction; logged warning).
- Size cap per payload (config; default e.g. 256 KiB) with overflow stored as a ref/hash. Redaction and caps apply uniformly to persistence **and** exporters.

## 12. Configuration & CLI

- **Config:** `catacomb.yaml` (+ env overrides + flags). Sections: `listen` (hook/OTLP/api ports, unix socket), `store` (engine, path), `sources` (enable/disable each adapter, projects dir), `graph` (`granularity: rich|lean`, default `rich`; sequence-edge on/off), `exporters` (per-target DSN, mode, on/off), `privacy` (mode, redaction rules, size cap), `ui` (enable, bind), `retention`.
- **CLI** (`catacomb`):
  - `daemon` — run the sidecar.
  - `install-hooks [--project|--global]` — wire the hook forwarder into settings.json.
  - `env` — print env to enable native OTel traces.
  - `run -- <cmd>` — wrap a Claude Code invocation (stream-json tee + run-id).
  - `ingest stream-json` — read NDJSON from stdin.
  - `replay <path>` — offline graph build from transcript JSONL.
  - `export --to jsonl|otlp|neo4j|postgres [--run <id>] [--mode]` — snapshot/stream to a sink.
  - `snapshot [--run <id>]` — dump current graph.
  - `runs` / `inspect <run_id>` — list/query the forest from the terminal.

## 13. Concurrency, Performance, Backpressure

- High subagent fan-out and (future) downstream rollouts multiply event volume; the pipeline is built around **bounded channels** with explicit policies.
- Adapters → observation log: bounded queue; **hooks fail open** (forwarder drops + warns rather than blocking the agent). OTLP/stream-json/JSONL backpressure within the daemon (buffer + spill to store).
- Reducer is single-writer per run (sharded by `run_id`) for lock-free merge; reads are served from immutable snapshots.
- Exporters/subscribers are decoupled via the delta bus with per-consumer bounded buffers and a configurable drop-or-block policy; a slow neo4j sink must not stall ingestion.
- Targets (non-binding): sustain ≥ a few thousand observations/sec on a laptop; p99 ingest→delta latency in the low tens of ms.

## 14. Extensibility

- **Adapters** and **Exporters** are interfaces; third parties add sources/sinks without touching core.
- **`Node.annotations`** is a reserved, Catacomb-ignored map — the bridge for downstream layers (e.g. a step-level eval system computing MC-value/advantage, or any external scorer) to attach metadata keyed to canonical nodes.
- **OpenInference/OTel conformance** at boundaries keeps the graph portable to external backends (e.g. Arize Phoenix) alongside Catacomb's own store.
- Runtime-neutral core leaves room for non–Claude-Code agent runtimes later.

## 15. Determinism & Reproducibility Metadata

Catacomb does not control sampling (Claude Code/Agent SDK expose no temperature/seed today). To make runs comparable for downstream analysis, each `Run` records: pinned `model_id` (dated snapshot, not a moving alias), and version hashes of prompts / skills / subagent definitions / Catacomb config, plus the Claude Code/SDK version (for #53954 and span-schema gating). Catacomb surfaces this metadata; it does not attempt bit-exact replay.

## 16. Testing Strategy

- **Fixture replay:** recorded corpora of hook/OTLP/stream-json/JSONL observations per scenario → run through the reducer → assert against **golden graphs** (nodes/edges/fields). Fully deterministic, no live Claude Code.
- **Reduction invariants:** property tests that the canonical graph is independent of observation arrival order (commutativity) and that rebuild-from-log == materialized state.
- **Reconciliation tests:** same action delivered by 2–4 sources collapses to one node with correct per-field precedence and provenance.
- **Adapter contract tests** + **exporter round-trip** (export → reload → graph-equality) for jsonl/neo4j/postgres.
- **Soak/backpressure** tests for high fan-out and slow sinks.

## 17. Risks & Caveats

- **OTel traces are beta:** span names/attributes may change between releases. Mitigation: a versioned span-attribute map; the graph degrades gracefully to hooks+JSONL if OTel shapes are unknown.
- **Agent SDK streaming gap (#53954):** on the SDK streaming path only `llm_request` spans fire. Mitigation: hooks backbone + JSONL subagent-tree reconstruction make the graph whole without relying on OTel nesting; verify per CLI/SDK version.
- **Subagent-span nesting is version-dependent** (~v2.1.145+). Mitigation: detect version; prefer JSONL/`parent_tool_use_id` for the tree when in doubt.
- **Hook event taxonomy is version-dependent.** Mitigation: isolate event-name knowledge behind the versioned parser; the §3 set is the known-stable core, with unknown events recorded generically rather than dropped.
- **Transcript threading fields are largely undocumented and version-fragile** (§5.8, ADR-0009): `parentUuid`/`leafUuid`/`promptId`/`interruptedMessageId`/`isSidechain`/`agentId` are on-disk-only (the SDK hides them), the subagent→parent join leans on the community-observed meta `toolUseId` (a documented key is still an open request), and layouts differ across versions (`Task`↔`Agent`, inline↔separate sidechains). Mitigation: read the files (not the SDK surface) for threading; resolve layout/field duality behind the versioned parser; keep both regeneration branches and mark (never delete) `superseded` nodes; treat absent fields as "unknown," not "none."
- **stream-json envelope is undocumented** (#24612/#24596). Mitigation: isolate parsing behind a single schema module; treat stream-json as a secondary structural signal, not a sole source.
- **Privacy:** payloads may contain secrets. Mitigation: redaction defaults on; refs-only mode; size caps; uniform application to store and exporters.

## 18. Delivery Milestones

The full v1 is large; build it as plan-able increments, each independently useful:

- **M0 — Core + hooks + jsonl.** `catacombcore` (model, observation log, reducer, SQLite store) + hook adapter + `install-hooks` + jsonl exporter + `replay`. M0 already ingests **two** sources (hooks + JSONL) keyed by the shared `tool_use_id`, so it performs canonical-entity merge with per-field precedence + provenance (ADR-0003) across those two — reconciliation is foundational here, not deferred. (The first sub-plan, M0.1, is offline core + the JSONL source only; the hook source and its merge land in M0.2.)
- **M1 — OTel enrichment + full precedence + CDC.** OTLP receiver + the full four-source per-field precedence table + CDC bus + **OTLP/OpenInference passthrough exporter** (free external visualization while building later milestones).
- **M2 — stream-json + JSONL tailer.** Full four-source ingestion; subagent-tree backfill; markers.
- **M3 — Realtime surfaces.** WS/SSE + gRPC stream.
- **M4 — neo4j + postgres exporters.** Materialized upsert + CDC + snapshot.
- **M5 — Web UI.** Four views over the live feed.

## 19. Resolved Decisions (v1)

- **Multi-session run-id:** env `CATACOMB_RUN_ID` (primary, inherited by child sessions) → fallback `session_id`; `run --run-id` is sugar. Marker-driven grouping deferred.
- **Default durable store:** SQLite (OLTP write path, zero-config recovery); DuckDB optional behind the `Store` interface; heavy analytics via export.
- **OTLP/OpenInference passthrough exporter:** included in v1; lands in M1 for free external visualization.
- **`tool_call` identity fallback:** conservative — provisional `identity=heuristic` node, merged only on a strong key; a rare duplicate is preferred over a wrong merge.

Rationale for each is captured in `docs/adr/`.
