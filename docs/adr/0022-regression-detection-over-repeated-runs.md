# ADR-0022: Regression detection over repeated runs (baskets, baselines, aggregation)

- **Status:** Accepted
- **Date:** 2026-07-02
- **Deciders:** @realkarych
- **Related:** spec §2 (non-goals), §5.4, §5.6, §14, §15; ADR-0005, ADR-0011, ADR-0016, ADR-0017; review `docs/reviews/2026-07-02-eval-readiness-and-competitive-review.md`

## Context

The target product scenario is: change a pipeline component (a skill, an MCP tool, a prompt) → re-run an identical basket of agentic tasks → diff the runs at user-defined checkpoints → decide whether the change regressed the system. The substrate for this shipped (`step_key`/`phase_key`, pairwise `diff`, phase `subgraph`, annotations, repro fingerprints), but three things make the scenario inexpressible today:

- **Non-determinism.** Two runs of the same task are two samples from a distribution. A pairwise diff faithfully reports every sampled difference, so it cannot distinguish a regression from sampling noise. Any trustworthy verdict needs k repetitions per variant and aggregation — no ADR covers this.
- **No experiment coordinates.** `run_id` groups the executions of one wrapper invocation (ADR-0005), but nothing records *which basket, task, variant, and repetition* a run belongs to, so run groups cannot even be selected for comparison.
- **No verdict machinery.** There is no baseline concept, no thresholds, no gate with an exit code — nothing a CI job can consume.

A scope boundary must also be drawn: the design spec declares "no evaluation / scoring / optimality" a non-goal. Comparing distributions of *deterministic observables already in the graph* (status, presence, duration, cost, tokens) is analytics over the graph — the same family as `diff`, which is in scope. Judging semantic output *quality* stays out (delegated via annotations to DeepEval/agentevals per ADR-0016).

## Decision

Build the regression layer as **deterministic distributional comparison of run groups, aligned by `step_key`/`phase_key`**, in four pieces.

1. **Run labels — experiment coordinates.** `Run` gains an open `labels map[string]string`, populated at capture time from the `CATACOMB_LABELS` env var (`k=v,k2=v2`, inherited by child sessions exactly like `CATACOMB_RUN_ID`) and `catacomb run --label k=v`. Labels are opaque to the reducer, persisted with the run, and selectable (`labels.basket=checkout`, `labels.variant=skill-v2`). The bench runner (3) sets the conventional labels `basket`, `task`, `variant`, `rep`; nothing in the store schema hard-codes those names. Stored inside the existing `runs.body` JSON — a schema-version bump per ADR-0017 only if an index proves necessary.

2. **Group aggregation.** A new pure package aggregates a *run group* (any label/run-id selector resolving to a set of executions of the same task) into, per `step_key` and per `phase_key`:
   - **presence rate** — fraction of runs containing the step/phase (a missing agent-emitted checkpoint is thereby a first-class signal, not an error);
   - **status rates** — ok / error / blocked / cancelled / unknown fractions;
   - **metric quantiles** — median and p90 of `duration_ms`, `cost_usd`, `tokens_in/out`, plus run-level totals;
   - **numeric annotations (opt-in)** — an `annotations.<owner>.<key>` whose values parse as numbers aggregates like a built-in metric, so external scorers' verdicts can join the comparison without Catacomb interpreting them.

3. **Bench runner — deterministic basket execution.** `catacomb bench` reads a declarative basket file (YAML: tasks × variants × k repetitions; each task is a command template with a working directory) and, for each cell, invokes the existing `catacomb run` wrapper with generated `--run-id` and labels, **emitting task-level start/end markers itself**. Agent-emitted checkpoints (`mcp__catacomb__mark` per a CLAUDE.md convention) remain the *in-run* channel; the runner-emitted markers guarantee that at minimum the task boundary always aligns even when the agent forgets to mark. The runner writes a manifest (basket hash, run ids, repro fingerprints) and is resumable per cell.

4. **Comparison, verdict, gate.** `catacomb regress --baseline <selector> --candidate <selector>` compares two groups and emits a per-checkpoint / per-step / run-level report (human table + `--json`) and an exit code (0 pass, 1 regression, 2 error) for CI:
   - **Rates** (presence, error): flagged only when the Wilson 95% intervals of baseline and candidate are disjoint **and** the delta exceeds the configured threshold — closed-form, dependency-free, suppresses small-k noise.
   - **Metrics:** candidate median flagged when outside baseline median ± max(configured threshold, a baseline-IQR-derived noise band).
   - **Alignment honesty:** changing the component under test legitimately changes prompts and therefore some `step_key` input hashes. The report always states **alignment coverage** (fraction of baseline steps matched in the candidate); when coverage is low, step-level verdicts are labeled untrustworthy and the phase level (checkpoints) — robust to step drift by construction — carries the verdict. This is precisely why checkpoints are the primary comparison axis.
   - **Baselines:** a named baseline is a persisted pointer (name → resolved execution set + repro fingerprints + created-at) in a new store table, so "golden" groups survive label churn.

Minimum-support rule everywhere: no verdict is issued for k below a configured floor (default 3); the report says "insufficient runs" instead of guessing.

## Alternatives considered

- **Pairwise diff as the verdict (status quo)** — reports sampling noise as regressions; unusable as a gate. Kept as the drill-down view under the aggregate.
- **Full statistical framework** (permutation tests, Mann-Whitney, FDR control) — heavier machinery than k=3..10 runs can feed; deferred as a future amendment once real usage shows the simple bands mis-flagging.
- **AgentAssay-style whole-trace statistical fingerprints** — detects *that* behavior changed but cannot localize *where*; loses the checkpoint/step localization that is the product. Possible later as a cheap pre-screen, not the primary.
- **Built-in LLM judge / semantic scoring** — violates the evaluation-agnostic boundary; stays delegated to external scorers whose numeric annotations pipe into (2).
- **First-class experiment/variant tables** — a heavier schema for what labels + a baselines pointer express; revisit only if management UX outgrows selectors.
- **Marker synthesis from step patterns** (inferring checkpoints when the agent forgot to mark) — magic that can mis-align phases silently; rejected in favor of runner-emitted boundaries + presence-rate visibility of missing marks.

## Consequences

- **+** The target scenario becomes expressible end-to-end: basket → k runs per variant → aggregate → gate, with checkpoints as the noise-robust comparison axis and CI-consumable output.
- **+** Stays inside the deterministic-analytics boundary: no judging, no scoring; external quality signals join via the existing annotations contract (ADR-0016).
- **+** Labels are generic infrastructure (useful beyond bench: tagging CI runs, environments) rather than a bespoke experiment schema.
- **−** New surface: a basket format, two CLI verbs, an aggregation package, a baselines table — all perpetual maintenance.
- **−** The statistics are deliberately simple (Wilson intervals, IQR bands); they suppress noise but are not hypothesis tests, and the ADR must be amended if they prove miscalibrated.
- **−** Step-level comparison degrades exactly when the tested change rewrites prompts (alignment drift); the design mitigates by reporting coverage and privileging phase-level verdicts, but users must place checkpoints for the guarantee to bite.
