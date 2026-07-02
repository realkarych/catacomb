# PR-C: `catacomb regress` + Persisted Baselines Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The verdict layer of ADR-0022 §4: compare a baseline run group against a candidate group and emit a per-checkpoint/per-step/run-level report with a CI-consumable exit code, plus named persisted baselines.

**Architecture:** a pure `regress/` package compares two `aggregate.Report`s under a `Thresholds` config — Wilson 95% intervals for rates, median-vs-IQR-band for metrics, alignment coverage deciding whether step verdicts are trustworthy; findings are deterministic and ordered. CLI `catacomb regress` resolves `--baseline`/`--candidate` selectors (`label:k=v,...` or `name:<baseline>`), loads groups via `loadRunGroup`, aggregates, compares, renders (human table / `--json`), exits 0 pass / 1 regression / 2 operational error. `catacomb baseline set|list|rm` persists named baselines in a new `baselines` store table (schema v2 via the ADR-0017 migration runner).

**Tech Stack:** Go 1.26, stdlib only (math for sqrt), testify, table-driven tests.

## Global Constraints

- No comments in Go code (only directives); enforced by `internal/codepolicy`.
- 100% test coverage (`make cover`); TDD — failing test first, RED evidence in reports.
- `gofumpt`/`goimports` (local prefix `github.com/realkarych/catacomb`); `make lint` exit 0 (run `golangci-lint cache clean` first if stale sibling-worktree issues appear).
- **Determinism:** `regress.Compare` is pure; findings sorted (scope order total→phase→step, then key, then metric name); no map-iteration leaks; no wall clock inside Compare (baseline `CreatedAt` uses an injected `now func() time.Time` at the CLI layer).
- Exit codes: 0 = pass (incl. insufficient unless `--strict`), 1 = regression (or insufficient under `--strict`), 2 = operational error (bad selector, store open failure, empty group resolution).
- Only REGRESSIONS affect the exit code; improvements and notable-but-insignificant changes are informational.
- Guard every stat: minimum support (default k≥3 per side, flag-configurable) and `MetricStats.N == 0` rows (all-open phases per ADR-0022 Amendments) are skipped with an explicit `insufficient` finding, never guessed.

---

### Task 1: `regress` math core — Wilson intervals, bands, row comparison

**Files:**

- Create: `regress/wilson.go`, `regress/compare.go` (types + row-level logic)
- Create: `regress/wilson_test.go`, `regress/compare_test.go`

**Interfaces (exact):**

```go
package regress

type Thresholds struct {
	PresenceDelta  float64
	ErrorRateDelta float64
	MetricRelDelta float64
	IQRFactor      float64
	MinSupport     int
	CoverageFloor  float64
}

func DefaultThresholds() Thresholds
```

`DefaultThresholds()` = `{PresenceDelta: 0.2, ErrorRateDelta: 0.1, MetricRelDelta: 0.25, IQRFactor: 1.5, MinSupport: 3, CoverageFloor: 0.7}`.

```go
type Verdict string

const (
	VerdictOK           Verdict = "ok"
	VerdictRegression   Verdict = "regression"
	VerdictImprovement  Verdict = "improvement"
	VerdictNotable      Verdict = "notable"
	VerdictInsufficient Verdict = "insufficient"
)

type Finding struct {
	Scope     string  `json:"scope"`
	Key       string  `json:"key,omitempty"`
	Name      string  `json:"name,omitempty"`
	Metric    string  `json:"metric"`
	Verdict   Verdict `json:"verdict"`
	Baseline  float64 `json:"baseline"`
	Candidate float64 `json:"candidate"`
	Delta     float64 `json:"delta"`
	BandLo    float64 `json:"band_lo,omitempty"`
	BandHi    float64 `json:"band_hi,omitempty"`
	Detail    string  `json:"detail,omitempty"`
}

func wilson(successes, n int, z float64) (lo, hi float64)
```

**Binding math:**

