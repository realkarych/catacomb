#!/usr/bin/env bash
# Scenario 70 — subagent reduction from a REAL separate sub-transcript (two-file
# reduce). Every other hermetic subagent fixture puts the subagent's turns INLINE in
# the single main transcript (isSidechain lines) — a shape real Claude Code never
# feeds to reduce. Production `claude -p`/interactive writes each delegated subagent
# to its OWN subagents/agent-*.jsonl sub-transcript beside the main file, and bench's
# resolveTranscripts snapshots both (Main = <root>/*/$sid.jsonl, Subagents = glob
# <root>/*/$sid/subagents/agent-*.jsonl) so regress reduces main + subs TOGETHER via
# loadGraphOffline(main, subs). This scenario drives that real two-file shape through
# the whole path to a subagent-node gate.
#
# emit-subtranscript.sh (the dedicated emitter, not the shared emit.sh) writes the
# main transcript with the delegating Agent tool_use, and — only when
# WITH_SUBTRANSCRIPT=1 (baseline/baseline2) — a SEPARATE sub-transcript under
# $sid/subagents/ carrying the subagent's own isSidechain/agentId turns and a
# tool_use. degraded does the work inline via Bash: no Agent call, no sub-transcript,
# no subagents/ dir. Assertions:
#   - CAPTURE: the baseline run dir contains subagents/agent-*.jsonl (bench snapshotted
#     the real second file); a degraded run dir has no subagents/ dir at all.
#   - GATE (the core new coverage): regress --fail-on-notable gates (exit 1) on a
#     step-scope presence finding named Glob (present 5/5 -> 0/5). Glob is emitted ONLY
#     inside the sub-transcript — absent from BOTH the main and the degraded transcripts
#     — so a Glob step exists in the reduced baseline graph ONLY when catacomb merges
#     subagents/agent-*.jsonl into the main via loadGraphOffline(main, subs) (runsdir.go).
#     Break that two-file reduce and the Glob step never appears, so this gate fails: it
#     is genuinely keyed on the merge, guarding the whole capture->merge->reduce path.
#     (The delegating Agent tool_use lives in the MAIN transcript and drops regardless of
#     the merge, so it is kept only as a SECONDARY check — not the merge-sensitive signal.)
#   - STRUCTURE + NON-VACUITY: replay reads only the main file, so the two-file
#     evidence is reduced by concatenating session.jsonl + subagents/agent-*.jsonl and
#     replaying the concat — the baseline graph carries a "type":"subagent" node
#     (subagent_type general-purpose), a degraded run carries none, AND the baseline
#     session.jsonl ALONE (no sub-transcript) carries none. That last check is the
#     non-vacuity guard: the subagent node exists ONLY because the separate file was
#     reduced in, so the gate fails open if bench ever stops capturing sub-transcripts
#     or the two-file reduce breaks.
#   - A-vs-A: baseline vs baseline2 must NOT gate (exit 0, zero regressions).
# Sourced by run.sh with lib.sh loaded and PROD/WORK/HERMETIC_* exported. Zero API spend.
set -euo pipefail
echo "== prod.70 subagent sub-transcript: bench baseline/degraded/baseline2 (3 variants x 5 reps) =="
w="$WORK/subagent-subtranscript"; mkdir -p "$w/cellwork" "$w/runs"
sed -e "s|__PROD__|$PROD|g" -e "s|__WORK__|$w|g" \
  "$PROD/fixtures/70-subagent-subtranscript.basket.yaml.tmpl" > "$w/basket.yaml"
HERMETIC_PROJECTS="$w/projects" run_json 0 "$w/bench.out" "bench prod-subagent-subtranscript basket" -- \
  catacomb bench "$w/basket.yaml" --projects-dir "$w/projects" --runs-dir "$w/runs" --manifest "$w/m.jsonl"

bdir="$w/runs/bench-prod-subagent-subtranscript-subagent-baseline-r1"
ddir="$w/runs/bench-prod-subagent-subtranscript-subagent-degraded-r1"

echo "== prod.70 subagent sub-transcript: bench snapshotted the REAL separate sub-transcript =="
# resolveTranscripts globbed <sid>/subagents/agent-*.jsonl and evidence.Write copied it
# into the run dir. nullglob is on (dispatcher), so an unmatched glob is an empty array.
base_subs=("$bdir"/subagents/agent-*.jsonl)
rc=0; { [ -d "$bdir/subagents" ] && [ "${#base_subs[@]}" -ge 1 ] && [ -s "${base_subs[0]}" ]; } || rc=1
record "$rc" "baseline run dir carries a captured subagents/agent-*.jsonl sub-transcript"
rc=0; grep -q '"isSidechain":true' "${base_subs[0]:-/dev/null}" || rc=1
record "$rc" "captured sub-transcript is the subagent's own isSidechain turns"
rc=0; if [ -d "$ddir/subagents" ]; then rc=1; fi
record "$rc" "degraded run dir has NO subagents/ dir (inline, nothing to snapshot)"

