# Versioning policy

Catacomb follows [SemVer 2.0.0](https://semver.org). Releases are annotated tags `vX.Y.Z` on `master` commits; `publish.yml` refuses tags that are not ancestors of `master` or whose commit lacks green required checks, then builds, signs, and publishes via goreleaser (the `release` environment).

## The compatibility surface

For version arithmetic, catacomb's "public API" is the union of these eight contracts. A change is *breaking* iff it can invalidate a working user setup — a basket, a verifier, recorded evidence, a baseline, a caller workflow, or a script parsing our output:

1. **CLI** — commands, flags and their defaults, exit-code semantics (`0` ok / `1` regression / `2` operational), and machine-readable output shapes: `--json`, the `regress --format` value set (`human|json|markdown`), and the markdown report's document shape (verdict headline, summary line, findings table, collapsible reliability/audit section — the surface PR comments and their consumers are built on). Human-readable table output is *not* a contract; `--json`, `--format json`, and `--format markdown` are. `--json` on `regress` is a deprecated alias for `--format json`; removing it is breaking until it goes through a deprecation cycle. Statistical-test selectors are flags like any other (e.g. `regress --paired-test`, default `sign`): changing a selector's default silently changes verdicts and is breaking.
2. **Basket YAML schema** — field names, types, validation semantics (`KnownFields` means any rename is breaking by construction).
3. **Verifier exec contract** — `CATACOMB_*` env vars, the scores-JSONL dialect (including reserved `verifier.pass` and provenance fields), `verify.json` ledger shape, artifact capture semantics.
4. **Evidence layout** — directory structure under a runs dir, `meta.json` schema (field removal/retyping is breaking; addition is not).
5. **Store** — baseline and record bodies, and schema migrations. A lossless auto-migration is compatible; a migration that drops or rewrites user data is breaking. Record bodies carry a schema version `v` (`regress.RecordVersion`, currently `2` — v2 added the optional `project` identity stamp for fleet-side joins); bumps are additive-only, and readers accept every version from 1 through their own, so recorded history never needs a migration.
6. **Key schemes** — `stepkey`/`phasekey` scheme identity. A scheme change silently mis-aligns every existing baseline; version stamps + `--strict` refuse it at runtime, and it is always MAJOR (pre-1.0: always MINOR, never PATCH).
7. **Python SDKs** — the public API of `catacomb-verifier` and `catacomb-judge` (`integrations/`); they version with the repo.
8. **GitHub Action** — the `catacomb-gate` composite action (`.github/actions/catacomb-gate`): input names, defaults, and semantics; output names (`verdict`, `exit-code`, `report-json`) and their meanings; the exit-code re-raise behavior; and the sticky-comment marker convention (`<!-- catacomb-gate:<baseline-identity> -->`). Removing or renaming an input/output, changing a default, or changing what an output carries is breaking. The action versions with the repo while it lives in-repo.

Not part of the surface: model behavior, agent-CLI transcript drift — Claude Code and Codex alike (handled by the per-runtime version watchlist as PATCH-level parser fixes), descriptive env stamps content (exception: the `meta.json` `env.agent_runtime` stamp is the parser-dispatch key for recorded evidence, so it rides surface #4), wording of notes/advisories on stderr.

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
