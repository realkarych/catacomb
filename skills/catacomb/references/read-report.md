# Reading a regress report

A `regress` report is one screen of text, and it reads top-down: the header states the
overall verdict, the finding rows say which metric moved and by how much, and the
epilogue lines add provenance. The exit code is the same verdict in one number — that is
what CI acts on. New to the vocabulary (noise band, `steps_trusted`, scope)? Read
[concepts.md](concepts.md) first.

Anchor on a real report and walk it line by line:

```text
baseline runs 5  candidate runs 5
coverage steps 1.00  phases 1.00  steps_trusted true  overall regression
sensitivity: gate cannot fire at this support (paired gate needs k>=5 tasks)
VERDICT       SCOPE   KEY  NAME         METRIC       BASELINE  CANDIDATE  BAND
regression    total   -    -            cost_usd     0.00      0.01       [0.00, 0.00]
ok            total   -    -            error_rate   0.00      0.00       [0.00, 0.35]
regression    total   -    -            tokens_out   147.00    465.00     [91.50, 202.50]
insufficient  paired  -    -            cost_usd     0.00      0.00       -
regression    phase   …    task:answer  tokens_out   147.00    465.00     [91.50, 202.50]
audit: baseline run … cost_usd 0.0109 vs group median 0.0033 (band 0.0016)
```

Each element below maps to a next action.

## The header line

`baseline runs 5  candidate runs 5` are the two group sizes — the comparison is always
group-vs-group, never a single side-by-side diff. The next line is the summary:
`coverage steps 1.00  phases 1.00` is how much of the step and phase axis actually
aligned across the two groups, `steps_trusted true` says that alignment held (prompt
churn did not degrade it), and `overall regression` is the verdict that maps to the exit
code. When `steps_trusted` reads `false` or coverage drops, the step-level numbers rest
on a shaky mapping — see [coverage and steps_trusted](concepts.md#coverage-and-steps_trusted).

## `total` rows — the headline

The `total` rows are the run-level verdict, and they are the rows that gate. Each one
compares the candidate median against the baseline's **noise band** (`BAND`): inside the
band is `ok`, outside it is `regression`. In this report:

- `cost_usd` — candidate `0.01` sits outside `[0.00, 0.00]` → `regression`.
- `error_rate` — candidate `0.00` sits inside `[0.00, 0.35]` → `ok`.
- `tokens_out` — candidate `465.00` sits far outside `[91.50, 202.50]` → `regression`.

A band derived from the baseline group, not a single baseline run, is what absorbs
ordinary run-to-run noise; a candidate has to clear that noise to count as a real move.
See [verdicts and noise bands](concepts.md#verdicts-and-noise-bands).

## `paired` rows — the per-task sign test

The `paired` scope is the exact per-task sign test: it pairs each task's baseline median
against its candidate median and asks whether the deltas lean one way across tasks. It
catches systematic drift *below* the total band — a small cost creep repeated across
every task — that the median rows miss. It needs **≥5 matched tasks** to run; below that
it reports `insufficient` with an empty `-` band, exactly as here. **Action:** add tasks
to the basket to arm it ([setup.md](setup.md)). More reps cannot fix a `paired
insufficient` — the support is a task count, not a rep count.

## `phase` rows — pin it to a checkpoint

A `phase` row localizes a regression to one declared checkpoint window. Here the
`tokens_out` blow-up is pinned to `task:answer` — the same numbers as the total row, but
now attributed to a named phase instead of the whole run. That attribution only exists
when the agent marks phases and the basket declares `checkpoints:`. If the phase axis is
missing or its coverage is low, you lose this localization (and `steps_trusted` may go
`false`). **Action:** add checkpoints to re-anchor the comparison to named phases
([accuracy.md](accuracy.md)).

## The `sensitivity:` line

The `sensitivity:` note explains why an axis could not fire at the current support —
here, `paired gate needs k>=5 tasks`. It names the smallest support at which a unanimous
shift on that axis *would* gate, so you know exactly how much to add. It is a note about
statistical reach, not a verdict, and it never changes the exit code on its own.

## The `audit:` line

The `audit:` line flags a single individual run whose value sits outside its own group's
audit band — here one baseline run spent `cost_usd 0.0109` against a group median of
`0.0033`. It is provenance, not a separate verdict: the block is computed after the
findings, feeds no verdict, and never affects the exit code. Treat it as an invitation to
read that run's evidence. Note that `cost_usd` and `duration_ms` flags are noisy under
prompt caching — `tokens_out` and `turns` are the trustworthy anomaly axes.

## The exit code

The headline verdict is also the process exit code, and CI keys the merge decision off it
alone:

- `0` — `ok`, mergeable.
- `1` — `regression`, the merge is blocked (this report exits `1`).
- `2` — operational error: the gate could not run. Not a regression — fix the setup,
  never treat it as a pass.

See [exit codes](concepts.md#exit-codes).

## Decision guide

Match the report to the fix:

| The report shows… | Do this |
|---|---|
| `insufficient` on a `paired` row | Add tasks to the basket so the sign test can fire (≥5 matched tasks) — [setup.md](setup.md) |
| `steps_trusted false` or low step coverage | Add checkpoints to re-anchor the comparison to named phases — [accuracy.md](accuracy.md) |
| A `regression` whose band looks too wide or noisy | Raise `reps` to tighten the noise band, so a real move clears it — [setup.md](setup.md) |
| A `regression` you have reviewed and accept | Re-pin the baseline to the new numbers so it stops firing — [accuracy.md](accuracy.md) |
