# README-as-Guide Rewrite Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rebuild `README.md` as a FastAPI-style guide (plain-words opening, one-command install, Create it → Run it → Gate it → Upgrade it tutorial with real captured output, methodology citations) and re-anchor `docs/guide/` as the reference layer behind it.

**Architecture:** Docs-only change driven by one live capture session: a tiny reproducible demo basket is run with the real `claude` CLI, its outputs are spliced verbatim into the new README, and the guide's entry pages are rewritten to point at the README tutorial instead of duplicating it. A repo-wide link check closes the loop.

**Tech Stack:** Markdown, `claude` CLI, `catacomb` (built from this worktree), bash, python3 (link checker).

**Spec:** `docs/superpowers/specs/2026-07-13-readme-guide-rewrite-design.md`

## Global Constraints

- Docs only: no Go code changes; `make test` / coverage gate must stay untouched and green.
- Visual identity preserved: lockup `<picture>` block, badge row, centered emoji headers (`## <p align=center>… </p>`), `<hr>` separators.
- The tutorial shows **exactly** what was run: demo file contents and command outputs in the README are byte-copies from the capture directory (home dir replaced with `~`, nothing else rewritten).
- Citations: only sources from the spec's verified mapping get "supports X" framing; Spider 2.0 / ELT-Bench / BIRD appear only in one further-reading line; no invented sources, no Google paper.
- All prose in English. No comments policy applies to Go only; shell/YAML comments in README examples are fine and encouraged.
- `CAPTURE_DIR` = `/private/tmp/claude-501/-Users-karych-src-catacomb/a0f5e7d5-46ca-4bf8-9554-c679562d8946/scratchpad/readme-capture` (any refresh re-run may substitute its own absolute path).
- Worktree: all work happens in `/Users/karych/src/catacomb/.claude/worktrees/readme-guide-rewrite` on branch `worktree-readme-guide-rewrite`.
- Model policy: README/guide prose tasks and reviews run on the session default model; only the link-sweep (Task 4) may run on Opus.

---

### Task 1: Live demo capture

Build catacomb, run the reproducible demo basket with the live `claude` CLI, and save every output the README will splice. **No repo files change in this task.** Deliverable: a populated `$CAPTURE_DIR` and a passing assertion that the gate fired (exit `1`) on a metric regression.

**Files:**
- Create (outside repo): `$CAPTURE_DIR/demo/agent.sh`, `$CAPTURE_DIR/demo/demo.yaml`
- Create (outside repo): `$CAPTURE_DIR/out/bench.txt`, `$CAPTURE_DIR/out/regress.txt`, `$CAPTURE_DIR/out/regress-exit-code.txt`, `$CAPTURE_DIR/out/baseline-set.txt`, `$CAPTURE_DIR/out/regress-record.txt`, `$CAPTURE_DIR/out/trends.txt`

**Interfaces:**
- Consumes: nothing from other tasks.
- Produces: `$CAPTURE_DIR/demo/*` (the exact demo files Task 2 embeds) and `$CAPTURE_DIR/out/*.txt` (the exact outputs Task 2 splices). Task 2 reads these paths verbatim.

- [ ] **Step 1: Build the binary and confirm the live CLI exists**

```bash
cd /Users/karych/src/catacomb/.claude/worktrees/readme-guide-rewrite
make build
./bin/catacomb version
claude --version
```

Expected: `make build` succeeds; both version commands print a version string. If `claude` is missing, STOP and report — the fallback (hermetic stub, synthetic numbers labeled as such) needs an explicit go-ahead from the orchestrator.

- [ ] **Step 2: Write the demo files**

```bash
mkdir -p "$CAPTURE_DIR/demo" "$CAPTURE_DIR/out"
```

Write `$CAPTURE_DIR/demo/agent.sh` with exactly:

```sh
#!/usr/bin/env bash
# agent.sh — the agent under test. bench runs this once per cell.
set -euo pipefail
exec claude -p "${PROMPT}" \
  --model claude-haiku-4-5 \
  --output-format stream-json \
  --verbose \
  --strict-mcp-config \
  --setting-sources project
```

Write `$CAPTURE_DIR/demo/demo.yaml` with exactly:

```yaml
# demo.yaml — the same question, with and without a chain-of-thought
# instruction someone wants to add to the shared prompt.
basket: demo
reps: 5
tasks:
  - id: answer
    cmd: ["./agent.sh"]
    dir: .
variants:
  - id: main
    env:
      PROMPT: "In one short sentence, what is a hash function?"
  - id: candidate
    env:
      PROMPT: "In one short sentence, what is a hash function? Think step by step and write out your full reasoning before you answer."
```

