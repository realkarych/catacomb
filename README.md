<p align="center">
  <a href="https://github.com/realkarych/catacomb">
    <picture>
      <source media="(prefers-color-scheme: dark)" srcset="docs/assets/catacomb-lockup-dark.svg">
      <img alt="Catacomb" src="docs/assets/catacomb-lockup-light.svg" width="360">
    </picture>
  </a>
</p>

<p align="center">
  Offline eval gate for
  <a href="https://www.anthropic.com/claude-code">Claude Code</a> agentic pipelines.<br>
  Run prompt baskets, reduce the recorded transcripts into one canonical
  <b>action graph</b>, and gate regressions statistically in CI.
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

Changing a prompt, a skill, or an MCP tool changes your agent's behavior — but two runs
of the same task are two samples from a distribution, so eyeballing one diff cannot
tell a real regression from sampling noise. Catacomb closes that gap offline:
`catacomb bench` runs a declarative basket of tasks × variants × reps, records each
cell's Claude Code transcripts (including every subagent's sub-transcript) as a
secret-redacted evidence directory, and `catacomb regress` reduces both groups to
canonical execution graphs — prompts, turns, tool calls, MCP calls, subagents — aligns
them by step and phase keys, and returns a statistical verdict with a CI-consumable
exit code. No daemon, no service, no network: the whole loop is plain local files.

It is domain- and evaluation-agnostic: it compares deterministic observables (presence,
errors, duration, cost, tokens) and leaves a per-node annotation slot so external
scorers (such as the shipped DeepEval bridge) can gate on quality through the same
mechanism.

## <p align=center>✨ Highlights</p>

- **A gate, not a vibe check.** `regress` compares repeated runs with one-sided Wilson bounds and IQR noise bands (ADR-0022), reports `regression`/`notable`/`insufficient` per finding, and maps the verdict to the exit code your CI already understands.
- **An outline, not a hairball.** Each run reduces to a collapsible tree — `session → prompt → turn → tool` — with each subagent nested under the turn that spawned it, rebuilt deterministically from the transcripts.
- **Stable identity across runs.** Step keys hash each call's redacted, salient input; phase keys name checkpoint windows the agent marks via the shipped `catacomb mcp` marker tool — so comparisons survive prompt churn.
- **Redacted evidence you can share.** Transcript copies pass through secret redaction on write (ADR-0024); step keys and payload hashes are computed post-redaction, so no artifact catacomb writes encodes a raw secret.
- **Longitudinal memory.** Pin golden groups as named, version-stamped baselines; `--record` appends every comparison to an append-only history that `trends` replays.
- **Bring your own viewer.** Catacomb ships no UI: watching runs live is delegated to a vendor substrate fed by that vendor's first-party Claude Code plugin — Phoenix is the recommended one ([ADR-0026](docs/adr/0026-form-factor-pivot-offline-eval-gate.md)). Catacomb stays the offline capture, diff, and regression-gate layer.

> **Status:** the offline gate is implemented end-to-end — the bench runner, redacted evidence capture, the reconciling reducer, step/phase keys, the statistical gate, named baselines with version stamps, recorded history (`trends`), external score gating, and the DeepEval bridge — built and maintained under a 100%-test-coverage, TDD gate. The daemon, live ingestion, and exporters were removed per [ADR-0026](docs/adr/0026-form-factor-pivot-offline-eval-gate.md).

<hr>

## <p align=center>📦 Installation</p>

### Homebrew (macOS)

```sh
brew tap realkarych/tap
brew trust realkarych/tap   # newer Homebrew requires trusting third-party taps
brew install catacomb       # first install
brew upgrade catacomb       # later updates
```

### Docker images

**Package:** <https://github.com/realkarych/catacomb/pkgs/container/catacomb>.

```sh
docker run --rm ghcr.io/realkarych/catacomb:latest version
```

### Debian / Ubuntu (APT)

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

### Other distros / Windows

