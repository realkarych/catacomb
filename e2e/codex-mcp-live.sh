#!/usr/bin/env bash
# Live Codex MCP basket cell wrapper: a real stdio MCP server handshake between
# `codex exec` and the e2e MCP server (e2e/mcp-e2ekit/server.py) — the exact
# server the Claude live-MCP basket (mcp-record.sh) already speaks to. Codex
# registers the server per-invocation via `-c mcp_servers.<name>.*` config
# overrides instead of a rendered mcp.json, so no ~/.codex/config.toml mutation
# is needed and each cell stays isolated (the same isolation posture
# --mcp-config/--strict-mcp-config give the claude cell).
#
# The task's workspace.cmd stages mcp-e2ekit/ (and this script + verify_emit.py)
# from E2E_DIR into the cell dir before this runs, so `$PWD/mcp-e2ekit/server.py`
# is this cell's own copy — an absolute path is required because codex spawns
# the server from wherever it feels like, not necessarily this cwd.
#
# baseline/baseline2 instruct the model to call mcp__e2ekit__record (a codex
# mcp_call step node; the server persists the value to out/result.csv, scored
# by verify_emit.py into verifier.pass); degraded instructs no tool use at all
# (no MCP node and no artifact). E2EKIT_OUT is exported as an ABSOLUTE path so
# the server subprocess codex spawns writes to THIS cell's out/result.csv no
# matter what cwd the server process itself runs in.
#
# `set -u` (via -euo pipefail) makes an unset MCP_INSTRUCTION a loud failure
# the run.sh manifest assertions catch, not a silently empty prompt.
# --skip-git-repo-check / closed stdin / gpt-5.4-mini at low reasoning effort
# match the existing codex-live.sh posture.
set -euo pipefail
mkdir -p out
rm -f out/result.csv
export E2EKIT_OUT="$PWD/out/result.csv"
mcp_args="[\"$PWD/mcp-e2ekit/server.py\"]"
exec codex exec -m gpt-5.4-mini -c model_reasoning_effort=low --skip-git-repo-check --json \
  -c mcp_servers.e2ekit.command=python3 \
  -c mcp_servers.e2ekit.args="$mcp_args" \
  "$MCP_INSTRUCTION" < /dev/null
