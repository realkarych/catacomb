#!/usr/bin/env bash
# Fixture-emitting agent for the hermetic production scenarios. Like
# e2e/hermetic/agent.sh but transcript-only and template-parameterised: it renders
# the template named by SCENARIO_TMPL (a per-variant transcript) into the projects
# dir bench reads, and prints the two stream-json lines bench needs on stdout. When
# EMIT_CSV is set it also writes that literal text to out/result.csv so a verify
# hook can score the artifact (used by the composite scenario). No API spend.
set -euo pipefail
sid="$(python3 -c 'import uuid; print(uuid.uuid4())')"
mkdir -p out "$HERMETIC_PROJECTS/hermetic"
[ -z "${EMIT_CSV:-}" ] || printf '%s' "$EMIT_CSV" > out/result.csv
sed "s/__SESSION_ID__/$sid/g" "$SCENARIO_TMPL" > "$HERMETIC_PROJECTS/hermetic/$sid.jsonl"
printf '{"type":"system","session_id":"%s"}\n' "$sid"
printf '{"type":"result","session_id":"%s","total_cost_usd":0.0}\n' "$sid"
