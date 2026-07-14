# PR-G: `regress --record` + `catacomb trends` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Longitudinal memory for a named baseline (P1 roadmap PR-G): `regress --record` persists each comparison outcome; `catacomb trends <baseline>` shows the verdict/metric evolution chronologically — the "did the candidate line get better or worse over time" view, terminal-only.

**Architecture:** a `regress.Record` envelope (candidate selector, thresholds, annotation specs WITH direction — closing the PR-F follow-up — the full `Report`, and a `CreatedAt` stamped via the CLI `nowFn` seam) marshaled to JSON and stored opaquely in a new `regress_results` table (schema v3 via the ADR-0017 runner; store stays model-typed — body is `json.RawMessage`). `trends` decodes records read-only and renders a chronological table or JSON. No daemon surface, no reducer changes.

**Tech Stack:** Go 1.26, stdlib, testify.

## Global Constraints

- No comments in Go code; 100% coverage; TDD RED first; `make lint` 0 (cache clean if stale); codepolicy; gofumpt; testify; no time.Sleep; deterministic rendering (records ordered by per-baseline seq; no wall clock outside the `nowFn` seam).
- Store layering preserved: the store speaks `model` types only (`model.RegressResult{Baseline string; Seq int; Body json.RawMessage}`); the `regress.Record` schema is owned by the `regress` package and (de)serialized at the CLI layer.
- `--record` is only valid with `--baseline name:<x>` (recording against an ad-hoc label selector has no stable identity) — otherwise operational exit 2.
- Recording must not change the comparison outcome or exit code: record-append happens after rendering; an append failure is an operational error (exit 2) with the report already printed.
- markdownlint on touched docs.

---

### Task 1: schema v3 + `model.RegressResult` + store methods

**Files:**

- Modify: `store/migrate.go` (currentSchemaVersion=3, `applySchemaV3`), `store/store.go` (interface), `store/sqlite.go`, `store/memory/memory.go`
- Create: `model/regressresult.go`
- Test: store tests incl. migration v2→v3 and fresh/upgrade DDL convergence (mirror the existing full-DDL convergence test)

**Interfaces (exact):**

```go
package model

type RegressResult struct {
	Baseline  string          `json:"baseline"`
	Seq       int             `json:"seq"`
	Body      json.RawMessage `json:"body"`
}
```

Store interface additions: `AppendRegressResult(baseline string, body json.RawMessage) (int, error)` (assigns per-baseline seq = MAX(seq)+1 starting at 1, in one transaction); `RegressResultsFor(baseline string) ([]model.RegressResult, error)` (ascending seq; empty slice-or-nil for unknown baseline — match the ListBaselines nil convention). Table: `regress_results(baseline TEXT NOT NULL, seq INTEGER NOT NULL, body TEXT NOT NULL, PRIMARY KEY(baseline, seq))`.

- [ ] **Step 1: Failing tests** — append assigns 1,2,3 per baseline independently (two baselines interleaved); results ascending; unknown baseline empty; migration v2→v3 (build a v2 fixture per the existing migrate-test pattern) + fresh/upgrade convergence via full `sqlite_master` DDL comparison; `ErrSchemaTooNew` (v4 refused); memory-store parity via the shared contract test file (storetest); error paths per existing store test conventions (scan/iter/exec/decode).
- [ ] **Step 2:** RED → implement (sqlite + memory + fake-store stubs where compilation demands) → GREEN; `go test ./store/... ./model/`, `make cover`, `make lint`, codepolicy.
- [ ] **Step 3: Commit** — `feat(store): schema v3 — regress_results table (per-baseline sequenced records)`

### Task 2: `regress.Record` + `--record` flag + `catacomb trends`

**Files:**

- Modify: `regress/regress.go` (Record type only — no Compare changes), `cmd/catacomb/regress.go` (`--record` flag + append path + read-write open when recording), `cmd/catacomb/root.go`
- Create: `cmd/catacomb/trends.go`
- Test: `cmd/catacomb/regress_test.go`, `cmd/catacomb/trends_test.go`

