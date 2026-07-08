# Catacomb — agent & contributor guide

Offline eval gate for Claude Code agentic pipelines. A single CLI runs prompt baskets (`bench`), records each cell's transcripts as secret-redacted evidence dirs, reduces transcript JSONL into one canonical action graph (`reduce`), derives cross-run step and phase keys (`stepkey`/`phasekey`), aggregates repeated runs (`aggregate`), and gates regressions statistically (`regress`) against baselines stored in embedded SQLite (`store`: baselines + recorded history only). A stdlib-only stdio MCP server (`mcp`) ships the in-run `mark` checkpoint tool. No daemon, no network: the pipeline is `bench → transcript JSONL → reduce → step/phase keys → aggregate → regress`.

- Design spec → [`docs/specs/2026-06-20-catacomb-design.md`](docs/specs/2026-06-20-catacomb-design.md)
- Architecture decisions → [`docs/adr/`](docs/adr/)
- Implementation plans → [`docs/plans/`](docs/plans/)

**Status:** pivoted per [ADR-0026](docs/adr/0026-form-factor-pivot-offline-eval-gate.md) (2026-07-06) — catacomb is the offline eval gate; observability is delegated to a vendor substrate. PV-1/PV-2 (offline gate, baselines, stamps, scores), PV-3 (viewer deletion), PV-4 (daemon/ingest/exporter/gRPC deletion, store slim, DeepEval retarget, guide repositioning), and PV-5 (residual cleanup: dead `repro` OTLP fields, store schema v5, offline Claude Code version watchlist) have landed; PV-6 (extended calibration) is next. Sequence and gates live in the [pivot roadmap](docs/superpowers/plans/2026-07-06-pivot-roadmap.md).

## Principles

- **Simplest thing that works.** Stdlib first; minimal dependencies; **pure Go, no cgo** (single static cross-platform binary). SQLite via `modernc.org/sqlite`, never `mattn/go-sqlite3`.
- **Deterministic core.** The canonical graph is a deterministic reduction of an append-only observation stream: the same observations in any order converge to the same graph once genuine terminals arrive, since provisional statuses are reversible and superseded by any later terminal (the §16 commutativity invariant).
- **TDD by default.** Failing test first, then the minimal implementation, then refactor under green. Not a suggestion — the process.

## Comments: forbidden

**No comments in Go code. None.** No doc comments, no inline comments, no commented-out code. Well-named identifiers and readable code carry the meaning; if a piece of code seems to need a comment, rename or refactor it instead.

The **only** allowed comments are the `//go:build`, `//go:embed`, and `//go:generate` directives. Everything else (including `//nolint` and doc comments) is rejected. Files carrying the standard `// Code generated … DO NOT EDIT.` header are skipped wholesale, so generated code is never our concern.

This is enforced in CI by a test in [`internal/codepolicy`](internal/codepolicy) that parses every hand-written `.go` file and fails on any non-directive comment. A failing build is the rule doing its job — delete the comment.

## Go conventions

- **Dependency inversion:** the *consumer* package declares the interface it needs; providers satisfy it. No importing another package's concrete structs to reach across a boundary.
- **No global mutable state**, no `init()` side effects, no constructors with hidden I/O. Wire dependencies explicitly from `main`.
- **No "static methods"** — a method that ignores its receiver is a package-level function; write it as one. Do not create empty structs just to group functions.
- **No `any`/`interface{}` as a data type.** Use concrete types or generics. (`map[string]any` for genuinely open attribute bags is fine.)
- **Errors:** sentinels checked with `errors.Is`/`errors.As`, never by string; wrap with operation context, `fmt.Errorf("pkg.Op: %w", err)`; log once, at the top.
- **Logging:** `log/slog`, JSON. Never log or serialize secrets; payloads only ever leave through the redaction policy (ADR-0008).
- **Concurrency:** `context.Context` is the first parameter for I/O; every goroutine has a defined exit; share by communicating or guard with a mutex and document ownership.
- **Formatting:** `gofumpt` + `goimports` (local prefix `github.com/realkarych/catacomb`). CI fails if not applied.

## Testing & coverage

- `go test -race`. Table-driven tests are the default; `testify/require` for fatal assertions, `testify/assert` otherwise.
- Mock through the **caller's** interface; never mock third-party SDKs directly — wrap them.
- No `time.Sleep` in tests (enforced by `forbidigo`); use deadlines, channels, or `testing/synctest`.
- Brittle tests (iteration order, error-string parsing, wall clock) are rewritten, not suppressed.
- **Coverage is 100%** outside the minimal, justified exclusions in [`.testcoverage.yml`](.testcoverage.yml). The threshold does not go down. Code that cannot be unit-tested is a refactoring signal (extract a pure function, inject a dependency), not a reason to add an exclusion.

## Workflow

- Every change goes through a feature branch and PR: `git checkout -b <type>/<short-desc>` from `master`. No direct commits to `master` (the initial scaffold aside).
- One PR = one logical change. CI must be green before merge. Merge is **squash** (linear `master`).
- No `--no-verify`; no force-push to `master` (only to your own feature branch).
- Never commit `.env`, `*.pem`, `*.key`, or any secret.

## CI / linters

| Gate | Tool |
|------|------|
| Go lint | `golangci-lint` (config: [`.golangci.yml`](.golangci.yml)) |
| No comments | `go test ./internal/codepolicy/` |
| Coverage 100% | `go-test-coverage` ([`.testcoverage.yml`](.testcoverage.yml)) |
| Docs lint | `markdownlint` ([`.markdownlint.json`](.markdownlint.json)) |
| DeepEval bridge | `pytest` over [`integrations/deepeval`](integrations/deepeval) (own workflow) |

## Build / dev

```
make build   # build bin/catacomb
make test    # go test -race + coverage profile
make cover   # test + 100% coverage gate
make fuzz    # reducer commutativity fuzzer (30s; not in cover/CI)
make lint    # golangci-lint
make fmt     # gofumpt + goimports
```
