# ADR-0027: Verification layer and reliability metrics (post-pivot vector)

- **Status:** Accepted
- **Date:** 2026-07-12
- **Deciders:** @realkarych
- **Related:** [2026-07-12 gap review](../reviews/2026-07-12-eval-best-practices-gap-review.md); ADR-0016, ADR-0022, ADR-0023, ADR-0026; SP1 design spec [2026-07-12-sp1-verifier-contract-design.md](../specs/2026-07-12-sp1-verifier-contract-design.md)

## Context

ADR-0026 completed the pivot: catacomb is a hermetic offline statistical gate for checkpoint-level regressions in Claude Code agentic flows, validated end-to-end (PV-6a/PV-6b). The target scenario now extends beyond regression gating to **benchmarking agentic pipelines for data engineering and analytics on a proprietary data stack** (YTsaurus, YQL, in-house ETL frameworks). The 2026-07-12 gap review mapped the published agentic-eval canon (Anthropic, OpenAI, τ-bench/HAL/Spider-2.0-class benchmarks, live competitor docs) against the codebase and found:

1. The gate measures behavioral observables (presence, status, duration, cost, tokens) but has **no concept of task success**. The field standard for data work is execution-based verification (canonicalized result-set comparison, golden-table matching). Without a success bit there is no pass^k, no accuracy axis, no benchmarking.
2. The proprietary stack means **no vendor will ever ship the domain verifiers**; the only viable route is a language-agnostic contract for user-supplied verifiers. Verified live: no product ships a git-hook-style exec verifier; pass^k exists only in Inspect AI (a harness that owns the agent loop); between-run significance testing exists nowhere.
3. The reviewed canon prescribes practices catacomb lacks in order of weight: execution-based outcome verification; pass^k reliability and paired-difference statistics; environment/resource stamping; transcript audit; judge meta-calibration (provenance, human-agreement measurement).

## Decision

Extend catacomb with a **verification layer and reliability metrics**, as five sequenced sub-projects. Each enters as an extension of an existing surface — the scores path (ADR-0016/0022), aggregate axes, stamps (ADR-0017/0026 §6), export, report notes — never as a new subsystem. Everything LLM-dependent or stack-specific stays behind an exec/JSONL boundary on the user's side; the core stays deterministic, offline, stdlib-first.

| SP | Contents | Depends on |
|---|---|---|
| SP1 | **Verifier contract:** `verify:`/`artifacts:` in baskets; bench runs the verifier per cell (argv exec, `CATACOMB_*` env, scores-JSONL on stdout); run-level annotations (a scores line without `step_key`); reserved `verifier.pass` gates by default as a higher-better rate through the existing Wilson machinery; offline `catacomb verify` re-verification over runs-dirs; artifact capture into evidence dirs (redacted, size-capped); judge/verifier provenance fields on scores lines; Python mini-SDK (`integrations/verifier`, stdlib-only) with a canonicalized CSV/JSONL table comparator; hermetic per-PR SQL E2E + live weekly SQL basket | — |
| SP2 | **Reliability metrics:** pass^k (unbiased `C(c,k)/C(n,k)` estimator) over verifier outcomes; paired sign/Wilcoxon signed-rank tests over per-task deltas (closed-form, no RNG), active at ≥5–10 tasks with an "insufficient tasks" disclosure below, addressing the k-invariant continuous-band blind spot (PV-6a Table B) | SP1 |
| SP3 | **Env stamps + Pareto:** model id, resources, sampling params in `meta.json`; accuracy-vs-cost table (dominated-row marking) in `trends`/`--json` | stamps: —; Pareto: SP1 |
| SP4 | **Audit (deterministic half):** per-cell token/cost/duration/turn-count outlier flags vs group median as report notes; deterministic evidence sampling + prompt-pack export for external LLM inspection, findings returning via `--scores` | SP1 (gaming detection value) |
| SP5 | **Judge utilities:** agreement calculator (Spearman/κ/TPR/TNR vs hand-labeled sets) and panel aggregation in `integrations/` (Python, no LLM calls) | SP1 (provenance fields) |

Non-goals, decided now to bound scope: no judge runner or alignment UI (LangSmith's actively-defended ground); no LLM calls or network in the Go core; no built-in connectors (YQL/YTsaurus/Logos verifiers are user code); no Go comparator subcommand until a non-Python consumer appears; no LangSmith-Insights-style clustering (Phoenix owns the eyes per ADR-0026); no bootstrap/FDR statistics; task-corpus authoring is organizational work outside this ADR.

Every SP follows the repo process: brainstormed design spec → implementation plan → subagent-driven TDD execution → review cycle → green CI. Acceptance for each SP is **end-to-end**: hermetic per-PR E2E asserting the full `bench → verify → aggregate → regress → exit code` cycle through the built binary on a near-real task (SQL over sqlite), plus the live weekly gate exercising the same path with real `claude -p`.

## Alternatives considered

- **Verdict as a first-class model entity** (dedicated type in model/store, dedicated regress logic). Rejected: breaks the ADR-0022 evaluation-agnostic boundary, duplicates the rate machinery that run-level annotations reuse for free, and is the largest change wave for equal expressiveness.
- **Contract without a hook** (document run-level scores; users run verifiers themselves and hand-carry `--scores`). Rejected: nothing executable to E2E-test, no offline re-verification, poor DX; fails the acceptance bar this ADR sets.
- **In-core comparators/connectors for the data stack.** Rejected: dependency and maintenance surface against ADR-0026's constitution; the Python SDK carries the universal comparator, the stack stays user-side.
- **Bootstrap CIs / permutation tests for paired comparison.** Rejected for RNG and explainability; sign/Wilcoxon are closed-form, deterministic, and adequate at basket scale.
- **Building judge calibration into the core.** Rejected: requires LLM calls, competes head-on with LangSmith Align Evals, and violates the deterministic-core identity; provenance in core + Python utilities capture the value.
- **Status quo** (regression gate only). Rejected: the target scenario is benchmarking; without a success axis catacomb cannot express it, and the review shows the adjacent competitive slots (exec verifier, pass^k, paired stats, Pareto) are empty today.

## Consequences

- **+** The gate gains an accuracy axis while staying hermetic: verifiers are subprocesses over recorded evidence; CI still needs only the binary, the transcripts, and the user's verifier.
- **+** The existing statistics extend to correctness for free (pass rate through Wilson bounds; sensitivity disclosure covers the new axis), then sharpen with pass^k and paired tests where the market has nothing.
- **+** Proprietary-stack verifiers become first-class without any stack code in core; the Python SDK lowers authoring cost for data teams.
- **−** New permanent surfaces: basket fields, one CLI verb (`verify`), a run-level scores dialect, a Python package, an expanded E2E harness — each with tests and docs to maintain.
- **−** Verifier quality becomes part of the trust chain: a weak verifier caps what the gate can certify (imperfect-verifier ceiling, arXiv 2411.17501). Mitigated by contract E2E fixtures, idempotent re-verification, and SP4 outlier screens for gaming.
- **−** Artifacts capture enlarges evidence dirs and adds a documented residual risk for binary artifacts (text is redacted on write; binaries are size-capped and hashed, their content the basket author's responsibility).
- **Risk:** run-level annotations at small k inherit the PV-6a Wilson floors (k=3 gates only full flips). Accepted: the sensitivity disclosure extends to the pass-rate axis, and SP2's paired tests recover power on the continuous side.
