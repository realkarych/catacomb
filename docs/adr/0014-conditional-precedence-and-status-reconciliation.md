# ADR-0014: Conditional structure precedence and status reconciliation

- **Status:** Accepted
- **Date:** 2026-06-20
- **Deciders:** @realkarych
- **Related:** spec §3, §5.5, §7, §15, §16, §17; ADR-0002, ADR-0003, ADR-0009, ADR-0012, amended by [ADR-0026](0026-form-factor-pivot-offline-eval-gate.md)

## Context

Three reconciliation gaps surfaced, all around how conflicting/late observations resolve:

1. **Static precedence contradicts the known OTel defect.** §7 ranks structure precedence "OTel span tree → JSONL tree" *unconditionally*, but §17/ADR-0002 declare OTel structure **known-broken on the SDK streaming path** (#53954, only `llm_request` spans) and JSONL the subagent-tree truth. On the very high-fan-out subagent case, a collapsed OTel parent would wrongly win over the correct JSONL parent. §15 records the SDK/CLI version "for #53954 gating" but nothing consumes it.
2. **No status lattice.** §7's precedence table has no row for `status`, so an inferred `cancelled` (ADR-0009) or `unknown` (ADR-0012) and a genuine late `tool_result` (`ok`/`error`) resolve by arrival order — breaking the §16 commutativity invariant.
3. **No cross-subtree cascade.** ADR-0009 scopes interruption/supersede to "that turn" and its direct `tool_call`s; a cancelled subagent dispatch leaves the child subagent subtree (in a separate file) running forever; a superseded branch doesn't sweep its subagent descendants.

## Decision

1. **Structure precedence is conditional on a per-execution OTel-completeness verdict.** Derive the verdict from the §15 SDK/CLI version + observed span kinds: when the #53954 profile is detected (SDK streaming, only `llm_request` kinds, no tool-span children), **demote OTel below JSONL for `parent_child`** for that execution. Equivalently: an OTel span lacking children / a `tool_use_id` never wins `parent_child` over a JSONL-derived edge. The gate is specified, not "degrade gracefully" prose.
2. **Status lattice** (added to §7): a real **terminal observation always beats an inferred one** — `ok`/`error` (from a genuine `tool_result`/span) > `cancelled` (interrupt-inferred) / `unknown` (orphan-inferred) > `running` > `pending`. `cancelled`/`unknown` are **provisional**; any later genuine terminal supersedes them. This makes final status independent of arrival order.
3. **One transitive cascade pass over the `parent_child` closure**, regardless of source file: when a turn/`tool_call` becomes `cancelled` or `superseded`, mark descendant `tool_call`s and any `subagent` subtree reached via `tool_call → subagent` edges with the same status (status-only, tagged `attrs.cancel_cause`), **except** descendants that already hold a genuine terminal (a subagent that finished before the interrupt keeps its `ok`/`error`).
4. **`assistant_turn` id is pinned to `message.id` (`msg_*`)**, explicitly distinct from the JSONL `uuid`, so an `interruptedMessageId` reliably matches its turn; if the named turn is absent (cross-compaction/cross-file), create a provisional `cancelled` placeholder keyed by it for later merge rather than dropping the signal.

## Alternatives considered

- **Keep static source-ranked precedence** — wrong on the user's actual SDK path; the table and prose stay contradictory. Rejected.
- **Last-writer-wins on status** — order-dependent, breaks commutativity and the rebuild invariant. Rejected for the lattice.
- **No cascade (close orphans only by TTL, ADR-0012)** — leaves cancelled/superseded subagent subtrees mislabeled until a timeout; the cascade is deterministic and immediate. Kept both: cascade for known cause, TTL/`unknown` for genuinely silent orphans.

## Consequences

- **+** The subagent tree is correct on the SDK path; status is order-independent (commutativity holds); interruption/supersede sweep their whole subtree across files.
- **+** Resolves the §7-vs-§17 contradiction with a concrete, version-gated mechanism.
- **−** The reducer must compute and store a per-execution OTel-completeness verdict and run a closure pass on cancel/supersede; more logic than a static table.
- **−** The verdict depends on version detection (§15/§17), a version-fragile input; conservative default is "treat OTel structure as incomplete unless proven whole."

## Amendments

- **Prefer the local structural rule; latch the verdict:** the v1 mechanism is purely local — an OTel `parent_child` edge wins **only if** that span has OTel children or a `tool_use_id` linking it to a tool node; otherwise JSONL wins (correct under #53954 by construction, no version detection needed). The per-execution version-keyed verdict is **monotonically latched** (the #53954 profile is an SDK/CLI-version property that does not change mid-execution) and demoted to an optional optimization — it never flaps a parent mid-stream.
- **Edge revisions (with ADR-0015/0021):** edges carry a `rev` (or an `edge_delete` tombstone) so a re-parent emits delete-old + upsert-new under the same `rev`-guard as nodes; a demoted/re-parented edge **re-anchors orphaned descendants to the surviving ancestor, never drops them** (preserving the ADR-0021 forest invariant).
- **Lattice = the status rule:** status reconciliation uses this lattice (not the per-field precedence table of ADR-0003/§7, which governs non-status fields); the lattice covers all nine statuses per ADR-0012's amendment.
