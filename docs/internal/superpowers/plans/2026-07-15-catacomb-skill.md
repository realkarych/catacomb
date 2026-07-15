# Catacomb Agent Skill Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship one bundled Claude Code Agent Skill, `catacomb`, that lets a Claude Code agent drive the catacomb CLI in an engineer's own project — set up a basket, run the gate, read the verdict, and tighten fidelity.

**Architecture:** A single umbrella skill under `skills/catacomb/` — a short `SKILL.md` router (trigger surface + dispatch table) plus a `references/` tree, one file per workflow, using progressive disclosure. The skill drives the shipped CLI and links to `docs/guide/` as canonical; it does not restate or wrap catacomb behavior.

**Tech Stack:** Markdown only (Agent Skill format: YAML frontmatter + `references/*.md`). No Go. Validation is `markdownlint-cli` (already a CI gate) plus a live dry run against a throwaway project.

**Spec:** [`docs/internal/specs/2026-07-15-catacomb-skill-design.md`](../../specs/2026-07-15-catacomb-skill-design.md)

## Global Constraints

Every task's requirements implicitly include this section.

- **In-repo, Markdown only.** Files live under `skills/catacomb/`. The repo's Go gates (no-comments, 100% coverage, gofumpt) do NOT apply. Do not add Go code.
- **markdownlint is the gate.** CI runs `markdownlint '**/*.md' --ignore node_modules` with `markdownlint-cli@0.49.0` against config `.markdownlint.json` (MD013 line-length OFF, MD024 siblings_only, MD033/MD040/MD041 OFF). Every `skills/**/*.md` file is linted automatically. Each task ends green.
- **Drive the CLI, do not reinvent it.** Every command, flag, selector, and exit code the skill emits must match the current CLI and `docs/guide/`. Where the guide is canonical, link to it (relative path from repo root works in-repo; use the public docs path only if the skill is later extracted).
- **No new CLI behavior.** The skill only drives existing commands. Any CLI gap is filed separately, never patched inside the skill.
- **Version floors to state in the skill:** catacomb **v0.2.0+** (a build without `bench`/`regress` is a stale pre-pivot install); `go install` path needs **Go ≥ 1.26**; `claude` on PATH and signed in.
- **Canonical CLI facts** (verbatim, reuse across tasks):
  - stream-json flags: `--output-format stream-json --verbose --strict-mcp-config --setting-sources project`
  - exit codes: `0` ok · `1` regression · `2` operational error
  - MCP marker config: `{"mcpServers":{"catacomb":{"command":"catacomb","args":["mcp"]}}}`, tool `mcp__catacomb__mark`
  - default runs dir: `~/.catacomb/runs`; default projects dir: `~/.claude/projects`
- **Workflow.** Work stays in this worktree (`worktree-catacomb-skill`); feature-branch + squash-merge PR; no direct commits to `master`; no `--no-verify`.

---

## File Structure

```
skills/catacomb/
  SKILL.md            router: what catacomb is, when to load, dispatch table   (Task 1)
  references/
    concepts.md       shared vocabulary the other references link to           (Task 1)
    setup.md          agent.sh + basket.yaml, pre-flight, sizing, first bench  (Task 2)
    ci-gate.md        CI job, secrets, baseline pin, --record, exit-code gate  (Task 3)
    read-report.md    reading a regress table + diagnosis → next action        (Task 4)
    accuracy.md       MCP mark checkpoints, verifiers, baselines/trends         (Task 5)
    import.md         ingesting interactive TUI sessions                       (Task 6)
    troubleshoot.md   install/version, sign-in, format drift, cost             (Task 7)
```

**Concept anchors** defined by `concepts.md` (Task 1), linked by every later reference — use these exact heading slugs:

- `concepts.md#basket-cell-evidence`
- `concepts.md#phases-and-checkpoints`
- `concepts.md#verdicts-and-noise-bands`
- `concepts.md#coverage-and-steps_trusted`
- `concepts.md#exit-codes`

**Parallelism:** Task 1 is the interface and runs first. Tasks 2–7 touch disjoint files and may run as parallel implementer subagents in separate worktrees. Task 8 depends on all.

---

## Task 1: Skill scaffold — `SKILL.md` router + `concepts.md`

**Files:**

- Create: `skills/catacomb/SKILL.md`
- Create: `skills/catacomb/references/concepts.md`

