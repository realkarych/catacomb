# Design — Close all five critical-analysis axes

**Date:** 2026-07-11
**Status:** Approved (design), pending spec review → implementation plan
**Owner:** Andrey Karchevsky

## 1. Context & motivation

A sober critical review of the repository surfaced five gap axes. All five are in
scope for this effort; the SQLite store is explicitly out of scope (kept as-is per
owner decision). The work is executed multi-agent, worktree-isolated, parallelized
where files are disjoint, with a dedicated deep-review + verification pass per axis
before merge.

The five axes:

- **A — Security scanning in CI.** CI currently runs lint + test + coverage and no
  security gate. Add a comprehensive scanner suite.
- **B — Release integrity.** Release binaries ship with no checksums, no signatures,
  no SBOM, no provenance; third-party Actions are pinned to mutable version tags.
  Replace the hand-rolled release pipeline with GoReleaser.
- **C — Governance & agent files.** No `SECURITY.md`, empty `.claude/settings.json`,
  no checked-in agent definitions, no contributor/community files. The model policy
  is changing (see §2) and must be codified.
- **D — Redaction hardening.** The secret-redaction engine is a ~12-pattern denylist;
  the README makes an absolute guarantee it cannot honor. Broaden coverage, add an
  entropy gate, fuzz the engine, and soften the README claim.
- **E — Subprocess timeout/cancellation.** `runChildLocal` uses `exec.Command`
  without a context, so a hung `claude -p` cell cannot be timed out or cancelled.

## 2. Decisions

### 2.1 Model policy (supersedes AGENTS.md "opus only")

Fable 5 is treated as the strongest model. New policy:

- **Fable** — everything where quality and decisions matter: all Go implementation
  (TDD), deep code review, verification, and architectural decisions.
- **Opus** — mechanical and read-only work: repository scouting/exploration and
  mechanical edits (e.g., pinning Actions to SHAs).

This **inverts** the current AGENTS.md rule (opus-only, fable/haiku/sonnet forbidden)
but preserves its rationale framing — "put the strongest model on the gated work."
Codifying this (AGENTS.md + memory) is part of Axis C. Because axis worktrees branch
from `master` (which still carries the old rule until C lands), every dispatched
subagent is told the policy explicitly in its prompt rather than relying on AGENTS.md.

### 2.2 Per-axis approach decisions

- **A:** Comprehensive scanners — govulncheck, gosec, gitleaks, actionlint, zizmor,
  CodeQL, dependency-review — plus pinning all third-party Actions to commit SHAs.
- **B:** GoReleaser full replacement (not incremental hardening).
- **D:** Expand the hand-maintained ruleset + add an entropy gate + fuzz target +
  soften the README claim (do not vendor/generate the gitleaks corpus).
- **SQLite:** unchanged. No store changes in this effort.

## 3. Axis A — Security scanning in CI

**New file:** `.github/workflows/security.yml` (a dedicated workflow, kept separate
from `ci.yml` so scanner noise/latency does not gate the fast lint/test loop unless
we want it to).

Jobs (each least-privilege `permissions`):

- **govulncheck** — official Go vuln scanner (`golang.org/x/vuln/cmd/govulncheck`)
  over `./...`. Fails on known-vuln symbols actually reached.
- **gosec** — Go SAST. Config file (not inline `//nolint`, which is banned) for any
  justified suppressions.
- **gitleaks** — secret scan on push + PR. **Requires `.gitleaks.toml` with an
  allowlist** for `redact/testdata/**` and redaction test files, which intentionally
  contain fake secrets and would otherwise fail the gate permanently.
- **actionlint + zizmor** — workflow linting and workflow-security analysis.
- **CodeQL** — GitHub's Go analysis (init → autobuild → analyze).
- **dependency-review** — PR-only; blocks PRs that introduce vulnerable/newly-flagged
  dependencies.

**Action pinning:** every third-party Action across `ci.yml`, `e2e-live.yml`,
`python-deepeval.yml`, and the new `security.yml` is pinned to a full commit SHA with
a trailing `# vX.Y.Z` comment. `publish.yml` is excluded here — Axis B rewrites it.
Dependabot already updates SHA pins.

**Triage note:** CodeQL/gosec may surface findings in existing code. Confirmed true
findings are fixed; false positives are suppressed via config files only.

**Verification:** `actionlint` and `zizmor` run clean over all workflows; each new job
is syntactically valid; gitleaks passes on the current tree given the allowlist;
govulncheck passes on the current dependency set.

