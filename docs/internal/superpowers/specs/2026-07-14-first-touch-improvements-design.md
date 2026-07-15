# First-touch improvements — design

- **Date:** 2026-07-14
- **Status:** approved design, pending implementation plan
- **Scope:** release automation (`publish.yml`, repo environment settings, new watchdog workflow), basket path-resolution contract (Go, 0.MINOR), README amendments on top of the 2026-07-13 README-as-guide rewrite, `docs/` restructure
- **Builds on:** `2026-07-13-readme-guide-rewrite-design.md` (README/guide structure), ADR-0028 (SP-W workspace isolation)

## Problem

A four-persona first-touch audit (fresh agents restricted to the public surface:
README, `docs/`, executed install + tutorial + authoring a basket from scratch)
mapped the visitor funnel and found the happy path solid — install to first
`regress` verdict in 5–8 minutes, first basket written from docs alone with zero
edits, 0 broken links out of 92 — but four trust leaks:

1. **Release channels drift.** The `release` environment requires a manual
   approval, so a pushed tag can sit unpublished for hours (v0.1.0: 11 h 9 m)
   while `go install @latest` already serves the new tag. During that window
   brew installs a binary that lacks commands the README advertises (`verify`,
   `pack` were missing from cask 0.0.3 while README Step 4 depends on
   `verify`). A `go install` build reports `catacomb version` → `dev`, so even
   a correct install looks unversioned, and evidence stamps record
   `CatacombVersion: dev`.
2. **Two runtime traps in the basket contract that no lint catches.** (a) The
   base for resolving a relative `dir` is undocumented and only discoverable by
   spending agent budget. (b) Every doc example uses `["python3",
   "./verify_x.py"]`, which works inline (cwd = workdir) and silently breaks in
   offline `catacomb verify` (cwd = evidence dir) — the exact loop the docs
   promote. Also: required/optional/default status of top-level fields is
   undocumented, YAML type errors leak Go internals (`cannot unmarshal !!str
   into []string`), a single-variant basket runs silently although `regress`
   can never gate it, and `regress` repeats an identical transcript-version
   warning once per evidence dir (10× noise in the tutorial).
3. **README retention gaps.** No differentiation from promptfoo /
   LangSmith-Braintrust / Inspect; no maturity signal (no version badge, no
   pre-1.0 line); the Claude Code hard requirement is buried mid-page; the
   first verdict table shows `insufficient` and `audit:` rows before they are
   explained; the Methodology section is a wall of seven arXiv citations; the
   regression axes (cost, latency, correctness) are absent from the tagline.
4. **`docs/` reads as a workshop, not documentation.** ~95 % of files under
   `docs/` are internal (superpowers/ 53, plans/ 29, specs/ 20, reviews/ 7)
   with no landing index; README's "Documentation & Development" links straight
   into `docs/plans/` milestone codes; the troubleshooting table is buried at
   the bottom of `privacy-and-operations.md` with no inbound link; the reading
   order is triplicated and already diverged; the ADR index is missing
   ADR-0028; the `CATACOMB_*` verifier table is copied in three places.

## Decisions (from brainstorm)

1. **Versioning stays human, publishing goes zero-touch.** The semver decision
   and `git tag` remain manual per `VERSIONING.md`; everything after the tag
   push must run with zero manual steps. No release-please.
2. **README remains the tutorial** (FastAPI model, per the 2026-07-13 design).
   A long page is fine; the bar is "nothing superfluous". This design amends
   that structure rather than replacing it.
3. **Competitor comparison ships as a short README section** (5–7 lines), not
   a standalone page.
4. **The path traps are fixed in code, not documented around** — a contract
   change riding a 0.MINOR release.
5. **Internal docs move under `docs/internal/`** with a new `docs/README.md`
   landing index.
6. **Two-tier naming.** Hook: "Regression testing for Claude Code agents".
   Technical category: "offline eval gate", introduced once in the README
   ("Catacomb is an offline eval gate: …") and used consistently everywhere
   else (guide, package descriptions).

