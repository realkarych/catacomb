#!/usr/bin/env bash
# Scenario 58 — codex subagent: the `runtime: codex` live-subagent basket path
# (basket-codex-subagent.yaml) end-to-end, entirely offline — the deterministic
# mirror of a signal that stays discretionary live. Modeled directly on 55's
# import path (no codex binary, zero API spend): a fake ~/.codex/sessions tree
# (YYYY/MM/DD day dirs) is staged with 5 baseline main rollouts (each carrying a
# spawn_agent/wait_agent function_call pair around a shared exec_command step)
# + 5 matching child rollouts (each child's session_meta.parent_thread_id points
# at its own baseline rep's thread) + 5 degraded main rollouts (same shared
# exec_command step but failing, no spawn_agent call, no child rollout at all).
# Everything enters through `catacomb import --session-id <thread-id>
# --sessions-dir <root>` and is gated by `catacomb regress`. What is asserted:
#   (1) resolution: every baseline thread id resolves to its rollout AND gains
#       its own subagents/agent-<child-thread-id>.jsonl (the parent_thread_id
#       peek runs per import, so all 5 baseline reps delegate, not just r1 as in
#       55) while every degraded thread id resolves with no subagents/ dir at
#       all — the "spawn_agent forces a child, no-spawn_agent has none" split
#       basket-codex-subagent.yaml documents for the live leg;
#   (2) stamps: baseline r1's meta.json env carries agent_runtime=codex and
#       agent_version from the rollout's cli_version, same dispatch key as 55/56;
#   (3) structural node (the "reduces (baseline) / drops (degraded)" claim,
#       checked directly per cell BEFORE the aggregate gate, not inferred from
#       it): exporting baseline r1's evidence dir yields a "type":"subagent"
#       node with subagent_type=counter (agent_role from the child's
#       session_meta); exporting degraded r1's evidence dir yields NO
#       "type":"subagent" node at all;
#   (4) the GATE (THIS gate, unlike basket-codex-subagent.yaml's live leg, IS
#       hard-asserted — the fixtures make delegation deterministic, not
#       discretionary): baseline-vs-degraded regress gates (exit 1). The
#       delegation split alone cannot be the metric that fires this: baseline's
#       and degraded's step sets (spawn_agent/wait_agent/subagent vs plain
#       exec_command) barely overlap, so regress's own coverage floor
#       correctly DOWNGRADES a pure step-presence delta to "notable" rather
#       than "regression" (same reasoning 55/56 already encode: they gate
#       through a token/error delta on a step BOTH variants share, not through
#       a baseline-only step's presence). This scenario follows that same
#       proven mechanic: both variants run the identical ./tally.sh
#       exec_command step; baseline succeeds (exit 0), degraded fails (exit 3,
#       "Process exited with code 3" — codex's error signal) and reports 3x
#       tokens_out (100->300). regress gates on total tokens_out and total
#       error_rate exactly as 55 does — the aggregate verdict — while (3) above
#       already hard-asserts the structural subagent claim directly;
#   (5) NON-VACUITY: the same 5 baseline rollouts imported again as variant
#       baseline2 (A-vs-A) do NOT gate — exit 0, zero regressions, zero
#       notables — so (4) fires on the planted degradation, not on
#       codex-import noise. 5 reps per variant clear regress's MinSupport.
# Sourced by run.sh with lib.sh loaded and PROD/WORK/HERMETIC_* exported.
set -euo pipefail
echo "== prod.58 codex-subagent: stage a fake codex sessions tree (5 baseline+child + 5 degraded) =="
w="$WORK/codex-subagent"; mkdir -p "$w/runs"
day="$w/sessions/2026/07/16"; mkdir -p "$day"
base_uuid() { printf '019f6b85-58c0-7be3-81dc-ae856386020%s' "$1"; }
deg_uuid() { printf '019f6b85-58de-7be3-81dc-ae856386030%s' "$1"; }
child_uuid() { printf '019f6b85-58c4-7be3-81dc-ae856386040%s' "$1"; }
for r in 1 2 3 4 5; do
  sed "s/__THREAD_ID__/$(base_uuid "$r")/g" \
    "$PROD/fixtures/58-codex-main.jsonl.tmpl" > "$day/rollout-2026-07-16T15-40-00-$(base_uuid "$r").jsonl"
  sed -e "s/__THREAD_ID__/$(child_uuid "$r")/g" -e "s/__PARENT_THREAD_ID__/$(base_uuid "$r")/g" \
    "$PROD/fixtures/58-codex-child.jsonl.tmpl" > "$day/rollout-2026-07-16T15-40-03-$(child_uuid "$r").jsonl"
  sed "s/__THREAD_ID__/$(deg_uuid "$r")/g" \
    "$PROD/fixtures/58-codex-degraded.jsonl.tmpl" > "$day/rollout-2026-07-16T15-40-00-$(deg_uuid "$r").jsonl"
