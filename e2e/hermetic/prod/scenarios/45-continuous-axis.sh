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
