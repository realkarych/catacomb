#!/usr/bin/env bash
# Continuous-basket cell wrapper (PV-6b methodology).
#
# `catacomb bench` execs this command directly, with NO shell, using the task's
# `dir` as the working directory. The per-variant prompt arrives via the
# TASK_PROMPT environment variable (set by the basket's `variant.env`): a
# `baseline` variant asks for a one-sentence answer, a `verbose` variant asks for
# a long essay, driving a large, gateable `tokens_out` growth. This basket
# exercises the continuous (metric) gate, not checkpoints, so it needs no MCP
# server — and bare --strict-mcp-config (with no --mcp-config) loads NONE, keeping
# ambient user/project MCP config out of the child runs so local and CI runs are
# identical. `set -u` makes an unset TASK_PROMPT a loud failure, not an empty prompt.
set -euo pipefail

exec claude -p "${TASK_PROMPT}" \
	--model claude-haiku-4-5 \
	--output-format stream-json \
	--verbose \
	--strict-mcp-config
