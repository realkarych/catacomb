# ADR-0003: Reconciliation — canonical entity with per-field precedence and provenance

- **Status:** Accepted
- **Date:** 2026-06-20
- **Deciders:** @realkarych
- **Related:** spec §5.1, §5.5, §7; ADR-0002, ADR-0010, ADR-0011, ADR-0014, ADR-0016, ADR-0018
- **Amended by:** ADR-0014 (structure precedence is conditional on the #53954 verdict) and ADR-0018 (the merge tiebreak is `seq`, not `observed_at`). The Decision text below predates these two changes; spec §7 is the current statement.

## Context

A single real action (e.g. one tool call) is observed by up to four sources at once, each with different fidelity and arrival time. The tool must present **one** coherent node per action — not duplicates or a per-source jumble — while remaining auditable and tolerant of out-of-order delivery.

## Decision

Model each real action as **one canonical entity**; sources are **observations** merged into it.

- **Identity:** canonical ids derived from stable shared keys, with `tool_use_id` as the linchpin tying hooks ↔ JSONL ↔ stream-json (and OTel tool spans via their `tool_use_id` attribute). Session = `session_id`; subagent = `agent_id` / spawning `tool_use_id`; assistant turn = `message_id`.
- **Merge:** observations idempotently upsert the canonical node using a **per-field precedence** table (structure: OTel→JSONL→stream-json→hooks; timing: OTel→hooks→JSONL→stream-json; cost/tokens: OTel→stream-json→JSONL; payload: hooks/JSONL→stream-json). Ties broken by latest `observed_at`.
- **Provenance:** every contributing observation is recorded on the node (`sources`), so any field is traceable to its origin.
- **System of record:** an **append-only observation log**; the canonical graph is a **deterministic reduction** of that log and can always be rebuilt.
- **Conservative identity fallback:** when no strong key is present (e.g. an OTel-only tool observation with no `tool_use_id`), create a *provisional* node tagged `identity=heuristic`; merge into a canonical node only if a strong key arrives later. A rare duplicate is preferred over a wrong merge.

## Alternatives considered

- **Multigraph + unified-view-on-query** — keep each source's view separate, union at read time. More faithful to raw data but heavier and more complex queries. Rejected.
- **Single-primary + fallback** — pick the best available source per run, no merge. Simpler but discards cross-source enrichment (e.g. OTel cost on a hook-captured tool). Rejected.

## Consequences

- **+** One coherent, auditable node per action; cross-source enrichment retained.
- **+** Deterministic, rebuildable, order-independent (commutative merge given precedence + timestamps).
- **−** A precedence table and id-derivation rules to maintain and test.
- **−** Rare duplicates possible in the no-strong-key case (accepted trade-off).
