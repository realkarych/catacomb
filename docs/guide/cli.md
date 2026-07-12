# CLI reference

Catacomb is a single offline binary. Every command reads local files — Claude Code
transcripts under `~/.claude/projects`, [`bench`](#bench) evidence directories, or the
local SQLite store — and no command opens a network connection. Most read commands
accept `--json` for machine-readable output. For task-oriented recipes see
[workflows.md](workflows.md).

The command set:

| Command | Purpose |
| --- | --- |
| [`bench`](#bench) | Run a benchmark basket and record redacted evidence per cell |
| [`verify`](#verify) | Re-run a basket's verifiers offline over recorded evidence dirs |
| [`baseline`](#baseline-set) | Manage named baselines (`set`, `list`, `rm`) |
| [`regress`](#regress) | Compare a candidate run group against a baseline and gate |
| [`trends`](#trends) | Replay a baseline's recorded regression history |
| [`diff`](#diff) | Diff two session transcripts by `step_key` |
| [`subgraph`](#subgraph) | Extract the execution subgraph of a checkpoint phase |
| [`export`](#export) | Export a transcript or evidence dir as a JSONL graph snapshot |
| [`replay`](#replay) | Build a graph from one transcript and print a summary |
| [`mcp`](#mcp) | Run the stdio MCP server exposing the `mark` checkpoint tool |
| [`version`](#version) | Print the version |

Exit codes are uniform: `0` success, `1` regression (a stopped `--fail-fast`
basket, or a failing [`verify`](#verify) cell), `2` operational error (bad input,
missing files, store problems).

Any command that parses transcripts (`bench`, `regress`, `diff`, `subgraph`, `export`,
`replay`) may print up to two advisory lines to **stderr**: a format-drift count for
records it did not recognize, and a version-ceiling notice when a transcript's Claude
Code version is newer than the release this binary was tested against (for example
`warning: transcript Claude Code version 2.2.0 is newer than tested 2.1.199`). Both are
diagnostic only — `stdout`/`--json` stay clean and neither changes the exit code. See
[Format drift](privacy-and-operations.md#format-drift) for what they mean and what to do.

---

## bench

Run a benchmark basket: expand tasks × variants × reps into cells, execute each as a
plain local process, verify checkpoints against the recorded transcripts, and write a
manifest plus one evidence directory per cell.

```sh
catacomb bench <basket.yaml> [flags]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--manifest` | `<basket>.manifest.jsonl` | Manifest output path |
| `--resume` | false | Skip cells already recorded in the manifest |
| `--fail-fast` | false | Stop at the first failing cell |
| `--dry-run` | false | Print the cell expansion table and exit without executing |
| `--projects-dir` | `~/.claude/projects` | Claude projects directory holding session transcripts |
| `--runs-dir` | `~/.catacomb/runs` | Evidence output directory for bench runs |

A basket is a declarative YAML file. `tasks × variants × reps` expands to one *cell* per
combination, and cells run sequentially:

```yaml
basket: checkout
reps: 5
tasks:
  - id: add-item
    cmd: ["claude", "-p", "add an item to the cart", "--output-format", "stream-json"]
    dir: services/cart          # optional working directory
    env: { MODE: fast }         # optional per-task env
    checkpoints: [plan, tests.pass]   # optional declared phases to verify
    timeout: 30s                # optional per-task deadline (Go duration; unset = no limit)
variants:
  - id: baseline
    env: { MODEL: opus }        # optional per-variant env (wins over task env)
  - id: candidate
    env: { MODEL: sonnet }
    setup: ["git checkout feature"]   # optional pre-cell commands
```

Each cell runs under run-id `bench-<basket>-<task>-<variant>-r<rep>` and carries the
labels `basket`, `task`, `variant`, and `rep`, so `baseline` and `regress` selectors
work unchanged. Cell labels win over any inherited from the `CATACOMB_LABELS`
environment variable. The basket name and each task and variant `id` must match
`^[A-Za-z0-9._-]+$` (no spaces, commas, or `=`, which would corrupt `CATACOMB_LABELS`
and the epilogue selectors) and be at most 256 bytes; task and variant `id`s must be
unique, and baskets whose dash-joined ids would collide into the same run-id are
rejected at load.

Each cell's child runs as a plain local process with `CATACOMB_LABELS` (the merged
ambient + cell labels) and `CATACOMB_RUN_ID` (the cell's run-id) added to its
environment, while the runner peeks its stdout for the stream-json `session_id` and the
terminal `result` event's `total_cost_usd` — the task `cmd` must emit stream-json
(`claude -p <prompt> --output-format stream-json`), which is where the session id comes
from. After the child exits the runner resolves the session's transcripts under
`--projects-dir` — the main `<session-id>.jsonl` plus any `subagents/agent-*.jsonl` —
retrying for up to ~3 s while the file lands; a session id matching no transcript (or
more than one) records the reason in the cell's manifest `note` and skips verification
and evidence for that cell.

A task's optional `timeout:` — a Go duration string such as `30s` or `5m` — puts a
per-cell deadline on the whole cell: the variant's `setup:` commands and the child
process share one deadline. The value is validated at basket load (an invalid or
negative duration rejects the basket), and it is opt-in: unset means no deadline,
though `Ctrl-C`/`SIGTERM` still cancels the run either way.

For each cell the runner synthesizes `task:<id>` start/end phase markers from the
child's wall-clock start and end, giving `regress` a stable checkpoint axis even when
the agent forgets to mark its own. A task may also declare `checkpoints:` — phase names
the agent is expected to mark *itself* (via `mcp__catacomb__mark`, per a CLAUDE.md
convention; see [mcp](#mcp)) during the run. Each name must match `^[A-Za-z0-9._:-]+$`
(the colon is allowed here, unlike task and variant `id`s), be at most 256 bytes, be
unique within the task, and may not equal the reserved runner marker `task:<id>`. After
each cell the runner rebuilds the graph from the transcripts and checks which declared
checkpoints are present as markers. Verification is best-effort and **never gates**: it
is skipped when the cell surfaced no session id or the transcripts could not be
resolved or parsed (the skip reason is recorded in the manifest `note`). Checkpoints
absent from the graph are recorded in the manifest's `missing_checkpoints` list, warned
to stderr as `cell <run-id>: missing checkpoints: <names>`, and rolled up on success —
just before the copy-pasteable epilogue — into one
`checkpoints[<task>]: <name> <hit>/<verified>` summary line per declared name, where
`hit` counts the cells the marker was found in and `verified` counts the cells where
verification actually ran. Missing phases are visibility only here; they earn a verdict
downstream as presence-rate drops in `regress`.

Each cell whose transcripts resolved — including a cell whose child exited nonzero —
writes an evidence directory `<runs-dir>/<run-id>/` holding
secret-redacted copies of the transcripts (`session.jsonl`, `subagents/agent-*.jsonl`)
plus a `meta.json` (run id, task, variant, rep, session id, labels, exit code,
`cost_usd`, basket hash, the `task:<id>` marker window, and `finished_at`). An
evidence-write failure keeps the cell's result and notes the error. See
[Privacy and operations](privacy-and-operations.md) for what redaction removes.

`meta.json` also carries an `env` block stamping the environment the cell ran under:
`catacomb_version` (the recording binary), `model_id` (the model the child *actually*
ran, read from the transcript's assistant messages — ground truth, not the requested
model; omitted when the transcript carries no assistant turn), `claude_code_version`
(the transcript's Claude Code version; omitted when the transcript does not report
one), and `resources` (`os`, `arch`, and `cpus` of the host that executed the cell).
The stamps are descriptive provenance only — they never gate, join no `--strict` check,
and carry no hostname or runner identity: model drift between groups is often the very
axis under comparison, and host resources legitimately vary across runners. Sampling
parameters are not stamped — Claude Code transcripts do not report them, and the child
argv that could set them is already pinned byte-exactly by the basket hash. Evidence
recorded before the stamps existed simply lacks the `env` key and stays valid
everywhere.

The manifest is JSONL, written incrementally — one object per completed cell (run-id,
task, variant, rep, exit code, session id, `marked`, an optional `missing_checkpoints`
list, `cost_usd`, `evidence_dir`, basket hash, finish time, and an optional `note`).
`--resume` reads it back and skips cells already present; if the basket file changed
since the recorded run (its content hash no longer matches) resume errors out — delete
the manifest or revert the basket. If the manifest already has entries and you pass
neither `--resume` nor a fresh `--manifest`, `bench` refuses (exit `2`) rather than
silently appending a second run's cells.

`setup` commands run before **every** cell, in the task's working directory, as **plain
`exec`**: each line is split on whitespace and run directly, with **no shell** — pipes,
redirects, `&&`, quoting, variable expansion, and globbing are not interpreted. Wrap a
script if you need shell features. Setup inherits **only the parent process
environment** — not the task or variant `env`, and not
`CATACOMB_RUN_ID`/`CATACOMB_LABELS` — and because it re-runs before each cell it must
be **idempotent** (a `git checkout <branch>` is fine; an `echo >> file` that
accumulates is not).

A failing cell is recorded and the basket continues (deciding whether a change
regressed is `catacomb regress`'s job, not the runner's). Exit codes: `0` every cell
ran (even if some cells failed), `1` `--fail-fast` stopped at a failing cell, `2`
operational error (bad basket, a non-fresh manifest, manifest I/O, a resume hash
mismatch, or an unresolvable home directory — set `--projects-dir` and `--runs-dir`
explicitly). On success the runner prints a `marked <n>/<total> cells` summary, the
checkpoint rollup, and a copy-pasteable epilogue: with two or more variants, a
[`regress --runs-dir`](#regress) comparing the first two. Append `,task=<id>` to the
epilogue's `label:` selectors to narrow the comparison to a single task. When the
basket declares `reps < 5`, the epilogue also appends a one-line note recommending
`reps: 5` or more, because the rate gate cannot fire reliably below that (see
[Gate sensitivity at small k](workflows.md#gate-sensitivity-at-small-k)).

```sh
catacomb bench checkout.yaml
catacomb bench checkout.yaml --dry-run
catacomb bench checkout.yaml --resume --fail-fast
```

---

## verify

Re-run a basket's verifiers **offline** over already-recorded evidence directories,
without launching any agent. The basket file is the source of truth for each task's
`verify:` block (a `cmd`, an optional `env`, and an optional `timeout`), so a verifier can
evolve after the runs were recorded — fix a comparator, tighten a judge prompt, add a
score — and be replayed against the saved evidence at zero agent cost.

```sh
catacomb verify <basket.yaml> --runs-dir <dir> [--label k=v[,k=v...]]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--runs-dir` | `~/.catacomb/runs` | Evidence directory holding the recorded [`bench`](#bench) runs to re-verify |
| `--label` | (none) | Restrict to runs whose recorded labels match every comma-separated `k=v` term (AND) |

`verify` scans `<runs-dir>/*/meta.json` and re-verifies each recorded cell whose `basket`
label equals the basket's name, whose task id resolves to a basket task that declares
`verify:`, and — when `--label` is given — whose labels match every term. A cell whose
resolved task carries no `verify:` is skipped silently, as is any run belonging to another
basket or filtered out by `--label`. The variant `env` is taken from the matching basket
variant, resolved by the run's recorded `variant` label; a run whose recorded variant is
no longer in the basket is reported as a per-cell error and does not abort the rest.

Each matched cell runs the task's `verify.cmd` as a **plain `exec`** (argv, no shell) with
its working directory set to the cell's evidence dir and the verifier contract on its
environment:

| Variable | Value |
| --- | --- |
| `CATACOMB_EVIDENCE_DIR` | the cell's evidence dir (redacted transcripts, `meta.json`, captured `artifacts/`) |
| `CATACOMB_WORKDIR` | **empty** offline — there is no hot workdir, so a re-verifiable verifier reads only from evidence |
| `CATACOMB_RUN_ID`, `CATACOMB_BASKET`, `CATACOMB_TASK`, `CATACOMB_VARIANT`, `CATACOMB_REP` | the cell's coordinates from `meta.json` |
| `CATACOMB_AGENT_EXIT_CODE` | the agent child's recorded exit code |

The task and variant `env:` maps and the verifier's own `verify.env` are layered on top.
The verifier's stdout is scores JSONL (the [run-level scores](#run-level-scores) dialect)
and is rewritten to `<evidence>/scores.jsonl`; stderr passes through to the operator. A
verification record — `cmd`, a sha256 of cmd+env, exit code, duration, timestamp, and
`mode` (`offline` here, `bench` when [`bench`](#bench) ran the verifier inline) — is
written to `<evidence>/verify.json`, leaving the immutable `meta.json` execution ledger
untouched. Re-verification is idempotent: each run rewrites `verify.json` and
`scores.jsonl`, so a recorded verdict is reproduced by re-running.

A **non-zero verifier exit is an operational failure**, not a failing verdict (a failing
check is `verifier.pass: 0` at exit `0`): its scores are not applied and the failure is
recorded in that cell's `verify.json`. `verify` prints one line per matched cell to stdout
— `verify <run-id>: ok` or `verify <run-id>: error (<detail>)` — and when any matched
cell's recorded basket hash differs from the current basket file it prints one advisory
line to stderr (`warning: basket hash differs from recorded runs (verifiers may be newer
than the evidence)`).

Exit codes: `0` every matched cell verified cleanly, `1` one or more operational verifier
failures (each recorded in its `verify.json`), `2` operational error (a bad basket, an
unreadable `--runs-dir`, or a selector that matched no runs).

`verify` slots between [`bench`](#bench) and [`regress`](#regress): record once, iterate on
the verifiers offline, then gate. `regress --runs-dir` auto-loads each cell's rewritten
`scores.jsonl` and gates on `verifier.pass` by default (see
[Run-level scores](#run-level-scores)).

```sh
catacomb bench basket.yaml --runs-dir runs    # agents + inline verification
catacomb verify basket.yaml --runs-dir runs   # iterate on verifiers, zero agent cost
catacomb regress --runs-dir runs \
  --baseline label:basket=checkout,variant=a --candidate label:basket=checkout,variant=b
```

---

## baseline set

Create or replace a named baseline from a label selector over evidence directories,
resolved now.

```sh
catacomb baseline set <name> --label k=v [--label ...] [flags]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--label` | **required** (repeatable) | `k=v` selector; the baseline captures every run matching all terms (AND) |
| `--db` | `~/.catacomb/catacomb.db` | SQLite database path for the baselines table |
| `--runs-dir` | `~/.catacomb/runs` | Evidence dir to resolve the label selector from |

The command scans `<runs-dir>/*/meta.json`, matches the labels against each run's
recorded labels (all terms ANDed), sorts the matching run IDs, and persists them with
the selector, a created-at timestamp, the runs dir they were resolved from, and the
version stamps of the setting binary — the catacomb version and the step-key scheme —
under `<name>`; `regress` checks those stamps whenever the baseline is resolved by name
(see [Baseline version stamps](#baseline-version-stamps)). The store at `--db` is
created if it does not exist yet, so the very first store-touching command can be this
one.

The name must be non-empty, at most 128 bytes, and free of leading or trailing
whitespace; at least one `--label` is required. Errors when the selector matches no
runs. Re-running with the same name replaces the stored baseline. The evidence dirs are
not copied — [`regress`](#regress) re-reads the pinned runs from disk and warns when
pointed at a different directory. A saved baseline is referenced by `regress` as
`name:<baseline>`, so a golden group survives later label churn.

```sh
catacomb baseline set golden --label basket=checkout --label variant=main
catacomb baseline set golden --label basket=checkout,variant=main --runs-dir ~/.catacomb/runs
```

---

## baseline list

List stored baselines.

```sh
catacomb baseline list [flags]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--db` | `~/.catacomb/catacomb.db` | SQLite database path |
| `--json` | false | Emit JSON output |

Prints a table with columns `NAME`, `RUNS`, `SELECTOR`, `CREATED`, sorted by name;
`SELECTOR` shows the sorted `k=v` terms and `CREATED` is a UTC RFC3339 timestamp.
`--json` emits the stored records including each baseline's resolved run IDs, the
`runs_dir` it was resolved from, and the version `stamps` (catacomb version and
step-key scheme) recorded at set time. On a store created by an older binary this
command fails with a hint to run a write-path command (`baseline set`) to migrate the
schema.

```sh
catacomb baseline list --json
```

---

## baseline rm

Remove a stored baseline.

```sh
catacomb baseline rm <name> [flags]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--db` | `~/.catacomb/catacomb.db` | SQLite database path |

Deletes the named baseline.

```sh
catacomb baseline rm golden
```

---

## regress

Compare a candidate run group against a baseline and gate on the verdict.

```sh
catacomb regress --baseline <selector> --candidate <selector> [flags]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--baseline` | (empty) | Baseline selector: `label:k=v[,k=v...]` or `name:<baseline>` |
| `--candidate` | (empty) | Candidate selector (same grammar) |
| `--runs-dir` | `~/.catacomb/runs` | Evidence dir to resolve selectors from: `label:` scans it, `name:` reads `--db`'s baselines table, `--record` appends there |
| `--db` | `~/.catacomb/catacomb.db` | SQLite database path for `name:` baselines and `--record` |
| `--json` | false | Emit the full report as JSON |
| `--strict` | false | Treat an insufficient-data verdict as a failure (exit `1`); refuse a stampless or stamp-mismatched `name:` baseline (exit `2`). A basket with fewer tasks than `--paired-min-tasks` always carries paired `insufficient` findings, so with every other axis clean it reports `insufficient` — never `ok` — and fails `--strict` structurally: more repetitions cannot fix it; add tasks, or lower `--paired-min-tasks` deliberately |
| `--record` | false | Append this comparison to the baseline's history for [`trends`](#trends) (requires `--baseline name:<x>`) |
| `--annotation` | (none) | Numeric annotation to gate on: `owner.key[:higher-better\|lower-better]` (repeatable) |
| `--scores` | (none) | JSONL file of external scores applied as node annotations before comparison (see [Gating on external scores](#gating-on-external-scores)) |
| `--min-support` | 3 | Minimum runs per group for a trusted comparison (must be ≥ 1) |
| `--presence-delta` | 0.2 | Presence-rate delta threshold |
| `--error-delta` | 0.1 | Error-rate delta threshold |
| `--annotation-rate-delta` | 0.1 | Rate delta threshold for run-level binary annotations (e.g. `verifier.pass`; must be > 0) |
| `--paired-alpha` | 0.05 | Significance level for the paired per-task sign test (must be in (0,1)) |
| `--paired-min-tasks` | 5 | Minimum matched tasks before the paired sign test can gate (must be > 0) |
| `--metric-rel-delta` | 0.25 | Relative metric delta threshold |
| `--iqr-factor` | 1.5 | IQR band factor for the metric noise band |
| `--coverage-floor` | 0.7 | Step-alignment coverage below which step verdicts are downgraded |
| `--z` | 1.645 | One-sided Wilson z for the rate gates (`1.645` = 95% one-sided); higher z requires stronger evidence to flag (flags less) |
| `--fail-on-notable` | false | Count `notable` findings toward the gate (exit `1`) |

Both selectors must be supplied, and both resolve against [`bench`](#bench) evidence
directories under `--runs-dir`:

- `label:k=v[,k=v...]` scans `<runs-dir>/*/meta.json`, matches the terms against each
  run's recorded labels (all terms ANDed), and rebuilds every matching run's graph from
  its redacted transcripts, re-applying the `task:<id>` marker window from `meta.json`
  so checkpoint phases and run timing carry over. No store is touched.
- `name:<baseline>` reads the baseline row from the `--db` baselines table (read-only)
  and loads its pinned run IDs from `<runs-dir>/<run-id>/` — every pinned run's
  evidence dir must be present and readable, or the command exits `2` naming the run
  and dir. A baseline records the runs dir it was resolved from; when the `--runs-dir`
  flag names a different directory, a stderr warning notes the recorded dir and the
  flag wins.

Groups are aggregated and compared per
[ADR-0022](../adr/0022-regression-detection-over-repeated-runs.md) §4:

- **Rates** (presence, error, and run-level binary annotations such as `verifier.pass`)
  use one-sided Wilson bounds (default z `1.645`, tunable with `--z`) and are flagged as a
  `regression` only when the baseline and candidate bounds are disjoint *and* the delta
  exceeds the threshold (`--presence-delta`, `--error-delta`, or `--annotation-rate-delta`
  respectively); a delta over the threshold with overlapping bounds is reported as
  `notable`, which gates only under `--fail-on-notable`. When even a maximal flip at the
  actual group sizes cannot reach `regression`, the report (human and `--json`) carries a
  `sensitivity:` note naming the smallest `k` at which the gate could fire; see
  [Gate sensitivity at small k](workflows.md#gate-sensitivity-at-small-k).
- **Metrics** (`duration_ms`, `cost_usd`, `tokens_in`, `tokens_out`, `occurrences`; run
  totals also `nodes`) flag the candidate median when it falls outside the baseline
  median ± `max(metric-rel-delta × |median|, iqr-factor × IQR)` band. The `nodes` and
  `occurrences` count metrics are one-sided (higher = flagged) per
  [ADR-0022 Amendments](../adr/0022-regression-detection-over-repeated-runs.md#amendments),
  so a pipeline that legitimately grew may need `--metric-rel-delta` raised to keep
  ordinary growth inside the band.
- **Paired per-task deltas** (scope `paired`): when both groups carry `task` labels
  (any [`bench`](#bench) basket does), every task present in both groups with
  `--min-support` runs per side contributes one delta per continuous metric
  (`duration_ms`, `cost_usd`, `tokens_in`, `tokens_out`) — the candidate per-task
  median minus the baseline per-task median. An exact one-sided sign test over the
  non-zero deltas (zero deltas are dropped) flags `regression` when the probability of
  seeing that many increases under no change is at most `--paired-alpha`; the detail
  always carries the evidence (`+7/8 tasks, p=0.03516`), `improvement` is symmetric,
  and a paired `regression` gates (exit `1`) like any other. All four paired metrics
  are tested at the same `--paired-alpha` with no multiplicity correction: they are
  strongly correlated, so the aggregate false-positive rate under the null is bounded
  by ~4× alpha and lands well below that in practice. This is the axis that
  catches systematic drift *below* the metric band: a +10% cost creep repeated across
  8 tasks fires at p=0.0039 while staying inside every median band. Fewer than
  `--paired-min-tasks` matched tasks reports `insufficient` instead of a guess, and
  whenever the paired layer is active but cannot fire — too few matched tasks, or
  unanimity at the current task count cannot reach `--paired-alpha` — the
  `sensitivity:` note names the smallest task count at which a unanimous shift would
  gate. An `ok` paired row is omitted from the findings like any non-total row. See
  [when the paired test fires](workflows.md#catching-drift-below-the-band-the-paired-sign-test).
- **Alignment coverage** (fraction of baseline steps matched in the candidate) is
  always reported; below `--coverage-floor` step-level regressions are downgraded to
  `notable` and the checkpoint (phase) level carries the verdict (under
  `--fail-on-notable` those downgraded findings still gate).
- Groups below `--min-support` yield an `insufficient` verdict instead of a guess.

### Task reliability (pass^k)

When both groups carry per-task `verifier.pass` outcomes (a [`bench`](#bench) basket
with a [verifier](workflows.md#verifying-task-outcomes)), the report carries a
`reliability` block: for a task with `n` scored runs and `c` passes,
`pass^k = C(c,k)/C(n,k)` — the unbiased estimate of "all `k` independent trials
succeed" — computed for `k` = 1..`k_max`, where `k_max` is the smallest scored `n` in
the group so curves stay comparable across tasks. `--json` carries the full per-task
curves plus the unweighted `mean` curve over tasks; the human report renders one
epilogue line per group with the mean curve's endpoints:

```text
reliability (candidate): pass^1 0.93 -> pass^5 0.67 (7 tasks)
```

A flat curve is a reliable agent; a steep drop from pass^1 to pass^k is a coin-flipper
that happens to average well. The block is **informational only — it never gates**: the
same binary data already gates through the `ann:verifier.pass` rate axis
([run-level scores](#run-level-scores)), and double-gating one signal would only
inflate false positives.

## Baseline version stamps

`baseline set` stamps every baseline with the versions that resolved it: the catacomb
version and the step-key scheme (`stepkey/v1`). Whenever `--baseline` resolves by
`name:`, `regress` compares those stamps against its own: a baseline with no stamps
(set by a pre-stamp catacomb) or with differing stamps prints a stderr warning, and
under `--strict` is refused as an operational error (exit `2`) instead. A step-key
scheme change shifts step identity between the pinned runs and the candidate, so a
cross-version comparison can quietly align nothing; after upgrading catacomb, re-run
`baseline set` to re-pin the group under the current stamps. The check covers the
`--baseline` side only (`label:` selectors carry no stamps, and a `name:` candidate is
not checked), and stdout and `--json` stay clean either way.

## Gating on external scores

`--annotation owner.key` folds a numeric annotation — an external scorer's verdict
supplied from a `--scores` file — into the comparison as if it were a built-in metric.
The annotation values aggregate per `step_key` and are flagged with the same median ±
`max(metric-rel-delta × |median|, iqr-factor × IQR)` band as other metrics, but with a
declared direction. When the same `step_key` occurs more than once within a single run,
that run's annotation values for the key are **summed** (like cost and tokens), and the
compared medians are taken across the per-run sums:

- `owner.key` or `owner.key:higher-better` (default): a higher score is better, so a
  candidate median that drops below the band is the `regression` and a rise is an
  `improvement`.
- `owner.key:lower-better`: a lower score is better, so the direction inverts — a rise
  is the `regression`.

The flag is repeatable and each key gates independently; a duplicate key (across flags
or directions) is an operational error (exit `2`). Annotation gating runs at two scopes —
individual **steps** (per `step_key`, described here) and the **run total** (run-level
scores, [below](#run-level-scores)); phase rows carry no annotation block per
[ADR-0022 Amendments](../adr/0022-regression-detection-over-repeated-runs.md#amendments).
Because a score is only sampled on the runs that
actually carry it, an annotation's `N` can be below the step's `Present` (a step
reached in every run but scored in only some); an annotation whose `N` falls below
`--min-support`, or one present on only one side, is reported `insufficient` rather
than guessed. A configured key that never fires on any step in either group prints a
warning to stderr (stdout and `--json` stay clean).

`--scores file.jsonl` supplies the values. Each line is one JSON object:

```json
{"step_key": "1f0c9a4b2d8e7f36", "key": "deepeval.tool_correctness", "value": 0.92, "run_id": "bench-checkout-work-task-candidate-r1"}
```

`step_key` names the step the score lands on (take it from the `KEY` column of a
`regress` table, or from [`subgraph --json`](#subgraph) node output), `key` uses the
same `owner.key` grammar as `--annotation`, `value` must be a number, and `run_id` is
optional: set it to score a single run — one line per scored run is the normal shape —
or omit it and the value lands on every run in **both** groups that carries the step
key, which flattens both medians to the same value. A line that omits `step_key` is a
**run-level** score ([below](#run-level-scores)). Extra provenance fields — `tool`,
`tool_version`, `prompt_hash`, or any other key an evaluator emits — are tolerated and
ignored, so a scorer can record its own metadata on the same line. Blank lines are
skipped; a malformed line is an operational error (exit `2`) naming the file and line.
Values apply in memory to both groups before aggregation — nothing is written back to
the evidence dirs or the store. Entries that match no node are counted into a single
stderr warning (`N score entries matched no node`); stdout and `--json` stay clean. The
file only supplies values: each key still needs its `--annotation owner.key[:direction]`
flag to declare a gate — the sole exception is `verifier.pass`, which gates by default
([below](#run-level-scores)) — or the scores are inert.

### Run-level scores

A score line that **omits `step_key`** attaches to the run as a whole rather than to a
node, and gates at the `total` scope alongside `cost_usd`, `nodes`, and the run-total
rates. In a `--scores` file a run-level line **requires `run_id`** (there is no node to
match it by; a run-level line without one is an operational error, exit `2`):

```json
{"step_key": "", "key": "verifier.pass", "value": 1, "run_id": "bench-checkout-work-task-candidate-r1"}
```

- **`verifier.pass` gates by default.** The reserved key `verifier.pass` (higher-better)
  is compared even with no `--annotation` flag, so a candidate whose pass rate drops
  below the baseline flags a `regression` out of the box. Any other run-level key still
  needs `--annotation owner.key[:direction]` to gate.
- **Binary annotations use the rate gate.** When every value for a key is `0` or `1`,
  the key is gated as a rate with one-sided Wilson bounds and the `--annotation-rate-delta`
  threshold (default `0.1`) — the same disjoint-bounds-and-delta rule as presence and
  error rates — and the human `DETAIL` column shows the raw counts as `ones a/n -> b/m`.
  A key with non-`{0,1}` values is treated as continuous and gated with the metric median
  band instead.

Run-level scores can also be supplied without `--scores`: when `regress` resolves runs
from `--runs-dir`, it auto-loads a `scores.jsonl` sitting in each run's evidence dir, and
a run-level line there may omit `run_id` (it defaults to that run's ID). A `--scores`
file is layered on top; when one of its entries sets a key an evidence file already
provided, the flag value wins and a stderr warning notes the count
(`N entries overrode evidence-provided values`).

Comparison runs at four scopes — run totals, paired per-task deltas, checkpoint
phases, and steps. The human
table prints `VERDICT SCOPE KEY NAME METRIC BASELINE CANDIDATE BAND DETAIL` with
presence-normalized values (presence rate, not absence); the `DETAIL` column carries the
per-finding note (raw counts such as `present a/n -> b/m` or `ones a/n -> b/m`, an
`insufficient` reason, or a coverage-downgrade note), and is `-` when empty. `--json`
emits the full report (presence rows carry absence rates plus the same `detail` field). Exit codes: `0`
pass, `1` regression (or `insufficient` with `--strict`), `2` operational error
(invalid selector, unknown baseline, missing store, empty group, a missing pinned
evidence dir, a [stamp refusal](#baseline-version-stamps) under `--strict`, or
`--min-support` below 1). Resolving a `name:` baseline on a store created by an older
binary also exits `2` with a hint to run a write-path command (`baseline set`) to
migrate the schema.

## Recording history

`--record` appends the full comparison — candidate selector, thresholds, annotation
specs, and the complete report — to the named baseline's append-only history,
replayable later with [`trends`](#trends). It requires `--baseline name:<baseline>` (a
`label:` group has no stable identity to append under, so `--record` with a `label:`
baseline is an operational error) and opens the store at `--db` read-write for the
append. The record is appended *after* the verdict is rendered
to stdout, and a failed append is itself an operational error (exit `2`) that takes
precedence over the verdict: a regression that could not be durably recorded exits `2`,
not `1`, so a broken store never masquerades as a clean regression signal.

The store must already exist: `--record` requires a `name:` baseline, and resolving one
against an absent store fails first (exit `2`), so the store is created by
[`baseline set`](#baseline-set), never by `--record`. Each record carries the version
stamps of the recording binary (catacomb version and step-key scheme) in its body
alongside the report.

Sequence numbers are assigned atomically in a single statement, so a record is never
silently overwritten. But concurrent `--record` writers against one store file — a
fan-out CI matrix whose shards all record into the same database — can still collide on
SQLite's write lock: a losing writer fails loudly with `SQLITE_BUSY` and exits `2`
without corrupting the history, rather than blocking or tearing a record. Serialize the
recorders (record from one shard, or gate on a lock) or give each shard its own store
file.

```sh
catacomb regress --baseline label:basket=checkout,variant=main \
  --candidate label:basket=checkout,variant=candidate
catacomb regress --baseline name:golden --candidate label:variant=candidate --json
catacomb regress --baseline name:golden --candidate label:variant=candidate --record --strict
catacomb regress --baseline name:golden --candidate label:variant=candidate \
  --scores scores.jsonl --annotation deepeval.tool_correctness
catacomb regress --baseline name:golden --candidate label:variant=candidate \
  --scores verifier.jsonl
```

The last form gates on `verifier.pass` with no `--annotation` flag, since that key gates
by default.

---

## trends

Show the recorded regression history for a baseline — the append-only trail written by
`regress --record`.

```sh
catacomb trends <baseline> [flags]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--db` | `~/.catacomb/catacomb.db` | SQLite database path |
| `--metric` | (empty) | Narrow to one total-scope metric: `duration_ms`, `cost_usd`, `tokens_in`, `tokens_out`, `nodes`, or `error_rate` |
| `--pareto` | false | Render the history as an [accuracy-vs-cost Pareto table](#accuracy-vs-cost-pareto); composes with `--json`, mutually exclusive with `--metric` |
| `--json` | false | Emit the history as JSON |

Records print oldest-first by sequence number. Without `--metric` or `--pareto`, the
wide table prints one row per recorded run — `SEQ CREATED CANDIDATE VERDICT REGRESSIONS
INSUFFICIENT DURATION_MS COST_USD ERROR_RATE` — a per-run scoreboard of the overall
verdict, the finding counts, and the candidate run-total values. `--metric <m>` swaps
to a narrowed table — `SEQ CREATED CANDIDATE VERDICT BASELINE-VALUE CANDIDATE-VALUE
BAND` — tracking one total-scope metric's baseline value, candidate value, and noise
band across the history so drift on a single axis is legible; a run whose report
carries no total-scope finding for that metric renders `-` in those columns. Value
cells are formatted to two decimals. `CREATED` is each record's `created_at` timestamp,
formatted RFC3339 in UTC.

Each record also stamps the baseline's `created_at` at record time. If a row was
recorded against a different definition of the baseline than the one that exists now —
the baseline was deleted and recreated (or re-`set`) under the same name — its `SEQ`
cell carries a trailing `*` and a footnote
`* recorded against a previous definition of this baseline` prints after the table, so
a spliced history is never read as a continuous one.

`--json` emits the raw stored history verbatim as `[{"seq":N,"record":<stored bytes>}]`:
each `record` is the exact JSON body that was written, byte-for-byte, not a
re-encoding. A body carries a schema version field `v` (currently `1`), the candidate
selector, thresholds, annotation specs, the report, its own `created_at` (RFC3339 UTC),
a `baseline_created_at` stamp mirroring the baseline's `created_at` at record time, and
the recording binary's version `stamps` (catacomb version and step-key scheme; records
written before stamps existed lack the field) — ready for dashboards or diffing
scripts. A record whose `v` is not understood by this binary is an exit-`2` error
naming the sequence and version (upgrade catacomb).

### Accuracy-vs-cost Pareto

`--pareto` re-reads the same recorded history as a two-axis trade-off table. Each
record contributes one point: accuracy is its total-scope `ann:verifier.pass` finding's
candidate value (the pass rate the gate already uses) and cost is its total-scope
`cost_usd` finding's candidate value (the median per-run cost); one `baseline` row is
added from the newest record's baseline values of the same two findings. Nothing is computed or persisted
anew — every value comes from reports already stored by `--record`.

```text
SEQ  CREATED               CANDIDATE                 ACCURACY  COST_USD  DOMINATED
-    -                     baseline                  1.00      0.0102    no
2    2026-07-12T18:04:11Z  label:variant=cand        1.00      0.0102    no
1*   2026-07-12T17:58:03Z  label:variant=degraded    0.00      0.0102    yes
3    2026-07-12T18:09:44Z  label:variant=old         -         0.0110    -
```

A row is `DOMINATED yes` when some other row is at least as accurate and at most as
costly, with strict advantage on at least one axis — rows equal on both axes (the
first two above) do not dominate each other and both stay `no`. Rows sort by cost
ascending, then accuracy descending, then sequence ascending, so the Pareto frontier
reads top-down. A row that lacks an axis — either accuracy (a record whose report
carries no total-scope `ann:verifier.pass` finding: pre-verifier history) or cost
(cost-less evidence) — carries no domination verdict: it renders `-` in the missing
cells and in `DOMINATED`, sinks to the bottom of the table in sequence order, and one
epilogue note counts how many rows were not compared. The splice marker (`*` and its
footnote) applies unchanged.

`--pareto --json` emits `{"baseline": "<name>", "points": [...]}` instead: every point
carries `source` (`"baseline"` or `"record"`), and record points add `seq`,
`candidate`, `created_at`, and `spliced` (the baseline point carries none of those).
`accuracy` and `cost_usd` are omitted when the finding is absent from the record's
report, and `dominated` is omitted — not `false` — for a point that lacks an axis.

`--pareto` is mutually exclusive with `--metric` (operational error, exit `2`) and
composes with `--json`.

Exit codes: `0` success, `2` operational error. An unknown `--metric` (outside the set
above), `--pareto` combined with `--metric`, an unknown baseline (`baseline not
found`), a known baseline with no recorded runs (`has no recorded regress runs`), and a
record written by a newer schema version are distinct exit-`2` errors, as are a missing
store and one created by an older binary whose schema needs migrating (run a write-path
command such as `baseline set`). `trends` opens the store read-only and never migrates
it.

```sh
catacomb trends golden
catacomb trends golden --metric error_rate
catacomb trends golden --pareto
catacomb trends golden --json
```

---

## diff

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
| `--a-from` | (empty) | Range start checkpoint for side A |
| `--a-to` | (empty) | Range end checkpoint for side A |
| `--b-from` | (empty) | Range start checkpoint for side B |
| `--b-to` | (empty) | Range end checkpoint for side B |

Reports added, removed, changed, and unchanged steps with per-field deltas (args,
status, cost, duration, tokens). A within-run phase comparison uses the same transcript
on both sides with different `--a-phase`/`--b-phase` selectors. `from`/`to` must be set
together per side and are mutually exclusive with that side's phase selector. See
[workflows.md](workflows.md) for the checkpoints + diff recipe.

```sh
catacomb diff run-a.jsonl run-b.jsonl --phase eval-loop --json
```

---

## subgraph

Extract the execution subgraph of a checkpoint phase from a session transcript.

```sh
catacomb subgraph <session.jsonl> [flags]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--phase` | (empty) | Phase selector (`name` or `name,occurrence`); mutually exclusive with `--from`/`--to` |
| `--from` | (empty) | Range start checkpoint |
| `--to` | (empty) | Range end checkpoint |
| `--json` | false | Emit `{nodes, edges}` JSON instead of summary lines |

Prints `nodes: N  edges: M` followed by node lines, or structured JSON with `--json`.

```sh
catacomb subgraph session.jsonl --phase eval-loop --json
```

---

## export

Export a transcript or evidence directory as a JSONL graph snapshot.

```sh
catacomb export <transcript.jsonl | evidence-dir> [flags]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--to` | `jsonl` | Export format: `jsonl` |
| `--out` | (empty) | Write to file instead of stdout |

The input is either a single transcript file or a [`bench`](#bench) evidence directory.
A directory loads like a `regress` run: `session.jsonl` plus `subagents/agent-*.jsonl`,
with the `task:<id>` boundary markers re-applied from `meta.json`. The output is the
materialized graph as JSONL — `{"kind":"node"…}`, `{"kind":"edge"…}`, and
`{"kind":"run"…}` records — the input format of the
[DeepEval bridge](https://github.com/realkarych/catacomb/tree/master/integrations/deepeval).

```sh
catacomb export ~/.catacomb/runs/bench-checkout-add-item-candidate-r1 --out run.jsonl
catacomb export ~/.claude/projects/my-project/<session>.jsonl --to jsonl
```

---

## replay

Build an in-memory graph from a single recorded Claude Code transcript and print a
node/edge summary.

```sh
catacomb replay <transcript.jsonl> [flags]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--export-jsonl` | (empty) | Also write a JSONL graph snapshot to this path |

Nothing is persisted: the graph is built, summarized, and discarded (or snapshotted
with `--export-jsonl`). Useful for a quick look at what catacomb sees in a session.

```sh
catacomb replay ~/.claude/projects/<project>/<session>.jsonl
```

---

## mcp

Run the catacomb MCP server over stdio (JSON-RPC 2.0, newline-delimited). It exposes a
single `mark` tool so an in-run agent can record phase checkpoints without any
hand-rolled stub.

```sh
catacomb mcp
```

Takes no flags or arguments; it reads requests from stdin and writes responses to
stdout, and exits when stdin closes (or on `SIGINT`/`SIGTERM`). The `mark` tool takes:

| Field | Required | Meaning |
| --- | --- | --- |
| `name` | **required** | Phase name |
| `boundary` | **required** | `start` or `end` |
| `occurrence` | optional int | Occurrence index for repeated same-name phases |
| `state_ref` | optional | Opaque state reference stored on the marker node |

Wire it into Claude Code with `--mcp-config` so the agent can call the
`mcp__catacomb__mark` checkpoint tool during a run — the server named `catacomb`
exposing `mark` surfaces as `mcp__catacomb__mark`:

```json
{"mcpServers":{"catacomb":{"command":"catacomb","args":["mcp"]}}}
```

Pass `--strict-mcp-config` alongside it (as the bench cells should) so only the
catacomb server is loaded and no ambient MCP config leaks in. The tool is a pure
acknowledgement — the marker rides the transcript as the tool-call record, and the
catacomb reducer synthesizes the phase boundary from the tool-call input, so the server
needs no configuration and fails open. See
[workflows.md](workflows.md#placing-markers) for the checkpoints workflow.

---

## version

Print the version string.

```sh
catacomb version
```
