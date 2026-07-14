# `catacomb down` — lifecycle teardown & artifact removal

Date: 2026-06-29
Status: Design approved, pending spec review

## Problem

`catacomb up` is the one-shot "do everything" entrypoint: it installs Claude Code
hooks, starts the daemon, creates the SQLite database, writes the discovery file,
and opens the UI. There is no inverse. To undo any of it a user must currently:

1. find and `kill` the daemon pid,
2. run `install-hooks --uninstall` to prune hook entries,
3. `rm catacomb.db catacomb.db-wal catacomb.db-shm`,
4. `rm -rf ~/.catacomb`.

None of this is discoverable or documented. The CLI has no `stop`/`down`/`restart`,
no data-removal command, and the `Store` interface exposes no delete path. Anyone
who types `catacomb down` (the docker-compose mental model) gets "unknown command".

This design adds a single lifecycle-teardown command, `catacomb down`, that fills
the missing daemon-stop gap and provides scoped, safe, agent-friendly removal of
the artifacts catacomb creates.

## Artifacts catacomb creates (teardown targets)

Local:

- SQLite DB `catacomb.db` (+ `-wal`, `-shm`) — **in the daemon's working directory
  by default** (configurable via `--db`). This CWD-relative default means databases
  scatter across the filesystem when `up` runs in multiple projects.
- Discovery file `~/.catacomb/run/daemon.json` (or `$CATACOMB_DISCOVERY`, or
  `$XDG_RUNTIME_DIR/catacomb/daemon.json`, or `/tmp/catacomb/daemon.json`).
- Daemon log `<discovery-path>.log`.
- Hook entries inside `./.claude/settings.json` and/or `~/.claude/settings.json`
  (the surrounding files belong to Claude Code; only catacomb entries are ours).

External (only when configured):

- Postgres tables `nodes`, `edges`, `runs`.
- Neo4j node labels (`Session`, `Run`, `ToolCall`, …) and relationships.

Not ours (never touched): `~/.claude/projects/` transcripts.

## Goals

- One discoverable command to stop the daemon and, with escalation, remove
  catacomb's artifacts.
- Safe by default: the bare verb does the least-destructive thing; data loss
  always requires explicit opt-in plus confirmation.
- Usable by LLM agents non-interactively: `--dry-run`, `--yes`, `--json`,
  meaningful exit codes, no hangs on a confirmation prompt in a non-TTY.
- Honest about what it cannot know (CWD-scattered databases) rather than
  pretending "delete everything" succeeded.

## Non-goals (out of scope; tracked separately)

- Per-run deletion (`catacomb runs rm <id>`) and `Store` delete methods.
- Changing the default DB location away from CWD.
- A persistent registry/manifest of every DB path ever opened.
- Rolling `--json` / `--quiet` / standardized exit codes across *all* commands.

These go to the UX backlog, not this spec.

## Command design

Default principle: **the bare verb does the least-destructive thing — it only
stops the daemon.** Destructive scope is added by flags. This is safer than the
strict docker-compose model (where `down` immediately edits the user's
`settings.json`), gives the missing `stop` for free, and does not punish
`up`/`down` cycles during a session.

Every invocation stops the daemon first; the scope flags are independent and
each *add* their own removal on top of that stop. They compose freely (e.g.
`--uninstall --purge`), and `--all` is the shorthand for the two local ones.
`--purge` does **not** imply `--uninstall`: purge alone deletes data but leaves
hooks installed.

```
catacomb down [flags]

Lifecycle:
  (no flags)     Gracefully stop the running daemon. Idempotent (no daemon → success).
                 This is the missing `stop`. Every flag below also performs this stop.
  --uninstall    Also remove catacomb hook entries from .claude/settings.json (project + global)
  --purge        Also delete local data/state: the daemon's DB (+ -wal/-shm) and ~/.catacomb (run dir, log)
  --external     Also drop catacomb tables in Postgres/Neo4j (requires connection flags; NEVER implied)
  --all          = --uninstall + --purge   (everything LOCAL; --external is never part of --all)

Targeting:
  --db <path>    Explicitly target a specific DB file (repeatable) for --purge —
                 for databases `down` cannot discover on its own.

Safety / agents:
  --dry-run      Print exactly what would be stopped/removed/dropped; change nothing. Exit 0.
  -y, --yes      Skip confirmation. Required in a non-TTY destructive run
                 (otherwise refuse with a clear message and a nonzero exit code).
  --json         Machine-readable report of planned/performed actions.
  --force        Escalate a stuck daemon stop from SIGTERM to SIGKILL.
```

