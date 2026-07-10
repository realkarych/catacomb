# ADR-0005: Run model — persistent forest, session_id anchor, env wrapper run-id

- **Status:** Accepted
- **Date:** 2026-06-20
- **Deciders:** @realkarych
- **Related:** spec §5.4; ADR-0002, ADR-0006, ADR-0011, ADR-0012, amended by [ADR-0026](0026-form-factor-pivot-offline-eval-gate.md)
- **Amended by:** ADR-0011 (`run_id` is a non-identifying grouping label; node identity is the per-session `execution_id`) and ADR-0012 (run lifecycle: `session_ended`/`run_ended`/idle-reaper). The "`run_id` is the subgraph key" framing below is superseded by ADR-0011.

## Context

The daemon serves a "service alongside" role: it should observe many runs over time, let consumers compare and aggregate across them, and join the four signal sources for a given run. Some logical pipelines also span **multiple** Claude Code sessions (separate invocations or restarts) that must be grouped.

## Decision

- **Persistent forest of runs:** the daemon holds many runs; each is a connected subgraph identified by `run_id`; cross-run queries (compare, aggregate) are supported.
- **Cross-source anchor:** `run_id := session_id`, because `session_id` appears in all four sources (hook payload/transcript, OTel attributes, stream-json `system.init`, JSONL filename) and is therefore the natural join key.
- **Multi-session grouping:** a **wrapper run id** groups N sessions into one logical run. Primary mechanism: env **`CATACOMB_RUN_ID`** set by the orchestrator and inherited by every child session (SDK and CLI); `catacomb run --run-id <id>` is sugar that sets it for the wrapped process; absent the env, fall back to `session_id`. Marker-driven grouping is deferred.

## Alternatives considered

- **One active run at a time** (finalize → export → reset) — simpler memory/state but no forest, no cross-run comparison. Rejected.
- **Per-session only, no cross-run** — loses the comparison/aggregation use case the forest enables. Rejected.
- **Marker-driven run grouping as primary** — flexible but requires in-band markers on every path; deferred to a later milestone behind the simpler env mechanism.

## Consequences

- **+** Cross-run comparison and aggregation (a prerequisite for any downstream analysis).
- **+** Robust source join via a key present everywhere.
- **−** Multi-session grouping requires the orchestrator to propagate an env var.
- **−** A growing forest needs retention/eviction controls (config; see ADR-0006).
