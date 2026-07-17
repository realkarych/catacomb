# Codex CLI ingestion — design (stages 1–3)

- **Date:** 2026-07-16
- **Status:** approved design ([ADR-0031](../../adr/0031-multi-runtime-ingestion-codex.md))
- **Related:** [ADR-0030](../../adr/0030-interactive-session-import.md) (import entry point), [ADR-0025](../../adr/0025-capture-format-drift-detection.md) (drift watchlist, amended per-runtime), [ADR-0016](../../adr/0016-cross-run-step-identity-and-annotations.md) (salience projections)
- **Format research:** verified against a local Codex CLI 0.144.4 install (fresh probe sessions) and the `openai/codex` source (`codex-rs/protocol/src/protocol.rs`, `codex-rs/rollout/src/recorder.rs`, `codex-rs/exec/src/exec_events.rs`) on 2026-07-16.

Catacomb ingests OpenAI Codex CLI sessions through the same
observation → reduce → keys → aggregate → regress pipeline it uses for Claude
Code, entering at `catacomb import` (stage 1), gaining gate-quality salience and
pricing (stage 2), and `bench` spawn support (stage 3). This spec is
self-contained: everything an implementer needs about the Codex format is in §1.

## 1. The Codex rollout format (reference)

### 1.1 Location and naming

- One JSONL file per session (thread):
  `$CODEX_HOME/sessions/YYYY/MM/DD/rollout-YYYY-MM-DDThh-mm-ss-<thread-uuid>.jsonl`
  where `CODEX_HOME` defaults to `~/.codex`. The filename timestamp is **local**
  time; timestamps inside are UTC — resolve by uuid glob, never by reconstructed
  timestamp.
- Cold files are background-compressed to `rollout-…jsonl.zst` (zstd). An
  adapter must read both.
- Each **subagent** is its own rollout file; the child's `session_meta` carries
  `parent_thread_id` plus `source: {"subagent": {"thread_spawn": {...}}}`.
- `codex exec --ephemeral` writes no file. `codex exec --json` announces
  `{"type":"thread.started","thread_id":"<uuid>"}` as the first stdout event;
  `thread_id` equals the rollout filename uuid and `session_meta.session_id`.

### 1.2 Line shape

Every line: `{"timestamp":"<UTC ISO-8601 ms Z>","type":"<record>","payload":{...}}`
(newer builds add an optional `ordinal` int). Record types (serde tag `type`,
content `payload`):

| record type | role |
|---|---|
| `session_meta` | first line; session identity |
| `turn_context` | per-turn context (model, cwd, sandbox) |
| `response_item` | conversation items (tagged by `payload.type`) |
| `event_msg` | UI-level events (tagged by `payload.type`) |
| `compacted` | history rewrite marker (`{message, replacement_history}`) |
| `world_state` | environment snapshot (≥0.14x) |
| others (`inter_agent_communication`, …) | tolerated, drift-counted |

### 1.3 `session_meta` payload (fields the adapter reads)

```json
{"session_id":"019f6b85-…","id":"019f6b85-…","timestamp":"…Z",
 "cwd":"/abs/path","originator":"codex_exec","cli_version":"0.144.4",
 "source":"exec","thread_source":"user",
 "parent_thread_id":"<uuid, subagent files only>",
 "agent_nickname":"…","agent_role":"explorer",
 "git":{"commit_hash":"…","branch":"…","repository_url":"…"},
 "base_instructions":{"text":"…"}}
```

`session_id` may be absent on older files (backfill from `id`). `source` is a
string (`"exec"`, `"cli"`) or an object for subagents:
`{"subagent":{"thread_spawn":{"parent_thread_id":"…","depth":1,"agent_role":"…","agent_nickname":"…"}}}`.
Top-level `parent_thread_id` and the `thread_spawn` copy coexist; read either.

### 1.4 `turn_context` payload (fields the adapter reads)

```json
{"turn_id":"019f6b85-…","cwd":"…","model":"gpt-5.4-mini","effort":"low",
 "sandbox_policy":{"type":"read-only"},"approval_policy":"never"}
```

### 1.5 `response_item` payload types

All items in ≥0.14x carry
`"internal_chat_message_metadata_passthrough":{"turn_id":"<uuid>"}` — the turn
attribution. Types the adapter maps:

- `message` — `{type:"message", role:"user"|"assistant"|"developer",
  content:[{type:"input_text"|"output_text", text}], phase?, id?}`. **The first
  turn injects developer/user preamble** (environment context, skills, plugins)
  before the real prompt — a `role:"user"` message is a human prompt only when
  the same text also appears as an `event_msg:user_message` (§1.6).
