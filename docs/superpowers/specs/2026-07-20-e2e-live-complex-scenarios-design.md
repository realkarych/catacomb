# Design — complex live scenarios for the E2E Live Gate

**Date:** 2026-07-20
**Status:** approved (brainstorming), pending spec review
**Budget mandate:** the E2E Live Gate (`.github/workflows/e2e-live.yml` → `e2e/run.sh`)
may spend up to **$20 per run**, twice weekly (~$40/week). Today it spends ~$3–7 over
114 live cells. This design invests the new headroom in the complex scenarios the
original production-scenario design (`2026-07-15-e2e-production-scenarios-design.md`,
§2/§8) *deliberately deferred out of the live lane "by budget"* — they exist only in the
free hermetic lane today.

## 1. Problem

The live gate covers the headline agentic features in isolation — a subagent basket, a
skill basket, an MCP basket, a continuous (`tokens_out`) basket, presence/mark, the SQL
verifier, failmode/error-delta — each single-axis on real `claude -p` evidence. But the
*compound* and *cross-cutting* behaviors are proven only over deterministic hermetic
fixtures, never on real evidence:

- **Composite session** — a subagent that marks a checkpoint, invokes a skill, and
  produces a verifiable artifact, so `subagent + marker + skill + verifier` nodes
  co-exist in one graph. Only `e2e/hermetic/prod/scenarios/40-composite.sh`.
- **Nested subagents** — a subagent that itself spawns a subagent. Only
  `25-multi-nested-subagent.sh`.
- **Secret redaction on real evidence** — a real session that emits a secret-shaped
  token, proving the live capture+redaction seam scrubs it. Only `60-redaction.sh`
  (fixture transcript). Live redaction is unasserted end-to-end — the highest-severity
  bug class (a redaction leak) has no live moat.
- **A second continuous axis** — the continuous gate fires only on `tokens_out`. A
  regression on `tokens_in` (context/prompt bloat, or a harness that stops sending part
  of the context) is invisible live.

Nothing here needs a product change: every node type (`subagent`, `marker`, `skill`,
`mcp`) and every continuous metric (`tokens_in`, `tokens_out`, `turns`, `cost_usd`,
`duration_ms`, `cache_*`) already exists and is reduced today. This is **e2e fixtures +
workflow YAML only — zero production-Go changes.**

## 2. Goals / non-goals

Goals:

- Prove on **real** evidence, in the live lane, that (a) a maximally-rich multi-node
  session reduces without the node types clobbering each other, (b) nested-subagent
  depth reduces with correct parent attribution, (c) the live capture+redaction seam
  scrubs a real secret-shaped token, and (d) a second continuous axis (`tokens_in`)
  gates.
- Preserve the PV-6b discipline every basket follows: an **A-vs-A control** (`baseline`
  vs `baseline2`) that must NOT gate, plus a **seeded regression** (`degraded`) that
  MUST gate, attributed to the swapped axis.
- Preserve the **soft-live / hard-hermetic** rule for every stochastic assertion: the
  live leg LOGS, the hermetic mirror over fabricated evidence ASSERTS. Structurally
  forced signals (delegation forced via tool-allowances) stay hard live.
- Keep the run **within the $20 ceiling**, enforced — not hoped — via a cost-guard.

Non-goals:

- No product-Go change (node types + reducer paths + continuous metrics already exist).
  If a spike surfaces a genuine reducer gap, that becomes its own TDD'd task under the
  100%-coverage gate, not a silent workaround here (same posture as the 2026-07-15 §9).
- No new product MCP tool surface. The composite reuses the existing `catacomb mark`
  MCP tool and the existing `e2e/mcp-e2ekit` fixture server.
- `reps` stays at **5** for every new basket (the standing default; the user accepted
  the resulting worst-case spend), with the cost-guard as the ceiling backstop.

## 3. The four components

Each is a live basket (or basket edit) plus a hermetic hard-mirror. All four are
additive to `e2e/run.sh` and `e2e-live.yml`.

### 3.1 Composite mega-basket (headline)

