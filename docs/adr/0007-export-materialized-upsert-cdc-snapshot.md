# ADR-0007: Export model — materialized graph (upsert + CDC) and snapshot

- **Status:** Superseded by [ADR-0026](0026-form-factor-pivot-offline-eval-gate.md)
- **Date:** 2026-06-20
- **Deciders:** @realkarych
- **Related:** spec §10, §5.7; ADR-0004, ADR-0006, ADR-0012, ADR-0015
- **Amended by:** ADR-0015 — the GraphDelta vocabulary adds `node_merge` and `session_ended`, upserts are `rev`-guarded, and OTLP finalizes on a genuine terminal / lifecycle-close. The 5-variant delta list in the Decision below is extended by spec §7 (the canonical enum).

## Context

Catacomb is a realtime tool whose graph must flow into external stores both **continuously** (live downstream consumption) and **on demand** (a full dump of a run). Targets differ in shape: a graph database, a relational database, line-delimited files, and OTel/OpenInference backends.

## Decision

Define a pluggable `Exporter` interface with **materialized-graph semantics** as the default across all targets, plus streaming and snapshot modes:

- **Materialized + idempotent upsert** by canonical id, so a node/edge that mutates (start→end, enrichment) updates in place rather than duplicating.
- **Streaming = CDC**: graph deltas drive incremental sink updates. The canonical variant set is pinned in spec §7: `node_upsert`, `edge_upsert`, `node_status`, `node_merge`, `run_started`, `session_ended`, `run_ended`. (`node_delete` is intentionally absent — id changes fold into `node_merge`, and removal is handled by retention eviction, ADR-0012.)
- **Snapshot**: `catacomb export --to <target> [--run <id>]` for a full dump.
- **Targets (v1):** `jsonl` (materialized node/edge records; also an event-log mode), **`otlp` (OpenInference passthrough)** via the ADR-0004 export mapper, `neo4j` (nodes+relationships, `MERGE`), `postgres` (`nodes`/`edges` tables, `INSERT … ON CONFLICT`, JSONB attrs, optional `pg_notify`).

## Alternatives considered

- **Append-only event-log export only** — simplest exporter, but pushes materialization/dedup downstream onto every consumer. Kept as a jsonl *mode*, not the default.
- **Snapshot-only (no streaming)** — contradicts the realtime goal. Rejected.

## Consequences

- **+** Downstream stores hold a clean, deduplicated, queryable graph that tracks the live run.
- **+** OTLP passthrough yields free trajectory visualization in external backends at near-zero added code.
- **−** CDC plumbing and per-target schema mappings to build and test (round-trip equality).
- **−** Slow sinks must not stall ingestion; addressed by a decoupled delta bus with bounded per-consumer buffers (spec §13).
