# Gate self-check (`catacomb calibrate`) — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development — one fresh implementer subagent per task, review after each task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** `catacomb calibrate` audits one variant's recorded runs offline — a time-ordered A/A split (drift detector) plus leave-one-out influence — reusing the shipped verdict path, changing no gate behavior. Decision record: [ADR-0034](../../adr/0034-gate-self-check.md).

**Architecture:** a new pure package `calibrate` importing `aggregate` + `regress` (no cycle — both are siblings; `cmd/catacomb` already composes them). The CLI resolves one run group (same selector machinery as `regress`, one side instead of two), passes it in resolved order to `calibrate.Calibrate`, and renders. Pure, deterministic, stdlib-only.

**Tech stack:** Go stdlib. No new deps.

## Global constraints

- **No comments in Go code**; TDD; 100% coverage (`make cover`); gofumpt; `make lint`; testify; sentinel errors + operational() exit-2.
- `calibrate` is pure: `Calibrate(runs []aggregate.RunGraph, th regress.Thresholds) CalibrateReport` — no I/O beyond the caller; deterministic (no time, no map iteration; the run order is the caller-provided slice order).
- The gate path is UNTOUCHED — `regress.Compare`/`aggregate.Aggregate` are reused as-is, not modified. If a change to them seems needed, STOP and report (it almost certainly isn't).
- Split semantics (binding): runs in resolved order; first half = `runs[:k/2]`, second = `runs[k/2:]` (integer division). Require BOTH halves ≥ `th.MinSupport` → else the report is `insufficient` naming the needed k (`2*MinSupport`). Leave-one-out runs only when `k-1 ≥ 2*MinSupport` (k ≥ 7 at default MinSupport 3); below that, LOO is reported as skipped with the k it needs.
- Commit after every green task; branch `feat/gate-self-check` (based on this plan-doc branch).

---

### Task 1: `calibrate` package — pure core + report

**Files:**

- Create: `calibrate/calibrate.go`, `calibrate/calibrate_test.go`

**Interfaces (produced):**

```go
package calibrate

type CalibrateReport struct {
	Runs        int              `json:"runs"`
	MinSupport  int              `json:"min_support"`
	Sufficient  bool             `json:"sufficient"`
	Detail      string           `json:"detail,omitempty"`
	Split       *SplitResult     `json:"split,omitempty"`
	Influence   *InfluenceResult `json:"influence,omitempty"`
}

type SplitResult struct {
	FirstN  int              `json:"first_n"`
	SecondN int              `json:"second_n"`
	Verdict regress.Verdict  `json:"verdict"`
	Drift   []DriftFinding   `json:"drift,omitempty"`
}

type DriftFinding struct {
	Scope   string  `json:"scope"`
	Metric  string  `json:"metric"`
	Verdict regress.Verdict `json:"verdict"`
	Baseline  float64 `json:"baseline"`
	Candidate float64 `json:"candidate"`
	Detail  string  `json:"detail,omitempty"`
}

type InfluenceResult struct {
	Evaluated  bool     `json:"evaluated"`
	Detail     string   `json:"detail,omitempty"`
	FlippingRuns []FlipFinding `json:"flipping_runs,omitempty"`
}

type FlipFinding struct {
	DroppedIndex int             `json:"dropped_index"`
	From         regress.Verdict `json:"from"`
	To           regress.Verdict `json:"to"`
}

func Calibrate(runs []aggregate.RunGraph, th regress.Thresholds) CalibrateReport
```

Logic:
- `k := len(runs)`. If `k < 2*th.MinSupport` → `Sufficient=false`, `Detail = fmt.Sprintf("self-check needs k>=%d runs (have %d)", 2*th.MinSupport, k)`, no Split/Influence.
- Else `Sufficient=true`. Split: `firstN := k/2`, first = runs[:firstN], second = runs[firstN:]. Build the A/A verdict via a small internal `compareGroups(first, second, th)` that calls `aggregate.Aggregate` on each half and `regress.Compare` (mirror cmd/catacomb/regress.go's regressReport composition — `aggregate.Options{}` with no annotation keys for the base self-check; NOTE annotations are out of scope for v1, document it). Map every `regression`/`notable` Finding in the resulting Report into a `DriftFinding` (these are the "drift" signals — same variant, so any gating verdict is drift, not a real regression). `Split.Verdict` = the Report's OverallVerdict.
- Influence: only when `k-1 >= 2*th.MinSupport`. For each index i in 0..k-1, drop runs[i], re-split the remaining k-1 in order, recompute the overall verdict; if it differs from `Split.Verdict`, append a FlipFinding{i, Split.Verdict, newVerdict}. `Evaluated=true`. Else `Evaluated=false`, `Detail = fmt.Sprintf("leave-one-out needs k>=%d runs (have %d)", 2*th.MinSupport+1, k)`.
- Determinism: iterate indices in order; never range a map. Reuse `regress` types (Verdict); do not duplicate Compare logic.

- [ ] **Step 1: failing tests** — construct fixture `[]aggregate.RunGraph` (build minimal RunGraphs via the aggregate test helpers — read aggregate/aggregate_test.go for how RunGraphs are constructed in tests; if none are easily constructible, build via the reduce/graph path used elsewhere, or a tiny helper): (a) k=4 → insufficient, needed k=6; (b) k=6 identical runs → A/A clean (Split.Verdict ok/insufficient, no drift), Influence skipped (k=6<7); (c) k=6 with a duration outlier in the second half that gates → Split has a duration DriftFinding; (d) k=7 where dropping the outlier flips the verdict → Influence.FlippingRuns names that index; (e) determinism: two Calibrate calls on the same input are deeply equal.
- [ ] **Step 2–4:** RED → implement (reusing Aggregate+Compare) → GREEN.
- [ ] **Step 5:** `make fmt && make cover && make lint`; commit `feat(calibrate): pure A/A self-check core (time-ordered split + leave-one-out)`.

### Task 2: `catacomb calibrate` CLI verb + render

**Files:**

- Create: `cmd/catacomb/calibrate.go`, `cmd/catacomb/calibrate_test.go`, `calibrate/render.go`, `calibrate/render_test.go`
- Modify: `cmd/catacomb/root.go` (register the command)

**Interfaces:**

- `calibrate.RenderHuman(r CalibrateReport, w io.Writer)` and `calibrate.RenderJSON(r CalibrateReport, w io.Writer) error` — mirror the regress renderers' style (a headline line: `self-check: <sufficient?> · runs <k> · A/A <verdict>`; drift findings as a small table or lines; influence lines listing flipping run indices; the insufficient/skip notes). Determinism as regress.
- CLI: `catacomb calibrate --runs-dir <dir> [--db <path>] --group label:...|name:... [--format human|json]` plus the threshold flags `regress` exposes that affect Compare (`--min-support`, `--metric-rel-delta`, `--iqr-factor`, `--z`, etc. — reuse the SAME threshold flag wiring `regress` uses; factor the shared flag registration if clean, else duplicate the flag definitions that matter — prefer reuse). Resolve ONE group with the same selector resolution `regress` uses for its baseline side (read cmd/catacomb/regress.go + runsdir.go for `resolveGroup`/label/name resolution; reuse it). Order: the resolved run order (document it as the recorded/run-id order — a sequence proxy). Dispatch format via a resolver like Task-1-of-the-GHA-branch's `resolveRegressFormat` (markdown NOT needed for calibrate v1 — accept human|json only; unknown → exit 2).
- Empty/unknown group → operational error (exit 2), same shape as regress's.

- [ ] **Step 1: failing tests** — CLI over fixture evidence (reuse the hermetic/regress fixture evidence-dir helpers): `calibrate --group label:...` on a 6-run variant emits the headline + A/A line; `--format json` parses to CalibrateReport; unknown group → exit 2; `--format bogus` → exit 2; a known-drifting fixture surfaces the drift finding.
- [ ] **Step 2–4:** RED → implement (verb + render + resolution reuse) → GREEN.
- [ ] **Step 5:** `make fmt && make cover && make lint`; commit `feat(cmd): catacomb calibrate — offline gate self-check verb`.

### Task 3: hermetic E2E + docs (incl. the k-vs-k workflow)

**Files:**

- Modify: `e2e/hermetic/run.sh` (or a prod scenario) — stage a variant with ≥6 fixture runs, run `calibrate --group ... --format json`, assert: sufficient true, the A/A verdict field present, and (with a seeded second-half drift) a drift finding on the drifted metric; a <6-run group → sufficient false with the needed-k detail. Hermetic, $0.
- Docs: `docs/guide/cli.md` (a `calibrate` command section: purpose, flags, exit codes, the "not a real regression — drift on your own runs" framing, and the two explicit non-goals); `docs/guide/workflows.md` (a "Self-check your gate" recipe + the **true k-vs-k A/A workflow**: bench one variant at 2k reps, `regress` first-k vs last-k, as the statistically-honest FP observation); `docs/adr/README.md` (0034 row — verify it's added, this branch adds it); `docs/VERSIONING.md` (calibrate joins the CLI surface); `README.md` (one line if the features list mentions the gate's honesty story).
- markdownlint + link-check clean; full hermetic suite green.

- [ ] Implement; commit `test(e2e)+docs: gate self-check scenario and k-vs-k workflow`.

### Task 4: final review + PR

- [ ] Final whole-branch review (most capable model; named risks: determinism of split/LOO, gate-path untouched, the two non-goals genuinely not implemented, honest insufficiency reporting); fix wave if needed; re-review.
- [ ] Optional local validation: run `calibrate` against one of the earlier live codex/haiku probe run groups if ≥6 exist, capture output. PR `feat: catacomb calibrate — offline gate self-check (ADR-0034)` — base: the ADR docs branch.

## Deliberately out of scope

- Any family-wise false-positive RATE number (ADR-0034 non-goal).
- Auto-suggesting `--metric-rel-delta` (ADR-0034 non-goal).
- Annotation/verifier axes in the self-check (v1 audits the observable metric + rate axes only; note it).
- A `regress --self-check` flag alias (deferred; separate verb ships first).
