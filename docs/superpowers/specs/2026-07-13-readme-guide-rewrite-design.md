# README-as-guide rewrite — design

- **Date:** 2026-07-13
- **Status:** approved design, pending implementation plan
- **Scope:** `README.md` + `docs/guide/` restructure (docs only, no Go changes)

## Problem

The current README is a polished storefront, not a guide. The essence is buried in two
dense paragraphs that lean on internal vocabulary (action graph, Wilson bounds, step
keys, ADR references) before the reader knows why they should care. There is no
"create it → run it → check it" path: the reader sees a 150-line `--help` dump instead
of what the tool's output actually looks like, and the single most compelling artifact
— the `regress` verdict table catching a real regression — never appears. Meanwhile
`docs/guide/getting-started.md` and `workflows.md` already contain exactly the
step-by-step material the README lacks, but GitHub visitors read the README and only
the README.

Reference model: the FastAPI README — definition in one plain sentence, one-command
install, then a complete miniature tutorial with the expected output shown after every
step, then an "example upgrade" second act, then a recap.

## Decisions (from brainstorm)

1. **Audience:** an active Claude Code user with tuned prompts/skills/MCP tools who is
   afraid to change them. Claude Code concepts (transcripts, subagents, MCP) are
   assumed; only catacomb concepts (basket, evidence, gate) are explained.
2. **Real output:** every tutorial step shows genuinely captured output, including the
   `regress` verdict table. No hand-written mock output.
3. **Install:** one recommended command inline (Homebrew), all other channels collapsed
   into `<details>` blocks in the same section.
4. **Scope:** the whole doc bundle is restructured — README becomes the tutorial
   funnel, `docs/guide/` becomes the reference depth (approach A).

## Goals

- A first-time visitor understands what catacomb is within one screen, with zero
  catacomb-specific jargon required up front.
- The README alone takes the reader from install to a working CI gate, with real
  output shown after each step.
- The tutorial example is copy-paste reproducible on any machine with Claude Code.
- The visual identity is preserved: lockup, badge row, centered emoji headers,
  `<hr>` separators.
- The guide stops duplicating the tutorial and gains an explicit reading order.

## Non-goals

- No docs site (mkdocs or otherwise).
- No content rewrite of `concepts.md`, `workflows.md`, `cli.md`, `configuration.md`,
  `ingestion.md`, `privacy-and-operations.md` beyond link/anchor fixes.
- No Go code changes; the coverage gate is untouched.
- No new marketing assets.

## README structure

Top to bottom:

1. **Hero** — lockup and badges unchanged. New tagline in plain words, e.g.:
   *"Regression testing for Claude Code agents. Change a prompt, a skill, or an MCP
   tool — then let statistics, not vibes, tell you whether your agent got worse."*
2. **Opening paragraph** (3–4 sentences, no jargon): you changed a prompt — did the
   agent get worse? One run cannot answer; agents are nondeterministic. Catacomb runs
   the task N times per variant and compares the groups statistically; the verdict is
   an exit code for CI. The terms *action graph*, *Wilson bounds*, *step keys* do not
   appear here.
3. **Features** — replaces "✨ Highlights". Same facts, benefit-first phrasing:
   gate in CI, plain local files (no daemon/network), secret-redacted evidence,
   comparisons survive prompt rewrites (checkpoints), baselines and drift history,
   plug-in correctness checks (verifier). Each bullet ≤ 2 lines, at most one term of
   art per bullet, linked to the guide page that explains it.
4. **Requirements** — Claude Code installed and `claude` on PATH. Nothing else.
5. **Installation** — Homebrew one-liner inline; `<details>` blocks for Docker,
   APT, release archives (incl. the Windows `Unblock-File` note), and
   `go install`/source. Existing channel content is preserved verbatim inside the
   details blocks.
6. **Tutorial** — the core, FastAPI-shaped:
   - **Create it** — a minimal, self-contained basket YAML (tasks × variants × reps;
     no checkpoints yet). See "Reproducible example" below.
   - **Run it** — `catacomb bench demo.yaml` + captured, trimmed output (cell
     progress lines and the "Next steps:" epilogue). Three sentences on what
     happened: local processes, transcripts resolved from `~/.claude/projects`,
     redacted evidence written under `~/.catacomb/runs`.
   - **Gate it** — the `regress` command + the captured verdict table **catching the
     seeded regression** (exit `1`), then the exit-code contract (0/1/2) and a
     ready-to-paste CI snippet.
   - **Upgrade it** — three short upgrades, each a few lines of config plus captured
     or quoted output where it earns its place: (a) pin a named baseline, `--record`,
     `trends`; (b) checkpoints via `catacomb mcp` (the one-line `--mcp-config` JSON
     and the CLAUDE.md marking instruction); (c) the verifier contract — "did it get
     the *right answer*" — `artifacts:` + `verify:` + the two-call Python SDK
     (`Cell.from_env()` / `emit`). The verifier currently appears nowhere in the
     README despite being the flagship ADR-0027 scenario.
   - **Recap** — ~4 lines: what the reader now has; link to `workflows.md` for
     recipes (small-k sensitivity, paired sign test, external scores, Pareto).
