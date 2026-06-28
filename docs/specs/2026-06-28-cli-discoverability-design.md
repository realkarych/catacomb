# Catacomb — CLI Discoverability Design

**Status:** Draft for review
**Date:** 2026-06-28
**License:** Apache-2.0
**Language:** Go

> *The two questions every new user asks — "how do I watch all my sessions?" and "how do I see the ones I already ran?" — currently have no answer in `--help` or the README. You have to read the source to find `install-hooks --global` and `daemon --transcript-dir`. This spec makes the CLI answer those questions itself.*

---

## 1. Summary

Two core workflows are reachable today only by reading Go source:

- **Observe every session** (all projects, not just the current directory) requires `install-hooks --global`, which `up` never surfaces.
- **Load past sessions into the UI** requires starting the daemon with `--transcript-dir ~/.claude/projects`, which `up` never does and the README never mentions.

This change makes both workflows first-class on `up`, exposes what the daemon is observing through discovery + `status`, and rewrites the help text and README so neither workflow requires source-diving.

## 2. Goals & Non-Goals

### Goals

- `catacomb up --global` installs hooks for **all** projects (`~/.claude/settings.json`).
- `catacomb up --history` starts the daemon tailing `~/.claude/projects`, backfilling past sessions.
- When a daemon is already running, `up --history` never restarts it; it prints the exact restart command and continues (install hooks, open UI).
- `catacomb status` shows what the daemon is observing (transcript scope).
- `catacomb --help` and every relevant subcommand carry a `Long` description and `Example`s; the root help lists the common recipes.
- The README documents "observe every session" and "load past sessions" as named workflows.

### Non-Goals

- No auto-restart of a running daemon (the user chose "inform, never kill").
- No change to `up`'s default daemon paths (still cwd-relative `catacomb.db`, default discovery).
- No change to the graph model, ingestion, or exporters.
- No interactive prompts in `up`.

## 3. Behavior

### 3.1 `up --global`

`--global` switches hook installation from `./.claude/settings.json` to `~/.claude/settings.json`, reusing the existing `settingsPath(project, global)` resolver. It does not touch the daemon. `up --global --history` combines with §3.2.

### 3.2 `up --history`

When `up` **starts** the daemon, `--history` appends `--transcript-dir <home>/.claude/projects` to the spawned `daemon` invocation (`<home>` from `os.UserHomeDir`). The tailer then backfills every existing `*.jsonl` (sessions and `*/*/subagents/agent-*.jsonl`) and follows live appends.

When a daemon is **already running**, `up` does not restart it. It reads the running daemon's scope from discovery (§3.3) and:

- if the scope already covers `~/.claude/projects`, prints `observing all history already ✓`;
- otherwise prints the exact restart command, reconstructed from the discovery-recorded flags (`--transcript-dir`, `--db`, `--allow-payload-access`).

In both cases `up` still installs hooks and opens the UI — `--history` is additive, never destructive.

### 3.3 Discovery enrichment

`daemon.Discovery` gains three fields the daemon already knows, written at startup in `cmd/catacomb/daemon.go`:

- `TranscriptDir string` — the tailed directory (empty = history off);
- `DBPath string` — the SQLite path;
- `AllowPayloadAccess bool` — whether the payload endpoint is enabled.

These are loopback-only and already colocated with the bearer token, so they add no new exposure. They let `up` compare scope and render a copy-pasteable restart command, and let `status` show scope.

### 3.4 `status`

`status` gains one line: `observing  <transcript-dir>` (or `observing  — (history off; see: catacomb up --history)` when empty). This is the only runtime signal today that says what the daemon is actually watching.

## 4. Help text

- **Root** (`catacomb --help`): a `Long` with a **Common recipes** block — observe-all (`up --global`), load-history (`up --history`), read-content (`--allow-payload-access`).
- **Per-command** `Long` + `Example`: `up`, `daemon`, `install-hooks`, `replay`, `observe`, `demo`. Each states what it does, its scope (project vs global, live vs historical), and a worked example.

## 5. README

Rewrite Quickstart to stop overselling `up` as "does everything". Add two named sections:

- **Observe every session** — `catacomb up --global`.
- **Load past sessions** — `catacomb up --history` (and the equivalent `daemon --transcript-dir`).

Clarify the default `catacomb.db`/discovery locations and that tail cursors are persisted, so re-running the daemon does not duplicate history.

## 6. Testing

TDD, 100% coverage, no comments (per `AGENTS.md`).

- `up` flag matrix: `--global` → global settings path; `--history` start path appends `--transcript-dir`; already-running path prints restart hint vs. `already ✓`; combinations.
- Discovery round-trips the three new fields (marshal/unmarshal, omitempty where appropriate).
- `daemon` writes the new discovery fields from its config.
- `status` renders the observing line for empty and non-empty scope.
- Help text is asserted through command construction (no new branches → coverage stays green); new `RunE` branches are exercised.

Mock through the caller's interface; no `time.Sleep`; table-driven.

## 7. Delivery

One feature branch `feat/cli-discoverability`, one squash PR containing the spec, the behavior change, and the docs. Work is delegated to subagents and orchestrated from the main session.

## 8. Out of scope / future

- Stable home-relative daemon db for `up` (today's cwd-relative `catacomb.db` is a known wart).
- `catacomb restart` as a first-class command.
- Auto-restart with scope reconciliation.
