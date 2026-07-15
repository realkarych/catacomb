# Catacomb Agent Skill — Design Spec

**Status:** Draft
**Date:** 2026-07-15
**License:** Apache-2.0
**Audience:** engineers adopting catacomb in their own Claude Code projects

> *One bundled Claude Code Agent Skill that lets a Claude Code agent drive catacomb on an engineer's behalf — scaffold a basket, run the gate, read the verdict, tighten fidelity — without the engineer having to memorize the CLI surface.*

---

## 1. Summary

Catacomb is a CLI. Adopting it still asks an engineer to learn a basket schema, the
right `claude` stream-json flags, a `regress` report grammar, and a CI wiring pattern.
This spec defines a single **bundled Agent Skill**, `catacomb`, that carries that
knowledge so a Claude Code agent working in the engineer's own repository can do the
fiddly parts: inspect how the project invokes `claude`, write `agent.sh` and
`basket.yaml`, run the first `bench`, wire the CI merge gate, interpret a `regress`
report, and — as a second wave — add checkpoints, verifiers, baselines, and import.

The skill is **one umbrella skill with progressive disclosure**: a short `SKILL.md`
router plus a `references/` tree. The router carries the trigger surface and dispatches
to a reference file per workflow; depth lives in the references so the skill's resident
context stays small.

The skill teaches Claude to *drive the shipped CLI*. It does not reimplement, wrap, or
restate catacomb's behavior; where catacomb's own docs are canonical, the references
point at them rather than copy them.

## 2. Goals & Non-Goals

### Goals

- **One skill, broad reach.** A single trigger surface that fires on the full span of
  catacomb intents (setup, CI, reading reports, checkpoints/verifiers, baselines,
  import, troubleshooting) and routes to the right reference.
- **Claude does the fiddly part.** The value is Claude producing correct artifacts —
  `agent.sh` with the right flags, a `basket.yaml` for the *real* agent, a CI job, a
  verifier — not a prose re-explanation of the CLI.
- **Grounded in the shipped surface.** Every command, flag, selector, and exit code the
  skill emits matches the current CLI and the guide under `docs/guide/`.
- **Small resident footprint.** `SKILL.md` stays a router; workflow depth lives in
  `references/` and loads only when the workflow is engaged.
- **Stands alone.** The skill is authored and versioned in this repo but depends on
  nothing repo-internal at runtime — it works in any engineer's project against an
  installed `catacomb` binary.

### Non-Goals

- **Not a plugin/marketplace deliverable in this spec.** The skill lives in-repo under
  `skills/`. Publishing that directory as a distributable plugin is a later, separate
  concern; the design must not preclude it, but does not implement it.
- **No new CLI behavior.** The skill only drives existing commands. Any gap it exposes
  in the CLI is filed separately, not patched inside the skill.
- **Not a docs replacement.** The guide under `docs/guide/` stays canonical for
  reference detail; the skill links to it rather than forking it.
- **No multiple task-scoped skills.** Explicitly rejected in favor of the umbrella
  form (see §4).

## 3. Background & Constraints

- **Form factor.** Claude Code Agent Skill: a `SKILL.md` with YAML frontmatter
  (`name`, `description`) plus supporting files, using progressive disclosure — the
  `description` decides when the skill loads, and the body pulls in `references/*.md`
  on demand.
- **In-repo, Markdown.** The skill is Markdown, not Go. The repo's Go gates
  (no-comments, 100% coverage, gofumpt) do not apply to it. The worktree and
  feature-branch/PR workflow still do.
- **Trigger economics of one skill.** With a single skill, the `description` is the
  only thing standing between an engineer's phrasing and the skill loading. It must
  enumerate the intents concretely; a vague one-liner under-triggers.
- **Ground truth drifts.** The CLI evolves. The skill's correctness is pinned to the
  CLI at authoring time; keeping it current is an ongoing maintenance obligation, noted
  in §8.

## 4. Decision: one umbrella skill

Three structures were considered:

- **A — one umbrella skill + `references/`** *(chosen).* Single trigger, progressive
  disclosure. Cohesive, low duplication, one place to maintain.
- **B — a set of task-scoped skills (one per focus area).** Crisper per-intent
  triggering, but more surfaces to maintain and shared vocabulary duplicated across
  skills.
- **C — one skill per CLI command.** Maximally precise, but noisy and fights catacomb's
  own "cohesive units" principle.

**A is chosen.** The workflows share one vocabulary (basket, cell, evidence, phases,
bands, verdicts, exit codes) and one narrative arc (set up → run → read → tighten),
which a single skill with a shared `concepts` reference expresses without duplication.
The cost — a `description` that must carry every intent — is paid once in §5 and is
cheaper than keeping several descriptions and several copies of the shared vocabulary
in sync.

## 5. Structure

```
skills/catacomb/
  SKILL.md            router: what catacomb is, when to load, dispatch table
  references/
    concepts.md       shared vocabulary the other references lean on
    setup.md          agent.sh + basket.yaml, capture pre-flight, sizing, first bench
    ci-gate.md        CI job (GitHub Actions, SHA-pinned), secrets, baseline, --record, exit-code gate
    read-report.md    reading a regress table + diagnosing verdicts → next action
    accuracy.md       MCP mark checkpoints, per-task verifiers, baselines/trends
    import.md         ingesting interactive TUI sessions
    troubleshoot.md   install/version, sign-in, format drift, cost surprises
```

