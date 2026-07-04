# Privacy and operations

## Privacy and security

### Network exposure

The catacomb daemon binds to `127.0.0.1` on a random port, for both HTTP and gRPC.
It is never reachable from outside the local machine.

### Bearer token

Every request requires `Authorization: Bearer <token>` or a `?token=<token>` query
parameter. The token is a 64-character hex string (32 bytes from `crypto/rand`),
compared in constant time. It is printed at daemon startup and written to the discovery
file (`~/.catacomb/run/daemon.json`, mode 0600; directory mode 0700).

### Graph holds structure, not content

Graph API and SSE responses carry timing, token counts, costs, statuses, step keys,
and a content hash per node — payload content is never inlined into them. Payload
bodies are persisted in the database (already redacted, see below) and are served
only through a dedicated endpoint that is off by default.

### Secrets at rest

Redaction is enforced on the write path
([ADR-0024](../adr/0024-secrets-at-rest-write-path-redaction.md)): every
observation is passed through the redaction rules before it is applied to the
graph and appended to the store, and every node delta is redacted again before
persistence. What is on disk in `catacomb.db` is already redacted; serve-time
and export-time redaction still run as defense in depth. `payload_hash` is the
sha256 of the redacted payload — no pre-redaction hash is stored or exported.

The `redact` package applies regex patterns for:

- AWS access keys
- GitHub tokens and PATs
- OpenAI `sk-` keys
- Slack `xox*` tokens
- JWTs
- PEM private keys
- Google `AIza` keys
- Bearer tokens in headers
- Connection strings (DSN/URL forms)
- High-entropy hex and base64 strings

It also matches key-path globs including `password`, `secret`, `token`, `apikey`,
`auth`, `credential`, and `private_key`.

Payload handling is configurable via the `payloads` config section
(mode `redact` | `refs` | `all`, plus `max_bytes`; see
[Configuration](configuration.md)). Ordering is redact-then-cap: payloads are
redacted first, then sides larger than `max_bytes` are replaced by a typed
`‹ref:len,hash›` reference; non-UTF-8 payloads become `‹binary:len,hash›`
references. In `refs` mode no payload bodies are stored at all — note that the
`transcript_path` attr still points at the unredacted transcript file on disk.
`all` disables write-path redaction (a startup warning is logged); serve-time
redaction still applies. Redaction false positives destroy data at rest — there
is no raw copy to recover; `all` is the explicit opt-out.

Databases created before schema v4 are scrubbed by a one-time data migration on
the first write-path open (`catacomb up` / `catacomb daemon`): all existing
observation, node, and run bodies are rewritten through the redactor, hashes
are recomputed, and the file is vacuumed so old row images do not linger. The
migration is idempotent. Read-only commands (`runs`, `inspect`, `export`, …)
refuse pre-v4 databases with a schema-outdated error until a write-path command
has migrated them. Quarantine holds raw bytes only for input that could not be
parsed; persist-failure quarantine (input that parsed but failed to persist) is
redacted before it is written.

### Known residuals

Write-path redaction narrows what a copied `catacomb.db` can leak; it does not
make the database a vault. Known residuals, each a deliberate design choice or
an accepted trade-off:

1. **Quarantine holds raw bytes only for input that could not be parsed
   (including panics during ingestion).**
   Malformed input is stored verbatim in the `quarantine` table so it can be
   diagnosed; redaction never saw it. A panic while ingesting a payload
   quarantines the raw payload the same way. Persist-failure quarantine — input
   that parsed but failed to persist — is redacted before it is written.
2. **Annotation values persist unredacted.** The annotation write endpoint
   (gated local API, off by default) stores caller-provided values as given.
3. **Entropy-looking paths lose repro hashes.** A cwd path segment that looks
   like a high-entropy string is redacted at rest, so repro hashes for such
   paths come back `absent`. This is deterministic — it never corrupts other
   data.
4. **`redacted` in the payload response is marker detection.** Literal
   `‹redacted:…›` or `‹ref:…›` text occurring in genuine content reads as a
   false positive, and a payload capped to a typed ref also reads as
   `redacted`.
