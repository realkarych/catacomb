# ADR-0019: Operability, fault isolation, and self-observation

- **Status:** Superseded by [ADR-0026](0026-form-factor-pivot-offline-eval-gate.md)
- **Date:** 2026-06-20
- **Deciders:** @realkarych
- **Related:** spec §4, §6, §13, §17; ADR-0002, ADR-0013, ADR-0015

## Context

Catacomb leans on **beta and undocumented** inputs (OTel beta, undocumented `stream-json`, version-fragile JSONL) — the inputs most likely to malform — yet the design has no fault-isolation contract: in Go an unrecovered panic in any goroutine **crashes the whole process**, so a single bad record stops ingestion for *every* run, loses the in-memory graph, and makes live agents' hooks fail open (drop). Separately, the design introduces several silent failure modes — fail-open hook drops (§13), bounded-buffer exporter drops (ADR-0015), unbounded run-forest growth, store write errors — with **no health surface, no metrics, no self-instrumentation**, so an operator cannot tell that data is being lost. Finally, dogfooding creates loops: the JSONL tailer ingests Catacomb's own dev session, and the OTLP exporter can be pointed at its own receiver.

## Decision

1. **Per-adapter supervision with a `recover()` boundary.** Every adapter (and every reducer shard) runs under a supervised goroutine; a panic is converted into a logged error + a **quarantined poison observation** (raw bytes retained for diagnosis) and the adapter **restarts with backoff** — it never crashes the daemon. The reducer is isolated per execution shard (ADR-0011) so one bad merge cannot kill others. Invariant (with a fixture): *one bad input never stops other runs.*
2. **Health + metrics surface.** A local health/readiness endpoint plus metrics: per-adapter liveness (up/restarting/failed), ingest queue depth, hook-drop count, exporter `deltas_dropped`/lag (ADR-0015), store write errors, open-run count, reducer-shard count, resident memory. The failure modes the design introduces are made **visible**, not silent.
3. **Lossy-run self-declaration.** When fail-open drops or buffer drops affect a run, the run is flagged `lossy` (with counts) so consumers know the graph for that run is incomplete rather than authoritative.
4. **Dogfooding loop-breaking.** The tailer excludes Catacomb's own session/project dir by default; the OTLP exporter refuses an endpoint equal to the daemon's own receiver (cross-ref ADR-0013). Self-observation is opt-in and explicitly fenced.
5. **Soft caps.** Default soft limits on open runs / resident memory; exceeding them sheds or evicts per ADR-0012's reaper and logs/metricizes the action — never an OOM crash.

## Alternatives considered

- **Let panics crash and rely on a process supervisor to restart** — loses the in-memory graph and all other runs' ingestion on any single bad beta record; unacceptable given the input risk. Rejected for in-process recover()+restart.
- **No metrics / health (status quo)** — the design's own silent-loss modes stay invisible. Rejected.
- **Always self-observe (no loop-break)** — produces feedback loops and runaway growth. Rejected for opt-in self-observation.

## Consequences

- **+** A malformed beta/undocumented input degrades to a quarantined record and an adapter restart, never a daemon crash; operators can see drops, lag, and growth; incomplete runs are labeled.
- **+** Composes with ADR-0013 (security) and ADR-0015 (exporter visibility).
- **−** Supervision, quarantine, a metrics/health surface, and caps are real implementation surface beyond the happy path.
- **−** `recover()` boundaries must be disciplined (recover only at adapter/shard edges, never to mask logic bugs); a poison-quarantine store to manage and bound.
