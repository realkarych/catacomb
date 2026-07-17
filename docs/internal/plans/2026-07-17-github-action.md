# GitHub Action + `regress --format markdown` — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development — one fresh implementer subagent per task, review after each task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** ship `regress --format markdown` (PR-comment-ready, rendered in the binary) and a thin `catacomb-gate` composite Action that installs a pinned binary, benches/regresses, and upserts a sticky PR comment. Decision record: [ADR-0033](../../adr/0033-github-action.md).

**Architecture:** all rendering is Go (tested to 100%); the composite `action.yml` is logic-free shell (install + invoke + pipe). Reuses the deterministic `regress.Report`, the `--json`/exit-code contracts, and the signed release artifacts.

**Tech stack:** Go stdlib; GitHub composite action YAML; `gh` CLI for comment upsert. No new Go deps.

## Global constraints

- **No comments in Go code**; TDD; 100% coverage (`make cover`); gofumpt; `make lint`; testify; sentinel errors.
- **No existing pipeline breaks**: `--json` keeps working as a deprecated alias for `--format json`; `--format` default is `human` (byte-identical to today's default output).
- Markdown output renders purely from `regress.Report` (a pure function — no I/O beyond the writer).
- Composite action shell contains NO presentation logic (no jq-formatting the report); actionlint + zizmor clean; any actions referenced are SHA-pinned.
- Commit after every green task; branch `feat/github-action` (based on this plan-doc branch).

---

### Task 1: `--format` flag with `--json` as deprecated alias

**Files:**

- Modify: `cmd/catacomb/regress.go` (flag definition + dispatch), `cmd/catacomb/regress_test.go` (or the relevant CLI test file)

**Interfaces:**

- Add `--format string` (default `"human"`; accepted: `human`, `json`, `markdown`). Keep `--json bool` registered but marked deprecated (`cmd.Flags().MarkDeprecated("json", "use --format json")` — verify cobra's exact API; the flag must still set the format).
- Resolution: if `--format` is explicitly set, it wins; else if `--json` is set, format = json; else human. An explicit `--format` value other than the three → operational error (exit 2) `regress --format: unknown format %q (want human|json|markdown)`.
- Dispatch in `regressReport`: switch on the resolved format → `RenderHuman` / `RenderJSON` / `RenderMarkdown` (Task 2 provides RenderMarkdown; for THIS task, wire only human+json and add a `case "markdown"` that returns a temporary "not yet" — NO: Task 2 lands RenderMarkdown; to keep tasks independently green, this task adds `--format` resolving human/json and treats markdown as an accepted value that Task 2 wires. Simplest: land Task 1 and Task 2 together is tempting but keep them separate — so in Task 1, `markdown` resolves but calls a stub `renderMarkdownStub` that writes the human format, and Task 2 replaces the stub with the real renderer + tests. AVOID stubs that ship: instead, Task 1 rejects `markdown` as "unknown format" until Task 2, and Task 2's commit adds `markdown` to the accepted set AND the renderer atomically). Implement Task 1 as: accepted = {human, json}; `--format markdown` → the unknown-format error. Task 2 adds markdown to the set.

- [ ] **Step 1: failing tests** — `--format json` == `--json` output (byte-identical); `--json` still works (deprecation warning on stderr acceptable, assert stdout unchanged); `--format human` == default; `--format bogus` → exit 2 with the message; `--format markdown` → exit 2 "unknown format" (until Task 2). Both flags set → `--format` wins.
- [ ] **Step 2–4:** RED → implement resolution + dispatch → GREEN.
- [ ] **Step 5:** `make fmt && make cover && make lint`; commit `feat(cmd): regress --format flag (json/human), --json deprecated alias`.

### Task 2: `RenderMarkdown` + wire `--format markdown`

**Files:**

- Modify: `regress/render.go` (add `RenderMarkdown`), `regress/render_test.go`, `cmd/catacomb/regress.go` (add `markdown` to accepted set + dispatch), `cmd/catacomb/regress_test.go`

**Interface (produced):** `func RenderMarkdown(r Report, w io.Writer)` — pure, mirrors RenderHuman's data but as markdown:

- Headline: `**Verdict: <overall>**` mapped to a leading emoji/word — regression → `❌ regression`, ok → `✅ ok`, insufficient → `⚠️ insufficient`, improvement → `✅ improvement`, notable → `⚠️ notable`. Then a line `baseline <N> runs · candidate <M> runs · coverage steps <x> phases <y>`.
- Sensitivity: if present and any axis unreachable, a blockquote `> ⚠️ gate cannot fire at this support: <axes>` (reuse the existing `formatSensitivity`/`formatPairedSensitivity` helpers — do NOT duplicate their logic).
- Findings table: a markdown table with header `| Verdict | Scope | Key | Name | Metric | Baseline | Candidate | Band | Detail |`. By default include only gating scopes? NO — include ALL findings (the human renderer does), but sort/keep the existing order; escape `|` in Detail values (a `strings.ReplaceAll(detail, "|", "\\|")` helper). Reuse `renderValue`/`formatBand`/`keyOrDash`.
- Reliability + audit: as a foldable `<details><summary>reliability & audit</summary>` block containing the same lines RenderHuman emits (reuse `formatReliability`/`formatCellFlag`). Omit the block entirely when both are nil.
- A trailing footer line: `<sub>catacomb regress · exit <n></sub>` — NO: the renderer does not know the exit code (that's the CLI's mapping). Omit exit from the renderer; the Action's comment step can prepend context. Keep the renderer purely about the Report.

Determinism: no time, no map iteration (Findings is a slice; sensitivity axes built in fixed order as RenderHuman does).

- [ ] **Step 1: failing tests** — golden-style: a Report with a regression finding + an insufficient sensitivity + audit → assert the markdown contains the headline emoji, a well-formed table row per finding, the sensitivity blockquote, the folded details block; a `|`-containing Detail is escaped; an all-ok Report with nil reliability/audit omits the details block; empty findings → table header only (or a "no findings" line — pick and pin). Byte-determinism across two renders.
- [ ] **Step 2–4:** RED → implement (reusing existing helpers, no logic duplication) → GREEN; add `markdown` to the CLI accepted set + dispatch; a CLI test that `regress --format markdown` over fixture evidence emits the headline and a table.
- [ ] **Step 5:** `make fmt && make cover && make lint`; commit `feat(regress): markdown render mode for PR comments`.

### Task 3: composite Action + usage doc

**Files:**

- Create: `.github/actions/catacomb-gate/action.yml`, `.github/actions/catacomb-gate/README.md`
- Create: `.github/workflows/action-selftest.yml` (a hermetic self-test that runs the action against in-repo fixture evidence — NO real bench, NO API spend — proving install→regress→comment-render end to end; the comment step runs in dry-run/echo mode on the self-test since there's no real PR context, OR guarded by `if: github.event_name == 'pull_request'`).

**action.yml contract (inputs):** `version` (catacomb release tag, required — pinned by the caller), `basket` (path; optional), `candidate-runs-dir` (skip bench when set), `baseline` (label/name selector) OR `baseline-bundle` (path to an ADR-0032 bundle; when set, `baseline import` runs first), `db` (store path), `runs-dir`, `reps`/`model` (cost levers passed to bench env), `strict` (bool), `comment` (bool, default true), `github-token` (for the comment). **outputs:** `verdict`, `exit-code`, `report-json` (path). Steps: (1) download+verify the pinned release (checksums.txt + cosign — mirror docs/RELEASING.md verification; SHA-pin any actions used); (2) if `baseline-bundle` set → `catacomb baseline import`; (3) if no `candidate-runs-dir` → `catacomb bench $basket` with reps/model env; (4) `catacomb regress ... --format markdown > comment.md; catacomb regress ... --json > report.json; echo exit`; capture exit code WITHOUT failing the step (so the comment posts even on regression), set outputs; (5) if `comment` and PR context → upsert sticky comment via `gh pr comment` with a stable marker (`<!-- catacomb-gate:<baseline> -->`) — find-and-update or create; (6) a final step re-raises the gate exit code so the check fails on regression.

Shell rule: steps 4-5 contain no report parsing beyond reading catacomb's own outputs; the marker-based find/update is the only gh logic.

- [ ] **Step 1:** author action.yml + README (inputs/outputs table, minimal usage example, cost warning for bench-on-PR, baseline-bundle-recommended note); write the self-test workflow driving fixture evidence (reuse e2e/hermetic fixtures or a tiny committed evidence dir) asserting the markdown comment file contains the verdict headline and the step exit code matches the seeded verdict.
- [ ] **Step 2:** `actionlint` + `zizmor` clean locally (pip/go-run per existing security.yml pattern); `bash -n` any inline scripts; validate the YAML.
- [ ] **Step 3:** commit `feat(ci): catacomb-gate composite action + hermetic self-test`.

### Task 4: docs + versioning

**Files:**

- Modify: `docs/guide/cli.md` (regress `--format` flag row; note `--json` deprecated; document the three formats and the markdown use case), `docs/guide/workflows.md` (a "Gate a PR with the Action" recipe pointing at `.github/actions/catacomb-gate`, baseline-bundle-based restore), `docs/VERSIONING.md` (Action inputs/outputs + `--format`/markdown-doc-shape join the surfaces), `README.md` (one line in the CI/features area linking the Action), `docs/adr/README.md` (0033 row), `skills/catacomb/references/ci-gate.md` (show the Action as the primary path).
- markdownlint + relative-link check clean.

- [ ] Implement; commit `docs: catacomb-gate action + regress --format`.

### Task 5: final review + PR

- [ ] Final whole-branch review (determinism of the markdown renderer, `--json` back-compat, action shell has no presentation logic, self-test is genuinely hermetic); fix wave if needed; re-review.
- [ ] PR `feat: catacomb-gate GitHub Action + regress --format markdown (ADR-0033)` — base: the ADR docs branch.

## Deliberately out of scope

- Extracting to a standalone `realkarych/catacomb-action` marketplace repo with an independent tag (follow-up once the input contract proves stable).
- A Docker action variant.
- Auto-opening issues or non-PR comment surfaces.