`e2e/basket-composite.yaml` + `e2e/composite.sh`, reusing `e2e/skills/e2e-emit/` and the
`catacomb mark` MCP tool. One session; the subagent performs several **forced** actions:
`mark` a checkpoint → invoke the `e2e-emit` skill → write the verifiable
`out/result.csv`.

- **baseline / baseline2**: the main agent is given ONLY `Task` (no `Bash`/`Write`), so
  it physically must delegate; the subagent is given `Skill`, `mcp__catacomb__mark`, and
  `Write`. The reduced graph then carries **`subagent` + `marker` + `skill` nodes AND
  `verifier.pass` simultaneously** — the reducer path that co-existence exercises has
  never run on live evidence.
- **degraded**: the main agent gets `Skill`/`mcp__catacomb__mark`/`Write` and NO `Task`,
  so it does the work inline without delegating and without the skill → the `subagent`
  node (primary) and the `skill` node drop.
- **Gate (hard, live):** `subagent`-presence separation, structurally forced by the
  tool-allowance split → deterministic, exactly like `basket-subagent.yaml`.
- **Log (soft, live):** the co-existence of `marker` + `skill` + `subagent` nodes in one
  baseline graph (a 3-action subagent script is obedience-stochastic on sonnet).
- **Hard-mirror:** `e2e/hermetic/prod/scenarios/40-composite.sh` already asserts the
  deterministic co-existence + gate over fixtures.
- **Shape/cost:** 1 task × 3 variants × 5 reps = 15 cells, sonnet, subagent spawns a
  child. ~$4.5–7.5.

### 3.2 Nested subagents

`e2e/basket-nested.yaml` + `e2e/nested.sh`. Two-level nesting: main (`Task` only) →
subagent A (`Task` only) → subagent B (`Bash(sqlite3:*)`) runs the seeded SQL query and
writes `out/result.csv`.

- **baseline / baseline2**: depth 2 — the reducer must synthesize the nested `subagent`
  nodes from the `subagents/agent-*.jsonl` sub-transcripts with correct parent
  attribution.
- **degraded**: depth 1 — subagent A is instructed (and tooled) to run the query itself,
  no B → the depth-2 node is absent → gate.
- **Gate (hard, live):** presence separation on the depth-2 subagent node. Note catacomb
  models nesting *only* as a `parent_child` edge chain — there is no `parent_agent_id`
  field on a node (the documented finding in `25-multi-nested-subagent.sh`) — so the
  assertion targets the depth-2 subagent node and its inbound parent-child edge, not a
  "depth" scalar. Forced via the per-level tool-allowance split.
- **Hard-mirror:** `e2e/hermetic/prod/scenarios/25-multi-nested-subagent.sh`.
- **Shape/cost:** 1 task × 3 variants × 5 reps = 15 cells, sonnet, two children per
  baseline cell. ~$3–5.

### 3.3 Live redaction gate

`e2e/basket-redaction.yaml` + `e2e/redaction.sh`. The workspace `cmd` seeds a file
containing a runtime-assembled **fake** GitHub token (`ghp_` + inert body, matching
`redact`'s `reGitHubToken` but never committed secret-shaped — same assembly trick as
`60-redaction.sh`). The cell instruction: `cat` that file and copy its content into
`out/result.csv`. The token therefore flows through a **`tool_result` payload**
deterministically — emission depends on the model running `cat`, not on it reproducing a
literal string.

- **Assert (hard, live):** the raw fake token is ABSENT from every captured
  `session.jsonl` AND from the `pack` third-party-auditor bundle.
- **Non-vacuity (hard, live, ≥ majority of reps):** the `‹redacted:github-token›`
  placeholder is PRESENT in the captured evidence → proves redaction actually fired
  (rather than the model simply never emitting the token). Robust because the `cat` step
  is deterministic.
- This is an **invariant / moat** against a future redaction-policy regression, not a
  statistical A-vs-A gate — so it is a single variant × 5 reps, not the three-variant
  shape. The hard *policy-regression* proof stays hermetic in `60-redaction.sh`.
- **Shape/cost:** 1 variant × 5 reps = 5 cells, sonnet (obedience of the two-step
  cat→write). ~$0.3.

### 3.4 Second continuous axis (`tokens_in`)

