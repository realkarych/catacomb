# ADR-0024: Secrets at rest — enforcing redaction on the write path

- **Status:** Accepted
- **Date:** 2026-07-02
- **Deciders:** @realkarych
- **Related:** ADR-0008, ADR-0020, ADR-0017; spec §5.2, §10, §11; review `docs/reviews/2026-07-02-post-p0-cto-design-review.md` §4.2

## Context

ADR-0008 decided "apply redaction rules **before persistence**/export"; ADR-0020 hardened the scheme (value-scanning pack, whole-node surface, post-redaction hashing, redact-then-cap ordering, binary refs, per-sink modes). The shipped implementation applies redaction only on the **read and export paths** (`redact.Node` at serve/export time). What is actually on disk diverges from both ADRs:

- `observations.body` and `nodes.body` persist **raw, unredacted** tool inputs/outputs and prompt text;
- `PayloadHash` is computed over **pre-redaction** content and stored/exported, the exact brute-force channel ADR-0020 §3 closed on paper;
- the `full+hash+redact` / `refs-only` / `all` **modes** and the **size cap** of ADR-0008 do not exist (the only payload cap anywhere is the OTLP exporter's 32 KiB).

A copied, synced, or exfiltrated `catacomb.db` therefore leaks everything redaction was designed to catch. For a tool whose README leads with privacy, the gap between accepted ADRs and disk reality is a design defect, not a backlog item.

## Decision

Implement the write-path clause of ADR-0008/0020, with one recorded simplification, and scrub existing data.

1. **Redact at the persist boundary.** Observation payloads pass through the ADR-0020 rules (value pack + key globs, whole-surface fields) before `Persist`; node payloads and node-level redaction surface (`name`, `subagent_type`, whitelisted attrs) before `AppendDeltas`. Redaction is a pure, deterministic function of the bytes, applied before the append-only log — so replay/rebuild determinism (ADR-0003) is preserved and rebuilt graphs equal live ones.
2. **Post-redaction hashing, and only that.** `PayloadHash` becomes sha256 of the **redacted** payload. The pre-redaction hash is dropped entirely rather than retained as ADR-0020 §3's HMAC'd local integrity hash — no consumer of pre-redaction identity exists today. This is the deliberate simplification of ADR-0020 §3; if a dedup consumer ever needs raw-content identity, that amendment reopens.
3. **Modes and cap become real.** Config gains `payloads.mode: redact | refs | all` (default `redact`) and `payloads.max_bytes` (default 262144). Ordering is redact-then-cap per ADR-0020 §4; overflow is stored as a typed ref (length + post-redaction hash), and binary/non-UTF-8 payloads as `‹binary:len,sha256›` refs per ADR-0020 §5. `all` logs the ADR-0008 warning at startup. Per-sink mode overrides (ADR-0020 §6) stay deferred until a second consumer of per-sink fidelity exists.
4. **Scrub migration.** Schema v4 via the ADR-0017 runner: a data migration rewrites every existing `observations.body` and `nodes.body` through the redactor and recomputes hashes. The redactor is idempotent (its output contains no spans its own rules match), so the migration is a fixed point and safe to re-enter. Read-only opens of pre-v4 databases surface `ErrSchemaOutdated` exactly as today.
5. **Read/export-time redaction stays.** It is cheap, idempotent, and covers the window between parse and persist in memory-served views — defense in depth, not dead code.

## Alternatives considered

- **Amend ADR-0008/0020 to bless read-time-only redaction** — keeps secrets at rest in a product that markets local privacy; the DB file itself is the asset most likely to leave the machine (backups, sync folders). Rejected.
- **A manual `catacomb scrub` command instead of a migration** — secrets linger until an operator remembers to run it; the migration runs unconditionally on first open by a v4 binary. Rejected.
- **Retain the HMAC'd pre-redaction integrity hash (ADR-0020 §3 verbatim)** — key management cost with no present consumer. Deferred explicitly, not silently.
- **Redact only nodes, leave the observation log raw** — the log is the system of record and the primary at-rest liability; redacting the derived view but not the source is cosmetic. Rejected.

## Consequences

- **+** A catacomb database at rest contains only redacted content; ADR-0008/0020 stop being aspirational; the migration retroactively closes the exposure for existing databases.
- **+** Store/export consistency (ADR-0015) simplifies: every consumer downstream of the store sees the same redacted truth.
- **−** Redaction false positives now destroy data at rest — there is no raw copy to recover. `all` mode remains the explicit opt-out; the redact pack's test bar rises accordingly (this is the real price of the decision).
- **−** The v4 migration is O(rows) at first open; large existing databases pay a one-time startup scrub.
- **−** Dedup identity is now identity of redacted content: payloads differing only inside redacted spans collide. Accepted — the redacted form is the only form that exists downstream.
