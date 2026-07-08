# PV-6b — Live-basket calibration of the regression gate

- **Date:** 2026-07-08
- **Scope:** validate the PV-6a synthetic gate-power boundaries against **real** `claude -p` agentic runs; confirm the offline gate catches genuine regressions and does not false-positive on real agentic/caching variance.
- **Environment:** `claude` 2.1.202 (Claude Code), model `claude-haiku-4-5`, macOS; `catacomb` at PV-6a HEAD driving `bench`/`regress` offline (no daemon).
- **Total spend:** **$0.43** (30 bench cells + 2 feasibility smoke calls).

## Verdict

The gate behaves on live data exactly as PV-6a's math predicts. A real checkpoint-presence regression (agent stops marking a phase) gates at k=5; a real continuous regression (≫25% token growth) gates at k=5; **both A-vs-A controls produced zero false regressions** despite genuinely noisy per-cell cost (Claude Code prompt-cache variance spanning $0.004–0.02 on identical prompts). The phase axis carried every verdict and step-level findings were coverage-downgraded to `notable`, live-confirming the V-2/PV-6a "trust the phase axis" conclusion. PV-6 (a: synthetic characterization, b: live validation) is complete.

## Method

Two heterogeneous baskets, each 3 variants × 5 reps (k=5), run through `catacomb bench` (daemonless) with each cell a live `claude -p … --output-format stream-json` invocation. Variants differ only by a per-variant instruction injected via `variant.env` and read by a thin wrapper script (bench execs `cmd` without a shell, so env-interpolation happens in the wrapper) — faithfully modelling "change one instruction/skill → observe regression." Checkpoints ride the real `mcp__catacomb__mark` tool via `catacomb mcp`.

- **Basket 1 (presence):** `baseline` marks a `verify` checkpoint; `degraded` omits the mark instruction; `baseline2` == baseline (A-vs-A control).
- **Basket 2 (continuous):** `baseline` = one-sentence answer; `verbose` = six-paragraph essay (large token growth); `baseline2` == baseline (A-vs-A control).

## Results

### Basket 1 — presence

Per-variant `verify` presence (from the bench manifest) was a clean split — no stochastic leakage:

| Variant | `verify` present | Role |
|---|---|---|
| baseline | 5/5 | reference |
| degraded | 0/5 | injected regression |
| baseline2 | 5/5 | A-vs-A control |

`regress` verdicts:

| Comparison | Overall | Decisive finding |
|---|---|---|
| baseline vs degraded | **regression** | phase `verify` **presence 5/5 → 0/5** (regression) |
| baseline vs baseline2 (A-vs-A) | **ok** | 0 regressions |

Notes, all correct and informative:

- Resource metrics on `degraded` (`cost_usd`, `duration_ms`, `tokens_in`, `nodes`) were flagged **improvement**, not regression — the degraded agent does less work, and the one-sided gate correctly treats "less" as improvement, not a false regression.
- Step-level `ToolSearch` presence differences appeared only as **notable** (coverage-floor downgrade), never driving the verdict — live confirmation that steps under-align and the phase axis is authoritative.

### Basket 2 — continuous

| Comparison | Overall | Decisive finding |
|---|---|---|
| baseline vs verbose | **regression** | `tokens_out` **median 243 → 1932** (+695%, regression) |
| baseline vs baseline2 (A-vs-A) | **ok** | 0 regressions |

Notes:

- `tokens_out` was the clean regressor. `cost_usd` did **not** gate on the verbose comparison and did not false-positive on A-vs-A: haiku output is cheap and per-cell cost is dominated by prompt-cache creation/read variance, so the cost median stayed inside the band. **Token count is a more reliable continuous regressor than cost** under caching — a practical calibration finding.
- The A-vs-A control gating `ok` despite ~5× cost spread across identical-prompt reps validates the median/IQR band's robustness to real variance (the "no variance term" continuous gate still avoids false positives because the median is stable).

