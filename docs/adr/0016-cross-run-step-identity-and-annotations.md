# ADR-0016: Cross-run step/phase identity and the annotations contract

- **Status:** Accepted
- **Date:** 2026-06-20
- **Deciders:** @realkarych
- **Related:** spec §5.2, §5.5, §5.6, §14, §15; ADR-0003, ADR-0009, ADR-0010, ADR-0011

## Context

Catacomb's stated north star is to be the tracing foundation for a downstream step-level evaluation layer (MC-value/advantage across repeated runs). The interrogation found this **inexpressible today**:

- Every canonical node id is execution-scoped and derived from per-run-random values (`tool_use_id`/`message_id`/`agentId`; ADR-0011). Across two runs of the same pipeline the "same logical step" (read→edit→test) has different ids — confirmed: tool/message/uuid ids never recur across sessions, and even identical prompts hash to different ids. So "the same step in run A and run B" has **no join key**.
- `annotations` is offered as the eval bridge (§14) but is keyed to volatile execution-scoped node ids, is **write-anything with no ownership/conflict rule**, and is declared "Catacomb never reads/writes" while Catacomb's own machinery **churns the nodes**: heuristic→canonical merge changes ids, supersede/cancel changes status, crash-rebuild replays the log (which never contained the externally-written annotation) — silently destroying the eval layer's investment.

## Decision

Introduce a **second identity axis** plus an explicit **annotations lifecycle contract**.

1. **`step_key`** — a best-effort, **run-invariant** node identity computed from features stable across runs: the structural path from the session root (prompt-subtree ordinal, depth, sibling index **on the active branch**), the tool name, the subagent `agentType`, and a **normalized hash of salient input** (e.g. file path + normalized args for `Edit`; the command for `Bash`) — deliberately **excluding** volatile ids, timestamps, and `seq`. Stored as `Node.step_key` (nullable), with a `step_key_method`/confidence tag (like `identity=heuristic`). The marker analogue is **`phase_key := marker name`** (markers are user-defined and already stable, §5.6).
2. **`step_key` is advisory, not identity:** it never merges nodes within a run (that is `execution_id`-keyed, ADR-0011); it is the **join key the eval layer uses across runs** and the recommended key for attaching annotations.
3. **Annotations contract:**
   - **Namespaced per writer:** `annotations.<owner>.<key>` (no schema-less collisions; multiple consumers coexist).
   - **Carry-over across Catacomb-controlled lifecycle events:** on heuristic→canonical **merge**, on **supersede/cancel**, and on **rebuild-from-log**, annotations are **preserved and re-keyed to the surviving canonical node**; collision policy is stated (default: union; same key → last-writer-wins).
   - **Durable across rebuild:** annotations live in their own store table that is part of the recovery path (they are *not* in the observation log, so rebuild must re-attach them by node id and, where present, `step_key`).
   - Catacomb still **does not interpret** annotation contents; it only preserves/relocates them.
4. **Recommended usage:** the eval layer keys its data by `step_key` (cross-run) rather than the execution-scoped node id, so its data survives both node churn and re-runs.

## Alternatives considered

- **Make node ids deterministic/content-addressed** so they recur across runs — collides legitimately-distinct actions within a run and fights ADR-0003/0011; rejected in favor of a *separate* advisory axis.
- **Leave annotations as an opaque map** — silently lost on merge/rebuild; the eval bridge would be unreliable. Rejected.
- **Store annotations in the observation log** — they are external, post-hoc, and mutable; they don't belong in the immutable source of record. Rejected for a dedicated, recovery-aware side table.

## Consequences

- **+** The north-star use case becomes expressible: an evaluator can align "the same step/phase" across runs via `step_key`/`phase_key` and attach durable, namespaced annotations that survive Catacomb's own merges, supersedes, and rebuilds.
- **+** Keeps within-run identity (ADR-0011) and cross-run identity cleanly separate.
- **−** `step_key` is heuristic: different inputs can collide or the same logical step can drift if the pipeline changes; it is explicitly best-effort with a confidence tag, never used for within-run merging.
- **−** Adds an annotations store table and lifecycle handling (merge/supersede/rebuild carry-over) — real complexity for a slot Catacomb otherwise ignores.

## Amendments

- **`step_key` is stable and granularity-independent:** a pure function of the **final, post-cascade** graph, computed over the **rich/core-tier** structural path (pre-contraction, so lean mode yields the same key) walking **only live (non-`superseded`/`abandoned`) ancestors**; superseded/abandoned siblings are excluded from ordinal/sibling-index counting. A late interrupt or lean mode does not change a step's key.
- **Hashed post-redaction:** the salient-input hash is computed **after** redaction (ADR-0020), with redaction placeholders normalized to a stable typed token (e.g. `‹redacted:uri›`) plus non-secret structural features — so a rotating secret normalizes identically across runs (no leak via the key) and distinct-but-redacted inputs do not collide.
- **Durable annotation handle:** annotations are keyed by the immutable **(`execution_id`, source-native event key)** (the content key ADR-0010 uses for idempotent ingest), **not** the derived node id and **not** `step_key`. On a reducer-bump rebuild they are re-attached by that handle and `step_key` is recomputed as a secondary index — neither moving handle orphans them.
- **Annotations in CDC (with ADR-0015):** a write bumps `rev` + emits `node_upsert`; `node_merge` moves them old→new.
- **`phase_key` discriminates iterations:** `phase_key` = enclosing-step structural path + marker name + **occurrence-ordinal within scope**, so attempt k of a repeating phase aligns across runs while staying distinct within a run.
