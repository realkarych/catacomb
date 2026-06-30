# Ingestion

Catacomb collects data from four sources and reconciles them into one canonical action graph. All four can run simultaneously; each contributes depth the others lack. See [`concepts.md`](concepts.md) for the graph model and [`docs/adr/`](../adr/) for reconciliation architecture (ADRs 0002 and 0003).

## Hooks

Hooks are the backbone ingestion path. They fire on every Claude Code event regardless of transport, making them the most reliable source for session coverage.

Install for the current project:

```sh
catacomb install-hooks
```

Install globally for all projects:

```sh
catacomb install-hooks --global
```

`catacomb up` installs hooks automatically — project-local by default, or globally with `--global`.

Each hook entry runs `CATACOMB_DISCOVERY=<path> catacomb hook <Type>` and POSTs the event payload to `/hook/{type}` on the daemon. The `catacomb hook` command reads from stdin and fails silently so it never interrupts a Claude Code session.

Covered hook events: `SessionStart`, `UserPromptSubmit`, `PreToolUse`, `PostToolUse`, `SubagentStop`, `Stop`, `SessionEnd`, `PreCompact`, `Notification`.

To remove hook entries from `settings.json`:

```sh
catacomb install-hooks --uninstall
```

Or as part of full teardown:

```sh
catacomb down --uninstall
```

## OpenTelemetry

Claude Code emits native OTLP traces that carry cost, token counts, and the real span tree. The daemon exposes an OTLP receiver at `POST /v1/traces` (protobuf over HTTP) and a gRPC endpoint.

Wire it before launching Claude Code:

```sh
eval "$(catacomb env --protocol http)"
```

Use `--protocol grpc` for gRPC transport. The `catacomb env` command reads the discovery file and emits the following variables into your shell:

- `CLAUDE_CODE_ENABLE_TELEMETRY=1`
- `OTEL_TRACES_EXPORTER=otlp`
- `OTEL_EXPORTER_OTLP_PROTOCOL` — `http/protobuf` or `grpc`
- `OTEL_EXPORTER_OTLP_ENDPOINT` — the daemon's loopback address
- `OTEL_EXPORTER_OTLP_HEADERS` — `authorization=Bearer <token>`

These variables apply to the current shell session. Re-run `eval "$(catacomb env)"` after restarting the daemon because the token and port change on each start.

## Stream-json

The stream-json source carries structural hints — in particular `parent_tool_use_id` — that help the graph builder correctly nest tool calls. The daemon receives these at `POST /v1/stream-json` (NDJSON).

Use `catacomb run` to tee a command's stream-json output to both the daemon and the terminal:

```sh
catacomb run -- claude --output-format stream-json <prompt>
```

To group multiple sessions under one run label, pass `--run-id`:

```sh
catacomb run --run-id my-experiment -- claude --output-format stream-json <prompt>
```

`catacomb run` sets `CATACOMB_RUN_ID` in the child process environment. The daemon tags all ingested events from that child with the run ID. The child's exit code is preserved.

## Transcript JSONL

Transcript tailing is the authoritative source for the subagent tree. Claude Code writes `.jsonl` transcript files under `~/.claude/projects/`; the daemon discovers and tails them incrementally. Cursors are persisted in the `tail_cursors` table so restarts do not re-ingest already-processed lines.

Enable tailing for the current session at startup:

```sh
catacomb up --history
```

Or configure permanently in the config file (see [`configuration.md`](configuration.md)):

```yaml
sources:
  jsonl:
    enabled: true
    transcript_dir: ~/.claude/projects
```

Or pass the flag directly to the daemon:

```sh
catacomb daemon --transcript-dir ~/.claude/projects
```

`up --history` is the recommended way to backfill sessions that ran before the daemon started. Paths matching globs in `sources.jsonl.exclude` (or the `--transcript-exclude` flag) are never tailed; the active database file and the current working directory are always excluded automatically.

## Reconciliation

All four sources are merged by canonical entity precedence into one graph. Hooks provide broad event coverage; OpenTelemetry adds cost and token data with the real span tree; stream-json adds structural parent links; transcript JSONL provides the authoritative subagent tree. No source duplicates another's authoritative fields — they complement each other.

For architecture details, see [`docs/adr/`](../adr/) (ADRs 0002 and 0003). For the graph data model, see [`concepts.md`](concepts.md).
