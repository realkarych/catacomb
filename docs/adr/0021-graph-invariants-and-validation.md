# ADR-0021: Graph invariants and validation

- **Status:** Accepted
- **Date:** 2026-06-20
- **Deciders:** @realkarych
- **Related:** spec §5.2, §5.3, §9, §16; ADR-0003, ADR-0004, ADR-0009, ADR-0011, ADR-0014

## Context

Exporters, layouts, and the OpenInference mapping (ADR-0004/0007) all assume the `parent_child` structure is a **DAG / span-tree**, but nothing asserts or enforces it. The interrogation showed several ways the graph can become malformed: conflicting cross-source parents (OTel vs JSONL, a nested-subagent version flip) can produce `A→B` and `B→A`; a provisional→canonical id rewrite (ADR-0003/0011) can create a self-loop or a duplicate; and **lean mode** (ADR-0004) folds `assistant_turn`/`hook_event` — which ADR-0009 makes a *mandatory interior vertex* of the `user_prompt → assistant_turn → tool_call` spine — leaving `tool_call` edges that reference a **node id that doesn't exist** (dangling edges, broken neo4j `MERGE`/pg joins).

## Decision

1. **`parent_child` is a forest invariant:** every node has **at most one** `parent_child` parent (the highest-precedence one, ADR-0014 wins), and the relation is **acyclic**. The reducer enforces this on every merge.
2. **Cycle and self-loop checks:** after any id rewrite (provisional→canonical) or cross-source parent change, drop self-loops and reject a parent assignment that would close a cycle (keep the higher-precedence edge, demote the other to a non-structural relation or drop with a logged conflict).
3. **Lean-mode edge contraction:** when a detail-tier node is folded (ADR-0004), its `parent_child` edges are **transitively re-linked** (a `tool_call` re-parents to the nearest surviving ancestor, e.g. its `user_prompt`), and `sequence`/`marker_span`/`data_dep` edges referencing it are dropped or re-anchored. **No edge is ever emitted whose endpoint is unmaterialized.**
4. **Reclassify `assistant_turn` as core-tier**, since it is structurally load-bearing for the spine (ADR-0009); only `hook_event` and `sequence` edges remain detail-tier. This removes the most common lean-mode dangling-edge case at the source.
5. **Invariant tests (extend §16):** property tests assert, on every reduced graph and every export, that `parent_child` is an acyclic forest, that no edge has a missing endpoint (in any granularity mode), and that lean-mode contraction preserves reachability of surviving nodes.

## Alternatives considered

- **Trust sources to be acyclic/tree-shaped** — false under cross-source conflict, version flips, and id rewrites; exporters then emit dangling/cyclic graphs. Rejected.
- **Allow multiple parents (true DAG) for `parent_child`** — complicates "who is the owner" for attribution/eval and the tree-shaped exporters; `data_dep` already exists for non-tree relations. Rejected: keep `parent_child` a forest, use other edge types for DAG relations.
- **Keep `assistant_turn` detail-tier and special-case it in lean** — leaves the dangling-edge trap one config flag away. Rejected for reclassification + general contraction.

## Consequences

- **+** The graph is always a valid acyclic forest with no dangling edges, in both rich and lean modes; exporters and the UI can rely on it; conflicts are resolved deterministically and logged.
- **+** Makes the §16 invariant suite enforce structure, not just field values.
- **−** Cycle/forest enforcement and edge contraction add reducer work on every merge and a contraction pass in lean mode.
- **−** Demoting a conflicting parent edge can lose a (wrong) structural link; the conflict is logged for diagnosis rather than silently kept.
