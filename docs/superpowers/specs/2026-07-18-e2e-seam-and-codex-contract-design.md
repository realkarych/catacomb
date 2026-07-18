# Design — export→deepeval e2e seam + codex fixture contract test

**Date:** 2026-07-18
**Status:** approved (brainstorming), pending spec review
**Motivation:** the 2026-07-18 e2e-coverage audit found the functional surface
near-completely covered, with two residual seams left unasserted end-to-end:

1. `catacomb export` output is never fed into `catacomb-deepeval` by any test —
   each side is exercised alone (export in e2e, deepeval in its own pytest), so
   the handoff between them is untested.
2. The hermetic codex fake fixtures (`e2e/hermetic/prod/fixtures/*-codex*.jsonl.tmpl`)
   are hand-authored and can silently drift from the real codex rollout format
   the project pins and validates against (`drift.TestedCodexVersion`).

This spec closes both, plus schedules one ad-hoc live gate run afterward.

## Scope

- **F1** — an `export → catacomb-deepeval` seam test at two levels sharing one fixture.
- **F2** — a Go contract test in `ingest/codex` anchoring the codex fixtures to the
  pinned version, plus the version sync it forces.
- **Item 3** — an out-of-schedule E2E Live Gate run once F1 and F2 land.

## Non-goals

- `catacomb-deepeval --trace-metrics` (needs `ANTHROPIC_API_KEY` and makes network
  calls) — out of scope; both seam levels stay offline and keyless.
- Any change to the codex parser (`ingest/codex`) behavior.
- A genuine live codex rollout capture as the contract anchor — deferred; the chosen
  anchor is the curated `ingest/codex/testdata` reference (see F2 limitation).

---

## F1 — export→deepeval seam

### Shared fixture

A single committed Claude Code transcript at
`integrations/deepeval/tests/testdata/seam_session.jsonl`, modeled on the existing
Claude transcript fixtures (e.g. `e2e/hermetic/transcript.jsonl.tmpl`). It contains
one `user_prompt`, one `assistant_turn`, and two `tool_call` records with distinct,
stable tool names (a `Bash` call and an `mcp__…` call). Tool-name identity is all the
default `ToolCorrectnessMetric` (name-match) needs, so payload capture is optional;
if payloads are included they must be already-redacted.

Two expected-tools JSON files live beside it:

- `seam_expected_pass.json` — the exact two tool names the transcript calls → metric
  score `1.0` → PASS.
- `seam_expected_fail.json` — a single tool name the transcript never calls → metric
  score `0.0` → FAIL (score `0` is unambiguously below the `0.5` threshold, so the
  FAIL direction cannot flake on scoring-formula details).

### Level A — per-PR hermetic author-mode smoke ($0, zero dependency)

New scenario `e2e/hermetic/prod/scenarios/85-deepeval-seam.sh`, auto-registered by the
`prod/run.sh` `scenarios/*.sh` glob (no dispatcher edit needed). It:

1. runs `catacomb export "$REPO/integrations/deepeval/tests/testdata/seam_session.jsonl" --to jsonl --out s.jsonl`;
2. runs `python3 -m catacomb_deepeval s.jsonl` (author mode — no `--expected`, so the
   heavyweight `deepeval` package is never imported; the invocation uses the package's
   `__main__.py`);
3. parses the printed JSON and asserts `tools_called` equals the expected two names and
   that `input` / `actual_output` are present.

`integrations/deepeval/src` is added to the `PYTHONPATH` export in
`e2e/hermetic/run.sh:190`, alongside the existing `integrations/verifier/src` and
`integrations/judge/src`, so `python3 -m catacomb_deepeval` resolves without install.

This proves the export→reader→adapter half of the handoff on every PR, deterministically,
with no API spend and no package install.

### Level B — full metric seam in `python-deepeval.yml`

A new job `seam` in `.github/workflows/python-deepeval.yml`:

1. `actions/setup-go` (from `go.mod`) + `make build`, calling `bin/catacomb` directly
   (no `PATH` step);
2. `actions/setup-python` 3.12 + `pip install -e 'integrations/deepeval[deepeval]'`
   from the repo root (brings in `deepeval`);
3. `catacomb export …/seam_session.jsonl --to jsonl --out s.jsonl`;
4. `catacomb-deepeval s.jsonl --expected seam_expected_pass.json` → assert **exit 0**;
5. `catacomb-deepeval s.jsonl --expected seam_expected_fail.json` → assert **exit 1**.

This runs the real `ToolCorrectnessMetric` over a real catacomb export in both verdict
directions — the complete handoff including the metric.

### Why both

Level A gives always-on, per-PR, zero-cost protection of the export/adapter contract;
Level B proves the metric actually runs and gates over a real export. Neither subsumes
the other: A cannot run the metric (no `deepeval` in the hermetic job), B does not run
per-PR-fast and depends on the heavyweight install.

