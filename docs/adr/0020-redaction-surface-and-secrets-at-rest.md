# ADR-0020: Redaction surface and secrets-at-rest hardening

- **Status:** Accepted
- **Date:** 2026-06-20
- **Deciders:** @realkarych
- **Related:** spec §5.2, §10, §11; ADR-0008, ADR-0013, ADR-0015, amended by [ADR-0026](0026-form-factor-pivot-offline-eval-gate.md)

## Context

ADR-0008 stores payloads with `sha256` + redaction, framing the target as "tool inputs/outputs, prompt text." The interrogation found several **default-on leaks** that make redaction cosmetic:

1. The bundled redaction is **key-path globs** (`*.api_key`, `authorization`) with **no default value-scanning regex**, so a credential that is a *value* inside free text — e.g. `export DATABASE_URL="postgres://kesha:kesha_dev_password@localhost/…"` in a `Bash.command` (confirmed shape on disk) — is a **false negative**: stored and exported verbatim.
2. Redaction is scoped to the **`payload` field only** (§5.2). Sensitive context also lives in `name`, `subagent_type`, and `attrs` (`cwd`, `transcript_path`, `description`, `mcp_*`) — outside §11 entirely (and exported as neo4j labels / pg JSONB).
3. `payload_hash` is computed over **pre-redaction** content and exported next to the redacted span; a low-entropy/structured secret (known prefix/charset/length) can be **brute-forced from the hash**, so redaction is undone.

## Decision

1. **Ship a non-empty default value-scanning regex pack** that runs on **values regardless of key**, applied to free-text fields (`command`, `code`, `content`): connection-string URIs with embedded credentials, common token shapes (`AKIA…`, `ghp_…`, `sk-…`, `xox…`), PEM blocks, JWTs, and high-entropy base64/hex runs. Key-path globs remain, but are no longer the only mechanism.
2. **Extend the redaction surface from `payload` to the whole node:** `name`, `subagent_type`, and a whitelisted set of `attrs` (`cwd`, `transcript_path`, command-like fields, `description`, `mcp_*`) are scrubbed by the same rules. Markers' `state_ref` is treated as opaque but redaction-eligible.
3. **Hash post-redaction for any stored/exported `payload_hash`** (dedup still works on the redacted form). Any pre-redaction integrity hash is **local-only and HMAC'd with a per-deployment key**, never exported, so it cannot be brute-forced off-box.
4. **Define cap-vs-redact ordering:** **redact first, then apply the size cap**, so truncation can never cut off the span a redaction rule would have matched.
5. **Binary/non-UTF-8/base64 payloads** that can't be value-scanned are stored as a typed `‹binary:len,sha256›` reference by default (not raw), unless the operator opts into `all` mode.
6. **`refs-only` honesty + per-sink mode:** in `refs-only`, the stored `transcript_path` ref points at an **unredacted** on-disk file — documented as such; redaction modes (`full+hash+redact`/`refs-only`/`all`) are settable **per exporter**, so `all` is not a single sticky global kill switch shared across sinks.

## Alternatives considered

- **Keep key-glob-only redaction** — misses the most common real secret (a value inside `Bash.command`). Rejected.
- **Redact only `payload`** — leaks via `name`/`attrs`/labels. Rejected for whole-node redaction.
- **Export the pre-redaction hash for dedup** — brute-forceable; rejected for post-redaction hashing + local HMAC integrity hash.

## Consequences

- **+** The common credential-in-command leak, the metadata leak surface, and the hash-brute-force undo are closed by default; truncation can't defeat redaction; `all` is scoped per sink.
- **+** Composes with the trust boundary (ADR-0013) for defense in depth.
- **−** Value-scanning regexes have false positives (over-redaction) and a per-event cost; the pack is tunable and documented as best-effort, not a guarantee.
- **−** Whole-node redaction + per-sink modes add config surface and must be applied uniformly across store and every exporter (ADR-0015).
