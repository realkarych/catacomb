# Complex live E2E scenarios — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add four complex scenarios to the token-spending E2E Live Gate — a composite mega-basket (≥3 phases), nested subagents, a live redaction gate, and a second continuous axis (`tokens_in`) — plus a report-only cost total, all within the ~$20/run target.

**Architecture:** Every addition is an `e2e/` fixture (bash wrapper + YAML basket) wired into `e2e/run.sh` as a new self-asserting section, mirrored by a deterministic `$0` hermetic scenario. Delegation and phase structure are FORCED via tool-allowances / staged custom agents where possible (hard live gate); genuinely stochastic signals are LOGGED live and hard-asserted in the hermetic mirror. Zero production-Go changes: all node types (`subagent`, `marker`, `skill`) and continuous metrics (`tokens_in`, `cost_usd`) already exist and reduce today.

**Tech Stack:** Bash, YAML, Python 3 stdlib, the `claude -p` CLI (`@anthropic-ai/claude-code`), `catacomb` CLI (`bench`/`reduce`/`replay`/`regress`/`pack`). No Go changes.

## Global Constraints

- **Work in a git worktree.** This plan is authored on branch `docs/e2e-live-complex-scenarios` at `/Users/karych/src/catacomb/.claude/worktrees/docs/e2e-live-complex-scenarios`. Each task commits there (or in a per-task isolated worktree for parallel work). Never edit the shared checkout. Use absolute paths into the worktree.
- **No comments in Go code.** This plan adds NO Go. Bash/Python/YAML fixtures may (and should) carry explanatory comments matching the dense header style of the existing `e2e/*.sh` files.
- **e2e is outside the Go 100%-coverage gate.** Correctness is enforced by self-asserting steps: A-vs-A control exits 0, seeded regression exits 1, node/placeholder presence pinned. A reducer/gate/redaction regression turns the hermetic PR run red.
- **Mixed-model policy.** Delegation/skill/mark/multi-step baskets pin `CHILD_MODEL: claude-sonnet-5` (Haiku no longer reliably obeys). Cheap metric-only baskets stay on the default `claude-haiku-4-5`. A `$0` preflight guard in `run.sh` statically enforces the Sonnet pin on sensitive baskets — new sensitive baskets (composite, nested, redaction) MUST be added to that guard's list (Task 8).
- **Soft-live / hard-hermetic.** Any assertion that depends on live model obedience beyond a structurally-forced signal is LOGGED live and hard-asserted in a hermetic mirror over fabricated evidence.
- **Cost is report-only.** The run is NEVER failed on cost. `$20` is a target the report makes observable, not an enforced ceiling.
- **`reps: 5`** for every new basket.
- **Anti-gaming workspace layout.** Baskets that produce `out/result.csv` run each cell in a fresh per-cell workspace whose `workspace.cmd` copies the wrapper/verifier from `$E2E_DIR` (exported by `run.sh`), so an artifact cannot survive cell-to-cell. Mirror `basket-sql.yaml` / `basket-skill.yaml`.
- **Local validation is `$0`.** No task runs live `claude -p` (that costs money and needs a secret). Each task is validated by `catacomb bench <basket> --dry-run` (cell-count + parse), `bash -n`/`shellcheck` on wrappers, and the deterministic hermetic mirror. The true live validation is ONE maintainer-triggered dispatch at the end (Task 9).

---

## File Structure

```
e2e/bigprompt.sh                 NEW  wrapper: builds a large-input prompt (tokens_in axis)
e2e/basket-tokensin.yaml         NEW  tokens_in continuous-axis basket (baseline/bigprompt/baseline2)
e2e/redaction.sh                 NEW  wrapper: cat a seeded fake token into out/result.csv
e2e/basket-redaction.yaml        NEW  live redaction basket (1 variant x 5 reps)
e2e/nested.sh                    NEW  wrapper: forced two-level subagent nesting
e2e/basket-nested.yaml           NEW  nested-subagent basket (baseline/degraded/baseline2)
e2e/agents/sql-delegator.md      NEW  custom subagent (tools: Task) to force nesting
e2e/composite.sh                 NEW  wrapper: main marks outer phase + delegates; subagent marks 2 inner phases + skill + artifact
e2e/basket-composite.yaml        NEW  composite mega-basket (baseline/degraded/baseline2)
e2e/run.sh                       EDIT +4 basket sections, new runs/manifest vars, cost-report breakdown, cost header
e2e/hermetic/prod/fixtures/composite.jsonl.tmpl        EDIT +orchestration outer phase + 2nd work occurrence
e2e/hermetic/prod/scenarios/40-composite.sh            EDIT +3-phase (name/occurrence/enclosing) hard-asserts
e2e/hermetic/prod/fixtures/45-tokensin-*.jsonl.tmpl    NEW  tokens_in delta fixtures
e2e/hermetic/prod/scenarios/45-continuous-axis.sh      NEW  hermetic tokens_in gate hard-assert
.github/workflows/e2e-live.yml   EDIT timeout-minutes bump + cost-header text
AGENTS.md                        EDIT E2E row mentions the complex live baskets
```

**Serialization.** Tasks 1–5 and 7–8 all edit `e2e/run.sh`, so they run **serially** (shared file). Task 6 (hermetic composite) touches only `e2e/hermetic/prod/` and may run in parallel with a run.sh task if worktree-isolated, but is logically the mirror of Task 5. Recommended order: 1 → 2 → 3 → 4 → 5 → 6 → 7 → 8 → 9.

---

## Task 1: Second continuous axis (`tokens_in`)

Cheapest, self-contained warm-up. A large-input / short-output basket gating the `tokens_in` continuous axis, plus its hermetic mirror.

**Files:**

- Create: `e2e/bigprompt.sh`
- Create: `e2e/basket-tokensin.yaml`
- Modify: `e2e/run.sh` (new `runs`/`manifest` vars near lines 170–189; new bench+gate section after step `u`)
- Create: `e2e/hermetic/prod/fixtures/45-tokensin-base.jsonl.tmpl`, `45-tokensin-big.jsonl.tmpl`
- Create: `e2e/hermetic/prod/scenarios/45-continuous-axis.sh`

**Interfaces:**

- Produces: basket id `e2e-tokensin`; run dir var `runs_tokensin`; manifest var `manifest_tokensin` (consumed by the cost report in Task 7).

- [ ] **Step 1: Write `e2e/bigprompt.sh`**

```bash
#!/usr/bin/env bash
# tokens_in continuous-axis wrapper. Builds a controllable-size input prompt by
# repeating a filler line PROMPT_REPEATS times, then asks for a one-sentence
# answer so tokens_out stays flat while tokens_in scales with PROMPT_REPEATS.
# baseline/baseline2 use PROMPT_REPEATS=0 (tiny input); bigprompt uses a large
# value so tokens_in trips the continuous gate against baseline while the A-vs-A
# (baseline vs baseline2) stays flat. Haiku is fine: input tokens are cheap and
# the axis is deterministic (input-token count is a near-deterministic function of
# the prompt). Isolation flags match the other live wrappers so local runs match CI.
set -euo pipefail
filler=""
i=0
while [ "$i" -lt "${PROMPT_REPEATS:-0}" ]; do
	filler="${filler}Context line ${i}: catacomb evaluates agentic pipelines offline against stored baselines and gates regressions statistically. "
	i=$((i + 1))
done
exec claude -p "${filler}Ignore the context above. In one short sentence, what is a hash function?" \
	--model "${CHILD_MODEL:-claude-haiku-4-5}" \
	--output-format stream-json \
	--verbose \
	--strict-mcp-config \
	--setting-sources project
```

- [ ] **Step 2: Make it executable and syntax-check**

Run: `chmod +x e2e/bigprompt.sh && bash -n e2e/bigprompt.sh`
Expected: exit 0, no output.

- [ ] **Step 3: Write `e2e/basket-tokensin.yaml`**

