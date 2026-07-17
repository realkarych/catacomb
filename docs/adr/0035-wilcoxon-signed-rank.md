# ADR-0035: Opt-in exact Wilcoxon signed-rank on the paired axis

- **Status:** Accepted
- **Date:** 2026-07-17
- **Deciders:** @realkarych
- **Amends:** [ADR-0027](0027-verification-layer-and-reliability-metrics.md) (SP2 listed "paired sign/Wilcoxon signed-rank tests" as contents; only the exact sign test shipped, with Wilcoxon deferred on tie-handling complexity — this ADR discharges that deferral as an opt-in)
- **Related:** [ADR-0022](0022-regression-detection-over-repeated-runs.md) (the paired axis and its multiplicity accounting), [ADR-0023](0023-regression-gate-sensitivity-at-small-k.md) (the paired sensitivity disclosure, unchanged by this ADR)

## Context

The shipped paired axis is an exact one-sided sign test over per-task median deltas
(`regress/paired.go`). It has a provable dead zone in exactly the 5–10-task regime
SP2 advertised: with n nonzero deltas it fires only on near-unanimity (first
non-unanimous fire is 7/8, p=0.0352). A real cost/latency/token regression where one
task moved the other way, or moved by a small magnitude, slips. The sign test also
discards magnitude entirely — eight tasks each +1% fires identically to eight tasks
each +40%.

An exact Wilcoxon signed-rank test uses the rank magnitudes of the deltas, so at n=6
it fires with one small-magnitude discordant task (asymptotic relative efficiency
~1.5 vs the sign test) — materially more power in the product's stated target
regime. SP2 deferred it only because tie handling looked heavy; at the ≤20-task
scale here the exact null is a cheap deterministic computation, so the deferral no
longer holds.

## Decision

Add an **opt-in** `regress --paired-test sign|wilcoxon` (default `sign` — no
behavior change for anyone). When `wilcoxon` is selected it **replaces** the sign
test **per metric** — it never runs alongside it — so the paired multiple-comparison
family stays exactly four metrics (`duration_ms`, `cost_usd`, `tokens_in`,
`tokens_out`), the same family the union-bound multiplicity note already documents.

Exact test, RNG-free, deterministic:

1. **Deltas.** For each metric-supported matched task, `d = cand.Median −
   base.Median` (identical inputs to the sign test). **Zero deltas are discarded**
   (the reduced-sample convention), matching the shipped sign test's zero handling —
   Pratt's zero-inclusion changes the null for no demonstrated benefit here and is a
   non-goal.
2. **Ranks.** Rank the surviving `|d|` ascending with **mid-ranks** for ties (average
   rank within a tie group). To keep the null exact and integer-arithmetic,
   **double every rank** (mid-ranks are half-integers at worst, so `2·rank` is an
   integer).
3. **Statistic.** `W+ = Σ (2·rank_i)` over tasks with `d_i > 0`.
4. **Exact null.** Under H0 the deltas are symmetric about 0, so each observed
   `|d|` carries its sign independently with probability ½. The null distribution of
   `W+` is therefore the subset-sum distribution of the doubled ranks: the
   coefficients of `∏_i (1 + x^{2·rank_i}) / 2^n`. Compute it by a dynamic program
   over achievable integer sums (`O(n · Σ2·rank) = O(n³)`, trivial at n≤20). The
   one-sided regression p-value is `P(W+ ≥ observed)`; the improvement p-value is the
   mirror `P(W+ ≤ observed)` (equivalently the regression tail of `−d`).
5. **Verdict.** `p_reg ≤ PairedAlpha → regression`; else `p_imp ≤ PairedAlpha →
   improvement`; else `ok`. `matched < PairedMinTasks → insufficient` exactly as
   today. Detail string reports the statistic and p (e.g. `W+ …/… tasks, p=…`).

**Sensitivity disclosure is unchanged.** The paired reachability floor is the
smallest n at which the *most extreme* configuration gates. For both tests that is
the all-same-sign case, whose exact p is `2^{−n}` (only the all-positive subset has
`W+ ≥ total`), so `minUnanimousTasks`/`smallestFiringTasks` and the
`PairedSensitivity` row hold for Wilcoxon verbatim. The two tests differ in **power
on non-unanimous configurations**, not in the reachability floor.

## Alternatives considered

- **Add Wilcoxon as a fifth paired test alongside the sign test.** Rejected: it
  doubles the paired family (4→8) and inflates the family-wise false-positive rate
  the multiplicity note bounds. Mutually-exclusive per-metric selection keeps the
  family flat.
- **Default to Wilcoxon.** Rejected for v1: the sign test is the documented,
  calibrated default; changing verdicts silently for every existing user is a
  breaking behavior change. Opt-in first; a later ADR may flip the default with the
  golden-expectation updates that requires.
- **Pratt zero-handling (keep zeros).** Rejected: changes the conditional null,
  diverges from the shipped sign test's discard convention, for no demonstrated
  benefit at this scale.
- **Asymptotic-normal Wilcoxon with a continuity correction.** Rejected: the exact
  null is cheap here, and ADR-0023's whole thesis is that asymptotic approximations
  undercover at small n — an approximate paired test would contradict the gate's own
  small-n discipline.

## Consequences

- Teams running 6–10-task baskets can gate a real regression that one noisy task
  would otherwise mask — the exact SP2 gap the paired test exists to close.
- `--paired-test` joins the CLI compatibility surface (VERSIONING.md); the default
  and every existing pipeline are unchanged.
- The `regress` finding `Detail` for the paired axis gains a Wilcoxon form; consumers
  parsing that free-text string (they should not) see a different shape only under
  `--paired-test wilcoxon`.
- The exact-null DP is self-contained in `regress/paired.go`, RNG-free and
  deterministic, so it fits the closed-form/deterministic constitution; its
  explainability is a rank-sum p rather than the sign test's `+k/n` count, a mild
  trade the opt-in makes explicit.
