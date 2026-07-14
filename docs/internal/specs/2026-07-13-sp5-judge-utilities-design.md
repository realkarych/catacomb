# SP5 — Judge utilities: design

- **Date:** 2026-07-13
- **Status:** approved design (ADR-0027 SP5; decisions adjudicated there and in the
  [gap review](../reviews/2026-07-12-eval-best-practices-gap-review.md) §2.4/§4.5; execution
  pre-approved)
- **Related:** [ADR-0027](../../adr/0027-verification-layer-and-reliability-metrics.md),
  SP1 spec [2026-07-12-sp1-verifier-contract-design.md](2026-07-12-sp1-verifier-contract-design.md)
  (scores dialect + provenance), `integrations/verifier` (sibling package conventions),
  `integrations/deepeval` (console-script precedent)

SP5 closes the ADR-0027 wave: **judge meta-evaluation utilities** — an agreement calculator
(Spearman ρ, Cohen's κ, TPR/TNR against a hand-labeled set) and panel aggregation — as a new
stdlib-only Python package in `integrations/`. No LLM calls, no network, no judge runner, no
alignment UI (ADR-0027 non-goals; that ground is LangSmith's). The Go core is untouched: the
SP1 provenance seam (verbatim `tool`/`tool_version`/`prompt_hash` on `scores.jsonl` lines,
preserved on disk, ignored by the gate) is the entire dependency — judge scores and their
provenance coexist only in evidence `scores.jsonl` files, which is exactly where these
utilities read.

## 1. Package

`integrations/judge/` — peer of `verifier/` and `deepeval/`, following the verifier's
conventions and the deepeval CLI precedent:

- `pyproject.toml`: name `catacomb-judge`, version `0.1.0`, hatchling, src-layout
  (`src/catacomb_judge`), `requires-python = ">=3.10"`, `dependencies = []` (stdlib-only,
  hard policy), optional `test = ["pytest>=7"]`, pytest `pythonpath = ["src", "."]`,
  `[project.scripts] catacomb-judge = "catacomb_judge.cli:main"`.
- Modules: `__init__.py` (public API + `__version__`), `_metrics.py` (pure functions),
  `_io.py` (scores/labels loading + joining), `cli.py` (argparse), `__main__.py`
  (`python3 -m catacomb_judge` — the hermetic driver runs the SDK off `PYTHONPATH`, no pip).
- Style per the verifier package: `from __future__ import annotations`, frozen dataclasses,
  full type hints, docstrings allowed (codepolicy is Go-only), `_`-prefixed private helpers.
- Exit codes (deepeval/regress convention): 0 ok; 1 calibration-gate failure (only when a
  gate flag is passed, see §3); 2 usage/IO/format errors (argparse errors, unreadable files,
  malformed lines).

## 2. Inputs

- **Judge scores** — the SP1 scores-JSONL dialect, read from explicit file paths and/or
  `--runs-dir <dir>` (scans `<dir>/*/scores.jsonl`, the bench evidence layout). Relevant
  fields per line: `key` (strict `owner.key`), `value` (number), `run_id` (required for the
  join; lines without it are skipped with a stderr note), optional `step_key`, optional
  provenance — **`tool` identifies the judge** (missing `tool` ⇒ judge id `"unknown"`).
  Unknown extra fields tolerated (mirror of the Go parser's tolerance).
- **Hand-labeled set** (greenfield format, defined here): JSONL, one object per line:

  ```json
  {"run_id": "r-01", "key": "judge.groundedness", "label": 1}
  ```

  `run_id` and `key` join a label to score lines on the same coordinates; `label` is a
  number; optional `step_key` narrows the join to step-level lines. Duplicate
  (run_id, key[, step_key]) label lines are a format error (exit 2) — a gold set must be
  unambiguous. Labels with no matching score (and vice versa) are counted and disclosed,
  never silently dropped.

## 3. `catacomb-judge agreement`

```text
catacomb-judge agreement --labels labels.jsonl (--runs-dir DIR | scores.jsonl ...)
                         [--key judge.x] [--threshold 0.5] [--min-kappa K] [--json]
```

Joins scores to labels on (run_id, key[, step_key]) and reports, per `key` and per judge
(`tool`), plus an `overall` row per key pooling all judges:

- `n` — matched pairs; `unmatched_labels` / `unmatched_scores` — disclosure counts.
- **Spearman ρ** — on raw values vs raw labels; average ranks for ties; omitted (JSON key
  absent, `-` in the table) when n < 2 or either side has zero variance.
- **Binarization** for the three categorical metrics: both score and label binarize at the
  same `--threshold` (default 0.5): `x >= threshold → 1`. The canon's dominant judge shape
  is binary 0/1, which passes through unchanged at the default.
- **Cohen's κ** — on binarized pairs, `κ = (p_o − p_e)/(1 − p_e)`; omitted when `p_e = 1`
  (both sides constant at the same value — κ undefined).
- **TPR** = TP/(TP+FN), omitted when the label set has no positives; **TNR** = TN/(TN+FP),
  omitted when no negatives — the imbalance-robust pair the canon prescribes instead of
  accuracy (OpenAI flywheel; gap review §2.4).
- `--key` restricts to one annotation key (default: every key present in both inputs).
- `--min-kappa K` is the optional calibration gate (Patronus canon: κ > 0.8): exit 1 when
  any reported judge κ (not `overall`) is below K or omitted; without the flag the command
  never exits 1. This is the only gating semantic in SP5, it is opt-in, and it lives
  entirely outside the Go gate.
- Human output: one tabwriter-style aligned table (KEY / JUDGE / N / SPEARMAN / KAPPA /
  TPR / TNR) plus disclosure lines for unmatched counts; `--json` emits the same as a
  single JSON document (omitted metrics = absent keys, mirroring the Go omitempty
  discipline).

## 4. `catacomb-judge panel`

```text
catacomb-judge panel (--runs-dir DIR | scores.jsonl ...) [--key judge.x]
                     [--vote] [--min-judges 2] [--out FILE]
```

Aggregates a panel of heterogeneous judges (PoLL pattern, gap review §2.4) into a single
score stream **emitted in the same scores-JSONL dialect**, so the output feeds directly
back into `regress --scores` / evidence:

- Groups score lines by (run_id, key[, step_key]); each distinct `tool` is one judge.
  Groups with fewer than `--min-judges` (default 2) distinct tools are skipped with a
  stderr note — a panel of one is not a panel. Lines lacking `tool` are skipped with a
  note (panel identity requires provenance; this is the SP1 dependency doing its job).
- Default aggregation: **mean** of the judges' values (binary in → agreement fraction out).
  `--vote`: strict majority on values binarized at 0.5 — requires an odd number of judges
  in every group (even counts are a usage error, exit 2, naming the offending group) so
  ties cannot occur.
- Output lines: `{"key": <key>, "value": <aggregate>, "run_id": ..., "tool": "panel",
  "tool_version": <package version>, "panel_size": N}` (+ `step_key` when grouped on it) —
  compact JSON, stdout by default, `--out FILE` to write a file usable as
  `regress --scores`.

## 5. Dormancy, compatibility, boundaries

- Zero Go changes; no store/schema/Record surfaces touched; the gate's behavior is
  byte-identical. The utilities are read-only over evidence.
- stdlib-only, no RNG, deterministic: same inputs ⇒ same outputs (map iteration is
  neutralized by explicit sorting of keys/judges/run_ids everywhere output order matters).
- Not in SP5: judge runner / LLM calls / alignment UI (ADR non-goals); multi-class κ
  (binary via threshold only — revisit with field evidence); judge consistency re-scoring
  (Bloom ×5 — operator procedure, not a library); weighting schemes in panel aggregation
  (mean and strict-majority only); self-preference detection; Go-side parsing of
  provenance.

## 6. Testing and acceptance

- pytest per the verifier package's bar (exhaustive small-module tables; `capsys`,
  `tmp_path`, `pytest.raises(..., match=...)`); TDD. No Python coverage gate exists in CI —
  match the sibling packages' de-facto thoroughness anyway.
- **Metric correctness against hand-computed values**: Spearman incl. average-rank ties and
  both omission cases; κ incl. a hand-computed 2×2 table, κ=1 perfect, κ=0 chance-level,
  p_e=1 omission; TPR/TNR incl. one-sided omission; binarization boundary (value exactly at
  threshold → 1).
- **CLI behavior**: exit codes (0/1/2) incl. `--min-kappa` gate both directions; duplicate
  label error; unmatched disclosure counts; deterministic output ordering; `--json` shape
  with absent-key omissions; panel mean/vote incl. even-panel usage error and
  sub-`--min-judges` skip notes; round-trip — panel output parses as valid scores-JSONL
  input to the agreement command itself.
- **CI**: third job in `.github/workflows/python-deepeval.yml` cloned from the
  `verifier-sdk` job (`pytest (Python 3.12 + judge-sdk)`, working-directory
  `integrations/judge`, same pinned action SHAs).
- **Hermetic E2E extension** (append; renumber nothing): using the driver's existing
  evidence (base 5/5 `verifier.pass=1`, degraded 0/5) plus a fixture labels file matching
  those outcomes, invoke `python3 -m catacomb_judge agreement` (PYTHONPATH onto
  `integrations/judge/src`, mirroring the verifier wiring) and assert exact deterministic
  metrics on the pooled pairs (perfect agreement: κ=1, TPR=1, TNR=1) and the `--min-kappa
  0.8` gate exiting 0; then a deliberately flipped-label variant asserting the recomputed
  κ value and gate exit 1. Panel: two-judge fixture scores → mean and vote outputs asserted
  byte-exactly, and the panel output accepted by `regress --scores` through the built
  binary (loop closed).
- **Docs**: `integrations/judge/README.md` (with the standard outside-the-Go-gates
  disclaimer); `docs/guide/workflows.md` new section "## Calibrating a judge" after the
  SP4 audit section (labels format, agreement metrics and when each is omitted, κ>0.8
  guidance, panel workflow feeding `--scores`, the canon's caveats: ~40 labeled transcripts
  suffice, judge never from the tested model's family); cli.md cross-reference from the
  scores/provenance section to the new workflow.
