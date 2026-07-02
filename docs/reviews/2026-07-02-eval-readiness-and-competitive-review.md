# Eval-readiness design review & competitive analysis

**Date:** 2026-07-02
**Scope:** full-codebase design review (architecture, product surface, eval readiness) plus an adversarially verified market survey of LLM/agent observability and evaluation platforms (21 primary sources fetched 2026-07-02; 20 confirmed / 5 refuted claims), with targeted follow-ups on Arize Phoenix, Braintrust, and W&B Weave.
**Target use case under review:** Catacomb as the foundation for *checkpoint-diff evals* — run an identical basket of agentic tasks (each a Claude Code flow with subagents, MCP tools, and skills) before and after changing a component (skill, MCP tool, prompt), diff the runs at user-defined checkpoints, and detect regressions.

---

## 1. Verdict

**Keep it. But do not call it done: the foundation is built, the product on top of it is not.**

1. **As an observability tool, Catacomb is ready to adopt today.** Every designed surface is implemented and tested: four-source capture, deterministic reducer, SQLite with migrations, SSE/gRPC, web UI + TUI, six exporters.
2. **As an eval foundation, the substrate is done and the experiment-management layer is 0% built.** `step_key`/`phase_key`, pairwise diff, checkpoint subgraph diff, annotations, and repro fingerprints all shipped (PRs #53–#82). Task baskets, baselines, N-run aggregation, statistical treatment of non-determinism, regression gates, and a batch runner are entirely absent — the word "basket" does not occur in the repository. This is roughly 40–50% of the remaining journey to the target scenario, and the hardest part.
3. **There is no off-the-shelf replacement.** Verified across 7 platforms and 5 niche tools: none can diff agentic-flow runs at user-defined checkpoints. Dropping Catacomb for Phoenix would trade away the only differentiating capability and still require building a custom layer on top of foreign traces — without `step_key`.

The existential threats are not architectural: bus factor = 1 with zero external adoption, permanent drift of undocumented Claude Code formats, and the unsolved statistics of non-determinism (a pairwise diff of two stochastic runs will produce false regressions).

---

## 2. Current project state

Metrics at review time: 12 days since first commit (2026-06-20), 99 commits, one author, ~16,000 production Go LOC + ~34,600 test LOC (ratio 2.16), 234 Go files, three-OS CI, 100% coverage gate.

| Layer | State | Notes |
| --- | --- | --- |
| Capture (hooks, OTel, stream-json, JSONL + subagent sub-transcripts) | ✅ Implemented | The most complete capture scheme on the market: competitors read one source; Catacomb reconciles four with per-field precedence and provenance |
| Storage (SQLite, append-only log + materialized graph) | ✅ Implemented | Schema versioning + migrations (ADR-0017, #97), rebuild-from-log, watermark recovery |
| Live surfaces (SSE, gRPC, web UI, TUI) | ✅ Implemented | Outline + Timeline + Diff views; lazy subagents; force-graph view removed as unusable |
| Export (jsonl, OTLP/OpenInference, neo4j, postgres, agentevals, evalview) | ✅ Implemented | OpenInference export = a ready bridge into Phoenix as a hedge |
| Eval substrate (step_key, phase_key, diff, subgraph, annotations, repro) | ✅ Implemented | Cross-run step identity is the key asset no competitor has |
| **Eval management (baskets, baselines, N-way aggregation, gates, scoring)** | ❌ **Absent** | Greenfield. Only primitives: `run --run-id` grouping and pairwise `diff` |
| Re-execution harness (run a basket k times per variant) | ❌ Absent | `catacomb run` wraps a single command; no batch runner |
| Multi-user / remote | ❌ Absent | Deliberate non-goal (ADR-0013): loopback, single operator |

Documentation drift: AGENTS.md still says "early development (milestone M0.1)", contradicting the README's "all designed surfaces are implemented". Should be fixed.

### Engineering quality

Strong and verifiable: 100% coverage enforced by two independent gates (`make cover` + CI), `-race` everywhere, reducer determinism covered by commutativity property tests, PID-reuse-proof singleton guard, value-scanning secret redaction, quarantine instead of panics on malformed input. Two observations:

- **No fuzz/generative testing** — although the system's central promise ("the same observations, in any order, produce the same graph") is exactly the class of invariant fuzzing finds and hand-written tables do not.
- **The discipline (100% coverage, TDD, no comments) taxes solo velocity.** It pays off for the deterministic core (`reduce/`, `store/`); for UI/CLI plumbing a 2:1 test-to-code ratio pre-adoption is a debatable allocation. A choice, not a defect — recorded as a cost.

---

## 3. Competitive landscape (as of 2026-07-02)

| Tool | Ingests CC sessions | Subagents/MCP/skills | Run-to-run diff | User checkpoints | Local-first | License/cost | Momentum 2025–26 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| **Catacomb** | 4 sources, reconciled | All first-class (skill nodes #74) | Trajectory-level by step_key + phase subgraph | ✅ Only one on the market | ✅ Single binary, SQLite | Apache-2.0 | 12 days old, 0 stars, 1 author |
| Langfuse | ✅ Official plugin (hook+JSONL) | Tools yes; plugin fragile (SDK 4.x pin), ~209 installs | ❌ (UX around observations, not trajectories) | ❌ | Self-host MIT, all features | MIT (ee/ folders commercial) | Acquired by ClickHouse 01/2026, 30.2k★ |
| LangSmith | ✅ Official plugin | Subagents traced only on completion; system prompts excluded | agentevals: structural match vs reference (strict/unordered/subset/superset) | ❌ | ❌ Self-host Enterprise-only | Closed-source | Active; plugin v0.2.0 on 2026-07-01 |
| W&B Weave | ✅ Official plugin (OTel GenAI) | Subagents first-class; MCP/skills = generic tool spans | ✅ Trace-to-trace diff with baseline — **closest analog**, but row-based, no semantic alignment | ❌ | ❌ Self-managed = paid license + ClickHouse | Free/Pro $60/mo + $0.10/MB ingest; **no secret redaction** | Acquired by CoreWeave (~$1.7B) |
| Braintrust | ❌ Generic OTLP only, no plugin | Generic spans | Dataset-row diff (outputs+scores), regression blocking — strong, but not trajectories | ❌ | ❌ Hybrid = Enterprise-only | Closed; Pro $249/mo | $80M Series B 02/2026 |
| Arize Phoenix | ⚠️ arize-harness-tracing: on-point but ~16★; subagent fidelity undocumented; native CC spans render generic (not OpenInference) | MCP capture claimed; verify by testing | ❌ Output-level experiment comparison only | ❌ | ✅ pip/Docker, SQLite/Postgres, free | ELv2 (no resale as a service) | 10.4k★, high release cadence |
| promptfoo | ❌ (own harness) | Single-run trajectory assertions | Prompt×model matrix + CI checks; **not** trajectory diff | ❌ | ⚠️ "Fully local" claim refuted 0-3 — verify egress | MIT | 22.8k★, active |
| agentevals | ❌ | ❌ (0 hits for anthropic/mcp in repo) | Structural match vs a reference trajectory | ❌ | ✅ Library | MIT, 632★ | Alive |
| AgentAssay | ❌ No CC adapter | — | Statistical trace fingerprints, baseline-vs-candidate — **the idea worth borrowing** | ❌ | ✅ | AGPL + commercial, 4★ | Preprint 03/2026, single author |
| claude-devtools | ✅ JSONL, zero-config | Recursive subagent trees; token attribution across 7 categories incl. skills and CLAUDE.md layers | ❌ Pure viewer | ❌ | ✅ Electron/Node, zero egress | MIT | **3.6k★ in 4 months** |
| CC native OTel | — (it is a source) | Subagents nested (since ~v2.1.145) | — | — | — | — | Traces beta, unstable schema, SDK-streaming gap (#53954) |

Structural conclusions:

1. **Ingest and viewing are commoditized.** Official Langfuse/LangSmith/Weave plugins cover capture; claude-devtools (3.6k stars in 4 months) covers session viewing — a niche where Catacomb competes head-on and is behind on adoption by orders of magnitude. Further investment in the viewer half has poor ROI.
2. **Checkpoint-diff of trajectories is a verified vacuum.** The research synthesis (which survived refutation attempts): "checkpoint-level run-to-run diffing would have to be custom-built on top of captured traces". The only motion in this direction is Weave (trace diff, but row-based, no phases) and AgentAssay (statistics, but 4 stars and no CC adapter).
3. **Local-first + completeness + Apache-2.0 coexist nowhere else.** Langfuse is MIT but ClickHouse-stack and observation-centric; Phoenix self-host is free but ELv2 with the weakest CC ingest of the big five; Weave and Braintrust are clouds.

---

## 4. Gap analysis against the target scenario

Target: change a skill → run an identical task basket (agentic flows with subagents/MCP/skills) → diff runs at checkpoints declared by the user (e.g. via a CLAUDE.md convention) → detect regressions.

| Scenario step | Catacomb today | Best competitor | Gap |
| --- | --- | --- | --- |
| Capture CC logs | ✅ Maximum fidelity | Langfuse/Weave plugins | None |
| View sessions | ✅ Web UI + TUI | claude-devtools (richer token attribution) | None functionally; huge in adoption |
| Define a task basket | ❌ | promptfoo (test matrix) | **Greenfield** |
| Run basket k× per variant | ❌ (`run` wraps one command) | promptfoo CI | **Greenfield** |
| User-defined checkpoints | ✅ Markers: CLI / POST / `mcp__catacomb__mark` | Nobody | None — unique |
| "Checkpoints in CLAUDE.md" convention | ⚠️ Mechanism exists (agent calls mark per instructions); reliability does not | — | Medium: if the agent forgets to mark, phase alignment collapses |
| Diff at checkpoints | ✅ `diff --phase`, `subgraph`, UI Diff view — strictly pairwise | Weave trace diff (no phases) | Medium: no N-way |
| Regression detection | ❌ No baseline, thresholds, aggregation, CI exit code | Braintrust (output-level) | **Greenfield + unsolved statistics** |
| Scoring storage | ⚠️ Annotations substrate exists; management does not | Braintrust/Phoenix | Medium |

**The critical design hole covered by no ADR: non-determinism.** One run of A against one run of B compares two samples from distributions; a diff will faithfully show differences but cannot distinguish a regression from sampling noise. The eval layer must operate on k runs per variant with aggregation keyed by `step_key`/`phase_key` (phase pass-rate, median/quantile cost/duration/tokens, per-step error rate) and a significance threshold. That is precisely AgentAssay's thesis — the only 2026 work in this niche. Catacomb's substrate (run_id grouping + step_key) is ready for it; the layer itself is undesigned. The next ADR should be exactly this.

---

## 5. Build vs buy: the "drop it for Phoenix" option

What migrating to Phoenix actually buys:

- **Gains:** mature self-host UI, datasets/experiments with output-level comparison, the OpenInference ecosystem, a 10k-star community.
- **Losses:** CC capture (Arize's harness tool has 16 stars; subagent fidelity undocumented; native CC spans render as generic spans in Phoenix), checkpoints, trajectory diff, `step_key`, local secret redaction, the single binary.
- **Still must build:** the entire eval layer (baskets, runs, phase alignment, regression statistics) — now on top of a foreign span model without cross-run step identity, which would have to be reinvented over OpenInference attributes.

"Buy" purchases nothing that is missing and sells what is already built and working. The same holds for Langfuse/Weave with the adjustments from the matrix.

**When "drop it" would be right** — the honest applicability boundary: if the real need narrows to "logs + session viewing + before/after comparison of final outputs on a fixed dataset", that is assembled in a day from the Langfuse plugin + promptfoo/Braintrust, and Catacomb is over-engineering for it. Catacomb's value stands or falls with the requirement for **trajectory/checkpoint-level** regression detection.

Separately: the OpenInference exporter already makes Phoenix a **complement**, not an alternative — Catacomb traces can be mirrored there for visualization. That is a live hedge against this verdict being wrong; keep it.

---

## 6. Risks

| # | Risk | Severity | Mitigation / note |
| --- | --- | --- | --- |
| 1 | Bus factor = 1, zero adoption, 12-day-old project | High | claude-devtools shows the niche can grow 3.6k★ in 4 months — but has no eval layer. The differentiation must ship before someone bolts a diff onto an adopted viewer |
| 2 | CC format drift: JSONL undocumented, OTel traces beta, v2.1.20 precedent | High | Permanent tax. The versioned parser (§17 of the design spec) is the right mitigation, but 4 parsers = 4× the surface of a single-source plugin. The price of fidelity; accept consciously |
| 3 | Non-determinism → false regressions in pairwise diff | High | Undesigned. Without N-run aggregation, eval verdicts will be untrustworthy — and that is the target product |
| 4 | Marker emission unreliability under a CLAUDE.md convention | Medium | The agent is a stochastic checkpoint emitter. Needs a deterministic channel (harness-emitted markers from basket config rules) with agent-side mark as a supplement |
| 5 | Anthropic ships this first-party (they own analytics, OTel, the whole stack) | Medium | Their current analytics is aggregate usage metrics, not sessions or evals. But the plugin marketplace shows where they are looking |
| 6 | Surface sprawl: neo4j/postgres exporters, TUI + web UI in parallel | Low–Medium | Built and covered — do not delete, but freeze: every exporter is perpetual maintenance |

---

## 7. Recommendations (prioritized)

**P0 — what turns the substrate into the product (~2–4 weeks at current velocity):**

1. **ADR on the regression model**: k runs per variant, aggregation by `step_key`/`phase_key`, metrics (phase pass-rate, median cost/duration/tokens, per-step error rate), significance threshold. The most important remaining design decision.
2. **Basket runner** (`catacomb bench` or similar): a declarative basket (tasks × variants × k repetitions) → a series of `run --run-id` invocations → a persisted run group.
3. **N-way aggregated diff with a baseline**: `diff --baseline <group> --candidate <group>` on top of the existing pairwise engine + a regression gate with a CI exit code.

**P1:**

1. Persistent baselines ("golden run group") and trends over them.
2. Deterministic checkpoint emission from the harness wrapper (rules in the basket config); the CLAUDE.md convention as a supplementary channel, not the only one.

**Do-not-do list:**

- Do not extend the viewer — that niche is lost to claude-devtools on adoption and is not the bet.
- Do not add exporters/backends; freeze the surface of the existing ones.
- Do not build an in-house LLM judge/scoring — annotations + the DeepEval/agentevals interops already delegate this correctly.

**Hygiene:** fix the stale status in AGENTS.md (M0.1 → current); add fuzz tests for reducer commutativity.

---

**One-line summary:** Catacomb is a well-designed, solidly built foundation for the one niche that is verifiably empty in July 2026 (checkpoint-diff regression testing of agentic flows), but the product on top of the foundation has not been started; dropping it for Phoenix would trade a unique asset for a commodity and leave the same work undone. Adopt as observability now; the "ready as an eval foundation" verdict comes after the P0 block.
