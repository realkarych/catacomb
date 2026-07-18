#!/usr/bin/env bash
# Scenario 56 — codex bench: the `runtime: codex` basket SPAWN path end-to-end,
# entirely offline. Where 55 proves the codex reduce path via `catacomb import`,
# this scenario proves `catacomb bench` itself drives codex cells: the basket's
# task cmd is a fake codex CLI (fixtures/56-fake-codex.sh — bash/sed/date only,
# zero network) that prints a codex-exec event stream to stdout and drops a
# templated rollout into the staged sessions tree. What is asserted:
#   (1) the peek->resolve loop: bench reads the thread id from the
#       {"type":"thread.started","thread_id":...} stdout line and resolves
#       <sessions-dir>/YYYY/MM/DD/rollout-<ts>-<threadid>.jsonl for every cell —
#       "marked 6/6 cells" (2 variants x 3 reps), each cell a distinct thread id
#       derived from the fake's counter file. Rollout timestamps are stamped near
#       NOW by the fake (an improvement over 55's hardcoded past instants), so
#       event times and the bench-marker run window are coherent;
#   (2) stamps: every cell's meta.json env carries agent_runtime=codex and
#       agent_version from the rollout's cli_version, and — codex reports no
#       spend — cost_usd appears in NEITHER meta.json NOR the manifest (the
#       *float64 stays nil and omitempty drops the key);
#   (3) checkpoints: the task declares `checkpoints: [plan]` and the rollouts
#       carry the mcp__catacomb__mark pair, so the checkpoint summary reports
#       plan 6/6 and the exported graph has a "type":"marker" name=plan node;
#   (4) the epilogue: bench prints the regress next-step hint for the two
#       variants (printOfflineEpilogue runs for codex baskets too);
#   (5) the GATE: degraded rollouts differ by an error tool result ("Process
#       exited with code 3") and 3x tokens_out, so baseline-vs-degraded gates
#       (exit 1) on total tokens_out, total error_rate, and a phase-scope plan
#       tokens_out finding (the token_count event sits inside the plan marker
#       window). 3 reps per variant meet regress's default MinSupport of 3.
# A-vs-A non-vacuity over the same codex reduce path is already covered by 55;
# this scenario asserts the degraded gate only. Sourced by run.sh with lib.sh
# loaded and PROD/WORK/HERMETIC_* exported. Zero API spend.
set -euo pipefail
echo "== prod.56 codex-bench: bench a runtime:codex basket against a fake codex CLI =="
w="$WORK/codex-bench"; mkdir -p "$w/runs" "$w/sessions"
sed -e "s|__PROD__|$PROD|g" -e "s|__WORK__|$w|g" "$PROD/fixtures/56-codex.basket.yaml.tmpl" > "$w/basket.yaml"
run_json 0 "$w/bench.out" "bench prod-codex-bench basket (fake codex spawn, 2 variants x 3 reps)" -- \
  catacomb bench "$w/basket.yaml" --sessions-dir "$w/sessions" --runs-dir "$w/runs" --manifest "$w/m.jsonl"
rc=0; grep -q "marked 6/6 cells" "$w/bench.out" || rc=1
record "$rc" "bench marked 6/6 cells (thread.started peek -> day-dir rollout resolution, per-cell)"
rc=0; grep -q 'checkpoints\[probe\]: plan 6/6' "$w/bench.out" || rc=1
record "$rc" "checkpoint summary reports plan 6/6 (mark MCP pair honored in every rollout)"
rc=0; grep -q -- "--baseline label:basket=prod-codex-bench,variant=baseline --candidate label:basket=prod-codex-bench,variant=degraded" "$w/bench.out" || rc=1
record "$rc" "epilogue prints the regress next-step hint for baseline vs degraded"

echo "== prod.56 codex-bench: per-cell evidence stamps + manifest (agent_runtime=codex, no cost_usd) =="
rc=0; python3 - "$w/runs" <<'PY' || rc=$?
import json, os, sys
runs = sys.argv[1]
bad, ids = [], set()
for v in ("baseline", "degraded"):
    for r in (1, 2, 3):
        cell = "bench-prod-codex-bench-probe-%s-r%d" % (v, r)
        d = os.path.join(runs, cell)
        if not os.path.isfile(os.path.join(d, "session.jsonl")):
            bad.append("%s: session.jsonl missing" % cell)
            continue
        m = json.load(open(os.path.join(d, "meta.json")))
        env = m.get("env") or {}
        if env.get("agent_runtime") != "codex":
            bad.append("%s: agent_runtime=%r" % (cell, env.get("agent_runtime")))
        if env.get("agent_version") != "0.144.4":
            bad.append("%s: agent_version=%r" % (cell, env.get("agent_version")))
        if "cost_usd" in m:
            bad.append("%s: cost_usd present in meta.json" % cell)
        if m.get("task") != "probe" or m.get("variant") != v or m.get("rep") != r:
            bad.append("%s: cell labels wrong" % cell)
        ids.add(m.get("session_id"))
if len(ids) != 6:
    bad.append("expected 6 distinct thread ids, got %d: %s" % (len(ids), sorted(ids)))
if bad:
    print("\n".join(bad), file=sys.stderr)
    sys.exit(1)
PY
record "$rc" "all 6 cells stamp agent_runtime=codex + agent_version=0.144.4, omit cost_usd, and carry distinct thread ids"
rc=0; python3 - "$w/m.jsonl" <<'PY' || rc=$?
import json, sys
entries = [json.loads(l) for l in open(sys.argv[1]) if l.strip()]
bad = []
if len(entries) != 6:
    bad.append("expected 6 manifest entries, got %d" % len(entries))
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
    if e.get("missing_checkpoints"):
        bad.append("%s: missing_checkpoints=%r" % (rid, e.get("missing_checkpoints")))
