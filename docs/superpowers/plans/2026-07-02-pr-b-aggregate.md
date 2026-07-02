# PR-B: Run-Group Aggregation Package Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A pure `aggregate/` package that folds a group of runs (same task, k repetitions) into per-`step_key` / per-`phase_key` / run-level statistics — the input `catacomb regress` (PR-C) will compare (ADR-0022 §2).

**Architecture:** input is `[]RunGraph` (one per run: `model.Run` + its nodes + edges, loaded from the store); output is a deterministic `Report` (lexicographic key order, permutation-invariant). Steps aggregate per-run sums keyed by `Node.StepKey`; phases aggregate marker nodes keyed by `Node.PhaseKey` with member roll-ups via `marker_span` edges; numeric annotations (flat `owner.key` keys holding `json.RawMessage`) join as opt-in metrics. Quantiles are nearest-rank (no interpolation). A batch loader in `cmd/catacomb` selects run groups by label selector (PR-A `model.MatchLabels`) and reattaches annotations via a newly exported `reduce.(*Graph).ApplyAnnotations`.

**Tech Stack:** Go 1.26, stdlib only (no new deps), testify, table-driven tests.

## Global Constraints

- No comments in Go code (only `//go:build`/`//go:embed`/`//go:generate`); enforced by `internal/codepolicy`.
- 100% test coverage (`make cover`); TDD — failing test first, RED evidence in reports.
- `gofumpt` + `goimports` (local prefix `github.com/realkarych/catacomb`); `make lint` exit 0.
- **Determinism:** `Aggregate` is a pure function; permuting the input slice yields a byte-identical marshaled Report; no wall clock, no map-iteration order in any output slice.
- Errors: sentinels + `fmt.Errorf("pkg.Op: %w", err)`. No `time.Sleep` in tests.
- Superseded/abandoned nodes are OFF the active branch: exclude nodes with `Status` `superseded` or `abandoned` from ALL aggregation.
- k=1 groups must aggregate without division-by-zero; empty groups return an empty Report (Runs=0), not an error.

---

### Task 1: `aggregate` core — types, quantiles, step rows, run totals

**Files:**

- Create: `aggregate/aggregate.go` (types + `Aggregate`)
- Create: `aggregate/quantile.go`
- Create: `aggregate/aggregate_test.go`, `aggregate/quantile_test.go`

**Interfaces:**

- Produces (exact):

```go
package aggregate

type RunGraph struct {
	Run   model.Run
	Nodes []*model.Node
	Edges []*model.Edge
}

type Options struct {
	AnnotationKeys []string
}

type MetricStats struct {
	N      int     `json:"n"`
	Median float64 `json:"median"`
	P90    float64 `json:"p90"`
}

type Row struct {
	Key          string                       `json:"key"`
	Name         string                       `json:"name,omitempty"`
	Present      int                          `json:"present"`
	PresenceRate float64                      `json:"presence_rate"`
	StatusRates  map[model.Status]float64     `json:"status_rates"`
	Occurrences  MetricStats                  `json:"occurrences"`
	DurationMS   MetricStats                  `json:"duration_ms"`
	CostUSD      MetricStats                  `json:"cost_usd"`
	TokensIn     MetricStats                  `json:"tokens_in"`
	TokensOut    MetricStats                  `json:"tokens_out"`
	Annotations  map[string]MetricStats       `json:"annotations,omitempty"`
}

type RunTotals struct {
	DurationMS MetricStats `json:"duration_ms"`
	CostUSD    MetricStats `json:"cost_usd"`
	TokensIn   MetricStats `json:"tokens_in"`
	TokensOut  MetricStats `json:"tokens_out"`
	Nodes      MetricStats `json:"nodes"`
	ErrorRate  float64     `json:"error_rate"`
}

type Report struct {
	Runs   int       `json:"runs"`
	Steps  []Row     `json:"steps"`
	Phases []Row     `json:"phases"`
	Totals RunTotals `json:"totals"`
}

func Aggregate(group []RunGraph, opts Options) Report
```

- `quantile.go`: `func nearestRank(sorted []float64, q float64) float64` — nearest-rank method: for n values, rank = ceil(q*n) clamped to [1,n]; returns `sorted[rank-1]`; empty slice → 0. `func stats(values []float64) MetricStats` — sorts a copy, returns `{N: len, Median: nearestRank(s, 0.5), P90: nearestRank(s, 0.9)}`; empty → zero MetricStats.

