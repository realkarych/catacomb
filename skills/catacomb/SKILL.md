---
name: catacomb
description: Use when working with catacomb, the offline regression gate for Claude Code agents — setting up a basket and agent.sh, running `bench`, wiring the CI merge gate, reading a `regress` report (verdicts, noise bands, coverage), adding phase checkpoints or per-task verifiers, pinning baselines/trends, importing interactive sessions, or troubleshooting install/capture issues.
---

# Catacomb

Catacomb is an offline regression gate for Claude Code agents. You change a prompt,
a skill, an MCP tool, or CLAUDE.md, and catacomb runs the same tasks repeatedly under
the old setup and the new one, compares the two groups with small-sample statistics,
and maps the verdict to a CI exit code. No daemon, no network — plain local files.

Pipeline: `bench → reduce → step/phase keys → aggregate → regress`. You mostly touch
the two ends: `bench` runs a basket and records secret-redacted evidence; `regress`
compares a baseline group against a candidate group and exits `0` (ok) / `1`
(regression) / `2` (operational error).

Requirements to check first: `catacomb version` prints **v0.2.0 or newer** (a build
with no `bench`/`regress` is a stale pre-pivot install), and `claude` is on PATH and
signed in (`claude -p hello`). `bench` spends real money through Claude Code.

## Pick the workflow

| The engineer wants to… | Read |
|---|---|
| Start gating an agent — scaffold `agent.sh` + `basket.yaml`, run the first `bench` | `references/setup.md` |
| Turn a passing local gate into a CI merge gate | `references/ci-gate.md` |
| Understand a `regress` report or decide what to do about a verdict | `references/read-report.md` |
| Make the gate more trustworthy — checkpoints, verifiers, baselines/trends | `references/accuracy.md` |
| Feed a hand-run interactive (TUI) session into the gate | `references/import.md` |
| Fix install, sign-in, format-drift, or cost problems | `references/troubleshoot.md` |

New to the vocabulary (basket, cell, phase, noise band, `steps_trusted`)? Start with
`references/concepts.md`.
