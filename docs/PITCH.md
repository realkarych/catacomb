# Catacomb — evaluation brief for technical leadership

Audience: CTOs and heads of platform/AI engineering evaluating catacomb for CI
adoption. This brief covers the scientific basis, the methodology, and the
competitive landscape, and it leads with limitations where they exist. Product
documentation lives in the [user guide](guide/README.md); install and tutorial in
the [README](../README.md).

All claims below about third-party tools and the research literature were
re-verified against primary sources (vendor documentation, arXiv, official
engineering blogs) in July 2026. Links are inline.

## The claim

Catacomb is an offline, single-binary statistical regression gate for
[Claude Code](https://www.anthropic.com/claude-code) agents: run the same tasks
repeatedly under a baseline and a candidate configuration, compare the two groups
with statistics built for small samples, and map the verdict to the CI exit code
your pipeline already understands. No daemon, no service, no network — evidence is
secret-redacted local files.

Three verified facts organize the case:

1. **Agent behavior is a distribution, not a value.** The eval literature
   quantifies run-to-run variance so large that single-run comparisons are
   uninformative (details below).
2. **No mainstream tool ships group-vs-group statistical gating** as of mid-2026.
   Every incumbent has a "repeat" primitive; none turns repeats into a
   significance-based CI verdict (verified per-vendor below).
3. **Upstream regressions arrive unannounced.** Anthropic's own
   [postmortem](https://www.anthropic.com/engineering/a-postmortem-of-three-recent-issues)
   describes three infrastructure bugs that silently degraded Claude output quality
   for weeks. A consumer-side gate is the only defense the consumer controls.

## 1. The problem

Teams tune agents — prompts, skills, MCP tools, CLAUDE.md — and every change is a
gamble reviewed by eye. The measured facts:

- **Run-to-run variance dominates single runs.** On
  [τ-bench](https://arxiv.org/abs/2406.12045) (ICLR 2025), GPT-4o's retail success
  falls from ~60% on one trial (pass^1) to under 25% on eight consecutive trials
  (pass^8). [Hochlehnert et al.](https://arxiv.org/abs/2504.07086) (COLM 2025)
  measure 5–15 percentage points of Pass@1 standard deviation across random seeds
  and recommend at least ten repeated runs as the reporting floor.
  [Terminal-Bench 2.0](https://arxiv.org/abs/2601.11868) (ICLR 2026) runs every
  model-agent pair at least five times and reports 95% confidence intervals —
  repeated runs with uncertainty reporting are now the community baseline for
  *leaderboards*, but nobody applies them to *per-change CI gating*.
- **Naive statistics are wrong at CI-realistic sample sizes.**
  [Bowyer, Aitchison & Ivanova](https://arxiv.org/abs/2503.01747) (ICML 2025
  Spotlight) show CLT-based error bars dramatically undercover below a few hundred
  datapoints (~70% actual coverage at nominal 95% for N=10) — and explicitly
  recommend Wilson score intervals, the construction catacomb gates on.
- **The upstream moves under you.** Beyond Anthropic's postmortem, the
  [2025 DORA report](https://dora.dev/insights/balancing-ai-tensions/) finds AI
  adoption correlated with increased delivery instability, and
  [Gartner projects](https://www.gartner.com/en/newsroom/press-releases/2025-06-25-gartner-predicts-over-40-percent-of-agentic-ai-projects-will-be-canceled-by-end-of-2027)
  over 40% of agentic AI projects canceled by end-2027, naming inadequate risk
  controls among the causes.
- **The practice is already emerging inside platform teams.** GitHub runs
  [more than 4,000 offline tests in CI](https://github.blog/ai-and-ml/generative-ai/how-we-evaluate-models-for-github-copilot/)
  against Copilot's agentic behavior; OpenAI publishes first-party guidance on
  [regression-testing agent skills from execution traces](https://developers.openai.com/blog/eval-skills);
  Microsoft ships a [GitHub Action for agent evals](https://github.com/microsoft/ai-agent-evals).
  The pattern is validated; the statistical rigor is the open gap.

## 2. Methodology

Catacomb reduces each recorded session (main transcript plus every subagent
sub-transcript) into a deterministic action graph, aligns runs across repetitions
with two identity axes (advisory step keys; churn-robust checkpoint phase keys),
and compares baseline and candidate groups per metric family. The design decisions
and their grounding:

| Design decision | Grounding | Where implemented |
|---|---|---|
| Repeated, isolated trials per variant; group-vs-group comparison, never single-run diffs | [τ-bench](https://arxiv.org/abs/2406.12045) pass^k decay; [Hochlehnert et al.](https://arxiv.org/abs/2504.07086) seed variance; [Anthropic eval guidance](https://www.anthropic.com/engineering/demystifying-evals-for-ai-agents) | `bench` matrix (tasks × variants × reps); [ADR-0022](adr/0022-regression-detection-over-repeated-runs.md) |
| One-sided Wilson score bounds for rates (error rate, verifier pass, step presence), gated on disjoint bounds AND a minimum delta | [Bowyer et al.](https://arxiv.org/abs/2503.01747): CLT undercovers at small n; Wilson recommended by name | `regress` rate axes; recalibrated in [ADR-0023](adr/0023-regression-gate-sensitivity-at-small-k.md) after an analytically derived small-k impotence fix |
| Exact paired sign test on per-task differences | [Miller](https://arxiv.org/abs/2411.00640): paired designs are free variance reduction; exact tails instead of Miller's CLT intervals per Bowyer's small-n critique | `regress` paired axis (min 5 matched tasks, exact binomial tails) |
| Median + IQR noise bands for continuous metrics (duration, tokens, cost) — an engineering tolerance, deliberately **not** a hypothesis test | Documented as such; the honest limits are in section 5 | `regress` metric axes; [ADR-0022](adr/0022-regression-detection-over-repeated-runs.md) |
| pass^k reliability reporting with the unbiased hypergeometric estimator; reported, never gated (no double-testing of one signal) | [τ-bench](https://arxiv.org/abs/2406.12045) pass^k definition | `regress` reliability block ([CLI reference](guide/cli.md)) |
| Deterministic, offline-re-runnable outcome verifiers, kept out of the agent's reach | [SWE-bench Verified](https://openai.com/index/introducing-swe-bench-verified/): 61.1% of sampled tasks flagged for unfair tests — verifiers must themselves be auditable | per-task `verify` contract ([workflows](guide/workflows.md)) |
| LLM judges stay outside the core; external scores ride the same statistical gate, and judge agreement is measured (Cohen's kappa) before a judge may gate | [GDPval](https://arxiv.org/abs/2510.04374): expert graders trail human inter-rater agreement | `--scores` re-entry + `integrations/judge` calibration utilities |
| Transcripts are first-class evidence; the harness itself is audited | [Holistic Agent Leaderboard](https://arxiv.org/abs/2510.11977): transcript inspection catches shortcuts that pass outcome verifiers | evidence dirs, `pack` audit bundles, graph `diff` |

Two properties matter more than any single test:

- **The gate discloses its own power.** Below minimum support every row reads
  `insufficient` rather than guessing, and each invocation prints when the gate
  *cannot* fire at the current sample size ("paired gate needs k>=5 tasks"). A red
  verdict and an underpowered non-verdict are never conflated.
- **The whole pipeline is deterministic and offline.** The same evidence reduces to
  the same graph and the same verdict on any machine — comparisons are
  reproducible artifacts, not dashboard snapshots.

## 3. Where catacomb sits in the landscape

Primary-source verification (July 2026) of every mainstream eval/observability
platform and the new statistical entrants:

| Tool | Repeats primitive | Group statistics → CI verdict | Claude Code sessions | Offline, no daemon |
|---|---|---|---|---|
| [promptfoo](https://www.promptfoo.dev/docs/integrations/ci-cd/) | `--repeat` | No — flat pass-rate threshold | Yes (drives Claude Agent SDK sessions) | Yes (CLI) |
| [DeepEval / Confident AI](https://www.confident-ai.com/docs/llm-evaluation/dashboards/ab-regression-testing) | `-r` reruns | No — per-case diff on cloud dashboard | No (API-level tracing) | Partially (metrics need an LLM judge) |
| [LangSmith](https://docs.langchain.com/langsmith/repetition) | `num_repetitions` | No — mean/stddev display only | Traces yes (first-party plugin) | No (SaaS / enterprise K8s) |
| [Braintrust](https://www.braintrust.dev/docs/evaluate/run-evaluations) | `trialCount` | No — score diffs in PR comment; CLI fails only on exceptions | Traces yes (first-party plugin) | No (control plane attached) |
| [Langfuse](https://langfuse.com/docs/evaluation/experiments/experiments-ci-cd) | none | No — user-written threshold on mean | Traces yes (Stop-hook plugin) | No (server stack) |
| [Arize Phoenix](https://arize.com/blog/evals-in-ci-how-to-write-llm-evals-as-tests) | `repetitions=N` | No — pass-rate thresholds in pytest | Traces yes (hooks plugin) | Server required (self-hostable) |
| [W&B Weave](https://docs.wandb.ai/weave/guides/core-types/evaluations) | `trials` | No — side-by-side deltas, no gate | Traces yes (plugin runs a background daemon; docs state no redaction) | No |
| [Inspect AI](https://inspect.aisi.org.uk/metrics.html) (UK AISI) | `epochs` + bootstrap/clustered stderr | No two-config comparison, no gate | Runs Claude Code; cannot ingest transcripts | Yes (MIT, local CLI) |
| [OpenAI Evals](https://developers.openai.com/api/docs/deprecations) | — | — | — | Deprecated; shutdown Nov 2026, users migrated to promptfoo |
| [AgentAssay](https://arxiv.org/abs/2603.02601) (2026-03) | Yes | Yes — Wilson/Clopper-Pearson, SPRT | No adapter | pytest plugin; AGPL (corporate stop-lists) |
| [agentrial](https://github.com/alepot55/agentrial) (2026-02) | Yes | Yes — Wilson CI, Fisher exact, CUSUM | No adapter | Early stage (single author, v0.2.0) |

The read across three groups:

- **Platforms** (LangSmith, Braintrust, Langfuse, Phoenix, Weave) all shipped
  first-party Claude Code *tracing* in 2025–2026 — the demand signal — but none
  performs statistical inference over repeated runs, and all are server-attached.
  Catacomb treats them as complements, not competitors: observability is
  explicitly delegated to that substrate
  ([ADR-0026](adr/0026-form-factor-pivot-offline-eval-gate.md)).
- **promptfoo** is the closest incumbent by form factor (local CLI, exit codes,
  first-party agent-session evaluation) and was
  [acquired by OpenAI](https://openai.com/index/openai-to-acquire-promptfoo/)
  in March 2026 — the strongest market validation this category has received. Its
  gate remains a pass-rate threshold with no statistical machinery.
- **The statistical entrants** (AgentAssay, agentrial) validate the methodology
  from the academic side, weeks-to-months old, with no Claude Code support and —
  for AgentAssay — an AGPL license that most corporate OSS policies reject.

The specific combination — small-sample inference (Wilson bounds, exact sign test,
IQR bands) over real agent-session graphs, offline and daemonless, with
secret-redacted evidence and an exit-code verdict — is, as of this writing,
occupied by catacomb alone. None of the incumbents documents secret redaction of
stored transcripts at all.

## 4. Engineering due diligence, preempted

What a platform-team review finds, stated up front:

- **Test discipline:** 100% line coverage enforced in CI (file, package, and total
  thresholds; two justified exclusions), race-enabled tests on Linux/macOS/Windows,
  a 200+-assertion hermetic E2E suite (fixture transcripts, zero API spend)
  required on every PR — including subagent, skill, MCP, redaction, and import
  scenarios — plus four weekly fuzz targets, one of which fuzzes the reducer's
  order-tolerance invariant.
- **Live calibration:** a weekly gate runs 100+ real `claude -p` cells across six
  baskets, asserting seeded regressions are caught and A-vs-A controls stay clean,
  with spend accounting ([workflows guide](guide/workflows.md)).
- **Supply chain:** every GitHub Action SHA-pinned; workflows linted by
  actionlint and zizmor; gitleaks, govulncheck, gosec, and CodeQL on schedule;
  release artifacts ship syft SBOMs and keyless cosign signatures; the publish
  pipeline refuses tags whose commits lack the full set of green required checks
  ([release process](RELEASING.md)).
- **Dependency surface:** four direct runtime dependencies, pure Go (no cgo),
  static cross-platform binaries. The store is embedded SQLite with schema
  versioning and refuse-on-newer guards.
- **Data handling:** the binary has no network imports; evidence passes write-path
  secret redaction with the residual classes explicitly enumerated and quantified
  ([privacy and operations](guide/privacy-and-operations.md)) rather than
  hand-waved.
- **Compatibility:** a seven-surface versioning contract (CLI, basket schema,
  verifier protocol, evidence layout, store schema, key schemes, SDKs) with
  pre-1.0 rules and measurable 1.0 criteria ([VERSIONING.md](VERSIONING.md)).

## 5. Honest limitations

Stated here because a serious evaluation will find them anyway:

- **Small-k power is bounded.** At the realistic 3–10 repetitions the rate gates
  need large effects to fire; the gate discloses this per invocation instead of
  pretending otherwise ([ADR-0023](adr/0023-regression-gate-sensitivity-at-small-k.md)).
- **Wall-clock and cost are noisy axes.** The project's own calibration measured
  ~2x duration drift between identical sequential batches and up to ~5x cost
  variance from prompt caching; `tokens_out` is the validated reliable continuous
  regressor. Continuous bands are a fixed engineering tolerance, not a
  significance test, and the empirical family-wise false-positive rate at default
  thresholds rests on a small number of A-vs-A controls today. The roadmap item
  "gate self-check" (below) exists to close exactly this gap.
- **Independence is an assumption.** Recent work
  ([arXiv:2603.29231](https://arxiv.org/abs/2603.29231)) reports that repeated
  agent episodes can violate i.i.d. with positively correlated errors; Wilson
  bounds and the sign test assume independent trials. Whether the violation makes
  the gate conservative or anti-conservative in this regime is an open question we
  track rather than hide.
- **Single-vendor format coupling.** The sole ingestion source today is Claude
  Code's internal transcript format; drift is detected (counters plus a
  tested-version watchlist) but detection is advisory. The first roadmap item
  below is the structural answer.
- **Redaction is best-effort.** The residual classes are documented with measured
  failure rates; transcripts remain content-complete (prompts, code) — data
  classification for shared evidence is the adopter's call, and the docs say so.
- **Bus factor is one.** Mitigation is fork-viability by construction: Apache-2.0,
  four dependencies, pure Go, 100% coverage, and a 30-ADR decision record.

## 6. Roadmap

Every item below survived an adversarial feasibility review against the codebase
(July 2026); framings reflect the corrected scope, not the ambition.

1. **Codex CLI ingestion** (in progress) — a second transcript adapter parsing
   OpenAI Codex rollout sessions into the same evidence/graph/gate pipeline,
   staged: import-only first, then runtime-aware step salience, pricing, and drift
   ceilings, then `bench` spawn support with a live E2E leg. Scope boundary stated
   honestly: per-runtime baselines under one gate vocabulary — not cross-vendor
   step-level A/B, which step identity makes impossible by construction.
2. **Gate self-check** — an offline A/A audit over a user's own recorded runs:
   time-ordered and leave-one-out splits re-run through the real verdict function
   to report drift sensitivity and single-run influence before a red verdict is
   trusted; plus a documented 2k-rep A/A workflow for measuring false-positive
   behavior at the true operating point.
3. **Interleaved cell ordering, then bounded `--parallel`** — interleaving removes
   the time-of-day confound between variant groups (a validity fix); bounded
   parallelism adds throughput on top of the per-cell workspace isolation that
   already ships.
4. **Baseline bundle, then a GitHub Action** — `baseline export/import` packages a
   pinned baseline (store row, stamps, evidence) as one verifiable artifact for
   ephemeral CI; the Action wraps pinned-install, bench with cost levers, regress,
   and a sticky PR comment rendering the verdict table and sensitivity lines.
5. **Judge-loop closure in documentation** — one end-to-end recipe (pack → any
   external judge → provenance-stamped scores → the same statistical gate), with
   judge-agreement calibration before a judge may gate. The boundary is the
   feature: no LLM calls inside the tool, no data egress.
6. **Opt-in exact Wilcoxon signed-rank** for the paired axis — fires on
   majority-plus-magnitude drift where the sign test needs near-unanimity;
   replaces (never runs beside) the sign test per metric to keep the multiple
   comparison family flat.
7. **Scale envelope** — payload-stripped aggregation (bounded memory for large
   groups), an allocation-gated benchmark suite, and a published "tested to N runs
   × M MB" envelope.
8. **Fleet-ready exports** — a repo-identity stamp plus the already-versioned JSON
   contracts (`regress --json`, `trends --json`, evidence metadata), so a
   monorepo fleet rolls verdict, drift, and spend data into its existing
   warehouse. Deliberately not a hosted service.
9. **Windows artifact smoke** — execute the shipped Windows zip through a real
   bench→verify→regress loop in CI (unit tests already gate releases on Windows),
   with an honest note that the bundled E2E fixtures are Unix-shell based.

## 7. Adoption shape

A pilot is one repository and one afternoon: a two-file basket (agent script plus
YAML matrix), `catacomb bench` on a cheap model tier (the tutorial's demo costs
cents), `catacomb baseline set` to pin the golden group, and `catacomb regress
--record` as a CI step whose exit code is the gate. No infrastructure is
provisioned, no data leaves the runner, and removal is deleting a binary and a
directory. The [tutorial](../README.md) is the pilot script.
