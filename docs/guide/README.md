# Catacomb user guide

Catacomb is real-time execution-graph observability for Claude Code agentic
sessions. It runs as a loopback sidecar daemon, captures a session's prompts,
turns, tool calls, MCP calls, and subagents from four sources — hooks, native
OpenTelemetry, stream-json, and transcript JSONL — reconciles them into one
canonical action graph, persists to embedded SQLite, and serves it live through
an embedded web UI, a terminal observer, SSE, and gRPC.

## 30-second quickstart

Install:

```sh
go install github.com/realkarych/catacomb/cmd/catacomb@latest
```

Start the daemon, install hooks for the current project, and open the web UI:

```sh
catacomb up
```

Open Claude Code in the same directory and start a session. The graph appears
in the UI immediately.

## Contents

- [Getting started](getting-started.md) — install, first session, and reading content
- [Concepts](concepts.md) — the action graph, ingestion sources, and phases
- [CLI reference](cli.md) — every command, flag, and argument
- [Configuration](configuration.md) — config file, flags, environment variables, and defaults
- [Ingestion](ingestion.md) — wiring the four sources to Claude Code
- [Workflows](workflows.md) — observe, backfill, diff, checkpoints, annotations, and export
- [UI](ui.md) — web UI and terminal observer
- [Privacy and operations](privacy-and-operations.md) — security model, redaction, and troubleshooting
