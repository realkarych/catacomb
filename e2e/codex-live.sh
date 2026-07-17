#!/usr/bin/env bash
# Codex-basket cell wrapper (the optional live codex leg).
#
# `catacomb bench` execs this command directly, with NO shell, using the task's
# `dir` as the working directory. The per-variant prompt arrives via the PROMPT
# environment variable (set by the basket's `variant.env`); `set -u` makes an
# unset PROMPT a loud failure the run.sh manifest assertions catch, not an empty
# prompt. --json streams codex-exec events to stdout, where bench peeks the
# {"type":"thread.started","thread_id":...} line and resolves the cell's rollout
# under --sessions-dir (default ~/.codex/sessions — exactly where a live
# `codex exec` writes it). --skip-git-repo-check lets the cell run outside a git
# repository, and stdin is closed so codex takes the prompt from argv instead of
# waiting on a pipe. gpt-5.4-mini at low reasoning effort keeps the leg cheap
# (~$0.05-equivalent per full run; codex reports token counts, no dollar cost).
set -euo pipefail

exec codex exec -m gpt-5.4-mini -c model_reasoning_effort=low --skip-git-repo-check --json "$PROMPT" < /dev/null
