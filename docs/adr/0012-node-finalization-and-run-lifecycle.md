# ADR-0012: Node finalization and run lifecycle

- **Status:** Accepted
- **Date:** 2026-06-20
- **Deciders:** @realkarych
- **Related:** spec §5.2, §7, §8, §9, §10; ADR-0005, ADR-0007, ADR-0009, ADR-0014, ADR-0015

## Context

ADR-0009 handles interruption (a `tool_call` whose turn is cut → `cancelled`). The interrogation found the **dual case unhandled**: a `tool_call` (often a subagent dispatch) whose terminal observation **never arrives and was not interrupted** — a crashed/OOM subagent, a dropped MCP server, an abandoned tool, or a `PostToolUse` lost while the daemon was down. The node is created `pending`/`running` and stays there forever; the status enum has no value for "outcome unknown," and `cancelled` is reserved for interruption. Relatedly, `run_ended` appears in the CDC vocabulary (spec §7, ADR-0007) with **no defined trigger or owner**, and a wrapper run whose orchestrator crashes never ends — its reducer shard leaks and TTL eviction (keyed on end-time) never fires.

## Decision

1. **New terminal status `unknown`** (distinct from `cancelled`/`error`): "we will not learn the outcome." Added to the §5.2 enum.
2. **Node closure triggers** for a non-terminal in-flight node:
   - **Offline:** JSONL EOF on a non-live transcript with a `tool_use` lacking both a matching `tool_result` and an `interruptedMessageId` → `unknown`.
   - **Live:** a configurable inactivity TTL after the parent turn finalizes / `SessionEnd` arrives while a descendant is still open → `unknown`.
   - **Run end:** `run_ended` with non-terminal descendants closes them `unknown`.
   Each closure emits a `node_status` delta so live consumers and exporters converge.
3. **`unknown` is provisional under the status lattice (ADR-0014):** a genuine terminal observation (`ok`/`error`) arriving later always supersedes an inferred `unknown` or `cancelled`, keeping reduction order-independent (spec §16).
4. **Run lifecycle is explicit.** A run/execution is **open** on its first observation and has no intrinsic end. Two distinct end signals:
   - **`session_ended`** — a session reaches `SessionEnd` (+ transcript EOF/quiescence). Exporters finalize a session on this.
   - **`run_ended`** — fires only on an explicit wrapper exit / end-marker, or via the **idle-reaper** (below). Never assumed from `session_ended` alone (a wrapper run may have more sessions).
5. **Idle-reaper.** A run/execution with no new observation for a quiescence window → status `abandoned` + a synthetic `run_ended{reason:timeout}`; this releases the reducer shard (recoverable from the store) and makes the run TTL-eligible with `end_time = last-observation`. This is the missing owner/trigger for `run_ended`.
6. **Eviction gates on liveness** (cross-ref ADR-0006): never evict a run that is not `run_ended`, is a wrapper sibling with any active session, or is behind any exporter/subscriber watermark.

## Alternatives considered

- **Overload `cancelled` for orphans** — conflates "user interrupted" with "outcome unknown," loses a real distinction the eval/repair layer needs, and collides with ADR-0009's `interruptedMessageId`-keyed semantics. Rejected.
- **Leave in-flight nodes open forever** — leaks reducer shards, blocks eviction, and makes a "completed run" never queryable as complete. Rejected.
- **Derive `run_ended` from the last `SessionEnd`** — wrong for wrapper runs spanning multiple sessions. Rejected for the explicit `session_ended` vs `run_ended` split.

## Consequences

- **+** Every node reaches a terminal status; "completed/abandoned" runs are queryable; reducer shards and storage are reclaimable; `run_ended` has a defined owner.
- **+** The lattice keeps late terminals authoritative, preserving commutativity.
- **−** Adds a timer/reaper concern and one config knob (quiescence window); a too-short window can prematurely mark `unknown`/`abandoned` (mitigated: provisional, reversible by a later terminal).
- **−** Adds `unknown`/`abandoned` to the status vocabulary that exporters and the UI must render.
