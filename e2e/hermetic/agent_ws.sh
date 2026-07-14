#!/usr/bin/env bash
set -euo pipefail
[ ! -f marker ] || exit 7
[ -z "${CATACOMB_PATCH:-}" ] || exit 8
touch marker
sid="$(python3 -c 'import uuid; print(uuid.uuid4())')"
mkdir -p "$HERMETIC_PROJECTS/hermetic"
sed "s/__SESSION_ID__/$sid/g" "$HERMETIC_TDIR/transcript.jsonl.tmpl" \
  > "$HERMETIC_PROJECTS/hermetic/$sid.jsonl"
printf '{"type":"system","session_id":"%s"}\n' "$sid"
printf '{"type":"result","session_id":"%s","total_cost_usd":0.0}\n' "$sid"
