# CI merge gate

Once the local gate from [setup.md](setup.md) passes, wire it into CI so a regression
blocks the merge instead of relying on someone remembering to run `regress` by hand. The
whole mechanism is one exit code: `regress` exits non-zero on a regression, that fails the
job, and a failed required check blocks the PR. On GitHub Actions the packaged
`catacomb-gate` composite action stands the whole job up in one step and posts the
verdict on the PR; the manual job below it is the same mechanism spelled out, and
ports to any CI at the end.

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
needs both halves of the reference: the baseline row and the pinned evidence dirs. Export
them as **one bundle**:

```sh
catacomb baseline export golden --db catacomb.db --runs-dir runs --out golden.tar.gz
```

The bundle is a single hash-verified `.tar.gz` carrying the row and every pinned run.
Store it where the job can retrieve it — an uploaded workflow artifact, a release asset,
or the object store your CI already uses — and re-export whenever the golden group is
re-pinned. Export is byte-deterministic (the same baseline always yields the same bytes),
so the artifact can be content-addressed and cached. Committing `catacomb.db` and the
`runs/` tree into the repository still resolves and remains a workable fallback, but it
bloats the repo, invites binary-database merge conflicts, and ties no integrity check
between the row and the evidence — prefer the bundle. See [accuracy.md](accuracy.md) for
pinning, re-pinning after an intended shift, and trend history in depth.

## The GitHub Action (primary path)

Catacomb ships the whole job as a composite action — `catacomb-gate`, living in the
catacomb repository under `.github/actions/catacomb-gate` (in-repo for now, not yet a
marketplace action). It installs a pinned, checksum-verified catacomb release,
optionally benches the PR's basket, runs `catacomb regress`, posts the verdict as a
**sticky PR comment** (the exact `regress --format markdown` output, updated in place
on re-runs), and re-raises the regress exit code so the check fails on a regression.
Reference it by its in-repo path:

```yaml
name: catacomb-gate
on: pull_request
permissions:
  contents: read
  pull-requests: write # sticky verdict comment
jobs:
  gate:
    runs-on: ubuntu-latest
    env:
      ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
    steps:
      - uses: actions/checkout@<sha>  # v7
        with:
          persist-credentials: false
      - uses: realkarych/catacomb/.github/actions/catacomb-gate@<sha>  # pin to a release tag/SHA
        with:
          version: v0.2.0           # catacomb release the gate runs on — pin it
          basket: basket.yaml       # benched on every PR; omit to gate pre-benched evidence
          baseline: name:golden
          candidate: label:basket=<name>,variant=candidate
          db: catacomb.db           # committed store holding the golden baseline
          runs-dir: runs            # evidence dir holding the baseline group
```

The committed `catacomb.db` + `runs/` pair is the same baseline-restore obligation as
the manual job below — the action does not conjure the baseline. A `baseline-bundle`
input that restores one via `catacomb baseline import` exists, but it needs a catacomb
release that ships `baseline import`; no tagged release carries it yet, so on current
releases that path fails with catacomb's unknown-command error. Other inputs worth
knowing: `candidate-runs-dir` (hand in pre-benched evidence and skip the bench),
`strict`, `comment: "false"` (no PR comment), and `reps`/`model` (exported to the
bench step's env for baskets written to read them). The action exposes `verdict`,
`exit-code`, and `report-json` outputs for downstream steps; the full input/output
table is in the action's README (`.github/actions/catacomb-gate/README.md`).

## The manual job (the underlying mechanism)

The action is thin shell around three catacomb invocations — install, `bench`,
`regress` — so when its inputs don't cover your shape (or you want to see exactly what
runs), write the job directly. Drop this in `.github/workflows/catacomb-gate.yml` as
the starting point. The action `@<sha>` pins are placeholders: this repo pins Actions
to commit SHAs, so resolve each one to the current release SHA for the tag noted in
the trailing comment before committing.

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
      - name: Fetch baseline bundle
        env:
          GH_TOKEN: ${{ github.token }}
        run: gh release download catacomb-baseline --pattern golden.tar.gz
      - name: Restore baseline
        run: catacomb baseline import golden.tar.gz --db catacomb.db --runs-dir runs
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

`go install` needs Go ≥ 1.26, which the `setup-go` step provides. The fetch step is
whatever your artifact store makes retrievable — a release asset (shown), a workflow
artifact, or an object-store download; the contract is only that `golden.tar.gz` exists
before the restore. `baseline import` verifies every file hash before landing anything —
a corrupted or tampered bundle fails the job with exit `2` instead of gating against
damaged evidence — and rewrites the baseline's recorded runs dir to the local `runs/`,
so `name:golden` resolves warning-free. The bench then writes the candidate group into
the same `runs/`, `regress` compares it against the imported `name:golden`, and the
final upload runs `if: always()` so the evidence is attached even when the gate fails —
that archive is what you download to read the report.

## Secrets

`bench` drives the real `claude` CLI, so the job needs auth as a repository secret. Set one
of:

- `ANTHROPIC_API_KEY` — API billing.
- `CLAUDE_CODE_OAUTH_TOKEN` — a Claude Pro/Max subscription (generate it with
  `claude setup-token`).

Pass whichever you set through the job's (or the bench step's) `env:`, as the
`ANTHROPIC_API_KEY` lines in both snippets above show — the action's bench step
inherits the job env. **`bench` spends real money on every PR**, so keep the CI
variant cheap.

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
secret to the environment, fetch the exported bundle and `catacomb baseline import` it at
job start so `name:golden` resolves, run `bench` to produce the candidate group and then
`regress` against the pinned baseline, and let the non-zero exit fail the job.
