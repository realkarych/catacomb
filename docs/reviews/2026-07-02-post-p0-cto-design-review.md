# Post-P0 CTO design review & competitive re-verification

**Date:** 2026-07-02 (evening; HEAD `1cfe778`, after PR #99–#107)
**Method:** two full-codebase audits (package/CLI/storage/capture map; ADR-vs-implementation matrix) plus an adversarially verified market survey (104 research agents, 22 sources fetched 2026-07-02, 25 claims put to 3-vote refutation: 22 confirmed, 3 refuted), plus a hand-verified analysis of the regression-gate statistics in `regress/`.
**Prior review:** `2026-07-02-eval-readiness-and-competitive-review.md` (same day, pre-P0-merge). This review supersedes its state assessment; its market matrix remains largely valid and is updated below.

---

## 1. Verdict

**Adopt for own use now. Do not drop it for an off-the-shelf platform. But "implemented" is not "validated": until one real basket has been run and the gate calibration fixed (ADR-0023), regression verdicts must not be trusted as CI gates.**

1. **The target scenario is expressible end-to-end for the first time.** Checkpoints → `bench` (tasks × variants × k reps) → `aggregate` → `regress --baseline/--candidate` → CI exit code. The P0 block (ADR-0022, PR #99–#104) closed all three gaps the morning review named; the P1 block (PR #105–#107) added annotation gates, longitudinal trends, and declared-checkpoint verification the same day.
2. **The niche is re-verified empty.** No surveyed tool — Arize Phoenix/AX, Langfuse, LangSmith, Braintrust, promptfoo, W&B Weave, DeepEval, HoneyHive, claude-code-log, Anthropic's own guidance — supports checkpoint-level run-vs-run diffing of repeated agentic runs for component-change regression detection. Every platform stops at final-output or test-case-level comparison. Independent academic corroboration: AgentEval (arXiv 2604.23581, ACL 2026 Industry) reports step-level evaluation catches **2.17× more failures** than end-to-end scoring (recall 0.89 vs 0.41) and names the step-level gap across LangSmith, Phoenix, Braintrust, Weave, MLflow, Inspect, and AgentOps.
3. **Migrating to Phoenix/Langfuse buys nothing.** Capture and viewing are commoditized (an argument against further Catacomb investment in those layers, not against Catacomb); the diff/regression layer would have to be rebuilt from zero on a foreign span model without `step_key`.
4. **One concrete calibration defect found** (§4.1): at default settings the CI gate cannot fail on any rate regression — including a checkpoint vanishing from 100% of candidate runs. Cheap to fix; decorative until fixed. ADR-0023 is that fix.

---

## 2. Project state (facts, HEAD `1cfe778`)

| Metric | Value |
| --- | --- |
| Age / commits / authors | 13 days, 108 commits, 1 author (+dependabot) |
| Volume | ~57.7k LOC Go (265 files, ~34 packages), ~5.1k LOC Svelte 5, ~1.4k LOC Python (DeepEval adapter) |
| Quality gates | 100% coverage (`-race`, 3 OS), test:prod ≈ 2:1, shared store contract suite, codepolicy comment gate |
| ADRs | 22/22 implemented and tested; ADR-0022 matches code including its own amendments |
| Distribution | brew tap, signed APT repo, multi-arch Docker (GHCR), 6 GOOS/GOARCH |
| External adoption | **0 stars, 0 forks, 0 issues, 1 release (v0.0.1)** |

Eval layer after P0+P1: `stepkey`/`phasekey`, pairwise `diff` (4-tier alignment cascade), `subgraph`, `aggregate` (presence/status rates, quantile metrics per step/phase), `regress` + persisted baselines + annotation gates (`--annotation owner.key[:direction]`), `regress --record` + `catacomb trends` (schema v3), `bench` with declared checkpoints (`checkpoints:`) and post-cell verification, `repro` fingerprints.

## 3. Target-scenario readiness

Scenario: change a skill → re-run an identical basket → diff runs at user checkpoints → detect regression.

| Stage | State | Note |
| --- | --- | --- |
| Capture CC logs (agent+subagents+MCP+skills) | ✅ Works | Highest-fidelity capture surveyed; thinking blocks not captured at all |
| User checkpoints (CLAUDE.md convention) | ⚠️ Mechanism yes, reliability partial | `mcp__catacomb__mark` / CLI / POST; agent is a stochastic emitter; bench covers task boundaries best-effort and now verifies declared checkpoints post-cell (#107) |
| Run basket k× per variant | ✅ Works | `bench`: deterministic run-ids/labels, resumable manifest, sequential |
| Aggregate run group | ✅ Works | presence/status rates, median/quantiles per step_key/phase_key |
| Diff + verdict vs baseline | ✅ Implemented / ❌ Miscalibrated | §4.1 — defaults produce a gate that cannot fire on rates |
| External semantic scores in gate | ✅ Works (#105) | Direction-aware annotation gates |
| Baseline trends | ✅ Works (#106) | `regress --record`, `catacomb trends`, schema v3 |
| Validated on a real project | ❌ Never | Entire eval layer merged today; zero operational evidence |

## 4. Findings (descending severity)

### 4.1. Gate calibration: systematic false negatives at small k (High) → ADR-0023

Verified against `regress/compare.go`, `regress/regress.go`, `cmd/catacomb/regress.go`. The only exit-code-1 verdict, `regression`, requires **disjoint 95% Wilson intervals** (z=1.96) AND delta for rate metrics. Closed-form consequences:

| Situation | k per side | Verdict | Exit |
| --- | --- | --- | --- |
| Checkpoint present 3/3 baseline → 0/3 candidate | 3 (default MinSupport) | `notable` | **0 — CI green** |
| Same full flip | 4+ | `regression` | 1 |
| Error rate 10% → 40% | ≈40 | `regression` | 1 |
| Error rate 10% → 40% | < ≈40 | `notable`/`ok` | 0 |

`notable` never gates; `--strict` escalates only `insufficient`. At realistic k (3–10) the rate gate fires only on near-total flips with k≥4; everything else needs a human reading the report. ADR-0022 reserved amendment "if miscalibrated" — this is that case, established analytically. Fix (ADR-0023): one-sided Wilson bounds (z=1.645 default, `--z`), `--fail-on-notable`, computed sensitivity disclosure in the report, bench epilogue nudge toward `reps: 5`, sensitivity table in the guide.

### 4.2. Secrets at rest: code diverges from ADR-0008/0020 (High) → ADR-0024

Redaction runs only on read/export paths (`redact.Node`); raw tool inputs/outputs persist in `nodes.body`/`observations.body`; `PayloadHash` is pre-redaction (the brute-force channel ADR-0020 §3 closed on paper); ADR-0008's modes and size cap don't exist (only the OTLP 32 KiB cap). A copied `catacomb.db` leaks everything redaction was designed to catch. Fix (ADR-0024): redact at the persist boundary, post-redaction hashing, `payloads.mode`/`max_bytes` config, schema v4 scrub migration for existing databases.

### 4.3. Claude Code format coupling with zero version gating (High, chronic) → ADR-0025

~40 hardcoded undocumented format strings across 5 parsers (hook envelope, transcript JSONL + `subagents/agent-*.jsonl` layout, stream-json, OTel span names, pricing table). `claude_code_version` is captured but never consulted; unknown shapes are silently dropped, so upstream drift manifests as silent graph thinning. Fix (ADR-0025): per-source unrecognized-record counters surfaced in `status`/`/metrics`/rate-limited logs, plus a tested-version watchlist warning. Fail-open always.

### 4.4. Zero operational validation of the eval layer (High, temporal) → roadmap V-1

100% coverage proves the code matches its spec, not that the statistical model fits real agentic variance. IQR bands and Wilson thresholds have never been checked against real multi-run dispersion. The cheapest de-risking step available: one real basket (2 tasks × 2 variants × 5 reps) before any verdict is trusted.

### 4.5. Remaining

| Finding | Severity | Note |
| --- | --- | --- |
| Bus factor = 1, zero external users | High (unchanged) | The one risk code cannot fix |
| In-task checkpoint reliability | Medium | Deliberate ADR-0022 trade-off (presence-rate visibility); #107 verification narrows it |
| Surface breadth: 6 exporters, 2 viewers, gRPC+SSE, Python adapter | Medium | Built and covered, but permanent solo maintenance cost; freeze per prior review |
| Thinking blocks not captured | Low-Medium | Zero `thinking` handling anywhere; potentially useful behavioral diff signal |
| Pricing table exact-match only | Low | Breaks on every new model id; prefix-family fallback wanted |
| Doc drift: `AGENTS.md:9` status, spec §2 non-goals never amended for ADR-0022 | Low | Hygiene batch |

## 5. Competitive landscape (verified 2026-07-02)

| Tool | CC capture | Eval primitives | Checkpoint run-vs-run diff | Self-host / license | Verified update |
| --- | --- | --- | --- | --- | --- |
| **Catacomb** | 4 reconciled sources | baskets, baselines, N-run aggregation, gates, trends | ✅ only one surveyed | ✅ single binary / Apache-2.0 | P0+P1 merged today |
| CC native OTel | — (is a source) | none | none | — | Full surface now: nested subagents (v2.1.139/145), MCP events, skill events; content opt-in via env; **beta schema, bug #53954 on SDK path** |
| Langfuse | ✅ marketplace plugin (Stop-hook parses JSONL) | platform has datasets/experiments; CC integration docs mention none of dataset/diff/regression | none | ✅ MIT (ee/ commercial) | Plugin ~5 weeks old, 209 installs; docs silent on subagents/MCP/skills |
| Arize Phoenix / AX | ⚠️ `arize-claude-code-plugin` **exists** (9 hook events, OpenInference) — documented for paid Arize AX, not self-host Phoenix | output-level experiments | none | Phoenix: ✅ ELv2 | Morning claim "weakest ingest" partially refuted (0-3): plugin exists, but not for Phoenix |
| promptfoo | ✅ `anthropic:claude-agent-sdk` provider runs the agent under eval | closest harness: YAML baskets, `--repeat`, trajectory assertions over OTel spans | none (per-test-case final-output before/after) | MIT | **Acquired by OpenAI 03/2026**; issue #7333: provider spans not reaching its OTLP receiver |
| Braintrust | ❌ generic OTLP | strongest experiments: immutable runs, per-test-case diff by input, PR gating | none (test-case granularity) | ❌ Enterprise-only | Span-scoped scorers exist; no trajectory alignment |
| Anthropic guidance | — | "re-run task suites at ~100% pass rate; read the transcripts" | none — zero checkpoint/diff mentions | — | Observability story is export-only |
| claude-code-log / claude-devtools | ✅ read ~/.claude | none (pure viewers) | none | MIT | Viewer niche taken on adoption (1.1k★ / 3.6k★) |
| LangSmith, W&B Weave, DeepEval, HoneyHive | — | — | none (per AgentEval survey + morning review) | — | **Coverage gap**: not directly re-verified this round; LangSmith Engine (05/2026) is moving toward failure analysis — watch |

Refuted during verification (documented so they aren't re-asserted): "Phoenix capture requires developer-written instrumentation glue" (0-3 — the Arize plugin exists); "claude-code-log's multi-agent visibility is partial" (0-3); "Anthropic states no single tool covers the workflow" (0-3 — they don't say that).

Structural conclusions:

1. **Capture is commoditizing.** Native CC OTel already covers subagents, MCP, and skills. Long-term the four parsers — the project's costliest maintenance — should degrade toward "native OTel primary + JSONL fallback", not ossify. While #53954 is open and the schema is beta, four sources remain justified.
2. **Differentiation lives only in `stepkey`/`phasekey` + aggregate/regress/bench/trends.** Everything else competitors either do or will.
3. **The vacuum is real but the window is short.** Vendors (LangSmith Engine) are moving into agentic failure analysis; a quarter or two, not a year.

## 6. Build vs buy

Unchanged, strengthened: migration buys a mature UI and a community, sells `step_key`/checkpoints/locality, and leaves 100% of the diff/regression layer to rebuild on a foreign span model. Even Anthropic offers nothing closer than "re-run the suite, expect ~100% pass".

Honest applicability boundary (still holds): if the real need shrinks to "logs + viewer + before/after on final outputs over a fixed dataset", assemble Langfuse plugin + promptfoo/Braintrust in a day and Catacomb is over-engineering. Catacomb's value stands or falls with checkpoint-level regression detection. Note the OpenAI acquisition makes the "simple" promptfoo path less reliable for a Claude-centric stack. The OpenInference exporter remains the live hedge (mirror into Phoenix); keep it.

## 7. Recommendations

**Hardening block — "from implemented to trustworthy" (ordered; roadmap `docs/superpowers/plans/2026-07-02-post-review-hardening-roadmap.md`):**

1. **ADR-0023** — gate sensitivity: one-sided z=1.645 default + `--z`, `--fail-on-notable`, sensitivity disclosure, epilogue nudge, guide table. (PR-J)
2. **ADR-0025** — format drift detection: per-source unknown-record counters, status/metrics/log surfacing, version watchlist. (PR-K)
3. **ADR-0024** — secrets at rest: write-path redaction, post-redaction hashing, payload modes + cap, v4 scrub migration. (PR-L)
4. **Hygiene**: AGENTS.md status, spec §2 amendment note, pricing prefix fallback, reducer commutativity fuzz target. (PR-M)
5. **Dogfood calibration run** (V-1): one real basket (2 tasks × 2 variants × 5 reps, bounded budget) against a scratch project; record observed variance vs thresholds in a results doc; adjust defaults only on evidence.

**Do-not-do (re-confirmed by the market data):** no viewer expansion (niche lost on adoption), no new exporters/backends, no in-house LLM judge (annotation gates + DeepEval/agentevals interops already delegate correctly).

## 8. What would change the verdict

- The need narrows to final-output evals → drop Catacomb, use promptfoo/Braintrust (a day of glue).
- Anthropic or LangSmith Engine ships first-party trajectory diffing → reassess immediately; the OpenInference mirror is the hedge.
- Four-parser upkeep exceeds available time → cut to OTel+JSONL; a tunable cost, not a fatal one.

**One line:** the foundation is now a product that has never been plugged in; the niche is verified empty but the window is short; adopt now for own use, trust the gate only after ADR-0023 lands and one real basket has run.
