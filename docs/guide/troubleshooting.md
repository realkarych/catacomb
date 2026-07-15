# Troubleshooting

Common symptoms and their fixes. If your error isn't listed, the
[Privacy and operations](privacy-and-operations.md) page has the operational detail
behind most of these.

| Symptom | Action |
| --- | --- |
| Manifest note says `no session id observed` | The cell's `cmd` must emit stream-json: run `claude` with `--output-format stream-json` |
| Manifest note says `transcripts not found` | Check `--projects-dir` points at the Claude projects dir that owns the session; bench retries for ~3 s after the child exits |
| `selector matched no runs` | Inspect `<runs-dir>/*/meta.json` labels; check `--runs-dir` and the `label:` terms (all terms are ANDed) |
| `no catacomb store found` | Create the store with a write-path command: `catacomb baseline set` |
| `store schema is older than this binary` | Run a write-path command (`catacomb baseline set`) to migrate it |
| `on-disk schema is newer than this catacomb binary` | Upgrade catacomb |
| `SQLITE_BUSY` on `regress --record` | Serialize the recorders or give each CI shard its own `--db` file |
| `cell <run-id>: missing checkpoints: …` warnings | The agent never called `mcp__catacomb__mark` for those phases — check the `--mcp-config` wiring and the CLAUDE.md marking convention |
| `warning: N unrecognized transcript record(s)` | Transcript format drift — see [Format drift](privacy-and-operations.md#format-drift) |
| `warning: transcript Claude Code version … is newer than tested …` | Claude Code outran this binary's tested version ceiling — upgrade catacomb; see [Format drift](privacy-and-operations.md#format-drift) |
| `brew` installed an older version than the latest release | Run `brew update && brew upgrade --cask catacomb`; brew, apt, and docker converge within minutes of a release, while `go install` serves the tag immediately |
| Offline `catacomb verify` cannot find the verifier script | Basket paths resolve against the basket file's directory, not your shell's cwd — keep the verifier next to the basket and reference it as `./verify.py`. See [basket.md](basket.md) |
