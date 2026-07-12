# SP2 Reliability Metrics Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add pass^k reliability reporting and a paired sign test over per-task medians to the regress gate, per the SP2 spec.

**Architecture:** One PR wave on branch `feat/reliability-metrics`, four tasks: (1) per-task stats in aggregate; (2) pass^k reliability block in regress; (3) paired sign test findings + disclosure + CLI flags; (4) characterization tests, hermetic-E2E assertions, docs. Everything is dormant when runs carry no `task` label — PV-6a fixtures stay byte-identical.

**Tech Stack:** Go stdlib only; exact types and semantics in `docs/specs/2026-07-12-sp2-reliability-metrics-design.md` (the spec is the source of truth for every struct and formula — read it first, use it verbatim).

## Global Constraints

- **No comments in Go code** (internal/codepolicy). **TDD**; **coverage 100%**; `make lint` green; gosec (`go run github.com/securego/gosec/v2/cmd/gosec@latest -exclude=G304 ./...`) Issues: 0 — avoid credential-looking identifiers.
- Table-driven tests; testify require/assert; no time.Sleep; no new Go deps.
- **HARD:** `regress/power_test.go` untouched and `go test ./regress/ -run TestGatePower` passes byte-identically — the SP2 layer must be provably dormant without task labels.
- One PR, branch `feat/reliability-metrics` from master, squash after green CI (Hermetic E2E is now a required check).

## File Map

| Task | Create | Modify |
|---|---|---|
| 1 | — | `aggregate/aggregate.go`, `aggregate/aggregate_test.go` |
| 2 | `regress/reliability.go`, `regress/reliability_test.go` | `regress/regress.go`, `regress/render.go` (+ tests) |
| 3 | `regress/paired.go`, `regress/paired_test.go` | `regress/compare.go` (Thresholds), `regress/regress.go`, `regress/sensitivity.go`, `regress/render.go`, `cmd/catacomb/regress.go` (+ tests) |
| 4 | `regress/paired_power_test.go` | `e2e/hermetic/run.sh`, `docs/guide/cli.md`, `docs/guide/workflows.md` |

---

### Task 1: Per-task stats in aggregate

