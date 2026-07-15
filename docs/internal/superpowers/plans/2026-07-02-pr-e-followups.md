# PR-E: Follow-ups & Pre-existing Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close every non-blocking follow-up recorded across the PR #100–#103 reviews plus the pre-existing `readOnlyDSN` bug — one hardening PR, no new features.

**Architecture:** three independent fix clusters (CLI/store, daemon/labels, aggregate/bench/tests+docs), each a self-contained TDD task; no schema changes, no new commands.

**Tech Stack:** Go 1.26, stdlib, testify.

## Global Constraints

- No comments in Go code (only directives); 100% coverage (`make cover`); TDD with RED evidence where behavior changes; `make lint` 0 (run `golangci-lint cache clean` first if stale sibling-worktree issues appear); codepolicy; gofumpt/goimports; testify; no time.Sleep.
- Every fix is behavior-preserving for callers not exercising the bug (additive JSON fields, tightened errors only where the old behavior was wrong).
- markdownlint on touched docs.

---

### Task 1: CLI/store hardening

**Files:** `cmd/catacomb/status.go` (~22), `store/sqlite.go` (`readOnlyDSN` ~95 + tests), `cmd/catacomb/baseline.go` (error wrapping), `cmd/catacomb/regress_test.go` (one assertion), tests alongside.

**Fixes (binding):**

1. **HTTP client timeout (shared by `up`/`status`/bench preflight):** `statusHTTPClient = &http.Client{}` gets `Timeout: 5 * time.Second`. A daemon that accepts but never responds must not hang `status`/`up`/`bench`. Test: an httptest server that blocks longer than a shortened injected timeout — check how statusHTTPClient is injected (`httpClient` field ~line 60) and test at that seam with a small timeout client rather than 5s waits (no time.Sleep — use a handler blocking on a channel released by t.Cleanup).
2. **`readOnlyDSN` relative-path bug (pre-existing, breaks `regress`/`runs`/`baseline` on `--db ./x.db`):** resolve the path with `filepath.Abs` before URL construction (error → fall back to current behavior is NOT acceptable; `readOnlyDSN` returns only string — change signature to `(string, error)` or resolve Abs in `openSQLiteReadOnly` before calling it, whichever is the smaller clean diff; callers propagate the error). Tests: relative path opens successfully (create a temp db with a relative path via t.Chdir), absolute path unchanged, existing DSN tests still pass.
3. **Baseline exit-class consistency:** `baseline set/list/rm` wrap store-open and resolution failures in `operational(...)` (exit 2) exactly like `regress` — a broken store is operational everywhere. Update tests asserting exit codes through `run()`.
4. **Test tightening:** `TestRegressNameSelectorFewerRunsWarns` asserts `Equal 0` instead of `NotEqual 2`.

- [ ] TDD → implement → `go test ./cmd/... ./store/...`, `make cover`, `make lint`, codepolicy → commit `fix(cmd,store): http timeout, readOnlyDSN relative paths, baseline operational exits`

### Task 2: daemon/labels polish

**Files:** `daemon/sessions.go` (`summarizeSession` ~262, `SummarizeSession` ~258), `daemon/server.go` (`handleStreamJSON` ~102), tests alongside.

**Fixes (binding):**

1. **Live `/v1/sessions` surfaces labels:** `(d *Daemon).summarizeSession` populates `SessionSummary.Labels` from the matched run the same way `summarizeGraphs` does (first-wins over the deterministic pick below). Additive `labels,omitempty` — no consumer break. Test: live daemon handler test asserting labels in the sessions JSON.
2. **Deterministic multi-run label pick:** where a session spans multiple runs, pick labels from the run with the lexicographically-lowest `Run.ID` (both in `summarizeGraphs`-backed free functions and the live path — extract one tiny shared helper if that avoids duplication). Test: two runs with different labels in one summary scope → lowest-ID run's labels chosen, stable across map-order permutations (build graphs in both insertion orders).
3. **Canonicalize-once hoist:** `handleStreamJSON` computes `canonical := model.FormatLabels(model.ParseLabels(rawHeader))` once before the scan loop and passes the canonical string per line (the per-line re-canonicalization inside `IngestStreamJSONWithLabels` is idempotent, so the public method's contract is unchanged). Assert existing stream-labels tests still pass unchanged; add no new surface.

- [ ] TDD → implement → `go test ./daemon/`, `make cover`, `make lint`, codepolicy → commit `fix(daemon): labels in live sessions, deterministic multi-run pick, canonicalize-once`

### Task 3: aggregate/store/bench test-and-docs hardening

**Files:** `regress/compare_test.go`, `store/migrate_test.go`, `cmd/catacomb/storeread.go` (+test), `cmd/catacomb/streamjson.go` (`lineObserver`) (+test), `docs/adr/0022-regression-detection-over-repeated-runs.md`, `docs/guide/cli.md`.

**Fixes (binding):**

1. **Negative-median band test:** one `compareMetric` table case with baseline Median < 0 pinning the `math.Abs` rel-band path (e.g. Median -100, P25 -110, P75 -90 → band uses abs).
2. **Convergence test strengthened:** `TestFreshAndV1UpgradeConvergeOnSchema` compares `SELECT name, sql FROM sqlite_master WHERE sql IS NOT NULL ORDER BY name` sets (full DDL incl. indexes), not just table names.
3. **`loadRunGroup` single-pass snapshotting:** replace the per-run `collectSnapshot(graphs, r.ID)` loop (O(runs×graphs) with full re-snapshot each call) with one snapshot pass per graph, bucketing nodes/edges by `RunID` into the selected runs' RunGraphs. Same output (sorted by ID) — assert with the existing loader tests plus a new multi-run ordering test; this is a refactor, outputs must be byte-identical.
4. **`lineObserver` hardening:** cap the internal buffer (e.g. 1 MiB — a session_id line can't legitimately exceed it; on overflow drop the buffer and stop observing) and flush the final unterminated line on Close/teardown so a session id emitted without a trailing newline still triggers the start marker. Tests: unterminated-line flush produces the marker; overflow path stops observation without affecting the child.
5. **Docs/ADR notes:** ADR-0022 Amendments gains one bullet — `nodes` (totals) and `occurrences` (rows) metrics assume higher = regression; a legitimately-grown pipeline trips them and the mitigation is raising `--metric-rel-delta` or reading the finding as informational. cli.md bench section gains one sentence: the start/end marker POSTs are synchronous with a bounded (2s) timeout — a slow/down daemon adds up to ~4s per cell.

- [ ] TDD → implement → `go test ./regress/ ./store/... ./cmd/...`, `make cover`, `make lint`, codepolicy, markdownlint (ADR + cli.md) → commit `fix(cmd,regress,store): loader single-pass, lineObserver hardening, test strengthening; ADR/docs notes`

### Task 4: final review, live-verify, PR, merge

- [ ] Whole-branch review (most capable model) from `git merge-base origin/master HEAD`; fix wave; re-verify.
- [ ] Live-verify: relative `--db` now works for `runs`/`regress`/`baseline list` against a real store; `status` against a hung fake daemon times out within the client timeout; live `/v1/sessions` shows labels (daemon + labeled run + curl).
- [ ] `make cover && make lint && go test ./internal/codepolicy/` + markdownlint (docs + this plan).
- [ ] Push `fix/followups`, open PR `fix: harden follow-ups from PR #100–#103 reviews + pre-existing readOnlyDSN bug`, CI green, squash-merge (authorized).