5. **A crash between the v4 scrub commit and the vacuum leaves free pages.**
   The scrub commit and the `VACUUM` are separate steps; if the process dies
   between them, pre-scrub row images can linger in the file's free pages
   until an incidental vacuum — the vacuum trigger is gated on the
   schema-version bump, which has already committed.
6. **Stored hashes are not always `hash(stored bytes)`.** In `all` mode the
   stored hash is the parser wire-bytes hash, which differs from the hash of
   the stored bytes for non-compact wire payloads. In `refs` mode observation
   rows carry the content hash while the bodies are typed refs.

### Payload endpoint and content access

`GET /v1/sessions/{hash}/nodes/{nodeId}/payload` returns the message or tool
input/output for one node. The endpoint returns `403 Forbidden` unless the daemon was
started with `--allow-payload-access` (or `daemon.allow_payload_access: true` in
config).

The stored (already-redacted) payload is passed through serve-time redaction once
more before being returned. In the response, `redacted` is true when the stored
content carries redaction markers or serve-time redaction fired; the `redactions`
list reports serve-time findings only, as `{path, reason}` — write-path findings
are not persisted.

### Annotations access

`POST /v1/sessions/{hash}/nodes/{nodeId}/annotations` accepts `{owner, key, value}`
and returns `403 Forbidden` unless `--allow-annotations` is set (or
`daemon.allow_annotations: true`). `owner` and `key` must not contain dots; `value`
must be valid JSON.

## Operations

### Health and metrics

```
GET /healthz    → 200 (no body)
GET /metrics    → JSON object
```

The `/metrics` response fields:

| Field | Meaning |
| --- | --- |
| `uptime_seconds` | Seconds since daemon started |
| `open_runs` | Currently active runs |
| `shards` | In-memory execution shards in use |
| `max_seq` | Highest sequence number seen |
| `quarantined` | Nodes moved to the quarantine table |
| `evicted` | Sessions evicted from memory to the store |
| `store_write_errors` | Failed store writes (non-blocking) |
| `deltas_dropped` | SSE deltas dropped under backpressure |
| `exporter_lag` | Pending records waiting for the exporter |
| `reaper_window_seconds` | Configured reaper window |
| `lossy_runs` | Runs where memory pressure forced a node merge |
| `drift` | Well-formed but unrecognized records per `source/reason` key (omitted when zero) |

### Format drift

Catacomb watches the upstream Claude Code formats it parses
([ADR-0025](../adr/0025-capture-format-drift-detection.md)). Records that parse
cleanly but match no known shape are counted per `(source, reason)`; reasons are
coarse buckets (`unknown_record_type`, `unknown_subtype`, `unknown_span_name`,
`unknown_hook_event`, `unknown_content_block`) and never contain payload
content. Counters live in daemon memory (reset on restart), surface in
`/metrics` under `drift`, and `catacomb status` prints `drift` rows only when at
least one counter is nonzero. The daemon logs a warning on the first occurrence
per key and every 100th thereafter. Malformed input still goes to quarantine;
drift covers the complementary healthy-but-unknown class.

The binary also carries a tested Claude Code version ceiling. A newer `claude_code_version`
logs one warning per daemon lifetime but sets `format_watch: true` in the meta
of every run whose version exceeds the ceiling; capture proceeds identically. Bump
`TestedClaudeCodeVersion` in `ingest/drift` after verifying a new Claude Code
release (release-checklist item).

### Daemon status

```sh
catacomb status
catacomb status --json
```

Prints address, PID, uptime, token age, observing/history directory, store backend,
sinks, sources, reaper window, shard counts, session and node counts, drift counters
(when nonzero), and overall health.

### Lifecycle

```sh
# Start daemon, install hooks, open UI
catacomb up

# Stop daemon (graceful: SIGTERM, ~5 s, then SIGKILL with --force)
catacomb down

# Preview what down would do without making changes
catacomb down --dry-run

# Remove hooks and delete the local db and ~/.catacomb state (no prompt)
catacomb down --all --yes

# Stop and restart with the same config
catacomb restart

# View daemon log
catacomb logs
catacomb logs --follow
```

### Memory bounding

The daemon bounds memory use with three mechanisms.

**Max shards** (`--max-shards`, default 4096): soft cap on in-memory execution shards.
When the cap is reached, the reaper evicts idle sessions to the store.

