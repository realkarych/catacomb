# Full live validation — design (100% functional coverage on real evidence, both runtimes)

- **Date:** 2026-07-17
- **Status:** approved design ([ADR-0036](../../adr/0036-full-live-validation.md))
- **Related:** [ADR-0027](../../adr/0027-verification-layer-and-reliability-metrics.md) (A-vs-A + seeded-regression calibration), [ADR-0031](../../adr/0031-multi-runtime-ingestion-codex.md) (Codex runtime; amended), [ADR-0030](../../adr/0030-interactive-session-import.md) (`import`), [ADR-0034](../../adr/0034-gate-self-check.md) (`calibrate`), [ADR-0035](../../adr/0035-wilcoxon-signed-rank.md) (paired axis); the [PV-6b live calibration review](../reviews/2026-07-08-pv6b-live-calibration.md) and the [e2e production-scenarios design](../superpowers/specs/2026-07-15-e2e-production-scenarios-design.md).
- **Grounding:** functional surface + per-item live recipes and the per-basket Haiku-risk verdict are the blueprint compiled at `be58e2c`; Codex capability matrix (MCP/subagent/skill, cheapest model, ingest coverage) verified against Codex CLI 0.144.5. All file:line references verified against the `feat/live-full-validation` worktree.

This spec is the design of record for expanding `e2e/run.sh` from ~34 to the full ~95 functional items on real Claude evidence, and for bringing the Codex live leg to MCP + subagent + skill-substitute parity plus its own offline-transform coverage. It is implementation-ready: §2 is the Claude coverage map, §3 the Codex parity design, §4 the hermetic-mirror strategy, §5 the mixed-model policy, §6 cost, §7 non-goals.

## 1. Architecture and invariants (what does not change)

`e2e/run.sh` is a self-asserting driver: it `bench`es heterogeneous live baskets, then runs the full offline pipeline over the real evidence, asserting two invariants per basket (ADR-0027):

- **A-vs-A** (`baseline` vs `baseline2`, byte-identical env) must **not** gate — the false-positive check. Continuous metrics use a widened `--metric-rel-delta 2.0` band (`ava_metric_band`, run.sh:194) to absorb inter-batch API latency/cost/token drift; presence and error-rate stay at DEFAULT sensitivity (the moat).
- **baseline vs degraded** (one instruction/tool swap) must **gate**, attributed to the swapped instruction (a phase/step presence drop, a `verifier.pass` drop, or a continuous drift).

Step banners are `== <letter>. <desc> ==` echo lines (a…w). Helpers: `run_expect <want> <label> -- cmd`, `run_json <want> <outfile> <label> -- cmd`, `pass`/`failrec`/`skip`/`record`, `verdict_of <json>`. Evidence lands in per-basket runs-dirs `runs1`…`runs7` and manifests `manifest1`…`manifest7`; `$db` holds baselines; `$artifacts` holds every `regress --json`.

This design **adds assertions and baskets only**. No catacomb flags, no schema fields, no reduce/regress behavior changes. Every new live assertion targets an existing CLI contract; the "new" surface is purely e2e wiring and hermetic fixtures.

## 2. Claude 100% live-coverage map

All additions **append to `e2e/run.sh` after the relevant basket bench, reusing that basket's runs-dir** — zero incremental model spend (the one exception, `--error-delta`, is called out). Grouped by CLI area; each row cites the live recipe and where it slots.

### 2.1 `bench` lifecycle flags (insert around steps a / i)

| Flag | Live recipe | Spend |
|---|---|---|
| `--dry-run` | before step a: `bench basket-presence.yaml --dry-run`, assert the printed expansion lists all 30 planned cells; no execution. | $0 |
| `--resume` | after step a completes: re-invoke `bench basket-presence.yaml --runs-dir "$runs1" --manifest "$manifest1" --resume`, assert exit 0 with 0 newly-executed cells (manifest already complete). | $0 |
| `--workspaces-dir` | on step i's sql bench: pass `--workspaces-dir "$work/live-workspaces"`, assert the per-cell workspace root exists under it. | $0 (already a paid cell) |
| `--keep-workspaces` | same call + `--keep-workspaces`, assert the 15 per-cell workspace dirs survive after bench exits (first proof over a REAL per-cell workspace). | $0 |
| explicit `--projects-dir` | re-run presence bench with `--projects-dir "$HOME/.claude/projects"` (the default, passed explicitly), assert identical manifest shape. Low value; "flag parses" only. | $0 |

