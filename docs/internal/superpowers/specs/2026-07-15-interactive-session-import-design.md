# Interactive-session import — design

- **Date:** 2026-07-15
- **Status:** approved design, pending implementation plan
- **Scope:** new `catacomb import` subcommand (Go, 0.MINOR — new compatibility surface: a second CLI entry point into the evidence pipeline), a small shared-helper refactor of the bench offline-evidence path, docs corrections everywhere `claude -p` is presented as the only way to feed the gate, ADR-0030
- **Builds on:** ADR-0026 (offline eval gate form factor), ADR-0027 (verification layer — `verify` reuse), the offline-evidence path introduced for `bench` (`cmd/catacomb/transcripts.go`, `cmd/catacomb/bench.go` `recordOfflineEvidence`)

## Problem

The pipeline has exactly one entry point that produces evidence: `catacomb bench`.
`bench` spawns each cell's `cmd` as a child and **peeks its stdout for the
stream-json `session_id`** (and the terminal `result` event's `total_cost_usd`),
then resolves the session's transcript under `--projects-dir` and writes a
redacted evidence directory. Everything downstream — `verify`, `aggregate`,
`regress`, `trends` — consumes those evidence directories (`meta.json` + redacted
transcripts + the `task:<id>` marker window), never a bare transcript.

Consequences confirmed against the code and against current Claude Code
(`claude` v2.1.x, `claude-code-guide` research 2026-07-15):

1. **Interactive sessions cannot enter the gate.** A human-run `claude` TUI
   session never emits stream-json on stdout, so `bench` records
   `no session id observed` and skips verification and evidence. Yet the
   interactive session writes a transcript JSONL of the *same* format to
   `~/.claude/projects/<encoded-cwd>/<session-id>.jsonl`.
2. **The docs present `claude -p … --output-format stream-json` as the only
   supported agent invocation** (`docs/guide/cli.md` bench section,
   `docs/guide/basket.md` `cmd` field + examples, `troubleshooting.md`), which is
   true for `bench` but reads as a property of the whole tool.
3. There is no first-class way to take an *already-finished* session (interactive
   or otherwise) and turn it into a run the gate can compare. The only bridge
   today is to hand-synthesize an evidence dir, which is undocumented and
   error-prone.

## Decisions (from brainstorm)

1. **New `catacomb import` subcommand, basket-anchored.** The basket is the source
   of truth for `task`, `variant` selection, `verify:`, `checkpoints:`, and
   labels — exactly as `verify` treats it — so the evidence dir `import` writes is
   indistinguishable from a `bench` cell and `verify`/`regress` work unchanged.
   The task's `cmd` is **ignored** (the agent was run by hand).
2. **Marker window = synthesize `task:<id>` from transcript timestamps AND honor
   `mark` markers.** `bench` synthesizes the `task:<id>` window from child
   wall-clock; `import` synthesizes it from the transcript's earliest/latest
   record timestamps, and additionally honors any `mcp__catacomb__mark`
   checkpoints already present in the graph. This matches bench-cell behavior.
3. **Evidence only; `verify` stays a separate step.** `import` writes the evidence
   dir and stops. Verifiers are run afterward with the existing
   `catacomb verify <basket> --runs-dir …`. Single responsibility, zero
   duplication of verify logic.
4. **Two inputs:** `--session-id <uuid>` (resolved under `--projects-dir`) as the
   primary path, plus `--transcript <path>` as an escape hatch pointing directly
   at a main `.jsonl` — because current Claude Code offers no TUI way to display
   the live session id.
5. **Run-id scheme `import-<basket>-<task>-<variant>-r<rep>`** (distinct prefix so
   an import never clobbers a `bench-…` cell), overridable with `--run-id`. Labels
   are identical to a bench cell, so `regress`/`verify` selectors match.

## Non-goals

- No agent launch, no network, no LLM calls (unchanged core constraint).
- No manifest. `import` writes only the evidence dir; `verify`/`regress`/`trends`
  scan `meta.json`, so no manifest is needed. (`--resume` is a bench concept.)
- No cost back-fill from a headless re-run. See "Cost" below — the token-derived
  `cost_usd` metric already works; the authoritative run-total is simply absent.
- No change to the transcript parser, redaction, graph reduction, or `regress`.
- No new verifier modes; `import` does not run `verify:`.

## Goals

- A session run entirely by hand in the interactive TUI can be turned into a run
  the gate compares, with a single command and no `claude -p`.
- The resulting evidence dir is byte-for-byte shaped like a `bench` cell:
  `verify`, `aggregate`, `regress`, `trends` operate on it with no special-casing.
- Docs stop implying `claude -p` is the only way to feed the gate; the interactive
  path is documented end-to-end, including how to make the session id knowable.

## Design

### Command surface

```
catacomb import <basket.yaml> --task <id> --variant <id>
                (--session-id <uuid> | --transcript <path>)
                [--rep <n>] [--run-id <id>]
                [--projects-dir <dir>] [--runs-dir <dir>]
                [--label k=v[,k=v...]]
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--task` | (required) | Task id in the basket; selects `verify:`/`checkpoints:`/labels |
| `--variant` | (required) | Variant id in the basket; selects variant labels |
| `--session-id` | — | Session UUID; resolved under `--projects-dir`. Mutually exclusive with `--transcript` |
| `--transcript` | — | Direct path to a main session `.jsonl`. Subagents resolved by convention from its path |
| `--rep` | `1` | Repetition index; becomes the `rep` label and run-id suffix |
| `--run-id` | `import-<basket>-<task>-<variant>-r<rep>` | Evidence dir name under `--runs-dir` |
| `--projects-dir` | `~/.claude/projects` | Claude projects dir holding session transcripts |
| `--runs-dir` | `~/.catacomb/runs` | Evidence output directory |
| `--label` | — | Extra ambient labels merged under cell labels (cell labels win) |

Exactly one of `--session-id` / `--transcript` must be supplied. `--task` and
`--variant` must resolve to a task and variant in the basket, or the command exits
`2`. The basket is loaded and validated exactly as `verify` loads it; a task
whose `cmd` is present is fine — `cmd` is not consulted.

### Reuse map (the feature is a thin front-end)

The offline-evidence path `bench` already runs after a child exits is the whole
engine. `import` reuses it directly:

| Step | Existing function | Import's use |
| --- | --- | --- |
| Resolve transcripts | `resolveTranscripts(projectsDir, sessionID)` (`transcripts.go`) | `--session-id` path. No retry (session already finished) |
| Build graph (+ price tokens, extract model/version/marks) | `loadGraphOffline(main, subs, execID, pricer, boundary)` (`bench.go`) | Unchanged |
| Marker names present | `graphMarkerNames(g)` | Honor `mark` checkpoints; warn on declared-but-missing |
| Env stamps | `benchEnvStamps(g.RunsSnapshot(), sessionID, nil)` | `workspace` = nil |
| Write redacted evidence + meta | `evidence.Write` via `writeOfflineEvidence` / `offlineFiles` | Unchanged (redaction free) |

**Refactor (DRY, single-responsibility):** the middle of
`recordOfflineEvidence` — from `loadGraphOffline` through `writeOfflineEvidence` —
is today entangled with `bench.Cell`, `bench.ManifestEntry`, and `offlineOpts`.
Extract a small shared helper that takes a plain struct
(`transcriptSet`, task id, variant id, labels, marker window, cost pointer,
basket hash) and returns the written evidence dir (or an error). Both
`bench`'s offline path and `import` call it. No behavior change for bench;
its manifest/checkpoint bookkeeping stays in `recordOfflineEvidence` around the
shared core.

### Data flow

1. Load + validate basket; select task/variant (`cmd` ignored).
2. Resolve the transcript:
   - `--session-id` → `resolveTranscripts(projectsDir, sid)` → `transcriptSet`
     (main + `<sid>/subagents/agent-*.jsonl`). Not-found / ambiguous → exit `2`
     with the same message shape bench uses.
   - `--transcript <path>` → main = path; `sid` derived from the file's base name
     (`<sid>.jsonl`); subagents resolved from the sibling `<sid>/subagents/` dir if
     present. If the base name is not a plausible session id, `sid` falls back to
     the base name (used only as the evidence `session_id` stamp).
3. Compute the marker window: parse the transcript's earliest and latest record
   `timestamp` → `MarkerStart` / `MarkerEnd` (UTC). Pass through
   `boundaryObservations(sid, "task:"+taskID, start, end)` so the `task:<id>` phase
   spans the session, identical to how bench feeds the boundary.
4. `loadGraphOffline(main, subs, newExecutionID(), pricer, boundary)`.
5. Build labels: `{basket, task, variant, rep}` from the basket name + flags, then
   merge `--label` terms underneath (cell labels win), mirroring bench's
   ambient+cell merge.
6. `benchEnvStamps(g.RunsSnapshot(), sid, nil)`; `model_id` / `claude_code_version`
   come from the transcript, `resources` from the host running `import`.
7. Build `evidence.Meta` (see below) and write via the shared helper.
8. Best-effort checkpoint check: for each declared `checkpoints:` name absent from
   `graphMarkerNames(g)`, print `import <run-id>: missing checkpoints: <names>` to
   stderr. Never gates.
9. Print a short confirmation (run-id, evidence dir, model/version, whether the
   `task:<id>` marker and any declared checkpoints were found).

### `evidence.Meta` produced

Identical to `offlineMeta` except:

- `MarkerStart` / `MarkerEnd` = transcript timestamp bounds (not child wall-clock).
- `CostUSD` = `nil` (no stream-json `result` event; see Cost).
- `BasketHash` = the basket content hash (same hash bench stamps), so a
  `regress --strict` `name:` baseline stamp check behaves consistently.
- `ExitCode` = `0` (the human "completed" the session; there is no child exit).
- `Env.Workspace` = nil.

### Cost

Research confirmed an interactive transcript records **token usage but not
`total_cost_usd`** (that figure exists only in the headless stream-json `result`
event). This is not a real gap for gating:

- The `cost_usd` **metric** consumed by `regress` is derived from token usage via
  catacomb's pricing table — the same `pricer` `loadGraphOffline` already applies
  offline. Tokens are present in the transcript, so `cost_usd` is available for
  imported runs **and comparable** to bench runs (both pricer-derived).
- `meta.CostUSD` (the authoritative run-total) has no analogue in an interactive
  transcript and is recorded as `null`, exactly as bench does when no `result`
  event fired. `regress` does not read `meta.CostUSD` for the metric gate.

Documented explicitly so nobody expects the run-total figure on imported cells.

### Ergonomics (docs)

Because there is no TUI way to read the live session id, the recommended workflow
pins it up front:

```sh
SID=$(uuidgen)
claude --session-id "$SID" --mcp-config catacomb-mcp.json   # do the task, mark phases
catacomb import checkout.yaml --task add-item --variant trunk --session-id "$SID"
catacomb verify  checkout.yaml --runs-dir ~/.catacomb/runs
catacomb regress --baseline label:variant=trunk --candidate label:variant=patched
```

The exact spelling of the "pre-set the session id" flag (`--session-id` vs a named
alternative) is confirmed against `claude --help` during planning; the
`--transcript <path>` escape hatch makes the feature usable regardless (find the
newest file under `~/.claude/projects/<encoded-cwd>/`).

## Error handling

- Missing/duplicate `--task`/`--variant` in basket → exit `2`, named.
- Both or neither of `--session-id`/`--transcript` → exit `2`.
- Transcript not found / ambiguous session → exit `2`, message shape reused from
  `resolveTranscripts`.
- Transcript present but unparseable / empty (no timestamped records to bound the
  window) → exit `2` with a clear message.
- Evidence write failure → exit `2`, error surfaced (no partial-success silence).
- Format-drift / version-ceiling advisories → stderr only, exit unchanged (same as
  every other transcript-parsing command).
- Declared checkpoint missing → stderr warning, exit `0` (never gates).

## Testing (TDD, 100% coverage)

Unit:
- Flag validation: required task/variant, session-id⊕transcript exclusivity,
  rep/run-id defaulting, label merge precedence.
- `--session-id` resolution: found, not-found, ambiguous.
- `--transcript` resolution: main-only, main+subagents, sid derivation from base
  name, non-uuid base name fallback.
- Marker window synthesis from first/last transcript timestamps; empty/undated
  transcript rejected.
- Meta shape: `CostUSD` nil, `MarkerName`/window, labels, run-id scheme,
  workspace-nil env stamps, model/version pulled from transcript.
- Honor `mark`: declared checkpoint present vs missing (stderr warning, exit 0).
- Shared-helper refactor: bench offline path unchanged (existing bench tests stay
  green); helper covered from both callers.

Integration / E2E:
- `import → verify → regress` over a small fixture transcript (no `claude -p`
  anywhere): an A-vs-B pair of imported sessions produces a `regress` verdict.
- Add an interactive-path leg to the hermetic E2E asserting the full cycle with a
  recorded fixture transcript and zero agent spawn.

## Documentation changes (explicitly in scope)

Correct every place `claude -p` reads as the only option, and document the new
path:

- `docs/guide/cli.md`: add `import` to the command table; new `## import` section;
  soften the bench wording ("the task `cmd` must emit stream-json") to "for
  `bench`-driven cells" and cross-link `import` for hand-run sessions.
- `docs/guide/basket.md`: note by the `cmd` field and the `claude -p` examples that
  `import` bypasses `cmd`.
- `docs/guide/troubleshooting.md`: `no session id observed` / `transcripts not
  found` rows point at `import` for interactive sessions.
- `docs/guide/workflows.md`: add the interactive-session workflow (pin `--session-id`,
  wire `--mcp-config`, `import → verify → regress`).
- `AGENTS.md`: the one-line pipeline description gains `import` as a second entry
  point beside `bench`.
- `README.md`: mention the interactive path where the tutorial presents `claude -p`.
- `docs/adr/0030-interactive-session-import.md`: record the decision, the
  second-entry-point change to the pipeline, and the cost/marker semantics.
- `docs/adr/README.md`: index ADR-0030.

## Open questions — resolved

- **Session id acquisition** → `--session-id` primary + `--transcript` escape
  hatch; docs pin the id up front. Exact pre-set flag confirmed at planning.
- **Subagent file layout drift** (research noted current Claude Code may inline
  subagents via `parent_tool_use_id` rather than `subagents/agent-*.jsonl`) →
  out of scope; `import` reuses `resolveTranscripts` verbatim so it behaves
  identically to `bench`. Any subagent-format change is a pre-existing, separate
  concern for the shared resolver.
- **Cost** → token-derived `cost_usd` metric works; `meta.CostUSD` null by design.