done
cp "$PROD/fixtures/58-codex.basket.yaml.tmpl" "$w/basket.yaml"

run_json 0 "$w/import.out" "import baseline r1 by --session-id against the staged sessions root" -- \
  catacomb import "$w/basket.yaml" --task delegate --variant baseline --rep 1 \
  --session-id "$(base_uuid 1)" --sessions-dir "$w/sessions" --runs-dir "$w/runs"

echo "== prod.58 codex-subagent: 5 reps per variant (baseline / degraded / baseline2) =="
rc=0
for r in 2 3 4 5; do
  catacomb import "$w/basket.yaml" --task delegate --variant baseline --rep "$r" \
    --session-id "$(base_uuid "$r")" --sessions-dir "$w/sessions" --runs-dir "$w/runs" >/dev/null 2>&1 || rc=1
done
for r in 1 2 3 4 5; do
  catacomb import "$w/basket.yaml" --task delegate --variant degraded --rep "$r" \
    --session-id "$(deg_uuid "$r")" --sessions-dir "$w/sessions" --runs-dir "$w/runs" >/dev/null 2>&1 || rc=1
  catacomb import "$w/basket.yaml" --task delegate --variant baseline2 --rep "$r" \
    --session-id "$(base_uuid "$r")" --sessions-dir "$w/sessions" --runs-dir "$w/runs" >/dev/null 2>&1 || rc=1
done
record "$rc" "imported 5 reps each of baseline (spawn_agent+child), degraded (no spawn_agent, no child), baseline2 (A-vs-A twin)"

rundir="$w/runs/import-prod-codex-subagent-delegate-baseline-r1"
rc=0; { [ -f "$rundir/meta.json" ] && [ -f "$rundir/session.jsonl" ]; } || rc=1
record "$rc" "import wrote a bench-cell evidence dir (meta.json + session.jsonl)"

echo "== prod.58 codex-subagent: every baseline rep delegates, every degraded rep does not =="
rc=0
for r in 1 2 3 4 5; do
  [ -f "$w/runs/import-prod-codex-subagent-delegate-baseline-r$r/subagents/agent-$(child_uuid "$r").jsonl" ] || rc=1
done
record "$rc" "all 5 baseline reps discovered their own child via parent_thread_id (subagents/agent-<child>.jsonl each)"
rc=0
for r in 1 2 3 4 5; do
  [ ! -d "$w/runs/import-prod-codex-subagent-delegate-degraded-r$r/subagents" ] || rc=1
done
record "$rc" "all 5 degraded reps have no subagents dir (no spawn_agent call, no child rollout planted)"

rc=0; python3 - "$rundir/meta.json" <<'PY' || rc=$?
import json, sys
m = json.load(open(sys.argv[1]))
env = m.get("env") or {}
checks = {
    "agent_runtime=codex": env.get("agent_runtime") == "codex",
    "agent_version from cli_version": env.get("agent_version") == "0.144.4",
    "task label": m.get("task") == "delegate",
    "variant label": m.get("variant") == "baseline",
    "session id is the thread id": m.get("session_id") == "019f6b85-58c0-7be3-81dc-ae8563860201",
}
bad = [k for k, ok in checks.items() if not ok]
if bad:
    print("meta.json checks failed: %s" % ", ".join(bad), file=sys.stderr)
    sys.exit(1)
PY
record "$rc" "meta.json stamps agent_runtime=codex + agent_version=0.144.4 and carries cell labels"

echo "== prod.58 codex-subagent: export -> subagent node present (baseline) / absent (degraded) =="
basesnap="$w/export-baseline.snap.jsonl"
run_json 0 "$w/export-baseline.out" "export baseline-r1 evidence dir -> jsonl graph snapshot" -- \
  catacomb export "$rundir" --out "$basesnap"
