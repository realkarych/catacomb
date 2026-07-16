#!/usr/bin/env bash
# Scenario 95 — reducer EDGE/ERROR paths a round-2 audit found E2E-dark: three
# deterministic reducer behaviours that were unit-tested but never driven end-to-end.
# Every step here is offline (rendered fixture transcripts through replay/export/
# regress), zero API spend. Sourced by run.sh with lib.sh loaded and PROD/WORK/
# HERMETIC_* exported; catacomb is on PATH.
#
#   (1) StatusError. Every other fixture uses tool_result is_error:false, so the
#       StatusError reduce path (jsonl sets attrs.status="error"; reduce/resolveStatus
#       promotes the tool_call node to it) never ran E2E — yet error_rate is a gated
#       total metric. A degraded transcript (tool_result is_error:true) is replayed to
#       a JSONL snapshot and its EdgeTool tool_call node MUST carry status "error"; an
#       otherwise-identical ok control (is_error:false) MUST carry status "ok" and never
#       "error". Non-vacuity: the SAME tool name reduces to "ok" in the control, so the
#       "error" status is caused by is_error:true, not emitted unconditionally. If
#       resolveStatus regressed (e.g. dropped the terminal-error promotion), the degraded
#       node would read "ok"/empty and this fails. BONUS: the same ok-vs-error contrast
#       built as evidence dirs drives `regress` — the TOTAL error_rate axis flips from
#       ok (0/3 vs 0/3) to a gating regression (0/3 vs 3/3, exit 1), proving the failure
#       reaches the error_rate gate, not just the node.
#
#   (2) drift / version watchlist. No fixture tripped a drift signal. One transcript
#       carries a top-level "version":"9.9.9" (newer than the tested floor 2.1.199,
#       ingest/drift/drift.go) AND one unknown top-level record type — driving BOTH
#       offline.go warnings onto STDERR: the version-watchlist line and the
#       unrecognized-record count line. A clean control (version == the tested floor,
#       all-known record types) MUST emit NO warning at all. Non-vacuity: the control
#       shares the pipeline, so the warnings are caused by the newer version / unknown
#       record, not printed unconditionally. If the drift floor or the unknown-type
#       detector regressed, the exact strings would not appear (or the control would
#       spuriously warn) and this fails.
#
#   (3) pricing. Every other fixture carries no usage or an unpriceable model, so
#       catacomb's own pricer never produced a non-zero cost E2E. A transcript with real
#       usage (input 1,000,000 / output 500,000 tokens) on a model catacomb prices
#       (claude-sonnet-4-5: $3.00 / MTok in, $15.00 / MTok out — pricing/pricing.go
#       defaultTable) is exported; the assistant_turn node's cost_usd MUST equal the
#       EXACT expected 1.0*3.00 + 0.5*15.00 = 10.50 (cost_source "estimated"), computed
#       by catacomb's pricer, not read from any claude total_cost_usd. Non-vacuity: the
#       assertion is the exact USD, so any rate change (e.g. sonnet in 3.00 -> 3.50 would
#       give 10.50 -> 11.00) or a broken pricer (cost 0 / unpriced) fails it.
set -euo pipefail

w="$WORK/reduce-edges"; rm -rf "$w"; mkdir -p "$w"

echo "== prod.95 StatusError: tool_result is_error:true reduces the tool_call node to status 'error' =="
sed 's/__SESSION_ID__/prod95-toolerr-deg/g' "$PROD/fixtures/95-toolerr-degraded.jsonl.tmpl" > "$w/deg.jsonl"
sed 's/__SESSION_ID__/prod95-toolerr-ok/g'  "$PROD/fixtures/95-toolerr-ok.jsonl.tmpl"       > "$w/ok.jsonl"
run_json 0 "$w/deg.replay" "replay degraded (is_error:true) --export-jsonl" -- \
  catacomb replay "$w/deg.jsonl" --export-jsonl "$w/deg.export.jsonl"
run_json 0 "$w/ok.replay" "replay ok control (is_error:false) --export-jsonl" -- \
  catacomb replay "$w/ok.jsonl" --export-jsonl "$w/ok.export.jsonl"
