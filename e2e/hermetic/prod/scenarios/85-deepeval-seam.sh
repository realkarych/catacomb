#!/usr/bin/env bash
set -euo pipefail
echo "== prod.85 deepeval-seam: catacomb export -> catacomb-deepeval author mode =="
w="$WORK/deepeval-seam"; mkdir -p "$w"
fixture="$REPO/integrations/deepeval/tests/testdata/seam_session.jsonl"
snap="$w/s.jsonl"
run_json 0 "$w/export.out" "export seam transcript -> jsonl snapshot" -- \
  catacomb export "$fixture" --to jsonl --out "$snap"
rc=0
python3 -m catacomb_deepeval "$snap" >"$w/adapter.json" 2>"$w/adapter.err" || rc=$?
record "$rc" "catacomb-deepeval author mode reads the export (exit 0)"
rc=0; python3 - "$w/adapter.json" <<'PY' || rc=$?
import json, sys
d = json.load(open(sys.argv[1]))
names = [t["name"] for t in d.get("tools_called", [])]
errs = []
if names != ["Bash", "mcp__fs__read"]:
    errs.append("tools_called names=%r want [Bash, mcp__fs__read]" % names)
if not d.get("input"):
    errs.append("input empty")
if not d.get("actual_output"):
    errs.append("actual_output empty")
if errs:
    print("\n".join(errs), file=sys.stderr); sys.exit(1)
PY
record "$rc" "export->adapter carries input, actual_output, tools_called [Bash, mcp__fs__read]"
