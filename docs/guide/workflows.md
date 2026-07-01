# Workflows

Task recipes for common catacomb operations.

## Observe a live session

Start the daemon, install hooks, and open the web UI in one command.

```sh
catacomb up
```

The daemon installs hooks into `./.claude/settings.json` for the current project and
opens the sessions list in the default browser. Start Claude Code in the same directory;
catacomb captures the session automatically.

Use `--no-open` to print the URL without opening a browser, or `-F`/`--foreground` to
run the daemon attached to the terminal:

```sh
catacomb up --no-open
catacomb up --foreground
```

## Backfill history

Load sessions from past transcripts so they appear in the UI alongside live ones.

```sh
catacomb up --history
```

`--history` passes `~/.claude/projects` as the transcript directory to the daemon, which
tails `.jsonl` files incrementally. Cursors are persisted in the `tail_cursors` table so
restarts do not duplicate events.

## Compare two runs

`catacomb diff` diffs two session transcripts by `step_key` and reports added, removed,
changed, and unchanged steps with per-field deltas (args, status, cost, duration, tokens).

```sh
catacomb diff session-a.jsonl session-b.jsonl
catacomb diff session-a.jsonl session-b.jsonl --json
```

## Checkpoints and phase-scoped diff

Checkpoints let you name phases within a session so you can scope diffs and subgraph
views to a specific window of work.

### Placing markers

During a run, place a phase boundary with the CLI, an HTTP call, or the MCP tool:

```sh
# Mark the start and end of a phase
catacomb mark --session <hash> --name plan --boundary start
catacomb mark --session <hash> --name plan --boundary end

# Repeated phase — use occurrence to distinguish
catacomb mark --session <hash> --name retry --boundary start --occurrence 1
```

Repeated phases of the same name pair by LIFO nesting when you omit
`--occurrence`: each `end` closes the most recently opened, still-open phase of
that name, so nested same-name phases bracket correctly (the inner one closes
first). Occurrence numbers follow start order (first start is `0`). Reach for an
explicit `--occurrence` on both the start and the end only when same-name phases
genuinely overlap (neither nested nor sequential) — there LIFO cannot tell which
end belongs to which start, and the explicit occurrence pins the pairing.

The agent can also call `mcp__catacomb__mark` directly, which rides the trace stream.

The HTTP endpoint `POST /v1/mark` accepts the same fields: `session_id`, `name`,
`boundary` (start or end), `occurrence` (optional, defaults to 0), and `state_ref`
(optional).

### Selector syntax

A phase selector is `name` or `name,occurrence`. When occurrence is omitted it defaults
to 0.

### Diffing scoped to a phase

```sh
# Scope both sides to the same phase
catacomb diff a.jsonl b.jsonl --phase plan

# Scope each side independently
catacomb diff a.jsonl b.jsonl --a-phase plan --b-phase plan,1

# Scope by range (from and to must be set together per side)
catacomb diff a.jsonl b.jsonl --a-from plan --a-to impl --b-from plan --b-to impl
```

The HTTP equivalent is `GET /v1/diff` with query parameters `a`, `b`, and any of
`phase`, `aPhase`, `bPhase`, `aFrom`, `aTo`, `bFrom`, `bTo`. A missing or unknown
phase returns `400`; an unknown session returns `404`.

For a within-run comparison — same session on both sides, different phases — pass the
same session hash as both `a` and `b` with different `--a-phase`/`--b-phase` selectors.

See [docs/api/phases.md](../api/phases.md) for the full parameter reference.

### Extracting a phase subgraph

`catacomb subgraph` extracts the execution subgraph delimited by a checkpoint phase and
prints node/edge counts plus node lines.

```sh
catacomb subgraph session.jsonl --phase plan
catacomb subgraph session.jsonl --from plan --to impl
catacomb subgraph session.jsonl --phase plan --json
```

The HTTP focus endpoint `GET /v1/sessions/{hash}/phase/{name}` (where `{name}` may be
`name,occurrence`) returns the phase subgraph as a JSON array of node/edge upsert
events. Unknown session or phase returns `404`; invalid selector returns `400`.

The web UI diff view has per-side phase pickers. It derives available phase names by
reading `marker` nodes from `GET /v1/sessions/{hash}/graph`, then calls `/v1/diff` with
`aPhase`/`bPhase`. There is no separate phases listing endpoint.

## Annotations

Attach structured metadata to any node. Annotations require the daemon to be started
with `--allow-annotations` (or `daemon.allow_annotations: true` in config).

```sh
catacomb daemon --allow-annotations
```

Write an annotation:

```sh
curl -H "Authorization: Bearer <token>" \
  -X POST "http://127.0.0.1:<port>/v1/sessions/<hash>/nodes/<nodeId>/annotations" \
  -H "Content-Type: application/json" \
  -d '{"owner":"eval","key":"score","value":0.9}'
```

`owner` and `key` must not contain dots. `value` must be valid JSON.

## Export

Export the materialized graph to an external sink. Two paths exist.

- **Live sinks**: configured under `sinks:` in `~/.catacomb/config.yaml`, they stream
  graph deltas to the target as the session grows.
- **One-shot export**: `catacomb export --to <sink>` reads the stored database and
  writes the full materialized graph in a single pass.

See the [Export targets](privacy-and-operations.md#export-targets) section in the
privacy and operations guide for flags and output details for each sink (jsonl, postgres,
neo4j, otlp).