**Interfaces:**

- Produces (later tasks consume):
  - The seven reference filenames wired into the `SKILL.md` dispatch table (below). Later tasks fill each file; the filenames are fixed here.
  - The five concept anchors listed in File Structure. Later references link to these exact slugs.

- [ ] **Step 1: Write `SKILL.md`**

Frontmatter and body verbatim (adjust prose only):

````markdown
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
````

- [ ] **Step 2: Write `concepts.md`**

Create `skills/catacomb/references/concepts.md` with exactly these five second-level
headings (their slugs are the linked anchors), each 2–5 sentences, no code required:

- `## Basket, cell, evidence` — a basket is the YAML matrix of tasks × variants × reps;
  each combination is a **cell** run as a plain local process; each run is written to
  `runs/<run-id>/` as a secret-redacted **evidence** dir (transcripts + `meta.json`
  with labels, exit code, cost). Link: `../../../docs/guide/concepts.md`.
- `## Phases and checkpoints` — the agent can name phases of its own run by calling the
  MCP `mark` tool; declared `checkpoints:` become stable comparison rows in `regress`,
  robust to prompt churn that re-keys steps.
- `## Verdicts and noise bands` — `regress` compares each candidate metric against a
  **noise band** derived from the baseline group; verdicts are `ok`, `regression`, and
  `insufficient` (not enough support to fire). Comparison is group-vs-group, never a
  single side-by-side diff.
- `## Coverage and steps_trusted` — `coverage` reports how much of the step/phase axis
  aligned across runs; `steps_trusted` is false when prompt churn degraded step
  alignment, which is the signal to add checkpoints.
- `## Exit codes` — `0` ok · `1` regression · `2` operational error. The exit code is
  the gate.

- [ ] **Step 3: Lint**

Run: `npx markdownlint-cli@0.49.0 'skills/**/*.md'`
Expected: PASS (no output, exit 0).

- [ ] **Step 4: Verify the dispatch table names real files**

Run: `for f in setup ci-gate read-report accuracy import troubleshoot concepts; do grep -q "references/$f.md" skills/catacomb/SKILL.md && echo "ok $f" || echo "MISSING $f"; done`
Expected: `ok` for all seven except `concepts` also appears — every reference filename is present in `SKILL.md`.

- [ ] **Step 5: Commit**

```bash
git add skills/catacomb/SKILL.md skills/catacomb/references/concepts.md
git commit -m "feat(skill): catacomb skill router + concepts reference"
```

---

## Task 2: `setup.md` — scaffold and first bench (flagship)

**Files:**

- Create: `skills/catacomb/references/setup.md`

**Interfaces:**

- Consumes: concept anchors from Task 1 (`#basket-cell-evidence`, `#exit-codes`).
- Produces: the canonical `agent.sh` and `basket.yaml` templates other references may reference by name.

- [ ] **Step 1: Write `setup.md`**

Structure it as an ordered workflow with these sections. Every code block below must
appear verbatim (engineer fills the `<…>` placeholders):

1. **Inspect how the project invokes `claude`.** Instruct Claude to look for an existing
   `claude` invocation (scripts, Makefile, CI, README) and reuse its model/flags. State
   the goal: one variable — the change under test — moves between `main` and `candidate`;
   everything else is held fixed.

2. **Scaffold `agent.sh`** (the agent under test; `bench` runs it once per cell):

   ```sh
   #!/usr/bin/env bash
   set -euo pipefail
   exec claude -p "${PROMPT}" \
     --model claude-haiku-4-5 \
     --output-format stream-json \
     --verbose \
     --strict-mcp-config \
     --setting-sources project
   ```

   Explain: `--output-format stream-json --verbose` is how catacomb finds the session
   transcript; `--strict-mcp-config --setting-sources project` keep user-scope plugins
   and hooks out of the benchmark so runs compare across machines. Then `chmod +x agent.sh`.

3. **Author `basket.yaml`** — the tasks × variants × reps matrix, framing the change as
   baseline vs candidate:

   ```yaml
   basket: <name>
   reps: 5
   tasks:
     - id: <task-id>
       cmd: ["./agent.sh"]
       dir: .
   variants:
     - id: main
       env:
         PROMPT: "<baseline prompt>"
     - id: candidate
       env:
         PROMPT: "<candidate prompt>"
   ```

