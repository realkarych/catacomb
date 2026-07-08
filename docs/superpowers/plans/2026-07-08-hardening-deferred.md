# Hardening: fix all pre-existing + deferred items

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Three tight tasks, review between. Steps use `- [ ]`.

**Goal:** close the real deferred/pre-existing items accumulated across the ADR-0026 pivot (PV-1…PV-6). Only genuine defects, dead code, and real test-robustness gaps — cosmetic nits are deliberately left (see Non-goals) to avoid churn.

**Scope discipline:** every change on `master` (15f638c) verified real by audit. No behavior change to the eval gate itself; correctness/cleanliness/coverage only.

## Global Constraints

- No comments in Go (only `//go:build`/`//go:embed`/`//go:generate`); enforced by `internal/codepolicy`.
- 100% file/package/total coverage after every task; TDD failing-first where behavior changes; `golangci-lint` clean; every commit builds + full suite green.
- Go 1.26 (so `json:"...,omitzero"` is available).
- Before merge: markdownlint ALL tracked md; and verify EVERY CI check conclusion is SUCCESS (not just `--watch` exit) — `gh pr view N --json statusCheckRollup`.

## Non-goals (deliberately left — not defects)

- `offlineEnv` sets `CATACOMB_RUN_ID` unconditionally — `cell.RunID` is never empty; correct as-is.
- `scores.go` non-numeric-value error / warning wording — already line-numbered + field-named.
- `store.isMissingTable` substring match — pragmatic standard for the modernc driver (no typed error), load-bearing for migration; leave.
- `redactLines` blank-line/newline normalization; `ScanRuns` symlink skip; parity 100-yr window; `CostUSD` recorded on failure paths — all harmless/intentional.
- `model.ReproMeta`, `Run.Repro`, `reduce.applyReproMeta`, `redact.Run` repro handling — LIVE (populated from transcript attrs, exported, redacted). Do NOT touch.

---

## Task 1: correctness + quality + doc fixes

**Files:** `cmd/catacomb/transcripts.go`, `cmd/catacomb/offline.go`, `cmd/catacomb/regress.go`, `model/baseline.go`, `regress/regress.go` (+ their tests).

- [ ] **1a — `resolveTranscriptsRetry` false-success + trailing sleep** (`transcripts.go`). Current: `attempts<=0` runs the loop zero times → returns `(transcriptSet{}, nil)` — a nil-error empty result. And it `sleepFn(delay)` after the final failed attempt (wasted delay on the failure path). Fix: if `attempts < 1`, treat as 1 attempt (or return a clear error) — pick "clamp to ≥1 attempt" so callers passing 0 still get a real resolve+error; and do not sleep after the last attempt (`if i < attempts-1 { sleepFn(delay) }`). Test: `attempts=0` returns a non-nil error (no false success); a 2-attempt failure calls `sleepFn` exactly once; success path unchanged.
- [ ] **1b — `loadGraphOffline` dead alloc + caller-slice mutation** (`offline.go`). Replace `g := reduce.NewGraph(); if pricer != nil { g = reduce.NewGraphWithPricer(pricer) }` with an if/else (no throwaway alloc). Build the observation slice without mutating the caller's `extra` (copy each element into a local before setting `ExecutionID`/`Seq`, or append copies). Preserve the exact resulting graph (Seq numbering identical — the existing determinism/marker tests must stay green unchanged).
- [ ] **1c — `Stamps` `omitempty`→`omitzero`** (`model/baseline.go:11`, `regress/regress.go:28`). `omitempty` on a struct field is a no-op → a zero `Stamps` serializes as `"stamps":{}`. Use `json:"stamps,omitzero"` (Go 1.26) so a zero `Stamps` is omitted entirely. Keep the inner-string `omitempty` on `model/stamps.go`. Test: a `Baseline`/`Record` with zero `Stamps` marshals WITHOUT a `stamps` key; with non-zero stamps the key is present; legacy bodies still unmarshal.
- [ ] **1d — `--strict` help text accuracy** (`regress.go:69`). The flag now also refuses on missing/mismatched version stamps (`checkBaselineStamps`→`stampIssue`), not only insufficient data; and stamp refusal is an operational error. Update the flag help to state both effects and the ACTUAL exit code (verify: run the strict-stamp-mismatch path and confirm exit code, then word the help to match — do not claim "exit 1" if it is exit 2).

