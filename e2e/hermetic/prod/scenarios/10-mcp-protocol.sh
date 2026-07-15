#!/usr/bin/env bash
# Scenario 10 — real e2e MCP server: (a) protocol-conformance smoke of server.py,
# and (b) the generic mcp__server__tool step node gates. Sourced by run.sh with
# lib.sh already loaded and PROD/WORK/HERMETIC_* exported.
set -euo pipefail
echo "== prod.10 mcp: protocol smoke of e2e/mcp-e2ekit/server.py =="
rc=0; python3 "$REPO/e2e/mcp-e2ekit/smoke.py" || rc=$?
record "$rc" "e2e MCP server passes JSON-RPC protocol conformance"

echo "== prod.10 mcp: generic mcp step node present in baseline, absent in degraded =="
w="$WORK/mcp"; mkdir -p "$w/cellwork" "$w/runs"
sed -e "s|__PROD__|$PROD|g" -e "s|__WORK__|$w|g" "$PROD/fixtures/mcp.basket.yaml.tmpl" > "$w/basket.yaml"
HERMETIC_PROJECTS="$w/projects" run_json 0 "$w/bench.out" "bench prod-mcp basket" -- \
  catacomb bench "$w/basket.yaml" --projects-dir "$w/projects" --runs-dir "$w/runs" --manifest "$w/m.jsonl"
run_json 1 "$w/regress.json" "degraded drops mcp step node -> STEP notable gate" -- \
  catacomb regress --runs-dir "$w/runs" \
  --baseline label:basket=prod-mcp,variant=baseline \
  --candidate label:basket=prod-mcp,variant=degraded --fail-on-notable --json
rc=0; python3 - "$w/regress.json" <<'PY' || rc=$?
import json, sys
rep = json.load(open(sys.argv[1]))
hits = [f for f in rep.get("findings", []) if f.get("scope") == "step" and f.get("metric") == "presence" and f.get("verdict") == "notable"]
if not hits:
    print("no step-scope presence/notable finding; findings:", file=sys.stderr)
    for f in rep.get("findings", []):
        print("  ", {k: f.get(k) for k in ("scope", "name", "metric", "verdict", "detail")}, file=sys.stderr)
    sys.exit(1)
print("step-scope notable finding present (mcp__e2ekit__record node dropped)")
PY
record "$rc" "regress attributes a STEP-scope notable finding to the dropped mcp node"
run_json 0 "$w/ava.json" "A-vs-A must NOT gate" -- \
  catacomb regress --runs-dir "$w/runs" \
  --baseline label:basket=prod-mcp,variant=baseline \
  --candidate label:basket=prod-mcp,variant=baseline2 --fail-on-notable --metric-rel-delta 0.5 --json
rc=0; python3 - "$w/ava.json" <<'PY' || rc=$?
import json, sys
r = json.load(open(sys.argv[1]))
notable = [f for f in r.get("findings", []) if f.get("verdict") == "notable"]
sys.exit(0 if r["regressions"] == 0 and not notable else 1)
PY
record "$rc" "A-vs-A reports zero regressions and no notable findings"
