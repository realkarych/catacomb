# SP2 — Reliability metrics: design

- **Date:** 2026-07-12
- **Status:** approved design (ADR-0027 SP2; decisions adjudicated there and in the [gap review](../reviews/2026-07-12-eval-best-practices-gap-review.md) §2.2/§4.2; execution pre-approved)
- **Related:** [ADR-0027](../../adr/0027-verification-layer-and-reliability-metrics.md), [ADR-0023](../../adr/0023-regression-gate-sensitivity-at-small-k.md), SP1 spec [2026-07-12-sp1-verifier-contract-design.md](2026-07-12-sp1-verifier-contract-design.md), PV-6a [gate-power calibration](../reviews/2026-07-08-gate-power-calibration.md)

SP1 gave the gate a task-success axis (`verifier.pass` per cell). SP2 adds the two reliability instruments the canon prescribes on top of it: **pass^k** (τ-bench's all-k-trials-succeed estimator — the metric that separates a demo from a shippable agent) and a **paired sign test** over per-task medians (the closed-form answer to PV-6a's honest limit: the continuous band is k-invariant and blind below +25% at any k). Both are deterministic, dependency-free, and RNG-free.

## 1. Per-task statistics in aggregate

`aggregate.Report` gains a task axis derived from `Run.Labels["task"]`:

```go
type TaskOutcome struct {
	N    int `json:"n"`
	Ones int `json:"ones"`
}

type TaskStats struct {
	Task       string       `json:"task"`
	Runs       int          `json:"runs"`
	Outcome    *TaskOutcome `json:"outcome,omitempty"`
	DurationMS MetricStats  `json:"duration_ms"`
	CostUSD    MetricStats  `json:"cost_usd"`
	TokensIn   MetricStats  `json:"tokens_in"`
	TokensOut  MetricStats  `json:"tokens_out"`
}
```

