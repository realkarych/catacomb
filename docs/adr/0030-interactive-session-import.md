# ADR-0030: Interactive session import

- **Status:** Accepted
- **Date:** 2026-07-15
- **Deciders:** @realkarych
- **Related:** [ADR-0026](0026-form-factor-pivot-offline-eval-gate.md) (offline eval gate constitution); [ADR-0027](0027-verification-layer-and-reliability-metrics.md) (offline `catacomb verify` and the evidence-then-verify split); [ADR-0022](0022-regression-detection-over-repeated-runs.md) (baskets, labels, and the runs-dir the gate reads); evidence-layout compatibility surface ([VERSIONING.md](../VERSIONING.md) #4)

## Context

The pipeline had a single evidence entry point. `catacomb bench` spawns the agent with `claude -p` and peeks its stream-json stdout for the `session_id` and the terminal `result` event's `total_cost_usd`, then reduces the transcript into a bench-cell-shaped evidence dir that `verify`/`regress` consume. Stream-json is headless-only: an interactive Claude Code TUI session emits no machine-readable stdout, so a session run by hand could not enter the gate at all.

Yet the interactive session writes a transcript of the **same** format `bench` already parses, to `~/.claude/projects/<encoded-cwd>/<session-id>.jsonl`. The evidence the gate needs is on disk; only the entry point was missing. The single-entry-point design meant the only way to gate a task was to re-run it headless, which is not always possible (interactive-only flows) and never reproduces the exact session the operator wants to gate.

## Decision

Add `catacomb import`, a second evidence entry point that ingests an already-finished session transcript as a bench-cell-shaped evidence dir. It is **basket-anchored**: the basket is the source of truth for task, variant, verifier contract, checkpoints, and labels; the task's agent `cmd` is ignored, because import never spawns an agent.

1. The transcript is resolved by `--session-id` (looked up under `--projects-dir`, default `~/.claude/projects`) or by `--transcript` (a direct path); exactly one is required. Subagent transcripts alongside the main file are picked up as they are for bench.
2. Reduction reuses the existing offline-evidence path unchanged — `resolveTranscripts → parseTranscripts → graphFromObservations → benchEnvStamps → evidence.Write` — so imported evidence is byte-shaped like a bench cell.
3. The `task:<id>` marker window is **synthesized** from the transcript's first and last record timestamps (there is no stream-json wrapper to stamp it), and any `mcp__catacomb__mark` checkpoints present in the transcript are honored. Declared checkpoints that the transcript never marked are warned about, not gated.
4. The run-id is prefixed `import-…` (default `import-<basket>-<task>-<variant>-r<rep>`), distinct from bench's `bench-…`, while the labels are **identical** to a bench cell (`basket`/`task`/`variant`/`rep` plus any `--label`), so downstream selectors match imported and benched cells uniformly.

Import is **evidence-only**: it writes a redacted evidence dir and stops. Verification stays a separate `catacomb verify` step (ADR-0027), so the same offline verifier re-runs against imported evidence exactly as it does against bench evidence.

### Cost semantics

An interactive transcript carries no terminal `result` event and therefore no `total_cost_usd`, so `meta.CostUSD` is unset/omitted for imports rather than fabricated. The token-derived `cost_usd` **metric** is still priced from the transcript's token counts through the pricing table (the same pricer bench uses), so it stays directly comparable to bench runs. The distinction is deliberate: the *reported* cost field is absent because the source did not report one; the *derived* cost metric is present because it is computed the same way regardless of entry point.

## Alternatives considered

- **A `bench` attach/ingest mode** (a flag on `bench` that reads a transcript instead of spawning). Rejected: it overloads bench's single responsibility — spawn an agent and capture its run — with a second, contradictory mode that spawns nothing, and blurs the clean `bench` (produce by running) vs `import` (produce from a finished run) split. Two commands with one job each read better than one command with a mode switch.
- **Freeform-flag import** (no basket; `--task`, `--verify`, and `--label` supplied entirely on the command line). Rejected: it loses the `verify:` contract and the bench-cell label parity that let downstream `verify`/`regress` selectors treat imported and benched cells identically. Anchoring on the basket keeps a single source of truth for the verifier contract and the labels, so imported evidence is a peer of bench evidence rather than a parallel, hand-labeled shape the gate has to special-case.

## Consequences

- There are now two entry points into one pipeline: both `bench` and `import` land in the same `transcript JSONL → reduce → evidence → verify → regress` flow, and everything downstream of `evidence.Write` is unchanged. Interactive sessions can now be gated with no `claude -p`.
- Import depends on the transcript JSONL format, which is internal and undocumented — a risk shared by every transcript-parsing command (`bench` included), mitigated by the existing capture-format drift/version advisories (ADR-0025). Import adds no new external contract; it widens exposure to the same one bench already carries.
- The evidence layout (VERSIONING.md #4) is reused verbatim, so imported dirs coexist with bench dirs in one runs-dir and share baselines; the only visible differences are the `import-` run-id prefix and the omitted `meta.CostUSD`.
