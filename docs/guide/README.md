# Catacomb user guide

Catacomb is real-time execution-graph observability for Claude Code agentic
sessions. It runs as a loopback sidecar daemon, captures a session's prompts,
turns, tool calls, MCP calls, and subagents from four sources — hooks, native
OpenTelemetry, stream-json, and transcript JSONL — reconciles them into one
canonical action graph, persists to embedded SQLite, and serves it live through
SSE and gRPC.

## 30-second quickstart

Install:

```sh
go install github.com/realkarych/catacomb/cmd/catacomb@latest
```

Start the daemon and install hooks for the current project:

```sh
catacomb up
```

Open Claude Code in the same directory and start a session. The daemon captures
the graph as the session runs; `catacomb status` shows session and node counts.
Watching runs live in a UI is delegated to a vendor substrate such as Phoenix
([ADR-0026](../adr/0026-form-factor-pivot-offline-eval-gate.md) §2).

## Contents

- [Getting started](getting-started.md) — install, first session, and reading content
- [Concepts](concepts.md) — the action graph, ingestion sources, and phases
- [CLI reference](cli.md) — every command, flag, and argument
- [Configuration](configuration.md) — config file, flags, environment variables, and defaults
- [Ingestion](ingestion.md) — wiring the four sources to Claude Code
- [Workflows](workflows.md) — capture, backfill, diff, checkpoints, regression gating, annotations, and export
- [Privacy and operations](privacy-and-operations.md) — security model, redaction, and troubleshooting
