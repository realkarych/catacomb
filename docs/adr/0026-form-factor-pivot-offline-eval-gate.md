# ADR-0026: Form factor pivot — offline eval gate over vendor observability

- **Status:** Accepted
- **Date:** 2026-07-06
- **Deciders:** @realkarych
- **Related:** [2026-07-05 CTO review](../reviews/2026-07-05-competitive-cto-review.md), ADR-0001, ADR-0016, ADR-0017, ADR-0022, ADR-0023; supersession table in Consequences

## Context

Catacomb was built as an observability platform (ADR-0001: daemon sidecar; ADR-0002: four-source capture) with an eval layer on top (ADR-0022/0023: baskets, baselines, statistical regression gate). The 2026-07-05 review established three facts that break that shape:

1. **Capture and display commoditized.** Between Dec 2025 and Jun 2026, Phoenix, LangSmith, Langfuse, Braintrust, and W&B Weave all shipped first-party Claude Code ingestion (hooks/JSONL → traces) plus mature session UIs. Suite-level before/after score comparison, evals, and self-hosting are likewise covered by several of them.
2. **The defensible part is feature-sized.** Nobody in the market does: a checkpoint as a first-class segmentation axis, structural run-to-run diff at a checkpoint, or statistical regression detection with significance handling for a component swap. Everything else catacomb carries — daemon, four parsers, six exporters, two viewers, gRPC/SSE — is purchasable and is platform-sized maintenance on a bus factor of 1.
3. **The offline core already exists.** `loadGraph()` (`cmd/catacomb/replay.go`) builds a graph from a transcript file and already feeds `catacomb diff`; `aggregate.Aggregate([]RunGraph)` is a pure function; store coupling is confined to selector resolution and record persistence in `cmd/catacomb/regress.go`. The dogfood calibration (2026-07-04) proved verdicts ride the phase axis and that JSONL is the structure-authoritative source (ADR-0014 already demotes OTel structure).

## Decision

Catacomb narrows to a single product: **a statistical gate for checkpoint-level regressions in Claude Code agentic flows.** The pipeline is `bench → transcript JSONL → reduce → step/phase keys → aggregate → regress → exit code`. Observability (capture for humans, live views, dashboards) is delegated to an off-the-shelf vendor substrate. Concretely:

### 1. Eval source of truth: transcripts, offline

