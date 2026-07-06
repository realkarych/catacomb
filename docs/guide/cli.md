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

### mcp

Run the catacomb MCP server over stdio (JSON-RPC 2.0, newline-delimited). It exposes a single
`mark` tool so an in-run agent can record phase checkpoints without any hand-rolled stub.

```sh
catacomb mcp
```

Takes no flags or arguments; it reads requests from stdin and writes responses to stdout, and
exits when stdin closes (or on `SIGINT`/`SIGTERM`). The `mark` tool takes:

| Field | Required | Meaning |
| --- | --- | --- |
| `name` | **required** | Phase name |
| `boundary` | **required** | `start` or `end` |
| `occurrence` | optional int | Occurrence index for repeated same-name phases |
| `state_ref` | optional | Opaque state reference stored on the marker node |

Wire it into Claude Code with `--mcp-config` so the agent can call the `mcp__catacomb__mark`
checkpoint tool during a run — the server named `catacomb` exposing `mark` surfaces as
`mcp__catacomb__mark`:

```json
{"mcpServers":{"catacomb":{"command":"catacomb","args":["mcp"]}}}
```

Pass `--strict-mcp-config` alongside it (as the bench cells do) so only the catacomb server is
loaded and no ambient MCP config leaks in. The tool is a pure acknowledgement — it never contacts
the daemon; the marker is synthesized from the tool-call input on the trace stream, so it needs no
configuration and fails open. See [workflows.md](workflows.md#placing-markers) for the
checkpoints workflow.

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
| `--run-id` | (empty) | Sets `CATACOMB_RUN_ID` for the child process (multi-session grouping); validated as `[A-Za-z0-9._-]{1,256}`, invalid values are rejected before the child starts |
| `--label` | (repeatable) | `k=v` label recorded on the run; adds to `CATACOMB_LABELS`, flag wins per key |

Exits with the child's exit code. The child inherits `CATACOMB_DISCOVERY` from the
environment so hooks also fire. Labels passed with `--label` merge over any inherited from
`CATACOMB_LABELS` and ride the child's hook and stream-json events to the daemon; OpenTelemetry
spans and transcript backfill are not labeled. See
[ingestion.md](ingestion.md#run-labels) for label rules and caps.
The child's terminal stream-json `result` event finalizes the run as the child exits: run
status becomes `ok` (or `error` when the result reports `is_error`), `ended_at` is set, and
run-scope `duration_ms` flows into `runs`/`regress` — no hooks required. Hooks are still worth
installing for richer capture: authoritative start/end timing (hook observations outrank
stream-json ones), tool payload precedence, permission-deny (`blocked`) statuses, and
compaction/notification markers. When both fire, the run ends once — hook timing wins and the
second end signal is a no-op. The idle reaper remains the fallback for streams that die before
`result` (such runs end `abandoned` after the quiescence window).

A run spanning multiple sessions is grouped by run id: the forwarders send `CATACOMB_RUN_ID`
on the `X-Catacomb-Run-ID` header and the daemon tags every event with it, so sessions sharing
one run id fold into a single run at read time (`status` = most severe/live across sessions,
`ended_at` = last session to end). Query the group with `catacomb runs --run-id <id>` or
`catacomb inspect <id>`. Bench cells are single-session, so `bench` and `regress` are
unaffected by the fold.

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
| `--run-id` | (empty) | Keep only the run with this exact run id (validated `[A-Za-z0-9._-]{1,256}`); resolves a `bench` cell's manifest run id to its stored run |

Outputs a table (or JSON) of runs with status, start time, tool counts, token counts, and
cost. Cost prefers the session total reported by Claude Code itself (the stream-json
`result` event; `cost_source: reported`) and falls back to summing per-turn
estimates when no reported total exists. Token totals always come from per-turn
usage — the cumulative `result` record is never double-counted. Databases
written by older catacomb versions are re-summarized correctly on read; no
migration is needed. Repeat `--label` to narrow the listing to runs carrying all of the given labels; the
JSON output includes each run's `labels` object. See
[ingestion.md](ingestion.md#run-labels) for how labels are attached. Pass `--run-id <id>` to keep
only that one run; sessions sharing a `CATACOMB_RUN_ID` are already folded into a single row, and a
`bench` cell's manifest `run_id` (e.g. `bench-<basket>-<task>-<variant>-r<rep>`) resolves here.
`--run-id` and `--label` combine with AND: a run is kept only when it matches the given run id and
carries every requested label.

```sh
catacomb runs --db ~/.catacomb/catacomb.db --json
catacomb runs --label basket=checkout --label rep=1 --json
catacomb runs --run-id sprint-42 --json
```

---

### bench

Run a benchmark basket: expand tasks × variants × reps into cells, execute each through
`catacomb run`, mark task phases, and record a manifest.

```sh
catacomb bench <basket.yaml> [flags]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--manifest` | `<basket>.manifest.jsonl` | Manifest output path |
| `--resume` | false | Skip cells already recorded in the manifest |
| `--fail-fast` | false | Stop at the first failing cell |
| `--dry-run` | false | Print the cell expansion table and exit without executing |
| `--offline` | false | Run daemonless: read Claude transcripts and write evidence dirs (see [Offline mode](#offline-mode)) |
| `--projects-dir` | `~/.claude/projects` | Claude projects directory holding session transcripts (`--offline` only) |
| `--runs-dir` | `~/.catacomb/runs` | Evidence output directory for `--offline` runs |

A basket is a declarative YAML file. `tasks × variants × reps` expands to one *cell* per
combination, and cells run sequentially:

```yaml
basket: checkout
reps: 5
tasks:
  - id: add-item
    cmd: ["claude", "-p", "add an item to the cart"]
    dir: services/cart          # optional working directory
    env: { MODE: fast }         # optional per-task env
    checkpoints: [plan, tests.pass]   # optional declared phases to verify
variants:
  - id: baseline
    env: { MODEL: opus }        # optional per-variant env (wins over task env)
  - id: candidate
    env: { MODEL: sonnet }
    setup: ["git checkout feature"]   # optional pre-cell commands
```

Each cell runs under run-id `bench-<basket>-<task>-<variant>-r<rep>` and carries the labels
`basket`, `task`, `variant`, and `rep`, so `runs`, `baseline`, and `regress` selectors work
unchanged. Cell labels win over any inherited from `CATACOMB_LABELS`. The basket name and each task and
variant `id` must match `^[A-Za-z0-9._-]+$` (no spaces, commas, or `=`, which would corrupt
`CATACOMB_LABELS` and the epilogue selectors) and be at most 256 bytes; task and variant `id`s
must be unique, and baskets whose dash-joined ids would collide into the same run-id are
rejected at load.

For each cell the runner emits `task:<id>` start/end phase markers around the child, on a
best-effort basis — it needs a `session_id` in the child's stream-json to place them, and each
manifest entry records whether the markers landed (`marked: true|false`). These `task:<id>`
phases give `regress` a stable checkpoint axis even when the agent forgets to mark its own. Each
marker POST is synchronous with a bounded 2s timeout, so a slow or down daemon adds up to ~4s
per cell (start plus end) before the run moves on.

A task may also declare `checkpoints:` — phase names the agent is expected to mark *itself*
(via `mcp__catacomb__mark`, per a CLAUDE.md convention) during the run. Each name must match
`^[A-Za-z0-9._:-]+$` (the colon is allowed here, unlike task and variant `id`s), be at most 256
bytes, be unique within the task, and may not equal the reserved runner marker `task:<id>`.
After each cell — once it has observed a `session_id` — the runner fetches the finalized session
graph and checks which declared checkpoints are present as markers. Verification is best-effort
and **never gates**: it is skipped when the cell surfaced no session id or the graph fetch fails
(the skip reason is recorded in the manifest `note`). Checkpoints that are absent from the graph
are recorded in the manifest's `missing_checkpoints` list, warned to stderr as
`cell <run-id>: missing checkpoints: <names>`, and rolled up on success — just before the
copy-pasteable epilogue below — into one `checkpoints[<task>]: <name> <hit>/<verified>` summary
line per declared name, where `hit` counts the cells the marker was found in and `verified`
counts the cells where verification actually ran. Because the marks ride the same stream tee as
the child's events, a lossy or failed stream forward can surface as a FALSE miss — the
`catacomb stream-json:` stderr warning is the correlating signal — while `regress` presence rates
over reps absorb the occasional loss. Missing phases are visibility only here; they
earn a verdict downstream as presence-rate drops in `regress`.

The manifest is JSONL, written incrementally — one object per completed cell (run-id, task,
variant, rep, exit code, session id, `marked`, an optional `missing_checkpoints` list, basket
hash, finish time, and an optional `note`). `--resume` reads it back and skips cells already
present; if the basket file changed since the recorded run (its content hash no longer matches)
resume errors out — delete the manifest or revert the basket.

`setup` commands run before **every** cell, in the task's working directory, as **plain `exec`**:
each line is split on whitespace and run directly, with **no shell** — pipes, redirects, `&&`,
quoting, variable expansion, and globbing are not interpreted. Wrap a script if you need shell
features. Setup inherits **only the parent process environment** — not the task or variant
`env`, and not `CATACOMB_RUN_ID`/`CATACOMB_LABELS` — and because it re-runs before each cell it
must be **idempotent** (a `git checkout <branch>` is fine; an `echo >> file` that accumulates is
not).

Without `--offline`, `bench` requires a running daemon: at start (except for `--dry-run`) it
reads the discovery file and pings `/healthz`, exiting `2` with a hint to `catacomb up` if the
daemon is missing or unreachable. If the manifest already has entries and you pass neither `--resume` nor a fresh
`--manifest`, `bench` refuses (exit `2`) rather than silently appending a second run's cells.

Cells finalize from the child's stream `result` event, so run status, `ended_at`, and run-scope
`duration_ms` work without hooks. Installing hooks (project-local is enough) still sharpens end
timing and adds permission and notification capture.

A failing cell is recorded and the basket continues (deciding whether a change regressed is
`catacomb regress`'s job, not the runner's). Exit codes: `0` every cell ran (even if some cells
failed), `1` `--fail-fast` stopped at a failing cell, `2` operational error (bad basket,
unreachable daemon, a non-fresh manifest, manifest I/O, or a resume hash mismatch). On success
the runner prints a `marked <n>/<total> cells` summary and a copy-pasteable epilogue: a
`baseline set` for the first variant and, with two or more variants, a `regress` comparing the
first two. Append `,task=<id>` to the epilogue's `label:` selectors to narrow the comparison to
a single task (e.g. `label:basket=checkout,variant=baseline,task=add-item`). When the basket
declares `reps < 5`, the epilogue also appends a one-line note recommending `reps: 5` or more,
because the rate gate cannot fire reliably below that (see
[Gate sensitivity at small k](workflows.md#gate-sensitivity-at-small-k)).

#### Offline mode

`--offline` runs the same basket with no daemon at all: no discovery file, no `/healthz`
preflight, no stream forward, no marker POSTs. The child runs as a plain local process while
the runner peeks its stdout for the stream-json `session_id` and the terminal `result` event's
`total_cost_usd` — the task `cmd` must emit stream-json (`claude -p <prompt> --output-format
stream-json`), exactly as the daemon path already needs for the session id. After the child
exits the runner resolves the session's transcripts under `--projects-dir` — the main
`<session-id>.jsonl` plus any `subagents/agent-*.jsonl` — retrying for up to ~3 s while the
file lands; a session id matching no transcript (or more than one) records the reason in the
cell's manifest `note` and skips verification and evidence for that cell.

The `task:<id>` phase markers are synthesized from the child's wall-clock start and end instead
of being POSTed, so a slow daemon can no longer add ~4 s per cell, and declared `checkpoints:`
are verified in-process against the graph rebuilt from the transcripts — same
`missing_checkpoints` manifest field, stderr warnings, and `checkpoints[<task>]` rollup as the
daemon path, minus the lossy-stream false-miss caveat, since the transcript is read directly.
Each cell then writes an evidence directory `<runs-dir>/<run-id>/` holding secret-redacted
copies of the transcripts (`session.jsonl`, `subagents/agent-*.jsonl`) plus a `meta.json` (run
id, task, variant, rep, session id, labels, exit code, `cost_usd`, basket hash, the
`task:<id>` marker window, and `finished_at`); the manifest entry gains `cost_usd` (from the
`result` event) and `evidence_dir`. An evidence-write failure keeps the cell's result and
notes the error. On success the epilogue prints a copy-pasteable
[`regress --runs-dir`](#regress) comparing the first two variants over these evidence
directories; pin the golden variant by name with [`baseline set --runs-dir`](#baseline-set)
when the comparison should survive label churn. When the home
directory cannot be resolved, `--projects-dir` and `--runs-dir` must be set explicitly (exit
`2`). Offline mode is the PV-1 slice of ADR-0026; offline baselines, recorded history, and
`--scores` gating (PV-2) are covered under [regress](#regress).

```sh
catacomb bench checkout.yaml
catacomb bench checkout.yaml --dry-run
catacomb bench checkout.yaml --resume --fail-fast
catacomb bench checkout.yaml --offline
```

---

### baseline set

Create or replace a named baseline from a label selector, resolved now — against the store, or
against offline evidence dirs with `--runs-dir`.

```sh
catacomb baseline set <name> --label k=v [--label ...] [flags]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--label` | **required** (repeatable) | `k=v` selector; the baseline captures every run matching all terms (AND) |
| `--db` | `~/.catacomb/catacomb.db` | SQLite database path |
| `--runs-dir` | (none) | Resolve the selector from [`bench --offline`](#bench) evidence dirs instead of the store |

Resolves the label selector immediately, sorts the matching run IDs, and persists them with the
selector, per-run repro fingerprints, a created-at timestamp, and the version stamps of the
setting binary — the catacomb version and the step-key scheme — under `<name>`; `regress`
checks those stamps whenever the baseline is resolved by name (see
[Baseline version stamps](#baseline-version-stamps)). The name must be
non-empty, at most 128 bytes, and free of leading or trailing whitespace; at least one `--label`
is required. Requires read-write store access; errors when the selector matches no runs, and —
without `--runs-dir` — when the store does not exist. Re-running with the same name replaces the
stored baseline. A saved baseline is
referenced by `regress` as `name:<baseline>`, so a golden group survives later label churn.

With `--runs-dir` the selector resolves offline instead: the command scans
`<runs-dir>/*/meta.json`, matches the labels against each run's recorded labels (all terms
ANDed), and pins the matching run IDs — no daemon involved, and the store at `--db` is created
if it does not exist yet, so the very first offline command can be this one. The baseline also
records the runs dir it was set from; the evidence dirs are not copied, so
[`regress --runs-dir`](#comparing-offline-evidence) re-reads the pinned runs from disk and warns
when pointed at a different directory. Offline baselines carry no per-run repro fingerprints
(those exist only for store-resolved runs).

```sh
catacomb baseline set golden --label basket=checkout --label variant=main
catacomb baseline set golden --label basket=checkout,variant=main --runs-dir ~/.catacomb/runs
```

---

### baseline list

List stored baselines.

```sh
catacomb baseline list [flags]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--db` | `~/.catacomb/catacomb.db` | SQLite database path |
| `--json` | false | Emit JSON output |

Prints a table with columns `NAME`, `RUNS`, `SELECTOR`, `CREATED`, sorted by name; `SELECTOR`
shows the sorted `k=v` terms and `CREATED` is a UTC RFC3339 timestamp. `--json` emits the stored
records including each baseline's resolved run IDs, a `repro` field mapping run IDs to the
repro fingerprints captured at `baseline set` time, the `runs_dir` an offline baseline was
resolved from, and the version `stamps` (catacomb version and step-key scheme) recorded at set
time. On a store created by an older binary
(schema v1) this command fails with a hint to run a write-path command (`catacomb up` or
`baseline set`) to migrate the schema.

```sh
catacomb baseline list --json
```

---

### baseline rm

Remove a stored baseline.

```sh
catacomb baseline rm <name> [flags]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--db` | `~/.catacomb/catacomb.db` | SQLite database path |

Deletes the named baseline. Requires read-write store access.

```sh
catacomb baseline rm golden
```

---

### regress

Compare a candidate run group against a baseline and gate on the verdict.

```sh
catacomb regress --baseline <selector> --candidate <selector> [flags]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--baseline` | (empty) | Baseline selector: `label:k=v[,k=v...]` or `name:<baseline>` |
| `--candidate` | (empty) | Candidate selector (same grammar) |
| `--db` | `~/.catacomb/catacomb.db` | SQLite database path |
| `--runs-dir` | (none) | Resolve selectors from [`bench --offline`](#bench) evidence dirs instead of the store; `name:` baselines and `--record` still use `--db` (see [Comparing offline evidence](#comparing-offline-evidence)) |
| `--json` | false | Emit the full report as JSON |
| `--strict` | false | Treat an insufficient-data verdict as a failure (exit `1`); refuse a stampless or stamp-mismatched `name:` baseline (exit `2`) |
| `--record` | false | Append this comparison to the baseline's history for [`trends`](#trends) (requires `--baseline name:<x>`) |
| `--annotation` | (none) | Numeric annotation to gate on: `owner.key[:higher-better\|lower-better]` (repeatable) |
| `--scores` | (none) | JSONL file of external scores applied as node annotations before comparison (see [Gating on external scores](#gating-on-external-scores)) |
| `--min-support` | 3 | Minimum runs per group for a trusted comparison (must be ≥ 1) |
| `--presence-delta` | 0.2 | Presence-rate delta threshold |
| `--error-delta` | 0.1 | Error-rate delta threshold |
| `--metric-rel-delta` | 0.25 | Relative metric delta threshold |
| `--iqr-factor` | 1.5 | IQR band factor for the metric noise band |
| `--coverage-floor` | 0.7 | Step-alignment coverage below which step verdicts are downgraded |
| `--z` | 1.645 | One-sided Wilson z for the rate gates (`1.645` = 95% one-sided); higher z requires stronger evidence to flag (flags less) |
| `--fail-on-notable` | false | Count `notable` findings toward the gate (exit `1`) |

Both selectors must be supplied. `label:k=v[,k=v...]` resolves runs by label (all terms ANDed);
`name:<baseline>` resolves a baseline saved with `baseline set`. When a `name:` baseline resolves
fewer runs than it stored (some runs were pruned since `set`), a warning is printed to stderr —
stdout and `--json` output stay clean. Groups are aggregated and compared per
[ADR-0022](../adr/0022-regression-detection-over-repeated-runs.md) §4:

- **Rates** (presence, error) use one-sided Wilson bounds (default z `1.645`, tunable with
  `--z`) and are flagged as a `regression` only when the baseline and candidate bounds are
  disjoint *and* the delta exceeds the threshold; a delta over the threshold with overlapping
  bounds is reported as `notable`, which gates only under `--fail-on-notable`. When even a
  maximal flip at the actual group sizes cannot reach `regression`, the report (human and
  `--json`) carries a `sensitivity:` note naming the smallest `k` at which the gate could fire;
  see [Gate sensitivity at small k](workflows.md#gate-sensitivity-at-small-k).
- **Metrics** (`duration_ms`, `cost_usd`, `tokens_in`, `tokens_out`, `occurrences`; run totals
  also `nodes`) flag the candidate median when it falls outside the baseline median ±
  `max(metric-rel-delta × |median|, iqr-factor × IQR)` band. The `nodes` and `occurrences` count
  metrics are one-sided (higher = flagged) per
  [ADR-0022 Amendments](../adr/0022-regression-detection-over-repeated-runs.md#amendments), so a
  pipeline that legitimately grew may need `--metric-rel-delta` raised to keep ordinary growth
  inside the band.
- **Alignment coverage** (fraction of baseline steps matched in the candidate) is always
  reported; below `--coverage-floor` step-level regressions are downgraded to `notable` and the
  checkpoint (phase) level carries the verdict (under `--fail-on-notable` those downgraded
  findings still gate).
- Groups below `--min-support` yield an `insufficient` verdict instead of a guess.

#### Comparing offline evidence

`--runs-dir` resolves the run groups from [`bench --offline`](#bench) evidence directories
instead of the store — no daemon; the database at `--db` is touched only to look up `name:`
baselines and to append `--record` history. It scans
`<runs-dir>/*/meta.json`, matches `label:` selectors against each run's recorded labels (all
terms ANDed), and rebuilds every matching run's graph from its redacted transcripts,
re-applying the `task:<id>` marker window from `meta.json` so checkpoint phases and run timing
carry over. Aggregation, thresholds, scopes, output, and exit codes are unchanged.

`name:` selectors resolve offline too: the baseline row is read from the `--db` baselines table
(read-only) and its pinned run IDs load from `<runs-dir>/<run-id>/` — every pinned run's
evidence dir must be present and readable, or the command exits `2` naming the run and dir. A
baseline set with [`baseline set --runs-dir`](#baseline-set) records the runs dir it was
resolved from; when the `--runs-dir` flag names a different directory, a stderr warning notes
the recorded dir and the flag wins. `--record` appends to the same store, opening it read-write
(see [Recording history](#recording-history)). Annotation gates
(`--annotation`) fire under `--runs-dir` when their values arrive through a
[`--scores` file](#gating-on-external-scores); evidence dirs carry no store-written
annotations, so a gated key without a scores file still only triggers the never-fired stderr
warning.

#### Baseline version stamps

`baseline set` stamps every baseline with the versions that resolved it: the catacomb version
and the step-key scheme (`stepkey/v1`). Whenever `--baseline` resolves by `name:` — with or
without `--runs-dir` — `regress` compares those stamps against its own: a baseline with no
stamps (set by a pre-stamp catacomb) or with differing stamps prints a stderr warning, and
under `--strict` is refused as an operational error (exit `2`) instead. A step-key scheme
change shifts step identity between the pinned runs and the candidate, so a cross-version
comparison can quietly align nothing; after upgrading catacomb, re-run `baseline set` to re-pin
the group under the current stamps. The check covers the `--baseline` side only (`label:`
selectors carry no stamps, and a `name:` candidate is not checked), and stdout and `--json`
stay clean either way.

#### Gating on external scores

`--annotation owner.key` folds a numeric annotation — an external scorer's verdict written back
onto the graph (see [Annotations](workflows.md#annotations)) or supplied from a `--scores` file
(below) — into the comparison as if it were a
built-in metric. The annotation values aggregate per `step_key` and are flagged with the same
median ± `max(metric-rel-delta × |median|, iqr-factor × IQR)` band as other metrics, but with a
declared direction. When the same `step_key` occurs more than once within a single run, that
run's annotation values for the key are **summed** (like cost and tokens), and the compared
medians are taken across the per-run sums:

- `owner.key` or `owner.key:higher-better` (default): a higher score is better, so a candidate
  median that drops below the band is the `regression` and a rise is an `improvement`.
- `owner.key:lower-better`: a lower score is better, so the direction inverts — a rise is the
  `regression`.

The flag is repeatable and each key gates independently; a duplicate key (across flags or
directions) is an operational error (exit `2`). Annotation gating is **step-scoped only** per
[ADR-0022 Amendments](../adr/0022-regression-detection-over-repeated-runs.md#amendments): phase
rows carry no annotation block. Because a score is only sampled on the runs that actually carry
it, an annotation's `N` can be below the step's `Present` (a step reached in every run but scored
in only some); an annotation whose `N` falls below `--min-support`, or one present on only one
side, is reported `insufficient` rather than guessed. A configured key that never fires on any
step in either group prints a warning to stderr (stdout and `--json` stay clean).

`--scores file.jsonl` supplies those values from a file instead of the store — the only way
annotations reach a `--runs-dir` comparison, and equally usable against the store. Each line is
one JSON object:

```json
{"step_key": "1f0c9a4b2d8e7f36", "key": "deepeval.tool_correctness", "value": 0.92, "run_id": "bench-checkout-work-task-candidate-r1"}
```

`step_key` names the step the score lands on (take it from the `KEY` column of a `regress`
table, or from [`subgraph --json`](#subgraph) node output), `key` uses the same `owner.key`
grammar as `--annotation`, `value` must be a number, and `run_id` is optional: set it to score
a single run — one line per scored run is the normal shape — or omit it and the value lands on
every run in **both** groups that carries the step key, which flattens both medians to the same
value. Blank lines are skipped; a malformed line is an operational error (exit `2`) naming the
file and line. Values apply in memory to both groups before aggregation — nothing is written
back to the store or the evidence dirs — and they overwrite a store-written annotation under
the same key on the nodes they match. Entries that match no node are counted into a single
stderr warning (`N score entries matched no node`); stdout and `--json` stay clean. The file
only supplies values: each key still needs its `--annotation owner.key[:direction]` flag to
declare a gate, or the scores are inert.

Comparison runs at three scopes — run totals, checkpoint phases, and steps. The human table
prints `VERDICT SCOPE KEY NAME METRIC BASELINE CANDIDATE BAND` with presence-normalized values
(presence rate, not absence); `--json` emits the full report (presence rows carry absence rates
plus a clarifying `detail` field). Exit codes: `0` pass, `1` regression (or `insufficient` with
`--strict`), `2` operational error (invalid selector, unknown baseline, missing store, empty
group, a [stamp refusal](#baseline-version-stamps) under `--strict`, or `--min-support` below
1). Resolving a `name:` baseline on a store created by an older
binary (schema v1) also exits `2` with a hint to run a write-path command (`catacomb up` or
`baseline set`) to migrate the schema.

#### Recording history

`--record` appends the full comparison — candidate selector, thresholds, annotation specs, and the
complete report — to the named baseline's append-only history, replayable later with
[`trends`](#trends). It requires `--baseline name:<baseline>` (a `label:` group has no stable
identity to append under, so `--record` with a `label:` baseline is an operational error) and opens
the store read-write, migrating an older schema in the process. The record is appended *after* the
verdict is rendered to stdout, and a failed append is itself an operational error (exit `2`) that
takes precedence over the verdict: a regression that could not be durably recorded exits `2`, not
`1`, so a broken store never masquerades as a clean regression signal.

Recording works under `--runs-dir` too: the store at `--db` is opened read-write solely to
write the record, so a fully offline loop still accumulates [`trends`](#trends) history. The
store must already exist: `--record` requires a `name:` baseline, and resolving one against an
absent store fails first (exit `2`), so in an offline loop the store is created by
[`baseline set --runs-dir`](#baseline-set), never by `--record`. Each record carries the
version stamps of the recording binary (catacomb version and step-key scheme) in its body
alongside the report.

Sequence numbers are assigned atomically in a single statement, so a record is never silently
overwritten. But concurrent `--record` writers against one store file — a fan-out CI matrix whose
shards all record into the same database — can still collide on SQLite's write lock: a losing writer
fails loudly with `SQLITE_BUSY` and exits `2` without corrupting the history, rather than blocking or
tearing a record. Serialize the recorders (record from one shard, or gate on a lock) or give each
shard its own store file.

```sh
catacomb regress --baseline name:golden --candidate label:basket=checkout,variant=candidate
catacomb regress --baseline label:variant=main --candidate label:variant=candidate --json
catacomb regress --baseline name:golden --candidate label:variant=candidate \
  --annotation deepeval.tool_correctness
catacomb regress --baseline name:golden --candidate label:variant=candidate --record
catacomb regress --runs-dir ~/.catacomb/runs \
  --baseline label:basket=checkout,variant=main --candidate label:basket=checkout,variant=candidate
catacomb regress --runs-dir ~/.catacomb/runs --baseline name:golden \
  --candidate label:basket=checkout,variant=candidate --record --strict
catacomb regress --runs-dir ~/.catacomb/runs --baseline name:golden \
  --candidate label:basket=checkout,variant=candidate \
  --scores scores.jsonl --annotation deepeval.tool_correctness
```

---

### trends

Show the recorded regression history for a baseline — the append-only trail written by
`regress --record`.

```sh
catacomb trends <baseline> [flags]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--db` | `~/.catacomb/catacomb.db` | SQLite database path |
| `--metric` | (empty) | Narrow to one total-scope metric: `duration_ms`, `cost_usd`, `tokens_in`, `tokens_out`, `nodes`, or `error_rate` |
| `--json` | false | Emit the history as JSON |

Records print oldest-first by sequence number. Without `--metric`, the wide table prints one row per
recorded run — `SEQ CREATED CANDIDATE VERDICT REGRESSIONS INSUFFICIENT DURATION_MS COST_USD
ERROR_RATE` — a per-run scoreboard of the overall verdict, the finding counts, and the candidate
run-total values. `--metric <m>` swaps to a narrowed table — `SEQ CREATED CANDIDATE VERDICT
BASELINE-VALUE CANDIDATE-VALUE BAND` — tracking one total-scope metric's baseline value, candidate
value, and noise band across the history so drift on a single axis is legible; a run whose report
carries no total-scope finding for that metric renders `-` in those columns. Value cells are
formatted to two decimals. `CREATED` is each record's `created_at` timestamp, formatted RFC3339 in
UTC.

Each record also stamps the baseline's `created_at` at record time. If a row was recorded against a
different definition of the baseline than the one that exists now — the baseline was deleted and
recreated (or re-`set`) under the same name — its `SEQ` cell carries a trailing `*` and a footnote
`* recorded against a previous definition of this baseline` prints after the table, so a spliced
history is never read as a continuous one.

`--json` emits the raw stored history verbatim as `[{"seq":N,"record":<stored bytes>}]`: each
`record` is the exact JSON body that was written, byte-for-byte, not a re-encoding. A body carries a
schema version field `v` (currently `1`), the candidate selector, thresholds, annotation specs, the
report, its own `created_at` (RFC3339 UTC), a `baseline_created_at` stamp mirroring the
baseline's `created_at` at record time, and the recording binary's version `stamps` (catacomb
version and step-key scheme; records written before stamps existed lack the field) — ready for
dashboards or diffing scripts. A record whose `v`
is not understood by this binary is an exit-`2` error naming the sequence and version (upgrade
catacomb).

Exit codes: `0` success, `2` operational error. An unknown `--metric` (outside the set above), an
unknown baseline (`baseline not found`), a known baseline with no recorded runs (`has no
recorded regress runs`), and a record written by a newer schema version are distinct exit-`2`
errors, as are a missing store and one created by an older binary whose schema needs migrating (run
a write-path command such as `catacomb up` or `baseline set`). `trends` opens the store read-only
and never migrates it.

```sh
catacomb trends golden
catacomb trends golden --metric error_rate
catacomb trends golden --json
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