- `reasoning` — `{type:"reasoning", id?, summary:[], encrypted_content:"…"}`.
  Encrypted; contributes nothing to payloads. Skip silently (known type).
- `function_call` — `{type:"function_call", name:"exec_command"|"write_stdin"|
  "apply_patch"|"spawn_agent"|"wait_agent"|…, arguments:"<JSON-encoded string>",
  call_id:"call_…", id?}`.
- `function_call_output` — `{type:"function_call_output", call_id:"call_…",
  output:"<prose string; header lines include 'Process exited with code N'>"}`.
- `custom_tool_call` / `custom_tool_call_output` — same shape with `input`
  instead of `arguments`; `name` observed: `apply_patch`.
- `mcp_tool_call` family — carries `server`, `tool_name`/`tool`, `arguments`,
  `result`; paired begin/end also arrive as `event_msg` (§1.6). Exact on-disk
  shape unverified — the adapter recognizes it by `payload.type` prefix
  `mcp_tool_call` and by the `event_msg` pair, and a hermetic fixture pins the
  shape we support.
- `tool_search_call`/`tool_search_output`, `web_search_call`, others — known,
  skipped (no node), not drift-counted once listed in the known set.

### 1.6 `event_msg` payload types

- `user_message` — `{message:"<text>"}`; the authoritative human-prompt signal.
- `agent_message` — `{message, phase}`; assistant visible output.
- `task_started` (alias `turn_started`) — `{turn_id, started_at:int}`.
- `task_complete` — `{turn_id, last_agent_message, duration_ms:int,
  time_to_first_token_ms:int}`. Missing when a session was interrupted.
- `token_count` — `{info:{total_token_usage:{input_tokens,cached_input_tokens,
  output_tokens,reasoning_output_tokens,total_tokens}, last_token_usage:{…},
  model_context_window}, rate_limits:{…}}`. `info` may be null. **No dollar
  cost exists anywhere in the format.**
- `mcp_tool_call_begin`/`mcp_tool_call_end` — `{invocation:{server, tool,
  arguments}, call_id}`; end adds a result/error.
- `error`, `session_error`, `stream_error`, `turn_aborted` — error surfaces.
- `context_compacted`, `exec_command_begin/end`, `patch_apply_begin/end`,
  collab events, deltas, approvals — known, skipped.

Turn shape: `task_started` → `turn_context` → items stamped with `turn_id` →
`token_count` → `task_complete`.

### 1.7 Drift posture

No schema-version field exists; the only signal is `cli_version`. Upstream
changes observed within six weeks: `.zst` compression, `ordinal` field,
`world_state`, multi-agent items. The adapter is a tolerant reader: unknown
record types and unknown `payload.type`s bump drift counts and are skipped;
undecodable JSON lines stay hard errors (same policy as the Claude adapter).

## 2. Stage 1 — import-only Codex support

### 2.1 New package `ingest/codex`

Same contract as `ingest/jsonl` so `cmd/catacomb` composes either:

```go
package codex

func Parse(r io.Reader, executionID string, nextSeq func() uint64,
    observedAt func(time.Time) time.Time) ([]model.Observation, drift.Counts, error)
```

Emission rules (all observations: `Source: model.SourceJSONL`, `RunID` = the
**main** session id — see §2.3 for subagent files):

