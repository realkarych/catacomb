# Workflows

Task recipes for common catacomb operations.

## Capture a live session

Start the daemon and install hooks in one command.

```sh
catacomb up
```

`up` installs hooks into `./.claude/settings.json` for the current project and prints
the daemon address. Start Claude Code in the same directory; catacomb captures the
session automatically. Watching a session live in a UI is delegated to a vendor
substrate such as Phoenix (see
[Daemonless benching](#daemonless-benching-adr-0026)).

Use `-F`/`--foreground` to run the daemon attached to the terminal:

```sh
catacomb up --foreground
```

## Backfill history

Load sessions from past transcripts so they sit in the store alongside live ones.

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

The agent can also call `mcp__catacomb__mark` directly, which rides the trace
stream. That tool is served by `catacomb mcp` — a stdlib stdio MCP server that
ships with catacomb (no hand-rolled stub needed). Wire it in with `--mcp-config`:

```json
{"mcpServers":{"catacomb":{"command":"catacomb","args":["mcp"]}}}
```

The server named `catacomb` exposing the `mark` tool surfaces to the agent as
`mcp__catacomb__mark`. The call is a pure acknowledgement — the marker is
synthesized from the tool-call input on the trace stream, so no daemon
connection is required and the tool fails open. See
[cli.md](cli.md#mcp) for the tool schema and `--strict-mcp-config` note.

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

There is no separate phases listing endpoint: a client derives available phase names by
reading `marker` nodes from `GET /v1/sessions/{hash}/graph`, then calls `/v1/diff` with
`aPhase`/`bPhase`.

## Regression-testing a change

When you change a pipeline component — a skill, an MCP tool, a prompt — two runs of the same
task are two samples from a distribution, so a single `catacomb diff` cannot separate a real
regression from sampling noise. `catacomb regress` compares two *groups* of repeated runs
statistically and returns a CI-consumable verdict and exit code. See
[ADR-0022](../adr/0022-regression-detection-over-repeated-runs.md) for the method.

### Declare a basket

Rather than hand-rolling the repetition loop, declare the matrix once as a basket and let
`catacomb bench` expand and run it. A basket is `tasks × variants × reps`; each combination is
one *cell*, run under a generated run-id and labeled `basket`/`task`/`variant`/`rep`:

```yaml
# checkout.yaml
basket: checkout
reps: 5
tasks:
  - id: work-task
    cmd: ["claude", "-p", "work the checkout task"]
    checkpoints: [plan, tests.pass]   # phases you expect the agent to mark
variants:
  - id: main
  - id: candidate
    setup: ["git checkout candidate-branch"]
```

### Run the basket

```sh
catacomb bench checkout.yaml
```

The runner executes every cell sequentially through `catacomb run` — ten cells here (two
variants × five reps) — appending each result to `checkout.yaml.manifest.jsonl` as it goes. A
failing cell is recorded and the run continues; re-invoke with `--resume` to pick up where an
interrupted basket left off. Because bench applies the `basket`/`variant` labels for you, the
selectors below need no change.

On success it prints a copy-pasteable epilogue wiring up the next two steps:

```text
Next steps:
  catacomb baseline set checkout-main --label basket=checkout,variant=main
  catacomb regress --baseline label:basket=checkout,variant=main --candidate label:basket=checkout,variant=candidate
```

For each cell bench also emits `task:<id>` start/end markers around the child (best-effort — it
needs a `session_id` in the child's stream-json). These surface as **phase rows** in the
`regress` table under the checkpoint scope, so the task boundary is always a stable, noise-robust
comparison axis even when the agent never called `mcp__catacomb__mark`. See
[bench](cli.md#bench) for the full basket schema, the manifest and `--resume` semantics, and the
`setup` no-shell limitation.

The `checkpoints:` list declares the in-run phases you expect the agent to mark for itself: by
convention the agent calls `mcp__catacomb__mark` at each phase (wired through a CLAUDE.md
instruction), and after each cell bench fetches the session graph and reports any declared
checkpoint it did not find. A miss shows up immediately as a `cell <run-id>: missing checkpoints:
...` warning on stderr and a `checkpoints[<task>]: <name> <hit>/<verified>` summary line, and
then downstream as a presence-rate drop for that phase in `regress` — bench only reports the
gap, `regress` decides whether it is a regression.

### Daemonless benching (ADR-0026)

The same loop runs with no daemon and no store. `bench --offline` executes each cell as a plain
local process: it peeks the child's stream-json for the session id and the authoritative
`total_cost_usd`, resolves the session's transcripts from `~/.claude/projects`
(`--projects-dir`), synthesizes the `task:<id>` boundary markers from the child's wall clock,
verifies declared `checkpoints:` in-process against the graph rebuilt from those transcripts,
and writes a secret-redacted evidence directory per cell under `~/.catacomb/runs`
(`--runs-dir`):

```sh
catacomb bench checkout.yaml --offline
```

```text
~/.catacomb/runs/<run-id>/
├── session.jsonl             # main transcript, secret-redacted
├── subagents/agent-*.jsonl   # subagent transcripts, when present
└── meta.json                 # run id, labels, exit code, cost_usd, marker window
```

Watching runs live is delegated to the vendor substrate per
[ADR-0026 §2](../adr/0026-form-factor-pivot-offline-eval-gate.md): feed sessions to
Phoenix through its first-party Claude Code plugin — catacomb ships no viewer of its own.

Manifest entries gain `cost_usd` and `evidence_dir`, and the epilogue prints the matching
offline comparison ready to paste. `regress --runs-dir` resolves `label:` selectors by scanning
the evidence directories' `meta.json` — no store involved — and rebuilds each matching run's
graph from its transcripts, so verdicts, thresholds, and exit codes are exactly the
[Compare and gate](#compare-and-gate) ones:

```sh
catacomb regress --runs-dir ~/.catacomb/runs \
  --baseline label:basket=checkout,variant=main \
  --candidate label:basket=checkout,variant=candidate
```

In-task checkpoints still ride the `mark` MCP tool: the agent calls `mcp__catacomb__mark`
(served by `catacomb mcp`, wired with `--mcp-config` as
[above](#checkpoints-and-phase-scoped-diff)), and offline bench observes the call in the
transcript instead of over HTTP — the convention and the basket `checkpoints:` lists carry over
unchanged.

The rest of the loop is offline too. Pin the golden group by name — `baseline set --runs-dir`
resolves the labels from the evidence dirs, creates `~/.catacomb/catacomb.db` if it does not
exist yet, and records which runs dir the pinned runs live in:

```sh
catacomb baseline set checkout-main --label basket=checkout,variant=main \
  --runs-dir ~/.catacomb/runs
catacomb regress --runs-dir ~/.catacomb/runs --baseline name:checkout-main \
  --candidate label:basket=checkout,variant=candidate --record
```

The `name:` baseline resolves offline — its pinned run IDs load straight from
`<runs-dir>/<run-id>/` (pointing `--runs-dir` somewhere other than the recorded dir warns, and
the flag wins) — and `--record` appends the comparison to the baseline's history in the same
database, so [`trends checkout-main`](cli.md#trends) replays the drift exactly as in the
daemon-backed loop. Baselines carry version stamps (catacomb version and step-key scheme) from
`set` time: after upgrading catacomb, a stamp mismatch warns — or fails the gate with
`--strict` — until the baseline is re-`set`, so a scheme change never silently misaligns pinned
evidence. External quality scores come along too: score the redacted transcripts however you
like, write one JSONL line per scored step per run, and feed the file to the same gate:

```sh
catacomb regress --runs-dir ~/.catacomb/runs --baseline name:checkout-main \
  --candidate label:basket=checkout,variant=candidate \
  --scores scores.jsonl --annotation grader.task_score
```

Each `--scores` line is `{"step_key": ..., "key": "grader.task_score", "value": 0.92,
"run_id": ...}`; the values land as node annotations on both groups before comparison, so the
[external-score gate](#gate-on-external-scores-optional) works without a daemon or the
annotations endpoint. This offline loop is ADR-0026 (the form-factor pivot to an offline eval
gate): the runner and the comparison were the PV-1 slice; named baselines, recorded history,
version stamps, and scores files are the PV-2 slice. Flag details in [bench](cli.md#bench) and
[regress](cli.md#regress).

### Label two run groups by hand

`catacomb bench` is a thin loop over `catacomb run`; when you need one-off control you can drive
the same two groups directly, tagging each run with a `--label` variant and a unique `--run-id`:

```sh
# Baseline variant, k repetitions
for i in 1 2 3 4 5; do
  catacomb run --run-id checkout-main-$i --label basket=checkout,variant=main \
    -- claude -p "work the checkout task"
done

# Candidate variant, same basket, k repetitions
for i in 1 2 3 4 5; do
  catacomb run --run-id checkout-cand-$i --label basket=checkout,variant=candidate \
    -- claude -p "work the checkout task"
done
```

`--label` accepts a comma-separated `k=v` list or repeated flags; labels ride the child's hook
and stream-json events to the daemon. See [ingestion.md](ingestion.md#run-labels) for label
rules and caps.

### Pin a baseline (optional)

Once a group is "golden," save it so it survives later label churn. `baseline set` resolves the
selector against the store now and stores the matching run IDs under a name:

```sh
catacomb baseline set golden --label basket=checkout --label variant=main
catacomb baseline list
```

### Compare and gate

Compare two label selectors directly, or reference the saved baseline by name. Both selector
forms — `label:k=v[,k=v...]` and `name:<baseline>` — are interchangeable on either side:

```sh
# Both sides by label selector
catacomb regress \
  --baseline label:basket=checkout,variant=main \
  --candidate label:basket=checkout,variant=candidate

# Baseline by name, candidate by label
catacomb regress --baseline name:golden \
  --candidate label:basket=checkout,variant=candidate --json
```

The summary line reports the baseline and candidate run counts, alignment coverage, and the
overall verdict; the table lists per-scope findings (run totals, checkpoint phases, steps) with a
verdict, the baseline and candidate values, and the noise band. The overall verdict maps to the
exit code — `ok` is `0`, `regression` is `1`, an operational error (bad selector, unknown
baseline, missing store, empty group) is `2`. Add `--strict` to also fail with `1` when data is
insufficient. A CI gate is then just:

```sh
catacomb regress --baseline name:golden --candidate label:variant=candidate --record || exit 1
```

`--record` appends each comparison to the `golden` baseline's history (the flag needs a `name:`
baseline), so every CI run leaves a durable, replayable record without changing the gate's verdict
or exit code. If the append itself fails the command exits `2` rather than the verdict's `1`, so a
broken store trips the gate instead of passing silently.

In a fan-out CI matrix, serialize the recording: have a single shard run `--record` (or gate it on a
lock), or give each shard its own store file. Concurrent `--record` writers on one store can collide
on SQLite's write lock and fail loudly with `SQLITE_BUSY` (exit `2`, no corruption) rather than
queue.

### Gate sensitivity at small k

The rate gate (presence, error rate) hard-flags a `regression` only when the baseline and
candidate one-sided Wilson bounds are disjoint *and* the delta clears its threshold, so at small
run counts a real flip can land as `notable` — which does not gate — instead of `regression`. At
the default thresholds (`--z` 1.645, `--presence-delta` 0.2, `--error-delta` 0.1, `--min-support`
3) the smallest presence drop that can hard-flag depends on the runs per side:

| Runs per side (k) | Smallest presence drop that can hard-flag |
| --- | --- |
| 3–4 | 100% → 0% (full flip only) |
| 5 | 100% → 20% |
| 10 | 100% → 50% |

`regress` computes this for the actual group sizes and, when even a full flip cannot reach
`regression`, prints a `sensitivity:` line naming the smallest `k` at which the gate could fire,
so a gate that cannot fire is never silent about it. Add `--fail-on-notable` to count `notable`
findings toward the gate (exit `1`); it trades precision for recall at small k, since at `k=3`
the delta threshold alone would also flag ordinary sampling noise such as 1/3 → 2/3.

### Watching drift over time

With `--record` in the gate, each run accumulates in the baseline's append-only history. `trends`
replays it oldest-first — one row per recorded comparison — so slow drift is visible even when no
single run tripped the gate:

```sh
catacomb trends golden
```

Narrow to one total-scope metric to watch a single axis across the history — its baseline value,
candidate value, and noise band per run:

```sh
catacomb trends golden --metric error_rate
```

`trends` reads the store read-only; see [`trends`](cli.md#trends) for the full table shapes, the
`--json` form, and exit codes.

### Gate on external scores (optional)

Catacomb compares deterministic observables (status, presence, duration, cost, tokens); it does
not judge output *quality*. To fold a quality signal into the same gate, score the runs with an
external evaluator and write the verdict back as a numeric annotation, then let `regress` treat it
as one more metric. Daemonless, skip the write-back: put the scores in a JSONL file and pass
[`--scores`](cli.md#gating-on-external-scores), as in
[Daemonless benching](#daemonless-benching-adr-0026).

1. **Score the runs.** Export each session and run the DeepEval integration under
   [`integrations/deepeval`](https://github.com/realkarych/catacomb/tree/master/integrations/deepeval),
   whose default `ToolCorrectnessMetric` path is deterministic and offline (no LLM judge, no API
   key). The export carries node payload content (tool inputs and outputs, secret-redacted)
   automatically whenever the ingest source captured it — no extra flag:

   ```sh
   catacomb export --to jsonl --run <run-id> --out session.jsonl
   catacomb-deepeval session.jsonl --run <run-id> --expected expected.json
   ```

2. **Write the score back as an annotation.** Scores must land on step-key-eligible nodes —
   `tool_call`, `mcp_call`, `skill`, or `subagent` — or they never reach a step row and the gate
   silently passes. Fetch the session graph and pick the node that performs the step under test
   (the same logical call recurs across the basket, so its `step_key` aligns run-to-run):

   ```sh
   curl -s -H "Authorization: Bearer <token>" \
     "http://127.0.0.1:<port>/v1/sessions/<hash>/graph" |
     jq -r '.[] | select(.kind == "node_upsert" and .node.step_key != null)
            | [.node.id, .node.type, .node.name] | @tsv'
   ```

   With the daemon started `--allow-annotations` (see [Annotations](#annotations)), POST the
   score onto the chosen node id:

   ```sh
   curl -H "Authorization: Bearer <token>" \
     -X POST "http://127.0.0.1:<port>/v1/sessions/<hash>/nodes/<nodeId>/annotations" \
     -H "Content-Type: application/json" \
     -d '{"owner":"deepeval","key":"tool_correctness","value":0.92}'
   ```

3. **Gate on it.** Add `--annotation deepeval.tool_correctness` (higher-better by default; append
   `:lower-better` for a penalty-style score). The score aggregates per `step_key` and flags with
   the metric noise band, so a candidate whose median score drops out of the baseline band is a
   `regression`:

   ```sh
   catacomb regress --baseline name:golden --candidate label:variant=candidate \
     --annotation deepeval.tool_correctness --strict
   ```

   Annotation gating is step-scoped only (per ADR-0022); a key sampled below `--min-support` runs,
   or present on only one side, is reported `insufficient` rather than guessed. In CI, add
   `--strict` (as above) so an under-annotated group fails the gate with exit `1` instead of
   passing silently.

### Practical notes

- **Use `k` ≥ 5.** Minimum support is 3 (`--min-support`), and at the default thresholds a full
  presence or error-rate flip hard-flags a `regression` from `k=3`; a partial drop needs `k` ≥ 5
  to reach `regression` (see the [sensitivity table above](#gate-sensitivity-at-small-k)). Five or
  more repetitions per variant keeps headroom for a real flip to gate.
- **Lean on checkpoints when the change rewrites prompts.** Changing the component under test
  alters some prompt hashes, so `step_key` alignment degrades and step-level coverage drops.
  Below `--coverage-floor` (default 0.7) step verdicts are downgraded to `notable`, and the
  checkpoint (phase) level — robust to step drift by construction — carries the verdict. Mark
  task phases with `catacomb mark` (or `mcp__catacomb__mark`) so there is always a stable,
  noise-robust comparison axis; see
  [Checkpoints and phase-scoped diff](#checkpoints-and-phase-scoped-diff).

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
