# SP1 Verifier Contract Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give catacomb a task-success axis: baskets declare an executable verifier per task; bench and an offline `catacomb verify` verb run it against each cell's evidence; its verdict enters the model as run-level annotations gated by the existing Wilson machinery.

**Architecture:** Five PR waves, each independently shippable: (A) run-level scores through aggregate/regress; (B) bench verify hook + artifact capture; (C) offline `verify` verb; (D) Python mini-SDK; (E) hermetic SQL E2E + live weekly basket. Semantics stay in the user's subprocess — the core executes and aggregates, never judges. Spec: `docs/specs/2026-07-12-sp1-verifier-contract-design.md`; decision: ADR-0027.

**Tech Stack:** Go stdlib only (no new Go deps), cobra CLI, existing `evidence`/`aggregate`/`regress`/`bench` packages; Python ≥3.10 stdlib-only SDK; sqlite3 + bash for E2E fixtures.

## Global Constraints

- **No comments in Go code** — none; only `//go:build`, `//go:embed`, `//go:generate` directives (enforced by `internal/codepolicy`).
- **TDD**: failing test first, minimal implementation, refactor under green. **Coverage is 100%** (`make cover`); the threshold never goes down.
- `gofumpt` + `goimports` (local prefix `github.com/realkarych/catacomb`); `make lint` green.
- Table-driven tests; `testify/require` fatal, `testify/assert` otherwise; no `time.Sleep` in tests; mock through the caller's interface.
- No new Go dependencies. Python SDK: stdlib only.
- Every agent working on this repo uses `opus` (AGENTS.md).
- One PR per wave, branch `feat/<short-desc>` from `master`, squash merge, green CI before merge. No `--no-verify`.
- Docs pass `markdownlint` (`.markdownlint.json`).
- All error sentinels checked with `errors.Is`; wrap with `fmt.Errorf("pkg.Op: %w", err)`.

## File Map

| Wave | Create | Modify |
|---|---|---|
| A | — | `cmd/catacomb/scores.go`, `cmd/catacomb/runsdir.go`, `cmd/catacomb/regress.go`, `aggregate/aggregate.go`, `regress/compare.go`, `regress/regress.go`, `regress/sensitivity.go`, `regress/render.go`, `docs/guide/cli.md`, `docs/guide/workflows.md` |
| B | `cmd/catacomb/verifycell.go`, `evidence/artifacts.go`, `evidence/verifyrecord.go` | `bench/basket.go`, `bench/manifest.go`, `evidence/evidence.go`, `cmd/catacomb/bench.go` |
| C | `cmd/catacomb/verify.go` | `cmd/catacomb/root.go`, `docs/guide/cli.md` |
| D | `integrations/verifier/pyproject.toml`, `integrations/verifier/README.md`, `integrations/verifier/src/catacomb_verifier/__init__.py`, `integrations/verifier/tests/test_cell.py`, `integrations/verifier/tests/test_emit.py`, `integrations/verifier/tests/test_compare_tables.py` | `.github/workflows/python-deepeval.yml` |
| E | `e2e/hermetic/run.sh`, `e2e/hermetic/agent.sh`, `e2e/hermetic/verify_sql.py`, `e2e/hermetic/basket.yaml.tmpl`, `e2e/hermetic/seed.sql`, `e2e/hermetic/golden.csv`, `e2e/hermetic/transcript.jsonl.tmpl`, `.github/workflows/e2e-hermetic.yml` | `e2e/run.sh` (live sql basket), `docs/guide/workflows.md` |

---

## Wave A — run-level scores through the gate (PR `feat/run-level-scores`)

### Task A1: Run-level score lines in the scores parser

**Files:**
- Modify: `cmd/catacomb/scores.go`
- Test: `cmd/catacomb/scores_test.go`

**Interfaces:**
- Consumes: existing `scoreEntry{StepKey, Key, Value, RunID string/float64}` and `parseScoreLine(line string) (scoreEntry, error)`.
- Produces: `parseScoreLine` accepts lines without `step_key` (run-level). `loadScores(path string)` errors on a run-level entry with empty `run_id` (external files must address a run). Unknown JSON fields (e.g. `tool`, `tool_version`, `prompt_hash` provenance) are tolerated — assert, don't implement.

- [ ] **Step 1: Write failing tests** in `cmd/catacomb/scores_test.go` (extend the existing table style):

```go
func TestParseScoreLineRunLevel(t *testing.T) {
	e, err := parseScoreLine(`{"key":"verifier.pass","value":1,"run_id":"r1"}`)
	require.NoError(t, err)
	assert.Equal(t, scoreEntry{Key: "verifier.pass", Value: 1, RunID: "r1"}, e)
}

func TestParseScoreLineToleratesProvenanceFields(t *testing.T) {
	e, err := parseScoreLine(`{"key":"judge.groundedness","value":0.8,"run_id":"r1","tool":"deepeval","tool_version":"3.1","prompt_hash":"abc"}`)
	require.NoError(t, err)
	assert.InDelta(t, 0.8, e.Value, 1e-9)
}

func TestLoadScoresRunLevelRequiresRunID(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	require.NoError(t, os.WriteFile(p, []byte(`{"key":"verifier.pass","value":1}`+"\n"), 0o600))
	_, err := loadScores(p)
	require.ErrorContains(t, err, `run-level score requires "run_id"`)
}
```

- [ ] **Step 2:** `go test ./cmd/catacomb/ -run TestParseScoreLineRunLevel -v` → FAIL (`missing "step_key"`).
- [ ] **Step 3: Implement.** In `parseScoreLine`, delete the `l.StepKey == ""` rejection. In `loadScores`, after parsing each entry add:

```go
if e.StepKey == "" && e.RunID == "" {
	return nil, fmt.Errorf("scores %s line %d: %w", path, i+1, errRunLevelNeedsRunID)
}
```

with `var errRunLevelNeedsRunID = errors.New(`run-level score requires "run_id"`)` at file top.
- [ ] **Step 4:** `go test ./cmd/catacomb/ -run 'TestParseScoreLine|TestLoadScores' -v` → PASS; full `make test` green.
- [ ] **Step 5:** Commit: `git commit -m "feat(scores): accept run-level score lines (no step_key)"`.

### Task A2: Run-level annotations in aggregate

**Files:**
- Modify: `aggregate/aggregate.go`
- Test: `aggregate/aggregate_test.go`

**Interfaces:**
- Produces (later tasks rely on these exact shapes):

```go
type RunGraph struct {
	Run         model.Run
	Nodes       []*model.Node
	Edges       []*model.Edge
	Annotations map[string]float64
}

type AnnotationTotals struct {
	N      int         `json:"n"`
	Ones   int         `json:"ones"`
	Binary bool        `json:"binary"`
	Stats  MetricStats `json:"stats"`
}
```

