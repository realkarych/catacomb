#!/usr/bin/env bash
# Scenario 80 — CLI-contract coverage the round-2 audit flagged as untested:
#
#   (A) exit-2 operational contract. regress/verify return exit 2 (not 0/1) for
#       operational errors — a broken selector or config, distinct from "regressed"
#       (exit 1). CI consumers branch on that. Two paths: a label selector that
#       matches no runs (ErrEmptyGroup, "selector matched no runs"), and an invalid
#       selector prefix (parse error). Both must exit 2 and name the fault on stderr.
#
#   (B) non-default regress thresholds actually change a verdict. Every gate-
#       calibration flag was exercised only at its default; nothing proved a
#       caller-tuned value flips the outcome. For three flags this asserts the SAME
#       body of evidence yields DIFFERENT verdicts under default vs tuned — the proof
#       each flag is wired through Compare and applied:
#         --min-support     : a 2-run group is "insufficient" at the default (3) but a
#                             trusted "regression" at 2.
#         --metric-rel-delta: a +20% tokens_out delta sits inside the default 0.25 band
#                             (ok) but outside a tight 0.10 band (regression).
#         --presence-delta  : a 5/5 -> 3/5 step-presence drop (absence delta 0.4) gates
#                             under --fail-on-notable at the default 0.20 but not at 0.50.
#
#   (C) bench lifecycle flags (mirrors e2e/run.sh steps a0/a1/i2, live but Go-unit-only
#       until now): a fabricated basket run through the REAL `catacomb bench` (task cmd
#       `sh -c "exit 0"`, no session observed — bench still records a manifest entry per
#       cell regardless of session-peek success, so this exercises real bench mechanics
#       at $0, no fake-agent transcript needed):
#         --dry-run        : prints the RUN_ID/TASK/VARIANT/REP table for all 4 planned
#                             cells (1 task x 2 variants x 2 reps) and writes no manifest.
#         --workspaces-dir/--keep-workspaces: every cell's per-cell workspace dir survives
#                             teardown under the given root and holds the files its
#                             workspace.cmd staged (mirrors the live SQL basket's
#                             sql-live.sh/verify_sql.py copy-in).
#         --resume          : re-invoked over the manifest the first bench call just
#                             wrote, every cell is already completed, so it skips all 4
#                             (0 newly executed) and prints NO "marked N/M" summary line
#                             (runBenchCells only prints it when executed > 0 — pinned by
#                             the Go unit TestRunBenchCellsResumeAllSkippedOmitsMarked).
#
# Evidence run dirs are built directly (meta.json + session.jsonl) rather than via
# bench: identical bench reps can only ever produce 0/N or N/N presence and a zero
# IQR, so per-run control over token counts and step presence is what makes each flip
# deterministic. Section (C) is the exception — it drives the real `bench` binary to
# exercise its lifecycle flags, not the reduce/regress pipeline. Zero API spend.
# Sourced by run.sh with lib.sh loaded and PROD/WORK/HERMETIC_* exported. catacomb is
# on PATH.
set -euo pipefail

w="$WORK/cli-contracts"; rm -rf "$w"; mkdir -p "$w"

# mkrun <runs-dir> <run-id> <group> <output_tokens> [<extra-tool-use-json>]
# Writes a minimal captured run: an assistant turn carrying <output_tokens> plus a
# Read step node, an optional second tool_use, and a 5s duration window. model
# "e2e-model-x" is unpriced, so cost_usd stays 0 and never confounds a token delta.
mkrun() {
  local rd=$1 rid=$2 grp=$3 tok=$4 extra=${5:-}
  local dir="$rd/$rid" sid="sid-$rid"
  mkdir -p "$dir"
  cat >"$dir/meta.json" <<EOF
{"run_id":"$rid","task":"contract","variant":"$grp","rep":1,"session_id":"$sid","labels":{"grp":"$grp"},"exit_code":0,"basket_hash":"h","marker_name":"","marker_start":"2026-06-20T10:00:00Z","marker_end":"2026-06-20T10:00:05Z","finished_at":"2026-06-20T10:00:05Z"}
EOF
  {
    printf '{"type":"user","uuid":"u1","sessionId":"%s","timestamp":"2026-06-20T10:00:00Z","message":{"role":"user","content":"go"}}\n' "$sid"
    printf '{"type":"assistant","uuid":"a1","sessionId":"%s","timestamp":"2026-06-20T10:00:01Z","message":{"role":"assistant","id":"m1","model":"e2e-model-x","usage":{"input_tokens":50,"output_tokens":%s},"content":[{"type":"tool_use","id":"tRead","name":"Read","input":{"file_path":"a"}}%s]}}\n' "$sid" "$tok" "$extra"
    printf '{"type":"user","uuid":"u2","parent_tool_use_id":"tRead","sessionId":"%s","timestamp":"2026-06-20T10:00:02Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tRead","content":"ok","is_error":false}]}}\n' "$sid"
  } >"$dir/session.jsonl"
}
glob=',{"type":"tool_use","id":"tGlob","name":"Glob","input":{"pattern":"*.go"}}'

