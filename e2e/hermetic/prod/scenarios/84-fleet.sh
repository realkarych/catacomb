#!/usr/bin/env bash
# Scenario 84 — the fleet-export path (PR #207, RecordVersion 2) had ZERO end-to-end
# coverage; only Go unit tests exercised `regress.Record.Project` and `trends --json`.
# This scenario proves, on the built binary against a real SQLite --db, that:
#
#   (1) `regress --record --project acme-search` stamps the project identity into the
#       appended history row, and `trends <baseline> --json` surfaces it at
#       entry.record.project — the field is not merely serialized, it round-trips
#       through the store and the read path.
#   (2) Non-vacuity control: a second record on the SAME baseline written WITHOUT
#       --project carries NO "project" key at all (Record.Project is `omitempty`), so
#       the stamp is driven by the flag, not emitted unconditionally.
#   (3) Every recorded entry carries record.v == 2 (regress.RecordVersion), proving the
#       history row format the fleet feature depends on.
#   (4) The guard: `--project` without `--record` is a usage error naming
#       "--project requires --record" (cmd/catacomb/regress.go), exits non-zero
#       (operational error, exit 2) and never touches the db.
#
# Every step here is offline (two fabricated captured-run variants, no fixtures
# needed), zero API spend. Sourced by run.sh with lib.sh loaded and PROD/WORK/
# HERMETIC_* exported; catacomb is on PATH.
set -euo pipefail

w="$WORK/fleet"; rm -rf "$w"; mkdir -p "$w"
runs="$w/runs"; mkdir -p "$runs"

echo "== prod.84 fabricate 2 variants x 3 reps: baseline and cand carry identical evidence =="
# mkcell <run-id> <variant> <rep>: a minimal captured run. baseline and cand share the
# SAME marker window and tool result, so the comparison itself stays "ok" (exit 0) on
# both --record invocations below -- this scenario is about the --project stamp, not
# about driving a gating regression.
mkcell() {
  local rid=$1 var=$2 rep=$3 dir="$runs/$1" sid="sid-$1"
  mkdir -p "$dir"
  cat >"$dir/meta.json" <<EOF
{"run_id":"$rid","task":"fleet","variant":"$var","rep":$rep,"session_id":"$sid","labels":{"variant":"$var"},"exit_code":0,"basket_hash":"h","marker_name":"","marker_start":"2026-06-20T10:00:00Z","marker_end":"2026-06-20T10:00:05Z","finished_at":"2026-06-20T10:00:05Z"}
EOF
  {
    printf '{"type":"user","uuid":"u1","sessionId":"%s","timestamp":"2026-06-20T10:00:00Z","message":{"role":"user","content":"go"}}\n' "$sid"
    printf '{"type":"assistant","uuid":"a1","sessionId":"%s","timestamp":"2026-06-20T10:00:01Z","message":{"role":"assistant","id":"m1","model":"e2e-model-x","content":[{"type":"tool_use","id":"tRun","name":"FleetTool","input":{"cmd":"x"}}]}}\n' "$sid"
    printf '{"type":"user","uuid":"u2","parent_tool_use_id":"tRun","sessionId":"%s","timestamp":"2026-06-20T10:00:02Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tRun","content":"ok","is_error":false}]}}\n' "$sid"
  } >"$dir/session.jsonl"
}
for i in 1 2 3; do mkcell "baseline-r$i" baseline "$i"; done
for i in 1 2 3; do mkcell "cand-r$i" cand "$i"; done
rc=0; [ "$(find "$runs" -mindepth 1 -maxdepth 1 -type d | wc -l | tr -d ' ')" -eq 6 ] || rc=1
record "$rc" "fabricated 6 run dirs (2 variants x 3 reps)"

echo "== prod.84 pin the named baseline fleet-base in a fresh db =="
run_json 0 "$w/baseline-set.out" "baseline set fleet-base --label variant=baseline" -- \
  catacomb baseline set fleet-base --label variant=baseline \
  --db "$w/fleet.db" --runs-dir "$runs"

echo "== prod.84 record WITH --project acme-search, then WITHOUT --project (non-vacuity control) =="
run_json 0 "$w/record-stamped.json" "regress --record --project acme-search (exit 0, identical evidence)" -- \
  catacomb regress --runs-dir "$runs" --db "$w/fleet.db" \
  --baseline name:fleet-base --candidate label:variant=cand \
  --record --project acme-search
run_json 0 "$w/record-unstamped.json" "regress --record (no --project) (exit 0, identical evidence)" -- \
  catacomb regress --runs-dir "$runs" --db "$w/fleet.db" \
  --baseline name:fleet-base --candidate label:variant=cand \
  --record

echo "== prod.84 guard: --project without --record is a usage error naming '--project requires --record' =="
run_json 2 "$w/guard.out" "regress --project nope (no --record) rejected before touching the db" -- \
  catacomb regress --runs-dir "$runs" --db "$w/fleet.db" \
  --baseline name:fleet-base --candidate label:variant=cand --project nope
rc=0
grep -qF -- "--project requires --record" "$w/guard.out.stderr" || rc=1
record "$rc" "guard stderr names '--project requires --record'"

echo "== prod.84 trends --json surfaces the project stamp at record.project; control has no project key =="
run_json 0 "$w/trends.json" "trends fleet-base --db --json (recorded history)" -- \
  catacomb trends fleet-base --db "$w/fleet.db" --json
rc=0; python3 - "$w/trends.json" <<'PY' || rc=$?
import json, sys

entries = json.load(open(sys.argv[1]))
errs = []
if len(entries) != 2:
    errs.append(f"want exactly 2 history entries, got {len(entries)}")

records = [e["record"] for e in entries]
for rec in records:
    if rec.get("v") != 2:
        errs.append(f"record.v={rec.get('v')!r} want 2 (RecordVersion) for every entry")

stamped = [r for r in records if r.get("project") == "acme-search"]
if len(stamped) != 1:
    errs.append(f"want exactly 1 entry with record.project=='acme-search', got {len(stamped)}")

unstamped = [r for r in records if "project" not in r]
if len(unstamped) != 1:
    errs.append(f"want exactly 1 entry with NO 'project' key (non-vacuity control), got {len(unstamped)}")

# Non-vacuity: the control entry must not merely have an empty/falsy project value --
# the key itself must be absent, proving Record.Project's `omitempty` and that the
# stamp is driven by the --project flag, not emitted unconditionally on every record.
for r in records:
    if "project" in r and r["project"] == "":
        errs.append(f"a record carries an EMPTY project key {r!r} -- omitempty broken (key should be absent, not empty)")

if errs:
    for x in errs:
        print("  -", x, file=sys.stderr)
    sys.exit(1)
print(f"trends --json: {len(entries)} entries, both record.v==2; "
      f"stamped entry record.project=='acme-search', control entry has NO project key")
PY
record "$rc" "trends --json: project stamp round-trips to record.project; control entry omits the key entirely (v==2 on both)"
