# ADR-0034: Gate self-check — offline A/A drift and influence audit

- **Status:** Accepted
- **Date:** 2026-07-17
- **Deciders:** @realkarych
- **Amends:** [ADR-0022](0022-regression-detection-over-repeated-runs.md) (the "Full statistical framework (permutation tests, Mann-Whitney, FDR control) … deferred as a future amendment once real usage shows the simple bands mis-flagging" line — this ADR is that amendment, scoped to an offline A/A self-audit, not a gate-path change)
- **Related:** [ADR-0023](0023-regression-gate-sensitivity-at-small-k.md) (precedent: `regress` evaluates its own verdict function on the real code path — §3 sensitivity disclosure; this ADR reuses the same pure `Compare` over run subsets); [ADR-0026](0026-form-factor-pivot-offline-eval-gate.md) (offline/deterministic constitution)

## Context

The metric gate's continuous bands (median ± IQR·factor) carry no probabilistic
guarantee — their false-positive rate depends on real dispersion. The project's own
live calibration made this concrete: `e2e/run.sh` asserts its A-vs-A controls only
at a **widened** `--metric-rel-delta` because identical sequential batches drift ~2x
on `duration_ms` (inter-batch API latency), which a default band would false-flag.
That is exactly the "real usage shows the simple bands mis-flagging" trigger
ADR-0022 named for revisiting the deferred permutation machinery.

The naive response — "measure and print the family-wise false-positive rate" — is
indefensible and was rejected in adversarial review: at the default `MinSupport = 3`
a disjoint A/A split needs ≥3 runs per side, so a k=5 basket has **zero** gateable
splits, and a k=6 basket yields only 20 overlapping (3,3) splits sharing all six
runs — a "rate" from that has no error bars and would not survive a statistician in
the buyer's org. What IS defensible, and genuinely useful before a red verdict is
trusted, is a per-basket **self-audit** on the user's own recorded runs: does an
A/A of one variant against itself gate at the current thresholds, is a verdict
riding on a single outlier run, and is the continuous axis picking up environmental
drift rather than a real effect.

## Decision

Add **`catacomb calibrate`**, an offline, deterministic self-audit verb that reuses
the shipped verdict path (`aggregate.Aggregate` + `regress.Compare`) over subsets of
**one variant's** recorded runs. It never spends money, never touches the gate's
own behavior, and reports three things:

1. **Time-ordered A/A split.** Sort the selected runs by each run's evidence
   timestamps (started/ended, falling back to run-id order when timestamps are
   absent), split into a first half and a second
   half of ≥`MinSupport` each (so the verb requires **k ≥ 2·MinSupport = 6** runs;
   below that it reports `insufficient` and names the k it needs, mirroring
   ADR-0023's honesty). Run `Compare` with baseline = first half, candidate =
   second half. A `regression`/`notable` verdict here is not a real regression — the
   two halves are the same variant — so it is reported as a **drift finding**: the
   named metric picks up environmental drift within identical runs, and continuous
   verdicts on this basket ride that drift. A clean result is reported as "A/A clean
   at current thresholds."
2. **Leave-one-out influence.** For the same split, drop each single run in turn and
   re-evaluate; report every run whose removal flips the overall verdict. A verdict
   that hinges on one run is fragile — the operator sees exactly which run, and on
   which metric.
3. **Support and threshold echo.** The report states the run count, the effective
   thresholds used, and — when k < 6 — the explicit `insufficient` note, so the
   self-check is never silently empty.

Two framing rules, both from the adversarial review, are load-bearing **non-goals**:

- **No headline "empirical family-wise false-positive rate" number.** The overlapping
  half-splits of a 6–10-run basket cannot support one; publishing it would be false
  precision. The verb reports *whether* A/A gates and *what drifts*, not a rate.
- **No auto-suggested `--metric-rel-delta`.** Fitting a threshold to make A/A clean is
  data-dredging, and it is structurally unsound while `MetricRelDelta` is a single
  global knob: widening it for noisy `duration_ms` simultaneously blinds
  `tokens_out`, the one continuous axis live calibration validated as reliable. The
  verb surfaces the drift; choosing thresholds stays the operator's judgment.

Alongside the verb, document a **true k-vs-k A/A workflow**: run one variant at
`2k` reps and `regress` its first k against its last k. That — not the overlapping
split — is the statistically honest way to observe false-positive behavior at the
real operating support, and it costs real bench budget, which is why it is a
documented recipe rather than something `calibrate` fabricates from too few runs.

## Alternatives considered

- **Print a family-wise FP rate from resampled splits.** Rejected: undefined at k=5,
  n≈few overlapping splits at k=6–10, no error bars — false precision that invites
  the exact "your gate's own FP rate is unmeasured" objection it pretends to answer.
- **Auto-tune `--metric-rel-delta` to keep A/A clean.** Rejected: data-dredging, and
  a global knob can't be tuned for one metric without degrading another.
- **Fold the self-check into `regress` (a `--self-check` flag).** Considered; kept as
  a separate verb because `regress` compares two groups and `calibrate` audits one,
  and overloading the gate command with a same-variant mode blurs the contract. A
  thin flag alias may be added later if demand appears.
- **Random-permutation A/A (Monte Carlo).** Rejected: RNG breaks the deterministic
  constitution and adds nothing over exhaustive/ordered enumeration at these run
  counts.

## Consequences

- The known weakness (continuous bands are a fixed tolerance, not a hypothesis test,
  with duration drift documented in `e2e/run.sh`) becomes an operator-facing tool:
  "before you trust a red verdict, run `catacomb calibrate` against your own
  baseline and see whether it would false-flag, and why." No competing gate ships
  this — it is the statistical-honesty pitch made actionable.
- `calibrate` is a new CLI surface (VERSIONING.md #1). It is pure/offline/stdlib and
  changes no gate behavior, so it carries no evidence- or store-format impact.
- ADR-0022's permutation-machinery deferral is now partially discharged: the A/A
  split is a permutation test in all but name, admitted here for *self-audit only*,
  explicitly not for the gate verdict (which keeps its bands per ADR-0022/0023).
- Reliability of the continuous axis becomes a per-basket, per-operator observable
  rather than a project-wide n=3 anecdote.
