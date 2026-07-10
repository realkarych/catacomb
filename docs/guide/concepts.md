# Concepts

## The action graph

Catacomb reduces a session's transcripts into a directed graph of nodes and edges. The
graph is deterministic: the same transcript records, in any order, converge to the same
graph.

### Node types

The graph has eight node types:

- `session` — a Claude Code session root
- `user_prompt` — a user message submitted to the model
- `assistant_turn` — a model response turn
- `tool_call` — a tool invocation (file read, shell command, etc.)
- `mcp_call` — an MCP tool invocation
- `skill` — a skill invocation
- `subagent` — a spawned subagent
- `marker` — a phase boundary (see [Phases and checkpoints](#phases-and-checkpoints))

### Edge types

Two edge types connect nodes:

- `parent_child` — structural containment (session → prompt, turn → tool)
- `marker_span` — links a marker to every node inside its time window

### Identity and scope

Every node is keyed by an `execution_id` — a ULID minted for each graph build — so
graphs built from different transcripts (or twice from the same one) never collide.
Cross-run identity does not ride node IDs at all; it rides `step_key` (see
[Per-node fields](#per-node-fields)).

A **run** is one `bench` cell: a single session (plus its subagent sub-transcripts)
recorded as an evidence directory with a `meta.json` carrying the run id
(`bench-<basket>-<task>-<variant>-r<rep>`), labels, exit code, cost, and the `task:<id>`
marker window. `regress` and `baseline set` group runs by matching those labels.

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

Each subagent nests under the turn that spawned it, and its inner tree is parsed from
its own `subagents/agent-*.jsonl` sub-transcript.

### Per-node fields

Each node carries:

- **Timing** — `t_start`, `t_end`, `duration_ms`
- **Cost and tokens** — from transcript usage records, with reported totals preferred
  over per-turn estimates
- **Status** — running, ok, error, blocked, or unknown
- **`payload_hash`** — a hash of the (redacted) content, not the content itself
- **`step_key`** — cross-run identity for diffing, aggregation, and score annotations
- **`phase_key`** — which phase this node belongs to (if any)
- **`annotations`** — a per-node slot where `--scores` values land before comparison
- **`tier`** — `core` or `detail`, controlling export granularity

A `step_key` names "the same logical step" across runs: it hashes the node's position
and its redacted, salient tool input (for an edit, the file path; for a shell call, the
command), so repeated runs of the same pipeline align step-to-step even though every
node ID differs. The scheme is versioned (`stepkey/v1`) and stamped onto baselines so a
scheme change is detected rather than silently misaligning groups.

## The transcript source

The graph is built from one source: Claude Code's transcript JSONL files (the main
session transcript plus one sub-transcript per subagent). `bench` snapshots them into
redacted evidence directories; the other commands parse them directly. See
[Ingestion](ingestion.md) for the file layout and drift handling. Earlier catacomb
versions reconciled four live sources through a daemon; that design is historical —
see the
[ADR-0026 supersession map](../adr/0026-form-factor-pivot-offline-eval-gate.md#supersession-map).

## Phases and checkpoints

A **marker** node records a named phase boundary. Markers come from two places:

- the agent calling the `mcp__catacomb__mark` tool during a run (served by
  [`catacomb mcp`](cli.md#mcp)); the tool call lands in the transcript and the reducer
  synthesizes the marker from it;
- `bench` synthesizing `task:<id>` start/end markers around each cell from the child's
  wall-clock window.

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
an explicit `occurrence` on **both** the start and the end, that pairing wins
over LIFO — this is how you disambiguate phases of the same name that genuinely
*overlap* (neither nested nor sequential), which are otherwise ambiguous.

Phases are used for scoped diffs, subgraph extraction, and the checkpoint scope of
`regress`. See [Workflows](workflows.md) for recipes.
