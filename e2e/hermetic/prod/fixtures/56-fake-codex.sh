#!/usr/bin/env bash
# Fake codex CLI for the hermetic codex-bench scenario. Stands in for
# `codex exec --experimental-json` as a basket task cmd: it derives a unique
# thread id from a per-invocation counter file under FAKE_SESSIONS_DIR (bench
# runs cells serially, so a plain read-increment-write is race-free), renders
# the rollout template named by FAKE_ROLLOUT_TMPL (per-variant, from
# variants[].env) into the YYYY/MM/DD day-dir tree bench resolves against, and
# prints the thread.started / item.completed / turn.completed event lines a
# real codex exec stream emits — thread.started is what bench's codex peeker
# reads the session id from. Rollout timestamps are stamped near NOW (__TS__ /
# __EPOCH__), so event times land inside the bench run window instead of at a
# hardcoded past instant. bash/sed/date only; zero network, zero API spend.
set -euo pipefail
seq_file="$FAKE_SESSIONS_DIR/.seq"
mkdir -p "$FAKE_SESSIONS_DIR"
n=$(( $(cat "$seq_file" 2>/dev/null || echo 0) + 1 ))
printf '%s' "$n" > "$seq_file"
tid="$(printf '019f6b85-627f-7be3-81dc-ae85638902%02d' "$n")"
ts="$(date -u +%Y-%m-%dT%H:%M:%S)"
epoch="$(date -u +%s)"
day="$FAKE_SESSIONS_DIR/$(date -u +%Y/%m/%d)"
mkdir -p "$day"
sed -e "s/__THREAD_ID__/$tid/g" -e "s/__TS__/$ts/g" -e "s/__EPOCH__/$epoch/g" \
  "$FAKE_ROLLOUT_TMPL" > "$day/rollout-$(date -u +%Y-%m-%dT%H-%M-%S)-$tid.jsonl"
printf '{"type":"thread.started","thread_id":"%s"}\n' "$tid"
printf '{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"probe done"}}\n'
printf '{"type":"turn.completed","usage":{"input_tokens":1200,"cached_input_tokens":200,"output_tokens":100}}\n'