### 2.2 Failure-mode coverage — the one near-$0 exception (new files)

`--fail-fast` and `--error-delta` need a **failing** cell, which no current basket produces (every manifest assertion requires `exit_code==0`). New minimal artifacts:

- **`e2e/basket-failmode.yaml` + `e2e/failmode.sh`** (Haiku per §5).
  - `--fail-fast`: a variant whose wrapper `exit 1`s **before** calling any model; `bench basket-failmode.yaml --fail-fast` must stop after that cell. **$0** (never reaches the model).
  - `--error-delta`: a Haiku task, two variants — `clean` runs a succeeding Bash tool, `errseed` runs a failing Bash tool (`sh -c 'exit 3'`) so the reduced graph carries a tool `error` node → `error_rate` 0→1; `regress ... --error-delta <below 1.0>` must gate on total `error_rate`. **~cents** (a few Haiku single-tool cells) — the sole non-$0 Claude addition. **Fallback:** if zero new Claude spend is required, drop the `errseed` variant and document `--error-delta` as a live gap (still hermetically covered). Open question for the orchestrator (§8).

### 2.3 `verify` standalone (after steps i/j, sql evidence)

- `verify basket-sql.yaml --runs-dir "$runs3"` → assert 15/15 `verify … ok` lines (standalone re-verify reproduces `verifier.pass` idempotently over real transcripts). Restrict with `--label variant=baseline` → assert only 5/5 lines. **$0.**

### 2.4 `regress` format + threshold flags (append after steps d/f/k/q)

Reuse existing evidence; real API jitter supplies natural per-cell spread for the IQR/audit flips.