if bad:
    print("\n".join(bad), file=sys.stderr)
    sys.exit(1)
PY
record "$rc" "manifest has 6 marked entries with thread-id session ids, exit 0, no missing checkpoints, no cost_usd"

echo "== prod.56 codex-bench: export a benched cell -> plan marker node (codex meta-stamp dispatch) =="
rundir="$w/runs/bench-prod-codex-bench-probe-baseline-r1"
snap="$w/export.snap.jsonl"
run_json 0 "$w/export.out" "export baseline-r1 evidence dir -> jsonl graph snapshot" -- \
  catacomb export "$rundir" --out "$snap"
rc=0; grep -Eq '"type":"marker"[^}]*"name":"plan"|"name":"plan"[^}]*"type":"marker"' "$snap" || rc=1
record "$rc" "mcp__catacomb__mark pair became a \"type\":\"marker\" name=plan checkpoint node"

echo "== prod.56 codex-bench: degraded (error result + 3x tokens_out) -> regress GATES (exit 1) =="
run_json 1 "$w/regress.json" "baseline vs degraded -> tokens_out + error_rate gate" -- \
  catacomb regress --runs-dir "$w/runs" \
  --baseline label:basket=prod-codex-bench,variant=baseline \
  --candidate label:basket=prod-codex-bench,variant=degraded --json
rc=0; python3 - "$w/regress.json" <<'PY' || rc=$?
import json, sys
rep = json.load(open(sys.argv[1]))
f = rep.get("findings", [])
def has(scope, metric, name=None):
    return any(x.get("scope") == scope and x.get("metric") == metric
               and (name is None or x.get("name") == name)
               and x.get("verdict") == "regression" for x in f)
# tokens_out 100 -> 300 (the 3x token_count plant) and error_rate 0 -> 1 (the
# "Process exited with code 3" tool result) gate at total scope; the plan
# checkpoint surfaces as its own phase-scope tokens_out regression because the
# token_count event sits inside the plan marker window.
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

echo "== prod.56 codex-bench: Task-15 Part-A offline transforms over the benched codex evidence (\$0) =="
# The SAME offline transforms run.sh's codex leg appends over LIVE codex evidence (Task 15
# Part A: pack/calibrate/regress-flags), here re-run over the fabricated codex runs-dir this
# scenario already benched — the $0 hermetic mirror proving the offline pipeline is
# runtime-agnostic on codex evidence. render/parse + structural facts only, the same idioms
# the live blocks use (run.sh v5b/v5c pack v6a calibrate v6b).
run_json 0 "$w/pack.out" "pack codex baseline evidence --sample 2 (bundle shape)" -- \
  catacomb pack label:basket=prod-codex-bench,variant=baseline --runs-dir "$w/runs" --out "$w/pack" --sample 2
rc=0
grep -Fqx "packed 2 of 3 runs into $w/pack" "$w/pack.out" || rc=1
[ -f "$w/pack/pack.json" ] || rc=1
[ -s "$w/pack/INSTRUCTIONS.md" ] || rc=1
pdirs=$(find "$w/pack" -mindepth 1 -maxdepth 1 -type d | wc -l | tr -d ' ')
[ "$pdirs" -eq 2 ] || rc=1
record "$rc" "pack over codex evidence: packed 2 of 3, pack.json + INSTRUCTIONS.md + 2 sampled run dirs"

rc=0
catacomb calibrate --runs-dir "$w/runs" --group label:basket=prod-codex-bench,variant=baseline --format json >"$w/cal.json" 2>/dev/null || rc=$?
rc=0
python3 -c 'import json,sys; r=json.load(open(sys.argv[1])); sys.exit(0 if r.get("runs")==3 and r.get("sufficient") is False else 1)' "$w/cal.json" || rc=1
record "$rc" "calibrate over codex evidence: runs=3 sufficient=false at the default self-check floor"
rc=0
catacomb calibrate --runs-dir "$w/runs" --group label:basket=prod-codex-bench,variant=baseline --format json --min-support 1 >"$w/cal1.json" 2>/dev/null || rc=$?
rc=0
python3 -c 'import json,sys; r=json.load(open(sys.argv[1])); sys.exit(0 if r.get("min_support")==1 and r.get("sufficient") is True and r.get("split") else 1)' "$w/cal1.json" || rc=1
record "$rc" "calibrate --min-support 1: threshold passthrough flips sufficient=true with a populated split (codex evidence)"

catacomb regress --runs-dir "$w/runs" --baseline label:basket=prod-codex-bench,variant=baseline --candidate label:basket=prod-codex-bench,variant=degraded --format markdown >"$w/regress.md" 2>/dev/null || true
rc=0
grep -q '^\*\*Verdict: ' "$w/regress.md" || rc=1
grep -q '^| Verdict | Scope | Key | Name | Metric | Baseline | Candidate | Band | Detail |$' "$w/regress.md" || rc=1
record "$rc" "regress --format markdown over codex evidence renders the bold Verdict header + findings-table header"

run_json 1 "$w/regress-ms.json" "regress --min-support 6 --strict over codex evidence -> insufficient (exit 1)" -- \
  catacomb regress --runs-dir "$w/runs" --baseline label:basket=prod-codex-bench,variant=baseline --candidate label:basket=prod-codex-bench,variant=degraded --min-support 6 --strict --json
rc=0
python3 -c 'import json,sys; r=json.load(open(sys.argv[1])); sys.exit(0 if r["overall_verdict"]=="insufficient" else 1)' "$w/regress-ms.json" || rc=1
record "$rc" "regress --min-support 6 --strict flips codex baseline-vs-degraded to overall_verdict=insufficient (exit 1)"
