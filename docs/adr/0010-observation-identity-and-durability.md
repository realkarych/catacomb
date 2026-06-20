# ADR-0010: Observation identity and durability

- **Status:** Accepted
- **Date:** 2026-06-20
- **Deciders:** @realkarych
- **Related:** spec §5.1, §7, §8, §16; ADR-0003, ADR-0006, ADR-0011, ADR-0017, ADR-0018

## Context

ADR-0003 makes the append-only **observation log the system of record** and the canonical graph a deterministic reduction of it (spec §16: rebuild-from-log == materialized state). The design interrogation found this foundation contradicted by the M0.1 plan: `obs_id` was a per-parse 0-based counter (`obs-<seq>`), the `observations` table was keyed on `obs_id` alone and written with `INSERT OR REPLACE`, and the three materialized writes ran as independent autocommit `Exec`s with no transaction and no WAL. Re-parsing or replaying a transcript silently clobbers prior rows; a crash between writes tears the materialized tables; and `seq` did not survive restarts, so "monotonic per-daemon receive order" was not real. Every downstream guarantee rests on this layer.

## Decision

1. **`obs_id` is a real ULID** minted at ingest (as spec §5.1 always promised), globally unique across daemon lifetimes and runs. The observation log is keyed on `obs_id`, with `run_id`, `execution_id` (ADR-0011), and `source` as indexed columns, never as the sole identity.
2. **Append is insert-only.** Use `INSERT` (not `INSERT OR REPLACE`); a duplicate `obs_id` is a detected no-op/error, never a clobber. Observations are immutable once written.
3. **Replay is idempotent.** Re-ingesting the same transcript does not duplicate or corrupt: ingestion is keyed by a stable content key (`source` + the source-native event key, e.g. `tool_use_id`/`uuid`, + `payload_hash`) and a per-source ingest watermark; an already-seen event is skipped.
4. **One observation applies atomically.** Appending an observation and applying its materialized effects (node upsert(s) + edge upsert(s)) happen in **one SQLite transaction** — all-or-nothing. A durable **high-watermark = max(seq) reduced-and-committed** is persisted.
5. **`seq` is a persisted monotonic counter** (unsigned), restored on boot, authoritative for receive order and as the merge tiebreak (ADR-0018 makes `seq`, not wall-clock, the tiebreak).
6. **Recovery is defined.** On boot, treat the materialized tables as a cache valid only up to the committed watermark; replay observations with `seq` > watermark; "reconcile" = re-reduce those observations. The graph is always rebuildable from the log within a reducer version (ADR-0017).
7. **Durability mode:** WAL + `synchronous=NORMAL`, documented as the default.

## Alternatives considered

- **Compound PK `(run_id, source, obs_id)` keeping the counter** — fixes cross-run collision but leaves `obs_id` non-portable and replay still clobbering within a run. Rejected in favor of true ULIDs (spec already promised them).
- **Keep `INSERT OR REPLACE` as "idempotent upsert"** — silently mutates the system of record and defeats rebuild determinism. Rejected.
- **Per-statement autocommit + best-effort reconcile** — cannot deliver the crash-recovery ADR-0006 claims. Rejected for the per-observation transaction.

## Consequences

- **+** The observation log is genuinely immutable, globally unique, and replay-safe; rebuild-from-log holds; crashes leave the store consistent up to the watermark.
- **+** `seq` becomes a real total order for the reducer and exporters (cursors, ADR-0015).
- **−** A transaction per observation costs more than batched autocommit; mitigated by WAL and optional batching of independent observations within one transaction.
- **−** ULID minting adds a tiny per-event cost and a dependency; acceptable and already in the spec.
- **−** This supersedes the M0.1 plan's `obs-<seq>` counter and `INSERT OR REPLACE`; the plan and store code must be corrected.
