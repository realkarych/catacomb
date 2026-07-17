#!/usr/bin/env bash
# Scenario 59 — codex skill artifact-substitute: Codex has no dedicated
# skill-invocation event — no tool call or stream event distinguishes "a skill
# fired" from any other file read or exec_command (see
# docs/internal/plans/2026-07-17-full-live-validation.md Task 13 and the
# codex-capabilities research it cites). This scenario pins the documented
# substitute contract end-to-end, entirely offline, against a fake codex CLI
# (fixtures/59-fake-codex.sh) standing in for `codex exec --json`:
#   (1) baseline's fabricated rollout reads .agents/skills/e2e-emit/SKILL.md via
#       an exec_command function_call and the fake CLI writes the
#       CATACOMB-SKILL-OK artifact to out/result.csv; degraded's rollout never
#       mentions SKILL.md and the fake CLI writes NO artifact at all (absent,
#       not merely wrong) -- e2e/verify_emit.py, the EXACT same script the live
#       Claude skill basket and basket-codex-skill.yaml both use, scores
#       verifier.pass 1 in baseline, 0 in degraded;
#   (2) the ARTIFACT gate: baseline-vs-degraded regress asserts an
#       ann:verifier.pass regression finding and gates (exit 1) -- the only
#       HARD assertion this scenario makes, matching the plan's "verify the
#       work artifact only" design decision for Codex skills;
#   (3) a SOFT, LOGGED-ONLY grep of the baseline rollout for the SKILL.md path
#       inside a function_call's arguments -- reported via `pass` (never
#       `record`/`failrec`) on either branch, so it can never fail this
#       scenario regardless of outcome, mirroring the live leg's
#       logged-not-gated posture for the identical signal;
#   (4) the negative-space assertion this whole basket exists to make honest:
#       exporting baseline's graph snapshot contains NO "type":"skill" node --
#       reduce/skill.go's isSkill() only matches the Claude tool names "Skill"/
#       "SlashCommand", so a codex exec_command can never be classified as one,
#       no matter what its arguments say. This is a platform-limitation pin,
#       not a bug to fix: the plan explicitly forbids inventing a structural
#       codex skill node.
# Sourced by run.sh with lib.sh loaded and PROD/WORK/HERMETIC_*/REPO exported.
# Zero API spend.
set -euo pipefail
echo "== prod.59 codex-skill: bench baseline/degraded (2 variants x 3 reps) against a fake codex CLI =="
w="$WORK/codex-skill"; mkdir -p "$w/cellwork" "$w/runs" "$w/sessions"
export PYTHONPATH="$REPO/integrations/verifier/src${PYTHONPATH:+:$PYTHONPATH}"
sed -e "s|__PROD__|$PROD|g" -e "s|__WORK__|$w|g" -e "s|__REPO__|$REPO|g" \
  "$PROD/fixtures/59-codex.basket.yaml.tmpl" > "$w/basket.yaml"
run_json 0 "$w/bench.out" "bench prod-codex-skill basket (fake codex spawn, 2 variants x 3 reps)" -- \
  catacomb bench "$w/basket.yaml" --sessions-dir "$w/sessions" --runs-dir "$w/runs" --manifest "$w/m.jsonl"

echo "== prod.59 codex-skill: manifest non-vacuity: 6 entries, exit 0, session ids present =="
rc=0; python3 - "$w/m.jsonl" <<'PY' || rc=$?
import json, sys
entries = [json.loads(l) for l in open(sys.argv[1]) if l.strip()]
bad = []
if len(entries) != 6:
    bad.append("expected 6 manifest entries, got %d" % len(entries))
for e in entries:
    rid = e.get("run_id", "?")
    if e.get("exit_code") != 0:
        bad.append("%s: exit_code=%r" % (rid, e.get("exit_code")))
    if not e.get("session_id"):
        bad.append("%s: no session_id" % rid)
if bad:
    print("\n".join(bad), file=sys.stderr)
    sys.exit(1)
PY
record "$rc" "manifest has 6 entries, all exit 0, all carry a session id (non-vacuous evidence)"

echo "== prod.59 codex-skill: verify_emit.py scores verifier.pass 1 in baseline, 0 in degraded -> ann:verifier.pass regression gate (exit 1) =="
run_json 1 "$w/regress.json" "baseline vs degraded -> verifier.pass artifact gate" -- \
  catacomb regress --runs-dir "$w/runs" \
  --baseline label:basket=prod-codex-skill,variant=baseline \
  --candidate label:basket=prod-codex-skill,variant=degraded --json
rc=0; python3 - "$w/regress.json" <<'PY' || rc=$?
import json, sys
rep = json.load(open(sys.argv[1]))
f = rep.get("findings", [])
ann = any(x.get("metric") == "ann:verifier.pass" and x.get("verdict") == "regression" for x in f)
if not ann:
    print("no ann:verifier.pass regression finding; findings:", file=sys.stderr)
    for x in f:
        print("  ", {k: x.get(k) for k in ("scope", "name", "metric", "verdict", "detail")}, file=sys.stderr)
    sys.exit(1)
print("ann:verifier.pass regression finding present (1 -> 0)")
PY
record "$rc" "regress gates on ann:verifier.pass dropping from baseline (1) to degraded (0)"

echo "== prod.59 codex-skill: baseline-r1 evidence -> soft grep (LOGGED, never gated) + NO skill node (HARD, gated) =="
rundir="$w/runs/bench-prod-codex-skill-skill-baseline-r1"
if [ -f "$rundir/session.jsonl" ] && grep -q '\.agents/skills/e2e-emit/SKILL\.md' "$rundir/session.jsonl"; then
  pass "soft grep: baseline rollout's function_call arguments reference .agents/skills/e2e-emit/SKILL.md (LOGGED, not gated)"
else
  pass "soft grep: no SKILL.md reference found in baseline rollout (LOGGED, not gated -- Codex has no skill-invocation event to assert on)"
fi

snap="$w/export.snap.jsonl"
run_json 0 "$w/export.out" "export baseline-r1 evidence dir -> jsonl graph snapshot" -- \
  catacomb export "$rundir" --out "$snap"
rc=0; if grep -q '"type":"skill"' "$snap"; then rc=1; fi
record "$rc" "baseline codex graph snapshot contains NO \"type\":\"skill\" node (Codex has no skill-invocation event; artifact + soft grep are the documented substitute)"
