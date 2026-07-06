# Getting started

## Install

Install the latest release:

```sh
go install github.com/realkarych/catacomb/cmd/catacomb@latest
```

Or build from source (requires Go 1.26):

```sh
make build        # produces bin/catacomb
```

## Start the daemon

`catacomb up` starts the daemon if it is not already running, installs hook
entries into `./.claude/settings.json` for the current directory, and prints
the daemon address:

```sh
catacomb up
```

Open Claude Code in the same directory and run a session. The daemon captures
the action graph as the session runs; `catacomb status` shows session and node
counts.

## Observe every project

By default, `catacomb up` installs hooks for the current directory only. To
observe sessions in every project, pass `--global`:

```sh
catacomb up --global
```

This writes hook entries into `~/.claude/settings.json` so any Claude Code
session — from any directory — is observed.

## Load past sessions

Hooks only capture sessions that start after they are installed. To backfill
sessions you have already run, pass `--history`:

```sh
catacomb up --history
```

The daemon tails `~/.claude/projects`, reads every existing transcript on
startup, then follows new ones live. Tail cursors are persisted so restarting
the daemon does not duplicate history.

Combine both flags for full coverage:

```sh
catacomb up --global --history
```

## Watch runs in a UI

Catacomb ships no viewer. To watch sessions live in a UI, feed them to a
vendor substrate through that vendor's first-party Claude Code plugin —
Phoenix is the recommended substrate
([ADR-0026](../adr/0026-form-factor-pivot-offline-eval-gate.md) §2).

## Reading message content

Graph responses carry a content hash — not the conversation text. Payload
bodies are redacted on the write path before being stored. To read message and
tool content over the API, start the daemon with `--allow-payload-access`:

```sh
catacomb daemon --allow-payload-access
```

Content is served through a token-gated endpoint that redacts once more at
serve time. See [Privacy and operations](privacy-and-operations.md) for what is
redacted and how.

## Next steps

- [Concepts](concepts.md) — understand the action graph and how sources are reconciled
- [Ingestion](ingestion.md) — wire OpenTelemetry and stream-json for richer data
- [Workflows](workflows.md) — diff sessions, set checkpoints, and export the graph
- [CLI reference](cli.md) — all commands and flags
