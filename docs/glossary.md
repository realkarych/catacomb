# Glossary

Formal definitions of every term catacomb uses across the [evaluation
brief](PITCH.md), the [user guide](guide/README.md), the [ADR log](adr/README.md),
and CLI output. Entries are a single flat A–Z list. Each definition ends with its
authoritative source.

Terms that describe the **superseded daemon architecture** (ADR-0001, ADR-0002,
ADR-0007, ADR-0013, ADR-0015, ADR-0019, all folded by
[ADR-0026](adr/0026-form-factor-pivot-offline-eval-gate.md)) are marked
*(historical)* and are kept only so older ADRs remain readable.

---

## A

**A/A control** — A comparison in which both groups are drawn from the *same*
variant, so any `regression` or `notable` verdict is by construction a false
positive rather than a real regression. Used to observe the gate's false-positive
behaviour at the operating thresholds. *(ADR-0034; workflows.md; PITCH §4)*

**A/B comparison** — The normal `regress` mode: a baseline group (A) against a
genuinely different candidate configuration (B), gated for regression. Contrast
with an *A/A control*. *(PITCH §2; ADR-0022)*

**action graph** — The directed graph of *nodes* and *edges* that catacomb
produces by reducing a session's transcript records. It is deterministic: the same
transcript records, in any order, converge to the same graph. It is the internal
representation every command computes over. *(concepts.md; ADR-0004)*

**aggregate** — The step that combines a group's repeated runs, per `step_key` and
`phase_key`, into per-metric distributions before comparison. Both group loaders
strip node payloads first, so `regress` memory scales with graph structure, not
transcript size. *(ADR-0022; privacy-and-operations.md)*

**alignment coverage / coverage floor** — The fraction of baseline steps matched in
the candidate group, always reported by `regress`. Below `--coverage-floor`
(default 0.7) step-level verdicts are downgraded to `notable` (untrustworthy) and
the phase level carries the verdict, because changing the component under test
rewrites prompts and shifts `step_key` input hashes. *(ADR-0022; cli.md)*

**annotations** — A per-node slot where external `--scores` values are attached in
memory before comparison; catacomb never interprets their contents, only preserves
and relocates them across graph lifecycle events. Namespaced per writer
(`annotations.<owner>.<key>`). *(concepts.md; ADR-0016)*

**artifacts** — A task's optional list of glob patterns captured, relative to the
cell's working directory, into the evidence directory's `artifacts/`. Text
artifacts are redacted line by line; binaries are copied raw under per-file (10
MiB) and total (50 MiB) caps. `import` captures no artifacts. *(basket.md; cli.md;
workflows.md)*

**assistant_turn** — A *node type*: one model response turn. It is a load-bearing
interior vertex of the prompt→turn→tool structure and is always `core`-tier, so
lean granularity never folds it. *(concepts.md; ADR-0021)*

## B

**baseline** — A named, saved group of evidence runs, created from a label selector
resolved at `baseline set` time. Only the sorted run IDs, the selector, the
resolving runs directory, a timestamp, and version stamps are persisted in the
store — evidence dirs are not copied; `regress` re-reads the pinned runs from disk.
Referenced as `name:<baseline>` so a golden group survives label churn. *(cli.md;
workflows.md)*

**baseline bundle** — One portable, verifiable, byte-deterministic `.tar.gz`
packaging a baseline plus every one of its pinned evidence runs. `baseline export`
writes a `bundle.json` manifest (format version, the baseline record, a SHA-256 per
file) then the run files; `baseline import` verifies every hash before committing
anything. Deterministic bytes let a bundle be content-addressed, cached, or
hash-diffed. Does not carry `--record` history. *(ADR-0032; cli.md)*

**baseline set / list / rm** — The subcommands that manage baselines: `baseline
set <name> --label k=v` pins the currently matching runs under a name; `baseline
list` prints them; `baseline rm <name>` deletes one. *(cli.md)*

**basket** — A declarative YAML file that defines a benchmark as a matrix of `tasks
× variants × reps`. It is the single source of truth for what a cell is — task,
variant, verifier contract, checkpoints, labels — under both `bench` and `import`.
Unknown or misspelled keys are rejected at load. *(basket.md; workflows.md)*

**basket hash** — A hash pinning the basket file's raw bytes, stamped into evidence
and used to guard `--resume`. It covers the basket bytes only, not a referenced
`workspace.patch`; taken over raw bytes, so path resolution does not change cell
identity. *(cli.md; ADR-0029)*

**basket matrix** — The Cartesian expansion `tasks × variants × reps` that `bench`
turns into one *cell* per combination (e.g. 2 tasks × 2 variants × 5 reps = 20
cells). *(basket.md; cli.md)*

**bench** — The command that runs a basket: it expands the matrix into cells,
executes each as a plain local process, verifies declared checkpoints against the
recorded transcripts, and writes a manifest plus one evidence directory per cell.
*(cli.md)*

**bench cell / cell** — One combination of (task, variant, rep) in the expanded
matrix; the unit of execution. Each cell runs under run-id
`bench-<basket>-<task>-<variant>-r<rep>`, carries `basket`/`task`/`variant`/`rep`
labels, and — when its transcripts resolve — writes one evidence directory. In the
audit context, one cell equals one run. *(cli.md)*

## C

**calibrate** — The command that self-checks the gate's power over **one**
variant's recorded runs before a red verdict is trusted. It splits the runs into a
time-ordered first/second half (an A/A comparison), runs the full gate over the
split to surface *drift*, then drops each run in turn (*leave-one-out influence*).
Any gating verdict here is not a real regression. Needs k ≥ 2×min-support (6 at
defaults) for the split, k ≥ 2×min-support+1 (7) for influence; always exits 0 on a
rendered self-check (2 only on operational error), and suggests no thresholds.
*(ADR-0034; cli.md; workflows.md)*

**canonical id / canonical entity** — A node's stable, `execution_id`-prefixed
identifier (`tool_call = execution_id:tool_use_id`, `assistant_turn =
execution_id:message_id`, `subagent = execution_id:agentId`, `session =
execution_id`). The "canonical entity" is the graph-native `Node`/`Edge` model that
is the deterministic reduction of the observation log. *(ADR-0011; ADR-0004)*

