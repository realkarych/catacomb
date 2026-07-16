#!/usr/bin/env bash
# Sub-transcript-emitting agent for scenario 70. A dedicated twin of emit.sh (the
# shared emitter is intentionally left untouched): it reproduces the on-disk shape
# real `claude -p`/interactive writes for a delegated subagent — the MAIN transcript
# in the projects dir plus a SEPARATE sub-transcript under a `<sid>/subagents/`
# directory, exactly what bench's resolveTranscripts globs and reduces together via
# loadGraphOffline(main, subs). It renders the main template named by SCENARIO_TMPL
# into $HERMETIC_PROJECTS/hermetic/$sid.jsonl, and — only when WITH_SUBTRANSCRIPT=1 —
# renders SUBAGENT_TMPL (the subagent's own isSidechain/agentId turns, carrying a
# top-level subagent_type and a tool_use) into
# $HERMETIC_PROJECTS/hermetic/$sid/subagents/agent-<id>.jsonl, so the delegating
# variant writes a real second file and the inline variant writes none. Prints the
# two stream-json lines bench reads on stdout. No API spend.
set -euo pipefail
sid="$(python3 -c 'import uuid; print(uuid.uuid4())')"
mkdir -p out "$HERMETIC_PROJECTS/hermetic"
sed "s/__SESSION_ID__/$sid/g" "$SCENARIO_TMPL" > "$HERMETIC_PROJECTS/hermetic/$sid.jsonl"
if [ "${WITH_SUBTRANSCRIPT:-0}" = "1" ]; then
	aid="$(python3 -c 'import uuid; print(uuid.uuid4().hex[:16])')"
	subdir="$HERMETIC_PROJECTS/hermetic/$sid/subagents"
	mkdir -p "$subdir"
	sed -e "s/__SESSION_ID__/$sid/g" -e "s/__AGENT_ID__/$aid/g" "$SUBAGENT_TMPL" \
		> "$subdir/agent-$aid.jsonl"
fi
printf '{"type":"system","session_id":"%s"}\n' "$sid"
printf '{"type":"result","session_id":"%s","total_cost_usd":0.0}\n' "$sid"
