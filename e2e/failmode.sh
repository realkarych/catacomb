#!/usr/bin/env bash
# Failure-mode basket cell wrapper: --fail-fast ($0, no model call) and
# --error-delta (a real Bash-tool error reduced from a live claude -p) coverage.
#
# `catacomb bench` execs this directly, with NO shell, inside the cell's working
# directory; the task's workspace.cmd copies this script in from E2E_DIR (same
# idiom as sql-live.sh/subagent.sh). `set -u` makes an unset FAILMODE a loud
# failure rather than a silent branch.
#
# FAILMODE=prefail exits 1 BEFORE any model call: run.sh benches this branch
# under --fail-fast so the whole basket stops after the very first cell, proving
# --fail-fast at true $0 (no claude invocation ever happens on this path).
#
# FAILMODE=clean and FAILMODE=toolerr each run a real `claude -p` (Haiku) that
# executes exactly one Bash(sh:*) command: clean succeeds (sh -c 'exit 0'),
# toolerr fails (sh -c 'exit 3'). Both let claude -p itself finish and exit 0 —
# only the Bash TOOL result carries the error, so the reduced graph's tool_call
# node status is what differs (offline is_error -> status "error", the same
# reducer path 95-reduce-edges.sh proves E2E-dark without this basket). That
# separation is what `regress --error-delta` gates on (total-scope error_rate).
# The prompt tells the toolerr agent the command is EXPECTED to fail so a cheap
# Haiku model does not spend turns retrying or "fixing" it.
set -euo pipefail

case "$FAILMODE" in
prefail)
	exit 1
	;;
clean)
	exec claude -p "Run exactly this one Bash command and nothing else, then reply done: sh -c 'exit 0'" \
		--model "${CHILD_MODEL:-claude-haiku-4-5}" \
		--output-format stream-json \
		--verbose \
		--setting-sources project \
		--strict-mcp-config \
		--allowedTools "Bash(sh:*)"
	;;
toolerr)
	exec claude -p "Run exactly this one Bash command and nothing else, then reply done: sh -c 'exit 3'. It is EXPECTED to fail with a nonzero exit code — that is the point of this task. Do NOT retry it, do NOT try to fix or work around the failure, just run it once and reply done." \
		--model "${CHILD_MODEL:-claude-haiku-4-5}" \
		--output-format stream-json \
		--verbose \
		--setting-sources project \
		--strict-mcp-config \
		--allowedTools "Bash(sh:*)"
	;;
*)
	printf 'failmode.sh: unknown FAILMODE=%s (want prefail|clean|toolerr)\n' "$FAILMODE" >&2
	exit 2
	;;
esac
