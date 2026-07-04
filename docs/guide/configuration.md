# Configuration

Catacomb reads settings from four layers applied in this order — later layers win:

1. Built-in defaults.
2. The config file (`~/.catacomb/config.yaml` by default; override with `--config <path>`).
3. Environment variables.
4. Command-line flags.

Pass `--config` to `catacomb daemon` to use a non-default path. (`catacomb up` starts the daemon using the default config path; it does not accept `--config` itself.)

## Config file schema

The file is YAML. All keys are optional; omit any section to accept its defaults.

```yaml
daemon:
  discovery: ""                 # discovery file path (else resolved, see below)
  reaper_window: 30m            # idle window before a run is abandoned
  max_shards: 4096              # soft cap on in-memory execution shards
  allow_payload_access: false   # enable the payload content endpoint
  allow_annotations: false      # enable the annotation write endpoint
store:
  backend: sqlite               # sqlite | memory | postgres
  sqlite:
    path: ~/.catacomb/catacomb.db
  postgres:
    dsn: ""                     # when backend: postgres
sources:
  hooks:       { enabled: true }
  otel:        { enabled: true }
  stream_json: { enabled: true }
  jsonl:
    enabled: false
    transcript_dir: ~/.claude/projects
    exclude: []                 # globs never tailed (db + cwd always excluded)
sinks:                          # zero or more live export sinks
  - type: postgres              # postgres|neo4j|otlp|jsonl
    dsn: "postgres://..."
  - type: neo4j
    uri: "bolt://localhost:7687"
    user: "neo4j"
    password: "..."
  - type: otlp
    endpoint: "grpc://host:4317"  # or http(s)://...
    project: "catacomb"
  - type: jsonl
    path: "/path/out.jsonl"
payloads:
  mode: redact                  # redact | refs | all
  max_bytes: 262144             # per-side payload cap; overflow becomes a typed ref
```

## Store backends

The default backend is **sqlite**, writing to `~/.catacomb/catacomb.db` as configured in `store.sqlite.path`. The `--db` flag overrides this path; its default is `~/.catacomb/catacomb.db` (same as `store.sqlite.path`).

The **memory** backend holds the graph only in process memory. Nothing is persisted across restarts; it is intended for testing or ephemeral use.

**postgres** as a primary store (`backend: postgres`) may not be available in all builds (`ErrBackendNotImplemented`). Use sqlite as the primary store and configure postgres as a sink instead — the sink path is fully supported and streams deltas as they happen.

## Sinks

The `sinks:` list defines live export destinations. Deltas are forwarded in real time as the graph grows. Each entry requires its sink-specific field:

- `postgres` — `dsn`
- `neo4j` — `uri`, `user`, `password`
- `otlp` — `endpoint` (`grpc://` or `http(s)://`)
- `jsonl` — `path`

Duplicates are rejected at startup. For one-shot export from the stored database, see the `catacomb export` command in [`cli.md`](cli.md).

## Payload redaction and caps

`payloads.mode` controls what payload content is persisted
([ADR-0024](../adr/0024-secrets-at-rest-write-path-redaction.md)):
`redact` (default) stores redacted payloads; `refs` stores only typed
`‹ref:len,hash›` references plus post-redaction hashes; `all` stores raw
payloads (startup warning; serve/export-time redaction still applies;
oversized sides are still capped to typed refs).
`payloads.max_bytes` (default 262144) caps each payload side after redaction;
oversized sides are replaced by a typed reference. `max_bytes: 0` means the
default 262144, not zero. `catacomb replay` always
uses the defaults (`redact`/262144). See
[Privacy and operations](privacy-and-operations.md) for the redaction rules
and the at-rest guarantees.

## Discovery file

The discovery file is a small JSON file written by the daemon so that clients — hooks, `catacomb ui`, `catacomb env`, and others — can locate the running daemon without a fixed port.

Resolution order (first path that can be written wins):

1. `$CATACOMB_DISCOVERY`
2. `$XDG_RUNTIME_DIR/catacomb/daemon.json`
3. `~/.catacomb/run/daemon.json`
4. `/tmp/catacomb/daemon.json`

The file and its parent directory are created with permissions 0600/0700. It contains: `Addr`, `Token`, `GRPCAddr`, `Pid`, `StartedAt`, `TranscriptDir`, `DBPath`, `AllowPayloadAccess`, `AllowAnnotations`. The daemon removes it on shutdown or `catacomb down`.

## Environment variables

| Variable | Effect |
| --- | --- |
| `CATACOMB_DISCOVERY` | Path to the discovery file. Clients and installed hooks read this to locate the daemon. |
| `CATACOMB_RUN_ID` | Groups multiple sessions under one run id. Set by `catacomb run --run-id` (and inherited by hooks); the forwarders send it on `X-Catacomb-Run-ID` and the daemon tags every event with it. Sessions sharing the value form one run (folded at read time): status is the most severe/live across sessions, `ended_at` is the last session to end. Must match `[A-Za-z0-9._-]{1,256}`; an invalid value falls back to per-session grouping. Query with `catacomb runs --run-id <id>`. |

The `OTEL_*` and `CLAUDE_CODE_*` variables are emitted by `catacomb env` and are covered in [ingestion.md](ingestion.md).

## Defaults

| Setting | Default | Override |
| --- | --- | --- |
| Config file | `~/.catacomb/config.yaml` | `--config` |
| Discovery file | `$XDG_RUNTIME_DIR/catacomb/daemon.json` → `~/.catacomb/run/daemon.json` → `/tmp/catacomb/daemon.json` | `$CATACOMB_DISCOVERY` / `--discovery` |
| SQLite DB | `~/.catacomb/catacomb.db` | `--db` / `store.sqlite.path` |
| Transcript tailing | off | `--transcript-dir` / `sources.jsonl` / `up --history` |
| Payload access | off (403) | `--allow-payload-access` / `daemon.allow_payload_access` |
| Annotations | off (403) | `--allow-annotations` / `daemon.allow_annotations` |
| Reaper window | 30m | `--reaper-window` / `daemon.reaper_window` |
| Max shards | 4096 | `--max-shards` / `daemon.max_shards` |
| Payload mode | `redact` | `payloads.mode` |
| Payload cap | 262144 bytes | `payloads.max_bytes` |

For the full CLI flags reference, see [`cli.md`](cli.md). For wiring ingestion sources, see [`ingestion.md`](ingestion.md).
