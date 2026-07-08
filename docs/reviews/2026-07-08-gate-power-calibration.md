# Gate-power calibration — deterministic characterization of `regress.Compare` (PV-6a)

**Date:** 2026-07-08
**Scope:** the analytical (free, no live Claude) half of PV-6. Characterizes the statistical power of the regression gate — "what regression magnitude does it reliably catch at k = {3, 5, 10, 20, 30}?" — by driving the real `regress.Compare` across an effect-size × k sweep on synthetic `aggregate.Report` inputs. Every number below is produced and pinned by `regress/power_test.go` (`TestGatePowerPresence`, `TestGatePowerContinuousKInvariant`, `TestGatePowerErrorRate`); the test doubles as a calibration guard — a future threshold change that moves any boundary fails it. Live validation is PV-6b (§6).
**Verdict up front:** the gate is a **two-regime detector**. On the rate axes (presence, error-rate) power **sharpens with k**: Wilson score intervals narrow as runs accumulate, so the minimum reliably-detected shift falls from a full 100% flip at k=3 toward the delta floor (20% presence / 10% error) by k≈30. On the continuous axis (duration/cost/token medians) the gate is a **fixed tolerance band** `max(0.25·|median|, 1.5·IQR)` with **no term in k** beyond the `MinSupport=3` cutoff — more runs stabilize the median point estimate but never tighten the band, so the minimum detectable median shift is **k-invariant** (+30% across the realistic IQR/median ≤ 0.15 regime tested; the IQR term only overtakes the relative term above IQR/median ≈ 0.167). Practical floors: at **k=3** only total flips gate on rate axes and nothing under +25% gates on continuous; **k=5** needs ~80% rate flips; **k=10** catches ~50% rate drops; **k=20–30** approaches the 20–25% delta floor; a continuous regression must exceed **25% of the baseline median at any k**. No threshold-default changes are proposed here — that decision is owed to PV-6b, on real dispersion.

---

## 1. Method

Driven end-to-end through the real gate entry point — `regress.Compare(regress.Input{Baseline, Candidate}, regress.DefaultThresholds())` — never the low-level comparators. `DefaultThresholds()`: `PresenceDelta 0.2`, `ErrorRateDelta 0.1`, `MetricRelDelta 0.25`, `IQRFactor 1.5`, `MinSupport 3`, `Z 1.645`.

Load-bearing gate facts the sweep exploits:

- Presence is fed to `compareRate` as **absence** (`runs − present`), so a presence drop becomes a rising absence rate; it gates `regression` when the Wilson(absence) intervals of baseline and candidate are disjoint **and** the delta exceeds `PresenceDelta`. A full flip (present k/k → 0/k) is absence 0/k → k/k.
- Phase and Totals findings pass `active=false`, so they are **never** coverage-downgraded. The characterization therefore lives on the **phase + totals axes** (the trusted axes); the step axis is coverage-gated and deliberately avoided.
- `compareMetric` band = `max(0.25·|median|, 1.5·(P75−P25))`; `regression` iff candidate median **strictly** exceeds the baseline band-high. There is **no N term** — continuous power is a fixed tolerance, invariant to k beyond `MinSupport`.
- Below `MinSupport=3` every metric/rate is `insufficient` (cannot gate). `computeSensitivity`/`minFullFlipRuns` disclose the small-k reachability of the rate gates.

Sweep construction (synthetic `aggregate.Report` pairs):

- **Presence:** one phase row `p1`, baseline present k/k, candidate present p/k for p sweeping k → 0 (row omitted at p=0, matching the natural absence encoding). All non-presence metrics held equal, so only presence can gate. The **largest p** still yielding `OverallVerdict == regression` is the boundary.
- **Continuous (`tokens_out` on Totals):** baseline median M = 2000 with P25/P75 set to a chosen IQR; candidate median M·(1+e) with e swept in +5% steps; every other total held equal, so only `tokens_out` can gate. The smallest gating e is recorded at each k and its **k-invariance asserted** (identical at k=3 and k=30).
- **error-rate (Totals):** baseline 0/k errors, candidate q/k, q sweeping 1 → k.

## 2. Table A — presence / rate power

Phase axis, baseline present k/k, defaults. "Largest gating candidate presence p" is the highest surviving-presence count that still gates; the drop is the complement.

| Runs k | Largest gating presence p | Min detectable presence drop | Full flip (p=0) gates? | `MinFullFlipRuns` |
| --- | --- | --- | --- | --- |
| 3 | 0 | 3/3 (100%) | yes | 3 |
| 5 | 1 | 4/5 (80%) | yes | 3 |
| 10 | 5 | 5/10 (50%) | yes | 3 |
| 20 | 15 | 5/20 (25%) | yes | 3 |
| 30 | 23 | 7/30 (23.3%) | yes | 3 |

Reading it: at **k=3 no partial drop gates at all** — only a complete disappearance (3/3 → 0/3) fires, because three noisy observations cannot produce disjoint Wilson intervals for any intermediate rate. This is the honest, slightly ugly result the plan anticipated. As k grows the Wilson intervals sharpen and the boundary falls, until by k≈20–30 the `PresenceDelta` floor (20%) becomes the binding constraint rather than interval overlap (k=20 gates at exactly 25%; k=30 at 23.3%, one absence above the 20% delta line). A **full flip always gates for k ≥ 3** — `minFullFlipRuns(DefaultThresholds(), PresenceDelta) = 3`, cross-checked in the test; at every k in the sweep `Report.Sensitivity` is `nil` (the gate is reachable, so nothing is disclosed).

