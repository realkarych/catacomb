#!/usr/bin/env bash
# Scenario 50 — import-path subagent, now with a GATE. Two things are proven:
#   (1) the `import` ENTRY POINT ingests a subagent sub-transcript into a
#       "type":"subagent" graph node carrying subagent_type (the P0 fix: import reads
#       subagent_type stamped top-level on the interactive session's sidechain lines),
#       verified by replaying the imported evidence to a graph snapshot; and
#   (2) that the subagent's ABSENCE is CAUGHT — not merely that the node is produced.
#       Earlier this scenario only greped the snapshot for presence, so a reducer that
#       silently stopped emitting the subagent node would still have passed. It now
#       imports a DEGRADED twin (a realistic interactive session that does the DB
#       inspection INLINE via Bash — no Agent delegation, no sidechain, no
#       subagent_type) alongside the baseline and runs `catacomb regress
#       --fail-on-notable` baseline-vs-degraded: dropping the delegated subagent gates
#       (exit 1) as a step-scope presence notable on the "Agent" delegation step
#       (present 5/5 -> 0/5). NON-VACUITY: an A-vs-A comparison (interactive vs an
#       identically-imported interactive2) does NOT gate (exit 0, zero notable), so the
#       gate fires on the dropped subagent, not on import noise.
#
# Both variants enter through `catacomb import --transcript <session.jsonl>` (no agent
# spawn) into bench-cell evidence dirs; 5 reps each clear regress's MinSupport so the
# presence finding is a real notable rather than "insufficient". Baseline reuses the
# existing single-variant basket (interactive); degraded/interactive2 come from a second
# basket sharing the same basket NAME (prod-import-subagent) so regress groups them.
# Sourced by run.sh with lib.sh loaded and PROD/WORK/HERMETIC_* exported. Zero API spend.
set -euo pipefail
echo "== prod.50 import-subagent: import an interactive session (with sidechain) + a degraded twin =="
w="$WORK/import-subagent"; mkdir -p "$w/runs"
sid="import-subagent"
transcript="$w/$sid.jsonl"
sed "s/__SESSION_ID__/$sid/g" "$PROD/fixtures/import-subagent.jsonl.tmpl" > "$transcript"
cp "$PROD/fixtures/import-subagent.basket.yaml.tmpl" "$w/basket.yaml"
degraded="$w/import-subagent-degraded.jsonl"
sed "s/__SESSION_ID__/import-subagent-degraded/g" "$PROD/fixtures/50-import-degraded.jsonl.tmpl" > "$degraded"
cp "$PROD/fixtures/50-import-degraded.basket.yaml.tmpl" "$w/basket-degraded.yaml"

run_json 0 "$w/import.out" "import interactive transcript into fresh evidence (no agent spawn)" -- \
  catacomb import "$w/basket.yaml" --task delegate --variant interactive --rep 1 \
  --transcript "$transcript" --runs-dir "$w/runs"

echo "== prod.50 import-subagent: 5 reps per variant (interactive / degraded / interactive2) =="
rc=0
for r in 2 3 4 5; do
  catacomb import "$w/basket.yaml" --task delegate --variant interactive --rep "$r" \
    --transcript "$transcript" --runs-dir "$w/runs" >/dev/null 2>&1 || rc=1
done
for r in 1 2 3 4 5; do
  catacomb import "$w/basket-degraded.yaml" --task delegate --variant degraded --rep "$r" \
    --transcript "$degraded" --runs-dir "$w/runs" >/dev/null 2>&1 || rc=1
  catacomb import "$w/basket-degraded.yaml" --task delegate --variant interactive2 --rep "$r" \
    --transcript "$transcript" --runs-dir "$w/runs" >/dev/null 2>&1 || rc=1
done
record "$rc" "imported 5 reps each of interactive (with subagent), degraded (inline), interactive2 (A-vs-A twin)"

