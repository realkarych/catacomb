# Eval-Management P1 Roadmap — Implementation Plan

> **For agentic workers:** This is the PROGRAM-level plan. Each PR gets its own detailed plan (authored just-in-time via superpowers:writing-plans) and is executed with superpowers:subagent-driven-development.

**Goal:** Extend the shipped P0 regression pipeline (labels → aggregate → regress/baselines → bench, PR #99–#104) with the three P1 capabilities recorded in the 2026-07-02 review and ADR-0022: external-score gating, longitudinal trends, and declared-checkpoint verification.

**Architecture:** all three are additive layers over the existing substrate — no reducer/identity changes, no viewer expansion, no new exporters.

## Dependency DAG

```
PR-F (annotation gates in regress)  ── independent
PR-G (regress --record + trends)    ── independent (schema v3)
PR-H (declared checkpoints + verification) ── independent (bench + graph query)
```

Execution order: PR-F → PR-G → PR-H (serial; shared files argue against parallel landing).

## PR-F — Annotation gates in `catacomb regress`

External scorers (DeepEval etc.) write numeric annotations via the ADR-0016 contract; `aggregate` already folds allowlisted `owner.key` values into step-row `MetricStats`. PR-F pipes them into the verdict: repeatable `--annotation <owner.key>[:higher-better|lower-better]` (default higher-better — score semantics), aggregation allowlist wired for both groups, step-row `compareMetric` with direction inversion for higher-better keys (candidate median BELOW the baseline band = regression), findings `Metric: "ann:<owner.key>"`, standard N/min-support/coverage gating. Docs: cli.md + workflows.md (DeepEval → annotations → gate walkthrough pointer).

## PR-G — `regress --record` + `catacomb trends`

Longitudinal memory for a baseline: `regress --record` (requires `--baseline name:<x>`) appends the produced `regress.Report` plus candidate selector, thresholds, and `nowFn()` timestamp into a new `regress_results` store table (schema v3 via the ADR-0017 runner; body JSON keyed by baseline name + auto ordinal). `catacomb trends <baseline> [--metric <m>] [--json]` lists recorded results chronologically: per-record overall verdict, regression count, and the candidate median evolution for the chosen metric (default: totals duration_ms/cost_usd/error_rate summary table). Read-only rendering — no plotting, no viewer.

## PR-H — Declared checkpoints + post-run verification

Basket tasks gain `checkpoints: [name, ...]` — the phases the agent is EXPECTED to mark in-run (CLAUDE.md/mcp `catacomb__mark` convention). Deterministic VERIFICATION, not synthesis (ADR-0022 explicitly rejects pattern-based marker synthesis): after each cell, bench fetches the session graph (existing `GET /v1/sessions/{hash}/graph`) and checks each declared name appears as a marker node; missing names are recorded per cell in the manifest (`missing_checkpoints`), warned on stderr, and summarized in the epilogue (`checkpoints: plan 8/8, verify 5/8`). ADR-0022 gets an amendment bullet recording declared-checkpoint verification semantics. Docs: basket schema + workflows.

## Global constraints (all PRs)

Worktree per PR; no comments in Go; 100% coverage TDD-first; `make lint` 0; codepolicy; deterministic outputs; markdownlint; live-verify before merge; squash-merge on green CI (authorized).
