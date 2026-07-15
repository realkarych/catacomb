# ADR-0023: Regression gate sensitivity at small run counts

- **Status:** Accepted
- **Date:** 2026-07-02
- **Deciders:** @realkarych
- **Related:** ADR-0022 §4; review `docs/internal/reviews/2026-07-02-post-p0-cto-design-review.md` §4.1
- **Amends:** ADR-0022 — the rate-comparison statistics and the gate's escalation surface.

## Context

ADR-0022 §4 flags a rate regression (presence, error rate) only when the 95% Wilson intervals of baseline and candidate are disjoint **and** the delta exceeds the configured threshold. `regression` is the only verdict that drives exit code 1; `notable` (delta exceeded, intervals overlap) never gates, and `--strict` escalates only `insufficient`.

The post-P0 design review demonstrated a closed-form consequence nobody had computed: at the default `MinSupport` floor of k=3, the rate gate **cannot fire at all**. A full presence flip — a checkpoint present in 3/3 baseline runs and 0/3 candidate runs — produces intervals [0.44, 1.0] vs [0, 0.56] at z=1.96, which overlap, so the verdict is `notable` and CI exits 0. A full flip first hard-flags at k=4; a 10pp→40pp error-rate shift needs ≈40 repetitions per side. Metric comparisons (median vs IQR band) are unaffected and gate fine at k=3.

At realistic repetition counts (agentic runs are expensive; k=3–10), the checkpoint/error gate is therefore nearly inert — the mirror-image failure of the sampling noise ADR-0022 set out to suppress. ADR-0022 reserved an amendment "if the simple bands prove miscalibrated"; this is that case, established analytically rather than empirically.

## Decision

Keep the dependency-free closed-form statistics; recalibrate the defaults, add an explicit escalation knob, and make gate impotence visible.

1. **One-sided rate bounds.** The regression and improvement checks are directional, so each uses one-sided Wilson bounds at 95% one-sided confidence (z=1.645) instead of two-sided 95% (z=1.96). The comparison rule is unchanged (disjoint bounds AND delta); only z changes. Effect: a full flip hard-flags at k=3, and the ≈40-rep example above drops to ≈25. z is configurable via `regress --z <float>` (validated > 0) for operators who want a stricter or looser gate.
2. **`--fail-on-notable`.** Off by default. When set, `notable` findings count toward the gate exactly like regressions (exit 1). This is the recall-over-precision knob for small k; it is not the default because at k=3 the delta thresholds alone would flag 1/3→2/3 sampling noise.
3. **Sensitivity disclosure.** `regress` evaluates its own verdict function on the maximally separated inputs for the actual group sizes (baseline 0/bN vs candidate cN/cN, same code path, same thresholds). If even that cannot produce `regression`, the report — human and `--json` — carries an explicit sensitivity note naming the smallest k at which a full flip would gate. A gate that cannot fire is never silent about it.
4. **Bench epilogue nudge.** When a basket declares `reps < 5`, the `catacomb bench` epilogue appends one line recommending `reps: 5` or more for rate-gate sensitivity.
5. **Documentation.** `docs/guide/workflows.md` gains a sensitivity table (k × smallest hard-flaggable effect at default thresholds); `docs/guide/cli.md` documents `--z` and `--fail-on-notable`.

Defaults change without a compatibility shim: the tool is pre-1.0 with zero external adopters, and the current default is demonstrably unable to perform its stated job at its own documented support floor.

## Alternatives considered

- **Keep z=1.96 and only document the insensitivity** — an honest decorative gate is still decorative; the default configuration must be able to flag a total checkpoint loss at the documented minimum k. Rejected.
- **Gate on `notable` by default** — collapses ADR-0022's two-tier design and lets pure sampling noise (1/3 vs 2/3) fail CI. Rejected as a default; shipped as the opt-in flag.
- **Proper hypothesis tests (Fisher exact, permutation, Mann-Whitney, FDR)** — remains deferred per ADR-0022's original adjudication; k=3–10 cannot feed them, and the closed-form keeps the tool dependency-free.
- **Beta-posterior (Bayesian) intervals** — better behaved at tiny k but imports a prior that must be explained and defended; not worth it while the one-sided Wilson bound fixes the observed failure.

## Consequences

- **+** The default gate can hard-flag a full checkpoint loss at k=3, and the k required for partial effects drops by roughly a third.
- **+** Gate impotence is computed and disclosed per invocation instead of discovered by a human after a false green.
- **−** One-sided 95% is a weaker per-comparison guarantee than two-sided 95%; marginally more false regressions are possible. Mitigated by the unchanged delta AND-condition and by `--z` for operators who want the old strictness.
- **−** Two new flags (`--z`, `--fail-on-notable`) are permanent CLI surface.
- **−** The sensitivity note is one more report block to maintain; it reuses the verdict function, so it cannot drift from the real gate.
