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
entries into `./.claude/settings.json` for the current directory, prints the
bearer URL, and opens the web UI:

```sh
catacomb up
```

Open Claude Code in the same directory and run a session. The action graph
appears in the UI immediately.

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

## Open the UI

If the daemon is already running, open the web UI without restarting it:

```sh
catacomb ui
```

This prints the bearer URL and opens the browser. You can also copy the URL
from the terminal to open it manually.

## Reading message content

By default the graph stores a content hash — not the conversation text. To
read message and tool content in the UI, start the daemon with
`--allow-payload-access`:

```sh
catacomb daemon --allow-payload-access
```

Content is served through a token-gated endpoint with serve-time secret
redaction. See [Privacy and operations](privacy-and-operations.md) for what is
redacted and how.

## Next steps

- [Concepts](concepts.md) — understand the action graph and how sources are reconciled
- [Ingestion](ingestion.md) — wire OpenTelemetry and stream-json for richer data
- [Workflows](workflows.md) — diff sessions, set checkpoints, and export the graph
- [CLI reference](cli.md) — all commands and flags
