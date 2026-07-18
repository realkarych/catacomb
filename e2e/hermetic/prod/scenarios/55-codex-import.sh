#!/usr/bin/env bash
# Scenario 55 — codex import: the `runtime: codex` basket path end-to-end, entirely
# offline. A fake ~/.codex/sessions tree (YYYY/MM/DD day dirs) is staged with 10 main
# rollouts — 5 baseline + 5 degraded, each under a distinct thread uuid — plus one
# subagent child rollout whose first-line session_meta carries parent_thread_id
# pointing at the baseline-r1 thread. Everything enters through `catacomb import
# --session-id <thread-id> --sessions-dir <root>` (no codex binary, zero API spend)
# and is gated by `catacomb regress` over the imported evidence:
#   (1) resolution: each thread id resolves to its rollout under the day-dir tree and
#       the child is discovered by the parent_thread_id peek — the baseline-r1
#       evidence dir gains subagents/agent-<child-thread-id>.jsonl and its exported
#       graph a "type":"subagent" node (subagent_type=explorer from agent_role);
#   (2) stamps: meta.json env carries agent_runtime=codex and agent_version from the
#       rollout's cli_version — the dispatch key regress/verify/export use to pick
#       the codex parser when consuming this evidence;
#   (3) checkpoints: the basket task declares `checkpoints: [plan]` and the rollouts
#       carry the mcp__catacomb__mark MCP pair (boundary start/end), so import warns
#       about nothing and the plan marker becomes a real phase — asserted twice, as
#       a "type":"marker" name=plan node in the export and as a phase-scope finding
#       named plan in the regress report;
#   (4) the GATE: degraded rollouts differ by an error tool result ("Process exited
#       with code 3" — codex has no structured error flag, the exit-code prose IS
#       the signal) and 3x tokens_out, so baseline-vs-degraded gates (exit 1) with
#       total tokens_out 100->300 and total error_rate 0->1 regressions, plus the
#       phase-scope plan tokens_out regression of (3);
#   (5) NON-VACUITY: the same 5 baseline rollouts imported again as variant
#       baseline2 (A-vs-A) do NOT gate — exit 0, zero regressions, zero notables —
#       so (4) fires on the planted degradation, not on codex-import noise.
# The plan end-mark lands after task_complete so the turn node (which carries the
# token_count usage) falls inside the plan marker window — that is what routes the
# tokens_out delta to the phase scope in (3)/(4). 5 reps per variant clear regress's
# MinSupport. Sourced by run.sh with lib.sh loaded and PROD/WORK/HERMETIC_* exported.
set -euo pipefail
echo "== prod.55 codex-import: stage a fake codex sessions tree (5 baseline + 5 degraded + 1 child) =="
w="$WORK/codex-import"; mkdir -p "$w/runs"
day="$w/sessions/2026/07/16"; mkdir -p "$day"
base_uuid() { printf '019f6b85-c0de-7be3-81dc-ae856386020%s' "$1"; }
deg_uuid() { printf '019f6b85-de60-7be3-81dc-ae856386030%s' "$1"; }
child_uuid="019f6b85-c41d-7be3-81dc-ae85638604aa"
for r in 1 2 3 4 5; do
  sed "s/__THREAD_ID__/$(base_uuid "$r")/g" "$PROD/fixtures/55-codex-main.jsonl.tmpl" \
    > "$day/rollout-2026-07-16T15-40-00-$(base_uuid "$r").jsonl"
  sed "s/__THREAD_ID__/$(deg_uuid "$r")/g" "$PROD/fixtures/55-codex-degraded.jsonl.tmpl" \
    > "$day/rollout-2026-07-16T15-40-00-$(deg_uuid "$r").jsonl"
done
sed -e "s/__THREAD_ID__/$child_uuid/g" -e "s/__PARENT_THREAD_ID__/$(base_uuid 1)/g" \
  "$PROD/fixtures/55-codex-child.jsonl.tmpl" > "$day/rollout-2026-07-16T15-40-03-$child_uuid.jsonl"
cp "$PROD/fixtures/55-codex.basket.yaml.tmpl" "$w/basket.yaml"

