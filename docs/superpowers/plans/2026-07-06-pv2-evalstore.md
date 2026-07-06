# PV-2: Offline Baselines, Version Stamps, Scores File Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `baseline set`/`regress name:`/`--record` work offline over evidence dirs; baselines and records carry version stamps (ADR-0026 §6, closing the ADR-0017 gap); external scores feed annotation gates via a file; three PV-1 backlog fixes land.

**Architecture:** Additive. Baselines/records stay JSON blobs in SQLite (`baselines`, `regress_results`) — new fields need NO schema migration (`currentSchemaVersion` stays 4; old rows unmarshal with zero-valued new fields). Offline `name:` resolution opens the store for the baselines table only — the graph tables are never read on the `--runs-dir` path. Scores are applied to in-memory `aggregate.RunGraph` node annotations before aggregation, so the existing `--annotation` gate machinery works unchanged.

**Tech Stack:** Go 1.26; no new dependencies.

## Global Constraints

- No comments in Go (only `//go:build`, `//go:embed`, `//go:generate`); enforced by `internal/codepolicy`.
- 100% file/package/total coverage; every error branch tested; TDD failing-first.
- `forbidigo` bans `time.Sleep` calls; `unparam` is strict (drop dead returns rather than `//nolint`).
- Module `github.com/realkarych/catacomb`. Conventional commits + trailer.
- Facts: `model.Baseline{Name, RunIDs, Selector, CreatedAt, Repro}` (model/baseline.go); `model.RegressResult{Baseline, Seq, Body}`; store fns `UpsertBaseline/GetBaseline/ListBaselines/DeleteBaseline/AppendRegressResult/RegressResultsFor` (store/sqlite.go:518-618); DDL `baselines(name TEXT PK, body TEXT)`, `regress_results(baseline, seq, body)` (store/migrate.go:17-19); CLI version var `Version = "dev"` (cmd/catacomb/version.go:5); stepkey scheme const `scheme = "stepkey/v1"` unexported (stepkey/stepkey.go:18); record body struct + `--record` flow live in cmd/catacomb/regress.go (`appendRecord`, `recordBaselineName`) and are rendered by cmd/catacomb/trends.go; PV-1 surfaces: `evidence` pkg, `evidenceRunGraph`/`resolveSelectorRunsDir` (cmd/catacomb/runsdir.go), `offlineEnv` (cmd/catacomb/bench.go), `boundaryObservations`/`loadGraphOffline` (cmd/catacomb/offline.go).

---

### Task 1: stamps in the model + exported stepkey scheme

**Files:**

- Modify: `stepkey/stepkey.go` (rename unexported `scheme` const to exported `Scheme`, update the two internal uses)
- Modify: `model/baseline.go`
- Create: `model/stamps.go`
- Test: `model/stamps_test.go`, extend `model/baseline_test.go`, `stepkey/stepkey_test.go`

**Interfaces (produce exactly):**

- `stepkey.Scheme` — exported const, value `"stepkey/v1"`, used by the two hash sites.
- `type Stamps struct { CatacombVersion string json:"catacomb_version,omitempty"; StepKeyScheme string json:"stepkey_scheme,omitempty" }` in `model/stamps.go`.
- `func (s Stamps) Zero() bool` — true when both fields empty.
- `func (s Stamps) Mismatch(other Stamps) bool` — true when either field differs (zero-vs-nonzero counts as differing).
- `model.Baseline` gains `RunsDir string json:"runs_dir,omitempty"` and `Stamps Stamps json:"stamps,omitempty"`.

**Tests:** stamps zero/mismatch truth table; Baseline JSON roundtrip with and without new fields (old-body compatibility: unmarshal a legacy JSON body without the fields → zero values); stepkey golden values unchanged by the rename (existing tests must not change hashes).

---

### Task 2: PV-1 backlog fixes (evidence Rel guard, offline CATACOMB_RUN_ID, Run overlay)

**Files:**

- Modify: `evidence/evidence.go` (`Write`), `cmd/catacomb/bench.go` (`offlineEnv`), `cmd/catacomb/runsdir.go` (`evidenceRunGraph`)
- Test: extend `evidence/evidence_test.go`, `cmd/catacomb/bench_offline_test.go`, `cmd/catacomb/runsdir_test.go`

**Interfaces:**

- `evidence.Write` rejects any `SourceFile.Rel` that is absolute or escapes the dir: guard with `filepath.IsLocal(f.Rel)`; error text contains `"rel"` and the offending value.
- `offlineEnv` additionally exports `CATACOMB_RUN_ID=<cell.RunID>` (daemon-path parity; daemon exports it via `runChildObserved`).
- `evidenceRunGraph` overlays `Run.Status` and `Run.ModelID` from the loaded graph's `RunsSnapshot()` (first run in the snapshot); marker-window `StartedAt/EndedAt` stay authoritative (do NOT take snapshot times).

**Tests:** `Write` with `Rel: "../x"` and an absolute path → error, dir untouched; offline cell env carries both `CATACOMB_LABELS` and `CATACOMB_RUN_ID` (assert via helper child echoing env or by unit-testing `offlineEnv`); `evidenceRunGraph` returns Run with Status/ModelID from a fixture whose transcript finalizes (use `testdata/session_marked.jsonl`; assert non-empty ModelID if the fixture carries one, else construct expectation from the snapshot — pin actual values in the test after one exploratory run).

---

### Task 3: offline `baseline set --runs-dir`

**Files:**

- Modify: `cmd/catacomb/baseline.go`
- Test: extend `cmd/catacomb/baseline_test.go`

**Interfaces:**

