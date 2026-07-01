# Eval-Management Layer Roadmap — Implementation Plan

> **For agentic workers:** This is the PROGRAM-level plan. Each PR below gets its own detailed, bite-sized task plan (authored just-in-time before implementation via superpowers:writing-plans) and is executed with superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax for tracking at the per-PR plan level.

**Goal:** Implement ADR-0022 — turn the shipped eval substrate (`step_key`/`phase_key`, pairwise diff, markers, annotations, repro) into a usable regression-testing product: run labels → group aggregation → `catacomb regress` gate → `catacomb bench` basket runner.

**Architecture:** deterministic distributional comparison of run groups aligned by `step_key`/`phase_key` (ADR-0022). Labels are generic run metadata; aggregation is a pure package over store reads; `regress` compares two groups with Wilson intervals (rates) and IQR bands (metric medians) and exits nonzero on regression; `bench` drives k×tasks×variants runs through the existing `catacomb run` wrapper with runner-emitted task markers. No LLM judging, no scoring — external scores join as numeric annotations.

**Tech Stack:** Go 1.26 (pure Go, no cgo), `modernc.org/sqlite`, cobra CLI, YAML basket config via the existing config decoding approach (`gopkg.in/yaml.v3` already vendored for `config/`).

## Global Constraints

- **Always work in a git worktree** under `.claude/worktrees/` (native `EnterWorktree`); never edit the shared checkout.
- **Pure Go, no cgo**; stdlib-first; SQLite only via `modernc.org/sqlite`.
- **No comments in Go code** — only `//go:build`/`//go:embed`/`//go:generate` directives (enforced by `internal/codepolicy`).
- **100% test coverage, TDD-first** (`make cover`); the threshold never goes down.
- **Deterministic core:** aggregation and comparison are pure functions — same inputs, any order → same report. No wall-clock, no map-iteration dependence in output ordering.
- **Errors:** sentinels + `errors.Is`/`errors.As`; wrap `fmt.Errorf("pkg.Op: %w", err)`. **Logging:** `log/slog` JSON; payloads only via redaction policy.
- **Workflow:** one PR = one logical change; feature branch from `master`; squash-merge; CI green before merge; no `--no-verify`.
- **Live-verify gate:** before merging each PR, verify against real runs (a real store with ≥2 recorded run groups), not only unit tests.
- **Scope boundary (ADR-0022):** no semantic scoring, no LLM judge, no new exporters, no viewer expansion.

---

## Strategic framing (why these PRs)

From the 2026-07-02 review (`docs/reviews/2026-07-02-eval-readiness-and-competitive-review.md`): checkpoint-diff regression testing of agentic flows is a verified market vacuum; Catacomb's substrate is ready but the management layer is 0% built, and pairwise diff alone produces false regressions on non-deterministic agents. The four gaps, in dependency order: (1) runs carry no experiment coordinates; (2) nothing aggregates a run group; (3) nothing compares groups or gates; (4) nothing executes a basket.

## Dependency DAG

```
PR-A (run labels) ──▶ PR-B (aggregate pkg) ──▶ PR-C (regress + baselines)
        └──────────────────────────────────▶ PR-D (bench runner)
```

**Execution order (serial, keeps `master` green):** PR-A → PR-B → PR-C → PR-D. PR-D only needs PR-A but lands last so `bench` can print a ready-made `regress` invocation in its epilogue.

---

## PR-A — Run labels (experiment coordinates)

**Goal:** every run can carry open `k=v` labels, inherited by child sessions, persisted, and selectable — the coordinates `bench`/`regress` need.

**Files:** `model/model.go` (`Run.Labels map[string]string`), `ingest/` (label capture from env-derived observation attrs), `cmd/catacomb/run.go` (`--label k=v` repeatable → sets `CATACOMB_LABELS` for the child, alongside `CATACOMB_RUN_ID`), `daemon/` (populate labels on run upsert), `store/` (labels round-trip inside `runs.body`; a pure selector helper `MatchLabels(run, selector)`), `cmd/catacomb/runs.go` (`--label` filter, labels column in `--json`).

**Contract:** `CATACOMB_LABELS="basket=checkout,variant=skill-v2,task=t1,rep=3"` — comma-separated `k=v`, keys `[a-z0-9_.-]+`, last write wins per key across sources, labels merge (union) across the executions of one run; malformed pairs are dropped with a warning, never fatal. Selector syntax: repeated `--label k=v` terms AND-ed.

**Acceptance:** labels set via env on a wrapped run appear on the persisted `Run` and in `catacomb runs --json`; child-session inheritance verified through the `run` wrapper env; selector filters correctly; absent labels ⇒ empty map, not nil-panic. 100% cover.

**Risks:** label injection via env on hook path — labels are metadata only, never identity (ADR-0011 untouched); cap count (32) and value length (256) to bound `runs.body` growth.

## PR-B — Run-group aggregation package

**Goal:** a pure `aggregate/` package that folds a set of finalized run snapshots into per-`step_key` / per-`phase_key` / run-level statistics (ADR-0022 §2).

**Files:** new `aggregate/aggregate.go`, `aggregate/quantile.go` (+ tests); read path in `store/` to load run snapshots by selector (reuses PR-A `MatchLabels`).

