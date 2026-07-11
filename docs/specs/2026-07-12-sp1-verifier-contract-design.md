# SP1 — Verifier contract: design

- **Date:** 2026-07-12
- **Status:** approved design (brainstormed section-by-section, user-validated)
- **Related:** [ADR-0027](../adr/0027-verification-layer-and-reliability-metrics.md) (decision + non-goals), [ADR-0022](../adr/0022-regression-detection-over-repeated-runs.md) (annotations contract), [ADR-0023](../adr/0023-regression-gate-sensitivity-at-small-k.md) (rate gate), [gap review](../reviews/2026-07-12-eval-best-practices-gap-review.md)

Catacomb gains a task-success axis: baskets declare an executable verifier per task; bench (and an offline `verify` verb) run it against each cell's evidence; its verdict enters the model as run-level annotations and is gated by the existing statistics. Semantics stay in the user's subprocess — the core neither judges nor interprets, it executes and aggregates.

## 1. Contract

### Basket declaration

```yaml
tasks:
  - id: sql-report
    cmd: ["./agent.sh"]
    dir: ./work
    checkpoints: [verify]
    artifacts: ["out/*.csv"]            # globs relative to dir
    verify:
      cmd: ["python3", "verify_sql.py"] # argv, no shell — same rule as task cmd
      env: { GOLDEN: golden.csv }       # layered over the variant env
      timeout: 120s                     # default 60s
```

Both fields are optional; a task without `verify:` behaves exactly as today.

### Execution

After a cell completes and its evidence dir is written, bench runs `verify.cmd` in the cell's workdir. Environment: variant env + `verify.env` + contract variables:

| Variable | Value |
|---|---|
| `CATACOMB_EVIDENCE_DIR` | the cell's evidence dir (transcripts, meta.json, artifacts) |
| `CATACOMB_WORKDIR` | the cell's working directory (bench mode only; empty offline) |
| `CATACOMB_RUN_ID`, `CATACOMB_BASKET`, `CATACOMB_TASK`, `CATACOMB_VARIANT`, `CATACOMB_REP` | cell coordinates |
| `CATACOMB_AGENT_EXIT_CODE` | the agent child's exit code |

### Output

stdout is scores JSONL (the ADR-0016/0022 format) extended with a run-level dialect: a line **without** `step_key` annotates the whole run; `run_id` defaults to the current cell. Convention: `{"key":"verifier.pass","value":1}` (0/1) plus any continuous scores (`verifier.row_diff`, `judge.groundedness`, …). bench writes the collected lines to `<evidence>/scores.jsonl`. stderr passes through to the operator.

Optional provenance fields on any scores line — `tool`, `tool_version`, `prompt_hash` — recorded verbatim into annotations metadata; this is the judge-provenance slice of ADR-0027 SP5, landed here so the schema is extended once.

### Exit code is not the verdict

A failed task is `verifier.pass: 0` with exit 0. A non-zero exit is an **operational failure** of the verifier itself: no scores are applied, the failure is recorded (see `verify.json`, §3), and downstream it surfaces as a missing verdict — the ADR-0022 best-effort-with-recorded-visibility philosophy; the gate never silently swallows tooling failures.

### Artifacts

Offline re-verification of stateful tasks is impossible without captured outputs (the result set lives in an ephemeral workdir). `artifacts:` globs are copied at cell end into `<evidence>/artifacts/`: text files through the existing redaction policy (ADR-0020/0024 discipline unchanged), sizes capped (per-file and total; overflow recorded in meta.json, not fatal), list + sha256 recorded in meta.json. Binary artifacts are copied as-is under the cap — a documented residual risk owned by the basket author.

## 2. Run-level annotations in aggregate/regress

