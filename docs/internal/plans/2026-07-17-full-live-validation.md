# Full live validation — implementation plan (100% functional coverage, both runtimes)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development — one fresh implementer subagent per task, review after each task. Steps use checkbox (`- [ ]`) syntax. Parallel implementers are encouraged for tasks in the same parallel-group (disjoint files, each in its own isolated worktree); tasks that append to `e2e/run.sh` share a file and stay serial.

**Goal:** expand the paid live gate (`e2e/run.sh`, workflow `e2e-live.yml`, Mon+Thu) from ~34 to the full ~95 functional items on **real Claude evidence**, and bring the Codex live leg to MCP + subagent + skill-substitute parity plus its own offline-transform coverage — validating 100% of catacomb's functionality on real evidence for both runtimes. Decision record: [ADR-0036](../../adr/0036-full-live-validation.md); design of record: [2026-07-17-full-live-validation-design.md](../specs/2026-07-17-full-live-validation-design.md).

**Architecture:** the live gate is a self-asserting bash driver — `bench` heterogeneous live baskets, then run the offline pipeline over the real evidence asserting A-vs-A-does-not-gate and seeded-regression-must-gate (ADR-0027). This plan adds **assertions and baskets only**: no catacomb flags, no schema fields, no reduce/regress changes. Almost every new item is an offline transform appended after an existing basket bench, reusing its runs-dir at $0. The Codex additions are three new `gpt-5.4-mini` baskets (MCP config-registered, subagent prompt-forced, skill artifact-substitute) behind the existing skip-clean codex auth probe. Every live assertion is mirrored by a hermetic prod scenario over fabricated evidence — the $0 contract an implementer validates against (§Global constraints).

**Tech stack:** bash + python3 assertions (existing run.sh idiom); Codex CLI `codex exec`; hermetic prod scenarios (`bash`/`sed`/`date`, zero network). New Claude spend: a few cents (one optional Haiku `--error-delta` cell). Codex: pennies, token-billed. **No Go changes are expected** — if any prove necessary, TDD + 100% coverage (`make cover`) applies without exception. No new dependencies.

## Global constraints

- **Zero API spend in-session.** Implementer subagents CANNOT authenticate `claude -p` or `codex exec` — there is no API key in the dev environment. Every task's Definition of Done is: (a) the code/scenario is structurally correct and `shellcheck`-clean (`bash -n` on run.sh; `shellcheck` on touched wrappers/scenarios); (b) it is validated HERMETICALLY at $0 — the new live assertion is mirrored by a hermetic prod scenario running the SAME catacomb invocation + SAME python assertion over fabricated evidence, and `make build && CATACOMB_BIN="$PWD/bin/catacomb" e2e/hermetic/run.sh` stays green (prepend `$PWD/bin` to PATH — a stale system `catacomb` may exist); (c) `make build` stays green, and `make cover` stays at 100% if any Go changes (none expected).
- **The LIVE legs (real model) are validated ONLY on the Mon+Thu schedule or a manual `workflow_dispatch`.** No task asks an implementer to run the live gate. A task is "done" when its hermetic mirror passes and its run.sh append is `bash -n`/`shellcheck`-clean.
- **$0-incremental where possible.** The ~61 Claude offline items add NO model spend (they run existing commands over evidence the live baskets already produce). The only genuinely new spend is the optional Haiku `--error-delta` cell (cents) and the three Codex `gpt-5.4-mini` baskets (pennies).
- **Reuse, don't duplicate.** Offline-coverage steps append to `e2e/run.sh` after the relevant basket benches, reusing its runs-dir. Codex baskets template off the existing Claude baskets (`basket-mcp.yaml`, `subagent.sh`, `basket-codex.yaml`) adapted to `codex exec` + per-invocation `-c mcp_servers.*` / `-c agents.max_threads`. Hermetic mirrors template off `56-codex-bench.sh` + `56-fake-codex.sh`.
- **Verdict posture:** where a Codex seeded regression is reliable (existing basket-codex `tokens_out`, MCP-node drop), switch the verdict from *logged* to **asserted**; where the base rate is unknown (subagent `spawn_agent`), keep it **logged/soft** live (the hermetic mirror is still asserted) until calibration data accrues. State this in each basket comment.
- **Mixed-model policy (do not blanket-switch):** Haiku for `continuous`, `echo`, and every new cheap cell; **Sonnet pinned** (via `CHILD_MODEL`) on presence-mark, sql, subagent, skill, mcp. A preflight guardrail assertion enforces it (Task 10).
- **run.sh is a serial resource.** Tasks that append to `e2e/run.sh` (parallel-group **R**) MUST land serially. Tasks that only create/modify disjoint files (new Codex baskets/wrappers + new hermetic scenarios, parallel-group **P**; docs, group **D**) may run in parallel in isolated worktrees.
- **No comments in Go code** (repo rule; N/A for shell/docs/yaml). Commit after every green task.

