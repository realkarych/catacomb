# ADR-0017: Catacomb data-format versioning and migration

- **Status:** Accepted
- **Date:** 2026-06-20
- **Deciders:** @realkarych
- **Related:** spec §8, §15, §16, §17; ADR-0006, ADR-0010

## Context

Catacomb persists an append-only observation log and materialized tables and promises the graph is "rebuildable from the log forever" (spec §16). But §15 versions only *Claude Code's* formats, never **Catacomb's own**. The interrogation showed the upgrade path is undefined: a new binary over a months-old SQLite store either replays old observation bodies through a **new reducer** (which can shift canonical ids → duplicate nodes, or hit unknown enum values) or loads **old materialized rows** that no longer deserialize into the new struct. There is no `schema_version`, no per-body format version, no migration runner, and no forward/back-compat policy. "Rebuildable from the log" only holds if old bodies stay interpretable by the code reading them.

## Decision

1. **Three version stamps in the store:**
   - **`schema_version`** (table DDL shape) — `PRAGMA user_version`.
   - **`reducer_version`** (the reduction logic that maps observations → graph).
   - **`body_schema_version`** per observation (the shape of the stored body).
2. **Refuse-or-migrate on boot:** if the store's `schema_version` ≠ the binary's, run a **DDL migration runner** (ordered, forward-only) before serving; if it cannot, refuse to start with a clear message rather than corrupt.
3. **Reducer changes re-derive, not reinterpret in place:** when `reducer_version` changes, the graph is **rebuilt from the observation log** (it is the system of record, ADR-0010) under the new reducer, into fresh materialized tables. Rebuild determinism is therefore guaranteed **only within a reducer version** — stated explicitly (refines §16).
4. **The versioned parser (§17) is replay-aware, not just live-ingest-aware:** it must interpret a stored `body_schema_version` it wrote months ago, not only today's live wire shapes. Old bodies are read through the parser version that understands them.
5. **Compatibility policy:** forward-compat = ignore unknown columns/fields; **downgrade = hard error** (a newer store on an older binary refuses). Unknown enum values encountered on replay map to a defined fallback (e.g. status `unknown`, ADR-0012), never a panic.
6. **Migrations are tested** as part of the suite (apply over a fixture store from each prior schema; assert rebuild equality within a reducer version).

## Alternatives considered

- **No versioning, assume forward-only in-place reads** — the status quo; silently shifts ids or fails to deserialize on upgrade. Rejected.
- **Only DDL migrations, reinterpret bodies in place** — breaks when reducer *logic* (not just table shape) changes ids. Rejected for the rebuild-from-log-on-reducer-change rule.
- **Freeze the format** — unrealistic for an early-development tool. Rejected.

## Consequences

- **+** Upgrades are safe: tables migrate, the graph rebuilds deterministically under the new reducer, old bodies stay interpretable, downgrades fail loudly instead of corrupting.
- **+** Makes the §16 rebuild guarantee precise (per reducer version) instead of aspirational.
- **−** A migration runner, three version stamps, and replay-aware parser versions to maintain — ongoing cost as the format evolves.
- **−** A reducer-version bump forces a full rebuild from the log (potentially large); acceptable as a rare upgrade-time cost, and bounded by retention.

## Amendments

- **Annotations survive a reducer-bump rebuild (with ADR-0016):** the dedicated annotations side-table is **not** reconstructed from the observation log (the log never contained it); it is preserved across the rebuild and **re-attached** by its durable `(execution_id, source-native key)` handle, with `step_key` recomputed by the new reducer. The boot sequence (ADR-0010 Amendments) runs the rebuild without touching that table.
