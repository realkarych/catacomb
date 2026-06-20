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
- **No multi-tenant SaaS, accounts, or RBAC.** Single-operator local/self-hosted tool. (This is *not* "no security": the daemon still has a local trust boundary — unix-socket/bearer-token ingress, no unauthenticated exfiltration — per ADR-0013.)
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
  obs_id        string         ULID, globally unique, insert-only (ADR-0010)
  execution_id  string         per-session ULID; identity prefix for node ids (ADR-0011)
  run_id        string         grouping label (wrapper or session_id), non-identifying (ADR-0011)
  source        enum{hook, otel, stream_json, jsonl}
  kind          string         source-native event type (e.g. "PostToolUse", "claude_code.tool")
  correlation   {              any subset present
    session_id, tool_use_id, parent_tool_use_id,
    span_id, parent_span_id, agent_id, parent_agent_id,
    message_id, uuid
  }
  attrs         map[string]any source-native fields
  payload       Payload?       tool input/output, prompt text (subject to redaction; §11)
  event_time    time?          source event time (UTC); drives t_start/t_end (ADR-0018)
  observed_at   time           daemon ingest time (UTC); metadata, not the order key
  seq           uint64         persisted monotonic receive order; the merge tiebreak (ADR-0010, ADR-0018)
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
  status        enum{pending, running, ok, error, blocked, cancelled, superseded, unknown, abandoned}
  t_start, t_end time?
  duration_ms   int?
  tokens_in, tokens_out int?
  cost_usd      float?
  attrs         map[string]any model, cwd, span_id, transcript_path, mcp_server_name, mcp_tool_name, permission_decision, ...
  payload       Payload?       redacted per policy
  payload_hash  string?        sha256 of pre-redaction payload (dedup/integrity)
  sources       []SourceRef    {source, obs_id, observed_at} that contributed (provenance)
  step_key      string?        run-invariant best-effort identity for cross-run joins (ADR-0016)
  rev           uint64         per-node monotonic revision (= originating seq) for export upserts (ADR-0015)
  tier          enum{core, detail}  core always emitted; detail (hook_event) only when graph.granularity = rich (§12). assistant_turn is core — load-bearing spine (ADR-0021)
  annotations   map[string]any reserved, per-writer namespaced; Catacomb does not interpret them but PRESERVES/re-keys across merge/supersede/rebuild (ADR-0016)
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

**Two keys, separated (ADR-0011).** Each session attach mints an **`execution_id`** (ULID) — the physical execution instance and the **identity prefix** for canonical node ids (§5.5). **`run_id`** is a **non-identifying grouping label**: the wrapper from env **`CATACOMB_RUN_ID`** (inherited by every child session, SDK and CLI; `catacomb run --run-id <id>` is sugar), or `session_id` by default. A `Run` is the set of executions sharing a `run_id`; a replayed transcript gets a fresh `execution_id` (never colliding with its live origin), and a reused `--run-id` groups for comparison **without merging nodes**. Run-level reproducibility metadata (§15): pinned `model_id`, version hashes of prompts / skills / subagent definitions / Catacomb config.

**Lifecycle (ADR-0012).** A run/execution is **open** on first observation with no intrinsic end. `session_ended` fires on `SessionEnd` + transcript EOF/quiescence (exporters finalize a session here). `run_ended` fires only on an explicit wrapper exit / end-marker, or via the **idle-reaper** (a run with no observation for a quiescence window → status `abandoned` + synthetic `run_ended{reason:timeout}`, releasing its reducer shard). Eviction gates on liveness (never evict a not-yet-ended run, an active wrapper sibling, or a run behind an exporter watermark).

### 5.5 Canonical id derivation (so all sources collapse to one node)

The linchpin is `tool_use_id`, which is shared across hooks, JSONL, and stream-json; OTel tool spans carry it as an attribute when present. **All canonical ids are prefixed by the per-session `execution_id` (ADR-0011), never the grouping `run_id`**, so session-scoped keys (`tool_use_id`/`message_id`/`agentId`) cannot collide across a wrapper run or a reused `--run-id`.