| rollout record | observation kind | correlation | attrs | payload |
|---|---|---|---|---|
| `session_meta` | (none — identity only) | — | contributes `codex_version`, `cwd`, `agent_runtime:"codex"` stamped onto every emitted observation of the file (mirrors the Claude adapter stamping `claude_code_version`/`cwd`) | — |
| `event_msg:user_message` | `user_prompt` | `UUID: model.PromptUUID(sessionID, text)` | `prompt_kind: model.PromptKind(text)` | Input = JSON-encoded text |
| `turn_context` + `event_msg:token_count` + `event_msg:task_complete` (grouped by `turn_id`) | `assistant_turn` | `MessageID: turn_id` | `model`, `tokens_in` (uncached input: max(0, last_token_usage.input_tokens − cached_input_tokens) — OpenAI's `input_tokens` includes cache reads while Anthropic's excludes them, so subtracting keeps pricing correct and Claude-parity), `tokens_out` (output_tokens), `cache_read_in` (cached_input_tokens), `cache_write` (cache_write_input_tokens, omitted when 0; whether `input_tokens` also includes it is unverified upstream, so it is not subtracted), `duration_ms` | Output = JSON-encoded final `message` role=assistant text of the turn, when present |
| `response_item:function_call` | `assistant_tool_use` | `ToolUseID: call_id`, `MessageID: turn_id` | `name` (native: `exec_command`, `apply_patch`, …) | Input = the decoded `arguments` JSON (it arrives as a string; decode once, fall back to raw) |
| `response_item:function_call_output` | `tool_result` | `ToolUseID: call_id` | `status`: `error` when the output header contains `Process exited with code N`, N≠0, or the paired begin/end reported an error; else `ok` | Output = JSON-encoded output string |
| `custom_tool_call`/`_output` | same as function_call pair | same | same | Input = JSON-encoded `input` string |
| `event_msg:mcp_tool_call_begin` | `assistant_tool_use` | `ToolUseID: call_id` | `name: "mcp__<server>__<tool>"` | Input = `arguments` |
| `event_msg:mcp_tool_call_end` | `tool_result` | `ToolUseID: call_id` | `status` from result/error | Output = result JSON when present |
| `function_call` name `spawn_agent`/`wait_agent` | `assistant_tool_use` (a plain tool call in the parent) | as function_call | as function_call | as function_call |
| subagent file (whole) | its observations, plus one `subagent_stop` | `AgentID: <child thread id>`, `SessionID: <main session id>` | `subagent_type: <agent_role or "codex-agent">` | — |
| unknown record / payload type | (none) | — | drift bump | — |

The `mcp__<server>__<tool>` naming makes the existing reducer classify the node
as `mcp_call` and lets `mcp__catacomb__mark` checkpoints work unchanged.

Reader: `bufio.Scanner` with the same 1MiB/16MiB buffer policy as
`ingest/jsonl`; a `.zst` path is wrapped in `zstd.NewReader`
(`github.com/klauspost/compress/zstd`, pure Go — the one new dependency).

### 2.2 Drift: per-runtime watchlist (`ingest/drift`)

```go
const TestedCodexVersion = "0.144.4"

func NewerThanTestedFor(runtime, v string) bool  // runtime: "claude-code" | "codex"
```

`NewerThanTested` stays (Claude path untouched); `cmd/catacomb/offline.go`'s
`warnVersion` generalizes to warn per runtime, reading `codex_version` attrs the
adapter stamps. Warning text mirrors the existing one:
`warning: transcript Codex version X is newer than tested Y`.

### 2.3 Resolution (`cmd/catacomb/codextranscripts.go`)

```go
func resolveCodexTranscripts(sessionsRoot, threadID string) (transcriptSet, error)
```

1. Main: `filepath.Glob(sessionsRoot, "*", "*", "*", "rollout-*-"+threadID+".jsonl")`
   plus the `.jsonl.zst` variant; exactly one match required (same
   zero/ambiguous error shape as `resolveTranscripts`).
2. Subagents: walk the same tree; for each candidate rollout, decode **only the
   first line**; collect files whose `session_meta` `parent_thread_id` (top-level
   or `thread_spawn`) equals `threadID`; recurse for grandchildren (depth-first,
   cycle-guarded). Sorted for determinism.
3. Returns the same `transcriptSet{Main, Subagents}` shape import already uses.

Subagent files carry their own session ids; the adapter is invoked with the
**main** session id as `RunID` for every file (the child's identity lives in
`AgentID`), so the reducer merges them into one run exactly like Claude
sub-transcripts.

### 2.4 Basket surface (`bench/basket.go`)

```go
type Basket struct {
    // existing fields…
    Runtime string `yaml:"runtime,omitempty" json:"runtime,omitempty"`
}
```

- Allowed values `""` (= `claude-code`), `claude-code`, `codex`; anything else
  is `ErrBasketRuntime` at load. The field participates in the basket hash
  automatically (it hashes the file).
- Stage 1: `bench` rejects `runtime: codex` with an operational error
  `bench: runtime "codex" is import-only for now — run the session with codex
  exec and use catacomb import` (tested).

### 2.5 Import wiring (`cmd/catacomb/import.go`)

- New flag: `--sessions-dir` (default `~/.codex/sessions`), used only when the
  basket declares `runtime: codex`; `--projects-dir` keeps its Claude meaning.
- `--session-id` resolves via `resolveCodexTranscripts`; `--transcript` accepts
  a direct rollout path (subagent discovery then scans the transcript's own
  directory tree the same way).
