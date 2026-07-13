# SP5 Judge Utilities Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development
> (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship `integrations/judge` — agreement calculator (Spearman/κ/TPR/TNR vs a
hand-labeled set) and panel aggregation — per the SP5 spec.

**Architecture:** One PR wave on branch `feat/judge-utilities`, four tasks: (1) package
scaffold + pure metric functions; (2) scores/labels IO + the `agreement` command; (3) the
`panel` command + CI job; (4) hermetic-E2E assertions and docs. Python only — zero Go
changes; the SP1 provenance seam is consumed, not extended.

**Tech Stack:** Python ≥3.10, stdlib only, hatchling src-layout, pytest; exact formats,
formulas, flags, and exit codes in `docs/specs/2026-07-13-sp5-judge-utilities-design.md` —
the spec is the source of truth; read it first, use it verbatim.

## Global Constraints

- **Stdlib-only** (`dependencies = []`); Python floor `>=3.10`; src-layout mirroring
  `integrations/verifier`; style per that package (`from __future__ import annotations`,
  frozen dataclasses, full type hints; docstrings allowed — codepolicy is Go-only).
- **Deterministic, no RNG, no LLM calls, no network.** Output ordering explicitly sorted.
- Exit codes: 0 ok; 1 only for the opt-in `--min-kappa` gate; 2 usage/IO/format.
- TDD; pytest bar per the verifier package (exhaustive tables for small modules).
- **Zero Go changes**; `regress/power_test.go` untouched; Go gates unaffected.
- markdownlint clean on all new/changed `.md` (README included); shellcheck clean on
  run.sh changes.
- One PR, branch `feat/judge-utilities` from master, squash after green CI.

## File Map

| Task | Create | Modify |
|---|---|---|
| 1 | `integrations/judge/pyproject.toml`, `integrations/judge/src/catacomb_judge/__init__.py`, `.../_metrics.py`, `integrations/judge/tests/test_metrics.py` | — |
| 2 | `.../_io.py`, `.../cli.py`, `.../__main__.py`, `integrations/judge/tests/test_io.py`, `.../tests/test_agreement_cli.py` | `src/catacomb_judge/__init__.py` |
| 3 | `integrations/judge/tests/test_panel_cli.py` | `.../cli.py`, `.github/workflows/python-deepeval.yml` |
| 4 | `integrations/judge/README.md` | `e2e/hermetic/run.sh`, `docs/guide/workflows.md`, `docs/guide/cli.md` |

---

### Task 1: Package scaffold + metrics

**Interfaces (spec §1/§3, verbatim):** pyproject per spec §1 (name `catacomb-judge`,
hatchling, src-layout, `>=3.10`, `dependencies = []`, `test = ["pytest>=7"]`, pytest
`pythonpath = ["src", "."]`, `[project.scripts] catacomb-judge = "catacomb_judge.cli:main"`
— the cli module lands in Task 2; declaring the entry point now is fine).
`_metrics.py` pure functions over `list[float]` pairs:

```python
def spearman(xs: list[float], ys: list[float]) -> float | None
def binarize(values: list[float], threshold: float) -> list[int]
def kappa(a: list[int], b: list[int]) -> float | None
def tpr(labels: list[int], preds: list[int]) -> float | None
def tnr(labels: list[int], preds: list[int]) -> float | None
```

`spearman`: average ranks for ties, Pearson on ranks; `None` when n < 2 or zero variance
on either side. `binarize`: `x >= threshold → 1` (boundary → 1). `kappa`:
`(p_o − p_e)/(1 − p_e)` over binary categories; `None` when `p_e == 1`. `tpr`: `None`
when no positive labels; `tnr`: `None` when no negatives. `__init__.py` exports them +
`__version__ = "0.1.0"`.

- [ ] **Step 1:** Failing tests (hand-computed): spearman perfect=1.0, inverse=-1.0, a
  ties case verified by hand with average ranks, n<2 → None, constant side → None;
  kappa 2×2 hand table (e.g. TP=4 TN=3 FP=2 FN=1 → p_o=0.7, p_e=0.5, κ=0.4), perfect=1,
  chance-level=0, p_e=1 → None; tpr/tnr tables incl. one-sided None; binarize boundary
  (exactly threshold → 1) and defaults.
- [ ] **Step 2:** `cd integrations/judge && pip install -e '.[test]' && pytest -q` → FAIL.
  **Step 3:** Implement. **Step 4:** pytest PASS. **Step 5:** Commit
  `feat(judge): catacomb-judge package — agreement metrics (spearman, kappa, tpr/tnr)`.

### Task 2: IO + `agreement` command

**Interfaces (spec §2/§3, verbatim):** `_io.py`: load scores-JSONL (fields key/value/
run_id/step_key/tool; skip lines without run_id with a stderr note; tolerate unknown
fields; `tool` defaults `"unknown"`), load labels-JSONL (`run_id`/`key`/`label` + optional
`step_key`; duplicate coordinates → format error), `--runs-dir` scanning of
`<dir>/*/scores.jsonl` (sorted). Join on (run_id, key[, step_key]); count
unmatched_labels/unmatched_scores. `cli.py` `agreement` subcommand exactly per spec §3:
flags `--labels` (required), positional scores files and/or `--runs-dir`, `--key`,
`--threshold` (default 0.5), `--min-kappa`, `--json`; per-(key, tool) rows + pooled
`overall` row per key; omitted metrics = absent JSON keys / `-` cells; exit semantics —
1 only under `--min-kappa` when any judge (non-overall) κ < K or omitted; 2 on usage/IO
(argparse `prog="catacomb-judge"`, errors to stderr — the deepeval cli.py pattern).
Deterministic ordering: keys sorted, tools sorted, `overall` last within a key.

- [ ] **Step 1:** Failing tests: join incl. step_key narrowing and unmatched counts;
  duplicate-label exit 2 with message; runs-dir scan sorted; agreement table golden
  (two judges + overall, one omitted metric each way); `--json` exact document (absent
  keys asserted); `--min-kappa` gate both directions incl. omitted-κ ⇒ fail; `--key`
  filter; malformed JSONL line → exit 2 naming file:line; determinism (shuffled input
  files ⇒ identical output).
- [ ] **Step 2:** FAIL. **Step 3:** Implement. **Step 4:** pytest PASS. **Step 5:** Commit
  `feat(judge): agreement command — scores/labels join + calibration gate`.

### Task 3: `panel` command + CI

**Interfaces (spec §4, verbatim):** `panel` subcommand: same scores inputs + `--key`,
`--vote`, `--min-judges` (default 2), `--out`; group by (run_id, key[, step_key]); judges
= distinct `tool` values, lines lacking `tool` skipped with note; groups under
`--min-judges` skipped with note; mean by default; `--vote` = strict majority on values
binarized at 0.5, even judge count in ANY group → usage error exit 2 naming the group.
Output lines compact JSON: `{"key", "value", "run_id", "tool": "panel", "tool_version":
<__version__>, "panel_size": N}` (+ `step_key` when present), sorted by (run_id, key),
stdout or `--out`. CI: third job `judge-sdk` in `.github/workflows/python-deepeval.yml`
cloned from `verifier-sdk` (name `pytest (Python 3.12 + judge-sdk)`, working-directory
`integrations/judge`, same pinned action SHAs).

- [ ] **Step 1:** Failing tests: mean and vote hand-computed (3 judges binary → fraction
  and majority); even-panel exit 2; min-judges skip note; missing-tool skip note; output
  line shape byte-exact incl. field order and `panel_size`; `--out` file written;
  round-trip — panel output parses as agreement-command scores input; ordering
  determinism.
- [ ] **Step 2:** FAIL. **Step 3:** Implement + CI job. **Step 4:** pytest PASS;
  `actionlint` clean on the workflow. **Step 5:** Commit
  `feat(judge): panel aggregation command; judge-sdk CI job`.

### Task 4: E2E and docs

- [ ] **Step 1:** Extend `e2e/hermetic/run.sh` (existing style; append steps, renumber
  nothing; PYTHONPATH wiring mirrors the verifier's at run.sh ~line 127, adding
  `integrations/judge/src`): (a) write a fixture labels JSONL matching the driver's known
  outcomes (base cells `verifier.pass`=1, degraded =0), run `python3 -m catacomb_judge
  agreement --labels ... --runs-dir ... --key verifier.pass --json --min-kappa 0.8` over
  base+degraded evidence pooled → assert exit 0 and exact metrics (perfect agreement:
  kappa 1, tpr 1, tnr 1); (b) flipped-label labels file → assert the hand-recomputed κ
  value and `--min-kappa 0.8` exit 1; (c) panel: fixture two-judge scores files → assert
  mean and vote outputs byte-exactly AND feed the panel output to
  `regress --scores <panel.jsonl> --annotation <key>:higher-better` through the built
  binary asserting the annotation finding appears (loop closed). Re-run driver → PASS;
  state old→new assertion count. shellcheck clean. TDD flip-protocol evidence.
- [ ] **Step 2:** Docs: `integrations/judge/README.md` (sibling READMEs' shape incl. the
  outside-the-Go-gates disclaimer; labels format, both commands, exit codes);
  `docs/guide/workflows.md` new `## Calibrating a judge` section after the audit section
  (labels format, metric omission semantics, κ>0.8 guidance, panel → `--scores` loop,
  canon caveats: ~40 labeled transcripts suffice; judge never from the tested model's
  family); `docs/guide/cli.md` cross-reference from the scores/provenance passage.
  markdownlint clean via the repo invocation.
- [ ] **Step 3:** Full gates (Go untouched — still run `make cover`/`make lint`/
  TestGatePower to prove it; pytest all three Python packages; hermetic driver;
  shellcheck; markdownlint; actionlint). Commit
  `test(e2e): judge agreement + panel assertions; docs`.

## Acceptance

Hermetic driver green with the new assertions on the PR; both commands proven end-to-end
(agreement gate both directions; panel output consumed by `regress --scores` through the
built binary); judge-sdk CI job green; zero Go diffs; docs accurate.

## Self-review notes

Spec coverage: §1→Task 1 scaffold, §2→Task 2 IO, §3→Task 2 CLI, §4→Task 3, §6→per-task
test lists + Task 4. Interfaces consistent: Task 2 consumes `_metrics` signatures exactly
as Task 1 defines them; Task 3 reuses Task 2's `_io` loaders; Task 4 invokes via
`python3 -m catacomb_judge` (the `__main__.py` Task 2 ships). No placeholders; formulas,
defaults, exit codes, and output shapes stated exactly.