4. **Pre-flight the matrix (no spend):**

   ```sh
   catacomb bench basket.yaml --dry-run
   ```

   Explain: `--dry-run` prints the cell-expansion table and exits without executing —
   confirm the cell count is what you expect before spending.

5. **Confirm capture on one cheap cell.** The #1 silent setup failure is an agent
   command that never emits a resolvable transcript. `reps` lives in the basket (there
   is no `--reps` flag), so write a one-cell `preflight.yaml`, run it, then check
   evidence landed:

   ```yaml
   # preflight.yaml — one cheap cell to confirm capture
   basket: preflight
   reps: 1
   tasks:
     - id: <task-id>
       cmd: ["./agent.sh"]
       dir: .
   variants:
     - id: main
       env:
         PROMPT: "<any short prompt>"
   ```

   ```sh
   catacomb bench preflight.yaml --runs-dir runs
   ls runs/*/ | head
   ```

   If a `runs/<run-id>/` dir with a transcript appears, capture works. (If not, send the
   engineer to `troubleshoot.md`.)

6. **Size the basket.** Give the reps/tasks/cost tradeoff: `reps` drives statistical
   power and cost linearly; the **paired** gate needs **≥5 tasks** to fire; more reps
   tighten the noise band. Recommend starting at `reps: 5` on the cheapest adequate
   model and scaling up only if verdicts come back `insufficient`. Reference the
   small-`k` sensitivity work: `../../../docs/adr/0023-regression-gate-sensitivity-at-small-k.md`.

7. **Run the first bench and hand off:**

   ```sh
   catacomb bench basket.yaml --runs-dir runs
   catacomb regress --runs-dir runs \
     --baseline label:basket=<name>,variant=main \
     --candidate label:basket=<name>,variant=candidate
   ```

   Then point at `read-report.md` to interpret the verdict.

- [ ] **Step 2: Lint**

Run: `npx markdownlint-cli@0.49.0 'skills/**/*.md'`
Expected: PASS.

- [ ] **Step 3: Fidelity cross-check**

Run: `grep -nE "bench|--dry-run|--runs-dir|stream-json|--strict-mcp-config" docs/guide/cli.md | head`
Expected: every flag used in `setup.md` appears in the guide. Fix any mismatch.

- [ ] **Step 4: Commit**

```bash
git add skills/catacomb/references/setup.md
git commit -m "feat(skill): setup reference — scaffold and first bench"
```

---

## Task 3: `ci-gate.md` — CI merge gate

**Files:**

- Create: `skills/catacomb/references/ci-gate.md`

**Interfaces:**

- Consumes: concept anchors (`#exit-codes`), the `agent.sh`/`basket.yaml` from `setup.md`.

- [ ] **Step 1: Write `ci-gate.md`**

Cover, in order:

1. **What the gate needs.** A pinned baseline (golden group), the candidate bench in CI,
   and a `regress` call whose exit code blocks the merge.

2. **Pin the baseline once** (committed DB so CI can read it):

   ```sh
   catacomb baseline set golden --label basket=<name>,variant=main \
     --runs-dir runs --db catacomb.db
   ```

   Explain: names survive label churn; commit `catacomb.db` (and the baseline `runs/`
   group, or store it as a retrievable artifact) so CI has the golden reference.

3. **GitHub Actions job** — provide this as the concrete starting point. Note the SHA
   pins are placeholders the engineer resolves to current release SHAs (repo convention:
   pin Actions to commit SHAs):

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

4. **Secrets.** `ANTHROPIC_API_KEY` for API billing, or `CLAUDE_CODE_OAUTH_TOKEN` for
   subscription auth — set one as a repo secret. Note `bench` spends real money on every
   PR; keep it cheap.

5. **Cost knobs.** Lower `reps` and use the cheapest adequate model in the CI variant;
   gate on the metrics that matter (the exit code already encodes the overall verdict).
   `--record` accumulates history for `trends` (see `accuracy.md`).

