# ADR-0018: Time model

- **Status:** Accepted
- **Date:** 2026-06-20
- **Deciders:** @realkarych
- **Related:** spec §5.1, §5.2, §7, §16; ADR-0003, ADR-0010

## Context

The design mixes two different clocks without distinguishing them. §7 uses `observed_at` (daemon **ingest** time) as the merge tiebreak, while node `t_start`/`t_end` come from source **event** time (OTel span times, transcript timestamps). The heuristic-identity epsilon (§5.5) compares timestamps that may originate from **different clocks**. There is no timezone normalization, no wall-clock-vs-monotonic distinction, and no handling of clock skew / NTP steps / machine suspend — any of which can make `observed_at` non-monotonic and corrupt order-dependent merges. Meanwhile `seq` (ADR-0010) is a true monotonic counter but the rules name `observed_at`.

## Decision

1. **Separate event-time from ingest-time explicitly.** Each observation keeps both: `event_time` (from the source, may be absent/untrusted) and `observed_at` (daemon ingest, wall clock). Node `t_start`/`t_end` are derived from `event_time`; `observed_at` is metadata, never a correctness input on its own.
2. **`seq` is the authoritative order and merge tiebreak**, never wall-clock. ADR-0003's "ties broken by latest `observed_at`" is amended to "ties broken by `seq`" (monotonic, persisted, gap-free per daemon — ADR-0010).
3. **All stored times are UTC**, normalized at ingest; source-local offsets are preserved in attrs if present but never used for ordering.
4. **Durations are computed from `event_time`** and **rejected if negative** (clock skew between sources) — a negative/implausible duration is dropped to null with a flag, not stored as a negative number.
5. **Heuristic-identity time windows compare only same-clock timestamps** (e.g. two OTel spans), never cross-clock; cross-clock proximity is not a merge signal.
6. **Monotonic vs wall:** wall-clock (`observed_at`, timestamps) is for display/event-time; ordering, tiebreaks, watermarks, and `rev` use the monotonic `seq`. Machine suspend / NTP steps therefore cannot reorder the reduction.

## Alternatives considered

- **Keep `observed_at` as the tiebreak** — non-monotonic under skew/NTP/suspend, making merges order-dependent and breaking commutativity (§16). Rejected for `seq`.
- **Trust source event-time for ordering** — sources disagree and some omit it; event-time is for display, not the authoritative order. Rejected.
- **Ignore the distinction (status quo)** — silent mis-ordering and negative durations. Rejected.

## Consequences

- **+** Ordering and merges are deterministic regardless of clock anomalies; durations are sane; display keeps human event-time.
- **+** Makes §16 commutativity robust: the tiebreak is a true monotonic counter.
- **−** Two time fields per observation and a normalization step at ingest; slightly more storage and care in mappers.
- **−** Some sources lack event-time; nodes then have `seq`-order but coarse/absent `t_start` (display degrades, ordering does not).