## 3. Table B — continuous-metric power (k-invariant)

`tokens_out` on Totals, baseline median M = 2000, band = `max(0.25·M, 1.5·IQR)`. The effect sweep steps in +5% increments. The "min detectable median shift" is identical at k=3 and k=30 (and every k between) — the k-invariance is asserted, not merely narrated.

| IQR/M | IQR | Band = max(0.25·M, 1.5·IQR) | Band window | Min detectable shift | k=3 | k=30 |
| --- | --- | --- | --- | --- | --- | --- |
| 0.00 | 0 | 500 (relative term) | [1500, 2500] | +30% | +30% | +30% |
| 0.05 | 100 | 500 (relative term) | [1500, 2500] | +30% | +30% | +30% |
| 0.15 | 300 | 500 (relative term) | [1500, 2500] | +30% | +30% | +30% |
| 0.30 | 600 | 900 (IQR term) | [1100, 2900] | +50% | +50% | +50% |

Reading it: for the realistic dispersion range (IQR/median ≤ 0.15) the **relative term dominates** — the band is a flat ±25% of the median regardless of spread, so the smallest gating grid step is +30% (the +25% shift lands exactly on the band edge and does **not** gate, since the comparison is strict `>`). The IQR term only takes over once IQR/median > 0.25/1.5 ≈ **0.167**; the 0.30 row shows that regime — a 1.5·IQR = 900 band, gating at +50% (the +45% edge again does not gate). Across **all four IQR settings the boundary is byte-identical at k=3 and k=30**: more repetitions shrink the sampling noise in the median *estimate*, but the tolerance band itself has no k dependence. Continuous power is a property of the band formula alone.

## 4. Table C — error-rate power

Totals `error_rate`, baseline 0/k, `ErrorRateDelta 0.1`, defaults.

| Runs k | Smallest gating error count q | Min detectable error rate |
| --- | --- | --- |
| 3 | 3 | 3/3 (100%) |
| 5 | 4 | 4/5 (80%) |
| 10 | 5 | 5/10 (50%) |
| 20 | 5 | 5/20 (25%) |
| 30 | 5 | 5/30 (16.7%) |

Same Wilson machinery as presence, so the small-k boundaries coincide (Wilson-bound at k = 3, 5, 10). They diverge at large k because `ErrorRateDelta` (0.1) is half of `PresenceDelta` (0.2): at k=30 the error gate stays Wilson-bound at q=5 (16.7%) while presence is already delta-bound at 23.3%. `minFullFlipRuns(DefaultThresholds(), ErrorRateDelta) = 3`, so a 0 → 100% error flip gates for k ≥ 3.

## 5. Honest limits

1. **The continuous gate has no variance / significance term.** The band is a fixed tolerance, not a hypothesis test; it cannot separate a real, systematic +20% cost or token creep from noise, because +20% < the 25% relative band **at every k**. This is deliberate — deterministic, explainable, and in line with the market (no mainstream Claude-Code eval tool ships statistical significance testing; see the V-2 competitive review, [2026-07-05-competitive-cto-review.md](2026-07-05-competitive-cto-review.md)) — but it means the continuous gate is a **coarse guardrail, not a sensitive detector**. Lower `MetricRelDelta` toward a metric's known real IQR/median when a workload's dispersion is small, or add a distributional / rank test (e.g. Mann–Whitney U over the two run groups) if sub-band drift must be caught. A complementary failure mode at the **near-zero** end is already observed on live data — F2 in the [2026-07-04 dogfood calibration](2026-07-04-dogfood-calibration.md): near-zero medians gate on trivial absolute deltas because both band terms collapse, with an absolute-floor knob as the deferred fix. Together these bound where the band is trustworthy — neither near zero nor for sub-25% shifts.
2. **Small-k `insufficient`.** Below `MinSupport=3` nothing gates. At k=3 only full flips gate on the rate axes (Table A); `computeSensitivity`/`minFullFlipRuns` are the disclosure surface for this and confirm `MinFullFlipRuns=3` for both presence and error at the default Z.
3. **These are SYNTHETIC distributions.** Each boundary isolates one axis on clean inputs — a single phase, degenerate or fixed IQR, identical non-swept metrics. Real agentic runs have correlated, heavy-tailed, cross-basket variance, and the design review flagged that the bands "have never been checked against real multi-run dispersion" ([2026-07-02-post-p0-cto-design-review.md](2026-07-02-post-p0-cto-design-review.md)). The one live basket to date (V-1, [2026-07-04 dogfood calibration](2026-07-04-dogfood-calibration.md)) already **corroborates the presence floor** — a deliberate degradation gates a full-flip presence regression at both k=5 and k=10, exactly as Table A predicts — but the continuous band width and the mid-range rate boundaries have not yet been validated against measured agentic IQRs.

## 6. PV-6b pointer (live validation, user-budget-gated)

PV-6b is the live half of PV-6: 2–3 real baskets (tasks × variants × reps at a cheap model), gated on an explicit user budget. It measures real per-phase and per-total IQR/median and error dispersion, then checks that (a) an A-vs-A control produces **no false regressions** at these k and (b) the synthetic min-detectable magnitudes in Tables A–C hold when the inputs are measured distributions rather than constructed ones. Only after PV-6b should the Table B band width and the `DefaultThresholds()` values be treated as validated for production gating.
