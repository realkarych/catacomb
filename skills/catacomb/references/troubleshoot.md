# Troubleshooting

Symptom → cause → fix for the failures you hit most while standing up a bench. For
anything not listed, the full [troubleshooting guide](../../../docs/guide/troubleshooting.md)
has the operational detail.

| Symptom | Cause | Fix |
| --- | --- | --- |
| `catacomb bench` / `catacomb regress`: `unknown command`, or the subcommand is missing entirely | Stale **pre-pivot** install — the old command set predates the bench/regress form factor | Reinstall a v0.2.0+ build: `brew uninstall catacomb && brew install --cask catacomb`, or `go install github.com/realkarych/catacomb/cmd/catacomb@latest`. Confirm with `catacomb version` (expect **v0.2.0 or newer**). |
| `claude` errors / auth failures while a cell runs | Claude Code isn't signed in — `bench` drives the real `claude` CLI and spends real money | Verify the CLI first with `claude -p hello`. Then sign into your Claude subscription, or set `ANTHROPIC_API_KEY` for API billing. |
| No `runs/<id>/` [evidence](concepts.md#basket-cell-evidence) directory / no transcript captured | The agent command didn't emit a resolvable stream-json session, or `--projects-dir` points at the wrong directory | Add `--output-format stream-json --verbose` to the `claude` invocation — that's how the runner peeks the `session_id` and resolves the transcript. Confirm `--projects-dir` points at the real `~/.claude/projects`. See [setup.md](setup.md) for the one-cheap-cell capture check. |
| `warning: transcript … newer than tested …` on stderr | Claude Code is newer than the version catacomb was validated against — the tested-version watchlist | Harmless — it never touches the graph, `stdout`, `--json`, or the exit code. Upgrade catacomb when convenient. Background: the [tested-version watchlist](../../../docs/guide/ingestion.md). |
| Unexpected / higher-than-expected cost | Too many reps or cells, or an expensive model — every cell calls the paid `claude` | Lower `reps` (there is **no `--reps` flag** — reps live in the basket), pick the cheapest adequate model, and pre-flight with `catacomb bench <basket> --dry-run` to see the cell count before spending. |