echo "== prod.70 subagent sub-transcript: two-file reduce feeds the gate (exit 1) =="
run_json 1 "$w/regress.json" "degraded drops the delegated subagent -> STEP notable gate" -- \
  catacomb regress --runs-dir "$w/runs" \
  --baseline label:basket=prod-subagent-subtranscript,variant=baseline \
  --candidate label:basket=prod-subagent-subtranscript,variant=degraded --fail-on-notable --json
rc=0; python3 - "$w/regress.json" <<'PY' || rc=$?
import json, sys
rep = json.load(open(sys.argv[1]))

def drop(name):
    return [
        f for f in rep.get("findings", [])
        if f.get("scope") == "step" and f.get("name") == name
        and f.get("metric") == "presence" and f.get("verdict") in ("regression", "notable")
        and "present 5/5 -> 0/5" in (f.get("detail") or "")
    ]

# Glob is emitted ONLY inside the sub-transcript (absent from the main AND the degraded
# transcript), so a Glob step exists in the reduced baseline graph — and thus this
# presence drop — ONLY when catacomb merges subagents/agent-*.jsonl into the main via
# loadGraphOffline(main, subs). This is the merge-sensitive signal: break the two-file
# reduce and the Glob finding vanishes, failing the gate.
sub_only = drop("Glob")
# The delegating Agent tool_use lives in the MAIN transcript and drops regardless of the
# merge; kept as a secondary check, NOT the merge-sensitive one.
agent = drop("Agent")
if not sub_only or not agent:
    print("missing required step presence drop(s):", file=sys.stderr)
    print("  Glob (sub-only, merge-sensitive):", bool(sub_only),
          " Agent (main, secondary):", bool(agent), file=sys.stderr)
    for f in rep.get("findings", []):
        print("  ", {k: f.get(k) for k in ("scope", "name", "metric", "verdict", "detail")}, file=sys.stderr)
    sys.exit(1)
print("sub-only Glob step presence/notable finding present (exists only via the two-file "
      "merge); delegating Agent step also dropped")
PY
record "$rc" "regress gates on the sub-only Glob step presence drop (5/5 -> 0/5) — proving the two-file merge reduced the sub-transcript in — plus the secondary Agent drop"

echo "== prod.70 subagent sub-transcript: structural subagent node + non-vacuity =="
# replay reads only the main file, so the two-file evidence is reduced by concatenating
# session.jsonl + the captured subagents/agent-*.jsonl and replaying the concat.
cat "$bdir/session.jsonl" "${base_subs[@]}" > "$w/base-concat.jsonl"
run_json 0 "$w/replay-base.out" "replay baseline main+sub concat -> export jsonl snapshot" -- \
  catacomb replay "$w/base-concat.jsonl" --export-jsonl "$w/base.snap.jsonl"
run_json 0 "$w/replay-deg.out" "replay degraded session -> export jsonl snapshot" -- \
  catacomb replay "$ddir/session.jsonl" --export-jsonl "$w/deg.snap.jsonl"
run_json 0 "$w/replay-mainonly.out" "replay baseline main ALONE (no sub) -> export jsonl snapshot" -- \
  catacomb replay "$bdir/session.jsonl" --export-jsonl "$w/base-mainonly.snap.jsonl"
rc=0; grep -q '"type":"subagent"[^}]*"subagent_type":"general-purpose"' "$w/base.snap.jsonl" || rc=1
record "$rc" "baseline two-file reduce yields a \"type\":\"subagent\" node (subagent_type=general-purpose)"
rc=0; if grep -q '"type":"subagent"' "$w/deg.snap.jsonl"; then rc=1; fi
record "$rc" "degraded graph snapshot contains no \"type\":\"subagent\" node"
rc=0; if grep -q '"type":"subagent"' "$w/base-mainonly.snap.jsonl"; then rc=1; fi
record "$rc" "non-vacuity: baseline main ALONE has no subagent node (it comes only from the separate sub-transcript)"

echo "== prod.70 subagent sub-transcript: A-vs-A must NOT gate =="
run_json 0 "$w/ava.json" "A-vs-A must NOT gate" -- \
  catacomb regress --runs-dir "$w/runs" \
  --baseline label:basket=prod-subagent-subtranscript,variant=baseline \
  --candidate label:basket=prod-subagent-subtranscript,variant=baseline2 --metric-rel-delta "$PROD_AVA_METRIC_BAND" --json
rc=0; python3 - "$w/ava.json" <<'PY' || rc=$?
import json, sys
r = json.load(open(sys.argv[1]))
notable = [f for f in r.get("findings", []) if f.get("verdict") == "notable"]
sys.exit(0 if r["regressions"] == 0 and not notable else 1)
PY
record "$rc" "A-vs-A reports zero regressions and no notable findings"