- `baseline set <name> --label k=v[,…] --runs-dir <dir>` — resolves the group by scanning `evidence.ScanRuns(dir)` + `evidence.MatchLabels` (same semantics as regress `label:`); persists `model.Baseline{Name, RunIDs: metas' RunIDs (sorted), Selector: parsed labels, CreatedAt: nowFn-equivalent already used, RunsDir: dir, Stamps: currentStamps(), Repro: nil}` via `UpsertBaseline` (store open read-write as today; graph tables untouched).
- `func currentStamps() model.Stamps` in cmd (uses `Version` + `stepkey.Scheme`) — shared by Task 4.
- Empty match → operational error naming the selector. Without `--runs-dir` the existing store-resolution path is byte-identical (stamps are ALSO added to store-path baselines — `currentStamps()` in both branches).
- `baseline list` unchanged except it must roundtrip new fields transparently (no rendering changes required; extend one assertion that stamps survive list→JSON).

**Tests:** offline set persists correct RunIDs/Selector/RunsDir/Stamps (open store read-only and `GetBaseline`); empty match errors; store-path set now carries stamps; legacy baseline row (hand-inserted JSON without stamps) still lists.

---

### Task 4: `regress name:` offline + stamps check + `--record` offline

**Files:**

- Modify: `cmd/catacomb/regress.go`, `cmd/catacomb/runsdir.go`
- Test: extend `cmd/catacomb/runsdir_test.go`, `cmd/catacomb/regress_test.go`

**Interfaces:**

- `resolveSelectorRunsDir` regains the 3-value shape `([]aggregate.RunGraph, model.Baseline, error)` — now legitimate (`unparam` satisfied because name: returns a real Baseline). `name:<b>`: open store read-only (baselines table only), `GetBaseline`; missing → the existing `ErrBaselineNotFound` idiom; resolve each `RunID` as `<runsDir>/<RunID>` evidence dir (runs-dir precedence: the `--runs-dir` flag wins; if flag set and baseline.RunsDir differs, warn to stderr); each dir loaded via `evidenceRunGraph`; missing dir → operational error naming run id and dir.
- Stamps check in `runRegressRunsDir` (and the store path when the baseline came from `name:`): `b.Stamps.Zero()` → warn "baseline <name> has no version stamps (pre-PV-2)"; `b.Stamps.Mismatch(currentStamps())` → warn with both stamp pairs; under `--strict`, both cases become operational errors. Warnings go to errOut once per baseline.
- `--record` with `--runs-dir`: now allowed — opens the store read-write ONLY for `appendRecord` (graph tables untouched); the record body gains `Stamps model.Stamps json:"stamps,omitempty"` (record struct in regress.go; trends tolerates unknown fields — verify by running trends test).
- The PV-1 error strings ("offline baselines land in PV-2", "--record requires the store") are deleted with their tests replaced by the working-path tests.

**Tests:** name: offline resolves groups (baseline written by Task 3 helper or direct `UpsertBaseline`); missing baseline; missing run dir errors with run id; runs-dir precedence warn; stamps zero-warn, mismatch-warn, strict-refuse (both cases); `--record --runs-dir` appends a record whose body carries stamps (read back via `RegressResultsFor`); full offline gate flow still green (existing parity test untouched).

---

### Task 5: `--scores` file → annotation gates offline

**Files:**

- Create: `cmd/catacomb/scores.go`
- Modify: `cmd/catacomb/regress.go` (flag + application point)
- Test: `cmd/catacomb/scores_test.go`, extend `cmd/catacomb/regress_test.go`

**Interfaces:**

- Flag `--scores <file.jsonl>` on regress (default ""); usable with both `--runs-dir` and store paths.
- Line schema (one JSON object per line): `{"step_key": string (required), "key": string (required, owner.key form — validate with the same rule as parseAnnotationSpec's key check), "value": number (required), "run_id": string (optional — when set, applies only to the RunGraph whose Run.ID matches)}`. Malformed line or invalid key/step_key-empty → operational error naming the line number. Blank lines skipped.
- `func loadScores(path string) ([]scoreEntry, error)`; `func applyScores(groups [][]aggregate.RunGraph, entries []scoreEntry) (applied, unmatched int)` — sets `node.Annotations[key] = value` (float64) on every node whose `StepKey` matches (and run matches when scoped); applied after BOTH groups are resolved, before `regressReport`. `unmatched > 0` → single stderr warning with the count.
- End-to-end: scores + `--annotation owner.quality:higher-better` produce annotation Findings offline (drop from 1.0×3 to 0.0×3 across groups → regression), exercising `compareAnnotation` through the real command.

**Tests:** loader (valid, malformed JSON, bad key form, missing step_key, blank lines, file absent); application (matching, run-scoped, unmatched count); end-to-end offline annotation regression + A-vs-A annotation ok; store path accepts the flag too (smoke: no error when file valid).

---

### Task 6: docs + gates

**Files:**

- Modify: `docs/guide/cli.md` (baseline `--runs-dir`, regress `name:` offline, `--record` offline, `--scores`, stamps + `--strict` semantics; fix the PV-1 sentence that said annotations never fire under `--runs-dir`), `docs/guide/workflows.md` (extend the daemonless section: pin a baseline, record trends, feed external scores)
- Gates: `npx --yes markdownlint-cli@0.49.0 'docs/**/*.md' --config .markdownlint.json`; `go build ./... && go test ./...`; `make cover` (100%); `golangci-lint run --timeout=5m`.

**Tests:** documented flags/wording verified against code before writing (same discipline as PV-1 Task 8).

---

## Self-review checklist

- Stamps flow one way: `currentStamps()` (cmd) → baseline/record bodies (model/store) → checked in regress; stepkey.Scheme is the single scheme source.
- No schema migration introduced; legacy JSON bodies covered by tests in Tasks 1, 3, 4.
- The parity gate and all PV-1 tests remain untouched and green.
