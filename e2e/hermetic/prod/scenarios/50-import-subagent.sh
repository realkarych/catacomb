#!/usr/bin/env bash
# Scenario 50 — import-path subagent: the bench (headless `claude -p`) path can never
# emit sidechain lines, so the bench scenarios (20) synthesize the "subagent" node from
# a bench-shaped transcript. This scenario proves the OTHER entry point: a realistic
# INTERACTIVE Claude Code session — which DOES carry top-level isSidechain/agentId/
# parent_tool_use_id/subagent_type on every sidechain line — is ingested by
# `catacomb import --transcript <session.jsonl>` (no agent spawn) into a bench-cell
# evidence dir, then `catacomb replay --export-jsonl` reduces it. The snapshot must
# contain the full "type":"subagent" graph node AND carry subagent_type=general-purpose
# (the P0 fix: import reads subagent_type stamped top-level on the sidechain lines).
# Sourced by run.sh with lib.sh loaded and PROD/WORK/HERMETIC_* exported. Zero API spend.
set -euo pipefail
echo "== prod.50 import-subagent: ingest an interactive session (with sidechain) via import =="
w="$WORK/import-subagent"; mkdir -p "$w/runs"
sid="import-subagent"
transcript="$w/$sid.jsonl"
sed "s/__SESSION_ID__/$sid/g" "$PROD/fixtures/import-subagent.jsonl.tmpl" > "$transcript"
cp "$PROD/fixtures/import-subagent.basket.yaml.tmpl" "$w/basket.yaml"

run_json 0 "$w/import.out" "import interactive transcript into fresh evidence (no agent spawn)" -- \
  catacomb import "$w/basket.yaml" --task delegate --variant interactive \
  --transcript "$transcript" --runs-dir "$w/runs"

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
record "$rc" "imported graph snapshot contains a \"type\":\"subagent\" node (bench path cannot)"
rc=0; grep -q '"type":"subagent"[^}]*"subagent_type":"general-purpose"' "$snap" || rc=1
record "$rc" "imported subagent node carries subagent_type=general-purpose (P0 fix on import path)"
rc=0; grep -q '"name":"Agent"' "$snap" || rc=1
record "$rc" "imported graph names the delegating tool Agent"
