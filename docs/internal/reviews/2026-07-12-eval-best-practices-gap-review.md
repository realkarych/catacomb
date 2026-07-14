# Eval best-practices gap review — agentic-eval canon vs catacomb (V-3)

- **Date:** 2026-07-12
- **Scope:** map the published state of the art for evaluating agentic pipelines (Anthropic, OpenAI, academic and industry benchmarks, eval-platform features) against post-pivot catacomb; derive the development vector toward the target scenario.
- **Target scenario:** internal benchmarking of Claude-based agentic pipelines for data engineers and analysts at a large organization, on a proprietary data stack (YTsaurus, YQL, in-house ETL frameworks) — so no vendor tool will ever ship the domain verifiers, and the gate must accept user-supplied ones.
- **Method:** three parallel research passes over primary sources (Anthropic engineering/research posts and papers; OpenAI platform docs, cookbooks, and benchmark papers; academic/industry benchmarks 2024–2026), plus a dedicated verification pass over live competitor docs (fetched 2026-07-11/12, not training data). Catacomb's side re-read first-hand: ADR-0022/0023/0026, the PV-6a/PV-6b calibrations, the V-2 competitive review, and the `bench`/`regress`/`scores`/`aggregate` surfaces.

## Verdict in one line

Catacomb already implements the regression-evals canon better than the market — outcome-over-trajectory grading, a statistical gate that discloses its own sensitivity, validated with planted regressions and A-vs-A controls; what the target scenario adds is the capability-benchmarking half, and its four missing load-bearing pieces are (1) execution-based task verification with user-supplied verifiers, (2) pass^k and paired statistics, (3) an ETL task corpus with isolated environments, and (4) judge meta-calibration for non-numeric artifacts.

## 1. Where catacomb already matches the canon

| Canon | Source | Catacomb today |
|---|---|---|
| "Grade what the agent produced, not the path it took"; end-state over trajectory | Anthropic *Demystifying evals for AI agents* (2026-01); τ-bench | Phase-axis-authoritative verdicts; checkpoint segmentation (ADR-0022); live-confirmed in PV-6b |
| Report uncertainty; gates must know their own power | Anthropic *Adding Error Bars to Evals* (arXiv 2411.00640) | Wilson one-sided bounds + delta AND-rule; per-invocation sensitivity disclosure (ADR-0023) — effectively a built-in power analysis; no shipping platform has either (V-2, re-verified) |
| Validate the harness with planted regressions ("model organisms"); audit the cheap gate against ground truth | Anthropic Bloom (2025-12); OpenAI Preparedness v2 | PV-6a synthetic power tables pinned by tests + PV-6b live calibration: seeded presence and continuous regressions gate, both A-vs-A controls clean, $0.43 |
| Separate capability evals from regression evals; regression suite ≈ 100% pass | Anthropic *Demystifying evals* | Catacomb is the regression gate by construction (ADR-0022/0023) |
| Isolation and contamination hygiene; version everything | Anthropic *Demystifying evals*; OpenAI agent-improvement-loop cookbook | Evidence dirs redacted on write; basket hash; version stamps + `--strict` (ADR-0026 §6); Claude Code version watchlist |
| Cost/latency/tokens as first-class metrics | Anthropic *Writing effective tools*; HAL | Aggregated and gated per step/phase/run; PV-6b finding that tokens out-regress cost under prompt caching |

## 2. Gaps against the canon (ordered by weight for the target scenario)

### 2.1 No execution-based verification of task outcomes