```yaml
# tokens_in continuous-axis basket: does a large INPUT (with a deliberately short
# output) trip the continuous gate on tokens_in — an axis distinct from the
# tokens_out gate of basket-continuous.yaml? 1 task x 3 variants x 5 reps = 15
# cells. baseline/baseline2 send a tiny prompt (A-vs-A control, must NOT gate);
# bigprompt sends a ~600-line filler prompt (seeded regression: tokens_in far past
# the band) while still asking for one sentence, so tokens_out stays flat and
# tokens_in is the sole moving axis. `dir: .` matches basket-continuous.yaml so
# run.sh's cd into e2e resolves ./bigprompt.sh. Haiku: input tokens are cheap.
basket: e2e-tokensin
reps: 5
tasks:
  - id: bigprompt
    cmd: ["./bigprompt.sh"]
    dir: .
variants:
  - id: baseline
    env: { PROMPT_REPEATS: "0" }
  - id: bigprompt
    env: { PROMPT_REPEATS: "600" }
  - id: baseline2
    env: { PROMPT_REPEATS: "0" }
```

- [ ] **Step 4: Validate the basket parses and plans 15 cells (`$0`)**

Run: `cd e2e && catacomb bench basket-tokensin.yaml --dry-run | grep -c '^bench-e2e-tokensin-'; cd ..`
Expected: `15`

- [ ] **Step 5: Add run/manifest vars to `e2e/run.sh`**

After the existing `manifest9="$work/manifest-codex-mcp.jsonl"` line (~line 189), add:

```bash
runs_tokensin="$work/runs-tokensin"
manifest_tokensin="$work/manifest-tokensin.jsonl"
```

- [ ] **Step 6: Add the bench + gate section to `e2e/run.sh`**

Insert immediately AFTER step `u` (the `mcp A-vs-A control` block, ~line 2210) and BEFORE the codex leg (`v`, ~line 2280):

```bash
echo "== u2. bench e2e-tokensin basket (15 live claude -p cells) — the tokens_in continuous axis =="
run_expect 0 "bench e2e-tokensin basket" -- \
	catacomb bench basket-tokensin.yaml --runs-dir "$runs_tokensin" --manifest "$manifest_tokensin"

echo "== u3. tokens_in seeded regression (baseline vs bigprompt) must gate on the tokens_in axis =="
run_json 1 "$artifacts/regress-tokensin-seeded.json" \
	"tokens_in seeded regression (baseline vs bigprompt)" -- \
	catacomb regress --runs-dir "$runs_tokensin" \
	--baseline label:basket=e2e-tokensin,variant=baseline \
	--candidate label:basket=e2e-tokensin,variant=bigprompt --json
rc=0
python3 - "$artifacts/regress-tokensin-seeded.json" <<'PY' || rc=$?
import json, sys
rep = json.load(open(sys.argv[1]))
hit = [f for f in rep.get("findings", [])
       if f.get("metric") == "tokens_in" and f.get("verdict") in ("regression", "notable")]
if not hit:
    print("no tokens_in regression/notable finding; findings were:", file=sys.stderr)
    for f in rep.get("findings", []):
        print("  ", {k: f.get(k) for k in ("scope", "metric", "verdict")}, file=sys.stderr)
    sys.exit(1)
print("tokens_in gate fires:", ", ".join(f"{f['scope']}:{f['verdict']}" for f in hit))
PY
record "$rc" "tokens_in seeded regression gates on the tokens_in continuous axis (baseline vs bigprompt)"

echo "== u4. tokens_in A-vs-A control (baseline vs baseline2) must NOT gate =="
run_json 0 "$artifacts/regress-tokensin-AvA.json" \
	"tokens_in A-vs-A must NOT gate (continuous band widened)" -- \
	catacomb regress --runs-dir "$runs_tokensin" \
	--baseline label:basket=e2e-tokensin,variant=baseline \
	--candidate label:basket=e2e-tokensin,variant=baseline2 \
	--metric-rel-delta "$ava_metric_band" --json
rc=0
python3 -c 'import json,sys; r=json.load(open(sys.argv[1])); sys.exit(0 if r["regressions"]==0 and r["overall_verdict"]!="regression" else 1)' "$artifacts/regress-tokensin-AvA.json" || rc=$?
record "$rc" "tokens_in A-vs-A reports zero regressions"
```

- [ ] **Step 7: Confirm the exact continuous-metric key is `tokens_in`**

Run: `grep -rn '"tokens_in"' reduce/reduce.go`
Expected: at least one hit (the reducer emits the `tokens_in` continuous metric). If the emitted key differs, use that key in Step 6's Python and the hermetic fixture below.

- [ ] **Step 8: Write the hermetic tokens_in fixtures**

Model the input-token field on an existing fixture that carries usage — inspect one first:

Run: `grep -rln 'input_tokens\|usage' e2e/hermetic/prod/fixtures/*.tmpl reduce/*.go | head`

Then create `e2e/hermetic/prod/fixtures/45-tokensin-base.jsonl.tmpl` (small input) and `45-tokensin-big.jsonl.tmpl` (large input) — two transcripts identical except for the assistant message `usage.input_tokens` (small vs large), using the field name confirmed above. Use the `composite.jsonl.tmpl` line shape (a `user` line then an `assistant` line carrying `message.usage`) as the template, e.g. the assistant line:

```
{"type":"assistant","uuid":"a1","sessionId":"__SESSION_ID__","timestamp":"2026-06-20T10:00:01Z","message":{"role":"assistant","id":"m1","model":"claude-haiku-4-5","usage":{"input_tokens":200,"output_tokens":10},"content":[{"type":"text","text":"a"}]}}
```

with `input_tokens:200` in `-base` and `input_tokens:20000` in `-big`.

- [ ] **Step 9: Write `e2e/hermetic/prod/scenarios/45-continuous-axis.sh`**

```bash
#!/usr/bin/env bash
# Scenario 45 — second continuous axis (tokens_in): a large-input transcript trips
# the continuous gate on tokens_in while an A-vs-A (base vs base) over the same
# small-input transcript does not. The deterministic hard-mirror of the live
# e2e-tokensin basket (e2e/run.sh steps u2-u4). Fabricated evidence, zero API spend.
set -euo pipefail
echo "== prod.45 tokens_in: large-input transcript gates the tokens_in axis =="
w="$WORK/tokensin"; mkdir -p "$w/cellwork" "$w/runs"
cat > "$w/basket.yaml" <<YAML
basket: prod-tokensin
reps: 5
tasks:
  - id: t
    cmd: ["$PROD/fixtures/emit.sh"]
    dir: $w/cellwork
variants:
  - id: baseline
    env: { SCENARIO_TMPL: "$PROD/fixtures/45-tokensin-base.jsonl.tmpl" }
  - id: big
    env: { SCENARIO_TMPL: "$PROD/fixtures/45-tokensin-big.jsonl.tmpl" }
  - id: baseline2
    env: { SCENARIO_TMPL: "$PROD/fixtures/45-tokensin-base.jsonl.tmpl" }
YAML
HERMETIC_PROJECTS="$w/projects" run_json 0 "$w/bench.out" "bench prod-tokensin basket" -- \
	catacomb bench "$w/basket.yaml" --projects-dir "$w/projects" --runs-dir "$w/runs" --manifest "$w/m.jsonl"

run_json 1 "$w/seeded.json" "large input -> tokens_in gate" -- \
	catacomb regress --runs-dir "$w/runs" \
	--baseline label:basket=prod-tokensin,variant=baseline \
	--candidate label:basket=prod-tokensin,variant=big --json
rc=0; python3 - "$w/seeded.json" <<'PY' || rc=$?
import json, sys
rep = json.load(open(sys.argv[1]))
hit = [f for f in rep.get("findings", []) if f.get("metric") == "tokens_in" and f.get("verdict") in ("regression", "notable")]
if not hit:
    print("no tokens_in finding; findings:", file=sys.stderr)
    for f in rep.get("findings", []): print("  ", {k: f.get(k) for k in ("scope","metric","verdict")}, file=sys.stderr)
    sys.exit(1)
print("tokens_in gate fires deterministically")
PY
record "$rc" "prod.45 tokens_in seeded regression gates on the tokens_in axis"

run_json 0 "$w/ava.json" "prod.45 tokens_in A-vs-A must NOT gate" -- \
	catacomb regress --runs-dir "$w/runs" \
	--baseline label:basket=prod-tokensin,variant=baseline \
	--candidate label:basket=prod-tokensin,variant=baseline2 --metric-rel-delta "$PROD_AVA_METRIC_BAND" --json
rc=0; python3 -c 'import json,sys; r=json.load(open(sys.argv[1])); sys.exit(0 if r["regressions"]==0 else 1)' "$w/ava.json" || rc=$?
record "$rc" "prod.45 tokens_in A-vs-A reports zero regressions"
```

