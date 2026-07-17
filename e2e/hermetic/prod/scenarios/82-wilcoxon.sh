#!/usr/bin/env bash
# Scenario 82 — the Wilcoxon paired test (ADR-0035, --paired-test wilcoxon) had ZERO
# end-to-end coverage; only regress/paired_test.go exercised it. This scenario proves,
# on IDENTICAL fabricated evidence, that the magnitude-weighted signed-rank test fires
# a paired duration_ms REGRESSION where the default sign test does not.
#
# Design (thresholds are regress.DefaultThresholds(): MinSupport=3, PairedMinTasks=5,
# PairedAlpha=0.05): 6 tasks (t1..t6) x 2 variants (baseline, cand) x 3 reps = 36
# captured run dirs. Every run shares the SAME marker_start; only marker_end varies, so
# duration_ms is the sole metric under motion. baseline is always a 5000ms window; cand
# is 5000ms + a per-task delta (+600,+500,+400,+300,+200,-100 for t1..t6) -- 5 tasks
# widen, 1 narrows. Every session carries no usage block and an unpriced model id, so
# cost_usd/tokens_in/tokens_out are unsupported (N=0) and their paired axis stays ok.
#
# Sign test (6 per-task median deltas: 5 positive, 1 negative): pReg = P(X>=5) for
# X~Binomial(6,0.5) = 7/64 ~= 0.1094 > 0.05 alpha -> paired/duration_ms verdict "ok".
# Wilcoxon signed-rank: ranking by |delta| puts the lone negative (-100, smallest
# magnitude) at rank 1, so W+ = 20/21 and pReg = 2/64 = 0.03125 <= 0.05 -> paired/
# duration_ms verdict "regression". Both invocations query the SAME runs-dir with the
# SAME --baseline/--candidate selectors; only --paired-test differs.
#
# regress's own report renderer (regress.filterFindings) DROPS any non-total finding
# whose verdict is "ok" -- passing per-task/per-phase/per-step diagnostics are only
# surfaced when they are notable. That means the sign run's paired/duration_ms finding
# (verdict ok) does not appear ANYWHERE in its report (json, human, or markdown) --
# confirmed empirically against the built binary, not assumed. Its absence there IS the
# proof of "ok": filterFindings guarantees a paired finding is emitted iff its verdict
# is not ok, so under wilcoxon (verdict regression) it MUST appear, and under sign
# (verdict ok) it MUST NOT. The same asymmetry shows up in the exit code: wilcoxon's
# lone paired regression is the only gating axis (every total-scope finding, including
# TOTAL duration_ms, stays ok on this evidence) so wilcoxon exits 1 and sign exits 0 --
# --paired-test alone flips both the report contents and the process exit code.
set -euo pipefail

w="$WORK/wilcoxon"; rm -rf "$w"; mkdir -p "$w"
runs="$w/runs"; mkdir -p "$runs"

echo "== prod.82 fabricate 6 tasks x {baseline,cand} x 3 reps: only marker window (duration_ms) moves =="
# mkcell <run-id> <task> <variant> <rep> <window-ms>: a captured run whose ONLY
# cross-variant axis is the marker_start/marker_end window width (unpriced model, no
# usage block, so cost_usd/tokens_in/tokens_out stay unsupported/absent).
mkcell() {
  local rid=$1 task=$2 variant=$3 rep=$4 width_ms=$5
  local dir="$runs/$rid" sid="sid-$rid"
  local sec=$((width_ms / 1000)) ms=$((width_ms % 1000))
  local mend
  mend=$(printf '2026-06-20T10:00:%02d.%03dZ' "$sec" "$ms")
  mkdir -p "$dir"
  cat >"$dir/meta.json" <<EOF
{"run_id":"$rid","task":"$task","variant":"$variant","rep":$rep,"session_id":"$sid","labels":{"task":"$task","variant":"$variant"},"exit_code":0,"basket_hash":"h","marker_name":"","marker_start":"2026-06-20T10:00:00.000Z","marker_end":"$mend","finished_at":"$mend"}
EOF
  {
    printf '{"type":"user","uuid":"u1","sessionId":"%s","timestamp":"2026-06-20T10:00:00Z","message":{"role":"user","content":"go"}}\n' "$sid"
    printf '{"type":"assistant","uuid":"a1","sessionId":"%s","timestamp":"2026-06-20T10:00:01Z","message":{"role":"assistant","id":"m1","model":"e2e-model-x","content":[{"type":"tool_use","id":"tRun","name":"RunTask","input":{"cmd":"x"}}]}}\n' "$sid"
    printf '{"type":"user","uuid":"u2","parent_tool_use_id":"tRun","sessionId":"%s","timestamp":"2026-06-20T10:00:02Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tRun","content":"ok","is_error":false}]}}\n' "$sid"
  } >"$dir/session.jsonl"
}
# task-cand-delta-ms <task>: the per-task candidate delta from the design table above.
task-cand-delta-ms() {
  case "$1" in
    t1) echo 600 ;;
    t2) echo 500 ;;
    t3) echo 400 ;;
    t4) echo 300 ;;
    t5) echo 200 ;;
    t6) echo -100 ;;
  esac
}
for task in t1 t2 t3 t4 t5 t6; do
  delta=$(task-cand-delta-ms "$task")
  cand_width=$((5000 + delta))
  for rep in 1 2 3; do
    mkcell "${task}-baseline-r${rep}" "$task" baseline "$rep" 5000
    mkcell "${task}-cand-r${rep}" "$task" cand "$rep" "$cand_width"
  done