A new `bigprompt` variant in `e2e/basket-continuous.yaml`: a multi-KB `TASK_PROMPT`
(filler context) that still demands a one-sentence answer, so **`tokens_in` spikes while
`tokens_out` stays flat** — an isolated axis, more deterministic than `tokens_out`
(input-token count is a near-deterministic function of the prompt).

- **Gate:** a `regress` assertion on the continuous `tokens_in` axis — baseline vs
  bigprompt gates; the existing A-vs-A (baseline vs baseline2) still does not. A distinct
  regression class: prompt/context bloat, or a harness dropping part of the context.
- **Hard-mirror:** a new assertion in the hermetic continuous scenario over a fabricated
  `tokens_in` delta (deterministic).
- **Shape/cost:** +1 variant → the continuous basket becomes 4 variants × 5 reps = 20
  cells (+5 cheap haiku cells). ~$0.25.

## 4. Cost-guard ($20 as an enforced invariant)

Extend the existing `w. cost report` step in `e2e/run.sh`: sum `cost_usd` across every
bench manifest / `regress --json` the run produced and **fail the run (non-zero exit)
when the total exceeds `E2E_COST_CEILING_USD` (default `20`).** The ceiling becomes a
checked invariant instead of an estimate.

- Under OAuth/subscription billing `cost_usd` may report `0`; the guard is then
  vacuously safe (it can never false-fail). Under `ANTHROPIC_API_KEY` (API billing, what
  the workflow uses) `cost_usd` is populated, so the guard is live.
- The threshold is env-overridable so a maintainer can raise it for a one-off deep run
  without editing the driver.

## 5. Risk mitigation — spike first

Before wiring the two expensive baskets (§3.1, §3.2), a single throwaway probe run (the
§9 pattern from the 2026-07-15 design) confirms, on the pinned `claude -p` version:

1. a subagent can invoke the skill + `mark` + write the artifact in one dispatch (§3.1);
2. a subagent can spawn a nested subagent via `Task` in `claude -p` (§3.2);
3. the fake token flows through `cat` → `tool_result` → live redaction and lands as the
   placeholder (§3.3).

A resolved risk that turns out to be a genuine reducer gap is re-scoped as its own
TDD'd product task — not worked around in fixtures.

## 6. Files

```
e2e/basket-composite.yaml        composite live basket (§3.1)
e2e/composite.sh                 composite cell wrapper
e2e/basket-nested.yaml           nested-subagent live basket (§3.2)
e2e/nested.sh                    nested cell wrapper
e2e/basket-redaction.yaml        live redaction basket (§3.3)
e2e/redaction.sh                 redaction cell wrapper (seeds fake token, cat→write)
e2e/basket-continuous.yaml       + bigprompt variant (§3.4)
e2e/run.sh                       + 4 basket sections, cost-guard, updated cost header
e2e/hermetic/prod/scenarios/     + tokens_in hermetic assert (§3.4);
                                   40/25/60 mirrors for §3.1–3.3 already exist
.github/workflows/e2e-live.yml   timeout-minutes bump + cost-header text
AGENTS.md                        E2E row(s) mention the complex live baskets
```

## 7. Testing & coverage

The additions are bash / YAML e2e fixtures, outside the Go 100%-coverage gate.
Correctness is enforced the way the existing E2E enforces it: every step is
self-asserting (A-vs-A exits 0, seeded regression exits 1, node presence /
`verifier.pass` / redaction placeholder pinned), so a reducer/gate/redaction regression
turns the hermetic PR run red for free, and the live leg catches drift twice weekly. Any
Go product change surfaced by §5 is TDD-first under the existing gates. The hermetic
mirrors (`40`, `25`, `60`, and the new `tokens_in` assert) carry the deterministic hard
proofs; the live baskets prove the real CLI produces such sessions and the cost-guard
keeps the run under budget.

## 8. Expected cost

Existing ~$7 + composite ~$4.5–7.5 + nested ~$3–5 + redaction ~$0.3 + `tokens_in`
~$0.25 = **~$14–20 per run**, with the §4 cost-guard hard-capping the worst case at $20.
Figures are order-of-magnitude estimates to confirm on the first dispatch (per the run.sh
cost header convention).
