# Phase-scoped diff & subgraph API

Checkpoints (phase markers) are placed with `catacomb mark` / `mcp__catacomb__mark` /
`POST /v1/mark`. The endpoints below scope a diff or a graph view to the subgraph a
checkpoint delimits.

## Selector syntax

A selector is `name` or `name,occurrence` (occurrence defaults to `0`).

## `GET /v1/diff`

Diff two sessions, optionally scoped per side.

| Query param | Meaning |
| --- | --- |
| `a`, `b` | session hashes (required) |
| `phase` | scope BOTH sides to this phase |
| `aPhase`, `bPhase` | per-side phase (overrides `phase` for that side) |
| `aFrom`/`aTo`, `bFrom`/`bTo` | per-side range: subgraph between two checkpoints `[from, to)` |

`from`/`to` must be set together and are mutually exclusive with a phase selector on the
same side. Missing phase → `400`; invalid selector → `400`; unknown session → `404`.
With no selector params the response is byte-for-byte the unscoped diff.

## `GET /v1/sessions/{hash}/phase/{name}`

Returns the subgraph of one phase as a JSON array of SSE-style node/edge upsert events
(the same shape as `GET /v1/sessions/{hash}/subagent/{agentId}`). `{name}` may be
`name,occurrence`. Unknown session or phase → `404`; invalid selector → `400`.

## CLI

- `catacomb diff A.jsonl B.jsonl --phase plan` — diff a phase across two runs.
- `catacomb diff A.jsonl B.jsonl --a-from plan --a-to impl --b-from plan --b-to impl` — range.
- `catacomb subgraph session.jsonl --phase plan [--json]` — print one phase's subgraph.
- `catacomb subgraph session.jsonl --from plan --to impl` — print a range subgraph.
