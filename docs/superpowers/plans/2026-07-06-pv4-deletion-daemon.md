# PV-4: Deletion Wave II — Daemon, Ingest, Exporters Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Delete the daemon and everything that only exists to feed or manage it (ADR-0026 §3): `daemon/`, `ingest/{hook,otel,streamjson,tail}`, `cdc/`, gRPC/proto, the lifecycle commands, and — resequenced from PV-5 into this wave because they are daemon-sink-driven — `export/{otlp,neo4j,postgres,agentevals,evalview,build}`. The offline path becomes the only path.

**Architecture:** Rewire-then-delete. Tasks 1–2 rework the surviving commands (bench/regress/baseline/export/replay) to be offline-only while the daemon still compiles, so every commit stays green. Task 3 is the deletion. Task 4 slims the store to baselines+records. Task 5 scrubs docs. Resequencing note: the roadmap placed exporter deletion in PV-5; they are deleted here because config sinks and the CDC bus die with the daemon and would leave the exporters orphaned — PV-5 shrinks to repositioning + DeepEval verification.

**Tech Stack:** deletions; no new dependencies. go.mod loses otel-sdk/otlp, grpc, protobuf(+buf), pgx, neo4j-driver.

## Global Constraints

- No comments in Go; 100% file/package/total coverage holds after every task; TDD where behavior changes; golangci-lint clean; every commit builds and passes the full suite.
- Immutable history docs (docs/adr, docs/reviews, docs/plans, docs/specs, docs/superpowers) untouched except nothing.
- KEEP untouched: `mcp/` (stdlib-only stdio server), `repro/` (ADR keep; PV-6 wires offline fingerprints), `evidence/`, `model/`, `reduce/`, `stepkey/`, `phasekey/`, `subgraph/`, `diff/`, `aggregate/`, `regress/`, `pricing/`, `redact/`, `ingest/jsonl`, `export/jsonl`, `integrations/deepeval` (bridge input format is `xjsonl.Snapshot` — preserved by Task 2).
- Decisions baked (plan-owner): `replay` drops `--db` persistence (in-memory + existing `--export-jsonl`); `runs`/`inspect`/`snapshot`/`demo`/`env`/`batchconfig`/`config-resolve` commands are deleted; store schema stays v4 (fresh dbs still create graph tables — dead but harmless; a v5 drop is PV-6 material); `label:` selectors resolve ONLY via runs-dir scanning (graph-store label resolution dies); `--runs-dir` gains the `~/.catacomb/runs` default on regress/baseline (matching bench).

---

### Task 1: bench/regress/baseline go offline-only

**Files:** modify `cmd/catacomb/bench.go` (delete the daemon cell path: `benchPreflight`, `runBenchCell`, `fetchSessionMarkers`, `markState`, `graphEvent`; `--offline` flag removed — offline IS bench; keep `--projects-dir`/`--runs-dir`), `cmd/catacomb/regress.go` + `runsdir.go` (store-path selector resolution dies: `resolveSelector`/`resolveLabelSelector`/`resolveNameSelector` + `loadRunGroup*` in `storeread.go`; the runs-dir path becomes the only path; `--runs-dir` defaults to `~/.catacomb/runs`; `--record`/`name:`/stamps/`--scores` semantics unchanged), `cmd/catacomb/baseline.go` (store-resolution arm dies; `--runs-dir` default likewise), `cmd/catacomb/streamjson.go` (file dies; MOVE `lineObserver` + `execCommand` seam to `childlocal.go`; `newRunCmd`/`newIngestCmd`/`streamForward`/`runChildObserved`/`lossyWriter` deleted — `catacomb run`/`ingest` commands die), root.go registrations.

**Contract:** `catacomb bench <basket>` runs daemonless by default (existing offline tests become THE bench tests; daemon-path tests deleted); `regress --baseline label:… --candidate label:…` works with zero store for label:, slim store for name:/record; parity gate test untouched and green; every existing offline test passes unmodified except flag spelling. Repo rules per Global Constraints.

### Task 2: export/replay retarget to transcripts

