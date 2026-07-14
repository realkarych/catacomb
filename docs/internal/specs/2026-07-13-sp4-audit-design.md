# SP4 — Audit (deterministic half): design

- **Date:** 2026-07-13
- **Status:** approved design (ADR-0027 SP4; decisions adjudicated there and in the
  [gap review](../reviews/2026-07-12-eval-best-practices-gap-review.md) §4.4; execution pre-approved)
- **Related:** [ADR-0027](../../adr/0027-verification-layer-and-reliability-metrics.md),
  [ADR-0022](../../adr/0022-regression-detection-over-repeated-runs.md) (deterministic observables),
  [ADR-0024](../../adr/0024-secrets-at-rest-write-path-redaction.md) (evidence redaction),
  SP1 spec [2026-07-12-sp1-verifier-contract-design.md](2026-07-12-sp1-verifier-contract-design.md)
  (scores round-trip), PV-6b [live calibration](../reviews/2026-07-08-pv6b-live-calibration.md)
  (cost/duration noise under prompt caching)

SP4 adds the deterministic half of trace audit: **per-cell outlier flags** on the regress
report (gaming/anomaly screen: a cell that spends wildly more tokens or turns than its group
deserves eyes) and a **prompt-pack export** that bundles deterministically sampled, already-
redacted evidence for external LLM inspection, whose findings return through the existing
`--scores` boundary. No LLM calls, no network, no clustering in core (ADR-0027 non-goals).

## 1. Per-cell outlier flags — `audit` block on the regress report

- **Cells.** A cell = one run. New exported extraction in aggregate (the only package that
  knows the summation rules):

```go
type Cell struct {
	RunID      string            `json:"run_id"`
	Labels     map[string]string `json:"labels,omitempty"`
	DurationMS float64           `json:"duration_ms"`
	CostUSD    float64           `json:"cost_usd"`
	TokensIn   float64           `json:"tokens_in"`
	TokensOut  float64           `json:"tokens_out"`
	Turns      float64           `json:"turns"`
}

func Cells(group []RunGraph) []Cell
```

  Sums reuse the shared per-run helper (`runNodeSums`) and `runDuration` (unmeasured
  duration contributes 0 to that cell — the cell is still listed; flags on duration then
  compare against the group like any value). `Turns` = count of `assistant_turn` nodes.
  Output sorted by RunID. Aggregation itself is untouched — cells are extracted from the
  same `[]RunGraph` BEFORE `Aggregate` collapses per-run values.

- **Wiring.** `regress.Input` gains `BaselineCells, CandidateCells []aggregate.Cell`
  (optional; nil ⇒ audit dormant). The cmd layer passes `aggregate.Cells(group)` for both
  sides — it already holds the groups it aggregates. `Compare` computes the audit block
  after findings; nothing feeds verdicts (`overallVerdict` reads only Findings — audit is
  structurally non-gating, per ADR).