| Node type | Canonical id | Fallback |
|---|---|---|
| `session` | `execution_id` | — |
| `user_prompt` | `execution_id : uuid` | `execution_id : t_start` |
| `assistant_turn` | `execution_id : message.id` (the `msg_*`, distinct from JSONL `uuid`; ADR-0014) | `execution_id : span_id` |
| `tool_call` / `mcp_call` | `execution_id : tool_use_id` | `execution_id : span_id` (OTel-only); last-resort heuristic = `execution_id : agent_id : name : t_start±ε` |
| `subagent` | `execution_id : agentId` (the `agent-<agentId>.jsonl` file id) | spawning tool_use id from `agent-<agentId>.meta.json` `toolUseId` → parent `Agent`/`Task` `tool_call`; live: hook `agent_id` / SDK `parent_tool_use_id`; old layout: inline `isSidechain:true` threaded by `parentUuid` |
| `hook_event` | `execution_id : obs_id` | — |
| `marker` | `execution_id : marker_seq` | — |

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
- **Cascade & orphans (ADR-0012, ADR-0014).** Cancellation/supersede **cascades** to descendant `tool_call`s and `subagent` subtrees across files (the `parent_child` closure), except descendants that already hold a genuine terminal. A `tool_call` whose terminal observation **never arrives and was not interrupted** (crashed subagent, dropped MCP, lost `PostToolUse`) is closed `unknown` (EOF / inactivity TTL / run-end), distinct from `cancelled`; an idle run is `abandoned`. A genuine late terminal (`ok`/`error`) always supersedes an inferred status (order-independent).
- **Compaction threading.** A `compact_boundary` severs and re-stitches the `parentUuid` tree; the upward walk continues across it via `logicalParentUuid` / `compactMetadata.preservedSegment.headUuid`, so post-compaction turns resolve to their true pre-compaction ancestor prompt instead of orphaning on the synthetic `isCompactSummary` record.
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
- **Per-field precedence** (higher wins; ties broken by `seq`, ADR-0018 — never wall-clock):

  | Field group | Precedence (high → low) |
  |---|---|
  | Structure (parent/child, nesting) | **conditional (ADR-0014):** JSONL tree outranks OTel when the #53954 profile is detected; otherwise OTel span tree → JSONL tree → stream-json `parent_tool_use_id` → hook heuristics |
  | Timing (`t_start`/`t_end`/`duration`) | OTel spans → hooks (pre/post) → JSONL → stream-json |
  | Cost / tokens | OTel metrics/usage → stream-json `result.usage` → JSONL |
  | Payload (input/output) | hooks (`tool_input`/`tool_response`) / JSONL (full) → stream-json deltas |
  | MCP attrs | OTel `mcp_*` → hooks mcp fields → name-pattern parse |

- **Status lattice (ADR-0014):** a genuine terminal (`ok`/`error`) always beats an inferred `cancelled`/`unknown`; the inferred ones are provisional and superseded by any later genuine terminal, so final status is order-independent. Interruption and supersede **cascade transitively over the `parent_child` closure** (across subagent files), except descendants that already hold a genuine terminal.
- **Provenance:** every contributing observation is recorded in `Node.sources`, so any field is traceable to its origin and the merge is auditable.
- **CDC:** each create/field-change/edge-add emits a typed **GraphDelta** (`node_upsert`, `edge_upsert`, `node_status`, `node_merge`, `run_started`, `run_ended`) carrying a per-node `rev` (= originating `seq`) to the fan-out bus; subscribers and streaming exporters consume the same stream and apply **rev-guarded conditional upserts** (ADR-0015).

## 8. Persistence

