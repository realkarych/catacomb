# catacomb-judge

A stdlib-only Python package of judge meta-evaluation utilities: an agreement
calculator that measures LLM judges against a hand-labeled gold set (Spearman ρ,
Cohen's κ, TPR/TNR), and a panel aggregator that folds several judges' scores
into one stream. Both read the scores-JSONL dialect that catacomb evidence and
`regress --scores` already speak; neither calls an LLM, touches the network, or
runs a judge — scoring happens outside, and these tools only measure and combine
what came back.

This directory is outside the Go 100%-coverage gate and the no-comments rule;
both apply only to Go packages. The package depends on nothing but the standard
library.

## Inputs

**Judge scores** are ordinary scores-JSONL lines, given as file paths and/or
`--runs-dir DIR` (scans `DIR/*/scores.jsonl`, the bench evidence layout — the
two sources combine):

```json
{"key": "judge.groundedness", "value": 1, "run_id": "bench-checkout-work-task-candidate-r1", "tool": "claude-haiku-judge", "tool_version": "1", "prompt_hash": "9b2f"}
```

`key` (strict `owner.key`), a numeric `value`, and `run_id` are required —
lines without a `run_id` cannot join to anything and are skipped with a stderr
note. `step_key` is optional and narrows the join to step-level lines. The
provenance field **`tool` identifies the judge** (a missing `tool` reads as
judge `"unknown"`); unknown extra fields are tolerated, mirroring the Go
parser.

**Labels** are the hand-labeled gold set, one JSON object per line:

```json
{"run_id": "bench-checkout-work-task-candidate-r1", "key": "judge.groundedness", "label": 1}
```

`run_id` and `key` (plus optional `step_key`) join a label to score lines on
the same coordinates; `label` is a number. A duplicate (run_id, key[,
step_key]) label is a format error (exit 2) — a gold set must be unambiguous.
Labels with no matching score, and scores with no matching label, are counted
and disclosed, never silently dropped.

## `catacomb-judge agreement`

```text
catacomb-judge agreement --labels labels.jsonl (--runs-dir DIR | scores.jsonl ...)
                         [--key judge.x] [--threshold 0.5] [--min-kappa K] [--json]
```

Joins scores to labels and reports, per key and per judge (`tool`), plus an
`overall` row per key pooling all judges:

```text
KEY                 JUDGE               N  SPEARMAN  KAPPA  TPR    TNR
judge.groundedness  claude-haiku-judge  4  0.577     0.500  1.000  0.500
judge.groundedness  gemini-judge        4  1.000     1.000  1.000  1.000
judge.groundedness  overall             8  0.775     0.750  1.000  0.750
```

- **N** — matched (score, label) pairs; unmatched counts on either side print
  as disclosure lines under the table.
- **SPEARMAN** — rank correlation on the raw values (average ranks for ties).
- **KAPPA, TPR, TNR** — computed after both score and label binarize at the
  same `--threshold` (default 0.5; a value exactly at the threshold maps
  to 1). The canon's dominant judge shape is binary 0/1, which passes through
  unchanged at the default. TPR/TNR are the imbalance-robust pair the canon
  prescribes instead of raw accuracy.

A metric that would be meaningless is omitted (`-` in the table, absent key in
`--json`) rather than reported as a number: Spearman when there are fewer than
two pairs or either side has zero variance; κ when both binarized sides are
constant at the same value (chance agreement is certain, κ is undefined); TPR
when the gold set
has no positives; TNR when it has no negatives.

`--min-kappa K` is the opt-in calibration gate: exit 1 when any judge's κ is
below K **or omitted**, with a stderr line naming the judge
(`judge.groundedness/claude-haiku-judge: kappa 0.500 < 0.8`). The comparison is
strictly `<`, so a judge at exactly K passes; the `overall` row is informative
and never gates. Without the flag the command never exits 1.

## `catacomb-judge panel`

```text
catacomb-judge panel (--runs-dir DIR | scores.jsonl ...) [--key judge.x]
                     [--vote] [--min-judges 2] [--out FILE]
```

Groups score lines by (run_id, key[, step_key]) — each distinct `tool` is one
judge — and emits one aggregate line per group **in the same scores-JSONL
dialect**, so the output feeds straight back into `regress --scores`:

```json
{"key":"judge.groundedness","value":0.3333333333333333,"run_id":"bench-checkout-work-task-candidate-r2","tool":"panel","tool_version":"0.1.0","panel_size":3}
```

- Default aggregation is the **mean** of the judges' values (binary in →
  agreement fraction out; a whole-number mean is emitted as an integer).
  `--vote` takes a strict majority on values binarized at 0.5 and requires an
  odd number of judges in every group (an even panel is a usage error, exit 2,
  naming the group) so ties cannot occur.
- Groups with fewer than `--min-judges` distinct judges (default 2) are
  skipped with a stderr note — a panel of one is not a panel. Lines without
  `tool` are skipped with a note: panel identity requires provenance. One tool
  emitting two lines for the same group is ambiguous input (exit 2).
- Output goes to stdout, or `--out FILE`; order is deterministic (sorted by
  run_id, key, step_key), byte-identical across input file orders.

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | success (including an empty report) |
| 1 | `--min-kappa` calibration gate failed (only when the flag is passed) |
| 2 | usage, IO, or format errors (bad flags, unreadable files, malformed or ambiguous lines) |

## Example

Judges scored a bench run's cells into per-run files (or straight into
evidence `scores.jsonl`); a human labeled a sample of the same runs:

```bash
pip install -e integrations/judge

catacomb-judge agreement --labels gold.jsonl --runs-dir runs --min-kappa 0.8
catacomb-judge panel --runs-dir runs --key judge.groundedness --vote --out panel.jsonl

catacomb regress --runs-dir runs \
  --baseline label:variant=baseline --candidate label:variant=candidate \
  --scores panel.jsonl --annotation judge.groundedness:higher-better
```

See [Calibrating a judge](../../docs/guide/workflows.md#calibrating-a-judge)
for the full workflow, including when to trust which metric.

## Testing

```bash
cd integrations/judge
pip install -e '.[test]'
pytest -q
```
