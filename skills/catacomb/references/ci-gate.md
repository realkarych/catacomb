# CI merge gate

Once the local gate from [setup.md](setup.md) passes, wire it into CI so a regression
blocks the merge instead of relying on someone remembering to run `regress` by hand. The
whole mechanism is one exit code: `regress` exits non-zero on a regression, that fails the
job, and a failed required check blocks the PR. The steps below stand it up on GitHub
Actions first; the same three moves port to any CI at the end.

## What the gate needs

Three pieces:

- A **pinned baseline** — a golden group captured once and referenced by name, so the bar
  a PR is measured against does not move underneath it.
- The **candidate bench in CI** — each PR runs the same basket to produce a fresh
  candidate group.
- A **`regress` call whose exit code blocks the merge** — it compares candidate against
  the pinned baseline and fails the job on a regression.

## Pin the baseline once

Capture the golden group on a known-good tree and store it under a stable name:

```sh
catacomb baseline set golden --label basket=<name>,variant=main \
  --runs-dir runs --db catacomb.db
```

A **name** survives label churn: `regress --baseline name:golden` keeps resolving to this
group even as basket labels evolve, where a raw `label:` selector would silently re-match.
The command does not copy the evidence — it records the resolved run IDs and selector in
`--db`, and `regress` re-reads those `runs/` directories from disk at compare time. So CI
needs both halves of the reference: commit `catacomb.db` **and** the baseline's `runs/`
group (or publish the group as a retrievable artifact the job restores before `regress`).
Miss either and the baseline cannot resolve. See [accuracy.md](accuracy.md) for pinning,
re-pinning after an intended shift, and trend history in depth.

## The GitHub Actions job

Drop this in `.github/workflows/catacomb-gate.yml` as the starting point. The action
`@<sha>` pins are placeholders: this repo pins Actions to commit SHAs, so resolve each one
to the current release SHA for the tag noted in the trailing comment before committing.

```yaml
name: catacomb-gate
on: pull_request
jobs:
  gate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@<sha>  # v4
      - uses: actions/setup-go@<sha>  # v5
        with:
          go-version: '1.26'
      - name: Install catacomb
        run: go install github.com/realkarych/catacomb/cmd/catacomb@v0.2.0
      - name: Bench candidate
        env:
          ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
        run: catacomb bench basket.yaml --runs-dir runs
      - name: Regress against pinned baseline
        run: |
          catacomb regress --runs-dir runs --db catacomb.db \
            --baseline name:golden \
            --candidate label:basket=<name>,variant=candidate \
            --record
      - uses: actions/upload-artifact@<sha>  # v4
        if: always()
        with:
          name: catacomb-runs
          path: runs/
```

`go install` needs Go ≥ 1.26, which the `setup-go` step provides. The bench writes the
candidate group into `runs/`, `regress` compares it against `name:golden` from the
committed `catacomb.db`, and the final upload runs `if: always()` so the evidence is
attached even when the gate fails — that archive is what you download to read the report.

## Secrets

`bench` drives the real `claude` CLI, so the job needs auth as a repository secret. Set one
of:

- `ANTHROPIC_API_KEY` — API billing.
- `CLAUDE_CODE_OAUTH_TOKEN` — a Claude Pro/Max subscription (generate it with
  `claude setup-token`).

Pass whichever you set through the bench step's `env:`, exactly as the `ANTHROPIC_API_KEY`
line above shows. **`bench` spends real money on every PR**, so keep the CI variant cheap.

## Keeping CI cheap

Because every PR pays for a full candidate group, tune the CI basket for cost, not for the
richest signal:

- Lower **`reps`** — it drives cost linearly. Use the smallest count that still clears the
  noise band for the metrics you gate on.
- Use the **cheapest adequate model** in the CI variant — the model is the largest cost
  lever.
- Gate on the metrics that matter; you do not need to assert every axis, because the exit
  code already encodes the overall verdict.

`--record` appends each comparison to the `name:golden` baseline's history, accumulating
the series that `trends` replays later — see [accuracy.md](accuracy.md). On an ephemeral CI
runner those appended rows live only in that run's `catacomb.db` and are discarded when the
job ends unless you persist the DB back (commit it, or add `catacomb.db` to the uploaded
artifact), so durable `trends` history is something you build in a persistent store rather
than the PR gate.

## The gate

The merge decision keys off the [`regress` exit code](concepts.md#exit-codes) alone:

- `0` — ok, the job passes and the PR is mergeable.
- `1` — regression, the job fails and the merge is blocked. This is the gate doing its job.
- `2` — operational error: the gate could not run (missing baseline, unresolved evidence,
  a stale store). The non-zero code fails the job too, but it is not a regression — surface
  it and fix the setup; never paper over it or treat it as a pass.

Make the `gate` job a required status check on the protected branch so a `1` or `2`
actually holds the merge.

## Other CI

Any CI works — nothing here is GitHub-specific. Install the binary (a release archive or
`go install github.com/realkarych/catacomb/cmd/catacomb@<version>`), expose the same auth
secret to the environment, run `bench` to produce the candidate group and then `regress`
against the pinned baseline, and let the non-zero exit fail the job. Commit or restore
`catacomb.db` and the baseline `runs/` group the same way so `name:golden` resolves.
