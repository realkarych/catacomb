# M1a — OTLP/HTTP receiver + `ingest/otel` adapter Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Accept Claude Code's native OpenTelemetry as a second live ingress over OTLP/HTTP, map spans to observations that merge into the existing graph by canonical id, and wire `catacomb env` so a real session can point at the daemon.

**Architecture:** A new pure-function `ingest/otel.Parse` (mirroring `ingest/hook.Parse`) converts an OTLP `ExportTraceServiceRequest` into `[]model.Observation{Source: SourceOTel}` using EXISTING reducer kinds. A `POST /v1/traces` route on the existing daemon HTTP mux (behind `authed`, under `recover`/quarantine) calls `Daemon.IngestOTLP`, which routes through the existing `applyAndPersist` path. **No reducer change** — cross-source merge happens through the existing canonical-id functions (`ToolCallID`, `AssistantTurnID`, `SubagentID`); the OTel-specific enrichment/precedence/#53954-edges/cascade are M1b.

**Tech Stack:** Go 1.26, `go.opentelemetry.io/proto/otlp` (added in commit `1a750a1`, indirect until imported here), pure-Go, `testify`.

## Global Constraints

- Go 1.26 pure-Go; **NO comments** in Go except directives (`internal/codepolicy`).
- **100% line coverage** under `-race` (`make cover`); `golangci-lint v2` clean (gofumpt, goimports local-prefix `github.com/realkarych/catacomb`, govet shadow, unparam, errcheck, bodyclose); **never `go mod tidy`** (deps already added — `go get` only if something is missing).
- Daemon ingress under `d.mu` + per-request `recover()` → quarantine (ADR-0019); `/v1/traces` behind the existing bearer-token `authed` middleware. `nowFn` for time. Cross-platform.
- **M1a/M1b boundary (binding):** M1a does NOT modify `reduce/`. `ingest/otel` emits observations with the kinds the existing reducer already handles. Adding the `model.Edge.Rev` FIELD is M1a; POPULATING it (and all precedence/cascade/#53954/Rev logic) is M1b.

## Interfaces consumed (existing)

- `model.Observation{ObsID, RunID, ExecutionID, Source, Kind, Correlation, Attrs, Payload, EventTime, ObservedAt, Seq}`; `model.Correlation` already has `SessionID, ToolUseID, SpanID, ParentSpanID, AgentID, MessageID`; `model.SourceOTel`; `model.Payload`; `model.Edge`.
- `ingest/hook.Parse(hookType string, payload []byte, executionID string, nextSeq func() uint64) ([]model.Observation, error)` — the adapter pattern to mirror (ULID obs_id, `nowFn`, returns observations).
- The reducer kinds the existing `reduce.Apply` switch handles: `session_start`, `session_end`, `user_prompt`, `assistant_turn` (→ node + `applyTokens`), `assistant_tool_use`/`tool_result` (→ `applyTool`: merges by `ToolCallID(exec, tool_use_id)`, sets name/status/payload/parent), `subagent_stop` (→ `applySubagent` by `AgentID`), `marker`, `run_ended`, `stop`.
- `daemon.Daemon` with `Ingest`/`ingestLocked`/`applyAndPersist`/`sessionIDOf`/`next`/`execBySession`; `Handler(token)` (mux: `POST /hook/{type}` via `authed`, `GET /healthz`, `GET /metrics`); the `recover()`+`quarantine` pattern in `Ingest`.
- `daemon.Discovery` struct + `WriteDiscovery`/`ReadDiscovery`; the `catacomb` cobra root (`cmd/catacomb`), existing verbs (version/replay/hook/install-hooks/daemon).
- OTLP proto (imported here for the first time): `collectorv1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"` (`ExportTraceServiceRequest`), `tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"` (`ResourceSpans`, `ScopeSpans`, `Span`), `commonv1 "go.opentelemetry.io/proto/otlp/common/v1"` (`KeyValue`, `AnyValue`). The implementer must read the exact field/type names from the vendored package before writing code.

---

## Task 1: `ingest/otel.Parse` — span → observation mapping

**Files:** Create `ingest/otel/otel.go`, `ingest/otel/otel_test.go`.

**Interfaces:** Produces `otel.Parse(req *collectorv1.ExportTraceServiceRequest, executionID string, nextSeq func() uint64) ([]model.Observation, error)` — pure, mirrors `hook.Parse`. Each `Span` → one `model.Observation{Source: model.SourceOTel}`.

**Mapping (per spec §4 — this is the authority for attribute names):**

- Resource attr `session.id`/`session_id`/`gen_ai.conversation.id` → `Correlation.SessionID` + `Observation.RunID` (applied to every span under the `ResourceSpans`).
- `Span.SpanId`/`ParentSpanId` (hex-encode) → `Correlation.SpanID`/`ParentSpanID`. Span start/end nanos → `EventTime`/(t_end attr).
- **Kind selection (emit EXISTING reducer kinds):**
  - interaction / `claude_code.llm_request` / `chat {model}` / OpenInference `LLM` → kind **`assistant_turn`**; `message.id` attr → `Correlation.MessageID`; token attrs → `Attrs["tokens_in"]`/`["tokens_out"]` (the reducer's `applyTokens` reads `attrs`), model → `Attrs["model"]`.
  - tool / `claude_code.tool` / `execute_tool {name}` / OpenInference `TOOL` → kind **`assistant_tool_use`**; `tool_use_id`/`gen_ai.tool.call.id` → `Correlation.ToolUseID` (the cross-source linchpin); `tool_name`/`gen_ai.tool.name` → `Attrs["name"]`; status from span status → `Attrs["status"]`; `mcp_*` attrs → `Attrs` (the reducer routes MCP via the `mcp__` name prefix today — keep the name in `Attrs["name"]`).
  - agent / `invoke_agent {name}` / OpenInference `AGENT` → kind **`subagent_stop`**; agent id → `Correlation.AgentID`; `subagent_type` → `Attrs["subagent_type"]`.
  - `claude_code.hook` / OpenInference `CHAIN` → kind **`marker`** with `Attrs["hook_event"]`.
  - unknown span name with no recognized attrs → **skip** (return no observation for that span; `attrs.identity` heuristic handling is deferred — do NOT emit a node that pollutes the graph).
- `nextSeq()` per emitted observation; `ObsID = ulid.Make()`; `ObservedAt = nowFn().UTC()` (add a package `var nowFn = time.Now` seam, matching `ingest/hook`).

**Tests (construct `ExportTraceServiceRequest` Go structs directly — proto, not JSON fixtures):**

- [ ] Write failing tests in `ingest/otel/otel_test.go`: build requests with (a) an LLM span (assert kind `assistant_turn`, tokens in attrs, message id), (b) a tool span carrying `tool_use_id` (assert kind `assistant_tool_use`, `Correlation.ToolUseID` set, name/status in attrs), (c) an agent span (kind `subagent_stop`, AgentID), (d) a hook span (kind `marker`), (e) an unknown span (no observation emitted), (f) resource `session.id` propagated to `RunID`/`SessionID` across multiple spans, (g) empty request → empty slice. Cover every attribute-name fallback branch (GenAI vs Claude-Code vs OpenInference) the implementation adds — only add a fallback branch if a test exercises it (YAGNI; do not add unreached attribute lookups).
- [ ] Run → FAIL (`ingest/otel` undefined).
- [ ] Implement `Parse` (read the OTLP proto package for exact `KeyValue`/`AnyValue` accessors; write an attr-lookup helper returning string/int from `[]*commonv1.KeyValue`).
- [ ] Run → PASS; `make cover` 100% for `ingest/otel`; `make lint` 0.
- [ ] Commit: `feat(ingest): OTLP span -> observation adapter (ingest/otel)`.

---

## Task 2: OTLP/HTTP receiver + `Daemon.IngestOTLP`

**Files:** Modify `daemon/daemon.go` (`IngestOTLP`), `daemon/server.go` (`POST /v1/traces`); Test `daemon/daemon_test.go`, `daemon/server_test.go`.

**Interfaces:** Produces `Daemon.IngestOTLP(req *collectorv1.ExportTraceServiceRequest) error` and the `POST /v1/traces` route.

- [ ] **Failing tests:**
  - `daemon/daemon_test.go`: `TestIngestOTLPMergesByToolUseID` — first `d.Ingest("PreToolUse", {session_id, tool_name, tool_use_id})` then `d.IngestOTLP(req)` with a tool span carrying the same `tool_use_id`; assert exactly ONE `tool_call` node for that `ToolCallID`, and `len(node.Sources) == 2` (one hook SourceRef, one otel SourceRef) — proving cross-source merge via the existing reducer. Also `TestIngestOTLPNewSession` (an OTLP request for an unseen session_id mints an execID + applies). `TestIngestOTLPParseError`/`TestIngestOTLPPanic` → quarantined, fail-open (reuse the `appendErrStore`/`applyFn` seams + a malformed/empty request path).
  - `daemon/server_test.go`: `TestOTLPHTTPEndpoint` — `proto.Marshal` an `ExportTraceServiceRequest`, `POST /v1/traces` with `Authorization: Bearer <token>` and `Content-Type: application/x-protobuf`; assert 200 + an `ExportTraceServiceResponse` body. `TestOTLPHTTPUnauthorized` — no token → 401 (behind `authed`). `TestOTLPHTTPBadBody` — non-proto body → 400.
- [ ] Run → FAIL.
- [ ] **Implement `IngestOTLP`** in `daemon/daemon.go`, mirroring `Ingest`'s recover/quarantine wrapper: extract `session_id` from the request's first resource attrs (a small helper `sessionIDOfOTLP(req)`); resolve `execID` via `execBySession` (mint if new, with lazy-reload parity to `ingestLocked`); call `ingest/otel.Parse(req, execID, d.next)`; loop `applyAndPersist` + set `lastSeen`. On panic/parse/persist error → `quarantine` (raw = `proto.Marshal(req)` best-effort or a marker) + return nil (fail-open). Run under `d.mu`.
- [ ] **Add the route** in `daemon/server.go` `Handler`, behind `authed`: read the body (`io.ReadAll`), `proto.Unmarshal` into `*collectorv1.ExportTraceServiceRequest` (400 on error), call `d.IngestOTLP(req)`, write a marshaled empty `ExportTraceServiceResponse` (200). Add `google.golang.org/protobuf/proto` + the otlp collector import.
- [ ] Run → PASS; `make cover` 100%; `make lint` 0; `make build`; `GOOS=windows go build ./...`.
- [ ] Commit: `feat(daemon): OTLP/HTTP receiver + IngestOTLP (fail-open, token-gated)`.

---

## Task 3: `catacomb env` verb + `model.Edge.Rev`

**Files:** Create `cmd/catacomb/env.go`, `cmd/catacomb/env_test.go`; Modify `cmd/catacomb/root.go` (register), `model/model.go` (`Edge.Rev`).

**Interfaces:** Produces `newEnvCmd()`; `model.Edge.Rev uint64`.

- [ ] **Failing tests** (`cmd/catacomb/env_test.go`): `TestEnvCmd` — write a discovery file (`Discovery{Addr, Token}`), run `catacomb env --discovery <path>`, capture stdout, assert it contains `CLAUDE_CODE_ENABLE_TELEMETRY=1`, `OTEL_TRACES_EXPORTER=otlp`, `OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf`, `OTEL_EXPORTER_OTLP_ENDPOINT=http://<addr>` (the hook HTTP listener addr — OTLP/HTTP shares it; the `/v1/traces` path is implied by the OTLP SDK), and `OTEL_EXPORTER_OTLP_HEADERS=authorization=Bearer <token>`. `TestEnvCmdMissingDiscovery` → error. (Model edits are covered by existing reduce/store tests recompiling; add a trivial `model` assertion only if coverage needs it — `Edge.Rev` is a plain field, populated in M1b.)
- [ ] Run → FAIL.
- [ ] **Implement** `newEnvCmd()` (a `--discovery` flag defaulting to `DiscoveryPath()`, reads `ReadDiscovery`, prints the five env lines to `cmd.OutOrStdout()`); register in `root.go`. Add `Rev uint64` `json:"rev,omitempty"` to `model.Edge`.
- [ ] Run → PASS; full gate (`make cover` 100%, `make lint` 0, `make build`, windows).
- [ ] Commit: `feat(cmd): catacomb env verb (OTLP/HTTP) + model.Edge.Rev field`.

---

## Deferred (documented)

- **OTLP/gRPC receiver** (2nd loopback listener + `TraceServiceServer` + bearer interceptor + supervisor + `otlp_grpc_addr` in discovery) → a focused follow-up increment **M1a-grpc** after M1a (HTTP suffices for Claude Code via `OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf`). `catacomb env` emits the HTTP protocol for now.
- **Precedence / enrichment / #53954 edges / cascade / superseded / Edge.Rev population** → M1b.
- **CDC bus + passthrough exporter** → M1c.
- **catacomb.yaml** config → not adopted; flags + discovery file only.

## Self-Review

- **Spec coverage:** §3 receiver (HTTP slice), §4 mapping (Task 1 emits existing kinds; full enrichment deferred to M1b per the boundary), §8 M1a deliverables (ingest/otel, HTTP receiver, IngestOTLP, catacomb env, Edge.Rev) — gRPC explicitly deferred. ✓
- **M1a/M1b boundary:** no `reduce/` edits in this plan; merge proven via `node.Sources` length, not new reducer logic. `Edge.Rev` field added, not populated. ✓
- **Type consistency:** `otel.Parse` signature mirrors `hook.Parse`; `IngestOTLP` mirrors `Ingest`; `Correlation` fields already exist; OTLP imports named consistently (`collectorv1`/`tracev1`/`commonv1`). The implementer reads the proto package for exact accessors before coding.
- **Coverage risk:** `ingest/otel` attribute fallbacks — add a branch only if a test drives it (YAGNI), keeping 100% genuine.
