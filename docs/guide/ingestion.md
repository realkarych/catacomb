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

`catacomb run` sets `CATACOMB_RUN_ID` in the child process environment, and the
forwarders send it to the daemon on the `X-Catacomb-Run-ID` header (the hook
forwarder inherits it from the child's environment). The daemon tags every
ingested event from those sessions with the run id, overriding the per-session
default, so sessions sharing one `CATACOMB_RUN_ID` are grouped into a single
run. Grouping is computed at read time from the stored observations (so it
survives a daemon restart) and shows up in `catacomb runs` and `catacomb
inspect`; query one group with `catacomb runs --run-id <id>` or
`catacomb inspect <id>`. A grouped run reports `status` as the most severe/live
state across its sessions (error, else running while any session is live, else
ok) and `ended_at` as the last session to end. An invalid run id (not matching
`[A-Za-z0-9._-]{1,256}`) is ignored and the session falls back to per-session
grouping. The child's exit code is preserved.

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

## Run labels

Labels are arbitrary `k=v` metadata attached to a run so you can group and filter later — for
example `basket=checkout` or `rep=1`. They ride the hook and stream-json ingestion paths and
are stored on the run record. OpenTelemetry spans and transcript backfill (`up --history`,
`replay`) are not labeled.

Set them with the `CATACOMB_LABELS` environment variable, a comma-separated list of `k=v`
pairs. Child processes inherit the variable, so every session launched under it is tagged:

```sh
CATACOMB_LABELS=basket=checkout,rep=1 claude --output-format stream-json <prompt>
```

`catacomb run --label` adds labels without exporting the variable yourself; repeat the flag
for multiple pairs. Flag values win per key over anything inherited from `CATACOMB_LABELS`:

```sh
catacomb run --label basket=checkout --label rep=1 -- claude --output-format stream-json <prompt>
```

Both the hook and stream-json forwarders send the resolved labels to the daemon in the
`X-Catacomb-Labels` header. When POSTing to the daemon directly, set that header yourself to
attach labels to the ingested events.

Labels are normalized before storage:

- At most 32 pairs are kept; extra pairs are dropped.
- Each key must match `[a-z0-9_.-]{1,64}`; invalid keys are dropped.
- Each value may be at most 256 bytes; longer pairs are dropped.

List and filter stored runs by label with `catacomb runs --label` (see
[cli.md](cli.md#runs)). Repeat the flag to require multiple labels — the terms are ANDed, so a
run must carry every `k=v` to match.

## Reconciliation

All four sources are merged by canonical entity precedence into one graph. Hooks provide broad event coverage; OpenTelemetry adds cost and token data with the real span tree; stream-json adds structural parent links; transcript JSONL provides the authoritative subagent tree. No source duplicates another's authoritative fields — they complement each other.

For architecture details, see [`docs/adr/`](../adr/) (ADRs 0002 and 0003). For the graph data model, see [`concepts.md`](concepts.md).
