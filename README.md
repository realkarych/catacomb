<p align="center">
  <a href="https://github.com/realkarych/catacomb">
    <picture>
      <source media="(prefers-color-scheme: dark)" srcset="docs/assets/catacomb-lockup-dark.svg">
      <img alt="Catacomb" src="docs/assets/catacomb-lockup-light.svg" width="360">
    </picture>
  </a>
</p>

<p align="center">
  Real-time execution-graph observability for
  <a href="https://www.anthropic.com/claude-code">Claude Code</a> agentic sessions.<br>
  Prompts, turns, tool calls, MCP calls, and subagents — reconciled into one
  queryable <b>action graph</b>, live in a web UI and a terminal observer.
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

Catacomb runs as a sidecar daemon next to Claude Code and captures everything a
session does — prompts, assistant turns, tool calls, MCP calls, and subagents —
from four signal sources: hooks, native OpenTelemetry, `stream-json`, and
transcript JSONL (including each subagent's sub-transcript). It reconciles them
into one canonical **action graph**, persists it to embedded SQLite, serves it
live over SSE and gRPC, and renders it in an embedded web UI and a terminal
observer. The same graph exports as a materialized artifact to `jsonl`,
OTLP/OpenInference, `neo4j`, and `postgres`.

It is domain- and evaluation-agnostic: it builds a faithful, queryable graph and
leaves a per-node annotation slot for downstream tooling to attach its own
metadata.

## <p align=center>✨ Highlights</p>

- **An outline, not a hairball.** The web UI is a virtualized, collapsible tree — `session → prompt → turn → tool` — that stays readable at thousands of nodes. (An earlier force-directed graph view was removed after it proved unusable on real sessions.)
- **Subagents you can actually inspect.** Each subagent nests under the turn that spawned it (`turn → Agent tool call → subagent → its prompt/turns/tools`), labelled with its task. A subagent's inner work is lazy-loaded on expand, so a session with hundreds of subagents still loads fast.
- **Content inspection, gated and redaction-aware.** Conversation text and tool input/output are redacted before they ever reach disk (write-path redaction, ADR-0024) and served only through an authorization-gated endpoint (off by default) with serve-time redaction as defense in depth — never inlined into graph responses.
- **Terminal observer.** `catacomb observe` is a full TUI over the same live feed (sessions → tree → node detail).
- **Silence when healthy.** Status is surfaced only when it carries signal (failures, live activity); a calm session stays calm.

> **Status:** all designed surfaces are implemented — four-source ingestion (incl. subagent sub-transcripts), the reconciling reducer, SQLite persistence, live SSE + gRPC, the embedded web UI and the terminal observer, and the four exporters. Built and maintained under a 100%-test-coverage, TDD gate.

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
Catacomb builds a real-time execution graph of your Claude Code sessions —
prompts, turns, tool calls, MCP calls, and subagents — and serves it in a
web UI and a terminal observer.

Common recipes:
  Observe every session (all projects):
      catacomb up --global

  Load past sessions into the UI:
      catacomb up --history

  Read conversation content in the UI (off by default):
      catacomb daemon --allow-payload-access

Run 'catacomb <command> --help' for details on any command.

Usage:
  catacomb [command]

Observe:
  down          Stop the daemon and optionally remove catacomb's artifacts
  logs          Print the daemon log (use -f to follow)
  observe       Interactive terminal observer for a Claude session
  restart       Stop the running daemon and start a fresh one
  status        Print daemon addr, pid, uptime, and session/node counts
  ui            Open the catacomb web UI in the default browser
  up            Start the daemon (if needed), install hooks, and open the UI
  watch         Stream live graph deltas from the catacomb daemon (SSE)

Setup:
  daemon        Run the catacomb daemon (receives hook events, builds the live graph)
  env           Print OTLP environment variables for connecting to the running daemon
  install-hooks Wire the catacomb hook forwarder into Claude Code settings.json

Advanced:
  demo          Ingest a bundled synthetic transcript into the running daemon
  diff          Diff two session transcripts by step_key
  export        Export graph data to an external sink (jsonl, otlp, neo4j, postgres, agentevals, evalview)
  hook          Forward a Claude Code hook event to the catacomb daemon
  ingest        Forward Claude Code output to the catacomb daemon
  inspect       Show detailed summary for a specific run
  mark          Record a phase boundary marker in a running session
  replay        Build a graph from a recorded Claude Code transcript
  run           Run a Claude Code command, tee its stream-json to the terminal and the daemon
  runs          List all runs in the stored catacomb database
  snapshot      Dump current graph state as JSONL
  subgraph      Extract the execution subgraph of a checkpoint phase
  version       Print the version
```

<hr>

## <p align=center>🚀 Quickstart</p>

```sh
catacomb up
```

`catacomb up` starts the daemon if it is not already running, installs the
Claude Code hooks for the **current directory**, prints the bearer URL, and
opens the web UI. It observes **live** sessions started under that directory.

### Observe every session

To observe sessions in **every** project (not just the current directory),
install the hooks globally:

```sh
catacomb up --global
```

This writes `~/.claude/settings.json`, so any Claude Code session — from any
directory — is observed.

### Load past sessions

`up` and the hooks only see sessions that run *after* they are installed. To
backfill the sessions you have **already** run, start the daemon tailing the
Claude Code transcript directory:

```sh
catacomb up --history          # tails ~/.claude/projects when starting the daemon
```

On startup the daemon reads every existing transcript (sessions and their
subagents) and then follows live ones. Tail cursors are persisted, so
re-running the daemon does not duplicate history. If a daemon is already
running, `up --history` prints the exact command to restart it with history
enabled rather than restarting it for you.

Combine both for full coverage:

```sh
catacomb up --global --history
```

Other commands:

```sh
catacomb status           # daemon addr, pid, uptime, what it's observing, counts
catacomb observe [hash]   # interactive terminal observer
catacomb ui               # print the bearer URL and (re-)open the browser
catacomb demo             # ingest the bundled demo transcript into a running daemon
catacomb version          # print the version
```

By default the daemon's database is `catacomb.db` in the directory you launch
it from, and its discovery file lives under `~/.catacomb/run/`.

<hr>

## <p align=center>🔒 Privacy</p>

Catacomb observes your sessions locally. Graph responses hold structure, timing,
token/cost metadata, and a content *hash* — conversation text is never inlined
into them. Payload bodies pass through secret redaction on the write path
(ADR-0024) before they touch the database, so what sits in `catacomb.db` is
already redacted. Message and tool content is served only when the daemon is
started with `--allow-payload-access`, through a token-gated endpoint that
redacts once more at serve time. The HTTP surface binds to loopback and is gated
by a bearer token printed at startup.

<hr>

## <p align=center>📚 Documentation & Development</p>

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
