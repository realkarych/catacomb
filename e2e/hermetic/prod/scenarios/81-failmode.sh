#!/usr/bin/env bash
# Scenario 81 — hermetic mirror of the live e2e/run.sh failure-mode basket
# (e2e/basket-failmode.yaml, e2e/failmode.sh): --fail-fast ($0, no model call)
# and --error-delta (a real Bash-tool error reduced to a "status":"error"
# tool_call node) coverage, at zero API spend.
#
#   (1) --fail-fast, against the REAL e2e/failmode.sh (not a stand-in): a
#       1-cell fabricated basket points its task cmd directly at the repo's
#       own e2e/failmode.sh with FAILMODE=prefail, the branch that exits 1
#       BEFORE any claude invocation. `bench ... --fail-fast` MUST stop after
#       that one cell: exit 1, a "fail-fast" stderr line, and exactly ONE
#       manifest entry recording exit_code 1. Non-vacuity: the SAME basket
#       benched WITHOUT --fail-fast still records that cell as exit_code 1 in
#       its manifest, but the overall bench invocation exits 0 — proving the
#       nonzero exit above is caused by --fail-fast, not merely by the cell
#       failing (bench does not otherwise propagate a single cell's failure
#       into its own exit code).
#
#   (2) --error-delta, over a fabricated toolerr-vs-clean rollout pair —
#       modeled on 95-reduce-edges.sh's StatusError mkcell pattern (the
#       reducer path this basket's live "clean"/"errseed" variants exercise
#       for real via claude -p): 3 "clean" cells whose sole tool_result carries
#       is_error:false and 3 "errseed" cells whose sole tool_result carries
#       is_error:true, otherwise identical (unpriced model, no usage, fixed
#       5s window) so total error_rate is the only axis that can move.
#       `regress --baseline variant=clean --candidate variant=errseed
#       --error-delta 0.5 --json` MUST gate (exit 1) on total error_rate
#       0 -> 1. Non-vacuity: a clean-vs-clean control over the SAME "clean"
#       cells must NOT gate (exit 0, error_rate stays 0).
set -euo pipefail

w="$WORK/failmode"; rm -rf "$w"; mkdir -p "$w"

echo "== prod.81 --fail-fast: the REAL e2e/failmode.sh (FAILMODE=prefail) stops bench after 1 cell, \$0 =="
cat >"$w/basket-failfast.yaml" <<EOF
basket: e2e-failmode-hermetic
reps: 1
tasks:
  - id: failmode
    cmd: ["$REPO/e2e/failmode.sh"]
    timeout: 10s
variants:
  - id: prefail
    env:
      FAILMODE: prefail
EOF
mkdir -p "$w/projects" "$w/runs-ff" "$w/runs-noff"
run_json 1 "$w/ff.out" "bench --fail-fast stops after the sole prefail cell (exit 1)" -- \
  catacomb bench "$w/basket-failfast.yaml" --projects-dir "$w/projects" \
  --runs-dir "$w/runs-ff" --manifest "$w/ff.manifest.jsonl" --fail-fast
rc=0
grep -q 'fail-fast' "$w/ff.out.stderr" || rc=1
record "$rc" "fail-fast error names the flag on stderr"
rc=0
mlines=$(wc -l <"$w/ff.manifest.jsonl" | tr -d ' ')
[ "$mlines" -eq 1 ] || rc=1
record "$rc" "manifest has exactly 1 entry after --fail-fast ($mlines/1) -- \$0 stop"
rc=0
grep -q '"exit_code":1' "$w/ff.manifest.jsonl" || rc=1
grep -q '"variant":"prefail"' "$w/ff.manifest.jsonl" || rc=1
grep -q '"session_id"' "$w/ff.manifest.jsonl" && rc=1
record "$rc" "the sole manifest entry is the prefail cell: exit_code 1, no session_id (no claude call was ever made)"

echo "== prod.81 --fail-fast non-vacuity: the SAME failing cell WITHOUT --fail-fast still exits bench 0 =="
run_json 0 "$w/noff.out" "same basket without --fail-fast: bench exits 0 despite the failing cell" -- \
  catacomb bench "$w/basket-failfast.yaml" --projects-dir "$w/projects" \
  --runs-dir "$w/runs-noff" --manifest "$w/noff.manifest.jsonl"
