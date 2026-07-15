# Production-scenario E2E — subagents · skills · MCP

Status: approved design (2026-07-15). Next: implementation plan (writing-plans),
then subagent-driven execution per AGENTS.md.

## 1. Problem

catacomb reduces Claude Code transcript JSONL into a canonical action graph. The
model already carries first-class node types for the three headline agentic
features:

- `NodeSubagent` — synthesized from `isSidechain` / `agentId` transcript lines
  (`ingest/jsonl/jsonl.go`) and `subagent_stop` observations (`reduce/reduce.go`).
- `NodeSkill` — upgraded from a `Skill` tool_use block (`reduce/skill_test.go`).
- `NodeMarker` — phase boundaries from the `mcp__catacomb__mark` MCP tool.

But **no E2E has ever driven a real (or fixture) session that dispatches a
subagent, invokes a skill, or exercises a general MCP tool through the whole
offline pipeline** (`bench → reduce → stepkey/phasekey → aggregate → regress`) up
to a gate. Those reducer paths are covered only by synthetic unit-test
observations. Worse, the existing live baskets deliberately *forbid* subagents and
skills (`"no subagents, no skills"` in `e2e/basket-presence.yaml`) to keep phase
keys clean — so the production paths are not just untested end-to-end, they are
actively excluded.

This design closes that gap in **both CI lanes**, gating on **both** a
presence/structure signal and a verifier-result signal.

## 2. Goals / non-goals

Goals:

- Exercise subagent dispatch, skill invocation, and a **real** general MCP tool
  end-to-end through the pipeline to a gate, in both lanes.
- Gate on two signals per feature where it applies: **presence** (the
  subagent/skill/MCP node appears in the graph — a STEP- or PHASE-scope
  regression when it is missing) and **result** (`verifier.pass` — the delegated
  work produced the correct artifact).
- Preserve the PV-6b discipline every existing basket follows: an **A-vs-A
  control** (`baseline` vs `baseline2`) that must NOT gate, plus a **seeded
  regression** (`degraded`) that MUST gate, attributed to the swapped instruction.
- Keep the whole addition **within e2e/ fixtures + workflow YAML — zero
  production-Go changes** (the node types and reducer paths already exist).

Non-goals:

- No new product MCP tool surface. `catacomb mcp` keeps serving only `mark`; the
  general MCP tool is a separate **e2e fixture server**, not a product change.
- No new reducer/model behavior *by design*. If a spike shows a genuine gap (for
  example subagent/skill nodes not participating in the STEP-scope gate), that
  becomes its own TDD'd product task under the 100%-coverage gate — tracked as a
  risk (§8), not assumed here.
- No live composite mega-basket (budget bound). The composite "complex session"
  lives in the free hermetic lane.

## 3. Two lanes (recap of the existing split)

| Lane | Workflow | Trigger | Cost | Determinism |
|------|----------|---------|------|-------------|
| Hermetic | `e2e-hermetic.yml` → `e2e/hermetic/run.sh` | every PR + push + dispatch | $0 | fixture transcripts, byte-deterministic |
| Live | `e2e-live.yml` → `e2e/run.sh` | weekly + dispatch | ~$1.7 today | real `claude -p` |

New production scenarios extend both:

- **Hermetic** gets the deterministic reducer-path coverage (subagent / skill /
  general-MCP nodes) **and** the composite complex-session scenario, plus a
  protocol-conformance smoke of the e2e MCP server. Runs on every PR.
- **Live** gets three **focused** per-feature baskets (one axis each, for reliable
  live obedience and attributable gating) on the real CLI, weekly.

## 4. Gating axes per case

| Case | Presence / structure | Result (annotation) |
|------|----------------------|---------------------|
| Subagent | `Task` tool node / `subagent` node present in `baseline`, absent in `degraded` (work done inline) → STEP-scope regression, exactly like the existing `echo` step-axis | subagent writes `out/result.csv`; `verify_sql.py` scores it → `ann:verifier.pass` (degraded subagent returns the wrong result → 5/5 → 0/5) |
| Skill | `Skill` node present in `baseline`, absent in `degraded` (skill not invoked) → STEP-scope regression | skill produces the verifiable artifact → `ann:verifier.pass` |
| MCP | multi-request session against the real e2e MCP server: `mcp__e2ekit__<tool>` STEP node present in `baseline`, absent in `degraded` → STEP-scope regression | the tool writes/returns a value the agent must land in the artifact → `ann:verifier.pass` (primary signal remains presence; verifier asserted where reliable, see §7) |

Every basket keeps the three-variant shape: `baseline` (reference),
`degraded` (seeded regression, must gate), `baseline2` (A-vs-A control, must not
gate).

