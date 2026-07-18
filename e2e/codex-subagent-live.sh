#!/usr/bin/env bash
# Codex live-subagent basket cell wrapper (the optional live codex leg).
#
# `catacomb bench` execs this command directly, with NO shell, using the task's
# `dir` as the working directory. The per-variant prompt arrives via the PROMPT
# environment variable (set by the basket's `variant.env`); `set -u` makes an
# unset PROMPT a loud failure the run.sh manifest assertions catch, not an empty
# prompt. --json streams codex-exec events to stdout, where bench peeks the
# {"type":"thread.started","thread_id":...} line and resolves the cell's rollout
# under --sessions-dir (default ~/.codex/sessions), same peek/resolve contract as
# codex-live.sh. -c agents.max_threads=2 raises the delegation ceiling above the
# single spawn_agent call the baseline prompt asks for (default agents.max_threads
# is 6, so this is a floor, not a real constraint — it documents intent: this cell
# expects at most one child thread). Unlike codex-live.sh, delegation itself is
# NOT flag-forced: Codex has no CLI switch for spawn_agent, only a discretionary
# model choice the PROMPT text drives (baseline instructs it, degraded forbids
# it) — see basket-codex-subagent.yaml's header for why the live verdict stays
# logged rather than gated. --skip-git-repo-check lets the cell run outside a git
# repository, and stdin is closed so codex takes the prompt from argv instead of
# waiting on a pipe. gpt-5.4-mini at low reasoning effort keeps the leg cheap;
# codex reports token counts, no dollar cost.
set -euo pipefail

exec codex exec -c agents.max_threads=2 -m gpt-5.4-mini -c model_reasoning_effort=low --skip-git-repo-check --json "$PROMPT" < /dev/null
