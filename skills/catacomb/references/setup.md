# Setup: scaffold and first bench

This is the flagship workflow: scaffold `agent.sh` and `basket.yaml` for a real
agent, confirm catacomb can capture its transcript, size the basket, and run the
first `bench`. New to the vocabulary (basket, cell, evidence)? Read
[concepts.md](concepts.md) first.

The whole method rests on one discipline: **one variable — the change under test —
moves between `main` and `candidate`; everything else is held fixed.** Same tasks,
same model, same flags, same working tree. If two things change at once, the verdict
can't tell you which one moved the metric.

## 1. Inspect how the project invokes `claude`

Before scaffolding anything, find how this project already runs `claude` and reuse
it. Look in the obvious places — run scripts, `Makefile` targets, CI workflows, the
`README` — for an existing `claude -p …` invocation, and note its model and flags.
Reusing them keeps the benchmark faithful to how the agent actually runs, and it
gives you a concrete baseline to hold fixed while you move the one variable under
test.

If there is no existing invocation, the scaffold below is a safe default: the
cheapest capable model plus the flags catacomb needs to observe the run.

## 2. Scaffold `agent.sh`

`agent.sh` is the agent under test. `bench` runs it once per cell, passing the
cell's variant `env` (here, `PROMPT`) into the process:

```sh
#!/usr/bin/env bash
set -euo pipefail
exec claude -p "${PROMPT}" \
  --model claude-haiku-4-5 \
  --output-format stream-json \
  --verbose \
  --strict-mcp-config \
  --setting-sources project
```

Why these flags:

- `--output-format stream-json --verbose` is how catacomb finds the session
  transcript — the runner peeks the child's stdout for the stream-json `session_id`
  and the terminal `result` event, then resolves the recorded transcript from it.
  Without them there is nothing to capture.
- `--strict-mcp-config --setting-sources project` keep user-scope plugins and hooks
  out of the benchmark, so a run made on your machine compares against a run made on
  someone else's. Only project-scoped config participates.

Make it executable:

```sh
chmod +x agent.sh
```

## 3. Author `basket.yaml`

The basket is the tasks × variants × reps matrix. Frame the change under test as
`main` (baseline) vs `candidate`, with the moving variable expressed in the variant
`env` — here, two prompts:

```yaml
basket: <name>
reps: 5
tasks:
  - id: <task-id>
    cmd: ["./agent.sh"]
    dir: .
variants:
  - id: main
    env:
      PROMPT: "<baseline prompt>"
  - id: candidate
    env:
      PROMPT: "<candidate prompt>"
```

Each `task × variant × rep` combination is one **cell**. This basket expands to
`1 task × 2 variants × 5 reps = 10` cells. `dir: .` runs the agent from the project
root; point it wherever the agent expects to start.

## 4. Pre-flight the matrix (no spend)

Confirm the matrix expands to what you expect before spending a cent:

```sh
catacomb bench basket.yaml --dry-run
```

`--dry-run` prints the cell-expansion table and exits without executing anything.
Check the cell count against your mental math (10, above). A surprising count means a
task or variant is mis-specified — fix the YAML before you run for real.

## 5. Confirm capture on one cheap cell

The #1 silent setup failure is an `agent.sh` that runs fine but never emits a
resolvable transcript — so catacomb records a cell with no
[evidence](concepts.md#basket-cell-evidence), and everything downstream quietly has
nothing to read. Catch it now, on one cheap cell, before you pay for the full
matrix.

`reps` lives in the basket — there is no `--reps` flag — so write a one-cell
`preflight.yaml`:

```yaml
# preflight.yaml — one cheap cell to confirm capture
basket: preflight
reps: 1
tasks:
  - id: <task-id>
    cmd: ["./agent.sh"]
    dir: .
variants:
  - id: main
    env:
      PROMPT: "<any short prompt>"
```

Run it and check that evidence landed:

```sh
catacomb bench preflight.yaml --runs-dir runs
ls runs/*/ | head
```

If a `runs/<run-id>/` directory with a transcript appears, capture works — move on.
If it doesn't, the agent isn't emitting stream-json the runner can resolve; take it
to [troubleshoot.md](troubleshoot.md) before spending on the full basket.

## 6. Size the basket

`reps`, tasks, and model trade off statistical power against cost:

- **`reps`** drives statistical power and cost linearly. More reps tighten the noise
  band, so a real difference is more likely to clear it; they also multiply spend
  one-for-one.
- **Tasks** gate which comparisons can fire. The **paired** analysis needs **≥5
  tasks** to run at all — below that, catacomb falls back to the wider unpaired band
  and paired verdicts stay silent.
- **Model** is the largest cost lever. Benchmark on the cheapest model that still
  exercises the behavior under test.

Start at `reps: 5` on the cheapest adequate model, and scale up only when verdicts
come back `insufficient` — the signal that the groups are too small or too noisy to
decide. The small-`k` sensitivity tradeoffs behind these defaults are worked out in
[ADR-0023](../../../docs/adr/0023-regression-gate-sensitivity-at-small-k.md).

## 7. Run the first bench and hand off

Record the full matrix, then gate the candidate group against the baseline group:

```sh
catacomb bench basket.yaml --runs-dir runs
catacomb regress --runs-dir runs \
  --baseline label:basket=<name>,variant=main \
  --candidate label:basket=<name>,variant=candidate
```

`regress` compares the two groups and maps the verdict to its
[exit code](concepts.md#exit-codes) — `0` ok, `1` regression, `2` operational error.

To read what the report is telling you — verdicts, noise bands, coverage, and what to
do about each — go to [read-report.md](read-report.md).