- **Durable layer:** embedded DB, **SQLite by default** (ubiquitous, zero-config), **DuckDB optional** (columnar, better for cross-run analytical queries). Behind a `Store` interface.
- **Tables:** `observations` (append-only log), `nodes`, `edges`, `runs`, `markers`. Nodes/edges hold the materialized graph; observations allow full rebuild.
- **In-memory graph** serves realtime reads/subscriptions; the durable store is written through and is the recovery source.
- **Atomicity & recovery (ADR-0010):** appending an observation and applying its node/edge upserts happen in **one transaction**, and a durable committed **watermark = max(seq)** is persisted. On boot, materialized tables are a cache valid only to the watermark; replay `observations` with `seq` > watermark and re-reduce. Durability mode is WAL + `synchronous=NORMAL`. A **single-writer lock** (PID/OS lock on the DB file) prevents two daemons corrupting one store; one-shot CLI verbs (`replay`/`export`) open read-only.
- **Format versioning (ADR-0017):** the store stamps `schema_version` + `reducer_version` + per-observation `body_schema_version`; on boot the daemon refuses-or-migrates, and a `reducer_version` bump rebuilds the graph from the log (rebuild determinism holds **within** a reducer version). The versioned parser (§17) is replay-aware, not just live-ingest-aware.
- **Retention (gated on liveness, ADR-0012):** append-only by default; optional per-run TTL / max-runs eviction, but never evicts a not-yet-`run_ended` run, an active wrapper sibling, or a run behind any exporter/subscriber watermark. No enforcement in the hot path.

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
- **otlp (OpenInference passthrough)** — re-emits the graph as OpenInference spans to any OTLP endpoint (Arize Phoenix, Tempo, Honeycomb, …) via the §5.7 export mapper. Because spans are immutable, OTLP emits a span only on **terminal status + a settle window** (snapshot-at-finalization, ADR-0015) — explicitly carved out of the upsert-everywhere semantics. Near-zero added code; gives free trajectory visualization in external backends.
- **neo4j** — nodes as labeled nodes (`:Session`, `:ToolCall`, `:Subagent`, `:McpCall`, `:Marker`, …), edges as relationships (`PARENT_OF`, `NEXT`, `IN_PHASE`). `MERGE` on canonical id for idempotent upsert; CDC applied via Bolt. Snapshot = batched `MERGE`.
- **postgres** — `nodes` / `edges` tables keyed by canonical id (`INSERT … ON CONFLICT DO UPDATE`), JSONB for `attrs`/`payload`/`annotations`; optional adjacency views and `pg_notify` for downstream CDC. Snapshot = upsert in a transaction.

Both streaming (continuous CDC as the graph grows) and snapshot (`catacomb export --to jsonl|otlp|neo4j|postgres [--run <id>]`) are supported for every target.

**Correctness under failure (ADR-0015).** Sink upserts are **conditional on `rev`** (pg `WHERE excluded.rev > nodes.rev`; neo4j `MERGE … SET` rev-guarded), so reordered/stale deltas are ignored. The bus is non-durable and FIFO-per-node; a `drop` policy never loses state — it marks the node dirty and **re-emits its full current state** once the buffer drains (coalesce-to-latest). On (re)connect or restart an exporter runs `Snapshot` to current state, then attaches to live CDC, resuming from a per-exporter `seq` cursor (full snapshot if the cursor is behind retention). Id changes propagate via `node_merge`; provisional heuristic nodes are buffered locally and not exported until they stabilize.

## 11. Payload Handling & Privacy

Configurable; default **store payload + hash + redaction** (hardened per ADR-0020).

