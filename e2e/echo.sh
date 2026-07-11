#!/usr/bin/env bash
# Step-axis cell wrapper for the presence basket's `echo` task.
#
# `catacomb bench` execs this directly, with NO shell, using the task's `dir` as the
# working directory. It runs ONE guaranteed Bash `echo` step — a stable,
# cross-variant step-key-eligible node the diff/subgraph/export/scores smokes need
# (mark calls are consumed into phase markers, not step nodes, and a haiku is
# otherwise tool-less). Kept single-tool (--allowedTools Bash) and short so tool
# obedience stays high, exactly like the mark-only haiku task.
#
# --strict-mcp-config + --setting-sources project isolate the child from ambient MCP
# and user-scope hooks/plugins so local runs match CI. The whole prompt IS the
# per-variant STEP_INSTRUCTION (variant.env): baseline/baseline2 carry the Bash `echo`
# directive plus the direct-call hardening; the degraded variant instead says to use
# no tools, so its echo step vanishes — a step-scope seeded regression, coherent with
# no contradicting wrapper text. `set -u` makes an unset STEP_INSTRUCTION a loud failure.
set -euo pipefail

exec claude -p "${STEP_INSTRUCTION}" \
	--model "${CHILD_MODEL:-claude-haiku-4-5}" \
	--output-format stream-json \
	--verbose \
	--strict-mcp-config \
	--setting-sources project \
	--allowedTools "Bash"