## Cross-check against PV-6a synthetic boundaries

| Axis | PV-6a synthetic floor | PV-6b live | Consistent? |
|---|---|---|---|
| Presence @k=5 | min detectable drop 80% (present ≤1/5 gates) | full flip 5/5→0/5 (100% drop) gated | ✓ (past the floor) |
| Continuous | fixed band `max(25%, 1.5·IQR)`; only shifts >25% gate | +695% gated; noisy-but-stable-median A-vs-A did not | ✓ |
| False-positive rate | low by construction (disjoint Wilson CIs; robust bands) | 0/2 A-vs-A controls false-gated | ✓ |

Bonus: the PV-5 **version watchlist fired on every cell** — `warning: transcript Claude Code version 2.1.202 is newer than tested 2.1.199` — validating that offline drift/version surfacing works on real transcripts.

## Honest limits

- **Full-flip only.** The degraded agent marked `verify` in 0/5 and baseline in 5/5 — a clean 100% flip. Inducing a *controlled partial* presence rate (e.g. exactly 3/5) on a stochastic agent is not reliably possible, so the exact partial floors (80% @k=5, 50% @k=10) remain validated by the PV-6a math, not live.
- **k=5, one model, two baskets.** Live cost/time bounded the sweep; PV-6a covers the k∈{3,5,10,20,30} range synthetically. Haiku only; a stronger model may mark more reliably (raising baseline presence) but does not change the gate's detection logic.
- **Cost noise.** Prompt-cache variance makes `cost_usd` a noisier regressor than `tokens_out`; calibrate continuous gates on token counts where possible.

## Bottom line

The checkpoint-diff + statistical-gate core — the one thing no off-the-shelf tool does (V-2 review) — is now **validated end-to-end on live Claude Code runs**: it catches a real checkpoint-presence regression and a real continuous regression at k=5, attributes them to the swapped instruction, and produced **zero false positives** on both A-vs-A controls, with the phase axis carrying the verdict exactly as characterized. Trust it at the phase axis, k≥5, for effects above the PV-6a floors.

## Reproducibility

MCP config (`mcp.json`):

```json
{"mcpServers":{"catacomb":{"command":"catacomb","args":["mcp"]}}}
```

Wrapper (`presence.sh`) — the per-variant instruction arrives via `PHASE_INSTRUCTION`:

```bash
#!/bin/bash
exec claude -p "Write a haiku about the sea (three short lines). ${PHASE_INSTRUCTION}" \
  --model haiku --output-format stream-json --verbose \
  --mcp-config mcp.json --allowedTools "mcp__catacomb__mark"
```

Basket 1 (`basket1.yaml`):

```yaml
basket: pv6b-presence
reps: 5
tasks:
  - id: haiku
    cmd: ["./presence.sh"]
    dir: ./work1
    checkpoints:
      - verify
variants:
  - id: baseline
    env:
      PHASE_INSTRUCTION: "Before writing, call mcp__catacomb__mark name=verify boundary=start; after writing, call it name=verify boundary=end."
  - id: degraded
    env:
      PHASE_INSTRUCTION: "Do not use any tools. Just write the haiku."
  - id: baseline2
    env:
      PHASE_INSTRUCTION: "Before writing, call mcp__catacomb__mark name=verify boundary=start; after writing, call it name=verify boundary=end."
```

Basket 2 (`basket2.yaml`) swaps the wrapper for `exec claude -p "${TASK_PROMPT}" …` and sets `TASK_PROMPT` to a one-sentence answer (baseline/baseline2) versus a six-paragraph essay (verbose).

Run and gate:

```sh
catacomb bench basket1.yaml --runs-dir runs1
catacomb regress --runs-dir runs1 \
  --baseline label:basket=pv6b-presence,variant=baseline \
  --candidate label:basket=pv6b-presence,variant=degraded
```