The field standard for data work is executing the artifact and comparing result sets, not judging text: canonicalized result-set/DataFrame comparison (numeric tolerance ~1e-4, row-order insensitivity absent an ORDER BY, header variance, type coercion — Text2Analysis, DABStep, InfiAgent-DABench), row-count plus full-column match against golden tables (ELT-Bench SRDEL/SRDT), per-artifact-type oracles (DA-Code), strict "extra rows/cols = wrong" to kill execution-accuracy false positives (Databricks Genie; BIRD's known EX-false-positive weakness). Anthropic's cookbook: code-based grading is "by far the best grading method if you can design an eval that allows for it." Catacomb deliberately measures only deterministic observables (ADR-0022 boundary); external scorers exist only as the manual `--scores` JSONL path, step-level, numeric — there is no task-success concept, so no benchmarking is expressible.

### 2.2 No reliability metric (pass^k) and no paired statistics

τ-bench's pass^k (`mean over tasks of C(c,k)/C(n,k)` — all k trials succeed) is the headline reliability metric; Anthropic prescribes "pass^k for agents where consistency is essential"; GPT-4o drops from pass@1 ≈ 61% to pass^8 ≈ 25% on τ-bench retail, the gap that makes agents unshippable. Separately, baseline and candidate run the same tasks, so paired-difference analysis (Anthropic error-bars paper: per-question score correlations 0.3–0.7 make paired tests far more powerful than independent-interval comparison) directly addresses catacomb's honestly documented blind spot: the continuous band is k-invariant and blind below +25% (PV-6a Table B). Closed-form paired tests (sign test, Wilcoxon signed-rank) fit the dependency-free, RNG-free DNA; bootstrap does not.

### 2.3 No task corpus and no environment isolation

Canon sourcing: 20–50 tasks from real failures to start (Anthropic); ≥50 failing production examples, open→axial coding of a failure taxonomy (OpenAI evaluation flywheel); tasks contributed as PRs by domain teams (Anthropic ownership model). The classes with the highest discrimination for DE agents are precisely where SOTA is near zero: dbt/data-model transformation (ELT-Bench SRDT 3.9%), enterprise-schema SQL (Spider 2.0 21.3%), SQL repair (BIRD-CRITIC ~39%), multi-turn SQL (BIRD-Interact ~24%). Per-trial isolation is a correctness requirement, not hygiene — Anthropic observed Claude reading prior trials' git history; ETL state makes this acute. Two-sided (should-act / should-not-act) tasks are absent from the basket model. Resource configuration is an experimental variable (Anthropic infra-noise: 6pp spread on Terminal-Bench from resources alone; sub-3pp deltas deserve skepticism).

### 2.4 No judge meta-calibration for non-numeric artifacts

Analyst deliverables (reports, explanations, charts) need LLM/VLM judges eventually; the canon demands measuring the judge before trusting it: a held-out human-labeled set (~40 transcripts suffices — Bloom: Opus 4.1 Spearman 0.86, Sonnet 4.5 0.75), agreement within ~5% of human inter-rater agreement (GDPval: 66% vs 71% IRA), TPR/TNR instead of accuracy on imbalanced data (OpenAI flywheel, 20/40/40 split), judge consistency by re-scoring ×5 (Bloom), a panel of 3 cheap heterogeneous judges over one expensive one (PoLL, 7–8× cheaper, less self-preference), never judging with the tested model's family (MT-Bench: self-preference +10–25%). InfiAgent-DABench abandoned its GPT-4 judge at 67% human agreement for format-prompting + regex — the bar is real. Catacomb has no judge provenance in scores, no agreement machinery.

### 2.5 No transcript-audit workflow

"You won't know if your graders are working well unless you read the transcripts" (Anthropic); HAL made LLM log-inspection a first-class eval component (caught agents web-searching benchmark answers and bugs in τ-bench itself); eval-awareness shows up as token anomalies (one problem at 38× median consumption). Evidence dirs are the perfect substrate; nothing samples, screens, or packages them for review.

### 2.6 Organizational scale

Local SQLite baselines don't share across an organization; no model × scaffold × suite reporting (HAL's three axes); accuracy-vs-cost must be read jointly (Princeton *AI Agents That Matter*: LATS 88% at $134.50 vs retry 92% at $2.51 on HumanEval — cost is a free variable that inflates rankings); benchmarks are perishable (OpenAI retired SWE-bench Verified; withheld gold and private/rotated tasks are the defenses).

## 3. Competitive verification (live docs, 2026-07-11/12)

Verified per feature against current documentation; "nobody" below means absent from every fetched doc set (LangSmith, Langfuse, Braintrust, Phoenix, W&B Weave, promptfoo, Inspect AI, DeepEval, OpenAI Evals).