**Aggregation semantics (binding):**

- **Step rows** — for each run, group its included nodes (StepKey non-empty, status not superseded/abandoned) by StepKey. Per run per key compute: occurrence count; SUM of `DurationMS`, `CostUSD`, `TokensIn`, `TokensOut` (nil pointers contribute nothing); worst status across occurrences (severity order: `error` > `blocked` > `cancelled` > `unknown` > `running` > `pending` > `ok` — worst wins). Across runs: `Present` = number of runs with ≥1 occurrence; `PresenceRate = Present / Runs`; `StatusRates[s]` = fraction **of the runs where the key is present** whose worst status is s (rates over Present, not Runs — absence is already visible in PresenceRate); metric stats over the per-run sums (only runs where present contribute; `N` = contributing run count); `Occurrences` stats over per-run counts. `Name` = the node `Name` of the first occurrence in the lexicographically-first contributing run (deterministic tie-break: lowest `Run.ID`, then lowest node ID).
- **Phase rows** — same shape keyed by marker nodes' `PhaseKey` (only `Type == model.NodeMarker`, PhaseKey non-empty). Per run per key: marker duration (TEnd−TStart when both set), worst marker status, occurrence count, and member roll-up — SUM of CostUSD/TokensIn/TokensOut over nodes reachable via `marker_span` edges (`Src` = marker ID, `Dst` = member id; exclude superseded/abandoned members; members' `DurationMS` is NOT summed — the marker's own wall-clock duration is the phase duration).
- **Run totals** — per run: duration = `EndedAt−StartedAt` when both set; cost/tokens = sum over ALL included (non-superseded/abandoned) nodes; node count of included nodes. `ErrorRate` = fraction of runs whose `Run.Status` is `error` OR that contain ≥1 included node with status `error`.
- **Ordering:** `Steps` and `Phases` sorted by `Key` ascending. All computation iterates runs in input order but accumulates into per-key slices sorted before stats — the output must be permutation-invariant (property-tested).

- [ ] **Step 1: Write failing tests.** Quantile table tests (empty; single; even/odd counts; q=0.5/0.9 hand-computed, e.g. `[1,2,3,4]` median=2 by nearest-rank ceil(0.5*4)=2, p90=4). Aggregate fixture test: build 3 RunGraphs by hand (runs r1/r2/r3) with: a step key `s1` present in all (varying cost, one run with 2 occurrences, one with an `error` occurrence), a step `s2` present only in r1, a superseded node that must be ignored, a marker phase `p1` in 2 runs with marker_span members, run start/end times. Assert every field of the resulting Report against hand-computed values. Include a k=1 group case and an empty group case.
- [ ] **Step 2: Run to verify failure** — `go test ./aggregate/ -v` → FAIL (package missing).
- [ ] **Step 3: Implement** `quantile.go` then `aggregate.go` per the binding semantics.
- [ ] **Step 4: Permutation property test** — shuffle-free determinism: for the 3-run fixture, aggregate every permutation of the input slice (6 permutations, generate them explicitly — no randomness) and assert `json.Marshal` outputs are byte-identical.
- [ ] **Step 5: Run all tests, `make lint`, `go test ./internal/codepolicy/`** → green.
- [ ] **Step 6: Commit** — `feat(aggregate): run-group aggregation core (steps, phases, totals, nearest-rank quantiles)`

### Task 2: numeric annotation metrics

**Files:**

- Modify: `aggregate/aggregate.go`
- Test: extend `aggregate/aggregate_test.go`

**Interfaces:**

- Consumes: `Node.Annotations map[string]any` — flat keys `owner.key`, values `json.RawMessage` (see `model.SetAnnotation`, model/annotation.go:15).
- Produces: for each allowlisted key in `Options.AnnotationKeys` (exact string match against the flat `owner.key`), a `Row.Annotations[key] MetricStats` on step rows — per run per step-key: SUM of numeric values across occurrences; a value is numeric iff `json.Unmarshal` into `float64` succeeds (values arriving as `json.RawMessage`, `[]byte`, or already-decoded `float64` must all work — type-switch); non-numeric values are skipped silently. Keys with zero numeric contributions across the whole group are omitted from the map (no empty stats).