- [ ] **Step 10: Make the scenario executable and run the hermetic prod suite**

Run: `chmod +x e2e/hermetic/prod/scenarios/45-continuous-axis.sh && bash e2e/hermetic/run.sh 2>&1 | tail -30`
Expected: all PASS including the new `prod.45` lines (the `scenarios/*.sh` glob auto-registers it). If tokens_in does not gate, raise `input_tokens` in `-big` until the delta clears the default band, and confirm the metric key from Step 7.

- [ ] **Step 11: Commit**

```bash
git add e2e/bigprompt.sh e2e/basket-tokensin.yaml e2e/run.sh \
	e2e/hermetic/prod/fixtures/45-tokensin-*.jsonl.tmpl e2e/hermetic/prod/scenarios/45-continuous-axis.sh
git commit -m "test(e2e): add tokens_in continuous-axis live basket + hermetic mirror"
```

---

## Task 2: Live redaction gate

A live session emits a fake secret-shaped token through a `tool_result`; the captured evidence must scrub it. Invariant/moat — single variant, not a seeded triple. Hermetic hard-proof already exists (`60-redaction.sh`).

**Files:**

- Create: `e2e/redaction.sh`
- Create: `e2e/basket-redaction.yaml`
- Modify: `e2e/run.sh` (vars + new section)

**Interfaces:**

- Produces: basket id `e2e-redaction`; `runs_redaction`; `manifest_redaction` (cost report, Task 7). Sensitive basket → add to the model-policy guard (Task 8).

- [ ] **Step 1: Write `e2e/redaction.sh`**

```bash
#!/usr/bin/env bash
# Live redaction basket cell wrapper. The workspace.cmd seeds secret.txt with a
# runtime-assembled FAKE GitHub token (matches redact's reGitHubToken but is never
# committed secret-shaped). This wrapper instructs the agent to cat that file and
# copy its content into out/result.csv, so the token flows through a Bash
# tool_result payload deterministically — emission depends on the model running
# cat, NOT on it reproducing a literal secret. bench captures + redacts at capture
# time; run.sh then asserts the raw token is absent and the placeholder present.
# Sonnet for reliable two-step (cat then write) obedience. Isolation flags match
# the other live wrappers.
set -euo pipefail
mkdir -p out
rm -f out/result.csv
exec claude -p "Run exactly: cat secret.txt — then write its exact content as the only line of out/result.csv using the Write tool. Do not paraphrase or alter the content." \
	--model "${CHILD_MODEL:-claude-sonnet-5}" \
	--output-format stream-json --verbose \
	--setting-sources project --strict-mcp-config \
	--allowedTools "Bash(cat:*),Write"
```

- [ ] **Step 2: Executable + syntax check**

Run: `chmod +x e2e/redaction.sh && bash -n e2e/redaction.sh`
Expected: exit 0.

- [ ] **Step 3: Write `e2e/basket-redaction.yaml`**

The `workspace.cmd` assembles the fake token from inert chunks at runtime (no committed line is secret-shaped — same trick as `60-redaction.sh`) and copies the wrapper in from `$E2E_DIR`.

```yaml
# Live redaction basket: a real claude -p session emits a fake secret-shaped token
# through a Bash tool_result; the captured evidence must scrub it. This proves the
# LIVE capture+redaction seam (ADR-0008) end-to-end, which no other live basket
# does. 1 variant x 5 reps = 5 cells (an invariant/moat, not an A-vs-A gate — the
# hard redaction-policy-regression proof lives in the hermetic 60-redaction.sh).
# The workspace assembles the fake ghp_ token at RUNTIME from inert chunks so no
# committed line is itself secret-shaped (gitleaks scans committed lines), writes
# it to secret.txt, and copies the wrapper from E2E_DIR. Sonnet for reliable
# cat-then-write obedience.
basket: e2e-redaction
reps: 5
tasks:
  - id: redaction
    cmd: ["./redaction.sh"]
    workspace:
      cmd: ["sh", "-c", "cp \"$E2E_DIR\"/redaction.sh . && p=ghp_ && b=FAKEfakeFAKEfake0123456789ABCDEF012345 && printf '%s%s' \"$p\" \"$b\" > secret.txt"]
    timeout: 180s
    env:
      CHILD_MODEL: claude-sonnet-5
variants:
  - id: baseline
    env: {}
```

- [ ] **Step 4: Validate dry-run plans 5 cells (`$0`)**

Run: `cd e2e && catacomb bench basket-redaction.yaml --dry-run | grep -c '^bench-e2e-redaction-'; cd ..`
Expected: `5`

- [ ] **Step 5: Add run/manifest vars to `e2e/run.sh`**

Below the Task 1 vars:

```bash
runs_redaction="$work/runs-redaction"
manifest_redaction="$work/manifest-redaction.jsonl"
```

- [ ] **Step 6: Add the bench + assertion section to `e2e/run.sh`**

Insert after the Task 1 `u4` block:

```bash
echo "== u5. bench e2e-redaction basket (5 live claude -p cells) — live secret redaction seam =="
run_expect 0 "bench e2e-redaction basket" -- \
	catacomb bench basket-redaction.yaml --runs-dir "$runs_redaction" --manifest "$manifest_redaction"

echo "== u6. live redaction: raw fake token ABSENT from all captured evidence; placeholder present in a majority =="
# The fake token is assembled the SAME way the basket workspace assembles it, so
# this driver never carries a committed secret-shaped line either.
red_p="ghp_"; red_b="FAKEfakeFAKEfake0123456789ABCDEF012345"; red_token="${red_p}${red_b}"
red_total=0; red_leaks=0; red_placeholder_hits=0
for d in "$runs_redaction"/bench-e2e-redaction-redaction-baseline-r*; do
	[ -f "$d/session.jsonl" ] || continue
	red_total=$((red_total + 1))
	if grep -Fq "$red_token" "$d/session.jsonl"; then red_leaks=$((red_leaks + 1)); fi
	if grep -Fq '‹redacted:github-token›' "$d/session.jsonl"; then red_placeholder_hits=$((red_placeholder_hits + 1)); fi
done
rc=0
[ "$red_leaks" -eq 0 ] || rc=1
record "$rc" "live redaction: raw fake token absent from ALL captured session.jsonl ($red_leaks leaks in $red_total runs)"
rc=0
[ "$red_placeholder_hits" -ge 3 ] || rc=1
record "$rc" "live redaction non-vacuity: ‹redacted:github-token› placeholder present in a majority of runs ($red_placeholder_hits/$red_total >=3)"

echo "== u7. live redaction: pack (third-party-auditor bundle) also scrubbed =="
run_json 0 "$artifacts/redaction-pack.out" "pack e2e-redaction for external audit" -- \
	catacomb pack label:basket=e2e-redaction --runs-dir "$runs_redaction" --out "$work/redaction-pack"
rc=0
if grep -Frq "$red_token" "$work/redaction-pack" 2>/dev/null; then rc=1; fi
record "$rc" "live redaction: packed bundle contains no raw fake token"
```

- [ ] **Step 7: Confirm the placeholder string matches `redact`'s output**

Run: `grep -rn 'redacted:github-token' redact/*.go`
Expected: a hit confirming the exact placeholder `‹redacted:github-token›`. If the label differs, use the actual one in Step 6.

- [ ] **Step 8: Confirm `bash -n` on the edited run.sh**

Run: `bash -n e2e/run.sh`
Expected: exit 0.

- [ ] **Step 9: Commit**

```bash
git add e2e/redaction.sh e2e/basket-redaction.yaml e2e/run.sh
git commit -m "test(e2e): add live secret-redaction gate to the E2E live basket"
```

---

## Task 3: Nested subagents

Two forced levels of delegation via a staged custom subagent (`tools: Task`), gating on depth-2 subagent-node presence. Hermetic hard-mirror `25-multi-nested-subagent.sh` already exists.

