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
- High-entropy hex, base64 (including `/`-bearing spans such as AWS secret access
  keys), and base64url strings, gated by a Shannon-entropy threshold so low-entropy
  lookalikes — UUIDs, repeated patterns, and most file paths — pass through untouched

It also redacts any value whose key path matches a sensitive token: `password`,
`passwd`, `secret`, `token`, `apikey`/`api_key`, `auth`, `credential`,
`private_key`/`privatekey`, `sessionkey`/`session_key`. Matches are replaced with
typed `‹redacted:reason›` placeholders.

### Known residuals

Redaction narrows what a copied evidence dir or store can leak; it does not make your
filesystem a vault. Deliberate trade-offs to know about:

1. **The denylist is best-effort, not a guarantee.** A secret in a shape no rule
   recognizes — or a generic token below the entropy gate — survives redaction. Treat
   evidence dirs as reduced-risk, not secret-free. Classes the entropy gate
   deliberately does not catch:
   - **Hex- or base32-encoded ASCII secrets.** Encoding ASCII this way typically holds
     entropy to ~2.8–3.3 bits — largely inside the band of legitimate hashes and
     identifiers the gate exists to spare — so many but not all such secrets fall below
     the gate. Mixed-case-and-digit ASCII near the top of that range (H≈3.3) crosses the
     threshold and is caught; lower-diversity encodings are not.
   - **UUID-shaped secrets** (Heroku API keys, for example) are structurally
     indistinguishable from the session-id UUIDs that saturate every transcript.
   - **Adversarial padding dilution.** Content shaped by an attacker can pad a secret
     with repetitive filler until the span falls below the entropy gate — inherent to
     any span-based denylist.
   - **A sub-threshold tail at minimum length.** Roughly 3% of random 32-character
     base64url secrets happen to measure below the 4.3-bit gate; the tail is
     near-zero by 36 characters.
2. **The source transcripts are not catacomb's.** Claude Code's own files under
   `~/.claude/projects` remain unredacted on disk; catacomb reads them but never
   rewrites them. Evidence dirs are the redacted, shareable copy.
3. **Redaction false positives destroy data in the evidence copy.** There is no raw
   copy inside the evidence dir to recover from — by design. The original transcript
   under `~/.claude/projects` is the fallback while Claude Code retains it. The entropy
   gate can over-redact high-diversity path segments — content-addressed store hashes or
   case-and-digit-diverse CI paths of ≥40 characters — a safe-direction false positive
   that loses data rather than leaking it.
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

The same path carries a **version watchlist**, kept per runtime: catacomb records the
newest Claude Code and Codex CLI versions it has been tested against — one ceiling
each — and a transcript stamped with a version newer than its runtime's ceiling prints
a second advisory line:

```text
warning: transcript Claude Code version 2.2.0 is newer than tested 2.1.199
warning: transcript Codex version 0.150.0 is newer than tested 0.144.5
```

It is the companion to the drift count — a heads-up that the agent CLI outran the release
this catacomb was validated on, so an unrecognized shape may be lurking even when the
drift count is still zero. Both lines share the same rules: emitted only when triggered,
on any command that parses transcripts (`bench`, `regress`, `diff`, `subgraph`,
`export`, `replay`), and never touching stdout, `--json`, or the exit code.

### Scale

`regress` is the memory-heavy path: it holds the reduced graphs of every run in both
the baseline and the candidate group at once. Aggregation and gating read only graph
structure and per-node metrics — never payloads — so the group loader strips each
run's payloads, node attrs, and per-observation source refs immediately after
reduction and score application. Held-group memory therefore scales with graph
*structure* (node and edge count), not with transcript size: a run's raw payload
bytes are live only while that single run is being parsed. Commands that do need
payloads (`diff`, `export`, `pack`, `replay`) still load full graphs.

Measured envelope (`make bench`, Apple M4 Pro, Go 1.26): one regress gate is tested
to 16 runs (8 baseline + 8 candidate) × ~2,000 nodes per session (1,000 tool calls
with 2KB outputs each), completing in ~8.4s with ~1.2GB of cumulative allocations
(`1223717496 B/op` from `-benchmem`; transient, reclaimed by GC). Holding one such
8-run group retains ~2.1MB of heap per run after the strip versus ~5.2MB without it
(measured `retained-B` ~16.7MB vs ~41.5MB per group) — the difference is the payload
bytes, so the saving grows with transcript verbosity up to the 256KB per-node payload
cap. Benchmarks are informational: run them with `make bench`; they are deliberately
not a blocking CI gate, since shared-runner timings are noise.

### Troubleshooting

See [Troubleshooting](troubleshooting.md) for a table of common symptoms and fixes.
