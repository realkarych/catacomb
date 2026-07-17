# CLI reference

Catacomb is a single offline binary. Every command reads local files — Claude Code
transcripts under `~/.claude/projects` (or Codex rollouts under `~/.codex/sessions`;
see [Runtimes](ingestion.md#runtimes)), [`bench`](#bench) evidence directories, or the
local SQLite store — and no command opens a network connection. Most read commands
accept `--json` for machine-readable output. For task-oriented recipes see
[workflows.md](workflows.md).

The command set:

| Command | Purpose |
| --- | --- |
| [`bench`](#bench) | Run a benchmark basket and record redacted evidence per cell |
| [`verify`](#verify) | Re-run a basket's verifiers offline over recorded evidence dirs |
| [`import`](#import) | Ingest an already-finished session transcript as an evidence dir |
| [`baseline`](#baseline-set) | Manage named baselines (`set`, `list`, `rm`) |
| [`regress`](#regress) | Compare a candidate run group against a baseline and gate |
| [`trends`](#trends) | Replay a baseline's recorded regression history |
| [`diff`](#diff) | Diff two session transcripts by `step_key` |
| [`subgraph`](#subgraph) | Extract the execution subgraph of a checkpoint phase |
| [`export`](#export) | Export a transcript or evidence dir as a JSONL graph snapshot |
| [`pack`](#pack) | Export a deterministic sample of evidence runs as a bundle for external audit |
| [`replay`](#replay) | Build a graph from one transcript and print a summary |
| [`mcp`](#mcp) | Run the stdio MCP server exposing the `mark` checkpoint tool |
| [`version`](#version) | Print the version |

Exit codes are uniform: `0` success, `1` regression (a stopped `--fail-fast`
basket, or a failing [`verify`](#verify) cell), `2` operational error (bad input,
missing files, store problems).

Any command that parses transcripts (`bench`, `import`, `regress`, `diff`, `subgraph`,
`export`, `replay`) may print up to two advisory lines to **stderr**: a format-drift count for
records it did not recognize, and a version-ceiling notice when a transcript's agent CLI
version — Claude Code or Codex, each with its own ceiling — is newer than the release
this binary was tested against (for example
`warning: transcript Claude Code version 2.2.0 is newer than tested 2.1.199`). Both are
diagnostic only — `stdout`/`--json` stay clean and neither changes the exit code. See
[Format drift](privacy-and-operations.md#format-drift) for what they mean and what to do.

---

## bench

Run a benchmark basket: expand tasks × variants × reps into cells, execute each as a
plain local process, verify checkpoints against the recorded transcripts, and write a
manifest plus one evidence directory per cell.

```sh
catacomb bench <basket.yaml> [flags]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--manifest` | `<basket>.manifest.jsonl` | Manifest output path |
| `--resume` | false | Skip cells already recorded in the manifest |
| `--fail-fast` | false | Stop at the first failing cell |
| `--dry-run` | false | Print the cell expansion table and exit without executing |
| `--projects-dir` | `~/.claude/projects` | Claude projects directory holding session transcripts |
| `--sessions-dir` | `~/.codex/sessions` (or `$CODEX_HOME/sessions` when set) | Codex sessions directory holding rollout transcripts (for a [`runtime: codex`](basket.md#top-level-fields) basket) |
| `--runs-dir` | `~/.catacomb/runs` | Evidence output directory for bench runs |
| `--workspaces-dir` | OS temp dir | Base directory for per-cell workspace dirs (see [Workspace isolation](#workspace-isolation)) |
| `--keep-workspaces` | false | Keep per-cell workspace dirs after teardown; kept paths are printed to stderr |

A basket is a declarative YAML file. `tasks × variants × reps` expands to one *cell* per
combination, and cells run sequentially. The full basket file schema — every field, its
type, and validation rules — is documented in [basket.md](basket.md).

Each cell runs under run-id `bench-<basket>-<task>-<variant>-r<rep>` and carries the
labels `basket`, `task`, `variant`, and `rep`, so `baseline` and `regress` selectors
work unchanged. Cell labels win over any inherited from the `CATACOMB_LABELS`
environment variable. The basket name and each task and variant `id` must match
`^[A-Za-z0-9._-]+$` (no spaces, commas, or `=`, which would corrupt `CATACOMB_LABELS`
and the epilogue selectors) and be at most 256 bytes; task and variant `id`s must be
unique, and baskets whose dash-joined ids would collide into the same run-id are
rejected at load.

Each cell's child runs as a plain local process with `CATACOMB_LABELS` (the merged
ambient + cell labels) and `CATACOMB_RUN_ID` (the cell's run-id) added to its
environment, while the runner peeks its stdout for the stream-json `session_id` and the
terminal `result` event's `total_cost_usd` — a **bench-driven** cell's `cmd` must emit
stream-json (`claude -p <prompt> --output-format stream-json`), which is where the runner
learns the session id. For a session run by hand (interactive TUI), which streams no
stream-json on stdout, record it with [`import`](#import) instead. After the child exits
the runner resolves the session's transcripts under
`--projects-dir` — the main `<session-id>.jsonl` plus any `subagents/agent-*.jsonl` —
retrying for up to ~3 s while the file lands; a session id matching no transcript (or
more than one) records the reason in the cell's manifest `note` and skips verification
and evidence for that cell.

Under a [`runtime: codex`](basket.md#top-level-fields) basket the same contract shifts
vocabularies: the cell's `cmd` must emit the Codex exec JSON stream
(`codex exec --json <prompt>`), where the runner peeks the first `thread.started` event for the
session's **thread id**; after the child exits it resolves the rollout — plus any
subagent rollouts linked by `parent_thread_id` — under `--sessions-dir` instead of
`--projects-dir` (see [Codex sessions](#codex-sessions-runtime-codex)). In a wrapper
script, redirect stdin away (`codex exec --json "$PROMPT" < /dev/null`): when stdin is
not a tty, `codex` reads the prompt from it instead of argv. Codex emits no terminal
cost event, so the cell's manifest and `meta.json` carry no reported `cost_usd` —
exactly like an [imported](#codex-sessions-runtime-codex) Codex session; the
token-derived `cost_usd` *metric* still prices through the built-in OpenAI tiers.
Everything else — run-ids, labels, checkpoints, evidence shape, the epilogue — is
unchanged.

A task's optional `timeout:` — a Go duration string such as `30s` or `5m` — puts a
per-cell deadline on the whole cell: the variant's `setup:` commands and the child
process share one deadline. The value is validated at basket load (an invalid or
negative duration rejects the basket), and it is opt-in: unset means no deadline,
though `Ctrl-C`/`SIGTERM` still cancels the run either way.

For each cell the runner synthesizes `task:<id>` start/end phase markers from the
child's wall-clock start and end, giving `regress` a stable checkpoint axis even when
the agent forgets to mark its own. A task may also declare `checkpoints:` — phase names
the agent is expected to mark *itself* (via `mcp__catacomb__mark`, per a CLAUDE.md
convention; see [mcp](#mcp)) during the run. Each name must match `^[A-Za-z0-9._:-]+$`
(the colon is allowed here, unlike task and variant `id`s), be at most 256 bytes, be
unique within the task, and may not equal the reserved runner marker `task:<id>`. After
each cell the runner rebuilds the graph from the transcripts and checks which declared
checkpoints are present as markers. Verification is best-effort and **never gates**: it
is skipped when the cell surfaced no session id or the transcripts could not be
resolved or parsed (the skip reason is recorded in the manifest `note`). Checkpoints
absent from the graph are recorded in the manifest's `missing_checkpoints` list, warned
to stderr as `cell <run-id>: missing checkpoints: <names>`, and rolled up on success —
just before the copy-pasteable epilogue — into one
`checkpoints[<task>]: <name> <hit>/<verified>` summary line per declared name, where
`hit` counts the cells the marker was found in and `verified` counts the cells where
verification actually ran. Missing phases are visibility only here; they earn a verdict
downstream as presence-rate drops in `regress`.

Each cell whose transcripts resolved — including a cell whose child exited nonzero —
writes an evidence directory `<runs-dir>/<run-id>/` holding
secret-redacted copies of the transcripts (`session.jsonl`, `subagents/agent-*.jsonl`)
plus a `meta.json` (run id, task, variant, rep, session id, labels, exit code,
`cost_usd`, basket hash, the `task:<id>` marker window, and `finished_at`). An
evidence-write failure keeps the cell's result and notes the error. See
[Privacy and operations](privacy-and-operations.md) for what redaction removes.

`meta.json` also carries an `env` block stamping the environment the cell ran under:
`catacomb_version` (the recording binary), `model_id` (the model the child *actually*
ran, read from the transcript's assistant messages — ground truth, not the requested
model; omitted when the transcript carries no assistant turn), `claude_code_version`
(the transcript's Claude Code version; omitted when the transcript does not report
one), and `resources` (`os`, `arch`, and `cpus` of the host that executed the cell).
The stamps are descriptive provenance only — they never gate, join no `--strict` check,
and carry no hostname or runner identity: model drift between groups is often the very
axis under comparison, and host resources legitimately vary across runners. Sampling
parameters are not stamped — Claude Code transcripts do not report them, and the child
argv that could set them is already pinned byte-exactly by the basket hash. Evidence
recorded before the stamps existed simply lacks the `env` key and stays valid
everywhere.

The manifest is JSONL, written incrementally — one object per completed cell (run-id,
task, variant, rep, exit code, session id, `marked`, an optional `missing_checkpoints`
list, `cost_usd`, `evidence_dir`, basket hash, finish time, and an optional `note`).
`--resume` reads it back and skips cells already present; if the basket file changed
since the recorded run (its content hash no longer matches) resume errors out — delete
the manifest or revert the basket. If the manifest already has entries and you pass
neither `--resume` nor a fresh `--manifest`, `bench` refuses (exit `2`) rather than
silently appending a second run's cells. The hash pins the basket bytes only, **not**
the bytes of a referenced `workspace.patch`: editing the patch between a run and its
resume passes the guard and yields one manifest whose cells carry different
`env.workspace.patch_sha256` stamps — the stamps make the drift detectable per cell.

`setup` commands run before **every** cell, in the cell's working directory, as **plain
`exec`**: each line is split on whitespace and run directly, with **no shell** — pipes,
redirects, `&&`, quoting, variable expansion, and globbing are not interpreted. Wrap a
script if you need shell features. Setup inherits **only the parent process
environment** — not the task or variant `env`, and not
`CATACOMB_RUN_ID`/`CATACOMB_LABELS` — and because it re-runs before each cell it must
be **idempotent** (a `git checkout <branch>` is fine; an `echo >> file` that
accumulates is not).

A failing cell is recorded and the basket continues (deciding whether a change
regressed is `catacomb regress`'s job, not the runner's). Exit codes: `0` every cell
ran (even if some cells failed), `1` `--fail-fast` stopped at a failing cell, `2`
operational error (bad basket, a non-fresh manifest, manifest I/O, a resume hash
mismatch, or an unresolvable home directory — set `--projects-dir` (or `--sessions-dir`
for a `runtime: codex` basket) and `--runs-dir` explicitly). On success the runner prints a
`marked <n>/<total> cells` summary, the
checkpoint rollup, and a copy-pasteable epilogue: with two or more variants, a
[`regress --runs-dir`](#regress) comparing the first two. Append `,task=<id>` to the
epilogue's `label:` selectors to narrow the comparison to a single task. When the
basket declares `reps < 5`, the epilogue also appends a one-line note recommending
`reps: 5` or more, because the rate gate cannot fire reliably below that (see
[Gate sensitivity at small k](workflows.md#gate-sensitivity-at-small-k)).

```sh
catacomb bench checkout.yaml
catacomb bench checkout.yaml --dry-run
catacomb bench checkout.yaml --resume --fail-fast
```

### Workspace isolation

A task may declare a `workspace:` block — a user-supplied command that materializes a
**fresh working directory for every cell**, so repetitions never contaminate each other
(rep 2 reading rep 1's leftover files or git history is contamination, not skill). The
canonical use is benchmarking against a pinned revision, optionally with a patch
overlaid:

```yaml
tasks:
  - id: etl-fix
    workspace:
      cmd: ["arc", "mount", "-r", "r123456", "."]
      rev: "r123456"
      teardown: ["arc", "unmount", "."]
    cmd: ["claude", "-p", "...", "--output-format", "stream-json"]
    artifacts: ["out/result.csv"]
    verify: { cmd: ["python3", "./verify_tables.py"] }

variants:
  - id: trunk
  - id: patched
    workspace:
      cmd: ["sh", "apply.sh"]
      patch: fix.patch
      rev: "r123456+fix"
      teardown: ["arc", "unmount", "."]
```

`workspace.cmd` is required within the block and runs as **plain `exec`** (argv, no
shell) — like `verify.cmd`, not the whitespace-split `setup:` lines. `patch`, `rev`,
and `teardown` are optional. A variant may declare its own `workspace:`, which replaces
the task's **wholesale** — no field merging (`env` maps merge; `workspace` replaces) —
so an override that only changes the patch restates `cmd`. `rev` is an opaque string
stamped into evidence; catacomb never interprets it and never runs VCS or diff
semantics of its own — checkout and patch application belong entirely to the
user-supplied command.

For each cell whose effective workspace is declared, the runner creates a fresh
directory named after the cell's run-id under `--workspaces-dir` (default: the OS temp
dir) and runs `workspace.cmd` in it. That directory then becomes the cell's working
directory for everything that follows: the variant's `setup:` commands, the agent
child, artifact capture (`artifacts:` globs resolve against it), and the inline
verifier (its cwd and `CATACOMB_WORKDIR` in bench mode). `dir` and `workspace` are
mutually exclusive — the two roots would compete — so a task declaring both is rejected
at load, as is a basket pairing a variant `workspace` with any task that declares
`dir`. Materialization shares the task's `timeout:`: the workspace command, setup, and
the child all draw on one deadline.

An optional `patch:` names a file resolved relative to the basket file's directory
(absolute paths pass through). At load the file must be readable — its sha256 is
computed once, and an unreadable patch rejects the basket (exit `2`) before any cell
runs. The absolute path is handed to `workspace.cmd` as `CATACOMB_PATCH`, and **only**
to `workspace.cmd` — the agent and the verifier never see the variable (a verifier that
cares reads `rev` and `patch_sha256` from `meta.json`). Beyond `CATACOMB_PATCH`,
`workspace.cmd` inherits only the parent process environment; task and variant `env:`
maps stay agent-scoped, as with `setup:`.

`teardown` (optional argv) runs in the workspace directory after the cell
**unconditionally** — after success, failure, or timeout — on a fresh context with a
1-minute deadline of its own, because the cell's deadline may already be expired and a
leaked mount must not depend on the cell surviving. The directory is then removed,
unless `--keep-workspaces`: teardown still runs, and the kept path is printed to stderr
(`workspace kept: <path>`). A failing teardown or removal appends a note to the cell's
manifest entry and warns to stderr, but never flips a verdict.

A failing `workspace.cmd` — non-zero exit, spawn error, or deadline expiry — is an
**operational cell failure**: the manifest records the exit code and the note
`workspace failed`, no evidence is written, and teardown and cleanup still run.
`--fail-fast` treats it like any other failing cell.

Workspace cells stamp `meta.json`'s `env` block with a `workspace` object carrying the
declared `rev` and the patch bytes' `patch_sha256`. Like the other env stamps these are
descriptive provenance only — they never gate and join nothing. Cells without an
effective workspace carry no `workspace` stamp and run byte-identically to a basket
written before this block existed.

Because workspace directories are ephemeral, offline [`verify`](#verify) is unchanged:
it still runs with cwd = the evidence dir and `CATACOMB_WORKDIR` empty. A workspace
task's verifier must therefore read from captured evidence — declare `artifacts:` globs
for whatever the verifier needs to see again offline.

---

## verify

Re-run a basket's verifiers **offline** over already-recorded evidence directories,
without launching any agent. The basket file is the source of truth for each task's
`verify:` block (a `cmd`, an optional `env`, and an optional `timeout`), so a verifier can
evolve after the runs were recorded — fix a comparator, tighten a judge prompt, add a
score — and be replayed against the saved evidence at zero agent cost.

```sh
catacomb verify <basket.yaml> --runs-dir <dir> [--label k=v[,k=v...]]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--runs-dir` | `~/.catacomb/runs` | Evidence directory holding the recorded [`bench`](#bench) runs to re-verify |
| `--label` | (none) | Restrict to runs whose recorded labels match every comma-separated `k=v` term (AND) |

`verify` scans `<runs-dir>/*/meta.json` and re-verifies each recorded cell whose `basket`
label equals the basket's name, whose task id resolves to a basket task that declares
`verify:`, and — when `--label` is given — whose labels match every term. A cell whose
resolved task carries no `verify:` is skipped silently, as is any run belonging to another
basket or filtered out by `--label`. The variant `env` is taken from the matching basket
variant, resolved by the run's recorded `variant` label; a run whose recorded variant is
no longer in the basket is reported as a per-cell error and does not abort the rest.

Each matched cell runs the task's `verify.cmd` as a **plain `exec`** (argv, no shell) with
its working directory set to the cell's evidence dir and the same `CATACOMB_*` verifier
contract `bench` sets — see [The verifier contract](workflows.md#the-verifier-contract) for
the full table. Offline there is no hot workdir, so `CATACOMB_WORKDIR` is **empty** and a
re-verifiable verifier reads only from evidence. The task and variant `env:` maps and the
verifier's own `verify.env` are layered on top.
The verifier's stdout is scores JSONL (the [run-level scores](#run-level-scores) dialect)
and is rewritten to `<evidence>/scores.jsonl`; stderr passes through to the operator. A
verification record — `cmd`, a sha256 of cmd+env, exit code, duration, timestamp, and
`mode` (`offline` here, `bench` when [`bench`](#bench) ran the verifier inline) — is
written to `<evidence>/verify.json`, leaving the immutable `meta.json` execution ledger
untouched. Re-verification is idempotent: each run rewrites `verify.json` and
`scores.jsonl`, so a recorded verdict is reproduced by re-running.

A **non-zero verifier exit is an operational failure**, not a failing verdict (a failing
check is `verifier.pass: 0` at exit `0`): its scores are not applied and the failure is
recorded in that cell's `verify.json`. `verify` prints one line per matched cell to stdout
— `verify <run-id>: ok` or `verify <run-id>: error (<detail>)` — and when any matched
cell's recorded basket hash differs from the current basket file it prints one advisory
line to stderr (`warning: basket hash differs from recorded runs (verifiers may be newer
than the evidence)`).

Exit codes: `0` every matched cell verified cleanly, `1` one or more operational verifier
failures (each recorded in its `verify.json`), `2` operational error (a bad basket, an
unreadable `--runs-dir`, or a selector that matched no runs).

`verify` slots between [`bench`](#bench) and [`regress`](#regress): record once, iterate on
the verifiers offline, then gate. `regress --runs-dir` auto-loads each cell's rewritten
`scores.jsonl` and gates on `verifier.pass` by default (see
[Run-level scores](#run-level-scores)). See
[The bench → verify → regress cycle](workflows.md#the-bench--verify--regress-cycle) in the
workflows guide for the worked loop.

---

## import

Ingest an **already-finished** session transcript — including one from a session you ran
by hand in the interactive TUI — into a [`bench`](#bench)-cell-shaped evidence directory,
so [`verify`](#verify) and [`regress`](#regress) read it with no special case. Where
`bench` launches the agent and records what it observes, `import` records a run you drove
yourself: you ran the agent, `import` shapes its transcript into the same evidence.

```sh
catacomb import <basket.yaml> --task <id> --variant <id> \
  (--session-id <uuid> | --transcript <path>) [flags]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--task` | **required** | Task id in the basket — selects its `verify`, `checkpoints`, and labels |
| `--variant` | **required** | Variant id in the basket |
| `--session-id` | (none) | Session UUID resolved under `--projects-dir` (for a `runtime: codex` basket: the thread id, resolved under `--sessions-dir`); mutually exclusive with `--transcript` (exactly one required) |
| `--transcript` | (none) | Direct path to a main session `.jsonl` (for `runtime: codex`: a rollout `.jsonl` or `.jsonl.zst`); mutually exclusive with `--session-id` |
| `--rep` | `1` | Repetition index, recorded as the `rep` label |
| `--run-id` | `import-<basket>-<task>-<variant>-r<rep>` | Evidence dir name under `--runs-dir` |
| `--projects-dir` | `~/.claude/projects` | Claude projects dir holding session transcripts (for `--session-id`) |
| `--sessions-dir` | `~/.codex/sessions` (or `$CODEX_HOME/sessions` when set) | Codex sessions dir holding rollout transcripts (for `--session-id` under `runtime: codex`) |
| `--runs-dir` | `~/.catacomb/runs` | Evidence output directory |
| `--label` | (none) | Extra ambient labels merged under the cell labels (`k=v`, comma-separated) |

The **basket is the source of truth** for the cell's `task`, `variant`, `verify:`,
`checkpoints:`, and labels — exactly as under `bench`. The task `cmd` is **ignored**: you
ran the agent yourself, so there is nothing for `import` to launch. `--task` and
`--variant` must name a task and variant that exist in the basket.

Two input modes select the transcript, and **exactly one is required**:

- `--session-id <uuid>` resolves the session's transcripts under `--projects-dir` — the
  main `<session-id>.jsonl` plus any `subagents/agent-*.jsonl` — the same resolution
  `bench` does after its child exits. Pin the id up front so this always works (see the
  workflow below).
- `--transcript <path>` points straight at a main session `.jsonl` (its subagent
  transcripts are read from `<transcript-dir>/<session>/subagents/agent-*.jsonl`). Reach
  for it when the session id is unknown — the newest file under
  `~/.claude/projects/<encoded-cwd>/` is the session you just ran.

Under a [`runtime: codex`](basket.md#top-level-fields) basket both modes exist but
resolve against Codex's rollout files instead — see
[Codex sessions](#codex-sessions-runtime-codex) below.

`import` writes `<runs-dir>/<run-id>/` — `session.jsonl`, `subagents/agent-*.jsonl` when
present, and a `meta.json` — secret-redacted and shaped like a bench cell's evidence dir.
It carries the same redacted transcripts and `meta.json` a bench cell does, so the
transcript-driven downstream commands read it unchanged; the one thing it does **not**
carry is a captured `artifacts/` directory. The default run-id is
`import-<basket>-<task>-<variant>-r<rep>` (the `bench-` prefix a bench cell carries becomes
`import-`), overridable with `--run-id`. On success it prints one line,
`import <run-id>: <dir>`.

`import` captures **no `artifacts/`**: it runs no task `cmd`, so there is no live workdir
to copy files from. A `verify:` that reads a captured artifact — `cell.artifact("out/result.csv")`,
say — therefore cannot score an imported cell; it errors with no verdict. A verifier meant
to run over imports should read the transcript and the rest of the evidence dir, not
`artifacts/`.

The `task:<id>` marker window is synthesized from the transcript's first and last record
timestamps, giving `regress` the same stable phase axis a bench cell carries. Any
`mcp__catacomb__mark` checkpoints the agent recorded during the session are honored;
a `checkpoints:` name the task declares but the transcript never marked is **warned to
stderr** (`import <run-id>: missing checkpoints: <names>`) and never gates — visibility
only, exactly as under `bench`.

`meta.json`'s `cost_usd` field is **omitted** for an import: an interactive transcript carries
no terminal `total_cost_usd` for `import` to read, so the field is left unset and its key never
appears in the file. The token-derived `cost_usd` *metric*
still works — it is priced from the transcript's token counts through the built-in pricing
table — so cost gating in [`regress`](#regress) stays comparable between imported and
bench-recorded runs. The table carries OpenAI GPT-5-family tiers alongside the Anthropic
ones, so this holds for Codex sessions too — with one long-context caveat; see
[Codex sessions](#codex-sessions-runtime-codex).

Verification stays a **separate step**: `import` only records evidence. Run
[`verify`](#verify) afterward to score the task's `verify:` block over it, then
[`regress`](#regress) to gate — the same `bench → verify → regress` cycle, entered one cell
at a time.

Exit codes: `0` the evidence dir was written, `2` operational error (a bad basket, an
unknown `--task` or `--variant`, neither or both of `--session-id`/`--transcript`, an
unresolvable transcript, a transcript with no timestamped records, or an evidence-write
failure).

### Codex sessions (`runtime: codex`)

When the basket declares [`runtime: codex`](basket.md#top-level-fields), `import`
ingests OpenAI Codex CLI sessions. Codex persists each session as a **rollout** —
`~/.codex/sessions/YYYY/MM/DD/rollout-<timestamp>-<thread-id>.jsonl`, zstd-compressed
to `.jsonl.zst` when cold; catacomb reads both forms (see
[Runtimes](ingestion.md#runtimes)). The two input modes become:

- `--session-id <thread-id>` resolves the rollout under `--sessions-dir` (default
  `~/.codex/sessions`, or `$CODEX_HOME/sessions` when set). The session id here is
  Codex's **thread id**: `codex exec --json` announces it as the first
  `thread.started` event on stdout, and it is the
  trailing UUID of the rollout filename. Subagent rollouts are discovered anywhere
  under `--sessions-dir` by the `parent_thread_id` recorded in each child's first line
  — transitively, so nested subagents come along — and land in evidence as
  `subagents/agent-<thread-id>.jsonl`.
- `--transcript <path>` points straight at a rollout file (`.jsonl` or `.jsonl.zst`);
  the thread id is derived from the filename, so the file must keep its
  `rollout-<timestamp>-<thread-id>.jsonl[.zst]` name. Subagent children are discovered
  under the transcript's own directory — the day directory — so a session whose
  subagents span midnight into the next day's directory needs `--session-id` mode
  instead.

Checkpoints work exactly as under Claude Code: register the catacomb [`mcp`](#mcp)
server in Codex's config (`~/.codex/config.toml`),

```toml
[mcp_servers.catacomb]
command = "catacomb"
args = ["mcp"]
```

and the agent's `mcp__catacomb__mark` calls ride the rollout as MCP tool-call records,
reducing to the same marker nodes and honoring the task's `checkpoints:`.

Cost semantics keep one Codex-specific wrinkle: rollouts report token usage but no
dollar cost, so the *reported* `cost_usd` in `meta.json` stays absent — there is nothing
to read. The token-derived `cost_usd` *metric* is estimated, though: the built-in
pricing table carries OpenAI GPT-5-family tiers
([ADR-0031](../adr/0031-multi-runtime-ingestion-codex.md) stage 2) — `gpt-5.4-mini`
prices at $0.75 input / $0.075 cache-read / $4.50 output per MTok, for example — and
model ids without a published price, such as `codex-auto-review`, stay unpriced rather
than guessed. One caveat: OpenAI bills prompts past 272K input tokens on its 1M-context
models at 2× input / 1.5× output, which catacomb's flat estimate does not model, so
long-context requests are undercounted. Token metrics follow Claude Code semantics:
`tokens_in` counts **uncached** input (the rollout's `input_tokens` minus
`cached_input_tokens`), cached input is tracked separately and priced at the cache-read
rate, and a cache write maps through whenever the rollout reports one. `tokens_in`,
`tokens_out`, and `duration_ms` are first-class and gate normally.

Everything else is unchanged: the evidence dir has the same shape — with the runtime
and the rollout's CLI version stamped into `meta.json`'s `env` block as
`agent_runtime`/`agent_version` — and [`verify`](#verify) and [`regress`](#regress)
consume it with no special case.

### Recommended workflow

Pin the session id before you start so `import` can always find the transcript afterward:

```sh
SID=$(uuidgen)
claude --session-id "$SID" --mcp-config catacomb-mcp.json
# … do the task by hand; call mcp__catacomb__mark at each checkpoint …
catacomb import basket.yaml --task work-task --variant candidate --session-id "$SID"
catacomb verify basket.yaml --runs-dir ~/.catacomb/runs
```

When you did not pin the id, fall back to `--transcript` with the newest transcript under
your project's Claude projects dir:

```sh
catacomb import basket.yaml --task work-task --variant candidate \
  --transcript "$(ls -t ~/.claude/projects/<encoded-cwd>/*.jsonl | head -1)"
```

See [Importing a hand-run interactive session](workflows.md#importing-a-hand-run-interactive-session)
for the full recipe.

---

## baseline set

Create or replace a named baseline from a label selector over evidence directories,
resolved now.

```sh
catacomb baseline set <name> --label k=v [--label ...] [flags]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--label` | **required** (repeatable) | `k=v` selector; the baseline captures every run matching all terms (AND) |
| `--db` | `~/.catacomb/catacomb.db` | SQLite database path for the baselines table |
| `--runs-dir` | `~/.catacomb/runs` | Evidence dir to resolve the label selector from |

The command scans `<runs-dir>/*/meta.json`, matches the labels against each run's
recorded labels (all terms ANDed), sorts the matching run IDs, and persists them with
the selector, a created-at timestamp, the runs dir they were resolved from, and the
version stamps of the setting binary — the catacomb version and the step-key scheme —
under `<name>`; `regress` checks those stamps whenever the baseline is resolved by name
(see [Baseline version stamps](#baseline-version-stamps)). The store at `--db` is
created if it does not exist yet, so the very first store-touching command can be this
one.

The name must be non-empty, at most 128 bytes, and free of leading or trailing
whitespace; at least one `--label` is required. Errors when the selector matches no
runs. Re-running with the same name replaces the stored baseline. The evidence dirs are
not copied — [`regress`](#regress) re-reads the pinned runs from disk and warns when
pointed at a different directory. A saved baseline is referenced by `regress` as
`name:<baseline>`, so a golden group survives later label churn.

```sh
catacomb baseline set golden --label basket=checkout --label variant=main
catacomb baseline set golden --label basket=checkout,variant=main --runs-dir ~/.catacomb/runs
```

---

## baseline list

List stored baselines.

```sh
catacomb baseline list [flags]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--db` | `~/.catacomb/catacomb.db` | SQLite database path |
| `--json` | false | Emit JSON output |

Prints a table with columns `NAME`, `RUNS`, `SELECTOR`, `CREATED`, sorted by name;
`SELECTOR` shows the sorted `k=v` terms and `CREATED` is a UTC RFC3339 timestamp.
`--json` emits the stored records including each baseline's resolved run IDs, the
`runs_dir` it was resolved from, and the version `stamps` (catacomb version and
step-key scheme) recorded at set time. On a store created by an older binary this
command fails with a hint to run a write-path command (`baseline set`) to migrate the
schema.

```sh
catacomb baseline list --json
```

---

## baseline rm

Remove a stored baseline.

```sh
catacomb baseline rm <name> [flags]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--db` | `~/.catacomb/catacomb.db` | SQLite database path |

Deletes the named baseline.

```sh
catacomb baseline rm golden
```

---

## regress

Compare a candidate run group against a baseline and gate on the verdict.

```sh
catacomb regress --baseline <selector> --candidate <selector> [flags]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--baseline` | (empty) | Baseline selector: `label:k=v[,k=v...]` or `name:<baseline>` |
| `--candidate` | (empty) | Candidate selector (same grammar) |
| `--runs-dir` | `~/.catacomb/runs` | Evidence dir to resolve selectors from: `label:` scans it, `name:` reads `--db`'s baselines table, `--record` appends there |
| `--db` | `~/.catacomb/catacomb.db` | SQLite database path for `name:` baselines and `--record` |
| `--format` | `human` | Output format: `human`, `json` (the full report as JSON), or `markdown` (a PR-comment-ready document; see [Report formats](#report-formats)) |
| `--json` | false | **Deprecated** alias for `--format json` (prints a deprecation notice to stderr); an explicit `--format` wins when both are given |
| `--strict` | false | Treat an insufficient-data verdict as a failure (exit `1`); refuse a stampless or stamp-mismatched `name:` baseline (exit `2`). A basket with fewer tasks than `--paired-min-tasks` always carries paired `insufficient` findings, so with every other axis clean it reports `insufficient` — never `ok` — and fails `--strict` structurally: more repetitions cannot fix it; add tasks, or lower `--paired-min-tasks` deliberately |
| `--record` | false | Append this comparison to the baseline's history for [`trends`](#trends) (requires `--baseline name:<x>`) |
| `--project` | (empty) | Project identity stamped into the recorded history row (`project` in the record body) for fleet-level joins; requires `--record` |
| `--annotation` | (none) | Numeric annotation to gate on: `owner.key[:higher-better\|lower-better]` (repeatable) |
| `--scores` | (none) | JSONL file of external scores applied as node annotations before comparison (see [Gating on external scores](#gating-on-external-scores)) |
| `--min-support` | 3 | Minimum runs per group for a trusted comparison (must be ≥ 1) |
| `--presence-delta` | 0.2 | Presence-rate delta threshold |
| `--error-delta` | 0.1 | Error-rate delta threshold |
| `--annotation-rate-delta` | 0.1 | Rate delta threshold for run-level binary annotations (e.g. `verifier.pass`; must be > 0) |
| `--paired-alpha` | 0.05 | Significance level for the paired per-task test (must be in (0,1)) |
| `--paired-min-tasks` | 5 | Minimum matched tasks before the paired test can gate (must be > 0) |
| `--paired-test` | `sign` | Paired per-task test: `sign` (exact sign test) or `wilcoxon` (exact Wilcoxon signed-rank; see the sign-vs-wilcoxon note below) |
| `--metric-rel-delta` | 0.25 | Relative metric delta threshold |
| `--iqr-factor` | 1.5 | IQR band factor for the metric noise band |
| `--audit-iqr-factor` | 3.0 | IQR band factor for [per-cell outlier audit](#per-cell-outlier-audit) flags (must be > 0) |
| `--audit-rel-delta` | 0.5 | Relative delta floor for per-cell outlier audit flags (must be > 0) |
| `--coverage-floor` | 0.7 | Step-alignment coverage below which step verdicts are downgraded |
| `--z` | 1.645 | One-sided Wilson z for the rate gates (`1.645` = 95% one-sided); higher z requires stronger evidence to flag (flags less) |
| `--fail-on-notable` | false | Count `notable` findings toward the gate (exit `1`) |

Both selectors must be supplied, and both resolve against [`bench`](#bench) evidence
directories under `--runs-dir`:

- `label:k=v[,k=v...]` scans `<runs-dir>/*/meta.json`, matches the terms against each
  run's recorded labels (all terms ANDed), and rebuilds every matching run's graph from
  its redacted transcripts, re-applying the `task:<id>` marker window from `meta.json`
  so checkpoint phases and run timing carry over. No store is touched.
- `name:<baseline>` reads the baseline row from the `--db` baselines table (read-only)
  and loads its pinned run IDs from `<runs-dir>/<run-id>/` — every pinned run's
  evidence dir must be present and readable, or the command exits `2` naming the run
  and dir. A baseline records the runs dir it was resolved from; when the `--runs-dir`
  flag names a different directory, a stderr warning notes the recorded dir and the
  flag wins.

Both loaders strip node payloads before the groups are held for aggregation — the
gate never reads them — so `regress` memory scales with graph structure, not
transcript size; see [Scale](privacy-and-operations.md#scale) for the measured
envelope.

Groups are aggregated and compared per
[ADR-0022](../adr/0022-regression-detection-over-repeated-runs.md) §4:

- **Rates** (presence, error, and run-level binary annotations such as `verifier.pass`)
  use one-sided Wilson bounds (default z `1.645`, tunable with `--z`) and are flagged as a
  `regression` only when the baseline and candidate bounds are disjoint *and* the delta
  exceeds the threshold (`--presence-delta`, `--error-delta`, or `--annotation-rate-delta`
  respectively); a delta over the threshold with overlapping bounds is reported as
  `notable`, which gates only under `--fail-on-notable`. When even a maximal flip at the
  actual group sizes cannot reach `regression`, the report (human and `--json`) carries a
  `sensitivity:` note naming the smallest `k` at which the gate could fire; see
  [Gate sensitivity at small k](workflows.md#gate-sensitivity-at-small-k).
- **Metrics** (`duration_ms`, `cost_usd`, `tokens_in`, `tokens_out`, `occurrences`; run
  totals also `nodes`) flag the candidate median when it falls outside the baseline
  median ± `max(metric-rel-delta × |median|, iqr-factor × IQR)` band. The `nodes` and
  `occurrences` count metrics are one-sided (higher = flagged) per
  [ADR-0022 Amendments](../adr/0022-regression-detection-over-repeated-runs.md#amendments),
  so a pipeline that legitimately grew may need `--metric-rel-delta` raised to keep
  ordinary growth inside the band.
- **Paired per-task deltas** (scope `paired`): when both groups carry `task` labels
  (any [`bench`](#bench) basket does), every task present in both groups with
  `--min-support` runs per side contributes one delta per continuous metric
  (`duration_ms`, `cost_usd`, `tokens_in`, `tokens_out`) — the candidate per-task
  median minus the baseline per-task median. An exact one-sided sign test over the
  non-zero deltas (zero deltas are dropped) flags `regression` when the probability of
  seeing that many increases under no change is at most `--paired-alpha`; the detail
  always carries the evidence (`+7/8 tasks, p=0.03516`), `improvement` is symmetric,
  and a paired `regression` gates (exit `1`) like any other. All four paired metrics
  are tested at the same `--paired-alpha` with no multiplicity correction: they are
  strongly correlated, so the aggregate false-positive rate under the null is bounded
  by ~4× alpha and lands well below that in practice. This is the axis that
  catches systematic drift *below* the metric band: a +10% cost creep repeated across
  8 tasks fires at p=0.0039 while staying inside every median band. Fewer than
  `--paired-min-tasks` matched tasks reports `insufficient` instead of a guess, and
  whenever the paired layer is active but cannot fire — too few matched tasks, or
  unanimity at the current task count cannot reach `--paired-alpha` — the
  `sensitivity:` note names the smallest task count at which a unanimous shift would
  gate. An `ok` paired row is omitted from the findings like any non-total row. See
  [when the paired test fires](workflows.md#catching-drift-below-the-band-the-paired-sign-test).
  **Sign vs wilcoxon:** `--paired-test wilcoxon` swaps in an exact Wilcoxon
  signed-rank test per metric — it *replaces* the sign test rather than running
  alongside it, so the paired family stays those same four metrics. Where the sign
  test only counts delta directions, Wilcoxon ranks the delta magnitudes (mid-ranks
  on ties; zero deltas still dropped) and computes the exact null distribution by a
  deterministic, RNG-free dynamic program — no asymptotic approximation. That buys
  real power in 6–10-task baskets: one small-magnitude discordant task among six
  fires at p=0.031 (detail `W+ 20/21 over 6 tasks, p=0.03125`) where the sign test's
  5/6 stalls at p=0.109. The reachability floor is unchanged (a unanimous shift has
  p=2^-n under both tests), so the `sensitivity:` note reads the same; the default
  remains `sign`.
- **Alignment coverage** (fraction of baseline steps matched in the candidate) is
  always reported; below `--coverage-floor` step-level regressions are downgraded to
  `notable` and the checkpoint (phase) level carries the verdict (under
  `--fail-on-notable` those downgraded findings still gate).
- Groups below `--min-support` yield an `insufficient` verdict instead of a guess.

### Task reliability (pass^k)

When both groups carry per-task `verifier.pass` outcomes (a [`bench`](#bench) basket
with a [verifier](workflows.md#verifying-task-outcomes)), the report carries a
`reliability` block: for a task with `n` scored runs and `c` passes,
`pass^k = C(c,k)/C(n,k)` — the unbiased estimate of "all `k` independent trials
succeed" — computed for `k` = 1..`k_max`, where `k_max` is the smallest scored `n` in
the group so curves stay comparable across tasks. `--json` carries the full per-task
curves plus the unweighted `mean` curve over tasks; the human report renders one
epilogue line per group with the mean curve's endpoints:

```text
reliability (candidate): pass^1 0.93 -> pass^5 0.67 (7 tasks)
```

A flat curve is a reliable agent; a steep drop from pass^1 to pass^k is a coin-flipper
that happens to average well. The block is **informational only — it never gates**: the
same binary data already gates through the `ann:verifier.pass` rate axis
([run-level scores](#run-level-scores)), and double-gating one signal would only
inflate false positives.

### Per-cell outlier audit

The gate compares group medians, so a single anomalous cell — a run that spends wildly
more tokens or turns than its group — which is how gaming, retry loops, and runaway
tool use look from the outside — can hide inside a clean verdict. Every comparison therefore also
screens the individual cells (one cell = one run) of both groups. For each group and
each of `duration_ms`, `cost_usd`, `tokens_in`, `tokens_out`, and `turns` (the run's
assistant-turn count), a cell value `v` is flagged against the group median `m` when

```text
|v − m| > max(audit-rel-delta × |m|, audit-iqr-factor × IQR)
```

with the group's `IQR = P75 − P25`. The defaults are deliberately less twitchy than the
gate's own noise band: `--audit-iqr-factor` 3.0 (Tukey's far-out fences, vs the gate's
1.5) and `--audit-rel-delta` 0.5 — a cell must sit at least 50% away from the group
median even when the IQR collapses to 0, which keeps near-identical groups quiet.
Groups with fewer than 3 cells produce no flags (a median and IQR of 1–2 points is
noise). Both flags must be > 0 (operational error, exit `2`).

Flags land in the report as an `audit` block — in `--json`, `baseline`/`candidate`
arrays of `{run_id, task, metric, value, median, band}` sorted by run id — and in the
human report as one epilogue line per flag:

```text
audit: candidate run r07 (task sql) tokens_out 1932 vs group median 243 (band 121.5)
```

The block is **structurally non-gating**: it is computed after the findings, feeds no
verdict, and never affects the exit code. A flagged cell is an invitation to read that
run's evidence — [`pack`](#pack) bundles it for review — not a regression. When no flag
fires the block is omitted entirely. Treat `cost_usd` and `duration_ms` flags with
care: under prompt caching, real per-run cost spreads up to ~5× between byte-identical
runs (measured 2026-07-08, [PV-6b](../internal/reviews/2026-07-08-pv6b-live-calibration.md)), and wall-clock duration
inherits runner load — `tokens_out` and `turns` are the trustworthy anomaly axes. In
particular, the first run of a bench batch often pays a cold-start premium and can flag
on `duration_ms` routinely — expected, not an anomaly.

## Baseline version stamps

`baseline set` stamps every baseline with the versions that resolved it: the catacomb
version and the step-key scheme (`stepkey/v1`). Whenever `--baseline` resolves by
`name:`, `regress` compares those stamps against its own: a baseline with no stamps
(set by a pre-stamp catacomb) or with differing stamps prints a stderr warning, and
under `--strict` is refused as an operational error (exit `2`) instead. A step-key
scheme change shifts step identity between the pinned runs and the candidate, so a
cross-version comparison can quietly align nothing; after upgrading catacomb, re-run
`baseline set` to re-pin the group under the current stamps. The check covers the
`--baseline` side only (`label:` selectors carry no stamps, and a `name:` candidate is
not checked), and stdout and `--json` stay clean either way.

## Gating on external scores

`--annotation owner.key` folds a numeric annotation — an external scorer's verdict
supplied from a `--scores` file — into the comparison as if it were a built-in metric.
The annotation values aggregate per `step_key` and are flagged with the same median ±
`max(metric-rel-delta × |median|, iqr-factor × IQR)` band as other metrics, but with a
declared direction. When the same `step_key` occurs more than once within a single run,
that run's annotation values for the key are **summed** (like cost and tokens), and the
compared medians are taken across the per-run sums:

- `owner.key` or `owner.key:higher-better` (default): a higher score is better, so a
  candidate median that drops below the band is the `regression` and a rise is an
  `improvement`.
- `owner.key:lower-better`: a lower score is better, so the direction inverts — a rise
  is the `regression`.

The flag is repeatable and each key gates independently; a duplicate key (across flags
or directions) is an operational error (exit `2`). Annotation gating runs at two scopes —
individual **steps** (per `step_key`, described here) and the **run total** (run-level
scores, [below](#run-level-scores)); phase rows carry no annotation block per
[ADR-0022 Amendments](../adr/0022-regression-detection-over-repeated-runs.md#amendments).
Because a score is only sampled on the runs that
actually carry it, an annotation's `N` can be below the step's `Present` (a step
reached in every run but scored in only some); an annotation whose `N` falls below
`--min-support`, or one present on only one side, is reported `insufficient` rather
than guessed. A configured key that never fires on any step in either group prints a
warning to stderr (stdout and `--json` stay clean).

`--scores file.jsonl` supplies the values. Each line is one JSON object:

```json
{"step_key": "1f0c9a4b2d8e7f36", "key": "deepeval.tool_correctness", "value": 0.92, "run_id": "bench-checkout-work-task-candidate-r1"}
```

`step_key` names the step the score lands on (take it from the `KEY` column of a
`regress` table, or from [`subgraph --json`](#subgraph) node output), `key` uses the
same `owner.key` grammar as `--annotation`, `value` must be a number, and `run_id` is
optional: set it to score a single run — one line per scored run is the normal shape —
or omit it and the value lands on every run in **both** groups that carries the step
key, which flattens both medians to the same value. A line that omits `step_key` is a
**run-level** score ([below](#run-level-scores)). Extra provenance fields — `tool`,
`tool_version`, `prompt_hash`, or any other key an evaluator emits — are tolerated and
ignored, so a scorer can record its own metadata on the same line. Ignored by the gate
does not mean inert: the `catacomb-judge` utilities read `tool` back as the judge's
identity, to measure judge-vs-gold agreement and to aggregate judge panels into new
`--scores` input — see [Calibrating a judge](workflows.md#calibrating-a-judge). Blank lines are
skipped; a malformed line is an operational error (exit `2`) naming the file and line.
Values apply in memory to both groups before aggregation — nothing is written back to
the evidence dirs or the store. Entries that match no node are counted into a single
stderr warning (`N score entries matched no node`); stdout and `--json` stay clean. The
file only supplies values: each key still needs its `--annotation owner.key[:direction]`
flag to declare a gate — the sole exception is `verifier.pass`, which gates by default
([below](#run-level-scores)) — or the scores are inert.

### Run-level scores

A score line that **omits `step_key`** attaches to the run as a whole rather than to a
node, and gates at the `total` scope alongside `cost_usd`, `nodes`, and the run-total
rates. In a `--scores` file a run-level line **requires `run_id`** (there is no node to
match it by; a run-level line without one is an operational error, exit `2`):

```json
{"step_key": "", "key": "verifier.pass", "value": 1, "run_id": "bench-checkout-work-task-candidate-r1"}
```

- **`verifier.pass` gates by default.** The reserved key `verifier.pass` (higher-better)
  is compared even with no `--annotation` flag, so a candidate whose pass rate drops
  below the baseline flags a `regression` out of the box. Any other run-level key still
  needs `--annotation owner.key[:direction]` to gate.
- **Binary annotations use the rate gate.** When every value for a key is `0` or `1`,
  the key is gated as a rate with one-sided Wilson bounds and the `--annotation-rate-delta`
  threshold (default `0.1`) — the same disjoint-bounds-and-delta rule as presence and
  error rates — and the human `DETAIL` column shows the raw counts as `ones a/n -> b/m`.
  A key with non-`{0,1}` values is treated as continuous and gated with the metric median
  band instead.

Run-level scores can also be supplied without `--scores`: when `regress` resolves runs
from `--runs-dir`, it auto-loads a `scores.jsonl` sitting in each run's evidence dir, and
a run-level line there may omit `run_id` (it defaults to that run's ID). A `--scores`
file is layered on top; when one of its entries sets a key an evidence file already
provided, the flag value wins and a stderr warning notes the count
(`N entries overrode evidence-provided values`).

Comparison runs at four scopes — run totals, paired per-task deltas, checkpoint
phases, and steps. The human
table prints `VERDICT SCOPE KEY NAME METRIC BASELINE CANDIDATE BAND DETAIL` with
presence-normalized values (presence rate, not absence); the `DETAIL` column carries the
per-finding note (raw counts such as `present a/n -> b/m` or `ones a/n -> b/m`, an
`insufficient` reason, or a coverage-downgrade note), and is `-` when empty. `--format json`
emits the full report (presence rows carry absence rates plus the same `detail` field). Exit codes: `0`
pass, `1` regression (or `insufficient` with `--strict`), `2` operational error
(invalid selector, unknown baseline, missing store, empty group, a missing pinned
evidence dir, a [stamp refusal](#baseline-version-stamps) under `--strict`, or
`--min-support` below 1). Resolving a `name:` baseline on a store created by an older
binary also exits `2` with a hint to run a write-path command (`baseline set`) to
migrate the schema.

### Report formats

`--format` selects how the report is rendered; the comparison, verdict, and exit code
are identical across all three:

- `human` (default) — the table described above, for terminals. Its exact layout is
  **not** a compatibility contract.
- `json` — the full report as JSON, for scripts and dashboards. The legacy `--json`
  flag is a **deprecated** alias for `--format json`: it still works (with a stderr
  deprecation notice) and an explicit `--format` wins when both are given.
- `markdown` — the same report as a markdown document sized for a PR comment: a bold
  emoji verdict headline (`**Verdict: ❌ regression**`), a one-line run-count and
  coverage summary, a sensitivity warning when the gate cannot fire at the current
  support, the findings table, and a collapsible `reliability & audit` section when
  those blocks are present. This is the body the bundled
  [catacomb-gate GitHub Action](../../.github/actions/catacomb-gate/README.md) posts
  as its sticky PR comment — see
  [Gate a PR with the Action](workflows.md#gate-a-pr-with-the-action).

## Recording history

`--record` appends the full comparison — candidate selector, thresholds, annotation
specs, and the complete report — to the named baseline's append-only history,
replayable later with [`trends`](#trends). It requires `--baseline name:<baseline>` (a
`label:` group has no stable identity to append under, so `--record` with a `label:`
baseline is an operational error) and opens the store at `--db` read-write for the
append. The record is appended *after* the verdict is rendered
to stdout, and a failed append is itself an operational error (exit `2`) that takes
precedence over the verdict: a regression that could not be durably recorded exits `2`,
not `1`, so a broken store never masquerades as a clean regression signal.

The store must already exist: `--record` requires a `name:` baseline, and resolving one
against an absent store fails first (exit `2`), so the store is created by
[`baseline set`](#baseline-set), never by `--record`. Each record carries the version
stamps of the recording binary (catacomb version and step-key scheme) in its body
alongside the report. With `--project <id>` the body also carries a stable project
identity, so histories exported from many repositories can be joined fleet-side; see
[Roll up a fleet](workflows.md#roll-up-a-fleet). `--project` without `--record` is an
operational error (exit `2`) — the stamp has nowhere to land.

Sequence numbers are assigned atomically in a single statement, so a record is never
silently overwritten. But concurrent `--record` writers against one store file — a
fan-out CI matrix whose shards all record into the same database — can still collide on
SQLite's write lock: a losing writer fails loudly with `SQLITE_BUSY` and exits `2`
without corrupting the history, rather than blocking or tearing a record. Serialize the
recorders (record from one shard, or gate on a lock) or give each shard its own store
file.

```sh
catacomb regress --baseline label:basket=checkout,variant=main \
  --candidate label:basket=checkout,variant=candidate
catacomb regress --baseline name:golden --candidate label:variant=candidate --format json
catacomb regress --baseline name:golden --candidate label:variant=candidate --format markdown
catacomb regress --baseline name:golden --candidate label:variant=candidate --record --strict
catacomb regress --baseline name:golden --candidate label:variant=candidate \
  --scores scores.jsonl --annotation deepeval.tool_correctness
catacomb regress --baseline name:golden --candidate label:variant=candidate \
  --scores verifier.jsonl
```

The last form gates on `verifier.pass` with no `--annotation` flag, since that key gates
by default.

---

## trends

Show the recorded regression history for a baseline — the append-only trail written by
`regress --record`.

```sh
catacomb trends <baseline> [flags]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--db` | `~/.catacomb/catacomb.db` | SQLite database path |
| `--metric` | (empty) | Narrow to one total-scope metric: `duration_ms`, `cost_usd`, `tokens_in`, `tokens_out`, `nodes`, or `error_rate` |
| `--pareto` | false | Render the history as an [accuracy-vs-cost Pareto table](#accuracy-vs-cost-pareto); composes with `--json`, mutually exclusive with `--metric` |
| `--json` | false | Emit the history as JSON |

Records print oldest-first by sequence number. Without `--metric` or `--pareto`, the
wide table prints one row per recorded run — `SEQ CREATED CANDIDATE VERDICT REGRESSIONS
INSUFFICIENT DURATION_MS COST_USD ERROR_RATE` — a per-run scoreboard of the overall
verdict, the finding counts, and the candidate run-total values. `--metric <m>` swaps
to a narrowed table — `SEQ CREATED CANDIDATE VERDICT BASELINE-VALUE CANDIDATE-VALUE
BAND` — tracking one total-scope metric's baseline value, candidate value, and noise
band across the history so drift on a single axis is legible; a run whose report
carries no total-scope finding for that metric renders `-` in those columns. Value
cells are formatted to two decimals. `CREATED` is each record's `created_at` timestamp,
formatted RFC3339 in UTC.

Each record also stamps the baseline's `created_at` at record time. If a row was
recorded against a different definition of the baseline than the one that exists now —
the baseline was deleted and recreated (or re-`set`) under the same name — its `SEQ`
cell carries a trailing `*` and a footnote
`* recorded against a previous definition of this baseline` prints after the table, so
a spliced history is never read as a continuous one.

`--json` emits the raw stored history verbatim as `[{"seq":N,"record":<stored bytes>}]`:
each `record` is the exact JSON body that was written, byte-for-byte, not a
re-encoding. A body carries a schema version field `v` (currently `2`; the schema is
additive-only, and every version from `1` through the current one still renders), the
candidate selector, the `project` identity when recorded with
[`--project`](#regress) (records written without it lack the field), thresholds,
annotation specs, the report, its own `created_at` (RFC3339 UTC), a
`baseline_created_at` stamp mirroring the baseline's `created_at` at record time, and
the recording binary's version `stamps` (catacomb version and step-key scheme; records
written before stamps existed lack the field) — ready for dashboards or diffing
scripts. A record whose `v` is newer than this binary understands is an exit-`2` error
naming the sequence and version (upgrade catacomb).

### Accuracy-vs-cost Pareto

`--pareto` re-reads the same recorded history as a two-axis trade-off table. Each
record contributes one point: accuracy is its total-scope `ann:verifier.pass` finding's
candidate value (the pass rate the gate already uses) and cost is its total-scope
`cost_usd` finding's candidate value (the median per-run cost); one `baseline` row is
added from the newest record's baseline values of the same two findings. Nothing is computed or persisted
anew — every value comes from reports already stored by `--record`.

```text
SEQ  CREATED               CANDIDATE                 ACCURACY  COST_USD  DOMINATED
-    -                     baseline                  1.00      0.0102    no
2    2026-07-12T18:04:11Z  label:variant=cand        1.00      0.0102    no
1*   2026-07-12T17:58:03Z  label:variant=degraded    0.00      0.0102    yes
3    2026-07-12T18:09:44Z  label:variant=old         -         0.0110    -
```

A row is `DOMINATED yes` when some other row is at least as accurate and at most as
costly, with strict advantage on at least one axis — rows equal on both axes (the
first two above) do not dominate each other and both stay `no`. Rows sort by cost
ascending, then accuracy descending, then sequence ascending, so the Pareto frontier
reads top-down. A row that lacks an axis — either accuracy (a record whose report
carries no total-scope `ann:verifier.pass` measurement: pre-verifier history, or a
mixed comparison where only one side carried the verifier annotation) or cost (a
report with no total-scope `cost_usd` finding) — carries no domination verdict: it
renders `-` in the missing cells and in `DOMINATED`, sinks to the bottom of the table
in sequence order, and one epilogue note counts how many rows were not compared.
Evidence without reported cost aggregates as cost `0.0`, not as a missing axis — a
record written by this binary always carries a cost value (it renders `0.0000` and
can dominate); the missing-cost path serves records produced by other writers. The
splice marker (`*` and its footnote) applies unchanged.

`--pareto --json` emits `{"baseline": "<name>", "points": [...]}` instead: every point
carries `source` (`"baseline"` or `"record"`), and record points add `seq`,
`candidate`, `created_at`, and `spliced` (the baseline point carries none of those).
`accuracy` and `cost_usd` are omitted when the record's report carries no measurement
for that axis (the finding is missing, or is the absence placeholder a one-sided
annotation history produces), and `dominated` is omitted — not `false` — for a point
that lacks an axis.

`--pareto` is mutually exclusive with `--metric` (operational error, exit `2`) and
composes with `--json`.

Exit codes: `0` success, `2` operational error. An unknown `--metric` (outside the set
above), `--pareto` combined with `--metric`, an unknown baseline (`baseline not
found`), a known baseline with no recorded runs (`has no recorded regress runs`), and a
record written by a newer schema version are distinct exit-`2` errors, as are a missing
store and one created by an older binary whose schema needs migrating (run a write-path
command such as `baseline set`). `trends` opens the store read-only and never migrates
it.

```sh
catacomb trends golden
catacomb trends golden --metric error_rate
catacomb trends golden --pareto
catacomb trends golden --json
```

---

## diff

Diff two session transcripts by `step_key`.

```sh
catacomb diff <A.jsonl> <B.jsonl> [flags]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--json` | false | Emit JSON output |
| `--phase` | (empty) | Scope both sides to this phase (`name` or `name,occurrence`) |
| `--a-phase` | (empty) | Scope side A to this phase |
| `--b-phase` | (empty) | Scope side B to this phase |
| `--a-from` | (empty) | Range start checkpoint for side A |
| `--a-to` | (empty) | Range end checkpoint for side A |
| `--b-from` | (empty) | Range start checkpoint for side B |
| `--b-to` | (empty) | Range end checkpoint for side B |

Reports added, removed, changed, and unchanged steps with per-field deltas (args,
status, cost, duration, tokens). A within-run phase comparison uses the same transcript
on both sides with different `--a-phase`/`--b-phase` selectors. `from`/`to` must be set
together per side and are mutually exclusive with that side's phase selector. See
[workflows.md](workflows.md) for the checkpoints + diff recipe.

```sh
catacomb diff run-a.jsonl run-b.jsonl --phase eval-loop --json
```

---

## subgraph

Extract the execution subgraph of a checkpoint phase from a session transcript.

```sh
catacomb subgraph <session.jsonl> [flags]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--phase` | (empty) | Phase selector (`name` or `name,occurrence`); mutually exclusive with `--from`/`--to` |
| `--from` | (empty) | Range start checkpoint |
| `--to` | (empty) | Range end checkpoint |
| `--json` | false | Emit `{nodes, edges}` JSON instead of summary lines |

Prints `nodes: N  edges: M` followed by node lines, or structured JSON with `--json`.

```sh
catacomb subgraph session.jsonl --phase eval-loop --json
```

---

## export

Export a transcript or evidence directory as a JSONL graph snapshot.

```sh
catacomb export <transcript.jsonl | evidence-dir> [flags]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--to` | `jsonl` | Export format: `jsonl` |
| `--out` | (empty) | Write to file instead of stdout |

The input is either a single transcript file or a [`bench`](#bench) evidence directory.
A directory loads like a `regress` run: `session.jsonl` plus `subagents/agent-*.jsonl`,
with the `task:<id>` boundary markers re-applied from `meta.json`. The output is the
materialized graph as JSONL — `{"kind":"node"…}`, `{"kind":"edge"…}`, and
`{"kind":"run"…}` records — the input format of the
[DeepEval bridge](https://github.com/realkarych/catacomb/tree/master/integrations/deepeval).

```sh
catacomb export ~/.catacomb/runs/bench-checkout-add-item-candidate-r1 --out run.jsonl
catacomb export ~/.claude/projects/my-project/<session>.jsonl --to jsonl
```

---

## pack

Export a deterministic sample of evidence runs as a bundle for external audit.

```sh
catacomb pack <selector> --runs-dir <dir> --out <dir> [flags]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--runs-dir` | (empty) | Evidence dir to resolve the selector from (required) |
| `--out` | (empty) | Bundle output dir; must not already exist (required) |
| `--sample` | 3 | Number of runs to sample by run-id stride (must be > 0) |
| `--db` | `~/.catacomb/catacomb.db` | SQLite database path for `name:` selectors |

The selector is the same `label:`/`name:` grammar as [`regress`](#regress), resolved
against the same evidence dirs (a `name:` baseline reads its pinned run IDs from
`--db`).

Sampling is deterministic — no RNG: the selected runs are sorted by run ID and
`--sample N` takes the evenly spaced indices `floor(i × len / N)` for `i = 0..N−1`
(fewer than `N` runs means all of them). The stride covers the range —
first/middle/last rather than clustering on the head — and two invocations over the
same evidence always pick the same cells, so a pack is reproducible from the run list
alone.

Each sampled run's evidence dir is copied **verbatim** into `<out>/<run_id>/`:
`session.jsonl` and `subagents/` transcripts, `meta.json`, `scores.jsonl`,
`verify.json`, and captured `artifacts/` where present. Evidence content is
secret-redacted at write time
([ADR-0024](../adr/0024-secrets-at-rest-write-path-redaction.md)), so the bundle
inherits the redaction guarantee with no second pass — with the same caveats as
evidence at rest: `verify.json` error text and binary artifacts pass through
unredacted (see the [fidelity notes](workflows.md#what-capture-does-to-artifacts)),
so review them before shipping a pack to an external service. Alongside the run dirs:

- `pack.json` — the manifest: `selector`, `runs_dir`, `sample_rule` (the literal
  `"runid-stride"`; a future rule change must change the string), `requested` (the
  `--sample` value), `runs` (the sampled IDs), and `created_at`.
- `INSTRUCTIONS.md` — a fixed template for the external inspector: what the bundle
  contains, what to look for (shortcuts, gaming, tool misuse, fabricated results), and
  the exact scores-JSONL contract for returning findings, provenance included.

The return loop is the existing scores boundary — nothing new to integrate. The
inspector (a human, or an LLM driven outside catacomb — the judge prompt and spend are
the user's business) writes one JSONL line per run-level finding, stamped with a
`tool` field naming the judge that produced it (optional `tool_version` and
`prompt_hash` refine the provenance):

```json
{"key":"audit.clean","value":1,"run_id":"<run id>","tool":"<judge name>"}
```

and the findings gate like any other [run-level score](#run-level-scores):

```sh
catacomb regress --scores findings.jsonl --annotation audit.clean:higher-better ...
```

(`:lower-better` when a higher value is worse). The gate ignores the provenance
fields, but they let the same file feed `catacomb-judge agreement` and
`catacomb-judge panel` first — calibrating or aggregating the judge before its
scores gate (see [Calibrating a judge](workflows.md#calibrating-a-judge)). See
[Auditing cells](workflows.md#auditing-cells) for the full loop.

Exit codes: `0` success (stdout reports `packed N of M runs into <out>`), `2`
operational error — a missing `--runs-dir` or `--out`, `--sample` below 1, an invalid
selector, an empty selection, or an `--out` dir that already exists (packs are
immutable snapshots, never merged into).

```sh
catacomb pack label:basket=sql,variant=candidate --runs-dir runs --out audit-pack
catacomb pack name:golden --runs-dir runs --out audit-pack --sample 5
```

---

## replay

Build an in-memory graph from a single recorded Claude Code transcript and print a
node/edge summary.

```sh
catacomb replay <transcript.jsonl> [flags]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--export-jsonl` | (empty) | Also write a JSONL graph snapshot to this path |

Nothing is persisted: the graph is built, summarized, and discarded (or snapshotted
with `--export-jsonl`). Useful for a quick look at what catacomb sees in a session.

```sh
catacomb replay ~/.claude/projects/<project>/<session>.jsonl
```

---

## mcp

Run the catacomb MCP server over stdio (JSON-RPC 2.0, newline-delimited). It exposes a
single `mark` tool so an in-run agent can record phase checkpoints without any
hand-rolled stub.

```sh
catacomb mcp
```

Takes no flags or arguments; it reads requests from stdin and writes responses to
stdout, and exits when stdin closes (or on `SIGINT`/`SIGTERM`). The `mark` tool takes:

| Field | Required | Meaning |
| --- | --- | --- |
| `name` | **required** | Phase name |
| `boundary` | **required** | `start` or `end` |
| `occurrence` | optional int | Occurrence index for repeated same-name phases |
| `state_ref` | optional | Opaque state reference stored on the marker node |

Wire it into Claude Code with `--mcp-config` so the agent can call the
`mcp__catacomb__mark` checkpoint tool during a run — the server named `catacomb`
exposing `mark` surfaces as `mcp__catacomb__mark`:

```json
{"mcpServers":{"catacomb":{"command":"catacomb","args":["mcp"]}}}
```

Pass `--strict-mcp-config` alongside it (as the bench cells should) so only the
catacomb server is loaded and no ambient MCP config leaks in. The tool is a pure
acknowledgement — the marker rides the transcript as the tool-call record, and the
catacomb reducer synthesizes the phase boundary from the tool-call input, so the server
needs no configuration and fails open. See
[workflows.md](workflows.md#placing-markers) for the checkpoints workflow.

---

## version

Print the version string.

```sh
catacomb version
```