rundir="$w/runs/import-prod-import-subagent-delegate-interactive-r1"
rc=0; { [ -f "$rundir/meta.json" ] && [ -f "$rundir/session.jsonl" ]; } || rc=1
record "$rc" "import wrote a bench-cell evidence dir (meta.json + session.jsonl)"
rc=0; python3 -c 'import json,sys; m=json.load(open(sys.argv[1])); sys.exit(0 if m["task"]=="delegate" and m["variant"]=="interactive" else 1)' \
  "$rundir/meta.json" || rc=1
record "$rc" "imported cell carries task=delegate variant=interactive labels"

echo "== prod.50 import-subagent: replay the imported session -> graph snapshot =="
snap="$w/import.snap.jsonl"
run_json 0 "$w/replay.out" "replay imported session -> export jsonl snapshot" -- \
  catacomb replay "$rundir/session.jsonl" --export-jsonl "$snap"

rc=0; grep -q '"type":"subagent"' "$snap" || rc=1
record "$rc" "imported graph snapshot contains a \"type\":\"subagent\" node (import entry point)"
rc=0; grep -q '"type":"subagent"[^}]*"subagent_type":"general-purpose"' "$snap" || rc=1
record "$rc" "imported subagent node carries subagent_type=general-purpose (P0 fix on import path)"
rc=0; grep -q '"name":"Agent"' "$snap" || rc=1
record "$rc" "imported graph names the delegating tool Agent"

echo "== prod.50 import-subagent: degraded drops the subagent -> regress GATES (exit 1) =="
run_json 1 "$w/regress.json" "degraded (inline, no subagent) vs baseline -> STEP notable gate" -- \
  catacomb regress --runs-dir "$w/runs" \
  --baseline label:basket=prod-import-subagent,variant=interactive \
  --candidate label:basket=prod-import-subagent,variant=degraded --fail-on-notable --json
rc=0; python3 - "$w/regress.json" <<'PY' || rc=$?
import json, sys
rep = json.load(open(sys.argv[1]))
# The delegating "Agent" tool step exists ONLY in the baseline (the interactive session
# that spawned the subagent). The degraded twin does the work inline via Bash, so that
# step drops 5/5 -> 0/5 — the specific, subagent-attributable signal the gate fires on.
agent = [
    f for f in rep.get("findings", [])
    if f.get("scope") == "step" and f.get("name") == "Agent"
    and f.get("metric") == "presence" and f.get("verdict") in ("regression", "notable")
    and "present 5/5 -> 0/5" in (f.get("detail") or "")
]
if not agent:
    print("missing the Agent-delegation step presence drop (present 5/5 -> 0/5):", file=sys.stderr)
    for f in rep.get("findings", []):
        print("  ", {k: f.get(k) for k in ("scope", "name", "metric", "verdict", "detail")}, file=sys.stderr)
    sys.exit(1)
print("Agent-delegation step dropped 5/5 -> 0/5 (subagent absence caught by the gate)")
PY
record "$rc" "regress gates (exit 1) on the dropped Agent-delegation step presence (5/5 -> 0/5) — subagent absence is CAUGHT, not just its presence produced"

echo "== prod.50 import-subagent: A-vs-A must NOT gate (non-vacuity) =="
run_json 0 "$w/ava.json" "A-vs-A (interactive vs interactive2) must NOT gate" -- \
  catacomb regress --runs-dir "$w/runs" \
  --baseline label:basket=prod-import-subagent,variant=interactive \
  --candidate label:basket=prod-import-subagent,variant=interactive2 --fail-on-notable --json
rc=0; python3 - "$w/ava.json" <<'PY' || rc=$?
import json, sys
r = json.load(open(sys.argv[1]))
notable = [f for f in r.get("findings", []) if f.get("verdict") == "notable"]
sys.exit(0 if r["regressions"] == 0 and not notable else 1)
PY
record "$rc" "A-vs-A reports zero regressions and no notable findings (the gate would pass at exit 0 if the subagent weren't dropped)"
