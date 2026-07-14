# Basket schema

A **basket** is a declarative YAML file that defines a benchmark as a matrix of
**tasks × variants × reps**. [`catacomb bench`](cli.md#bench) expands that matrix into
one *cell* per combination, runs the cells sequentially, and writes one evidence
directory per cell. Offline [`catacomb verify`](cli.md#verify) reads the same basket to
re-run each task's verifier over recorded evidence.

This page is the authoritative reference for every field, its type, and its validation
rules. For what `bench` *does* at runtime — run-ids, labels, evidence layout, the
epilogue — see [the `bench` command](cli.md#bench); for CLI flags, environment
variables, and defaults see [Configuration](configuration.md).

Unknown or misspelled keys are rejected at load — the decoder does not ignore them — and
a value of the wrong shape reports what it expected (for example, `expected a list of
strings, but got a single value`).

## Top-level fields

The top-level document is a `Basket`:

| Field | Type | Required | Default | Notes |
| --- | --- | --- | --- | --- |
| `basket` | string | yes | — | The basket name. Charset `^[A-Za-z0-9._-]+$` (no spaces, commas, or `=`), at most 256 bytes. Becomes the `basket` label on every cell. |
| `reps` | int | yes | — | Repetitions per cell. Must be `>= 1`; a missing or `< 1` value fails at load with `reps must be >= 1`. |
| `tasks` | list | yes (≥1) | — | One or more [tasks](#task). |
| `variants` | list | yes (≥1) | — | One or more [variants](#variant). A single variant runs and records evidence, but `regress` needs ≥2 variants to gate — see [What happens if](#what-happens-if). |

Task and variant `id`s must be unique within their list, and baskets whose dash-joined
ids would collide into the same run-id (`bench-<basket>-<task>-<variant>-r<rep>`) are
rejected at load.

## Task

Each entry of `tasks` is a `Task`: the agent command and how to run and check it.

| Field | Type | Required | Default | Notes |
| --- | --- | --- | --- | --- |
| `id` | string | yes | — | Unique within `tasks`. Charset `^[A-Za-z0-9._-]+$`, at most 256 bytes. |
| `cmd` | list of strings | yes | — | The agent command, run as a plain `exec` (argv, no shell). `argv[0]` as a bare word is resolved on `PATH`; a `./`- or `../`-prefixed element resolves against the basket directory (see [Path resolution](#path-resolution)). The command must emit stream-json so the runner can read the session id. |
| `dir` | string | no | the process working directory (where you run `catacomb`) | Working directory for the cell. A relative value resolves against the basket file's directory. Mutually exclusive with `workspace`. |
| `env` | map string→string | no | — | Extra environment for the agent child. A variant's `env` wins per key. |
| `checkpoints` | list of strings | no | — | Phase names the agent is expected to mark itself. Charset `^[A-Za-z0-9._:-]+$` (colon allowed here), at most 256 bytes, unique within the task; may not equal the reserved `task:<id>` marker. |
| `timeout` | string (Go duration) | no | no limit | Per-cell deadline, e.g. `30s` or `5m`. Must carry a unit and must not be negative. Covers the workspace command, `setup`, and the child together. |
| `artifacts` | list of glob strings | no | — | Files to capture, globbed relative to the working directory. Each must be local — no `..` escape. |
| `verify` | mapping | no | — | Offline [verifier](#verify) for the task. |
| `workspace` | mapping | no | — | Per-cell [workspace](#workspace) provisioning. Mutually exclusive with `dir`. |

## Verify

The optional `verify` mapping on a task declares an offline verifier. The basket is the
source of truth for it, so a verifier can evolve after the runs were recorded and be
replayed with [`catacomb verify`](cli.md#verify) at zero agent cost.

| Field | Type | Required | Default | Notes |
| --- | --- | --- | --- | --- |
| `cmd` | list of strings | yes (within `verify`) | — | The verifier command, run as a plain `exec`. Same [path resolution](#path-resolution) as a task `cmd`. |
| `env` | map string→string | no | — | Extra environment for the verifier. |
| `timeout` | string (Go duration) | no | 1 minute | Verifier deadline; a unit is required when set. An empty or omitted value defaults to 1 minute. |

## Variant

Each entry of `variants` is a `Variant`: an axis that differs across the matrix, usually
the model or a config flag carried in `env`.

| Field | Type | Required | Default | Notes |
| --- | --- | --- | --- | --- |
| `id` | string | yes | — | Unique within `variants`. Same charset and length rules as a task `id`. |
| `env` | map string→string | no | — | Per-variant environment — the axis that differs. Merged over each task's `env`, winning per key. |
| `setup` | list of strings | no | — | Commands run before the agent in every cell, whitespace-split and run with **no shell**. Must be idempotent, since they re-run before each cell. |
| `workspace` | mapping | no | — | Per-cell [workspace](#workspace). A variant workspace replaces the task's **wholesale** — no field merge. |

## Workspace

The optional `workspace` mapping (on a task or a variant) materializes a **fresh working
directory for every cell**, so repetitions never contaminate each other. This is an
advanced feature; see [ADR-0028](../adr/0028-per-cell-workspace-isolation.md) for the
design and [Workspace isolation](cli.md#workspace-isolation) for the full runtime
behavior (deadlines, teardown, stamps, offline verify).

| Field | Type | Required | Default | Notes |
| --- | --- | --- | --- | --- |
| `cmd` | list of strings | yes (within `workspace`) | — | Provisioning command that creates the cell's fresh working directory. Plain `exec`. |
| `patch` | string | no | — | Path to a patch file, resolved relative to the basket file (absolute paths pass through). Must be readable at load — an unreadable patch rejects the basket. Its sha256 is stamped into evidence, and the absolute path is handed to `cmd` as `CATACOMB_PATCH`. |
| `rev` | string | no | — | Opaque base-revision string, stamped into evidence. Catacomb never interprets it. |
| `teardown` | list of strings | no | — | Cleanup command run in the workspace directory after the cell, unconditionally. Plain `exec`. |

When a cell has both a task and a variant workspace, the variant's replaces the task's;
[`EffectiveWorkspace`](cli.md#workspace-isolation) is the one that runs.

## Path resolution

> A relative path in a basket resolves against the directory containing the basket file,
> never the process's current directory. `dir` is always resolved; every `./`- or
> `../`-prefixed element of `cmd` and `verify.cmd` is resolved; bare words (`python3`)
> and absolute paths are left untouched.

For example, in `["python3", "./verify.py"]` the bare word `python3` is left alone and
found on `PATH`, while `./verify.py` resolves against the basket file's directory. This
is what makes inline `catacomb bench` and offline `catacomb verify` agree on the same
basket regardless of the directory you launch them from. See
[ADR-0029](../adr/0029-basket-relative-path-resolution.md).

Scope: only `dir`, `cmd`, and `verify.cmd` change resolution base this way. `artifacts`
globs resolve against the cell's working directory (and must stay local), and a relative
`workspace.patch` resolves against the basket file through its own loader step — these
keep their own semantics.

An **omitted** `dir` is different: the cell then runs in the process working directory —
your shell's cwd when you invoke `catacomb` — which also governs where `artifacts` globs
resolve. For reproducible runs, set `dir` explicitly or run `catacomb bench` from the
basket's own directory.

## Mutual exclusion: dir vs workspace

`dir` and `workspace` are mutually exclusive — the two working-directory roots would
compete. This is enforced at two levels:

- **Task level:** a single task that declares both `dir` and `workspace` is rejected at
  load.
- **Cross level:** a basket that pairs a variant `workspace` with *any* task that
  declares `dir` is also rejected, because the variant workspace would apply to that
  task's cells too.

Either rejection is `dir and workspace are mutually exclusive`.

## Timeouts

`timeout` values are Go duration strings and **must carry a unit**: `"30s"`, `"5m"`,
`"2h"`. A bare number like `30` is rejected, as is a negative duration. A task's
`timeout` is opt-in — unset means no deadline — and it bounds the whole cell (workspace
command, `setup`, and the child share it). A verifier's `verify.timeout` is separate and
**defaults to 1 minute** when omitted.

## Artifacts

`artifacts` is a list of glob patterns, captured relative to the cell's working
directory. Each pattern must be **local**: its non-glob prefix cannot escape the working
directory with `..`. An empty pattern or one that escapes is rejected as
`invalid artifact glob`. Because workspace directories are ephemeral, a workspace task's
offline verifier can only see what `artifacts` captured — declare globs for whatever the
verifier needs to read again.

## What happens if

- **You omit `reps` (or set it below 1)?** The basket fails to load with
  `reps must be >= 1`.
- **A task declares both `dir` and `workspace`?** Rejected at load with
  `dir and workspace are mutually exclusive` (likewise a variant `workspace` alongside
  any task `dir`).
- **You declare a single variant?** `bench` runs and records evidence normally, but
  `regress` needs at least two variants to gate — with one variant there is nothing to
  compare, and `bench` prints an advisory saying so.
- **A `timeout` has no unit (e.g. `30`)?** Rejected as an invalid timeout — add a unit
  (`30s`).
- **You misspell a field, or two task ids collide into one run-id?** The basket is
  rejected at load: unknown keys are not silently ignored, and colliding run-ids are
  refused.
- **A workspace task's verifier reads a file that was not captured?** Offline `verify`
  runs from the evidence directory with no live workspace, so it fails to find the file
  — capture it with `artifacts`.

## A complete example

A minimal basket that loads — two tasks, two variants, and a verifier. The verifier
script `verify_cart.py` sits next to this file and is referenced as `./verify_cart.py`,
so it resolves the same way inline and offline:

```yaml
basket: checkout
reps: 5

tasks:
  - id: add-item
    cmd: ["claude", "-p", "add an item to the cart", "--output-format", "stream-json"]
    checkpoints: [plan, tests.pass]
    timeout: 5m
    verify:
      cmd: ["python3", "./verify_cart.py"]
      timeout: 30s
  - id: remove-item
    cmd: ["claude", "-p", "remove an item from the cart", "--output-format", "stream-json"]
    timeout: 5m
    verify:
      cmd: ["python3", "./verify_cart.py"]

variants:
  - id: baseline
    env: { MODEL: opus }
  - id: candidate
    env: { MODEL: sonnet }
```

This expands to `2 tasks × 2 variants × 5 reps = 20` cells. Run it with
`catacomb bench checkout.yaml`; re-run the verifiers later with
`catacomb verify checkout.yaml --runs-dir <dir>`.