| Flag | Live recipe | Evidence |
|---|---|---|
| `--format markdown` | re-issue step k's sql seeded-regression with `--format markdown`; assert markdown header shape (`#`/`**`). | sql |
| `--min-support` | step f continuous baseline-vs-baseline2 (5 reps/group) + `--min-support 6 --strict --json` → assert `insufficient` / exit 2 (deterministic 5<6). | continuous |
| `--presence-delta` | re-run step d presence seeded-regression with `--presence-delta 1.5` (above the true 1.0 delta) → verify-phase finding no longer gates (contrast step d's default-band gate on the SAME evidence). | presence |
| `--iqr-factor` | tighten `--iqr-factor` on step f A-vs-A → assert a stricter band flags a `notable`/`regression` the default band does not. | continuous |
| `--coverage-floor` | re-run step q skill seeded-regression (dropped Skill node, downgraded to `notable` by the 0.7 floor) with `--coverage-floor 0` → SAME finding now `regression`. | skill |
| `--z` | tighten `--z 3.0` on step l sql A-vs-A (`verifier.pass` ~5/5 both sides) → still zero regressions (non-flip smoke). | sql |
| `--annotation-rate-delta` | step k sql seeded regression (`verifier.pass` 5/5→0/5) + `--annotation-rate-delta 0.99` (just under the true 1.0 delta) → STILL gates. | sql |
| `--audit-iqr-factor` / `--audit-rel-delta` | tighten each on step f's continuous comparison → the `audit` block names ≥1 flagged cell the default run does not. | continuous |
| `--project` | add `--project e2e-live` to step e's `--record` call → `trends … --json` surfaces `"project":"e2e-live"` on that row. | presence |

### 2.5 Paired axis (append after steps d/e; presence is the only ≥2-task basket)

- `--paired-test sign|wilcoxon`: drop `task=` from the presence selector so `haiku`+`echo` pool; assert a `paired`-scope finding renders under each test; compare. Evidence: presence.
- `--paired-min-tasks`: `--paired-test sign --paired-min-tasks 3` (only 2 tasks exist) → paired axis reports "not reachable" in `sensitivity`.
- `--paired-alpha`: exercised alongside the above at a non-default value. **$0.**

### 2.6 `calibrate` standalone (after step f, continuous evidence)

- `calibrate --runs-dir "$runs2" --group label:basket=e2e-continuous,variant=baseline --format json` → assert it renders a reachable A/A-split verdict over the real n=5 baseline group (first live proof `calibrate` works on non-synthetic data). Add one `--min-support 2` variant to prove threshold-flag passthrough (`bindThresholdFlags`). **$0.**

### 2.7 `baseline` round-trip + `trends` flags (after step e)

- `baseline list --db "$db" --json` → `e2e-presence-main` with `runs=5`.
- `baseline export e2e-presence-main --db "$db" --runs-dir "$runs1" --out "$work/live-baseline.tar.gz"` → non-empty bundle.
- `baseline import` into a fresh db/runs-dir → reimported run count matches AND `regress --db … name:e2e-presence-main --candidate …degraded --json` gates IDENTICALLY to step d (round-trip fidelity on real content-hashes).
- `baseline rm e2e-presence-main --db "$db"` at the **very end** (last consumer) → a follow-up `baseline list` no longer shows it.
- `trends --json` (record a 2nd `--record` row first), `trends --metric duration_ms`, `trends --pareto` (pin a baseline off the sql `verifier.pass` axis, record ≥2 rows) → all parse/render. **$0.**

### 2.8 `diff` / `subgraph` / `export` / `replay` / `mcp` (append near step g, haiku+echo evidence)

- `diff --json`; `diff --phase verify` (both sides); `diff --a-phase verify --b-from verify --b-to verify` (asymmetric per-side scoping) → side A matches the symmetric `--phase verify` scoping, but side B's `--from/--to verify` is a zero-width empty window, so every side-A item is unmatched (all `removed`) — the asymmetric diff does NOT equal the symmetric one.
- `subgraph --from verify --to verify --json` → a well-formed but EMPTY, zero-width window (RangeWindow scopes `[from.start, to.start)`, so `--from X --to X` is empty), strictly narrower than the non-empty `--phase verify` smoke — range mode is NOT equal to phase mode on live evidence.
- `export "$echo_base_dir/session.jsonl"` (transcript-file branch) → same `step_key` content as the evidence-dir export.
- `replay "$chosen/session.jsonl"` → promote the existing internal helper to an explicit node/edge-summary assertion.
- `mcp`: an explicit protocol smoke — `echo '{"jsonrpc":"2.0","id":1,"method":"initialize",…}' | catacomb mcp` — confirming the same binary that served the live presence cells. **$0.**

### 2.9 `pack` + `import` (after steps i/m, sql + subagent evidence)

- `pack label:basket=e2e-sql,variant=baseline --runs-dir "$runs3" --out "$work/pack-sql" --sample 3` → 3 run dirs + `pack.json` + `INSTRUCTIONS.md`, and one sampled `session.jsonl` contains real `claude -p` stream-json (`"type":"assistant"`). Repeat with `name:e2e-presence-main` for the `--db` selector.
- `import basket-subagent.yaml --task subagent --variant baseline --session-id <sid from manifest4> --projects-dir "$HOME/.claude/projects" --runs-dir "$work/import-runs" --rep 1` → the imported evidence reduces to the SAME subagent-node presence bench captured (real transcript, already paid for). Repeat pointing `--transcript` directly at the resolved `.jsonl` (direct-path branch + `subagents/agent-*.jsonl` glob). **$0.**

## 3. Codex parity design

Cheapest usable model: **`gpt-5.4-mini` at `model_reasoning_effort=low`** (nano is API-only, unavailable in Codex CLI). All Codex baskets are gated behind the existing skip-clean auth probe (run.sh §v: `codex login status` no-spend probe, else a capped single paid ping via `CODEX_API_KEY`) — **no second auth probe is added.** Rollouts have no dollar cost, so these legs never enter the spend total.

### 3.1 Codex MCP basket — asserted

- **Files:** `e2e/basket-codex-mcp.yaml` + `e2e/codex-mcp-live.sh`, templated off `basket-mcp.yaml`. Reuse the existing `mcp-e2ekit/server.py` + `verify_emit.py`; register per-invocation (no global `~/.codex/config.toml` mutation, keeps `reps>1` cells isolated):

  ```sh
  exec codex exec -m gpt-5.4-mini -c model_reasoning_effort=low --skip-git-repo-check --json \
    -c mcp_servers.e2ekit.command=python3 \
    -c mcp_servers.e2ekit.args='["<abs>/mcp-e2ekit/server.py"]' \
    "$MCP_INSTRUCTION" < /dev/null
  ```

- **Variants:** `baseline`/`baseline2` instruct calling `mcp__e2ekit__record` with value `CATACOMB-SKILL-OK` (→ `mcp__e2ekit__record` step node, server writes `out/result.csv`, `verifier.pass`); `degraded` uses no tool (no MCP node AND no artifact — both signals gate). `E2EKIT_OUT` points the server at the cell's absolute `out/result.csv`, as in the Claude wrapper.
- **Ingest:** nothing to build — `ingest/codex/codex.go:341-393` already maps `mcp_tool_call_begin/end` to `mcp__<server>__<tool>`, which `reduce/reduce.go:476` classifies as `mcp_call`. Pure basket authoring.
- **Verdict:** MCP-node drop is deterministic once wired (config-driven registration; single-tool single-call is the favorable obedience shape) → the seeded regression gate is **asserted** (exit 1, dropped `mcp__e2ekit__record` node + failed verifier), plus baseline calls-the-tool in a majority (≥2/3) asserted. A-vs-A hard-asserted with the widened continuous band (same as Claude step u). If `gpt-5.4-mini` under-calls at `low`, the documented lever is bumping just this basket to `model_reasoning_effort=medium`.

### 3.2 Codex subagent basket — hermetic asserted, live logged

- **Files:** `e2e/basket-codex-subagent.yaml` + `e2e/codex-subagent-live.sh`, templated off `basket-subagent.yaml`. `codex exec -c agents.max_threads=2 -m gpt-5.4-mini -c model_reasoning_effort=low --json "$PROMPT" < /dev/null`.
- **Variants:** `baseline` prompt explicitly instructs delegation ("Delegate the … task to a subagent (spawn_agent) and report its result"); `degraded` forbids it ("Do it yourself; do not spawn any subagent"). The child rollout (`session_meta.parent_thread_id`) reduces to a `subagent` node via `resolveCodexTranscripts` + `appendSubagentStop` (`ingest/codex/codex.go:507-524`).
- **Ingest:** nothing to build — proven by scenario 56's parent/child fixtures.
- **Verdict — the key difference from MCP:** Codex delegation is **prompt-discretionary**, not flag-forced (no `--allowedTools "Task"` analogue; the model *chooses* to call `spawn_agent`). A live `baseline` run can spawn 0/3. So the **live** subagent verdict stays **logged/soft** (calibration data only; only regress *erroring* — exit 2 — fails the leg), exactly the posture basket-codex documents for a new runtime. The **hermetic mirror is asserted** (deterministic parent+child fixtures: subagent node present in baseline, absent in degraded → gate). Lever if it under-fires: `model_reasoning_effort=medium`, recorded but not defaulted.

### 3.3 Codex skill basket — artifact-verify substitute (platform limitation)

- **Files:** `e2e/basket-codex-skill.yaml` + `e2e/codex-skill-live.sh`. Stage a real `SKILL.md` under the cell's `.agents/skills/e2e-emit/` (Codex's discovery path), mirroring `e2e/skills/e2e-emit/SKILL.md`.
- **Variants:** `baseline`/`baseline2` prompt: "Use the e2e-emit skill (see .agents/skills/e2e-emit/SKILL.md) to produce out/result.csv"; `degraded` writes the token directly, no skill mention.
- **What is asserted:** the **artifact only** — `verify_emit.py` on `out/result.csv`, gating `verifier.pass` 1→0 between baseline and degraded. **No skill node** is claimed or asserted: Codex emits no skill-invocation event (`reduce/skill.go:10-12` matches only Claude tool names; a skill use reduces to an ordinary `assistant_tool_use` file read, indistinguishable from any read). As a **soft** secondary signal, grep the rollout `function_call`/`exec_command` arguments for a path match on `.agents/skills/e2e-emit/SKILL.md` (proves the file was read — logged, never gated).
- **Verdict:** artifact-verifier gate **asserted** (reliable); the soft skill-read grep **logged**. Documented in the basket comment as a Codex platform limitation, in the same direct voice basket-codex uses.

