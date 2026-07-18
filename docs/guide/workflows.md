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
    checkpoints: [plan, tests.pass]   # agent must be wired to call mcp__catacomb__mark — see Placing markers
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

### Ship the baseline to CI

A `name:` baseline is two halves — the store row and the pinned evidence dirs — and an
ephemeral CI runner starts with neither. Move both as one artifact: export the bundle once
(and again on every golden refresh), store it where the job can fetch it (a CI artifact, a
release asset, the object store your CI already uses), and import it at job start:

```sh
# once, on the machine that pinned the baseline
catacomb baseline export checkout-main --out checkout-main.tar.gz

# at job start, on the ephemeral runner
catacomb baseline import checkout-main.tar.gz --db catacomb.db --runs-dir runs
catacomb regress --db catacomb.db --runs-dir runs \
  --baseline name:checkout-main \
  --candidate label:basket=checkout,variant=candidate
```

The bundle is byte-deterministic and hash-verified on import (see
[`baseline export`](cli.md#baseline-export) / [`import`](cli.md#baseline-import)): the same
golden group restores bit-identically on every runner, and a corrupted or tampered artifact
fails the job with exit `2` instead of gating against damaged evidence. Import rewrites the
baseline's recorded runs dir to the local `--runs-dir`, so the gate resolves without a
runs-dir warning. The bundle carries the baseline and its evidence only — recorded
`--record`/`trends` history stays a persistent-store concern.

The alternative — committing `catacomb.db` and the baseline's `runs/` tree to the repository
(or uploading and restoring both halves by hand) — still works, but it bloats the repo,
invites binary-database merge conflicts, and ties no integrity check between the row and the
evidence it references; prefer the bundle.

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

The same history also reads as an accuracy-vs-cost trade-off. `--pareto` turns each recorded
comparison into a point — accuracy is the candidate's recorded `verifier.pass` rate, cost its
recorded `cost_usd` — plus one row for the baseline itself:

```sh
catacomb trends checkout-main --pareto
```

Read the table top-down: rows sort by cost ascending, then accuracy descending, so the Pareto
frontier — the candidates no other row beats — leads the table. A `DOMINATED yes` row is a
strictly worse deal: some row above it is at least as accurate *and* at least as cheap, with a
strict advantage on one of the two — same accuracy for more money, or less accuracy for the
same money, has no reason to win. Two rows equal on both axes — an A-vs-A control recorded
against its own baseline, say — dominate nothing and both stay `no`: domination needs a strict
advantage somewhere, and between exact ties the table has no opinion. A row that lacks an axis
(recorded before the task had a verifier, when only one side of the comparison carried it, or
written without a cost axis by another tool) is listed but never compared — it carries no
verdict either way, and a note under the table counts such rows. Rows are comparable to the
extent the recorded comparisons share the task basket: the table marks baseline redefinition
(the `*` splice marker) but not drift in candidate composition.

`trends` reads the store read-only; see [`trends`](cli.md#trends) for the full table shapes, the
[Pareto column and JSON semantics](cli.md#accuracy-vs-cost-pareto), the `--json` form, and exit
codes. Running many repositories? Stamp each recorded comparison with `--project` and join the
exports fleet-side — see [Roll up a fleet](#roll-up-a-fleet).

### Self-check your gate

The continuous metric bands (`duration_ms`, `cost_usd`, tokens) are a fixed tolerance, not a
hypothesis test — their false-positive behavior depends on how much your environment drifts
between identical runs. Before trusting a red verdict on one of those axes, audit the gate
against the variant's *own* recorded runs
([ADR-0034](../adr/0034-gate-self-check.md)):

```sh
catacomb calibrate --runs-dir ~/.catacomb/runs \
  --group label:basket=checkout,variant=main
```

`calibrate` takes **one** selector, splits its runs into a time-ordered first and second half,
and runs the full gate over that A/A split — both halves are the same variant, so **a drift
finding is not a real regression**: a gating verdict here means environmental drift across the
recorded sequence (API latency, runner load, a model-side change) or an outlier run. With 7+
runs it also drops each run in turn and names every run whose removal flips the overall verdict,
so a verdict riding on a single outlier is visible before you act on it. The split needs
`2 × --min-support` runs (6 at defaults) and reports `insufficient` with the k it needs below
that. Pass the same threshold flags your CI gate uses — the point is to audit the verdict
function as configured — and read the result as a pre-flight check: if the baseline's own
history gates against itself on `duration_ms`, a red `duration_ms` verdict against that baseline
is not trustworthy evidence. `calibrate` itself never gates: exit `0` on every rendered
self-check, `2` only on operational errors. It deliberately reports no false-positive *rate*
and suggests no threshold — see [calibrate](cli.md#calibrate) for both non-goals and the full
report shape.

### The true k-vs-k A/A check

`calibrate` audits the runs you already have, for $0 — but its halves each carry only half the
support your real gate runs at, and one overlapping half-split of a small group is an audit, not
a false-positive observation. The statistically honest way to observe false-positive behavior at
the real operating support is to spend bench budget on it: run **one** variant at `2k` reps and
`regress` its first `k` against its last `k` as two disjoint groups at the full support `k` your
gate actually uses.

Selectors match labels exactly (there is no `rep` range syntax), so declare the same variant
twice — identical id-only entries, no setup difference — and let bench's task → variant → rep
cell order make the second entry the *later* half of the wall clock:

```yaml
# checkout-aa.yaml — same basket, one real variant declared twice
basket: checkout-aa
reps: 5   # k: the per-side support your gate runs at
tasks:
  - id: work-task
    cmd: ["claude", "-p", "work the checkout task", "--output-format", "stream-json"]
variants:
  - id: main
  - id: main-later   # identical to main: the last k of a 2k-rep A/A
```

```sh
catacomb bench checkout-aa.yaml
catacomb regress --runs-dir ~/.catacomb/runs \
  --baseline label:basket=checkout-aa,variant=main \
  --candidate label:basket=checkout-aa,variant=main-later
```

Nothing changed between the groups except time, so any `regression` here is a directly observed
false positive at your exact thresholds and support — the same observation `calibrate` can only
approximate from half-support halves. Exit `1` means the gate as configured would have flagged
this variant against itself: widen the offending band deliberately, or treat that axis's
verdicts as advisory for this basket. Repeat the run to accumulate observations before drawing a
rate — one A/A batch is one observation, not a false-positive rate (the same honesty rule
[`calibrate`](cli.md#calibrate) applies to itself). The project's own live E2E does exactly this
control weekly (`e2e/run.sh`, the A-vs-A step) and is where the documented ~2× `duration_ms`
inter-batch drift was measured.

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
  discloses it, no matter how strong the drift. Under `--strict` that is a structural failure,
  not a flaky one: with fewer tasks than `--paired-min-tasks` in the basket the paired findings are
  always `insufficient`, so an otherwise-clean report lands `insufficient` — never `ok` — and
  exits `1` on every run, however many repetitions you add. Grow the basket to five tasks, or
  lower `--paired-min-tasks` as a conscious choice. Five or more tasks per basket give systematic
  sub-band drift a path to gate; see
  [the paired sign test](#catching-drift-below-the-band-the-paired-sign-test).
- **Lean on checkpoints when the change rewrites prompts.** Changing the component under test
  alters some prompt hashes, so `step_key` alignment degrades and step-level coverage drops.
  Below `--coverage-floor` (default 0.7) step verdicts are downgraded to `notable`, and the
  checkpoint (phase) level — robust to step drift by construction — carries the verdict. Declare
  task `checkpoints:` and wire `mcp__catacomb__mark` so there is always a stable, noise-robust
  comparison axis; see [Checkpoints and phase-scoped diff](#checkpoints-and-phase-scoped-diff).

## Gate a PR with the Action

The CI gate above — install catacomb, bench the candidate, `regress`, let the exit code
fail the job — ships prepackaged as a composite GitHub Action:
[`catacomb-gate`](../../.github/actions/catacomb-gate/README.md)
([ADR-0033](../adr/0033-github-action.md)). It installs a pinned, checksum-verified
catacomb release, optionally restores a baseline, optionally benches the PR's basket,
runs the gate, posts the verdict as a **sticky PR comment** — the exact
`regress --format markdown` output ([report formats](cli.md#report-formats)) — and
re-raises the `regress` exit code so the check fails on a regression.

The action currently lives *inside this repository* under
`.github/actions/catacomb-gate` — it is not a standalone marketplace action yet
(extraction is a follow-up) — so a caller workflow references it by its in-repo path:

```yaml
name: catacomb gate

on:
  pull_request:

permissions:
  contents: read
  pull-requests: write # for the sticky verdict comment

jobs:
  gate:
    runs-on: ubuntu-latest
    env:
      ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }} # bench drives the real claude CLI
    steps:
      - uses: actions/checkout@9c091bb21b7c1c1d1991bb908d89e4e9dddfe3e0 # v7.0.0
        with:
          persist-credentials: false
      - uses: realkarych/catacomb/.github/actions/catacomb-gate@master # pin to a tag/SHA
        with:
          version: v0.2.0
          basket: eval/basket.yaml
          baseline-bundle: eval/baseline.bundle
          baseline: label:variant=main
          candidate: label:variant=pr
          runs-dir: ${{ runner.temp }}/catacomb-runs
```

(A workflow in the catacomb repository itself uses the local form,
`uses: ./.github/actions/catacomb-gate` — that is exactly what the
[hermetic self-test](../../.github/workflows/action-selftest.yml) does.)

Three notes keep the recipe honest:

- **Bench on PRs spends real API budget.** With `basket` set (and `candidate-runs-dir`
  empty), every PR run executes the basket's cells against your agent CLI, with auth
  from the job `env` (`ANTHROPIC_API_KEY`, or `CLAUDE_CODE_OAUTH_TOKEN`). Size the
  basket and its `reps` for PR budgets — or bench elsewhere and hand the evidence in
  via `candidate-runs-dir`, which skips the bench step entirely.
- **`baseline-bundle` is the forward-looking restore path.** It runs
  `catacomb baseline import` before gating, and that subcommand ships with the
  ADR-0032 baseline-bundle work — no tagged catacomb release carries it yet, so on
  current releases the import step fails with catacomb's own unknown-command error.
  Until it ships, restore the baseline the way [the CI gate above](#pin-a-baseline)
  does: point `runs-dir`/`db` at evidence that already holds the pinned group
  (committed, or restored from an artifact) and select it with `baseline`.
- **The exit code is re-raised after the comment posts.** `0` passes the check; `1`
  (regression — with `strict`, also insufficient data) fails it; `2` means the gate
  *could not run* and also fails it, with a comment that says so rather than
  rendering a false regression. The action exposes `verdict`, `exit-code`, and
  `report-json` outputs for downstream steps.

The full input/output table — including `reps`/`model` env passthrough, `strict`,
`comment`, and the `catacomb-bin` test seam — lives in the
[action README](../../.github/actions/catacomb-gate/README.md).

## Importing a hand-run interactive session

`bench` drives the agent for you, but sometimes you run the agent **by hand** — a session
in the interactive Claude Code TUI, where you type the prompts and watch it work. That
session's transcript is just as real as a bench cell's, and
[`catacomb import`](cli.md#import) ingests it into the same evidence shape, so
[`verify`](cli.md#verify) and [`regress`](cli.md#regress) treat it exactly like a
bench-recorded cell. It is a second entry point to the same gate, one cell at a time.

The one thing to arrange up front is the **session id**, so `import` can find the
transcript afterward. Pin it before you start:

```sh
SID=$(uuidgen)
claude --session-id "$SID" --mcp-config catacomb-mcp.json
```

Then do the task by hand. To keep the checkpoint axis, call the `mcp__catacomb__mark` tool
at each phase boundary during the session (the [MCP marker tool](#placing-markers) works
the same whether an agent or you invoke it) — the marks ride the transcript and `import`
honors them. When you are done, ingest the finished session against the basket that
declares the task's `verify:`, `checkpoints:`, and labels:

```sh
catacomb import checkout.yaml --task work-task --variant candidate --session-id "$SID"
catacomb verify checkout.yaml --runs-dir ~/.catacomb/runs
catacomb regress --runs-dir ~/.catacomb/runs \
  --baseline label:basket=checkout,variant=main \
  --candidate label:basket=checkout,variant=candidate
```

`import` writes a redacted `~/.catacomb/runs/<run-id>/` evidence dir (default run-id
`import-<basket>-<task>-<variant>-r<rep>`), synthesizing the `task:<id>` marker window from
the transcript's first and last timestamps. The task `cmd` is ignored — you ran the agent
yourself — and `verify` stays a separate step, so the imported cell flows through the same
`bench → verify → regress` cycle. Repeat the import with different `--rep` values (or
`--run-id`s) to build a group `regress` can gate.

**If you did not pin the id**, fall back to `--transcript` and point it at the newest
transcript under your project's Claude projects dir — Claude Code writes each session to
`~/.claude/projects/<encoded-cwd>/<session>.jsonl`, encoding the working directory into the
folder name:

```sh
catacomb import checkout.yaml --task work-task --variant candidate \
  --transcript "$(ls -t ~/.claude/projects/<encoded-cwd>/*.jsonl | head -1)"
```

See [import](cli.md#import) for the full flag table, the omitted-`cost_usd` note, and exit
codes.

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

## Auditing cells

The gate compares group medians, so a single cell that cheats its way to a pass — or burns
50× the tokens getting there — can hide inside a clean verdict. The audit loop is the
counterpart: deterministic per-cell outlier flags point at runs worth reading, `pack`
bundles their already-redacted evidence for an external reviewer, and the reviewer's
findings come back through the same scores gate. Catacomb never calls an LLM itself — the
judge, its prompt, and its budget stay outside, the same boundary as
[external scores](#gate-on-external-scores-optional).

1. **Read the audit flags.** Every `regress` report screens each group's cells against the
   group median on `duration_ms`, `cost_usd`, `tokens_in`, `tokens_out`, and `turns`
   ([rule and thresholds](cli.md#per-cell-outlier-audit); tune with `--audit-iqr-factor`
   and `--audit-rel-delta`). A flag is an epilogue line, never a verdict — the exit code
   is untouched:

   ```text
   audit: candidate run bench-sql-sql-candidate-r7 (task sql) tokens_out 1932 vs group median 243 (band 121.5)
   ```

   Trust `tokens_out` and `turns` flags most. Cost and duration are noisy by nature —
   under prompt caching, real per-run cost spreads up to ~5× between byte-identical runs
   (measured 2026-07-08, [PV-6b](../internal/reviews/2026-07-08-pv6b-live-calibration.md)), and wall-clock duration
   inherits runner load — so read those flags as "look here", not as evidence by
   themselves.

2. **Pack the evidence.** [`pack`](cli.md#pack) exports a deterministic sample of the
   flagged group — same-run-list-every-time stride sampling, evidence copied verbatim,
   redacted by construction — plus a `pack.json` manifest and an `INSTRUCTIONS.md`
   briefing for the reviewer:

   ```sh
   catacomb pack label:basket=sql,variant=candidate --runs-dir runs --out audit-pack
   ```

3. **Inspect it externally.** The bundle is self-describing, so any reviewer works: a
   human reading transcripts, or an LLM you drive yourself — for example:

   ```sh
   claude -p "Read audit-pack/INSTRUCTIONS.md, inspect every run directory in
   audit-pack/, and write your verdicts to findings.jsonl exactly as instructed."
   ```

   The prompt, the model, and the spend are yours; catacomb only defines the contract:
   one JSONL line per run-level finding, an `audit.`-prefixed key, a numeric value, the
   `run_id` it applies to, and a `tool` field naming the judge that produced the line
   (optionally `tool_version` and `prompt_hash` too) —
   `{"key":"audit.clean","value":1,"run_id":"<run id>","tool":"<judge name>"}`.

4. **Gate the findings.** The returned file is an ordinary [`--scores`](cli.md#gating-on-external-scores)
   file, and `--annotation` declares the gate direction:

   ```sh
   catacomb regress --runs-dir runs \
     --baseline label:basket=sql,variant=baseline \
     --candidate label:basket=sql,variant=candidate \
     --scores findings.jsonl --annotation audit.clean:higher-better
   ```

   When every value is `0`/`1` the key gates as a rate (the same Wilson-bounds rule as
   `verifier.pass`); a key scored on only one side reports `insufficient` rather than
   guessing, so partial audits surface instead of silently passing. And because each
   finding carries `tool` provenance, the same file can first be calibrated against a
   gold set or aggregated across several reviewers via `catacomb-judge` before it
   gates — see [Calibrating a judge](#calibrating-a-judge).

## Calibrating a judge

The scores this section calibrates are exactly the provenance-stamped findings a
[`pack` audit](#auditing-cells) — or any external judge writing the same scores-JSONL
dialect — produces, so the full loop reads: pack the flagged cells, judge them (each
line stamped with `tool`), calibrate the judge against a gold set
(`agreement --min-kappa`) and/or aggregate a `panel`, then gate the calibrated scores
with [`regress --scores`](cli.md#gating-on-external-scores). At no point does catacomb
run a judge: it defines the provenance contract, calibrates whatever judge you bring
against measured human agreement before that judge may gate anything, and gates
deterministically on the result — nothing leaves your machine beyond the pack you
chose to ship.

An LLM judge is a measurement instrument, and an uncalibrated instrument should gate
nothing. Before a judge's scores join the gate, measure the judge against a small
hand-labeled gold set; when single-judge agreement stalls, aggregate several judges
into a panel instead of prompt-tuning one judge forever. Both utilities live in
[`integrations/judge`](../../integrations/judge/README.md) (`catacomb-judge`,
stdlib-only), read the same scores-JSONL dialect the gate consumes, and run fully
offline — the judge itself, its prompt, and its spend stay outside catacomb, the same
boundary as the audit loop above.

1. **Hand-label a gold set.** One JSONL line per verdict, on the same coordinates the
   judge scored — `run_id` plus the annotation `key` (plus `step_key` for step-level
   scores):

   ```json
   {"run_id": "bench-checkout-work-task-candidate-r1", "key": "judge.groundedness", "label": 1}
   ```

   The canon's bar is lower than it looks: **~40 labeled transcripts suffice** to expose
   a judge that cannot be trusted ([gap
   review](../internal/reviews/2026-07-12-eval-best-practices-gap-review.md) §2.4, 2026-07-12). Duplicate
   labels for one coordinate are rejected outright — a gold set must be unambiguous.

2. **Measure agreement.** Point the calculator at the labels and the judge's scores
   (files or a whole evidence dir); each distinct `tool` on the score lines is reported
   as its own judge:

   ```sh
   catacomb-judge agreement --labels gold.jsonl --runs-dir runs
   ```

   Spearman ρ ranks the raw values; κ and TPR/TNR see both sides binarized at
   `--threshold` (default 0.5 — binary 0/1 judges pass through unchanged). TPR and TNR
   replace accuracy on purpose: on an imbalanced gold set a judge that answers "pass"
   every time scores high accuracy while its TNR is 0. A metric that would be
   meaningless is omitted (`-` in the table, absent key in `--json`), not fudged:
   Spearman under two pairs or at zero variance, κ when both binarized sides are
   constant at the same value, TPR without positive labels, TNR without negative ones.

3. **Gate the calibration.** `--min-kappa 0.8` turns the report into a pass/fail step —
   the canon treats **κ > 0.8** as the trust threshold for letting a judge gate
   anything. Exit 1 names each judge whose κ falls below the bar *or is omitted*
   (an unmeasurable judge is an uncalibrated judge). The comparison is strictly `<`,
   so a judge at exactly the bar passes; the pooled `overall` row never gates:

   ```sh
   catacomb-judge agreement --labels gold.jsonl --runs-dir runs --min-kappa 0.8
   ```

4. **Aggregate a panel.** Heterogeneous judges wash out each other's biases. `panel`
   groups score lines by (run_id, key[, step_key]) — one judge per distinct `tool`,
   provenance required — and emits one line per group in the same scores dialect:
   the mean by default (binary in → agreement fraction out), or `--vote` for a strict
   majority over an odd panel. The output is ordinary [`--scores`
   input](cli.md#gating-on-external-scores), so the loop closes on the existing gate:

   ```sh
   catacomb-judge panel --runs-dir runs --key judge.groundedness --vote --out panel.jsonl
   catacomb regress --runs-dir runs \
     --baseline label:basket=checkout,variant=baseline \
     --candidate label:basket=checkout,variant=candidate \
     --scores panel.jsonl --annotation judge.groundedness:higher-better
   ```

Two caveats travel with every judge, calibrated or not: never pick the judge from the
model family being tested — self-preference bias survives calibration — and re-measure
whenever the judge's prompt or model changes, because κ certifies the (judge, prompt,
threshold) triple you measured, not the judge in general.

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

## Roll up a fleet

One gate guards one repository; a fleet is many repositories each running their own
gate. Catacomb deliberately ships no hosted service, collector, or daemon to aggregate
them ([ADR-0026](../adr/0026-form-factor-pivot-offline-eval-gate.md)) — what it owns is
the stable, versioned per-repo JSON contract, and the roll-up is a join in whatever
warehouse your org already runs.

Give each repository's CI a stable project id and stamp it into every recorded
comparison (`--project` requires `--record`), then export the history from the same
job:

```sh
catacomb regress --baseline name:golden --candidate label:variant=candidate \
  --record --project payments-api --strict
catacomb trends golden --json > trends-payments-api.json
```

`--project` writes a `project` field into the record body, and `trends --json` replays
the stored bodies verbatim, so every exported row self-identifies its repository. A
fleet job collects the per-repo files — build artifacts, an object-store bucket,
whatever the CI already has — flattens them, and loads them into the org's warehouse
(BigQuery, Splunk, a data lake), joining on `project`:

```sh
jq -c '.[].record' trends-*.json > fleet-records.jsonl
```

Each line is one recorded comparison carrying `project`, `created_at`, the thresholds,
the full report, and the recording binary's version `stamps` — enough to chart verdict
rates, cost drift, or `verifier.pass` rates across the fleet with no catacomb binary in
the loop. The body is versioned (`v`, additive-only; see the
[versioning policy](../VERSIONING.md)), so a loader keyed on documented fields survives
upgrades, and records written before `--project` existed simply lack the field.

The `project` stamp lives on the recorded history, not on evidence dirs. Per-run
evidence is already labeled (`basket`, `task`, `variant`, `rep` on every [`bench`](cli.md#bench)
cell, and [`import`](cli.md#import) `--label project=payments-api` adds free-form
pairs), and evidence stays local to each repository's CI — only the recorded history
JSON travels.

## Continuous live validation

The offline gate is itself validated end-to-end against the real `claude -p` CLI by the
[E2E Live Gate](../../.github/workflows/e2e-live.yml) workflow (`e2e/run.sh`), a
CI-portable rerun of the [PV-6b calibration](../internal/reviews/2026-07-08-pv6b-live-calibration.md)
methodology. It runs a suite of live Claude baskets and asserts the gate's behavior on the real
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
The five delegation-sensitive baskets — checkpoint (mark), SQL (verifier), subagent,
skill, and MCP — run on Sonnet for instruction-following and delegation reliability,
while the continuous, echo (step), and failure-mode tasks stay on Haiku (which also
exercises multi-model pricing); a `$0` preflight guardrail enforces this mixed-model
policy so a blanket-Haiku swap fails the run early. An optional Codex leg runs after the Claude baskets when a
signed-in `codex` CLI is on the runner's PATH and is skipped otherwise, leaving the
overall exit unaffected. Beyond the original `e2e/basket-codex.yaml` (six live
`codex exec --json` cells on `gpt-5.4-mini`), it benches three more `gpt-5.4-mini`
baskets — MCP (`basket-codex-mcp`, **asserted**: a real stdio MCP server handshakes
with `codex exec`, and the seeded baseline-vs-degraded regression gates on a dropped
`mcp__e2ekit__record` node plus a failed verifier), subagent
(`basket-codex-subagent`, logged/soft, since codex delegation is prompt-discretionary
so the live verdict is logged and only a `regress` error fails the leg), and skill
(`basket-codex-skill`, artifact-substitute: codex emits no skill-invocation event, so
it verifies the work artifact plus a soft `SKILL.md`-read grep and asserts that **no**
structural skill node is produced — a documented Codex platform limitation) — then
re-runs the offline transforms over the resulting codex evidence. Codex reports token
counts but no dollar cost, so these cells are token-billed pennies and never contribute
to the run's cost total.

Because it spends real API budget (~$3–7 per run), it is not part of per-PR CI: trigger it
by hand from the Actions tab (`workflow_dispatch`) or let the twice-weekly (Mon+Thu)
schedule run it. It
needs either the `ANTHROPIC_API_KEY` repository secret (API billing) or
`CLAUDE_CODE_OAUTH_TOKEN` (a Claude Pro/Max subscription; generate it with
`claude setup-token`); when both are set the API key wins. It fails fast with a clear
message when neither is set.
