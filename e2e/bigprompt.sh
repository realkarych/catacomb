#!/usr/bin/env bash
# tokens_in continuous-axis wrapper. Builds a controllable-size input prompt by
# repeating a filler line PROMPT_REPEATS times, then asks for a one-sentence
# answer so tokens_out stays flat while tokens_in scales with PROMPT_REPEATS.
# baseline/baseline2 use PROMPT_REPEATS=0 (tiny input); bigprompt uses a large
# value so tokens_in trips the continuous gate against baseline while the A-vs-A
# (baseline vs baseline2) stays flat. Haiku is fine: input tokens are cheap and
# the axis is deterministic (input-token count is a near-deterministic function of
# the prompt). Isolation flags match the other live wrappers so local runs match CI.
set -euo pipefail
filler=""
i=0
while [ "$i" -lt "${PROMPT_REPEATS:-0}" ]; do
	filler="${filler}Context line ${i}: catacomb evaluates agentic pipelines offline against stored baselines and gates regressions statistically. "
	i=$((i + 1))
done
exec claude -p "${filler}Ignore the context above. In one short sentence, what is a hash function?" \
	--model "${CHILD_MODEL:-claude-haiku-4-5}" \
	--output-format stream-json \
	--verbose \
	--strict-mcp-config \
	--setting-sources project