rc=0; python3 - "$w/deg.export.jsonl" "$w/ok.export.jsonl" <<'PY' || rc=$?
import json, sys
def toolnode(path, name):
    for line in open(path):
        line = line.strip()
        if not line:
            continue
        o = json.loads(line)
        if o.get("kind") == "node" and o.get("type") == "tool_call" and o.get("name") == name:
            return o
    return None
errs = []
deg = toolnode(sys.argv[1], "EdgeTool")
okn = toolnode(sys.argv[2], "EdgeTool")
if deg is None:
    errs.append("degraded export has no EdgeTool tool_call node")
elif deg.get("status") != "error":
    errs.append(f"degraded EdgeTool status={deg.get('status')!r} want 'error'")
if okn is None:
    errs.append("ok export has no EdgeTool tool_call node")
elif okn.get("status") != "ok":
    errs.append(f"ok EdgeTool status={okn.get('status')!r} want 'ok'")
elif okn.get("status") == "error":
    errs.append("non-vacuity broken: ok control EdgeTool also reads 'error'")
if errs:
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
print("StatusError: degraded EdgeTool status='error'; ok control EdgeTool status='ok' (never 'error')")
PY
record "$rc" "StatusError: is_error:true reduces tool_call node to status 'error'; ok control stays 'ok'"

echo "== prod.95 StatusError (bonus): error_rate axis flips ok -> gating regression via regress =="
er="$w/errrate"; mkdir -p "$er"
# mkcell <run-id> <variant> <is_error bool> <result-text>: a minimal captured run whose
# only cross-variant difference is the tool_result is_error flag (unpriced model, no
# usage, identical 5s window) so error_rate is the sole axis that can move.
mkcell() {
  local rid=$1 var=$2 iserr=$3 txt=$4 dir="$er/$1" sid="sid-$1"
  mkdir -p "$dir"
  cat >"$dir/meta.json" <<EOF
{"run_id":"$rid","task":"edge","variant":"$var","rep":1,"session_id":"$sid","labels":{"variant":"$var"},"exit_code":0,"basket_hash":"h","marker_name":"","marker_start":"2026-06-20T10:00:00Z","marker_end":"2026-06-20T10:00:05Z","finished_at":"2026-06-20T10:00:05Z"}
EOF
  {
    printf '{"type":"user","uuid":"u1","sessionId":"%s","timestamp":"2026-06-20T10:00:00Z","message":{"role":"user","content":"go"}}\n' "$sid"
    printf '{"type":"assistant","uuid":"a1","sessionId":"%s","timestamp":"2026-06-20T10:00:01Z","message":{"role":"assistant","id":"m1","model":"e2e-model-x","content":[{"type":"tool_use","id":"tRun","name":"RunTask","input":{"cmd":"x"}}]}}\n' "$sid"
    printf '{"type":"user","uuid":"u2","parent_tool_use_id":"tRun","sessionId":"%s","timestamp":"2026-06-20T10:00:02Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tRun","content":"%s","is_error":%s}]}}\n' "$sid" "$txt" "$iserr"
  } >"$dir/session.jsonl"
}
for i in 1 2 3; do mkcell "base-r$i" baseline false ok; done
for i in 1 2 3; do mkcell "deg-r$i"  degraded true  boom; done
run_json 1 "$er/regress-deg.json" "regress degraded(3x error) vs baseline(3x ok): error_rate gates (exit 1)" -- \
  catacomb regress --runs-dir "$er" \
  --baseline label:variant=baseline --candidate label:variant=degraded --json
run_json 0 "$er/regress-ctl.json" "regress baseline vs baseline: no error_rate move (exit 0)" -- \
  catacomb regress --runs-dir "$er" \
  --baseline label:variant=baseline --candidate label:variant=baseline --json
rc=0; python3 - "$er/regress-deg.json" "$er/regress-ctl.json" <<'PY' || rc=$?
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
    errs.append("degraded regress report has no total error_rate finding")
else:
    if deg["verdict"] != "regression":
        errs.append(f"degraded error_rate verdict={deg['verdict']!r} want 'regression'")
    if deg["baseline"] != 0 or deg["candidate"] != 1:
        errs.append(f"degraded error_rate baseline/candidate={deg['baseline']}/{deg['candidate']} want 0/1")
if ctl is None:
    errs.append("control regress report has no total error_rate finding")
