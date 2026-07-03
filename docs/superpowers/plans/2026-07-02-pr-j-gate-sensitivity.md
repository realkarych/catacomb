# PR-J: Regression Gate Sensitivity (ADR-0023) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement ADR-0023 — one-sided Wilson rate bounds (z=1.645 default, `--z`), `--fail-on-notable` escalation, per-invocation sensitivity disclosure, and the bench epilogue nudge — so the default `catacomb regress` gate can hard-flag a full checkpoint flip at k=3 instead of exiting 0.

**Architecture:** all changes live in `regress/` (thresholds, verdict, new sensitivity computation, rendering), `cmd/catacomb/regress.go` (two flags + validation), `cmd/catacomb/bench.go` (one epilogue line), and `docs/guide/`. No schema, reducer, or aggregate changes. The `Record` JSON (regress --record) gains the two new `Thresholds` fields additively; `RecordVersion` stays 1.

**Tech Stack:** Go 1.26, testify, table-driven tests. Repo rules: NO comments in Go (codepolicy), 100% coverage, TDD, `gofumpt`+`goimports`, no `time.Sleep` in tests.

## Global Constraints

- No comments in Go code — none, not even doc comments (`internal/codepolicy` fails the build otherwise).
- 100% test coverage (`make cover`); TDD: failing test first, minimal implementation, refactor under green.
- `make lint` must pass (golangci-lint); `make fmt` before committing.
- Deterministic outputs; no wall-clock, no map-iteration-order dependence.
- Reference math (verify in tests, do not trust the plan blindly): Wilson one-sided-style interval via existing `wilson()`; at z=1.96 absence 0/3 → [0, 0.5615] vs 3/3 → [0.4385, 1] overlap (no gate at k=3, full flip first gates at k=4); at z=1.645 0/3 → [0, 0.4740] vs 3/3 → [0.5260, 1] disjoint (gates at k=3). At z=1.645: k=5 hard-flags presence 5/5→1/5; k=10 hard-flags 10/10→5/10; k=4 still only the full flip.

---

### Task 1: Thresholds gain Z and FailOnNotable; rate comparisons go one-sided by default

**Files:**

- Modify: `regress/compare.go` (Thresholds struct, DefaultThresholds, compareRate)
- Modify: `regress/wilson.go` (delete `const wilsonZ = 1.96`)
- Test: `regress/compare_test.go`

**Interfaces:**

- Produces: `Thresholds.Z float64` (default 1.645) and `Thresholds.FailOnNotable bool` (default false); `compareRate` uses `th.Z`. Task 2, 3, 5 rely on these exact field names.

- [ ] **Step 1: Write failing tests**