- **Outlier rule** (per group, per metric over duration_ms/cost_usd/tokens_in/tokens_out/
  turns): with group median `m` and `IQR = P75 − P25` (nearest-rank, the package's existing
  quantile semantics), a cell value `v` is flagged iff

  `|v − m| > max(AuditRelDelta·|m|, AuditIQRFactor·IQR)`

  Thresholds join `regress.Thresholds`: `AuditIQRFactor` (default **3.0** — Tukey far-out;
  the gate's own band uses 1.5, audit is deliberately less twitchy) and `AuditRelDelta`
  (default **0.5** — a cell must be ≥50% away from the group median even when IQR≈0; this
  floor is what keeps byte-identical hermetic groups and prompt-cache cost noise from
  flagging, per PV-6b's ~5× cost spread finding). CLI: `--audit-iqr-factor`,
  `--audit-rel-delta`, both validated > 0, plumbed like the existing threshold flags.
  Groups with fewer than 3 cells produce no flags (median±IQR of 1–2 points is noise).

- **Report shape.** `regress.Report` gains `Audit *Audit` (JSON `audit`, omitempty),
  parallel to `Sensitivity`/`Reliability`:

```go
type CellFlag struct {
	RunID  string  `json:"run_id"`
	Task   string  `json:"task,omitempty"`
	Metric string  `json:"metric"`
	Value  float64 `json:"value"`
	Median float64 `json:"median"`
	Band   float64 `json:"band"`
}

type Audit struct {
	Baseline  []CellFlag `json:"baseline,omitempty"`
	Candidate []CellFlag `json:"candidate,omitempty"`
}
```

  `Task` = the cell's `task` label when present. Flags sorted by RunID then metric (metric
  order: the fixed list above). `Audit` is nil when no cells were provided OR no flag fired
  — absence means "screen ran clean or didn't run"; the human line distinguishes nothing
  (silence), matching the sensitivity-block convention.

- **Human render** (epilogue lines, sensitivity-note discipline), one line per flag:

  ```text
  audit: candidate run r07 (task sql) tokens_out 1932 vs group median 243 (band 121.5)
  ```

  Values render `%g`; the line names group, run, task (when known), metric, value, median,
  band.

## 2. Prompt-pack export — `catacomb pack`

- **Command.** `catacomb pack <selector> --runs-dir <dir> --out <dir> [--sample N] [--db <path>]`
  — selector is the existing `label:`/`name:` grammar (same resolution machinery as
  `regress --runs-dir`, including baseline label resolution via `--db` for `name:`).
  Errors: missing `--runs-dir`/`--out` and empty selection are operational (exit 2);
  `--sample` validated > 0 (default **3**).

- **Deterministic sampling** (no RNG — determinism invariant): sort selected runs by RunID;
  take N evenly spaced indices `floor(i·len/N)` for `i = 0..N-1`, deduplicated (fewer than
  N runs ⇒ all of them). Stride sampling covers the range (first/middle/last) instead of
  clustering on the head, and two invocations over the same evidence always pick the same
  cells.

- **Bundle.** For each sampled run, copy its evidence directory verbatim (transcripts,
  `subagents/`, `meta.json`, `scores.jsonl`, `verify.json`, `artifacts/`) into
  `<out>/<run_id>/`. Evidence content is redacted at write time by construction (ADR-0024:
  transcripts and text artifacts pass the redactor before landing in evidence dirs), so the
  pack inherits the redaction guarantee with no second pass. The out dir must not already
  exist (refuse rather than merge — packs are immutable snapshots).

- **Manifest.** `<out>/pack.json`:

```go
type PackManifest struct {
	Selector   string    `json:"selector"`
	RunsDir    string    `json:"runs_dir"`
	SampleRule string    `json:"sample_rule"`
	Requested  int       `json:"requested"`
	Runs       []string  `json:"runs"`
	CreatedAt  time.Time `json:"created_at"`
}
```

  `SampleRule` is the literal `"runid-stride"` (a future rule change must change the
  string). Plus `<out>/INSTRUCTIONS.md`: a short, static template for the external
  inspector — what the bundle contains, what to look for (shortcuts, gaming, tool misuse,
  fabricated results), and the exact scores-JSONL contract for returning findings
  (`{"key":"audit.<finding>","value":0|1,"run_id":"<run id>"}` per line; run-level lines
  require `run_id`; findings gate via `regress --scores <file> --annotation audit.<finding>:higher-better`
  or lower-better as appropriate). The template is fixed text (no per-run generation beyond
  the run list) — the LLM prompt itself is the user's business, per the exec boundary.

- **Round-trip.** Nothing new: the returned JSONL is the SP1 dialect; `--scores` /
  evidence `scores.jsonl` already apply run-level annotations, and `--annotation` gates
  them. SP4 ships the loop's missing first half (sample + bundle) and documents the cycle.

## 3. Dormancy and compatibility

- No cells passed to `Compare` (any caller other than the updated cmd layer, old
  serialized flows) → `Audit` nil → JSON and human output byte-identical to pre-SP4.
  Records store the full `Report`, so `audit` appears in new records only when flags fired;
  old records unmarshal unchanged (additive omitempty; `Record.V` stays 1; store schema
  stays 5). `trends` output is untouched (it reads findings and named blocks explicitly).
- A-vs-A fires nothing on the four deterministic axes (cost, tokens in/out, turns —
  identical values ⇒ deviations 0), and the `AuditRelDelta` floor keeps near-identical
  groups quiet; live wall-clock warm-up (a first cell paying a cold-start premium) can
  legitimately flag `duration_ms` — that is the screen disclosing, not misfiring. The
  hermetic E2E asserts dormancy on a duration-pinned copy for this reason. Audit never
  affects exit codes by construction.
- `pack` is read-only over evidence and additive on disk; no store DDL, no schema bumps,
  no new deps (stdlib file copy).
- Evaluation-agnostic boundary (ADR-0022): flags read the same deterministic observables
  the gate already aggregates; judgment stays outside (pack → external LLM → scores).

## 4. Testing and acceptance

- **TDD** throughout; 100% coverage; no comments; no new deps; no RNG.
- **Unit:** `aggregate.Cells` (sums vs known fixture, turns counting, unmeasured duration
  → 0, sort, labels passthrough); outlier rule calibration table (PV-6a pattern: pinned
  boundaries — a value exactly at the band does not flag, just beyond does; IQR=0 group
  needs the rel-delta floor exceeded; groups of 1–2 cells never flag; both groups flagged
  independently); render golden lines; JSON round-trip incl. omitempty; flag validation
  (`--audit-iqr-factor`/`--audit-rel-delta` reject ≤ 0); stride sampling table (N≥len,
  N=1, N=3 over 5/15 runs — exact indices pinned); pack refuses existing out dir; manifest
  shape; bundle completeness (every evidence file copied, byte-identical).
- **Hermetic E2E extension** (append; renumber nothing): (a) A-vs-A regress `--json`
  carries NO `audit` key (deterministic evidence, dormancy proven end-to-end); (b) a
  planted-outlier comparison — the driver fabricates one extra evidence dir with inflated
  tokens_out (reusing its transcript-template machinery) — asserts the `audit` block names
  exactly that run/metric with the expected value/median, and exit code is UNCHANGED by
  the flag (non-gating proven); (c) `catacomb pack` over the sql basket: manifest fields,
  sampled run list exactly matches the stride rule, bundle contains meta.json +
  session.jsonl per sampled run, INSTRUCTIONS.md present; (d) round-trip: a hand-written
  `audit.clean` scores line fed via `--scores` + `--annotation audit.clean:higher-better`
  produces the annotation finding (loop closed through the built binary).
- **Docs:** cli.md (`audit` block on regress + both flags; `pack` command section);
  workflows.md (the audit loop: flags → pack → external `claude -p` → scores → gate;
  when to trust cost/duration flags — PV-6b noise caveat).

## 5. Boundaries

Not in SP4: LLM calls, network, or judge prompts in core (INSTRUCTIONS.md is static text);
clustering or failure-mode mining (LangSmith Insights ground — non-goal per ADR); gating on
audit flags (notes only; revisit only with field evidence, ADR-0022→0023 pattern);
per-step/per-phase outlier flags (run-level cells only); archive/tar output for `pack`
(directory bundle only — compression is the user's business); provenance fields on scores
lines (SP5); pass^k/paired/pareto surfaces (SP2/SP3, untouched).