> **Revised 2026-07-20 after a live probe** (see memory `claude-p-subagent-tool-behavior`): in `claude -p` 2.1.215 a subagent keeps its OWN full toolset (it runs Bash), and nesting works — so the earlier fear that a Task-only main yields a toolless subagent is disproven. BUT a subagent's file WRITE can be blocked by Claude Code's bash-tool sandbox, so this basket **does NOT assert any `out/result.csv` artifact** — it gates purely on subagent-node DEPTH (count), exactly like `basket-subagent.yaml`. The leaf runs the SQL query and reports the result in its reply (no file write); `verify_sql.py` is NOT copied or used.

**Files:**

- Create: `e2e/agents/sql-delegator.md`
- Create: `e2e/nested.sh`
- Create: `e2e/basket-nested.yaml`
- Modify: `e2e/run.sh` (vars + section)

**Interfaces:**

- Consumes: the SQL seed DB path `$SQL_DB` (already exported by run.sh's SQL setup, ~line 196–204). The gate does NOT verify an artifact, so `verify_sql.py` is not used.
- Produces: basket id `e2e-nested`; `runs_nested`; `manifest_nested`. Sensitive basket → add to the model-policy guard (Task 8).

- [ ] **Step 1: Write the custom subagent `e2e/agents/sql-delegator.md`**

```markdown
---
name: sql-delegator
description: Delegates a SQL task one more level down. Has ONLY the Task tool, so it cannot run the query itself and must spawn a general-purpose subagent to do it.
tools: Task
---

You have ONLY the Task tool. You cannot run any command yourself. When asked to run
a SQL query, spawn a subagent (call the Task tool with subagent_type general-purpose)
and instruct that subagent to run the read-only sqlite3 query and report the rows in
its reply (do not write any file). Report its result.
```

- [ ] **Step 2: Write `e2e/nested.sh`**

```bash
#!/usr/bin/env bash
# Live nested-subagent basket cell wrapper. Two FORCED levels of delegation:
# baseline/baseline2 give the MAIN agent ONLY "Task", so it must delegate to the
# staged custom subagent sql-delegator (tools: Task), which in turn has ONLY Task
# and must delegate again to a general-purpose subagent that runs sqlite3 — depth 2.
# degraded gives the main agent "Task" but instructs it to delegate to a
# general-purpose subagent that runs the query ITSELF (depth 1, no nested node) —
# the seeded nesting regression. The workspace stages e2e/agents/ into the cell's
# .claude/agents/ so --setting-sources project discovers the custom agent. reduce
# synthesizes a subagent node per agentId from the subagents/agent-*.jsonl
# sub-transcripts; run.sh counts DEPTH (>=2 nodes for baseline, <=1 for degraded).
# The gate is subagent-node depth ONLY: no out/result.csv is asserted (a subagent's
# file write can be sandbox-blocked in CI — probe finding), so the leaf just runs the
# read query and reports the totals in its reply, matching basket-subagent.yaml's
# "no artifact verified" posture. Sonnet for reliable multi-level obedience.
set -euo pipefail
exec claude -p "There is a SQLite database (table orders(id, region, status, amount)) at ${SQL_DB}. ${NESTED_INSTRUCTION} The final subagent must run this read-only query and report the rows in its reply (do NOT write any file): sqlite3 -header -csv \"${SQL_DB}\" \"SELECT region, SUM(amount) AS total FROM orders WHERE status='paid' GROUP BY region ORDER BY region\"" \
	--model "${CHILD_MODEL:-claude-sonnet-5}" \
	--output-format stream-json --verbose \
	--setting-sources project --strict-mcp-config \
	--allowedTools "${NESTED_TOOLS}"
```

- [ ] **Step 3: Executable + syntax check both**

Run: `chmod +x e2e/nested.sh && bash -n e2e/nested.sh`
Expected: exit 0.

- [ ] **Step 4: Write `e2e/basket-nested.yaml`**

```yaml
# Live nested-subagent basket: does a subagent that itself spawns a subagent reduce
# to a DEPTH-2 chain of subagent nodes (parent_child edge chain — catacomb models
# nesting only via edges, no parent_agent_id field, per 25-multi-nested-subagent.sh)?
# 1 task x 3 variants x 5 reps = 15 cells. baseline/baseline2 force two levels: main
# has ONLY Task -> delegates to the staged sql-delegator custom agent (tools: Task)
# -> which delegates to a general-purpose subagent that runs sqlite3 (>=2 subagent
# nodes). degraded delegates one level to a general-purpose subagent that runs the
# query itself (1 subagent node — the seeded nesting regression). The workspace
# stages e2e/agents/ into .claude/agents/. Gate is on subagent-node DEPTH only; the
# leaf reports the query result in its reply and NO out/result.csv is asserted (a
# subagent's file write can be sandbox-blocked in CI — probe finding), matching
# basket-subagent.yaml's no-artifact posture. Sonnet for reliable multi-level obedience.
basket: e2e-nested
reps: 5
tasks:
  - id: nested
    cmd: ["./nested.sh"]
    workspace:
      cmd: ["sh", "-c", "mkdir -p .claude/agents && cp -R \"$E2E_DIR\"/agents/. .claude/agents/ && cp \"$E2E_DIR\"/nested.sh ."]
    timeout: 420s
    env:
      CHILD_MODEL: claude-sonnet-5
variants:
  - id: baseline
    env:
      NESTED_TOOLS: "Task"
      NESTED_INSTRUCTION: "You have ONLY the Task tool. Spawn a subagent with subagent_type sql-delegator and pass it the task; it will delegate one more level down."
  - id: degraded
    env:
      NESTED_TOOLS: "Task"
      NESTED_INSTRUCTION: "Spawn ONE subagent with subagent_type general-purpose and have THAT subagent run the sqlite3 query itself directly. Do not nest any further."
  - id: baseline2
    env:
      NESTED_TOOLS: "Task"
      NESTED_INSTRUCTION: "You have ONLY the Task tool. Spawn a subagent with subagent_type sql-delegator and pass it the task; it will delegate one more level down."
```

- [ ] **Step 5: Validate dry-run plans 15 cells (`$0`)**

Run: `cd e2e && catacomb bench basket-nested.yaml --dry-run | grep -c '^bench-e2e-nested-'; cd ..`
Expected: `15`

- [ ] **Step 6: Add run/manifest vars to `e2e/run.sh`**

```bash
runs_nested="$work/runs-nested"
manifest_nested="$work/manifest-nested.jsonl"
```

- [ ] **Step 7: Add the bench + gate section to `e2e/run.sh`**

Insert after the Task 2 `u7` block. This reuses the full-evidence reduction pattern from step `n` (combine `session.jsonl` + all `subagents/agent-*.jsonl`, replay, count subagent nodes):

```bash
echo "== u8. bench e2e-nested basket (15 live claude -p cells) — two-level subagent nesting =="
run_expect 0 "bench e2e-nested basket" -- \
	catacomb bench basket-nested.yaml --runs-dir "$runs_nested" --manifest "$manifest_nested"

echo "== u9. nested-subagent depth separation (seeded regression): baseline reduces >=2 subagent nodes, degraded <=1 =="
# Depth is the count of distinct subagent nodes reduced from the FULL evidence
# (session.jsonl + subagents/agent-*.jsonl): baseline forces two levels (>=2 nodes),
# degraded one level (1 node). Tolerant of live jitter/timeouts (they only shrink the
# denominator), like step n.
count_subagent_depth() { # <variant> -> "hits total" where hits = runs with >=2 subagent nodes
	local variant="$1" hits=0 total=0 d comb snap sf n
	for d in "$runs_nested"/bench-e2e-nested-nested-"$variant"-r*; do
		[ -f "$d/session.jsonl" ] || continue
		total=$((total + 1))
		comb="$work/nested-comb-$(basename "$d").jsonl"
		cat "$d/session.jsonl" > "$comb"
		for sf in "$d"/subagents/agent-*.jsonl; do
			[ -f "$sf" ] && cat "$sf" >> "$comb"
		done
		snap="$work/nested-full-$(basename "$d").jsonl"
		catacomb replay "$comb" --export-jsonl "$snap" >/dev/null 2>&1 || continue
		n=$(grep -o '"type":"subagent"' "$snap" | wc -l | tr -d ' ')
		if [ "$n" -ge 2 ]; then hits=$((hits + 1)); fi
	done
	printf '%s %s' "$hits" "$total"
}
read -r nested_base_hits nested_base_total <<<"$(count_subagent_depth baseline)"
read -r nested_deg_hits nested_deg_total <<<"$(count_subagent_depth degraded)"
read -r nested_base2_hits nested_base2_total <<<"$(count_subagent_depth baseline2)"
rc=0
{ [ "$nested_base_hits" -ge 3 ] && [ "$nested_deg_hits" -le 1 ]; } || rc=1
record "$rc" "nested depth: >=2 subagent nodes in a majority of baseline runs, <=1 in degraded (baseline $nested_base_hits/$nested_base_total >=3 vs degraded $nested_deg_hits/$nested_deg_total <=1)"

echo "== u10. nested A-vs-A depth specificity: baseline2 also nests in a majority (no spurious separation) =="
rc=0
[ "$nested_base2_hits" -ge 3 ] || rc=1
record "$rc" "nested A-vs-A: baseline2 also reaches depth 2 in a majority ($nested_base2_hits/$nested_base2_total >=3)"
```

- [ ] **Step 8: `bash -n` on run.sh**

Run: `bash -n e2e/run.sh`
Expected: exit 0.

- [ ] **Step 9: Commit**

```bash
git add e2e/agents/sql-delegator.md e2e/nested.sh e2e/basket-nested.yaml e2e/run.sh
git commit -m "test(e2e): add nested-subagent live basket (forced two-level delegation)"
```

---

## Task 4: Composite mega-basket

One session carrying ≥3 distinct phases (distinct name / occurrence / subagent-enclosing) + skill + verifiable artifact; hard-gates on forced subagent presence, logs the rich-node coexistence.

> **Validated 2026-07-20 by the live probe** (memory `claude-p-subagent-tool-behavior`): a `claude -p` 2.1.215 subagent CAN invoke `mcp__catacomb__mark` and `Skill` from inside itself — so the composite's subagent marking `work` twice and invoking the skill is viable. The only residual uncertainty is whether the subagent-invoked skill's file WRITE lands in CI (bash-tool sandbox), so the artifact/`verifier.pass` stays SOFT (the basket keeps the `verify:` hook, but the run.sh section hard-gates only the forced subagent presence and LOGS coexistence + verifier — never hard-fails on the artifact).

**Files:**

- Create: `e2e/composite.sh`
- Create: `e2e/basket-composite.yaml`
- Modify: `e2e/run.sh` (vars + section)

**Interfaces:**

- Consumes: `e2e/mcp.json` (the `catacomb mcp` mark server, existing), `e2e/skills/e2e-emit/` (existing), `e2e/verify_emit.py` (existing).
- Produces: basket id `e2e-composite`; `runs_composite`; `manifest_composite`. Sensitive basket → model-policy guard (Task 8).

- [ ] **Step 1: Write `e2e/composite.sh`**

```bash
#!/usr/bin/env bash
# Live composite mega-basket cell wrapper. One session, THREE distinct phases plus a
# skill and a verifiable artifact:
#   - main agent (tools: Task + mcp__catacomb__mark) marks the OUTER "orchestration"
#     phase (top-level enclosing step key) and MUST delegate the inner work (it has no
#     Write/Skill/Bash);
#   - the general-purpose subagent marks the INNER "work" phase TWICE (occ 0 and occ 1
#     — the reducer auto-assigns occurrence via assignOccurrences), invokes the e2e-emit
#     skill (Skill node), and writes out/result.csv (verifier.pass).
# The reduced baseline graph therefore carries subagent + three phase keys (distinct
# name orchestration vs work; occurrence work#0 vs work#1; top-level vs subagent
# enclosing step key) + skill node + verifier.pass simultaneously. degraded gives the
# main agent Write/Skill/mark and NO Task: it does the work inline, marks only the
# outer phase, and skips the skill — dropping the subagent node (primary gate) plus the
# skill node and the two subagent-scoped work phases. The workspace stages the skill
# dir + copies the wrapper/verifier. mark is served by `catacomb mcp` via mcp.json.
# Sonnet for reliable multi-step obedience. Isolation flags match the other wrappers.
set -euo pipefail
mkdir -p out
rm -f out/result.csv
exec claude -p "${COMPOSITE_INSTRUCTION}" \
	--model "${CHILD_MODEL:-claude-sonnet-5}" \
	--output-format stream-json --verbose \
	--mcp-config "$PWD/mcp.json" --strict-mcp-config \
	--setting-sources project \
	--allowedTools "${COMPOSITE_TOOLS}"
```

- [ ] **Step 2: Executable + syntax check**

Run: `chmod +x e2e/composite.sh && bash -n e2e/composite.sh`
Expected: exit 0.

- [ ] **Step 3: Write `e2e/basket-composite.yaml`**

The workspace stages the skill dir (like `basket-skill.yaml`), copies the wrapper + `verify_emit.py`, and renders a per-cell `mcp.json` pointing at the `catacomb mcp` mark server (copy `e2e/mcp.json`, which already references `catacomb mcp` on PATH).

```yaml
# Live composite mega-basket: one session exercising subagent + THREE distinct phases
# (distinct name / occurrence / subagent-enclosing step key) + skill + verifiable
# artifact together — the reducer path where all node types co-exist, never run on
# live evidence. 1 task x 3 variants x 5 reps = 15 cells. baseline/baseline2: main has
# ONLY Task + mark (marks the outer orchestration phase, must delegate); the
# general-purpose subagent marks the inner work phase twice (occ 0/1), invokes the
# e2e-emit skill, writes out/result.csv. degraded: main has mark + Skill + Write and NO
# Task -> inline, one phase, no skill -> drops the subagent node (primary gate), the
# skill node, and the two subagent-scoped work phases. The gate (run.sh) is the FORCED
# subagent-presence separation; the phase/skill co-existence is LOGGED live and
# hard-asserted in the hermetic 40-composite.sh. The workspace stages the skill dir,
# copies wrapper + verifier, and the mark tool is served by `catacomb mcp` via mcp.json.
# Sonnet for reliable multi-step obedience.
basket: e2e-composite
reps: 5
tasks:
  - id: composite
    cmd: ["./composite.sh"]
    workspace:
      cmd: ["sh", "-c", "mkdir -p .claude/skills && cp -R \"$E2E_DIR\"/skills/e2e-emit .claude/skills/ && cp \"$E2E_DIR\"/composite.sh \"$E2E_DIR\"/verify_emit.py \"$E2E_DIR\"/mcp.json ."]
    timeout: 420s
    checkpoints: [orchestration, work]
    artifacts: ["out/result.csv"]
    verify:
      cmd: ["python3", "./verify_emit.py"]
      timeout: 30s
    env:
      CHILD_MODEL: claude-sonnet-5
variants:
  - id: baseline
    env:
      COMPOSITE_TOOLS: "Task,mcp__catacomb__mark"
      COMPOSITE_INSTRUCTION: "Call mcp__catacomb__mark with name=orchestration boundary=start. Then spawn a subagent (Task, subagent_type general-purpose) and instruct it to, in order: call mcp__catacomb__mark name=work boundary=start; use the e2e-emit skill to produce out/result.csv; call mcp__catacomb__mark name=work boundary=end; call mcp__catacomb__mark name=work boundary=start again; call mcp__catacomb__mark name=work boundary=end again. After the subagent returns, call mcp__catacomb__mark with name=orchestration boundary=end. You have no other tools; you MUST delegate the file work."
  - id: degraded
    env:
      COMPOSITE_TOOLS: "mcp__catacomb__mark,Skill,Write"
      COMPOSITE_INSTRUCTION: "Call mcp__catacomb__mark with name=orchestration boundary=start. Then, WITHOUT using any skill and WITHOUT delegating, write exactly the text CATACOMB-SKILL-OK to out/result.csv using the Write tool. Then call mcp__catacomb__mark with name=orchestration boundary=end."
  - id: baseline2
    env:
      COMPOSITE_TOOLS: "Task,mcp__catacomb__mark"
      COMPOSITE_INSTRUCTION: "Call mcp__catacomb__mark with name=orchestration boundary=start. Then spawn a subagent (Task, subagent_type general-purpose) and instruct it to, in order: call mcp__catacomb__mark name=work boundary=start; use the e2e-emit skill to produce out/result.csv; call mcp__catacomb__mark name=work boundary=end; call mcp__catacomb__mark name=work boundary=start again; call mcp__catacomb__mark name=work boundary=end again. After the subagent returns, call mcp__catacomb__mark with name=orchestration boundary=end. You have no other tools; you MUST delegate the file work."
```

- [ ] **Step 4: Validate dry-run plans 15 cells (`$0`)**

Run: `cd e2e && catacomb bench basket-composite.yaml --dry-run | grep -c '^bench-e2e-composite-'; cd ..`
Expected: `15`

- [ ] **Step 5: Add run/manifest vars to `e2e/run.sh`**

```bash
runs_composite="$work/runs-composite"
manifest_composite="$work/manifest-composite.jsonl"
```

- [ ] **Step 6: Add the bench + gate section to `e2e/run.sh`**

Insert after the Task 3 `u10` block. Hard gate = forced subagent-presence separation (reuse the full-evidence reduction pattern). Log the phase/skill coexistence.

```bash
echo "== u11. bench e2e-composite basket (15 live claude -p cells) — subagent+phases+skill+verifier in one session =="
run_expect 0 "bench e2e-composite basket" -- \
	catacomb bench basket-composite.yaml --runs-dir "$runs_composite" --manifest "$manifest_composite"

echo "== u12. composite subagent-presence separation (seeded regression): baseline delegates in a majority, degraded near-never =="
count_composite_subagents() { # <variant> -> "hits total"
	local variant="$1" hits=0 total=0 d comb snap sf
	for d in "$runs_composite"/bench-e2e-composite-composite-"$variant"-r*; do
		[ -f "$d/session.jsonl" ] || continue
		total=$((total + 1))
		comb="$work/composite-comb-$(basename "$d").jsonl"
		cat "$d/session.jsonl" > "$comb"
		for sf in "$d"/subagents/agent-*.jsonl; do
			[ -f "$sf" ] && cat "$sf" >> "$comb"
		done
		snap="$work/composite-full-$(basename "$d").jsonl"
		catacomb replay "$comb" --export-jsonl "$snap" >/dev/null 2>&1 || continue
		if grep -q '"type":"subagent"' "$snap"; then hits=$((hits + 1)); fi
	done
	printf '%s %s' "$hits" "$total"
}
read -r comp_base_hits comp_base_total <<<"$(count_composite_subagents baseline)"
read -r comp_deg_hits comp_deg_total <<<"$(count_composite_subagents degraded)"
rc=0
{ [ "$comp_base_hits" -ge 3 ] && [ "$comp_deg_hits" -le 1 ]; } || rc=1
record "$rc" "composite delegation: subagent node in a majority of baseline runs, absent in degraded (baseline $comp_base_hits/$comp_base_total >=3 vs degraded $comp_deg_hits/$comp_deg_total <=1)"

echo "== u13. composite rich-node coexistence (LOGGED — soft-live; hard-asserted in hermetic 40-composite.sh) =="
# Multi-action subagent obedience is stochastic on sonnet, so this is informational.
# It reports how many baseline runs reduced subagent + skill + >=2 distinct phase keys
# together; the hermetic 40-composite.sh asserts the deterministic version.
comp_rich=0
for d in "$runs_composite"/bench-e2e-composite-composite-baseline-r*; do
	snap="$work/composite-full-$(basename "$d").jsonl"
	[ -f "$snap" ] || continue
	has_sub=$(grep -c '"type":"subagent"' "$snap" || true)
	has_skill=$(grep -c '"type":"skill"' "$snap" || true)
	n_phase=$(grep -o '"phase_key":"[0-9a-f]*"' "$snap" | sort -u | wc -l | tr -d ' ' || true)
	if [ "$has_sub" -ge 1 ] && [ "$has_skill" -ge 1 ] && [ "$n_phase" -ge 2 ]; then comp_rich=$((comp_rich + 1)); fi
done
echo "  LOG   composite rich-node coexistence: $comp_rich baseline run(s) reduced subagent+skill+>=2 phase keys together (informational)"
```

- [ ] **Step 7: Confirm the skill and phase_key node markers in reduced output**

Run: `grep -rn '"type":"skill"\|phase_key' reduce/reduce.go model/model.go | head`
Expected: confirms the reduced-node `type` string for a skill node and the `phase_key` field name used in Step 6's greps. Adjust the greps to the actual strings if they differ.

- [ ] **Step 8: `bash -n` on run.sh**

Run: `bash -n e2e/run.sh`
Expected: exit 0.

- [ ] **Step 9: Commit**

```bash
git add e2e/composite.sh e2e/basket-composite.yaml e2e/run.sh
git commit -m "test(e2e): add composite mega-basket (subagent+3 phases+skill+verifier)"
```

---

## Task 5: Extend the hermetic composite mirror to 3 phases

Make `40-composite.sh` the hard-mirror the composite live basket claims: three phases separated by name, occurrence, and enclosing step key.

**Files:**

- Modify: `e2e/hermetic/prod/fixtures/composite.jsonl.tmpl` (add an outer `orchestration` phase around the Task, and a second `work` occurrence inside the subagent)
- Modify: `e2e/hermetic/prod/fixtures/composite.basket.yaml.tmpl` (`checkpoints: [orchestration, work]`)
- Modify: `e2e/hermetic/prod/scenarios/40-composite.sh` (add name/occurrence/enclosing hard-asserts)

**Interfaces:**

- Consumes: the reducer's phase-key derivation (`phasekey.Compute(enclosingStepKey, markerName, occurrence)`) and occurrence auto-assignment (`reduce/marker.go::assignOccurrences`).

- [ ] **Step 1: Add an outer top-level `orchestration` phase to `composite.jsonl.tmpl`**

The existing fixture opens with `u1` (user "go") then `a1` (the Task tool_use). Insert a MAIN-agent (no `isSidechain`) `orchestration` start BEFORE the Task and an `orchestration` end AFTER the Task result (`u7`). Add these two assistant lines (main-agent, so they carry a top-level enclosing step key, distinct from the subagent's `work`):

Insert after `u1` (before `a1`):

```
{"type":"assistant","uuid":"aOS","sessionId":"__SESSION_ID__","timestamp":"2026-06-20T10:00:00.5Z","message":{"role":"assistant","id":"mOS","model":"claude-sonnet-5","content":[{"type":"tool_use","id":"tOrchS","name":"mcp__catacomb__mark","input":{"name":"orchestration","boundary":"start"}}]}}
{"type":"user","uuid":"uOS","sessionId":"__SESSION_ID__","timestamp":"2026-06-20T10:00:00.6Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tOrchS","content":"","is_error":false}]}}
```

Append after `u7` (the Task tool_result):

```
{"type":"assistant","uuid":"aOE","sessionId":"__SESSION_ID__","timestamp":"2026-06-20T10:00:11Z","message":{"role":"assistant","id":"mOE","model":"claude-sonnet-5","content":[{"type":"tool_use","id":"tOrchE","name":"mcp__catacomb__mark","input":{"name":"orchestration","boundary":"end"}}]}}
{"type":"user","uuid":"uOE","sessionId":"__SESSION_ID__","timestamp":"2026-06-20T10:00:12Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tOrchE","content":"","is_error":false}]}}
```

- [ ] **Step 2: Add a second `work` occurrence inside the subagent in `composite.jsonl.tmpl`**

After the existing `work` end (`a5`/`u6` pair, `tMarkE`), insert a second `work` start/end pair inside the subagent (same `isSidechain`/`agentId`/`parent_tool_use_id`), which the reducer auto-assigns occurrence 1:

```
{"type":"assistant","uuid":"a5b","sessionId":"__SESSION_ID__","timestamp":"2026-06-20T10:00:08.5Z","isSidechain":true,"agentId":"agent1","subagent_type":"general-purpose","parent_tool_use_id":"tTask","message":{"role":"assistant","id":"m5b","model":"claude-sonnet-5","content":[{"type":"tool_use","id":"tMark2S","name":"mcp__catacomb__mark","input":{"name":"work","boundary":"start"}}]}}
{"type":"user","uuid":"u6b","parent_tool_use_id":"tTask","sessionId":"__SESSION_ID__","timestamp":"2026-06-20T10:00:08.6Z","isSidechain":true,"agentId":"agent1","subagent_type":"general-purpose","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tMark2S","content":"","is_error":false}]}}
{"type":"assistant","uuid":"a5c","sessionId":"__SESSION_ID__","timestamp":"2026-06-20T10:00:08.7Z","isSidechain":true,"agentId":"agent1","subagent_type":"general-purpose","parent_tool_use_id":"tTask","message":{"role":"assistant","id":"m5c","model":"claude-sonnet-5","content":[{"type":"tool_use","id":"tMark2E","name":"mcp__catacomb__mark","input":{"name":"work","boundary":"end"}}]}}
{"type":"user","uuid":"u6c","parent_tool_use_id":"tTask","sessionId":"__SESSION_ID__","timestamp":"2026-06-20T10:00:08.8Z","isSidechain":true,"agentId":"agent1","subagent_type":"general-purpose","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tMark2E","content":"","is_error":false}]}}
```

- [ ] **Step 3: Update `checkpoints` in `composite.basket.yaml.tmpl`**

Change `checkpoints: [work]` to `checkpoints: [orchestration, work]`.

- [ ] **Step 4: Add the three-axis hard-asserts to `40-composite.sh`**

After the existing `subagent-scoped work phase dropped -> PHASE regression` block, add a step that reduces one baseline run and asserts three DISTINCT phase keys exist covering the three axes. Append before the `A-vs-A must NOT gate` block:

```bash
echo "== prod.40 composite: >=3 distinct phase keys (name / occurrence / enclosing) =="
base_dir=$(find "$w/runs" -type d -name 'bench-prod-composite-composite-baseline-r*' | sort | head -1)
snap="$w/base.snap.jsonl"
catacomb replay "$base_dir/session.jsonl" --export-jsonl "$snap" >/dev/null 2>&1 || true
rc=0; python3 - "$snap" <<'PY' || rc=$?
import json, sys
names, keys = set(), set()
work_occ = 0
for line in open(sys.argv[1]):
    line = line.strip()
    if not line:
        continue
    o = json.loads(line)
    if o.get("kind") == "marker" or o.get("type") == "marker":
        n = o.get("name") or o.get("marker") or ""
        k = o.get("phase_key")
        if n:
            names.add(n)
        if k:
            keys.add(k)
        if n == "work":
            work_occ += 1
errs = []
if not {"orchestration", "work"} <= names:
    errs.append(f"want phases orchestration+work, got names={sorted(names)}")
if work_occ < 2:
    errs.append(f"want work marked twice (occurrence axis), got {work_occ}")
if len(keys) < 3:
    errs.append(f"want >=3 distinct phase keys (name/occurrence/enclosing), got {len(keys)}: {sorted(keys)}")
if errs:
    for e in errs:
        print("  -", e, file=sys.stderr)
    sys.exit(1)
print(f"composite reduces {len(keys)} distinct phase keys across names={sorted(names)}, work occurrences={work_occ}")
PY
record "$rc" "composite reduces >=3 distinct phase keys (distinct name, repeated-work occurrence, subagent-enclosing step key)"
```

- [ ] **Step 5: Confirm the marker node's field names in reduced JSONL**

Run: `catacomb replay e2e/hermetic/prod/fixtures/composite.jsonl.tmpl --export-jsonl /tmp/probe.jsonl 2>/dev/null; grep -i 'phase_key\|marker\|"kind"' /tmp/probe.jsonl | head`
Note: the template still carries `__SESSION_ID__`, so this may error — instead confirm field names from `model/model.go` (`Kind`, `PhaseKey`) and `reduce/marker.go` and adjust the Step 4 Python accessors (`o.get("kind")`, `o.get("phase_key")`, `o.get("name")`) to the real reduced-node shape.

- [ ] **Step 6: Run the hermetic prod suite**

Run: `bash e2e/hermetic/run.sh 2>&1 | tail -30`
Expected: all PASS including the extended `prod.40` lines. The existing STEP/PHASE/ANNOTATION/A-vs-A asserts must still pass (the added phases don't remove the `work` phase the PHASE assert targets).

- [ ] **Step 7: Commit**

```bash
git add e2e/hermetic/prod/fixtures/composite.jsonl.tmpl e2e/hermetic/prod/fixtures/composite.basket.yaml.tmpl e2e/hermetic/prod/scenarios/40-composite.sh
git commit -m "test(e2e): extend hermetic composite mirror to 3 phases (name/occurrence/enclosing)"
```

---

## Task 6: Cost report — per-basket breakdown, report-only

Extend the existing `w. cost report` to include the new manifests and print a per-basket breakdown. Never fails the run.

**Files:**

- Modify: `e2e/run.sh` (the `w. cost report` block, ~lines 3040–3075)

- [ ] **Step 1: Replace the cost-report Python with a per-basket breakdown**

Replace the `python3 - "$manifest1" ... <<'PY' ... PY` block in step `w` with one that reads named manifests and prints a per-basket line plus the total. Keep writing `$artifacts/cost.txt`:

```bash
echo "== w. cost report (informational — never fails the run) =="
python3 - "$artifacts/cost.txt" <<PY || true
import json, sys

manifests = {
    "presence": "$manifest1",
    "continuous": "$manifest2",
    "sql": "$manifest3",
    "subagent": "$manifest4",
    "skill": "$manifest5",
    "mcp": "$manifest6",
    "failmode": "$manifest8",
    "tokensin": "$manifest_tokensin",
    "redaction": "$manifest_redaction",
    "nested": "$manifest_nested",
    "composite": "$manifest_composite",
}
out = open(sys.argv[1], "w")
total = 0.0
for name, path in manifests.items():
    sub = 0.0
    try:
        for line in open(path):
            line = line.strip()
            if not line:
                continue
            c = json.loads(line).get("cost_usd")
            if isinstance(c, (int, float)):
                sub += c
    except (OSError, ValueError):
        continue
    total += sub
    msg = f"  {name:<11} \${sub:.2f}"
    print(msg)
    out.write(msg + "\n")
msg = f"total live spend: \${total:.2f} (target \$20; report-only, never fails the run)"
print(msg)
out.write(msg + "\n")
PY
```

Note the `<<PY` (unquoted heredoc) so the `$manifest_*` shell vars expand; the `\$` escapes keep literal dollar signs in the Python f-strings.

- [ ] **Step 2: Keep the existing codex-note block**

The `if [ "$codex_leg_ran" -eq 1 ]; then ... codex_note=... fi; echo "$codex_note"; printf ... >>"$artifacts/cost.txt"` block that follows stays unchanged (codex reports no dollar cost).

- [ ] **Step 3: `bash -n` and a dry sanity check of the Python**

Run: `bash -n e2e/run.sh && python3 -c 'import ast,io; print("py-ok")'`
Expected: exit 0, `py-ok`. (Full execution is exercised on the live dispatch; here only syntax is checked.)

- [ ] **Step 4: Commit**

```bash
git add e2e/run.sh
git commit -m "test(e2e): cost report — per-basket breakdown, report-only (never fails)"
```

---

## Task 7: Workflow + docs

Bump the live workflow timeout, update its cost header, add the new baskets to the model-policy guard, and update AGENTS.md.

**Files:**

- Modify: `.github/workflows/e2e-live.yml` (`timeout-minutes`, header comment)
- Modify: `e2e/run.sh` (cost header comment; model-policy guard basket list)
- Modify: `AGENTS.md` (E2E row)

- [ ] **Step 1: Add the new sensitive baskets to the model-policy guard**

Inspect the guard (`run.sh` ~lines 136–167, "model-policy guardrail (Task 10)") and add `basket-composite.yaml`, `basket-nested.yaml`, and `basket-redaction.yaml` to the list of files that must carry `CHILD_MODEL: claude-sonnet-5`.

Run first: `sed -n '136,167p' e2e/run.sh`
Then add the three basket filenames to whatever array/loop enumerates the sensitive baskets, matching the existing syntax exactly.

- [ ] **Step 2: Bump `timeout-minutes` in `.github/workflows/e2e-live.yml`**

The new baskets add 50 cells (tokensin 15 + redaction 5 + nested 15 + composite 15), the sonnet/subagent-spawning ones being slow. Change `timeout-minutes: 75` to `timeout-minutes: 120`.

- [ ] **Step 3: Update the cost header in `.github/workflows/e2e-live.yml`**

Update the header comment's cost/scope figures: the claude side now benches the six original production baskets + failmode + **tokensin (15) + redaction (5) + nested (15) + composite (15) = 50 new cells**, raising the estimate to **~$14–20 per run**, with a report-only per-basket cost breakdown in the summary. Replace the `~$3–7 per run — 114 live bench cells` phrasing accordingly (now 164 cells).

- [ ] **Step 4: Update the `Cost:` header block in `e2e/run.sh`**

Update the run.sh header comment (~lines 49–56) with the same new figures and a one-line note that the four complex baskets (composite/nested/redaction/tokensin) and the report-only cost total were added.

- [ ] **Step 5: Update `AGENTS.md`**

Find the E2E row/paragraph:

Run: `grep -n 'e2e\|E2E\|live gate\|basket' AGENTS.md | head`

Add a sentence to the E2E description noting the live gate now also drives a composite mega-basket (≥3 phases), nested subagents, a live redaction gate, and a `tokens_in` continuous axis, with a report-only cost total.

- [ ] **Step 6: `bash -n` and lint the workflow YAML**

Run: `bash -n e2e/run.sh && python3 -c 'import yaml,sys; yaml.safe_load(open(".github/workflows/e2e-live.yml")); print("yaml-ok")'`
Expected: exit 0, `yaml-ok`.

- [ ] **Step 7: Commit**

```bash
git add .github/workflows/e2e-live.yml e2e/run.sh AGENTS.md
git commit -m "ci(e2e): wire complex baskets into the live gate (timeout, guard, cost header, docs)"
```

---

## Task 8: Full hermetic suite green + open a PR

- [ ] **Step 1: Run the entire hermetic e2e suite**

Run: `bash e2e/hermetic/run.sh 2>&1 | tail -40`
Expected: all PASS (the new `prod.45` and extended `prod.40` included; nothing else regressed).

- [ ] **Step 2: Dry-run every new basket once more (`$0`)**

Run:

```bash
cd e2e
for b in basket-tokensin basket-redaction basket-nested basket-composite; do
	printf '%s: ' "$b"; catacomb bench "$b.yaml" --dry-run | grep -c "^bench-"
done
cd ..
```

Expected: `basket-tokensin: 15`, `basket-redaction: 5`, `basket-nested: 15`, `basket-composite: 15`.

- [ ] **Step 3: Confirm the model-policy guard passes over the real basket files**

Run: `bash e2e/run.sh 2>&1 | sed -n '1,40p'` — but this will try to bench live and needs a key; instead extract and run ONLY the guard. If the guard is a standalone function, call it; otherwise confirm by inspection that `basket-composite.yaml`, `basket-nested.yaml`, `basket-redaction.yaml` each contain `CHILD_MODEL: claude-sonnet-5` and appear in the guard list:

Run: `grep -l 'claude-sonnet-5' e2e/basket-composite.yaml e2e/basket-nested.yaml e2e/basket-redaction.yaml`
Expected: all three listed.

- [ ] **Step 4: Push the branch and open a PR**

```bash
git push -u origin docs/e2e-live-complex-scenarios
gh pr create --title "test(e2e): complex live gate scenarios — composite, nested, redaction, tokens_in" \
	--body "$(cat <<'BODY'
Adds four complex scenarios to the token-spending E2E Live Gate, per
docs/superpowers/specs/2026-07-20-e2e-live-complex-scenarios-design.md:

- composite mega-basket: subagent marks an outer phase + delegates; the subagent
  marks the inner `work` phase twice and invokes the e2e-emit skill, so subagent +
  three phase keys (name / occurrence / subagent-enclosing) + skill + verifier
  co-exist; hard gate on forced subagent presence, coexistence logged; hermetic
  40-composite.sh extended to hard-assert the three-axis phase separation.
- nested subagents: two forced levels via a staged `sql-delegator` custom agent;
  gate on depth-2 subagent-node count.
- live redaction gate: a real session emits a fake `ghp_` token through a Bash
  tool_result; captured evidence + pack bundle must scrub it to the placeholder.
- second continuous axis `tokens_in`: a large-input / short-output basket gates the
  tokens_in axis; new hermetic mirror 45-continuous-axis.sh.
- cost report: per-basket `cost_usd` breakdown + total, report-only (never fails).

Fixtures + workflow YAML only; zero production-Go change. Hermetic PR run is green;
the live leg validates on the next dispatch (Task 9).
BODY
)"
```

---

## Task 9: One maintainer-triggered live dispatch (closeout — needs a secret + budget)

Not automatable in a subagent (spends real budget, needs an Anthropic secret). After the hermetic PR is green and merged (or on the branch), the maintainer triggers ONE live dispatch and reviews the results — this is the only true validation of live obedience and the cost figure.

- [ ] **Step 1: Trigger the live gate**

Run: `gh workflow run e2e-live.yml --ref docs/e2e-live-complex-scenarios`
(or from master after merge). Requires `ANTHROPIC_API_KEY` (or `CLAUDE_CODE_OAUTH_TOKEN`) set in repo secrets.

- [ ] **Step 2: Watch the run and review the outcome**

Run: `gh run watch $(gh run list --workflow=e2e-live.yml --limit 1 --json databaseId -q '.[0].databaseId')`
Confirm: every new step (`u2`–`u13`) PASSes; the `w. cost report` per-basket breakdown and total print; total is within the ~$20 target.

- [ ] **Step 3: If a live signal is unreliable, iterate the prompt/reps**

If a forced gate (composite/nested subagent presence) shows insufficient separation, tighten the wrapper instruction (single-purpose, per the PV-6b recipe) or confirm the custom-agent tool restriction applied under `--setting-sources project`. If obedience of the composite's multi-step subagent script is low, that only affects the LOGGED coexistence line (u13), not the hard gate (u12). Re-dispatch after any change. If the reducer is found to mis-handle a real case (e.g. nested depth, occurrence), open a separate TDD'd Go task under the 100%-coverage gate — do not work around it in fixtures.

- [ ] **Step 4: Record the confirmed cost in the spec**

Once green, append the observed per-basket cost + total to the design spec's §8 (replacing the order-of-magnitude estimate with the measured figure) and commit.

---

## Self-Review

**Spec coverage:**

- §3.1 composite (≥3 phases, name/occurrence/enclosing) → Task 4 (live) + Task 5 (hermetic hard-mirror). ✓
- §3.2 nested subagents → Task 3. ✓
- §3.3 live redaction gate → Task 2. ✓
- §3.4 second continuous axis tokens_in → Task 1. ✓
- §4 cost report (report-only, per-basket) → Task 6. ✓
- §5 spike-first risk mitigation → folded into per-task `--dry-run` + hermetic validation + the explicit field-name confirmation steps (1.7, 2.7, 4.7, 5.5) and the closeout live dispatch (Task 9). ✓
- §6 files → all covered across Tasks 1–7. ✓
- §7 testing (self-asserting, hermetic hard proofs, no Go change) → Tasks 5, 6, 8. ✓
- Mixed-model guard for new sensitive baskets → Task 7 Step 1. ✓

**Placeholder scan:** No "TBD"/"handle edge cases". The field-name confirmation steps (Step 1.7, 4.7, 5.5) are legitimate verify-against-the-codebase steps, each with an exact `grep`/`replay` command and a stated fallback, not vague placeholders.

**Type/name consistency:** basket ids (`e2e-tokensin`, `e2e-redaction`, `e2e-nested`, `e2e-composite`) and var names (`runs_*`/`manifest_*`) are used identically across the bench call, the regress selectors, and the cost report. The `count_*` helpers each scope to their own basket's run dir. Phase names (`orchestration`, `work`) match between the live wrapper (Task 4), the basket `checkpoints` (Task 4), and the hermetic fixture (Task 5).

**Known live risks (validated only on Task 9 dispatch):** (a) the `sql-delegator` custom-agent tool restriction applying under `claude -p --setting-sources project`; (b) the composite subagent obeying a 5-mark script (mitigated: only the LOGGED line depends on it, the hard gate does not); (c) the model reproducing a fake token faithfully (mitigated: emission is via `cat`, deterministic). Each has a stated fallback in Task 9 Step 3.
