#!/usr/bin/env bash
# Scenario 57 — codex MCP: the `runtime: codex` MCP basket end-to-end, entirely
# offline — the hermetic mirror for e2e/basket-codex-mcp.yaml (the live leg is
# wired into run.sh by a later task; this scenario stands alone). Where 56
# proves the codex bench SPAWN path via a plan marker, this scenario proves the
# same spawn path carries a real MCP tool call: the basket task cmd is
# fixtures/57-fake-codex-mcp.sh (a variant of 56-fake-codex.sh — bash/sed/date
# only, zero network) that renders a codex rollout carrying
# mcp_tool_call_begin/end on {server:e2ekit,tool:record} and writes
# out/result.csv the way the real e2ekit MCP server would (there is no live MCP
# handshake to drive offline — the fake CLI stands in for the whole
# codex-plus-server round trip, not just the CLI binary). What is asserted:
#   (1) the peek->resolve loop marks all 9 cells (3 variants x 3 reps) and the
#       verify hook actually runs through bench end to end: "verify[probe]:
#       pass 6/9" (baseline + baseline2 record the token and produce the
#       artifact; degraded does neither);
#   (2) stamps: every cell's meta.json env carries agent_runtime=codex and
#       cost_usd appears in neither meta.json nor the manifest (codex reports
#       no spend) — the identical contract 56 already proves for the bare
#       spawn path;
#   (3) the exported baseline graph carries a "type":"mcp_call" node (the
#       generic mcp__server__tool step path ingest/codex/codex.go maps from
#       mcp_tool_call_begin/end — the same convention already proven for
#       Claude in 10-mcp-protocol.sh); the degraded graph carries none;
#   (4) the GATE: baseline-vs-degraded regress (plain `--json`, no
#       `--fail-on-notable` — the ann:verifier.pass regression alone is enough
#       to gate) fires exit 1 with BOTH a STEP-scope presence/notable finding
#       (the dropped mcp__e2ekit__record node — a single-step-key comparison
#       downgrades to notable, same as 10-mcp-protocol.sh's claude-side twin)
#       AND an ann:verifier.pass regression (the artifact drop);
#   (5) NON-VACUITY: baseline-vs-baseline2 (A-vs-A — same rollouts, same
#       artifact) does NOT gate: exit 0, zero regressions, zero notables — so
#       (4) fires on the planted degradation, not on codex-MCP-import noise.
# 3 reps per variant meets regress's default MinSupport of 3, matching
# basket-codex-mcp.yaml's own live rep count. Sourced by run.sh with lib.sh
# loaded and PROD/WORK/HERMETIC_* exported. Zero API spend.
set -euo pipefail
echo "== prod.57 codex-mcp: bench a runtime:codex MCP basket against a fake codex CLI =="
w="$WORK/codex-mcp"; mkdir -p "$w/runs" "$w/sessions" "$w/cellwork"
export PYTHONPATH="$REPO/integrations/verifier/src${PYTHONPATH:+:$PYTHONPATH}"
sed -e "s|__PROD__|$PROD|g" -e "s|__WORK__|$w|g" -e "s|__REPO__|$REPO|g" \
  "$PROD/fixtures/57-codex-mcp.basket.yaml.tmpl" > "$w/basket.yaml"
run_json 0 "$w/bench.out" "bench prod-codex-mcp basket (fake codex spawn, 3 variants x 3 reps)" -- \
  catacomb bench "$w/basket.yaml" --sessions-dir "$w/sessions" --runs-dir "$w/runs" --manifest "$w/m.jsonl"
rc=0; grep -q "marked 9/9 cells" "$w/bench.out" || rc=1
record "$rc" "bench marked 9/9 cells (thread.started peek -> day-dir rollout resolution, per-cell)"
rc=0; grep -q "verify\[probe\]: pass 6/9" "$w/bench.out" || rc=1
record "$rc" "verify hook ran through bench end to end: 6/9 pass (baseline+baseline2 record the token, degraded doesn't)"
rc=0; grep -q -- "--baseline label:basket=prod-codex-mcp,variant=baseline --candidate label:basket=prod-codex-mcp,variant=degraded" "$w/bench.out" || rc=1
record "$rc" "epilogue prints the regress next-step hint for baseline vs degraded"

echo "== prod.57 codex-mcp: per-cell evidence stamps + manifest (agent_runtime=codex, no cost_usd) =="
rc=0; python3 - "$w/runs" <<'PY' || rc=$?
import json, os, sys
runs = sys.argv[1]
bad, ids = [], set()
for v in ("baseline", "degraded", "baseline2"):
    for r in (1, 2, 3):
        cell = "bench-prod-codex-mcp-probe-%s-r%d" % (v, r)
        d = os.path.join(runs, cell)
        if not os.path.isfile(os.path.join(d, "session.jsonl")):
            bad.append("%s: session.jsonl missing" % cell)
            continue
        m = json.load(open(os.path.join(d, "meta.json")))
        env = m.get("env") or {}
        if env.get("agent_runtime") != "codex":
            bad.append("%s: agent_runtime=%r" % (cell, env.get("agent_runtime")))
        if "cost_usd" in m:
            bad.append("%s: cost_usd present in meta.json" % cell)
        if m.get("task") != "probe" or m.get("variant") != v or m.get("rep") != r:
            bad.append("%s: cell labels wrong" % cell)
        ids.add(m.get("session_id"))
