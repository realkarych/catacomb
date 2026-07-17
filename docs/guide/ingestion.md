# Ingestion

Catacomb has one ingestion source: the transcript JSONL files an agent CLI writes to
disk. Every command builds its graph by parsing them — there is no daemon, no hooks, and
no telemetry receiver. Two runtimes are supported, Claude Code (the default) and
OpenAI's Codex CLI — see [Runtimes](#runtimes). See
[`concepts.md`](concepts.md) for the graph model.

## Transcript JSONL

Claude Code records every session as an append-only `.jsonl` transcript under
`~/.claude/projects/`:

```text
~/.claude/projects/<project>/
├── <session-id>.jsonl                     # the main session transcript
└── <session-id>/subagents/agent-*.jsonl   # one sub-transcript per spawned subagent
```

The transcript is the authoritative record of the session: prompts, assistant turns, tool
calls (including MCP calls), token usage, cost, and the full subagent tree via the
sub-transcripts. Catacomb parses the main transcript plus all of its subagent transcripts
and reduces them into one canonical execution graph.

Commands consume transcripts in two ways:

- **`bench`** resolves them by session id: each cell's child process must emit stream-json
  (`claude -p <prompt> --output-format stream-json`) so the runner can peek the
  `session_id`; after the child exits, bench looks up
  `<projects-dir>/*/<session-id>.jsonl` and the matching `subagents/` directory (retrying
  for up to ~3 s while the file lands) and copies them — secret-redacted — into the cell's
  evidence directory (a [`runtime: codex`](#runtimes) cell instead peeks the
  `thread.started` event of `codex exec --json` and resolves the rollout under
  `--sessions-dir`). `regress`, `baseline set`, and `export` then read those evidence
  copies.
- **`replay`, `diff`, `subgraph`, and `export`** take a transcript path (or an evidence
  dir, for `export`) directly on the command line.

## Runtimes

Parsing is a per-runtime adapter behind one seam
([ADR-0031](../adr/0031-multi-runtime-ingestion-codex.md)); everything downstream of it
— the graph, step and phase keys, evidence, verify, regress, baselines — is
runtime-neutral. Two adapters exist:

- **Claude Code** (`claude-code`, the default) — the transcript layout above. Every
  command speaks it.
- **Codex CLI** (`codex`) — OpenAI's agent CLI, with both entry points:
  [`bench`](cli.md#bench) drives cells whose `cmd` emits the `codex exec --json`
  stream and resolves each cell's rollout under `--sessions-dir`, and a session run by
  hand (`codex exec` or the interactive TUI) is recorded with
  [`catacomb import`](cli.md#import) — see
  [Codex sessions](cli.md#codex-sessions-runtime-codex) for the ingestion mechanics.

A basket declares its runtime in the top-level
[`runtime:` field](basket.md#top-level-fields) — one basket, one runtime; there are no
per-command runtime flags. Evidence `meta.json` stamps the runtime into its `env` block
(`agent_runtime`, plus the recording CLI's version as `agent_version`), and the
commands that re-reduce recorded evidence — `regress`, and `export` over an evidence
dir — dispatch on that stamp to pick the right parser. `verify` execs verifiers over
the evidence dir without parsing transcripts at all. Codex evidence — bench-recorded
or imported — therefore flows through the whole gate with no special case.

Codex persists each session as a **rollout**: an append-only JSONL file under a
date-partitioned tree, named by the session's thread id:

```text
~/.codex/sessions/YYYY/MM/DD/
└── rollout-<timestamp>-<thread-id>.jsonl   # one per thread; .jsonl.zst when cold
```

Cold rollouts are zstd-compressed to `.jsonl.zst` by Codex itself; catacomb reads both
forms transparently (a pure-Go decoder, no system `zstd`), and the copies written into
evidence are always plain, secret-redacted `.jsonl`. Subagents are separate rollout
files whose first-line `session_meta` names the parent thread (`parent_thread_id`);
import discovers children by scanning the sessions tree for that link — transitively,
so nested subagents come along — and merges each one into evidence as
`subagents/agent-<thread-id>.jsonl`, joining the same run exactly like Claude Code
sub-transcripts. Checkpoints carry over unchanged too: an `mcp__catacomb__mark` call
recorded in a rollout (the [`catacomb mcp`](cli.md#mcp) server registered in Codex's
`[mcp_servers.catacomb]` config) reduces to the same marker node.

Rollouts report token usage but no dollar cost — and `codex exec` emits no terminal
cost event for `bench` to peek — so Codex evidence carries no
reported `cost_usd` in `meta.json`; the token-derived `cost_usd` metric is **estimated**
from the built-in pricing table, which carries OpenAI GPT-5-family tiers (ADR-0031
stage 2) — model ids with no published price stay unpriced (one pinned exception:
`gpt-5.4-cyber`, unpriced upstream, falls to the `gpt-5.4` family by prefix).
`tokens_in` means the same thing it does for Claude Code: uncached input tokens, with
cached input counted separately at the cache-read rate. Note that OpenAI's long-context
surcharge (2× input / 1.5× output past 272K input tokens on 1M-context models) is not
modeled, so the flat estimate undercounts such requests. `tokens_in`, `tokens_out`, and
`duration_ms` are first-class metrics throughout.

What stays Claude-only for now: the raw-transcript commands —
`replay`, `diff`, `subgraph`, and `export` **given a transcript path** — which parse
Claude Code JSONL only. `export` over an evidence *directory* dispatches on
the meta stamp and works for Codex evidence.

## Checkpoint markers

Phase boundaries ride the same transcript. When the agent calls the
`mcp__catacomb__mark` tool (served by the shipped [`catacomb mcp`](cli.md#mcp) stdio
server), the tool call lands in the transcript like any other, and the reducer
synthesizes a `marker` node from its input — no wiring beyond `--mcp-config`. `bench`
additionally synthesizes `task:<id>` start/end markers from each cell's wall-clock
window and stores that window in the evidence `meta.json`, so run boundaries survive
into `regress` comparisons.

## Run metadata

Transcripts carry the session; the run-level metadata around it comes from `bench`:

- **Labels.** Arbitrary `k=v` metadata for grouping and filtering. Each cell carries
  `basket`/`task`/`variant`/`rep` labels, merged over any ambient ones from the
  `CATACOMB_LABELS` environment variable (cell labels win per key). Labels are stored in
  the evidence `meta.json` and matched by `label:` selectors in `regress` and
  `baseline set`. Keys must match `[a-z0-9_.-]{1,64}`; values are capped at 256 bytes.
- **Run ids.** Each cell runs under `bench-<basket>-<task>-<variant>-r<rep>`, which names
  its evidence directory. The cell's child process gets `CATACOMB_RUN_ID` and
  `CATACOMB_LABELS` in its environment.
- **Exit code and cost.** The child's exit code and the stream-json `result` event's
  `total_cost_usd` are recorded in `meta.json` and the manifest (a `runtime: codex`
  cell records no reported cost — `codex exec` emits no cost event).

## Format drift

Both transcript formats evolve. Records that parse as JSON but match no known shape are
counted per reason (`unknown_record_type`, `unknown_content_block`, `bad_timestamp`) and
surfaced as a single stderr warning whenever a command parses transcripts:

```text
warning: 3 unrecognized transcript record(s) [unknown_record_type=3]
```

The graph is still built from everything that did parse; the warning is the signal to
upgrade catacomb ([ADR-0025](../adr/0025-capture-format-drift-detection.md)).

Catacomb also keeps a **version watchlist**, with one ceiling per runtime: it records
the newest Claude Code and Codex CLI versions it has been tested against, and when a
parsed transcript is stamped with a newer version it prints a second stderr line naming
both versions:

```text
warning: transcript Claude Code version 2.2.0 is newer than tested 2.1.199
warning: transcript Codex version 0.150.0 is newer than tested 0.144.5
```

This is the companion signal — a heads-up that the agent CLI moved past the release this
catacomb was validated on, so the parser may be a step behind. Like the drift count it
fires only when triggered, on any command that parses transcripts, and never touches the
graph, `stdout`, `--json`, or the exit code.

## Watching runs live

Catacomb does not capture for display. Watching a session live in a UI is delegated to a
vendor substrate — feed sessions to a substrate such as Phoenix through that vendor's
first-party Claude Code plugin
([ADR-0026 §2](../adr/0026-form-factor-pivot-offline-eval-gate.md)). Catacomb stays the
offline capture, diff, and regression-gate layer over the same transcripts.

## Historical: four-source ingestion

Earlier catacomb versions ran a sidecar daemon that reconciled four live sources — hooks,
native OpenTelemetry, stream-json, and transcript tailing — into the graph. That
architecture was retired: the transcript already contains everything the eval gate needs,
and the daemon existed mostly to feed a display path that vendors cover better. The
per-source designs (ADRs 0002 and 0003, among others) are superseded; see the
[ADR-0026 supersession map](../adr/0026-form-factor-pivot-offline-eval-gate.md#supersession-map)
for what replaced what.
