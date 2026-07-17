# ADR-0032: Baseline bundles — one verifiable artifact for CI restore

- **Status:** Accepted
- **Date:** 2026-07-17
- **Deciders:** @realkarych
- **Related:** [ADR-0026](0026-form-factor-pivot-offline-eval-gate.md) (offline constitution: "runs in CI with nothing but the binary and the files"); [ADR-0024](0024-secrets-at-rest-write-path-redaction.md) (evidence is redacted at rest, so bundles leak nothing new); [ADR-0016](0016-cross-run-step-identity-and-annotations.md) (baseline stamps travel with the bundle); [ADR-0017](0017-data-format-versioning-and-migration.md) (the bundle format is versioned); evidence and store compatibility surfaces ([VERSIONING.md](../VERSIONING.md) #4, #5)

## Context

A `name:` baseline is a store row (name, pinned run IDs, selector, stamps) plus the
pinned evidence directories it points at. Making one resolvable on an ephemeral CI
runner today means committing a binary SQLite database and a tree of transcripts
into the source repository, or hand-rolling artifact upload/restore of both halves
— the bundled skill documents exactly that, and it is the weakest step of the CI
story: merge-conflict-prone, repo-bloating, and with no integrity story tying the
db row to the evidence it references. The adversarial roadmap review confirmed the
gap is friction (a tar-by-hand workaround exists) but judged the shape most
enterprise CI expects — a single restorable, verifiable artifact — worth first-class
support.

## Decision

Two subcommands under the existing `baseline` verb (the top-level `import` name is
taken by session import, ADR-0030):

- **`catacomb baseline export <name>`** packages the baseline row and its complete
  evidence run directories into one **byte-deterministic** `.tar.gz` bundle:
  `bundle.json` (format version, baseline row, per-file sha256 manifest) followed
  by `runs/<run-id>/**`. Determinism is a contract, not an accident: entries are
  sorted, tar metadata is normalized (fixed mode, zero uid/gid, mtime taken from
  the baseline's `created_at`, no PAX extras), and gzip carries no timestamp —
  exporting the same baseline twice yields the same bytes, so bundles can be
  content-addressed and diffed by hash.
- **`catacomb baseline import <bundle>`** verifies the format version and every
  file hash, extracts the evidence under the local `--runs-dir`, and upserts the
  baseline row with `RunsDir` rewritten to that local directory (so `regress`
  emits no runs-dir mismatch warnings). Import is **idempotent**: a run directory
  that already exists is accepted only if its files hash-match the bundle
  (then skipped); any mismatch is a hard operational error, never an overwrite.

What the bundle deliberately does not carry: `--record`/trends history
(longitudinal history remains a persistent-store concern, exactly as the CI guide
already states) and anything outside the pinned run set. The audit `pack` command
is unchanged — different purpose (sampled reviewer bundle vs complete restorable
reference); `pack` may later adopt the same hash manifest.

Extraction is treated as untrusted input: entry names must be clean relative
paths under `runs/` (or exactly `bundle.json`), symlink and irregular entries are
rejected, and a version newer than the binary supports refuses with the same
posture as the store's `ErrSchemaTooNew`.

## Alternatives considered

- **Keep the documented commit-db-plus-runs-tree practice.** Rejected as the
  primary story: binary db merge conflicts, repo bloat, no integrity binding, and
  enterprise security review flags transcripts inside source repos. It remains a
  workable fallback and stays documented.
- **Bundle the SQLite db file itself.** Rejected: the db carries unrelated
  baselines and recorded history; a bundle must be exactly one baseline's closure.
  A JSON row is also stable across store schema migrations in a way raw db bytes
  are not.
- **A generic `catacomb pack --restorable` mode.** Rejected: pack's contract is
  sampling + reviewer instructions; overloading it blurs two artifacts that differ
  in completeness guarantees, determinism requirements, and consumers.
- **Zip container.** Rejected: `archive/zip` central-directory timestamps and
  per-entry extra fields make byte-determinism fiddlier; tar+gzip with normalized
  headers is the smaller stdlib-only path.

## Consequences

- The CI recipe becomes: `baseline export` once (or on golden refresh), store the
  bundle as a CI artifact/object-store blob, `baseline import` at job start —
  no repo-committed db or transcript tree. The bundled skill's CI guide and the
  future GitHub Action build on this.
- A new versioned on-disk format joins the compatibility surface list
  (VERSIONING.md): `bundle.json` schema v1, additive-only within a major.
- Stamps travel inside the bundle, so the existing `--strict` stamp checks fire
  identically on imported baselines — scheme drift is caught on first use, not at
  import time.
- Evidence inside bundles is redacted-at-rest by construction (write-path
  redaction, ADR-0024); export adds no new data-exfiltration surface beyond what
  `runs/` already holds.
