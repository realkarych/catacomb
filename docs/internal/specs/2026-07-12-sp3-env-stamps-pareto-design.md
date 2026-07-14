# SP3 — Env stamps + Pareto table: design

- **Date:** 2026-07-12
- **Status:** approved design (ADR-0027 SP3; decisions adjudicated there and in the
  [gap review](../reviews/2026-07-12-eval-best-practices-gap-review.md) §4.3; execution pre-approved)
- **Related:** [ADR-0027](../../adr/0027-verification-layer-and-reliability-metrics.md),
  [ADR-0017](../../adr/0017-data-format-versioning-and-migration.md) (stamp discipline),
  [ADR-0026 §6](../../adr/0026-form-factor-pivot-offline-eval-gate.md) (version stamps),
  SP1 spec [2026-07-12-sp1-verifier-contract-design.md](2026-07-12-sp1-verifier-contract-design.md),
  SP2 spec [2026-07-12-sp2-reliability-metrics-design.md](2026-07-12-sp2-reliability-metrics-design.md)

SP3 adds the two provenance/reporting instruments ADR-0027 sequenced after the reliability
metrics: **environment stamps** on per-run evidence (`meta.json`) and an **accuracy-vs-cost
Pareto table** over recorded regress history (`trends`). Both are deterministic, offline,
stdlib-only. The competitive slot for the Pareto view is empty (gap review §3: "Nobody").

## 1. Env stamps in `evidence.Meta`

`meta.json` gains one nested, additive object:

```go
type EnvStamps struct {
	CatacombVersion   string    `json:"catacomb_version"`
	ModelID           string    `json:"model_id,omitempty"`
	ClaudeCodeVersion string    `json:"claude_code_version,omitempty"`
	Resources         Resources `json:"resources"`
}

type Resources struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
	CPUs int    `json:"cpus"`
}
```

`evidence.Meta` gains `Env *EnvStamps` (JSON `env`, omitempty). All additions are additive:
old meta.json files parse unchanged (nil `Env`), old binaries ignore the new key.

- **Sources (all already in hand at bench time; no new capture):**
  - `CatacombVersion` — the build-time `Version` var (same source as `currentStamps()`).
  - `ModelID` — the reduced offline graph's run model (`RunsSnapshot()` entry matching the
    cell's session id; populated by reduce from the assistant-message `model` field). Ground
    truth for what the child actually ran, not what was requested. Empty (omitted) when the
    transcript carries no assistant turn.
  - `ClaudeCodeVersion` — the transcript's top-level `version` field, already reduced into
    `Run.Repro.ClaudeCodeVersion`. Omitted when absent.
  - `Resources` — `runtime.GOOS`, `runtime.GOARCH`, `runtime.NumCPU()` at bench time on the
    host that executed the cell. Descriptive of the harness host; always present.
- **Stamping point:** `offlineMeta` (bench offline evidence write), reading the graph that
  `recordOfflineEvidence` already builds. The artifacts second pass (`StampArtifacts`) is
  untouched and keeps re-writing `meta.json` with the full struct, so `env` survives it.
- **Sampling params — documented deviation from the ADR wording.** Claude Code transcripts
  carry no temperature/sampling fields, and catacomb injects none into the child; the only
  place sampling flags can exist is the basket-authored child argv, which is already pinned
  byte-exactly by `basket_hash`. SP3 therefore stamps no sampling params; provenance for them
  is `basket_hash`. Revisit only if the transcript format starts reporting them.
- **Descriptive, never gating.** Env stamps do NOT join `model.Stamps`, `Mismatch`, or the
  `--strict` stamp check: model drift between groups is frequently the benchmark axis itself
  (comparing model A vs model B is the point), and host resources vary legitimately across
  runners. Comparability enforcement stays where ADR-0026 §6 put it (catacomb version +
  step-key scheme on baselines/records). Env stamps disclose; they do not refuse.
- **Hostname/runner identifiers are deliberately excluded** (non-deterministic,
  potentially sensitive; evidence dirs are a redaction surface per ADR-0024).

## 2. Pareto table in `trends`

`catacomb trends <baseline> --pareto` renders an accuracy-vs-cost table over the recorded
regress history; `--pareto --json` emits the same as JSON. No chart, no new persistence:
every axis is read from the already-stored `regress.Record` findings (`Record.V` stays 1,
schema untouched).

- **Point extraction.** Each stored record contributes one **candidate point**:
  - accuracy = the `total`-scope finding with `Metric == "ann:verifier.pass"`, `Candidate`
    value (the higher-better pass rate the gate already uses);
  - cost = the `total`-scope finding with `Metric == "cost_usd"`, `Candidate` value
    (median cost per run).
  One **baseline point** is added from the newest record's `Baseline` values of the same two
  findings (label `baseline`). A point missing either finding (pre-SP1 records with no
  verifier axis, cost-less evidence) is still listed but is **not comparable**. An absence
  finding — the total-scope `ann:verifier.pass` finding recorded when exactly one comparison
  side carried the annotation — provides no axis for either point: its zero-valued sides are
  placeholders, not measurements, so such rows take the non-comparable path above.