rc=0; grep -q '"type":"subagent"' "$basesnap" || rc=1
record "$rc" "baseline export contains a \"type\":\"subagent\" node (import entry point, codex child rollout)"
rc=0; grep -q '"type":"subagent"[^}]*"subagent_type":"counter"' "$basesnap" || rc=1
record "$rc" "subagent node carries subagent_type=counter (agent_role from the child session_meta)"

degdir="$w/runs/import-prod-codex-subagent-delegate-degraded-r1"
degsnap="$w/export-degraded.snap.jsonl"
run_json 0 "$w/export-degraded.out" "export degraded-r1 evidence dir -> jsonl graph snapshot" -- \
  catacomb export "$degdir" --out "$degsnap"
rc=0; grep -q '"type":"subagent"' "$degsnap" && rc=1
record "$rc" "degraded export contains NO \"type\":\"subagent\" node (no child rollout was ever discovered)"

echo "== prod.58 codex-subagent: baseline (delegates, tally ok) vs degraded (no delegation, tally fails) -> regress GATES (exit 1) =="
run_json 1 "$w/regress.json" "baseline vs degraded -> tokens_out + error_rate gate" -- \
  catacomb regress --runs-dir "$w/runs" \
  --baseline label:basket=prod-codex-subagent,variant=baseline \
  --candidate label:basket=prod-codex-subagent,variant=degraded --json
rc=0; python3 - "$w/regress.json" <<'PY' || rc=$?
import json, sys
rep = json.load(open(sys.argv[1]))
f = rep.get("findings", [])
def has(scope, metric, name=None):
    return any(x.get("scope") == scope and x.get("metric") == metric
               and (name is None or x.get("name") == name)
               and x.get("verdict") == "regression" for x in f)
# tokens_out 100 -> 300 (the 3x token_count plant) and error_rate 0 -> 1 (the
# "Process exited with code 3" tally.sh result) both gate at total scope, the
# same proven mechanic 55/56 use — a pure baseline-only step (spawn_agent /
# subagent) can't be the fired metric: it barely overlaps the degraded step
# set, so regress's coverage floor correctly downgrades that axis to
# "notable" (asserted separately below), not "regression".
tokens = has("total", "tokens_out")
errors = has("total", "error_rate")
if not (tokens and errors):
    print("missing axis: total tokens_out=%s total error_rate=%s" % (tokens, errors), file=sys.stderr)
    for x in f:
        print("  ", {k: x.get(k) for k in ("scope", "name", "metric", "verdict", "detail")}, file=sys.stderr)
    sys.exit(1)
# The subagent node's OWN stepkey (distinct from the named spawn_agent/
# wait_agent tool-call steps) carries no "name" at all — that absence is what
# uniquely identifies it among the step-scope presence findings below.
subagent_presence_notable = any(
    x.get("scope") == "step" and x.get("metric") == "presence" and x.get("verdict") == "notable"
    and x.get("name") is None and "present 5/5 -> 0/5" in str(x.get("detail", ""))
    for x in f
)
if not subagent_presence_notable:
    print("expected the subagent stepkey's presence delta (unnamed step, 5/5 -> 0/5) to surface as a notable finding", file=sys.stderr)
    for x in f:
        print("  ", {k: x.get(k) for k in ("scope", "name", "metric", "verdict", "detail")}, file=sys.stderr)
    sys.exit(1)
print("gate fires on total tokens_out (100->300) and total error_rate (0->1); subagent stepkey presence drop surfaces as a (non-gating) notable")
PY
record "$rc" "regress gates on total tokens_out (100->300) and total error_rate (0->1); subagent presence drop logged as notable"

echo "== prod.58 codex-subagent: A-vs-A must NOT gate (non-vacuity) =="
run_json 0 "$w/ava.json" "A-vs-A (baseline vs baseline2, same rollouts) must NOT gate" -- \
  catacomb regress --runs-dir "$w/runs" \
  --baseline label:basket=prod-codex-subagent,variant=baseline \
  --candidate label:basket=prod-codex-subagent,variant=baseline2 --fail-on-notable --json
rc=0; python3 -c 'import json,sys; r=json.load(open(sys.argv[1])); sys.exit(0 if r["regressions"]==0 and not [f for f in r.get("findings", []) if f.get("verdict")=="notable"] else 1)' "$w/ava.json" || rc=$?
record "$rc" "A-vs-A reports zero regressions and no notable findings (the gate would pass at exit 0 without the plant)"