**Contract:** input = `[]RunSnapshot` (nodes with `step_key`, `phase_key`, status, metrics; run totals; numeric annotations opt-in via allowlist `owner.key` patterns). Output per key: `PresenceRate` (runs containing key / total runs), `StatusRates map[Status]float64`, `Median`/`P90` for `duration_ms`, `cost_usd`, `tokens_in`, `tokens_out`, `N` (support). Run-level: totals quantiles. Quantiles = nearest-rank on sorted values (deterministic, no interpolation dependence). Numeric annotation values aggregate identically, keyed `ann:<owner>.<key>`. Output ordering: lexicographic by key — no map-order leakage.

**Acceptance:** property test — permuting input run order yields byte-identical aggregate; hand-computed fixtures for presence/status/quantiles; annotation allowlist respected; k=1 group aggregates without division-by-zero. 100% cover.

**Risks:** memory on huge groups — stream per run, keep only per-key metric slices; document practical k ceiling (~1000) rather than engineering for it (YAGNI).

## PR-C — `catacomb regress` + persisted baselines

**Goal:** the verdict: compare baseline vs candidate groups, report per checkpoint/step/run-level, exit code for CI (ADR-0022 §4).

**Files:** new `regress/` pkg (pure compare: Wilson intervals, IQR bands, thresholds, alignment coverage), `cmd/catacomb/regress.go`, `cmd/catacomb/baseline.go` (`set`/`list`/`rm`), `store/` (new `baselines` table: name PK, execution ids JSON, repro fingerprints, created_at — schema migration v2 per ADR-0017 runner), config for thresholds (flags + optional `regress:` block in basket YAML later).

**Contract:** rates flagged iff Wilson 95% CIs disjoint AND |delta| > threshold (defaults: presence 0.2, error-rate 0.1); metrics flagged iff candidate median outside baseline median ± max(rel-threshold 0.25 × baseline median, 1.5 × baseline IQR); minimum support k ≥ 3 per side else "insufficient runs" (exit 2 if either side insufficient? — no: verdict `insufficient`, exit 2 reserved for operational errors; insufficient ⇒ exit 0 with explicit warning, `--strict` flips it to 1). Report sections: run-level totals → phases (primary) → steps (labeled untrustworthy when alignment coverage < 0.7). `--json` mirrors the human table. Selectors: `--baseline name:<baseline>` or `--baseline label:k=v,...`; same for `--candidate`.

**Acceptance:** golden-file report tests (human + JSON); synthetic fixtures: identical groups → pass/exit 0; injected error-rate jump → regression/exit 1; low-coverage step drift → phase verdict carries, steps marked; baseline persistence round-trips and survives store reopen. 100% cover. **Live-verify:** record two real run groups of one task (one with an induced failure), gate catches it.

**Risks:** threshold calibration — defaults are documented guesses; every flag threshold is configurable and the report prints the bands used, so miscalibration is visible, not silent.

## PR-D — `catacomb bench` basket runner

**Goal:** execute a declarative basket (tasks × variants × k reps) through `catacomb run`, with runner-emitted task markers and a manifest (ADR-0022 §3).

**Files:** new `bench/` pkg (basket YAML schema, plan expansion, manifest), `cmd/catacomb/bench.go` (+ tests); reuses `cmd/catacomb/run.go` wrapper logic and `mark` emission client.

**Contract:** basket YAML: `basket: <name>`, `reps: <k>`, `tasks: [{id, cmd, dir, env?}]`, `variants: [{id, env?, setup?}]` (setup = optional shell command run once per variant, e.g. switch a skill version); expansion = tasks × variants × reps cells, each executed as `catacomb run --run-id <generated> --label basket=,task=,variant=,rep=` wrapping `cmd`; runner emits `mark --name task:<id> --boundary start|end` around each cell (deterministic checkpoint floor); manifest JSONL (cell → run_id, exit code, basket content hash) written incrementally — `--resume` skips cells already in the manifest; failures of a cell recorded, never abort the basket (`--fail-fast` opts in). Epilogue prints the matching `catacomb regress --baseline label:... --candidate label:...` command.

**Acceptance:** expansion/order deterministic; manifest resume skips completed cells; cell failure recorded + basket continues; markers present in the recorded runs; env/labels propagate to child. 100% cover. **Live-verify:** a 2-task × 2-variant × k=3 basket against a real daemon, then `regress` over the produced groups end-to-end.

**Risks:** long-running baskets — resume + incremental manifest are the mitigation; parallel cell execution is deliberately out (sequential keeps daemon sharding and attribution simple; YAGNI until proven needed).

---

## Execution strategy

- **Per PR:** fresh worktree off updated `master` → author the detailed bite-sized plan (writing-plans) → subagent-driven-development (fresh subagent per task, TDD, review between) → whole-branch review (requesting-code-review) → **live-verify** → push → open PR → wait for green CI → squash-merge → delete worktree → rebase next.
- **Docs:** each PR updates `docs/guide/` where it adds CLI surface (`cli.md`, `workflows.md`).
- **Interrupt autonomy only on genuine uncertainty** (a real design fork where the owner's intent is unclear) — otherwise proceed and report.