elif ctl["verdict"] != "ok" or ctl["candidate"] != 0:
    errs.append(f"control error_rate verdict={ctl['verdict']!r} candidate={ctl['candidate']} want ok/0")
if errs:
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
print("error_rate: baseline 0/3 vs degraded 3/3 -> regression (gates, exit 1); baseline-vs-baseline -> ok")
PY
record "$rc" "StatusError feeds the gate: total error_rate flips ok(0/3) -> regression(3/3), exit 1"

echo "== prod.95 drift: version-watchlist + unknown-record warnings fire on stderr; clean control silent =="
sed 's/__SESSION_ID__/prod95-drift/g'       "$PROD/fixtures/95-drift-version.jsonl.tmpl" > "$w/drift.jsonl"
sed 's/__SESSION_ID__/prod95-drift-clean/g' "$PROD/fixtures/95-drift-clean.jsonl.tmpl"   > "$w/drift-clean.jsonl"
run_json 0 "$w/drift.replay" "replay drift fixture (version 9.9.9 + unknown record)" -- \
  catacomb replay "$w/drift.jsonl" --export-jsonl "$w/drift.export.jsonl"
run_json 0 "$w/drift-clean.replay" "replay clean control (version 2.1.199, known types)" -- \
  catacomb replay "$w/drift-clean.jsonl" --export-jsonl "$w/drift-clean.export.jsonl"
rc=0
grep -qF "warning: transcript Claude Code version 9.9.9 is newer than tested 2.1.199" "$w/drift.replay.stderr" || rc=1
record "$rc" "drift: version-watchlist warning names 9.9.9 newer than tested 2.1.199"
rc=0
grep -qF "warning: 1 unrecognized transcript record(s) [unknown_record_type=1]" "$w/drift.replay.stderr" || rc=1
record "$rc" "drift: unrecognized-record warning counts the unknown top-level record (unknown_record_type=1)"
rc=0
grep -q '^warning:' "$w/drift-clean.replay.stderr" && rc=1
record "$rc" "drift non-vacuity: clean control (tested floor, known types) emits NO warning"

echo "== prod.95 pricing: catacomb's pricer yields the EXACT non-zero cost_usd on a priced turn =="
sed 's/__SESSION_ID__/prod95-pricing/g' "$PROD/fixtures/95-pricing.jsonl.tmpl" > "$w/price.jsonl"
run_json 0 "$w/price.export.out" "export priced fixture --to jsonl --out" -- \
  catacomb export "$w/price.jsonl" --to jsonl --out "$w/price.export.jsonl"
rc=0; python3 - "$w/price.export.jsonl" <<'PY' || rc=$?
import json, sys
turn = None
for line in open(sys.argv[1]):
    line = line.strip()
    if not line:
        continue
    o = json.loads(line)
    if o.get("kind") == "node" and o.get("type") == "assistant_turn":
        turn = o
        break
errs = []
if turn is None:
    errs.append("export has no assistant_turn node")
else:
    cost = turn.get("cost_usd")
    expected = 1_000_000 / 1_000_000 * 3.00 + 500_000 / 1_000_000 * 15.00  # = 10.50
    if cost is None:
        errs.append("assistant_turn node has no cost_usd (pricer produced nothing)")
    elif abs(cost - expected) > 1e-9:
        errs.append(f"cost_usd={cost!r} want exactly {expected!r}")
    elif cost == 0:
        errs.append("cost_usd is zero (non-vacuity: a priced turn must cost > 0)")
    src = (turn.get("attrs") or {}).get("cost_source")
    if src != "estimated":
        errs.append(f"cost_source={src!r} want 'estimated' (catacomb's own pricer)")
    if turn.get("tokens_in") != 1_000_000 or turn.get("tokens_out") != 500_000:
        errs.append(f"tokens_in/out={turn.get('tokens_in')}/{turn.get('tokens_out')} want 1000000/500000")
if errs:
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
print("pricing: claude-sonnet-4-5 1000000/500000 tok -> cost_usd=10.50 exactly (cost_source=estimated)")
PY
record "$rc" "pricing: catacomb pricer computes exact cost_usd=10.50 for claude-sonnet-4-5 (1e6/5e5 tok)"