Add to `regress/compare_test.go` (adapt assertion helpers to the file's existing style):

```go
func TestDefaultThresholdsZ(t *testing.T) {
	th := regress.DefaultThresholds()
	require.InDelta(t, 1.645, th.Z, 1e-9)
	require.False(t, th.FailOnNotable)
}

func TestCompareRateFullFlipGatesAtK3WithDefaultZ(t *testing.T) {
	th := regress.DefaultThresholds()
	f := regress.CompareRateForTest("phase", "k", "n", "presence", 0, 3, 3, 3, th.PresenceDelta, th)
	require.Equal(t, regress.VerdictRegression, f.Verdict)
}

func TestCompareRateFullFlipNotableAtK3WithZ196(t *testing.T) {
	th := regress.DefaultThresholds()
	th.Z = 1.96
	f := regress.CompareRateForTest("phase", "k", "n", "presence", 0, 3, 3, 3, th.PresenceDelta, th)
	require.Equal(t, regress.VerdictNotable, f.Verdict)
}
```

`compareRate` is unexported. Check `regress/` for an existing `export_test.go` or internal-test pattern first; if tests in the package are internal (`package regress`), call `compareRate` directly and drop the `regress.` prefix and the `ForTest` shim. Follow whichever the package already does — do NOT invent a new export shim if internal tests are the convention.

- [ ] **Step 2: Run tests, verify they fail**

Run: `go test ./regress/ -run 'TestDefaultThresholdsZ|TestCompareRateFullFlip' -v`
Expected: FAIL (`Z` undefined / verdicts wrong).

- [ ] **Step 3: Implement**

In `regress/compare.go`:

```go
type Thresholds struct {
	PresenceDelta  float64
	ErrorRateDelta float64
	MetricRelDelta float64
	IQRFactor      float64
	MinSupport     int
	CoverageFloor  float64
	Z              float64
	FailOnNotable  bool
}

func DefaultThresholds() Thresholds {
	return Thresholds{
		PresenceDelta:  0.2,
		ErrorRateDelta: 0.1,
		MetricRelDelta: 0.25,
		IQRFactor:      1.5,
		MinSupport:     3,
		CoverageFloor:  0.7,
		Z:              1.645,
	}
}
```

In `compareRate`, replace the two `wilson(..., wilsonZ)` calls with `wilson(..., th.Z)`. Delete `const wilsonZ = 1.96` from `regress/wilson.go`.

- [ ] **Step 4: Fix pre-existing tests that encoded z=1.96 behavior**

Run: `go test ./regress/ ./cmd/... 2>&1 | head -40`. Any table rows asserting `notable` for separations that now gate at z=1.645 must be re-derived (recompute the interval by hand, don't just flip the expectation blindly — the new expectations must match the reference math above). Keep at least one test pinning z=1.96-under-flag behavior (Step 1's third test does this).

- [ ] **Step 5: Full package green + commit**

Run: `go test -race ./regress/ ./cmd/...`
Expected: PASS.

```bash
git add regress/ cmd/
git commit -m "feat(regress): one-sided rate gate z=1.645 default, Thresholds.Z + FailOnNotable (ADR-0023)"
```

---

### Task 2: FailOnNotable escalates the overall verdict; Report counts notables

**Files:**

- Modify: `regress/regress.go` (Report struct, Compare, overallVerdict)
- Test: `regress/regress_test.go`

**Interfaces:**

- Consumes: `Thresholds.FailOnNotable` (Task 1).
- Produces: `Report.Notables int` (json `"notables"`); `overallVerdict(fs, regressions, notables, insufficient, failOnNotable)`.

- [ ] **Step 1: Write failing tests**

```go
func TestFailOnNotableEscalatesOverallVerdict(t *testing.T) {
	th := DefaultThresholds()
	th.Z = 1.96
	in := inputWithFullPresenceFlipK3(t)
	rep := Compare(in, th)
	require.Equal(t, VerdictOK, rep.OverallVerdict)
	require.Positive(t, rep.Notables)

	th.FailOnNotable = true
	rep = Compare(in, th)
	require.Equal(t, VerdictRegression, rep.OverallVerdict)
}
```

Build `inputWithFullPresenceFlipK3` from the package's existing test fixtures (there are aggregate.Report builders in regress_test.go — reuse them): baseline 3 runs all containing phase X, candidate 3 runs none containing it, z forced to 1.96 so the finding is `notable` not `regression`.

- [ ] **Step 2: Run, verify fail** — `go test ./regress/ -run TestFailOnNotable -v` → FAIL.

- [ ] **Step 3: Implement**

`Report` gains `Notables int` with json tag `"notables"` next to `Regressions`. In `Compare`:

```go
	rep.Regressions = countVerdict(findings, VerdictRegression)
	rep.Notables = countVerdict(findings, VerdictNotable)
	rep.Insufficient = countVerdict(findings, VerdictInsufficient)
	rep.OverallVerdict = overallVerdict(findings, rep.Regressions, rep.Notables, rep.Insufficient, th.FailOnNotable)
```

```go
func overallVerdict(fs []Finding, regressions, notables, insufficient int, failOnNotable bool) Verdict {
	switch {
	case regressions > 0:
		return VerdictRegression
	case failOnNotable && notables > 0:
		return VerdictRegression
	case insufficient > 0 && allNonInsufficientOK(fs):
		return VerdictInsufficient
	default:
		return VerdictOK
	}
}
```

- [ ] **Step 4: Green + commit** — `go test -race ./regress/...` → PASS; commit `feat(regress): --fail-on-notable escalation path + notables count`.

---

### Task 3: Sensitivity computation attached to the Report

**Files:**

- Create: `regress/sensitivity.go`
- Test: `regress/sensitivity_test.go`
- Modify: `regress/regress.go` (Report field + attach in Compare)

**Interfaces:**

- Consumes: `compareRate`, `Thresholds` (Task 1).
- Produces: `Report.Sensitivity *Sensitivity` (json `"sensitivity,omitempty"`); types below. Task 4 renders them.

- [ ] **Step 1: Write failing tests**

```go
func TestMinFullFlipRuns(t *testing.T) {
	th := DefaultThresholds()
	require.Equal(t, 3, minFullFlipRuns(th, th.PresenceDelta))
	th.Z = 1.96
	require.Equal(t, 4, minFullFlipRuns(th, th.PresenceDelta))
	th.PresenceDelta = 1.0
	require.Equal(t, 0, minFullFlipRuns(th, th.PresenceDelta))
}

func TestSensitivityOmittedWhenReachable(t *testing.T) {
	rep := Compare(inputWithFullPresenceFlipK3(t), DefaultThresholds())
	require.Nil(t, rep.Sensitivity)
}

func TestSensitivityPresentWhenUnreachable(t *testing.T) {
	th := DefaultThresholds()
	th.Z = 1.96
	rep := Compare(inputWithFullPresenceFlipK3(t), th)
	require.NotNil(t, rep.Sensitivity)
	require.False(t, rep.Sensitivity.Presence.Reachable)
	require.Equal(t, 4, rep.Sensitivity.Presence.MinFullFlipRuns)
	require.False(t, rep.Sensitivity.ErrorRate.Reachable)
}
```

- [ ] **Step 2: Run, verify fail.**

- [ ] **Step 3: Implement `regress/sensitivity.go`**

```go
package regress

const minFullFlipSearchCap = 1000

type RateSensitivity struct {
	Reachable       bool `json:"reachable"`
	MinFullFlipRuns int  `json:"min_full_flip_runs"`
}

type Sensitivity struct {
	Presence  RateSensitivity `json:"presence"`
	ErrorRate RateSensitivity `json:"error_rate"`
}

func rateGateReachable(bN, cN int, delta float64, th Thresholds) bool {
	return compareRate("", "", "", "", 0, bN, cN, cN, delta, th).Verdict == VerdictRegression
}

func minFullFlipRuns(th Thresholds, delta float64) int {
	for n := 1; n <= minFullFlipSearchCap; n++ {
		if rateGateReachable(n, n, delta, th) {
			return n
		}
	}
	return 0
}

func computeSensitivity(bRuns, cRuns int, th Thresholds) *Sensitivity {
	s := Sensitivity{
		Presence: RateSensitivity{
			Reachable:       rateGateReachable(bRuns, cRuns, th.PresenceDelta, th),
			MinFullFlipRuns: minFullFlipRuns(th, th.PresenceDelta),
		},
		ErrorRate: RateSensitivity{
			Reachable:       rateGateReachable(bRuns, cRuns, th.ErrorRateDelta, th),
			MinFullFlipRuns: minFullFlipRuns(th, th.ErrorRateDelta),
		},
	}
	if s.Presence.Reachable && s.ErrorRate.Reachable {
		return nil
	}
	return &s
}
```

In `Compare`, after the Coverage assignment: `rep.Sensitivity = computeSensitivity(b.Runs, c.Runs, th)`. `Report` gains `Sensitivity *Sensitivity` with json tag `"sensitivity,omitempty"`.

- [ ] **Step 4: Green + commit** — `feat(regress): per-invocation rate-gate sensitivity disclosure (ADR-0023)`.

---

### Task 4: Render the sensitivity note (human + JSON)

**Files:**

- Modify: `regress/render.go`
- Test: `regress/render_test.go`

**Interfaces:**

- Consumes: `Report.Sensitivity` (Task 3).

- [ ] **Step 1: Failing test**

```go
func TestRenderHumanSensitivityNote(t *testing.T) {
	rep := Report{BaselineRuns: 3, CandidateRuns: 3, OverallVerdict: VerdictOK,
		Sensitivity: &Sensitivity{
			Presence:  RateSensitivity{Reachable: false, MinFullFlipRuns: 4},
			ErrorRate: RateSensitivity{Reachable: false, MinFullFlipRuns: 4},
		}}
	var buf bytes.Buffer
	RenderHuman(rep, &buf)
	require.Contains(t, buf.String(), "sensitivity: rate gate cannot fire at this support (full flip needs k>=4 presence, k>=4 error_rate)")
}
```

Also add a case with `MinFullFlipRuns: 0` asserting the rendered segment `k>=never` is replaced by `unreachable` (exact string: `full flip unreachable presence`).

- [ ] **Step 2: Run, verify fail.**

- [ ] **Step 3: Implement** in `render.go`, after the coverage line in `RenderHuman`:

```go
	if r.Sensitivity != nil {
		_, _ = fmt.Fprintf(w, "sensitivity: rate gate cannot fire at this support (%s, %s)\n",
			formatSensitivity("presence", r.Sensitivity.Presence),
			formatSensitivity("error_rate", r.Sensitivity.ErrorRate))
	}
```

```go
func formatSensitivity(name string, rs RateSensitivity) string {
	if rs.MinFullFlipRuns == 0 {
		return fmt.Sprintf("full flip unreachable %s", name)
	}
	return fmt.Sprintf("full flip needs k>=%d %s", rs.MinFullFlipRuns, name)
}
```

- [ ] **Step 4: JSON shape test** — extend the existing RenderJSON test: report without sensitivity must NOT contain a `"sensitivity"` key; with it, must round-trip the numbers.

- [ ] **Step 5: Green + commit** — `feat(regress): render sensitivity note`.

---

### Task 5: CLI flags `--z` and `--fail-on-notable`

**Files:**

- Modify: `cmd/catacomb/regress.go` (bindRegressFlags, runRegress validation)
- Test: `cmd/catacomb/regress_test.go` (follow the file's existing harness for flag/exit-code tests)

**Interfaces:**

- Consumes: `Thresholds.Z`, `Thresholds.FailOnNotable` (Task 1), `Report.OverallVerdict` escalation (Task 2).

- [ ] **Step 1: Failing tests** — three behaviors, using the existing cmd test harness (in-memory or tmp SQLite fixtures already used by regress cmd tests):
  1. `--z 0` → operational error mentioning `--z must be > 0` (exit 2 path).
  2. Default run over a fixture where baseline 3/3 has a phase and candidate 3/3 lacks it → `errRegressionDetected` (exit 1 path) — this pins the ADR-0023 headline behavior end-to-end.
  3. `--z 1.96` on the same fixture → nil error (exit 0), and `--z 1.96 --fail-on-notable` → `errRegressionDetected`.

- [ ] **Step 2: Run, verify fail.**

- [ ] **Step 3: Implement.** In `bindRegressFlags` after the coverage-floor line:

```go
	cmd.Flags().Float64Var(&f.thresholds.Z, "z", def.Z, "one-sided Wilson z for rate gates (1.645 = 95% one-sided)")
	cmd.Flags().BoolVar(&f.thresholds.FailOnNotable, "fail-on-notable", def.FailOnNotable, "count notable findings toward the gate (exit 1)")
```

In `runRegress`, after the MinSupport validation:

```go
	if f.thresholds.Z <= 0 {
		return operational(fmt.Errorf("regress: --z must be > 0, got %g", f.thresholds.Z))
	}
```

- [ ] **Step 4: Green + commit** — `feat(cmd): regress --z and --fail-on-notable (ADR-0023)`.

---

### Task 6: Bench epilogue nudge at reps < 5

**Files:**

- Modify: `cmd/catacomb/bench.go` (printEpilogue)
- Test: `cmd/catacomb/bench_test.go`

- [ ] **Step 1: Failing test** — extend the existing epilogue test(s): basket with `Reps: 3` must render the indented line `note: reps=3 limits rate-gate sensitivity; prefer reps: 5 or more`; basket with `Reps: 5` must not contain `limits rate-gate sensitivity`.

- [ ] **Step 2: Run, verify fail.**

- [ ] **Step 3: Implement** at the end of `printEpilogue`:

```go
	if b.Reps < 5 {
		fmt.Fprintf(out, "  note: reps=%d limits rate-gate sensitivity; prefer reps: 5 or more\n", b.Reps)
	}
```

- [ ] **Step 4: Green + commit** — `feat(cmd): bench epilogue nudges reps>=5 for rate sensitivity`.

---

### Task 7: Docs, full gates, live verify

**Files:**

- Modify: `docs/guide/cli.md` (regress flag list: `--z`, `--fail-on-notable`; bench epilogue note)
- Modify: `docs/guide/workflows.md` (sensitivity subsection under the regress workflow)

- [ ] **Step 1: cli.md** — add the two flags to the regress section mirroring the existing flag-table style, with one sentence each matching the flag help strings.

- [ ] **Step 2: workflows.md** — add subsection "Gate sensitivity at small k" with this table (defaults: z=1.645, presence-delta 0.2, error-delta 0.1, min-support 3) and two sentences: the gate reports when it cannot fire; `--fail-on-notable` trades precision for recall.

```markdown
| Runs per side (k) | Smallest presence drop that can hard-flag |
| --- | --- |
| 3–4 | 100% → 0% (full flip only) |
| 5 | 100% → 20% |
| 10 | 100% → 50% |
```

- [ ] **Step 3: Full gates**

Run: `make fmt && make lint && make cover && npx -y markdownlint-cli@0.49.0 'docs/**/*.md'`
Expected: all pass, coverage 100%.

- [ ] **Step 4: Live verify** — build `make build`, then against `regress/testdata` / cmd fixtures replayed into a temp DB: run `bin/catacomb regress` with a 3-vs-3 full-flip group and confirm exit 1 + the table shows `regression`; rerun with `--z 1.96` and confirm exit 0 + sensitivity line printed. Capture both outputs in the PR description.

- [ ] **Step 5: Commit docs** — `docs(guide): regress --z/--fail-on-notable flags + sensitivity table`.
