#!/usr/bin/env bash
set -euo pipefail
sid="$(python3 -c 'import uuid; print(uuid.uuid4())')"
mkdir -p out "$HERMETIC_PROJECTS/hermetic"
sqlite3 -header -csv "$HERMETIC_DB" "$SQL_QUERY" > out/result.csv
sed "s/__SESSION_ID__/$sid/g" "$HERMETIC_TDIR/transcript.jsonl.tmpl" \
  > "$HERMETIC_PROJECTS/hermetic/$sid.jsonl"
printf '{"type":"system","session_id":"%s"}\n' "$sid"
printf '{"type":"result","session_id":"%s","total_cost_usd":0.0}\n' "$sid"