### 5.1 `SKILL.md`

- **Frontmatter.** `name: catacomb`. `description` enumerates the intents so the single
  trigger fires across the span:

  > *Use when working with catacomb, the offline regression gate for Claude Code agents
  > — setting up a basket and agent.sh, running `bench`, wiring the CI merge gate,
  > reading a `regress` report (verdicts, noise bands, coverage), adding phase
  > checkpoints or per-task verifiers, pinning baselines/trends, importing interactive
  > sessions, or troubleshooting install/capture issues.*

- **Body (~20–30 lines).** Two short paragraphs on what catacomb is and the pipeline
  (`bench → reduce → step/phase keys → aggregate → regress`), then a **dispatch table**
  mapping intent → reference file. No workflow depth in the body itself.

### 5.2 Reference files

Each reference is a self-contained workflow Claude can execute, leaning on
`concepts.md` for vocabulary rather than restating it.

- **`concepts.md`** — basket / cell / evidence dir; phases & checkpoints; noise band;
  `steps_trusted`; verdicts `ok` / `regression` / `insufficient`; exit codes
  `0` / `1` / `2`. Links to `docs/guide/concepts.md` as canonical.
- **`setup.md`** *(flagship)* — inspect how the project invokes `claude`; scaffold
  `agent.sh` with the correct stream-json flags
  (`--output-format stream-json --verbose --strict-mcp-config --setting-sources project`);
  frame the change-under-test as **baseline vs candidate**; author `basket.yaml`
  (tasks × variants × reps); run a **single-cell capture pre-flight** to confirm the
  transcript resolves before spending on a full basket; **size** the basket (reps /
  task count / rough cost, informed by ADR-0023's small-`k` sensitivity); run the first
  `bench`; hand off to `read-report`.
- **`ci-gate.md`** — generate a GitHub Actions job first (Actions pinned to commit SHAs,
  matching repo convention), wire secrets (`ANTHROPIC_API_KEY` or
  `CLAUDE_CODE_OAUTH_TOKEN`), pin a baseline, run `bench` candidate → `regress --record`
  → gate on exit code, persist `runs/` as artifacts, expose cost knobs (reps / model).
  Note non-GitHub CI as a variant.
- **`read-report.md`** — walk a real `regress` table: run-total headline, `BAND`,
  `coverage`, `steps_trusted`, total vs paired vs phase rows, `sensitivity` and `audit`
  lines; then map each shape to a next action (more reps; ≥5 tasks to arm the paired
  gate; add checkpoints when step coverage is low).
- **`accuracy.md`** — wire MCP `mark` checkpoints (`catacomb mcp`, `--mcp-config`, one
  CLAUDE.md instruction, `checkpoints:` on the task); write a per-task verifier
  (`artifacts:` + `verify.cmd`) using the shipped Python SDK under
  `integrations/verifier`; pin a baseline and replay `trends`.
- **`import.md`** — drive `catacomb import` to shape a hand-run interactive session into
  the same evidence a bench cell produces, so `verify` / `regress` work on it unchanged.
- **`troubleshoot.md`** — stale pre-pivot install (no `bench` / `regress`), `claude` not
  signed in, transcript-newer-than-tested format-drift warnings, unexpected cost.

## 6. Scope for v1

All reference files are created up front (Markdown is cheap), filled to depth by
priority:

- **v1, filled densely:** `concepts.md`, `setup.md`, `ci-gate.md`, `read-report.md` —
  the adoption spine (zero → first gate → CI → reading the verdict).
- **v1, skeleton + essentials:** `accuracy.md`, `import.md`, `troubleshoot.md` —
  correct and usable, deepened in a later wave.

`SKILL.md` is complete in v1: the router must dispatch to every reference from day one.

## 7. Validation

- **Trigger check.** Confirm the `description` loads the skill on representative
  phrasings ("help me start gating my agent", "add catacomb to CI", "what does this
  regress verdict mean", "make the gate more trustworthy", "ingest my interactive
  session").
- **Fidelity check.** Every command, flag, selector, and exit code emitted by a
  reference is cross-checked against the current CLI and `docs/guide/`.
- **Dry run.** Exercise `setup.md` end-to-end against a throwaway project — Claude
  produces `agent.sh` + `basket.yaml`, the capture pre-flight passes, `bench` records
  evidence, `regress` returns a verdict.
- **Authoring conformance.** Run the skill through the `superpowers:writing-skills`
  flow (frontmatter, description quality, progressive-disclosure hygiene).
- **Markdown lint.** The skill's Markdown passes the repo `markdownlint` config.

## 8. Maintenance

- **CLI drift.** The skill's correctness is pinned to the CLI at authoring time. When a
  command, flag, or report column changes, the affected reference is updated in the
  same change — treat the skill as part of the CLI's compatibility surface for
  documentation purposes.
- **Guide as canonical.** References link to `docs/guide/` for reference-level detail
  rather than forking it, so guide updates propagate without editing the skill.

## 9. Open questions

- **Distribution.** In-repo under `skills/` for now; whether and how to publish it as a
  standalone plugin is deferred (§2 Non-Goals).
- **Overview router as a skill.** A separate lightweight `catacomb-overview` router is
  intentionally omitted; revisit only if the single `description` proves to
  under-trigger in practice.