`--external` connection flags mirror the daemon/export flags:
`--postgres-export-dsn`, `--neo4j-export-uri`, `--neo4j-export-user`,
`--neo4j-export-password`.

## Target discovery (the "narrowed scope" decision)

Databases are CWD-scattered and the discovery file only knows about the *currently
running* daemon. `down` deliberately cleans only what it can know, and is honest
about the rest — no hidden registry.

Order of operations matters: **read discovery first** (capture `pid` and `db_path`),
**then** stop the daemon, **then** delete. Stopping first would lose `db_path`.

`--purge` targets:

1. `db_path` from the discovery file (if a daemon is/was running),
2. any explicit `--db <path>` values,
3. `~/.catacomb` (run dir + log).

If no discovery file exists (no daemon known), `db_path` is unknown: purge cleans
only `~/.catacomb` and explicit `--db` paths, and prints an honest warning —
"other databases may remain in directories where you ran catacomb; pass them with
`--db`." No magic discovery of CWD databases.

## Safety & agent ergonomics

- **Confirmation** is required only for data loss (`--purge` / `--external` /
  `--all`) on a TTY. `--uninstall` alone is its own consent (config edits are
  reversible) and prompts no confirmation.
- **Non-TTY without `--yes`** on a destructive scope → refuse with a clear message
  and a nonzero exit code (never hang an agent on a prompt).
- **`--dry-run`** and **`--json`** are first-class so an agent can preview the plan,
  then execute.
- **Exit codes:** `0` for success / idempotent no-op; nonzero for: failed to stop
  the daemon, missing credentials for `--external`, non-TTY without `--yes` on a
  destructive scope, partial failure.
- **Stopping the daemon:** SIGTERM the `pid` from discovery, then poll. If it does
  not exit within a timeout, error out (auto-SIGKILL only under `--force`). The
  daemon's graceful-shutdown path (flush, close store, remove discovery file) must
  be verified and, if missing, completed as part of this work.

## Architecture, reuse, and new code

- New `cmd/catacomb/down.go` (cobra command), registered in the existing command
  groups alongside `up`.
- Hook removal **reuses the existing `pruneCatacomb()`** logic from
  `cmd/catacomb/installhooks.go` — no duplication.
- **Local purge is file deletion, not `DROP TABLE`**: remove the DB file and its
  `-wal`/`-shm` siblings, and `os.RemoveAll` the `~/.catacomb` run dir. The `Store`
  interface needs no changes; the append-only contract is broken only at the
  filesystem level, after the daemon has stopped.
- **`--external` is the only part that needs new code**: a `Drop()` (or `DropAll()`)
  method on the Postgres and Neo4j exporters that drops the catacomb-owned tables /
  labels. Tested against stubs following the existing exporter test patterns.

## Testability (100% coverage, TDD-first)

Test seams:

- `CATACOMB_DISCOVERY` env override + `t.TempDir()` for discovery/run-dir paths.
- `--db <path>` into temp dirs for DB-file deletion.
- An injectable "process stopper" and "discovery reader" so the stop logic is
  unit-testable without a real daemon process.
- External drops tested against exporter stubs.

Every branch gets a test: no daemon running, daemon won't die within timeout,
`--force` SIGKILL, missing `--external` credentials, non-TTY without `--yes`,
`--dry-run` changes nothing, idempotent re-run, untracked-DB warning, `--json`
report shape.

## Sharp edges (explicit)

1. **`--external` is dangerous.** The table names (`nodes`/`edges`/`runs`) are
   generic; on a shared Postgres a drop could destroy the user's own tables.
   Mitigation: never part of `--all`/`--purge`, always explicit credentials plus
   confirmation plus printing the exact table names. **Candidate to defer to
   phase 2.**
2. **Stuck daemon.** No automatic `SIGKILL`; only `--force`.
3. **Scattered CWD databases.** Honest warning plus `--db`, no magic.

## Phasing

- **Phase 1 (local lifecycle):** bare `down` (stop), `--uninstall`, `--purge`,
  `--all`, `--db`, `--dry-run`, `--yes`, `--json`, `--force`, plus daemon
  graceful-shutdown verification. Covers the entire local artifact set and the
  missing `stop`.
- **Phase 2 (external):** `--external` with exporter `Drop` methods and the extra
  safety gating.
