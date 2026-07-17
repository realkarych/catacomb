#!/usr/bin/env bash
# Fake codex CLI for the hermetic codex-skill-substitute scenario (59). Same
# thread-id/rollout-rendering mechanics as fixtures/56-fake-codex.sh (bash/sed/
# date only, zero network): derives a unique thread id from a per-invocation
# counter file under FAKE_SESSIONS_DIR, renders the rollout template named by
# FAKE_ROLLOUT_TMPL into the YYYY/MM/DD day-dir tree bench resolves against, and
# prints the thread.started/item.completed/turn.completed event lines a real
# `codex exec --json` stream emits. One addition 56 does not need: when
# FAKE_ARTIFACT is set, this script writes that literal text to out/result.csv
# in the cell's cwd (the basket's `dir: __WORK__/cellwork`), standing in for
# whatever a real codex run of the e2e-emit skill would have produced; when
# unset (the degraded variant) NO artifact is written at all, so
# e2e/verify_emit.py fails closed on a missing file rather than a wrong one.
# The cellwork dir is REUSED across cells (bench runs them serially), so any
# stale out/result.csv from a prior cell is always removed first — otherwise a
# degraded cell that writes nothing would silently inherit a PREVIOUS cell's
# passing artifact.
# Codex has no dedicated skill-invocation event (see
# docs/internal/plans/2026-07-17-full-live-validation.md Task 13 and the
# codex-capabilities research it cites) — the rendered rollout's exec_command
# reading .agents/skills/e2e-emit/SKILL.md is the only representation of "the
# skill fired" a codex rollout can ever carry, which is exactly what this
# fixture pins for the scenario's soft-grep assertion. bash/sed/date only;
# zero network, zero API spend.
set -euo pipefail
seq_file="$FAKE_SESSIONS_DIR/.seq"
mkdir -p "$FAKE_SESSIONS_DIR"
n=$(( $(cat "$seq_file" 2>/dev/null || echo 0) + 1 ))
printf '%s' "$n" > "$seq_file"
tid="$(printf '019f6b85-627f-7be3-81dc-ae85638920%02d' "$n")"
ts="$(date -u +%Y-%m-%dT%H:%M:%S)"
epoch="$(date -u +%s)"
day="$FAKE_SESSIONS_DIR/$(date -u +%Y/%m/%d)"
mkdir -p "$day"
sed -e "s/__THREAD_ID__/$tid/g" -e "s/__TS__/$ts/g" -e "s/__EPOCH__/$epoch/g" \
  "$FAKE_ROLLOUT_TMPL" > "$day/rollout-$(date -u +%Y-%m-%dT%H-%M-%S)-$tid.jsonl"
mkdir -p out
rm -f out/result.csv
if [ -n "${FAKE_ARTIFACT:-}" ]; then
  printf '%s' "$FAKE_ARTIFACT" > out/result.csv
fi
printf '{"type":"thread.started","thread_id":"%s"}\n' "$tid"
printf '{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"done"}}\n'
printf '{"type":"turn.completed","usage":{"input_tokens":900,"cached_input_tokens":100,"output_tokens":%s}}\n' "${FAKE_TOKENS_OUT:-120}"
