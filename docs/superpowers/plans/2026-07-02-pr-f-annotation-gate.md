# PR-F: Annotation Gates in `catacomb regress` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** External numeric scores (ADR-0016 annotations, e.g. DeepEval verdicts) participate in the `regress` gate: `--annotation owner.key[:direction]` compares step-row annotation stats between groups with direction-aware bands.

**Architecture:** `regress.Input` gains `Annotations []AnnotationSpec`; `Compare` emits step-scope findings `Metric: "ann:<key>"` via the existing `compareMetric` with an inversion wrapper for higher-is-better keys; the CLI parses repeatable `--annotation` flags, feeds the keys into `aggregate.Options.AnnotationKeys` for BOTH groups, and threads specs into Compare. No aggregate changes (it already folds allowlisted keys); no schema changes.

**Tech Stack:** Go 1.26, stdlib, testify.

## Global Constraints

- No comments in Go code; 100% coverage; TDD RED first; `make lint` 0 (cache clean if stale); codepolicy; gofumpt; testify; no time.Sleep; deterministic ordering (annotation findings sort within the existing scope→key→metric comparator — `ann:` keys are just metric names).
- Direction default: **higher-better** (score semantics — a candidate median BELOW the baseline band flags regression); explicit `lower-better` inverts back to built-in-metric semantics.
- Standard gating applies unchanged: MinSupport on `MetricStats.N` (annotation N = contributing runs, may be < Present), step-downgrade under low alignment coverage, VerdictInsufficient short-circuits.
- Annotations are step-scope only (ADR-0022 Amendments); no phase/total annotation findings.

---

### Task 1: `regress` core — direction-aware annotation comparison

**Files:**

- Modify: `regress/regress.go` (Input, step-row assembly), `regress/compare.go` (inversion helper)
- Test: `regress/regress_test.go`, `regress/compare_test.go`

**Interfaces (exact):**

```go
type AnnotationSpec struct {
	Key          string `json:"key"`
	HigherBetter bool   `json:"higher_better"`
}

type Input struct {
	Baseline    aggregate.Report
	Candidate   aggregate.Report
	Annotations []AnnotationSpec
}
```

**Binding semantics:**

- New unexported `compareAnnotation(scope, key, name string, spec AnnotationSpec, b, c aggregate.MetricStats, th Thresholds) Finding`: for `HigherBetter == false` delegate straight to `compareMetric` with metric name `"ann:" + spec.Key`; for `HigherBetter == true` call `compareMetric` and then INVERT the verdict mapping (candidate median < BandLo → VerdictRegression; > BandHi → VerdictImprovement; bands themselves unchanged — they describe the baseline distribution either way). Notable/OK/Insufficient pass through untouched.
- Step-row assembly: after the existing per-key metric findings, for each `Input.Annotations` spec (in input order — the final sort normalizes output order): look up `Row.Annotations[spec.Key]` on both sides. Both present → compareAnnotation. Present on ONE side only → VerdictInsufficient finding with Detail `"annotation absent in baseline"`/`"...candidate"` (mirrors the absent-key metrics rule). Absent on both sides → no finding (the key simply didn't fire at this step). Specs with duplicate keys: last wins (validate at CLI layer instead — see Task 2 — so core stays permissive).
- Step-downgrade rule applies to annotation regressions exactly as to metric regressions (same code path — verify by test).

- [ ] **Step 1: Failing tests.** compareAnnotation table: higher-better regression (score drops below band), higher-better improvement (score above band), higher-better OK, lower-better delegates unchanged, insufficient passthrough (N < MinSupport keeps zero bands + insufficient). Compare-level: two synthetic Reports whose step rows carry `Annotations["eval.score"]` — regression flagged at trusted coverage; downgraded to notable under low coverage; absent-one-side → insufficient; absent-both → no finding; no `--annotation` specs → zero annotation findings (existing golden files unchanged).
- [ ] **Step 2:** RED → implement → GREEN; `go test ./regress/`, `make lint`, codepolicy.
- [ ] **Step 3: Commit** — `feat(regress): direction-aware annotation comparisons (ann:<key> step findings)`

### Task 2: CLI wiring + docs

**Files:**

- Modify: `cmd/catacomb/regress.go` (flag, parsing, aggregation options, Compare input)
- Modify: `docs/guide/cli.md`, `docs/guide/workflows.md`
- Test: `cmd/catacomb/regress_test.go`

**Binding contract:**

- Repeatable `--annotation <owner.key>[:higher-better|lower-better]` (StringArray). Parse: split on the LAST `:`; missing suffix → higher-better; unknown suffix or empty key → `operational(...)` error (exit 2) naming the flag value. Duplicate keys across flags → operational error (`duplicate --annotation key %q`). Key syntax: non-empty, contains at least one `.` separating owner and key (mirror the flat `owner.key` convention; no charset restriction beyond non-empty segments — annotations are namespaced by writers, not by catacomb).
- Wiring: parsed keys → `aggregate.Options{AnnotationKeys: keys}` for BOTH group aggregations; specs → `regress.Input.Annotations`. No flags → empty Options (current behavior byte-identical).
- Docs: cli.md regress entry gains the flag row + a short "Gating on external scores" paragraph (direction semantics, N-vs-Present note from ADR-0022 Amendments); workflows.md regression walkthrough gains a step: run DeepEval via `integrations/deepeval`, write scores back as annotations (POST /v1/.../annotations with `--allow-annotations`, referencing the existing annotations docs), then `regress --annotation deepeval.tool_correctness`.
- Tests (cobra-level over seeded stores with annotations — reuse the loader-test seeding that attaches annotations via store `UpsertAnnotation`): flag parse matrix (default direction, explicit both, bad suffix → exit 2, duplicate → exit 2); end-to-end: two groups whose annotation medians differ beyond the band → regression exit 1 with `ann:` finding in `--json`; no-flag run unchanged (golden-compatible).

- [ ] **Step 1: Failing tests** → **Step 2:** implement → GREEN; `go test ./cmd/... ./regress/`, `make cover`, `make lint`, codepolicy, markdownlint (both docs).
- [ ] **Step 3: Commit** — `feat(cmd): regress --annotation gates on external scores; docs`

### Task 3: final review, live-verify, PR, merge

- [ ] Whole-branch review (most capable model) from `git merge-base origin/master HEAD`; fix wave; re-verify.
- [ ] Live-verify: real daemon with `--allow-annotations`; two labeled groups; POST numeric annotations (curl) onto steps of both groups (higher on baseline); `regress --annotation <key>` flags the drop (exit 1) and `--json` carries the `ann:` finding; direction inversion checked with `:lower-better`.
- [ ] `make cover && make lint && codepolicy` + markdownlint (docs + both plan files).
- [ ] Push `feat/annotation-gate`, open PR `feat: regress annotation gates (P1, external-score gating)`, CI green, squash-merge (authorized).