### 3.4 Codex offline-transform coverage + verdict switch

- **Offline mirror of §2 over Codex evidence** the new baskets produce: `verify` standalone, `regress --format markdown/--min-support/--presence-delta/--z/…`, `pack`, `import` (Codex `--sessions-dir` + `--transcript`), `diff/subgraph/export/replay`, `calibrate` — all $0 offline transforms over the Codex runs-dirs. These append inside the codex leg (skip-clean with it).
- **Verdict switch on the existing `basket-codex.yaml`:** the continuous `tokens_out` separation (main "one short sentence" vs candidate "think step by step … showing your reasoning for each point") is a large, reliable delta reachable at reps=3 / default `MinSupport=3`. Flip step v's *logged* main-vs-candidate verdict to an **asserted** total-scope `tokens_out` regression gate (candidate > main → exit 1). A-vs-A and the subagent basket stay logged.

## 4. Hermetic-mirror strategy (the $0 validation contract)

`e2e/run.sh` needs real API auth, so no implementer can run it. The DoD substitute: **every new live assertion is mirrored by a hermetic prod scenario running the same catacomb invocation + same assertion over fabricated evidence.** The mirror is what proves the assertion *logic*; the live leg differs only in feeding real evidence.

- **Where a mirror already exists** (blueprint "Hermetic? yes" — e.g. `verify`, `calibrate`, `baseline list/rm/export/import`, `--min-support`, `--paired-test`, `subgraph --from/--to`, `pack`, `import`): the task reuses it and adds the run.sh live append; validation = run that scenario at $0.
- **Where no mirror exists** ("Hermetic? NO" — `--format markdown`, `--iqr-factor`, `--coverage-floor`, `--z`, `--annotation-rate-delta`, `--audit-iqr-factor`, `--audit-rel-delta`, `diff --a-phase/--b-phase`, `trends --metric`): the task **adds** a mirror assertion, extending `80-cli-contracts.sh`, `82-wilcoxon.sh`, `84-fleet.sh`, or `90-analysis-cmds.sh` with fabricated fixtures, then validates via the hermetic suite.
- **Codex modality mirrors are new scenarios** modeled on `56-codex-bench.sh` + its fake-codex fixture pattern (`fixtures/56-fake-codex.sh` prints the `thread.started`/`turn.completed` stream and renders a templated rollout into the day-dir tree; `bash`/`sed`/`date` only, zero network):
  - `57-codex-mcp.sh`: a fabricated rollout carrying `mcp_tool_call_begin/end` on `e2ekit.record` → asserts the `mcp__e2ekit__record` node reduces and a degraded (no-tool) rollout drops it → gate. Asserted.
  - `58-codex-subagent.sh`: a parent+child rollout pair (`parent_thread_id`) → asserts the `subagent` node reduces and a degraded (no child) drops it → gate. Asserted (deterministic fixtures; only the *live* verdict is logged).
  - `59-codex-skill.sh`: a fabricated rollout + `out/result.csv` artifact → asserts the `verifier.pass` gate on a wrong/absent artifact, and the soft grep matches a `SKILL.md` read in `function_call` args. Asserted (artifact); soft (grep).