## 5. The real e2e MCP server (no mock)

A genuine stdio MCP server fixture at **`e2e/mcp-e2ekit/`**, speaking the exact
protocol `catacomb mcp` speaks (verified against `mcp/server.go`):

- **Framing:** newline-delimited JSON-RPC 2.0 over stdio (no Content-Length).
- **`initialize`:** echo the client's `protocolVersion` (default `2025-06-18`),
  return `capabilities: {tools: {}}` and `serverInfo: {name, version}`.
- **`tools/list`:** return one genuine tool (name `record` — a non-`mark` tool so
  the reducer's general `mcp__server__tool` step-key path is exercised, distinct
  from `mark`'s phase-marker special case), with a real `inputSchema`.
- **`tools/call`:** validate arguments, do real work, return
  `{content: [{type: "text", text}], isError}`.
- **Notifications** (no `id`, for example `notifications/initialized`): ignored.

Language: **Python stdlib** — consistent with the existing e2e verify hooks
(`verify_sql.py`), and outside the Go codepolicy/coverage gates. (Go is a viable
alternative but would pull the fixture under no-comments + 100% coverage; the plan
may revisit, but Python is the default.)

The tool `record` does verifiable work (for example: accept a `value`, persist it
so the agent can read it back into `out/result.csv`, and return a confirmation),
so the MCP case can carry the result signal where live obedience allows.

Wiring:

- **Live** (`basket-mcp.yaml` → `mcp-record.sh`): `claude -p --mcp-config
  e2e/mcp-e2ekit/mcp.json --strict-mcp-config --setting-sources project
  --allowedTools "mcp__e2ekit__record,Bash(...)"`. Real handshake between the CLI
  and the server. `baseline` calls `record`; `degraded` is instructed not to use
  tools.
- **Hermetic:** two things — (a) a **protocol-conformance smoke**: drive
  `e2e/mcp-e2ekit/server` with a fixed 3-request JSON-RPC script and assert the
  responses (mirrors the existing step-19 smoke of `catacomb mcp`); (b) reduction
  of a fixture transcript that contains the `mcp__e2ekit__record` tool_use node,
  asserting the general MCP step-key node lands.

## 6. The real skill (minimal, protocol-conformant)

A genuine Claude Code skill at **`e2e/skills/e2e-emit/SKILL.md`** (concrete name;
the plan may rename but no `<placeholder>` survives into fixtures):

- Proper YAML frontmatter: `name`, `description` (the fields Claude Code's skill
  loader requires), and a body with a concrete, single action — write a fixed
  known token to `out/result.csv` — that a small `verify_emit.py` checks against
  the golden. Minimal, but a real skill.
- Discovered from **project scope** (`.claude/skills/`) so a live cell that stages
  it into its workspace `.claude/skills/` and runs with `--setting-sources
  project` finds it — self-contained, independent of the user's global/plugin
  skills.

Not a mock — a real, minimal skill exercised via the real `Skill` tool.

Wiring:

- **Live** (`basket-skill.yaml` → `skill.sh`): the workspace `cmd` stages the
  skill dir into the cell's `.claude/skills/`; `claude -p --setting-sources
  project --allowedTools "Skill,..."`. `baseline` invokes the skill; `degraded`
  does the work without it. Runs on sonnet (reliable multi-step obedience).
- **Hermetic:** fixture transcript with the `Skill` tool_use block, asserting the
  `NodeSkill` upgrade and its participation in the gate.

## 7. Live lane detail (`e2e/run.sh`, `e2e-live.yml`)

Three new baskets, each 1 task × 3 variants × 5 reps = 15 live cells (+45 total,
~$5–7/run; subagent cells cost more per cell as they spawn children):

- `e2e/basket-subagent.yaml` + `e2e/subagent.sh` — `--allowedTools
  "Task,Bash(sqlite3:*)"`; baseline delegates the seeded SQL task to a subagent
  that writes `out/result.csv`; degraded does it inline (no `Task` node); verify =
  the existing `verify_sql.py` against the golden. On sonnet.
- `e2e/basket-skill.yaml` + `e2e/skill.sh` + `e2e/skills/e2e-emit/SKILL.md` +
  `e2e/verify_emit.py` — §6. On sonnet.
- `e2e/basket-mcp.yaml` + `e2e/mcp-record.sh` + `e2e/mcp-e2ekit/` — §5.

`e2e/run.sh` extends with each basket's bench + A-vs-A control assertion
(exit 0) + seeded-regression assertion (exit 1, attributed to the swapped axis),
following the existing presence/continuous/sql structure and its
`record`/`run_json` bookkeeping. The driver's cost header is updated. `e2e-live.yml`
bumps `timeout-minutes` to absorb the extra cells.

## 8. Hermetic lane detail (`e2e/hermetic/`)

New fixtures + assertions, either as new steps in `e2e/hermetic/run.sh` or a
sibling `e2e/hermetic/prod/run.sh` invoked by it (the plan decides; a sibling
keeps the already-large `run.sh` readable):

- **Transcript templates** carrying: `isSidechain` subagent lines (incl. a `mark`
  pair *inside* the subagent, exercising the subagent-scoped phase key —
  `reduce/marker_test.go::TestSubagentEnclosingStepKey`), a `Skill` tool_use
  block, and an `mcp__e2ekit__record` tool_use block.
- **Baskets** with baseline/degraded/baseline2 variants whose fixture-emitting
  `agent.sh` prints the appropriate template (deterministic, zero API spend) —
  same mechanism as the existing hermetic sql basket.
- **Composite scenario:** one fixture session where a dispatched subagent marks a
  checkpoint, invokes the skill, calls the MCP tool, and produces a verifiable
  artifact — driven through the full pipeline, gating every axis at once
  (subagent + skill + marker nodes present vs absent; `verifier.pass` 5/5 → 0/5;
  A-vs-A does not gate).
- **MCP protocol smoke:** the fixed 3-request JSON-RPC conformance check of
  `e2e/mcp-e2ekit/server` (§5).
- **Assertions:** deterministic `regress --json` / graph-shape checks that the
  subagent/skill/marker nodes are present in baseline and absent in degraded, that
  the seeded regression gates (exit 1) and A-vs-A does not (exit 0), and that
  `verifier.pass` moves as designed.

## 9. Risks — validate FIRST in the plan (a spike task before fixtures)

1. **STEP-gate participation.** Confirm `subagent` and `skill` nodes participate
   in the STEP-scope regression the same way tool nodes (`echo`) do. If presence
   is better anchored on the `Task` / `Skill` **tool_use** node (which is a
   tool-call step node) than on the synthesized `subagent`/`skill` node, the
   fixtures target that. If neither gates presence today, that is a product gap →
   separate TDD task under 100% coverage, not silently worked around.
2. **Live skill discovery.** Confirm `claude -p --setting-sources project` loads a
   project `.claude/skills/` skill and that the `Skill` tool invokes it. If not,
   determine the minimal correct source set (`project,user`, or a plugin) — probe
   with a one-cell dispatch before wiring the full basket.
3. **Sidechain capture.** Confirm a real `claude -p` subagent dispatch writes
   `isSidechain`/`agentId` lines into the `session.jsonl` that bench captures from
   the projects dir. Probe before full wiring.
4. **Live obedience.** Subagent/skill/MCP obedience held on sonnet with
   single-purpose prompts (the PV-6b recipe); keep each basket single-axis.

The spike task resolves 1–3 against a throwaway run/fixture and records findings
before any basket is finalized. A resolved risk that turns into a product gap is
re-scoped as its own plan task.

## 10. File inventory (indicative)

```
e2e/mcp-e2ekit/server            real stdio MCP server (Python stdlib)
e2e/mcp-e2ekit/mcp.json          mcp-config pointing claude at the server
e2e/basket-subagent.yaml         live subagent basket
e2e/subagent.sh                  live subagent cell wrapper
e2e/basket-skill.yaml            live skill basket
e2e/skill.sh                     live skill cell wrapper
e2e/skills/e2e-emit/SKILL.md     real minimal project-scoped skill
e2e/verify_emit.py               skill-artifact verifier
e2e/basket-mcp.yaml              live MCP basket
e2e/mcp-record.sh                live MCP cell wrapper
e2e/hermetic/prod/               fixture transcripts + baskets + verify hooks + composite
e2e/run.sh                       + subagent/skill/mcp baskets, controls, seeded regressions
e2e/hermetic/run.sh              + production steps (or dispatch to prod/run.sh)
.github/workflows/e2e-live.yml   timeout bump + cost header
AGENTS.md                        E2E row(s) updated to mention the production baskets
```

## 11. Testing & coverage

The additions are bash / Python / YAML e2e fixtures, outside the Go 100%-coverage
gate. Correctness is enforced the way the existing E2E enforces it: every step is
self-asserting (A-vs-A must exit 0, seeded regression must exit 1, node presence
and `verifier.pass` pinned), so a reducer/gate regression turns the hermetic PR
run red. Any Go product change surfaced by §9 is developed TDD-first under the
existing gates. The e2e MCP server's protocol conformance is pinned by the
hermetic smoke (§5b).