if len(ids) != 9:
    bad.append("expected 9 distinct thread ids, got %d: %s" % (len(ids), sorted(ids)))
if bad:
    print("\n".join(bad), file=sys.stderr)
    sys.exit(1)
PY
record "$rc" "all 9 cells stamp agent_runtime=codex, omit cost_usd, carry distinct thread ids and correct labels"
rc=0; python3 - "$w/m.jsonl" <<'PY' || rc=$?
import json, sys
entries = [json.loads(l) for l in open(sys.argv[1]) if l.strip()]
bad = []
if len(entries) != 9:
    bad.append("expected 9 manifest entries, got %d" % len(entries))
for e in entries:
    rid = e.get("run_id", "?")
    if "cost_usd" in e:
        bad.append("%s: cost_usd present in manifest (want absent for codex)" % rid)
    if not e.get("marked"):
        bad.append("%s: not marked" % rid)
    if e.get("exit_code") != 0:
        bad.append("%s: exit_code=%r" % (rid, e.get("exit_code")))
    if not e.get("session_id"):
        bad.append("%s: no session_id" % rid)
if bad:
    print("\n".join(bad), file=sys.stderr)
    sys.exit(1)
PY
record "$rc" "manifest has 9 marked entries with thread-id session ids, exit 0, no cost_usd"

echo "== prod.57 codex-mcp: mcp_call graph node present in baseline, absent in degraded =="
base_snap="$w/baseline.snap.jsonl"; deg_snap="$w/degraded.snap.jsonl"
run_json 0 "$w/export-base.out" "export baseline-r1 evidence dir -> jsonl graph snapshot" -- \
  catacomb export "$w/runs/bench-prod-codex-mcp-probe-baseline-r1" --out "$base_snap"
run_json 0 "$w/export-deg.out" "export degraded-r1 evidence dir -> jsonl graph snapshot" -- \
  catacomb export "$w/runs/bench-prod-codex-mcp-probe-degraded-r1" --out "$deg_snap"
rc=0; grep -q '"type":"mcp_call"' "$base_snap" || rc=1
record "$rc" "baseline graph snapshot contains a \"type\":\"mcp_call\" node (mcp__e2ekit__record over codex)"
rc=0; if grep -q '"type":"mcp_call"' "$deg_snap"; then rc=1; fi
record "$rc" "degraded graph snapshot contains no \"type\":\"mcp_call\" node"

echo "== prod.57 codex-mcp: degraded (no tool + no artifact) -> regress GATES (exit 1) =="
run_json 1 "$w/regress.json" "baseline vs degraded -> STEP presence + ann:verifier.pass gate" -- \
  catacomb regress --runs-dir "$w/runs" \
  --baseline label:basket=prod-codex-mcp,variant=baseline \
  --candidate label:basket=prod-codex-mcp,variant=degraded --json
rc=0; python3 - "$w/regress.json" <<'PY' || rc=$?
import json, sys
rep = json.load(open(sys.argv[1]))
f = rep.get("findings", [])
step = any(x.get("scope") == "step" and x.get("metric") == "presence" and x.get("verdict") == "notable" for x in f)
ann = any(x.get("metric") == "ann:verifier.pass" and x.get("verdict") == "regression" for x in f)
if not (step and ann):
    print("missing axis: step=%s ann=%s" % (step, ann), file=sys.stderr)
    for x in f:
        print("  ", {k: x.get(k) for k in ("scope", "name", "metric", "verdict", "detail")}, file=sys.stderr)
    sys.exit(1)
print("gate fires on STEP (dropped mcp__e2ekit__record node, notable) and ann:verifier.pass (regression) together")
PY
record "$rc" "regress gates (exit 1) on the dropped mcp__e2ekit__record STEP node AND the ann:verifier.pass drop"

echo "== prod.57 codex-mcp: A-vs-A must NOT gate (non-vacuity) =="
run_json 0 "$w/ava.json" "A-vs-A (baseline vs baseline2, same rollouts + artifact) must NOT gate" -- \
  catacomb regress --runs-dir "$w/runs" \
  --baseline label:basket=prod-codex-mcp,variant=baseline \
  --candidate label:basket=prod-codex-mcp,variant=baseline2 --metric-rel-delta "$PROD_AVA_METRIC_BAND" --json
rc=0; python3 -c 'import json,sys; r=json.load(open(sys.argv[1])); sys.exit(0 if r["regressions"]==0 and not [f for f in r.get("findings", []) if f.get("verdict")=="notable"] else 1)' "$w/ava.json" || rc=$?
record "$rc" "A-vs-A reports zero regressions and no notable findings (the gate would pass at exit 0 without the plant)"
