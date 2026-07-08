# PV-6a: Deterministic Gate-Power Characterization

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development / executing-plans. One cohesive task (characterization test + calibration report). Steps use `- [ ]`.

**Goal:** Characterize the statistical power of the regression gate — "what regression magnitude does it reliably catch at k = {3,5,10,20,30}?" — by driving the real `regress.Compare` across an effect-size × k sweep on synthetic `aggregate.Report` inputs. Deterministic, free (no live Claude). This is the analytical half of PV-6; the live-basket validation (PV-6b) is a separate, user-budget-gated milestone.

**Why deterministic answers most of it:** gate power is a property of the math (Wilson score intervals for rates, `max(MetricRelDelta·|median|, IQRFactor·IQR)` band for continuous metrics, small-k sensitivity), not of Claude. Live runs only confirm real agentic variance matches the synthetic distributions.

## Load-bearing gate facts

Verified against `regress/compare.go` + `regress/regress.go`:

- Presence is fed to `compareRate` as **absence** (`regress.go:127`: `bRuns-bPresent`), so a presence drop → rising absence → `regression` when Wilson(absence) intervals are disjoint AND delta > `PresenceDelta` (0.2). Full flip present N/N→0/N ⇔ absence 0/N→N/N.
- Phase findings pass `active=false` (`regress.go:71`) → never coverage-downgraded. Totals likewise. Characterize on the **phase + totals axes** (the trusted axes; step axis is coverage-gated).
- `compareMetric` band = `max(0.25·|median|, 1.5·(P75−P25))`, regression iff `candidate median > baseline band-hi`. **No N term** — continuous power is a fixed tolerance band, invariant to k beyond `MinSupport`.
- `MinSupport=3` → below it, `insufficient`. `regress/sensitivity.go` `computeSensitivity`/`minFullFlipRuns` disclose when a rate gate is unreachable at the given k.
- Defaults: `DefaultThresholds()` (PresenceDelta 0.2, ErrorRateDelta 0.1, MetricRelDelta 0.25, IQRFactor 1.5, Z 1.645).

---

## Task 1: gate-power characterization test + calibration report

**Files:**

- Create: `regress/power_test.go` — a table-driven characterization that constructs synthetic `aggregate.Report` pairs and calls the exported `regress.Compare(regress.Input{Baseline, Candidate}, DefaultThresholds())`, asserting the detection boundary at each k. It doubles as a calibration regression guard (if a future threshold change moves the boundary, this test fails loudly).
- Create: `docs/reviews/2026-07-08-gate-power-calibration.md` — the calibration report (tables + methodology + honest limits + PV-6b pointer).

**Method (drive the REAL entry point):**

- Build `aggregate.Report{Runs:k, Phases:[]Row{...}, Totals:RunTotals{...}}` for baseline and candidate. Construct a single phase Row (`active` path is false for phases, so no downgrade) and Totals metrics.
- **Presence sweep:** baseline phase present k/k; candidate present p/k for p = k,k−1,…,0. For each k in {3,5,10,20,30}, record the largest candidate-present count p that still yields `OverallVerdict==regression` (the detection threshold), and whether a full flip (p=0) gates. Cross-check `computeSensitivity`/`minFullFlipRuns` for the same k.
- **Continuous sweep (tokens_out on Totals):** baseline median M (pick a representative, e.g. 2000) with a realistic IQR (sweep IQR/M ∈ {0, 0.05, 0.15}); candidate median M·(1+e) for effect e = 0.05,0.10,…,1.0. Record the smallest e yielding `regression` at each k — demonstrating it is **k-invariant** and equals `max(0.25, 1.5·IQR/M)`. Assert the k-invariance explicitly (same boundary at k=3 and k=30).
- **error_rate sweep:** baseline 0/k errors, candidate q/k; smallest q that gates at each k (Wilson, ErrorRateDelta 0.1).
- Keep MetricStats internally consistent: for a degenerate distribution set P25=P75=Median (IQR 0); for spread, set P25/P75 around the median. N = k.

**Report structure (`docs/reviews/…`):**

1. One-paragraph verdict: the gate is a **rate-sharpens-with-k, continuous-fixed-band** detector; state the practical detection floors.
2. Table A — presence/rate power: k × {min detectable presence drop, full-flip gates? y/n, MinFullFlipRuns}.
3. Table B — continuous-metric power: the fixed band (25% / 1.5·IQR), demonstrated k-invariant; what this means (more reps stabilize the median estimate but do NOT tighten the band).
4. Table C — error-rate power.
5. Honest limits: continuous gate has no variance/significance test (flagged in the V-2 review too); recommend when to lower `MetricRelDelta` or add a distributional test; small-k `insufficient` behavior; these are SYNTHETIC distributions — PV-6b validates against real agentic variance.
6. PV-6b pointer: live 2–3 baskets, user-budget-gated.

**Contract:**

- No comments in Go; 100% coverage held (a new `_test.go` adds coverage, never reduces it); the test asserts concrete boundaries (not just prints) so it is a real guard; `go build ./... && go test ./regress/ ./...`; `make cover` 100%; `golangci-lint run`; markdownlint the report.
- Numbers in the report must be produced BY the test run (paste real `Compare` outputs), not hand-computed. If a boundary surprises you (e.g. presence at k=3 can't gate a partial drop), report the real result — do not fudge to a prettier number.
- Commit `test(regress): deterministic gate-power characterization + calibration report (PV-6a)` + trailer.

---

## Self-review checklist

- The characterization drives `regress.Compare` (the real CLI entry point), not the low-level comparators directly.
- Presence uses the absence encoding; phases/totals (not steps) so no coverage downgrade muddies the boundary.
- Report numbers are copied from the actual test run; k-invariance of the continuous band is asserted, not just asserted in prose.