- **Validation command** for every task: `make build && CATACOMB_BIN="$PWD/bin/catacomb" e2e/hermetic/run.sh` (prepend `$PWD/bin` to PATH — a stale system `catacomb` may exist), plus `bash -n e2e/run.sh` + `shellcheck` on any touched shell. The paid legs (real `claude -p` / `codex exec`) run **only** on the Mon+Thu schedule or a manual `workflow_dispatch` — never in an implementer session.

## 5. Mixed-model policy (Claude)

`CHILD_MODEL` is a per-task wrapper convention (`--model "${CHILD_MODEL:-<default>}"`), not a catacomb flag — so the policy is enforced per basket, not globally.

| Basket / task | Model | Rationale |
|---|---|---|
| `continuous` (answer) | Haiku | model-agnostic; free-text, no tool obedience at stake. |
| `presence` (echo) | Haiku | single Bash call; obedience held on Haiku (basket note). |
| new `failmode`/`errseed` cells | **Haiku** | cheapest; no delegation-sensitive assertion. |
| `presence` (haiku/mark task) | **Sonnet** (pinned) | measured Haiku failure: mark obedience 4/5 then 2/5. |
| `sql` | **Sonnet** (pinned) | measured Haiku failure: ~60-80% multi-step tool adherence, below the ≥4/5 floor. |
| `subagent` | **Sonnet** (pinned) | forced-tool presence signal, but structured `Task` call above `echo`'s complexity; no Haiku data. |
| `skill` | **Sonnet** (pinned) | allowed-but-not-forced tool choice — the exact shape of the mark failure. |
| `mcp` | **Sonnet** (pinned) | single-tool single-call, but the `mark` failure was also nominally single-tool; untested on Haiku. |

