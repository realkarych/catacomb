# Dogfood calibration run — bench/regress on real agentic runs (V-1)

**Date:** 2026-07-04
**Scope:** first real-workload validation of the bench → aggregate → regress pipeline (roadmap [2026-07-02-post-review-hardening-roadmap.md](../superpowers/plans/2026-07-02-post-review-hardening-roadmap.md) §V-1): one basket of 2 tasks × 2 variants × 5 reps of live `claude -p --model haiku` runs against a scratch Go project, plus a 5-rep A-vs-A control basket and a reps=1 epilogue-nudge probe. 26 agent cells total, all against a throwaway daemon/db.
**Verdict up front:** the pipeline works end-to-end on real runs — the degraded variant gates `regression` (exit 1) on checkpoint presence exactly as designed, and an A-vs-A control at default thresholds produces **zero false regressions**. No threshold-default changes are proposed. Three pre-existing capture/docs gaps surfaced (cost double-count, `CATACOMB_RUN_ID` unconsumed, no shipped `mcp__catacomb__mark` server) — none caused by the eval layer, all worth follow-ups.

---

## 1. Setup

Everything ran from an isolated temp dir; the user's `~/.catacomb` and any running daemon were never touched.

- **Binary:** `make build` at worktree head (post PR-M, `170177c`).
- **Daemon:** `bin/catacomb daemon --db $TMP/daemon/catacomb.db --discovery $TMP/daemon/discovery.json` (defaults otherwise), stopped after the runs.
- **Scratch project:** a Go module with a `CLAUDE.md` carrying the checkpoint convention from [workflows.md](../guide/workflows.md#checkpoints-and-phase-scoped-diff): mark `impl` start/end around implementation, `verify` start/end around executing the result. Variant setup scripts swap `CLAUDE.md` and delete generated files before every cell, so cells are i.i.d.
- **Hooks:** `catacomb install-hooks --project` inside the scratch dir with `CATACOMB_DISCOVERY` pointing at the temp daemon, per the ingestion guide (the hook command hardcodes the discovery path at install time, so isolation holds). Without hooks a stream-json-only run never finalizes — see finding F6.
- **MCP mark tool:** the documented convention has the agent call `mcp__catacomb__mark`. Catacomb ships no MCP server, so the tool does not exist client-side out of the box (finding F8). A ~50-line stub MCP server named `catacomb` exposing a no-op `mark` tool was wired via `--mcp-config`; the marker rides the trace stream as documented — the reducer synthesizes markers from the tool-call input regardless of what the tool returns (`reduce/marker.go`).
- **Child command** (per cell): `claude -p "<task>" --model haiku --output-format stream-json --verbose --max-turns 30 --permission-mode bypassPermissions --mcp-config <stub> --strict-mcp-config --settings '<plugins off>'`.

### Basket (excerpt)

```yaml
basket: dogfood
reps: 5
tasks:
  - id: fizzbuzz
    dir: <scratch>/proj
    cmd: ["claude", "-p", "Write fizzbuzz.go ... then run it to verify ...", "--model", "haiku", ...]
    checkpoints: [impl, verify]
  - id: addtest
    cmd: ["claude", "-p", "Using TDD: write add_test.go ... then implement add.go so that `go test` passes ...", ...]
    checkpoints: [impl, verify]
variants:
  - id: normal
    setup: [<reset-normal.sh>]      # normal CLAUDE.md: mark impl/verify, always execute to verify
  - id: degraded
    setup: [<reset-degraded.sh>]    # same convention text + "NEVER run tests or execute code; do not
                                    # verify anything; skip the verify phase entirely; keep answers minimal"
```

The A-vs-A control (`basket2.yaml`) is the same fizzbuzz task under basket `dogfood2`, single variant `normal2` with the **normal** CLAUDE.md, reps 5 — behaviorally identical to `dogfood/normal`, disjoint labels.

## 2. Run stats

| Basket | Cells | Exit 0 | `marked` | Wall time | Cost (claude-reported) |
| --- | --- | --- | --- | --- | --- |
| dogfood (2×2×5) | 20 | 20 | 20/20 | 428 s | $0.59 |
| dogfood2 (A-vs-A control) | 5 | 5 | 5/5 | 122 s | $0.16 |
| nudgetest (reps=1, `echo` child) | 1 | 1 | 0/1 | ~1 s | $0 |

25 haiku cells, 15–31 s each, **$0.7482** claude-reported total (`total_cost_usd` summed over `result` events). Catacomb recorded **$1.2560** for the same runs — a systematic ≈ +68% inflation (finding F7). No cell failed, no `--resume` needed, no stream-tee loss warnings.

## 3. Checkpoint presence

Runner task-boundary markers landed in 25/25 cells (`marked 25/25`; each session graph carries a closed `task:<id>` phase). Agent-emitted checkpoints, from the bench epilogue of the main basket:

```text
marked 20/20 cells
checkpoints[fizzbuzz]: impl 10/10
checkpoints[fizzbuzz]: verify 5/10
checkpoints[addtest]: impl 10/10
checkpoints[addtest]: verify 5/10
Next steps:
  catacomb baseline set dogfood-normal --label basket=dogfood,variant=normal
  catacomb regress --baseline label:basket=dogfood,variant=normal --candidate label:basket=dogfood,variant=degraded
```

Presence by phase and group — the degradation is fully deterministic at this k:

| Phase | normal (k=10) | degraded (k=10) | normal2 (k=5) |
| --- | --- | --- | --- |
| `impl` | 10/10 | 10/10 | 5/5 |
| `verify` | 10/10 | **0/10** | 5/5 |
| `task:<id>` (runner) | 10/10 | 10/10 | 5/5 |

Every degraded cell warned `cell <run-id>: missing checkpoints: verify` on stderr at run time. Haiku followed the CLAUDE.md marking convention in 100% of normal cells (60/60 expected marker boundaries) — the documented convention is learnable by the cheapest current model with no few-shot examples. Tool-usage delta matches the design: normal fizzbuzz r1 = `Write` + `Bash` (go run) + markers `impl`,`verify`; degraded r1 = `Write` only, no `Bash`, no `verify` marker.

## 4. Aggregation sanity

Per-group medians/IQRs from `catacomb runs --db ... --json` (durations ms, catacomb-recorded costs):

| Group | k | dur med | dur IQR | cost med | tok_out med | tok_out IQR | nodes med | errors |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| dogfood/fizzbuzz/normal | 5 | 19399 | 2577 | 0.0527 | 1683 | 254 | 14 | 0/5 |
| dogfood/fizzbuzz/degraded | 5 | 18815 | 2258 | 0.0366 | 1345 | 199 | 10 | 0/5 |
| dogfood/addtest/normal | 5 | 24881 | 5613 | 0.0616 | 1993 | 244 | 16 | 0/5 |
| dogfood/addtest/degraded | 5 | 20945 | 3639 | 0.0431 | 1468 | 119 | 12 | 0/5 |
| dogfood2/fizzbuzz/normal2 | 5 | 23052 | 2691 | 0.0544 | 1641 | 227 | 14 | 0/5 |

Sane throughout: no null medians, IQRs are 10–25% of medians for durations and ~15% for tokens, degraded is consistently cheaper/smaller (it skips the execute step). Real cross-basket drift exists — normal2 duration median is +18.8% over normal fizzbuzz — and stays inside the default band (`max(0.25·|median|, 1.5·IQR)` = ±4850 ms here). Run-scope `duration_ms` only exists because hooks were installed (F6).

## 5. Regress verdicts

All commands ran against the quiesced store with `--db`; exit codes in parentheses.

### 5.1 normal vs degraded — the intended catch (exit 1)

```text
$ catacomb regress --baseline label:basket=dogfood,variant=normal --candidate label:basket=dogfood,variant=degraded
baseline runs 10  candidate runs 10
coverage steps 0.45  phases 0.75  steps_trusted false  overall regression
VERDICT       SCOPE  KEY       METRIC       BASELINE  CANDIDATE  BAND
regression    phase  593ca77d… presence     1.00      0.00       [0.79, 1.00]   # verify
insufficient  phase  593ca77d… metrics      0.00      0.00       -              # absent in candidate
improvement   phase  22c88fff… cost_usd     0.05      0.04       [0.04, 0.07]   # task:fizzbuzz
improvement   phase  d28ff636… tokens_out   1993.00   1468.00    [1494.75, 2491.25]  # task:addtest
improvement   total  -         cost_usd     0.05      0.04       [0.04, 0.07]
ok            total  -         duration_ms  21342.00  18815.00   [13119.00, 29565.00]
...(40 insufficient/notable step rows elided)
```

- The `verify` presence flip 10/10 → 0/10 is the sole gating finding; the JSON detail reads `present 10/10 -> 0/10`. Wilson bounds disjoint, delta 1.0 ≥ 0.2 — hard `regression`, exit 1, matching the ADR-0023 sensitivity table (k=10 can flag ≥50% drops).
- Cost/token drops correctly land as non-gating `improvement`.
- Step-alignment coverage across a haiku workload is poor (0.45 mixed-task, 0.43 single-task) — prompt-hash-bearing step keys rarely align across stochastic runs. The `--coverage-floor` (0.7) downgrade engaged (`steps_trusted false`), and the phase scope carried the verdict, exactly the ADR-0022 design intent. **Checkpoints are not a nice-to-have on real agentic runs; they are the only usable comparison axis besides run totals.**
- `--fail-on-notable` and `--json` both return the same overall `regression` (exit 1).

Narrowed per task (`,task=fizzbuzz` on both selectors, k=5 vs 5): same `verify` presence regression at band [0.65, 1.00] — the k=5 gate fires on a full flip as the PR-J table promises. It additionally flags `impl` phase `duration_ms` 4299 → 5909 (band [3224, 5374]) and `tokens_out` 2 → 6 (band [-1, 5]) as regressions — see F2.

### 5.2 A-vs-A control — false-positive check (exit 0)

```text
$ catacomb regress --baseline label:basket=dogfood,variant=normal,task=fizzbuzz --candidate label:basket=dogfood2,variant=normal2
baseline runs 5  candidate runs 5
coverage steps 0.71  phases 1.00  steps_trusted true  overall ok
```

Two behaviorally identical variants, run ~10 minutes apart: **zero regression findings at default thresholds**, all six total-scope metrics `ok`, `impl`/`verify`/`task:` phases aligned 1.00 with no findings. One step-scope `notable` (presence 0.40 → 0.00 on a step key sampled 2/5) — ordinary sampling noise, correctly kept below the gate. With `--fail-on-notable` that notable flips the run to `regression` (exit 1): an empirical false positive that confirms the documented precision-for-recall trade; the default (off) is right.

### 5.3 Support and sensitivity edges

| Command variation | Overall | Exit | Sensitivity line |
| --- | --- | --- | --- |
| A-vs-A, defaults (k=5/5) | ok | 0 | none — gate reachable at k=5 |
| A-vs-A `--min-support 6` | insufficient | 0 | `full flip needs k>=6 presence, ... error_rate` |
| A-vs-A `--min-support 6 --strict` | insufficient | 1 | same |
| A-vs-A `--z 3.0` | ok | 0 | `full flip needs k>=10 presence, ... error_rate` |
| rep=1 vs rep=2 (k=1/1) | insufficient | 0 | `full flip needs k>=3 presence, ... error_rate` |

The sensitivity note appears exactly when the rate gate cannot fire at the actual support and never otherwise; at the default z=1.645 with k=5 it is correctly absent. The reps=1 probe basket printed the PR-J epilogue nudge verbatim: `note: reps=1 limits rate-gate sensitivity; prefer reps: 5 or more`.

The selector grammar is AND-only, so "first 3 reps vs last 2" is not expressible (`rep=1` narrows to k=1); a rep-range or set selector was not needed for the CI use case and is not proposed.

## 6. Findings

**F1 — The pipeline is calibrated for this workload at defaults; no threshold changes proposed.** The one deliberate degradation gates `regression` at both k=5 and k=10; the A-vs-A control is clean at defaults; `insufficient`/`--strict`/sensitivity behaviors all match the ADR-0023 spec. Evidence does not support moving `--z`, `--presence-delta`, `--error-delta`, `--metric-rel-delta`, `--iqr-factor`, `--min-support`, or `--coverage-floor`.

**F2 — Tiny-magnitude phase metrics gate on trivial absolute deltas.** Per-task, `impl` `tokens_out` 2 → 6 is a `regression` with band [-1, 5]: a 4-token delta gates because both the relative term (0.25×2 = 0.5) and the IQR term collapse near zero. Here the difference was real (the degraded agent emits its file-written text inside the `impl` window), but the same math would gate noise on any near-zero metric. If this bites in practice, the right fix is an absolute-floor term in the band (a new knob, not a re-default) — deferred until a real workload produces a false positive from it.

**F3 — Step keys under-align on stochastic agent runs (coverage 0.43–0.71); the coverage-floor downgrade is what kept them from polluting verdicts.** The checkpoint axis carried every verdict in this experiment. The workflows.md guidance ("lean on checkpoints") is not optional advice for agentic children; consider promoting it in the bench docs.

**F4 — The documented CLAUDE.md marking convention works, at 100% adherence on haiku** (60/60 marker boundaries in normal cells), and the bench-side verification surfaced every degraded miss at run time with per-cell stderr warnings plus the `verify 5/10` rollup.

**F5 — `bench` UX held up:** 26/26 cells recorded through the incremental manifest, the single-variant epilogue correctly omits the `regress` suggestion, and the reps<5 nudge fires. The regress human table prints opaque `KEY` hashes for phases/steps while the JSON carries `name`; adding a NAME column would remove a real annoyance (three graph lookups during this analysis).

**F6 — Stream-json-only runs never finalize.** Without hooks, nothing emits `run_ended`: `catacomb run … -- claude -p …` leaves the run `running` until the 30 m reaper marks it `abandoned`, `EndedAt` stays empty, and run-scope `duration_ms` has no samples (the first smoke run demonstrated this). The bench guide's assumption that hooks fire is real and load-bearing; worth an explicit line in [cli.md#bench](../guide/cli.md#bench): install hooks (project-local is enough) or lose duration/finalization.

**F7 — Session cost is double-counted on the stream-json path (+68%, systematic).** Catacomb records the `result` event's reported `total_cost_usd` *and* prices each assistant turn's usage, then sums both into the session total: claude reported $0.7482 for the 25 cells, catacomb recorded $1.2560 (smoke runs: $0.0461 → $0.0771, $0.0395 → $0.0663 — consistent ≈ 1.68×). Regress comparisons are unaffected (both sides inflate equally), but absolute cost reporting is wrong. Pre-existing capture-layer bug, filed as follow-up.

**F8 — The `mcp__catacomb__mark` convention has no shipped server.** concepts.md/workflows.md/cli.md all reference the tool, and ingestion indeed needs no wiring (the reducer reads the tool-call input from the stream), but the *agent* can only call a tool that exists — which today means every adopter hand-writes an MCP stub. Ship a `catacomb mcp` stdio server (one `mark` tool, ~50 lines) or document the stub pattern; until then the flagship checkpoint convention is not usable out of the box.

**F9 — `CATACOMB_RUN_ID` is exported but never consumed.** `run --run-id` and bench set it for the child, docs promise multi-session grouping, but every ingest path keys runs by session id (`ingest/hook/hook.go:50`: `RunID: e.SessionID`). Bench is unaffected — cells are single-session and all selectors are label-driven — but the flag is currently a no-op and the manifest's `run_id` never appears in the store. Follow-up: either honor the env var on the hook/stream paths or stop documenting it as grouping.

**F10 — PR-K drift detection fired on real traffic:** claude 2.1.199 emits `system` subtypes (`hook_started`, `hook_response`, …) unknown to the stream-json parser; the daemon logged `format drift: well-formed input matched no known shape source=stream_json reason=unknown_subtype count=1100` while building correct graphs. Working as designed; add these subtypes to the known-shape set to keep the counter meaningful.

## 7. Bottom line

20-cell basket + control ran unattended in under 10 minutes for well under a dollar, produced the right verdict on a deliberately degraded variant with zero false regressions on the control, and every sensitivity/insufficiency edge behaved per spec. The eval-management layer is calibrated as shipped; the follow-ups are capture-layer (F7), packaging (F8), and docs (F6, F9) — none block using `bench`/`regress` for real gating today.
