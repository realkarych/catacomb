#!/usr/bin/env bash
# Live redaction basket cell wrapper. The workspace.cmd seeds secret.txt with a
# runtime-assembled FAKE GitHub token (matches redact's reGitHubToken but is never
# committed secret-shaped). This wrapper instructs the agent to cat that file and
# copy its content into out/result.csv, so the token flows through a Bash
# tool_result payload deterministically — emission depends on the model running
# cat, NOT on it reproducing a literal secret. bench captures + redacts at capture
# time; run.sh then asserts the raw token is absent and the placeholder present.
# Sonnet for reliable two-step (cat then write) obedience. Isolation flags match
# the other live wrappers.
set -euo pipefail
mkdir -p out
rm -f out/result.csv
exec claude -p "Run exactly: cat secret.txt — then write its exact content as the only line of out/result.csv using the Write tool. Do not paraphrase or alter the content." \
	--model "${CHILD_MODEL:-claude-sonnet-5}" \
	--output-format stream-json --verbose \
	--setting-sources project --strict-mcp-config \
	--allowedTools "Bash(cat:*),Write"
