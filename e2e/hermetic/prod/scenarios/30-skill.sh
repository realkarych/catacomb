#!/usr/bin/env bash
# Scenario 30 — skill invocation presence: baseline invokes the Skill tool (a Skill
# step node plus a synthesized "skill" graph node); degraded does the work inline via
# a Write (neither node). The dropped Skill step node gates as a STEP-scope presence
# notable finding under --fail-on-notable; the graph snapshot then proves the
# "type":"skill" node appears only in baseline. Sourced by run.sh with lib.sh
# already loaded and PROD/WORK/HERMETIC_* exported. Zero API spend.
set -euo pipefail
echo "== prod.30 skill: bench baseline/degraded/baseline2 (3 variants x 5 reps) =="
w="$WORK/skill"; mkdir -p "$w/cellwork" "$w/runs"
sed -e "s|__PROD__|$PROD|g" -e "s|__WORK__|$w|g" "$PROD/fixtures/skill.basket.yaml.tmpl" > "$w/basket.yaml"
HERMETIC_PROJECTS="$w/projects" run_json 0 "$w/bench.out" "bench prod-skill basket" -- \
  catacomb bench "$w/basket.yaml" --projects-dir "$w/projects" --runs-dir "$w/runs" --manifest "$w/m.jsonl"

echo "== prod.30 skill: degraded drops the Skill step node -> STEP notable gate =="
run_json 1 "$w/regress.json" "degraded drops Skill step node -> STEP notable gate" -- \
  catacomb regress --runs-dir "$w/runs" \
  --baseline label:basket=prod-skill,variant=baseline \
  --candidate label:basket=prod-skill,variant=degraded --fail-on-notable --json
rc=0; python3 - "$w/regress.json" <<'PY' || rc=$?
import json, sys
rep = json.load(open(sys.argv[1]))
hits = [f for f in rep.get("findings", []) if f.get("scope") == "step" and f.get("metric") == "presence" and f.get("verdict") == "notable"]
if not hits:
    print("no step-scope presence/notable finding; findings:", file=sys.stderr)
    for f in rep.get("findings", []):
        print("  ", {k: f.get(k) for k in ("scope", "name", "metric", "verdict", "detail")}, file=sys.stderr)
    sys.exit(1)
print("step-scope notable finding present (Skill node dropped)")
PY
record "$rc" "regress attributes a STEP-scope notable finding to the dropped Skill node"

echo "== prod.30 skill: skill graph node present in baseline, absent in degraded =="
base_snap="$w/baseline.snap.jsonl"; deg_snap="$w/degraded.snap.jsonl"
run_json 0 "$w/replay-base.out" "replay baseline session -> export jsonl snapshot" -- \
  catacomb replay "$w/runs/bench-prod-skill-skill-baseline-r1/session.jsonl" --export-jsonl "$base_snap"
run_json 0 "$w/replay-deg.out" "replay degraded session -> export jsonl snapshot" -- \
  catacomb replay "$w/runs/bench-prod-skill-skill-degraded-r1/session.jsonl" --export-jsonl "$deg_snap"
rc=0; grep -q '"type":"skill"' "$base_snap" || rc=1
record "$rc" "baseline graph snapshot contains a \"type\":\"skill\" node"
rc=0; if grep -q '"type":"skill"' "$deg_snap"; then rc=1; fi
record "$rc" "degraded graph snapshot contains no \"type\":\"skill\" node"

echo "== prod.30 skill: A-vs-A must NOT gate =="
run_json 0 "$w/ava.json" "A-vs-A must NOT gate" -- \
  catacomb regress --runs-dir "$w/runs" \
  --baseline label:basket=prod-skill,variant=baseline \
  --candidate label:basket=prod-skill,variant=baseline2 --fail-on-notable --metric-rel-delta "$PROD_AVA_METRIC_BAND" --json
rc=0; python3 - "$w/ava.json" <<'PY' || rc=$?
import json, sys
r = json.load(open(sys.argv[1]))
notable = [f for f in r.get("findings", []) if f.get("verdict") == "notable"]
sys.exit(0 if r["regressions"] == 0 and not notable else 1)
PY
record "$rc" "A-vs-A reports zero regressions and no notable findings"
