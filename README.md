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
  Change a prompt, a skill, or an MCP tool — then see whether<br>
  <b>cost, latency, or correctness</b> regressed, from statistics<br>
  over real transcripts, not vibes.
</p>

<!-- Badges -->
<p align="center">
  <a href="https://github.com/realkarych/catacomb/actions/workflows/ci.yml"><img alt="CI status" src="https://github.com/realkarych/catacomb/actions/workflows/ci.yml/badge.svg"></a>&nbsp;<!--
  --><a href="https://github.com/realkarych/catacomb/releases"><img alt="release" src="https://img.shields.io/github/v/release/realkarych/catacomb"></a>&nbsp;<!--
  --><a href="https://app.codecov.io/gh/realkarych/catacomb"><img alt="coverage" src="https://codecov.io/gh/realkarych/catacomb/branch/master/graph/badge.svg"></a>&nbsp;<!--
  --><a href="https://go.dev"><img alt="go version" src="https://img.shields.io/github/go-mod/go-version/realkarych/catacomb"></a>&nbsp;<!--
  --><a href="https://github.com/realkarych/catacomb/blob/master/LICENSE"><img alt="license Apache-2.0" src="https://img.shields.io/github/license/realkarych/catacomb"></a>&nbsp;<!--
  --><img alt="platforms" src="https://img.shields.io/badge/platform-linux%20%7C%20macOS%20%7C%20windows-blue">
</p>

<hr>

You spent weeks tuning your agent — prompts, skills, MCP tools, CLAUDE.md — and now
every change is a gamble: one good run after a prompt tweak proves nothing, and one
bad run proves nothing either. Catacomb settles the question with statistics.
`catacomb bench` runs the same tasks repeatedly under the old setup and the new one,
recording each run locally as secret-redacted evidence; `catacomb regress` compares the
two groups and maps the verdict to an exit code, so CI blocks the regression before it
merges. No daemon, no service, no network — the whole loop is plain local files.

It fits any Claude Code agent whose behavior you need to keep stable — coding agents,
data/ETL pipelines (SQL, transforms), research and tool-use loops — anywhere a
nondeterministic session needs a CI verdict instead of a vibe check.

