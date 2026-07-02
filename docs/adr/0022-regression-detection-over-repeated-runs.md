# ADR-0022: Regression detection over repeated runs (baskets, baselines, aggregation)

- **Status:** Accepted
- **Date:** 2026-07-02
- **Deciders:** @realkarych
- **Related:** spec §2 (non-goals), §5.4, §5.6, §14, §15; ADR-0005, ADR-0011, ADR-0016, ADR-0017; review `docs/reviews/2026-07-02-eval-readiness-and-competitive-review.md`
- **Amended by:** ADR-0023 — one-sided rate bounds (z=1.645 default, `--z`), `--fail-on-notable` escalation, and per-invocation sensitivity disclosure replace the two-sided Wilson-95% gate of §4.

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

## Amendments

Adjudications from the PR-B (aggregation package) review that refine §4 without changing the decision:

- **Annotation scope is step-level only for now.** Numeric annotation metrics (`annotations.<owner>.<key>`) aggregate at the `step_key` level exclusively. Phase markers are synthesized at snapshot time and marker-keyed annotations are not persisted or reattachable to a marker yet, so phase-level annotation metrics are deferred until marker-annotation persistence lands. Phase rows therefore carry no `annotations` block.
- **Duration distributions exclude unmeasured values.** Step, phase, and run-total `duration_ms` distributions sample only measured durations: a step run with all-nil occurrence durations, a phase run whose markers are all open (nil start or end), and a run missing either `started_at` or `ended_at` contribute no duration sample. Consequently a duration stat's `N` may be below `Present` (step/phase) or below `Runs` (totals). Zero-filling was rejected because a crashed run's 0 ms masks latency regressions by dragging the candidate median down.
- **Cost and token distributions keep zero-fill.** `cost_usd` and `tokens_in`/`tokens_out` are additive resources: an absent value is genuinely 0 spent, so those stats zero-fill and their `N` tracks `Present`/`Runs`. This asymmetry with duration is deliberate, per the PR-B review adjudication.

Adjudications from the PR-D (bench runner) review that refine §3 without changing the decision:

- **Task-boundary checkpoints are best-effort-with-recorded-visibility, not a guarantee.** The runner-emitted `task:<id>` start/end markers require an observed `session_id` in the child's stream-json before they can be placed; a cell whose child never surfaces a session id (or dies before it does) records no marks. Marker *acceptance* by the daemon is not the same as the phase *landing* in the finalized graph. The bench manifest therefore records `marked: true|false` per cell, and cells that failed to mark surface downstream as presence-rate drops in `regress` (per §2's "a missing agent-emitted checkpoint is a first-class signal"). This supersedes the §3 phrasing that the runner-emitted markers "guarantee that at minimum the task boundary always aligns": the boundary is best-effort with per-cell recorded visibility, not an unconditional guarantee.
- **The bench manifest records basket hash and run ids, not repro fingerprints.** §3 lists the manifest as holding "basket hash, run ids, repro fingerprints". The shipped manifest records the basket content hash and the per-cell run ids (plus task/variant/rep, exit code, session id, `marked`, finish time, and an optional note) but **not** repro fingerprints. Repro is captured daemon-side per run and persisted with baselines per §4; duplicating it in the client-written manifest would create a second, drift-prone source of truth. The manifest stays a thin, resumable execution ledger.

Adjudications from the PR-E (regression follow-ups) review that refine §2 without changing the decision:

- **The `nodes` and `occurrences` count metrics are one-sided: higher counts read as regressions.** The run-total `nodes` metric and the step-level `occurrences` metric flow through the same `max(metric-rel-delta × |median|, iqr-factor × IQR)` band as latency and cost, and that band flags the candidate only when its median rises *above* the upper bound. This is deliberate for latency and cost but blunt for counts: a pipeline that legitimately grew — more tool calls, a longer plan, an added phase — trips a `nodes`/`occurrences` regression even though nothing degraded. The mitigation is to raise `--metric-rel-delta` (or `--iqr-factor`) so ordinary growth stays inside the band, or to read a lone count finding as informational and let the latency, cost, and error-rate verdicts carry the gate. A signed or capacity-aware count model was rejected as scope creep; the band stays symmetric and the caveat is documented here.

Adjudications from the PR-H (declared checkpoints) review that refine §3 without changing the decision:

- **Declared checkpoints are verified post-cell, best-effort-with-recorded-visibility.** A basket task may list `checkpoints:` — the agent-emitted phase names the run is expected to mark itself (via `mcp__catacomb__mark`, per a CLAUDE.md convention). After each cell that surfaced a `session_id`, the runner fetches the finalized session graph and records which declared names are absent as markers: `missing_checkpoints` in the manifest, a stderr warning, and a per-task `checkpoints[<task>]: <name> <hit>/<verified>` epilogue summary. This mirrors the task-boundary floor's contract exactly — verification is visibility only, recorded per cell, and **never gates**: a cell with no session id or a failed graph fetch is skipped (reason recorded in the manifest note), and missing phases carry weight only downstream as presence-rate drops in `regress` (per §2), where the verdict is actually issued. Pattern-based marker synthesis stays rejected (per Alternatives): declared checkpoints assert what the agent *should* mark and report what it *did*, they never fabricate a marker.