**Guardrail:** a $0 preflight assertion in `run.sh` greps the five sensitive baskets for `CHILD_MODEL: claude-sonnet-5` and asserts `continuous`/`echo` do NOT pin Sonnet — so a future contributor cannot silently blanket-swap the gate to Haiku and re-introduce the measured failures. Migrating any pinned basket to Haiku later stays a per-task, evidence-gated change (re-run the gate; check that basket's own ≥4/5 reliability floors still hold) — never blanket.

Codex baskets all run `gpt-5.4-mini` (the only usable rung); no per-cell model split applies.

## 6. Cost estimate

- **Claude offline additions (§2.1, 2.3-2.9):** $0 — reuse existing basket evidence.
- **Claude `--error-delta` cell (§2.2):** a few cents (2-6 Haiku single-tool cells), or $0 if dropped as a documented gap.
- **Codex MCP + subagent + skill baskets (§3):** three `gpt-5.4-mini` baskets at `low` reasoning effort — pennies total, token-billed with no reported dollar cost (excluded from the run's dollar total, noted separately like the existing codex leg).
- **Total incremental over today's ~$3-7 Claude run:** cents. The dominant existing cost (105 Claude cells) is unchanged; this work adds coverage, not baskets on the expensive runtime.

## 7. Non-goals

- **A structural Codex skill node.** Impossible without a heuristic against no stable contract (ADR-0031's forbidden pattern); artifact + soft grep is the permanent substitute.
- **Hard-asserting the Codex subagent live spawn** before calibration data exists — logged until the base rate is known.
- **A blanket Haiku switch** — explicitly rejected; the five sensitive baskets stay Sonnet.
- **Cross-runtime step-level A/B** — permanently out (ADR-0031); Claude and Codex baskets are compared only within a runtime.
- **New catacomb flags or schema fields** — this is coverage of existing contracts only.
- **A CI-blocking Codex live gate** — the Codex legs stay optional/skip-clean until auth + budget are provisioned; the Claude gate remains the required leg.
- **Making the paid legs runnable in an implementer session** — they run only on schedule / dispatch.

## 8. Open questions for the orchestrator

- **`--error-delta` spend (§2.2):** accept the ~cents Haiku `errseed` cell to cover it live, or leave `--error-delta` as a documented live gap (hermetic-only)? Recommendation: accept it — pennies, and it is the only `error_rate` axis the live gate would otherwise never touch on real evidence.
- **Codex `reps`:** 3 (matches basket-codex, meets default `MinSupport=3`) vs 5 (more calibration headroom for the subagent base rate) — trades pennies for statistical margin. Recommendation: 3 for MCP/skill (deterministic), 5 for subagent (base-rate discovery).
