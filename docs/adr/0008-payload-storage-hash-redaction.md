# ADR-0008: Payload handling — store with hash and redaction

- **Status:** Accepted
- **Date:** 2026-06-20
- **Deciders:** @realkarych
- **Related:** spec §5.2, §11; ADR-0013, ADR-0020, amended by [ADR-0026](0026-form-factor-pivot-offline-eval-gate.md)
- **Amended by:** ADR-0020 — the redaction surface is the whole node (not just `payload`), with default value-scanning regexes, redact-before-cap ordering, and **post-redaction** hashing; ADR-0013 adds the daemon trust boundary that limits who can read payloads.

## Context

Node payloads (tool inputs/outputs, prompt text) are the most useful content for downstream consumers, but they are also where **secrets and sensitive data** appear. Catacomb persists and exports this content, so it must balance fidelity against leakage risk, under operator control.

## Decision

Make payload handling **configurable**, defaulting to **store + hash + redaction**:

- Store tool inputs/outputs and prompt text by default, plus **`sha256` of the pre-redaction payload** (for dedup and integrity).
- Apply **redaction rules** before persistence/export: regex patterns and key-path globs (e.g. `*.api_key`, `authorization`); redacted spans become `‹redacted:reason›` with the hash retained.
- **Modes:** `full+hash+redact` (default) · `refs-only` (store only `transcript_path` refs + hashes, no payload) · `all` (no redaction; logged warning).
- **Size cap** per payload (config; overflow stored as a ref/hash).
- Redaction and caps apply **uniformly to the store and to all exporters**.

## Alternatives considered

- **Refs-only always** — safest, but strips the content downstream needs. Offered as a mode, not the default.
- **Store everything, no redaction** — maximal fidelity but unacceptable default leakage risk. Offered as an explicit opt-in mode with a warning.

## Consequences

- **+** Useful content by default, with a clear safety mechanism and integrity hashes.
- **+** Operators can dial fidelity vs safety per deployment; redaction is consistent across store and exports.
- **−** Redaction rules must be authored and maintained; over-redaction can hide useful data, under-redaction can leak (operator responsibility).