**Interfaces (exact):**

```go
package regress

type Record struct {
	CandidateSelector string           `json:"candidate_selector"`
	Thresholds        Thresholds       `json:"thresholds"`
	Annotations       []AnnotationSpec `json:"annotations,omitempty"`
	Report            Report           `json:"report"`
	CreatedAt         time.Time        `json:"created_at"`
}
```

**Binding contract:**

- `regress --record`: boolean flag. Validation (operational exit 2): requires the baseline selector to be `name:<x>` form. Store opened READ-WRITE (`store.OpenSQLite`) when `--record` is set, read-only otherwise (unchanged) — note the write-open triggers the v3 migration; the record is appended AFTER the report renders, keyed by the baseline name, body = marshaled `regress.Record` (CreatedAt from the existing `nowFn` seam). Append failure → operational (report already printed; exit 2 overrides the verdict exit since the user asked for durable recording and didn't get it — document this precedence in cli.md).
- `catacomb trends <baseline> [--db] [--metric <name>] [--json]` (Advanced group): read-only open; unknown baseline name OR baseline with zero records → operational exit 2 with distinct messages (check `GetBaseline` for existence first). Default table: one row per record — `SEQ / CREATED / CANDIDATE / VERDICT / REGRESSIONS / INSUFFICIENT / DURATION_MS / COST_USD / ERROR_RATE` where the three metric columns show the CANDIDATE value from the total-scope finding with that metric (`-` when absent). `--metric <name>` (any finding metric incl. `ann:<key>` and phase/step metrics is out of scope — TOTAL-scope metrics only, validate against the known set duration_ms/cost_usd/tokens_in/tokens_out/nodes/error_rate, else operational) narrows the table to `SEQ / CREATED / CANDIDATE / VERDICT / BASELINE-VALUE / CANDIDATE-VALUE / BAND`. `--json` emits the decoded `[]regress.Record` with seq (wrap: `{"seq":N,"record":{...}}`) — full fidelity incl. annotation directions. Malformed stored body → operational error naming the seq (corrupt history must not render silently).
- Rendering deterministic: records in seq order; `text/tabwriter` consistent with existing tables; CreatedAt rendered RFC3339 UTC.
- Tests: record flow end-to-end over a seeded store (baseline set → regress --record twice with different candidate groups → trends shows 2 rows in order, verdicts/medians correct, --json round-trips into []regress.Record with annotations+directions when `--annotation` was used); --record with label: baseline selector → exit 2; trends unknown baseline → exit 2; zero records → exit 2 distinct message; --metric validation + narrowed table; malformed body → operational; recording does not alter the printed report or (on success) the exit code — pin exit 1 regression + successful record.

- [ ] **Step 1: Failing tests** → **Step 2:** implement → GREEN; `go test ./cmd/... ./regress/ ./store/...`, `make cover`, `make lint`, codepolicy.
- [ ] **Step 3: Commit** — `feat(cmd,regress): regress --record + catacomb trends (ADR-0022 P1 longitudinal memory)`

### Task 3: docs, final review, live-verify, PR, merge

- [ ] Docs: cli.md (`--record` on regress incl. exit-code precedence note + `trends` entry with both table forms); workflows.md (extend the CI walkthrough: `--record` each gate run, `trends` to inspect drift over time). markdownlint clean.
- [ ] Whole-branch review (most capable model) from `git merge-base origin/master HEAD`; fix wave; re-verify.
- [ ] Live-verify: real daemon + two labeled groups + `baseline set` → `regress --record --annotation ...` twice (one pass, one induced regression) → `trends` table shows both with correct verdicts and medians; `--metric error_rate` narrows; `--json` carries annotation direction; v2→v3 migration exercised on the existing live store.
- [ ] `make cover && make lint && codepolicy` + markdownlint (docs + this plan).
- [ ] Push `feat/trends`, open PR `feat: regress --record + catacomb trends (P1)`, CI green, squash-merge (authorized).