`RunTotals` gains `Annotations map[string]AnnotationTotals \`json:"annotations,omitempty"\``. One value per run per key (RunGraph.Annotations is a map — last writer wins upstream). `Binary` = every collected value is exactly 0 or 1. `Ones` counts values == 1.

- [ ] **Step 1: Write failing tests** (table-driven, alongside existing `runTotals` tests):

```go
func TestRunTotalsAnnotations(t *testing.T) {
	group := []RunGraph{
		{Run: model.Run{ID: "r1"}, Annotations: map[string]float64{"verifier.pass": 1, "verifier.row_diff": 0}},
		{Run: model.Run{ID: "r2"}, Annotations: map[string]float64{"verifier.pass": 0, "verifier.row_diff": 3}},
		{Run: model.Run{ID: "r3"}},
	}
	rep := Aggregate(group, Options{})
	pass := rep.Totals.Annotations["verifier.pass"]
	assert.Equal(t, 2, pass.N)
	assert.Equal(t, 1, pass.Ones)
	assert.True(t, pass.Binary)
	diff := rep.Totals.Annotations["verifier.row_diff"]
	assert.Equal(t, 2, diff.N)
	assert.False(t, diff.Binary)
	assert.InDelta(t, 1.5, diff.Stats.Median, 1e-9)
}

func TestRunTotalsAnnotationsEmpty(t *testing.T) {
	rep := Aggregate([]RunGraph{{Run: model.Run{ID: "r1"}}}, Options{})
	assert.Nil(t, rep.Totals.Annotations)
}
```

(`verifier.row_diff` is binary-ineligible because 3 ∉ {0,1}; note 0-only values ARE binary — add a table case `{0,0}` → `Binary: true, Ones: 0`.)
- [ ] **Step 2:** `go test ./aggregate/ -run TestRunTotalsAnnotations -v` → FAIL (unknown fields).
- [ ] **Step 3: Implement.** Add the two type changes above. In `runTotals`, collect:

```go
annVals := map[string][]float64{}
for _, rg := range group {
	for k, v := range rg.Annotations {
		annVals[k] = append(annVals[k], v)
	}
}
```

and build:

```go
func annotationTotals(vals map[string][]float64) map[string]AnnotationTotals {
	if len(vals) == 0 {
		return nil
	}
	out := make(map[string]AnnotationTotals, len(vals))
	for k, vs := range vals {
		t := AnnotationTotals{N: len(vs), Binary: true, Stats: stats(vs)}
		for _, v := range vs {
			switch v {
			case 1:
				t.Ones++
			case 0:
			default:
				t.Binary = false
			}
		}
		out[k] = t
	}
	return out
}
```

- [ ] **Step 4:** `go test ./aggregate/ -v` → PASS; `make cover` holds 100%.
- [ ] **Step 5:** Commit: `git commit -m "feat(aggregate): run-level annotations on RunGraph and RunTotals"`.

### Task A3: Apply run-level scores; auto-discover evidence scores.jsonl

**Files:**
- Modify: `cmd/catacomb/scores.go` (apply path), `cmd/catacomb/runsdir.go` (evidence auto-discovery)
- Test: `cmd/catacomb/scores_test.go`, `cmd/catacomb/runsdir_test.go`

**Interfaces:**
- Consumes: `aggregate.RunGraph.Annotations` (A2), `scoreEntry` (A1), `evidenceRunGraph(dir, m, pricer)` (existing).
- Produces:

```go
func applyEntriesToRunGraph(rg *aggregate.RunGraph, entries []scoreEntry) (applied, unmatched int)
func loadEvidenceScores(dir, runID string) ([]scoreEntry, error)
```

`loadEvidenceScores` reads `<dir>/scores.jsonl`; missing file → `(nil, nil)`; empty `run_id` in a line defaults to `runID`. `applyScores` (external `--scores` path) gains an `overridden` counter: an entry landing on an already-present run-level key or node annotation key counts as an override; `applyScoresFile` prints one summary warning `warning: scores: %d entries overrode evidence-provided values`. Run-level entries in the external path match by `Run.ID` across both groups.

- [ ] **Step 1: Write failing tests**:

```go
func TestApplyEntriesToRunGraphRunLevel(t *testing.T) {
	rg := aggregate.RunGraph{Run: model.Run{ID: "r1"}}
	applied, unmatched := applyEntriesToRunGraph(&rg, []scoreEntry{
		{Key: "verifier.pass", Value: 1, RunID: "r1"},
		{Key: "verifier.pass", Value: 0, RunID: "other"},
	})
	assert.Equal(t, 1, applied)
	assert.Equal(t, 1, unmatched)
	assert.Equal(t, map[string]float64{"verifier.pass": 1}, rg.Annotations)
}

func TestLoadEvidenceScoresFillsRunID(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "scores.jsonl"),
		[]byte(`{"key":"verifier.pass","value":1}`+"\n"), 0o600))
	entries, err := loadEvidenceScores(dir, "r9")
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "r9", entries[0].RunID)
}

func TestLoadEvidenceScoresMissingFile(t *testing.T) {
	entries, err := loadEvidenceScores(t.TempDir(), "r9")
	require.NoError(t, err)
	assert.Nil(t, entries)
}

func TestApplyScoresExternalOverridesEvidence(t *testing.T) {
	groups := [][]aggregate.RunGraph{{{Run: model.Run{ID: "r1"}, Annotations: map[string]float64{"verifier.pass": 1}}}}
	applied, unmatched, overridden := applyScores(groups, []scoreEntry{{Key: "verifier.pass", Value: 0, RunID: "r1"}})
	assert.Equal(t, 1, applied)
	assert.Equal(t, 0, unmatched)
	assert.Equal(t, 1, overridden)
	assert.InDelta(t, 0.0, groups[0][0].Annotations["verifier.pass"], 1e-9)
}
```

Plus a `runsdir_test.go` test: write a minimal evidence dir (reuse the existing fixture helper the current tests use for `evidenceRunGraph`) containing `scores.jsonl` with a run-level line, resolve via `resolveSelectorRunsDir`, and assert the resulting `RunGraph.Annotations["verifier.pass"] == 1`.
- [ ] **Step 2:** Run the four tests → FAIL (functions undefined / wrong signatures).
- [ ] **Step 3: Implement.** In `scores.go`: rewrite `applyScores` to return `(applied, unmatched, overridden int)`, iterating groups by index; step-level entries keep today's node loop but count overrides when `n.Annotations[e.Key]` already exists; run-level entries (`StepKey == ""`) match `group[i].Run.ID == e.RunID`, initialize the map, count override on existing key, set value. Add `applyEntriesToRunGraph` reusing the same logic over a single graph (evidence context: no override counting — evidence is the base layer). Add `loadEvidenceScores` (os.ReadFile; `errors.Is(err, fs.ErrNotExist)` → nil,nil; parse lines via `parseScoreLine`; fill empty RunID). In `runsdir.go`, at the end of `evidenceRunGraph` (both selector paths flow through it): load + apply evidence scores keyed by `m.RunID`, returning the error wrapped as `operational`.
- [ ] **Step 4:** `go test ./cmd/catacomb/ -v` → PASS; `make cover` 100%.
- [ ] **Step 5:** Commit: `git commit -m "feat(scores): apply run-level entries; auto-discover evidence scores.jsonl"`.

