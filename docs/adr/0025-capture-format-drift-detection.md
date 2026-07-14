# ADR-0025: Capture format drift detection

- **Status:** Accepted
- **Date:** 2026-07-02
- **Deciders:** @realkarych
- **Related:** ADR-0002, ADR-0017, ADR-0019; spec §17; review `docs/internal/reviews/2026-07-02-post-p0-cto-design-review.md` §4.3, amended by [ADR-0026](0026-form-factor-pivot-offline-eval-gate.md)

## Context

Four of the five ingest parsers bind roughly forty undocumented Claude Code format strings: hook event names and envelope fields, transcript JSONL record shapes plus the `~/.claude/projects/**/subagents/agent-*.jsonl` directory convention, the stream-json envelope, native OTel span names and attributes, and the hardcoded model-pricing table. By design (ADR-0019, fail-open), records with unknown `type`/subtype/span name are **silently dropped**. `claude_code_version` is captured into repro fingerprints but never consulted.

ADR-0017 versions Catacomb's **own** data format; nothing watches the **upstream** formats. When Claude Code ships a format change — it has before, and its trace schema is explicitly beta — the failure mode today is a silently thinner graph, discovered by a human noticing missing nodes days later. For a tool whose value proposition is fidelity, undetected fidelity loss is the worst failure mode available.

## Decision

Make drift observable. Never make it blocking.

1. **Per-source drift counters.** Each ingest parser reports, alongside its observations, counts of **well-formed but unrecognized** records — unknown record type, unknown subtype, unknown span name, unknown hook event, unknown content-block type — keyed by `(source, reason)`. Reason labels are coarse, enumerated buckets, not raw payload fragments (no leak surface).
2. **Aggregation and surfacing.** The daemon aggregates counters in its self-observation state and surfaces them where an operator already looks: a drift section in `catacomb status` (shown only when nonzero — silence-when-healthy), the `/metrics` endpoint, and `slog` warnings rate-limited to the first occurrence per `(source, reason)` and every Nth thereafter.
3. **Version watchlist.** The binary carries a compile-time tested-ceiling for Claude Code versions. On the first observed session whose `claude_code_version` exceeds it, the daemon logs one warning and flags the run's meta (`format_watch: true`). Nothing else changes — capture proceeds identically.
4. **Quarantine unchanged.** Malformed input keeps flowing to quarantine per ADR-0019. Drift counters cover the complementary class: input that parses cleanly but matches no known shape.

## Alternatives considered

- **Hard version gating (refuse to ingest from newer Claude Code)** — an observability sidecar must never block or degrade the tool it observes; partial capture beats none, and the graph records provenance. Rejected.
- **Schema inference / auto-adaptation for unknown shapes** — unbounded magic that converts silent data loss into silent data corruption. Rejected.
- **Persisting drift tallies in the quarantine table** — conflates the malformed dead-letter channel with healthy-but-unknown accounting and turns a debugging table into a metrics store. Rejected; counters are in-memory plus `/metrics`.
- **Doing nothing (status quo)** — accepts that the four-parser fidelity bet, the project's largest standing risk, has no early-warning system. Rejected.

## Consequences

- **+** Upstream format drift becomes a visible, alertable signal at the moment it starts, not an archaeology finding.
- **+** Zero behavior change on the capture path; fail-open is preserved end to end.
- **−** Each parser now maintains its own "known unknowns" enumeration — modest perpetual upkeep, the deliberate price of the early warning.
- **−** Counters are in-memory and reset with the daemon; longitudinal drift history is deferred until a real need appears.
- **−** The tested-version ceiling must be bumped each time compatibility is verified against a new Claude Code release — a one-line release-checklist item.
