# Codex ingestion stage 1 (import-only) — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task, one fresh implementer subagent per task, review after each task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `catacomb import` ingests OpenAI Codex CLI rollout sessions (main thread + subagent threads, plain and `.jsonl.zst`) into bench-cell-shaped evidence that `verify`/`regress` consume unchanged.

**Architecture:** a new tolerant-reader adapter `ingest/codex` emits the existing `model.Observation` stream (`Source: SourceJSONL`); `cmd/catacomb` dispatches parse/resolution on the basket's new `runtime` field; drift gains a per-runtime tested-version ceiling. Spec of record: [`docs/internal/specs/2026-07-16-codex-ingestion-design.md`](../specs/2026-07-16-codex-ingestion-design.md) — §1 is the complete rollout-format reference, §2 the stage-1 contract. Decision record: [ADR-0031](../../adr/0031-multi-runtime-ingestion-codex.md).

**Tech stack:** Go stdlib + `github.com/klauspost/compress/zstd` (new dependency, pure Go), testify, existing e2e/hermetic bash harness.

## Global constraints

- **No comments in Go code** — none, not even doc comments (`internal/codepolicy` gate).
- **TDD**: failing test first, minimal implementation, refactor under green. 100% file/package/total coverage (`make cover`); the threshold never goes down.
- `gofumpt` + `goimports` (local prefix `github.com/realkarych/catacomb`); `make lint` clean.
- Errors: sentinels + `errors.Is`, wrap as `fmt.Errorf("pkg.Op: %w", err)`. No `any` data types except genuinely open attr bags. Consumer-side interfaces.
- Table-driven tests, `testify/require`/`assert`, no `time.Sleep`.
- Native Codex tool names are preserved verbatim (no normalization onto Claude vocabulary).
- All observations from rollouts use `model.SourceJSONL` and carry `attrs["agent_runtime"]="codex"`, `attrs["codex_version"]=<cli_version>`, `attrs["cwd"]=<cwd>` (mirror of the Claude adapter's stamping loop).
- Commit after every green task; branch `feat/codex-import` from `master` in an isolated worktree.

---

### Task 1: per-runtime drift ceilings (`ingest/drift`)

**Files:**

- Modify: `ingest/drift/drift.go`
- Test: `ingest/drift/drift_test.go` (extend existing)

**Interfaces:**

- Produces: `drift.TestedCodexVersion` (const, `"0.144.4"`), `drift.RuntimeClaudeCode = "claude-code"`, `drift.RuntimeCodex = "codex"` (string consts), `func NewerThanTestedFor(runtime, v string) bool`.
- `NewerThanTested` (Claude-only) remains and delegates: `NewerThanTestedFor(RuntimeClaudeCode, v)`.

- [ ] **Step 1: failing test**

```go
func TestNewerThanTestedFor(t *testing.T) {
	cases := []struct {
		name, runtime, v string
		want             bool
	}{
		{"codex newer", drift.RuntimeCodex, "0.145.0", true},
		{"codex equal", drift.RuntimeCodex, drift.TestedCodexVersion, false},
		{"codex older", drift.RuntimeCodex, "0.133.0", false},
		{"claude delegates", drift.RuntimeClaudeCode, "99.0.0", true},
		{"unknown runtime never warns", "gemini", "99.0.0", false},
		{"empty version", drift.RuntimeCodex, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, drift.NewerThanTestedFor(tc.runtime, tc.v))
		})
	}
}
```

- [ ] **Step 2:** `go test ./ingest/drift/` → FAIL (undefined symbols).
- [ ] **Step 3:** minimal implementation: consts + a `testedFor(runtime string) (string, bool)` switch + `NewerThanTestedFor` guarding empty version/unknown runtime; rewire `NewerThanTested`.
- [ ] **Step 4:** `go test ./ingest/drift/` → PASS; coverage of the package stays 100%.
- [ ] **Step 5:** commit `feat(drift): per-runtime tested-version ceilings`.

### Task 2: `ingest/codex` — Parse core (session meta, prompts, turns)

**Files:**

- Create: `ingest/codex/codex.go`, `ingest/codex/codex_test.go`, `ingest/codex/testdata/basic.jsonl`

**Interfaces:**

- Produces: `func Parse(r io.Reader, mainRunID, executionID string, nextSeq func() uint64, observedAt func(time.Time) time.Time) ([]model.Observation, drift.Counts, error)` — mirrors `ingest/jsonl.Parse` with one addition: `mainRunID` pins `Observation.RunID` for subagent files; when `mainRunID == ""` the file's own `session_meta.session_id` is used.
- Emission contract: spec §2.1 table. This task covers: `session_meta` (identity + stamping), `event_msg:user_message` → `user_prompt` (UUID via `model.PromptUUID`, attr `prompt_kind` via `model.PromptKind`), turn grouping (`turn_context` + `event_msg:token_count` + `event_msg:task_complete` keyed by `turn_id`) → one `assistant_turn` with `Correlation.MessageID = turn_id`, attrs `model`, `tokens_in`, `tokens_out`, `cache_read_in`, `duration_ms`, payload Output = final assistant `message` text of the turn. Unknown record/payload types bump `drift.Counts` (`drift.ReasonUnknownRecordType` / a known-set check per spec §1); undecodable lines are hard errors; bad timestamps bump `drift.ReasonBadTimestamp`. Scanner buffer: 1MiB initial / 16MiB max, like `ingest/jsonl`.
- `testdata/basic.jsonl`: hand-built from the 2026-07-16 probe (spec §1 examples): `session_meta` (cli_version 0.144.4, cwd), `turn_context` (model gpt-5.4-mini, turn_id T1), `event_msg:task_started`, `event_msg:user_message` ("Reply with exactly: hello"), preamble `response_item:message` role=user (environment context — must NOT become a prompt), `response_item:reasoning` (encrypted — skipped, known), `response_item:message` role=assistant "hello" (turn_id T1), `event_msg:token_count` (input 11663, cached 5504, output 16), `event_msg:task_complete` (duration_ms), one unknown record type line.

- [ ] **Step 1: failing test** — table asserting, for `basic.jsonl`: exactly one `user_prompt` (text "Reply with exactly: hello", `prompt_kind` set, correct `PromptUUID`); exactly one `assistant_turn` (MessageID T1, model, tokens_in 11663, tokens_out 16, cache_read_in 5504, duration_ms set, Output payload "hello"); every observation has `RunID` = session id, `Source == model.SourceJSONL`, attrs `agent_runtime == "codex"`, `codex_version == "0.144.4"`, cwd set; drift counts contain exactly one `unknown_record_type`; `Seq` strictly increasing via the injected `nextSeq`.
- [ ] **Step 2:** run → FAIL (package does not exist).
- [ ] **Step 3:** implement: `rolloutLine{Timestamp, Type string, Payload json.RawMessage}` envelope; per-type payload structs; a `turnState` map keyed by `turn_id` flushed on `task_complete` (and at EOF for interrupted sessions — emit the turn with whatever it has); stamping loop identical in shape to the Claude adapter's version/cwd loop.
- [ ] **Step 4:** run → PASS.
- [ ] **Step 5:** error/edge tests in the same table: empty reader, invalid JSON line (hard error), `token_count` with null `info` (keep last non-null), missing `task_complete` (turn still emitted, no duration), timestamp garbage (drift bump). Implement until green.
- [ ] **Step 6:** commit `feat(ingest/codex): rollout parse core — prompts and turns`.

### Task 3: `ingest/codex` — tool calls, MCP, status

**Files:**

- Modify: `ingest/codex/codex.go`
- Test: extend `ingest/codex/codex_test.go`; create `ingest/codex/testdata/tools.jsonl`, `ingest/codex/testdata/mcp.jsonl`

**Interfaces:**

- Produces (per spec §2.1): `response_item:function_call` → `assistant_tool_use` (`ToolUseID: call_id`, `MessageID: turn_id`, attr `name` native, payload Input = decoded `arguments` string as JSON, raw fallback); `function_call_output` → `tool_result` (`status: "error"` iff header line matches `Process exited with code N`, N≠0; else `"ok"`; payload Output = JSON-encoded string); `custom_tool_call`/`_output` same via `input`; `event_msg:mcp_tool_call_begin/end` → `assistant_tool_use`/`tool_result` pair with `name: "mcp__"+server+"__"+tool` (end with error → status error). `spawn_agent`/`wait_agent` stay plain tool calls.
- `tools.jsonl` fixture: exec_command call (arguments `{"cmd":"echo probe-42",...}`) + output ("Process exited with code 0"), an apply_patch `custom_tool_call` + output, a failing exec (`Process exited with code 3`). `mcp.jsonl`: mark-shaped pair (`server:"catacomb"`, `tool:"mark"`, arguments `{"name":"plan","boundary":"start"}`) so the reducer's marker matcher recognizes it downstream.

- [ ] **Step 1: failing tests** asserting node-kind counts, call_id pairing, error status on exit-code 3, `mcp__catacomb__mark` name, decoded-JSON Input payloads.
- [ ] **Step 2:** run → FAIL. **Step 3:** implement (a small `exitCodeRe := regexp.MustCompile(`(?m)^Process exited with code (\d+)$`)`). **Step 4:** run → PASS.
- [ ] **Step 5:** integration check (no new surface): feed `mcp.jsonl` observations through `reduce` in a test under `ingest/codex` is NOT possible (dependency direction) — instead add a `cmd/catacomb`-level test in Task 6 asserting the graph gets an `mcp_call` node. Here just commit.
- [ ] **Step 6:** commit `feat(ingest/codex): tool, custom-tool, and MCP call mapping`.

### Task 4: `ingest/codex` — subagent files and zst opening

**Files:**

- Modify: `ingest/codex/codex.go`; `go.mod`/`go.sum` (add `github.com/klauspost/compress`)
- Create: `ingest/codex/open.go`, `ingest/codex/open_test.go`, `ingest/codex/testdata/child.jsonl` (subagent rollout: `session_meta` with `parent_thread_id` + `agent_role:"explorer"`, one turn), `.zst` fixture generated in-test (compress `basic.jsonl` with the library — no binary fixture committed)

**Interfaces:**

- Produces: `func Open(path string) (io.ReadCloser, error)` — plain file, or zstd-wrapped when `strings.HasSuffix(path, ".zst")` (wrap `*os.File` + `*zstd.Decoder` in a closer struct).
- Parse addition: when `session_meta` carries a parent thread id (top-level or `source.subagent.thread_spawn`), every emitted observation gets `Correlation.AgentID = <file's own session_id>`, `Correlation.SessionID = mainRunID`, and one trailing `subagent_stop` observation is appended (attr `subagent_type` = `agent_role`, fallback `"codex-agent"`) — the shape the reducer already turns into a `subagent` node (see the Claude adapter's sidechain branch).

- [ ] **Step 1: failing tests** — `Open` round-trips a zstd-compressed `basic.jsonl` byte-identically; `Parse(child.jsonl, mainRunID="MAIN", …)` yields observations with `RunID=="MAIN"`, `AgentID==<child id>`, plus a final `subagent_stop` with `subagent_type=="explorer"`.
- [ ] **Step 2:** FAIL → **Step 3:** `go get github.com/klauspost/compress@latest`, implement → **Step 4:** PASS.
- [ ] **Step 5:** commit `feat(ingest/codex): subagent rollouts and zstd transport`.

### Task 5: fuzz + adapter hardening

**Files:**

- Create: `ingest/codex/codex_fuzz_test.go` (mirror `ingest/jsonl`'s parse fuzzer: seed with all testdata lines; property: no panic, error XOR observations)
- Modify: `Makefile` (`make fuzz` gains the target), `.github/workflows/fuzz.yml` (new matrix entry, same 5m budget)

- [ ] **Step 1:** write the fuzz target; `go test -run=^$ -fuzz=FuzzParse -fuzztime=30s ./ingest/codex/` locally clean.
- [ ] **Step 2:** wire Makefile + workflow entry (copy the existing jsonl stanza; actions stay SHA-pinned).
- [ ] **Step 3:** commit `test(ingest/codex): parse fuzzer + weekly fuzz wiring`.

### Task 6: basket `runtime` field + import dispatch (`cmd/catacomb`, `bench`)

**Files:**

- Modify: `bench/basket.go` (field + validation), `bench/basket_test.go`
- Modify: `cmd/catacomb/import.go` (flag `--sessions-dir`, dispatch), `cmd/catacomb/offline.go` (`parseTranscriptsFor`, `warnVersion` per runtime), `cmd/catacomb/bench.go` (reject codex baskets)
- Create: `cmd/catacomb/codextranscripts.go` + `codextranscripts_test.go`
- Modify: `evidence/evidence.go` (`EnvStamps.AgentRuntime`, `EnvStamps.AgentVersion`, `omitempty`), `evidence/evidence_test.go`, `cmd/catacomb/envstamps*.go` (fill fields both runtimes)
- Test: extend `cmd/catacomb/import_test.go`, `import_integration_test.go`, `bench_offline_test.go`

**Interfaces:**

- Consumes: Task 1 `NewerThanTestedFor`; Task 2–4 `codex.Parse`/`codex.Open`.
- Produces: `Basket.Runtime string` (`""`→`claude-code`; sentinel `ErrBasketRuntime` for anything else but `claude-code`/`codex`); `func resolveCodexTranscripts(sessionsRoot, threadID string) (transcriptSet, error)` per spec §2.3 (three-level date glob, `.jsonl`+`.jsonl.zst`, exactly-one-main error parity, first-line child scan, depth-first with cycle guard, sorted); import flag `--sessions-dir` default `~/.codex/sessions`; `parseTranscriptsFor(runtime string, main string, subs []string, mainRunID, executionID string)` in `offline.go` dispatching adapter + version warning (`codex_version` attr / `claude_code_version` attr).
- Bench: loading a `runtime: codex` basket in `runBench` returns the operational error string from spec §2.4 (exit 2).

- [ ] **Step 1: failing basket tests** — `runtime: codex` loads; `runtime: gemini` → `ErrBasketRuntime`; hash changes when runtime changes.
- [ ] **Step 2–3:** implement field + validation → green; commit `feat(bench): basket runtime field`.
- [ ] **Step 4: failing resolver tests** — build a fake `sessions/2026/07/16/` tree in `t.TempDir()` with main + child + grandchild + decoy (other parent) + `.zst` main variant; assert discovery set, order, ambiguity error, zero-match error, cycle guard (two files claiming each other as parents).
- [ ] **Step 5–6:** implement `codextranscripts.go` → green; commit `feat(cmd): codex rollout resolution`.
- [ ] **Step 7: failing import tests** — with a codex basket + fixture rollouts under a fake sessions dir: `import --session-id` writes evidence whose `meta.json` has `AgentRuntime=="codex"`, `AgentVersion=="0.144.4"`, `CostUSD==nil`, labels/run-id parity with ADR-0030; graph contains `mcp_call` node for the mark fixture and `subagent` node for the child fixture (assert via the written `session.jsonl` re-reduced, following the existing `import_integration_test.go` pattern); `--transcript` direct-path variant; version warning fires once for a `cli_version` above the ceiling.
- [ ] **Step 8–9:** implement dispatch + stamps + warning → green; commit `feat(cmd): codex import dispatch + env stamps`.
- [ ] **Step 10: failing bench test** — codex basket → exit 2 with the documented message → implement → green; commit `feat(bench): reject import-only codex baskets`.
- [ ] **Step 11:** `make cover && make lint` — both green (100%).

### Task 7: hermetic E2E scenario

**Files:**

- Create: `e2e/hermetic/prod/scenarios/55-codex-import/` (run.sh + fixture templates, following `50-import-subagent`'s structure)
- Modify: `e2e/hermetic/prod/run.sh` (scenario registration, if list-based)

**Interfaces:**

- Consumes: the shipped `catacomb` binary only (CATACOMB_BIN), fixture rollouts templated from `ingest/codex/testdata` shapes with distinct session uuids per variant/rep.

- [ ] **Step 1:** scenario script: stage a fake `~/.codex/sessions` tree (5 baseline + 5 degraded rollouts: degraded variant has an error tool result + 3x tokens_out), codex basket with `checkpoints: [plan]` and the mark MCP fixture; `import` all 10; `verify`; `regress` A-vs-A (exit 0) and baseline-vs-degraded (exit 1, `tokens_out`/`error_rate` rows); assert `meta.json` `agent_runtime` stamp; assert a `subagent` node via `catacomb export`.
- [ ] **Step 2:** run `e2e/hermetic/run.sh` locally end-to-end → all PASS including scenario 55.
- [ ] **Step 3:** commit `test(e2e): hermetic codex-import production scenario`.

### Task 8: documentation

**Files:**

- Modify: `docs/guide/ingestion.md` (runtimes section: two adapters, per-runtime watchlist, rollout locations), `docs/guide/basket.md` (`runtime` field), `docs/guide/cli.md` (import: `--sessions-dir`, codex notes, bench rejection), `docs/guide/troubleshooting.md` (row: "bench says codex is import-only"), `docs/adr/README.md` (0031 row), `README.md` (one feature-bullet line + requirements note that Codex is import-only stage 1)

- [ ] **Step 1:** write docs; every flag/default cross-checked against the implementation.
- [ ] **Step 2:** `npx --yes markdownlint-cli@0.49.0 '**/*.md' --ignore node_modules` clean; relative links resolve (lychee offline args from `.github/workflows/docs-links.yml` if runnable locally).
- [ ] **Step 3:** commit `docs: codex import-only runtime`.

### Task 9: live validation (local, not CI)

- [ ] **Step 1:** on the maintainer machine (codex 0.144.4 authenticated): run one fresh `codex exec --json` probe in a scratch dir, `catacomb import` it with a real basket, `catacomb regress` an A-vs-A of two probe imports → exit 0; attach the transcript of this check to the PR description.
- [ ] **Step 2:** open PR `feat: codex import-only ingestion (ADR-0031 stage 1)`; CI green (unit 3-OS, cover 100%, hermetic incl. scenario 55, lint, codepolicy, security).

## Self-review checklist (run after Task 8)

- Spec §2 coverage: every contract line maps to a task (dispatch, resolver, stamps, watchlist, fixtures, docs).
- No placeholder steps; every Go snippet comment-free (codepolicy).
- Type consistency: `Parse(r, mainRunID, executionID, nextSeq, observedAt)` signature identical in Tasks 2/3/4/6; `transcriptSet` reused, not redefined.
