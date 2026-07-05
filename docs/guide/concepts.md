# Concepts

## The action graph

Catacomb builds a directed graph of nodes and edges for each session.

### Node types

The graph has eight node types:

- `session` — a Claude Code session root
- `user_prompt` — a user message submitted to the model
- `assistant_turn` — a model response turn
- `tool_call` — a tool invocation (file read, shell command, etc.)
- `mcp_call` — an MCP tool invocation
- `subagent` — a spawned subagent
- `hook_event` — a raw hook event (PreCompact, Notification, etc.)
- `marker` — a phase boundary (see [Phases and checkpoints](#phases-and-checkpoints))

### Edge types

Four edge types connect nodes:

- `parent_child` — structural containment (session → prompt, turn → tool)
- `sequence` — ordering between siblings
- `marker_span` — links a marker to every node inside its time window
- `data_dep` — data dependency between nodes

### Identity and scope

Every node is keyed by an `execution_id` — a ULID minted per session — not by
`run_id`. This means parallel or reused runs never collide.

A **run** groups one or more executions (sessions) under a label. The label
defaults to the session id; set `CATACOMB_RUN_ID` or use
`catacomb run --run-id <label>` to group related sessions together. The grouped
view (`catacomb runs`, `catacomb inspect`) folds the sessions at read time:
`status` is the most severe/live state across them (error, else running while
any session is live, else ok) and `ended_at` is the last session to end. The
per-session views remain available by querying a session id directly. Bench
cells are single-session, so `bench` and `regress` are unaffected by the fold.

Two boundaries of read-time grouping are worth knowing. The persisted `runs`
table row for a shared run id is last-writer-wins across its sessions, but no
read path consults it for grouping — `runs`, `inspect`, `regress`, and `bench`
all reconstruct the group from the stored observations — so that row is
cosmetic. And `aggregate`/`regress` read run `duration_ms` from a single anchor
session's run timing rather than the unioned span (node counts and cost are
unioned); because bench cells are single-session this never affects a shipped
`bench`/`regress` workflow, and `runs`/`inspect` still report the correct
unioned span.

### Session hierarchy

Sessions form a nested tree:

```
session
  └─ user_prompt
       └─ assistant_turn
            ├─ tool_call
            ├─ mcp_call
            └─ subagent
                 └─ user_prompt
                      └─ ...
```

Each subagent nests under the turn that spawned it. Subagent children are
lazy-loaded on expand in the UI, so sessions with hundreds of subagents stay
fast.

### Per-node fields

Each node carries:

- **Timing** — `t_start`, `t_end`, `duration_ms`
- **Cost and tokens** — ranked by source precedence
- **Status** — running, ok, error, blocked, or unknown
- **`payload_hash`** — a hash of the content (not the content itself)
- **`step_key`** — cross-run identity for diffing and annotation
- **`phase_key`** — which phase this node belongs to (if any)
- **`annotations`** — a per-node slot for downstream tooling to attach metadata
- **`tier`** — `core` or `detail`, controlling export granularity

## Ingestion sources

Catacomb reconciles four sources into one graph:

- **Hooks** — the backbone; fire on every Claude Code execution path; capture
  SessionStart, UserPromptSubmit, PreToolUse, PostToolUse, SubagentStop, Stop,
  SessionEnd, PreCompact, and Notification. See [Ingestion](ingestion.md).
- **OpenTelemetry** — native Claude Code telemetry; provides the clean span
  tree, per-node cost and token data, and MCP spans. See [Ingestion](ingestion.md).
- **stream-json** — structural hints (notably `parent_tool_use_id`) from Claude
  Code's output stream. See [Ingestion](ingestion.md).
- **Transcript JSONL** — the source of truth for the subagent tree; enables
  post-hoc backfill of past sessions. See [Ingestion](ingestion.md).

The four sources are merged by canonical entity precedence into one graph. See
[docs/adr/](../adr/) — specifically ADR-0002 and ADR-0003 — for the
reconciliation design.

## Phases and checkpoints

A **marker** node records a named phase boundary. You can insert one with:

```sh
catacomb mark --session <id> --name <name> --boundary start|end
```

Or from inside an agent session via the `mcp__catacomb__mark` tool (which
rides the trace stream without extra wiring), served by the shipped
[`catacomb mcp`](cli.md#mcp) stdio server.

A phase is the half-open time window `[start_marker.t_start, end_marker.t_start)`
— the start is inclusive, the end is exclusive. Every node whose `t_start`
falls inside that window receives a `marker_span` edge from the marker. The
half-open bound means a node whose `t_start` lands exactly on a shared boundary
(one phase's end is the next phase's start) belongs to the phase that starts at
it, never both, so it is never double-counted. The same rule scopes a `--from`/
`--to` range: `--from a --to b` excludes any node sitting exactly on b's start.
The `phase_key` is `hash(enclosingStepKey, name, occurrence)`
and is deterministic across runs, so the same phase in two different sessions
can be compared.

The selector syntax is `name` (occurrence defaults to 0) or `name,occurrence`
for when the same phase name appears more than once.

When the same phase name appears more than once, starts and ends pair by
**LIFO nesting** by default: each end closes the most recently opened,
still-open phase of that name (correct bracket matching), so a `plan` nested
inside an outer `plan` closes the inner one first. Occurrence numbers are
assigned to starts in time order (first start is occurrence 0). If you supply
an explicit `--occurrence` on **both** the start and the end, that pairing wins
over LIFO — this is how you disambiguate phases of the same name that genuinely
*overlap* (neither nested nor sequential), which are otherwise ambiguous.

Phases are used for scoped diffs and subgraph extraction. See
[Workflows](workflows.md) for recipes.