- [ ] **Step 1: Failing tests** — fixture with annotations: `eval.score` numeric on nodes in 2 runs (RawMessage `"0.8"` and float64 `0.6`), `eval.note` non-numeric (`"\"good\""`), a key not in the allowlist. Assert: allowlisted numeric key aggregates (correct median/N); non-numeric skipped; non-allowlisted absent; empty AnnotationKeys → nil/absent Annotations maps.
- [ ] **Step 2:** RED → implement → GREEN; permutation test still byte-identical (annotation maps sorted by `json.Marshal` naturally).
- [ ] **Step 3: Commit** — `feat(aggregate): opt-in numeric annotation metrics`

### Task 3: exported `reduce.ApplyAnnotations` + batch group loader by selector

**Files:**

- Modify: `reduce/graph.go` (new exported method) or new `reduce/annotations.go`
- Modify: `daemon/annotate.go` (`applyAnnotations` at line 111 delegates — behavior-preserving refactor)
- Modify: `cmd/catacomb/storeread.go` (new `loadRunGroup`)
- Test: `reduce/` + `cmd/catacomb/` extensions; `daemon/` existing tests must stay green unchanged

**Interfaces:**

- Produces: `func (g *Graph) ApplyAnnotations(anns []model.Annotation)` in `reduce` — moves the body of `daemon.applyAnnotations` (daemon/annotate.go:111-125: group by SourceKey, resolve node via the same source-key lookup, `model.SetAnnotation` per annotation). The daemon function becomes a one-line delegation. NOTE: `nodeBySourceKey` currently lives in daemon — move its logic into reduce too (exported or method-internal), leaving daemon a thin wrapper.
- Produces: in `cmd/catacomb/storeread.go`:

```go
func loadRunGroup(s store.Store, pricer reduce.Pricer, selector map[string]string) ([]aggregate.RunGraph, error)
```

Builds graphs via the existing `storeGraphs(s, pricer)`, reattaches annotations per execution (`s.AnnotationsForExecution` + `g.ApplyAnnotations` — mirror daemon.reattachAnnotations), collects runs via `collectRuns`, filters by `model.MatchLabels(r.Labels, selector)`, and for each selected run assembles `RunGraph{Run: r, Nodes, Edges}` from the graphs' snapshots filtered by `n.RunID == r.ID` (reuse the `collectSnapshot` filtering approach; nodes/edges sorted by ID). Returned groups sorted by `Run.ID`.

- [ ] **Step 1: Failing tests** — reduce: annotations attach to the right node by source key (port/adapt the existing daemon test scenario at the reduce level). cmd: seed a store with 3 runs (2 labeled `basket=b1` with annotations, 1 labeled `basket=b2`), `loadRunGroup(s, pricer, {"basket":"b1"})` returns exactly 2 RunGraphs, sorted, with nodes carrying reattached annotations; empty selector returns all 3.
- [ ] **Step 2:** RED → implement (move logic, delegate, loader) → GREEN. Daemon package tests must pass WITHOUT modification (pure refactor proof).
- [ ] **Step 3: `make cover`, `make lint`, codepolicy** → green.
- [ ] **Step 4: Commit** — `feat(reduce,cmd): exported ApplyAnnotations + label-selector run-group loader`

### Task 4: final review, live-verify, PR

- [ ] **Step 1:** Whole-branch review (requesting-code-review template, most capable model), review package from merge-base.
- [ ] **Step 2:** Fix wave if needed; re-verify.
- [ ] **Step 3:** Live-verify: seed a real store via a labeled `catacomb run` session (2 runs, same label), then a tiny Go test-program or existing test invoking `loadRunGroup` + `Aggregate` printing the Report JSON — confirm sane numbers on real data.
- [ ] **Step 4:** `make cover && make lint && go test ./internal/codepolicy/` + markdownlint on this plan file (`npx -y markdownlint-cli@0.49.0 docs/superpowers/plans/2026-07-02-pr-b-aggregate.md`).
- [ ] **Step 5:** Push `feat/aggregate`, open PR `feat: run-group aggregation package (ADR-0022 §2)` linking ADR-0022 + roadmap PR-B, wait CI green, squash-merge (authorized).
