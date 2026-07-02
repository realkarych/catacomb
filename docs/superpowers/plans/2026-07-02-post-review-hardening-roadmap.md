# Post-Review Hardening Roadmap — Implementation Plan

> **For agentic workers:** This is the PROGRAM-level plan. Each PR gets its own detailed plan (authored just-in-time via superpowers:writing-plans) and is executed with superpowers:subagent-driven-development.

**Goal:** Close the four gaps the post-P0 CTO review (`docs/reviews/2026-07-02-post-p0-cto-design-review.md`) found between "implemented" and "trustworthy": a rate gate that cannot fire at its own support floor, secrets at rest, silent upstream-format drift, and zero operational validation.

**Architecture:** three ADR-scoped hardening PRs plus a hygiene batch and a validation run — no new product surface, no reducer/identity changes, no viewer or exporter work.

## Dependency DAG

```text
PR-I (docs: review + ADR-0023/0024/0025 + this roadmap)
PR-J (gate sensitivity, ADR-0023)        ── after PR-I
PR-K (format drift detection, ADR-0025)  ── independent code, lands after PR-J
PR-L (secrets at rest, ADR-0024)         ── independent code, lands after PR-K (schema v4)
PR-M (hygiene batch)                     ── last code PR
V-1  (dogfood calibration run)           ── after PR-J; results doc PR
```

Execution order: PR-I → PR-J → PR-K → PR-L → PR-M → V-1 (serial; review discipline and shared CLI/docs files argue against parallel landing). V-1 may run any time after PR-J merges.

## PR-I — Docs: review, ADRs, roadmap

The review document, ADR-0023 (regression gate sensitivity at small run counts), ADR-0024 (secrets at rest — write-path redaction), ADR-0025 (capture format drift detection), ADR index + ADR-0022 "Amended by" pointer, this roadmap. Docs-only; markdownlint is the gate.

## PR-J — Gate sensitivity (ADR-0023)

`regress/`: one-sided Wilson bounds, z=1.645 default, `wilsonZ` becomes a `Thresholds` field; `--z` flag (validated > 0). `--fail-on-notable`: notable findings count toward the gate (report field + exit-code path). Sensitivity disclosure: evaluate the verdict function on maximally separated inputs (0/bN vs cN/cN) per rate family; when unreachable, emit a `sensitivity` block (human + JSON) naming the smallest k at which a full flip gates. `bench` epilogue: when `reps < 5`, append one recommendation line. Docs: sensitivity table in `docs/guide/workflows.md`, flags in `docs/guide/cli.md`.

## PR-K — Format drift detection (ADR-0025)

`ingest/{hook,otel,streamjson,jsonl}`: parsers report `(source, reason)` counts of well-formed-but-unrecognized records alongside observations (enumerated reason buckets, no payload fragments). `daemon`: aggregate counters in self-observation state; expose in `/metrics`; drift section in `catacomb status` only when nonzero; slog warnings rate-limited to first occurrence per key then every Nth. Version watchlist: compile-time tested-ceiling; first newer `claude_code_version` observation logs one warning and sets run meta `format_watch: true`. Quarantine untouched.

## PR-L — Secrets at rest (ADR-0024)

Redact at persist boundary (observations before `Persist`, nodes before `AppendDeltas`) with the ADR-0020 rule pack; `PayloadHash` = sha256 of redacted payload, pre-redaction hash dropped; config `payloads.mode: redact|refs|all` (default `redact`) + `payloads.max_bytes` (default 262144), redact-then-cap, binary refs; schema v4 data migration scrubbing existing `observations.body`/`nodes.body` through the redactor (idempotent fixed point); read/export-time redaction retained as defense in depth.

## PR-M — Hygiene batch

`AGENTS.md:9` status line (eval-management shipped through #107); spec §2 non-goals amendment note referencing ADR-0022's analytics boundary; `pricing/` longest-prefix family fallback with `estimated` provenance; reducer commutativity fuzz target (`go test -fuzz`, seed corpus committed, separate `make fuzz` target, excluded from the coverage gate's scope only if unavoidable).

## V-1 — Dogfood calibration run

After PR-J: one real basket — 2 tasks × 2 variants × 5 reps, cheap model, bounded budget — against a scratch sample project; variant B deliberately degrades a skill/CLAUDE.md instruction. Verify: declared checkpoints land, aggregation rows are sane, `regress` verdicts and sensitivity notes match observed variance. Record results and any evidence-based threshold-default adjustments in `docs/reviews/2026-07-02-dogfood-calibration.md` (small follow-up PR). No code changes in this item beyond threshold defaults if the evidence demands them.

## Global constraints (all PRs)

Worktree per PR; no comments in Go; 100% coverage TDD-first; `make lint` 0; codepolicy; deterministic outputs; markdownlint; live-verify before merge; squash-merge on green CI (authorized).
