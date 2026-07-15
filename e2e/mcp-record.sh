#!/usr/bin/env bash
# Live MCP basket cell wrapper: a real stdio handshake between `claude -p` and the
# e2e MCP server (e2e/mcp-e2ekit/server.py). The task's workspace.cmd stages the
# server dir into the cell and renders its ABSOLUTE path into a per-cell mcp.json,
# then copies this wrapper + verify_emit.py in from E2E_DIR. baseline calls the
# record tool (mcp__e2ekit__record -> a general MCP step node; the server persists
# the value to out/result.csv, which verify_emit.py scores into verifier.pass);
# degraded uses no tool (no MCP node AND no artifact — BOTH the dropped step node
# and the verifier failure gate). --mcp-config + --strict-mcp-config load ONLY the
# e2ekit server; --setting-sources project keeps ambient user-scope config out so
# local runs match CI, matching the other live wrappers.
#
# E2EKIT_OUT is exported as an ABSOLUTE path so the server subprocess claude spawns
# writes to THIS cell's out/result.csv no matter what cwd the server runs in; the
# verifier then reads it back from the same absolute artifact path.
set -euo pipefail
mkdir -p out
rm -f out/result.csv out/mcp-record.txt
export E2EKIT_OUT="$PWD/out/result.csv"
exec claude -p "${MCP_INSTRUCTION}" \
  --model "${CHILD_MODEL:-claude-sonnet-5}" \
  --output-format stream-json --verbose \
  --mcp-config "$PWD/mcp.json" --strict-mcp-config \
  --setting-sources project \
  --allowedTools "mcp__e2ekit__record"
