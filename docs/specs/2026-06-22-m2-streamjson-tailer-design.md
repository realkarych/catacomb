# M2 — stream-json ingest + JSONL tailer (design)

- **Status:** Approved (autonomous design — see [[autonomous-completion-mandate]])
- **Date:** 2026-06-22
- **Deciders:** @realkarych (delegated; decisions documented here)
- **Builds on:** M1 (merged): OTLP receiver + four-source-precedence scaffolding +
  status lattice/cascade + CDC bus + OTLP/OpenInference passthrough exporter
- **Implements (subsets of):** ADR-0001, ADR-0002, ADR-0003, ADR-0009, ADR-0010,
  ADR-0011, ADR-0012, ADR-0014, ADR-0018, ADR-0019

## 1. Context & goal

M0 (hooks + offline JSONL `replay`) and M1 (OTel + the four-source-precedence reducer
scaffolding + CDC bus + OTLP passthrough) are merged. The reducer, the CDC bus, the
nine-status lattice, the transitive cascade, `Rev` on nodes/edges, the `(source-rank,
seq)` stamp mechanism, and the `tool_use_id` cross-source linchpin all exist. Two of the
four `model.Source` values declared at `model/model.go:10-15` still have **no live
producer**: `SourceStreamJSON` and the live half of `SourceJSONL`.

M2 adds those two producers so **all four sources have a live ingress**, and in doing so
delivers the headline value of the JSONL source: the **subagent tree** that backfills
the #53954 gap (on the Agent SDK streaming path OTel collapses to flat
`claude_code.llm_request` spans, so parent→child subagent structure must come from the
transcript). Concretely:

- **Source A — stream-json** (`model.SourceStreamJSON`): consume Claude Code's
  `--output-format stream-json` NDJSON (the `claude -p` / Agent SDK streaming path). It
  contributes the structural hint `parent_tool_use_id`, `result.usage` tokens as a
  mid-tier source, and payload deltas. It arrives via a new token-gated
  `POST /v1/stream-json` daemon endpoint fed by two thin CLI forwarders.
- **Source B — JSONL tailer** (`model.SourceJSONL`, live): pure-Go polling watcher over
  the transcript directory that feeds newly-appended lines of each `<sessionId>.jsonl`
  (and subagent files) through the existing `ingest/jsonl` parser, with a persisted
  byte-offset cursor. JSONL is the subagent-tree truth and the #53954 backfill.

M2 also activates the **per-field four-source precedence** that M1 left as a table with no
producers below OTel: with stream-json becoming the third live source, the reducer's
OTel-vs-rest binary stamp mechanism is generalized to the full per-field-group source
ordering of the original spec §5.1 / M1 spec §5.1.