6. **The gate.** `regress` exits `1` on regression, which fails the job and blocks merge;
   `2` means operational error (surface it, don't treat as a pass). Link `#exit-codes`.

7. **Other CI.** One paragraph: any CI works — install the binary (release archive or
   `go install`), run `bench` then `regress`, let the non-zero exit fail the job.

- [ ] **Step 2: Lint**

Run: `npx markdownlint-cli@0.49.0 'skills/**/*.md'`
Expected: PASS.

- [ ] **Step 3: Fidelity cross-check**

Run: `grep -nE "baseline set|name:golden|--record|--db|CLAUDE_CODE_OAUTH_TOKEN|ANTHROPIC_API_KEY" docs/guide/cli.md README.md | head`
Expected: selectors, flags, and secret names match the guide/README. Fix mismatches.

- [ ] **Step 4: Commit**

```bash
git add skills/catacomb/references/ci-gate.md
git commit -m "feat(skill): ci-gate reference — CI merge gate"
```

---

## Task 4: `read-report.md` — interpret a regress report

**Files:**

- Create: `skills/catacomb/references/read-report.md`

**Interfaces:**

- Consumes: concept anchors (`#verdicts-and-noise-bands`, `#coverage-and-steps_trusted`, `#exit-codes`).

- [ ] **Step 1: Write `read-report.md`**

Anchor the explanation on a real report. Include this sample table verbatim, then walk
its parts:

```text
baseline runs 5  candidate runs 5
coverage steps 1.00  phases 1.00  steps_trusted true  overall regression
sensitivity: gate cannot fire at this support (paired gate needs k>=5 tasks)
VERDICT       SCOPE   KEY  NAME         METRIC       BASELINE  CANDIDATE  BAND
regression    total   -    -            cost_usd     0.00      0.01       [0.00, 0.00]
ok            total   -    -            error_rate   0.00      0.00       [0.00, 0.35]
regression    total   -    -            tokens_out   147.00    465.00     [91.50, 202.50]
insufficient  paired  -    -            cost_usd     0.00      0.00       -
regression    phase   …    task:answer  tokens_out   147.00    465.00     [91.50, 202.50]
audit: baseline run … cost_usd 0.0109 vs group median 0.0033 (band 0.0016)
```

Explain each element and map it to a next action:

- **Header line** — `coverage`, `steps_trusted`, and the `overall` verdict. Link
  `#coverage-and-steps_trusted`.
- **`total` rows** — the headline: each metric vs its noise `BAND`; outside the band →
  `regression`. Link `#verdicts-and-noise-bands`.
- **`paired` rows** — exact per-task sign test; needs **≥5 tasks**, else `insufficient`.
  Action: add tasks to arm it.
- **`phase` rows** — pin a regression to a declared checkpoint window. If missing/low
  coverage, action: add checkpoints (`accuracy.md`).
- **`sensitivity:` line** — why a gate could not fire at this support.
- **`audit:` line** — an individual run outside the group band; provenance, not a
  separate verdict.
- **Exit code** — `0`/`1`/`2` and what CI does with each. Link `#exit-codes`.

Close with a **decision guide**: `insufficient` on paired → add tasks; `steps_trusted
false` or low step coverage → add checkpoints; band too wide / noisy → raise `reps`;
`regression` you accept → re-pin the baseline.

- [ ] **Step 2: Lint**

Run: `npx markdownlint-cli@0.49.0 'skills/**/*.md'`
Expected: PASS.

- [ ] **Step 3: Fidelity cross-check**

Run: `grep -nE "steps_trusted|coverage|paired|sensitivity|audit|insufficient" docs/guide/cli.md docs/guide/concepts.md | head`
Expected: every column/term used appears in the guide. Fix mismatches.

- [ ] **Step 4: Commit**

```bash
git add skills/catacomb/references/read-report.md
git commit -m "feat(skill): read-report reference — interpret a regress report"
```

---

## Task 5: `accuracy.md` — checkpoints, verifiers, baselines/trends

**Files:**

- Create: `skills/catacomb/references/accuracy.md`

**Interfaces:**

- Consumes: concept anchors (`#phases-and-checkpoints`, `#coverage-and-steps_trusted`).

- [ ] **Step 1: Write `accuracy.md`**

Three sections, each a self-contained upgrade (skeleton + essentials depth):

1. **Phase checkpoints (MCP `mark`).** Wire the marker tool so prompt churn stops
   re-keying the comparison axis. Write the MCP config file:

   ```json
   {"mcpServers":{"catacomb":{"command":"catacomb","args":["mcp"]}}}
   ```

   Pass it via `claude --mcp-config <file>` in `agent.sh`; add one CLAUDE.md instruction
   (e.g. *"call `mcp__catacomb__mark` with `name: plan` before planning and after tests
   pass"*); declare `checkpoints: [plan, tests.pass]` on the task. Declared phases become
   stable `phase` rows in `regress`. Link `#phases-and-checkpoints`.

2. **Per-task verifier.** Check the answer itself, not just observables. Declare the
   files a task produces and a command that scores them:

   ```yaml
   tasks:
     - id: sql
       cmd: ["./agent.sh"]
       artifacts: ["out/result.csv"]
       verify:
         cmd: ["python3", "./verify_sql.py"]
         env: { GOLDEN: "/fixtures/golden.csv" }
   ```

   The verifier reads captured artifacts and emits one pass/fail line; its verdict rides
   the same statistical gate. Point at the shipped Python SDK:
   `../../../integrations/verifier`. Run with `catacomb verify basket.yaml --runs-dir runs`.

3. **Baselines and trends (longitudinal memory).** Pin a golden group, `--record` every
   comparison, replay the drift:

   ```sh
   catacomb baseline set demo-main --label basket=demo,variant=main --runs-dir runs --db demo.db
   catacomb regress --runs-dir runs --db demo.db \
     --baseline name:demo-main \
     --candidate label:basket=demo,variant=candidate --record
   catacomb trends demo-main --db demo.db
   ```

   Note `catacomb trends <name> --pareto` for the accuracy-vs-cost view.

- [ ] **Step 2: Lint**

Run: `npx markdownlint-cli@0.49.0 'skills/**/*.md'`
Expected: PASS.

- [ ] **Step 3: Fidelity cross-check**

Run: `grep -nE "mcp__catacomb__mark|checkpoints|artifacts|verify|baseline set|trends|--pareto" docs/guide/cli.md docs/guide/workflows.md README.md | head`
Expected: tool name, basket keys, and commands match. Confirm `integrations/verifier/README.md` exists: `ls integrations/verifier/README.md`.

- [ ] **Step 4: Commit**

```bash
git add skills/catacomb/references/accuracy.md
git commit -m "feat(skill): accuracy reference — checkpoints, verifiers, trends"
```

---

## Task 6: `import.md` — ingest interactive sessions

**Files:**

- Create: `skills/catacomb/references/import.md`

**Interfaces:**

- Consumes: concept anchors (`#basket-cell-evidence`).

- [ ] **Step 1: Write `import.md`**

Explain the second entry point: a session run by hand in the interactive TUI enters the
same pipeline at the transcript stage, so `verify`/`regress` work on it unchanged. Give
the recommended workflow verbatim:

```sh
# … do the task by hand in the TUI; call mcp__catacomb__mark at each checkpoint …
catacomb import basket.yaml --task <task-id> --variant <variant-id> --session-id "$SID"
catacomb verify basket.yaml --runs-dir ~/.catacomb/runs
```

Cover: `--task`/`--variant` map the hand-run session onto a basket cell; `--session-id`
selects which `~/.claude/projects` transcript to ingest (omit to use the newest);
imported runs land in the same evidence shape as `bench`, so they compare directly. Link
the guide: `../../../docs/guide/ingestion.md` and `../../../docs/guide/cli.md`.

- [ ] **Step 2: Lint**

Run: `npx markdownlint-cli@0.49.0 'skills/**/*.md'`
Expected: PASS.

- [ ] **Step 3: Fidelity cross-check**

Run: `sed -n '319,420p' docs/guide/cli.md`
Expected: `import` flags (`--task`, `--variant`, `--session-id`) match what the skill emits. Fix mismatches.

- [ ] **Step 4: Commit**

```bash
git add skills/catacomb/references/import.md
git commit -m "feat(skill): import reference — ingest interactive sessions"
```

---

## Task 7: `troubleshoot.md` — install, sign-in, drift, cost

**Files:**

- Create: `skills/catacomb/references/troubleshoot.md`

**Interfaces:**

- Consumes: concept anchors (`#basket-cell-evidence`).

- [ ] **Step 1: Write `troubleshoot.md`**

A symptom → cause → fix table, one row per common failure:

- **`catacomb bench`/`regress` not found / unknown command** → stale pre-pivot install →
  reinstall a v0.2.0+ build (`brew uninstall catacomb && brew install --cask catacomb`,
  or `go install …@latest`); confirm with `catacomb version`.
- **`claude` errors / auth failures during bench** → not signed in → `claude -p hello`;
  set `ANTHROPIC_API_KEY` or sign into the subscription.
- **No `runs/<id>/` transcript captured** → agent command didn't emit resolvable
  stream-json → confirm `--output-format stream-json --verbose`; check
  `--projects-dir` points at the real `~/.claude/projects`.
- **`warning: transcript … newer than tested …`** → Claude Code newer than catacomb's
  tested watchlist → harmless; note the tested-version watchlist
  (`../../../docs/guide/ingestion.md`).
- **Unexpected cost** → too many reps/cells or an expensive model → lower `reps`, use the
  cheapest adequate model, pre-flight with `--dry-run` to see the cell count first.

- [ ] **Step 2: Lint**

Run: `npx markdownlint-cli@0.49.0 'skills/**/*.md'`
Expected: PASS.

- [ ] **Step 3: Fidelity cross-check**

Run: `grep -nE "newer than tested|pre-pivot|--projects-dir|claude -p hello" docs/guide/*.md README.md | head`
Expected: symptoms/fixes match documented behavior. Fix mismatches.

- [ ] **Step 4: Commit**

```bash
git add skills/catacomb/references/troubleshoot.md
git commit -m "feat(skill): troubleshoot reference — install, sign-in, drift, cost"
```

---

## Task 8: Validation and finalize

**Files:**

- Modify (only if fixes needed): any `skills/catacomb/**` file.

**Interfaces:**

- Consumes: all of Tasks 1–7.

- [ ] **Step 1: Full markdownlint over the skill**

Run: `npx markdownlint-cli@0.49.0 'skills/**/*.md'`
Expected: PASS (exit 0).

- [ ] **Step 2: Dispatch + anchor integrity**

Run:
```bash
for f in setup ci-gate read-report accuracy import troubleshoot concepts; do
  test -f "skills/catacomb/references/$f.md" && echo "file ok $f" || echo "FILE MISSING $f"
  grep -q "references/$f.md" skills/catacomb/SKILL.md || echo "DISPATCH MISSING $f"
done
for a in basket-cell-evidence phases-and-checkpoints verdicts-and-noise-bands coverage-and-steps_trusted exit-codes; do
  grep -rqi "#$a" skills/catacomb/references/ || echo "ANCHOR UNLINKED $a"
done
```
Expected: `file ok` for all seven; no `MISSING`/`UNLINKED` lines.

- [ ] **Step 3: Trigger check via writing-skills**

Invoke `superpowers:writing-skills` and run its verification over `skills/catacomb`
(frontmatter validity, description quality, progressive-disclosure hygiene). Apply any
fixes it surfaces.

- [ ] **Step 4: Live dry run of the flagship**

In a throwaway dir, follow `setup.md` end to end with a trivial one-task basket: produce
`agent.sh` + `basket.yaml`, run `catacomb bench basket.yaml --dry-run` (validates, no
spend), then a one-cell `preflight.yaml` (`reps: 1`) real run, and confirm `runs/<id>/`
evidence appears and `catacomb regress …` returns a verdict. Record the outcome in the
PR description. If any skill instruction proved wrong, fix the reference and re-lint.

- [ ] **Step 5: Commit any fixes**

```bash
git add -A skills/catacomb
git commit -m "test(skill): validate catacomb skill — lint, anchors, dry run"
```

---

## Self-Review

- **Spec coverage:** §5 file layout → Tasks 1–7 (all eight files). §5.1 `SKILL.md`
  frontmatter/description/dispatch → Task 1. §5.2 each reference → Tasks 2–7. §6 v1 depth
  (dense: concepts/setup/ci-gate/read-report; skeleton: accuracy/import/troubleshoot) →
  reflected in task detail. §7 validation (trigger, fidelity, dry run, writing-skills,
  markdownlint) → Task 8 + per-task lint/fidelity steps. §8 maintenance (link to guide,
  no forking) → Global Constraints. No gaps.
- **Placeholders:** `<…>` tokens are intentional fill-ins with adjacent instructions, not
  vague TODOs; every command and code block is concrete. No banned patterns.
- **Type/name consistency:** the seven reference filenames and five concept anchor slugs
  are defined once in Task 1 and referenced identically in Tasks 2–8.