- **Schema.** Today a scores line requires `step_key` (step-level only, per the ADR-0022 amendment). A line without it is a run-level annotation. Existing files remain valid; keys keep the `owner.key` grammar.
- **Aggregation (Totals axis).** Run-level values form a per-group distribution alongside run totals. Values that are all in {0,1} across both groups aggregate through the **rate machinery**: one-sided Wilson (z=1.645), disjointness AND delta (new `AnnotationRateDelta`, default 0.1, matching `ErrorRateDelta`), `MinSupport`, sensitivity disclosure — all existing code paths. Any other numbers aggregate through the **metric machinery** (median ± max(rel-delta, IQR band)).
- **Gating.** The reserved key `verifier.pass` gates by default as a higher-better rate (its drop is a regression) so the common case works flagless. All other keys stay opt-in via `--annotation owner.key:higher-better|lower-better`.
- **Score discovery.** `regress --runs-dir` selectors already scan meta.json; when a cell's evidence dir contains `scores.jsonl`, it is applied automatically. `--scores <file>` remains for external/extra scores and **overrides** same-key entries from evidence (explicit wins), one warning per override.
- **Reporting.** Run-level rows appear in the Totals block (`annotations.verifier.pass: rate 5/5 → 2/5, regression`) in human and `--json` output. Step-level behavior is unchanged.
- **Boundary.** pass^k and paired tests are SP2; SP1 only guarantees the binary outcome exists in the model and is Wilson-gated.

## 3. Offline verb: `catacomb verify`

`catacomb verify <basket.yaml> --runs-dir <dir> [label: filters]` — for each recorded cell matched by task id from meta.json, runs the basket's `verify.cmd`. The basket file is the source of truth for the verifier command, so verifiers can evolve after runs are recorded and iterate at zero agent cost.

**Two modes, one contract.** In bench mode the verifier sees both a hot `CATACOMB_WORKDIR` and `CATACOMB_EVIDENCE_DIR`; offline, `CATACOMB_WORKDIR` is empty and the cwd is the evidence dir. The authoring rule (docs + SDK default): a verifier that claims re-verifiability reads only from evidence (artifacts, transcripts, meta).

**`verify.json`.** The verification record lives in `<evidence>/verify.json` (cmd, sha256 of cmd+env, exit code, duration, timestamp, mode `bench|offline`) — not in meta.json, which stays the immutable bench execution ledger. Each re-verification rewrites `verify.json` and `scores.jsonl` idempotently: the previous verdict is reproducible by re-running.

