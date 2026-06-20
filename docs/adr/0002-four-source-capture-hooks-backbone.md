# ADR-0002: Four-source capture with hooks as the backbone

- **Status:** Accepted
- **Date:** 2026-06-20
- **Deciders:** @realkarych
- **Related:** spec §3, §6; ADR-0003

## Context

Claude Code exposes observable signal through four partial channels, each strong in different ways: **hooks** fire on both the Agent SDK and interactive CLI paths and carry tool/MCP fields; **native OpenTelemetry traces** (beta) give a real parent→child span tree with cost/token and MCP signals, but on the **Agent SDK streaming path only `llm_request` spans fire** (issue #53954) and the schema is beta; **stream-json** carries structural hints (notably `parent_tool_use_id`) but its CLI envelope is undocumented; **transcript JSONL** is the source of truth for the subagent tree but is post-hoc on disk. The user drives Claude Code via **both** the SDK and the CLI.

No single source is complete on every path and version.

## Decision

Ingest **all four** sources, with **hooks as the backbone** and the others as complementary signal:

- **Hooks** — backbone; fire on every execution path. A tiny forwarder POSTs each event to the daemon.
- **Native OTel (OTLP receiver)** — enrichment: clean span tree, per-node cost/token, MCP spans (where whole — i.e. interactive CLI / `claude -p`).
- **stream-json reader** — structural signal, especially `parent_tool_use_id` for subagent linkage.
- **JSONL tailer** — source of truth for the subagent tree and backfill when OTel collapses on the SDK path; also powers fully offline replay.

Adapters implement a common Go `Adapter` interface so additional sources can be added without touching core.

## Alternatives considered

- **OTel-only** — simplest, standards-based, but the hierarchy collapses on the SDK streaming path (#53954) and the schema is beta. Rejected as a sole source.
- **Hooks-only** — robust across paths but flat (no native tree, weaker cost/structure). Rejected as a sole source; kept as the backbone.

## Consequences

- **+** The graph is whole across both driving modes and across version/beta drift.
- **+** Each source contributes its strength (tree from OTel/JSONL, cost from OTel, linkage from stream-json, universality from hooks).
- **−** Four adapters plus a reconciliation layer (ADR-0003) to merge overlapping observations.
- **−** Dependence on beta/undocumented surfaces; mitigated by isolating each behind a versioned parser and degrading gracefully to hooks+JSONL.