Download the pre-built archive from the
**[Releases](https://github.com/realkarych/catacomb/releases)** page, unpack it,
and add the binary to your `PATH`.

> On Windows, you may need `Unblock-File .\catacomb.exe` before first run.

### Go install (Go ≥ 1.26)

```sh
go install github.com/realkarych/catacomb/cmd/catacomb@latest
# make sure $GOBIN (default ~/go/bin) is on your PATH
```

Or build locally with `make build`.

<hr>

### ✅ Once installed, verify it works

```
❯ catacomb --help
Catacomb is an offline eval gate for Claude Code agentic pipelines. It runs
prompt baskets, reduces the recorded transcripts into a canonical execution
graph, derives step and phase keys, aggregates metrics, and gates regressions
against saved baselines.

Common recipes:
  Run a basket and record evidence:
      catacomb bench <basket.yaml>

  Gate a candidate against a baseline:
      catacomb regress --baseline label:variant=main --candidate label:variant=pr

  Build a graph from a single recorded transcript:
      catacomb replay <session>.jsonl

Run 'catacomb <command> --help' for details on any command.

Usage:
  catacomb [command]

Available Commands:
  baseline    Manage named baselines for regression comparison
  bench       Run a benchmark basket: expand cells, execute, mark phases, record a manifest
  completion  Generate the autocompletion script for the specified shell
  diff        Diff two session transcripts by step_key
  export      Export a transcript or evidence dir as a JSONL graph snapshot
  help        Help about any command
  mcp         Run the catacomb MCP stdio server (exposes the mark checkpoint tool)
  regress     Compare a candidate run group against a baseline
  replay      Build a graph from a recorded Claude Code transcript
  subgraph    Extract the execution subgraph of a checkpoint phase
  trends      Show the recorded regression history for a baseline
  version     Print the version
```

<hr>

## <p align=center>🚀 Quickstart</p>

Declare a basket — the matrix of tasks × variants × reps you want to compare:

```yaml
# checkout.yaml
basket: checkout
reps: 5
tasks:
  - id: work-task
    cmd: ["claude", "-p", "work the checkout task", "--output-format", "stream-json"]
variants:
  - id: main
  - id: candidate
    setup: ["git checkout candidate-branch"]
```

Run it:

```sh
catacomb bench checkout.yaml
```

Every cell runs as a plain local process; catacomb resolves its transcripts from
`~/.claude/projects`, verifies declared checkpoints, and writes a secret-redacted
evidence directory under `~/.catacomb/runs/<run-id>/` plus a manifest line.

Gate the candidate against the baseline:

```sh
catacomb regress \
  --baseline label:basket=checkout,variant=main \
  --candidate label:basket=checkout,variant=candidate
```

Exit code `0` is a pass, `1` is a regression, `2` is an operational error — drop it
straight into CI. Then go deeper:

```sh
catacomb baseline set golden --label basket=checkout,variant=main   # pin the golden group
catacomb regress --baseline name:golden \
  --candidate label:basket=checkout,variant=candidate --record      # gate + record history
catacomb trends golden                                              # replay the drift
catacomb diff run-a.jsonl run-b.jsonl                               # step-level diff
catacomb export ~/.catacomb/runs/<run-id> --out run.jsonl           # JSONL graph snapshot
```

See the [user guide](docs/guide/README.md) for the full loop — checkpoints, external
quality scores, and the DeepEval bridge.

<hr>

## <p align=center>🔒 Privacy</p>

Catacomb runs no daemon and opens no sockets — everything is local files. The
transcript copies it stores as evidence pass through secret redaction on the write path
(ADR-0024): API keys, tokens, private keys, connection strings, and high-entropy
values are replaced with typed markers before they touch disk. Graphs carry a content
*hash* per node, computed after redaction; step keys likewise hash only redacted
content. The SQLite store holds baselines and regression reports — never transcripts or
payloads.

<hr>

## <p align=center>📚 Documentation & Development</p>

- User guide → [`docs/guide/`](docs/guide/README.md)
- Design spec → [`docs/specs/2026-06-20-catacomb-design.md`](docs/specs/2026-06-20-catacomb-design.md)
- Architecture decisions (ADRs) → [`docs/adr/`](docs/adr/)
- Implementation plans → [`docs/plans/`](docs/plans/)
- Release process → [`docs/RELEASING.md`](docs/RELEASING.md)
- Contributor & agent guide → [`AGENTS.md`](AGENTS.md)

```sh
make build   # build bin/catacomb
make test    # tests with -race + coverage profile
make cover   # enforce the 100% coverage gate
make lint    # golangci-lint
```

<hr>

## <p align=center>🙏 Contribution</p>

### Found a bug?

- Please [open an issue](https://github.com/realkarych/catacomb/issues/new) with a clear description, reproduction steps (if possible), and expected vs. actual behavior.

### Have a question?

- Ping me on Telegram: [`@karych`](https://t.me/karych), or [open an issue](https://github.com/realkarych/catacomb/issues/new).

### Want to suggest a feature?

- [Open an issue](https://github.com/realkarych/catacomb/issues/new) describing the use case and the behavior you'd expect.

### Ready to contribute code?

- Read the [contributor & agent guide](AGENTS.md) first — the repo runs under a 100%-test-coverage, TDD-first gate.
- Fork the repo, create a branch, and open a pull request when ready (tag `@realkarych` for review).

Your feedback and contributions are always welcome 💙.

<hr>

## <p align=center>⚖️ License</p>

[Apache-2.0](LICENSE).
