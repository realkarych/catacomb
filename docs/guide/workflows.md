# Workflows

Task recipes for common catacomb operations.

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
    cmd: ["claude", "-p", "work the checkout task", "--output-format", "stream-json"]
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

The runner executes every cell as a plain local process — ten cells here (two variants × five
reps) — appending each result to `checkout.yaml.manifest.jsonl` as it goes. It peeks each
child's stream-json for the session id and the authoritative `total_cost_usd`, resolves the
session's transcripts from `~/.claude/projects` (`--projects-dir`), synthesizes `task:<id>`
boundary markers from the child's wall clock, verifies declared `checkpoints:` against the
graph rebuilt from those transcripts, and writes a secret-redacted evidence directory per cell
under `~/.catacomb/runs` (`--runs-dir`):

```text
~/.catacomb/runs/<run-id>/
├── session.jsonl             # main transcript, secret-redacted
├── subagents/agent-*.jsonl   # subagent transcripts, when present
└── meta.json                 # run id, labels, exit code, cost_usd, marker window
```

A failing cell is recorded and the run continues; re-invoke with `--resume` to pick up where an
interrupted basket left off. Because bench applies the `basket`/`variant` labels for you, the
selectors below need no change. On success it prints a copy-pasteable epilogue wiring up the
comparison:

```text
Next steps:
  catacomb regress --runs-dir ~/.catacomb/runs --baseline label:basket=checkout,variant=main --candidate label:basket=checkout,variant=candidate
```

