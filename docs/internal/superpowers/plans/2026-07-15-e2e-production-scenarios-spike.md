# Spike findings — production-scenario E2E (Task 0)

Resolved offline against `bin/catacomb` at base `950d886`. Risks 2–3 need an
Anthropic auth secret and are deferred to a CI `e2e-live` dispatch (no local auth).

## Risk 1 — does dropping a subagent/skill/mcp node gate? RESOLVED

Built an offline prototype (fixture-emitting agent + baseline transcript with a
`Task` tool + sidechain subagent vs a degraded transcript with inline Bash),
benched 3 variants × 5 reps, and ran `catacomb regress`.

**Finding shape when a step node is dropped** (baseline `Task` node, absent in
degraded):

```json
{"scope":"step","name":"Task","metric":"presence","verdict":"notable",
 "detail":"present 5/5 -> 0/5; step alignment coverage 0.00 below floor 0.70"}
```

- The verdict is **`notable`, NOT `regression`**. Plain `regress` returns
  **exit 0** (`overall_verdict: ok`, `regressions: 0`) — a dropped step node does
  NOT gate by default.
- `regress ... --fail-on-notable` makes notable findings count toward the gate:
  baseline-vs-degraded → **exit 1** (`overall_verdict: regression`); A-vs-A
  (baseline-vs-baseline2) with `--fail-on-notable --metric-rel-delta 0.5` →
  **exit 0** (0 notables). So the presence gate for subagent/skill/mcp scenarios
  is `--fail-on-notable`, and the assertion targets `verdict == "notable"` with
  `metric == "presence"` and `name == <Task|Skill|mcp__e2ekit__record>`.

**Contrast — phase (checkpoint) presence gates as a regression by default** (from
`e2e/run.sh` step d): `{"scope":"phase","name":"verify","metric":"presence",
"verdict":"regression"}` gates at exit 1 with plain `regress`, provided the task
declares `checkpoints: [<name>]`.

**Decision:** anchor presence on the `Task`/`Skill`/`mcp__e2ekit__record` tool
step node via `--fail-on-notable` (verdict `notable`); assert the `subagent`/
`skill` graph node as an additional structural check. The composite scenario adds
the phase axis (a subagent-scoped `mark work` → `scope:phase,name:work,
verdict:regression`) and the annotation axis (`ann:verifier.pass`).

## CLI surface (confirmed)

- `catacomb replay <t> --export-jsonl <snap>` writes a graph snapshot; node lines
  are `{"kind":"node",...,"type":"subagent"|"skill"|"tool_call"|...}`. There is
  **no `replay --json`**. Structural node check = export + `grep '"type":"subagent"'`.
- `catacomb regress` has **no `--checkpoint` flag**. Phase findings appear
  automatically from recorded markers when the task declares `checkpoints:`.
  Relevant flags: `--fail-on-notable`, `--json`, `--metric-rel-delta`,
  `--annotation`, `--annotation-rate-delta` (default 0.1), `--presence-delta`
  (0.2), `--min-support` (3), `--paired-min-tasks` (5).
- Verifier SDK (`e2e/verify_sql.py`): `from catacomb_verifier import Cell, emit`;
  `cell = Cell.from_env()`; `cell.artifact("out/result.csv")` → captured artifact
  path; `emit(passed=bool, tool="<id>", tool_version="1")` writes the
  `verifier.pass` annotation; `emit(key=..., value=float(...))` for extra keys.
- The hermetic fixture mechanism (bench + reduce from a fixture transcript emitted
  into `$HERMETIC_PROJECTS/hermetic/$sid.jsonl`) works exactly as `e2e/hermetic/
  agent.sh` does; the prototype reproduced it end-to-end with `--projects-dir`.

## Risks 2–3 — deferred to CI (no local auth)

- **Risk 2 (project skill discovery in `claude -p`):** validate via a one-cell
  dispatch in CI (`--setting-sources project` loads `.claude/skills/e2e-emit`).
  If it does not, fall back to `project,user`. Author Task 7 with
  `--setting-sources project`; confirm on the first `e2e-live` dispatch.
- **Risk 3 (sidechain capture):** confirm a real subagent dispatch writes
  `isSidechain`/`agentId` lines into the bench-captured `session.jsonl`. Validate
  on the same CI dispatch. The ingest struct tags (`ingest/jsonl/jsonl.go`) expect
  `isSidechain`, `agentId`, `parent_tool_use_id` — the fixtures use exactly these.
