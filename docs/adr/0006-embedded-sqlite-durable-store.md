# ADR-0006: Embedded SQLite as the default durable store

- **Status:** Accepted
- **Date:** 2026-06-20
- **Deciders:** @realkarych
- **Related:** spec §8; ADR-0003, ADR-0005, amended by [ADR-0026](0026-form-factor-pivot-offline-eval-gate.md)

## Context

The daemon needs durable, crash-recoverable, locally queryable state with **no external dependencies**. The hot path is **many small idempotent upserts** as observations stream in and enrich canonical nodes — an OLTP-shaped write workload. Cross-run analytical queries exist but are secondary and can be served by exports.

## Decision

Use an **embedded database, SQLite by default**, behind a `Store` interface; **DuckDB optional** for analytics-heavy deployments.

- **Driver:** the pure-Go `modernc.org/sqlite` (no cgo), so Catacomb stays a single static cross-platform binary; cgo-based drivers such as `mattn/go-sqlite3` are excluded. This no-cgo constraint is the load-bearing reason SQLite is viable as the zero-config default.
- Tables: `observations` (append-only log — the system of record), plus materialized `nodes` / `edges` / `runs` / `markers`.
- An **in-memory graph** serves realtime reads/subscriptions; the store is write-through (batched) and is the recovery source.
- **Recovery:** rebuild the in-memory graph by replaying `observations` (authoritative) or loading materialized tables (fast path) and reconciling.
- **Retention:** append-only by default; optional per-run TTL / max-runs eviction via config, off the hot path.

## Alternatives considered

- **In-memory + WAL** — fast, but re-implements querying and indexing over a log. Rejected as default.
- **Sinks as the store** (Postgres/Neo4j hold all state) — removes a local store but creates a hard dependency on an external service just to run the daemon. Rejected; offered instead as exporters (ADR-0007).
- **DuckDB as default** — better for cross-run analytics, but its OLAP engine is a weaker fit for high-frequency small writes; kept as an opt-in.

## Consequences

- **+** Zero-config, robust crash recovery, OLTP-appropriate write path.
- **+** Locally queryable without external services; observation log guarantees rebuildability.
- **+** DuckDB remains available where analytics dominate; heavy analytics otherwise served via export.
- **−** SQLite is less suited to large cross-run analytical scans; mitigated by exports and the optional DuckDB store.