- **Domination rule.** Over comparable points only: point A is dominated iff some point B has
  `accuracy(B) >= accuracy(A)` and `cost(B) <= cost(A)` with strict inequality on at least
  one axis. Equal-on-both-axes points do not dominate each other (the hermetic A-vs-A pair
  must render as two non-dominated rows). Non-comparable points carry no domination verdict.
- **Ordering.** Rows sort by cost ascending, then accuracy descending, then seq ascending —
  the Pareto frontier reads top-down; non-comparable rows sink to the bottom in seq order.
- **Human render** (tabwriter, trends conventions):

  ```text
  SEQ  CREATED               CANDIDATE                 ACCURACY  COST_USD  DOMINATED
  -    -                     baseline                  1.00      0.0102    no
  2    2026-07-12T18:04:11Z  label:variant=cand        1.00      0.0102    no
  1*   2026-07-12T17:58:03Z  label:variant=degraded    0.00      0.0102    yes
  3    2026-07-12T18:09:44Z  label:variant=old         -         0.0110    -
  ```

  The existing splice marker (`*` + footnote) applies unchanged. When any listed point is
  non-comparable, one epilogue note (sensitivity-note discipline) says how many rows lack an
  axis and why they carry no verdict: `pareto: N row(s) lack an accuracy axis (no
  ann:verifier.pass finding) and are not compared`.
- **JSON shape** (`--pareto --json`):

  ```json
  {
    "baseline": "sql-suite",
    "points": [
      {"source": "baseline", "accuracy": 1.0, "cost_usd": 0.0102, "dominated": false},
      {"source": "record", "seq": 2, "candidate": "label:variant=cand",
       "created_at": "2026-07-12T18:04:11Z", "accuracy": 1.0, "cost_usd": 0.0102,
       "dominated": false, "spliced": false},
      {"source": "record", "seq": 3, "candidate": "label:variant=old",
       "created_at": "2026-07-12T18:09:44Z", "cost_usd": 0.011, "spliced": false}
    ]
  }
  ```

  `accuracy`/`cost_usd` are omitted when the finding is absent; `dominated` is omitted (not
  false) for non-comparable points; `spliced` mirrors the splice footnote.
- **Flag semantics.** `--pareto` conflicts with `--metric` (operational error, exit 2:
  `trends: --pareto and --metric are mutually exclusive`). `--pareto` composes with `--json`.
  Record-version enforcement, baseline resolution, and splice detection are reused unchanged.

## 3. Dormancy and compatibility

- No verifier axis in history → every candidate point is non-comparable → the table still
  renders (costs + note), nothing gates, nothing errors. Pre-SP3 meta.json (nil `env`) stays
  valid everywhere; `ScanRuns`/`ReadMeta`/selector paths are untouched.
- No schema or record-version changes anywhere: store stays v5, `Record.V` stays 1, the new
  meta field is additive JSON. Old binaries reading new artifacts ignore `env` (ADR-0017 §5
  forward-compat); new binaries reading old artifacts see nil.
- Evaluation-agnostic boundary (ADR-0022): stamps read existing deterministic observables;
  the Pareto reads only stored gate output. No new statistics, no RNG, no network.

## 4. Testing and acceptance

- **TDD** throughout; 100% coverage; no comments; no new deps.
- **Unit:** EnvStamps JSON round-trip incl. nil/omitempty behavior and artifacts-second-pass
  survival; model_id present/absent (no assistant turn); Pareto point extraction from
  records (both findings, one missing, none); domination table incl. equal-points,
  strict-on-one-axis, non-comparable exclusion; ordering; splice propagation; `--pareto`
  ⊕ `--metric` conflict; JSON shape round-trip.
- **Hermetic E2E extension** (the SP1/SP2 driver, appended section, nothing renumbered):
  after `bench`, assert `meta.json` carries `env.catacomb_version`, `env.resources.os/arch/
  cpus >= 1`, and (if the fixture transcript carries a model) `env.model_id`; then record the
  A-vs-A and degraded comparisons against a baseline and assert `trends --pareto --json`:
  the degraded point is `dominated: true` (equal cost, strictly worse accuracy), the A-vs-A
  point and baseline are both non-dominated (equal on both axes), and the note/omission
  semantics hold for a record lacking the verifier axis if the driver has one (else skip).
- **Docs:** cli.md (`env` block in the evidence layout, `trends --pareto` + JSON shape,
  domination rule in one sentence), workflows.md (reading the Pareto: frontier rows,
  dominated rows, why equal A-vs-A rows both stay).

## 5. Boundaries

Not in SP3: sampling-param stamping (unavailable — see §1); gating or `--strict` semantics on
env stamps; charts or any visual frontier (Phoenix owns the eyes, ADR-0026); multi-baseline
or cross-baseline Pareto; per-task Pareto points (task axis stays inside the record); tokens
axes in the table (cost_usd only — tokens remain visible in default trends columns); Record
schema changes. SP4 (audit) and SP5 (judge utilities) are untouched.
