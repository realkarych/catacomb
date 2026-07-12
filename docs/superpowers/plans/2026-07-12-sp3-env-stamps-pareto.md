# SP3 Env Stamps + Pareto Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development
> (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stamp environment provenance into per-run evidence `meta.json` and add an
accuracy-vs-cost Pareto table to `trends`, per the SP3 spec.

**Architecture:** One PR wave on branch `feat/env-stamps-pareto`, three tasks: (1) `EnvStamps`
on `evidence.Meta` + bench-time stamping from the already-built offline graph; (2) Pareto
point extraction + domination marking + `--pareto` flag in trends; (3) hermetic-E2E
assertions and docs. No store DDL, no `Record.V` bump, everything additive.

**Tech Stack:** Go stdlib only; exact types, formats, and rules in
`docs/specs/2026-07-12-sp3-env-stamps-pareto-design.md` — the spec is the source of truth
for every struct, JSON tag, and the domination rule; read it first, use it verbatim.

## Global Constraints

- **No comments in Go code** (internal/codepolicy). **TDD**; **coverage 100%**; `make lint`
  green; gosec (`go run github.com/securego/gosec/v2/cmd/gosec@latest -exclude=G304 ./...`)
  Issues: 0.
- Table-driven tests; testify require/assert; no time.Sleep; no new Go deps.
- Env stamps are descriptive only: they must NOT touch `model.Stamps`, `Mismatch`, or any
  `--strict` path. No `os.Hostname()`, no env-var capture.
- No `Record`/store changes: `Record.V` stays 1, store schema stays 5, Pareto reads only
  stored findings.
- One PR, branch `feat/env-stamps-pareto` from master, squash after green CI (Hermetic E2E
  is a required check).

## File Map

| Task | Create | Modify |
|---|---|---|
| 1 | — | `evidence/evidence.go`, `evidence/evidence_test.go`, `cmd/catacomb/bench.go` (+ its tests) |
| 2 | `cmd/catacomb/pareto.go`, `cmd/catacomb/pareto_test.go` | `cmd/catacomb/trends.go`, `cmd/catacomb/trends_test.go` |
| 3 | — | `e2e/hermetic/run.sh`, `docs/guide/cli.md`, `docs/guide/workflows.md` |

---

### Task 1: EnvStamps in evidence meta

**Interfaces (spec §1, verbatim):** `Resources{OS, Arch string; CPUs int}` (JSON
os/arch/cpus), `EnvStamps{CatacombVersion string; ModelID string (omitempty);
ClaudeCodeVersion string (omitempty); Resources Resources}` (JSON catacomb_version/model_id/
claude_code_version/resources), `Meta.Env *EnvStamps` (JSON `env`, omitempty). Sources:
`Version` var; graph `RunsSnapshot()` entry matching the cell session id → `ModelID` and
`Repro.ClaudeCodeVersion`; `runtime.GOOS/GOARCH/NumCPU()`. Stamp in `offlineMeta` from the
graph `recordOfflineEvidence` already builds. `StampArtifacts` second pass must preserve
`env` (it re-marshals the full struct — prove it with a test, don't assume).

- [ ] **Step 1:** Failing tests: Meta JSON round-trip with and without `Env` (nil → key
  absent; populated → exact tags incl. omitted `model_id` when empty); `ScanRuns`/`ReadMeta`
  accept legacy meta.json without `env`; StampArtifacts on a meta carrying `Env` preserves it
  byte-faithfully; bench-side test that `offlineMeta` output carries catacomb version,
  resources with `CPUs >= 1`, and model_id/claude_code_version exactly when the fixture
  transcript provides them (both fixtures: with assistant turn + version, and without).
- [ ] **Step 2:** Run → FAIL. **Step 3:** Implement (types in evidence.go; wiring in
  bench.go). **Step 4:** `go test -race ./evidence/ ./cmd/... -count=1` PASS; `make cover`
  100%. **Step 5:** Commit `feat(evidence): env stamps in meta.json (model, claude-code
  version, resources)`.

### Task 2: Pareto table in trends

**Interfaces (spec §2, verbatim):** new file `cmd/catacomb/pareto.go`:
`paretoPoint{Source string (json source: "baseline"|"record"); Seq int (json seq, omitempty);
Candidate string (json candidate, omitempty); CreatedAt time.Time (json created_at,
omitzero); Accuracy *float64 (json accuracy, omitempty); CostUSD *float64 (json cost_usd,
omitempty); Dominated *bool (json dominated, omitempty); Spliced *bool (json spliced,
omitempty — pointer set for record points only, nil for the baseline point, so `false` still
serializes for records)}` and `paretoReport{Baseline string (json
baseline); Points []paretoPoint (json points)}`. Extraction: per record, accuracy = total
finding `ann:verifier.pass` `.Candidate`, cost = total finding `cost_usd` `.Candidate`
(reuse `totalFinding`); baseline point from the NEWEST record's `.Baseline` values.
Domination over points with both axes: dominated iff ∃ other with `acc >= && cost <=` and
strict on ≥1 axis; equal-on-both ⇒ neither dominates; single-axis/no-axis points get nil
`Dominated`. Sort: cost asc, accuracy desc, seq asc; non-comparable last by seq. Human
table columns `SEQ CREATED CANDIDATE ACCURACY COST_USD DOMINATED` ("-" for absent; baseline
row SEQ/CREATED "-"; `%.2f` accuracy, `%.4f` cost via existing trends value formatting
conventions; splice `*` + footnote reused); epilogue note when non-comparable rows exist:
`pareto: N row(s) lack an accuracy axis (no ann:verifier.pass finding) and are not compared`.
Flag `--pareto` in `newTrendsCmd`; `--pareto`+`--metric` → operational error
`trends: --pareto and --metric are mutually exclusive`; `--pareto --json` encodes
`paretoReport` with the trends JSON idiom.

- [ ] **Step 1:** Failing table-driven tests: extraction (record with both findings / missing
  verifier / missing cost / no records); baseline point from newest record; domination table
  (strictly-better-one-axis, equal-both ⇒ both false, chain of three, non-comparable
  excluded); ordering incl. non-comparable sink; human render golden lines (baseline row,
  dominated yes, "-" cells, splice star, note present/absent); JSON shape (omitted keys
  exactly per spec — assert raw JSON, not just round-trip); flag conflict exit 2 + message.
- [ ] **Step 2:** FAIL. **Step 3:** Implement pareto.go + trends.go branch (before
  metric/default branches). **Step 4:** `go test -race ./cmd/... -count=1` PASS; 100%;
  lint 0. **Step 5:** Commit `feat(trends): accuracy-vs-cost pareto table with dominated-row
  marking`.

### Task 3: E2E and docs

- [ ] **Step 1:** Extend `e2e/hermetic/run.sh` (existing helper/assert style; append a new
  section, renumber nothing): (a) after bench, assert `meta.json` `env.catacomb_version`
  non-empty, `env.resources.os/arch` non-empty, `env.resources.cpus >= 1`, and pin
  `env.model_id`/`env.claude_code_version` against what the hermetic fixture transcript
  actually carries (inspect the fixture first; assert presence+value if present, absence if
  not — no soft-pass); (b) create a fresh trends baseline, `regress --record` the A-vs-A and
  degraded comparisons, then `trends --pareto --json` asserts: degraded point
  `dominated == true` (equal cost, strictly worse accuracy), A-vs-A point and baseline point
  both `dominated == false`, and every point carries both axes (hermetic runs always have
  cost + verifier). Re-run driver → PASS; state the new assertion count. shellcheck clean.
- [ ] **Step 2:** Docs: cli.md — `env` block in the evidence-layout section, `trends
  --pareto` (columns, domination rule in one sentence, JSON keys incl. omission semantics,
  `--metric` conflict); workflows.md — reading the Pareto (frontier top-down, why equal
  A-vs-A rows are both non-dominated, dominated row = strictly worse deal). markdownlint
  clean via the repo invocation.
- [ ] **Step 3:** Full gates (race tests, `TestGatePower` untouched, `make cover` 100%,
  `make lint`, codepolicy, gosec, shellcheck, markdownlint). Commit `test(e2e): env-stamp +
  pareto assertions; docs`.

## Acceptance

Hermetic driver green with the new assertions on the PR (required check); env stamps
verifiably sourced from graph/runtime only; Pareto renders from stored records with the
spec's exact domination/ordering/omission semantics; 100% coverage; docs accurate.

## Self-review notes

Spec coverage: §1→Task 1, §2→Task 2, §4→Tasks 1–3 test lists, §3 dormancy asserted via
legacy-meta tests (Task 1) and no-verifier extraction rows (Task 2). Type names consistent
across tasks (`EnvStamps`/`Resources` produced in Task 1 are not consumed by Task 2 — the
two features are independent; Task 3 asserts both through the binary). No placeholders;
formats, error strings, and the domination rule stated exactly.