7. **How it works** — the `session → prompt → turn → tool` ASCII tree, two sentences
   on step keys / phase keys as cross-run identity, one sentence on the
   implemented-end-to-end status with the ADR-0026 link (absorbs the current Status
   blockquote). Links to `concepts.md`.
8. **Privacy** — trimmed to ~5 lines; keeps the honest best-effort caveat and the
   known-residuals link.
9. **Documentation map** — reading order with one-liners (start here → concepts →
   workflows → cli → configuration → ingestion → privacy), plus the dev commands
   block (`make build/test/cover/lint`) and AGENTS.md pointer.
10. **Contribution / License** — kept, lightly trimmed.

Removed outright: the `catacomb --help` dump; "✨ Highlights" in its current wording;
the standalone Status blockquote.

## Reproducible tutorial example

The tutorial basket must be runnable verbatim by any reader, finish in ~2 minutes,
cost cents, and visibly regress so the verdict table has something to catch.

Shape (final wording is an implementation detail; the README must show exactly what
was run for capture):

- A tiny work task driven by an instruction file, e.g. `TASK.md` tells the agent to
  write a small script and run it to verify. `cmd` pins a cheap model and uses
  `--output-format stream-json` (required by bench) with a permissive-enough
  permission mode for unattended runs.
- Two variants: `main` copies the good instruction file into place via `setup:`;
  `candidate` copies a degraded instruction (the verification step removed — modeled
  on the e2e live gate's swapped-instruction seeding). The visible effect: the run's
  Bash verification step vanishes, so `regress` reports a presence flip — a hard
  `regression` at the tutorial's `reps: 5`.
- `reps: 5` per the guide's own "use k ≥ 5" advice.

Capture procedure:

1. Build `bin/catacomb` in the worktree.
2. Run the exact basket from the README with the live `claude` CLI in a scratch
   workdir (2 variants × 5 reps, small model; expected cost well under $1).
3. Paste the trimmed real output of `bench` and `regress` into the README; replace
   the user home directory with `~`; keep table alignment intact.
4. Fallback if a live run is impossible in the implementation environment: drive the
   same basket through the hermetic e2e stub agent and label synthetic cost/token
   numbers as such. Live capture is strongly preferred.

Staleness risk is accepted: captured output can drift from future CLI output formats.
Mitigation: the capture procedure is documented in the implementation plan so refresh
is a mechanical re-run.

## Guide restructure

- `docs/guide/README.md` — drop the duplicated 30-second quickstart; becomes a
  reading map: "Start here: the README tutorial" followed by the ordered list of
  guide pages with one-line descriptions.
- `docs/guide/getting-started.md` — slims to a thin "Start here" pointer: sends the
  reader to the README tutorial, keeps only next-step links. The file stays (external
  links must not break); its unique content (there is little) moves into the README
  tutorial.
- All other guide files: content untouched; only links/anchors that the restructure
  invalidates are fixed (e.g. `workflows.md` and `concepts.md` references to
  getting-started sections, `getting-started.md`'s install pointer into the README).
- Sweep the rest of the repo (docs/adr, AGENTS.md, CONTRIBUTING.md, integrations
  READMEs) for links into the rewritten files and fix any that break.

## Verification

1. **Reproducibility:** every tutorial command is executed verbatim during
   implementation; the shown output is the captured output.
2. **Links:** all relative links and intra-doc anchors across README + docs/guide
   resolve after the restructure (scripted check in the worktree).
3. **Render:** visual check of the rendered README (GitHub-flavored markdown,
   including `<details>` blocks and the dark/light lockup) via a preview artifact,
   since the visual identity must survive.
4. **Process:** git worktree, implementation plan executed subagent-driven with
   review between tasks (per CLAUDE.md), PR to master, green CI before merge.

## Risks

- **Captured output staleness** — accepted; refresh is a re-run of the documented
  capture procedure.
- **Live-capture cost/environment** — cents; hermetic fallback documented.
- **README length** — grows in line count but drops in density; the tutorial is
  skimmable because every step is command → output → two sentences.
- **Broken inbound links** — mitigated by the repo-wide link sweep in verification.