run_json 0 "$w/import.out" "import baseline r1 by --session-id against the staged sessions root" -- \
  catacomb import "$w/basket.yaml" --task probe --variant baseline --rep 1 \
  --session-id "$(base_uuid 1)" --sessions-dir "$w/sessions" --runs-dir "$w/runs"
rc=0; ! grep -q "missing checkpoints" "$w/import.out.stderr" || rc=1
record "$rc" "import emits no missing-checkpoints warning (the plan mark pair is honored)"

echo "== prod.55 codex-import: 5 reps per variant (baseline / degraded / baseline2) =="
rc=0
for r in 2 3 4 5; do
  catacomb import "$w/basket.yaml" --task probe --variant baseline --rep "$r" \
    --session-id "$(base_uuid "$r")" --sessions-dir "$w/sessions" --runs-dir "$w/runs" >/dev/null 2>&1 || rc=1
done
for r in 1 2 3 4 5; do
  catacomb import "$w/basket.yaml" --task probe --variant degraded --rep "$r" \
    --session-id "$(deg_uuid "$r")" --sessions-dir "$w/sessions" --runs-dir "$w/runs" >/dev/null 2>&1 || rc=1
  catacomb import "$w/basket.yaml" --task probe --variant baseline2 --rep "$r" \
    --session-id "$(base_uuid "$r")" --sessions-dir "$w/sessions" --runs-dir "$w/runs" >/dev/null 2>&1 || rc=1
done
record "$rc" "imported 5 reps each of baseline, degraded (error tool result + 3x tokens_out), baseline2 (A-vs-A twin)"

rundir="$w/runs/import-prod-codex-import-probe-baseline-r1"
rc=0; { [ -f "$rundir/meta.json" ] && [ -f "$rundir/session.jsonl" ]; } || rc=1
record "$rc" "import wrote a bench-cell evidence dir (meta.json + session.jsonl)"
rc=0; [ -f "$rundir/subagents/agent-$child_uuid.jsonl" ] || rc=1
record "$rc" "child rollout discovered via parent_thread_id peek landed in subagents/ of baseline r1 only"
rc=0; [ ! -d "$w/runs/import-prod-codex-import-probe-baseline-r2/subagents" ] || rc=1
record "$rc" "baseline r2 (a different thread) has no subagents dir"
rc=0; python3 - "$rundir/meta.json" <<'PY' || rc=$?
import json, sys
m = json.load(open(sys.argv[1]))
env = m.get("env") or {}
checks = {
    "agent_runtime=codex": env.get("agent_runtime") == "codex",
    "agent_version from cli_version": env.get("agent_version") == "0.144.4",
    "task label": m.get("task") == "probe",
    "variant label": m.get("variant") == "baseline",
    "session id is the thread id": m.get("session_id") == "019f6b85-c0de-7be3-81dc-ae8563860201",
}
bad = [k for k, ok in checks.items() if not ok]
if bad:
    print("meta.json checks failed: %s" % ", ".join(bad), file=sys.stderr)
    sys.exit(1)
PY
record "$rc" "meta.json stamps agent_runtime=codex + agent_version=0.144.4 and carries cell labels"

echo "== prod.55 codex-import: export the imported evidence -> subagent + plan marker nodes =="
snap="$w/export.snap.jsonl"
run_json 0 "$w/export.out" "export baseline-r1 evidence dir -> jsonl graph snapshot (codex meta-stamp dispatch)" -- \
  catacomb export "$rundir" --out "$snap"
rc=0; grep -q '"type":"subagent"' "$snap" || rc=1
record "$rc" "exported graph contains a \"type\":\"subagent\" node (import entry point, codex child rollout)"
rc=0; grep -q '"type":"subagent"[^}]*"subagent_type":"explorer"' "$snap" || rc=1
record "$rc" "subagent node carries subagent_type=explorer (agent_role from the child session_meta)"
rc=0; grep -q '"type":"marker"[^}]*"name":"plan"\|"name":"plan"[^}]*"type":"marker"' "$snap" || rc=1
record "$rc" "mcp__catacomb__mark pair became a \"type\":\"marker\" name=plan checkpoint node"

