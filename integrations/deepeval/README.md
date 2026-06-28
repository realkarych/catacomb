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
| --- | --- |
| `input` | First `user_prompt` node `payload.input` |
| `actual_output` | Last `assistant_turn` node `payload.output` |
| `tools_called[].name` | `tool_call` / `mcp_call` node `name` |
| `tools_called[].input_parameters` | Node `payload.input` (JSON object) |
| `tools_called[].output` | Node `payload.output` parsed as JSON |
| `expected_tools` | Loaded from `--expected` JSON file |

## Trace metrics (LLM-judged)

`StepEfficiencyMetric` and `TaskCompletionMetric` are available via the
`--trace-metrics` CLI flag. They replay the catacomb session as a DeepEval
`@observe` trace (agent root span + ordered `llm`/`tool` child spans), then
run the metrics over the resulting trace dict. An Anthropic LLM judge scores
the trace — no real agent re-execution happens.

### Requirements

- `pip install 'catacomb-deepeval[deepeval]'` (deepeval 4.x)
- `ANTHROPIC_API_KEY` set in the environment

The flag exits immediately with an error (exit 2) when `ANTHROPIC_API_KEY` is
absent — no silent network attempts are made.

### Running

```bash
export ANTHROPIC_API_KEY=sk-ant-...

catacomb-deepeval session.jsonl --run <run-id> --trace-metrics
```

Example output:

```text
task_completion score: 0.950
task_completion reason: The agent successfully listed the files as requested.
step_efficiency score: 0.800
step_efficiency reason: Only one tool call was needed for this simple task.
```

Exit 0 means both metrics are at or above threshold (0.5 by default); exit 1
means at least one metric failed.

### How the replay works

The module `catacomb_deepeval.trace_replay` reads the ordered `steps` list
(populated by the reader from `assistant_turn` / `tool_call` / `mcp_call`
nodes) and replays them inside a nested `@observe` call tree:

```text
@observe(type="agent")   ← catacomb-session root span
  @observe(type="llm")   ← each assistant_turn node
  @observe(type="tool")  ← each tool_call / mcp_call node
  ...
```

`trace_manager.create_nested_spans_dict(root)` converts the in-memory span
tree into the dict that `StepEfficiencyMetric` / `TaskCompletionMetric` read
from `test_case._trace_dict`.

### Using the API directly

```python
from catacomb_deepeval.reader import load_jsonl, parse_session
from catacomb_deepeval.trace_replay import (
    make_anthropic_judge,
    run_task_completion,
    run_step_efficiency,
)

lines = load_jsonl("session.jsonl")
session = parse_session(lines, "run-001")
judge = make_anthropic_judge()

tc_score, tc_reason = run_task_completion(session, model=judge)
se_score, se_reason = run_step_efficiency(session, model=judge)
```

The default keyless `ToolCorrectnessMetric` path (no `--trace-metrics`) is
unchanged and requires no API key.

The `--argument-correctness` flag is reserved for a future
`ArgumentCorrectnessMetric` integration. It currently validates that
`ANTHROPIC_API_KEY` is set and exits with an error if it is not.
