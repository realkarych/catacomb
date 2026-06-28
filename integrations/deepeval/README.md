# catacomb-deepeval

Offline, deterministic process-scoring for catacomb sessions using
[DeepEval](https://github.com/confident-ai/deepeval)'s `ToolCorrectnessMetric`.
No LLM judge, no API key required for the default name-match mode.

## What it is

`catacomb-deepeval` reads catacomb's lossless JSONL export (nodes + run lines)
and feeds the extracted `input`, `actual_output`, and `tools_called`
directly into DeepEval's `ToolCorrectnessMetric`. The metric is deterministic
and runs fully offline (name-match mode).

This directory is outside the Go 100%-coverage gate and the no-comments rule;
both apply only to Go packages.

## Producing the input

Export a session from catacomb with payload access enabled:

```bash
catacomb export --to jsonl \
  --run <run-id> \
  --allow-payload-access \
  --out session.jsonl
```

Without `--allow-payload-access` the payload fields are absent; tool
`input_parameters` and `output` will be `null` in that case, which limits
evaluation to name-match mode.

## Writing expected-tools JSON

Three equivalent forms are supported:

**Name array** (simplest):

```json
["Bash", "mcp__fs__read"]
```

**Object array**:

```json
[{"name": "Bash"}, {"name": "mcp__fs__read"}]
```

**Envelope**:

```json
{"tools": ["Bash", "mcp__fs__read"]}
```

To discover what tools a session actually called (author mode):

```bash
catacomb-deepeval session.jsonl --run <id>
```

This prints the session as JSON without running any metric, letting you copy
the `tools_called` list into your expected file.

## Running the deterministic eval

```bash
pip install 'catacomb-deepeval[deepeval]'

catacomb-deepeval session.jsonl \
  --run <run-id> \
  --expected expected.json
```

Exit 0 means PASS; exit 1 means FAIL.

To also match on input parameters (still offline):

```bash
catacomb-deepeval session.jsonl --run <id> \
  --expected expected.json \
  --match input
```

## Field mapping

| DeepEval field | catacomb source |
|---|---|
| `input` | First `user_prompt` node `payload.input` |
| `actual_output` | Last `assistant_turn` node `payload.output` |
| `tools_called[].name` | `tool_call` / `mcp_call` node `name` |
| `tools_called[].input_parameters` | Node `payload.input` parsed as JSON object |
| `tools_called[].output` | Node `payload.output` parsed as JSON |
| `expected_tools` | Loaded from `--expected` JSON file |

## LLM-judge metrics (out of scope)

DeepEval's `StepEfficiencyMetric` and `TaskCompletionMetric` require an LLM
judge and an Anthropic API key. They also need traces injected via
`@observe` / replay. A sketch:

```python
# Conceptual only — not implemented in this package
from deepeval.integrations.anthropic import observe_anthropic
client = observe_anthropic(anthropic.Anthropic())
# replay catacomb session here, then run TaskCompletionMetric
```

The `--argument-correctness` flag is reserved for a future `ArgumentCorrectnessMetric`
integration (not yet implemented). It currently validates that `ANTHROPIC_API_KEY` is
set and exits with an error if it is not.