**Contract:** `go build ./... && go test ./...`; `make cover` 100%; `golangci-lint run`. Commit `fix: retry guard, dead alloc, omitzero stamps, accurate --strict help (hardening)`.

---

## Task 2: remove dead repro code

**Files:** delete `repro/repro.go` + `repro/repro_test.go` (whole package); modify `model/baseline.go` (drop the `Repro` field) + `cmd/catacomb/baseline_test.go` (the `assert.Empty(t, b.Repro)` at ~:258 — remove it, it only ever asserted the field is empty).

- [ ] **2a — verify** (grep) that `repro` the PACKAGE has zero importers except `repro/repro_test.go`, and `Baseline.Repro` has zero prod readers (only the one test assertion). Record the grep in the report.
- [ ] **2b — delete** the `repro/` package.
- [ ] **2c — remove** `model.Baseline.Repro` (`map[string]*ReproMeta`) and the stale test assertion. Old persisted baseline JSON carrying a `"repro"` key must still unmarshal (unknown key ignored) — add/keep a legacy-body test proving it.
- [ ] KEEP `model.ReproMeta`, `model.Run.Repro`, `reduce.applyReproMeta`, `redact.Run` — they are live.

**Contract:** `git grep -l 'catacomb/repro' -- '*.go'` empty after; build+test+cover 100%+lint green. Commit `refactor: delete dead repro fingerprint package + unused Baseline.Repro (hardening)`.

---

## Task 3: close real test-robustness gaps

**Files:** `cmd/catacomb/childlocal_test.go`, `cmd/catacomb/offline_test.go` (or evidence-adjacent), `store/migrate_test.go`.

- [ ] **3a — `streamPeek` cost-only-from-`result`** (`childlocal_test.go`): the guard `if e.Type == "result" && e.TotalCostUSD != nil` (childlocal.go:32) is untested on its negative side — feed a non-`result` event carrying `total_cost_usd` and assert `costUSD` stays nil; then a `result` event sets it. (Deleting the `e.Type=="result"` clause must break a test.)
- [ ] **3b — drift + version warnings coexist** (`offline_test.go`): one transcript containing BOTH an unrecognized record AND `claude_code_version` > `TestedClaudeCodeVersion` → assert BOTH the `unrecognized transcript record` and the `newer than tested` warnings appear (they share the `driftOut` seam; lock in that neither suppresses the other).
- [ ] **3c — exact user_version=4 → v5 open path** (`migrate_test.go`): seed a db stamped exactly `user_version=4` (raw SQL) with a graph table present + a baseline row, open through the real `OpenSQLite`, assert graph tables dropped, baseline survives, `user_version==5`. (The v4→v5 step is currently only exercised via the v3→v5 chain.)

**Contract:** each new test fails if its guarded behavior regresses; build+test+cover 100%+lint. Commit `test: cover cost-guard negative path, drift+version coexistence, v4→v5 open (hardening)`.

---

## Task 4 (controller): docs + gates + merge

- Roadmap/README note: the daemon-era `repro/` fingerprint *computer* was removed as dead (the reducer now reads pre-computed hashes from transcript attrs); `Baseline.Repro` dropped.
- Full gates + markdownlint all md; open PR; verify all check conclusions SUCCESS; squash-merge.

## Self-review checklist

- Only audited-real items changed; Non-goals untouched.
- `repro/` gone; `ReproMeta`/`Run.Repro` intact; legacy baseline JSON still unmarshals.
- Every new test is a real guard (deleting the guarded line breaks it).