## Goals

- A pushed tag publishes to every channel without human action, and channel
  drift is detected mechanically, not by user bug reports.
- `catacomb version` reports the module version for `go install` builds, and
  evidence stamps stop recording `dev` for released binaries.
- A basket written against the documented contract behaves identically in
  inline and offline verify, regardless of the invoker's cwd.
- A newcomer can answer required/optional/default questions about every basket
  field from one doc page without reading Go.
- The README answers "why not promptfoo/LangSmith/Inspect", signals maturity,
  and shows no output line before the text explains it.
- A GitHub visitor opening `docs/` sees user documentation first and internal
  material clearly fenced off.

## Non-goals

- No release-please or conventional-commit version automation.
- No full comparison page; no CHANGELOG.md (GitHub Releases remain the
  changelog).
- No verifier-script capture into evidence dirs (cross-machine offline verify
  beyond stamped absolute paths is out of scope; revisit if evidence
  portability becomes a requirement).
- No docs site; no rewrite of guide pages beyond the deltas listed here.
- No new required CI checks (the link checker is advisory at first).

## WS1 — Zero-touch release

**Environment gate.** Remove required reviewers from the `release`
environment; add a deployment ref policy restricting the environment to
`v*.*.*` tag refs. Safety is unchanged and automatic: the `verify` job already
refuses tags that are not ancestors of `origin/master` or whose commit lacks
the ten green required checks. Applied once via `gh api`; the exact commands
and the rationale are recorded in `docs/RELEASING.md` so the setting is
reproducible.

**Channel watchdog, two layers.**

- `verify-channels`, a final job in `publish.yml` (`needs: [goreleaser,
  update-apt]`): asserts the tap cask `version` equals the tag, the apt
  `Packages` index lists the tag's version, `ghcr.io/realkarych/catacomb:latest`
  and `:TAG` resolve to the same digest, and the GitHub Release carries the
  expected assets (archives, checksums, sigstore bundles, SBOMs). Any mismatch
  fails the release run where it is immediately visible. Bash + `gh api` +
  `curl` only, with bounded retries to absorb propagation lag.
- `channels-watch.yml`, scheduled weekly + `workflow_dispatch`: runs the same
  assertions against the latest GitHub Release; on mismatch opens or updates a
  single issue labeled `release-desync` (deduplicated by title). Catches drift
  between releases (manual tap edits, apt rot).

**Version stamping.** `Version` stays an ldflags variable defaulting to
`dev`. At startup, when it equals `dev`, fall back to
`debug.ReadBuildInfo().Main.Version` if that is non-empty and not `(devel)`.
Result: goreleaser builds report the tag (unchanged), `go install
…@vX.Y.Z` builds report `vX.Y.Z`, source/test builds keep `dev`. The fallback
feeds every existing consumer of `Version`, including `CatacombVersion` stamps
in evidence and baselines.

**Docs.** `RELEASING.md` documents the zero-touch flow, the watchdog layers,
and why the environment needs no approval. The README install section (WS3)
states which version each channel serves and how fast channels converge, moves
the old-formula migration note up beside the brew block, and adds a "stale
version? run `brew update`" line.

## WS2 — Basket path contract and load UX (0.MINOR)

**Resolution rule.** Relative paths in a basket file resolve against the
directory containing the basket file, never the invoker's cwd:

- `dir` (a path by type): always resolved.
- argv elements of `cmd`, `setup`, and `verify.cmd`: resolved only when
  explicitly relative — prefixed `./` or `../`. Bare words (`python3`) are
  left for PATH lookup. No existence heuristics; the rule is quotable in one
  sentence.

**Stamping.** Bench writes the resolved absolute argv into `meta.json`
(additive evidence-layout change), and offline `catacomb verify` executes that
stamped argv, making it independent of its own cwd. Documented limitation:
moving evidence to another machine requires the verifier to exist at the same
absolute path or an explicit command override.