**Exit codes** (regress convention): `0` — all matched cells verified (verdict contents are regress's business), `1` — one or more operational verifier failures (recorded in verify.json), `2` — usage/IO error.

Full post-SP1 cycle:

```sh
catacomb bench basket.yaml --runs-dir runs    # agents + inline verification
catacomb verify basket.yaml --runs-dir runs   # iterate on verifiers, zero agent cost
catacomb regress --runs-dir runs --baseline label:variant=a --candidate label:variant=b
```

## 4. Python mini-SDK (`integrations/verifier`)

Package `catacomb-verifier`, sibling of the DeepEval bridge; **stdlib-only** (csv/json/math — no pandas: no CI dependency weight, no supply-chain surface). Installed from the repo (path/git); PyPI deferred.

```python
from catacomb_verifier import Cell, emit, compare_tables

cell = Cell.from_env()                        # parses CATACOMB_*; .artifact(path) reads
                                              # from evidence, falls back to workdir in bench mode
res = compare_tables(
    cell.artifact("out/result.csv"), "golden.csv",
    float_tol=1e-4, ordered=False)            # defaults: strict (extra rows/cols fail),
                                              # unordered, tolerance 1e-4, header normalization
emit(passed=res.equal)                        # -> {"key":"verifier.pass","value":1}
emit(key="verifier.row_diff", value=res.row_diff)
```

Comparator rules follow the benchmark canon (Text2Analysis / DABStep / Databricks strictness): numeric tolerance instead of exact floats, row-order insensitivity unless `ordered=True`, header normalization (case/underscores), type coercion, **strict by default** (extra rows/columns fail — the execution-accuracy false-positive lesson). Formats: CSV and JSONL. Returns a structured diff (counts + first N mismatches, printed to stderr for humans).

Discipline: own pytest suite in the DeepEval-bridge CI workflow; contract fixtures (env + evidence layout) shared with the Go E2E harness so the core and the SDK are tested against **one** contract, not two retellings of it.

## 5. E2E verification and CI

### Tier 1 — hermetic per-PR E2E (`e2e/hermetic/`)

Self-asserting driver in the `e2e/run.sh` style; zero network, zero spend:

- **Scripted agent** `sql-agent.sh` emulates `claude -p --output-format stream-json`: prints stream-json (session id, terminal `result` with cost), writes a realistic transcript JSONL under a temp `$HOME/.claude/projects/<munged>/`, and **actually solves the task**: runs sqlite3 over a fixture DB with a variant-controlled query (baseline — correct SQL; degraded — dropped filter → wrong result set), writes `out/result.csv`.
- **Verifier** `verify_sql.py` uses the SDK: `compare_tables` against golden.csv → `verifier.pass`, `verifier.row_diff`.
- **Driver asserts exit codes and JSON fields, never golden bytes,** through the built binary:
  1. `bench` with inline verification → artifacts in evidence, sha256 in meta.json, scores.jsonl present;
  2. offline `verify` re-run → idempotence: same verdicts without a workdir (proves the re-verify contract);
  3. `regress` degraded → **exit 1** with a regression on `annotations.verifier.pass`; A-vs-A → **exit 0**, zero regressions;
  4. negative paths: verifier with non-zero exit → verify.json records the failure and the gate does not stay silent; `--scores` override → warning;
  5. `verify` verb exit codes: 1 with operational failures, 0 clean.
- CI: required job on ubuntu (sqlite3 + python3 preinstalled). Not in the 3-OS matrix — sqlite3 is not guaranteed on Windows runners (documented).

### Tier 2 — live weekly (extends `e2e/run.sh`)

A third basket, `sql`: real `claude -p` solves the same sqlite task (prompt carries the schema and requires saving the result set via sqlite3), same `verify_sql.py`, variants baseline / degraded-instruction / baseline2. Assertions: degraded gates on `verifier.pass`, A-vs-A does not. Adds ~$0.5 to the ~$1 weekly spend.

### Role of the tiers

Unit tests and 100% coverage remain the law of the repo (TDD); SP1's **acceptance** is E2E: every contract aspect (inline verification, offline re-verify, artifacts, failures, gating) has a hermetic assertion exercising the real binary and real subprocesses, and the live path is confirmed weekly. Contract fixtures are shared with the SDK's pytest suite (§4).

## 6. Boundaries, risks, deliverables

**SP1 deliberately does not include:** judge runners or LLM calls; connectors (YQL/YTsaurus/Logos verifiers are user code; the SDK only lowers the authoring bar); a Go comparator subcommand (YAGNI until a non-Python consumer appears); pass^k (SP2); Pareto (SP3); store changes (scores live in evidence dirs; everything is additive; existing scores files stay valid).

**Honest limits and risks:**

- **Gaming.** An agent can peek at goldens in the workdir (the SWE-Lancer isolation lesson). SP1's measure is documented hygiene — goldens/references outside the workdir, paths via `verify.env`; the E2E fixtures demonstrate the correct layout. Anomaly detection is SP4.
- **Artifacts and secrets.** Redaction applies to text artifacts; binaries are copied as-is under the size cap — recorded as the basket author's responsibility; transcript redaction is unchanged.
- **Offline re-verification of stateful tasks.** A verifier depending on live state (a torn-down warehouse) will not reproduce offline — the contract and SDK deliberately push toward verification-through-artifacts; verify.json records the mode so divergence is visible.
- **Small k.** The pass rate inherits the PV-6a Wilson floors (k=3 gates only full flips); the sensitivity disclosure extends to the new axis automatically.
- **SDK comparator scope.** CSV/JSONL of small/medium size; Parquet and large tables are out — authors go to their data-platform APIs directly.

**Deliverables:** this spec; ADR-0027; the gap review; then a written implementation plan executed subagent-driven (TDD, 100% coverage, no comments), with the hermetic E2E job and the live-basket extension as the acceptance gate.