Then:

```bash
chmod +x "$CAPTURE_DIR/demo/agent.sh"
```

- [ ] **Step 3: Dry-run the basket (fail fast on schema errors)**

```bash
cd "$CAPTURE_DIR/demo"
/Users/karych/src/catacomb/.claude/worktrees/readme-guide-rewrite/bin/catacomb bench demo.yaml --dry-run
```

Expected: a 10-row cell expansion table (1 task × 2 variants × 5 reps), exit 0.

- [ ] **Step 4: Run the basket live and capture bench output**

```bash
cd "$CAPTURE_DIR/demo"
export PATH="/Users/karych/src/catacomb/.claude/worktrees/readme-guide-rewrite/bin:$PATH"
catacomb bench demo.yaml --runs-dir runs 2>&1 | tee "$CAPTURE_DIR/out/bench.txt"
```

(The `PATH` export makes the invoked command byte-identical to what the README shows;
re-export it in every fresh shell of this task.)

Expected: 10 cells run sequentially (~2–4 min, well under $1 on haiku); output ends with a `marked 10/10 cells` summary and a copy-pasteable `Next steps:` epilogue containing `catacomb regress --runs-dir runs --baseline label:basket=demo,variant=main --candidate label:basket=demo,variant=candidate`. Exit 0.

- [ ] **Step 5: Run regress and capture the verdict table**

```bash
cd "$CAPTURE_DIR/demo"
catacomb regress --runs-dir runs \
  --baseline label:basket=demo,variant=main \
  --candidate label:basket=demo,variant=candidate \
  2>&1 | tee "$CAPTURE_DIR/out/regress.txt"; echo $? > "$CAPTURE_DIR/out/regress-exit-code.txt"
```

Expected: verdict table with at least one `regression` finding among `tokens_out` / `duration_ms` / `cost_usd` at the run-total scope; overall verdict `regression`; exit code file contains `1`.

- [ ] **Step 6: Contingency if the gate did not fire**

Only if Step 5's exit code is not `1` or no metric `regression` finding appears: replace the candidate `PROMPT` in `demo.yaml` with the e2e-proven seeded wording (verbatim from `e2e/basket-continuous.yaml`): `"Explain what a hash function is in a detailed six-paragraph essay. Write at least six full paragraphs covering definition, properties, collisions, cryptographic vs non-cryptographic uses, common algorithms, and practical pitfalls."` Then delete `runs/` and `demo.yaml.manifest.jsonl`, and repeat Steps 3–5. The README embeds whatever `demo.yaml` finally shipped, so no drift is possible.

- [ ] **Step 7: Capture the Upgrade-section outputs (no agent spend)**

```bash
cd "$CAPTURE_DIR/demo"
catacomb baseline set demo-main --label basket=demo,variant=main \
  --runs-dir runs --db demo.db 2>&1 | tee "$CAPTURE_DIR/out/baseline-set.txt"
catacomb regress --runs-dir runs --db demo.db \
  --baseline name:demo-main \
  --candidate label:basket=demo,variant=candidate \
  --record 2>&1 | tee "$CAPTURE_DIR/out/regress-record.txt" || true
catacomb trends demo-main --db demo.db 2>&1 | tee "$CAPTURE_DIR/out/trends.txt"
```

Expected: `baseline set` reports the pinned run count; the recorded regress reproduces the `regression` verdict; `trends` prints one history row. All three files non-empty.

- [ ] **Step 8: Anonymize home paths in captured outputs**

```bash
cd "$CAPTURE_DIR/out"
python3 - <<'EOF'
import os, pathlib
home = os.path.expanduser("~")
for p in pathlib.Path(".").glob("*.txt"):
    t = p.read_text()
    if home in t:
        p.write_text(t.replace(home, "~"))
print("anonymized")
EOF
grep -rL "$HOME" . && echo CLEAN
```

Expected: `CLEAN`; no other rewriting of the outputs (tables stay byte-exact otherwise).

- [ ] **Step 9: Report**

Return to the orchestrator: the final candidate PROMPT used, run wall-time, total cost (sum of `cost_usd` from `runs/*/meta.json`), the overall verdict line from `regress.txt`, and the list of files in `$CAPTURE_DIR/out/`.

---

### Task 2: Rewrite README.md

Replace `README.md` wholesale with the guide-shaped version below, splicing the Task 1 captures. The full target text is given here; the only insertions are the five `[SPLICE: …]` markers.

**Files:**
- Modify: `README.md` (full replacement)

