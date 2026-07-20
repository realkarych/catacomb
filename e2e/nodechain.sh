#!/usr/bin/env bash
# nodes-axis wrapper: builds a chain of CHAIN_LEN files where each names the next
# (link1 -> link2 -> ... -> linkN whose content is DONE), then makes the agent follow
# the chain with the Read tool. The chain is STRUCTURALLY forced: the agent cannot know
# file i+1 without reading file i, so it must make CHAIN_LEN sequential Read tool calls
# -> CHAIN_LEN step nodes in the reduced graph. baseline uses CHAIN_LEN=1 (few nodes);
# manynodes uses a long chain (many nodes) -> the seeded `nodes` continuous regression.
# Node count is a structural axis, independent of billing mode and distinct from
# tokens_out. Only the Read tool is allowed. Isolation flags match the other live
# wrappers so local runs match CI. The ${CHAIN_LEN:?} expansion makes an unset CHAIN_LEN a loud failure.
set -euo pipefail
n="${CHAIN_LEN:?CHAIN_LEN must be set}"
rm -f link*.txt
i=1
while [ "$i" -lt "$n" ]; do
	printf 'link%d.txt\n' "$((i + 1))" >"link${i}.txt"
	i=$((i + 1))
done
printf 'DONE\n' >"link${n}.txt"
exec claude -p "Read the file link1.txt with the Read tool. Its content is the name of the next file to read. Keep reading each named file with the Read tool, one at a time in order, until a file's content is exactly DONE. Do not guess file names — only read the name each file gives you. Reply done when you reach DONE." \
	--model "${CHILD_MODEL:-claude-haiku-4-5}" \
	--output-format stream-json --verbose \
	--strict-mcp-config --setting-sources project \
	--allowedTools "Read"
