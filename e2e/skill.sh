#!/usr/bin/env bash
# Live skill basket cell wrapper. The task's workspace.cmd stages the real e2e-emit
# skill dir into the cell's .claude/skills/ so --setting-sources project discovers it,
# and copies this wrapper + verify_emit.py in from E2E_DIR. baseline invokes the skill
# (the Skill tool -> a Skill step node; the skill writes the CATACOMB-SKILL-OK token to
# out/result.csv); degraded writes the SAME token inline with the Write tool (no Skill
# node — the seeded STEP regression). Both produce the correct artifact, so verify stays
# green on both — the only signal is the dropped Skill step. Sonnet for reliable
# multi-step obedience. Isolation flags (--setting-sources project --strict-mcp-config)
# match the other live wrappers so local runs match CI; --setting-sources project is what
# loads the staged project skill.
set -euo pipefail
mkdir -p out
rm -f out/result.csv
exec claude -p "${SKILL_INSTRUCTION}" \
	--model "${CHILD_MODEL:-claude-sonnet-5}" \
	--output-format stream-json --verbose \
	--setting-sources project --strict-mcp-config \
	--allowedTools "Skill,Write"