- **Redaction surface is the whole node, not just `payload`** (ADR-0020): tool inputs/outputs, prompt text, **and** `name`, `subagent_type`, marker `state_ref`, and a whitelisted set of `attrs` (`cwd`, `transcript_path`, command-like fields, `description`, `mcp_*`) are scrubbed by the same rules.
- **Rules = default value-scanning regexes + key-path globs.** A non-empty default regex pack scans **values regardless of key** (credential URIs, `AKIA…`/`ghp_…`/`sk-…`/`xox…`, PEM, JWTs, high-entropy runs) over free-text fields (`command`, `code`, `content`) — so a secret inside `Bash.command` is caught — plus key-path globs (`*.api_key`, `authorization`). Redacted spans become `‹redacted:reason›`.
- **Redact first, then size-cap** (config; default e.g. 256 KiB; overflow → ref/hash), so truncation can never cut off the span a rule would match. Applied uniformly to persistence **and** every exporter, per-sink.
- **Hashing is post-redaction** (`sha256` of the redacted payload, for dedup/integrity); any pre-redaction integrity hash is local-only and HMAC'd with a per-deployment key, never exported (so a low-entropy secret can't be brute-forced off its hash).
- Modes (settable **per exporter**): `full+hash+redact` (default) · `refs-only` (store only `transcript_path` refs + hashes — note the ref points at the **unredacted** on-disk file) · `all` (no redaction; logged warning). Binary/non-UTF-8 payloads are stored as `‹binary:len,sha256›` unless `all`.

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
- Reducer is single-writer per **`execution_id`** (ADR-0011) for lock-free merge; parallel sessions run on independent shards; reads are served from immutable snapshots.
- Exporters/subscribers are decoupled via the delta bus with per-consumer bounded buffers; `drop` is **coalesce-to-latest, never lossy** (ADR-0015) — a slow neo4j sink must not stall ingestion and must not silently diverge.
- **Fault isolation (ADR-0019):** every adapter and reducer shard runs under a supervised goroutine with a `recover()` boundary; a panic on a malformed beta/undocumented input becomes a quarantined poison observation + adapter restart with backoff, **never a daemon crash**. A health/metrics surface exposes queue depth, hook/exporter drop counts, store errors, open-run count, and per-adapter liveness; affected runs are flagged `lossy`. Dogfooding is loop-broken (the tailer excludes Catacomb's own session; the OTLP exporter refuses its own endpoint).
- Targets (non-binding): sustain ≥ a few thousand observations/sec on a laptop; p99 ingest→delta latency in the low tens of ms.

## 14. Extensibility

- **Adapters** and **Exporters** are interfaces; third parties add sources/sinks without touching core.
- **`Node.annotations`** is the bridge for downstream layers (e.g. a step-level eval system computing MC-value/advantage). Catacomb does not interpret it but is **not** indifferent to it (ADR-0016): annotations are **per-writer namespaced** (`annotations.<owner>.<key>`), live in their own recovery-aware store table, and are **preserved and re-keyed** across the lifecycle events Catacomb controls (heuristic→canonical merge, supersede/cancel, rebuild-from-log).
- **Cross-run identity (ADR-0016):** because canonical ids are execution-scoped and per-run-random, nodes also carry a best-effort **`step_key`** (run-invariant: structural path + tool name + normalized input hash) and markers a **`phase_key`** (= marker name). The eval layer keys its data by `step_key`/`phase_key` to align "the same step/phase" across repeated runs — the join key MC-value/advantage requires.
- **OpenInference/OTel conformance** at boundaries keeps the graph portable to external backends (e.g. Arize Phoenix) alongside Catacomb's own store.
- Runtime-neutral core leaves room for non–Claude-Code agent runtimes later.

## 15. Determinism & Reproducibility Metadata

Catacomb does not control sampling (Claude Code/Agent SDK expose no temperature/seed today). To make runs comparable for downstream analysis, each `Run` records: pinned `model_id` (dated snapshot, not a moving alias), and version hashes of prompts / skills / subagent definitions / Catacomb config, plus the Claude Code/SDK version (for #53954 and span-schema gating). Catacomb surfaces this metadata; it does not attempt bit-exact replay.

**Time model (ADR-0018):** every observation keeps both `event_time` (source, UTC — drives `t_start`/`t_end`) and `observed_at` (daemon ingest, UTC — metadata only). The authoritative order and merge tiebreak is the persisted monotonic **`seq`**, never wall-clock, so NTP steps / suspend / cross-source skew cannot reorder the reduction; negative durations (cross-clock skew) are dropped to null. **Catacomb versions its own format** (ADR-0017): `schema_version`/`reducer_version`/`body_schema_version`, distinct from the Claude Code version above.

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
- **Privacy:** payloads may contain secrets, and they hide in values (`Bash.command`) and metadata (`cwd`/`attrs`), not just keyed fields. Mitigation: whole-node redaction with default value-scanning regexes, redact-before-cap, post-redaction hashing, per-sink modes (ADR-0020) — best-effort, not a guarantee.
- **Local trust boundary:** every daemon ingress is unauthenticated by default and loopback is shared by all local processes, enabling forged-observation injection and bulk payload exfiltration. Mitigation: unix-socket `0600` ingress + per-daemon bearer token for any TCP/realtime surface + gated "subscribe all runs" (ADR-0013); residual same-user risk is documented.
- **Daemon resilience:** a panic on a beta/undocumented input could crash all runs; silent drops (fail-open hooks, bounded buffers) could hide data loss. Mitigation: per-adapter `recover()`/supervision + poison-quarantine, a health/metrics surface, and `lossy`-run flagging (ADR-0019).
- **Graph well-formedness:** cross-source parent conflicts and provisional→canonical id rewrites could create cycles/dangling edges, and lean-mode folding could orphan `tool_call`s. Mitigation: enforce `parent_child` as an acyclic forest, cycle/self-loop checks after id rewrite, lean-mode edge contraction, `assistant_turn` reclassified core (ADR-0021).
- **Exporter divergence:** dropped/reordered CDC deltas and id changes could silently desync sinks. Mitigation: `rev`-guarded conditional upserts, FIFO-per-node, coalesce-to-latest `drop`, snapshot-then-resume, `node_merge` (ADR-0015).

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

## 20. Hardening Decisions (ADR-0010 – ADR-0021)

A design interrogation surfaced gaps the first nine ADRs did not cover. Each is decided in its own ADR and woven into the sections above; summarized here as a map:

- **ADR-0010 — Observation identity & durability:** `obs_id` is a real globally-unique ULID, insert-only (no `REPLACE`); one observation applies in one transaction with a committed `seq` watermark; WAL; `seq` persisted. Fixes the system-of-record corruption and crash atomicity (§5.1, §8).
- **ADR-0011 — Canonical-id execution scope:** node ids are prefixed by a per-session `execution_id`, not the grouping `run_id`; reducer shards by `execution_id`. Fixes cross-session/wrapper-reuse collisions (§5.4, §5.5, §13).
- **ADR-0012 — Node finalization & run lifecycle:** `unknown`/`abandoned` terminal statuses, closure triggers, `session_ended` vs `run_ended`, idle-reaper, liveness-gated eviction (§5.2, §5.4, §5.8, §8).
- **ADR-0013 — Daemon security & trust boundary:** unix-socket `0600` ingress + bearer token for TCP/realtime; gated all-runs subscription; documented threat model (§2, §17).
- **ADR-0014 — Conditional precedence & status reconciliation:** structure precedence gated on the #53954 profile; status lattice; transitive cancel/supersede cascade; `assistant_turn` keyed on `message.id` (§7, §5.5, §5.8).
- **ADR-0015 — Exporter correctness under failure:** per-node `rev` + conditional upserts, FIFO-per-node, coalesce-to-latest `drop`, snapshot-then-resume, `node_merge`, OTLP finalize-at-terminal (§7, §10, §13).
- **ADR-0016 — Cross-run identity & annotations contract:** `step_key`/`phase_key` and namespaced, lifecycle-preserved annotations (§5.2, §14).
- **ADR-0017 — Data-format versioning & migration:** `schema_version`/`reducer_version`/`body_schema_version`, refuse-or-migrate, rebuild-on-reducer-bump (§8, §15).
- **ADR-0018 — Time model:** `event_time` vs `observed_at`; `seq` is the order/tiebreak, never wall-clock; UTC; negative-duration rejection (§5.1, §7, §15).
- **ADR-0019 — Operability, fault isolation & self-observation:** per-adapter `recover()`/supervision + poison-quarantine, health/metrics surface, `lossy`-run flagging, dogfooding loop-break (§13, §17).
- **ADR-0020 — Redaction surface & secrets-at-rest:** whole-node redaction, default value-scanning regexes, redact-before-cap, post-redaction hashing, per-sink modes (§11).
- **ADR-0021 — Graph invariants & validation:** `parent_child` acyclic forest, cycle/self-loop checks, lean-mode edge contraction, `assistant_turn` reclassified core (§5.2, §5.3, §16).

These land across milestones (§18): identity/durability/time/versioning (0010/0011/0017/0018) underpin **M0–M1**; precedence/status/lifecycle/invariants (0012/0014/0021) with **M1–M2**; exporter correctness (0015) with **M4**; security/operability (0013/0019) with the daemon in **M0.2+**; redaction (0020) wherever payloads are stored/exported; cross-run identity & annotations (0016) when the eval layer consumes the graph.
