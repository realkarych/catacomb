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

```sh
git tag v0.1.0
git push origin v0.1.0
```

Pushing a `v*.*.*` tag triggers `publish.yml`. You can also run it manually from
the Actions tab (`workflow_dispatch`) with a `tag` input.

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
