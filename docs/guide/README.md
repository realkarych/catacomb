# Catacomb user guide

Catacomb is an offline eval gate for Claude Code agentic pipelines. It runs prompt
baskets, reduces the recorded transcripts into a canonical execution graph, derives
step and phase keys, aggregates metrics, and gates regressions against saved baselines
— all from local files, with no daemon and no network.

## 30-second quickstart

Install:

```sh
go install github.com/realkarych/catacomb/cmd/catacomb@latest
```

Run a basket and gate the candidate:

```sh
catacomb bench checkout.yaml
catacomb regress --baseline label:basket=checkout,variant=main \
  --candidate label:basket=checkout,variant=candidate
```

`bench` records a secret-redacted evidence directory per cell under `~/.catacomb/runs`;
`regress` compares the two groups statistically and exits `1` on a regression. Watching
runs live in a UI is delegated to a vendor substrate such as Phoenix
([ADR-0026](../adr/0026-form-factor-pivot-offline-eval-gate.md) §2).

## Contents

- [Getting started](getting-started.md) — install, first basket, first gate
- [Concepts](concepts.md) — the action graph, step keys, and phases
- [CLI reference](cli.md) — every command, flag, and argument
- [Configuration](configuration.md) — flags, environment variables, and defaults
- [Ingestion](ingestion.md) — how transcripts become graphs
- [Workflows](workflows.md) — benching, gating, baselines, trends, scores, diff, and export
- [Privacy and operations](privacy-and-operations.md) — redaction, evidence dirs, and troubleshooting
