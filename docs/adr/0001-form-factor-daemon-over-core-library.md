# ADR-0001: Form factor — daemon sidecar over a reusable core library

- **Status:** Superseded by [ADR-0026](0026-form-factor-pivot-offline-eval-gate.md)
- **Date:** 2026-06-20
- **Deciders:** @realkarych
- **Related:** spec §4, §6

## Context

Catacomb must observe one or more **live** Claude Code instances and build a real-time execution graph. Claude Code is driven two ways at once — programmatically via the **Agent SDK** and **interactively via the CLI** — and emits signal through short-lived **hook** processes, a native **OTLP** exporter, a **stream-json** stdout, and transcript **JSONL** files. Consumers want a live graph (UIs, downstream tooling) *and* durable, queryable history across many runs.

The form factor must therefore: accept signal from external, short-lived producers; hold and serve live graph state; persist across runs; and remain reusable and testable in isolation.

## Decision

Ship a long-running **daemon (sidecar)** as the primary form factor, built on a **reusable Go core library** (`catacombcore`), with the **CLI** as a thin frontend over both.

- `catacombcore` owns the model, observation log, reducer, store, and adapter/exporter interfaces — no process or transport concerns; importable and unit-testable without a live agent.
- The **daemon** hosts the adapters (hook receiver, OTLP receiver, stream-json reader, JSONL tailer), the in-memory graph, the durable store, the realtime surfaces, and the exporters.
- Hooks invoke a tiny forwarder (`catacomb hook <type>`, same binary) that POSTs to the daemon over loopback and **fails open** (never blocks the agent).

## Alternatives considered

- **Embeddable library only** — the orchestrator imports Catacomb and feeds it events. Rejected as the *primary* form: it does not fit interactive CLI runs or external short-lived hook processes, and weakens the "service alongside" multi-session story. Still available via the core lib for embedders.
- **CLI wrapper only** (`run -- claude ...`) — wraps a single invocation and dumps on exit. Rejected as primary: weak realtime, no multi-run forest. Retained as one ingestion path (stream-json tee).

## Consequences

- **+** Core logic is transport-agnostic and testable without a live agent (fixture-replay → golden graph).
- **+** One daemon serves multiple concurrent runs and both driving modes.
- **+** The daemon can expose realtime surfaces and persist a run forest.
- **−** An operational surface to run (a process, ports/sockets) versus an in-process library.
- **−** The hook forwarder adds a tiny per-event process spawn; mitigated by fail-open behavior and loopback transport.
