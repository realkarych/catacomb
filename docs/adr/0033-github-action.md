# ADR-0033: First-class GitHub Action with PR-comment verdicts

- **Status:** Accepted
- **Date:** 2026-07-17
- **Deciders:** @realkarych
- **Related:** [ADR-0026](0026-form-factor-pivot-offline-eval-gate.md) (the product is a CI verdict — "runs in CI with nothing but the binary"); [ADR-0022](0022-regression-detection-over-repeated-runs.md) (the `regress` gate and its exit codes); ADR-0032 (baseline bundles, the recommended CI restore path this Action consumes — not yet written; link it here when it lands); CLI and `--json` compatibility surfaces ([VERSIONING.md](../VERSIONING.md) #1)

## Context

The product's whole thesis is "the artifact is a CI verdict," yet adopting it in CI
today means hand-writing a workflow that installs a pinned binary, runs `bench`,
runs `regress`, and reads an exit code — and the verdict lives in a job-log table or
a downloaded artifact, not where reviewers look. The competitive review named the
required-check-plus-sticky-PR-comment shape (codecov, danger) as the adoption
pattern enterprises already trust, and the adversarial roadmap review confirmed a
first-class Action is the highest-leverage adoption surface — cheap to build on the
stable `--json`/exit-code contracts, with one hard rule: comment rendering must live
in the Go binary (tested to the 100% gate), not in untestable composite-action bash.

## Decision

Two deliverables:

1. **`regress --format markdown`** (Go, in the binary): a new render mode beside
   `RenderHuman`/`RenderJSON` producing a PR-comment-ready markdown document — a
   headline verdict line, the findings as a markdown table (only gating scopes by
   default, with the full table foldable), the sensitivity disclosure ("this gate
   cannot fire at k=3"), and the audit/reliability notes. The comment renders
   purely from the deterministic `Report`, so it is a pure function tested like the
   other renderers. A `--format human|json|markdown` flag is added; the existing
   `--json` bool becomes a **deprecated alias** for `--format json` (kept working,
   documented as deprecated) so no existing pipeline breaks.

2. **`realkarych/catacomb-gate` composite Action** (`action.yml` at repo root under
   `.github/actions/catacomb-gate/`, plus a usage doc): a thin wrapper that
   installs a checksum/cosign-verified pinned catacomb release, optionally runs
   `bench` on the PR's basket (default on, with `reps`/`model`/`runs-dir` inputs
   surfacing the documented cost levers, and a `candidate-runs-dir` input to skip
   bench for pre-benched evidence), restores the baseline (a `baseline-bundle`
   input consuming ADR-0032 bundles, or a `baseline` label/name selector against a
   committed store), runs `regress --format markdown --json` (both: markdown for
   the comment, exit code for the gate), and upserts a **sticky** PR comment keyed
   by the baseline identity. The composite's shell stays logic-free — all rendering
   is the Go binary; the shell only installs, invokes, and pipes.

The exit-code contract is surfaced honestly in the comment: exit 1 (regression) is
a red verdict; exit 2 (operational — missing baseline, stale store) renders as
"the gate could not run," never a red X that reads as a regression.

## Alternatives considered

- **Render the comment in composite-action bash/jq.** Rejected: it puts
  presentation logic outside the 100%-coverage gate, duplicates the report schema
  in a second language, and rots against `--json` changes. The Go renderer is the
  single source of truth.
- **A published Docker action.** Rejected for v1: a composite action over the
  already-signed release binary is lighter, avoids a second image to maintain, and
  reuses the existing supply-chain story; a Docker action adds a container to
  publish for no capability gain.
- **Bake bench out of the Action (regress-only).** Rejected as the default: the
  PR gate's candidate evidence is produced by `bench` in CI by design, so bench
  belongs in the Action with cost levers — but a `candidate-runs-dir` input keeps
  the regress-only path available for teams that bench separately.
- **A standalone `realkarych/catacomb-action` repo.** Deferred: shipping the
  action in-repo under `.github/actions/` proves it end-to-end against the release
  first; extracting to a marketplace repo with an independent tag is a follow-up
  once the input contract is stable.

## Consequences

- Adopting catacomb in CI becomes a few lines of YAML instead of a 30-line
  hand-rolled workflow with SHA-pinning homework; the verdict lands in the PR.
- The Action inputs/outputs become a compatibility surface (VERSIONING.md);
  `--format` and the markdown document shape join the CLI surface (`--json`
  remains, deprecated).
- A solo maintainer takes on marketplace v1-tag discipline eventually; shipping
  in-repo first defers that cost while delivering the capability.
- The sensitivity line surfaced in-comment ("this gate cannot fire at k=3") is a
  differentiator no competing gate shows — the statistical-honesty pitch made
  visible in the exact spot a reviewer decides.
