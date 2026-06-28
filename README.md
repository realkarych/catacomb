# Catacomb

Real-time execution-graph observability for [Claude Code](https://www.anthropic.com/claude-code) agentic sessions.

Catacomb runs as a sidecar daemon next to Claude Code and captures everything a session does — prompts, assistant turns, tool calls, MCP calls, and subagents — from four signal sources: hooks, native OpenTelemetry, `stream-json`, and transcript JSONL (including each subagent's sub-transcript). It reconciles them into one canonical **action graph**, persists it to embedded SQLite, serves it live over SSE and gRPC, and renders it in an embedded web UI and a terminal observer. The same graph exports as a materialized artifact to `jsonl`, OTLP/OpenInference, `neo4j`, and `postgres`.

It is domain- and evaluation-agnostic: it builds a faithful, queryable graph and leaves a per-node annotation slot for downstream tooling to attach its own metadata.

## Highlights

- **An outline, not a hairball.** The web UI is a virtualized, collapsible tree — `session → prompt → turn → tool` — that stays readable at thousands of nodes. (An earlier force-directed graph view was removed after it proved unusable on real sessions.)
- **Subagents you can actually inspect.** Each subagent nests under the turn that spawned it (`turn → Agent tool call → subagent → its prompt/turns/tools`), labelled with its task. A subagent's inner work is lazy-loaded on expand, so a session with hundreds of subagents still loads fast.
- **Content inspection, gated and redaction-aware.** Conversation text and tool input/output are served only through an authorization-gated endpoint (off by default) with serve-time secret redaction — never inlined into the graph.
- **Terminal observer.** `catacomb observe` is a full TUI over the same live feed (sessions → tree → node detail).
- **Silence when healthy.** Status is surfaced only when it carries signal (failures, live activity); a calm session stays calm.

> **Status:** all designed surfaces are implemented — four-source ingestion (incl. subagent sub-transcripts), the reconciling reducer, SQLite persistence, live SSE + gRPC, the embedded web UI and the terminal observer, and the four exporters. Built and maintained under a 100%-test-coverage, TDD gate. Not yet tagged for release.

## Documentation

- Design spec → [`docs/specs/2026-06-20-catacomb-design.md`](docs/specs/2026-06-20-catacomb-design.md)
- Architecture decisions (ADRs) → [`docs/adr/`](docs/adr/)
- Implementation plans → [`docs/plans/`](docs/plans/)
- Contributor & agent guide → [`AGENTS.md`](AGENTS.md)

## Quickstart

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

To read conversation content in the UI, start the daemon with
`--allow-payload-access` (off by default — see [Privacy](#privacy)).

By default the daemon's database is `catacomb.db` in the directory you launch
it from, and its discovery file lives under `~/.catacomb/run/`.

Install from source (`go install github.com/realkarych/catacomb/cmd/catacomb@latest`)
or build locally with `make build`.

## Privacy

Catacomb observes your sessions locally. The graph holds structure, timing,
token/cost metadata, and a content *hash* — not the conversation text itself.
Message and tool content is served only when the daemon is started with
`--allow-payload-access`, through a token-gated endpoint that redacts secrets at
serve time. The HTTP surface binds to loopback and is gated by a bearer token
printed at startup.

## Development

```sh
make build   # build bin/catacomb
make test    # tests with -race + coverage profile
make cover   # enforce the 100% coverage gate
make lint    # golangci-lint
```

## License

[Apache-2.0](LICENSE).
