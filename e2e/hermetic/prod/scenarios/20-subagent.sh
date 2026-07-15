#!/usr/bin/env bash
# Scenario 20 — subagent presence: baseline delegates via the Task tool (a Task
# step node plus a synthesized "subagent" graph node); degraded does the work inline
# via Bash (neither node). The dropped Task step node gates as a STEP-scope presence
# notable finding under --fail-on-notable; the graph snapshot then proves the
# "type":"subagent" node appears only in baseline. Sourced by run.sh with lib.sh
# already loaded and PROD/WORK/HERMETIC_* exported. Zero API spend.
set -euo pipefail
echo "== prod.20 subagent: bench baseline/degraded/baseline2 (3 variants x 5 reps) =="
w="$WORK/subagent"; mkdir -p "$w/cellwork" "$w/runs"
sed -e "s|__PROD__|$PROD|g" -e "s|__WORK__|$w|g" "$PROD/fixtures/subagent.basket.yaml.tmpl" > "$w/basket.yaml"
HERMETIC_PROJECTS="$w/projects" run_json 0 "$w/bench.out" "bench prod-subagent basket" -- \
  catacomb bench "$w/basket.yaml" --projects-dir "$w/projects" --runs-dir "$w/runs" --manifest "$w/m.jsonl"

echo "== prod.20 subagent: degraded drops the Task step node -> STEP notable gate =="
run_json 1 "$w/regress.json" "degraded drops Task step node -> STEP notable gate" -- \
  catacomb regress --runs-dir "$w/runs" \
  --baseline label:basket=prod-subagent,variant=baseline \
  --candidate label:basket=prod-subagent,variant=degraded --fail-on-notable --json
rc=0; python3 - "$w/regress.json" <<'PY' || rc=$?
import json, sys
rep = json.load(open(sys.argv[1]))
hits = [f for f in rep.get("findings", []) if f.get("scope") == "step" and f.get("metric") == "presence" and f.get("verdict") == "notable"]
if not hits:
    print("no step-scope presence/notable finding; findings:", file=sys.stderr)
    for f in rep.get("findings", []):
        print("  ", {k: f.get(k) for k in ("scope", "name", "metric", "verdict", "detail")}, file=sys.stderr)
    sys.exit(1)
print("step-scope notable finding present (Task node dropped)")
PY
record "$rc" "regress attributes a STEP-scope notable finding to the dropped Task node"

echo "== prod.20 subagent: subagent graph node present in baseline, absent in degraded =="
base_snap="$w/baseline.snap.jsonl"; deg_snap="$w/degraded.snap.jsonl"
run_json 0 "$w/replay-base.out" "replay baseline session -> export jsonl snapshot" -- \
  catacomb replay "$w/runs/bench-prod-subagent-subagent-baseline-r1/session.jsonl" --export-jsonl "$base_snap"
run_json 0 "$w/replay-deg.out" "replay degraded session -> export jsonl snapshot" -- \
  catacomb replay "$w/runs/bench-prod-subagent-subagent-degraded-r1/session.jsonl" --export-jsonl "$deg_snap"
rc=0; grep -q '"type":"subagent"' "$base_snap" || rc=1
record "$rc" "baseline graph snapshot contains a \"type\":\"subagent\" node"
rc=0; if grep -q '"type":"subagent"' "$deg_snap"; then rc=1; fi
record "$rc" "degraded graph snapshot contains no \"type\":\"subagent\" node"

echo "== prod.20 subagent: A-vs-A must NOT gate =="
run_json 0 "$w/ava.json" "A-vs-A must NOT gate" -- \
  catacomb regress --runs-dir "$w/runs" \
  --baseline label:basket=prod-subagent,variant=baseline \
  --candidate label:basket=prod-subagent,variant=baseline2 --fail-on-notable --metric-rel-delta 0.5 --json
rc=0; python3 - "$w/ava.json" <<'PY' || rc=$?
import json, sys
r = json.load(open(sys.argv[1]))
notable = [f for f in r.get("findings", []) if f.get("verdict") == "notable"]
sys.exit(0 if r["regressions"] == 0 and not notable else 1)
PY
record "$rc" "A-vs-A reports zero regressions and no notable findings"