| Feature | State of the market |
|---|---|
| Language-agnostic exec verifier (git-hook-style: run a binary, read scores) | **Nobody ships it.** Closest: Inspect AI `sandbox().exec()` inside custom Python scorers (but Inspect owns the agent loop — tasks must be rewritten into its harness); promptfoo `python`/`ruby` interpreter-subprocess asserts + `webhook`. Braintrust/LangSmith/Phoenix/Weave: in-process Python/TS only (LangSmith UI evaluators explicitly deny network). OpenAI Evals python grader has **no network** (cannot reach a proprietary DB) and the platform **shuts down 2026-11-30** with an official migration path to promptfoo (acquired by OpenAI 2026-03-09, OSS continuity promised). Langfuse `POST /api/public/scores` is clean any-language *ingestion* but runs nothing |
| pass^k | **Inspect AI only** (`pass_k` epoch reducer, May 2026, citing τ-bench). Braintrust has a glossary page, not a metric; LangSmith repetitions report mean+stddev; everyone else nothing |
| Significance / CI when comparing two runs | **Nobody.** Braintrust confirmed still without significance concepts (2026-06 corroboration); Langfuse warns "averages can hide outliers" and ships averages; Inspect ships `stderr()` (with a cluster parameter) and `bootstrap_stderr()` as per-run metrics but no between-run test. Power analysis: nobody |
| Judge calibration | The one active front: **LangSmith Align Evals** (alignment score vs human labels + auto few-shot insertion of corrections); Galileo auto-tunes from ~5 annotations but surfaces no agreement number; Patronus recommends Cohen's κ > 0.8 semi-manually; Langfuse documents calibration as "not a built-in feature"; promptfoo `llm-rubric` is uncalibrated |
| Automated trace audit | **LangSmith Insights** (LLM clustering of traces into failure modes, paid tier) is the only productized one; Braintrust Loop is user-directed; Phoenix clusters embeddings (HDBSCAN/UMAP), not LLM audit. HAL-style cheating/shortcut inspection: **productized by nobody** |
| Accuracy-vs-cost Pareto view | **Nobody.** W&B Weave's Leaderboard ranks by eval metrics without cost; everyone shows cost as a column next to scores. HAL remains research-only |
| YQL / YTsaurus | No public YQL text-to-SQL benchmark exists. YTsaurus has a hermetic local mode (`ghcr.io/ytsaurus/local:stable`, single container; Go `testcontainers-ytsaurus` wrappers by nebius and tractoai) — user-side isolation is solvable without catacomb owning it |

## 4. Per-item verdicts (usefulness vs maintenance scope)

Assessed against the ADR-0026 constitution: hermetic offline CLI, deterministic core, stdlib-first, no network, evaluation-agnostic (ADR-0022), bus factor 1.

1. **Verifier contract (exec, run-level annotations) — build first.** Without a task-success concept there is no benchmarking at all. The contract formalizes the existing `--scores` path (catacomb runs the scorer instead of the user hand-carrying JSONL) and stays evaluation-agnostic: semantics live in the user's subprocess, catacomb deterministically aggregates. Run-level 0/1 annotations ride the existing Wilson machinery as a pass rate — the statistical gate extends to correctness with no new statistics. The competitive slot is empty, and proprietary stacks (YQL/YTsaurus/Logos) make exec-extensibility the only viable route. Out of the box: only a canonicalized CSV/JSONL table comparator (in the Python SDK); no connectors, no built-in judges.
2. **pass^k + paired stats — build second.** Closed forms, no RNG, no dependencies; deepens the moat where nobody competes (pass^k exists only in Inspect, a different form factor; paired tests nowhere). pass^k lands with item 1; paired sign/Wilcoxon waits until baskets carry ≥5–10 tasks and discloses "insufficient tasks" below that, like the existing sensitivity note. Bootstrap and FDR: rejected.
3. **Env stamps + Pareto — stamps immediately, Pareto after item 1.** meta.json gains model/resources/temperature fields (deterministic, stdlib, the ADR-0017 stamp discipline extended); Pareto is a sorted table + `--json` in `trends` once an accuracy axis exists — no charts, Phoenix owns the eyes.
4. **Audit — build the deterministic half only.** Outlier flags (token/cost/duration/turn-count per cell vs group median) as report notes, in the sensitivity-note pattern; deterministic sampling + prompt-pack export for external `claude -p` inspection, findings returning as `--scores`. No LLM calls in core, no Insights clone, no embedding clustering.
5. **Judge meta-eval — build minimally, last.** Core: optional provenance fields on scores lines (tool, version, prompt hash) so judge swaps can't silently corrupt longitudinal baselines. Python side (`integrations/`): agreement calculator (Spearman/κ/TPR/TNR vs a hand-labeled set) and panel aggregation — a few hundred lines, no LLM calls. No judge runner, no alignment UI: that is LangSmith's actively-defended ground and off-mission for a deterministic gate.

