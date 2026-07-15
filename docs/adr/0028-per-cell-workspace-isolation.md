# ADR-0028: Per-cell workspace isolation (SP-W)

- **Status:** Accepted
- **Date:** 2026-07-13
- **Deciders:** @realkarych
- **Related:** [ADR-0027](0027-verification-layer-and-reliability-metrics.md) (deferred per-trial isolation), [ADR-0026](0026-form-factor-pivot-offline-eval-gate.md) (constitution), [ADR-0022](0022-regression-detection-over-repeated-runs.md) (deterministic observables), SP-W design spec [2026-07-13-spw-workspace-isolation-design.md](../internal/specs/2026-07-13-spw-workspace-isolation-design.md)

## Context

The 2026-07-12 gap review deferred per-trial environment isolation as organizational, user-side work (§2.3). The 2026-07-13 research pass re-graded it: Anthropic's agent-evals canon states verbatim that each trial must start from a clean environment, and reports Claude gaining an unfair advantage in internal evals by reading the git history left over from previous trials. Today a basket task runs every rep in the same shared `dir`; ETL state, VCS metadata, caches, and prior agents' droppings persist across reps, so pass^k — computed over exactly those reps (SP2) — measures correlated trials. This is a correctness defect for the benchmarking half, not hygiene.

The same mechanism is the owner's target scenario: benchmarking agentic pipelines against a Yandex Arcadia checkout at a pinned revision, optionally with a patch file overlaid, and running the same basket across environments (trunk vs trunk+patch as variants of one task).

## Decision

Add an optional `workspace` block to the basket schema — on tasks, with wholesale per-variant override — that makes `bench` materialize a **fresh working directory per cell**:

1. `bench` creates a temporary directory per cell (`--workspaces-dir` to place it), runs the user-supplied `workspace.cmd` (plain argv, no shell) inside it, then runs the variant setup, the agent, artifact capture, and the verifier all rooted there.
2. A declared `workspace.patch` file is validated and hashed at basket load; its absolute path is exported to `workspace.cmd` as `CATACOMB_PATCH`. **Applying it is the user command's job** — the core carries zero VCS or diff-format semantics, exactly as verifier semantics live behind the SP1 exec boundary.
3. An optional `workspace.teardown` argv runs after the cell on a fresh context (the cell context may already be dead) so mounts (`arc unmount`) are always released; then the directory is removed unless `--keep-workspaces`.
4. Env stamps gain a descriptive `workspace{rev, patch_sha256}` block (omitted when absent). Like the SP3 stamps, it never gates; the declaration is already pinned by the basket hash.

Cells without `workspace` behave byte-identically to today (dormancy, as in SP2/SP3).

This reverses the ADR-0027 deferral **for the mechanism only**: catacomb owns fresh-dir-per-trial, lifecycle, and identity stamping; what gets materialized (arc/git/ya invocations, YTsaurus local-mode, testcontainers) stays user-side.

## Alternatives considered

- **Core applies the patch** (`git apply`/`patch(1)` subprocess). Rejected: external-binary dependency plus diff-dialect semantics (arc diffs are not git diffs) in a core that promised none (ADR-0026).
- **Docs-only convention** (users script isolation inside `variant.setup`). Rejected: the shared `dir` remains the default failure mode, nothing is stamped, and isolation cannot be proven by the hermetic E2E — the defect that motivated this ADR would survive as boilerplate every basket must remember.
- **Copy-on-write snapshots in core** (reflink/APFS clones of a template dir). Rejected: platform-specific, and materialization cost belongs to the user command, which can choose `arc mount` precisely because teardown exists.
- **Status quo.** Rejected: pass^k over contaminated reps is the exact failure mode the canon documents.

## Consequences

- pass^k and per-task outcomes are computed over genuinely independent trials; the Arcadia/patch benchmarking scenario is expressible in one basket.
- Per-cell isolated workdirs remove the shared-state hazard that made parallel cell execution unsafe; a future bounded `--parallel` builds on this.
- Workspace materialization joins the cell inside the task timeout; slow checkouts must budget for it (documented; `arc mount` is the fast path).