echo "== prod.55 codex-import: degraded (error result + 3x tokens_out) -> regress GATES (exit 1) =="
run_json 1 "$w/regress.json" "baseline vs degraded -> tokens_out + error_rate gate" -- \
  catacomb regress --runs-dir "$w/runs" \
  --baseline label:basket=prod-codex-import,variant=baseline \
  --candidate label:basket=prod-codex-import,variant=degraded --json
rc=0; python3 - "$w/regress.json" <<'PY' || rc=$?
import json, sys
rep = json.load(open(sys.argv[1]))
f = rep.get("findings", [])
def has(scope, metric, name=None):
    return any(x.get("scope") == scope and x.get("metric") == metric
               and (name is None or x.get("name") == name)
               and x.get("verdict") == "regression" for x in f)
# tokens_out 100 -> 300 (the 3x token_count plant) and error_rate 0 -> 1 (the
# "Process exited with code 3" tool result) both gate at total scope; the plan
# checkpoint surfaces as its own phase-scope tokens_out regression because the
# turn node sits inside the plan marker window.
tokens = has("total", "tokens_out")
errors = has("total", "error_rate")
phase = has("phase", "tokens_out", "plan")
if not (tokens and errors and phase):
    print("missing axis: total tokens_out=%s total error_rate=%s phase plan=%s"
          % (tokens, errors, phase), file=sys.stderr)
    for x in f:
        print("  ", {k: x.get(k) for k in ("scope", "name", "metric", "verdict", "detail")}, file=sys.stderr)
    sys.exit(1)
print("gate fires on total tokens_out, total error_rate, and the plan phase together")
PY
record "$rc" "regress gates on total tokens_out (100->300), total error_rate (0->1), and a phase-scope plan finding"

echo "== prod.55 codex-import: A-vs-A must NOT gate (non-vacuity) =="
run_json 0 "$w/ava.json" "A-vs-A (baseline vs baseline2, same rollouts) must NOT gate" -- \
  catacomb regress --runs-dir "$w/runs" \
  --baseline label:basket=prod-codex-import,variant=baseline \
  --candidate label:basket=prod-codex-import,variant=baseline2 --fail-on-notable --json
rc=0; python3 -c 'import json,sys; r=json.load(open(sys.argv[1])); sys.exit(0 if r["regressions"]==0 and not [f for f in r.get("findings", []) if f.get("verdict")=="notable"] else 1)' "$w/ava.json" || rc=$?
record "$rc" "A-vs-A reports zero regressions and no notable findings (the gate would pass at exit 0 without the plant)"

echo "== prod.55 codex-import: --transcript branch (direct rollout path) writes the SAME codex-stamped evidence =="
# Task-15 Part-A mirror: run.sh's codex leg imports a live thread by BOTH --session-id
# (proven above) and --transcript (the direct rollout-file path). Here the baseline-r1
# rollout this scenario staged is re-imported by --transcript into a fresh runs-dir; the
# basket's runtime:codex selects the codex reduce, so meta.json must stamp agent_runtime=codex
# exactly as the --session-id branch did. Structural / determinism only.
roll="$day/rollout-2026-07-16T15-40-00-$(base_uuid 1).jsonl"
rc=0; [ -f "$roll" ] || rc=1
record "$rc" "resolved the baseline r1 rollout file for the --transcript import"
run_json 0 "$w/import-transcript.out" "import baseline r1 by --transcript (direct rollout path)" -- \
  catacomb import "$w/basket.yaml" --task probe --variant baseline --rep 9 \
  --transcript "$roll" --runs-dir "$w/runs-transcript"
tdir="$w/runs-transcript/import-prod-codex-import-probe-baseline-r9"
rc=0; { [ -f "$tdir/meta.json" ] && [ -f "$tdir/session.jsonl" ]; } || rc=1
record "$rc" "import --transcript wrote a bench-cell evidence dir (meta.json + session.jsonl)"
rc=0; python3 -c 'import json,sys; m=json.load(open(sys.argv[1])); env=m.get("env") or {}; sys.exit(0 if env.get("agent_runtime")=="codex" else 1)' "$tdir/meta.json" || rc=1
record "$rc" "import --transcript meta.json stamps agent_runtime=codex (the codex dispatch key)"
