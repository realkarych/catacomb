# PR-M: Hygiene Batch Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Dispatch a fresh subagent per task with review between tasks; the orchestrating window only coordinates. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the four residual-hygiene items the post-review hardening roadmap (`docs/superpowers/plans/2026-07-02-post-review-hardening-roadmap.md` §PR-M) parks in one batch: (1) refresh the stale `AGENTS.md` status line; (2) note ADR-0022's analytics-boundary refinement in the design spec's §2 non-goals; (3) give the pricing engine a longest-prefix model-family fallback so an unrecognized-but-familial model id still yields an `estimated` cost; (4) add a `go test -fuzz` commutativity harness for the reducer, wired to a new `make fuzz` target that is deliberately excluded from the coverage/CI test gates.

**Architecture:** Four independent tasks — two doc-only (markdownlint-gated), two Go (lint + coverage gated) — plus a final gates-and-markdownlint sweep. No product-surface, reducer-identity, schema, viewer, or exporter changes.

- **Item 1 & 2 (docs):** single-line / single-parenthetical edits. No section rewrites; the spec amendment mirrors the existing inline `(… per ADR-00XX)` parenthetical style already used by the §2 security non-goal (line 35) rather than inventing an amendment banner.
- **Item 3 (pricing):** `pricing/pricing.go` gains a `families []family` slice on `Engine` and a `familyTier(id)` longest-prefix lookup consulted only on exact-table miss. Provenance is the existing `Result.Source` string (`"reported"` vs `"estimated"`); a family hit is still `"estimated"` — no new provenance value, so the single downstream consumer (`reduce/reduce.go:529` writing `n.Attrs["cost_source"]`) is untouched. `New()` wires `defaultFamilies()`; the existing `newEngineWithTable` keeps its signature (families nil) so `pricing_test.go` compiles unchanged.
- **Item 4 (fuzz):** a new `reduce/commutativity_fuzz_test.go` (package `reduce`, so it reuses the existing `canonGraph` helper and observation constructors from `reduce_test.go`). The fuzz input is a `uint64` seed driving a deterministic `math/rand/v2` shuffle of a fixed, checked-in observation corpus; the property is graph equality (`canonGraph`) against the corpus applied in canonical (declaration) order. Seeds are checked in via `f.Add` plus one `testdata/fuzz` corpus file, so the target runs as an ordinary test under `go test` (and therefore under `make cover` / CI) without ever entering `-fuzz` mode. A new `make fuzz` target runs it for 30s and is wired into neither `make cover` nor `.github/workflows/ci.yml`.

**Tech Stack:** Go 1.26, testify, table-driven tests, `math/rand/v2` (gosec is not enabled in `.golangci.yml`, so `rand/v2` in a test file is fine). Repo rules: NO comments in Go (`internal/codepolicy`), 100% coverage, TDD, gofumpt+goimports via `make fmt`, no `time.Sleep` in tests, deterministic outputs, every task ends with a commit. Docs: markdownlint (`.markdownlint.json`; MD013 line-length is disabled, so no wrapping constraint).

## Global Constraints

- Work in this worktree only (`worktree-chore+hygiene-batch`); never touch the shared checkout. Confirm with `git rev-parse --abbrev-ref HEAD`.
- No comments in Go code — none, not even doc comments. The fuzz harness lives entirely in `_test.go`; add no non-test Go code (nothing new to cover). Test files are not coverage-measured, so 100% stays intact by construction as long as `pricing/pricing.go` is the only non-test Go file touched.
- `make lint` 0 issues; `make fmt` before committing Go.
- Deterministic behavior: the fuzz shuffle is seeded PRNG only (never wall-clock); the canonical baseline is a fixed slice order.
- One commit per task; the final task adds the gates sweep and any formatting fixups.

---

### Task 1: Refresh the `AGENTS.md` status line

**Files:**

- Modify: `AGENTS.md` (line 9)

**Interfaces:** none (doc-only).

**Current text (verified) — `AGENTS.md:9`:**

```markdown
**Status:** observability substrate complete (M0–M5 + eval-core, PRs #1–#98); current focus is the eval-management layer — baskets, run-group aggregation, regression gates (ADR-0022).
```