# tokens_out verdict of the total-scope finding in a regress --json report.
tok_verdict='
import json, sys
def tv(p):
    for f in json.load(open(p))["findings"]:
        if f["scope"] == "total" and f["metric"] == "tokens_out":
            return f["verdict"]
    return None
'

# --- evidence: min-support group (2 runs/group; 100 vs 200 tokens_out) ---------------
for i in 1 2; do mkrun "$w/msupport" "base-r$i" baseline 100; done
for i in 1 2; do mkrun "$w/msupport" "cand-r$i" candidate 200; done

# --- evidence: metric-rel-delta group (3 runs/group; 100 vs 120 tokens_out) ----------
for i in 1 2 3; do mkrun "$w/metric" "base-r$i" baseline 100; done
for i in 1 2 3; do mkrun "$w/metric" "cand-r$i" candidate 120; done

# --- evidence: presence group (Glob present 5/5 baseline, 3/5 candidate) --------------
for i in 1 2 3 4 5; do mkrun "$w/presence" "base-r$i" baseline 100 "$glob"; done
mkrun "$w/presence" "cand-r1" candidate 100 "$glob"
mkrun "$w/presence" "cand-r2" candidate 100 "$glob"
mkrun "$w/presence" "cand-r3" candidate 100 "$glob"
mkrun "$w/presence" "cand-r4" candidate 100
mkrun "$w/presence" "cand-r5" candidate 100

echo "== prod.80 cli-contracts: exit-2 operational path (broken selector, not a regression) =="
run_json 2 "$w/e2-norun.out" "regress: baseline label selector matches no runs -> exit 2" -- \
  catacomb regress --runs-dir "$w/metric" \
  --baseline label:grp=absent --candidate label:grp=candidate --json
rc=0; grep -q "selector matched no runs" "$w/e2-norun.out.stderr" || rc=1
record "$rc" "no-runs selector stderr says 'selector matched no runs'"

run_json 2 "$w/e2-badsel.out" "regress: invalid selector prefix -> exit 2" -- \
  catacomb regress --runs-dir "$w/metric" \
  --baseline bogus:x --candidate label:grp=candidate --json
rc=0; grep -q "unknown prefix" "$w/e2-badsel.out.stderr" || rc=1
record "$rc" "invalid-selector stderr names the unknown prefix"

echo "== prod.80 cli-contracts: --min-support flips insufficient <-> regression (same evidence) =="
run_json 0 "$w/ms-default.out" "min-support default(3): 2-run group insufficient (exit 0)" -- \
  catacomb regress --runs-dir "$w/msupport" \
  --baseline label:grp=baseline --candidate label:grp=candidate --json
run_json 1 "$w/ms-tuned.out" "min-support 2: 2-run group trusted -> regression (exit 1)" -- \
  catacomb regress --runs-dir "$w/msupport" \
  --baseline label:grp=baseline --candidate label:grp=candidate --min-support 2 --json
rc=0; python3 - "$w/ms-default.out" "$w/ms-tuned.out" <<PY || rc=$?
$tok_verdict
d, t = tv(sys.argv[1]), tv(sys.argv[2])
if d != "insufficient" or t != "regression":
    print("min-support flip not observed: default=%r tuned=%r" % (d, t), file=sys.stderr)
    sys.exit(1)
print("min-support flip: total tokens_out insufficient(default 3) -> regression(tuned 2)")
PY
record "$rc" "--min-support flips total tokens_out insufficient(3) -> regression(2)"

echo "== prod.80 cli-contracts: --metric-rel-delta flips ok <-> regression (same evidence) =="
run_json 0 "$w/mr-default.out" "metric-rel-delta default(0.25): 100->120 within band -> ok (exit 0)" -- \
  catacomb regress --runs-dir "$w/metric" \
  --baseline label:grp=baseline --candidate label:grp=candidate --json
run_json 1 "$w/mr-tuned.out" "metric-rel-delta 0.10: 100->120 exceeds band -> regression (exit 1)" -- \
  catacomb regress --runs-dir "$w/metric" \
  --baseline label:grp=baseline --candidate label:grp=candidate --metric-rel-delta 0.1 --json
rc=0; python3 - "$w/mr-default.out" "$w/mr-tuned.out" <<PY || rc=$?
$tok_verdict
d, t = tv(sys.argv[1]), tv(sys.argv[2])
if d != "ok" or t != "regression":
    print("metric-rel-delta flip not observed: default=%r tuned=%r" % (d, t), file=sys.stderr)
    sys.exit(1)
print("metric-rel-delta flip: total tokens_out ok(default 0.25) -> regression(tight 0.10)")
PY
record "$rc" "--metric-rel-delta flips total tokens_out ok(0.25) -> regression(0.10)"

