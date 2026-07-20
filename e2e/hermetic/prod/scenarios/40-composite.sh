#!/usr/bin/env bash
# Scenario 40 — composite production session: a dispatched subagent marks the `work`
# phase (mcp__catacomb__mark start/end), invokes the Skill tool, calls the
# mcp__e2ekit__record MCP tool, and produces a verifiable artifact (out/result.csv),
# all in one fixture session. degraded strips every feature — no subagent, skill, mark,
# or MCP, just an inline Bash step — and emits the WRONG artifact, so the gate fires on
# three axes at once:
#   - STEP: the dropped Task/Skill/mcp step nodes. With no shared step keys, step
#     alignment coverage falls below the floor, so these presence regressions downgrade
#     to notable (scope=step, metric=presence, verdict=notable).
#   - PHASE: the subagent-scoped `work` marker present 5/5 in baseline, 0/5 in degraded
#     (scope=phase, name=work, metric=presence, verdict=regression). The basket task
#     declares `checkpoints: [work]`.
#   - ANNOTATION: the verify hook scores out/result.csv into verifier.pass — 5/5 in
#     baseline (CATACOMB-OK), 0/5 in degraded (WRONG) (metric=ann:verifier.pass,
#     verdict=regression).
# Both the phase and annotation drops are regressions, so the composite comparison
# gates under a plain `regress --json` (exit 1) without --fail-on-notable. The baseline
# also reduces three DISTINCT phase keys, exercising every axis of phasekey.Compute:
# a top-level `orchestration` phase (distinct name), the subagent-scoped `work` phase
# marked twice (occurrence 0 and 1), and its subagent-enclosing step key — asserted
# directly against the reduced snapshot. A-vs-A
# (baseline vs baseline2) stays clean. PYTHONPATH carries the verifier SDK so the verify
# hook can import catacomb_verifier. Sourced by run.sh with lib.sh loaded and
# PROD/WORK/HERMETIC_* exported. Zero API spend.
set -euo pipefail
echo "== prod.40 composite: subagent+skill+mcp+verifier all gate; A-vs-A clean =="
w="$WORK/composite"; mkdir -p "$w/cellwork" "$w/runs"
export PYTHONPATH="$REPO/integrations/verifier/src${PYTHONPATH:+:$PYTHONPATH}"
sed -e "s|__PROD__|$PROD|g" -e "s|__WORK__|$w|g" "$PROD/fixtures/composite.basket.yaml.tmpl" > "$w/basket.yaml"
HERMETIC_PROJECTS="$w/projects" run_json 0 "$w/bench.out" "bench prod-composite basket (3 variants x 5 reps)" -- \
  catacomb bench "$w/basket.yaml" --projects-dir "$w/projects" --runs-dir "$w/runs" --manifest "$w/m.jsonl"

echo "== prod.40 composite: degraded strips features + wrong artifact -> multi-axis gate =="
run_json 1 "$w/regress.json" "degraded strips features + wrong artifact -> gate" -- \
  catacomb regress --runs-dir "$w/runs" \
  --baseline label:basket=prod-composite,variant=baseline \
  --candidate label:basket=prod-composite,variant=degraded --json
rc=0; python3 - "$w/regress.json" <<'PY' || rc=$?
import json, sys
rep = json.load(open(sys.argv[1]))
f = rep.get("findings", [])
step = any(x.get("scope") == "step" and x.get("metric") == "presence" and x.get("verdict") == "notable" for x in f)
ann = any(x.get("metric") == "ann:verifier.pass" and x.get("verdict") == "regression" for x in f)
if not (step and ann):
    print("missing axis: step=%s ann=%s" % (step, ann), file=sys.stderr)
    for x in f: print("  ", {k: x.get(k) for k in ("scope", "name", "metric", "verdict", "detail")}, file=sys.stderr)
    sys.exit(1)
print("composite gate fires on STEP (notable) and ann:verifier.pass (regression) together")
PY
record "$rc" "composite gates on STEP (dropped nodes, notable) AND ann:verifier.pass (regression)"

echo "== prod.40 composite: subagent-scoped work phase dropped -> PHASE regression =="
run_json 1 "$w/phase.json" "checkpoint presence: work phase dropped -> PHASE gate" -- \
  catacomb regress --runs-dir "$w/runs" \
  --baseline label:basket=prod-composite,variant=baseline \
  --candidate label:basket=prod-composite,variant=degraded --json
rc=0; python3 - "$w/phase.json" <<'PY' || rc=$?
import json, sys
rep = json.load(open(sys.argv[1]))
hits = [x for x in rep.get("findings", [])
        if x.get("scope") == "phase" and x.get("name") == "work"
        and x.get("metric") == "presence" and x.get("verdict") == "regression"]
if not hits:
    print("no phase-scope work presence/regression finding; findings:", file=sys.stderr)
    for x in rep.get("findings", []):
        print("  ", {k: x.get(k) for k in ("scope", "name", "metric", "verdict", "detail")}, file=sys.stderr)
    sys.exit(1)
print("phase-scope regression present (work marker dropped in degraded)")
PY
record "$rc" "regress attributes a PHASE-scope regression to the dropped work marker"

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

echo "== prod.40 composite: A-vs-A must NOT gate =="
run_json 0 "$w/ava.json" "A-vs-A must NOT gate" -- \
  catacomb regress --runs-dir "$w/runs" \
  --baseline label:basket=prod-composite,variant=baseline \
  --candidate label:basket=prod-composite,variant=baseline2 --metric-rel-delta "$PROD_AVA_METRIC_BAND" --json
rc=0; python3 -c 'import json,sys; r=json.load(open(sys.argv[1])); sys.exit(0 if r["regressions"]==0 and not [f for f in r.get("findings", []) if f.get("verdict")=="notable"] else 1)' "$w/ava.json" || rc=$?
record "$rc" "A-vs-A reports zero regressions and no notable findings"