**Interfaces:**
- Consumes: `$CAPTURE_DIR/demo/agent.sh`, `$CAPTURE_DIR/demo/demo.yaml` (embedded byte-exact into the two "Create it" code blocks), `$CAPTURE_DIR/out/{bench,regress,baseline-set,regress-record,trends}.txt` (spliced byte-exact into the five output blocks).
- Produces: the new `README.md` with anchors `#-tutorial`, `#-methodology`, `#-features` that Task 3's guide pages link to.

- [ ] **Step 1: Write the new README**

Write `README.md` with exactly the following content. Rules for the splice markers:
- `[SPLICE FILE: <path>]` → replace the marker line with the byte-exact content of that file (it sits inside a fenced code block already).
- For output splices, trim only *repeated* per-cell progress lines in `bench.txt` to the first two and last two cells with a literal `…` line between, if the block exceeds ~25 lines. Never trim `regress.txt`'s verdict table.
- `bench.txt` is spliced as a literal `…` line followed by only the tail — from the `marked N/N cells` line through the `Next steps:` epilogue. The leading raw stream-json passthrough is elided, and the README prose must disclose the elision.
- `regress.txt` is spliced without its leading stderr `warning: transcript … newer than tested …` lines (the verdict table itself is never trimmed), and the README prose must disclose that such warnings may appear.
- After splicing, `grep -n "SPLICE" README.md` must return nothing.
- After splicing, adjust the one prose sentence under the regress splice ("made every
  run slower and several times more expensive") to match the captured magnitudes —
  never overstate what the table shows.

````markdown
<p align="center">
  <a href="https://github.com/realkarych/catacomb">
    <picture>
      <source media="(prefers-color-scheme: dark)" srcset="docs/assets/catacomb-lockup-dark.svg">
      <img alt="Catacomb" src="docs/assets/catacomb-lockup-light.svg" width="360">
    </picture>
  </a>
</p>

<p align="center">
  Regression testing for
  <a href="https://www.anthropic.com/claude-code">Claude Code</a> agents.<br>
  Change a prompt, a skill, or an MCP tool — then let statistics,<br>
  not vibes, tell you whether your agent got worse.
</p>

<!-- Badges -->
<p align="center">
  <a href="https://github.com/realkarych/catacomb/actions/workflows/ci.yml"><img alt="CI status" src="https://github.com/realkarych/catacomb/actions/workflows/ci.yml/badge.svg"></a>&nbsp;<!--
  --><a href="https://app.codecov.io/gh/realkarych/catacomb"><img alt="coverage" src="https://codecov.io/gh/realkarych/catacomb/branch/master/graph/badge.svg"></a>&nbsp;<!--
  --><a href="https://go.dev"><img alt="go version" src="https://img.shields.io/github/go-mod/go-version/realkarych/catacomb"></a>&nbsp;<!--
  --><a href="https://github.com/realkarych/catacomb/blob/master/LICENSE"><img alt="license Apache-2.0" src="https://img.shields.io/github/license/realkarych/catacomb"></a>&nbsp;<!--
  --><img alt="platforms" src="https://img.shields.io/badge/platform-linux%20%7C%20macOS%20%7C%20windows-blue">
</p>

<hr>

You spent weeks tuning your agent — prompts, skills, MCP tools, CLAUDE.md. Now every
change to that setup is a gamble: agents are nondeterministic, so one good run after a
prompt tweak proves nothing, and one bad run proves nothing either. Catacomb settles
the question with statistics. `catacomb bench` runs the same tasks repeatedly under
the old setup and the new one, recording each run locally as secret-redacted evidence;
`catacomb regress` compares the two groups and maps the verdict to an exit code, so CI
blocks the regression before it merges. No daemon, no service, no network — the whole
loop is plain local files.

## <p align=center>✨ Features</p>

- **A gate, not a vibe check.** Repeated runs per variant, compared with statistics built for small samples ([why this matters](#-methodology)); the verdict maps to the exit code your CI already understands.
- **Plain local files.** Evidence directories plus an optional SQLite store for baselines — nothing listens, nothing phones home.
- **Evidence you can share.** Recorded transcripts pass through best-effort secret redaction before they ever touch disk ([what is and isn't caught](docs/guide/privacy-and-operations.md)).
- **Comparisons survive prompt rewrites.** The agent can name phases of its own run (checkpoints), giving `regress` a stable axis even when prompt churn re-keys every step ([concepts](docs/guide/concepts.md#phases-and-checkpoints)).
- **Longitudinal memory.** Pin golden groups as named baselines; every recorded comparison accumulates into a history that `trends` replays ([workflows](docs/guide/workflows.md#watching-drift-over-time)).
- **Checks the answer, not just the path.** Declare a per-task verifier and its pass/fail verdict rides the same statistical gate ([verifying task outcomes](docs/guide/workflows.md#verifying-task-outcomes)).

<hr>

## <p align=center>🧰 Requirements</p>

[Claude Code](https://www.anthropic.com/claude-code) installed and `claude` on your
PATH. Catacomb itself is a single static binary — no runtime, no dependencies, no
config file.

<hr>

## <p align=center>📦 Installation</p>

```sh
brew tap realkarych/tap
brew trust realkarych/tap      # newer Homebrew requires trusting third-party taps
brew install --cask catacomb
```

<details>
<summary><b>Docker</b></summary>

**Package:** <https://github.com/realkarych/catacomb/pkgs/container/catacomb>.

```sh
docker run --rm ghcr.io/realkarych/catacomb:latest version
```

</details>

<details>
<summary><b>Debian / Ubuntu (APT)</b></summary>

```sh
# Import the signing key
curl -fsSL https://realkarych.github.io/catacomb-apt/public.key \
  | sudo tee /etc/apt/trusted.gpg.d/catacomb.asc

# Add the repository
echo "deb [arch=$(dpkg --print-architecture)] \
  https://realkarych.github.io/catacomb-apt stable main" \
  | sudo tee /etc/apt/sources.list.d/catacomb.list

# Install / update
sudo apt update
sudo apt install catacomb
```

</details>

<details>
<summary><b>Go install / from source (Go ≥ 1.26)</b></summary>

```sh
go install github.com/realkarych/catacomb/cmd/catacomb@latest
# make sure $GOBIN (default ~/go/bin) is on your PATH
```

Or clone the repo and `make build`.

</details>

<details>
<summary><b>Windows / other distros</b></summary>

Download the pre-built archive from the
**[Releases](https://github.com/realkarych/catacomb/releases)** page, unpack it, and
add the binary to your `PATH`. On Windows, you may need `Unblock-File .\catacomb.exe`
before first run.

</details>

> Upgrading from Homebrew: `brew upgrade --cask catacomb`. Migrating from the old
> formula: `brew uninstall catacomb && brew install --cask catacomb`.

<hr>

## <p align=center>🚀 Tutorial</p>

Ten minutes, two small files, one caught regression. The scenario: someone on your
team wants to add a chain-of-thought instruction to a shared prompt, and you want CI
to tell you what that does to the agent's behavior — before it merges.

### 1. Create it

Make an empty folder with two files. `agent.sh` is the agent command under test —
any command works as long as it emits stream-json, which is how catacomb finds the
session transcript:

```sh
[SPLICE FILE: $CAPTURE_DIR/demo/agent.sh]
```

(The last two flags keep your user-scope plugins and hooks out of the benchmark, so
runs are comparable across machines.)

`demo.yaml` declares the experiment — a *basket*: the matrix of tasks × variants ×
reps. Each combination is one cell, run as a plain local process:

```yaml
[SPLICE FILE: $CAPTURE_DIR/demo/demo.yaml]
```

```sh
chmod +x agent.sh
```

### 2. Run it

```sh
catacomb bench demo.yaml --runs-dir runs
```

Ten cells run sequentially — 2 variants × 5 reps, a couple of minutes and a few cents
on haiku:

```text
[SPLICE OUTPUT: $CAPTURE_DIR/out/bench.txt]
```

What just happened:

- each cell ran `agent.sh` as a plain local process and catacomb resolved its
  transcripts from `~/.claude/projects`;
- each run was written to `runs/<run-id>/` as a **secret-redacted** evidence
  directory — transcripts plus a `meta.json` with labels, exit code, and cost;
- the manifest (`demo.yaml.manifest.jsonl`) records every cell, so an interrupted
  basket resumes with `--resume`.

### 3. Gate it

Compare the candidate group against the baseline group — the epilogue above already
printed this command for you:

```sh
catacomb regress --runs-dir runs \
  --baseline label:basket=demo,variant=main \
  --candidate label:basket=demo,variant=candidate
```

```text
[SPLICE OUTPUT: $CAPTURE_DIR/out/regress.txt]
```

The chain-of-thought instruction made every run slower and several times more
expensive — the candidate's medians left the baseline's noise band, so the overall
verdict is `regression` and the exit code is `1`. That exit code is the gate:

```sh
catacomb regress … && echo "safe to merge"
# 0 = ok · 1 = regression · 2 = operational error
```

Two runs of an agent are two samples from a distribution — a single side-by-side diff
cannot tell a real regression from sampling noise, which is why every comparison here
is group-vs-group ([methodology](#-methodology)).

### 4. Upgrade it

**Pin a baseline and keep history.** Label selectors churn; names don't. Pin the
golden group once, then `--record` every CI comparison and replay the drift later:

```sh
catacomb baseline set demo-main --label basket=demo,variant=main \
  --runs-dir runs --db demo.db
catacomb regress --runs-dir runs --db demo.db \
  --baseline name:demo-main \
  --candidate label:basket=demo,variant=candidate --record
catacomb trends demo-main --db demo.db
```

```text
[SPLICE OUTPUT: $CAPTURE_DIR/out/trends.txt]
```

**Let the agent mark its own phases.** Prompt rewrites re-key steps, which degrades
step-level alignment. Checkpoints survive that: give the agent the shipped MCP marker
tool, tell it when to mark, and declare what you expect:

```json
{"mcpServers":{"catacomb":{"command":"catacomb","args":["mcp"]}}}
```

Pass that file via `claude --mcp-config`, add one line to your CLAUDE.md — e.g. *"call
`mcp__catacomb__mark` with `name: plan` before planning and after tests pass"* — and
declare `checkpoints: [plan, tests.pass]` on the task. Declared phases become stable
comparison rows in the `regress` table, robust to prompt churn
([concepts](docs/guide/concepts.md#phases-and-checkpoints)).

**Verify the answer itself.** Deterministic observables catch behavioral drift; whether
the agent produced the *right answer* is task-specific. Declare the files a task
produces and a command that scores them:

```yaml
tasks:
  - id: sql
    cmd: ["./agent.sh"]
    artifacts: ["out/result.csv"]
    verify:
      cmd: ["python3", "./verify_sql.py"]
      env: { GOLDEN: "/fixtures/golden.csv" }   # ground truth, OUTSIDE the workdir
```

The verifier reads the captured artifacts and emits one pass/fail line — two calls
with the shipped [Python SDK](integrations/verifier):

```python
from catacomb_verifier import Cell, emit, compare_tables

cell = Cell.from_env()
res = compare_tables(cell.artifact("out/result.csv"), os.environ["GOLDEN"], ordered=False)
emit(passed=res.equal, tool="verify_sql", tool_version="1")
```

`bench` runs it after every cell, and the resulting `verifier.pass` rate gates through
`regress` **by default** — same Wilson bounds as everything else
([verifying task outcomes](docs/guide/workflows.md#verifying-task-outcomes)).

### Recap

You now have: a declarative basket, redacted local evidence for every run, a
statistical gate wired to CI exit codes, a named baseline accumulating history, and
two quality axes (checkpoints, verifier) that survive prompt rewrites. Recipes for
all of it — sensitivity at small k, the paired sign test, external scores, the
accuracy-vs-cost Pareto table — live in the
[workflows guide](docs/guide/workflows.md).

<hr>

## <p align=center>🧠 How it works</p>

Catacomb reduces each session's transcripts (the main session plus every subagent
sub-transcript) into one deterministic execution graph:

```text
session
  └─ user_prompt
       └─ assistant_turn
            ├─ tool_call
            ├─ mcp_call
            └─ subagent
                 └─ user_prompt
                      └─ ...
```

Cross-run identity rides two keys: a **step key** hashes each call's redacted, salient
input, so "the same logical step" aligns across runs even though every node ID
differs; a **phase key** names checkpoint windows, so comparisons survive even when
prompt churn re-keys the steps. Groups of runs are then aggregated and compared per
[ADR-0022](docs/adr/0022-regression-detection-over-repeated-runs.md): Wilson bounds
for rates, IQR noise bands for metrics, an exact sign test for paired per-task drift.
The full pipeline — bench runner, redacted capture, reducer, keys, gate, baselines,
history — is implemented end-to-end under a 100%-test-coverage TDD gate; catacomb
deliberately ships no live viewer
([ADR-0026](docs/adr/0026-form-factor-pivot-offline-eval-gate.md)). The deeper story
is in [concepts](docs/guide/concepts.md).

<hr>

## <p align=center>🔬 Methodology</p>

The gate's design follows the published eval literature, not house heuristics:

- **Repeated, isolated trials; tasks from real failures; outcome-over-path scoring** — Anthropic, [Demystifying evals for AI agents](https://www.anthropic.com/engineering/demystifying-evals) (2026).
- **Group comparison with error bars; paired designs as free variance reduction** — Anthropic, [A statistical approach to model evals](https://www.anthropic.com/research/statistical-approach-to-model-evals); Miller, [Adding Error Bars to Evals](https://arxiv.org/abs/2411.00640).
- **Wilson bounds and the exact sign test are the right small-n tools** — naive CLT-based intervals undercover below a few hundred datapoints: Bowyer et al., [Don't use the CLT for LLM evals](https://arxiv.org/abs/2503.01747) (ICML 2025).
- **pass^k reporting and deterministic final-state verification** — [τ-bench](https://arxiv.org/abs/2406.12045) (ICLR 2025), which catacomb's [reliability block](docs/guide/cli.md#task-reliability-passk) and verifier model follow.
- **The harness and its transcripts are a first-class reliability concern** — transcript inspection catches shortcuts that pass outcome verifiers: [Holistic Agent Leaderboard](https://arxiv.org/abs/2510.11977) (ICLR 2026).
- **Verifiers must themselves be validated** — weak tests unfairly rejected valid solutions on 61.1% of flagged SWE-bench tasks ([SWE-bench Verified](https://openai.com/index/introducing-swe-bench-verified/)), and vetting reduces but does not eliminate the bias ([SWE-Bench+](https://arxiv.org/abs/2410.06992)). Hence catacomb's verifier contract keeps comparators total, offline-re-runnable, and out of the agent's reach.
- **LLM judges do not replace deterministic checks** — even expert-designed graders trail human inter-rater agreement ([GDPval](https://arxiv.org/abs/2510.04374)); judge protocol discipline per [OpenAI's evaluation best practices](https://platform.openai.com/docs/guides/evals). Catacomb gates on deterministic observables and lets external scorers ride the same mechanism instead of baking a judge in.

Further reading on domain benchmarks: [Spider 2.0](https://arxiv.org/abs/2411.07763),
[ELT-Bench](https://arxiv.org/abs/2504.04808), the
[BIRD family](https://bird-bench.github.io/).

<hr>

## <p align=center>🔒 Privacy</p>

Catacomb runs no daemon and opens no sockets — everything is local files. Evidence
transcripts pass through secret redaction on the write path (API keys, tokens, private
keys, connection strings, high-entropy values → typed markers). It is best-effort, not
a guarantee — the classes it deliberately does not catch are listed under
[known residuals](docs/guide/privacy-and-operations.md#known-residuals). Graphs and
step keys hash only post-redaction content; the SQLite store holds baselines and
reports, never transcripts or payloads.

<hr>

## <p align=center>📚 Documentation & Development</p>

Reading order:

1. This README — install, tutorial, the mental model
2. [Concepts](docs/guide/concepts.md) — the action graph, step keys, phases
3. [Workflows](docs/guide/workflows.md) — recipes: baselines, trends, verifiers, external scores, diff, export
4. [CLI reference](docs/guide/cli.md) — every command, flag, and exit code
5. [Configuration](docs/guide/configuration.md) · [Ingestion](docs/guide/ingestion.md) · [Privacy & operations](docs/guide/privacy-and-operations.md)

Design depth: [design spec](docs/specs/2026-06-20-catacomb-design.md) ·
[ADRs](docs/adr/) · [implementation plans](docs/plans/) ·
[release process](docs/RELEASING.md) · [contributor & agent guide](AGENTS.md).

```sh
make build   # build bin/catacomb
make test    # tests with -race + coverage profile
make cover   # enforce the 100% coverage gate
make lint    # golangci-lint
```

<hr>

## <p align=center>🙏 Contribution</p>

- **Found a bug?** [Open an issue](https://github.com/realkarych/catacomb/issues/new) with reproduction steps and expected vs. actual behavior.
- **Have a question?** Ping [`@karych`](https://t.me/karych) on Telegram, or open an issue.
- **Want a feature?** Open an issue describing the use case.
- **Ready to contribute code?** Read [AGENTS.md](AGENTS.md) first — the repo runs under a 100%-test-coverage, TDD-first gate. Fork, branch, PR (tag `@realkarych`).

Your feedback and contributions are always welcome 💙.

<hr>

## <p align=center>⚖️ License</p>

[Apache-2.0](LICENSE).
````

- [ ] **Step 2: Splice and verify markers are gone**

Apply the splice rules above, then:

```bash
grep -n "SPLICE" README.md
```

Expected: no output.

- [ ] **Step 3: Verify citation URLs**

For each external URL in the Methodology section, check it resolves:

```bash
for u in \
  https://www.anthropic.com/engineering/demystifying-evals \
  https://www.anthropic.com/research/statistical-approach-to-model-evals \
  https://arxiv.org/abs/2411.00640 \
  https://arxiv.org/abs/2503.01747 \
  https://arxiv.org/abs/2406.12045 \
  https://arxiv.org/abs/2510.11977 \
  https://openai.com/index/introducing-swe-bench-verified/ \
  https://arxiv.org/abs/2410.06992 \
  https://arxiv.org/abs/2510.04374 \
  https://platform.openai.com/docs/guides/evals \
  https://arxiv.org/abs/2411.07763 \
  https://arxiv.org/abs/2504.04808 \
  https://bird-bench.github.io/ ; do
  code=$(curl -s -o /dev/null -w "%{http_code}" -L --max-time 15 "$u")
  echo "$code $u"
done
```

Expected: every line `200` (or `3xx` followed by `200` via `-L`). A `403`/bot-block
from a vendor site is not a failure by itself — verify that URL with WebFetch instead;
a `404` means find the correct canonical URL for that exact title (the two Anthropic
posts and the OpenAI best-practices page are the ones most likely to need a corrected
slug) and fix the link in README.md. arXiv links must point at `/abs/` pages.

- [ ] **Step 4: Link-check the README (relative links + anchors)**

Write `$CAPTURE_DIR/linkcheck.py` with exactly:

```python
import re, sys, pathlib

repo = pathlib.Path(sys.argv[1]).resolve()
files = [pathlib.Path(p) for p in sys.argv[2:]]

def slugify(text):
    text = re.sub(r"<[^>]+>", "", text)
    text = re.sub(r"[`*_]", "", text).strip().lower()
    text = re.sub(r"[^\w\s-]", "", text, flags=re.UNICODE)
    return re.sub(r"\s+", "-", text)

def anchors(md_path):
    out = set()
    for line in md_path.read_text().splitlines():
        m = re.match(r"^#{1,6}\s+(.*)$", line)
        if m:
            out.add(slugify(m.group(1)))
        for name in re.findall(r'<a\s+(?:name|id)="([^"]+)"', line):
            out.add(name)
    return out

errors = []
for f in files:
    text = f.read_text()
    text = re.sub(r"```.*?```", "", text, flags=re.S)
    links = re.findall(r"\[[^\]]*\]\(([^)\s]+)\)", text)
    links += re.findall(r'href="([^"]+)"', text)
    for link in links:
        if re.match(r"^(https?:|mailto:|#$)", link):
            continue
        if link.startswith("#"):
            if link[1:] not in anchors(f):
                errors.append(f"{f}: broken intra-doc anchor {link}")
            continue
        target, _, frag = link.partition("#")
        tpath = (f.parent / target).resolve()
        if not tpath.exists():
            errors.append(f"{f}: missing target {link}")
            continue
        if frag and tpath.suffix == ".md" and frag not in anchors(tpath):
            errors.append(f"{f}: missing anchor #{frag} in {target}")

print("\n".join(errors) if errors else "OK")
sys.exit(1 if errors else 0)
```

Run it:

```bash
cd /Users/karych/src/catacomb/.claude/worktrees/readme-guide-rewrite
python3 "$CAPTURE_DIR/linkcheck.py" . README.md
```

Expected: `OK`. Fix any reported link before committing. (Known slug rule: the
centered emoji headers slug to a leading hyphen — `## <p align=center>🔬 Methodology</p>`
→ `#-methodology`; the script reproduces this because the emoji is stripped and the
leading space becomes a hyphen.)

- [ ] **Step 5: Sanity-check embedded commands against the capture**

Every command shown in the Tutorial must literally appear in the Task 1 procedure
(same flags, same selectors): compare by eye the six `sh` blocks in the Tutorial
against Task 1 Steps 4, 5, and 7. The `catacomb regress …` teaser in "Gate it" with
the `&& echo` suffix is presentation-only and exempt.

- [ ] **Step 6: Commit**

```bash
git add README.md
git commit -m "docs: rebuild README as a guided tutorial with captured output

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 3: Restructure the guide entry pages

Rewrite `docs/guide/README.md` as a reading map and `docs/guide/getting-started.md` as
a thin pointer to the README tutorial. No other guide file's content changes.

**Files:**
- Modify: `docs/guide/README.md` (full replacement)
- Modify: `docs/guide/getting-started.md` (full replacement)

**Interfaces:**
- Consumes: the README anchors produced by Task 2 (`#-tutorial`).
- Produces: nothing later tasks depend on beyond valid links.

- [ ] **Step 1: Replace docs/guide/README.md**

Write exactly:

```markdown
# Catacomb user guide

Start with the [README tutorial](../../README.md#-tutorial) — it takes you from
install to a working CI gate, with real command output at every step. This guide is
the depth behind it.

Reading order:

1. [Concepts](concepts.md) — the action graph, step keys, and phases: the vocabulary
   every other page uses
2. [Workflows](workflows.md) — task recipes: benching, gating, baselines, trends,
   verifiers, external scores, diff, and export
3. [CLI reference](cli.md) — every command, flag, and exit code
4. [Configuration](configuration.md) — flags, environment variables, and defaults
5. [Ingestion](ingestion.md) — how transcripts become graphs
6. [Privacy and operations](privacy-and-operations.md) — redaction, evidence dirs,
   and troubleshooting

([Getting started](getting-started.md) is kept as a pointer for old links.)
```

- [ ] **Step 2: Replace docs/guide/getting-started.md**

Write exactly:

```markdown
# Getting started

The getting-started walkthrough lives in the repository
[README tutorial](../../README.md#-tutorial): install, first basket, first gate,
baselines, checkpoints, and the verifier — with real command output at every step.

After the tutorial:

- [Concepts](concepts.md) — the action graph, step keys, and phases
- [Workflows](workflows.md) — baselines, recorded history, trends, verifiers, and
  external scores
- [CLI reference](cli.md) — all commands and flags
- [Privacy and operations](privacy-and-operations.md) — what is redacted, where, and
  troubleshooting
```

- [ ] **Step 3: Link-check the guide**

Using the same `$CAPTURE_DIR/linkcheck.py` from Task 2 (write it again verbatim if the
file is gone):

```bash
cd /Users/karych/src/catacomb/.claude/worktrees/readme-guide-rewrite
python3 "$CAPTURE_DIR/linkcheck.py" . docs/guide/*.md
```

Expected: `OK`.

- [ ] **Step 4: Commit**

```bash
git add docs/guide/README.md docs/guide/getting-started.md
git commit -m "docs(guide): re-anchor guide as reference depth behind the README tutorial

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 4: Repo-wide link sweep

Verify every Markdown file in the repo still links cleanly after the restructure; fix
what broke. Mechanical task — may run on Opus.

**Files:**
- Modify: any `.md` file with a link broken by Tasks 2–3 (expected: none or few; the
  only known inbound link to a rewritten section was `docs/guide/README.md` →
  `getting-started.md`, already handled in Task 3)

**Interfaces:**
- Consumes: `$CAPTURE_DIR/linkcheck.py` (write it again verbatim from Task 2 Step 4 if absent).
- Produces: a clean repo-wide link report.

- [ ] **Step 1: Run the checker over every tracked Markdown file**

```bash
cd /Users/karych/src/catacomb/.claude/worktrees/readme-guide-rewrite
git ls-files '*.md' | xargs python3 "$CAPTURE_DIR/linkcheck.py" .
```

Expected: `OK`, or a finite error list.

- [ ] **Step 2: Triage and fix**

Fix only breakage caused by this rewrite (links into `README.md` or the two rewritten
guide pages). Pre-existing broken links unrelated to the rewrite: do not fix silently —
list them in the task report for the orchestrator to decide.

- [ ] **Step 3: Re-run to green, commit if anything changed**

```bash
git ls-files '*.md' | xargs python3 "$CAPTURE_DIR/linkcheck.py" .
git diff --quiet || { git add -A '*.md' && git commit -m "docs: fix links broken by the README/guide restructure

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"; }
```

Expected: `OK` from the checker; commit only when fixes were needed.

---

### Task 5: Final verification

Confirm the branch is merge-ready: tests untouched and green, links green, README
internally consistent.

**Files:** none modified.

**Interfaces:**
- Consumes: everything above.
- Produces: a verification report for the orchestrator.

- [ ] **Step 1: Full test suite (must be untouched by this branch)**

```bash
cd /Users/karych/src/catacomb/.claude/worktrees/readme-guide-rewrite
git diff master --stat -- '*.go' | wc -l   # expected: 0
make test
```

Expected: zero Go changes; `make test` passes.

- [ ] **Step 2: Repo-wide link check, final**

```bash
git ls-files '*.md' | xargs python3 "$CAPTURE_DIR/linkcheck.py" .
```

Expected: `OK`.

- [ ] **Step 3: README consistency assertions**

```bash
grep -c "SPLICE" README.md                    # expected: 0
grep -n "catacomb --help" README.md           # expected: no output (help dump removed)
grep -n "✨ Features" README.md               # expected: one hit
grep -n "🔬 Methodology" README.md            # expected: one hit
grep -n "$HOME" README.md                     # expected: no output
```

- [ ] **Step 4: Report**

Report to the orchestrator: test result, link-check result, README line count, and
any pre-existing broken links found in Task 4 Step 2. The orchestrator then does the
visual render check (Artifact preview) and opens the PR.
