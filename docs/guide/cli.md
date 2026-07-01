# CLI reference

Every `catacomb` subcommand finds the running daemon through the discovery file
(see [configuration.md](configuration.md#discovery-file) for resolution order).
Most read-only commands accept `--json` to emit machine-readable output.
For task-oriented recipes see [workflows.md](workflows.md).

---

## Observe

### up

Start the daemon (if needed), install hooks, and open the web UI.

```sh
catacomb up [flags]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--no-open` | false | Print the UI URL instead of opening it |
| `--no-demo` | false | Skip the demo fallback when no session appears |
| `--global` | false | Install hooks into `~/.claude/settings.json` instead of `./.claude/settings.json` |
| `--history` | false | Tail `~/.claude/projects` so past sessions appear |
| `--foreground`, `-F` | false | Run the daemon attached (do not detach) |

Starts the daemon if it is not already running, installs hook entries into Claude Code
settings, and opens the web UI. By default watches the current working directory for live
sessions. `--history` backfills from the transcript directory. A synthetic demo session
loads automatically if nothing appears within a short window unless `--no-demo` is set.

```sh
catacomb up --history
```

---

### down

Stop the daemon and optionally remove catacomb artifacts.

```sh
catacomb down [flags]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--uninstall` | false | Remove catacomb hook entries from `settings.json` |
| `--purge` | false | Delete the local database and `~/.catacomb` state |
| `--all` | false | Equivalent to `--uninstall --purge` |
| `--db` | (repeatable) | Extra database file(s) to delete under `--purge` |
| `--force` | false | Send SIGKILL to a stuck daemon instead of SIGTERM |
| `--dry-run` | false | Preview what would be removed without changing anything |
| `-y`, `--yes` | false | Skip confirmation prompts (required in non-interactive shells) |
| `--json` | false | Emit a machine-readable report |

Sends SIGTERM and waits up to ~5 s; uses SIGKILL with `--force`. Removes the discovery
file. Destructive operations (`--purge`, `--uninstall`) require confirmation unless `--yes`
is passed or the shell is non-interactive.

```sh
catacomb down --all --yes
```

---

### restart

Stop the daemon, then start it again with the same configuration (transcript directory, database path).

```sh
catacomb restart [flags]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--force` | false | Send SIGKILL if the daemon does not stop cleanly |

Waits up to ~5 s for the new daemon to be ready before returning.

```sh
catacomb restart
```

---

### ui

Open the catacomb web UI in the default browser.

```sh
catacomb ui [flags]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--no-open` | false | Print the URL instead of opening it |

Requires a running daemon. Builds the URL from the discovery file's address and bearer token.
See [ui.md](ui.md) for a walkthrough of all views.

```sh
catacomb ui --no-open
```

---

### watch

Stream live graph deltas from the daemon to stdout (non-interactive SSE).

```sh
catacomb watch [flags]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--run` | (empty) | Filter deltas to a specific run ID |
| `--type` | (repeatable) | Include only nodes of these types |
| `--tier` | (repeatable) | Include only nodes at these tiers |

Prints JSON delta lines to stdout. Intended for scripting and pipeline use.

```sh
catacomb watch --run my-run-id | jq .
```

---

### status

Print daemon address, PID, uptime, and session/node counts.

```sh
catacomb status [--json]
```

Errors if no daemon is running. With `--json` emits a structured object including addr,
pid, uptime, token age, store backend, sinks, sources, reaper window, shard counts, session
and node counts, and a `healthy` field.

```sh
catacomb status --json
```

---

### observe

Interactive terminal observer for a Claude Code session.

```sh
catacomb observe [hash] [flags]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--no-color` | false | Disable ANSI colour output |

Pass an optional session hash to open that session directly; omit it to start in the
sessions list. Key bindings: `Tab` cycles focus; `j`/`k` or arrows navigate; `/` filters;
`Enter` selects; `h`/`l` collapse/expand tree nodes; `Space` toggles; `c` fetches payload
(when `--allow-payload-access` is on); `d` toggles debug; `q`/Ctrl-C quits.

```sh
catacomb observe 01HZABC123
```

---

### logs

View daemon output (reads the log file written next to the discovery file).

```sh
catacomb logs [flags]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--follow`, `-f` | false | Tail the log in real time |

```sh
catacomb logs -f
```

---

## Setup

### daemon

Run the catacomb daemon directly (receives hook events, builds the live graph).

```sh
catacomb daemon [flags]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--config` | `~/.catacomb/config.yaml` | Config file path |
| `--db` | `~/.catacomb/catacomb.db` | SQLite database path |
| `--discovery` | (from `$CATACOMB_DISCOVERY`) | Discovery file path |
| `--reaper-window` | `30m` | Idle window before a run is abandoned |
| `--max-shards` | `4096` | Soft cap on in-memory execution shards |
| `--transcript-dir` | (empty) | Directory to tail for JSONL transcripts (e.g. `~/.claude/projects`) |
| `--transcript-exclude` | (repeatable globs) | Paths to exclude from tailing (db and cwd are always excluded) |
| `--allow-payload-access` | false | Enable the payload content endpoint |
| `--allow-annotations` | false | Enable the annotation write endpoint |
| `--otlp-export-endpoint` | (empty) | Downstream OTLP export endpoint |
| `--otlp-export-project` | `catacomb` | OpenInference project name for OTLP export |
| `--postgres-export-dsn` | (empty) | PostgreSQL DSN for live sink |
| `--neo4j-export-uri` | (empty) | Neo4j bolt URI for live sink |
| `--neo4j-export-user` | (empty) | Neo4j username |
| `--neo4j-export-password` | (empty) | Neo4j password |

Usually started for you by `catacomb up`. Run directly when you need foreground control or
custom flags not exposed through `up`. Config file layering: built-in defaults → config file
→ environment → flags (flags win). See [configuration.md](configuration.md) for the full
config schema.

```sh
catacomb daemon --transcript-dir ~/.claude/projects --allow-payload-access
```

---

### install-hooks

Wire the catacomb hook forwarder into Claude Code `settings.json`.

```sh
catacomb install-hooks [flags]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--project` | false | Write `./.claude/settings.json` (project-local; default behavior when neither `--project` nor `--global` is passed) |
| `--global` | false | Write `~/.claude/settings.json` instead |
| `--uninstall` | false | Remove catacomb hook entries |

Registers hook commands for: `SessionStart`, `UserPromptSubmit`, `PreToolUse`,
`PostToolUse`, `SubagentStop`, `Stop`, `SessionEnd`, `PreCompact`, `Notification`.
Each entry runs `CATACOMB_DISCOVERY=<path> catacomb hook <Type>`. See
[ingestion.md](ingestion.md) for wiring details.

```sh
catacomb install-hooks --global
```

---

### env

Print OTLP environment variables for connecting to the running daemon.

```sh
catacomb env [flags]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--discovery` | (from `$CATACOMB_DISCOVERY`) | Discovery file path |
| `--protocol` | `http` | Transport: `http` or `grpc` |

Outputs `CLAUDE_CODE_ENABLE_TELEMETRY=1`, `OTEL_TRACES_EXPORTER=otlp`,
`OTEL_EXPORTER_OTLP_PROTOCOL`, `OTEL_EXPORTER_OTLP_ENDPOINT`, and
`OTEL_EXPORTER_OTLP_HEADERS=authorization=Bearer <token>`. Designed for eval with a
subshell.

```sh
eval "$(catacomb env --protocol grpc)"
```

---

## Advanced

### hook

Forward a Claude Code hook event to the daemon (reads payload from stdin, POSTs to daemon).

```sh
catacomb hook <type>
```

Internal command used by the hook entries that `install-hooks` writes. Fails silently to
never disrupt Claude Code.

---

### mark

Record a phase boundary marker in a running session.

```sh
catacomb mark --session <id> --name <name> --boundary <start|end> [flags]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--session` | **required** | Session hash to annotate |
| `--name` | **required** | Phase name |
| `--boundary` | **required** | `start` or `end` |
| `--occurrence` | `0` | Occurrence index for repeated phases with the same name |
| `--state-ref` | (empty) | Opaque state reference stored on the marker node |

POSTs to `/v1/mark`. See [workflows.md](workflows.md) for the checkpoints workflow and
`--phase` selector syntax (`name` or `name,occurrence`).

```sh
catacomb mark --session 01HZABC123 --name eval-loop --boundary start
```

---

### ingest stream-json

Read NDJSON from stdin and forward it to the daemon.

```sh
catacomb ingest stream-json
```

Advanced/internal command used by `catacomb run` to pipe a child process's stream-json
output. Prefer `catacomb run` for interactive use.

---

### run

Run a Claude Code command, tee its stream-json to the terminal and the daemon.

```sh
catacomb run [flags] -- <cmd...>
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--run-id` | (empty) | Sets `CATACOMB_RUN_ID` for the child process (multi-session grouping) |
| `--label` | (repeatable) | `k=v` label recorded on the run; adds to `CATACOMB_LABELS`, flag wins per key |

Exits with the child's exit code. The child inherits `CATACOMB_DISCOVERY` from the
environment so hooks also fire. Labels passed with `--label` merge over any inherited from
`CATACOMB_LABELS` and are carried to the daemon on every event from that child. See
[ingestion.md](ingestion.md#run-labels) for label rules and caps.

```sh
catacomb run --run-id sprint-42 --label basket=checkout -- claude --model claude-opus-4-5 "refactor the auth module"
```

---

### replay

Build a graph from a recorded Claude Code transcript (no daemon or UI required).

```sh
catacomb replay <transcript.jsonl> [flags]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--db` | `~/.catacomb/catacomb.db` | SQLite database path to write into |
| `--export-jsonl` | (empty) | Also write a JSONL snapshot to this path |

Standalone offline backfill. For live history tailing use `catacomb up --history` instead.

```sh
catacomb replay ~/.claude/projects/my-project/session.jsonl --db archive.db
```

---

### diff

Diff two session transcripts by `step_key`.

```sh
catacomb diff <A.jsonl> <B.jsonl> [flags]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--json` | false | Emit JSON output |
| `--phase` | (empty) | Scope both sides to this phase (`name` or `name,occurrence`) |
| `--a-phase` | (empty) | Scope side A to this phase |
| `--b-phase` | (empty) | Scope side B to this phase |
| `--a-from` | (empty) | Range start phase for side A |
| `--a-to` | (empty) | Range end phase for side A |
| `--b-from` | (empty) | Range start phase for side B |
| `--b-to` | (empty) | Range end phase for side B |

Reports added, removed, changed, and unchanged steps with per-field deltas (args, status,
cost, duration, tokens). A within-run phase comparison uses the same session on both sides
with different `--a-phase`/`--b-phase` selectors. See [workflows.md](workflows.md) for the
checkpoints + diff recipe.

```sh
catacomb diff run-a.jsonl run-b.jsonl --phase eval-loop --json
```

---

### subgraph

Extract the execution subgraph of a checkpoint phase from a session transcript.

```sh
catacomb subgraph <session.jsonl> [flags]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--phase` | (empty) | Phase selector (`name` or `name,occurrence`); mutually exclusive with `--from`/`--to` |
| `--from` | (empty) | Range start phase |
| `--to` | (empty) | Range end phase |
| `--json` | false | Emit `{nodes, edges}` JSON instead of summary lines |

Prints `nodes: N  edges: M` followed by node lines, or structured JSON with `--json`.

```sh
catacomb subgraph session.jsonl --phase eval-loop --json
```

---

### demo

Ingest the bundled synthetic transcript into the running daemon.

```sh
catacomb demo
```

Prints the demo session ID and the UI link. Useful for verifying the daemon and UI are
working before any real Claude Code sessions have run.

```sh
catacomb demo
```

---

### runs

List all runs stored in the catacomb database.

```sh
catacomb runs [flags]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--db` | `~/.catacomb/catacomb.db` | Database path to read from |
| `--json` | false | Emit JSON output |
| `--label` | (repeatable) | `k=v` selector; keep only runs matching every term (AND) |

Outputs a table (or JSON) of runs with status, start time, tool counts, token counts, and
cost. Repeat `--label` to narrow the listing to runs carrying all of the given labels; the
JSON output includes each run's `labels` object. See
[ingestion.md](ingestion.md#run-labels) for how labels are attached.

```sh
catacomb runs --db ~/.catacomb/catacomb.db --json
catacomb runs --label basket=checkout --label rep=1 --json
```

---

### snapshot

Dump the current graph state as JSONL.

```sh
catacomb snapshot [flags]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--db` | `~/.catacomb/catacomb.db` | Database path to read from |
| `--run` | (empty) | Filter to a specific run ID |
| `--out` | (empty) | Output file path (stdout if not set) |

Emits node, edge, and run JSONL records.

```sh
catacomb snapshot --db ~/.catacomb/catacomb.db --out graph.jsonl
```

---

### inspect

Show a detailed summary for a specific run.

```sh
catacomb inspect <run_id> [flags]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--db` | `~/.catacomb/catacomb.db` | Database path to read from |
| `--json` | false | Emit JSON output |

Includes status, start time, node and tool counts, tokens, cost, and a per-type node
breakdown.

```sh
catacomb inspect 01HZRUN456 --json
```

---

### export

Export graph data to an external sink.

```sh
catacomb export [flags]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--db` | `~/.catacomb/catacomb.db` | Database path to read from |
| `--to` | (empty) | Sink type: `jsonl`, `otlp`, `neo4j`, `postgres`, `agentevals`, `evalview` |
| `--run` | (empty) | Filter to a specific run ID |
| `--mode` | (empty) | `materialized` (default) or `events` (raw observations, jsonl only) |
| `--out` | (empty) | Output file path for `jsonl`, `agentevals`, `evalview` sinks |
| `--otlp-export-endpoint` | (empty) | OTLP endpoint URL |
| `--postgres-export-dsn` | (empty) | PostgreSQL connection DSN |
| `--neo4j-export-uri` | (empty) | Neo4j bolt URI |
| `--neo4j-export-user` | (empty) | Neo4j username |
| `--neo4j-export-password` | (empty) | Neo4j password |

One-shot export from the stored database. For streaming export as events arrive, configure
`sinks:` in the config file instead (see [configuration.md](configuration.md)). The `otlp`
sink refuses to export to the daemon's own address to prevent loops.

```sh
catacomb export --to jsonl --out graph.jsonl
catacomb export --to postgres --postgres-export-dsn "postgres://user:pass@localhost/db"
```

---

### version

Print the version string.

```sh
catacomb version
```
