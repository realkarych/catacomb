# PR-H: Declared Checkpoints + Post-Run Verification Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Basket tasks declare the in-run checkpoints the agent is EXPECTED to mark (`checkpoints: [plan, verify]`, the CLAUDE.md/mcp `catacomb__mark` convention); `catacomb bench` verifies after each cell that every declared name landed as a marker and makes misses loudly visible — deterministic VERIFICATION, never synthesis (ADR-0022 explicitly rejects pattern-based marker synthesis). Final P1 item.

**Architecture:** `bench.Task` gains `Checkpoints []string` (validated at Load); after each executed cell with an observed session id, the bench command fetches the session graph (existing `GET /v1/sessions/{id}/graph`, addr/token from the discovery file, `statusHTTPClient` with its 5s timeout), collects marker-node names, records `MissingCheckpoints` in the manifest entry, warns per cell on stderr, and prints a per-checkpoint epilogue summary (`checkpoints: plan 8/8, verify 5/8`). No daemon changes — the stream-path marks complete during `runChildObserved` teardown, where the `<-fwdDone` barrier waits for the daemon's applied-on-200 response before the function returns (not before `child.Wait()`), so a post-exit fetch observes every mark that was accepted during the cell.

**Tech Stack:** Go 1.26, stdlib, testify.

## Global Constraints

- No comments in Go code; 100% coverage; TDD RED first; `make lint` 0 (cache clean if stale); codepolicy; gofumpt; testify; no time.Sleep; deterministic (epilogue checkpoint order = declaration order per task, tasks in file order, dedup preserving first occurrence).
- Verification is best-effort-with-recorded-visibility (same contract as task markers, ADR-0022 Amendments): no session id observed OR graph fetch fails → verification skipped with a manifest Note + stderr line, never a cell failure; the summary counts only verified cells.
- Verification must not alter exit codes: a missing checkpoint is VISIBILITY (warning + manifest + summary), not failure — regressions are `regress`'s job (a missing phase surfaces there as a presence drop).
- markdownlint on touched docs.

---

### Task 1: `bench` package — checkpoint declarations + manifest field

**Files:**

- Modify: `bench/basket.go` (Task struct, validation), `bench/manifest.go` (ManifestEntry)
- Test: `bench/basket_test.go`, `bench/manifest_test.go`

**Interfaces (exact):**

```go
type Task struct {
	ID          string            `yaml:"id" json:"id"`
	Cmd         []string          `yaml:"cmd" json:"cmd"`
	Dir         string            `yaml:"dir,omitempty" json:"dir,omitempty"`
	Env         map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
	Checkpoints []string          `yaml:"checkpoints,omitempty" json:"checkpoints,omitempty"`
}
```

`ManifestEntry` gains `MissingCheckpoints []string \`json:"missing_checkpoints,omitempty"\`` (after Marked).

**Binding validation (extend `validate`):** each checkpoint name non-empty, ≤256 bytes, charset `^[A-Za-z0-9._:-]+$` (the ID charset PLUS `:` — marker names like `task:build` are legal), unique within its task (duplicates → field-named error, new sentinel or reuse `ErrCharset`-style pattern with a dedicated sentinel `ErrCheckpoint`); a checkpoint name must NOT collide with the runner's own task-boundary marker name `task:<that task's ID>` (reserved — error).

- [ ] **Step 1: Failing tests** — YAML fixture with checkpoints round-trips; validation table (empty name, >256, bad charset incl. space and comma, duplicate within task, reserved `task:<id>` collision, colon-containing name ACCEPTED); ManifestEntry JSON round-trip with and without the new field (omitempty pinned).
- [ ] **Step 2:** RED → implement → GREEN; `go test ./bench/`, `make lint`, codepolicy.
- [ ] **Step 3: Commit** — `feat(bench): declared checkpoints on tasks + manifest visibility field`

### Task 2: `catacomb bench` — post-cell verification + epilogue summary

**Files:**

- Modify: `cmd/catacomb/bench.go`
- Test: `cmd/catacomb/bench_test.go`

**Binding contract:**

- After a cell's markers are finished (post `marks.finish()`), if the cell's task declares checkpoints AND a session id was observed: GET `/v1/sessions/<sessionID>/graph` (discovery addr + bearer token; reuse `statusHTTPClient`); decode the event array (same shape the PR-F docs recipe uses: `kind == "node_upsert"`, `node.type == "marker"`, collect `node.name`); `MissingCheckpoints` = declared names (order preserved) absent from the collected set. Non-empty → one stderr line `cell <run-id>: missing checkpoints: a, b`. Fetch/decode failure OR daemon already unreachable → skip verification, `Note` gains `; checkpoint verification skipped: <reason>` (append, don't clobber existing notes), no stderr spam beyond one line, never a cell failure.
- Dry-run: no verification (no execution at all — unchanged).
- Epilogue (after the marked-summary line): for each task with declared checkpoints, per checkpoint name in declaration order: `checkpoints[<task>]: <name> <hit>/<verified>` where verified = executed cells of that task whose verification RAN (skipped cells excluded), hit = verified cells where the name was present. Omit the section entirely when no task declares checkpoints. Resume-skipped cells excluded (consistent with `marked n/total`).
- Tests (extend the existing fake-daemon httptest harness which already records /v1/mark — add a /v1/sessions/{id}/graph handler returning configurable marker sets): declared+present → no missing, summary full; declared+absent → manifest field, stderr line, summary partial; graph-fetch error → skipped note, summary excludes; no-session cell → skipped (existing no-session path), summary excludes; multiple tasks with different declarations → per-task summary lines in order; no declarations → no section, no fetches (pin zero graph requests).

- [ ] **Step 1: Failing tests** → **Step 2:** implement → GREEN; `go test ./cmd/... ./bench/`, `make cover`, `make lint`, codepolicy.
- [ ] **Step 3: Commit** — `feat(cmd): bench verifies declared checkpoints post-cell (visibility, not gating)`

### Task 3: ADR amendment, docs, final review, live-verify, PR, merge

- [ ] ADR-0022 Amendments bullet: declared checkpoints are verified post-cell against ingested markers (best-effort-with-recorded-visibility, consistent with the task-boundary floor); verification never gates — missing phases gate via presence rates in `regress`; synthesis remains rejected.
- [ ] Docs: cli.md bench entry (checkpoints field in the basket schema block, reserved `task:<id>` name, verification/summary semantics, skip conditions); workflows.md (declare checkpoints in the basket + CLAUDE.md convention sentence + how misses appear in bench output and as presence drops in regress). markdownlint clean.
- [ ] Whole-branch review (most capable model) from `git merge-base origin/master HEAD`; fix wave; re-verify.
- [ ] Live-verify: real daemon; basket where the child emits a mark for `plan` (via `catacomb mark` inside the cell command or a direct POST) but not `verify` → manifest shows `missing_checkpoints:["verify"]`, stderr warning, epilogue `plan 2/2, verify 0/2`; regress over the groups shows the phase presence drop.
- [ ] `make cover && make lint && codepolicy` + markdownlint (docs + this plan).
- [ ] Push `feat/checkpoints`, open PR `feat: bench declared checkpoints + post-run verification (P1)`, CI green, squash-merge (authorized).