echo "== prod.80 cli-contracts: --presence-delta flips the step-presence gate on/off (same evidence) =="
run_json 1 "$w/pd-default.out" "presence-delta default(0.2): 5/5->3/5 presence gates (exit 1)" -- \
  catacomb regress --runs-dir "$w/presence" \
  --baseline label:grp=baseline --candidate label:grp=candidate --fail-on-notable --json
run_json 0 "$w/pd-loose.out" "presence-delta 0.5: 5/5->3/5 within band -> no gate (exit 0)" -- \
  catacomb regress --runs-dir "$w/presence" \
  --baseline label:grp=baseline --candidate label:grp=candidate --presence-delta 0.5 --fail-on-notable --json
rc=0; python3 - "$w/pd-default.out" "$w/pd-loose.out" <<'PY' || rc=$?
import json, sys
def gate(p):
    r = json.load(open(p))
    return [f for f in r["findings"]
            if f["scope"] == "step" and f["metric"] == "presence"
            and f["verdict"] in ("notable", "regression")]
dg, lg = gate(sys.argv[1]), gate(sys.argv[2])
if not dg or lg:
    print("presence-delta flip not observed: default_gating=%r loose_gating=%r" % (dg, lg), file=sys.stderr)
    sys.exit(1)
print("presence-delta flip: step presence %s gates at default 0.2, silent at loose 0.5" % dg[0]["verdict"])
PY
record "$rc" "--presence-delta flips the step presence gate: gates at 0.2, silent at 0.5"

echo "== prod.80 cli-contracts: bench lifecycle flags --dry-run/--resume/--workspaces-dir/--keep-workspaces (fabricated basket, real bench binary, \$0) =="
lw="$w/lifecycle"; rm -rf "$lw"; mkdir -p "$lw/stage" "$lw/workspaces"
printf '#!/bin/sh\necho staged-wrapper\n'  >"$lw/stage/wrapper.sh"
printf '#!/bin/sh\necho staged-verify\n'   >"$lw/stage/verify.sh"
# The task cmd never observes a session id (bench still records a manifest entry per
# cell — no session id just means the entry carries a "no session id observed" note,
# same as any local no-op child), so this drives the real bench binary without a fake
# claude/codex transcript wrapper. The workspace cmd copies the two staged files in,
# mirroring the live SQL basket's sql-live.sh/verify_sql.py staging.
cat >"$lw/basket.yaml" <<EOF
basket: prod-lifecycle
reps: 2
tasks:
  - id: probe
    cmd: ["sh", "-c", "exit 0"]
    workspace:
      cmd: ["sh", "-c", "cp $lw/stage/wrapper.sh $lw/stage/verify.sh ."]
variants:
  - id: baseline
  - id: degraded
EOF

run_json 0 "$lw/dryrun.out" "bench lifecycle --dry-run lists 4 planned cells, writes no manifest" -- \
  catacomb bench "$lw/basket.yaml" --dry-run
rc=0
planned=$(grep -c '^bench-prod-lifecycle-probe-' "$lw/dryrun.out" || true)
[ "$planned" -eq 4 ] || rc=1
record "$rc" "dry-run planned-cell count: $planned/4 (1 task x 2 variants x 2 reps)"
rc=0
[ -f "$lw/basket.yaml.manifest.jsonl" ] && rc=1
record "$rc" "dry-run wrote no manifest (default <basket>.manifest.jsonl path absent)"

run_json 0 "$lw/bench1.out" "bench lifecycle basket (--workspaces-dir + --keep-workspaces)" -- \
  catacomb bench "$lw/basket.yaml" --runs-dir "$lw/runs" --manifest "$lw/m.jsonl" \
  --workspaces-dir "$lw/workspaces" --keep-workspaces
rc=0
ws_count=0
for d in "$lw"/workspaces/bench-prod-lifecycle-probe-*; do
  [ -d "$d" ] || continue
  ws_count=$((ws_count + 1))
  [ -f "$d/wrapper.sh" ] || rc=1
  [ -f "$d/verify.sh" ] || rc=1
done
[ "$ws_count" -eq 4 ] || rc=1
record "$rc" "--keep-workspaces kept $ws_count/4 per-cell workspace dirs, each holding the staged wrapper.sh + verify.sh"

run_json 0 "$lw/resume.out" "bench lifecycle --resume over the just-completed manifest: 0 newly-executed cells" -- \
  catacomb bench "$lw/basket.yaml" --runs-dir "$lw/runs" --manifest "$lw/m.jsonl" --resume
rc=0
skips=$(grep -c '(already completed)$' "$lw/resume.out" || true)
[ "$skips" -eq 4 ] || rc=1
record "$rc" "resume skip count: $skips/4 already-completed cells (0 newly executed)"
rc=0
grep -q '^marked ' "$lw/resume.out" && rc=1
record "$rc" "resume printed no 'marked N/M' summary (0 cells executed => the live re-invoke's zero-spend contract holds)"
rc=0
mlines=$(wc -l <"$lw/m.jsonl" | tr -d ' ')
[ "$mlines" -eq 4 ] || rc=1
record "$rc" "manifest unchanged at $mlines/4 entries after --resume"