- Parsing dispatches on the basket runtime: `codex.Parse` vs `ijsonl.Parse`
  (a small `parseTranscriptsFor(runtime, …)` shim in `offline.go`).
- Marker synthesis, evidence writing, labels, run-id (`import-…`), cost
  semantics (`CostUSD: nil`) are unchanged from ADR-0030.
- Env stamps: `evidence.EnvStamps` gains `AgentRuntime string` and
  `AgentVersion string` (additive, `omitempty`); the Codex path fills
  `{AgentRuntime: "codex", AgentVersion: <cli_version>}` and leaves
  `ClaudeCodeVersion` empty; the Claude path fills `AgentRuntime:
  "claude-code"` plus the existing field.

### 2.6 Hermetic fixtures and E2E

- Unit fixtures: `ingest/codex/testdata/` — a minimal single-turn rollout, a
  tool-call rollout, an MCP-mark rollout, a parent+child subagent pair, a
  `.jsonl.zst` variant, malformed/unknown-type lines. Derived from the
   2026-07-16 probe sessions (content fully synthetic/probe-generated).
- Hermetic E2E: a new production scenario `prod/scenarios/55-codex-import`
  driving `catacomb import` on a fixture rollout through `verify` + `regress`
  (A-vs-A clean and a seeded degraded pair), asserting graph node kinds
  (`mcp_call` from the mark fixture, `subagent` from the pair) and the
  `agent_runtime` stamp in `meta.json`.

## 3. Stage 2 — gate-quality metrics (separate plan)

- `stepkey`: salience projections for Codex tools — `exec_command` →
  `project(red, "cmd")`, `apply_patch` (function or custom form) → first
  file path parsed from the patch envelope, `write_stdin` → `project(red,
  "session_id")`; everything else falls to `canon` as today. Claude
  projections are untouched (salience changes re-key baselines — the Codex
  additions are behind names Claude never emits, so existing step keys are
  stable; a characterization test pins that). The `apply_patch` directive
  regex (unanchored, greedy path capture) is hash-frozen under `stepkey/v1` —
  tightening it later is a re-keying compatibility decision, not a cleanup.
- `pricing`: OpenAI families (`gpt-5`-prefixed and successors) with per-MTok
  tiers sourced from the published API price list, marked `Source:
  "estimated"`; ChatGPT-plan (credit-billed) runs price the same way with the
  same disclaimer — variant-to-variant deltas stay meaningful even when the
  absolute dollars are notional. Unknown models keep `cost_usd` unpriced,
  exactly like unknown Claude models today.
- Reliability: `token_count.info` can be null mid-stream; the adapter keeps the
  last non-null per turn (stage 1 already does this; stage 2 adds the
  power-test characterization for Codex-shaped groups).

## 4. Stage 3 — bench spawn (shipped as specified below)

- `childlocal.go` peek gains a Codex mode: first `--json` stdout line
  `thread.started` → session id; no terminal cost event exists, so the manifest
  cost stays nil (import parity). The basket contract for `runtime: codex`
  becomes `cmd` must emit the exec JSON stream (`codex exec --json …`), with
  `< /dev/null` guidance (codex reads stdin when not a tty).
- Live E2E: a `basket-codex.yaml` leg in `e2e/`, gated on `codex` being on PATH
  and authenticated (skip otherwise), runnable locally and via workflow
  dispatch; hermetic remains the CI gate.
- Watchlist bump discipline: `TestedCodexVersion` joins the release checklist
  line item ADR-0025 already prescribes for the Claude ceiling.

## 5. Explicit non-goals

- Cross-runtime step-level A/B (step terminals embed runtime tool vocabularies;
  permanently out).
- Parsing Codex desktop-app SQLite stores; the rollout JSONL is the contract.
- Decrypting `reasoning` items (upstream encrypts; nothing to parse).
- A public adapter plugin ABI (two in-tree adapters prove the seam).

## 6. Test plan summary

- `ingest/codex`: table-driven unit tests per record type, tolerant-reader
  drift tests, zst round-trip, subagent stamping, 100% coverage (TDD).
- `cmd/catacomb`: resolver tests (glob, zst, ambiguous, child discovery,
  cycles), import dispatch tests, bench rejection test, env-stamp tests —
  through the existing `import_test.go`/`offline_test.go` patterns.
- `ingest/drift`: per-runtime ceiling tests.
- Hermetic E2E scenario 55 (above); `make cover` gate holds at 100%.
- Fuzz (follow-up to stage 1, same PR if cheap): `FuzzCodexParse` mirroring the
  existing jsonl parse fuzzer.