---

## Phase 1 — Claude offline-coverage on real evidence (group R, serial on `e2e/run.sh`)

Each task appends assertions after the cited step and reuses that basket's runs-dir. Where a hermetic mirror already exists it is reused for validation; where it does not, the task adds it (noted).

### Task 1: bench lifecycle flags — `--dry-run`, `--resume`, `--workspaces-dir`, `--keep-workspaces`, explicit `--projects-dir`

**Files:** modify `e2e/run.sh` (inserts around steps a and i); mirror in `e2e/hermetic/prod/scenarios/80-cli-contracts.sh` (extend — `--resume`/`--dry-run` are Go-unit-only today).

**Steps:**

- [ ] Before step a: `run_expect 0 "bench presence --dry-run lists 30 cells" -- catacomb bench basket-presence.yaml --dry-run` and assert stdout lists 30 planned cells (grep the expansion count); no evidence dir created.
- [ ] After step a completes: re-invoke `bench basket-presence.yaml --runs-dir "$runs1" --manifest "$manifest1" --resume`; assert exit 0 and 0 newly-executed cells (parse the "marked N/30" / resume summary line → N already complete).
- [ ] On step i's sql bench call: add `--workspaces-dir "$work/live-workspaces" --keep-workspaces`; after bench, assert the workspace root exists and holds 15 per-cell dirs each containing the copied `sql-live.sh`/`verify_sql.py`.
- [ ] Add an explicit `--projects-dir "$HOME/.claude/projects"` presence re-bench asserting identical manifest cell count (flag-parses smoke; low priority — keep terse).
- [ ] Extend `80-cli-contracts.sh` with a fabricated-manifest `--resume`/`--dry-run`/`--keep-workspaces` mirror; run the hermetic suite green; `bash -n e2e/run.sh` + `shellcheck`.
- [ ] Commit `test(e2e): live bench lifecycle flags (dry-run/resume/workspaces-dir/keep-workspaces)`.

### Task 2: failure-mode coverage — `bench --fail-fast` ($0) + `regress --error-delta` (near-$0 Haiku)

**Files:** create `e2e/basket-failmode.yaml`, `e2e/failmode.sh`; modify `e2e/run.sh`; mirror in a new `e2e/hermetic/prod/scenarios/81-failmode.sh` + fixtures (fabricated error-tool rollout → `error_rate` 0→1).

**Interfaces:**

- `failmode.sh`: reads `FAILMODE` env. `FAILMODE=prefail` → `exit 1` **before** any model call (for `--fail-fast`, $0). `FAILMODE=toolerr` → `claude -p` (Haiku) instructed to run a failing Bash tool (`sh -c 'exit 3'`) so the reduced graph carries an `error` tool node; `FAILMODE=clean` → a succeeding Bash tool.
- `basket-failmode.yaml`: Haiku (§Task 10). A `prefail` task (reps 2, 1 variant) for `--fail-fast`; a `toolerr` task with `clean`/`errseed` variants (reps 3) for `--error-delta`.

**Steps:**

- [ ] Author the wrapper + basket; `shellcheck failmode.sh`.
- [ ] run.sh: `bench basket-failmode.yaml --fail-fast` on the prefail task → assert bench stops after the first failing cell (exit nonzero; manifest shows 1 attempted). Then `bench` the toolerr task; `regress --runs-dir … --baseline label:…,variant=clean --candidate label:…,variant=errseed --error-delta 0.5 --json` → assert a total-scope `error_rate` regression (0→1) gates (exit 1).
- [ ] Add `81-failmode.sh` mirror: a fabricated toolerr rollout (a tool_result with `Process exited with code 3`) and a clean one → same `--error-delta` gate over fixtures at $0; run hermetic suite green.
- [ ] **Fallback branch (orchestrator decision, spec §8):** if zero new Claude spend is mandated, drop the `errseed` variant + the live `--error-delta` append and keep ONLY the hermetic `81-failmode.sh` mirror; note `--error-delta` as a documented live gap in the run.sh header.
- [ ] Commit `test(e2e): failure-mode coverage — fail-fast (\$0) + error-delta`.

### Task 3: `verify` standalone (+ `--label`)

**Files:** modify `e2e/run.sh` (after steps i/j); mirror exists (`90-analysis-cmds.sh:171`, hermetic `run.sh:246-318`) — reuse.