done
rc=0; [ "$(find "$runs" -mindepth 1 -maxdepth 1 -type d | wc -l | tr -d ' ')" -eq 36 ] || rc=1
record "$rc" "fabricated 36 run dirs (6 tasks x 2 variants x 3 reps)"

echo "== prod.82 regress --paired-test wilcoxon vs sign on the SAME runs-dir/selectors =="
run_json 1 "$w/wilcoxon.json" "regress --paired-test wilcoxon --json (paired duration_ms regression gates)" -- \
  catacomb regress --runs-dir "$runs" \
  --baseline label:variant=baseline --candidate label:variant=cand \
  --paired-test wilcoxon --json
run_json 0 "$w/sign.json" "regress --paired-test sign --json (default; paired duration_ms stays ok)" -- \
  catacomb regress --runs-dir "$runs" \
  --baseline label:variant=baseline --candidate label:variant=cand \
  --paired-test sign --json

rc=0; python3 - "$w/wilcoxon.json" "$w/sign.json" <<'PY' || rc=$?
import json, sys

wil = json.load(open(sys.argv[1]))
sig = json.load(open(sys.argv[2]))
errs = []


def paired(report):
    return [f for f in report["findings"] if f["scope"] == "paired"]


wil_paired = paired(wil)
sig_paired = paired(sig)

wil_duration = [f for f in wil_paired if f["metric"] == "duration_ms"]
if len(wil_duration) != 1:
    errs.append(f"wilcoxon: want exactly 1 paired/duration_ms finding, got {wil_duration!r}")
else:
    f = wil_duration[0]
    if f["verdict"] != "regression":
        errs.append(f"wilcoxon paired/duration_ms verdict={f['verdict']!r} want 'regression'")
    if "W+" not in f.get("detail", ""):
        errs.append(f"wilcoxon paired/duration_ms detail {f.get('detail')!r} missing 'W+'")
    if f.get("detail") != "W+ 20/21 over 6 tasks, p=0.03125":
        errs.append(f"wilcoxon paired/duration_ms detail {f.get('detail')!r} want exact 'W+ 20/21 over 6 tasks, p=0.03125'")

# Non-vacuity + regress.filterFindings contract: a non-total finding is emitted iff its
# verdict is not "ok". The sign run's paired/duration_ms verdict IS "ok" (pReg=7/64 ~=
# 0.1094 > 0.05), so it must be ABSENT from findings entirely -- proving the flag alone,
# not the evidence (identical between the two invocations), flips the outcome.
sig_duration = [f for f in sig_paired if f["metric"] == "duration_ms"]
if sig_duration:
    errs.append(f"sign: paired/duration_ms present {sig_duration!r} want ABSENT (verdict ok is filtered)")

# The other three paired metrics (cost_usd/tokens_in/tokens_out) are unsupported on
# both variants (unpriced model, no usage block) so they stay "ok" under wilcoxon too
# -- also filtered, so the fire is isolated to duration_ms alone.
wil_other = [f for f in wil_paired if f["metric"] != "duration_ms"]
if wil_other:
    errs.append(f"wilcoxon: unexpected non-duration_ms paired findings {wil_other!r} want none (cost/tokens stay ok)")
if sig_paired:
    errs.append(f"sign: unexpected paired findings {sig_paired!r} want NONE at all (every paired axis is ok)")

if wil["overall_verdict"] != "regression":
    errs.append(f"wilcoxon overall_verdict={wil['overall_verdict']!r} want 'regression'")
if sig["overall_verdict"] != "ok":
    errs.append(f"sign overall_verdict={sig['overall_verdict']!r} want 'ok'")

if errs:
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
print("--paired-test alone flips duration_ms: wilcoxon paired finding present+regression "
      "(W+ 20/21, p=0.03125, exit 1); sign paired finding absent (ok, filtered, exit 0)")
PY
record "$rc" "wilcoxon fires paired duration_ms regression (W+); sign stays ok on the SAME evidence"
