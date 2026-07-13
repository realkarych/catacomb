# Versioning policy

Catacomb follows [SemVer 2.0.0](https://semver.org). Releases are annotated tags `vX.Y.Z` on `master` commits; `publish.yml` refuses tags that are not ancestors of `master` or whose commit lacks green required checks, then builds, signs, and publishes via goreleaser (the `release` environment).

## The compatibility surface

For version arithmetic, catacomb's "public API" is the union of these seven contracts. A change is *breaking* iff it can invalidate a working user setup — a basket, a verifier, recorded evidence, a baseline, or a script parsing our output:

1. **CLI** — commands, flags and their defaults, exit-code semantics (`0` ok / `1` regression / `2` operational), and `--json` output shapes. Human-readable table output is *not* a contract; `--json` is.
2. **Basket YAML schema** — field names, types, validation semantics (`KnownFields` means any rename is breaking by construction).
3. **Verifier exec contract** — `CATACOMB_*` env vars, the scores-JSONL dialect (including reserved `verifier.pass` and provenance fields), `verify.json` ledger shape, artifact capture semantics.
4. **Evidence layout** — directory structure under a runs dir, `meta.json` schema (field removal/retyping is breaking; addition is not).
5. **Store** — baseline and record bodies, and schema migrations. A lossless auto-migration is compatible; a migration that drops or rewrites user data is breaking.
6. **Key schemes** — `stepkey`/`phasekey` scheme identity. A scheme change silently mis-aligns every existing baseline; version stamps + `--strict` refuse it at runtime, and it is always MAJOR (pre-1.0: always MINOR, never PATCH).
7. **Python SDKs** — the public API of `catacomb-verifier` and `catacomb-judge` (`integrations/`); they version with the repo.

Not part of the surface: model behavior, Claude Code transcript drift (handled by the version watchlist as PATCH-level parser fixes), descriptive env stamps content, wording of notes/advisories on stderr.

## Pre-1.0 rules (current)

- **0.MINOR++** — any breaking change to a surface above, or a completed feature wave (an ADR sub-project or equivalent: new command, new contract, new metric family). Breaking changes ride only minors and must be called out in the release notes with a migration line.
- **0.x.PATCH++** — bug fixes, security/dependency fixes, transcript-parser watchlist bumps, performance work, doc corrections that ship with a binary anyway. No new surface, nothing removed.

## 1.0 criteria

Cut `v1.0.0` when all three hold:

1. The verifier exec contract and basket schema survive two consecutive minor releases unchanged.
2. The weekly live gate is green for four consecutive scheduled runs.
3. At least one production basket corpus (real tasks, user-owned verifiers) runs against it routinely.

## Post-1.0 rules

- **MAJOR** — breaking any surface: removing/renaming CLI flags or commands, changing exit-code or `--json` semantics, basket/scores/meta schema breaks, lossy store migrations, key-scheme changes.
- **MINOR** — additive: new commands/flags, new basket or meta fields, new metrics/report blocks, additive lossless migrations, new SDK APIs.
- **PATCH** — fixes with no surface delta.

## Process

- The release commit is always a `master` head; the tag is annotated with a one-paragraph summary (headline features / fixes / breaking notes).
- Cadence: tag after each merged feature wave, or when accumulated fixes are worth shipping — no calendar releases.
- After pushing a tag, confirm the `Publish Release` run concluded `success` (check conclusions, not the watch exit).
- Version bumps are decided against this document; when a change does not fit these rules, amend this document in the same PR that ships the change.
