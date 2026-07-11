# ADR-0004: Graph-native canonical model with OTel/OpenInference boundary mappers

- **Status:** Accepted
- **Date:** 2026-06-20
- **Deciders:** @realkarych
- **Related:** spec §5, §5.7; ADR-0003, ADR-0021, amended by [ADR-0026](0026-form-factor-pivot-offline-eval-gate.md)
- **Amended by:** ADR-0021 — `assistant_turn` is reclassified **core-tier** (load-bearing spine); lean mode folds only `hook_event`. The detail-tier list in the Decision below is superseded for `assistant_turn`.

## Context

The canonical representation must express a true **graph** (delegation trees, temporal sequence, future data-dependency edges) and absorb **non-OTel** sources (hooks, JSONL, stream-json), while remaining **interoperable** with the wider ecosystem (OpenTelemetry GenAI semantic conventions, OpenInference, backends like Arize Phoenix).

## Decision

Make the internal model **graph-native** (`Node` / `Edge`), and treat OTel/OpenInference as **bidirectional mappers at the boundaries**:

- **Import:** OTLP spans → Observations (span kind/attrs → node type/fields; `parent_span_id` → `parent_child`).
- **Export:** nodes/edges → OpenInference span kinds (`AGENT` subagent, `TOOL` tool/mcp, `LLM` assistant turn, `CHAIN` marker span).
- **Node granularity** is one axis with two halves: the `graph.granularity` config is `rich` (default) or `lean`, and each canonical node carries a `tier` of `core` or `detail`. `rich` materializes both tiers; `lean` materializes only `core`-tier nodes and folds `detail`-tier nodes (`hook_event`) into attributes/metrics on their enclosing node. The word `rich` names a granularity mode; the per-node values are `core`/`detail` to avoid overloading it. **`assistant_turn` is core** (a load-bearing interior vertex of the prompt→turn→tool spine; reclassified in ADR-0021), so lean never folds it.

## Alternatives considered

- **OTel spans as the canonical internal model** ("everything is a span") — maximal interop, but spans are tree-shaped and awkward for DAG edges and for non-OTel sources; couples the core to a beta schema. Rejected.
- **OpenInference as the canonical model** — same tree-shape limitation. Rejected.

## Consequences

- **+** Freedom to model DAG edges and heterogeneous sources; clean separation of core model from wire formats.
- **+** Portable to external OTel/OpenInference backends without owning their schema internally.
- **+** Granularity knob trades fidelity vs volume per deployment.
- **−** Mappers must track OTel beta span-schema drift; isolated and versioned to contain churn.