**Load and report UX.**

- Single-variant basket: stderr advisory at load and `--dry-run` ("1 variant:
  bench will record evidence, but regress needs ≥ 2 to gate"); not an error.
- YAML type errors translated to human phrasing (`task[0].cmd: expected a list
  of strings (got a single string)`); the doubled `bench.Load: … bench: …`
  prefix collapsed to one.
- Unitless `timeout` values get a hint (`use a duration with units, e.g.
  "30s"`).
- `regress` collapses identical transcript-version warnings into one line with
  a count.

**Docs and process.** New `docs/guide/basket.md` is the single source of truth
for the basket schema: a table of every field (type, required/optional,
default, allowed values), the path rule, workspace/dir exclusivity, and the
three "what happens if" cases from the audit (omitted `reps`, `dir` +
`workspace`, out-of-range values). `cli.md#bench` shrinks to a pointer;
`configuration.md` opens with a one-line pointer for readers hunting the
basket schema there. The contract change is recorded as **ADR-0029** and the
release notes carry a migration line: baskets that relied on cwd-relative
resolution must make paths relative to the basket file — anyone who ran bench
from the basket's directory is unaffected.

## WS3 — README amendments (FastAPI-style tutorial page)

Amendments to the 2026-07-13 structure, top to bottom:

1. **Hero:** tagline gains the regression axes ("… statistics over real
   transcripts tell you whether cost, latency, or correctness regressed") and
   is followed by the canonical category sentence ("Catacomb is an offline
   eval gate: …") and an explicit, first-screen Claude Code requirement.
2. **Badges:** add a GitHub release version badge; beneath the badge row one
   maturity line: "Pre-1.0: minor releases may carry breaking changes, always
   with migration notes."
3. **Verdict visual:** a colored SVG "screenshot" of a real `regress` verdict
   table (light/dark pair under `docs/assets/`, authored from genuine output)
   placed before the tutorial so the payoff is visible in seconds.
4. **Features:** keep the six bullets; expand "Wilson bounds" into plain words
   with a link.
5. **New "Why not promptfoo / LangSmith / Inspect" section**, 5–7 neutral
   lines: promptfoo targets prompt/RAG evals while catacomb scores whole agent
   sessions from real transcripts; LangSmith/Braintrust are hosted platforms
   while catacomb is local files and an exit code; Inspect is a research
   framework while catacomb is a purpose-built regression gate for Claude
   Code.
6. **Requirements/Installation:** per-channel version line (brew/apt/docker
   converge within minutes of a release; `go install` serves the tag
   directly), migration note beside the brew block, `brew update` hint, docker
   marked "for CI and `version`; the tutorial needs mounts", an explicit "bench
   spends real API money (~cents on haiku for the demo)" line, and a link to
   the tested Claude Code version watchlist.
7. **Tutorial:** unchanged in role; every step ends with a "check it" block;
   the first verdict table drops `insufficient` and `audit:` rows, which move
   to an "Understanding the report" subsection introduced before they appear;
   no output line precedes its explanation.
8. **How it works:** ASCII session graph stays; step/phase-key detail shrinks
   to two lines plus a `concepts.md` link.
9. **Methodology:** compressed to one paragraph plus a compact link list.
10. **Documentation section:** links go to the guide index, `basket.md`, the
    tutorial anchor, `troubleshooting.md`, and the ADR index — never into
    internal plans or specs.

**Consistency:** goreleaser/nfpm package descriptions change to the canonical
"Offline eval gate for Claude Code agents".

## WS4 — docs/ hygiene

1. **Move** `docs/{superpowers,plans,specs,reviews}` →
   `docs/internal/{…}` via `git mv`. Fix inbound links by grep sweep. Guide
   claims that cite internal reviews (e.g. the "~5× cost" figure in
   `cli.md:573`, `workflows.md:434`) inline the figure with its date and keep
   the internal link as provenance.
2. **`docs/README.md` landing index:** users → `guide/` (reading order lives
   only in `docs/guide/README.md`), basket schema → `basket.md`, stuck →
   `troubleshooting.md`; `internal/` labeled as development material. The
   `getting-started.md` stub is deleted after a grep confirms no inbound
   links.
3. **`docs/guide/troubleshooting.md`:** the table lifted from
   `privacy-and-operations.md` plus first-session symptoms: `no session id
   observed`, `SQLITE_BUSY`, missing checkpoints, "brew installed an old
   version" (→ `brew update`), "offline verify can't find the script"
   (post-WS2 semantics). Linked from README and the guide index.
4. **Single-source contracts:** the `CATACOMB_*` table lives canonically in
   `workflows.md`; `cli.md` and `integrations/verifier/README.md` keep a
   one-line summary plus a link. The duplicated bench→verify→regress example
   stays in `workflows.md`; `cli.md` links to it.
5. **Index fixes:** ADR-0028 added to the ADR index (ADR-0029 arrives with
   WS2).
6. **Link regression guard:** an advisory CI job checking relative links and
   anchors across README + `docs/` (offline mode, no network), not in the
   required-check set initially.

## Execution and sequencing

One implementation plan, four PRs, subagent-driven per repo rules (fresh
worktree per task, review after each):

- **PR-1 (WS1)** is file-disjoint from the rest and proceeds in parallel with
  everything.
- **PR-2 (WS2)** and **PR-3 (WS4)** both edit `docs/guide/cli.md`, so they
  serialize there: PR-3 (pure moves and dedup) merges first, PR-2 rebases its
  doc deltas (`basket.md`, the `cli.md#bench` shrink, the `configuration.md`
  pointer) on top; PR-2's Go work still proceeds in parallel.
- **PR-4 (WS3)** lands last: it references WS1 (badge, channel notes), WS2
  (path semantics in examples, `basket.md` links), and WS4 (new doc paths,
  troubleshooting link).
- The one-time environment settings change (`gh api`) executes alongside PR-1
  and is recorded in `RELEASING.md`.
- After all four merge: release **v0.2.0** (WS2 is a contract change; the rest
  rides along), tag annotated with the migration line, then confirm the
  `Publish Release` run and the new `verify-channels` job conclude `success`.

## Testing

- Go changes (version fallback, path resolution, meta stamping, advisories,
  error mapping, warning dedupe): TDD, 100 % coverage, `internal/codepolicy`
  clean (no comments).
- Path contract: unit tests for the prefix rule (`./`, `../`, bare words,
  absolute paths, `dir`), plus an offline-verify test proving a
  basket-relative `./verify.py` works when `catacomb verify` runs from an
  unrelated cwd. Hermetic E2E assertions extended accordingly.
- Workflows: `actionlint`/`zizmor` clean; `verify-channels` exercised by the
  next real release (and `channels-watch.yml` by `workflow_dispatch` right
  after merge).
- Docs: the new link-check job passes on the restructured tree; manual anchor
  sweep for moved files.

## Risks

- **No-approval publishing:** a bad tag can no longer be caught by a human
  pause. Mitigated by the `verify` job's ancestry + green-checks gate, the
  tag-scoped environment policy, and single-maintainer tag push rights.
- **Path-contract change breaks an existing basket** that relied on
  cwd-relative resolution with a cwd different from the basket dir. Judged
  rare (docs always demonstrated running from the basket dir); covered by the
  migration line and the 0.MINOR bump.
- **Watchdog flakiness** from channel propagation lag: bounded retries in
  `verify-channels`; the weekly watch deduplicates issues instead of spamming.
- **README growth vs "nothing superfluous":** the comparison, maturity, and
  requirement additions are ~15 lines total while the `insufficient`/`audit`
  cleanup and Methodology compression remove more than they add.