- **Wilson score interval** (z = 1.96 fixed): p̂ = successes/n; denom = 1 + z²/n; center = (p̂ + z²/(2n)) / denom; half = z·sqrt(p̂(1−p̂)/n + z²/(4n²)) / denom; lo = max(0, center−half), hi = min(1, center+half). n == 0 → (0, 1).
- **Rate comparison** (`compareRate(scope, key, name, metric string, bSucc, bN, cSucc, cN int, delta float64, th Thresholds) Finding`): rates p_b = bSucc/bN, p_c = cSucc/cN. If bN < MinSupport or cN < MinSupport → VerdictInsufficient. Regression direction is metric-specific and passed by the caller via the sign convention: callers pass counts such that an INCREASE in the rate is bad (for presence, callers pass "absence" counts = N−Present so a presence DROP becomes a rate increase; Detail explains in plain terms). Flag VerdictRegression iff Wilson CIs are disjoint (b.hi < c.lo) AND (p_c − p_b) > delta. If CIs overlap but (p_c − p_b) > delta → VerdictNotable. If disjoint in the improving direction (c.hi < b.lo) AND (p_b − p_c) > delta → VerdictImprovement. Else VerdictOK. BandLo/BandHi carry the baseline CI.
- **Metric comparison** (`compareMetric(scope, key, name, metric string, b, c aggregate.MetricStats, th Thresholds) Finding`): if b.N < MinSupport or c.N < MinSupport → VerdictInsufficient (Detail says which side). IQR = b.P75 − b.P25; band = max(th.MetricRelDelta × |b.Median|, th.IQRFactor × IQR); BandLo/Hi = b.Median ± band. c.Median > BandHi → VerdictRegression; c.Median < BandLo → VerdictImprovement; else VerdictOK. (Lower is better for all built-in metrics: duration, cost, tokens.)

- [ ] **Step 1: Failing tests.** Wilson table with hand-computed values (e.g. wilson(3,3): lo≈0.4385, hi=1.0; wilson(0,3): lo=0, hi≈0.5615; wilson(8,10) ≈ (0.4901, 0.9433); assert to 1e-3 tolerance; n=0 → (0,1)). compareRate: 0/5 vs 5/5 error-successes → regression (CIs disjoint: (0,0.43) vs (0.57,1)); 0/3 vs 3/3 → notable (CIs overlap, delta 1.0 > threshold); insufficient when either N < 3; improvement mirror case; ok case. compareMetric: baseline {N:5, Median:1000, P25:900, P75:1100} (IQR 200, band max(250, 300)=300 → [700,1300]) with candidate median 1400 → regression, 600 → improvement, 1200 → ok; insufficient at N<3 either side; zero-IQR baseline falls back to rel band.
- [ ] **Step 2:** RED → implement → GREEN; `make lint`, codepolicy.
- [ ] **Step 3: Commit** — `feat(regress): Wilson intervals, IQR bands, row-level comparisons`

### Task 2: `regress.Compare` — full report comparison + rendering

**Files:**

- Create: `regress/regress.go` (Compare + report assembly), `regress/render.go` (human + JSON)
- Create: `regress/regress_test.go`, `regress/render_test.go`

**Interfaces (exact):**

```go
type Input struct {
	Baseline  aggregate.Report
	Candidate aggregate.Report
}

type Coverage struct {
	Steps  float64 `json:"steps"`
	Phases float64 `json:"phases"`
}

type Report struct {
	BaselineRuns   int       `json:"baseline_runs"`
	CandidateRuns  int       `json:"candidate_runs"`
	Coverage       Coverage  `json:"coverage"`
	StepsTrusted   bool      `json:"steps_trusted"`
	Findings       []Finding `json:"findings"`
	Regressions    int       `json:"regressions"`
	Insufficient   int       `json:"insufficient"`
	OverallVerdict Verdict   `json:"overall_verdict"`
}

func Compare(in Input, th Thresholds) Report
func RenderHuman(r Report, w io.Writer)
func RenderJSON(r Report, w io.Writer) error
```

**Binding assembly semantics:**

