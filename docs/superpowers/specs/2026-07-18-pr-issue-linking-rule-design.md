# Design: issue-per-task + PR-links-issue rule

Date: 2026-07-18
Status: approved (brainstorming)

## Goal

Introduce a repository rule: every task starts with a GitHub issue, and every
pull request links to its issue. Enforcement is **documentation + PR template
only** — no CI gate (chosen by the repo owner).

## Background

The rule does not exist today:

- `AGENTS.md` → **Workflow** describes branch/PR conventions but says nothing
  about issues.
- `.github/PULL_REQUEST_TEMPLATE.md` has Summary / What changed / Testing /
  Checklist, but no issue-link field.
- `CONTRIBUTING.md` does not mention issues.
- Issue templates (`bug_report.md`, `feature_request.md`) exist but nothing
  ties a PR to them.
- No CI gate enforces linkage.

## The rule (what we document)

1. **Issue-first.** Every task starts with a GitHub issue — open one (using the
   `bug_report` or `feature_request` template) before creating the branch.
2. **PR links its issue.** Every PR references its issue in the description with
   a closing keyword: `Closes #N` (or `Fixes #N`), so the issue auto-closes on
   squash-merge. Use a plain `#N` reference when the PR intentionally should not
   close the issue (partial work toward it).
3. **Exceptions.** Automated PRs (e.g. Dependabot) and emergency hotfixes may be
   exempt — link an issue when practical, but it is not a hard block.

## Changes (three markdown files, no Go code)

### 1. `AGENTS.md` — Workflow section

Add two bullets and one exception note, matching the section's terse style:

- Issue-first: open an issue before branching.
- PR links its issue via `Closes #N`; plain `#N` when it should not close.
- Exception note: automated (Dependabot) and emergency-hotfix PRs are exempt.

### 2. `.github/PULL_REQUEST_TEMPLATE.md`

- Add a `## Related issue` section immediately after `# Summary`, with a
  `Closes #` placeholder.
- Add a checklist item: `- [ ] Linked to an issue (Closes #N)`.

### 3. `CONTRIBUTING.md`

Add one bullet mirroring the rule: open an issue per task and link it from the
PR (`Closes #N`).

## Out of scope (deliberately)

- No GitHub Actions workflow / required status check. The rule rests on
  discipline and review, per the repo owner's choice.
- No changes to issue templates themselves.

## Verification

- No Go code changes → 100% coverage gate is unaffected.
- Run `make lint` / `markdownlint` over the changed docs to keep the docs gate
  green.
