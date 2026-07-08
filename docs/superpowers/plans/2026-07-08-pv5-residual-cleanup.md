# PV-5: Residual Cleanup + Version Watchlist Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Finish the ADR-0026 pivot's tail: drop the daemon-era vestiges PV-4 left behind (`repro` OTLP fields, dead graph-table DDL), reintroduce the version watchlist offline (amended ADR-0025), and reconcile the roadmap (PV-4 absorbed PV-5's original exporter-deletion + docs rewrite).

**Architecture:** Small, mostly-mechanical cleanups plus one real feature (schema v5 dropping the dead graph tables) and one reintroduction (version-ceiling drift warning wired into the offline transcript-parse path). Every commit stays green; the offline gate's behavior is unchanged except the two new drift/version stderr signals.

**Tech Stack:** no new dependencies.

## Global Constraints

- No comments in Go (only `//go:build`, `//go:embed`, `//go:generate`); enforced by `internal/codepolicy`.
- 100% file/package/total coverage holds after every task; TDD failing-first; golangci-lint clean; every commit builds + full suite green.
- Immutable history docs (docs/adr, docs/reviews, docs/plans, docs/specs) untouched EXCEPT the pivot-roadmap reconciliation in Task 4 (which is a superpowers/plans doc — the roadmap itself, editable) and the ADR-0025/0026 status pass.
- Facts (verified on master 8c446d8): `repro/repro.go:24-27` `Config{OTLPEndpoint, OTLPProject, TranscriptDir}` — repro flows through `redact/run.go`; `store/migrate.go` runs a v1→v4 chain whose base `schema` creates `nodes`/`observations`/`runs` and whose v4 (`applySchemaV4`) scrubs them; the offline gate uses ONLY `baselines` + `regress_results`; the version watchlist (`drift.TestedClaudeCodeVersion`/`NewerThanTested`/`CompareVersions`) was deleted in PV-4 and is owed back per amended ADR-0025; `ingest/jsonl` already returns `drift.Counts`; `parseTranscripts`/`warnDrift`/`driftOut` seam exists in `cmd/catacomb/offline.go`; transcript records carry a Claude Code version field (confirm the exact JSON key in `ingest/jsonl/jsonl.go` + testdata before wiring).

---

### Task 1: drop repro's dead OTLP fields

**Files:** modify `repro/repro.go` (remove `OTLPEndpoint`/`OTLPProject` from `Config`); modify any test asserting them; grep `OTLPEndpoint|OTLPProject` first.

**Contract:** `git grep -n "OTLPEndpoint\|OTLPProject" -- '*.go'` empty after; `Config` = `{TranscriptDir}` only (or whatever non-OTLP fields survive); redact/run.go still compiles and its repro-carrying tests pass; 100% coverage held. If `TranscriptDir` is itself now unset by any producer, note it but KEEP it (repro population is PV-6 per ADR-0026) — only the OTLP fields are unambiguously dead.

---

### Task 2: schema v5 — drop the dead graph tables

**Files:** modify `store/migrate.go` (add v5 migration + bump `currentSchemaVersion` to 5; the base `schema` for fresh dbs stops creating `nodes`/`observations`/`runs`; v5 for existing dbs `DROP TABLE IF EXISTS` those three + any indices, then `VACUUM`); `store/sqlite.go` if the base DDL lives there; extend `store/migrate_test.go`.

**Contract:**
- Fresh db (`OpenSQLite` on a new path) creates EXACTLY `baselines` + `regress_results` (+ sqlite internal) — assert via `sqlite_master` query in a test.
- An old db seeded (raw SQL) with populated `nodes`/`observations`/`runs` + a baseline migrates to v5: graph tables gone, baseline row survives, `PRAGMA user_version` == 5. Secrets-at-rest note: dropping the tables removes their bytes; keep the `VACUUM` so freed pages are reclaimed (honors ADR-0024 for a pre-pivot db upgrading — better than v4's scrub since the data leaves entirely). Keep the v4 scrub migration in the chain (an old db still runs v4 before v5; harmless — the scrub then the drop; test the full v_old→v5 path).
- `ErrSchemaTooNew`/`ErrSchemaOutdated` semantics unchanged; write-path-triggers-migration unchanged.
- Full suite + 100% coverage; every new branch (drop when tables absent vs present) tested.

**Decision to record in the report:** whether the base `schema` const was split (offline base = 2 tables) or the graph-table creation was simply removed; and confirm the v4 scrub still has its own tests exercising the scrub logic on a v3 db (it must — the migration code survives).

---

### Task 3: reintroduce the version watchlist offline (ADR-0025)

**Files:** modify `ingest/drift/drift.go` (restore `TestedClaudeCodeVersion` const + `NewerThanTested(v string) bool` + `CompareVersions` — port from git history `git show 8c446d8~N:ingest/drift/drift.go` or the pre-PV-4 version; keep it minimal, semver-ish compare with the existing test style); modify `ingest/jsonl/jsonl.go` to surface the observed Claude Code version (it likely already parses it into an attr — expose a max-observed version through the `drift.Counts` return or a sibling return); wire `cmd/catacomb/offline.go` `warnDrift` (or a sibling `warnVersion`) to emit a stderr line when the observed version is newer than tested; set `driftOut` as the sink (same seam).

**Contract:**
- A transcript whose Claude Code version exceeds `TestedClaudeCodeVersion` → stderr warning naming observed + tested version, on every transcript-parsing command (bench/regress/export/replay/diff/subgraph — same reach as the unknown-record warning after PV-4's loadGraph reroute).
- A transcript at/below the ceiling → no version warning.
- `TestedClaudeCodeVersion` set to the current tested ceiling (reuse the value from git history unless a newer CC version is evidenced in testdata; record the choice).
- Unknown-record drift warning (PV-4) unchanged; both signals coexist.
- TDD; 100% coverage (version-compare edge cases: equal, older, newer, malformed → treat malformed as not-newer + no crash); lint.

**If wiring the version through the parser balloons the diff** (parser return-shape change rippling widely): STOP, implement the minimal viable version — parse the version in `parseTranscripts` directly from the already-parsed observations' attrs rather than threading it through `ingest/jsonl`'s return — and record the deviation.

---

### Task 4: docs + roadmap reconciliation + ADR status pass + gates

**Files:** modify `docs/superpowers/plans/2026-07-06-pivot-roadmap.md` (mark PV-5's original exporter-deletion/docs-rewrite as folded into PV-4; redefine the shipped PV-5 as this residual+watchlist wave; PV-6 stays = extended calibration); modify `docs/adr/0026-...md` supersession map / ADR-0025 amendment note if it now misstates the watchlist as deferred (it's reintroduced here); modify `docs/guide/{cli,privacy-and-operations,ingestion}.md` to document the version-ceiling warning alongside the unknown-record warning; `AGENTS.md` status line (PV-5 landed, PV-6 next).

**Contract:** documented behavior matches code (verify version + unknown-record warnings against a live run); markdownlint all md; `go build ./... && go test ./...`; `make cover` 100%; `golangci-lint run --timeout=5m`; `git grep -n "OTLPEndpoint\|OTLPProject" -- '*.go'` empty; fresh-db schema test green.

---

## Self-review checklist

- The two offline stderr signals (unknown-record count, version-ceiling) reach the same six transcript-parsing commands and share the `driftOut` seam.
- Schema v5: fresh db = 2 tables; old db migrates and drops graph tables with VACUUM; v4 scrub tests intact.
- Roadmap honestly reflects that PV-4 absorbed the original PV-5 deletion scope; no plan doc claims unbuilt work that shipped.
