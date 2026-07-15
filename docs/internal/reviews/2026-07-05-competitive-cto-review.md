# CTO review — competitive position and form-factor fit (V-2)

- **Date:** 2026-07-05
- **Scope:** fresh, independent verdict on (a) whether the checkpoint-diff eval core works end-to-end, (b) where the market moved, (c) whether catacomb should be replaced by an off-the-shelf stack (Arize Phoenix was the named candidate).
- **Method:** first-hand reads of the load-bearing code (`bench`, `regress`, `stepkey`, `wilson`, `sensitivity`, CI config); six parallel code-audit passes over the canonical checkout; eight competitor platforms verified against **live docs and changelogs (mid-2026)**, not training data. The 2026-07-02 reviews were consulted only to track finding status — every conclusion below was re-derived.

## Verdict in one line

Keep the checkpoint-diff + statistical-gate core — it is real, dogfood-validated, and unserved by the market. Stop carrying the platform around it: capture and display have commoditized underneath it, and the maintenance weight is platform-sized while the moat is feature-sized.

## 1. Findings status since V-1

All four written roadmaps (eval-core, P1, post-review hardening, V-1 findings) are fully shipped as of `58a1475`. V-1 findings F5–F10 all closed (#114–#116, #125–#127). Everything code could close from prior reviews is closed; what remains open is what code cannot close: adoption (zero external users), the market window, and validation depth (one calibration basket).

## 2. Core engine reality (first-hand verification)

| Capability | Status | Evidence |
|---|---|---|
| Basket runner executes real flows N times | **Works, dogfooded** | `cmd/catacomb/bench.go` — child execution, session-id peek, auto `task:<id>` marks, post-run checkpoint verification, resumable manifest |
| Statistical gate (rates + metrics) | **Works, strongest component** | one-sided Wilson intervals (z=1.645) + disjointness + delta (`regress/compare.go`), median ± max(rel, IQR·factor) bands, min-support floor, small-k sensitivity disclosure (`regress/sensitivity.go`, ADR-0023) |
| Checkpoint declaration via CLAUDE.md | **Convention, not code** | no parser exists; the agent calls the `mark` MCP tool; the reducer synthesizes markers from the observed tool call (`reduce/marker.go`) |
| Cross-run step identity | **Heuristic, weak under change** | `stepkey/v1` = tree-path + tool name + salient-input hash; changing the tested component changes prompts/inputs and breaks keys by construction; dogfood measured 0.43–0.71 step coverage |
| Checkpoint-scoped structural diff across two runs | **Plumbing works; pairwise only** | `catacomb diff A.jsonl B.jsonl --phase C` — single-run vs single-run, reports sampling noise as diffs |
| Group-vs-group structural diff at a checkpoint | **Absent** | the tagline implies it; no code path provides it — you get structural-but-noisy or statistical-but-coarse, never both |
| Quality/semantic scoring | **Out of scope by design** | external scorers feed numeric annotations into the gate (`--annotation`) |

**Trusted guarantee (demonstrated):** a checkpoint-level behavioral regression (presence, error rate, phase cost/latency/tokens) is caught at k≥3–5 with low false positives. The dogfood run reproduced exactly this: degraded variant → `regression` exit 1; A-vs-A control → zero false positives. The trusted comparison axis is the **phase/checkpoint, not the step**.

## 3. Capture, display, interop reality

- Four-source capture with deterministic commutative reduction (fuzz-tested) is genuinely strong; the OTel-demotion rule exists because Claude Code's native OTel is broken for agentic structure on the streaming path (anthropics/claude-code#53954).
- Verified gaps vs the ADRs: `parentUuid` threading not implemented (turn attribution is seq-order); interruption capture effectively absent (no parser emits `cancelled`/`superseded`); thinking blocks not captured; per-node cost outside OTel is estimated from the pricing table.
- Display (web UI + TUI, 17 Playwright specs, SSE resume, redaction gating) is product-grade. DiffView is a summary-level MVP.
- Six exporters + a DeepEval bridge are real, not stubs. Zero external consumers.
- Maturity: ~20.2k prod Go / ~43.7k test Go (2.16:1), 100% coverage enforced, 25 ADRs, 3-OS CI, real packaging. Substrate 4/5; eval layer 3/5 (young, correct instincts).
- One determinism gap where eval reproducibility lives: ADR-0017 designs three version stamps (`schema_version`, `reducer_version`, `body_schema_version`); only `schema_version` exists. Longitudinal baselines can silently diverge across catacomb's own reducer changes.

## 4. Market (verified mid-2026, live sources)

The decisive change since the 2026-07-02 review: **five platforms shipped first-party Claude Code ingestion between Dec 2025 and Jun 2026.** "Only we can capture CC agentic flows" is no longer true; what remains is a fidelity edge, not a category edge.

| Platform | Native CC ingest | Structural run diff | Statistical regression | Checkpoint primitive | Self-host OSS |
|---|---|---|---|---|---|
| Arize Phoenix | Yes — first-party plugin, 9 hooks incl. SubagentStop → OpenInference | No (score/label level, baseline flips) | No (eyeball; threshold alerts only in paid AX) | Partial (span attrs, DIY) | Yes (ELv2, single process) |
| Langfuse | Yes — official plugin, Stop hook reads session JSONL | No (item/output JSON diff) | No (own blog: no significance testing) | Partial (named spans) | Yes (MIT core, compose stack) |
| LangSmith | Yes — first-party MIT plugin | Partial (I/O text diff, not structure) | No (mean + stddev only) | No | Partial (enterprise self-host) |
| Braintrust | Yes — plugin (Dec 2025, logging) | No (row/JSON diff) | No (docs: no significance/CI/p-values) | Partial | Partial (hybrid, enterprise) |
| W&B Weave | Yes — official plugin (hooks → OTel) | Partial (row-based trace diff; closest analog) | No | No | Partial (backend licensed) |
| promptfoo | Partial (YAML harness; acquired by OpenAI 03/2026) | No (final-output per test) | No | No | Yes |
| UK AISI Inspect | No (own task/solver harness) | No | Partial (epochs, no significance gate) | No | Yes (MIT) |
| Helicone | Partial (proxy-only, maintenance mode) | No | No | Partial | Yes (Apache-2) |
| **catacomb** | **Yes — 4-source reconciled** | **Yes — checkpoint-scoped** (step level weak) | **Yes — Wilson + IQR + sensitivity** (phase level) | **Yes — first-class** | **Yes — local-first** |

Key sources: `github.com/Arize-ai/arize-claude-code-plugin`; `langfuse.com/integrations/other/claude-code`; `langfuse.com/blog/2025-11-06-experiment-interpretation` (no significance testing); `docs.langchain.com/langsmith/repetition` (mean+stddev only); `braintrust.dev/foundations/how-to-analyze-your-eval-results` ("does not report statistical significance"); `arize.com/docs/phoenix/datasets-and-experiments/how-to-experiments/run-experiments` (single-pass experiments); `github.com/langchain-ai/langsmith-claude-code-plugins`; `docs.helicone.ai/integrations/anthropic/claude-code`.

**The direct answer on Phoenix:** it gives free — CC ingestion with nested structure, a trace store and session UI, versioned-dataset experiments with aggregate score comparison, evals, self-hosting. It does not do — structural trace-tree diffing (renders trees, never diffs them), any statistics (no distributions, significance, or CI; experiments are single-pass), or attribution of a delta to a swapped component. Those are not "modest glue"; they are the product.

## 5. Layer decomposition (the whole argument in one table)

| Layer | Off-the-shelf coverage | Verdict |
|---|---|---|
| Capturing CC sessions (subagents/tools/MCP/skills) | Phoenix, LangSmith, Langfuse, Braintrust, Weave — first-party plugins | **Commodity** |
| Displaying agentic sessions | Same + their UIs | **Commodity** |
| Suite before/after on aggregate scores | Phoenix `run_experiment`, Langfuse Experiments, LangSmith, promptfoo | **Commodity** |
| Evals (LLM-judge / code / human) | All majors | **Commodity** |
| Self-host / local / OSS | Phoenix (ELv2), Langfuse (MIT), promptfoo, Inspect | **Commodity** |
| Four-source reconciliation, drift detection, provenance | Plugins are single-source | Fidelity edge, not category edge |
| **Checkpoint as a first-class segmentation axis** | Nobody | **Catacomb only** |
| **Structural run-to-run diff at a checkpoint** | Nobody (Weave closest, row-level) | **Catacomb only** |
| **Statistical regression attribution for a component swap** | Nobody (all delta+threshold/eyeball) | **Catacomb only** |

~80% of the stack catacomb carries itself is now purchasable. The defensible part is the three bottom rows — feature-sized, not platform-sized.

## 6. Risks

| Risk | Severity | Note |
|---|---|---|
| Bus factor = 1, zero external users | High | unchanged; code cannot fix it |
| Market window / first-party moves | High | promptfoo → OpenAI (03/2026); LangSmith Engine failure-analysis direction; five CC plugins in six months |
| Moat narrower than the tagline (step diff under-aligns) | Medium | measured 0.43–0.71 coverage; phase axis carries verdicts |
| Validation depth: one basket, cheap model | Medium | the statistical model met real agentic variance once |
| Four-parser coupling to undocumented CC formats | Medium, chronic | drift detection warns but is blind to same-shape renames |
| Surface breadth on solo maintenance | Medium, by choice | 6 exporters, 2 viewers, gRPC+SSE, Python bridge |
| `reducer_version` stamp unimplemented (ADR-0017) | Medium | silent baseline divergence across reducer changes |

## 7. Recommendations

1. Reposition as a feature, not a platform: "a statistical gate for checkpoint-level regressions in Claude Code agentic flows."
2. Freeze substrate growth: no new exporters/backends/viewers, no in-house LLM judge.
3. Test the "on top of Phoenix/Langfuse" hypothesis: let the vendor stack own capture+display; keep the diff+stats+checkpoint layer as the differentiator.
4. Close the "structural OR statistical" gap (group-vs-group structural diff at a checkpoint) — or take "structural step diff" out of the pitch.
5. Extend validation beyond one basket: 2–3 heterogeneous baskets, deliberate regressions of varying magnitude, measure gate power at k=5/10. Implement the `reducer_version` stamp before trusting cross-version baselines.
6. Attack bus factor directly or accept personal-tool status and shed the tax accordingly.

## 8. Outcome

This review led to the form-factor decision recorded in [ADR-0026](../../adr/0026-form-factor-pivot-offline-eval-gate.md): narrow catacomb to the offline eval gate; delegate observability to a vendor substrate (Phoenix recommended); keep transcripts as the gate's source of truth.
