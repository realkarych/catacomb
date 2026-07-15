# Catacomb concepts

Shared vocabulary for the catacomb workflows. For the full narrative, see the
[guide](../../../docs/guide/concepts.md).

## Basket, cell, evidence

A basket is the YAML matrix of tasks × variants × reps that defines what to run. Each
combination in that matrix is a **cell**, run as a plain local process — no daemon, no
network. Every run is written to `runs/<run-id>/` as a secret-redacted **evidence**
directory: the transcripts plus a `meta.json` carrying the run's labels, exit code, and
cost. Evidence is the only thing later stages read, so a run that produced no evidence
did not happen as far as the gate is concerned.

## Phases and checkpoints

An agent can name the phases of its own run by calling the MCP `mark` tool as it works.
Phases you declare under `checkpoints:` become stable comparison rows in `regress`,
anchored to the names you chose rather than to raw step order. That anchoring is what
keeps a comparison robust to prompt churn, which otherwise re-keys steps and scrambles
the alignment.

## Verdicts and noise bands

`regress` compares each candidate metric against a **noise band** derived from the
baseline group, not against a single baseline run. A metric inside the band is `ok`; one
that falls outside it is a `regression`; a metric without enough support to decide is
`insufficient`. The comparison is always group-vs-group, never a single side-by-side
diff, which is why each side needs several reps.

## Coverage and steps_trusted

`coverage` reports how much of the step/phase axis actually aligned across the runs being
compared. `steps_trusted` is false when prompt churn degraded that alignment, meaning the
step-level numbers rest on a shaky mapping. A false `steps_trusted` is the signal to add
checkpoints, which re-anchor the comparison to named phases.

## Exit codes

The exit code of `regress` is the gate: `0` ok · `1` regression · `2` operational error.
CI keys the merge decision off that code alone. Treat `2` as "the gate could not run,"
distinct from `1` which is a real regression to investigate.
