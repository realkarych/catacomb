# ADR-0015: Exporter correctness under failure

- **Status:** Accepted
- **Date:** 2026-06-20
- **Deciders:** @realkarych
- **Related:** spec §7, §9, §10, §13; ADR-0007, ADR-0010, ADR-0012

## Context

ADR-0007 promises exporters "track the live run" via materialized upsert + CDC, but the interrogation found the failure semantics undefined:

- §13 allows a slow sink with a bounded buffer and a "drop" policy, yet CDC carries **field-deltas, not snapshots**, so a dropped `node_status` delta (e.g. `running → error`) is **permanent silent divergence** with no error and no reconciliation.
- The delta bus is ephemeral fan-out; a sink **down for minutes** loses every delta emitted during the outage, and §8 recovery rebuilds only the in-memory graph — no per-exporter checkpoint, no gap-backfill.
- **No per-node ordering or rev guard:** two deltas for one node can apply out of order (coalescing/retry) setting `ok → running`; `pg ON CONFLICT DO UPDATE` and `neo4j MERGE+SET` are unconditional last-writer-wins (only jsonl has a `rev`).
- **Id changes aren't propagated:** §5.5 lets a provisional node collapse to a strong-key id later, but the GraphDelta vocabulary has no `node_merge`/`node_delete`, so the old downstream row is orphaned with dangling edges.
- **OTLP can't upsert** (immutable spans) yet §10 says it "emits as nodes finalize" without defining finalize for late-mutating nodes.

## Decision

1. **Every node carries a monotonic `rev`** (the originating `seq`, ADR-0010) on every GraphDelta and in every sink row. **All upserts are conditional:** pg `… WHERE excluded.rev > nodes.rev`; neo4j `MERGE … SET` guarded on `rev`. Stale/reordered deltas are ignored, not applied.
2. **FIFO-per-node ordering is guaranteed and documented** on the bus (the single-writer-per-execution reducer, ADR-0011, already produces source order; propagate `seq`).
3. **`drop` never loses state:** dropping intermediate deltas marks the node **dirty** and forces a **full re-emit of its current materialized state** once the buffer drains (coalesce-to-latest). Every exporter must be drivable purely from full node/edge state, so resync == re-upsert.
4. **Snapshot is the gap-backfill primitive.** On (re)connect or daemon restart, an exporter runs `Snapshot(run-filter)` to current materialized state, **then** attaches to live CDC. A **per-exporter cursor** (last-applied `seq`) enables incremental resume; fall back to full snapshot when the cursor is behind retention.
5. **Id changes get a `node_merge{old_id, new_id}` delta** that every sink implements (re-point edges, delete the old row) — or, per the conservative stance, provisional heuristic nodes are **buffered locally and not exported until they stabilize** (preferred default; matches ADR-0003's "rare duplicate over wrong merge" applied to export).
6. **OTLP passthrough emits a span only on terminal status + a settle window**, documented as **snapshot-at-finalization**, explicitly carved out of the "upsert across all targets" claim (spans are immutable).
7. **The bus is non-durable — stated plainly.** Strict tracking requires `policy=block+spill` or the snapshot-resume path; `drop` gives eventual-consistency via re-emit, never silent loss. Expose `deltas_dropped` / `exporter_lag` metrics (ADR-0019).

## Alternatives considered

- **Per-delta durable queue per exporter** — strongest, but heavy for a local tool; the snapshot-then-resume + cursor gives equivalent correctness with the store as the durable source. Rejected for v1.
- **Unconditional upserts (status quo)** — silent divergence under reorder/drop. Rejected for the `rev` guard.
- **Re-export full graph periodically** — wasteful and still races; dirty-node coalesced re-emit is targeted. Rejected.

## Consequences

- **+** Downstream stores converge to the true graph under slow sinks, drops, reorders, outages, and id changes — no silent divergence.
- **+** Snapshot + cursor gives clean recovery; `rev` makes upserts safe.
- **−** Every sink schema gains a `rev` column/property and conditional-write logic; OTLP gains a finalize/settle rule; more exporter complexity.
- **−** `drop` is now "lossy-intermediate, eventually-complete," not "lossy" — operators must understand the re-emit semantics; metrics make it visible.