- **Alignment coverage:** `Coverage.Steps` = fraction of baseline step keys also present in candidate (1.0 when baseline has none); same for phases. `StepsTrusted` = Coverage.Steps ≥ th.CoverageFloor.
- **Run totals findings** (scope `total`, always emitted): metric comparisons for `duration_ms`, `cost_usd`, `tokens_in`, `tokens_out` (via compareMetric on `Totals`), plus rate comparison `error_rate` (successes = round(ErrorRate×Runs), n = Runs).
- **Phase findings** (scope `phase`, per union of phase keys sorted): presence (absence counts, see Task 1 convention), error-status rate (successes = round(StatusRates["error"]×Present), n = Present), and metric comparisons duration/cost/tokens. Keys missing on one side: treat missing side's row as zero-Present (presence handles it); metric comparisons are skipped as VerdictInsufficient with Detail "absent in baseline/candidate".
- **Step findings** (scope `step`): same shape as phases; when `!StepsTrusted`, every step finding that would be VerdictRegression is DOWNGRADED to VerdictNotable with Detail noting low alignment coverage (the phase/total level carries the verdict per ADR-0022 §4) — the downgrade must be tested.
- **Noise control:** VerdictOK findings for phases/steps are NOT included in `Findings` (only total-scope OK rows are kept, so the report always shows the totals); regressions/improvements/notable/insufficient always included.
- **Overall verdict:** `regression` if Regressions > 0; else `insufficient` if Insufficient > 0 AND every non-insufficient finding is OK; else `ok`. Deterministic ordering: scope (total, phase, step), then Key, then Metric.
- **Rendering:** `RenderHuman` — a compact `text/tabwriter` table (columns VERDICT/SCOPE/KEY/METRIC/BASELINE/CANDIDATE/BAND) preceded by a two-line header (runs per side, coverage, trusted flag) — golden-file tested (testdata/golden_report.txt, byte-exact). `RenderJSON` — `json.MarshalIndent` of Report — golden-file tested.

- [ ] **Step 1: Failing tests** — build two synthetic aggregate.Reports covering: totals regression (cost), phase presence drop (regression at k=5), phase absent-in-candidate metric skip, step regression downgraded by low coverage, improvement, notable, insufficient (N<3), OK-filtering, overall verdict for each of regression/insufficient/ok. Golden files for both renderers.
- [ ] **Step 2:** RED → implement → GREEN; permutationally safe (findings order fixed by sort, not input); `make lint`, codepolicy.
- [ ] **Step 3: Commit** — `feat(regress): full group comparison, coverage gating, human/JSON rendering`

### Task 3: store schema v2 — persisted baselines

**Files:**

- Modify: `store/migrate.go` (currentSchemaVersion=2, `applySchemaV2` creating `baselines`), `store/store.go` (interface + `model.Baseline` usage), `store/sqlite.go` (methods), `store/memory/` (methods)
- Create: `model/baseline.go`
- Test: extend store tests incl. a v1→v2 migration test

**Interfaces (exact):**

