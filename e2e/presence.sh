#!/usr/bin/env bash
# Presence-basket cell wrapper (PV-6b methodology).
#
# `catacomb bench` execs this command directly, with NO shell, using the task's
# `dir` as the working directory. The per-variant instruction arrives via the
# PHASE_INSTRUCTION environment variable (set by the basket's `variant.env`); a
# `baseline` variant tells the agent to mark the `verify` checkpoint, a
# `degraded` variant tells it not to. `set -u` makes an unset PHASE_INSTRUCTION a
# loud failure the run.sh manifest assertions catch, rather than a silent
# baseline-shaped prompt.
#
# The MCP config is resolved from THIS script's own location so the wrapper works
# regardless of the caller's cwd. The mark tool is served by `catacomb mcp`
# (mcp.json), so `catacomb` must be on PATH during the live run.
# --strict-mcp-config loads ONLY the servers from --mcp-config (just catacomb) and
# ignores any ambient user/project MCP config, so local and CI runs are identical.
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"

exec claude -p "Write a haiku about the sea (three short lines). ${PHASE_INSTRUCTION}" \
	--model claude-haiku-4-5 \
	--output-format stream-json \
	--verbose \
	--mcp-config "${here}/mcp.json" \
	--strict-mcp-config \
	--allowedTools "mcp__catacomb__mark"
