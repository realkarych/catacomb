# Releasing catacomb

Catacomb is distributed through several channels, all driven by a single tag
push:

- **GitHub Releases** — cross-compiled archives for linux/darwin/windows on
  amd64/arm64
- **Homebrew cask** (macOS) — `realkarych/homebrew-tap`
- **APT** (Debian/Ubuntu) — `realkarych/catacomb-apt`, served via GitHub Pages
- **Docker** — `ghcr.io/realkarych/catacomb`
- **`go install`** — straight from the tagged source

The pipeline lives in
[`.github/workflows/publish.yml`](../.github/workflows/publish.yml).

Alongside the archives, each release publishes supply-chain evidence: a
`checksums.txt`, syft SBOMs for the archives, keyless cosign Sigstore bundles
(`*.sigstore.json`, verifiable with `cosign verify-blob --bundle`) over the
checksum file and the SBOMs, and keyless cosign signatures on the GHCR images
(built with buildx, which attaches its default build-provenance attestation on
push).

## Cutting a release

One pre-tag checklist item: after a green live validation against a newer Claude
Code or Codex CLI — the live E2E gate
([`e2e-live.yml`](../.github/workflows/e2e-live.yml)) or an equivalent local live
run — bump the matching tested-version ceiling (`TestedClaudeCodeVersion` /
`TestedCodexVersion` in [`ingest/drift`](../ingest/drift/drift.go)) in the same
PR that proved the newer CLI green, rather than trailing it as a separate chore,
so the shipped version-watchlist warning reflects what the release was actually
validated against. This is the one-line release-checklist item promised by
[ADR-0025](adr/0025-capture-format-drift-detection.md).

```sh
git tag v0.2.0
git push origin v0.2.0
```

Pushing a `v*.*.*` tag runs `publish.yml` end to end with no manual step:
`verify` refuses the tag unless its commit is an ancestor of `master` and
carries green required checks, then goreleaser publishes every channel, and
`verify-channels` asserts the tap cask, GHCR `:latest` digest, and release
assets all match the tag. A weekly `channels-watch.yml` re-checks the cask
against the latest release and files a `release-desync` issue on drift.

To re-run a publish for an existing tag (e.g. after a transient failure),
dispatch `publish.yml` from the Actions tab with the `tag` input — re-pushing
the tag won't re-trigger it. In the "Use workflow from" dropdown you must select
the tag ref itself (e.g. `v0.2.0`), not the `master` branch, or the `release`
environment's tag-only deployment policy will block the goreleaser job.

The `release` environment is scoped to `v*.*.*` tag refs and holds the
channel secrets; it has **no** required reviewers, because the automatic
`verify` gate already refuses unqualified tags. Configure the ref policy once:

```sh
gh api -X PUT repos/realkarych/catacomb/environments/release \
  --input - <<'JSON'
{"reviewers":[],"deployment_branch_policy":{"protected_branches":false,"custom_branch_policies":true}}
JSON
gh api -X POST repos/realkarych/catacomb/environments/release/deployment-branch-policies \
  -f 'name=v*.*.*' -f 'type=tag'
```

## One-time setup

These must exist before the first release.

### Repositories

- **`realkarych/homebrew-tap`** — shared Homebrew tap (already exists). The
  release job writes the cask `Casks/catacomb.rb` into it. One-time, on the
  first cask release: delete the stale `Formula/catacomb.rb` from the tap;
  existing formula users migrate with
  `brew uninstall catacomb && brew install --cask catacomb`.
- **`realkarych/catacomb-apt`** — APT host. Create it with a `gh-pages` branch
  and enable GitHub Pages (branch `gh-pages`, root). The job publishes the
  signed apt repo there.

### Secrets

Set these in the catacomb repo (Settings -> Secrets and variables -> Actions):

| Secret | Purpose |
| --- | --- |
| `HOMEBREW_TOKEN` | PAT with write access to `realkarych/homebrew-tap` |
| `DEB_TOKEN` | PAT with write access to `realkarych/catacomb-apt` |
| `APT_GPG_PRIVATE_KEY` | GPG private key that signs the APT repo |

`GITHUB_TOKEN` is provided automatically and is used for the GitHub Release and
the GHCR image push.

### GPG key for APT

```sh
gpg --full-generate-key                       # RSA 4096, no expiry
gpg --armor --export-secret-keys <KEYID>      # value for APT_GPG_PRIVATE_KEY
gpg --armor --export <KEYID> > public.key     # commit to catacomb-apt root
```

The public key must be reachable at
`https://realkarych.github.io/catacomb-apt/public.key` so users can trust the
repo.

### GHCR visibility

After the first image push, set the `catacomb` package visibility to public in
the package settings on GitHub.
