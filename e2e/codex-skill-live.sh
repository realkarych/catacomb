#!/usr/bin/env bash
# Live Codex skill-substitute basket cell wrapper. The task's workspace.cmd
# stages the real e2e-emit skill dir into the cell's .agents/skills/ (Codex's
# repo-scoped discovery path: .agents/skills/<name>/SKILL.md, per
# developers.openai.com/codex/skills) and copies this wrapper + verify_emit.py
# in from E2E_DIR. Codex has no dedicated skill-invocation event, so unlike
# skill.sh's --allowedTools "Skill,Write" (which forces a structural Skill tool
# call on the Claude side), there is nothing to force here: the model reads
# SKILL.md as an ordinary file (an exec_command in the rollout) only if it
# chooses to follow the prompt, and the ONLY thing catacomb can verify
# afterward is the resulting artifact plus a soft grep of that exec_command's
# arguments — both handled downstream by the basket's verify hook and by
# run.sh's (logged, never gated) grep. gpt-5.4-mini at low reasoning effort
# matches the other live codex wrappers (codex-live.sh); --skip-git-repo-check
# lets the cell run outside a git repository; stdin is closed so codex takes
# the prompt from argv instead of waiting on a pipe.
set -euo pipefail
mkdir -p out
rm -f out/result.csv
exec codex exec -m gpt-5.4-mini -c model_reasoning_effort=low --skip-git-repo-check --json "$SKILL_INSTRUCTION" < /dev/null
