#!/usr/bin/env bash
# Fake codex CLI for the hermetic codex-mcp scenario. A variant of
# 56-fake-codex.sh: same thread-id/rollout-render/event-stream shape (derives a
# unique thread id from a per-invocation counter file under
# FAKE_SESSIONS_DIR, renders the rollout template named by FAKE_ROLLOUT_TMPL
# into the YYYY/MM/DD day-dir tree bench resolves against, prints
# thread.started / item.completed / turn.completed), PLUS: when FAKE_ARTIFACT
# is set, writes that literal text to out/result.csv (relative to the cell's
# workdir) the way a real mcp__e2ekit__record call would have persisted it via
# the e2e MCP server — the fake CLI stands in for the whole codex-plus-MCP-
# server round trip, not just the CLI, since there is no live MCP handshake to
# drive offline. The baseline/baseline2 variants set FAKE_ARTIFACT; degraded
# leaves it unset, so no artifact is produced (verify_emit.py then scores an
# absent file as a fail, not just a wrong one). The task's `dir` is a single
# fixed cellwork directory shared across every cell in the basket (bench runs
# cells serially), so out/result.csv is unconditionally removed before the
# conditional write below — otherwise a degraded cell run right after a
# baseline cell (rep-major interleaving) would inherit the PRIOR cell's
# artifact instead of genuinely lacking one. bash/sed/date only; zero
# network, zero API spend.
set -euo pipefail
mkdir -p out
rm -f out/result.csv
seq_file="$FAKE_SESSIONS_DIR/.seq"
mkdir -p "$FAKE_SESSIONS_DIR"
n=$(( $(cat "$seq_file" 2>/dev/null || echo 0) + 1 ))
printf '%s' "$n" > "$seq_file"
tid="$(printf '019f6b85-cafe-7be3-81dc-ae85638905%02d' "$n")"
ts="$(date -u +%Y-%m-%dT%H:%M:%S)"
epoch="$(date -u +%s)"
day="$FAKE_SESSIONS_DIR/$(date -u +%Y/%m/%d)"
mkdir -p "$day"
sed -e "s/__THREAD_ID__/$tid/g" -e "s/__TS__/$ts/g" -e "s/__EPOCH__/$epoch/g" \
  "$FAKE_ROLLOUT_TMPL" > "$day/rollout-$(date -u +%Y-%m-%dT%H-%M-%S)-$tid.jsonl"
if [ -n "${FAKE_ARTIFACT:-}" ]; then
  printf '%s' "$FAKE_ARTIFACT" > out/result.csv
fi
printf '{"type":"thread.started","thread_id":"%s"}\n' "$tid"
printf '{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"done"}}\n'
printf '{"type":"turn.completed","usage":{"input_tokens":900,"cached_input_tokens":100,"output_tokens":50}}\n'
