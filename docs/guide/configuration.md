# Configuration

Catacomb has no config file and no daemon to configure. Every setting is a command-line
flag with a sensible default; the only environment variables are the two `bench` uses to
tag runs.

## Default paths

| Setting | Default | Used by | Override |
| --- | --- | --- | --- |
| Claude projects dir (transcripts) | `~/.claude/projects` | `bench` | `--projects-dir` |
| Evidence runs dir | `~/.catacomb/runs` | `bench`, `regress`, `baseline set` | `--runs-dir` |
| SQLite store | `~/.catacomb/catacomb.db` | `baseline`, `regress` (`name:`/`--record`), `trends` | `--db` |
| Bench manifest | `<basket>.manifest.jsonl` | `bench` | `--manifest` |

When the home directory cannot be resolved, the path-flag defaults are empty and the
commands that need them error out with exit `2` until the flags are set explicitly.

## Environment variables

| Variable | Effect |
| --- | --- |
| `CATACOMB_LABELS` | Comma-separated `k=v` list of ambient run labels. `bench` reads it and merges the pairs under each cell's own `basket`/`task`/`variant`/`rep` labels (cell labels win per key); the merged set is recorded in the cell's evidence `meta.json` and matched by `label:` selectors. Keys must match `[a-z0-9_.-]{1,64}`; values are capped at 256 bytes. |
| `CATACOMB_RUN_ID` | Set *by* `bench` in each cell's child environment (alongside the merged `CATACOMB_LABELS`) to the cell's run-id, so tooling running inside the cell can correlate itself with the evidence directory. |

## The store

The SQLite database at `--db` holds exactly two things: named **baselines**
(`baseline set`) and the append-only **regression history** (`regress --record`,
replayed by `trends`). Graphs are never persisted — they are rebuilt from transcripts on
every command.

- The store is created on first write by `baseline set` (in an offline loop that is
  always the first store-touching command; `regress --record` requires a `name:`
  baseline, so it never creates the store).
- Write-path opens (`baseline set`, `baseline rm`) migrate an older on-disk schema
  forward automatically. Read-only opens (`baseline list`, `trends`, and `regress`
  `name:` resolution) refuse an older schema with a hint to run a write-path command,
  and every command refuses a schema newer than the binary (upgrade catacomb).
- The current schema version is 5.

## Fixed policies

Two behaviors are deliberately not configurable:

- **Redaction** always runs: on the copies written into evidence dirs and on every
  observation parsed into a graph, with payload sides capped at 256 KiB (oversized
  content becomes a typed `‹ref:len,hash›` reference). See
  [Privacy and operations](privacy-and-operations.md).
- **Step-key scheme** (`stepkey/v1`) is versioned, not tunable; baselines record it as a
  [version stamp](cli.md#baseline-version-stamps) so a scheme change is detected instead
  of silently misaligning runs.

For the full per-command flag reference, see [`cli.md`](cli.md).
