# ADR-0011: Canonical-id contract under concurrency and reuse (execution scope)

- **Status:** Accepted
- **Date:** 2026-06-20
- **Deciders:** @realkarych
- **Related:** spec §5.4, §5.5, §13; ADR-0003, ADR-0005, ADR-0010, ADR-0016

## Context

Canonical node ids are `run_id`-prefixed (spec §5.5: `tool_call = run_id:tool_use_id`, `assistant_turn = run_id:message_id`, `subagent = run_id:agentId`). But `run_id` can be a **wrapper grouping N sessions** (spec §5.4 / ADR-0005), while `tool_use_id`, `message_id`, and `agentId` are unique only **within a session** (regenerated per session). So the id's uniqueness domain (a session) is smaller than its collision domain (a wrapper run). Two concurrent child sessions under one `CATACOMB_RUN_ID`, or a reused label like `--run-id nightly`, **collide unrelated actions into one node** — a wrong merge at the run layer, where ADR-0003's conservative heuristic guard does not apply. Separately, §13's "single-writer per run sharded by `run_id`" serializes genuinely parallel sessions onto one reducer shard.

## Decision

Separate the **logical grouping key** from the **physical execution instance**.

1. **`execution_id`** — a ULID minted when a session first attaches (one per Claude Code session / per replayed transcript). It is the **identity prefix** for canonical node ids: `tool_call = execution_id:tool_use_id`, `assistant_turn = execution_id:message_id`, `subagent = execution_id:agentId`, `session = execution_id`. These keys are session-scoped, matching the uniqueness domain of `tool_use_id`/`message_id`/`agentId`.
2. **`run_id`** is demoted to a **non-identifying grouping label** (the wrapper from `CATACOMB_RUN_ID`, or `session_id` by default). It never participates in node identity; it groups executions for cross-run queries and is carried as a node/edge attribute.
3. **A replayed transcript gets a fresh `execution_id`**, so replay never collides with the live run it came from; reusing a `--run-id` groups for comparison without merging nodes.
4. **The reducer shards by `execution_id`** (single-writer per execution), so parallel sessions run on independent shards.
5. **`session_id` ↔ `execution_id` mapping** is recorded on the `Run`/execution metadata so the source anchor (still `session_id`, ADR-0005) resolves to the right execution.

## Alternatives considered

- **Keep `run_id` as the prefix, forbid wrapper reuse** — pushes a correctness-critical invariant onto operators; a reused `nightly` id still silently merges everything. Rejected.
- **Prefix with `session_id` directly** — works for single-session runs but a replayed transcript shares the original `session_id` and would collide with the live execution; also conflates "where it came from" with "which execution instance." Rejected for an explicit `execution_id`.
- **Append `session_id` to every key in addition to `run_id`** — equivalent to `execution_id` but noisier and still ambiguous on replay. Rejected.

## Consequences

- **+** No cross-session id collisions under wrapper runs or label reuse; replay is isolated; parallel sessions get parallel reducer shards.
- **+** Cross-run comparison (the point of ADR-0005's forest) is preserved via the `run_id` grouping label and the cross-run `step_key` (ADR-0016), not via colliding node ids.
- **−** Changes the canonical-id contract that ADR-0003 (identity) and ADR-0005 (run model) reference; spec §5.5/§5.4 and the M0.1 plan's id helpers must be updated (`run_id:` → `execution_id:`).
- **−** One more id to mint and thread; mitigated by minting once per session attach and carrying it on every observation.