The line is stale: it names eval-management as "current focus", but that layer shipped (ADR-0022; PRs #100–#104 and follow-ups) and the post-review hardening batch (ADR-0023–0025) has since landed through PR-L.

- [ ] **Step 1: Confirm the current tail PR number**

Run: `git log --oneline -1 && gh pr list --state merged --limit 1 --json number,title 2>/dev/null || echo "gh unavailable"`
Use the highest merged PR number as `#<N>` below (the roadmap and MEMORY reference the #100–#111 range; if `gh` is unavailable, write `#1–#111` and note in the commit that the ceiling is approximate). Do not block on an exact value — the line is a pointer to the roadmap, which is the source of truth.

- [ ] **Step 2: Replace line 9**

Replace the exact current text above with:

```markdown
**Status:** observability substrate, eval-management layer, and post-review hardening all shipped (M0–M5, eval-core, ADR-0022 baskets/aggregation/regression gates, ADR-0023–0025 hardening; PRs #1–#111). Current state and open follow-ups live in the [post-review hardening roadmap](docs/superpowers/plans/2026-07-02-post-review-hardening-roadmap.md) and the [post-P0 CTO review](docs/reviews/2026-07-02-post-p0-cto-design-review.md).
```

Both referenced paths exist (verified). Adjust the tail PR number to Step 1's value if it differs.

- [ ] **Step 3: Lint + commit**

Run: `npx -y markdownlint-cli@0.49.0 AGENTS.md`
Expected: 0 errors (MD013 line-length is disabled).

```bash
git add AGENTS.md
git commit -m "docs(agents): refresh status line — substrate + eval-management + hardening shipped (ADR-0022..0025)"
```

---

### Task 2: Spec §2 non-goals — ADR-0022 boundary-refinement note

**Files:**

- Modify: `docs/specs/2026-06-20-catacomb-design.md` (§2 Non-Goals, the "No evaluation / scoring / optimality" bullet, currently line 33)

**Interfaces:** none (doc-only).

**How the spec marks amendments (verified):** the spec has no amendment-banner convention. Clarifications/scope-refinements are appended inline as a parenthetical that cites the deciding ADR — e.g. the §2 security non-goal (line 35): `… (This is *not* "no security": the daemon still has a local trust boundary … per ADR-0013.)`, and every §20 hardening bullet ends `(… §N)`. Mirror that: append one parenthetical to the existing bullet. Do NOT rewrite the section, add a heading, or touch §20.

**Current bullet (verified) — line 33:**

```markdown
- **No evaluation / scoring / optimality** (MC-value, rollouts, reward models, improvement reports). Out of scope; supported only via a generic annotation slot for downstream consumers.
```

ADR-0022's own boundary framing (quote it faithfully): *"Comparing distributions of deterministic observables already in the graph (status, presence, duration, cost, tokens) is analytics over the graph — the same family as `diff`, which is in scope. Judging semantic output quality stays out (delegated via annotations to DeepEval/agentevals per ADR-0016)."*

- [ ] **Step 1: Append the parenthetical**

Replace the current bullet (line 33) with:

```markdown
- **No evaluation / scoring / optimality** (MC-value, rollouts, reward models, improvement reports). Out of scope; supported only via a generic annotation slot for downstream consumers. (ADR-0022 refines this boundary, it does not move it: deterministic distributional comparison of observables already in the graph — status, presence, duration, cost, tokens — is in-scope analytics, the same family as `diff`; judging semantic output *quality* stays out, delegated to external scorers via annotations per ADR-0016.)
```

This is the only change. Leave every other §2 line, and §20, untouched.

- [ ] **Step 2: Lint + commit**

Run: `npx -y markdownlint-cli@0.49.0 docs/specs/2026-06-20-catacomb-design.md`
Expected: 0 errors.

```bash
git add docs/specs/2026-06-20-catacomb-design.md
git commit -m "docs(spec): note ADR-0022 analytics-boundary refinement in §2 non-goals"
```

---

### Task 3: pricing — longest-prefix model-family fallback

**Files:**

- Modify: `pricing/pricing.go`
- Test: `pricing/pricing_test.go`

**Interfaces:**

- `(*Engine).Cost(Inputs) (Result, bool)` — signature unchanged. On exact-table miss it now consults `familyTier`; on family hit returns `Result{USD: …, Source: "estimated"}`; on family miss still returns `Result{}, false`.
- New unexported: `type family struct { prefix string; tier Tier }`, `func (e *Engine) familyTier(id string) (Tier, bool)`, `func defaultFamilies() []family`, `func newEngineWithFamilies(t map[string]Tier, fams []family) *Engine`.
- `Engine` gains field `families []family`. `newEngineWithTable(t)` keeps its exact signature (families left nil) so existing tests compile without edits. `New()` wires `defaultFamilies()`.

Provenance mechanism (verified): provenance is the `Result.Source` string — `"reported"` when `Inputs.ReportedUSD != nil`, else `"estimated"` for any table-derived cost. The only consumer is `reduce/reduce.go:529` (`n.Attrs["cost_source"] = res.Source`); nothing switches on the value, so reusing `"estimated"` for family hits is safe and requires no downstream change.

- [ ] **Step 1: Write failing tests**

Add to `pricing/pricing_test.go` (imports already present: `testing`, `assert`, `require`):

```go
func TestCostPrefixFamilyFallbackEstimated(t *testing.T) {
	e := New()
	r, ok := e.Cost(Inputs{ModelID: "claude-opus-4-9", TokensIn: 1_000_000})
	require.True(t, ok)
	assert.Equal(t, "estimated", r.Source)
	assert.InDelta(t, 5.00, r.USD, 1e-9)
}

func TestCostUnknownFamilyYieldsNothing(t *testing.T) {
	e := New()
	_, ok := e.Cost(Inputs{ModelID: "gpt-5-turbo", TokensIn: 10})
	assert.False(t, ok)
}

func TestCostExactMatchTakesPrecedenceOverFamily(t *testing.T) {
	e := New()
	r, ok := e.Cost(Inputs{ModelID: "claude-haiku-4-5", TokensIn: 1_000_000})
	require.True(t, ok)
	assert.Equal(t, "estimated", r.Source)
	assert.InDelta(t, 1.00, r.USD, 1e-9)
}

func TestCostPrefixLongestFamilyWins(t *testing.T) {
	fams := []family{
		{prefix: "claude-opus-", tier: Tier{InputPerMTok: 5}},
		{prefix: "claude-opus-4-", tier: Tier{InputPerMTok: 9}},
	}
	e := newEngineWithFamilies(testTable(), fams)
	r, ok := e.Cost(Inputs{ModelID: "claude-opus-4-9", TokensIn: 1_000_000})
	require.True(t, ok)
	assert.Equal(t, "estimated", r.Source)
	assert.InDelta(t, 9.0, r.USD, 1e-9)
}

func TestCostNoFamiliesStillMissesUnknown(t *testing.T) {
	e := newEngineWithTable(testTable())
	_, ok := e.Cost(Inputs{ModelID: "claude-opus-4-9", TokensIn: 10})
	assert.False(t, ok)
}
```

Note: `TestCostPrefixLongestFamilyWins` deliberately lists the shorter prefix first to prove selection is by longest match, not by slice position. `TestCostNoFamiliesStillMissesUnknown` covers the `families == nil` no-match branch via `newEngineWithTable`. Existing tests (`TestCostReportedFirst`, `TestCostEstimateFromTiers`, `TestCostUnknownModel`, `TestCostZeroTokensKnownModel`, `TestNewHasRealTableEntry`) remain unchanged and must stay green.

- [ ] **Step 2: Run, verify fail**

Run: `go test ./pricing/`
Expected: FAIL (undefined `family`, `newEngineWithFamilies`; `claude-opus-4-9` currently misses).

- [ ] **Step 3: Implement in `pricing/pricing.go`**

Add `"strings"` to the imports. Add the field and constructor, and the family lookup:

```go
type family struct {
	prefix string
	tier   Tier
}

type Engine struct {
	table    map[string]Tier
	families []family
}

func New() *Engine {
	return newEngineWithFamilies(defaultTable(), defaultFamilies())
}

func newEngineWithTable(t map[string]Tier) *Engine {
	return &Engine{table: t}
}

func newEngineWithFamilies(t map[string]Tier, fams []family) *Engine {
	return &Engine{table: t, families: fams}
}
```

Extend `Cost` to fall through to the family table on exact miss:

```go
func (e *Engine) Cost(in Inputs) (Result, bool) {
	if in.ReportedUSD != nil {
		return Result{USD: *in.ReportedUSD, Source: "reported"}, true
	}
	tier, ok := e.table[in.ModelID]
	if !ok {
		tier, ok = e.familyTier(in.ModelID)
		if !ok {
			return Result{}, false
		}
	}
	usd := perMTok(in.TokensIn, tier.InputPerMTok) +
		perMTok(in.TokensOut, tier.OutputPerMTok) +
		perMTok(in.CacheReadIn, tier.CacheReadPerMTok) +
		perMTok(in.CacheWrite, tier.CacheWritePerMTok)
	return Result{USD: usd, Source: "estimated"}, true
}

func (e *Engine) familyTier(id string) (Tier, bool) {
	best := -1
	var chosen Tier
	for _, f := range e.families {
		if len(f.prefix) > best && strings.HasPrefix(id, f.prefix) {
			best = len(f.prefix)
			chosen = f.tier
		}
	}
	if best < 0 {
		return Tier{}, false
	}
	return chosen, true
}

func defaultFamilies() []family {
	return []family{
		{prefix: "claude-opus-", tier: Tier{InputPerMTok: 5.00, OutputPerMTok: 25.00, CacheReadPerMTok: 0.50, CacheWritePerMTok: 6.25}},
		{prefix: "claude-sonnet-", tier: Tier{InputPerMTok: 3.00, OutputPerMTok: 15.00, CacheReadPerMTok: 0.30, CacheWritePerMTok: 3.75}},
		{prefix: "claude-haiku-", tier: Tier{InputPerMTok: 1.00, OutputPerMTok: 5.00, CacheReadPerMTok: 0.10, CacheWritePerMTok: 1.25}},
		{prefix: "claude-fable-", tier: Tier{InputPerMTok: 10.00, OutputPerMTok: 50.00, CacheReadPerMTok: 1.00, CacheWritePerMTok: 12.50}},
	}
}
```

The `defaultTable()` function is unchanged. Do not add a doc comment to `familyTier` — codepolicy forbids it; the name carries the meaning.

- [ ] **Step 4: Green + commit**

Run: `go test -race ./pricing/ && go test ./pricing/ -cover`
Expected: PASS, coverage 100.0%. (`familyTier`'s match and no-match branches are covered by `TestCostPrefixFamilyFallbackEstimated`/`TestCostPrefixLongestFamilyWins` and `TestCostUnknownFamilyYieldsNothing`/`TestCostNoFamiliesStillMissesUnknown` respectively.)

Run: `make fmt && make lint`
Expected: clean.

```bash
git add pricing/
git commit -m "feat(pricing): longest-prefix model-family fallback with estimated provenance"
```

---

### Task 4: Reducer commutativity fuzz target + `make fuzz`

**Files:**

- Create: `reduce/commutativity_fuzz_test.go`
- Create: `reduce/testdata/fuzz/FuzzReductionCommutativity/seed_a` (checked-in seed corpus file)
- Modify: `Makefile` (add `fuzz` target)
- Modify: `AGENTS.md` (Build / dev block one-liner)

**Interfaces:**

- New fuzz test `FuzzReductionCommutativity(*testing.F)` in package `reduce`. Reuses, from `reduce_test.go` (same package): `canonGraph(*Graph) string`, and the observation constructors `sessionStartObs`, `sessionEndObs`, `toolObs`, `otelTool`, `hookTurn`, `otelTurn`, `runEndedObs` (all verified present). No new exported or non-test symbols.
- New `make fuzz` target. It is NOT referenced by `make cover`, `make test`, or `.github/workflows/ci.yml` (verified: CI's `test` and `coverage` jobs run `go test ./...` / `go test -race -coverpkg=./...`, neither of which passes `-fuzz`, so the target's seed corpus runs as ordinary tests and nothing enters fuzzing mode in CI).

**Why coverage stays 100%:** everything added here is either a `_test.go` file (not coverage-measured) or non-Go (`Makefile`, `AGENTS.md`). The fuzz function body executes during a normal `go test ./reduce/` run (Go replays every `f.Add` seed and every `testdata/fuzz` corpus entry as a subtest), so the harness is exercised by the existing gate without adding any uncovered production statement.

- [ ] **Step 1: Write the fuzz test (failing: file does not exist)**

Create `reduce/commutativity_fuzz_test.go`. NO comments anywhere in the file.

```go
package reduce

import (
	"math/rand/v2"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/realkarych/catacomb/model"
)

func commutativityCorpus() []model.Observation {
	t0 := time.Unix(100, 0).UTC()
	return []model.Observation{
		sessionStartObs("e1", "s1", 1),
		hookTurn("e1", "s1", "m1", 5, 2, t0, 2),
		otelTurn("e1", "s1", "m1", 50, 20, t0.Add(time.Second), 3),
		toolObs("e1", "s1", "t1", "Bash", "running", 4),
		toolObs("e1", "s1", "t1", "Bash", string(model.StatusOK), 6),
		otelTool("e1", "s1", "t2", "spanLeaf", "spanRoot", 7),
		toolObs("e1", "s1", "t3", "mcp__fs__read", "running", 8),
		runEndedObs("e1", "s1", "timeout", 9),
		toolObs("e1", "s1", "t4", "Read", string(model.StatusOK), 10),
		sessionEndObs("e1", "s1", 11),
	}
}

func canonicalCommutativityGraph() string {
	g := NewGraph()
	g.ApplyAll(commutativityCorpus())
	return canonGraph(g)
}

func shuffledBySeed(seed uint64) []model.Observation {
	obs := commutativityCorpus()
	r := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	r.Shuffle(len(obs), func(i, j int) { obs[i], obs[j] = obs[j], obs[i] })
	return obs
}

func FuzzReductionCommutativity(f *testing.F) {
	f.Add(uint64(0))
	f.Add(uint64(1))
	f.Add(uint64(42))
	f.Add(uint64(6364136223846793005))
	f.Add(^uint64(0))
	want := canonicalCommutativityGraph()
	f.Fuzz(func(t *testing.T, seed uint64) {
		g := NewGraph()
		g.ApplyAll(shuffledBySeed(seed))
		assert.Equal(t, want, canonGraph(g), "reduction diverged for shuffle seed %d", seed)
	})
}
```

Design notes (do not add these as code comments): the corpus is built entirely from constructors whose order-independence is already asserted by existing tests in `reduce_test.go` (token OTel-precedence, status-lattice latching, run reawaken-from-abandoned, session-end cascade), so a genuine divergence signals a real reducer bug, not a flaky corpus. The property compares each seeded shuffle against the corpus applied in declaration order — the canonical order — exactly as `TestReductionCommutativity` does across full permutations, but reachable at corpus sizes where full permutation is infeasible.

- [ ] **Step 2: Add the checked-in seed corpus file**

Create `reduce/testdata/fuzz/FuzzReductionCommutativity/seed_a` with exactly these two lines (Go corpus v1 format; the second line is the seed argument):

```text
go test fuzz v1
uint64(2654435769)
```

This is a literally checked-in seed corpus entry (in addition to the in-code `f.Add` seeds); Go replays it during ordinary `go test`.

- [ ] **Step 3: Run as a normal test, verify PASS (no `-fuzz`)**

Run: `go test -race ./reduce/ -run '^FuzzReductionCommutativity$' -v`
Expected: PASS — the subtests are `seed#0…#4` (the `f.Add` seeds) and `seed_a` (the corpus file). If any diverges, STOP: that is a reducer commutativity bug to investigate with superpowers:systematic-debugging, not a test to relax.

- [ ] **Step 4: Exercise the fuzzer briefly to confirm the harness actually fuzzes**

Run: `go test ./reduce/ -run '^$' -fuzz '^FuzzReductionCommutativity$' -fuzztime 15s`
Expected: `PASS` / `elapsed` with no new corpus entries added under `testdata/fuzz` reporting a failure. This is a smoke check only; do not commit any `-fuzz`-generated corpus files (they land in `$GOCACHE/fuzz`, not the repo). Confirm `git status` shows only the intended files.

- [ ] **Step 5: Add the `make fuzz` target**

In `Makefile`, add `fuzz` to the `.PHONY` line (the first one: `.PHONY: all build test cover lint fmt tidy clean proto help`) → append `fuzz`. Insert the target after the `cover` target and before `lint`:

```make
## Fuzz the reducer commutativity property for a short burst (not part of cover/CI)
fuzz:
	@go test -run '^$$' -fuzz '^FuzzReductionCommutativity$$' -fuzztime 30s ./reduce
```

Note the doubled `$$` — make expands `$` first, so `$$` yields the literal `$` the shell/regexp needs. Add a matching help line in the `help` target's echo block, after the `cover` line:

```make
	@echo "  fuzz    - fuzz the reducer commutativity property (30s; not in cover/CI)"
```

Do NOT add `fuzz` as a dependency of `cover`, `test`, or `all`; do NOT reference it from `.github/workflows/ci.yml`.

- [ ] **Step 6: Document in AGENTS.md Build / dev**

In `AGENTS.md`, inside the `## Build / dev` fenced block (the `make build … make fmt` list), add one line after `make cover`:

```text
make fuzz    # reducer commutativity fuzzer (30s; not in cover/CI)
```

- [ ] **Step 7: Verify the target and the gate isolation**

Run: `make fuzz`
Expected: runs ~30s, ends `PASS`.

Run: `go test ./reduce/`
Expected: PASS (confirms the seed corpus runs under the ordinary test path that `make cover` and CI use).

Run: `git grep -n 'fuzz' Makefile .github/workflows/ci.yml`
Expected: matches only in `Makefile` (the target + help + `.PHONY`), zero matches in `ci.yml` — proving the fuzzer is not wired into any CI gate.

- [ ] **Step 8: Fmt, lint, commit**

Run: `make fmt && make lint && npx -y markdownlint-cli@0.49.0 AGENTS.md`
Expected: clean.

```bash
git add reduce/commutativity_fuzz_test.go reduce/testdata/fuzz Makefile AGENTS.md
git commit -m "test(reduce): go test -fuzz commutativity harness + make fuzz target"
```

---

### Task 5: Full gates + markdownlint sweep

**Files:** none (verification only; commit only if `make fmt` reformats).

- [ ] **Step 1: Go gates**

Run: `make fmt && make lint && make cover`
Expected: fmt clean, lint 0 issues, coverage total/package/file 100.0%. The only production Go change in this batch is `pricing/pricing.go`; confirm the coverage report shows `pricing` at 100%.

- [ ] **Step 2: Markdownlint sweep (all touched docs + repo-wide, matching CI)**

Run: `npx -y markdownlint-cli@0.49.0 '**/*.md' --ignore node_modules`
Expected: 0 errors. (CI's `lint-docs` job runs `markdownlint '**/*.md' --ignore node_modules`; match it exactly so nothing surprises the pipeline.)

- [ ] **Step 3: Confirm fuzz isolation once more end-to-end**

Run: `go test ./... 2>&1 | tail -20`
Expected: all packages PASS, including `reduce` (fuzz seeds run as tests); no package hangs (nothing enters `-fuzz` mode).

- [ ] **Step 4: Final commit if needed**

If Step 1's `make fmt` reformatted anything:

```bash
git add -A
git commit -m "chore: gofumpt sweep for PR-M hygiene batch"
```

Otherwise no commit — the four task commits stand.

- [ ] **Step 5: Branch summary for the PR body**

Run: `git log --oneline master..HEAD`
Expected: four (or five, with the sweep) commits — status-line, spec-note, pricing-fallback, fuzz-target, [sweep]. Capture this list plus the Task 3 coverage line and the Task 4 Step 7 `git grep` output (proving fuzz is not in CI) for the PR description.