---

## F2 — codex fixture contract test

### Reference format and the drift it exposes

The curated codex reference lives in `ingest/codex/testdata/*.jsonl`
(`basic`, `child`, `mcp`, `tools`). Every rollout record has the envelope
`{timestamp, type, payload}` with `type ∈ {session_meta, turn_context, event_msg,
response_item, inter_agent_communication}` and, on `event_msg`/`response_item`,
`payload.type ∈ {task_started, user_message, message, reasoning, function_call,
function_call_output, custom_tool_call, custom_tool_call_output, mcp_tool_call,
mcp_tool_call_begin, mcp_tool_call_end, task_complete, token_count}`.

Both the reference testdata and the fake fixtures currently stamp `cli_version`
`0.144.4`, while the code pins `drift.TestedCodexVersion = "0.144.5"` (the version
`e2e-live.yml` installs and validates against). That mismatch is the drift the contract
test surfaces and then guards.

### The test

New `ingest/codex/contract_test.go`. It is test-only — no production `.go` code is
added — so the 100% coverage gate is unaffected. It asserts:

1. **Type subset.** `canon` is the set of `(type, payload.type)` pairs parsed from
   `ingest/codex/testdata/*.jsonl`. For every codex rollout fixture under
   `e2e/hermetic/prod/fixtures/*-codex*.jsonl.tmpl` (reached by a repo-relative path),
   the emitted `(type, payload.type)` pairs must be a subset of `canon`. A violation
   fails the test listing the offending fixture, line, and unknown pair — this is what
   catches a fake inventing a record shape the reference format does not have.

   Extraction reads the literal `type` and `payload.type` string tags from each rollout
   line. These tags are never templated, so extraction tolerates the `__PLACEHOLDER__`
   tokens that appear elsewhere in the fixtures (the implementation extracts the tags
   without requiring the whole line to be valid JSON — e.g. per-line lenient parse or a
   targeted scan; the plan picks the concrete mechanism).

2. **Version sync.** Every `session_meta` `cli_version` in both the testdata and the
   fixtures equals `drift.TestedCodexVersion`. This is the anchor to `0.144.5`.

### The version bump it forces

Assertion (2) fails until `0.144.4` is bumped to `0.144.5` everywhere it is stamped or
asserted: the 11 `*-codex*.jsonl.tmpl` fixtures, the 4 `ingest/codex/testdata/*.jsonl`
files, the 3 scenario scripts that assert `agent_version == "0.144.4"`
(`55-codex-import.sh`, `56-codex-bench.sh`, `58-codex-subagent.sh`), and the
`ingest/codex/codex_test.go` assertion — 19 files total. This is a patch-level bump; the
record envelope is unchanged, so the structural (subset) assertions stay green through it.
Going forward, when someone bumps `TestedCodexVersion` for a real codex release that
changed the envelope, this test fails until the fixtures are refreshed — exactly the
guard the audit called for.

### Known limitation

`canon` derives from the hand-authored testdata, not a byte-for-byte live capture. The
contract therefore proves internal consistency (fakes ⊆ reference) plus a hard tie to
the pinned version — it does not prove the reference itself matches reality. Item 3's
codex leg is the opportunity to later refresh the testdata from a genuine `0.144.5`
rollout; that strengthening is out of this spec's scope.

---

## Item 3 — ad-hoc live gate run

After F1 and F2 are merged (or at minimum green on their branches), trigger the E2E
Live Gate out of its twice-weekly schedule. This spends real API budget (~$3–7) and
needs an Anthropic auth secret (and optionally `CODEX_API_KEY` for the codex leg).
The trigger mechanism (GitHub `workflow_dispatch` via `gh workflow run e2e-live.yml`
vs. a local `e2e/run.sh` run against locally-authenticated CLIs) and the budget go-ahead
are confirmed with the user immediately before dispatch, not assumed here.

---

## Testing, gates, and execution

- **Go:** `contract_test.go` adds no production code, so `make cover` stays at 100%.
  No comments in the new `_test.go` (codepolicy). `gofumpt`/`goimports` applied.
- **Hermetic e2e:** `e2e/hermetic/run.sh` run locally must stay green — codex scenarios
  55–59 and the new scenario 85 — after the version bump.
- **Python:** `integrations/deepeval` pytest stays green; the new `seam` job runs the
  full chain.
- **Execution model (per CLAUDE.md):** worktree-isolated, TDD-first, subagent-driven with
  a review after each task. F1 and F2 both touch `e2e/hermetic/prod/` (F1 adds scenario
  85 and the `run.sh:190` PYTHONPATH line; F2 bumps `cli_version` across fixtures and
  scenarios), so they are **serialized — F2 first (bump + contract), then F1 (seam)** —
  to avoid fixture-file collisions rather than run as parallel worktrees. Final task
  breakdown and PR structure (one logical change per PR) are produced by writing-plans.