**Files:** modify `cmd/catacomb/export.go` — new shape: `catacomb export <transcript.jsonl | evidence-dir> --to jsonl [--out <file>]`; input resolved like regress's evidence loader (a dir → `session.jsonl` + `subagents/` + boundary markers from `meta.json`; a file → single transcript); output via `xjsonl.Snapshot(out, nodes, edges, runs)` — byte-format identical to today's jsonl export (DeepEval bridge contract); store/sink/otlp/postgres/neo4j modes and flags deleted. Modify `cmd/catacomb/replay.go` — `--db` and store persist die; keeps summary print + `--export-jsonl`. Delete `cmd/catacomb/{runs,inspect,snapshot}.go` (+tests) and registrations.

**Contract:** `integrations/deepeval` tests stay green against an export produced from the fixture transcript (add one cmd-level test asserting the snapshot format has kind:run/node/edge lines); replay tests reworked minus persist.

### Task 3: the deletion

**Files:** delete `daemon/`, `ingest/{hook,otel,streamjson,tail}`, `cdc/`, `gen/`, `proto/`, `buf.yaml`, `buf.gen.yaml`, `export/{otlp,neo4j,postgres,agentevals,evalview,build}`, commands `up/down/down_unix/down_windows/restart/status/logs/hook/installhooks/mark/daemon/demo/demo_assets/env/discovery/batchconfig/config_resolve/ownership*` (+tests, +testdata used only by them); config package slims to what survivors consume (inspect usage first: if nothing survives, delete `config/` and its flag plumbing); Makefile proto/buf targets; CI: remove anything referencing deleted paths; go.mod purge via `go mod tidy` (expect −otel −grpc −protobuf −pgx −neo4j).

**Contract:** after this task `go build ./...` compiles a CLI whose commands are exactly: `bench, regress, baseline, trends, diff, subgraph, export, replay, mcp, version` (root test asserts the set); `git grep -l "catacomb/daemon\|catacomb/cdc\|catacomb/ingest/hook\|catacomb/ingest/otel\|catacomb/ingest/streamjson\|catacomb/ingest/tail" -- '*.go'` empty; reduce's CDC delta emission: `reduce` imports `cdc` for `GraphDelta` — MOVE the delta type into `reduce` (or drop emission entirely if `DrainDeltas` has no surviving consumer — inspect; prefer dropping dead machinery, keep the fuzz invariant tests green); pricing/drift note below.

**Drift decision:** `ingest/drift` loses its daemon surfacing. Contract: preserve unknown-shape visibility offline — if `ingest/jsonl` exposes (or can cheaply expose) an unknown-record count, `parseTranscripts` warns to stderr when >0 and `ingest/drift` slims to the jsonl reasons + version watchlist; if that requires reworking the parser's return shape beyond a small change, DELETE `ingest/drift` and record the decision — PV-6 re-introduces drift visibility properly (per amended ADR-0025 either way).

### Task 4: store slim

**Files:** `store/store.go` (interface shrinks to Open/Close/migrate + `UpsertBaseline/GetBaseline/ListBaselines/DeleteBaseline/AppendRegressResult/RegressResultsFor`), `store/sqlite.go`, `store/memory/`, `store/storetest/`, `store/contract_test.go`, `cmd/catacomb/storeread.go` (drop graph loaders; keep `openReadStore`/`openWriteStore`), `cmd/catacomb/storepath.go` (keep — db path resolution).

**Contract:** schema v4 unchanged; all baseline/record/trends/regress tests green; `git grep -n "Persist(\|ObservationsForExecution" -- '*.go'` empty.

### Task 5: docs scrub + gates

**Files:** `docs/guide/{README,getting-started,ingestion,configuration,privacy-and-operations,cli,workflows,concepts}.md`, root `README.md`, `AGENTS.md` (status + architecture paragraph + CI table), `Dockerfile`/`.dockerignore` if they reference deleted paths.

**Contract:** ingestion.md rewritten around transcript capture + the vendor-plugin pointer (four-source content is historical → point at ADR-0026 supersession map); every documented command/flag verified against `--help`; full gates: markdownlint all md, `go build ./... && go test ./...`, `make cover` 100%, `golangci-lint run --timeout=5m`.

---

## Self-review checklist

- Wave order keeps every commit green: T1/T2 rewire while daemon still exists; T3 deletes; T4 slims; T5 documents.
- The parity gate and all PV-1/PV-2 offline tests survive unmodified (flag removal excepted).
- go.mod after T3 contains no otel/grpc/protobuf/pgx/neo4j; binary size drop noted in the PR body.