**Reaper window** (`--reaper-window`, default 30m): idle window before a session is
considered abandoned and evicted. Configurable via `daemon.reaper_window` in config.

**Quarantine**: malformed ingest payloads are written to a `quarantine` table rather
than silently dropped. The `quarantined` metric counts them.

`lossy_runs` counts runs where memory pressure forced a node merge, signaling that the
materialized graph may differ slightly from real-time observations.

`store_write_errors`, `deltas_dropped`, and `exporter_lag` count downstream failures
without blocking ingest.

### Troubleshooting

| Symptom | Action |
| --- | --- |
| No sessions appear | Verify hooks are installed: `catacomb install-hooks` or re-run `catacomb up` |
| Past sessions missing | Run `catacomb up --history` to tail `~/.claude/projects` |
| Cannot read content in UI or observer | Start the daemon with `--allow-payload-access` |
| "No daemon running" error from any command | Run `catacomb up` or check `catacomb status` |
| Wrong database is loaded | Set `--db <path>` or `store.sqlite.path` in config |

## Export targets

Catacomb forwards graph data to external sinks in two ways.

- **Live sinks**: configured under `sinks:` in `~/.catacomb/config.yaml`, they stream
  graph deltas to the target as the session grows.
- **One-shot export**: `catacomb export --to <sink>` reads the stored database and
  writes the full materialized graph in a single pass. Use `--run <id>` to filter to
  one run.

### JSONL

```sh
# Materialized graph (default) to a file
catacomb export --to jsonl --out graph.jsonl

# Raw observations mode
catacomb export --to jsonl --out events.jsonl --mode events

# Filter to one run
catacomb export --to jsonl --run <run-id> --out run.jsonl
```

Default mode (`materialized`) emits `{"kind":"node"…}`, `{"kind":"edge"…}`, and
`{"kind":"run"…}` records. `--mode events` emits raw observations instead. Omit `--out`
to write to stdout.

As a live sink:

```yaml
sinks:
  - type: jsonl
    path: /path/to/out.jsonl
```

### Postgres

```sh
catacomb export --to postgres --postgres-export-dsn "postgres://user:pass@host/db"
```

Auto-creates `nodes`, `edges`, and `runs` tables with JSONB columns for `attrs`,
`annotations`, `meta`, and `repro`. Upsert is idempotent by revision. Materialized
mode only.

As a live sink:

```yaml
sinks:
  - type: postgres
    dsn: "postgres://user:pass@host/db"
```

### Neo4j

```sh
catacomb export --to neo4j \
  --neo4j-export-uri bolt://localhost:7687 \
  --neo4j-export-user neo4j \
  --neo4j-export-password secret
```

Node labels: `Session`, `UserPrompt`, `AssistantTurn`, `ToolCall`, `Subagent`,
`McpCall`, `HookEvent`, `Marker`. Relationships: `PARENT_OF`, `NEXT`, `IN_PHASE`,
`DATA_DEP`. Nodes are merged with a revision guard to prevent duplicates.

As a live sink:

```yaml
sinks:
  - type: neo4j
    uri: bolt://localhost:7687
    user: neo4j
    password: secret
```

### OTLP / OpenInference

```sh
catacomb export --to otlp \
  --otlp-export-endpoint grpc://host:4317 \
  --otlp-export-project myproject
```

Exports OTel traces with OpenInference attributes: `span.kind` set to `AGENT`, `TOOL`,
`LLM`, or `CHAIN`; `gen_ai.*`, `llm.*`, and `tool.*` attributes; tokens and cost.
Serve-time payload redaction is applied before export. The exporter refuses to export to
its own daemon address.

The endpoint accepts `grpc://…` or `http(s)://…` forms. `--otlp-export-project`
(default `catacomb`) sets the OpenInference project name.

As a live sink:

```yaml
sinks:
  - type: otlp
    endpoint: grpc://host:4317
    project: catacomb
```

### Eval outputs

```sh
catacomb export --to agentevals --out eval.json
catacomb export --to evalview --out view.json
```

`agentevals` and `evalview` produce eval-oriented JSON files for downstream evaluation
tooling. Use `--out <file>` to write to a path; omit for stdout.