The gate reads **Claude Code session JSONL transcripts** (plus subagent sub-transcripts) directly. It is a pure function over files: no daemon, no service dependency, runs in CI with nothing but the binary and the transcript files. The existing `ingest/jsonl → reduce` path (today's `loadGraph`) becomes the only ingestion path.

### 2. Vendor substrate for observability; zero code coupling

Humans watch runs in a vendor tool fed by that vendor's own first-party Claude Code plugin. Catacomb neither writes to nor reads from the substrate; the integration is documentation only. Substrate study (verified mid-2026):

| | Phoenix (recommended) | Langfuse (alternative) | Others |
|---|---|---|---|
| Self-host weight | ELv2; single process (pip/docker, SQLite default) | MIT core; compose stack (postgres+clickhouse+redis+minio) | LangSmith/Braintrust enterprise-gated; Weave backend licensed; Helicone proxy in maintenance |
| CC plugin | first-party, 9 hooks incl. SubagentStop → OpenInference, near-realtime | first-party, Stop hook reads session JSONL at session end | SaaS-centric |
| Schema | OpenInference (catacomb has emitted it since PR-1 of the eval-core roadmap) | own observation model | — |

**Recommendation: Phoenix** (lightest local footprint, realtime hooks, familiar schema). Langfuse is the documented MIT alternative. Because the gate never touches the substrate, swapping it is a docs change.

### 3. Component disposition

| | Packages | Outcome |
|---|---|---|
| Keep (core) | `ingest/jsonl`, `reduce`, `stepkey`, `phasekey`, `subgraph`, `diff`, `aggregate`, `regress`, `bench`, `pricing`, `repro`, `mcp`, `model` | ~4.5k LOC |
| Slim | `store` → baselines + regress-records only (migrate discipline kept); `redact` → step-key normalization + copy redaction; `ingest/drift` → JSONL checks + version watchlist only; `cmd` 29 → ~10 commands; `config` → minimal | ~2.5k LOC |
| Delete | `daemon`, `tui`, `webui` (Go embed + ~9.8k TS + e2e), `ingest/{hook,otel,streamjson,tail}`, `cdc`, `export/{otlp,neo4j,postgres,agentevals,evalview,build}`, `gen/` + proto + gRPC, lifecycle commands (`up/down/status/restart/logs/observe/ui/watch/hook/install-hooks/mark/demo/…`) | −13k Go, −12.6k TS/e2e |
| Dependencies out | otel-sdk+otlp, grpc+protobuf, pgx, neo4j-driver, bubbletea+lipgloss | remain: cobra, yaml, ulid, modernc-sqlite |

`export/jsonl` survives as a pure function over a reduced graph — it is the input format of the DeepEval bridge, which stays.

CLI surface after the pivot: `bench`, `regress`, `baseline`, `trends`, `diff`, `subgraph`, `export`, `replay`, `mcp`, `version`.

### 4. Bench without the daemon

- The child still runs with `--output-format stream-json`; bench peeks `session_id` and takes the terminal `result` event's `total_cost_usd` as the authoritative run cost (V-1 F7 rule preserved).
- Transcript resolution: `~/.claude/projects/<munged-cwd>/<session_id>.jsonl` plus `subagents/agent-*.jsonl`, with a bounded retry after child exit.
- `task:<id>` boundary markers become synthetic observations injected at reduce time from the cell's wall clock (replacing daemon `/v1/mark`). In-task checkpoints stay agent-emitted via the `mark` MCP tool observed in the transcript — mechanism unchanged.
- Checkpoint verification inspects the in-process graph (replacing the daemon graph endpoint).
- **Evidence copies:** bench copies each cell's transcripts, redacted on write, into `~/.catacomb/runs/<run_id>/` with a `meta.json` (labels, session id, exit code, cost, basket hash, timestamps). Copies outlive Claude Code's transcript retention; baselines pin directories, not graph-store rows. Step keys are unaffected by redaction: `stepkey` hashes salient input **after** `redact.Redact` already.

### 5. Selector compatibility and annotations

- `regress`/`baseline`/`trends` CLI shapes are preserved. `label:` selectors resolve by scanning runs-dir `meta.json`; `name:` selectors resolve via the slim store.
- The daemon annotation API is replaced by `regress --scores <file.jsonl>`: external scorers (DeepEval bridge among them) emit `owner.key` numeric values keyed by step, applied to in-memory graphs before aggregation. Annotation gates (`--annotation owner.key:higher-better`) are unchanged.

### 6. Version stamps (ADR-0017, narrowed and finally implemented)

Baselines and regress records are stamped with the catacomb version and the step/phase-key scheme version. Comparing across mismatched stamps warns; `--strict` refuses. This closes the review's determinism gap in the only place it still matters — there is no long-lived graph store left to migrate.

### 7. Migration protocol

Staged PRs, each TDD with 100% coverage, subagent-driven, review cycle + green CI before merge:

| PR | Contents | Gate |
|---|---|---|
| PV-1 | Additive spike: offline bench mode, runs-dir evidence copies, offline selector resolution, in-process verification. Deletes nothing | **Parity:** a calibration basket reproduces the dogfood verdicts (degraded variant → `regression` exit 1; A-vs-A → zero false regressions) |
| PV-2 | Slim eval store (baselines/records), version stamps, `--scores` annotations | `regress`/`baseline`/`trends` free of the graph store |
| PV-3 | Deletion I: webui + tui + `observe`/`ui`/`watch` | tag `v0-platform-final` first |
| PV-4 | Deletion II: daemon, `ingest/{hook,otel,streamjson,tail}`, cdc, gRPC/proto, lifecycle commands, config slim | |
| PV-5 | Deletion III: exporters (jsonl stays), CI slim, README/guide repositioning, ADR statuses | DeepEval bridge green on the new path |
| PV-6 | Extended calibration: 2–3 heterogeneous baskets, deliberate regressions of varying magnitude, gate-power measurement | final report |

Deletion waves (PV-3+) begin only after this ADR is merged. `v0-platform-final` keeps the platform installable for archaeology.

## Alternatives considered

- **A: vendor substrate as the data source (read-back).** The plugin captures into Phoenix; catacomb reads spans back over REST/GraphQL and rebuilds the graph. Rejected: the gate acquires a service dependency (including in CI), a coupling to a third-party plugin's span schema (trading CC-format drift for plugin-schema drift, with worse visibility), a fidelity ceiling set by what the plugin captured, and a new span→graph adapter that replaces one parser with another plus a network layer.
- **Status quo (platform).** Rejected: the review shows ~80% of the maintained surface is now purchasable while the defensible moat is three feature-sized rows; carrying the platform on a bus factor of 1 starves the moat.
- **Greenfield rewrite.** Rejected: the offline core, the statistics, and the calibration evidence already exist and are the most-tested code in the repo; the pivot is replumbing around a validated engine, not a rewrite.
- **Langfuse as the primary substrate.** Viable (MIT), documented as the alternative; heavier local footprint and batch-at-session-end capture made Phoenix the default recommendation. The choice is intentionally non-binding.

## Consequences

- **+** The gate becomes hermetic: a binary plus transcript files; CI needs no services; nothing to keep alive on a laptop.
- **+** Maintenance surface drops from ~20.2k to ~7k prod Go LOC and sheds five heavy dependency families; one CC format coupling (JSONL) instead of four.
- **+** Positioning matches the moat; every remaining line serves the differentiator.
- **+** Evidence copies make baselines durable and reproducible independent of Claude Code retention.
- **−** Live observation, the web UI, the TUI, and all realtime surfaces are gone; humans use the vendor substrate.
- **−** Hook-only signals are lost: `blocked` (permission-deny) status, PreCompact/Notification markers, hook session events. Statuses now derive from JSONL (`ok`/`error` via `is_error`) plus synthetic finalization. Acceptable at the phase granularity the gate trusts.
- **−** OTel-authoritative timings are gone; timings come from JSONL timestamps.
- **−** Multi-session run grouping (`CATACOMB_RUN_ID`, ADR-0005) is demoted: a run is a bench cell (one session plus its subagents); cross-session grouping survives only as labels in runs-dir metadata.
- **−** The neo4j/postgres/OTLP/agentevals/evalview export paths and their hedge value are dropped (zero known consumers).
- **Risk:** single-source coupling to the undocumented JSONL format. Mitigated: it was already the structure-authoritative source; drift detection and the version watchlist stay, scoped to that parser.
- **Risk:** in-task checkpoint reliability remains agent behavior (unchanged from ADR-0022's model; bench verification reports misses).

### Supersession map

| ADR | Effect |
|---|---|
| 0001 (daemon form factor) | **Superseded** — the form factor is an offline CLI |
| 0002 (four-source capture) | **Superseded** — single source: transcript JSONL |
| 0003 (per-field precedence) | Amended — cross-source precedence is moot with one source; canonical-entity reduction and provenance stay |
| 0004 (graph model + OTel/OpenInference mappers) | Amended — graph model stays; both mapper directions are deleted |
| 0005 (run model, env run-id) | Amended — run = bench cell; env grouping demoted to labels |
| 0006 (SQLite durable store) | Amended — store shrinks to baselines + regress records; migrate discipline kept |
| 0007 (export CDC/snapshot) | **Superseded** — exporters deleted except pure jsonl |
| 0008/0020/0024 (payload, redaction, secrets at rest) | Amended — no payload storage remains; redaction applies to evidence copies and step-key normalization |
| 0009 (threading/interruption) | Amended — meta-record taxonomy stays for the JSONL parser; hook-based interruption signals moot |
| 0012 (finalization/reaper) | Amended — synthetic finalization at load time; no reaper |
| 0013 (daemon security) | **Superseded** — no daemon, no trust boundary |
| 0014 (conditional precedence + status lattice) | Amended — status lattice stays; structure-precedence conditions moot |
| 0015 (exporter correctness) | **Superseded** — no continuous exporters |
| 0017 (format versioning) | Amended — narrowed to store schema + baseline/record stamps (§6) |
| 0018 (time model) | Amended — `seq` ordering stays; single-writer simplifies it |
| 0019 (operability/fault isolation) | **Superseded** — no long-running process |
| 0025 (drift detection) | Amended — scoped to the JSONL parser + version watchlist |
| 0010/0011/0016/0021/0022/0023 | **Unchanged** — observation identity, id scoping, step/phase keys, invariants, and the regression model are the product |

## Validation criteria

1. Parity: the calibration basket reproduces the dogfood verdicts (degraded variant gates `regression`, exit 1; A-vs-A control yields zero false regressions at defaults).
2. The gate runs in CI with no services (binary + transcript fixtures).
3. 100% coverage holds; prod Go ≤ ~8k LOC; the dependency removals land in `go.mod`.
4. The DeepEval bridge is green against the post-pivot export path.
5. A basket session is visible in Phoenix via the vendor plugin (documented setup, manual smoke).