rc=0
grep -q '"exit_code":1' "$w/noff.manifest.jsonl" || rc=1
record "$rc" "non-vacuity: the cell itself still recorded exit_code 1 (bench only stops propagating it via --fail-fast)"

echo "== prod.81 --error-delta: fabricated toolerr(errseed) vs clean rollout — total error_rate 0 -> 1 gates =="
er="$w/errdelta"; mkdir -p "$er"
# mkcell <run-id> <variant> <is_error bool>: a minimal captured run whose only
# cross-variant difference is the Bash tool_result is_error flag (unpriced
# model, no usage, identical 5s window) -- same fixture shape as
# 95-reduce-edges.sh's StatusError mkcell, renamed to this basket's real live
# variant ids (clean/errseed) for direct correspondence.
mkcell() {
  local rid=$1 var=$2 iserr=$3 dir="$er/$1" sid="sid-$1" txt="ok" tool="tSh"
  [ "$iserr" = "true" ] && txt="boom"
  mkdir -p "$dir"
  cat >"$dir/meta.json" <<EOF
{"run_id":"$rid","task":"failmode","variant":"$var","rep":1,"session_id":"$sid","labels":{"variant":"$var"},"exit_code":0,"basket_hash":"h","marker_name":"","marker_start":"2026-06-20T10:00:00Z","marker_end":"2026-06-20T10:00:05Z","finished_at":"2026-06-20T10:00:05Z"}
EOF
  {
    printf '{"type":"user","uuid":"u1","sessionId":"%s","timestamp":"2026-06-20T10:00:00Z","message":{"role":"user","content":"go"}}\n' "$sid"
    printf '{"type":"assistant","uuid":"a1","sessionId":"%s","timestamp":"2026-06-20T10:00:01Z","message":{"role":"assistant","id":"m1","model":"e2e-model-x","content":[{"type":"tool_use","id":"%s","name":"Bash","input":{"command":"sh -c exit"}}]}}\n' "$sid" "$tool"
    printf '{"type":"user","uuid":"u2","parent_tool_use_id":"%s","sessionId":"%s","timestamp":"2026-06-20T10:00:02Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"%s","content":"%s","is_error":%s}]}}\n' "$tool" "$sid" "$tool" "$txt" "$iserr"
  } >"$dir/session.jsonl"
}
for i in 1 2 3; do mkcell "clean-r$i"   clean   false; done
for i in 1 2 3; do mkcell "errseed-r$i" errseed true;  done
run_json 1 "$er/regress-errseed.json" "regress clean vs errseed --error-delta 0.5: error_rate gates (exit 1)" -- \
  catacomb regress --runs-dir "$er" \
  --baseline label:variant=clean --candidate label:variant=errseed \
  --error-delta 0.5 --json
run_json 0 "$er/regress-ctl.json" "regress clean vs clean: no error_rate move (exit 0)" -- \
  catacomb regress --runs-dir "$er" \
  --baseline label:variant=clean --candidate label:variant=clean --json
rc=0; python3 - "$er/regress-errseed.json" "$er/regress-ctl.json" <<'PY' || rc=$?
import json, sys
def er_total(path):
    for f in json.load(open(path))["findings"]:
        if f["scope"] == "total" and f["metric"] == "error_rate":
            return f
    return None
errs = []
deg = er_total(sys.argv[1])
ctl = er_total(sys.argv[2])
if deg is None:
    errs.append("errseed regress report has no total error_rate finding")
else:
    if deg["verdict"] != "regression":
        errs.append(f"errseed error_rate verdict={deg['verdict']!r} want 'regression'")
    if deg["baseline"] != 0 or deg["candidate"] != 1:
        errs.append(f"errseed error_rate baseline/candidate={deg['baseline']}/{deg['candidate']} want 0/1")
if ctl is None:
    errs.append("control regress report has no total error_rate finding")
elif ctl["verdict"] != "ok" or ctl["candidate"] != 0:
    errs.append(f"control error_rate verdict={ctl['verdict']!r} candidate={ctl['candidate']} want ok/0")
if errs:
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
print("error_rate: clean 0/3 vs errseed 3/3 -> regression via --error-delta 0.5 (exit 1); clean-vs-clean -> ok")
PY
record "$rc" "--error-delta feeds the gate: total error_rate flips ok(0/3) -> regression(3/3), exit 1; clean-vs-clean stays ok"
