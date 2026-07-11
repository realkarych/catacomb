# Privacy and operations

## Privacy and security

### No network surface

Catacomb is a plain CLI. It runs no daemon, opens no sockets, and requires no token:
every command reads and writes local files only — Claude Code transcripts under
`~/.claude/projects`, evidence directories under `~/.catacomb/runs`, and the SQLite
store at `~/.catacomb/catacomb.db`. The only processes it starts are the ones you
declare yourself in a basket (`cmd` and `setup`). The trust boundary is your
filesystem.

### What catacomb writes, and what it redacts

Three artifacts leave a catacomb run, each with a defined redaction story
([ADR-0024](../adr/0024-secrets-at-rest-write-path-redaction.md)):

1. **Evidence directories** (`bench`). Each cell's transcripts are copied into
   `<runs-dir>/<run-id>/` **through the redactor, line by line, on write** — the
   evidence copy never contains the pre-redaction bytes. `meta.json` holds only run
   metadata (ids, labels, exit code, cost, timing). File modes are `0600` (files) and
   `0700` (directories).
2. **Graphs built in memory** (`regress`, `replay`, `diff`, `subgraph`, `export`).
   Every parsed observation passes through the redaction policy before it reaches the
   reducer: attributes and payloads are redacted, and each payload side is capped at
   256 KiB — an oversized side is replaced by a typed `‹ref:len,hash›` reference, and
   non-UTF-8 content becomes `‹binary:len,hash›`. A node's `payload_hash` is the sha256
   of the *redacted* payload; no pre-redaction hash is computed, stored, or exported.
   `export` output therefore carries only redacted content.
3. **The store** (`baseline set`, `regress --record`). It holds no transcripts and no
   payloads at all — only baseline definitions (name, pinned run IDs, selector, stamps)
   and the recorded regression reports (verdicts, step names, metric aggregates).

Step keys hash only redacted content by construction: the content term is the hash of
the **redacted**, salient-projected tool input (for example just the `file_path` of an
edit or the `command` of a shell call, after redaction), so a step key never hashes
pre-redaction bytes.

### Redaction rules

The `redact` package applies value patterns for:

- Connection strings with credentials (DSN/URL forms)
- AWS access keys
- GitHub tokens and PATs
- OpenAI `sk-` keys
- Slack `xox*` tokens
- PEM private key blocks
- Google `AIza` keys and `ya29.` OAuth tokens
- `Bearer` tokens
- JWTs
- Stripe `sk_live_`/`sk_test_` (and `rk_`/`pk_`) keys
- SendGrid `SG.` keys
- Twilio `SK` keys
- npm `npm_` and PyPI `pypi-` tokens
- GitLab `glpat-` tokens
- High-entropy hex, base64, and base64url strings, gated by a Shannon-entropy
  threshold so low-entropy lookalikes (file paths, UUIDs, repeated patterns) pass
  through untouched

It also redacts any value whose key path matches a sensitive token: `password`,
`passwd`, `secret`, `token`, `apikey`/`api_key`, `auth`, `credential`,
`private_key`/`privatekey`, `sessionkey`/`session_key`. Matches are replaced with
typed `‹redacted:reason›` placeholders.

### Known residuals

Redaction narrows what a copied evidence dir or store can leak; it does not make your
filesystem a vault. Deliberate trade-offs to know about:

1. **The denylist is best-effort, not a guarantee.** A secret in a shape no rule
   recognizes — or a generic token below the entropy gate — survives redaction. Treat
   evidence dirs as reduced-risk, not secret-free.
2. **The source transcripts are not catacomb's.** Claude Code's own files under
   `~/.claude/projects` remain unredacted on disk; catacomb reads them but never
   rewrites them. Evidence dirs are the redacted, shareable copy.
3. **Redaction false positives destroy data in the evidence copy.** There is no raw
   copy inside the evidence dir to recover from — by design. The original transcript
   under `~/.claude/projects` is the fallback while Claude Code retains it.
4. **Literal `‹redacted:…›`/`‹ref:…›` text in genuine content** is indistinguishable
   from a real redaction marker downstream.
5. **`--scores` files are yours.** Catacomb reads them and applies the values in
   memory; it neither redacts nor stores them.

## Operations

### Exit codes

Every command uses the same convention:

| Code | Meaning |
| --- | --- |
| `0` | Success (for `regress`: verdict `ok`) |
| `1` | Regression detected (`regress`), `insufficient` under `--strict`, or a `--fail-fast` stop (`bench`) |
| `2` | Operational error: bad input, missing files or store, empty group, schema mismatch |

### Format drift

Catacomb watches the transcript format it parses
([ADR-0025](../adr/0025-capture-format-drift-detection.md)). Records that parse as
JSON but match no known shape are counted per reason and surfaced as one stderr
warning per command invocation:

```text
warning: 3 unrecognized transcript record(s) [unknown_content_block=1, unknown_record_type=2]
```

The graph is still built from every record that did parse. A persistent warning after
a Claude Code update means the transcript format grew a shape this catacomb does not
know — upgrade catacomb. stdout and `--json` output stay clean; the warning never
changes an exit code.

The same path carries a **version watchlist**: catacomb records the newest Claude Code
version it has been tested against, and a transcript stamped with a newer version prints
a second advisory line:

```text
warning: transcript Claude Code version 2.2.0 is newer than tested 2.1.199
```

It is the companion to the drift count — a heads-up that Claude Code outran the release
this catacomb was validated on, so an unrecognized shape may be lurking even when the
drift count is still zero. Both lines share the same rules: emitted only when triggered,
on any command that parses transcripts (`bench`, `regress`, `diff`, `subgraph`,
`export`, `replay`), and never touching stdout, `--json`, or the exit code.

### Troubleshooting

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
| `warning: N unrecognized transcript record(s)` | Transcript format drift — see [Format drift](#format-drift) |
| `warning: transcript Claude Code version … is newer than tested …` | Claude Code outran this binary's tested version ceiling — upgrade catacomb; see [Format drift](#format-drift) |