The transcript and stream-json wire formats are officially **undocumented** (original spec
§3, §17; ADR-0002; issues #24612 / #24596 / #53954). As with the hook envelope, the
parsers use best-known field names validated by fixtures, with explicit **[VERIFY]**
markers for end-of-roadmap operator verification (Step 7).

## 2. Scope

**In scope:**

- `ingest/streamjson` adapter: NDJSON line → `[]model.Observation` (`Source:
  SourceStreamJSON`), pure, fixture-tested, mirroring `ingest/otel.Parse`.
- `POST /v1/stream-json` daemon endpoint (streamed NDJSON body) + `Daemon.IngestStreamJSON`
  mirroring `Daemon.IngestOTLP`.
- `catacomb ingest stream-json` (stdin NDJSON → streamed POST) and
  `catacomb run -- <cmd...>` (exec child, tee stdout to terminal + forward to daemon) CLI
  verbs — thin forwarders like `catacomb hook`.
- `ingest/jsonl.Parse` seam generalization: a `nextSeq func() uint64` seam (+ a clock
  seam) so the live tailer supplies the daemon's global `seq` and ingest-time `ObservedAt`
  while offline `replay` keeps its local counter and behaviour unchanged.
- JSONL tailer: pure-Go polling watcher (no fsnotify, no cgo), `tail_cursors` SQLite
  table, rotation/truncation detection, partial-line buffering, transcript-dir discovery,
  self-exclusion, `lossy`-run flagging, EOF≠closed.
- Subagent-tree structure: `parent_tool_use_id` → `parent_child` edges, and
  `isSidechain` / subagent-transcript records → subagent nodes.
- Reducer four-source precedence generalization: extend `sourceRank` / `stamp` /
  `applyTokens` / `mergePayload` and add a `parent_tool_use_id` structure consumer, all
  keeping the merge commutative.
- `--transcript-dir` daemon flag; tailer wired into `Daemon.Serve` under recover/backoff.

**Out of scope → later milestones / deferred (see §11):**

- WS/SSE + gRPC streaming graph surfaces → M3.
- neo4j / postgres exporters → M4.
- Web UI → M5.
- Full §5.8 threading beyond the subagent tree — interruption (`interruptedMessageId` →
  `cancelled`), regeneration/edit branches (`leafUuid` → `superseded`), compaction
  re-stitching (`logicalParentUuid`), the conversation `parentUuid` tree — deferred to
  M3+ (the reducer status machinery for these already exists from M1b; only the JSONL
  *producers* are deferred).
- Offline EOF→`unknown` closure for `replay` (original spec §5.8 "EOF / inactivity TTL"),
  deferred to M0.3-style follow-up; the live tailer treats EOF as not-closed (§5.7) and
  that is the only EOF behaviour M2 binds.
- ADR-0017 store format-versioning (`schema_version` / `reducer_version`) — not introduced
  by the new `tail_cursors` table; remains tech-debt.
- Idempotent cross-source re-ingest with a stable per-line `obs_id` (ADR-0010 clause 4) —
  M2's tailer dedup is offset-based (§5.3), so the same on-disk line is never re-read; the
  duplicate-obs-row concern is documented as residual, not solved here.
- Markers from stream-json/JSONL structural signals beyond what the existing `marker` kind
  already covers via hooks — no new marker producers in M2.

## 3. Architecture delta

M2 adds one ingress endpoint and one in-daemon watcher goroutine; both feed the existing
`applyAndPersist` path under `d.mu`. No change to the data-flow shape established in M0/M1.

```
            ┌─ ingestion ──────────────────────┐   ┌─ core ──────────────┐
 hooks ───▶ │ POST /hook/{type}                │   │                     │
 OTel  ───▶ │ POST /v1/traces  + OTLP/gRPC      │ ─▶│  reducer (one seq,  │ ─▶ CDC bus
 -p    ───▶ │ POST /v1/stream-json   ◀── NEW    │   │  one shard map,     │    (M1)
 jsonl ───▶ │ JSONL tailer goroutine ◀── NEW    │   │  d.mu serialized)   │ ─▶ store
            └──────────────────────────────────┘   └─────────────────────┘

  catacomb run -- <cmd...>  ─┐ tee child stdout → terminal
  catacomb ingest stream-json├─ streamed-POST NDJSON ─▶ POST /v1/stream-json
                  (stdin)   ─┘
```

The daemon remains the **single reducer owner**: one global `seq` source
(`daemon.go:312` `next()`), one `d.graphs` shard map, one `d.execBySession` resolution,
one recover/quarantine boundary. The wrapper and the tailer never build their own graph
(ADR-0001: the daemon observes; the CLI forwards). The daemon **never spawns Claude** —
`catacomb run` is a user-invoked wrapper, the daemon is observe-only.

## 4. Source A — stream-json

### 4.1 Transport: `POST /v1/stream-json` + two thin forwarders

A new token-gated route is registered on the existing mux (`daemon/server.go:23-35`),
alongside `POST /hook/{type}` and `POST /v1/traces`, behind the same `authed` middleware
(`daemon/server.go:55`, bearer token, ADR-0013):

```go
mux.HandleFunc("POST /v1/stream-json", d.authed(token, d.handleStreamJSON))
```

The body is **NDJSON streamed from the request**: `handleStreamJSON` reads the request
body with a `bufio.Scanner` (1 MiB initial / 16 MiB max buffer, matching
`ingest/jsonl.go:56`), and for each non-empty line calls `d.IngestStreamJSON(line,
sessionID)`. This is a long-lived streaming request, not POST-per-line: a single
`catacomb run` invocation holds one request open for the lifetime of the child process,
which matches the tee-as-you-go shape and avoids per-line request overhead. The handler
returns HTTP 200 with an empty body on clean stream end, HTTP 400 on a body read error.
A per-request `recover()` (mirroring `handleHook`) ensures one malformed stream never
crashes the daemon.

The `session_id` anchor is resolved **per line** (a `system.init` line carries it; later
lines repeat it), so a single stream is correctly attributed even before the daemon has
seen `system.init`. Lines with no resolvable `session_id` yet are buffered against an empty
key and re-attributed is **not** attempted — instead each line carries its own
`session_id` (stream-json repeats it on every envelope, original spec §3 row 3), so the
handler extracts it line-by-line and passes it to `IngestStreamJSON`.

**Two CLI forwarders** (both added to `newRootCmd`, `cmd/catacomb/root.go:5`; both read the
daemon address+token from the discovery file via `daemon.ReadDiscovery`, exactly like
`catacomb hook`, `cmd/catacomb/hook.go:28`):

- **`catacomb ingest stream-json`** — reads NDJSON from stdin, opens one streamed POST to
  `http://<addr>/v1/stream-json`, copies stdin into the request body, fails open with a
  local warning if the daemon is down (the hook-forwarder contract,
  `cmd/catacomb/hook.go:34-55`).
- **`catacomb run -- <cmd...>`** — execs `<cmd...>` (e.g.
  `claude -p --output-format stream-json`), wires the child's stdout through an
  `io.TeeReader` so it is **both** written to the user's terminal (`os.Stdout`) **and**
  copied into the streamed POST body. Child stderr/stdin pass through. Sets
  `CATACOMB_RUN_ID` in the child environment (inherited by every child session) for
  multi-session grouping (original spec §5.4, §12); if `--run-id` is given it is sugar for
  that env var. The wrapper exits with the child's exit code. If the daemon is down the
  child still runs and tees to the terminal; only forwarding is skipped (fail-open).

**Why a new endpoint and not the daemon spawning Claude:** keeps the daemon the single
reducer owner with one `seq` and one shard map; reuses the authed mux, recover/quarantine,
and lazy-shard-reload; keeps `catacomb run` a dependency-light forwarder. The daemon
spawning Claude is rejected by ADR-0001 (observe-only; non-goal original spec §2).
`catacomb run`
building its own self-contained graph is rejected (would duplicate the reducer and split
the forest).

### 4.2 Parser contract (`ingest/streamjson`)

New package `ingest/streamjson` with a single exported entry point mirroring
`ingest/otel.Parse` (`ingest/otel.go:18`) and `ingest/hook.Parse` (`ingest/hook.go:33`):

```go
func Parse(line []byte, executionID string, nextSeq func() uint64) ([]model.Observation, error)
```

The function is pure (no I/O). One NDJSON line yields zero or more observations, each with
`Source: model.SourceStreamJSON`, `RunID = session_id` (from the envelope),
`ExecutionID = executionID`, `Seq = nextSeq()` (the daemon's global seq),
`ObservedAt = nowFn().UTC()` (ingest time, ADR-0018), and `EventTime` = the envelope
timestamp when present (else `ObservedAt`). The package follows the `var nowFn = time.Now`
seam (`ingest/otel.go:16`, `ingest/hook.go:31`). An unknown/unrecognized envelope `type`
yields **zero observations, no error** (graceful degradation, never a quarantine — the
original spec §17 "single schema module" rule).

The `assistant` / `user` content-block decoding in stream-json is the same shape the JSONL
parser already implements (`ingest/jsonl.go` `decodeContent` / `userParts` /
`assistantParts`). M2 **duplicates and isolates** the block decoder inside
`ingest/streamjson` rather than sharing it: the original spec §6.3 / §17 mandate
"isolate envelope parsing behind a single schema module per source", and the
no-comments / 100%-coverage rules make two small, independently-fixtured decoders clearer
to verify than one shared helper threaded through two packages. (If a future refactor wants
DRY, a shared decoder in an internal package is a clean follow-up; not M2.)

### 4.3 Envelope → observation-kind mapping

The parser reuses the **existing** observation kinds the reducer already switches on
(`reduce/reduce.go:27-69`), so the happy path needs no new reducer kinds:

| stream-json envelope | observation kind(s) | correlation / attrs it owns | notes |
|---|---|---|---|
| `system` (subtype `init`) | `session_start` | `session_id`, `model` (→ `Attrs["model"]`) | anchors `session_id`→execID; enriches the session node |
| `assistant` (+ `tool_use` blocks) | `assistant_turn` + one `assistant_tool_use` per `tool_use` block | `message.id`→`MessageID`; per block `tool_use_id`→`ToolUseID`, `name`→`Attrs["name"]`, `input`→`Payload.Input`; `usage`→`Attrs["tokens_in"/"tokens_out"]` | same shape as JSONL `assistantParts`; `tool_use_id` is the linchpin |
| `user` (+ `tool_result` blocks) | one `tool_result` per `tool_result` block (+ `user_prompt` if text present) | `tool_use_id`→`ToolUseID`, `content`→`Payload.Output`, `is_error`→`Attrs["status"]` (`ok`/`error`) | same shape as JSONL `userParts` |
| `stream_event` | (structural) `assistant_tool_use` carrying only `Correlation.ParentToolUseID` (+ `ToolUseID`/`UUID` when present) | `parent_tool_use_id`→`ParentToolUseID`, `uuid`→`UUID` | the subagent-boundary signal; consumed by the new structure path (§6.4) |
| `result` | `assistant_turn` enrichment | `usage`→`Attrs["tokens_in"/"tokens_out"]`, cost→`Attrs["cost_usd"]` | mid-tier tokens (below OTel, above JSONL) — §6 |

`mcp_call` typing is automatic: a `tool_use` whose `name` matches `mcp__<server>__<tool>`
is retyped `NodeMCPCall` by the existing `applyTool` path (`reduce.go:84,226`) — no
stream-json-specific MCP handling.

### 4.4 Daemon ingest method

`Daemon.IngestStreamJSON(line []byte, sessionID string) error` mirrors
`Daemon.IngestOTLP` (`daemon.go:175`) exactly:

1. Take `d.mu`; install a `recover()` that quarantines the raw line and sets `err = nil`
   (fail-open, ADR-0019).
2. Resolve `execID` from `sessionID` via `d.execBySession` (mint a new ULID via
   `d.newExecID` if unknown), exactly as `ingestLocked` / `IngestOTLP` do.
3. Call the parser through a `streamParseFn` package var (a seam mirroring
   `parseFn = otelingest.Parse`, `daemon.go:31`) so the parse-error branch is coverable.
   On parser error: quarantine the line, return nil.
4. Lazy-reload the shard from `store.ObservationsForExecution` if the exec is known but not
   in memory (the `IngestOTLP` pattern, `daemon.go:205-218`).
5. For each observation, `applyAndPersist(g, o)` (already under `d.mu`; stamps nothing new
   — `seq` is already set by the parser via `d.next`) and update `d.lastSeen`.

Because the parser is called once per line and each call advances `d.next`, the global,
gap-free total order (ADR-0010/0018) is preserved across hooks, OTel, stream-json, and the
tailer.

### 4.5 [VERIFY] stream-json field uncertainties (Step 7)

The envelope is officially undocumented. The parser uses these best-known names; each is
marked **[VERIFY]** for operator capture (`claude -p --output-format stream-json --verbose
--include-partial-messages`) at end-of-roadmap:

| Concern | Assumed shape (best-known) | Uncertainty |
|---|---|---|
| envelope discriminator | top-level `type` ∈ {`system`,`assistant`,`user`,`stream_event`,`result`} | exact set; whether `--include-partial-messages` adds types **[VERIFY]** |
| `system.init` session | `session_id` (top-level) | top-level vs nested under `system` **[VERIFY]** |
| `parent_tool_use_id` placement | on `stream_event`; also checked on `assistant`/`user` | whether it rides only on `stream_event`; snake-case spelling **[VERIFY]** |
| assistant message id | `message.id` (the `msg_*`) | nesting under `message` **[VERIFY]** |
| token usage | `result.usage.input_tokens` / `output_tokens` (+ `assistant` `message.usage`) | field names; cache fields presence **[VERIFY]** |
| cost | `result.usage.cost_usd` or `result.total_cost_usd` | whether cost is present at all; key name **[VERIFY]** |
| tool blocks | `content[]` blocks `{type, id, name, input}` / `{type, tool_use_id, content, is_error}` | identical to JSONL block shape assumed **[VERIFY]** |

Unknown types and missing fields degrade to zero-observation / null, never to a hard parse
error (consistent with the hook and OTel adapters).

## 5. Source B — JSONL tailer

### 5.1 What it adds on top of the existing parser

The offline parser (`ingest/jsonl.go`, driven by `catacomb replay`,
`cmd/catacomb/replay.go:61`) already maps transcript records → observations. The tailer is
the stateful "feed only new lines, durably, across restarts" machinery wrapped around it.
It lives in a new package (e.g. `ingest/jsonl/tail` or `tailer`) and is wired into the
daemon (§5.8). It must provide: a polling watcher, a persisted byte-offset cursor,
rotation/truncation handling, partial-line buffering, transcript-dir discovery,
`session_id`→execID mapping, EOF≠closed semantics, self-exclusion, and `lossy` flagging.

### 5.2 Polling watcher (pure-Go, no fsnotify)

The watcher is an **injected-clock ticker** (default interval e.g. 500 ms, configurable),
not fsnotify. Rationale (binding): AGENTS.md "pure Go, no cgo … stdlib first; minimal
dependencies"; the original spec's "simplest thing that works". The §4 architecture diagram
in the original spec says "fsnotify" but that is non-binding illustration; no ADR mandates
it. fsnotify is pure-Go but adds a dependency with per-OS backends and edge cases (missed
events under rename storms) that need a rescan fallback anyway — and a poll *is* that
rescan. JSONL is already a laggy disk source (original spec §3), so sub-second latency buys
little.

Each tick the watcher:

1. `os.Stat`s every known file; if size grew, reads `[cursor, size)` (see §5.3–5.5).
2. Every N ticks (a coarser discovery cadence) re-globs the transcript dir for new session
   files (§5.6).

All I/O (stat, open, read, glob) and the clock go through injected seams (an `fs`-like
reader + a `nowFn`/ticker + a glob fn), mirroring the `os.*` indirection already used in
`daemon/discovery.go:19-24`, so tests run on a temp FS with a hand-driven clock — no real
timers, no real `~/.claude`. If the controller ever wants fsnotify it sits behind the same
seam, keeping tests clock-driven.

### 5.3 Byte-offset cursor + the `tail_cursors` table

Per watched file the tailer keeps a **byte offset** = bytes consumed up to and including
the last complete (newline-terminated) line. The cursor is persisted in a **new
`tail_cursors` SQLite table** so a daemon restart resumes mid-file rather than re-ingesting
the whole transcript and without missing lines appended while the daemon was down:

```sql
CREATE TABLE IF NOT EXISTS tail_cursors (
  path   TEXT PRIMARY KEY,
  offset INTEGER,
  inode  INTEGER,
  dev    INTEGER,
  size   INTEGER,
  mtime  INTEGER
);
```

Two new `Store` methods are added to the interface (`store/store.go:5`) and the SQLite
implementation (matching the one-table-per-concern DDL style, `store/sqlite.go:18-29`):

```go
LoadTailCursors() ([]model.TailCursor, error)
UpsertTailCursor(c model.TailCursor) error
```

`model.TailCursor{Path, Offset, Inode, Dev, Size, Mtime}` is a new small struct in
`model`. The cursor is written in the **observation-transaction discipline**: the cursor
for a file is advanced **only after** the observations covering those bytes have durably
committed via `applyAndPersist`. The cursor table is **derived state**, like the
materialized graph (ADR-0002): the observation log remains the system of record, and a
full rebuild re-reduces from observations (the cursor is a tailing cache, not a source of
truth). On boot the tailer `LoadTailCursors()` and seeds its in-memory state.

**Dedup is offset-based, not obs_id-based:** the tailer resumes from the persisted offset
and never re-reads consumed bytes, so a given on-disk line is parsed at most once in the
normal path. (A crash in the narrow window between "append obs" and "advance cursor" can
re-feed one line; because the reducer is an idempotent upsert by canonical id, a replayed
identical line is harmless to the graph — it appends a duplicate obs row + `Sources` entry,
which is the documented residual of ADR-0010 clause 4, out of scope §2.)

### 5.4 Rotation / truncation detection

On each stat the tailer compares against the persisted cursor:

- **size < cursor.offset** (file shrank) → truncation/rewrite: reset `offset = 0`, re-read
  from the start, and mark the run `lossy` (§8).
- **inode/dev changed** (file rotated/recreated; unix `syscall.Stat_t`) → reset `offset =
  0`, re-read, mark the run `lossy`.

Cross-platform: inode/dev are read on unix. Windows has no stable inode — the fallback is
size-shrink detection plus a first-N-bytes content fingerprint stored alongside the cursor;
this fallback is **[VERIFY]**-marked for operator confirmation on Windows. (Claude Code
transcripts are generally append-only, but compaction can rewrite a file in place, so the
defensive reset matters.)

### 5.5 Partial trailing line

A poll may read a half-written final line (the writer is mid-flush). The tailer reads
`[cursor, size)`, then advances the cursor **only past the last `\n`**; any bytes after the
last newline are an incomplete line, buffered in memory and prepended to the next read.
Only complete lines are handed to `ingest/jsonl.Parse`. (The existing `bufio.Scanner` in
the offline parser splits on `\n` but does not report whether the final chunk was
newline-terminated, so the tailer tracks "bytes up to the last `\n`" itself — it does not
delegate offset accounting to the scanner.)

### 5.6 Transcript-dir discovery

The tailer globs the transcript dir for session files and subagent files:

- `<transcript-dir>/<encoded-cwd>/*.jsonl` — main session transcripts
  (`<sessionId>.jsonl`).
- `<transcript-dir>/**/subagents/agent-*.jsonl` — subagent transcripts (original spec §3
  row 4 paths).

The default transcript dir is `~/.claude/projects` (resolved via the same `os.UserHomeDir`
seam as discovery). It is overridable by the `--transcript-dir` daemon flag (§5.8). New
files discovered on a later tick start at `offset = 0`. The filename `<sessionId>.jsonl`
supplies the `session_id`, mapped to `execID` via the daemon's `d.execBySession` so a tailed
session that also produced hooks/OTel/stream-json collapses onto the **same execution
shard** and the `tool_use_id` linchpin merges nodes (original spec §5.5 — the whole point).

### 5.7 `ingest/jsonl.Parse` seam generalization

Today `ingest/jsonl.ParseReader(r, executionID)` (`ingest/jsonl.go:54`) stamps its **own**
local `seq` starting at 0 and sets `EventTime == ObservedAt == transcript timestamp`
(`ingest/jsonl.go:59,80-81`). That is correct for the offline `replay` batch (its own store
via `Persist`), but the live tailer runs **inside the daemon** and must use the daemon's
global, gap-free `seq` (ADR-0010/0018) so its observations interleave correctly in the one
total order, and must record `ObservedAt = ingest time` (ADR-0018), keeping `EventTime` =
the transcript record timestamp.

M2 adds a `nextSeq func() uint64` seam **and** a clock seam to the parser, keeping `replay`
behaviour identical:

```go
func Parse(r io.Reader, executionID string, nextSeq func() uint64) ([]model.Observation, error)
```

- The live tailer passes `d.next` (the daemon's global counter) and the package `nowFn`
  supplies `ObservedAt`; `EventTime` stays the parsed transcript timestamp.
- `replay` passes a local closure (a counter starting at 0) so its `seq` sequence is
  byte-identical to today, and — to keep `EventTime == ObservedAt` exactly as the offline
  path does now — the offline call sets `ObservedAt` from the transcript timestamp. This is
  expressed by having `replay` pass its own no-op-clock behaviour, i.e. the parser sets
  `ObservedAt = EventTime` when called in the offline mode and `ObservedAt = nowFn()` for
  the live tailer. The two modes are distinguished by an explicit parameter (the parser
  takes an `observedAt func(eventTime time.Time) time.Time` resolver, or equivalently two
  thin entry points sharing one core) so `replay`'s golden output is unchanged. The
  existing `ParseReader(r, executionID)` is retained as a thin wrapper over the new
  `Parse` for `replay`, so the offline call site and its tests do not change.

The live tailer feeds lines one record-batch at a time (it controls offsets), so it calls
`Parse` per complete line (or per small batch of complete lines) with the same
`nextSeq`/clock seams, then routes each observation through `applyAndPersist`.

### 5.8 Daemon wiring

The tailer is started in `Daemon.Serve` (`daemon/server.go:145`) as a supervised goroutine
under the `Serve` context, mirroring `reapLoop` / `startExporter`
(`daemon/server.go:75,98`):

```go
go d.tailLoop(ctx)
```

`tailLoop` runs the poll ticker, performs watching/cursor/glob **outside** `d.mu` (file
I/O is not under the lock), and for each complete new line calls a daemon method that takes
`d.mu`, resolves the shard, parses, and `applyAndPersist`s — exactly the single-mutex
discipline M1 established (only the ingest call takes the lock). The whole loop is wrapped
in `recover()` + backoff (ADR-0019): a panic on a malformed transcript quarantines the line
and restarts the loop, never crashing the daemon.

A new `--transcript-dir` flag is added to `newDaemonCmd` (`cmd/catacomb/daemon.go:33-37`),
threaded through `runDaemonWith` and a `Daemon.SetTranscriptDir(string)` setter (mirroring
`SetOTLPEndpoint`). An empty value disables the tailer (the M1 endpoint-disabled pattern,
`server.go:102`). Following the ledger note preferring CLI flags over a YAML loader, the
exclusion knob (§7) is also a flag (`--transcript-exclude`, repeatable glob), with a
default cwd self-exclusion always applied.

## 6. Reducer four-source precedence generalization

### 6.1 The current binary mechanism

M1 left the reducer **OTel-vs-rest binary** while the table already named four tiers:

- `sourceRank` returns 1 for OTel, 0 for everything else (`reduce.go:126-131`).
- `applyTokens` is gated by an OTel bool `haveOTelTokens` (`reduce.go:197-211`) — it does
  not rank stream-json above JSONL.
- `mergePayload` is last-writer-wins per field with no source rank (`reduce.go:180-195`).
- `Correlation.ParentToolUseID` is declared (`model/model.go:56`) but has **no consumer**.
- timing in `stamp` uses `sourceRank`, so hook/JSONL/stream-json all tie at rank 0
  (earliest-time wins among them).

M2 generalizes this to the full per-field-group ordering of the original spec §5.1 / M1
spec §5.1 now that stream-json is a live producer (and the tailer makes JSONL live). The
existing `(source-rank, seq)` stamp mechanism is **extended, not replaced**, so the merge
stays commutative (the original spec §16 commutativity invariant): a per-field-group rank
plus the `seq` tiebreak remains order-independent.

### 6.2 The concrete per-field four-source ordering

This is the binding table M2 implements (high → low; tiebreak `seq`, ADR-0018, never
wall-clock):

| Field group | Source precedence (high → low) | Tiebreak | Reducer change |
|---|---|---|---|
| **Structure** (`parent_id`, edge) | JSONL → OTel (if span has children or `tool_use_id`, the #53954 gate) → OTel → stream-json `parent_tool_use_id` → hook heuristics | `seq` | new `parent_tool_use_id` consumer (§6.4) + a structure-rank guard on edge creation |
| **Timing** (`t_start`, `t_end`, `duration_ms`) | OTel → hook → JSONL → stream-json | `seq` | `sourceRank` → full 4-tier order; `stamp` already consults it |
| **Cost/tokens** (`tokens_in/out`, `cost_usd`) | OTel → stream-json `result.usage` → JSONL | `seq` | `applyTokens`: replace the OTel bool with a 3-tier token rank stamp |
| **Payload** (`input`, `output`) | hook / JSONL (full) → stream-json (deltas) | `seq` | `mergePayload`: add a payload-source-rank guard so a stream-json delta never overwrites a hook/JSONL full payload |
| **MCP attrs** (`mcp_server_name`, `mcp_tool_name`) | OTel `mcp_*` → hook mcp fields → name-pattern (`mcp__<srv>__<tool>`) | `seq` | none — `isMCP` name-pattern already runs for any source (`reduce.go:84,226`) |
| **Name** (`name`) | first non-empty from any source | `seq` (earliest) | none — `setName` is already source-agnostic earliest-seq (`reduce.go:168-178`) |
| **Status** | governed by the lattice, not this table (§5.2 of M1) | — | none — lattice + cascade complete (M1b); JSONL just *produces* `ok`/`error` from `tool_result` and provisional statuses (deferred §11) |

For the **timing** row the original/M1 §5.1 table ordered the non-OTel tiers
`hooks → JSONL → stream-json`; M2 makes that explicit in `sourceRank` so they no longer tie
at 0. (This is the one row where M1's binary collapse lost ordering; M2 restores it.)

### 6.3 Extending the stamp mechanism (kept commutative)

`fieldStamps` (`reduce.go:133-139`) gains per-group rank fields so each field-group records
the rank of the source that last set it, and a lower-ranked later observation never wins:

- `sourceRank(s model.Source) int` becomes the full timing order:
  `OTel=3, hook=2, JSONL=1, stream_json=0` (used by `stamp` for timing).
- A new `tokenRank(s) int` (`OTel=2, stream_json=1, JSONL=0`) replaces the `haveOTelTokens`
  bool in `applyTokens`; the function stamps `tokenRank` and only overwrites tokens when the
  incoming rank ≥ the stored rank (ties → `seq`, latest wins via the existing `Rev`/seq
  flow).
- A new `payloadRank(s) int` (`hook=JSONL=1, stream_json=0`) guards `mergePayload`: a
  stream-json delta (rank 0) never overwrites a stored rank-1 payload field; equal rank →
  the existing field-non-empty merge stands.
- A `structureRank` guard governs the new edge path (§6.4).

Commutativity holds because each guard is a pure function of (incoming source-rank, stored
source-rank, `seq`) with no dependence on arrival order: the highest-rank value wins, ties
break by `seq`, and `seq` is the global persisted order. New branches → new table-driven
tests asserting, for each field group, that all permutations of a multi-source observation
set produce the identical final node (the M1b commutativity property-test pattern).

### 6.4 The `parent_tool_use_id` / subagent-tree structure path

This is the JSONL/stream-json payoff and the only genuinely new reducer logic. Two
producers feed it:

- **stream-json `stream_event`** carries `parent_tool_use_id` (the SDK subagent boundary).
- **JSONL** carries the subagent structure on disk: `parent_tool_use_id` (when present),
  `isSidechain:true` inline records, and separate `subagents/agent-<agentId>.jsonl`
  transcript records.

The reducer gains a path that, when an observation carries `Correlation.ParentToolUseID`,
upserts a `parent_child` edge from the parent `tool_call` node
(`model.ToolCallID(execID, parentToolUseID)`) to the child node, **subject to the structure
precedence rank** (§6.2): JSONL-derived structure outranks an OTel collapsed tree, which
outranks a stream-json `parent_tool_use_id` edge, which outranks hook heuristics. The
existing `upsertEdgeGated` (`reduce.go:72-79`, the #53954 OTel gate) is extended to consult
the structure rank before promoting an edge, so a lower-ranked source never re-parents over
a higher-ranked edge.

Subagent **nodes**: a subagent transcript record (`isSidechain` / a
`subagents/agent-<agentId>.jsonl` file) produces a `subagent` node keyed
`model.SubagentID(execID, agentID)` (the existing `applySubagent` path,
`reduce.go:110-124`, already handles the node; M2's JSONL parser becomes a *producer* of
`subagent_stop`/subagent observations from the transcript instead of only from the
`SubagentStop` hook). The subagent attaches to its spawning `tool_call` via the
`parent_tool_use_id` edge above when the `meta.json` `toolUseId` / `parent_tool_use_id` is
known; otherwise it attaches to the session node (the existing fallback,
`reduce.go:123`).

DEFERRED (note as future, §11): interruption (`interruptedMessageId`), regeneration/edit
branches (`leafUuid` → `superseded`), compaction re-stitching, and the full conversation
`parentUuid` tree. M2 delivers the **subagent parent→child tree only** — the headline
backfill for issue #53954.

### 6.5 [VERIFY] JSONL transcript field uncertainties (Step 7)

The transcript schema is undocumented and version-fragile (original spec §17, ADR-0009).
The existing parser already reads `type`/`uuid`/`sessionId`/`timestamp`/`message`/`content`
blocks; M2 adds best-known threading/subagent field names, each **[VERIFY]**:

| Concern | Assumed field (best-known) | Uncertainty |
|---|---|---|
| subagent boundary | `parent_tool_use_id` on records; `toolUseId` in `agent-<agentId>.meta.json` | exact key; meta-file presence/shape **[VERIFY]** |
| sidechain layout | `isSidechain:true` inline vs separate `subagents/agent-*.jsonl` | which layout the operator's version emits **[VERIFY]** |
| subagent id | `agentId` (the `agent-<agentId>` file id) | spelling; vs `agent_id` **[VERIFY]** |
| tool-result error | `is_error` on `tool_result` blocks (already used) | confirmed name; kept **[VERIFY]** with the rest |
| token usage | `message.usage.input_tokens` / `output_tokens` (already used) | cache fields not consumed **[VERIFY]** |

Deferred threading fields (`parentUuid`, `leafUuid`, `interruptedMessageId`,
`logicalParentUuid`, `promptId`) are **not consumed in M2** (§11) and are noted here only so
the future producer knows they are undocumented too.

## 7. Self-exclusion, loop-break, and lossy

### 7.1 Self-exclusion (dogfooding loop-break, ADR-0019 clause 4)

**Binding** (ADR-0019, original spec §13/§17). When Catacomb is dogfooded on the very
Claude Code session developing it, the tailer would ingest Catacomb's own dev transcript →
a feedback loop / runaway growth. The tailer **excludes Catacomb's own session/project dir
by default**:

- **Default cwd exclusion:** skip the `<encoded-cwd>` directory matching Catacomb's own
  working directory (the daemon's cwd), so the developer's own session is not tailed. This
  is the robust default.
- **Explicit excludes:** a repeatable `--transcript-exclude <glob>` flag adds glob
  patterns over the discovered paths (opt-in to *include* by narrowing/clearing the
  default).
- Self-exclusion also covers Catacomb's own DB and any output it writes (never tail
  `catacomb.db` or files under the daemon's own state dir).

This is the JSONL half of the loop-break; the OTLP exporter already has the sibling guard
(`export/otlp` self-loop refusal, M1 §7.2). The tailer never tails Catacomb's own output.

### 7.2 `lossy`-run flagging (ADR-0019 clause 3)

**Binding** (ADR-0019, original spec §13/§17). When the tailer detects it may have lost
data, the affected **run is flagged `lossy`** so consumers know the graph is incomplete,
not authoritative. M2 sets `lossy` on **any detected gap**:

- rotation/truncation past the cursor with unread content (§5.4),
- a parse-quarantine mid-file,
- a read that the watcher cannot reconcile against the cursor.

Representation: `lossy` rides in `Run.Meta["lossy"] = true` (with a count, e.g.
`Run.Meta["lossy_gaps"]`), using the existing `model.Run.Meta` map (`model/model.go:135`)
— no new first-class `Run` field, no migration. It is surfaced via a new `lossy_runs`
counter in `Metrics` / `metricsSnapshot` (`daemon.go:262-302`, `GET /metrics`,
`server.go:30`), alongside `quarantined`. Because `Run.Meta` is part of the persisted run
body and runs flow through `UpsertRun` in `applyAndPersist`, the flag survives restart.

## 8. Decomposition

Two source plans, ordered M2a → M2b, mirroring the M1a/b/c split (each independently
testable to 100% coverage before the next). M2a is smaller and dependency-free and
re-exercises the "new live ingress source" pattern once; M2b is the heavier stateful
restart-aware one and relies on M2a's precedence generalization being in place. They share
no code beyond `model` / `reduce`.

### 8.1 M2a — stream-json source + reducer four-source precedence generalization

**Deliverables:**

- `ingest/streamjson.Parse` (pure; NDJSON line → observations) with a `testdata/*.ndjson`
  fixture corpus covering `system`/`assistant`/`user`/`stream_event`/`result` and unknown
  types. 100% covered.
- `POST /v1/stream-json` handler (streamed NDJSON body, per-request `recover()`) on the
  existing authed mux.
- `Daemon.IngestStreamJSON` mirroring `IngestOTLP` (recover/quarantine, `execBySession`
  resolution, lazy reload, `streamParseFn` seam, `applyAndPersist`).
- `catacomb ingest stream-json` (stdin → streamed POST, fail-open) and `catacomb run --
  <cmd...>` (exec + tee stdout + forward + `CATACOMB_RUN_ID`/`--run-id`), both seamed
  (subprocess factory + HTTP client) for tests without a real `claude` or daemon.
- Reducer per-field four-source precedence (§6): `sourceRank` 4-tier, `tokenRank`,
  `payloadRank`, the `parent_tool_use_id` structure consumer + structure-rank guard on
  `upsertEdgeGated`. New branches → commutativity tests.
- Deps: **none** (stdlib NDJSON via `encoding/json` + `bufio`; `os/exec` + `io.TeeReader`
  for the wrapper).

**Independence test:** feed a fixture NDJSON line whose `tool_use` block carries
`tool_use_id = "toolu_x"`, plus a hook `PreToolUse` with the same `tool_use_id`, via the
in-process path → assert exactly one merged `tool_call` node (the linchpin, the M1a test
shape). Plus a `stream_event` with `parent_tool_use_id` → assert the `parent_child` edge is
created at the stream-json structure rank, and is **not** allowed to overwrite a
JSONL/OTel edge when one already exists.

### 8.2 M2b — JSONL tailer + subagent tree

**Deliverables:**

- Tailer package: poll watcher (injected clock + FS seams), `tail_cursors` table +
  `LoadTailCursors`/`UpsertTailCursor` store methods + `model.TailCursor`, rotation/
  truncation reset, partial-line buffer, transcript-dir discovery, self-exclusion, `lossy`
  flag, EOF≠closed.
- `ingest/jsonl.Parse` seam generalization (`nextSeq` + clock) with `ParseReader` retained
  as a thin wrapper so `replay` and its golden tests are unchanged.
- Subagent-tree producers in the JSONL parser: `parent_tool_use_id` edges + `isSidechain` /
  subagent-file → subagent nodes (consumed by the §6.4 structure path).
- Daemon wiring: `tailLoop` supervised goroutine in `Serve` (recover/backoff, I/O outside
  `d.mu`, ingest under `d.mu`); `--transcript-dir` + `--transcript-exclude` flags +
  `SetTranscriptDir`; `lossy_runs` in `/metrics`.
- Deps: **none** (poll; pure-Go stat/glob).

**Independence test:** write lines to a temp transcript file across two hand-driven poll
ticks → assert only new lines ingested each tick; persist the cursor, restart the tailer →
assert no re-ingest from the persisted offset; truncate the file → assert `offset` reset,
re-read, and `Run.Meta["lossy"]` set; write a half-line then complete it on the next tick →
assert the partial line is buffered then ingested once. Plus: a subagent transcript with
`parent_tool_use_id` → assert a `subagent` node + a `parent_child` edge to the spawning
`tool_call`.

### 8.3 Independence / ordering

- M2a depends on nothing beyond merged M1 (it adds the precedence generalization that M2b
  then relies on for the structure/timing rows).
- M2b depends on M2a (the four-source stamp extension must be in place so the tailer's
  JSONL structure/timing/token observations rank correctly against stream-json and OTel).
- Optional **M2c** only if the subagent-tree/threading work overflows M2b — carve the
  `parent_tool_use_id`→edge structure path (and any subagent-node work) into its own slice.
  Default: keep it folded into M2a (the stream-json half of the consumer) and M2b (the
  JSONL half).

## 9. Constraints (inherited, binding)

- **Pure Go, no cgo.** Single static cross-platform binary; `modernc.org/sqlite` only.
  This is *why* the tailer polls instead of using fsnotify, and stream-json uses stdlib
  NDJSON — **zero new dependencies** in either M2a or M2b.
- **No comments in Go code** (`internal/codepolicy`). Zero doc/inline/commented-out code;
  only `//go:build` / `//go:embed` / `//go:generate` directives.
- **100% line coverage under `-race`**, TDD-first, threshold never lowered. Every new
  package (`ingest/streamjson`, the tailer package) and every new reducer branch
  (`sourceRank`/`tokenRank`/`payloadRank`/structure path) must be 100% covered → design
  pure functions and inject clock/FS/reader/subprocess/HTTP seams.
- **golangci-lint v2 clean.** No new warnings.
- **Never `go mod tidy`.** Neither M2a nor M2b adds a dependency, so no `go get` is needed;
  if one ever is, `go get <pkg>@latest` one at a time, never `go mod tidy`.
- **Loopback + bearer token ingress** (ADR-0013). `POST /v1/stream-json` sits behind the
  existing `authed` middleware, like `/hook/{type}` and `/v1/traces`.
- **Sequential / total-order reducer** (ADR-0010/0018). `seq` is stamped once at the
  daemon's serialized append boundary (`daemon.go:312` `next()`), global and gap-free. Both
  new sources take `seq` from `d.next` via the `nextSeq` seam — never a parser-local
  counter for the live path. Merge is the tiebreak, never wall-clock.
- **Single-mutex daemon discipline.** `d.graphs` / `d.execBySession` / `d.lastSeen` / bus /
  metrics under `d.mu`. The tailer does file I/O outside the lock; only the per-line ingest
  call takes `d.mu`. The stream-json handler ingests per line under `d.mu` via
  `IngestStreamJSON`.
- **Cross-platform.** Poll-stat + glob are cross-platform; inode/dev rotation detection has
  a Windows size-shrink + fingerprint fallback (§5.4, **[VERIFY]**).
- **Ingest-source `Parse(...) ([]model.Observation, error)` pattern.**
  `ingest/streamjson.Parse` and the generalized `ingest/jsonl.Parse` both follow the pure-
  function `Parse(input, executionID, nextSeq) ([]model.Observation, error)` contract
  shared by `ingest/hook` and `ingest/otel`.
- **`nowFn` injection.** No `time.Now()` in non-test code; both new/changed packages use
  the `var nowFn = time.Now` seam.
- **ADR-0019 fault isolation.** The stream-json handler and the tailer goroutine each run
  under `recover()`; one bad line → a quarantined poison observation + (for the tailer)
  loop restart with backoff, never a daemon crash. Reuse `d.quarantine`
  (`daemon.go:304`).
- **ADR-0002 enrichment / observation-log = system of record.** The graph is rebuildable
  from the obs log; the `tail_cursors` table is derived state (a tailing cache), valid only
  as an optimization — a rebuild re-reduces from observations.

## 10. Testing strategy (M2-specific)

- **Parser fixtures:** `ingest/streamjson` golden tests over a `testdata/*.ndjson` corpus;
  the generalized `ingest/jsonl.Parse` reuses the existing fixtures plus new subagent-tree
  fixtures.
- **Reduction commutativity (extended):** for each field group in §6.2, property tests that
  a multi-source observation set (otel + stream-json + jsonl + hook) yields the identical
  final graph under all permutations — the M1b invariant, now exercising four real sources.
- **Cross-source merge:** the same `tool_use_id` / `message_id` delivered by stream-json +
  hook (M2a) and by the tailer + OTel (M2b) collapses to one node with correct per-field
  precedence and provenance.
- **Tailer state machine:** temp-FS + hand-driven clock tests for append, restart-from-
  cursor, rotation/truncation→reset+lossy, partial-line buffering, discovery of new files,
  and self-exclusion of the cwd dir.
- **Forwarder seams:** `catacomb run` with a fake subprocess + fake HTTP client (tee +
  forward + exit code + fail-open when daemon down); `catacomb ingest stream-json` with a
  fake stdin + fake daemon.
- **Daemon ingest:** `IngestStreamJSON` recover/quarantine branch via the `streamParseFn`
  seam; lazy shard reload; `lossy_runs` in `/metrics`.

## 11. Deferred → M3+ / Step 7

- **Full §5.8 threading** beyond the subagent tree — interruption
  (`interruptedMessageId` → `cancelled`), regeneration/edit branches (`leafUuid` →
  `superseded`, marked never deleted), compaction re-stitching (`logicalParentUuid` /
  `compactMetadata.preservedSegment.headUuid`), and the conversation `parentUuid` tree.
  The reducer status machinery (`superseded`/`cancelled` ranks + transitive cascade) is
  already complete from M1b; only the JSONL *producers* are deferred → M3+ (or an M2c slice
  if it lands early).
- **Offline EOF→`unknown` closure for `replay`** (original spec §5.8 "EOF / inactivity
  TTL / run-end"): for the offline path, EOF means the transcript is complete, so still-
  open `tool_call`s with no terminal should close `unknown`. The live tailer's EOF≠closed
  rule (§5.7/§7) is the only EOF behaviour M2 binds; the offline half is deferred so the two
  do not contradict before both are designed.
- **Idempotent re-ingest / stable per-line `obs_id`** (ADR-0010 clause 4): M2's offset-
  based dedup means a line is never re-read in the normal path; the duplicate-obs-row /
  duplicate-`Sources`-entry edge case after a crash-in-the-window is documented residual,
  to be closed with a content-addressed `obs_id` later.
- **ADR-0017 store format-versioning:** the `tail_cursors` table does not introduce
  `schema_version` / `reducer_version`; remains tech-debt.
- **Shared block decoder** for `ingest/jsonl` ↔ `ingest/streamjson`: M2 duplicates-and-
  isolates (§4.2); a shared internal decoder is a clean later refactor.
- **fsnotify option** behind the watcher seam: only if a future operator needs sub-poll
  latency; the seam keeps it a drop-in without changing tests.
- **[VERIFY] (Step 7):** every field name in the §4.5 stream-json table and the §6.5 JSONL
  table is best-known and undocumented; an operator validates them against a real
  `claude -p --output-format stream-json --verbose --include-partial-messages` capture and
  a real transcript at end-of-roadmap, the same treatment the hook envelope got. The
  fixtures validate the pipeline + the fixtured contract today; Step 7 validates the
  contract against the live wire.

## 12. M2a implementation status

M2a (stream-json source + reducer three-live-source precedence generalization) is
implemented and merged across `ingest/streamjson`, `reduce`, `daemon`, and
`cmd/catacomb`. The §4.3 envelope→kind mapping and the §4.5 field names are coded
against the best-known stream-json schema but **remain [VERIFY] pending an operator
capture in Step 7** (`claude -p --output-format stream-json --verbose
--include-partial-messages`). The reducer precedence generalization landed for the
three live sources (OTel, hook, stream-json); the JSONL tier (§6.2) is inserted by M2b.