### Task A4: Gate run-level annotations in regress

**Files:**
- Modify: `regress/compare.go` (Thresholds), `regress/regress.go` (findings), `regress/sensitivity.go`
- Test: `regress/regress_test.go` (or the package's existing test files, matching their layout)

**Interfaces:**
- Consumes: `aggregate.AnnotationTotals` (A2), `compareRate`/`compareAnnotation`/`annotationAbsentFinding` (existing).
- Produces:

```go
const VerifierPassKey = "verifier.pass"
```

`Thresholds` gains `AnnotationRateDelta float64` (DefaultThresholds: `0.1`). `Sensitivity` gains `Annotation *RateSensitivity \`json:"annotation,omitempty"\``. `Compare` emits total-scope findings `metric: "ann:<key>"` for run-level annotations: implicit spec `{VerifierPassKey, HigherBetter: true}` unless the caller already specced that key; binary×binary → rate machinery (higher-better feeds failure counts, so a pass-rate drop is a rising failure rate → regression); otherwise → `compareAnnotation` on `.Stats`; one-side-absent → insufficient.

- [ ] **Step 1: Write failing tests** (table-driven; helper builds `aggregate.Report{Runs: k, Totals: aggregate.RunTotals{Annotations: ...}}` with all other metrics equal):

```go
func TestCompareRunAnnotationPassFullFlip(t *testing.T) {
	b := reportWithAnn(5, aggregate.AnnotationTotals{N: 5, Ones: 5, Binary: true, Stats: onesStats(5)})
	c := reportWithAnn(5, aggregate.AnnotationTotals{N: 5, Ones: 0, Binary: true, Stats: zerosStats(5)})
	rep := Compare(Input{Baseline: b, Candidate: c}, DefaultThresholds())
	f := findingByMetric(t, rep.Findings, "ann:verifier.pass")
	assert.Equal(t, VerdictRegression, f.Verdict)
	assert.Equal(t, VerdictRegression, rep.OverallVerdict)
}

func TestCompareRunAnnotationPassAvsA(t *testing.T) {
	b := reportWithAnn(5, aggregate.AnnotationTotals{N: 5, Ones: 5, Binary: true, Stats: onesStats(5)})
	rep := Compare(Input{Baseline: b, Candidate: b}, DefaultThresholds())
	f := findingByMetric(t, rep.Findings, "ann:verifier.pass")
	assert.Equal(t, VerdictOK, f.Verdict)
	assert.Equal(t, VerdictOK, rep.OverallVerdict)
}

func TestCompareRunAnnotationContinuousUsesBand(t *testing.T)      // non-binary → compareAnnotation path
func TestCompareRunAnnotationAbsentOneSideInsufficient(t *testing.T)
func TestCompareRunAnnotationLowerBetterSpec(t *testing.T)          // explicit spec {Key:"verifier.row_diff"} value-rate rising gates
func TestCompareRunAnnotationImprovementOnRisingPass(t *testing.T)  // 0/5 -> 5/5 = improvement
func TestSensitivityAnnotationAxisDisclosed(t *testing.T)           // k=2 runs with annotations -> Sensitivity.Annotation non-nil, Reachable=false
```

- [ ] **Step 2:** `go test ./regress/ -run TestCompareRunAnnotation -v` → FAIL.
- [ ] **Step 3: Implement.** Add `AnnotationRateDelta: 0.1` to `DefaultThresholds`. In `regress.go`:

```go
func effectiveRunAnnotationSpecs(specs []AnnotationSpec) []AnnotationSpec {
	for _, s := range specs {
		if s.Key == VerifierPassKey {
			return specs
		}
	}
	return append([]AnnotationSpec{{Key: VerifierPassKey, HigherBetter: true}}, specs...)
}

func runAnnotationFindings(b, c aggregate.Report, specs []AnnotationSpec, th Thresholds) []Finding {
	var out []Finding
	for _, spec := range effectiveRunAnnotationSpecs(specs) {
		bt, bok := b.Totals.Annotations[spec.Key]
		ct, cok := c.Totals.Annotations[spec.Key]
		switch {
		case !bok && !cok:
		case bok != cok:
			out = append(out, annotationAbsentFinding("total", "", "", spec, bok))
		case bt.Binary && ct.Binary:
			out = append(out, runAnnotationRate(spec, bt, ct, th))
		default:
			out = append(out, compareAnnotation("total", "", "", spec, bt.Stats, ct.Stats, th))
		}
	}
	return out
}

func runAnnotationRate(spec AnnotationSpec, bt, ct aggregate.AnnotationTotals, th Thresholds) Finding {
	bBad, cBad := bt.Ones, ct.Ones
	if spec.HigherBetter {
		bBad, cBad = bt.N-bt.Ones, ct.N-ct.Ones
	}
	f := compareRate("total", "", "", "ann:"+spec.Key, bBad, bt.N, cBad, ct.N, th.AnnotationRateDelta, th)
	note := fmt.Sprintf("ones %d/%d -> %d/%d", bt.Ones, bt.N, ct.Ones, ct.N)
	if f.Detail == "" {
		f.Detail = note
	} else {
		f.Detail = f.Detail + "; " + note
	}
	return f
}
```

Wire into `Compare`: `findings := totalsFindings(b, c, th)` → append `runAnnotationFindings(b, c, in.Annotations, th)...` right after. Sensitivity: change `computeSensitivity(bRuns, cRuns int, th Thresholds)` to `computeSensitivity(bRuns, cRuns int, th Thresholds, withAnnotations bool)`; when `withAnnotations`, fill `Annotation: &RateSensitivity{Reachable: rateGateReachable(bRuns, cRuns, th.AnnotationRateDelta, th), MinFullFlipRuns: minFullFlipRuns(th, th.AnnotationRateDelta)}` and include it in the all-reachable nil-return condition. `Compare` passes `withAnnotations := len(b.Totals.Annotations)+len(c.Totals.Annotations) > 0`.
- [ ] **Step 4:** `go test ./regress/ -v` → PASS. Run the calibration guard: `go test ./regress/ -run TestGatePower -v` → PASS untouched (the new axis must not shift existing boundaries).
- [ ] **Step 5:** `make cover` 100%; commit: `git commit -m "feat(regress): gate run-level annotations; verifier.pass gates by default"`.

### Task A5: CLI flag, rendering, docs

**Files:**
- Modify: `cmd/catacomb/regress.go` (flag `--annotation-rate-delta`), `regress/render.go` (human row for `ann:` totals findings renders like other totals), `docs/guide/cli.md`, `docs/guide/workflows.md`
- Test: `cmd/catacomb/regress_test.go`, `regress/render_test.go`

**Interfaces:**
- Consumes: `Thresholds.AnnotationRateDelta` (A4).
- Produces: `regress --annotation-rate-delta <float>` (default 0.1, validated > 0 like `--z` handling); human report shows run-level annotation findings in the totals block. Docs: scores schema (run-level dialect + provenance fields `tool`/`tool_version`/`prompt_hash`), `verifier.pass` default gating, override semantics of `--scores`.

- [ ] **Step 1:** Failing tests: flag plumbs into thresholds (mirror the existing `--z` test pattern in `cmd/catacomb/regress_test.go`); render test asserting an `ann:verifier.pass` finding renders with `ones a/n -> b/m` detail.
- [ ] **Step 2:** Run → FAIL.
- [ ] **Step 3:** Implement flag + validation (`errors.New("regress: --annotation-rate-delta must be > 0")` on `<= 0`), check `render.go` — if findings render generically by `Metric` no change may be needed; add the test either way.
- [ ] **Step 4:** `make test && make lint` → green.
- [ ] **Step 5:** Update the two guide docs (scores JSONL schema section: run-level line, required `run_id` in external files, provenance fields; `--scores` override warning; new flag). `npx markdownlint-cli docs/guide/cli.md docs/guide/workflows.md` → clean. Commit: `git commit -m "feat(regress): --annotation-rate-delta flag + run-level scores docs"`. Open PR `feat/run-level-scores`.

---

## Wave B — bench verify hook + artifacts (PR `feat/bench-verify-hook`)

### Task B1: `verify:` and `artifacts:` in the basket schema

**Files:**
- Modify: `bench/basket.go`
- Test: `bench/basket_test.go`

**Interfaces:**
- Produces:

```go
type Verify struct {
	Cmd     []string          `yaml:"cmd" json:"cmd"`
	Env     map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
	Timeout string            `yaml:"timeout,omitempty" json:"timeout,omitempty"`
}

func (v Verify) TimeoutDuration() (time.Duration, error)
```

`Task` gains `Artifacts []string \`yaml:"artifacts,omitempty" json:"artifacts,omitempty"\`` and `Verify *Verify \`yaml:"verify,omitempty" json:"verify,omitempty"\``. `Verify.TimeoutDuration()` returns 60s default when `Timeout == ""`. Validation errors: `ErrVerifyCmd = errors.New("bench: verify cmd is empty")`, `ErrArtifactGlob = errors.New("bench: invalid artifact glob")` (empty string or `filepath.IsLocal` false for the non-glob prefix — reject absolute/parent-escaping patterns).

- [ ] **Step 1:** Failing table-driven tests in `bench/basket_test.go`: YAML with `verify:` block parses (cmd/env/timeout round-trip); `verify: {cmd: []}` → `ErrVerifyCmd`; `verify: {cmd: [x], timeout: "nope"}` → `ErrTimeout`; `artifacts: [""]` → `ErrArtifactGlob`; `artifacts: ["../x"]` → `ErrArtifactGlob`; `TimeoutDuration()` default == `time.Minute`; basket hash changes when verify block changes (hash is over raw bytes — assert two loads differ).
- [ ] **Step 2:** Run → FAIL (yaml.KnownFields rejects unknown `verify`).
- [ ] **Step 3:** Implement types + validation inside `validateTasks` (new `validateVerify(i int, t Task) error` and `validateArtifacts(i int, t Task) error`).
- [ ] **Step 4:** `go test ./bench/ -v` → PASS; `make cover` 100%.
- [ ] **Step 5:** Commit: `git commit -m "feat(bench): verify and artifacts fields in basket schema"`.

### Task B2: Artifact capture into evidence

**Files:**
- Create: `evidence/artifacts.go`
- Modify: `evidence/evidence.go` (Meta fields)
- Test: `evidence/artifacts_test.go`

**Interfaces:**
- Produces:

```go
type ArtifactMeta struct {
	Rel    string `json:"rel"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

const (
	ArtifactsDirName    = "artifacts"
	ArtifactPerFileCap  = int64(10 << 20)
	ArtifactTotalCap    = int64(50 << 20)
)

func CaptureArtifacts(dir, workdir string, globs []string) ([]ArtifactMeta, string, error)
```

`Meta` gains `Artifacts []ArtifactMeta \`json:"artifacts,omitempty"\`` and `ArtifactsNote string \`json:"artifacts_note,omitempty"\``. Behavior: globs resolve relative to `workdir` (`filepath.Glob(filepath.Join(workdir, g))`); each match copies to `<dir>/artifacts/<rel>` where rel is the path relative to workdir; text files (valid UTF-8 in the first 8KB, no NUL byte) pass through `redactLines`, others copy raw; a file over `ArtifactPerFileCap` is skipped with a note; capture stops at `ArtifactTotalCap` with a note; the returned note aggregates skips (`""` when none). SHA256 is computed over the **written** (post-redaction) bytes. Returned `error` is only for I/O failures on files that should have been copied.

- [ ] **Step 1:** Failing tests: text artifact is redacted (write a line containing a fake secret the `redact` package rewrites — reuse whatever fixture `evidence`'s existing redaction tests use — assert the copied file differs and sha matches written bytes); binary artifact (bytes with NUL) copies byte-identical; per-file cap skip (write 1 byte over a test-shrunk cap — make caps parameters of an unexported `captureArtifacts(dir, workdir string, globs []string, perFileCap, totalCap int64)` and have the exported one pass the constants, so tests drive tiny caps); total cap stop; glob with no matches → empty meta, empty note, nil error; rel-path preservation for nested `out/sub/x.csv`.
- [ ] **Step 2:** Run → FAIL.
- [ ] **Step 3:** Implement per the interface. Text detection: read first 8KB, `utf8.Valid(chunk) && !bytes.ContainsRune(chunk, 0)`.
- [ ] **Step 4:** `go test ./evidence/ -v` → PASS; 100% cover (the I/O error path is testable with an unwritable dir via `os.Chmod` — if a path resists testing on Windows CI, restructure into a pure function instead of adding an exclusion).
- [ ] **Step 5:** Commit: `git commit -m "feat(evidence): artifact capture with redaction, caps, and hashes"`.

### Task B3: verify.json record

**Files:**
- Create: `evidence/verifyrecord.go`
- Test: `evidence/verifyrecord_test.go`

**Interfaces:**
- Produces:

```go
type VerifyRecord struct {
	Cmd        []string  `json:"cmd"`
	SHA256     string    `json:"sha256"`
	ExitCode   int       `json:"exit_code"`
	DurationMS int64     `json:"duration_ms"`
	Mode       string    `json:"mode"`
	FinishedAt time.Time `json:"finished_at"`
	Error      string    `json:"error,omitempty"`
}

func VerifyConfigSHA256(cmd []string, env map[string]string) string
func WriteVerify(dir string, r VerifyRecord) error
func ReadVerify(dir string) (VerifyRecord, bool, error)
```

File: `<dir>/verify.json`, 0o600, overwritten on each verification (idempotent). `ReadVerify` returns `(zero, false, nil)` when absent. `VerifyConfigSHA256` hashes the JSON encoding of `struct{Cmd []string; Env map[string]string}` with sorted-key marshaling (encoding/json sorts map keys — rely on it) — deterministic verifier identity.

- [ ] **Step 1:** Failing tests: write→read round-trip; absent → `(_, false, nil)`; rewrite replaces; `VerifyConfigSHA256` stable across map insertion order and differs when env differs.
- [ ] **Step 2:** Run → FAIL. **Step 3:** Implement. **Step 4:** PASS + 100%. **Step 5:** Commit `feat(evidence): verify.json record`.

### Task B4: Verifier runner wired into bench

**Files:**
- Create: `cmd/catacomb/verifycell.go`
- Modify: `cmd/catacomb/bench.go` (call site, epilogue), `bench/manifest.go` (entry fields)
- Test: `cmd/catacomb/verifycell_test.go`, `cmd/catacomb/bench_offline_test.go` (extend)

**Interfaces:**
- Consumes: `bench.Verify` (B1), `evidence.CaptureArtifacts` (B2), `evidence.WriteVerify`/`VerifyConfigSHA256` (B3), `parseScoreLine` (A1), `execCommandContext` var (existing, test seam).
- Produces:

```go
type verifySpec struct {
	EvidenceDir string
	Workdir     string
	RunID       string
	Basket      string
	Task        string
	Variant     string
	Rep         int
	AgentExit   int
	Mode        string
	ExtraEnv    []string
}

func runVerifyCell(ctx context.Context, stderr io.Writer, v bench.Verify, spec verifySpec) evidence.VerifyRecord
```

Behavior: builds env = `os.Environ()` + `spec.ExtraEnv` (variant env) + verify env + contract vars (`CATACOMB_EVIDENCE_DIR`, `CATACOMB_WORKDIR` — empty string when `spec.Mode == "offline"`, `CATACOMB_RUN_ID`, `CATACOMB_BASKET`, `CATACOMB_TASK`, `CATACOMB_VARIANT`, `CATACOMB_REP`, `CATACOMB_AGENT_EXIT_CODE`); cwd = Workdir in bench mode, EvidenceDir offline; applies `v.TimeoutDuration()` via `context.WithTimeout`; captures stdout to 1MB cap (over-cap = operational error); on exit 0 parses every non-blank stdout line via `parseScoreLine`, fills empty `RunID` with `spec.RunID`, any parse error = operational error (no scores written); on success writes `<EvidenceDir>/scores.jsonl` (re-marshaled lines) and always writes verify.json (Error set on operational failure; scores absent on failure). stderr of the child streams to `stderr`. `ManifestEntry` gains `Verified bool \`json:"verified,omitempty"\`` and `VerifyError string \`json:"verify_error,omitempty"\``.

- [ ] **Step 1:** Failing tests in `verifycell_test.go` using a stub script (the package's existing tests already exec helper scripts via `execCommandContext`; on Windows the offline tests are build-tagged — follow `bench_offline_test.go`'s existing pattern): success path writes scores.jsonl + verify.json with exit 0 and mode stamped; failing verifier (exit 3) → verify.json has ExitCode 3 + Error, no scores.jsonl; invalid stdout line → Error mentions the line number, no scores.jsonl; env contract — a verifier script that dumps env proves all `CATACOMB_*` vars present and `CATACOMB_WORKDIR` empty in offline mode; timeout (a sleeping script + 50ms verify timeout using the script clock, not test sleeps) → Error records `timed out`.
- [ ] **Step 2:** Run → FAIL. **Step 3:** Implement `verifycell.go` (~120 lines). **Step 4:** PASS + 100%.
- [ ] **Step 5: Wire into bench.** Failing test first (extend `bench_offline_test.go`): a basket whose task carries `artifacts:` + `verify:` runs a full offline bench against the fake-child fixture; assert evidence dir contains `artifacts/out/result.csv`, `scores.jsonl`, `verify.json`; manifest entry has `Verified: true`; a variant with a failing verifier records `VerifyError` and stderr carries `bench <run_id>: verify failed: ...`. Implement: in `recordOfflineEvidence` after `writeOfflineEvidence` succeeds — capture artifacts (results into meta before `evidence.Write`? No: `CaptureArtifacts` writes into the evidence dir created by `evidence.Write`, then meta must be re-stamped — instead extend `evidence.Write` with a final parameter `artifacts []ArtifactMeta`-producing closure? Keep it simple and explicit: call order is (1) `evidence.Write(dir, meta, files)` with meta lacking artifacts, (2) `CaptureArtifacts(dir, workdir, globs)`, (3) re-marshal meta with artifact list via a new `evidence.StampArtifacts(dir string, arts []ArtifactMeta, note string) error` that rewrites meta.json read-modify-write; add that function + test in this step), then `runVerifyCell` in mode `"bench"`, then set `entry.Verified`/`entry.VerifyError`. Epilogue: after the checkpoint summary print per-task verifier counts `verify[<task>]: pass <n>/<verified>` computed from parsed scores (`verifier.pass` value 1) tracked in a `verifyStats` mirroring `checkpointStats`.
- [ ] **Step 6:** `make test && make lint && make cover` → green, 100%.
- [ ] **Step 7:** Commit: `git commit -m "feat(bench): run task verifiers after cells; capture artifacts"`. Open PR `feat/bench-verify-hook`.

---

## Wave C — offline `catacomb verify` verb (PR `feat/verify-verb`)

### Task C1: The verb

**Files:**
- Create: `cmd/catacomb/verify.go`
- Modify: `cmd/catacomb/root.go` (register command)
- Test: `cmd/catacomb/verify_test.go`

**Interfaces:**
- Consumes: `bench.Load` (basket + hash), `evidence.ScanRuns`/`ReadMeta`, `runVerifyCell` (B4, mode `"offline"`).
- Produces: `catacomb verify <basket.yaml> --runs-dir <dir> [--label k=v[,k2=v2]]`. Matching: cells whose `meta.Labels["basket"] == basket.Name`, task id resolves to a basket task carrying `verify:`, and (when given) `--label` filters via `evidence.MatchLabels`. Per cell: `runVerifyCell` with `verifySpec{Mode: "offline", Workdir: "", EvidenceDir: dir, AgentExit: meta.ExitCode, ...}` and the variant env of the matching basket variant (variant id from labels; unknown variant → operational failure recorded for that cell). Warn once per differing hash: `warning: basket hash differs from recorded runs (verifiers may be newer than the evidence)`. Output one line per cell: `verify <run_id>: ok|error (<detail>)`. Exit codes: 0 all verified; 1 ≥1 operational verifier failure; 2 usage/IO (no runs matched is exit 2 with `ErrEmptyGroup`-style message).

- [ ] **Step 1:** Failing tests: happy path over two fixture evidence dirs (reuse bench offline fixtures) — scores.jsonl rewritten, verify.json mode `"offline"`, exit 0; failing verifier → exit 1 and the summary line says `error`; no matching runs → exit 2; `--label variant=baseline` filters; hash-mismatch warning fires when basket file edited after recording; task without `verify:` is skipped silently (assert it is NOT in the output).
- [ ] **Step 2:** Run → FAIL. **Step 3:** Implement (~150 lines, cobra command mirroring `bench`'s flag conventions; default `--runs-dir` = `benchDefaultDir(home, ".catacomb", "runs")`).
- [ ] **Step 4:** `make test` → PASS; 100%.
- [ ] **Step 5:** Docs: `docs/guide/cli.md` gains the verb (contract env table, exit codes, re-verify workflow with the three-command cycle from the spec §3); `markdownlint` clean. Commit `feat(cli): catacomb verify — offline re-verification over runs-dir`; open PR `feat/verify-verb`.

---

## Wave D — Python mini-SDK (PR `feat/verifier-sdk`)

### Task D1: Package scaffold, `Cell`, `emit`

**Files:**
- Create: `integrations/verifier/pyproject.toml`, `integrations/verifier/README.md`, `integrations/verifier/src/catacomb_verifier/__init__.py`, `integrations/verifier/tests/test_cell.py`, `integrations/verifier/tests/test_emit.py`

**Interfaces:**
- Produces (public API, all in `__init__.py`):

```python
@dataclass(frozen=True)
class Cell:
    evidence_dir: str
    workdir: str          # "" offline
    run_id: str
    basket: str
    task: str
    variant: str
    rep: int
    agent_exit_code: int

    @classmethod
    def from_env(cls) -> "Cell": ...   # KeyError -> SystemExit(2) with message on missing CATACOMB_EVIDENCE_DIR/RUN_ID
    def artifact(self, rel: str) -> str: ...  # evidence_dir/artifacts/rel if exists, else workdir/rel (bench mode), else FileNotFoundError

def emit(passed: bool | None = None, key: str | None = None, value: float | None = None,
         run_id: str | None = None, **provenance: str) -> None
```

`emit(passed=True)` prints `{"key":"verifier.pass","value":1}`; `emit(key="verifier.row_diff", value=3)` prints that line; provenance kwargs (`tool=`, `tool_version=`, `prompt_hash=`) pass through as JSON fields; exactly one of `passed`/`key` required (`ValueError` otherwise). Mirror the DeepEval bridge's pyproject layout (same build backend, python floor, pytest config).

- [ ] **Step 1:** Failing pytest: `from_env` round-trip from a monkeypatched env; missing env → SystemExit; `artifact` prefers `artifacts/` copy over workdir and raises when in neither; `emit` stdout JSON matches the Go schema exactly (parse with `json.loads`, assert keys); provenance passthrough; ValueError cases.
- [ ] **Step 2:** `cd integrations/verifier && python3 -m pytest -q` → FAIL.
- [ ] **Step 3:** Implement (stdlib only: `dataclasses`, `json`, `os`, `sys`).
- [ ] **Step 4:** pytest → PASS.
- [ ] **Step 5:** Commit `feat(verifier-sdk): package scaffold, Cell, emit`.

### Task D2: `compare_tables`

**Files:**
- Create: `integrations/verifier/src/catacomb_verifier/_tables.py`, `integrations/verifier/tests/test_compare_tables.py`
- Modify: `integrations/verifier/src/catacomb_verifier/__init__.py` (re-export)

**Interfaces:**
- Produces:

```python
@dataclass(frozen=True)
class CompareResult:
    equal: bool
    row_diff: int              # abs(len(got_rows) - len(want_rows))
    mismatches: list[str]      # first 10 human-readable cell/row diffs

def compare_tables(got: str, want: str, *, float_tol: float = 1e-4,
                   ordered: bool = False, strict: bool = True,
                   normalize_headers: bool = True) -> CompareResult
```

Rules (the benchmark canon, spec §4): format by extension (`.csv` via `csv` module, `.jsonl` via per-line `json.loads`); cell coercion `int → float → str` (strings stripped); header normalization lower/strip/spaces-and-dashes→underscores; `strict` → differing column sets or row counts ⇒ `equal=False`; `ordered=False` → rows sorted by their canonical string tuple before pairing; float comparison `abs(a-b) <= float_tol`; mismatches capped at 10 entries like `"row 3 col total: 100.0 != 101.5"`.

- [ ] **Step 1:** Failing pytest table: equal CSVs reordered rows → equal (unordered) / unequal (ordered=True); float within/outside tolerance; header variance `Region` vs `region` (on/off); extra column → unequal under strict with mismatch naming the column; row-count mismatch → `row_diff` set; jsonl vs csv cross-format comparison; empty files.
- [ ] **Step 2:** FAIL. **Step 3:** Implement (~120 lines stdlib). **Step 4:** PASS.
- [ ] **Step 5:** README: contract description (env table + stdout schema, copy from spec §1), quickstart (spec §4 snippet verbatim), re-verifiability rule (read from evidence only). Commit `feat(verifier-sdk): canonicalized table comparator`.

### Task D3: CI

**Files:**
- Modify: `.github/workflows/python-deepeval.yml`

- [ ] **Step 1:** Add a `verifier-sdk` job mirroring the deepeval job (same python version matrix, `working-directory: integrations/verifier`, `pip install -e . pytest && pytest -q`). Trigger paths: add `integrations/verifier/**` to the workflow's path filters if present.
- [ ] **Step 2:** Validate: `actionlint .github/workflows/python-deepeval.yml` if available, else YAML-parse check `python3 -c "import yaml,sys; yaml.safe_load(open('.github/workflows/python-deepeval.yml'))"`.
- [ ] **Step 3:** Commit `ci: run verifier-sdk pytest`; open PR `feat/verifier-sdk`.

---

## Wave E — E2E: hermetic per-PR + live weekly (PR `feat/hermetic-e2e`)

### Task E1: Fixtures

**Files:**
- Create: `e2e/hermetic/seed.sql`, `e2e/hermetic/golden.csv`, `e2e/hermetic/agent.sh`, `e2e/hermetic/verify_sql.py`, `e2e/hermetic/basket.yaml.tmpl`, `e2e/hermetic/transcript.jsonl.tmpl`

**Interfaces:**
- Consumes: SDK (D1/D2) via `PYTHONPATH`, bench `--projects-dir` transcript resolution `<projects>/<any-dir>/<session_id>.jsonl` (see `cmd/catacomb/transcripts.go:23`), stream-json peek (`session_id` on any line, `total_cost_usd` on the `result` line — `cmd/catacomb/childlocal.go`).
- Produces: a scripted agent that really solves a SQL task and is transcript-faithful.

`seed.sql`:

```sql
CREATE TABLE orders (id INTEGER PRIMARY KEY, region TEXT, status TEXT, amount REAL);
INSERT INTO orders VALUES
 (1,'east','paid',100.0),(2,'east','void',999.0),(3,'west','paid',50.5),
 (4,'west','paid',25.0),(5,'north','void',10.0),(6,'north','paid',75.25);
```

`golden.csv` (result of the correct query):

```csv
region,total
east,100.0
north,75.25
west,75.5
```

`agent.sh` (env in: `HERMETIC_DB`, `HERMETIC_PROJECTS`, `HERMETIC_TDIR` (dir of templates), `SQL_QUERY` from the variant):

```bash
#!/usr/bin/env bash
set -euo pipefail
sid="$(python3 -c 'import uuid; print(uuid.uuid4())')"
mkdir -p out "$HERMETIC_PROJECTS/hermetic"
sqlite3 -header -csv "$HERMETIC_DB" "$SQL_QUERY" > out/result.csv
sed "s/__SESSION_ID__/$sid/g" "$HERMETIC_TDIR/transcript.jsonl.tmpl" \
  > "$HERMETIC_PROJECTS/hermetic/$sid.jsonl"
printf '{"type":"system","session_id":"%s"}\n' "$sid"
printf '{"type":"result","session_id":"%s","total_cost_usd":0.0}\n' "$sid"
```

`transcript.jsonl.tmpl`: derive from an existing offline-test fixture — copy the smallest main-session transcript under `cmd/catacomb/testdata/` that the offline bench tests already run through `loadGraphOffline`, replace its session id with `__SESSION_ID__` everywhere, keep at least one assistant tool-use record so the graph is non-trivial. (Implementer: `ls cmd/catacomb/testdata/*.jsonl`, pick the fixture used by `bench_offline_test.go`.)

`verify_sql.py`:

```python
import os
from catacomb_verifier import Cell, emit, compare_tables

cell = Cell.from_env()
res = compare_tables(cell.artifact("out/result.csv"), os.environ["GOLDEN"], float_tol=1e-4, ordered=False)
emit(passed=res.equal, tool="verify_sql", tool_version="1")
emit(key="verifier.row_diff", value=float(res.row_diff))
```

`basket.yaml.tmpl` (driver substitutes `__WORK__`):

```yaml
basket: hermetic-sql
reps: 5
tasks:
  - id: sql
    cmd: ["./agent.sh"]
    dir: __WORK__/cellwork
    timeout: 60s
    artifacts: ["out/result.csv"]
    verify:
      cmd: ["python3", "__WORK__/verify_sql.py"]
      env: { GOLDEN: "__WORK__/golden.csv" }
      timeout: 30s
variants:
  - id: baseline
    env: { SQL_QUERY: "SELECT region, SUM(amount) AS total FROM orders WHERE status='paid' GROUP BY region ORDER BY region;" }
  - id: degraded
    env: { SQL_QUERY: "SELECT region, SUM(amount) AS total FROM orders GROUP BY region ORDER BY region;" }
  - id: baseline2
    env: { SQL_QUERY: "SELECT region, SUM(amount) AS total FROM orders WHERE status='paid' GROUP BY region ORDER BY region;" }
```

(Note goldens/verify script live under `__WORK__`, NOT under the cell workdir — the anti-gaming layout the spec documents.)

- [ ] **Step 1:** Create all six files; `shellcheck e2e/hermetic/agent.sh` clean; `sqlite3 :memory: < e2e/hermetic/seed.sql` runs; verify golden by hand: `sqlite3 -header -csv db "$BASELINE_QUERY"` equals `golden.csv`.
- [ ] **Step 2:** Commit `test(e2e): hermetic SQL fixtures`.

### Task E2: Hermetic driver

**Files:**
- Create: `e2e/hermetic/run.sh`

**Interfaces:**
- Consumes: built binary via `CATACOMB_BIN` (same contract as `e2e/run.sh`); fixtures (E1); SDK via `PYTHONPATH=$repo/integrations/verifier/src`.
- Produces: self-asserting driver, exit 0 = all assertions pass. Reuse `e2e/run.sh`'s helper conventions (`pass`/`failrec`/`run_expect`/`run_json`, failure array, summary block) — copy those helpers, keep the file standalone.

Assertion sequence (each a `run_expect`/`run_json` + explicit checks):

1. Setup: mktemp work; stage fixtures; `sed "s|__WORK__|$work|g" basket.yaml.tmpl > $work/basket.yaml`; seed db `sqlite3 "$work/e2e.db" < seed.sql`; export `HERMETIC_DB`, `HERMETIC_PROJECTS="$work/projects"`, `HERMETIC_TDIR`, `PYTHONPATH`.
2. `catacomb bench $work/basket.yaml --projects-dir $work/projects --runs-dir $work/runs --manifest $work/m.jsonl` → exit 0.
3. Evidence shape: for one baseline cell dir — `artifacts/out/result.csv` exists; `scores.jsonl` exists and contains `"verifier.pass"`; `verify.json` has `"mode":"bench"`; meta.json `artifacts` array non-empty with sha256.
4. Idempotent re-verify: snapshot all `scores.jsonl`, run `catacomb verify $work/basket.yaml --runs-dir $work/runs` → exit 0; diff snapshots byte-identical; `verify.json` now `"mode":"offline"`.
5. Gate, degraded: `catacomb regress --runs-dir $work/runs --baseline label:basket=hermetic-sql,variant=baseline --candidate label:basket=hermetic-sql,variant=degraded --json` → **exit 1**; JSON has a finding `metric == "ann:verifier.pass"` with `verdict == "regression"` (python3 json assert like `e2e/run.sh` does).
6. Gate, A-vs-A: baseline vs baseline2 → **exit 0**, zero regressions.
7. Operational failure visibility: run `catacomb verify` with a basket template variant whose verify cmd is `["false"]` (driver writes a second basket `basket-broken.yaml` pointing verify at `/usr/bin/false` — hash-mismatch warning also asserted here on stderr) → **exit 1**; `verify.json` `error` non-empty; stderr mentions the run id.
8. `--scores` override: craft one-line external scores file flipping a baseline cell's `verifier.pass` to 0, re-run regress A-vs-A with `--scores` → stderr contains `overrode`; exit stays 0 (one flip of five at defaults must not gate — k=5 partial floors, PV-6a).

- [ ] **Step 1:** Write the driver; `shellcheck` clean.
- [ ] **Step 2:** Local full loop: `make build && CATACOMB_BIN=bin/catacomb e2e/hermetic/run.sh` → `HERMETIC E2E: PASS` with 0 failures. Iterate until green — this validates waves A–D end-to-end for real.
- [ ] **Step 3:** Commit `test(e2e): hermetic verifier-contract driver`.

### Task E3: Per-PR workflow

**Files:**
- Create: `.github/workflows/e2e-hermetic.yml`

```yaml
name: e2e-hermetic
on:
  pull_request:
  push:
    branches: [master]
permissions:
  contents: read
jobs:
  hermetic:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v6
        with: { go-version-file: go.mod }
      - run: make build
      - run: CATACOMB_BIN=bin/catacomb e2e/hermetic/run.sh
```

(Match `actions/checkout`/`setup-go` versions to `ci.yml` — read it first and pin identically. ubuntu-latest ships sqlite3 and python3; the driver fails fast with a clear message if either is missing — add those two `command -v` guards like `e2e/run.sh` does.)

- [ ] **Step 1:** Create workflow; YAML-parse check; align action versions with `ci.yml`.
- [ ] **Step 2:** Commit `ci: hermetic verifier-contract E2E on every PR`; push; confirm the job runs green on the PR itself (this PR is its own first consumer).

### Task E4: Live weekly SQL basket

**Files:**
- Modify: `e2e/run.sh` (basket 3 + assertions), `docs/guide/workflows.md` (verifier-contract section)
- Create: `e2e/sql-live.sh`, `e2e/basket-sql.yaml`, `e2e/sql-seed.sql`, `e2e/sql-golden.csv` (reuse E1's seed/golden content)

**Interfaces:**
- Consumes: live-gate conventions from `e2e/run.sh` (auth env, `run_expect`, widened A-vs-A metric band, `--setting-sources project --strict-mcp-config`, pinned CLI, `CHILD_MODEL` mechanism — read the existing baskets first and mirror them exactly).
- Produces: third live basket `e2e-sql`, 3 variants × 5 reps, wrapper:

```bash
#!/bin/bash
exec claude -p "Here is a sqlite database at ${SQL_DB}. ${SQL_INSTRUCTION} Save the result as CSV with a header row to out/result.csv using the sqlite3 CLI (-header -csv). Do nothing else." \
  --model "${CHILD_MODEL:-claude-haiku-4-5}" --output-format stream-json --verbose \
  --setting-sources project --strict-mcp-config \
  --allowedTools "Bash(sqlite3:*)"
```

Variants: `baseline`/`baseline2` — `SQL_INSTRUCTION`: "Compute the total paid amount (status='paid') per region, columns region,total, ordered by region."; `degraded` — same sentence with "Include all orders regardless of status" (wrong result by construction). Task: `artifacts: ["out/result.csv"]`, `verify: {cmd: ["python3", "./verify_sql.py"], env: {GOLDEN: ./sql-golden.csv}}` (reuse `e2e/hermetic/verify_sql.py` via relative path or a copy — prefer referencing one file from both drivers). Assertions appended to `e2e/run.sh`: degraded → regress exit 1 with `ann:verifier.pass` regression; A-vs-A → exit 0. Cost note in the header comment: ~+$0.5.

- [ ] **Step 1:** Add files + assertions; `shellcheck` clean.
- [ ] **Step 2:** Live validation (requires user auth env; ~$0.5): run the extended `e2e/run.sh` locally once; iterate prompts until the degraded variant reliably produces wrong result-sets and baseline reliably correct (the PV-6b lesson: one allowed tool, verbose imperative instruction). Record spend + observations in the PR description.
- [ ] **Step 3:** Docs: `docs/guide/workflows.md` gains a "Verifying task outcomes" section: basket snippet, contract env table, re-verify cycle, anti-gaming layout note (goldens outside workdir).
- [ ] **Step 4:** Commit `test(e2e): live SQL basket exercises the verifier contract weekly`; `markdownlint` clean; open PR `feat/hermetic-e2e`; verify ALL checks' conclusions == SUCCESS before merge (per repo policy — not `--watch` exit).

---

## Acceptance (SP1 done means)

1. Hermetic E2E green on every PR: full `bench → artifacts → verify (inline+offline, idempotent) → regress` cycle through the built binary; degraded SQL gates exit 1 on `ann:verifier.pass`; A-vs-A exit 0; operational verifier failures are loud.
2. Live weekly gate exercises the same contract with real `claude -p` (~+$0.5/week).
3. `make cover` 100%, `make lint` green, codepolicy green (no comments), all five PRs squash-merged with green CI conclusions.
4. Docs updated: cli.md (scores dialect, `verify` verb, `--annotation-rate-delta`), workflows.md (verifier contract), SDK README.

## Self-review notes (done inline)

- Spec §1 contract env table ↔ B4 `verifySpec` env list — matched, including empty `CATACOMB_WORKDIR` offline.
- Spec §2 "AnnotationRateDelta default 0.1", implicit `verifier.pass` higher-better, override warning, evidence auto-discovery — Tasks A3–A5.
- Spec §3 verify.json fields/modes/exit codes — B3/C1; idempotence asserted in E2.
- Spec §4 SDK API and comparator defaults — D1/D2 signatures match the spec snippet verbatim.
- Spec §5 both tiers + negative paths + ubuntu-only rationale — E2/E3/E4.
- Spec §6 non-goals respected: no Go comparator, no judge runner, no store changes; artifacts caps + text-redaction per B2.
- Type consistency check: `scoreEntry` unchanged across A1/A3/B4; `aggregate.RunGraph.Annotations map[string]float64` used by A3/A4; `evidence.VerifyRecord` shared by B3/B4/C1; `verifySpec.Mode` values `"bench"`/`"offline"` consistent across B4/C1/E2 assertions.
