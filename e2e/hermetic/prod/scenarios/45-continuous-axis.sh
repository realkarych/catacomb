#!/usr/bin/env bash
# Scenario 45 — second continuous axis (nodes): a transcript that makes MANY tool
# calls trips the continuous gate on `nodes` (graph node count) while an A-vs-A (base
# vs base) over the same few-node transcript does not. `nodes` is a STRUCTURAL axis —
# more tool_use step nodes in the reduced graph — independent of billing mode and
# distinct from tokens_out. The deterministic hard-mirror of the live e2e-nodes basket
# (e2e/run.sh steps u2-u4): base follows a 1-link chain (one Read tool_use -> few
# nodes), big follows a 6-link chain (six Read tool_use -> many more nodes), and the two
# fixtures differ ONLY in the number of tool_use step nodes. Fabricated evidence, zero
# API spend.
set -euo pipefail
echo "== prod.45 nodes: many-tool-call transcript gates the nodes axis =="
w="$WORK/nodes"; mkdir -p "$w/cellwork" "$w/runs"
cat > "$w/basket.yaml" <<YAML
basket: prod-nodes
reps: 5
tasks:
  - id: t
    cmd: ["$PROD/fixtures/emit.sh"]
    dir: $w/cellwork
variants:
  - id: baseline
    env: { SCENARIO_TMPL: "$PROD/fixtures/45-nodes-base.jsonl.tmpl" }
  - id: big
    env: { SCENARIO_TMPL: "$PROD/fixtures/45-nodes-big.jsonl.tmpl" }
  - id: baseline2
    env: { SCENARIO_TMPL: "$PROD/fixtures/45-nodes-base.jsonl.tmpl" }
YAML
HERMETIC_PROJECTS="$w/projects" run_json 0 "$w/bench.out" "bench prod-nodes basket" -- \
  catacomb bench "$w/basket.yaml" --projects-dir "$w/projects" --runs-dir "$w/runs" --manifest "$w/m.jsonl"

run_json 1 "$w/seeded.json" "many tool calls -> nodes gate" -- \
  catacomb regress --runs-dir "$w/runs" \
  --baseline label:basket=prod-nodes,variant=baseline \
  --candidate label:basket=prod-nodes,variant=big --json
rc=0; python3 - "$w/seeded.json" <<'PY' || rc=$?
import json, sys
rep = json.load(open(sys.argv[1]))
hit = [f for f in rep.get("findings", []) if f.get("metric") == "nodes" and f.get("verdict") in ("regression", "notable")]
if not hit:
    print("no nodes finding; findings:", file=sys.stderr)
    for f in rep.get("findings", []): print("  ", {k: f.get(k) for k in ("scope","metric","verdict")}, file=sys.stderr)
    sys.exit(1)
print("nodes gate fires deterministically")
PY
record "$rc" "prod.45 nodes seeded regression gates on the nodes axis"

run_json 0 "$w/ava.json" "prod.45 nodes A-vs-A must NOT gate" -- \
  catacomb regress --runs-dir "$w/runs" \
  --baseline label:basket=prod-nodes,variant=baseline \
  --candidate label:basket=prod-nodes,variant=baseline2 --metric-rel-delta "$PROD_AVA_METRIC_BAND" --json
rc=0; python3 -c 'import json,sys; r=json.load(open(sys.argv[1])); sys.exit(0 if r["regressions"]==0 else 1)' "$w/ava.json" || rc=$?
record "$rc" "prod.45 nodes A-vs-A reports zero regressions"