The guard rail across all five: every item enters as an extension of an existing surface (scores path, aggregate axes, stamps, export, report notes), never as a new subsystem; everything LLM-dependent or stack-specific stays behind the exec/JSONL boundary on the user's side. This preserves the hermetic-CI property and the bus-factor budget.

## 5. Decision and sequencing

Recorded as [ADR-0027](../../adr/0027-verification-layer-and-reliability-metrics.md): five sub-projects, SP1 (verifier contract) → SP2 (pass^k + paired) → SP3 (env stamps + Pareto) → SP4 (audit) → SP5 (judge utilities), each its own spec → plan → subagent-driven implementation → E2E acceptance. SP1 design: [2026-07-12-sp1-verifier-contract-design.md](../specs/2026-07-12-sp1-verifier-contract-design.md). The deferred corpus work (gap 2.3) is intentionally out of this wave: task authoring is organizational, not code, and the contract must exist first.

## 6. Primary sources

Anthropic: *Demystifying evals for AI agents* (2026-01-09); *Adding Error Bars to Evals* (arXiv 2411.00640); *Quantifying infrastructure noise in agentic coding evals* (2026-02-05); *Building Effective Agents* (2024-12-19); SWE-bench harness post (2025-01-06); *Writing effective tools for agents* (2025-09-11); multi-agent research system post (2025-06-13); cookbook `building_evals.ipynb`; Petri (2025-10) / Petri 2.0 (2026); Bloom auto-evals (2025-12-19); BrowseComp eval-awareness post (2026-03-06).

OpenAI: Graders / Evals / trace-grading / agent-evals / evaluation-best-practices platform docs; evaluation-flywheel, eval-driven-system-design, agent-improvement-loop, macro-evals, RFT-grader cookbooks; GDPval (arXiv 2510.04374); PaperBench (2504.01848); SWE-Lancer (2502.12115); MLE-bench (2410.07095); BrowseComp (2504.12516); Preparedness Framework v2 (2025-04-15); Evals-platform sunset + promptfoo migration notice.

Benchmarks and methodology: τ-bench (2406.12045) / τ²-bench (2506.07982); AgentBench (2308.03688); WebArena (2307.13854); OSWorld (2404.07972); TheAgentCompany (2412.14161); GAIA (2311.12983); *AI Agents That Matter* (2407.01502); HAL (2510.11977); inference-scaling flaws (2411.17501); MT-Bench judge study (2306.05685); G-Eval (2303.16634); PoLL (2404.18796); Spider 2.0 (2411.07763); BIRD (NeurIPS 2023) + BIRD-CRITIC/Interact (2025); Spider2-V (2407.10956); ELT-Bench (2504.04808); DA-Code (2410.07331); InfiAgent-DABench (2401.05507); DSBench (2409.07703); DataSciBench (2502.13897); DABStep (2506.23719); Tapilot-Crossing (2403.05307); DS-1000 (2211.11501); Text2Analysis (2312.13671); TableBench (2408.09174).

Competitor docs verified live: promptfoo, Inspect AI, DeepEval, Braintrust, LangSmith, Langfuse, Arize Phoenix, W&B Weave, OpenAI Evals platform, Patronus, Galileo; YTsaurus local-mode docs and testcontainers wrappers.