**catacomb** — An offline, single-binary statistical regression gate for AI coding
agents. It runs prompt baskets, records each cell's transcripts as secret-redacted
evidence, reduces transcript JSONL into one canonical action graph, derives
cross-run step/phase identity, aggregates repeated runs, and gates regressions
statistically against stored baselines — no daemon, no service, no network.
*(AGENTS.md; README.md; ADR-0026)*

**catacomb-gate** — The composite GitHub Action that wraps the gate for CI: it
installs a pinned, checksum/cosign-verified release, optionally restores a
baseline, optionally benches the PR's basket, runs `regress --format markdown
--json`, posts the verdict as a *sticky PR comment*, and re-raises the `regress`
exit code so the check fails on a regression. All rendering is in the Go binary;
the shell stays logic-free. *(ADR-0033; workflows.md)*

**checkpoint / checkpoint scope** — A *phase* used as a comparison boundary by
`regress`. The checkpoint scope compares groups at the `phase_key` level; because
phase identity is robust to step drift, it is the primary comparison axis and
carries the verdict when step alignment coverage is low. *(concepts.md; ADR-0022)*

**checkpoints** — A task's optional list of phase names the agent is expected to
mark itself (via `mcp__catacomb__mark`) during the run. Each must match
`^[A-Za-z0-9._:-]+$`, be unique within the task, and not equal the reserved
`task:<id>` marker. Verification is best-effort and never gates; misses are
recorded, warned, and surface downstream as presence-rate drops. *(cli.md;
basket.md; workflows.md)*

**Codex CLI (runtime)** — OpenAI's agent CLI, catacomb's second supported
*runtime* (`runtime: codex`). Codex persists each session as a **rollout**: an
append-only JSONL file (optionally zstd-compressed) under a date-partitioned tree.
The session id is Codex's **thread id**, announced as the first `thread.started`
event of `codex exec --json`; subagents are separate rollouts linked by
`parent_thread_id`. Cross-runtime step-level A/B is a permanent non-goal.
*(ingestion.md; cli.md; ADR-0031)*

**Cohen's kappa (judge agreement)** — The chance-corrected agreement between a
judge's binarized verdicts and a hand-labelled gold set — the calibration gate a
judge must pass before it may gate. The canon treats **κ > 0.8** as the trust
threshold; an unmeasurable or uncalibrated judge is treated as failing. Reported by
`catacomb-judge agreement` alongside Spearman ρ and TPR/TNR. *(ADR-0027;
workflows.md; PITCH §2)*

**compatibility surface (versioning contract)** — The union of contracts that count
as catacomb's public API for SemVer arithmetic; a change is breaking iff it can
invalidate a working user setup. `VERSIONING.md` defines **nine** surfaces: (1) the
CLI, (2) the basket YAML schema, (3) the verifier exec contract, (4) the evidence
layout, (5) the store, (6) the key schemes, (7) the Python SDKs, (8) the baseline
bundle format, and (9) the GitHub Action. (PITCH §4 summarizes the first seven as a
"seven-surface" contract; `VERSIONING.md` is authoritative and lists nine.)
*(VERSIONING.md; PITCH §4)*

**cost_usd** — Two distinct notions. The *reported* `cost_usd` in `meta.json` is
read from the transcript's terminal cost event and is omitted for imports and Codex
runs. The *metric* `cost_usd` is priced from transcript token counts through the
built-in pricing table and works for every entry point, so cost gating stays
comparable. It is a noisy continuous axis (up to ~5× variance from prompt caching).
*(cli.md; ADR-0030; ADR-0031; PITCH §5)*

## D

**daemon / sidecar** *(historical)* — The original form factor: a long-running
process hosting ingest adapters, an in-memory graph, the durable store, realtime
surfaces, and exporters, with hooks POSTing to it over loopback. Deleted by
ADR-0026; catacomb now runs no daemon and opens no sockets. *(ADR-0001; ADR-0026)*

**data-format versioning** — Catacomb's policy for versioning its own persisted
formats so a new binary reading old state never silently shifts ids or fails to
deserialize. Post-pivot it reduces to the store schema version plus the version
stamps on baselines and regress records. *(ADR-0017; ADR-0026; VERSIONING.md)*

**diff** — The command that compares two session transcripts by `step_key`,
reporting added, removed, changed, and unchanged steps with per-field deltas (args,
status, cost, duration, tokens). Can be scoped to a phase or a range. Claude Code
only. *(cli.md; workflows.md)*

**disjoint bounds** — The gating condition on a rate: the baseline and candidate
one-sided Wilson bounds do not overlap. Combined with the *minimum delta* condition
(both must hold) to flag a `regression`; a delta over threshold with overlapping
bounds is only `notable`. *(ADR-0022; ADR-0023; cli.md)*

**dominated** — In the accuracy-vs-cost Pareto view, a recorded comparison for
which some other comparison is at least as accurate and at least as cheap, with a
strict advantage on one axis. Exact ties dominate nothing. *(workflows.md;
ADR-0027)*

**drift (calibrate)** — A `regression` or `notable` verdict on the A/A time-ordered
split in `calibrate`. Because both halves are the same variant it is not a real
regression, but the named metric picking up environmental drift across the recorded
sequence (API latency, runner load, a model-side change) or an outlier run.
Reported as a `drift:` line. *(ADR-0034; cli.md)*

**drift detection (capture format drift)** — Making upstream transcript-format
change observable but never blocking. Records that parse as JSON but match no known
shape are counted per reason and surfaced as one stderr warning per invocation; the
graph is still built from everything that parsed, and the warning never changes
stdout, `--json`, or the exit code. A persistent warning is the signal to upgrade
catacomb. *(ADR-0025; ingestion.md)*

## E

**edge** — A directed connection between two *nodes*. Catacomb's graph has two edge
types: `parent_child` and `marker_span`. *(concepts.md)*

**entropy gate** — A Shannon-entropy threshold (~4.3 bits) that lets high-entropy
hex/base64/base64url runs be redacted while sparing low-entropy lookalikes such as
UUIDs and file paths. It can over-redact high-diversity path segments — a
safe-direction false positive that loses data rather than leaking it.
*(privacy-and-operations.md)*

**evidence directory** — The per-cell output directory `<runs-dir>/<run-id>/`
holding secret-redacted transcripts (`session.jsonl`, `subagents/agent-*.jsonl`), a
`meta.json`, and — for bench cells — captured `artifacts/`, plus `scores.jsonl` and
`verify.json` after verification. Files are `0600`, directories `0700`. It is what
`regress`, `verify`, `pack`, and `baseline` read. *(cli.md;
privacy-and-operations.md)*

**event-time vs ingest-time** — Two separated clocks per observation. `event_time`
comes from the source (may be absent or untrusted) and derives node
`t_start`/`t_end`; `observed_at` is wall-clock ingest metadata, never a correctness
input on its own. Ordering and tiebreaks use the monotonic `seq`, never wall-clock;
stored times are UTC. *(ADR-0018)*

**execution_id** — A ULID minted per graph build (per session attach / per replayed
transcript). It is the identity prefix of every canonical node id, scoped to match
the uniqueness domain of `tool_use_id`/`message_id`/`agentId`, so graphs built from
different transcripts never collide. Cross-run identity does not ride it — that
rides `step_key`. *(concepts.md; ADR-0011)*

**exit code** — The gate's verdict as a process exit code: `0` = pass (`ok`), `1` =
regression (or `insufficient` under `--strict`), `2` = operational error (invalid
selector, unknown baseline, unreadable evidence, empty group, failed record append,
etc.). A failed record append (2) takes precedence over a regression (1) so a
broken store never masquerades as a clean signal. *(cli.md; VERSIONING.md)*

**export** — The command that emits a transcript or evidence directory as a
materialized JSONL graph snapshot (`node`/`edge`/`run` records). Over an evidence
directory it dispatches on the meta stamp and works for Codex; over a transcript
path it is Claude Code only. The input format for the DeepEval bridge. *(cli.md;
workflows.md)*

## F

**--fail-on-notable** — Off by default; when set, `notable` findings count toward
the gate exactly like regressions (exit 1). The recall-over-precision knob for
small run counts. *(ADR-0023; cli.md)*

**family-wise false positive rate** — The aggregate false-positive rate across the
multiple comparisons in the gate's test family. For the four correlated paired
metrics tested at the same `--paired-alpha` with no multiplicity correction it is
bounded by ~4× alpha (union bound). Catacomb deliberately publishes no empirical
family-wise number, rejecting it as false precision. *(cli.md; ADR-0034; ADR-0035;
PITCH §5)*

**forest** *(largely historical)* — The daemon-era model in which the store held
many runs, each a connected subgraph, supporting cross-run queries. In the current
offline model, `parent_child` is still a *forest invariant* — every node has at
most one parent and the relation is acyclic. *(ADR-0005; ADR-0021)*

**form factor / form factor pivot** — The shape in which catacomb ships and is
operated. The **pivot** (ADR-0026) narrowed catacomb from a daemon-based
observability platform to a single offline CLI — a statistical gate for
checkpoint-level regressions reading transcript JSONL as a pure function over files
— and delegated observability to an off-the-shelf vendor substrate. *(ADR-0001;
ADR-0026)*

**four-source capture** *(historical)* — The original ingestion model: reconciling
four live Claude Code producers — hook processes (the backbone), a native OTLP
exporter, stream-json stdout, and transcript JSONL — into one graph. Superseded by
ADR-0026; the sole source today is transcript JSONL. *(ADR-0002; ADR-0026)*

## G

**granularity (rich / lean)** — The `graph.granularity` config axis. `rich`
(default) materializes both node tiers; `lean` materializes only `core`-tier nodes
and folds `detail`-tier nodes into attributes/metrics on their enclosing node,
transitively re-linking edges so none is left dangling. *(ADR-0004; ADR-0021)*

## H

**half-open window** — The bounding rule for a *phase* (and for `--from`/`--to`
ranges): the start is inclusive and the end is exclusive,
`[start_marker.t_start, end_marker.t_start)`. A node whose `t_start` lands exactly
on a shared boundary belongs to the phase that starts at it, never both, so it is
never double-counted. *(concepts.md)*

**hooks backbone** *(historical)* — Claude Code hook processes used as the
ingestion backbone and system of record in the daemon architecture, with a tiny
forwarder POSTing events and failing open. Deleted by ADR-0026; hook-only signals
(e.g. `blocked`, PreCompact) are explicitly lost. *(ADR-0001; ADR-0013; ADR-0026)*

## I

**import** — The command that ingests an already-finished session transcript into a
bench-cell-shaped evidence directory, so `verify` and `regress` read it with no
special case. Basket-anchored (the task `cmd` is ignored); writes redacted
`session.jsonl`, subagent transcripts, and `meta.json`, but captures no artifacts.
Default run-id `import-<basket>-<task>-<variant>-r<rep>`. *(cli.md; ADR-0030)*

**improvement (verdict)** — The symmetric counterpart to `regression`: a candidate
that moves a rate in the better direction with disjoint bounds and delta exceeded,
or the mirror one-sided tail on the paired test. Reported, never gated as a
failure. *(cli.md; ADR-0035)*

**insufficient (verdict)** — The verdict issued when a group is below
`--min-support`, a scored key is present on only one side, or the paired layer has
too few matched tasks. The gate reports it instead of guessing; `--strict`
escalates it to exit 1. A red verdict and an underpowered non-verdict are never
conflated. *(PITCH §2; ADR-0022; cli.md)*

**interactive session import** — Using `import` to bring a session run by hand in
the interactive Claude Code TUI (which emits no machine-readable stdout) into the
same evidence shape as a bench cell — a second entry point to the same gate, one
cell at a time. *(cli.md; workflows.md; ADR-0030)*

**interleaved cell ordering (rep-major)** — Cells run sequentially in rep-major
order (rep → task → variant), so time-of-day API drift in latency and cost spreads
evenly across variants instead of biasing one side of a comparison. A validity fix
at zero wall-clock cost. *(cli.md; basket.md; PITCH §6)*

**IQR (interquartile range)** — `P75 − P25` of a group's metric distribution. Used
in the metric noise band (factor `--iqr-factor`, default 1.5) and, at a wider
factor (`--audit-iqr-factor`, default 3.0), in the per-cell outlier audit.
*(cli.md; ADR-0022)*

**IQR noise band / median band** — The rule that flags a continuous metric: the
candidate median is flagged when it falls outside `baseline median ±
max(metric-rel-delta × |median|, iqr-factor × IQR)`. It is an engineering
tolerance, not a hypothesis test, and is rep-count-invariant, so systematic drift
below the relative delta never flags here (that is the paired axis's job).
*(ADR-0022; cli.md; PITCH §2)*

## J

**judge (LLM judge)** — An external LLM-based scorer treated as a measurement
instrument that stays *outside* the core — catacomb never calls an LLM. Its scores
ride the same `--scores` gate, but an uncalibrated judge should gate nothing: its
agreement must be measured (Cohen's kappa) before it may gate, and never picked
from the model family under test. *(ADR-0027; workflows.md; PITCH §2)*

**judge panel** — Aggregating heterogeneous judges to wash out individual biases:
`catacomb-judge panel` groups score lines by run and key (one judge per distinct
`tool`, provenance required) and emits one line per group — the mean by default, or
a strict majority vote over an odd panel. Its output is ordinary `--scores` input.
*(ADR-0027; workflows.md)*

## L

**labels** — Arbitrary `k=v` metadata for grouping and filtering. Each cell carries
`basket`/`task`/`variant`/`rep` labels merged over any ambient ones; keys match
`[a-z0-9_.-]{1,64}` and values cap at 256 bytes. Stored in `meta.json` and matched
by `label:` selectors in `regress` and `baseline set`. *(ingestion.md)*

**leave-one-out influence** — In `calibrate`, dropping each single run in turn,
re-evaluating the split, and reporting every run whose removal flips the overall
verdict. A verdict hinging on one run is fragile. Needs k ≥ 2×min-support+1 (7 at
defaults). *(ADR-0034; cli.md; PITCH §6)*

**LIFO nesting / occurrence** — How repeated phases of the same name pair up. By
default each end closes the most recently opened still-open phase of that name
(correct bracket matching); occurrence numbers are assigned to starts in time order
(first start is occurrence 0). An explicit `occurrence` on both start and end
overrides LIFO, used to disambiguate genuinely overlapping same-name phases.
*(concepts.md)*

## M

**manifest** — A JSONL file (default `<basket>.manifest.jsonl`) written
incrementally, one object per completed cell (run-id, coordinates, exit code,
session id, marked checkpoints, cost, evidence dir, basket hash, finish time).
`--resume` reads it back and skips cells already present. *(cli.md)*

**marker** — A *node type* that records a named *phase* boundary. Markers come from
two places: the agent calling `mcp__catacomb__mark` during a run (the reducer
synthesizes the marker from the tool call), and `bench` synthesizing `task:<id>`
start/end markers around each cell from the child's wall-clock window. *(concepts.md;
cli.md)*

**marker_span** — An *edge type* linking a marker to every node whose `t_start`
falls inside its phase window. *(concepts.md)*

**matched tasks** — Tasks present in both groups (carrying `task` labels) with at
least `--min-support` runs per side; each contributes one per-task median delta per
continuous metric to the paired axis. The paired test refuses to gate below
`--paired-min-tasks` (default 5) matched tasks. *(cli.md; workflows.md)*

**mcp (command) / mark tool** — `catacomb mcp` runs the catacomb MCP server over
stdio, exposing a single `mark` tool so an in-run agent can record phase
checkpoints. Wired in via `--mcp-config`; the server named `catacomb` exposing
`mark` surfaces to the agent as `mcp__catacomb__mark`, whose fields are `name`,
`boundary` (`start`/`end`), optional `occurrence`, and an opaque `state_ref`.
*(cli.md; workflows.md; ingestion.md)*

**mcp_call** — A *node type*: an MCP tool invocation. *(concepts.md)*

**meta.json** — The immutable run-metadata ledger in each evidence directory:
run-id, coordinates, session id, labels, exit code, reported `cost_usd`, basket
hash, the `task:<id>` marker window, `finished_at`, and an `env` block stamping
`catacomb_version`, `model_id`, `claude_code_version`, `resources`, and (for
Codex/workspace cells) `agent_runtime`/`workspace`. The env stamps are descriptive
provenance and never gate. *(cli.md; ingestion.md)*

**metric axis / continuous metric** — The continuous observables compared with a
noise band rather than a hypothesis test: `duration_ms`, `cost_usd`, `tokens_in`,
`tokens_out`, `occurrences`, and (run totals) `nodes`. `tokens_out` is the
validated reliable regressor; duration and cost are noisy. *(ADR-0022; cli.md;
PITCH §5)*

**minimum delta** — The effect-size threshold a rate or metric difference must
clear *in addition to* the statistical condition before flagging: `--presence-delta`
(0.2), `--error-delta` (0.1), `--annotation-rate-delta` (0.1) for rates,
`--metric-rel-delta` (0.25) for metrics. The AND-with-statistics rule suppresses
small-sample noise. *(cli.md; ADR-0022; ADR-0023)*

**minimum support** — The minimum runs per group required for a trusted comparison
(`--min-support`, default 3, must be ≥ 1). Below it a finding is `insufficient`
rather than a guess. A floor applied everywhere. *(cli.md; ADR-0022)*

## N

**node** — A vertex of the *action graph*, keyed by an `execution_id` and carrying
timing, cost/token, status, `payload_hash`, `step_key`, `phase_key`, `annotations`,
and `tier` fields. *(concepts.md; ADR-0004)*

**node types** — The eight kinds of node: `session` (a session root), `user_prompt`
(a submitted user message), `assistant_turn` (a model response turn), `tool_call`
(a tool invocation), `mcp_call` (an MCP tool invocation), `skill` (a skill
invocation), `subagent` (a spawned subagent), and `marker` (a phase boundary).
*(concepts.md)*

**notable (verdict)** — A rate finding whose delta exceeds its threshold but whose
confidence bounds overlap; also the downgrade applied to step-level findings below
the coverage floor. It never gates by default (exit stays 0); `--fail-on-notable`
makes it count. *(ADR-0023; cli.md)*

## O

**offline eval gate** — Catacomb's current form factor and product identity: a gate
that is a pure function over local files (the binary plus transcript files), runs in
CI with no daemon, no service dependency, and no network. *(ADR-0026; AGENTS.md)*

**one-sided bound** — Because the regression and improvement checks are directional,
each rate uses a one-sided Wilson bound at 95% one-sided confidence (z = 1.645 by
default, `--z`) rather than a two-sided 95% bound (z = 1.96). Switching from
two-sided to one-sided is what lets the rate gate fire at small run counts at all.
*(ADR-0023; cli.md)*

**OTel / OpenInference mappers** *(historical)* — The boundary mappers translating
the graph to and from OpenTelemetry / OpenInference backends. Both directions were
deleted by ADR-0026; the substrate integration is now documentation-only, with zero
code coupling. *(ADR-0004; ADR-0007; ADR-0026)*

## P

**pack (audit bundle)** — `catacomb pack <selector>` exports a deterministic,
stride-sampled set of already-redacted evidence runs (copied verbatim) plus a
`pack.json` manifest and an `INSTRUCTIONS.md` briefing, for external human or
self-driven-LLM review. Findings return through the same `--scores` gate as an
`audit.`-prefixed key. Distinct from a baseline bundle: a sampled reviewer bundle
vs a complete restorable reference. *(cli.md; ADR-0032; workflows.md)*

**paired axis** — Scope `paired`: when both groups carry `task` labels, every
matched task contributes one candidate-minus-baseline delta of per-task medians per
continuous metric. A paired test over these deltas catches systematic drift *below*
the metric band (e.g. a +10% cost creep across eight tasks fires while staying
inside every median band). Grounded in Miller: paired designs are free variance
reduction. *(cli.md; ADR-0027; PITCH §2)*

**parent_child** — An *edge type*: structural containment (session → prompt, turn →
tool). A forest invariant — every node has at most one `parent_child` parent and the
relation is acyclic. *(concepts.md; ADR-0021)*

**Pareto frontier / dominated** — `trends --pareto` plots each recorded comparison
as an (accuracy = `verifier.pass` rate, cost = `cost_usd`) point, sorted so the
frontier leads; a row is *dominated* when another is at least as good on both axes
with a strict edge on one. *(workflows.md; ADR-0027)*

**pass^1** — The single-trial success rate (k = 1) endpoint of the pass^k curve;
equivalently the plain per-task verifier pass rate. *(cli.md; PITCH §1)*

**pass^k** — A reliability metric over per-task `verifier.pass` outcomes: for a task
with `n` scored runs and `c` passes, `pass^k = C(c,k)/C(n,k)` — the unbiased
estimate that all `k` independent trials succeed — computed for k = 1..k_max (the
smallest scored n in the group). A flat curve is a reliable agent; a steep drop is a
coin-flipper that averages well. Reported, never gated. *(cli.md; ADR-0027;
PITCH §1)*

**patch handover** — The mechanism by which a declared `workspace.patch`'s absolute
path is exported to `workspace.cmd` — and only to it — as `CATACOMB_PATCH`. Applying
the patch is the user command's job; catacomb carries no VCS or diff semantics. The
agent and the verifier never see it. *(cli.md; ADR-0028)*

**payload_hash** — A node field holding the SHA-256 of the node's *redacted*
content, not the content itself; no pre-redaction hash is computed, stored, or
exported. *(concepts.md; ADR-0024; privacy-and-operations.md)*

**per-cell outlier audit** — A structurally non-gating screen: every `regress`
comparison flags each group's individual cells against the group median on
`duration_ms`, `cost_usd`, `tokens_in`, `tokens_out`, and `turns` when
`|v − m| > max(audit-rel-delta × |m|, audit-iqr-factor × IQR)` (defaults 0.5 and
3.0; groups under three cells produce no flags). A flag is an invitation to read a
run's evidence, never a regression, and never affects the exit code. *(cli.md;
workflows.md)*

**per-cell workspace isolation** — An optional `workspace:` block that makes `bench`
materialize a *fresh working directory for every cell*, so repetitions cannot
contaminate each other. The runner runs `workspace.cmd` to provision it, uses it for
setup/agent/artifacts/verifier, and removes it after an unconditional `teardown`
(unless `--keep-workspaces`). *(ADR-0028; cli.md; basket.md)*

**phase** — The *half-open* time window between a start marker and an end marker,
`[start.t_start, end.t_start)`. Every node whose `t_start` falls inside receives a
`marker_span` edge. Phases scope diffs, subgraph extraction, and the checkpoint
scope of `regress`. *(concepts.md)*

**phase_key** — The cross-run identity of the phase a node belongs to, deterministic
so the same phase in two sessions can be compared. Computed as
`hash(enclosingStepKey, name, occurrence)`, so attempt k of a repeating phase aligns
across runs while staying distinct within a run. *(concepts.md; ADR-0016)*

**pre-1.0 rules / 1.0 criteria** — The versioning discipline while catacomb is
pre-1.0: `0.MINOR++` for any breaking change to a surface or a completed feature
wave (with a migration note), `0.x.PATCH++` for fixes that add no surface. `v1.0.0`
is cut only when the verifier contract and basket schema survive two consecutive
minors unchanged, the weekly live gate is green four consecutive runs, and at least
one real production basket runs against it routinely. *(VERSIONING.md)*

**presence rate** — The fraction of runs in a group that contain a given step or
phase, gated as a rate. A missing agent-emitted checkpoint is thereby a first-class
signal (a presence-rate drop), not an error. *(ADR-0022; workflows.md)*

**pricing table** — The built-in per-model table that prices the token-derived
`cost_usd` metric, carrying Anthropic and OpenAI GPT-5-family tiers. Model ids
without a published price stay unpriced rather than guessed; the OpenAI long-context
surcharge is not modeled, so very long requests are undercounted. *(cli.md;
ingestion.md; ADR-0031)*

**--project (project stamp)** — A `regress` flag that stamps a repository identity
into the recorded history row (the `project` field of record body schema v2) for
fleet-level joins across many repos. Requires `--record`; the stamp lives on the
recorded history, not on evidence. *(cli.md; workflows.md; VERSIONING.md)*

## R

**rate axis** — The family of higher-or-lower-is-worse rates gated with Wilson
bounds: presence rate, error rate, and run-level binary annotations such as
`verifier.pass`. A rate flags `regression` only when the one-sided bounds are
disjoint AND the delta exceeds threshold. *(cli.md; ADR-0022; PITCH §2)*

**reaper** *(historical)* — The daemon-era idle mechanism that set a run with no new
observation for a quiescence window to status `abandoned`, releasing the reducer
shard and making the run TTL-eligible. *(ADR-0012)*

**reconciliation** — Re-reducing observations to resolve conflicting or late data.
Non-status fields resolve by a per-field precedence table; status resolves by the
*status lattice* instead. *(ADR-0014; ADR-0010)*

**--record** — A `regress` flag that appends the full comparison (candidate
selector, thresholds, annotation specs, and the report) to a named baseline's
append-only history for `trends`. Requires `--baseline name:<x>`; a failed append is
an operational error (exit 2). *(cli.md; workflows.md)*

**redaction** — The process by which the `redact` package replaces secret and
sensitive values with typed `‹redacted:reason›` placeholders, applied by a pattern
pack (DSNs, cloud keys, tokens, PEM blocks, JWTs, high-entropy strings) and by
sensitive key-path tokens. It narrows what a copied evidence dir can leak but is
explicitly best-effort, not a guarantee. *(privacy-and-operations.md; ADR-0020)*

**reducer / reduce** — The component that turns parsed transcript observations into
the one canonical action graph: it enforces the forest/acyclicity invariants,
synthesizes markers from `mcp__catacomb__mark` tool calls, and produces a
deterministic result. "Reduce" is the act of building the graph. *(concepts.md;
ADR-0011; ADR-0021)*

**regress** — The command that compares a candidate run group against a baseline and
gates on the verdict. Selectors are `label:k=v[,...]` (scanning evidence
`meta.json`) or `name:<baseline>` (from the store). Groups are aggregated and
compared across four scopes — run totals, paired per-task deltas, checkpoint phases,
and steps — and the overall verdict maps to the exit code. *(cli.md; ADR-0022)*

**regression (verdict)** — The verdict that drives exit code 1 by default: a rate
with disjoint bounds and delta exceeded, a metric outside its noise band, or a
paired test below its alpha. The only verdict that fails the gate without a flag.
*(cli.md; ADR-0022)*

**regression gate** — Catacomb's core mechanism: run the same tasks repeatedly under
a baseline and a candidate configuration, compare the two groups with statistics
built for small samples, and map the verdict to a CI exit code. Offline,
deterministic, and dependency-free. *(PITCH §1; ADR-0022)*

**reliability block** — The report section carrying the pass^k curves.
Informational only — it never gates, because the underlying binary data already
gates via the `verifier.pass` rate axis, and double-gating one signal would inflate
false positives. *(cli.md; PITCH §2)*

**rep / repetition** — The `reps` count: repetitions per cell, an integer ≥ 1,
recorded as the `rep` label. The docs recommend `reps: 5` or more because the rate
gate cannot fire reliably below that. *(basket.md; cli.md)*

**replay** — The command that builds an in-memory graph from a single recorded
Claude Code transcript and prints a node/edge summary. Nothing is persisted. Claude
Code only. *(cli.md)*

**residual classes (known residuals)** — The secret classes redaction deliberately
does not catch: hex/base32-encoded ASCII secrets, UUID-shaped secrets
(indistinguishable from session-id UUIDs), adversarially padded secrets diluted
below the entropy gate, and a ~3% sub-threshold tail of short base64url secrets.
Documented and quantified rather than hand-waved; transcripts remain
content-complete. *(privacy-and-operations.md; PITCH §5)*

**run** — One `bench` cell: a single session (plus its subagent sub-transcripts)
recorded as an evidence directory whose `meta.json` carries the run-id, labels, exit
code, cost, and `task:<id>` marker window. `regress` and `baseline set` group runs
by matching those labels. *(concepts.md)*

**run (command)** — Sugar that sets `CATACOMB_RUN_ID` for a wrapped process so every
child session inherits the same wrapper run-id. *(ADR-0005)*

**run-id / run_id** — Two related things. A cell's *run-id* is its evidence
directory name, format `bench-<basket>-<task>-<variant>-r<rep>` (`import-` prefix
for imports). `run_id` as an identity concept is a non-identifying grouping label
(the `CATACOMB_RUN_ID` wrapper, or `session_id` by default); it never participates
in node identity — that rides `execution_id`, and cross-run identity rides
`step_key`. *(cli.md; ADR-0011; ADR-0005)*

**run-level scores** — A score line that omits `step_key`, attaching to the run as a
whole and gating at the `total` scope. When every value for a key is 0/1 it is gated
as a rate (one-sided Wilson bounds, `--annotation-rate-delta`); a non-{0,1} key is
gated with the metric median band. *(cli.md; workflows.md)*

**runtime** — The agent CLI whose sessions a basket gates, declared in the top-level
`runtime:` field: `claude-code` (default) or `codex`. One basket, one runtime;
mixed-runtime baskets are rejected. Parsing is a per-runtime adapter behind one
seam; everything downstream is runtime-neutral. *(basket.md; ingestion.md;
ADR-0031)*

## S

**--scores (scores re-entry)** — A `regress`/`calibrate` input: a JSONL file of
external scores applied as node annotations in memory before comparison (nothing
written back). Each line carries `key` (`owner.key` grammar), numeric `value`,
optional `step_key` (omitted = run-level), and `run_id`. This is how LLM judges and
other external signals ride the same statistical gate; each key needs its own
`--annotation` flag to gate, except `verifier.pass` which gates by default. *(cli.md;
workflows.md; PITCH §2)*

**scores.jsonl** — A file in the evidence directory holding a cell's scores in the
run-level scores dialect. A verifier's stdout is rewritten to it, and `regress
--runs-dir` auto-loads it, gating on `verifier.pass` by default. *(cli.md;
workflows.md)*

**secrets-at-rest / write-path redaction** — Redaction enforced at the persist
boundary, so what is on disk is redacted, not just what is served. Evidence copies
are written through the redactor line by line on write and never contain
pre-redaction bytes; redaction is a pure, deterministic function of the bytes.
*(ADR-0024; ADR-0020; privacy-and-operations.md)*

**sensitivity disclosure** — `regress` evaluating its own verdict function on the
maximally-separated inputs for the actual group sizes; when even that cannot reach
`regression`, the report prints a `sensitivity:` line naming the smallest run count
at which a full flip would gate. A gate that cannot fire is never silent about it.
*(ADR-0023; cli.md; PITCH §2)*

**seq** — A persisted, global, monotonic, gap-free counter assigned at the
observation-log append boundary; authoritative for receive order and the merge
tiebreak, and deliberately excluded from `step_key`. *(ADR-0010; ADR-0018)*

**session** — A *node type*: the session root. Also the unit a `run` records — one
session plus its subagent sub-transcripts. *(concepts.md)*

**session_id / session anchor** — The source-native session identifier, the
cross-source join key. It remains the anchor for a recorded session and is mapped to
an `execution_id` on the run metadata; used as the fallback `run_id` when no wrapper
is set. *(ADR-0005; ADR-0011)*

**setup** — A variant's optional list of commands run before the agent in every
cell, as plain exec (no shell — no pipes, redirects, `&&`, quoting, or globbing).
Must be idempotent because it re-runs before each cell. *(cli.md; basket.md)*

**sign test (exact paired)** — The default paired test (`--paired-test sign`): an
exact one-sided sign test over the non-zero per-task deltas (zero deltas dropped),
flagging `regression` when the probability of that many increases under no change is
≤ `--paired-alpha` (default 0.05), using exact binomial tails. The signal is
repeated *direction* across tasks, not magnitude; the smallest firing signal is five
unanimous shifts. *(cli.md; ADR-0035; PITCH §2)*

**single binary / daemonless** — Catacomb ships as one static cross-platform binary
with no runtime, no dependencies, and no config file — enabled by the pure-Go,
no-cgo constraint (`modernc.org/sqlite`). It runs no daemon, opens no sockets, and
needs no network. *(README.md; AGENTS.md; ADR-0006; ADR-0026)*

**skill** — A *node type*: a skill invocation. *(concepts.md)*

**status / status lattice** — A node's lifecycle status and the order-independent
rule that reconciles conflicting observations of it. concepts.md lists the everyday
statuses `pending`, `running`, `ok`, `error`; the full lattice adds inferred and
terminal states (`blocked`, `cancelled`, `unknown`, `superseded`, `abandoned`) and
guarantees a genuine terminal (`ok`/`error`/`blocked`) always beats a provisional
one regardless of arrival order. *(concepts.md; ADR-0012; ADR-0014)*

**step_key** — A run-invariant node identity computed from a node's structural path
plus a normalized, *redacted* hash of its salient tool input (an edit's file path, a
shell call's command), deliberately excluding volatile ids and timestamps. It is the
cross-run join key that aligns "the same logical step" across repeated runs and the
recommended key for attaching annotations. Versioned as `stepkey/v1` and stamped
onto baselines so a scheme change is detected, not silently misaligned. *(concepts.md;
ADR-0016)*

**store** — Catacomb's durable local state: an embedded SQLite database (pure-Go
driver) behind a `Store` interface. Post-pivot it holds only baselines and recorded
regression history — never transcripts or payloads. It is at schema v5 (the
daemon-era graph tables were dropped in the pivot) and refuses to open a
newer-than-supported schema. *(ADR-0006; ADR-0026; VERSIONING.md)*

**--strict** — A `regress` flag that escalates `insufficient` to exit 1 and refuses
a stampless or stamp-mismatched `name:` baseline (exit 2). *(cli.md; workflows.md)*

**subagent** — A *node type*: a spawned subagent, nested under the turn that
spawned it, its inner tree parsed from its own sub-transcript. *(concepts.md)*

**subagent sub-transcript** — The separate transcript per spawned subagent
(`subagents/agent-*.jsonl` for Claude Code; a linked rollout for Codex). Catacomb
parses the main transcript plus all subagent transcripts into one graph.
*(ingestion.md; cli.md)*

**subgraph** — The command that extracts the execution subgraph delimited by a
checkpoint phase from a transcript and prints node/edge counts plus node lines.
Claude Code only. *(cli.md; workflows.md)*

**superseded (ADR status)** — An ADR lifecycle status marking a decision replaced by
a later ADR; the superseded record stays on file for history but no longer governs.
ADR-0026 carries a supersession map assigning each prior ADR an effect of
Superseded, Amended, or Unchanged. *(adr/README.md; ADR-0026)*

## T

**task** — Each entry of a basket's `tasks` list: the agent command and how to run
and check it (`id`, `cmd`, optional `dir`/`env`/`checkpoints`/`timeout`/`artifacts`/
`verify`/`workspace`). The task `cmd` drives `bench` only — `import` ignores it.
*(basket.md)*

**task:\<id\> marker window** — Start/end phase markers the runner synthesizes from
the child's wall-clock window (for `import`, from the transcript's first and last
timestamps), giving `regress` a stable checkpoint axis even when the agent forgets
to mark its own. Surfaces as phase rows under the checkpoint scope. *(cli.md;
ingestion.md)*

**teardown** — A `workspace:` block's optional command, run in the workspace
directory unconditionally after the cell (success, failure, or timeout) on a fresh
context with its own one-minute deadline. A failing teardown warns but never flips a
verdict. *(ADR-0028; cli.md; basket.md)*

**tier (core / detail)** — A per-node value controlling export granularity: `core`
or `detail`. Under `rich` granularity both materialize; under `lean` only `core`
does. `assistant_turn` is core and never folded. *(concepts.md; ADR-0004)*

**timing fields (t_start / t_end / duration_ms)** — A node's per-node timing.
`t_start`/`t_end` are derived from source `event_time`; a negative duration is
dropped to null with a flag rather than stored. *(concepts.md; ADR-0018)*

**tokens_in / tokens_out** — First-class continuous metrics that gate normally.
`tokens_in` counts *uncached* input (cached input is tracked separately and priced
at the cache-read rate); `tokens_out` is the validated reliable continuous
regressor. Semantics are identical across Claude Code and Codex. *(cli.md;
ingestion.md; PITCH §5)*

**tool_call** — A *node type*: a tool invocation such as a file read or shell
command. *(concepts.md)*

**TPR / TNR** — True-positive-rate and true-negative-rate, which replace accuracy in
judge calibration on purpose: on an imbalanced gold set a judge that always answers
"pass" scores high accuracy while its TNR is 0. Metrics that would be meaningless are
omitted rather than fudged. *(workflows.md)*

**transcript JSONL** — Catacomb's single ingestion source: an agent CLI's
append-only `.jsonl` record of a session (the main transcript plus one
sub-transcript per subagent) — prompts, assistant turns, tool and MCP calls, token
usage, cost, and the subagent tree. Every command builds its graph by parsing it.
*(ingestion.md)*

**trends** — The command that replays a baseline's recorded regression history
(written by `regress --record`), oldest-first. Offers a wide verdict table, a
single-metric view, an accuracy-vs-cost `--pareto` table, and raw `--json`. Reads
the store read-only. *(cli.md; workflows.md)*

## U

**ULID** — The identifier type used for `execution_id` (minted per graph build) —
lexicographically sortable and collision-free across builds. *(concepts.md;
ADR-0011)*

**unbiased hypergeometric estimator** — The estimator behind pass^k:
`pass^k = C(c,k)/C(n,k)`, the unbiased estimate that all k independent trials
succeed, drawn without replacement from n runs with c passes. *(cli.md; ADR-0027;
PITCH §2)*

**user_prompt** — A *node type*: a user message submitted to the model.
*(concepts.md)*

## V

**variant** — Each entry of a basket's `variants` list: an axis that differs across
the matrix, usually the model or a config flag carried in `env` (merged over the
task's env). A single variant runs and records evidence, but `regress` needs ≥ 2
variants to gate. *(basket.md)*

**vendor substrate** — The off-the-shelf third-party observability tool (Phoenix
recommended, Langfuse the MIT alternative) to which humans delegate live viewing of
runs, fed by that vendor's own Claude Code plugin. Catacomb neither writes to nor
reads from it; the integration is documentation-only. *(ADR-0026)*

**verdict** — The classification the gate emits per finding and overall: `ok`,
`regression`, `notable`, `improvement`, or `insufficient`. Only `regression` (and
`insufficient` under `--strict`, or `notable` under `--fail-on-notable`) fails the
gate; the overall verdict maps to the exit code. *(cli.md; ADR-0022)*

**verify (command)** — Re-runs a basket's verifiers *offline* over already-recorded
evidence, launching no agent, so a verifier can be iterated at zero agent cost
between `bench` and `regress`. Idempotent: it rewrites `verify.json` and
`scores.jsonl`. *(cli.md; workflows.md)*

**verifier contract** — The environment `bench` (inline) and `verify` (offline) hand
a verifier: `CATACOMB_EVIDENCE_DIR`, `CATACOMB_WORKDIR` (empty offline), the cell
coordinates, and `CATACOMB_AGENT_EXIT_CODE`. The verifier reads captured artifacts,
decides pass/fail, and prints one scores-JSONL line. A non-zero verifier *exit* is
an operational failure, not a failing verdict — a failing check is `verifier.pass:
0` at exit 0. Verifiers must be deterministic, offline-re-runnable, and kept out of
the agent's reach. *(workflows.md; cli.md; ADR-0027; PITCH §2)*

**verifier.pass** — The reserved run-level score key (higher-better) that gates by
default with no `--annotation` flag; a candidate whose pass rate drops flags a
`regression`. When every value is 0/1 it is gated as a rate with one-sided Wilson
bounds. *(cli.md; ADR-0027; workflows.md)*

**verify.json** — A verification record in the evidence directory holding the
verifier `cmd`, a hash of cmd+env, exit code, duration, timestamp, and `mode`
(`offline` or `bench`). It records a non-zero verifier exit without touching the
immutable `meta.json`. *(cli.md; workflows.md)*

**version (command)** — Prints catacomb's version string. *(cli.md)*

## W

**WAL** *(historical)* — A write-ahead-log store design ("in-memory + WAL")
*considered and rejected* in favour of embedded SQLite, because it would
re-implement querying and indexing over a log. It appears in the docs only as this
rejected alternative, not as an adopted mechanism. *(ADR-0006)*

**wall-clock-ordered split** — In `calibrate`, sorting the selected runs by evidence
timestamps and splitting into a time-ordered first half vs second half (each ≥
min-support) to expose drift across the recorded sequence. The resolved order is
echoed for auditability. *(ADR-0034; cli.md)*

**Wilcoxon signed-rank test (exact, opt-in)** — `--paired-test wilcoxon`: an opt-in
exact signed-rank test that *replaces* the sign test per metric (so the comparison
family stays flat). It ranks delta magnitudes and derives the exact null as an
RNG-free subset-sum distribution, buying real power on non-unanimous configs where
the sign test needs near-unanimity — at six tasks, one small discordant task gates
where the sign test cannot. *(ADR-0035; cli.md)*

**Wilson score bound** — The closed-form confidence construction catacomb uses for
rate comparison. A rate flags `regression` only when the baseline and candidate
*one-sided* Wilson bounds are disjoint AND the delta exceeds the threshold. Chosen
because CLT-based error bars under-cover at the small sample sizes CI uses (~70%
actual coverage at nominal 95% for N = 10). *(ADR-0022; ADR-0023; PITCH §1)*

**workspace isolation** — See *per-cell workspace isolation*.
