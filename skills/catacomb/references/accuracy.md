# Accuracy: checkpoints, verifiers, and baselines

Three independent fidelity upgrades for a working basket. Reach for each when the
gate tells you to: **checkpoints** when prompt churn scrambles the step axis
(`steps_trusted false`), a **verifier** when observables pass but the answer is
wrong, and **baselines and trends** to pin a golden group and watch it drift over
time. New to the vocabulary? Read [concepts.md](concepts.md) first.

## 1. Phase checkpoints (MCP `mark`)

When the change under test rewrites prompts, the raw step order shifts and
`regress` loses the axis it aligns on — `steps_trusted` goes `false` and the
step-level numbers rest on a shaky mapping. Named phases fix the axis: the agent
marks its own phase boundaries, and the phases you declare under `checkpoints:`
become stable comparison rows anchored to names you chose, not to step order. See
[phases and checkpoints](concepts.md#phases-and-checkpoints) and
[coverage and steps_trusted](concepts.md#coverage-and-steps_trusted).

Wire it in three moves — no hand-rolled server; `catacomb mcp` ships the marker
tool.

**Write the MCP config file** (`mcp.json`):

```json
{"mcpServers":{"catacomb":{"command":"catacomb","args":["mcp"]}}}
```

The server named `catacomb` exposing `mark` surfaces to the agent as
`mcp__catacomb__mark`.

**Pass it to `claude` in `agent.sh`** with `--mcp-config` (full script in
[setup.md](setup.md)):

```sh
exec claude -p "${PROMPT}" \
  --mcp-config mcp.json \
  --strict-mcp-config \
  --output-format stream-json --verbose
```

`--strict-mcp-config` keeps the benchmark to just this server, so the run stays
reproducible on another machine.

**Tell the agent when to mark** — one CLAUDE.md instruction line:

```text
Call mcp__catacomb__mark with name: plan before planning, and with name: tests.pass after the tests pass.
```

**Declare the phases on the task** so `regress` expects them:

```yaml
tasks:
  - id: <task-id>
    cmd: ["./agent.sh"]
    checkpoints: [plan, tests.pass]
```

A declared checkpoint the agent never marked surfaces immediately as a
`missing checkpoints:` warning on stderr, so broken wiring is loud, not silent.
The marker call is a pure acknowledgement — the reducer synthesizes the phase
from the tool-call record, so the tool needs no configuration and fails open.

## 2. Per-task verifier

A green run is not a correct answer: exit `0` with clean observables says the
agent ran, not that it produced the right output. A verifier closes that gap — it
reads the artifacts the cell produced and scores them against ground truth, and
its verdict rides the same statistical gate as every other metric.

Declare the files the task produces and a command that scores them:

```yaml
tasks:
  - id: sql
    cmd: ["./agent.sh"]
    artifacts: ["out/result.csv"]
    verify:
      cmd: ["python3", "./verify_sql.py"]
      env: { GOLDEN: "/fixtures/golden.csv" }
```

After each cell, `bench` captures the declared `artifacts:` into the evidence
dir, then runs `verify.cmd` as a plain `exec` (argv, no shell) with the cell
context on its environment; `verify.env` layers the ground-truth path on top,
outside the workdir. The verifier reads the captured artifacts, decides pass/fail,
and emits one scores line:

```python
import os
from catacomb_verifier import Cell, emit, compare_tables

cell = Cell.from_env()
res = compare_tables(cell.artifact("out/result.csv"), os.environ["GOLDEN"], ordered=False)
emit(passed=res.equal, tool="verify_sql", tool_version="1")
```

`emit(passed=…)` writes the reserved `verifier.pass` key (`1`/`0`). A **failing
check is `verifier.pass: 0` at exit `0`**; a non-zero exit means the verifier
itself broke — keep verifiers total so a real regression never hides behind a
crashed comparator. The [`catacomb_verifier` SDK](../../../integrations/verifier)
reduces the contract to `Cell.from_env()` + `emit()`.

Re-score recorded evidence offline — no agent spend — with `verify`:

```sh
catacomb verify basket.yaml --runs-dir runs
```

Iterate the verifier here at zero cost, then gate as usual.

## 3. Baselines and trends

`regress` compares two groups in the moment; a **baseline** gives the comparison a
memory. Pin a golden group under a name so it survives later label churn, then
`--record` every gate run into that name's append-only history and replay the
drift.

**Pin the golden group** — `baseline set` resolves the selector against the
evidence dirs now and stores the matching run IDs under a name:

```sh
catacomb baseline set demo-main --label basket=demo,variant=main --runs-dir runs --db demo.db
```

**Record every comparison** against the pinned baseline (the `--record` flag needs
a `name:` baseline):

```sh
catacomb regress --runs-dir runs --db demo.db \
  --baseline name:demo-main \
  --candidate label:basket=demo,variant=candidate --record
```

Each run appends to `demo-main`'s history without changing the verdict or exit
code, so CI leaves a durable, replayable record for free.

**Replay the drift** — `trends` reads that history oldest-first, one row per
recorded comparison, so slow drift is visible even when no single run tripped the
gate:

```sh
catacomb trends demo-main --db demo.db
```

For the accuracy-vs-cost trade-off, `catacomb trends <name> --pareto` turns each
recorded comparison into a point — accuracy from `verifier.pass`, cost from
`cost_usd` — and leads the table with the Pareto frontier (the candidates no other
row beats).

**Re-pin after an accepted regression.** When you have reviewed a `regression` and
accept the new numbers, re-run `baseline set` with the same name to re-pin the
baseline to the new golden group. The old band stops firing, and the version stamp
refreshes so an upgraded step-key scheme never silently misaligns pinned evidence.
