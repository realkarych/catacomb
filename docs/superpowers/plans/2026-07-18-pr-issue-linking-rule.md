# PR-links-issue Rule Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Document a repository rule — every task gets a GitHub issue, and every PR links its issue (`Closes #N`) — in the guide, the PR template, and CONTRIBUTING.

**Architecture:** Documentation-only change across three markdown files. No CI gate, no Go code. The "test" for each edit is that `markdownlint` stays green over the changed files (docs are gated by `markdownlint '**/*.md'` in `.github/workflows/ci.yml`).

**Tech Stack:** Markdown only. Lint: `markdownlint-cli` (config `.markdownlint.json`).

## Global Constraints

- Markdown only — no Go code, so the 100% coverage gate is unaffected.
- `.markdownlint.json` governs docs lint; MD013 (line-length) is disabled. Keep bullet style `-` and blank lines around headings/lists.
- Match AGENTS.md's terse, bold-lead-in bullet style (e.g. `**Simplest thing that works.**`).
- Exceptions are soft: automated (Dependabot) and emergency-hotfix PRs are exempt; link an issue when practical.

---

### Task 1: Document the rule across the three files

**Files:**

- Modify: `AGENTS.md` (the `## Workflow` section, currently lines 56–62)
- Modify: `.github/PULL_REQUEST_TEMPLATE.md`
- Modify: `CONTRIBUTING.md`
- Verify: `markdownlint` over the three changed files

**Interfaces:**

- Consumes: nothing (leaf change).
- Produces: nothing other consumed by later tasks (single-task plan).

- [ ] **Step 1: Edit `AGENTS.md` `## Workflow` section**

Replace the current section body:

```markdown
## Workflow

- Every change goes through a feature branch and PR: `git checkout -b <type>/<short-desc>` from `master`. No direct commits to `master` (the initial scaffold aside).
- One PR = one logical change. CI must be green before merge. Merge is **squash** (linear `master`).
- No `--no-verify`; no force-push to `master` (only to your own feature branch).
- Never commit `.env`, `*.pem`, `*.key`, or any secret.
```

with:

```markdown
## Workflow

- **Issue-first.** Every task starts with a GitHub issue — open one (`bug_report`/`feature_request` template) before branching. Automated (Dependabot) and emergency-hotfix PRs are exempt.
- Every change goes through a feature branch and PR: `git checkout -b <type>/<short-desc>` from `master`. No direct commits to `master` (the initial scaffold aside).
- One PR = one logical change. CI must be green before merge. Merge is **squash** (linear `master`).
- **Link the issue.** The PR description references its issue with a closing keyword — `Closes #N` (or `Fixes #N`) so it auto-closes on squash-merge; use a plain `#N` when the PR should not close the issue.
- No `--no-verify`; no force-push to `master` (only to your own feature branch).
- Never commit `.env`, `*.pem`, `*.key`, or any secret.
```

- [ ] **Step 2: Edit `.github/PULL_REQUEST_TEMPLATE.md`**

Insert a `## Related issue` section right after the Summary block, and add a checklist item as the first checklist entry. Result:

```markdown
# Summary

One or two sentences: what this PR does and why.

## Related issue

Closes #

## What changed

- ...

## Testing

How this was verified (commands and results).

## Checklist

- [ ] Linked to an issue (`Closes #N`)
- [ ] CI is green
- [ ] Coverage stays at 100%
- [ ] No comments in Go code (only `//go:build`, `//go:embed`, `//go:generate`)
```

- [ ] **Step 3: Edit `CONTRIBUTING.md`**

Add an issue bullet as the first list item. Result:

```markdown
# Contributing

Read **[AGENTS.md](AGENTS.md)** first — it is the contributor and agent guide. The repo
runs under a 100%-test-coverage, TDD-first gate with no comments in Go code.

- Open a GitHub issue for each task before branching; link it from the PR with `Closes #N`. Automated and hotfix PRs are exempt.
- Branch from `master`: `git checkout -b <type>/<short-desc>`. One PR = one logical change.
- `make cover lint fmt` must pass locally. CI must be green before merge (squash).
- Never commit secrets. See [SECURITY.md](SECURITY.md) to report vulnerabilities.
```

- [ ] **Step 4: Verify markdownlint stays green**

Run (falls back to `npx` if `markdownlint` isn't on PATH):

```bash
markdownlint AGENTS.md .github/PULL_REQUEST_TEMPLATE.md CONTRIBUTING.md \
  || npx --yes markdownlint-cli@0.49.0 AGENTS.md .github/PULL_REQUEST_TEMPLATE.md CONTRIBUTING.md
```

Expected: no output, exit code 0. If the linter is unavailable in the environment, confirm by inspection that headings/lists keep surrounding blank lines and bullets use `-`.

- [ ] **Step 5: Commit**

```bash
git add AGENTS.md .github/PULL_REQUEST_TEMPLATE.md CONTRIBUTING.md
git commit -m "docs: require an issue per task and link it from every PR"
```

## Self-Review

- **Spec coverage:** issue-first → AGENTS.md bullet + CONTRIBUTING bullet; PR-links-issue → AGENTS.md bullet + PR template section/checklist + CONTRIBUTING bullet; exceptions → noted in all three. Covered.
- **Placeholder scan:** none.
- **Type consistency:** N/A (docs).
