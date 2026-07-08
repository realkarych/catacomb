# Getting started

Catacomb is an offline eval gate: you declare a basket of agent tasks, run it once per
variant, and gate the candidate against the baseline statistically. Everything happens
on your machine with plain files — no daemon, no service, no network.

## Install

Install the latest release:

```sh
go install github.com/realkarych/catacomb/cmd/catacomb@latest
```

Or build from source (requires Go 1.26):

```sh
make build        # produces bin/catacomb
```

Other channels (Homebrew, Docker, APT, release archives) are listed in the
[README](../../README.md).

## Declare a basket

A basket is a YAML matrix of `tasks × variants × reps`. Each combination is one *cell*;
the task `cmd` must emit stream-json so catacomb can find the session:

```yaml
# checkout.yaml
basket: checkout
reps: 5
tasks:
  - id: work-task
    cmd: ["claude", "-p", "work the checkout task", "--output-format", "stream-json"]
variants:
  - id: main
  - id: candidate
    setup: ["git checkout candidate-branch"]
```

## Run it

```sh
catacomb bench checkout.yaml
```

Each cell runs as a local process. After a cell exits, catacomb resolves the session's
transcripts under `~/.claude/projects`, rebuilds the execution graph, and writes a
secret-redacted evidence directory to `~/.catacomb/runs/<run-id>/` plus a line in
`checkout.yaml.manifest.jsonl`. On success it prints a copy-pasteable `regress` command.

## Gate the candidate

```sh
catacomb regress \
  --baseline label:basket=checkout,variant=main \
  --candidate label:basket=checkout,variant=candidate
```

`regress` scans the evidence directories, aggregates each group, and compares them
statistically — presence and error rates, duration, cost, tokens — printing a verdict
table. The exit code is the gate: `0` ok, `1` regression, `2` operational error. Pin
the golden group by name (`catacomb baseline set`) and add `--record` to accumulate
history for `catacomb trends`; see [Workflows](workflows.md).

## Wire checkpoints (recommended)

Give the agent the shipped MCP marker tool so it can name phases inside a run, and
declare the phases you expect in the basket (`checkpoints: [plan, tests.pass]`):

```json
{"mcpServers":{"catacomb":{"command":"catacomb","args":["mcp"]}}}
```

Pass that file to `claude --mcp-config`; a CLAUDE.md instruction tells the agent when
to call `mcp__catacomb__mark`. Phases give `regress` a stable comparison axis even when
prompts change. See [Concepts](concepts.md#phases-and-checkpoints).

## Look inside a single run

```sh
catacomb replay ~/.claude/projects/<project>/<session>.jsonl   # graph summary
catacomb diff run-a.jsonl run-b.jsonl                          # step-by-step diff
catacomb subgraph session.jsonl --phase plan                   # one phase's subgraph
catacomb export ~/.catacomb/runs/<run-id> --out run.jsonl      # JSONL graph snapshot
```

## Watch runs in a UI

Catacomb ships no viewer. To watch sessions live, feed them to a vendor substrate
through that vendor's first-party Claude Code plugin — Phoenix is the recommended
substrate ([ADR-0026](../adr/0026-form-factor-pivot-offline-eval-gate.md) §2).

## Next steps

- [Concepts](concepts.md) — the action graph, step keys, and phases
- [Workflows](workflows.md) — baselines, recorded history, trends, and external scores
- [CLI reference](cli.md) — all commands and flags
- [Privacy and operations](privacy-and-operations.md) — what is redacted, where, and
  troubleshooting
