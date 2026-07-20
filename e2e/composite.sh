#!/usr/bin/env bash
# Live composite mega-basket cell wrapper. One session, THREE distinct phases plus a
# skill and a verifiable artifact:
#   - main agent (tools: Task + mcp__catacomb__mark) marks the OUTER "orchestration"
#     phase (top-level enclosing step key) and MUST delegate the inner work (it has no
#     Write/Skill/Bash);
#   - the general-purpose subagent marks the INNER "work" phase TWICE (occ 0 and occ 1
#     — the reducer auto-assigns occurrence via assignOccurrences), invokes the e2e-emit
#     skill (Skill node), and writes out/result.csv (verifier.pass).
# The reduced baseline graph therefore carries subagent + three phase keys (distinct
# name orchestration vs work; occurrence work#0 vs work#1; top-level vs subagent
# enclosing step key) + skill node + verifier.pass simultaneously. degraded gives the
# main agent Write/Skill/mark and NO Task: it does the work inline, marks only the
# outer phase, and skips the skill — dropping the subagent node (primary gate) plus the
# skill node and the two subagent-scoped work phases. The workspace stages the skill
# dir + copies the wrapper/verifier. mark is served by `catacomb mcp` via mcp.json.
# Sonnet for reliable multi-step obedience. Isolation flags match the other wrappers.
set -euo pipefail
mkdir -p out
rm -f out/result.csv
exec claude -p "${COMPOSITE_INSTRUCTION}" \
	--model "${CHILD_MODEL:-claude-sonnet-5}" \
	--output-format stream-json --verbose \
	--mcp-config "$PWD/mcp.json" --strict-mcp-config \
	--setting-sources project \
	--allowedTools "${COMPOSITE_TOOLS}"
