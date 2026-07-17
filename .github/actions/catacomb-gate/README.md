# catacomb-gate

Gate a pull request on agent-pipeline regressions and put the verdict where
reviewers look. This composite action installs a pinned, checksum-verified
[catacomb](https://github.com/realkarych/catacomb) release, optionally benches
the PR's basket, runs `catacomb regress`, upserts a **sticky PR comment** with
the markdown verdict, and fails the check on regression.

All report rendering happens inside the catacomb binary (`regress --format
markdown` / `--format json`, tested to the repo's 100% coverage gate); the
action's shell only installs, invokes, and pipes. Decision record:
[ADR-0033](../../../docs/adr/0033-github-action.md).

## Usage

```yaml
name: catacomb gate

on:
  pull_request:

permissions:
  contents: read
  pull-requests: write # for the sticky verdict comment

jobs:
  gate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@9c091bb21b7c1c1d1991bb908d89e4e9dddfe3e0 # v7.0.0
        with:
          persist-credentials: false
      - uses: realkarych/catacomb/.github/actions/catacomb-gate@master # pin to a tag/SHA
        with:
          version: v0.2.0
          basket: eval/basket.yaml
          baseline-bundle: eval/baseline.bundle
          baseline: label:variant=main
          candidate: label:variant=pr
          runs-dir: ${{ runner.temp }}/catacomb-runs
          github-token: ${{ secrets.GITHUB_TOKEN }}
```

> **Cost warning — bench on PRs spends real API budget.** When `basket` is set
> and `candidate-runs-dir` is not, every PR run executes the basket's cells
> against your agent CLI. Size the basket (and its `reps`) for PR budgets, or
> bench elsewhere and hand the evidence in via `candidate-runs-dir`.

<!-- two separate callouts; the comment keeps MD028 quiet -->

> **Baseline restore — a bundle is the recommended path.** Commit (or fetch) an
> exported baseline bundle and pass it as `baseline-bundle`; the action runs
> `catacomb baseline import` before gating. This needs a catacomb `version`
> that ships `baseline import` — on older releases the action fails with
> catacomb's own unknown-command error. Alternatively, point `runs-dir`/`db` at
> a store that already holds the baseline evidence and select it with
> `baseline`.

## Inputs

| Input | Required | Default | Description |
| --- | --- | --- | --- |
| `version` | yes | — | catacomb release tag to install (e.g. `v0.2.0`). Pin it. Ignored when `catacomb-bin` is set. |
| `baseline` | yes | — | Baseline selector forwarded to `regress --baseline` (`label:k=v[,k=v...]` or `name:<baseline>`). |
| `candidate` | yes | — | Candidate selector forwarded to `regress --candidate` (same forms). With `basket`, select the freshly benched runs by their variant labels (e.g. `label:variant=pr`). |
| `basket` | no | `""` | Basket YAML to bench for candidate evidence. Empty skips bench. |
| `candidate-runs-dir` | no | `""` | Pre-benched evidence dir. When set, bench is skipped and this dir becomes the evidence dir for the whole gate (baseline import lands there; regress resolves both selectors from it), overriding `runs-dir`. |
| `baseline-bundle` | no | `""` | Baseline bundle to restore via `catacomb baseline import` before gating. |
| `db` | no | `""` | SQLite store path forwarded as `--db` (used by `name:` selectors and baseline import). Empty uses catacomb's default. |
| `runs-dir` | no | `""` | Evidence dir forwarded as `--runs-dir` to bench, baseline import, and regress. Empty uses catacomb's default. |
| `reps` | no | `""` | Exported to the bench step as the `REPS` env var. `catacomb bench` has **no** `--reps` flag — reps are authored in the basket YAML — but commands the basket spawns inherit the environment, so a basket written to read `$REPS` picks it up. No effect otherwise. |
| `model` | no | `""` | Exported to the bench step as the `MODEL` env var, inherited by the commands the basket spawns (e.g. `claude --model "$MODEL"`). No effect on baskets that ignore it. |
| `strict` | no | `"false"` | Forward `--strict`: fail on insufficient data (exit 1); refuse baselines with missing/mismatched version stamps (exit 2). |
| `comment` | no | `"true"` | Upsert the sticky PR comment (only in `pull_request` context). |
| `github-token` | no | `${{ github.token }}` | Token for the release download and the PR comment. Needs `pull-requests: write` when commenting. |
| `catacomb-bin` | no | `""` | Testing/advanced: path to a local catacomb binary to use instead of downloading a release (skips download and verification). Used by this repo's hermetic self-test. |

## Outputs

| Output | Description |
| --- | --- |
| `verdict` | catacomb's `overall_verdict` (`ok`, `regression`, `improvement`, `notable`, `insufficient`), or `operational` when the gate could not run (exit 2). |
| `exit-code` | The `catacomb regress` exit code (see below). |
| `report-json` | Path to the machine-readable regress report (`regress --format json`). Empty file when the gate could not run. |

## Exit-code semantics

The action's final step re-raises catacomb's exit code, so the check:

- **passes on `0`** — no regression;
- **fails on `1`** — regression detected (with `strict`, also insufficient
  data): a red verdict in the comment;
- **fails on `2`** — the gate **could not run** (operational error: broken
  selector, missing baseline, stale store, …). The comment says "gate could
  not run" — it is never rendered as a regression.

The verdict comment posts **before** the exit code is enforced, so a failing
gate still explains itself on the PR.

## Sticky comment

The comment body is exactly catacomb's `regress --format markdown` output,
prefixed with a hidden marker `<!-- catacomb-gate:<baseline-identity> -->`
(the `baseline` selector, or the bundle filename when only `baseline-bundle`
is given). Re-runs find the marker and update the same comment instead of
stacking new ones. One comment is maintained per baseline identity, so two
gates against different baselines keep separate comments. Only comments
authored by a bot account are candidates for the update (the default
`GITHUB_TOKEN` posts as `github-actions[bot]`), so a user who plants the
marker in their own comment cannot get it overwritten by — or mistaken
for — the gate's verdict. Consequently, a personal access token passed as
`github-token` posts comments as a **User**, which this bot-only matching
never finds, so every run stacks a new comment — stick with the default
`${{ github.token }}` (`GITHUB_TOKEN`) or a GitHub App token.

## Supply chain

The install step downloads the release archive for the runner's OS/arch from
GitHub Releases, verifies its SHA-256 against the release's `checksums.txt`,
and — when the release ships a `checksums.txt.sigstore.json` bundle and
`cosign` is available on the runner — cosign-verifies the checksum file
against the repo's keyless publish identity. Missing cosign or a missing
bundle downgrades gracefully to checksum-only verification; a checksum
mismatch always fails. Linux and macOS runners are the tested path (Windows
support is best-effort via the zip archive).

## Self-test

[.github/workflows/action-selftest.yml](../../workflows/action-selftest.yml)
exercises this action hermetically on every PR: it builds catacomb from
source, stages a deterministic fixture runs-dir, and drives the gate through
the `catacomb-bin` seam — a seeded regression must fail with exit 1 and render
the `**Verdict: ❌ regression**` headline; an identical candidate must pass
with exit 0. No API spend, no release download, no PR comments.