**Steps:**

- [ ] After step j: `run_expect 0 "verify sql standalone re-verify" -- catacomb verify basket-sql.yaml --runs-dir "$runs3"`; assert 15/15 `verify … ok` lines (parse the OK count).
- [ ] `catacomb verify basket-sql.yaml --runs-dir "$runs3" --label variant=baseline` → assert exactly 5/5 lines.
- [ ] Validate via `90-analysis-cmds.sh`; `bash -n` + `shellcheck`.
- [ ] Commit `test(e2e): live standalone verify (+ --label)`.

### Task 4: `regress` format + threshold flags

**Files:** modify `e2e/run.sh` (after steps d/f/k/q); mirror: extend `80-cli-contracts.sh` / `90-analysis-cmds.sh` for the NO-hermetic flags (`--format markdown`, `--iqr-factor`, `--coverage-floor`, `--z`, `--annotation-rate-delta`, `--audit-iqr-factor`, `--audit-rel-delta`); `--min-support`/`--presence-delta`/`--project` mirrors already exist.

**Steps (each an append with a concrete assert):**

- [ ] `--format markdown`: re-issue step k's sql seeded-regression with `--format markdown`; assert stdout contains markdown headers (`#`/`**`) and a table row.
- [ ] `--min-support`: step f continuous baseline-vs-baseline2 + `--min-support 6 --strict --json` → assert `overall_verdict=insufficient` / exit 2.
- [ ] `--presence-delta`: step d presence comparison + `--presence-delta 1.5` → assert the verify-phase presence finding no longer gates (contrast step d's default gate).
- [ ] `--iqr-factor` / `--audit-iqr-factor` / `--audit-rel-delta`: tighten each on step f's continuous comparison → assert a `notable`/`regression` (or an `audit`-block flagged cell) appears that the default run does not.
- [ ] `--coverage-floor`: step q skill seeded-regression + `--coverage-floor 0` → assert the dropped-Skill finding reports `regression` (not the default `notable`).
- [ ] `--z`: step l sql A-vs-A + `--z 3.0` → assert still zero regressions (non-flip smoke).
- [ ] `--annotation-rate-delta`: step k sql seeded-regression + `--annotation-rate-delta 0.99` → assert it STILL gates.
- [ ] `--project`: add `--project e2e-live` to step e's `--record` call → assert `trends e2e-presence-main --json` surfaces `"project":"e2e-live"` on that row.
- [ ] Add the missing hermetic mirrors; run hermetic suite green; `bash -n` + `shellcheck`.
- [ ] Commit `test(e2e): live regress format + threshold flags`.

### Task 5: paired axis — `--paired-test sign|wilcoxon`, `--paired-min-tasks`, `--paired-alpha`

**Files:** modify `e2e/run.sh` (after step e); mirror exists (`82-wilcoxon.sh`) — reuse/extend for `--paired-min-tasks` unreachable case.

**Steps:**

- [ ] Drop `task=` from the presence selector so `haiku`+`echo` pool: `regress --runs-dir "$runs1" --baseline label:basket=e2e-presence,variant=baseline --candidate label:basket=e2e-presence,variant=degraded --paired-test wilcoxon --json` → assert a `paired`-scope finding renders; repeat `--paired-test sign` and log the comparison.
- [ ] `--paired-test sign --paired-min-tasks 3` (only 2 tasks exist) → assert the paired axis reports "not reachable" in `sensitivity`.
- [ ] Exercise `--paired-alpha` at a non-default value alongside.
- [ ] Extend `82-wilcoxon.sh` with the `--paired-min-tasks` unreachable mirror if absent; hermetic green; `bash -n` + `shellcheck`.
- [ ] Commit `test(e2e): live paired axis (sign/wilcoxon/min-tasks/alpha)`.

### Task 6: `calibrate` standalone + threshold passthrough

**Files:** modify `e2e/run.sh` (after step f); mirror exists (hermetic `run.sh:1279-1456`) — reuse.

**Steps:**

- [ ] `run_expect 0 "calibrate continuous A/A" -- catacomb calibrate --runs-dir "$runs2" --group label:basket=e2e-continuous,variant=baseline --format json`; assert it renders a reachable A/A-split verdict (parse the JSON).
- [ ] Add a `--min-support 2` variant call to prove threshold-flag passthrough.
- [ ] Validate against the hermetic calibrate scenarios; `bash -n` + `shellcheck`.
- [ ] Commit `test(e2e): live calibrate self-check on real evidence`.

### Task 7: `baseline` round-trip + `trends --json/--metric/--pareto`

**Files:** modify `e2e/run.sh` (after step e; `baseline rm` at the very end); mirror exists (`84-fleet.sh`, hermetic step 18) — reuse.

**Steps:**

- [ ] `baseline list --db "$db" --json` → assert `e2e-presence-main` with `runs=5`.
- [ ] `baseline export e2e-presence-main --db "$db" --runs-dir "$runs1" --out "$work/live-baseline.tar.gz"` → non-empty bundle.
- [ ] `baseline import "$work/live-baseline.tar.gz" --db "$work/import.db" --runs-dir "$work/import-runs"` → assert reimported run count matches AND `regress --db "$work/import.db" --runs-dir "$work/import-runs" --baseline name:e2e-presence-main --candidate label:basket=e2e-presence,task=haiku,variant=degraded --json` gates IDENTICALLY to step d.
- [ ] Record a 2nd `--record` row (e.g. `--candidate …variant=baseline2`) → `trends e2e-presence-main --json` (both rows parse), `trends … --metric duration_ms` (renders). Pin a baseline off the sql `verifier.pass` axis, record ≥2 rows, `trends … --pareto --json` (resolves over real `verifier.pass`).
- [ ] At the very end of the driver (last consumer of `$db`): `baseline rm e2e-presence-main --db "$db"` → assert a follow-up `baseline list` no longer shows it.
- [ ] Validate via `84-fleet.sh`; `bash -n` + `shellcheck`.
- [ ] Commit `test(e2e): live baseline round-trip + trends json/metric/pareto`.

### Task 8: `diff` asymmetric + `subgraph --from/--to` + `export` transcript-mode + `replay` + `mcp` smoke

**Files:** modify `e2e/run.sh` (near step g); mirror: `90-analysis-cmds.sh` (extend for `--a-phase/--b-phase` asymmetric — symmetric-only today) + `10-mcp-protocol.sh` (reuse).

**Steps:**

- [ ] `diff --json` on step g's two haiku sessions (parses); `diff --phase verify` (both sides, narrower/empty); `diff --a-phase verify --b-from verify --b-to verify` → assert it matches the symmetric `--phase verify` diff (per-side plumbing).
- [ ] `subgraph "$chosen/session.jsonl" --from verify --to verify --json` → assert SAME node count as the `--phase verify` smoke.
- [ ] `export "$echo_base_dir/session.jsonl"` (transcript-file branch) → assert same `step_key` content as the evidence-dir export.
- [ ] `run_expect 0 "replay one live session" -- catacomb replay "$chosen/session.jsonl"` (promote the internal helper to an explicit node/edge-summary assertion).
- [ ] `mcp` protocol smoke: `echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"e2e","version":"0"}}}' | catacomb mcp` → assert a well-formed JSON-RPC response (mirror `10-mcp-protocol.sh`).
- [ ] Extend `90-analysis-cmds.sh` with the asymmetric-diff mirror; hermetic green; `bash -n` + `shellcheck`.
- [ ] Commit `test(e2e): live diff/subgraph/export/replay/mcp smokes`.

### Task 9: `pack` (+ `--db`) + `import` (`--session-id` + `--transcript`)

**Files:** modify `e2e/run.sh` (after steps i/m); mirror exists (hermetic step 13, `60-redaction.sh`, `50-import-subagent.sh`) — reuse.

**Steps:**

- [ ] `pack label:basket=e2e-sql,variant=baseline --runs-dir "$runs3" --out "$work/pack-sql" --sample 3` → assert 3 run dirs + `pack.json` + `INSTRUCTIONS.md`, and one sampled `session.jsonl` contains real stream-json (`"type":"assistant"`). Repeat `name:e2e-presence-main --db "$db"` for the `--db` selector.
- [ ] From `manifest4`, take one subagent baseline cell `session_id`: `import basket-subagent.yaml --task subagent --variant baseline --session-id <sid> --projects-dir "$HOME/.claude/projects" --runs-dir "$work/import-runs" --rep 1` → assert the imported evidence reduces to the SAME subagent-node presence bench captured. Repeat pointing `--transcript` at the resolved `.jsonl` (direct-path branch + `subagents/agent-*.jsonl` glob).
- [ ] Validate via the import/pack hermetic scenarios; `bash -n` + `shellcheck`.
- [ ] Commit `test(e2e): live pack + import (session-id/transcript) on real evidence`.

---

## Phase 2 — Mixed-model policy (group R, serial on `e2e/run.sh`)

### Task 10: model-policy codification + guardrail assertion

**Files:** modify `e2e/run.sh` (a preflight block near the binary checks); ensure `failmode.sh`/`basket-failmode.yaml` (Task 2) pin Haiku.

**Steps:**

- [ ] Add a $0 preflight assertion: grep `basket-presence.yaml` (haiku task), `basket-sql.yaml`, `basket-subagent.yaml`, `basket-skill.yaml`, `basket-mcp.yaml` for `CHILD_MODEL: claude-sonnet-5` (must be present); assert `basket-continuous.yaml` and the presence `echo` task do NOT pin Sonnet. Fail loudly if a sensitive basket lost its Sonnet pin (prevents a silent blanket-Haiku swap re-introducing the measured mark/sql failures).
- [ ] Confirm the new `failmode`/`errseed` cells default to Haiku (`${CHILD_MODEL:-claude-haiku-4-5}` in `failmode.sh`, no Sonnet override in the basket).
- [ ] Mirror: a fabricated-basket grep test is unnecessary (the assertion reads the real baskets); validate with `bash -n` + `shellcheck` and confirm the preflight passes against the current baskets.
- [ ] Commit `test(e2e): mixed-model policy guardrail (sonnet pins on the 5 sensitive baskets)`.

---

## Phase 3 — Codex MCP basket (group P, parallel — disjoint files)

### Task 11: Codex MCP basket + wrapper + hermetic scenario `57-codex-mcp`

**Files:** create `e2e/basket-codex-mcp.yaml`, `e2e/codex-mcp-live.sh`, `e2e/hermetic/prod/scenarios/57-codex-mcp.sh`, `e2e/hermetic/prod/fixtures/57-codex-mcp-*.jsonl.tmpl` (+ reuse `56-fake-codex.sh` or a variant). Does NOT touch `e2e/run.sh` (wired in Task 14).

**Interfaces:**

- `codex-mcp-live.sh`: `codex exec -m gpt-5.4-mini -c model_reasoning_effort=low --skip-git-repo-check --json -c mcp_servers.e2ekit.command=python3 -c mcp_servers.e2ekit.args='["<abs>/mcp-e2ekit/server.py"]' "$MCP_INSTRUCTION" < /dev/null`. Export `E2EKIT_OUT="$PWD/out/result.csv"` (absolute). `set -u` makes an unset `MCP_INSTRUCTION` a loud failure.
- `basket-codex-mcp.yaml`: `runtime: codex`, reps 3, one task with a `workspace.cmd` staging `mcp-e2ekit` + `verify_emit.py` (mirror `basket-mcp.yaml`), `artifacts: [out/result.csv]`, `verify: {cmd: [python3, ./verify_emit.py]}`; variants `baseline`/`baseline2` (call `mcp__e2ekit__record` with `CATACOMB-SKILL-OK`), `degraded` (no tool).

**Steps:**

- [ ] Author wrapper + basket; `shellcheck codex-mcp-live.sh`.
- [ ] `57-codex-mcp.sh`: fabricate a rollout carrying `mcp_tool_call_begin/end` on `{server:e2ekit,tool:record}` + an `out/result.csv` artifact (baseline) and a degraded rollout with no tool + wrong/absent artifact; drive `bench` via the fake-codex pattern, then `regress` baseline-vs-degraded → assert exit 1 with a dropped `mcp__e2ekit__record` step node AND a `verifier.pass` drop; assert A-vs-A over the codex reduce path does not gate. Model the fixture shapes on `56-codex-main.jsonl.tmpl`.
- [ ] Run hermetic suite green (`make build && CATACOMB_BIN="$PWD/bin/catacomb" e2e/hermetic/run.sh`).
- [ ] Commit `test(e2e): codex live MCP basket + hermetic mirror 57-codex-mcp`.

---

## Phase 4 — Codex subagent basket (group P, parallel — disjoint files)

### Task 12: Codex subagent basket + wrapper + hermetic scenario `58-codex-subagent`

**Files:** create `e2e/basket-codex-subagent.yaml`, `e2e/codex-subagent-live.sh`, `e2e/hermetic/prod/scenarios/58-codex-subagent.sh`, `e2e/hermetic/prod/fixtures/58-codex-*.jsonl.tmpl` (parent + child, mirror `55-codex-main`/`55-codex-child`). Does NOT touch `e2e/run.sh` (wired in Task 14).

**Interfaces:**

- `codex-subagent-live.sh`: `codex exec -c agents.max_threads=2 -m gpt-5.4-mini -c model_reasoning_effort=low --skip-git-repo-check --json "$PROMPT" < /dev/null`.
- `basket-codex-subagent.yaml`: `runtime: codex`, reps 5 (base-rate headroom, spec §8), variants `baseline` (prompt EXPLICITLY instructs `spawn_agent` delegation), `degraded` (forbids delegation), `baseline2` (same as baseline). Comment MUST state: live delegation is prompt-discretionary and may fire 0/N — the LIVE verdict is LOGGED/soft; only regress erroring fails the leg.

**Steps:**

- [ ] Author wrapper + basket; `shellcheck codex-subagent-live.sh`.
- [ ] `58-codex-subagent.sh`: fabricate a parent rollout with a `spawn_agent` function_call + a child rollout carrying `session_meta.parent_thread_id` → assert the `subagent` node reduces (baseline), and a degraded rollout with no child drops it → `regress` gates (exit 1). This hermetic gate IS asserted (deterministic fixtures) even though the live verdict is logged. Model on `55-codex-import.sh` + the child fixture.
- [ ] Run hermetic suite green.
- [ ] Commit `test(e2e): codex live subagent basket + hermetic mirror 58-codex-subagent`.

---

## Phase 5 — Codex skill artifact-substitute (group P, parallel — disjoint files)

### Task 13: Codex skill-substitute basket + wrapper + hermetic scenario `59-codex-skill`

**Files:** create `e2e/basket-codex-skill.yaml`, `e2e/codex-skill-live.sh`, `e2e/hermetic/prod/scenarios/59-codex-skill.sh`, `e2e/hermetic/prod/fixtures/59-codex-*.jsonl.tmpl`. Reuse `e2e/skills/e2e-emit/SKILL.md`. Does NOT touch `e2e/run.sh` (wired in Task 14).

**Interfaces:**

- `codex-skill-live.sh`: `codex exec -m gpt-5.4-mini -c model_reasoning_effort=low --skip-git-repo-check --json "$SKILL_INSTRUCTION" < /dev/null`. Workspace `workspace.cmd` stages the skill into `.agents/skills/e2e-emit/SKILL.md` (Codex discovery path) + copies `verify_emit.py`.
- `basket-codex-skill.yaml`: `runtime: codex`, reps 3, `artifacts: [out/result.csv]`, `verify: {cmd: [python3, ./verify_emit.py]}`; variants `baseline`/`baseline2` ("Use the e2e-emit skill (see .agents/skills/e2e-emit/SKILL.md) to produce out/result.csv"), `degraded` (writes the token directly, no skill mention). Comment MUST state: Codex has no skill-invocation event; this basket verifies the artifact only (+ soft grep of the SKILL.md read) — a documented Codex platform limitation. NO skill-node assertion.

**Steps:**

- [ ] Author wrapper + basket; `shellcheck codex-skill-live.sh`.
- [ ] `59-codex-skill.sh`: fabricate a codex rollout that reads `.agents/skills/e2e-emit/SKILL.md` via a `function_call`/`exec_command` and produces `out/result.csv`; a degraded rollout with a wrong/absent artifact → assert the `verifier.pass` gate (1→0, exit 1) and the SOFT grep matches the `SKILL.md` read in `function_call` args (logged, not gated). Explicitly assert NO skill node is claimed.
- [ ] Run hermetic suite green.
- [ ] Commit `test(e2e): codex skill artifact-substitute basket + hermetic mirror 59-codex-skill`.

---

## Phase 6 — Codex live wiring + offline coverage + verdict switch (group R, serial on `e2e/run.sh`)

### Task 14: wire the three Codex baskets into run.sh's codex leg (live assertions)

**Files:** modify `e2e/run.sh` (inside the step-v codex leg, behind the existing auth probe). Depends on Tasks 11-13.

**Steps:**

- [ ] Inside the `codex_leg_ran` block, add runs-dirs/manifests for the three baskets and bench each (reuse the existing `--sessions-dir` default contract).
- [ ] **MCP (asserted):** manifest 6/6 marked + `agent_runtime=codex` + no `cost_usd`; `regress baseline-vs-degraded` → assert exit 1 with a dropped `mcp__e2ekit__record` node AND a `verifier.pass` drop; assert baseline calls the tool in a majority (≥2/3); A-vs-A with the widened continuous band → zero regressions.
- [ ] **Subagent (logged/soft):** manifest marked + stamped; `regress baseline-vs-degraded` renders (exit 0 or 1 both acceptable) and `--json` parses; LOG the spawn rate and verdict (n on a discretionary call) — only regress erroring (exit 2) fails. Comment cites the `medium`-reasoning lever.
- [ ] **Skill (artifact asserted):** manifest marked + stamped; `regress baseline-vs-degraded` → assert the `verifier.pass` gate (exit 1); soft-grep the baseline rollouts for a `.agents/skills/e2e-emit/SKILL.md` read → LOG (never gate); assert NO skill node is present in the codex graph.
- [ ] `bash -n e2e/run.sh` + `shellcheck`; validate each assertion's LOGIC via its Task 11-13 hermetic scenario (the run.sh block mirrors those invocations).
- [ ] Commit `test(e2e): wire codex MCP/subagent/skill baskets into the live leg`.

### Task 15: Codex offline-coverage appends + existing-basket verdict switch

**Files:** modify `e2e/run.sh` (inside the codex leg); mirror: reuse `55-codex-import.sh`/`56-codex-bench.sh`, extend if a flag is uncovered.

**Steps:**

- [ ] Over the Codex evidence produced by Task 14's baskets, append the $0 offline transforms mirroring Phase 1: `verify` standalone, `regress --format markdown`/`--min-support`/`--z`, `pack`, `import` (Codex `--sessions-dir` + `--transcript`), `diff/subgraph/export/replay`, `calibrate`. Assert render/parse (these are the same offline contracts, now on codex evidence).
- [ ] **Verdict switch:** flip step v's existing `basket-codex.yaml` main-vs-candidate from LOGGED to ASSERTED — `regress --baseline label:basket=e2e-codex,variant=main --candidate label:basket=e2e-codex,variant=candidate --json` MUST gate (exit 1) on a total-scope `tokens_out` regression (candidate verbose > main terse). Keep A-vs-A / subagent logged. Update the step-v comment accordingly.
- [ ] Extend `56-codex-bench.sh` mirror if the tokens_out-asserted shape is not already covered (it is — reuse); `bash -n` + `shellcheck`; hermetic green.
- [ ] Commit `test(e2e): codex offline coverage + tokens_out verdict switch (logged->asserted)`.

---

## Phase 7 — Finalize (workflow + header serial; docs parallel)

### Task 16: workflow wiring + run.sh header/cost-note update (group R)

**Files:** modify `e2e/run.sh` (header comment + cost note), `.github/workflows/e2e-live.yml`.

**Steps:**

- [ ] Update the run.sh header: document the new Claude coverage (100% functional surface), the three Codex baskets, the mixed-model policy, and the `--error-delta` posture (per the Task 2 decision). Update the cost note (`== w.`) so the Codex baskets are counted in the "token-billed, no reported dollar cost" line, not the dollar total.
- [ ] `e2e-live.yml`: confirm `CODEX_API_KEY` is exported (it is, line 94) and the Codex CLI install step covers the new baskets; add a header line documenting the extra ~cents Codex spend + the ~cents optional Haiku `--error-delta` cell. Keep actions SHA-pinned; `actionlint` + `zizmor` clean. The Mon+Thu cron (`0 6 * * 1,4`) and `workflow_dispatch` are unchanged.
- [ ] `bash -n e2e/run.sh` + `shellcheck`; `actionlint .github/workflows/e2e-live.yml`.
- [ ] Commit `test(e2e): run.sh header + e2e-live workflow for full validation`.

### Task 17: ADR/spec/plan finalize + guide docs + README index (group D, parallel-safe once code frozen)

**Files:** modify `docs/adr/README.md` (add the 0036 row); cross-check `docs/adr/0036-full-live-validation.md`, `docs/internal/specs/2026-07-17-full-live-validation-design.md`, this plan; update `docs/guide/*` only where they claim Codex is bare-prompt/import-only for the live leg (grep `codex` in `docs/guide/`), and the README if it states the live gate's scope.

**Steps:**

- [ ] Add `| [0036](0036-full-live-validation.md) | Full live validation — 100% functional coverage on real evidence, both runtimes | Accepted |` to `docs/adr/README.md` in numeric order.
- [ ] Grep guide docs for stale "codex live = bare prompt" / "import-only" claims and update to reflect MCP + subagent + skill-substitute parity; note the Codex skill limitation honestly.
- [ ] `markdownlint` + relative-link/anchor check clean across the touched docs.
- [ ] Commit `docs: ADR-0036 index + guide updates for full live validation`.

### Task 18: final review + PR

**Steps:**

- [ ] Final whole-branch review (subagent, most capable model): hermetic mirrors genuinely exercise each new live assertion; verdict posture correct per basket (MCP/skill/continuous asserted, subagent live logged); no accidental blanket-Haiku; `shellcheck`/`actionlint` clean; every live assertion has a $0 mirror. Fix wave if needed; re-review.
- [ ] PR `feat: full live validation — 100% functional coverage on real evidence, both runtimes (ADR-0036)`.

## Deliberately out of scope

- Running the paid live legs in-session (schedule/dispatch only).
- A structural Codex skill node (platform limitation — artifact + soft grep is the permanent substitute).
- Hard-asserting the Codex subagent live spawn before calibration data exists.
- A blanket Haiku switch, or any change to the five Sonnet-pinned baskets.
- New catacomb flags / schema fields (this is coverage of existing contracts only).
- Making the Codex leg CI-blocking (stays optional/skip-clean).

## Task index

| # | Title | Files | Parallel-group | Hermetic-validation command |
|---|---|---|---|---|
| 1 | bench lifecycle flags (dry-run/resume/workspaces-dir/keep-workspaces/projects-dir) | `e2e/run.sh`; `80-cli-contracts.sh` | R (serial on run.sh) | `make build && CATACOMB_BIN="$PWD/bin/catacomb" e2e/hermetic/run.sh`; `bash -n e2e/run.sh` |
| 2 | failure-mode: `--fail-fast` (\$0) + `--error-delta` (near-\$0 Haiku) | create `basket-failmode.yaml`,`failmode.sh`,`81-failmode.sh`+fixtures; mod `e2e/run.sh` | R | hermetic suite + `shellcheck e2e/failmode.sh` |
| 3 | standalone `verify` (+`--label`) | `e2e/run.sh` (mirror `90-analysis-cmds.sh` reused) | R | hermetic suite; `bash -n e2e/run.sh` |
| 4 | `regress` format + threshold flags (markdown/min-support/presence-delta/iqr/coverage-floor/z/annotation-rate-delta/audit-*/project) | `e2e/run.sh`; extend `80-cli-contracts.sh`,`90-analysis-cmds.sh` | R | hermetic suite; `bash -n e2e/run.sh` |
| 5 | paired axis (sign/wilcoxon/min-tasks/alpha) | `e2e/run.sh`; `82-wilcoxon.sh` | R | hermetic suite (82); `bash -n e2e/run.sh` |
| 6 | `calibrate` standalone + threshold passthrough | `e2e/run.sh` (hermetic calibrate scenarios reused) | R | hermetic suite; `bash -n e2e/run.sh` |
| 7 | `baseline` round-trip + `trends json/metric/pareto` | `e2e/run.sh`; `84-fleet.sh` | R | hermetic suite (84); `bash -n e2e/run.sh` |
| 8 | `diff` asymmetric + `subgraph --from/--to` + `export` transcript + `replay` + `mcp` smoke | `e2e/run.sh`; extend `90-analysis-cmds.sh`,`10-mcp-protocol.sh` | R | hermetic suite; `bash -n e2e/run.sh` |
| 9 | `pack` (+`--db`) + `import` (session-id/transcript) | `e2e/run.sh` (hermetic 13/50/60 reused) | R | hermetic suite; `bash -n e2e/run.sh` |
| 10 | mixed-model policy guardrail assertion | `e2e/run.sh`; confirm `failmode.sh` haiku | R | `bash -n e2e/run.sh`; preflight passes on current baskets |
| 11 | Codex MCP basket + wrapper + `57-codex-mcp` | create `basket-codex-mcp.yaml`,`codex-mcp-live.sh`,`57-codex-mcp.sh`+fixtures | P (parallel, disjoint) | `make build && CATACOMB_BIN="$PWD/bin/catacomb" e2e/hermetic/run.sh` |
| 12 | Codex subagent basket + wrapper + `58-codex-subagent` | create `basket-codex-subagent.yaml`,`codex-subagent-live.sh`,`58-codex-subagent.sh`+fixtures | P | hermetic suite |
| 13 | Codex skill-substitute basket + wrapper + `59-codex-skill` | create `basket-codex-skill.yaml`,`codex-skill-live.sh`,`59-codex-skill.sh`+fixtures | P | hermetic suite |
| 14 | wire the 3 Codex baskets into run.sh codex leg (MCP asserted, subagent logged, skill artifact asserted) | `e2e/run.sh` | R (after 11-13) | mirror via 57/58/59; `bash -n e2e/run.sh` |
| 15 | Codex offline coverage + tokens_out verdict switch (logged→asserted) | `e2e/run.sh`; `56-codex-bench.sh` | R (after 14) | hermetic suite (55/56); `bash -n e2e/run.sh` |
| 16 | run.sh header + `e2e-live.yml` wiring | `e2e/run.sh`,`.github/workflows/e2e-live.yml` | R | `bash -n e2e/run.sh`; `actionlint`; `shellcheck` |
| 17 | ADR index + guide docs finalize | `docs/adr/README.md`,`docs/guide/*` | D (docs, parallel-safe) | `markdownlint` + link check |
| 18 | final review + PR | — | — (after all) | whole-branch review; hermetic suite green |