> **Requires [Claude Code](https://www.anthropic.com/claude-code)** installed with `claude` on your PATH — catacomb evaluates Claude Code agent transcripts (and OpenAI Codex CLI sessions — see [runtimes](docs/guide/ingestion.md#runtimes)).

## <p align=center>✨ Features</p>

- **A gate, not a vibe check.** Repeated runs per variant, compared with statistics built for small samples ([why this matters](#-methodology)); the verdict maps to the exit code your CI already understands.
- **Plain local files.** Evidence directories plus an optional SQLite store for baselines — nothing listens, nothing phones home.
- **Evidence you can share.** Recorded transcripts pass through best-effort secret redaction before they ever touch disk ([what is and isn't caught](docs/guide/privacy-and-operations.md)).
- **Comparisons survive prompt rewrites.** The agent can name phases of its own run (checkpoints), giving `regress` a stable axis even when prompt churn re-keys every step ([concepts](docs/guide/concepts.md#phases-and-checkpoints)).
- **Longitudinal memory.** Pin golden groups as named baselines; every recorded comparison accumulates into a history that `trends` replays ([workflows](docs/guide/workflows.md#watching-drift-over-time)) — and, stamped with `--project`, joins across a fleet of repos in your own warehouse ([roll up a fleet](docs/guide/workflows.md#roll-up-a-fleet)).
- **Checks the answer, not just the path.** Declare a per-task verifier and its pass/fail verdict rides the same statistical gate ([verifying task outcomes](docs/guide/workflows.md#verifying-task-outcomes)).
- **A gate that audits itself.** When it cannot fire it says so (the `sensitivity:` disclosure), and `catacomb calibrate` A/A-splits one variant's own recorded runs to show whether your thresholds would flag environmental drift alone — before you trust a red verdict ([self-check your gate](docs/guide/workflows.md#self-check-your-gate)).
- **Gate PRs from CI.** A bundled composite GitHub Action ([`catacomb-gate`](.github/actions/catacomb-gate)) installs a pinned release, runs the gate, and posts the verdict as a sticky PR comment ([recipe](docs/guide/workflows.md#gate-a-pr-with-the-action)) — it lives in this repo for now; marketplace extraction is a follow-up.
- **Gates OpenAI Codex CLI sessions too.** Declare `runtime: codex` in the basket and `catacomb bench` drives `codex exec` cells — or `catacomb import` ingests rollouts you recorded yourself — subagents, checkpoints, and token metrics included, through the same evidence and gate ([runtimes](docs/guide/ingestion.md#runtimes)).
- **Gate memory scales with graph structure, not transcript size.** `regress` strips payloads before aggregation; the measured envelope and `make bench` live in [operations](docs/guide/privacy-and-operations.md#scale).
- **Drive it from your agent.** A bundled [Claude Code skill](skills/catacomb/SKILL.md) teaches your agent to scaffold a basket, wire the CI gate, and read a `regress` verdict for you — just ask it to set up catacomb.

<hr>

## <p align=center>🧭 Where catacomb fits</p>

Catacomb overlaps with familiar eval tooling but occupies a different niche — reach for
it when the thing you're guarding is a *Claude Code agent* and the artifact you need is
a CI verdict:

- **promptfoo / DeepEval** evaluate prompts and RAG outputs against assertions. Catacomb scores whole *agent sessions* from real Claude Code transcripts — the action graph, not just the final string.
- **LangSmith / Braintrust** are hosted platforms with dashboards and accounts. Catacomb is plain local files and an exit code — no daemon, no service, no network.
- **Inspect** is a research framework for building evals. Catacomb is a purpose-built regression *gate*: repeated runs, small-sample statistics, and a CI exit code, specialized for Claude Code agents.

<hr>

## <p align=center>🎯 What you get</p>

You change a prompt; catacomb runs both versions and turns the difference into a CI
verdict. Here a chain-of-thought tweak made the agent slower and more expensive, so the
candidate metrics cross their noise bands and the gate returns non-zero:

```text
$ catacomb regress --runs-dir runs \
    --baseline label:basket=demo,variant=main \
    --candidate label:basket=demo,variant=candidate
baseline runs 5  candidate runs 5
coverage steps 1.00  phases 1.00  steps_trusted true  overall regression
VERDICT     SCOPE  METRIC       BASELINE  CANDIDATE  BAND
regression  total  cost_usd     0.00      0.01       [0.00, 0.00]
regression  total  duration_ms  5059.00   7165.00    [3755.50, 6362.50]
regression  total  tokens_out   147.00    465.00     [91.50, 202.50]
$ echo $?
1
```

<p align="center"><sub>A real <code>catacomb regress</code> verdict from the <a href="#-tutorial">tutorial</a> below (rows and columns abbreviated; the full report is in the tutorial) — the candidate got slower and more expensive, so CI fails (exit 1).</sub></p>

<hr>

## <p align=center>🧰 Requirements</p>

[Claude Code](https://www.anthropic.com/claude-code) installed and `claude` on your
PATH, and **signed in** — a Claude subscription, or `ANTHROPIC_API_KEY` set for API
billing. `catacomb bench` spends real money through Claude Code, so verify it works
first with `claude -p hello`. The exception is gating
[Codex CLI sessions](docs/guide/ingestion.md#runtimes): bench-driving a
`runtime: codex` basket needs the OpenAI Codex CLI (`codex`) installed and signed in
instead of `claude`, and `catacomb import` reads already-recorded rollout files, so it
needs neither. Catacomb itself is a single static binary — no runtime, no
dependencies, no config file.

<hr>

## <p align=center>📦 Installation</p>

```sh
brew tap realkarych/tap
brew trust realkarych/tap      # newer Homebrew requires trusting third-party taps
brew install --cask catacomb
```

> **Which channel serves what.** `go install …@latest` (Go ≥ 1.26) installs the tagged
> release the moment it lands and is the surest way to get exactly the version these
> docs describe; brew, apt, and docker converge within minutes. Always confirm with
> `catacomb version` after installing — if it looks old right after `brew install`, run
> `brew update` and reinstall. A build with no `bench`/`regress` command is a stale
> pre-pivot install; see the migration note below.

<details>
<summary><b>Docker</b></summary>

**Package:** <https://github.com/realkarych/catacomb/pkgs/container/catacomb>.

```sh
docker run --rm ghcr.io/realkarych/catacomb:latest version
```

The image is for CI and `catacomb version`; running the tutorial in docker also needs
`claude` and your `~/.claude/projects` mounted in.

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
before first run. The Windows binary is smoke-tested end-to-end in CI: a
`windows-latest` job builds `catacomb.exe` and drives a real
`bench → verify → regress` loop on every PR — see
[platform support](docs/guide/troubleshooting.md#platform-support).

</details>

> Upgrading from Homebrew: `brew upgrade --cask catacomb`. Migrating from the old
> formula (a pre-pivot build whose command set differs — no `bench`/`regress`):
> `brew uninstall catacomb && brew install --cask catacomb`.

<hr>

## <p align=center>🚀 Tutorial</p>

Ten minutes, two small files, one caught regression. The scenario: someone on your
team wants to add a chain-of-thought instruction to a shared prompt, and you want CI
to tell you what that does to the agent's behavior — before it merges.

> First, confirm your install is current: `catacomb version` should print **v0.2.0 or
> newer** — this tutorial uses the latest commands and flags. A build missing `bench` or
> `regress` entirely is a stale pre-pivot install (see the migration note above).

### 1. Create it

Make an empty folder with two files. `agent.sh` is the agent command under test —
any command works as long as it emits stream-json, which is how catacomb finds the
session transcript:

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

(The last two flags keep your user-scope plugins and hooks out of the benchmark, so
runs are comparable across machines.)

> **Prefer the interactive TUI?** You don't have to script `claude -p`. Run the task by
> hand, then ingest the finished session with
> [`catacomb import`](docs/guide/cli.md#import) — it shapes an interactive transcript into
> the same evidence a bench cell produces, so `verify` and `regress` work on it unchanged.

`demo.yaml` declares the experiment — a *basket*: the matrix of tasks × variants ×
reps. Each combination is one cell, run as a plain local process:

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

```sh
chmod +x agent.sh
```

### 2. Run it

`catacomb bench` calls the Anthropic API through Claude Code and spends real money — a
few cents on haiku for this demo.

```sh
catacomb bench demo.yaml --runs-dir runs
```

Ten cells run sequentially — 2 variants × 5 reps, about a minute and a few cents on
haiku. Each cell streams the agent's raw stream-json output to your terminal; the
block below shows only the tail:

```text
…
marked 10/10 cells
Next steps:
  catacomb regress --runs-dir runs --baseline label:basket=demo,variant=main --candidate label:basket=demo,variant=candidate
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

(If your Claude Code is newer than the versions catacomb has been tested against, a
few harmless `warning: transcript … newer than tested …` lines may precede the
table — the [tested-version watchlist](docs/guide/ingestion.md) tracks which builds
are known-good.)

The run-total block is the headline — each metric compared against its own noise
band (the `BAND` column):

```text
baseline runs 5  candidate runs 5
coverage steps 1.00  phases 1.00  steps_trusted true  overall regression
VERDICT       SCOPE   KEY                               NAME         METRIC       BASELINE  CANDIDATE  BAND                DETAIL
regression    total   -                                 -            cost_usd     0.00      0.01       [0.00, 0.00]        -
regression    total   -                                 -            duration_ms  5059.00   7165.00    [3755.50, 6362.50]  -
ok            total   -                                 -            error_rate   0.00      0.00       [0.00, 0.35]        -
ok            total   -                                 -            nodes        4.00      4.00       [3.00, 5.00]        -
ok            total   -                                 -            tokens_in    10.00     10.00      [7.50, 12.50]       -
regression    total   -                                 -            tokens_out   147.00    465.00     [91.50, 202.50]     -
```

The chain-of-thought instruction made every run roughly 3× chattier (median
`tokens_out` 147 → 465) and about 40% slower (median duration 5.1 s → 7.2 s), and
even at haiku pennies the cost crossed its noise band — the overall verdict is
`regression` and the exit code is `1`. That exit code is the gate:

```sh
catacomb regress --runs-dir runs \
  --baseline label:basket=demo,variant=main \
  --candidate label:basket=demo,variant=candidate \
  && echo "safe to merge"
# 0 = ok · 1 = regression · 2 = operational error
```

Two runs of an agent are two samples from a distribution — a single side-by-side diff
cannot tell a real regression from sampling noise, which is why every comparison here
is group-vs-group ([methodology](#-methodology)).

**Reading the rest of the report.** Below the run-total headline, the same command
prints two finer axes and an audit trail:

- **`paired`** rows run an exact per-task sign test, which needs at least five tasks.
  This demo has one, so they come back `insufficient`, and the `sensitivity:` line
  notes the paired gate cannot fire at this support — the run-total metrics are what
  fired the gate.
- **`phase`** rows compare declared checkpoint windows (here the single `task:answer`
  phase), pinning a regression to where in the run it happened.
- **`audit:`** lines flag an individual run whose metric sits outside the group band —
  provenance for a number, not a separate verdict.

Here is the complete report those three notes describe:

```text
baseline runs 5  candidate runs 5
coverage steps 1.00  phases 1.00  steps_trusted true  overall regression
sensitivity: gate cannot fire at this support (paired gate needs k>=5 tasks)
VERDICT       SCOPE   KEY                               NAME         METRIC       BASELINE  CANDIDATE  BAND                DETAIL
regression    total   -                                 -            cost_usd     0.00      0.01       [0.00, 0.00]        -
regression    total   -                                 -            duration_ms  5059.00   7165.00    [3755.50, 6362.50]  -
ok            total   -                                 -            error_rate   0.00      0.00       [0.00, 0.35]        -
ok            total   -                                 -            nodes        4.00      4.00       [3.00, 5.00]        -
ok            total   -                                 -            tokens_in    10.00     10.00      [7.50, 12.50]       -
regression    total   -                                 -            tokens_out   147.00    465.00     [91.50, 202.50]     -
insufficient  paired  -                                 -            cost_usd     0.00      0.00       -                   matched 1 task below paired min 5
insufficient  paired  -                                 -            duration_ms  0.00      0.00       -                   matched 1 task below paired min 5
insufficient  paired  -                                 -            tokens_in    0.00      0.00       -                   matched 1 task below paired min 5
insufficient  paired  -                                 -            tokens_out   0.00      0.00       -                   matched 1 task below paired min 5
regression    phase   eb1d7eb24fc38d7838cf7b81664c90e6  task:answer  cost_usd     0.00      0.01       [0.00, 0.00]        -
regression    phase   eb1d7eb24fc38d7838cf7b81664c90e6  task:answer  duration_ms  5059.00   7165.00    [3755.50, 6362.50]  -
regression    phase   eb1d7eb24fc38d7838cf7b81664c90e6  task:answer  tokens_out   147.00    465.00     [91.50, 202.50]     -
audit: baseline run bench-demo-answer-main-r1 (task answer) cost_usd 0.010954549999999999 vs group median 0.0033167 (band 0.00165835)
```

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
SEQ  CREATED               CANDIDATE                            VERDICT     REGRESSIONS  INSUFFICIENT  DURATION_MS  COST_USD  ERROR_RATE
1    2026-07-13T10:59:37Z  label:basket=demo,variant=candidate  regression  6            4             7165.00      0.01      0.00
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

> Install the SDK (PyPI publish pending): `pip install "catacomb-verifier @ git+https://github.com/realkarych/catacomb#subdirectory=integrations/verifier"`

```python
import os

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

- **Repeated, isolated trials; tasks from real failures; outcome-over-path scoring** — Anthropic, [Demystifying evals for AI agents](https://www.anthropic.com/engineering/demystifying-evals-for-ai-agents) (2026).
- **Group comparison with error bars; paired designs as free variance reduction** — Anthropic, [A statistical approach to model evals](https://www.anthropic.com/research/statistical-approach-to-model-evals); Miller, [Adding Error Bars to Evals](https://arxiv.org/abs/2411.00640).
- **Wilson bounds and the exact sign test are the right small-n tools** — naive CLT-based intervals undercover below a few hundred datapoints: Bowyer et al., [Don't use the CLT for LLM evals](https://arxiv.org/abs/2503.01747) (ICML 2025).
- **pass^k reporting and deterministic final-state verification** — [τ-bench](https://arxiv.org/abs/2406.12045) (ICLR 2025), which catacomb's [reliability block](docs/guide/cli.md#task-reliability-passk) and verifier model follow.
- **The harness and its transcripts are a first-class reliability concern** — transcript inspection catches shortcuts that pass outcome verifiers: [Holistic Agent Leaderboard](https://arxiv.org/abs/2510.11977) (ICLR 2026).
- **Verifiers must themselves be validated** — annotators flagged 61.1% of sampled SWE-bench tasks for unit tests that may unfairly reject valid solutions ([SWE-bench Verified](https://openai.com/index/introducing-swe-bench-verified/)), and vetting reduces but does not eliminate the bias ([SWE-Bench+](https://arxiv.org/abs/2410.06992)). Hence catacomb's verifier contract keeps comparators total, offline-re-runnable, and out of the agent's reach.
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

Full docs live under **[docs/](docs/README.md)**. Suggested reading order:

1. This README — install, [tutorial](#-tutorial), the mental model
2. [Concepts](docs/guide/concepts.md) — the action graph, step keys, phases
3. [Workflows](docs/guide/workflows.md) — recipes: baselines, trends, verifiers, external scores, diff, export
4. [CLI reference](docs/guide/cli.md) — every command, flag, and exit code
5. [Configuration](docs/guide/configuration.md) · [Ingestion](docs/guide/ingestion.md) · [Privacy & operations](docs/guide/privacy-and-operations.md)

Reference: [guide index](docs/guide/README.md) · [basket schema](docs/guide/basket.md) ·
[troubleshooting](docs/guide/troubleshooting.md) · [Claude Code skill](skills/catacomb/SKILL.md) ·
[ADR log](docs/adr/README.md) · [release process](docs/RELEASING.md) ·
[contributor & agent guide](AGENTS.md).

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
