# ADR-0029: Basket-relative path resolution

- **Status:** Accepted
- **Date:** 2026-07-15
- **Deciders:** @realkarych
- **Related:** Basket YAML schema surface ([VERSIONING.md](../VERSIONING.md) #2; the rejected alternative below would have touched evidence layout, #4); [ADR-0027](0027-verification-layer-and-reliability-metrics.md) (verifier contract and offline `catacomb verify`); [ADR-0028](0028-per-cell-workspace-isolation.md) (workspace isolation — adjacent but separate)

## Context

A basket points at scripts and working directories with relative paths: a task's `dir`, and `./`- or `../`-prefixed elements of `cmd` and `verify.cmd` (e.g. `["python3", "./verify.py"]`). These resolved against the **process working directory**. `catacomb bench` runs with the cwd the operator launched it from — in practice the basket's own directory — so inline runs found the scripts. But offline `catacomb verify` (ADR-0027) re-runs the verifier with cwd set to the **evidence directory**, not the workdir. The same `["python3","./verify.py"]` that passed inline therefore failed offline with a file-not-found — silently, and inside the exact bench→verify loop the docs promote.

The trap was invisible because the two entry points parse the same basket through different loaders — `Load` for bench, `LoadOffline` for offline verify — yet each resolved relative paths from whatever cwd it happened to inherit. Nothing in the basket told the reader which cwd was assumed.

## Decision

Resolve basket-relative exec paths against the directory containing the basket file, inside the shared `decodeBasket` loader, so both `Load` and `LoadOffline` apply one identical rule:

1. A task's `dir`, when relative, joins onto the basket directory (always).
2. Elements of `cmd` and `verify.cmd` that begin with `./` or `../` join onto the basket directory. Bare words (`python3`, `bash`) and absolute paths are left untouched, so `PATH` lookup and absolute references keep working.

Resolution runs once, at load, and mutates only the decoded basket struct. The basket hash is taken over the raw file bytes, so cell identity — and every existing runs-dir keyed by it — is unchanged.

Explicitly **out of scope**: `artifacts` (globbed relative to the workdir and constrained to `filepath.IsLocal`) and the `workspace` fields keep their own semantics; a relative `workspace.patch` already resolves against the basket directory through its own loader step (ADR-0028). Only `dir`, `cmd`, and `verify.cmd` change resolution base.

## Alternatives considered

- **Stamp the resolved absolute argv into `meta.json` and have offline verify read it.** Rejected: offline verify re-parses the basket via `LoadOffline`, so a stamped argv would be dead data — nothing reads it — and it would widen the evidence-layout compatibility surface (VERSIONING.md #4) for no benefit. Loader resolution confines the change to the basket-schema surface (VERSIONING.md #2).
- **Document the cwd behavior without changing it.** Rejected: a note does not repair the loop. It leaves a silent file-not-found trap inside a workflow the docs actively promote.
- **Status quo.** Rejected: inline and offline verify disagree on the same basket, which defeats the offline re-verification ADR-0027 introduced.

## Consequences

- Inline bench and offline verify now resolve the same relative paths identically; the promoted bench→verify loop works from either entry point.
- This is a basket-schema behavior change → 0.MINOR per VERSIONING.md. Release notes carry a migration line: a basket that relied on cwd-relative resolution from a cwd **other than** the basket directory must make those paths relative to the basket file instead. Anyone who ran `bench` from the basket's own directory is unaffected.
- The evidence layout is untouched and the basket hash is unchanged, so existing runs-dirs and baselines stay valid.
- For a `./`- or `../`-style `verify.cmd`, the recorded `verify.json` `cmd` — and the `VerifyConfigSHA256` folded over it — now captures the *resolved absolute* path, so that recorded string is host- and location-dependent and does not survive a basket relocation. This does not touch gating: the verdict rides the verifier's own `scores.jsonl` (`verifier.pass`), not this hash, and offline `catacomb verify` re-resolves the argv from the basket rather than reading `verify.json.cmd`. Only the recorded path string differs; the evidence shape is the same as before.
- Bare-word and absolute paths keep their meaning; only `./`/`../` argv elements and a relative `dir` change resolution base.
