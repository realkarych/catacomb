# Importing a hand-run interactive session

`bench` launches the agent and records what it observes. `import` is the second entry
point: a session you ran **by hand in the interactive TUI** enters the same pipeline at
the transcript stage. `import` shapes that transcript into a `bench`-cell-shaped
[evidence](concepts.md#basket-cell-evidence) directory, so `verify` and `regress` read it
with no special case — you ran the agent yourself, `import` records the run.

## Recommended workflow

Pin the session id before you start so `import` can always find the transcript afterward:

```sh
SID=$(uuidgen)
claude --session-id "$SID" --mcp-config catacomb-mcp.json
# … do the task by hand in the TUI; call mcp__catacomb__mark at each checkpoint …
catacomb import basket.yaml --task <task-id> --variant <variant-id> --session-id "$SID"
catacomb verify basket.yaml --runs-dir ~/.catacomb/runs
```

## Mapping the session onto a cell

- `--task` and `--variant` map the hand-run session onto one basket **cell**. The basket
  stays the source of truth for that cell's `verify:`, `checkpoints:`, and labels, exactly
  as under `bench`; both ids must name a task and variant that exist in the basket. The
  task `cmd` is **ignored** — you ran the agent, so there is nothing for `import` to
  launch.
- `--session-id` selects which `~/.claude/projects` transcript to ingest, resolving the
  main `<session-id>.jsonl` plus any `subagents/agent-*.jsonl`. Didn't pin an id up front?
  Don't pass `--session-id`; point `--transcript` at the newest transcript instead:

  ```sh
  catacomb import basket.yaml --task <task-id> --variant <variant-id> \
    --transcript "$(ls -t ~/.claude/projects/<encoded-cwd>/*.jsonl | head -1)"
  ```

  Exactly one of `--session-id` / `--transcript` is required.

## The evidence it writes

An imported run lands in the **same evidence shape as `bench`** — `session.jsonl`,
`subagents/agent-*.jsonl` when present, and a `meta.json`, all secret-redacted — so it
compares directly against bench-recorded runs in `regress`. Two things differ, both
because `import` launches no `cmd`:

- **No `artifacts/`.** A `verify:` that reads a captured artifact
  (`cell.artifact("out/result.csv")`) cannot score an imported cell — it errors with no
  verdict. Verifiers meant for imports read the transcript and evidence dir instead.
- **`cost_usd` field omitted** from `meta.json` (an interactive transcript carries no
  terminal total cost). The token-derived `cost_usd` *metric* still works, priced from the
  transcript's token counts, so cost gating in `regress` stays comparable.

Any `mcp__catacomb__mark` checkpoints the session recorded are honored; a declared
`checkpoints:` name the transcript never marked is warned to stderr and never gates.

Verification stays a **separate step**: `import` only records evidence. Run `verify` to
score the task's `verify:` block, then `regress` to gate — the same
`bench → verify → regress` cycle, entered one cell at a time.

## See also

- [Ingestion](../../../docs/guide/ingestion.md) — how catacomb reads transcript JSONL.
- [CLI reference](../../../docs/guide/cli.md) — the full `import` flag table and exit codes.
