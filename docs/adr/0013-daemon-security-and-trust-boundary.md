# ADR-0013: Daemon security and trust boundary

- **Status:** Superseded by [ADR-0026](0026-form-factor-pivot-offline-eval-gate.md)
- **Date:** 2026-06-20
- **Deciders:** @realkarych
- **Related:** spec §2 (Non-Goals), §4, §6, §9, §11; ADR-0001, ADR-0008, ADR-0020

## Context

Every daemon ingress — the hook HTTP receiver, the OTLP gRPC+HTTP receiver, and the realtime surfaces (WebSocket/SSE/gRPC/api) — currently has **zero authentication, authorization, or origin validation**. Non-Goals §2 defers auth and implicitly treats **loopback as the entire trust boundary**, but loopback is shared by *every* local process on a dev or multi-user box, not just Claude Code. The interrogation showed two unguarded attacks: (1) any local process (an npm postinstall, a VS Code extension, a second project) can POST **fabricated** `PostToolUse` events with attacker-chosen `tool_use_id`/`tool_input`/`tool_response` — and since hooks are the backbone and system of record, forged observations are reduced, persisted, and exported as real; (2) anything can subscribe to WS/SSE "all runs" and **exfiltrate every captured payload**, including secrets redaction missed (ADR-0008/0020). "No multi-tenant auth" (a product scope choice) is not the same as "no local trust boundary" (a security defect).

## Decision

Adopt an explicit **threat model and trust boundary** for the daemon; "single-operator, no SaaS auth" stays, but local ingress/egress is no longer implicitly trusted.

1. **Default ingress over a unix-domain socket** with `0600` permissions (owner-only) for the hook receiver and the control/api surface. The hook forwarder talks to that socket. This makes "same user" the boundary instead of "any local process."
2. **If TCP is used** (OTLP receiver, remote scenarios), require a **per-daemon bearer token** baked into the forwarder env and the `OTEL_EXPORTER_OTLP_HEADERS`; reject unauthenticated requests. `catacomb env`/`install-hooks` generate and wire the token.
3. **Realtime surfaces bind to localhost** by default; the **"subscribe all runs"** scope is gated behind the same token. A non-token subscriber can be restricted to a single run it already knows the id of (capability-style), at most.
4. **Export-target validation:** refuse an OTLP passthrough whose endpoint equals the daemon's own ingest endpoint (prevents accidental self-loops; cross-ref ADR-0019 self-observation).
5. **Stated residual risk:** a process running as the *same user* can read the token from the forwarder env / the socket. This is accepted and documented, not hidden; defense against same-user compromise is out of scope.
6. **Defense in depth with redaction (ADR-0020):** the boundary limits *who* can read payloads; redaction limits *what* leaks if they do.

## Alternatives considered

- **Keep loopback-only, no auth** — the status quo; fails on shared/dev boxes for both injection and exfiltration. Rejected.
- **Full mTLS / RBAC** — over-built for a single-operator local tool and contradicts the product Non-Goal. Rejected in favor of unix-socket + bearer token.
- **Origin/PID parentage validation** (only accept events from processes that are children of the observed run) — appealing but unreliable across the SDK/CLI/forwarder process shapes; noted as a possible future hardening, not the v1 mechanism.

## Consequences

- **+** Forged-observation injection and bulk payload exfiltration are closed against other-user and casual same-box processes; the trust boundary is explicit and documented.
- **+** Composes with redaction (ADR-0020) for defense in depth.
- **−** Unix-socket-first adds platform nuance (Windows named pipes; TCP fallback with token); the forwarder and receivers must support both.
- **−** A token to generate, store, and rotate; residual same-user risk remains and is documented rather than solved.

## Amendments

- **2026-06-21 — cross-platform transport (M0.2b):** the daemon's hook ingress uses **loopback TCP (`127.0.0.1:0`) + a per-daemon bearer token** (clause 2's TCP path), not the unix-domain socket of clause 1, so the daemon supports Linux/macOS/Windows uniformly. The token (32 random bytes from `crypto/rand`, hex) lives with the dynamic address in a `0600` **discovery file** (`$XDG_RUNTIME_DIR/catacomb/daemon.json` → `~/.catacomb/run/daemon.json` → temp); the forwarder reads it and sends `Authorization: Bearer <token>`, which the daemon verifies (401 otherwise). Clause 1 (unix socket) stays valid on unix but is unused in v1 — one transport keeps the forwarder and receiver simple and sidesteps the "Windows named pipes" nuance flagged in Consequences. Clause 5's residual same-user risk (the token is readable by same-user processes via the discovery file) stands; binding to `127.0.0.1` keeps it off the network.