The synthesized `task:<id>` markers surface as **phase rows** in the `regress` table under the
checkpoint scope, so the task boundary is always a stable, noise-robust comparison axis even
when the agent never called `mcp__catacomb__mark`. See [bench](cli.md#bench) for the full
basket schema, the manifest and `--resume` semantics, and the `setup` no-shell limitation.

The `checkpoints:` list declares the in-run phases you expect the agent to mark for itself: by
convention the agent calls `mcp__catacomb__mark` at each phase (wired through a CLAUDE.md
instruction, see [Placing markers](#placing-markers)), and after each cell bench rebuilds the
session graph from the transcripts and reports any declared checkpoint it did not find. A miss
shows up immediately as a `cell <run-id>: missing checkpoints: ...` warning on stderr and a
`checkpoints[<task>]: <name> <hit>/<verified>` summary line, and then downstream as a
presence-rate drop for that phase in `regress` — bench only reports the gap, `regress` decides
whether it is a regression.

Watching runs live in a UI is delegated to a vendor substrate per
[ADR-0026 §2](../adr/0026-form-factor-pivot-offline-eval-gate.md): feed sessions to a
substrate such as Phoenix through that vendor's first-party Claude Code plugin — catacomb
ships no viewer of its own.

### Compare and gate

`regress` resolves both selectors from the evidence directories — `label:` selectors scan
`<runs-dir>/*/meta.json` and rebuild each matching run's graph from its transcripts:

```sh
catacomb regress --runs-dir ~/.catacomb/runs \
  --baseline label:basket=checkout,variant=main \
  --candidate label:basket=checkout,variant=candidate
```

The summary line reports the baseline and candidate run counts, alignment coverage, and the
overall verdict; the table lists per-scope findings (run totals, checkpoint phases, steps) with a
verdict, the baseline and candidate values, and the noise band. The overall verdict maps to the
exit code — `ok` is `0`, `regression` is `1`, an operational error (bad selector, unknown
baseline, missing evidence dir, empty group) is `2`. Add `--strict` to also fail with `1` when
data is insufficient.

### Pin a baseline

Once a group is "golden," save it so it survives later label churn. `baseline set` resolves the
selector against the evidence dirs now, creates `~/.catacomb/catacomb.db` if it does not exist
yet, and pins the matching run IDs under a name (recording which runs dir they live in):

```sh
catacomb baseline set checkout-main --label basket=checkout,variant=main \
  --runs-dir ~/.catacomb/runs
catacomb baseline list
```

The `name:` baseline's pinned run IDs load straight from `<runs-dir>/<run-id>/` (pointing
`--runs-dir` somewhere other than the recorded dir warns, and the flag wins; a missing pinned
evidence dir is a hard error). Baselines carry version stamps (catacomb version and step-key
scheme) from `set` time: after upgrading catacomb, a stamp mismatch warns — or fails the gate
with `--strict` — until the baseline is re-`set`, so a scheme change never silently misaligns
pinned evidence. A CI gate is then just:

```sh
catacomb regress --baseline name:checkout-main \
  --candidate label:basket=checkout,variant=candidate --record || exit 1
```

`--record` appends each comparison to the `checkout-main` baseline's history (the flag needs a
`name:` baseline), so every CI run leaves a durable, replayable record without changing the
gate's verdict or exit code. If the append itself fails the command exits `2` rather than the
verdict's `1`, so a broken store trips the gate instead of passing silently.

In a fan-out CI matrix, serialize the recording: have a single shard run `--record` (or gate it
on a lock), or give each shard its own store file. Concurrent `--record` writers on one store
can collide on SQLite's write lock and fail loudly with `SQLITE_BUSY` (exit `2`, no corruption)
rather than queue.

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

### Catching drift below the band: the paired sign test

The metric noise band is rep-count-invariant: `duration_ms`, `cost_usd`, and token medians flag
only when the candidate leaves the baseline median ± `max(0.25 × |median|, 1.5 × IQR)` band, so
a systematic drift smaller than the relative delta — a +10% cost creep, say — can never flag
there, no matter how many reps you run. The paired sign test is the axis built for exactly that
shape: for every task present in both groups (with `--min-support` runs per side) it takes the
candidate-minus-baseline delta of the per-task medians and asks how likely that many increases
are under no change. Repeated *direction across tasks* is the signal, not magnitude:

- a +10% cost creep on **8 of 8** tasks gives p = 0.0039 ≤ `--paired-alpha` (default 0.05) and
  gates (exit `1`) — invisible to the band at any rep count;
- the same creep on **6 of 8** tasks does not fire (p = 0.1445) — direction that weak stays a
  band question, however large the deltas.

At the default `--paired-alpha` the smallest signal that can fire is five unanimous per-task
shifts (0.5⁵ = 0.03125), and `--paired-min-tasks` (default 5) refuses to gate below five
matched tasks regardless. When the paired layer is active but cannot fire, the `sensitivity:`
line names the smallest task count at which a unanimous shift would gate — same discipline as
the rate-gate disclosure above. See [regress](cli.md#regress) for the finding semantics.

### Watching drift over time

With `--record` in the gate, each run accumulates in the baseline's append-only history. `trends`
replays it oldest-first — one row per recorded comparison — so slow drift is visible even when no
single run tripped the gate:

```sh
catacomb trends checkout-main
```

Narrow to one total-scope metric to watch a single axis across the history — its baseline value,
candidate value, and noise band per run:

```sh
catacomb trends checkout-main --metric error_rate
```

`trends` reads the store read-only; see [`trends`](cli.md#trends) for the full table shapes, the
`--json` form, and exit codes.

### Gate on external scores (optional)

Catacomb compares deterministic observables (status, presence, duration, cost, tokens); it does
not judge output *quality*. To fold a quality signal into the same gate, score the runs with an
external evaluator, write the verdicts to a JSONL file, and pass
[`--scores`](cli.md#gating-on-external-scores) so they land as node annotations before the
comparison.

1. **Score the runs.** Export each cell's evidence dir and run the DeepEval integration under
   [`integrations/deepeval`](https://github.com/realkarych/catacomb/tree/master/integrations/deepeval),
   whose default `ToolCorrectnessMetric` path is deterministic and offline (no LLM judge, no API
   key). The export carries node payload content (tool inputs and outputs, secret-redacted)
   automatically:

   ```sh
   catacomb export ~/.catacomb/runs/<run-id> --out run.jsonl
   catacomb-deepeval run.jsonl --run <run-id> --expected expected.json
   ```

2. **Write the scores to a JSONL file.** Scores must land on step-key-eligible nodes —
   `tool_call`, `mcp_call`, `skill`, or `subagent` — or they never reach a step row and the gate
   silently passes. Take the `step_key` from the `KEY` column of a `regress` table or from
   [`subgraph --json`](cli.md#subgraph) node output, and write one line per scored step per run:

   ```json
   {"step_key": "1f0c9a4b2d8e7f36", "key": "deepeval.tool_correctness", "value": 0.92, "run_id": "bench-checkout-work-task-candidate-r1"}
   ```

3. **Gate on it.** Add `--annotation deepeval.tool_correctness` (higher-better by default; append
   `:lower-better` for a penalty-style score) and pass the file with `--scores`. The score
   aggregates per `step_key` and flags with the metric noise band, so a candidate whose median
   score drops out of the baseline band is a `regression`:

   ```sh
   catacomb regress --baseline name:checkout-main \
     --candidate label:basket=checkout,variant=candidate \
     --scores scores.jsonl --annotation deepeval.tool_correctness --strict
   ```

   This example gates a per-step score; a key sampled below `--min-support` runs, or present on
   only one side, is reported `insufficient` rather than guessed. In CI, add `--strict` (as above)
   so an under-annotated group fails the gate with exit `1` instead of passing silently.

4. **Or gate a whole-run verdict.** A score line that omits `step_key` is a **run-level** score
   ([`--scores` schema](cli.md#run-level-scores)) that gates at the run-total scope. In an external
   file it must carry `run_id`; a `scores.jsonl` dropped into a run's evidence dir is auto-loaded
   and may omit it. The reserved key `verifier.pass` gates **by default** — no `--annotation` flag
   — so a pass/fail verifier folds into the gate with just `--scores`:

   ```json
   {"key": "verifier.pass", "value": 1, "run_id": "bench-checkout-work-task-candidate-r1"}
   ```

   When every value is `0`/`1` the key is gated as a rate (`--annotation-rate-delta`, default 0.1,
   the same Wilson-bounds rule as presence and error rates), and the `DETAIL` column shows the
   `ones a/n -> b/m` counts; a continuous score uses the metric band instead.

### Practical notes

- **Use `k` ≥ 5.** Minimum support is 3 (`--min-support`), and at the default thresholds a full
  presence or error-rate flip hard-flags a `regression` from `k=3`; a partial drop needs `k` ≥ 5
  to reach `regression` (see the [sensitivity table above](#gate-sensitivity-at-small-k)). Five or
  more repetitions per variant keeps headroom for a real flip to gate.
- **Use ≥ 5 tasks when you want the paired axis.** The paired sign test refuses to gate below
  `--paired-min-tasks` (default 5) matched tasks, and at the default `--paired-alpha` (0.05) five
  unanimous per-task shifts is also the smallest signal that can reach `regression`
  (0.5⁵ = 0.03125) — a smaller basket reports `insufficient` and the `sensitivity:` line
  discloses it, no matter how strong the drift. Five or more tasks per basket give systematic
  sub-band drift a path to gate; see
  [the paired sign test](#catching-drift-below-the-band-the-paired-sign-test).
- **Lean on checkpoints when the change rewrites prompts.** Changing the component under test
  alters some prompt hashes, so `step_key` alignment degrades and step-level coverage drops.
  Below `--coverage-floor` (default 0.7) step verdicts are downgraded to `notable`, and the
  checkpoint (phase) level — robust to step drift by construction — carries the verdict. Declare
  task `checkpoints:` and wire `mcp__catacomb__mark` so there is always a stable, noise-robust
  comparison axis; see [Checkpoints and phase-scoped diff](#checkpoints-and-phase-scoped-diff).

## Verifying task outcomes

`regress` compares deterministic observables — status, presence, duration, cost, tokens — but
whether the agent produced the *right answer* is task-specific. The verifier contract folds an
outcome check into the same gate: a task declares the files it produces and a command that scores
them, `bench` runs that command after each cell, and its verdict rides through `regress` as the
run-level `verifier.pass` annotation, which
[gates by default](#gate-on-external-scores-optional).

### Declare artifacts and a verifier

Add `artifacts:` (the workdir-relative files the cell produces) and a `verify:` block (a `cmd`,
an optional `env`, and an optional `timeout`) to the task:

```yaml
# sql.yaml
basket: sql
reps: 5
tasks:
  - id: sql
    cmd: ["./agent.sh"]            # runs the agent, writes out/result.csv
    dir: work                      # the cell's workdir
    artifacts: ["out/result.csv"]
    verify:
      cmd: ["python3", "./verify_sql.py"]
      env: { GOLDEN: "/fixtures/golden.csv" }   # ground truth, OUTSIDE the workdir
      timeout: 30s
variants:
  - id: baseline
  - id: candidate
    setup: ["git checkout candidate-branch"]
```

After each cell `bench` captures the declared `artifacts:` into the evidence dir, then runs
`verify.cmd` as a plain `exec` (argv, no shell) with the contract on its environment. The
verifier reads the captured artifacts (and any ground truth), decides pass/fail, and prints one
scores-JSONL line — the
[verifier SDK](https://github.com/realkarych/catacomb/tree/master/integrations/verifier) reduces
that to two calls:

```python
import os
from catacomb_verifier import Cell, emit, compare_tables

cell = Cell.from_env()
res = compare_tables(cell.artifact("out/result.csv"), os.environ["GOLDEN"], ordered=False)
emit(passed=res.equal, tool="verify_sql", tool_version="1")
```

`emit(passed=…)` writes the reserved `verifier.pass` key (`1`/`0`) to the cell's `scores.jsonl`.

### The verifier contract

`bench` (inline, after each cell) and [`verify`](cli.md#verify) (offline, re-run over recorded
evidence) hand the verifier the same environment; `Cell.from_env()` reads it:

| Variable | Value |
| --- | --- |
| `CATACOMB_EVIDENCE_DIR` | the cell's evidence dir — redacted transcripts, `meta.json`, captured `artifacts/` |
| `CATACOMB_WORKDIR` | the hot workdir under `bench`; **empty** offline, so a re-runnable verifier reads only from evidence |
| `CATACOMB_RUN_ID`, `CATACOMB_BASKET`, `CATACOMB_TASK`, `CATACOMB_VARIANT`, `CATACOMB_REP` | the cell's coordinates |
| `CATACOMB_AGENT_EXIT_CODE` | the agent child's exit code |

`Cell.artifact(rel)` resolves a captured artifact, preferring the evidence copy so the verifier
reads the same bytes under `bench` and offline `verify`. The task/variant `env:` maps and the
verifier's own `verify.env` are layered on top, so a fixture path travels in `verify.env`. A
**non-zero verifier exit is an operational failure, not a failing verdict** — a failing check is
`verifier.pass: 0` at exit `0`. On a crash the scores are not applied and the error is stamped
into the cell's `verify.json`; keep verifiers total so a real regression never hides behind a
broken comparator.

### The bench → verify → regress cycle

Record once, iterate the verifier at zero agent cost, then gate:

```sh
catacomb bench sql.yaml --runs-dir runs        # agents + inline verification
catacomb verify sql.yaml --runs-dir runs       # re-score saved evidence, no agent spend
catacomb regress --runs-dir runs \
  --baseline label:basket=sql,variant=baseline \
  --candidate label:basket=sql,variant=candidate   # gates on ann:verifier.pass
```

`regress` auto-loads each cell's `scores.jsonl` and gates on `verifier.pass` by default (no
`--annotation` flag): when every value is `0`/`1` the key is gated as a rate with the same Wilson
bounds as presence, so a candidate whose pass rate drops out of the baseline band is a
`regression`. This is exercised on a real `claude -p` every week — a wrong SQL result-set failing
verification — by the SQL basket in the [live gate](#continuous-live-validation).

### Keep ground truth out of the workdir

The agent runs in the cell's workdir with whatever tools the task allows, and anything reachable
there is fair game for a model that has learned to satisfy a check by reading its answer. Keep the
golden — and any answer key the verifier compares against — **outside** the workdir, and hand its
path to the verifier through `verify.env` (or the driver's ambient env), rather than staging it
beside the agent's output. The verifier resolves the captured artifact from the evidence dir and
the golden from its own path, so the two never share a directory the agent can see.

### What capture does to artifacts

Captured artifacts are not byte-faithful copies — write verifiers against their *content*, not
their exact bytes:

- **Text artifacts are redacted and normalized.** A captured text file (valid UTF-8, no NUL) has
  its secrets redacted line by line, blank lines dropped, and a trailing newline forced. Compare
  parsed values (rows, fields, numbers), never a whole-file hash.
- **Binary artifacts are copied raw**, subject to the per-file (10 MiB) and total (50 MiB) caps;
  their fidelity — and staying under the caps — is the task author's responsibility.
- **`verify.json` error text is not redacted.** The operational-failure detail recorded on a
  verifier crash passes through verbatim, so keep secrets out of a verifier's stderr and error
  messages — print diffs and counts, not raw inputs.

## Compare two runs

`catacomb diff` diffs two session transcripts by `step_key` and reports added, removed,
changed, and unchanged steps with per-field deltas (args, status, cost, duration, tokens).

```sh
catacomb diff session-a.jsonl session-b.jsonl
catacomb diff session-a.jsonl session-b.jsonl --json
```

The inputs are transcript files — either straight from `~/.claude/projects/<project>/` or the
redacted `session.jsonl` inside a bench evidence dir.

## Checkpoints and phase-scoped diff

Checkpoints let you name phases within a session so you can scope diffs and subgraph
views to a specific window of work.

### Placing markers

The agent places a phase boundary by calling the `mcp__catacomb__mark` tool, served by
`catacomb mcp` — a stdlib stdio MCP server that ships with catacomb (no hand-rolled stub
needed). Wire it in with `--mcp-config`:

```json
{"mcpServers":{"catacomb":{"command":"catacomb","args":["mcp"]}}}
```

The server named `catacomb` exposing the `mark` tool surfaces to the agent as
`mcp__catacomb__mark`. The call is a pure acknowledgement — the tool-call record lands in
the transcript, and the catacomb reducer synthesizes the marker from it when the transcript
is parsed, so the tool needs no configuration and fails open. See [cli.md](cli.md#mcp) for
the tool schema and the `--strict-mcp-config` note. A CLAUDE.md instruction telling the
agent *when* to mark (e.g. "call `mcp__catacomb__mark` with `name: plan` before planning
and after tests pass") completes the wiring.

The tool takes `name`, `boundary` (`start` or `end`), and optionally `occurrence` and
`state_ref`.

Repeated phases of the same name pair by LIFO nesting when `occurrence` is omitted: each
`end` closes the most recently opened, still-open phase of that name, so nested same-name
phases bracket correctly (the inner one closes first). Occurrence numbers follow start
order (first start is `0`). Reach for an explicit `occurrence` on both the start and the
end only when same-name phases genuinely overlap (neither nested nor sequential) — there
LIFO cannot tell which end belongs to which start, and the explicit occurrence pins the
pairing.

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

For a within-run comparison — same session on both sides, different phases — pass the
same transcript as both `A` and `B` with different `--a-phase`/`--b-phase` selectors.

### Extracting a phase subgraph

`catacomb subgraph` extracts the execution subgraph delimited by a checkpoint phase and
prints node/edge counts plus node lines.

```sh
catacomb subgraph session.jsonl --phase plan
catacomb subgraph session.jsonl --from plan --to impl
catacomb subgraph session.jsonl --phase plan --json
```

## Export for downstream tooling

`catacomb export` turns a transcript or a bench evidence directory into a materialized
JSONL graph snapshot — `{"kind":"node"…}`, `{"kind":"edge"…}`, and `{"kind":"run"…}`
records:

```sh
catacomb export ~/.catacomb/runs/<run-id> --out run.jsonl
catacomb export ~/.claude/projects/<project>/<session>.jsonl --out session-graph.jsonl
```

This snapshot is the input format of the
[DeepEval bridge](https://github.com/realkarych/catacomb/tree/master/integrations/deepeval)
and a convenient shape for ad-hoc analysis (`jq`, notebooks, dashboards). See
[export](cli.md#export) for flags.

## Continuous live validation

The offline gate is itself validated end-to-end against the real `claude -p` CLI by the
[E2E Live Gate](../../.github/workflows/e2e-live.yml) workflow (`e2e/run.sh`), a
CI-portable rerun of the [PV-6b calibration](../reviews/2026-07-08-pv6b-live-calibration.md)
methodology. It runs three live baskets and asserts the gate's behavior on the real
evidence: the A-vs-A controls must raise no presence or verifier false positives at default
sensitivity (their continuous metrics are asserted at a widened band, since sequential
batches drift on API latency, cost, and tokens), while a seeded checkpoint-presence
regression (phase axis), a seeded continuous (`tokens_out`) regression, and a seeded
verifier-contract regression (a wrong SQL result-set that fails verification, gating on
`ann:verifier.pass`) must each gate at default thresholds, and a seeded step-scope
regression (a guaranteed Bash step that vanishes) must surface in the report — all
attributed to the swapped instruction. It also smoke-tests baseline
pin/record/trends, diff/subgraph/export, and the external-scores path on the live runs.
Each bench cell invokes `claude -p` with `--setting-sources project` and a strict MCP
config, isolating child runs from user-scope hooks and plugins so a local run matches CI.
The checkpoint (mark) and SQL (verifier) tasks run on Sonnet for instruction-following
reliability while the step and continuous tasks stay on Haiku, which also exercises
multi-model pricing.

Because it spends real API budget (~$1.7 per run), it is not part of per-PR CI: trigger it
by hand from the Actions tab (`workflow_dispatch`) or let the weekly schedule run it. It
needs either the `ANTHROPIC_API_KEY` repository secret (API billing) or
`CLAUDE_CODE_OAUTH_TOKEN` (a Claude Pro/Max subscription; generate it with
`claude setup-token`); when both are set the API key wins. It fails fast with a clear
message when neither is set.