`Report.Tasks []TaskStats` (JSON `tasks`, omitempty), sorted by task id. Runs whose `Labels["task"]` is empty are excluded; a group with no task labels produces `nil` — the whole SP2 layer is then dormant (this keeps the PV-6a power fixtures and any label-less selection byte-identical). `Outcome` is set only when the run carries the binary `verifier.pass` annotation (`Ones` counts value==1; runs without the annotation don't contribute to `N`). Metric distributions reuse the existing per-run totals machinery (duration excludes unmeasured, cost/tokens zero-fill — the ADR-0022 asymmetry).

## 2. pass^k — reliability reporting

For a task with `N` runs and `c = Ones` successes, the unbiased estimator of "all k independent trials succeed" is `pass^k = C(c,k)/C(n,k)` (0 when `c < k`) — τ-bench's estimator, closed-form.

- **Where:** a `Reliability` block on the regress report, computed per group from `Report.Tasks`:

```go
type TaskReliability struct {
	Task  string    `json:"task"`
	N     int       `json:"n"`
	Ones  int       `json:"ones"`
	PassK []float64 `json:"pass_k"`
}

type GroupReliability struct {
	Tasks []TaskReliability `json:"tasks"`
	KMax  int               `json:"k_max"`
	Mean  []float64         `json:"mean"`
}

type Reliability struct {
	Baseline  GroupReliability `json:"baseline"`
	Candidate GroupReliability `json:"candidate"`
}
```

`PassK[i]` is pass^(i+1) for k=1..KMax; `KMax` = the smallest task `N` in the group (curves stay comparable across tasks); `Mean[i]` = unweighted mean over tasks (τ-bench convention). `Report.Reliability *Reliability` (omitempty) is present only when both groups carry at least one task outcome.

- **Human render:** one line per group in the report epilogue: `reliability: pass^1 0.93 -> pass^5 0.67 (7 tasks)` style — first and last points of the mean curve; the full per-task curves live in `--json`.
- **Gating: none.** pass^k is informational — the same binary data already gates through the Wilson pass-rate axis (SP1); double-gating one signal would inflate false positives. This boundary is documented.

## 3. Paired sign test — per-task continuous deltas

- **Pairing:** for each task present in BOTH groups with `Runs >= MinSupport` on each side, and each continuous metric (`duration_ms`, `cost_usd`, `tokens_in`, `tokens_out`), the pair delta is `candidate median − baseline median` (per-task medians from `Report.Tasks`, nearest-rank as everywhere).
- **Test:** exact one-sided sign test. Drop zero deltas; with `m` non-zero deltas of which `s` are positive, `p_regression = P(X >= s | m, 0.5)` (binomial tail, closed form); `p_improvement` symmetric. No ranks, no RNG, no distributional assumptions — the deliberate trade against Wilcoxon's extra power (ADR-0027 named both; the sign test is chosen for SP2 because Wilcoxon's exact treatment of tied |deltas| adds complexity the task counts here can't justify; amend if sign-test power proves insufficient in practice, mirroring the ADR-0022→0023 pattern).
- **Verdicts:** new findings with scope `"paired"`, key = "", metric = the metric name. `regression` when `p_regression <= PairedAlpha`; `improvement` symmetric; `insufficient` when matched tasks `< PairedMinTasks` (with detail naming both counts); else `ok`. Detail always carries the evidence: `"+6/7 tasks, p=0.0625"`.
- **Gating:** a paired `regression` counts toward exit 1 exactly like any other regression — this is the instrument that catches systematic sub-25%-band drift (a +10% cost creep that repeats across 8 tasks gives p=0.0039 and gates; the same creep is invisible to the band at any k). Scope ordering: `paired` sorts after `total` in the findings list.
- **Thresholds:** `PairedAlpha` (default 0.05), `PairedMinTasks` (default 5) join `Thresholds` with CLI flags `--paired-alpha`, `--paired-min-tasks` (validated > 0; alpha in (0,1)).
- **Disclosure (ADR-0023 discipline):** when the paired layer is active but cannot fire — `n_tasks < PairedMinTasks`, or unanimity at the current `n_tasks` cannot reach alpha (`0.5^n_tasks > PairedAlpha`) — the report's sensitivity block gains a `paired` note naming the smallest task count at which a unanimous shift would gate. The gate never goes silently impotent.

## 4. Dormancy and compatibility

- No task labels → `Report.Tasks == nil` → no Reliability block, no paired findings, no disclosure. PV-6a power tests and all existing fixtures are untouched by construction; the calibration guard (`TestGatePower`) must pass byte-identically.
- All schema additions are omitempty; old records/baselines parse unchanged; `Thresholds` extension is additive (same treatment as `AnnotationRateDelta`).
- Evaluation-agnostic boundary (ADR-0022): the paired layer reads the same deterministic observables; pass^k reads only `verifier.pass`, the one key with sanctioned semantics.

## 5. Testing and acceptance

- **TDD** throughout; 100% coverage; no comments; no new deps.
- **Characterization tests** (PV-6a pattern): `regress/paired_power_test.go` pins the sign-test firing boundaries — smallest unanimous n_tasks at α=0.05 is 5; at n_tasks=8, s=7 fires (p≈0.035) but s=6 does not (p≈0.145); zero-delta handling; both-directions symmetry. `TestGatePower` untouched.
- **pass^k correctness table**: C(c,k)/C(n,k) against hand-computed values incl. τ-bench's motivating example shape (c=3,n=4: pass^1=0.75, pass^2=0.5, pass^3=0.25, pass^4=0).
- **Hermetic E2E extension** (the SP1 driver): assert the regress `--json` now carries `reliability` with baseline pass-curve all-1.0 and degraded all-0.0 for the sql task; assert the paired block reports `insufficient` with the disclosure note at n_tasks=1 (the honest path — a 1-task basket can never fire paired, and the driver proves the gate says so out loud). Multi-task firing stays in the deterministic Go tests (closed-form boundaries need no live runs).
- **Docs:** cli.md (flags, reliability block, paired findings semantics), workflows.md (when paired fires; the ≥5-tasks guidance mirroring the reps≥5 nudge).

## 6. Boundaries

Not in SP2: Wilcoxon signed-rank (documented deferral, §3); clustered SEs; pass^k gating; per-step paired tests (task axis only); basket-format changes (task ids already exist). SP3 (env stamps + Pareto) and later waves are untouched.