## 4. Axis B — Release integrity (GoReleaser, full replacement)

**New file:** `.goreleaser.yaml`:

- **builds:** `linux/darwin/windows × amd64/arm64`, `CGO_ENABLED=0`, `-trimpath`,
  `-ldflags "-s -w -X main.Version={{.Version}}"`.
- **archives:** `tar.gz` (unix) / `zip` (windows), matching current naming.
- **checksums:** `checksums.txt`.
- **signing (cosign, keyless/OIDC):** sign the checksums file (and thus transitively
  the artifacts) and the Docker image.
- **SBOM:** syft, per-artifact.
- **provenance:** SLSA build provenance / attestations.
- **nfpm:** `.deb` for amd64/arm64.
- **brews:** push the formula to `realkarych/homebrew-tap` (replaces the hand-rolled
  homebrew job).
- **dockers + docker_manifests:** multi-arch GHCR image (`linux/amd64`, `linux/arm64`),
  plus image signing.

**Rewrite `publish.yml`** to a thin workflow:

- `goreleaser release` job with `permissions: contents: write, packages: write,
  id-token: write` (id-token needed for cosign keyless + attestations). Secrets:
  `GITHUB_TOKEN`, `HOMEBREW_TOKEN` (tap push).
- A **slim APT job** that consumes GoReleaser's `dist/*.deb` and runs the existing
  bespoke aptly + GPG publish into `catacomb-apt@gh-pages` (GoReleaser does not manage
  that custom repo, so this logic is preserved, not reinvented).

**Highest blast-radius axis → deepest verification:**
`goreleaser release --snapshot --clean` runs locally/in-worktree and the verifier
asserts: all six archives present; `checksums.txt` present and well-formed; `.sig`/
attestation and SBOM artifacts present; rendered Homebrew formula is valid Ruby with
correct URLs/SHAs; `.deb` packages built. No real publish is performed in verification.

## 5. Axis C — Governance & agent files (lands first)

- **`SECURITY.md`** — private vulnerability-disclosure channel; explicit warning that
  **baskets are executable code — run only trusted baskets**; supported versions.
- **`AGENTS.md`** — replace the "Agent models: opus only" section with the §2.1 policy.
- **`.claude/settings.json`** — a permission allowlist for common read-only Bash/tools
  (fewer prompts) and a **fast Stop hook** running `make fmt` + the codepolicy test
  (mechanical guard for the no-comments rule). The full coverage gate stays in CI so
  the hook stays fast.
- **`.claude/agents/`** — checked-in subagent definitions: `implementer`, `reviewer`,
  `verifier` (Fable) and `scout` (Opus), matching §2.1.
- **Community files:** `CONTRIBUTING.md` (thin, points to AGENTS.md), `CODEOWNERS`,
  `.github/PULL_REQUEST_TEMPLATE.md`, `.github/ISSUE_TEMPLATE/{bug_report,feature_request}.md`.