```go
package model

type Baseline struct {
	Name      string            `json:"name"`
	RunIDs    []string          `json:"run_ids"`
	Selector  map[string]string `json:"selector,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
}
```

Store interface additions: `UpsertBaseline(b model.Baseline) error`, `GetBaseline(name string) (model.Baseline, bool, error)`, `ListBaselines() ([]model.Baseline, error)` (sorted by Name), `DeleteBaseline(name string) error` (no error when absent). Table: `baselines(name TEXT PRIMARY KEY, body TEXT NOT NULL)` — body is the JSON of the Baseline (consistent with runs/nodes pattern).

- [ ] **Step 1: Failing tests** — round-trip upsert/get/list/delete on sqlite + memory; get absent → ok=false; list sorted; upsert overwrites. **Migration test:** create a store at schema v1 (open with the current code path pinned to v1 via a direct `PRAGMA user_version=1` fixture after creating v1 tables — mirror the existing ADR-0017 migration test pattern, find it via `grep -rn "user_version" store/*_test.go`), reopen with v2 code → baselines table usable, existing rows intact; `ErrSchemaTooNew` guard still works (v3 db refused).
- [ ] **Step 2:** RED → implement → GREEN; `make cover` (both store impls 100%).
- [ ] **Step 3: Commit** — `feat(store): schema v2 — persisted baselines table (ADR-0017 migration)`

### Task 4: CLI — `catacomb baseline` + `catacomb regress`

**Files:**

- Create: `cmd/catacomb/baseline.go`, `cmd/catacomb/regress.go` (+ tests)
- Modify: `cmd/catacomb/root.go` (wire both under the Advanced group — read how existing cmds register)

**Contract:**

- `catacomb baseline set <name> --label k=v [--label ...] [--db]` — resolves the selector NOW: `loadRunGroup` (read-only open fails → but set needs WRITE: open read-write via the non-readonly opener used by daemon — find `store.Open`/`OpenSQLite`; adapt: resolve group via the same store handle), errors (exit 2 style message) if 0 runs match; persists `model.Baseline{Name, RunIDs: <sorted run ids>, Selector, CreatedAt: nowFn()}` (package-level `var nowFn = time.Now` seam for tests). `--label` terms validated strictly via the existing `validateLabelTerms`.
- `catacomb baseline list [--db] [--json]` — table NAME/RUNS/SELECTOR/CREATED or JSON. `catacomb baseline rm <name> [--db]`.
- `catacomb regress --baseline <sel> --candidate <sel> [--db] [--json] [--strict] [--min-support N] [--presence-delta F] [--error-delta F] [--metric-rel-delta F] [--iqr-factor F] [--coverage-floor F]` — selector grammar: `label:k=v[,k=v...]` (strict-validated) or `name:<baseline>` (looked up; missing name → operational error). Load both groups via `loadRunGroup` (for `name:` resolve to the stored RunIDs and filter loaded runs by ID membership — extend the loader with `loadRunGroupByIDs(s, pricer, ids)` reusing the same assembly, do NOT duplicate the snapshot logic); empty group on either side → operational error (exit 2) with a clear message. Aggregate both (empty `aggregate.Options{}` — annotation thresholds are future work), `regress.Compare`, render human or JSON. Exit: 0/1/2 per Global Constraints; `--strict` turns overall `insufficient` into exit 1. Use `cobra` `RunE` returning nil and calling an injected exit func? — NO: follow the repo's existing pattern for exit codes (read how `cmd/catacomb/run.go`/`main.go` propagate exit codes; `run()` in run.go returns 1 on error — regress needs exit 1 distinct from 2: return a sentinel error `errRegressionDetected` mapped in `main.go`/`run()` — check main.go's structure and follow its conventions; if `run()` only knows 0/1, add explicit os.Exit-style handling via the existing pattern for `catacomb run` child exit codes — investigate `runChild` exit-code propagation and mirror it).
- Tests: cobra-level for both commands against seeded stores (reuse `seedRunsWithLabels`-style helpers): baseline set/list/rm round-trip incl. 0-match error; regress label-vs-label pass (identical groups → exit 0, overall ok); injected error-rate jump at k=5 → regression exit 1; `name:` selector resolves; missing name → exit 2; `--strict` on insufficient → exit 1; `--json` output parses into `regress.Report`.

- [ ] **Step 1: Failing tests** → **Step 2:** implement → GREEN; `make cover`, `make lint`, codepolicy.
- [ ] **Step 3: Commit** — `feat(cmd): catacomb regress + baseline set/list/rm (ADR-0022 §4)`

### Task 5: docs, final review, live-verify, PR, merge

- [ ] **Step 1:** Docs: `docs/guide/cli.md` (both commands + selector grammar + exit codes + threshold flags), `docs/guide/workflows.md` (a "Regression testing a change" walkthrough: label two groups → baseline set → regress; note k≥5 recommended for presence flips to reach significance, k=3 minimum). markdownlint clean.
- [ ] **Step 2:** Whole-branch review (most capable model) with review package from `git merge-base origin/master HEAD`; fix wave; re-verify.
- [ ] **Step 3:** Live-verify: real daemon, two labeled groups of k=3 runs (one group with an induected failing run — e.g. a stream-json line with an error tool_result), `baseline set` + `regress` both selectors, confirm human output, `--json`, and exit codes (0 on identical, 1 with induced error-rate jump under lowered thresholds if k=3 insignificance blocks — document what was observed).
- [ ] **Step 4:** `make cover && make lint && go test ./internal/codepolicy/` + markdownlint on changed docs + this plan.
- [ ] **Step 5:** Push `feat/regress`, open PR `feat: catacomb regress + persisted baselines (ADR-0022 §4)`, CI green, squash-merge (authorized).
