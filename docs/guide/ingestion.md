# Ingestion

Catacomb has one ingestion source: the transcript JSONL files Claude Code writes to disk.
Every command builds its graph by parsing them — there is no daemon, no hooks, and no
telemetry receiver. See [`concepts.md`](concepts.md) for the graph model.

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
  evidence directory. `regress`, `baseline set`, and `export` then read those evidence
  copies.
- **`replay`, `diff`, `subgraph`, and `export`** take a transcript path (or an evidence
  dir, for `export`) directly on the command line.

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
  `total_cost_usd` are recorded in `meta.json` and the manifest.

## Format drift

Claude Code's transcript format evolves. Records that parse as JSON but match no known
shape are counted per reason (`unknown_record_type`, `unknown_content_block`) and
surfaced as a single stderr warning whenever a command parses transcripts:

```text
warning: 3 unrecognized transcript record(s) [unknown_record_type=3]
```

The graph is still built from everything that did parse; the warning is the signal to
upgrade catacomb ([ADR-0025](../adr/0025-capture-format-drift-detection.md)).

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