**Interfaces (spec §1, verbatim):** `TaskOutcome{N, Ones int}` (JSON n/ones), `TaskStats{Task string; Runs int; Outcome *TaskOutcome; DurationMS/CostUSD/TokensIn/TokensOut MetricStats}` (JSON task/runs/outcome omitempty/duration_ms/cost_usd/tokens_in/tokens_out), `Report.Tasks []TaskStats` (JSON tasks, omitempty) sorted by task id. Grouping key `Run.Labels["task"]`; empty label → run excluded; no labeled runs → nil. Outcome only from the binary `verifier.pass` annotation (value==1 counts toward Ones; runs lacking the annotation don't contribute to Outcome.N). Metric distributions per run-totals rules (duration excludes unmeasured; cost/tokens zero-fill).

- [ ] **Step 1:** Failing table-driven tests: two tasks with distinct medians and outcomes (one run lacking verifier.pass → Outcome.N < Runs); unlabeled runs excluded; all-unlabeled → `assert.Nil(rep.Tasks)`; sort order; nearest-rank medians consistent with existing stats.
- [ ] **Step 2:** Run → FAIL. **Step 3:** Implement (reuse the runTotals accumulation per task bucket). **Step 4:** `go test ./aggregate/ -race` PASS; `make cover` 100%. **Step 5:** Commit `feat(aggregate): per-task stats (outcomes + metric medians)`.

### Task 2: pass^k reliability block

**Interfaces (spec §2, verbatim):** `TaskReliability{Task string; N, Ones int; PassK []float64}`, `GroupReliability{Tasks []TaskReliability; KMax int; Mean []float64}`, `Reliability{Baseline, Candidate GroupReliability}`; `Report.Reliability *Reliability` (JSON reliability, omitempty) set only when BOTH groups have ≥1 task outcome. `passK(c, n, k) = C(c,k)/C(n,k)`, 0 when c<k; KMax = min task N in the group; Mean = unweighted mean over tasks per k. Human render epilogue line per group: `reliability (baseline): pass^1 0.93 -> pass^5 0.67 (7 tasks)`. No gating.

- [ ] **Step 1:** Failing tests: the estimator table incl. c=3,n=4 → {0.75, 0.5, 0.25, 0} and boundary cases (c=n → all 1.0; c=0 → all 0.0); KMax across unequal task Ns; Mean; nil when either side lacks outcomes; render line both present and absent; JSON round-trip.
- [ ] **Step 2:** FAIL. **Step 3:** Implement `regress/reliability.go` (binomial via big-free float ratio — compute C(c,k)/C(n,k) as a product of ratios to avoid overflow: `for i := 0; i < k; i++ { r *= float64(c-i) / float64(n-i) }`). Wire into `Compare` after findings. **Step 4:** PASS + 100% + TestGatePower untouched. **Step 5:** Commit `feat(regress): pass^k reliability reporting`.

### Task 3: Paired sign test

**Interfaces (spec §3, verbatim):** `Thresholds` += `PairedAlpha float64` (default 0.05), `PairedMinTasks int` (default 5). Pairs: tasks in both reports with `Runs >= MinSupport` both sides; deltas of per-task medians for duration_ms/cost_usd/tokens_in/tokens_out. Exact one-sided binomial: drop zeros; `p = P(X >= s | m, 0.5)` closed form (`math` only). Findings scope `"paired"` (sort after `total`: extend scopeOrder), metric = metric name, detail `"+6/7 tasks, p=0.0625"`; regression/improvement/insufficient/ok per spec; paired regression gates exit 1. Sensitivity block gains `Paired *RateSensitivity`-style disclosure (spec §3: note when `n_tasks < PairedMinTasks` OR `0.5^n_tasks > PairedAlpha`, naming the smallest firing task count). CLI: `--paired-alpha` (validate 0<a<1), `--paired-min-tasks` (validate >0), plumbed like `--annotation-rate-delta`; Record serialization additive.

- [ ] **Step 1:** Failing tests: p-value table (n=5 unanimous → 0.03125 gates at 0.05; n=8 s=7 → ~0.03516 gates, s=6 → ~0.14453 does not); zero-delta drop; improvement direction; insufficient below PairedMinTasks with counts in detail; dormant with nil Tasks; findings sort order; flags plumb + validation errors; disclosure note fires when 0.5^n > alpha.
- [ ] **Step 2:** FAIL. **Step 3:** Implement `regress/paired.go` + wiring. **Step 4:** Full gates + TestGatePower untouched + gosec 0. **Step 5:** Commit `feat(regress): paired sign test over per-task medians`.

### Task 4: Characterization, E2E, docs

- [ ] **Step 1:** `regress/paired_power_test.go` (PV-6a pattern, drives real `Compare`): pin smallest unanimous firing n_tasks == 5 at defaults; the n=8 boundary pair; symmetry; assert the disclosure text at n_tasks ∈ {1..4}. These are calibration guards — a future threshold change must fail them.
- [ ] **Step 2:** Extend `e2e/hermetic/run.sh` (follow its existing helper/assert style; renumber nothing — append a new section): regress --json asserts `reliability.baseline.mean` all 1.0 and `reliability.candidate.mean` all 0.0 on the degraded comparison (sql task: 5/5 vs 0/5), and the paired block reports insufficient with the disclosure note at n_tasks=1. Re-run driver locally → PASS (assertion count grows; state the new count). shellcheck clean.
- [ ] **Step 3:** Docs: cli.md (reliability block, paired findings, both flags), workflows.md (≥5-tasks guidance mirroring the reps≥5 nudge; when paired fires vs the band). markdownlint clean.
- [ ] **Step 4:** Full gates; commit `test(regress): paired power characterization; e2e reliability assertions; docs`.

## Acceptance

Hermetic driver green with the new assertions on the PR (required check); TestGatePower byte-identical; paired boundaries pinned by calibration tests; 100% coverage; docs accurate.

## Self-review notes

Spec coverage: §1→Task1, §2→Task2, §3→Task3, §5→Task4; dormancy (§4) asserted in Tasks 1–3 tests + the untouched power guard. Type names consistent across tasks (TaskStats/TaskOutcome consumed by reliability.go and paired.go). No placeholders; formulas and boundary numbers stated exactly.