- **Memory:** rewrite `subagent-models-opus-only` to the new policy (done directly in
  the user's memory dir, outside the repo).

**Verification:** `markdownlint` clean on new docs; `settings.json` is valid JSON and
the hook command runs; codepolicy still green.

## 6. Axis D — Redaction hardening (Go, TDD)

**`redact/redact.go` ruleset additions:** Stripe (`sk_live_`/`rk_live_`/`pk_live_`),
GCP service-account JSON (`"type":"service_account"` / `private_key_id`), Azure client
secrets & storage keys, SendGrid (`SG.`), Twilio (`AC…`/`SK…`), npm (`npm_`), PyPI
(`pypi-`), GitLab PAT (`glpat-`), Google OAuth (`ya29.`), and base64url via a widened
charset including `_` and `-`.

**Entropy gate:** add a Shannon-entropy helper. Lower the length threshold for the
generic high-entropy rules (e.g., 40 → 32) but only redact a generic candidate when
`len ≥ N AND entropy ≥ T`, preserving precision while widening recall. Named-key and
named-vendor rules keep firing regardless of entropy.

**Fuzz target:** `redact/redact_fuzz_test.go` — properties: (1) never panics on
arbitrary input; (2) output contains no residual of an injected known-pattern secret
(differential); (3) idempotence: `Redact(Redact(x)) == Redact(x)`.

**README:** change "no artifact catacomb writes encodes a raw secret" to a best-effort
/ blast-radius-reduction framing, plus the matching phrasing in `docs/guide/privacy-and-operations.md`
if present. This is the **only** axis that edits `README.md`. It does **not** touch
`AGENTS.md` (owned by C); AGENTS.md's redaction wording (line ~36) is not an absolute
claim and needs no change.

**Coverage:** 100% on all new code; RE2 keeps every pattern linear-time (no ReDoS).

## 7. Axis E — Subprocess timeout/cancellation (Go, TDD)

Thread `context.Context` into `runChildLocal` (`cmd/catacomb/childlocal.go`) and switch
`exec.Command` → `exec.CommandContext`. Add an **opt-in** per-task `timeout` to the
basket schema (`bench`), wired through the bench run path.

**Default is non-breaking:** when `timeout` is unset, no per-cell deadline is imposed
(exactly the current behavior), but context cancellation (parent ctx / Ctrl-C) is
*always* propagated to the child — that propagation is the core fix. A set `timeout`
adds a deadline on top.

**Tests:** a set timeout kills a long-running child and surfaces a typed error;
context cancellation returns promptly; unset `timeout` preserves current behavior;
the happy path is unchanged. 100% coverage, `-race`.

## 8. Execution & review architecture

- **One worktree + branch per axis** (repo rule). Axes are file-disjoint by design:
  B owns `publish.yml`; A owns the other workflows + `security.yml`; `README.md` only
  in D; `AGENTS.md` only in C. Disjoint files → safe parallel worktrees.
- **Ordering:** Axis **C lands first** (codifies the model policy and governance).
  A/B/D/E then proceed in parallel. Every subagent is told the model policy explicitly
  in-prompt (worktrees branch from `master`, which lags until C merges).
- **Per-axis pipeline:**
  1. **Scout (Opus, read-only)** — map exact files, callers, and test files; draft the
     TDD test list.
  2. **Implementer (Fable, worktree)** — TDD to green.
  3. **Deep review (Fable, parallel lenses)** — correctness, security, 100%-coverage
     honesty, and repo-policy conformance (no comments, gofumpt/goimports,
     dependency-inversion, no `time.Sleep`), with adversarial verification of each
     finding (independent skeptics; majority-refute kills a finding).
  4. **Fixer (Fable)** — apply confirmed findings.
  5. **Verifier (Fable)** — run `make cover lint` + `go test -race` (Go axes) or the
     axis-specific dry-run (config axes), plus a behavioral check; return evidence.
  6. **Orchestrator (main window)** — review diff + evidence, gate the PR.
- **Integration:** each axis = one squash PR, green CI. The main window orchestrates
  and never batch-executes plan tasks inline (CLAUDE.md rule).

## 9. Testing & verification strategy

- **Go axes (D, E):** TDD, 100% coverage, `-race`, fuzz (D).
- **Config axes (A, B, C):** dry-run validation — `goreleaser release --snapshot
  --clean` (B); `actionlint`/`zizmor` + gitleaks-with-allowlist (A); `markdownlint`
  (C); codepolicy green everywhere.
- **Deep verification (explicit requirement):** a dedicated adversarial review +
  verification pass per axis before merge, plus a final cross-axis integration review
  after all five land.

## 10. Sequencing & integration

1. Land Axis C (governance + model policy) first — memory + AGENTS.md become
   authoritative.
2. Fan out A, B, D, E in parallel worktrees.
3. Per-axis: implement → deep review → fix → verify → PR → green CI → squash merge.
4. Final cross-axis integration review on `master` after all merges.

## 11. Out of scope

- **SQLite store** — kept as-is (no migration to bbolt/flat files).
- **Vendoring/generating the gitleaks rule corpus** — rejected in favor of a curated
  hand-maintained ruleset (Axis D).
- Any unrelated refactoring not required by the five axes.

## 12. Risks & mitigations

- **B rewrites a working release pipeline.** Mitigation: `--snapshot` dry-run
  verification of every artifact class before merge; keep the bespoke APT publish
  logic intact rather than reinventing it.
- **A may surface pre-existing CodeQL/gosec findings.** Mitigation: fix true positives;
  suppress false positives via config files only (never inline `//nolint`).
- **D widening recall risks false positives.** Mitigation: entropy gate + 100%-covered
  table tests pinning both positive and negative cases.
- **Model-policy inversion contradicts current AGENTS.md.** Mitigation: land C first;
  pass the policy in-prompt to every subagent until C is authoritative.
