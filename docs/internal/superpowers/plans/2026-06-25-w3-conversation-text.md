# W3 — Conversation-Text Inspection Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Persist user/assistant message text into the existing `model.Payload` model during ingestion (`user` text → `Payload.Input`, `assistant` text → `Payload.Output`), wire the reduce merge so those payloads land on `user_prompt`/`assistant_turn` nodes, serve them through the unchanged gated + redacted `GET …/payload` endpoint, and render message-node payloads in the web UI as conversation text instead of raw JSON.

**Architecture:** This extends the existing gated, redaction-aware payload path end-to-end. No new services, routes, or dependencies. The only backend change is at three ingestion sites (`ingest/jsonl`, `ingest/streamjson`) plus two reduce call-sites that already exist but do not yet call `mergePayload`. Message text is JSON-encoded as a string value so `redact.Redact()` scans it exactly like a tool payload. The frontend gains one tiny pure predicate module (vitest-gated) and a rendering branch in `PayloadPanel.svelte`; the JSON branch for `tool_call`/`mcp_call` is preserved verbatim.

**Tech Stack:** Go daemon (SQLite, SSE), Svelte 5 + Vite web UI, vitest + Playwright. No new runtime dependency is introduced (see ambiguity note in Task 6 on markdown).

## Global Constraints

- No comments in Go code — none, not even doc comments. The only allowed comments are the `//go:build`, `//go:embed`, and `//go:generate` directives. Enforced by `internal/codepolicy`. The implementer must write zero comments in every Go file touched.
- Go: 100% test coverage (`make cover`, threshold never lowered), TDD-first, `golangci-lint` 0 issues. Every new branch and line must be covered — this plan specifies tests for redaction-applies, tool-payload-untouched, empty-text (no payload attached), and the reduce merge no-regression cases.
- Frontend: vitest 100% (`perFile`, threshold `100: true`) on the new pure predicate; the new module **must be added to the `coverage.include` list** in `webui/vitest.config.ts` or it is silently un-gated. `PayloadPanel.svelte` is not line-gated (Svelte components are excluded from coverage) but is covered by Playwright e2e.
- `npm run build` output in `webui/dist/` must be rebuilt and committed in sync; CI fails on a stale `dist` (`npm run check:dist`).
- Conventional-commit messages; every commit ends with the trailer `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`.
- This plan doc must itself be markdownlint-clean under `.markdownlint.json` (default true, MD013 off): blank line before and after every list (MD032) and every fenced block (MD031), blanks around headings (MD022), single H1 (MD025), no spaces inside code spans (MD038), escaped `\|` inside table cells (MD056), trailing newline (MD047), `-` bullets (MD004).

## Verified Code Facts

- `model.Payload{Input, Output json.RawMessage; Hash string}` (`model/model.go:65`). `model.HashPayload(p *Payload)` (`model/payload.go:8`) hashes `Input` then `Output` with SHA-256; this is the helper already used for tool payloads in both ingesters. Reuse it; do not invent a new hash.
- `ingest/jsonl/jsonl.go`: `decodeContent()` (line 139) returns the top-level message `text` for string content. `userParts()` (line 154) builds a `user_prompt` partial with **no payload and no attrs**. `assistantParts()` (line 181) builds an `assistant_turn` partial with **no text payload** but does build `&model.Payload{Input: b.Input}` for each `tool_use` block (line 197) — those must stay untouched.
- `ingest/streamjson/streamjson.go`: mirror structure. `userParts()` (line 180) builds `user_prompt` with `attrs: {"prompt": text}` (this attr must be preserved). `assistantParts()` (line 152) builds `assistant_turn` with no text payload; the result-type `assistant_turn` (line 131) carries no message text and must remain payload-free.
- `reduce/reduce.go`: the `user_prompt` case (line 46) and `assistant_turn` case (line 51) **do not currently call `g.mergePayload`** — so even if ingestion attaches a payload it is dropped today. Both cases must add `g.mergePayload(n, o.Payload, o.Source)`. `mergePayload` itself (line 288) needs no change. The `assistant_tool_use`/`tool_result` case (line 65 → `applyTool`, line 112) already calls `mergePayload`.
- `daemon/payload.go`: `buildPayloadView` (line 60) runs `redact.Redact()` over `Input` and `Output` independently and returns a `PayloadView`. `nodePayloadView` (line 31) gates on `d.allowPayloadAccess` (403) and returns `ErrPayloadNotFound` when payload is nil or both fields empty. No route or handler change is required; text flows through unchanged.
- `redact.Redact(raw []byte)` (`redact/redact.go:123`): for `json.Marshal("text")` (a quoted JSON string) it `Decode`s a top-level `string`, walks via `walkNode` → `redactStringValue` (path `""`), and scans the text against every value rule. A secret embedded in message text is redacted. Empty/`""` input returns `Result{Data: raw}` with no findings.
- `webui/web/src/components/PayloadPanel.svelte` receives props `{hash, nodeId, token}` only — it does **not** currently receive node type. `NodeDrawer.svelte` (line 128) renders `<PayloadPanel {hash} nodeId={node.id} {token} />` and has `node.type` in scope.
- `webui/web/src/lib/types.ts`: `Node.type: string` (line 15), `PayloadView` has `input?: unknown; output?: unknown; redacted: boolean; redactions: RedactionFinding[]` (line 71).
- `webui/vitest.config.ts`: coverage is gated only for files in the explicit `coverage.include` array; `web/src/**/*.svelte` is excluded. Threshold `100: true`, `perFile: true`.

---

## Task 1: jsonl ingestion — persist user prompt text into `Payload.Input`

**Files:**

- Modify: `ingest/jsonl/jsonl.go`
- Test: `ingest/jsonl/jsonl_test.go`

**Interfaces:**

- Consumes: `decodeContent(raw json.RawMessage) (string, []block, error)` (unchanged), `model.HashPayload(*model.Payload) string`.
- Produces (unchanged signature, new behavior): `userParts(base model.Correlation, text string, blocks []block) []partial` now attaches `payload: &model.Payload{Input: <json-encoded text>, Hash: …}` to the `user_prompt` partial when `text != ""`.

Steps:

- [ ] Write the failing test first. Add to `ingest/jsonl/jsonl_test.go`:

  ```go
  func TestParseReaderUserPromptTextPayload(t *testing.T) {
  	up := byKind(parseFixture(t), "user_prompt")
  	require.Len(t, up, 1)
  	require.NotNil(t, up[0].Payload)
  	assert.JSONEq(t, `"list files"`, string(up[0].Payload.Input))
  	assert.Empty(t, up[0].Payload.Output)
  	assert.NotEmpty(t, up[0].Payload.Hash)
  }
  ```

- [ ] Run it and confirm it FAILS (payload is nil today):

  ```sh
  go test ./ingest/jsonl/ -run TestParseReaderUserPromptTextPayload
  ```

  Expected: FAIL with a nil-pointer or `Payload` nil assertion failure.

- [ ] Implement. In `ingest/jsonl/jsonl.go`, replace the body of `userParts` so the `user_prompt` partial carries the encoded-text payload:

  ```go
  func userParts(base model.Correlation, text string, blocks []block) []partial {
  	var parts []partial
  	if text != "" {
  		enc, err := json.Marshal(text)
  		if err == nil {
  			pl := &model.Payload{Input: enc}
  			pl.Hash = model.HashPayload(pl)
  			parts = append(parts, partial{kind: "user_prompt", correlation: base, payload: pl})
  		}
  	}
  	for _, b := range blocks {
  		if b.Type != "tool_result" {
  			continue
  		}
  		status := model.StatusOK
  		if b.IsError {
  			status = model.StatusError
  		}
  		c := base
  		c.ToolUseID = b.ToolUseID
  		pl := &model.Payload{Output: b.Content}
  		pl.Hash = model.HashPayload(pl)
  		parts = append(parts, partial{
  			kind:        "tool_result",
  			correlation: c,
  			attrs:       map[string]any{"status": string(status)},
  			payload:     pl,
  		})
  	}
  	return parts
  }
  ```

- [ ] Re-run the new test and confirm it PASSES:

  ```sh
  go test ./ingest/jsonl/ -run TestParseReaderUserPromptTextPayload
  ```

  Expected: PASS.

- [ ] Coverage guard for the `json.Marshal` error branch. `json.Marshal` of a `string` cannot fail in practice, so the `if err == nil` guard would leave an uncoverable `else`. To keep 100% without an uncoverable branch, the implementation above has **no `else`** — the only branch is the happy path inside `if err == nil`, and the surrounding `if text != ""` is already exercised by `TestParseReaderUserPromptTextPayload` (true) and by `TestParseReaderMessageWithoutContent` / `TestParseReaderNonToolResultBlock` (false, empty text). Confirm the empty-text path stays payload-free by adding:

  ```go
  func TestParseReaderUserPromptEmptyTextNoPayload(t *testing.T) {
  	obs, err := ParseReader(strings.NewReader(
  		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"x","is_error":false}]}}`+"\n"), "e")
  	require.NoError(t, err)
  	assert.Empty(t, byKind(obs, "user_prompt"))
  }
  ```

- [ ] Run the full package test + coverage to confirm no regressions and 100% on the file:

  ```sh
  go test ./ingest/jsonl/ -cover
  ```

  Expected: PASS, `coverage: 100.0% of statements`.

- [ ] Commit:

  ```sh
  git add ingest/jsonl/jsonl.go ingest/jsonl/jsonl_test.go
  git commit -m "feat(jsonl): persist user prompt text into Payload.Input

  Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
  ```

---

## Task 2: jsonl ingestion — persist assistant response text into `Payload.Output`

**Files:**

- Modify: `ingest/jsonl/jsonl.go`
- Test: `ingest/jsonl/jsonl_test.go`

**Interfaces:**

- Consumes: `message{Content json.RawMessage; ID, Model string; Usage *usage}`, `decodeContent`, `model.HashPayload`.
- Produces (unchanged signature, new behavior): `assistantParts(base model.Correlation, msg message, blocks []block) []partial` attaches `payload: &model.Payload{Output: <json-encoded assistant text>, Hash: …}` to the `assistant_turn` partial when the assistant message has visible text; per-`tool_use` `Payload{Input: b.Input}` children are unchanged.

Steps:

- [ ] The current `assistantParts` is called from `decodeLine` as `assistantParts(base, msg, blocks)` and never receives the decoded text — `decodeContent` returns `text` but `decodeLine` discards it for the assistant branch (it only keeps `blocks` via the `text, blocks, err` tuple, and `text` is used only for `userParts`). Confirm by reading `decodeLine` (line 118: `text, blocks, err := decodeContent(...)`; line 128: `parts = assistantParts(base, msg, blocks)`). The assistant text must be threaded in. Change the call to pass `text`.

- [ ] Write the failing test first. Add to `ingest/jsonl/jsonl_test.go`:

  ```go
  func TestParseReaderAssistantTextPayload(t *testing.T) {
  	obs, err := ParseReader(strings.NewReader(
  		`{"type":"assistant","message":{"role":"assistant","id":"m","content":[{"type":"text","text":"here is the answer"}]}}`+"\n"), "e")
  	require.NoError(t, err)
  	turn := byKind(obs, "assistant_turn")
  	require.Len(t, turn, 1)
  	require.NotNil(t, turn[0].Payload)
  	assert.JSONEq(t, `"here is the answer"`, string(turn[0].Payload.Output))
  	assert.Empty(t, turn[0].Payload.Input)
  	assert.NotEmpty(t, turn[0].Payload.Hash)
  }

  func TestParseReaderAssistantToolUsePayloadUntouched(t *testing.T) {
  	tu := byKind(parseFixture(t), "assistant_tool_use")
  	require.Len(t, tu, 2)
  	require.NotNil(t, tu[0].Payload)
  	assert.JSONEq(t, `{"command":"ls"}`, string(tu[0].Payload.Input))
  	assert.Empty(t, tu[0].Payload.Output)
  }

  func TestParseReaderAssistantNoTextNoTurnPayload(t *testing.T) {
  	turn := byKind(parseFixture(t), "assistant_turn")
  	require.Len(t, turn, 2)
  	assert.Nil(t, turn[0].Payload)
  	assert.Nil(t, turn[1].Payload)
  }
  ```

  Note: the fixture `testdata/session.jsonl` assistant messages contain only `tool_use` blocks (no `text` block), so `decodeContent` returns empty `text` for them — hence `assistant_turn` partials there must remain payload-free, while `assistant_tool_use` children keep their input payload.

- [ ] Run and confirm FAIL:

  ```sh
  go test ./ingest/jsonl/ -run 'TestParseReaderAssistant(TextPayload|NoTextNoTurnPayload)'
  ```

  Expected: FAIL (turn payload nil for the text case).

- [ ] Implement. In `ingest/jsonl/jsonl.go`, thread the text into the assistant call and attach it. First, in `decodeLine`, change the assistant dispatch:

  ```go
  	case "assistant":
  		parts = assistantParts(base, msg, text, blocks)
  ```

- [ ] Then update `assistantParts` to accept and use the text:

  ```go
  func assistantParts(base model.Correlation, msg message, text string, blocks []block) []partial {
  	turn := base
  	turn.MessageID = msg.ID
  	attrs := map[string]any{"model": msg.Model}
  	if msg.Usage != nil {
  		attrs["tokens_in"] = msg.Usage.InputTokens
  		attrs["tokens_out"] = msg.Usage.OutputTokens
  	}
  	turnPart := partial{kind: "assistant_turn", correlation: turn, attrs: attrs}
  	if text != "" {
  		enc, err := json.Marshal(text)
  		if err == nil {
  			pl := &model.Payload{Output: enc}
  			pl.Hash = model.HashPayload(pl)
  			turnPart.payload = pl
  		}
  	}
  	parts := []partial{turnPart}
  	for _, b := range blocks {
  		if b.Type != "tool_use" {
  			continue
  		}
  		c := base
  		c.ToolUseID = b.ID
  		c.MessageID = msg.ID
  		pl := &model.Payload{Input: b.Input}
  		pl.Hash = model.HashPayload(pl)
  		parts = append(parts, partial{
  			kind:        "assistant_tool_use",
  			correlation: c,
  			attrs:       map[string]any{"name": b.Name},
  			payload:     pl,
  		})
  	}
  	return parts
  }
  ```

- [ ] Re-run the new tests and confirm PASS:

  ```sh
  go test ./ingest/jsonl/ -run 'TestParseReaderAssistant'
  ```

  Expected: PASS.

- [ ] Run the full package with coverage:

  ```sh
  go test ./ingest/jsonl/ -cover
  ```

  Expected: PASS, `coverage: 100.0% of statements`. The `text != ""` true branch is covered by `TestParseReaderAssistantTextPayload`; the false branch by `TestParseReaderAssistantNoTextNoTurnPayload` and the existing `TestParseReaderAssistantTextOnly`.

- [ ] Commit:

  ```sh
  git add ingest/jsonl/jsonl.go ingest/jsonl/jsonl_test.go
  git commit -m "feat(jsonl): persist assistant response text into Payload.Output

  Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
  ```

---

## Task 3: streamjson ingestion — mirror user + assistant text payloads

**Files:**

- Modify: `ingest/streamjson/streamjson.go`
- Test: `ingest/streamjson/streamjson_test.go`

**Interfaces:**

- Consumes: `decodeContent`, `model.HashPayload`, `message{ID, Model string; Content json.RawMessage; Usage *usage}`.
- Produces: `userParts(base, text, blocks)` attaches `Payload.Input` = encoded text while keeping `attrs: {"prompt": text}`; `assistantParts(base, msg, text, blocks)` attaches `Payload.Output` = encoded text. The `result`-type `assistant_turn` (in `build`, line 131) stays payload-free.

Steps:

- [ ] Write the failing tests first. Add to `ingest/streamjson/streamjson_test.go` (follow the existing single-line `Parse` convention; `byKind` is local to that file — if absent, fetch the first observation directly as the existing tests do):

  ```go
  func TestParseUserPromptTextPayload(t *testing.T) {
  	var seq uint64
  	next := func() uint64 { s := seq; seq++; return s }
  	obs, err := Parse([]byte(`{"type":"user","session_id":"s1","message":{"content":"hello there"}}`), "e", next)
  	require.NoError(t, err)
  	require.Len(t, obs, 1)
  	assert.Equal(t, "user_prompt", obs[0].Kind)
  	assert.Equal(t, "hello there", obs[0].Attrs["prompt"])
  	require.NotNil(t, obs[0].Payload)
  	assert.JSONEq(t, `"hello there"`, string(obs[0].Payload.Input))
  	assert.NotEmpty(t, obs[0].Payload.Hash)
  }

  func TestParseAssistantTextPayload(t *testing.T) {
  	var seq uint64
  	next := func() uint64 { s := seq; seq++; return s }
  	obs, err := Parse([]byte(`{"type":"assistant","session_id":"s1","message":{"id":"m1","content":[{"type":"text","text":"the reply"}]}}`), "e", next)
  	require.NoError(t, err)
  	turn := obs[0]
  	assert.Equal(t, "assistant_turn", turn.Kind)
  	require.NotNil(t, turn.Payload)
  	assert.JSONEq(t, `"the reply"`, string(turn.Payload.Output))
  	assert.Empty(t, turn.Payload.Input)
  }

  func TestParseResultTurnNoPayload(t *testing.T) {
  	var seq uint64
  	next := func() uint64 { s := seq; seq++; return s }
  	obs, err := Parse([]byte(`{"type":"result","session_id":"s1","usage":{"input_tokens":3,"output_tokens":4}}`), "e", next)
  	require.NoError(t, err)
  	require.Len(t, obs, 1)
  	assert.Equal(t, "assistant_turn", obs[0].Kind)
  	assert.Nil(t, obs[0].Payload)
  }
  ```

- [ ] Run and confirm FAIL:

  ```sh
  go test ./ingest/streamjson/ -run 'TestParse(UserPromptTextPayload|AssistantTextPayload)'
  ```

  Expected: FAIL (payload nil).

- [ ] Implement `userParts` (keep the `prompt` attr; add the payload):

  ```go
  func userParts(base model.Correlation, text string, blocks []block) []partial {
  	var parts []partial
  	if text != "" {
  		p := partial{kind: "user_prompt", correlation: base, attrs: map[string]any{"prompt": text}}
  		enc, err := json.Marshal(text)
  		if err == nil {
  			pl := &model.Payload{Input: enc}
  			pl.Hash = model.HashPayload(pl)
  			p.payload = pl
  		}
  		parts = append(parts, p)
  	}
  	for _, b := range blocks {
  		if b.Type != "tool_result" {
  			continue
  		}
  		status := model.StatusOK
  		if b.IsError {
  			status = model.StatusError
  		}
  		c := base
  		c.ToolUseID = b.ToolUseID
  		pl := &model.Payload{Output: b.Content}
  		pl.Hash = model.HashPayload(pl)
  		parts = append(parts, partial{
  			kind:        "tool_result",
  			correlation: c,
  			attrs:       map[string]any{"status": string(status)},
  			payload:     pl,
  		})
  	}
  	return parts
  }
  ```

- [ ] Implement `assistantParts` (thread text from the `user`/`assistant` branch of `build`). First, in `build`, change the assistant dispatch to capture and forward text:

  ```go
  	case "assistant":
  		var msg message
  		if len(e.Message) > 0 {
  			if err := json.Unmarshal(e.Message, &msg); err != nil {
  				return nil, fmt.Errorf("streamjson.build.assistant: %w", err)
  			}
  		}
  		text, blocks, err := decodeContent(msg.Content)
  		if err != nil {
  			return nil, err
  		}
  		return assistantParts(base, msg, text, blocks), nil
  ```

- [ ] Then update `assistantParts`:

  ```go
  func assistantParts(base model.Correlation, msg message, text string, blocks []block) []partial {
  	turn := base
  	turn.MessageID = msg.ID
  	attrs := map[string]any{"model": msg.Model}
  	if msg.Usage != nil {
  		attrs["tokens_in"] = msg.Usage.InputTokens
  		attrs["tokens_out"] = msg.Usage.OutputTokens
  	}
  	turnPart := partial{kind: "assistant_turn", correlation: turn, attrs: attrs}
  	if text != "" {
  		enc, err := json.Marshal(text)
  		if err == nil {
  			pl := &model.Payload{Output: enc}
  			pl.Hash = model.HashPayload(pl)
  			turnPart.payload = pl
  		}
  	}
  	parts := []partial{turnPart}
  	for _, b := range blocks {
  		if b.Type != "tool_use" {
  			continue
  		}
  		c := base
  		c.ToolUseID = b.ID
  		c.MessageID = msg.ID
  		pl := &model.Payload{Input: b.Input}
  		pl.Hash = model.HashPayload(pl)
  		parts = append(parts, partial{
  			kind:        "assistant_tool_use",
  			correlation: c,
  			attrs:       map[string]any{"name": b.Name},
  			payload:     pl,
  		})
  	}
  	return parts
  }
  ```

- [ ] Re-run the new tests and confirm PASS:

  ```sh
  go test ./ingest/streamjson/ -run 'TestParse(UserPromptTextPayload|AssistantTextPayload|ResultTurnNoPayload)'
  ```

  Expected: PASS.

- [ ] Full package coverage:

  ```sh
  go test ./ingest/streamjson/ -cover
  ```

  Expected: PASS, `coverage: 100.0% of statements`. The assistant `text != ""` false branch is covered by the existing `TestParseAssistantTurnAndToolUse` (tool-use only, no text) and `TestParseResultNoUsageNoCost`; the user false branch by existing non-tool-result tests.

- [ ] Commit:

  ```sh
  git add ingest/streamjson/streamjson.go ingest/streamjson/streamjson_test.go
  git commit -m "feat(streamjson): persist user and assistant message text into Payload

  Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
  ```

---

## Task 4: reduce — merge text payloads onto user_prompt / assistant_turn nodes

**Files:**

- Modify: `reduce/reduce.go`
- Test: `reduce/reduce_test.go`

**Interfaces:**

- Consumes: `model.Observation{Payload *model.Payload; Source model.Source; …}`.
- Produces: after `g.ApplyAll(obs)`, the `user_prompt` node carries `Payload.Input` and `PayloadHash`, and the `assistant_turn` node carries `Payload.Output` and `PayloadHash`. `mergePayload` (line 288) is reused unchanged; only the two case bodies gain a call to it.

Steps:

- [ ] This is the linchpin: ingestion now produces text payloads, but `reduce.go`'s `user_prompt` and `assistant_turn` cases drop them because they never call `g.mergePayload`. Write the failing test first. Add to `reduce/reduce_test.go` (the file already imports `model`, `encoding/json`, testify; reuse the existing `ob(kind, …)` / `NewGraph()` helpers — read the top of the file to match the exact constructor signatures before pasting):

  ```go
  func TestApplyUserPromptMergesTextPayload(t *testing.T) {
  	g := reduce.NewGraph()
  	o := model.Observation{
  		ObsID: "o1", RunID: "s1", ExecutionID: "e1", Source: model.SourceJSONL,
  		Kind: "user_prompt", Correlation: model.Correlation{SessionID: "s1", UUID: "u1"},
  		Payload:   &model.Payload{Input: json.RawMessage(`"list files"`)},
  		EventTime: time.Unix(1, 0).UTC(), Seq: 1,
  	}
  	o.Payload.Hash = model.HashPayload(o.Payload)
  	g.ApplyAll([]model.Observation{o})
  	n := g.Nodes[model.UserPromptID("e1", "u1")]
  	require.NotNil(t, n)
  	require.NotNil(t, n.Payload)
  	assert.JSONEq(t, `"list files"`, string(n.Payload.Input))
  	assert.NotEmpty(t, n.PayloadHash)
  }

  func TestApplyAssistantTurnMergesTextPayload(t *testing.T) {
  	g := reduce.NewGraph()
  	o := model.Observation{
  		ObsID: "o1", RunID: "s1", ExecutionID: "e1", Source: model.SourceJSONL,
  		Kind: "assistant_turn", Correlation: model.Correlation{SessionID: "s1", MessageID: "m1"},
  		Payload:   &model.Payload{Output: json.RawMessage(`"the reply"`)},
  		EventTime: time.Unix(1, 0).UTC(), Seq: 1,
  	}
  	o.Payload.Hash = model.HashPayload(o.Payload)
  	g.ApplyAll([]model.Observation{o})
  	n := g.Nodes[model.AssistantTurnID("e1", "m1")]
  	require.NotNil(t, n)
  	require.NotNil(t, n.Payload)
  	assert.JSONEq(t, `"the reply"`, string(n.Payload.Output))
  	assert.NotEmpty(t, n.PayloadHash)
  }

  func TestApplyAssistantTurnNilPayloadNoPanic(t *testing.T) {
  	g := reduce.NewGraph()
  	o := model.Observation{
  		ObsID: "o1", RunID: "s1", ExecutionID: "e1", Source: model.SourceStreamJSON,
  		Kind: "assistant_turn", Correlation: model.Correlation{SessionID: "s1", MessageID: "m2"},
  		EventTime: time.Unix(1, 0).UTC(), Seq: 1,
  	}
  	g.ApplyAll([]model.Observation{o})
  	n := g.Nodes[model.AssistantTurnID("e1", "m2")]
  	require.NotNil(t, n)
  	assert.Nil(t, n.Payload)
  }
  ```

- [ ] Run and confirm the first two FAIL, the third already passes (proving `mergePayload(nil)` is safe):

  ```sh
  go test ./reduce/ -run 'TestApply(UserPrompt|AssistantTurn)'
  ```

  Expected: FAIL on `TestApplyUserPromptMergesTextPayload` and `TestApplyAssistantTurnMergesTextPayload` (payload dropped); PASS on `TestApplyAssistantTurnNilPayloadNoPanic`.

- [ ] Implement. In `reduce/reduce.go`, add the merge call to the `user_prompt` case:

  ```go
  	case "user_prompt":
  		n := g.node(model.UserPromptID(o.ExecutionID, o.Correlation.UUID), o.RunID, model.NodeUserPrompt)
  		g.stamp(n, o)
  		g.mergePayload(n, o.Payload, o.Source)
  		g.emitNode(n, o)
  		g.upsertEdge(o.ExecutionID, o.RunID, model.SessionNodeID(o.ExecutionID), n.ID, o.Seq)
  ```

- [ ] And to the `assistant_turn` case (keep all existing token/cost/model logic; add the merge before `emitNode`):

  ```go
  	case "assistant_turn":
  		n := g.node(model.AssistantTurnID(o.ExecutionID, o.Correlation.MessageID), o.RunID, model.NodeAssistantTurn)
  		g.stamp(n, o)
  		g.stampEnd(n, o)
  		if g.applyTokens(n, o.Attrs, o.Source) {
  			g.applyCost(n, o.Attrs)
  		}
  		if m, ok := o.Attrs["model"].(string); ok && m != "" {
  			if n.Attrs == nil {
  				n.Attrs = map[string]any{}
  			}
  			n.Attrs["model"] = m
  		}
  		g.mergePayload(n, o.Payload, o.Source)
  		g.emitNode(n, o)
  ```

- [ ] Re-run and confirm PASS:

  ```sh
  go test ./reduce/ -run 'TestApply(UserPrompt|AssistantTurn)'
  ```

  Expected: PASS (all three).

- [ ] Add a no-regression test proving the `result`-then-`assistant_turn` merge (streamjson sends a payload-bearing `assistant_turn` from the message, then a payload-free one from `result`) keeps the text and does not clobber it. `mergePayload` only overwrites `Output` when `len(p.Output) > 0`, so the second (empty) observation must not wipe the first:

  ```go
  func TestApplyAssistantTurnResultDoesNotClobberText(t *testing.T) {
  	g := reduce.NewGraph()
  	first := model.Observation{
  		ObsID: "o1", RunID: "s1", ExecutionID: "e1", Source: model.SourceStreamJSON,
  		Kind: "assistant_turn", Correlation: model.Correlation{SessionID: "s1", MessageID: "m1"},
  		Payload:   &model.Payload{Output: json.RawMessage(`"keep me"`)},
  		EventTime: time.Unix(1, 0).UTC(), Seq: 1,
  	}
  	first.Payload.Hash = model.HashPayload(first.Payload)
  	second := model.Observation{
  		ObsID: "o2", RunID: "s1", ExecutionID: "e1", Source: model.SourceStreamJSON,
  		Kind: "assistant_turn", Correlation: model.Correlation{SessionID: "s1", MessageID: "m1"},
  		EventTime: time.Unix(2, 0).UTC(), Seq: 2,
  	}
  	g.ApplyAll([]model.Observation{first, second})
  	n := g.Nodes[model.AssistantTurnID("e1", "m1")]
  	require.NotNil(t, n.Payload)
  	assert.JSONEq(t, `"keep me"`, string(n.Payload.Output))
  }
  ```

- [ ] Run the full reduce package with coverage:

  ```sh
  go test ./reduce/ -cover
  ```

  Expected: PASS, `coverage: 100.0% of statements`. No new branch was added to `mergePayload`; the new lines are unconditional `g.mergePayload(...)` calls already exercised with non-nil and nil payloads.

- [ ] Run the cross-package integration to confirm ingestion + reduce land the text end-to-end through the public transcript path:

  ```sh
  go test ./ingest/... ./reduce/...
  ```

  Expected: PASS.

- [ ] Commit:

  ```sh
  git add reduce/reduce.go reduce/reduce_test.go
  git commit -m "feat(reduce): merge conversation-text payloads onto prompt and turn nodes

  Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
  ```

---

## Task 5: daemon — confirm conversation text flows through the gated, redacted endpoint

**Files:**

- Test: `daemon/payload_test.go` (new tests only; no production change expected)

**Interfaces:**

- Consumes: `(*Daemon).nodePayloadView(hash, selector string) (PayloadView, error)`, `(*Daemon).SetAllowPayloadAccess(bool)`, `model.HashPayload`.
- Produces: confirmation (via tests) that an `assistant_turn` node carrying `Payload.Output` text is returned by the existing handler when access is on, gets redacted when it contains a secret, and is 403-gated when access is off. No code edit; if a test fails, that is a real bug to fix in `daemon/payload.go`, not in the test.

Steps:

- [ ] Add tests that seed an `assistant_turn`-style text payload directly on a node and drive `nodePayloadView`, mirroring the existing `ingestSessionWithPayload` helper convention. Add to `daemon/payload_test.go`:

  ```go
  func seedTextPayload(t *testing.T, d *Daemon, nodeID string, out json.RawMessage) {
  	t.Helper()
  	d.mu.Lock()
  	g := d.graphs["exec1"]
  	p := &model.Payload{Output: out}
  	p.Hash = model.HashPayload(p)
  	g.Nodes[nodeID].Payload = p
  	g.Nodes[nodeID].PayloadHash = p.Hash
  	d.mu.Unlock()
  }

  func TestNodePayloadViewAssistantTextReturned(t *testing.T) {
  	d := New(tempStore(t))
  	fixedExecID(d)
  	d.SetAllowPayloadAccess(true)
  	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
  	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"p9","tool_input":{}}`)))
  	nodeID := model.ToolCallID("exec1", "p9")
  	seedTextPayload(t, d, nodeID, json.RawMessage(`"the assistant reply"`))
  	d.mu.Lock()
  	view, err := d.nodePayloadView("s1", nodeID)
  	d.mu.Unlock()
  	require.NoError(t, err)
  	assert.JSONEq(t, `"the assistant reply"`, string(view.Output))
  	assert.False(t, view.Redacted)
  	assert.Nil(t, view.Input)
  }

  func TestNodePayloadViewAssistantTextRedactsSecret(t *testing.T) {
  	d := New(tempStore(t))
  	fixedExecID(d)
  	d.SetAllowPayloadAccess(true)
  	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
  	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"p10","tool_input":{}}`)))
  	secret := "AKIAIOSFODNN7EXAMPLE"
  	nodeID := model.ToolCallID("exec1", "p10")
  	seedTextPayload(t, d, nodeID, json.RawMessage(`"my key is `+secret+`"`))
  	d.mu.Lock()
  	view, err := d.nodePayloadView("s1", nodeID)
  	d.mu.Unlock()
  	require.NoError(t, err)
  	assert.True(t, view.Redacted)
  	assert.NotContains(t, string(view.Output), secret)
  	require.NotEmpty(t, view.Redactions)
  }

  func TestNodePayloadViewAssistantTextGatedOff(t *testing.T) {
  	d := New(tempStore(t))
  	fixedExecID(d)
  	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
  	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"p11","tool_input":{}}`)))
  	nodeID := model.ToolCallID("exec1", "p11")
  	seedTextPayload(t, d, nodeID, json.RawMessage(`"secret reply"`))
  	d.mu.Lock()
  	_, err := d.nodePayloadView("s1", nodeID)
  	d.mu.Unlock()
  	assert.True(t, errors.Is(err, ErrPayloadAccessDisabled))
  }
  ```

- [ ] Run them and confirm PASS with no production edit (this proves the pass-through claim):

  ```sh
  go test ./daemon/ -run 'TestNodePayloadViewAssistantText'
  ```

  Expected: PASS. If `TestNodePayloadViewAssistantTextRedactsSecret` fails, fix `redact`/`buildPayloadView` per superpowers:systematic-debugging — do not weaken the assertion.

- [ ] Full daemon coverage:

  ```sh
  go test ./daemon/ -cover
  ```

  Expected: PASS, coverage unchanged at 100% (no new production lines).

- [ ] Run the whole backend gate before moving to the frontend:

  ```sh
  make cover
  ```

  Expected: PASS, 100% across all packages, no `go-test-coverage` threshold failure.

- [ ] Lint the backend:

  ```sh
  golangci-lint run
  ```

  Expected: 0 issues. Confirm no comments were introduced (`internal/codepolicy` runs as part of the suite).

- [ ] Commit:

  ```sh
  git add daemon/payload_test.go
  git commit -m "test(daemon): cover conversation-text payload pass-through and redaction

  Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
  ```

---

## Task 6: frontend — pure predicate `isConversationNode` (vitest 100%)

**Files:**

- Create: `webui/web/src/lib/conversation.ts`
- Create: `webui/web/src/lib/conversation.test.ts`
- Modify: `webui/vitest.config.ts`

**Interfaces:**

- Produces:

  ```ts
  export function isConversationNode(type: string): boolean
  export function conversationText(value: unknown): string
  ```

  `isConversationNode` returns `true` only for `'user_prompt'` and `'assistant_turn'`. `conversationText` turns the decoded payload field (which the backend serves as a JSON string value, surfacing in `PayloadView.input`/`output` as a JS `string`) into display text: returns the string as-is when it is a string, otherwise `JSON.stringify` of the value (defensive fallback for unexpected shapes), and `''` for `undefined`/`null`.

Steps:

- [ ] Write the failing test first. Create `webui/web/src/lib/conversation.test.ts`:

  ```ts
  import { describe, it, expect } from 'vitest';
  import { isConversationNode, conversationText } from './conversation';

  describe('isConversationNode', () => {
    it('is true for user_prompt', () => {
      expect(isConversationNode('user_prompt')).toBe(true);
    });

    it('is true for assistant_turn', () => {
      expect(isConversationNode('assistant_turn')).toBe(true);
    });

    it('is false for tool_call', () => {
      expect(isConversationNode('tool_call')).toBe(false);
    });

    it('is false for mcp_call', () => {
      expect(isConversationNode('mcp_call')).toBe(false);
    });

    it('is false for unknown types', () => {
      expect(isConversationNode('session')).toBe(false);
      expect(isConversationNode('')).toBe(false);
    });
  });

  describe('conversationText', () => {
    it('returns the string value unchanged', () => {
      expect(conversationText('hello world')).toBe('hello world');
    });

    it('returns empty string for undefined', () => {
      expect(conversationText(undefined)).toBe('');
    });

    it('returns empty string for null', () => {
      expect(conversationText(null)).toBe('');
    });

    it('stringifies a non-string value defensively', () => {
      expect(conversationText({ a: 1 })).toBe('{"a":1}');
    });
  });
  ```

- [ ] Run and confirm FAIL (module does not exist yet):

  ```sh
  cd webui && npx vitest run web/src/lib/conversation.test.ts
  ```

  Expected: FAIL — cannot resolve `./conversation`.

- [ ] Implement. Create `webui/web/src/lib/conversation.ts`:

  ```ts
  const CONVERSATION_TYPES = new Set(['user_prompt', 'assistant_turn']);

  export function isConversationNode(type: string): boolean {
    return CONVERSATION_TYPES.has(type);
  }

  export function conversationText(value: unknown): string {
    if (value === undefined || value === null) return '';
    if (typeof value === 'string') return value;
    return JSON.stringify(value);
  }
  ```

- [ ] Add the new module to the vitest coverage `include` list so it is 100%-gated (without this, the file is not measured). In `webui/vitest.config.ts`, inside `coverage.include`, add the entry:

  ```ts
        'web/src/lib/conversation.ts',
  ```

  Place it adjacent to the other `web/src/lib/*.ts` entries (for example right after `'web/src/lib/payload-view.ts',`).

- [ ] Re-run with coverage and confirm PASS at 100% for the file:

  ```sh
  cd webui && npx vitest run web/src/lib/conversation.test.ts --coverage
  ```

  Expected: PASS, `conversation.ts` at 100% statements/branches/functions/lines.

- [ ] Run the whole vitest suite to confirm the config edit did not break the global threshold:

  ```sh
  cd webui && npm run test
  ```

  Expected: PASS, overall coverage thresholds (`100: true`, `perFile`) satisfied.

- [ ] Commit:

  ```sh
  git add webui/web/src/lib/conversation.ts webui/web/src/lib/conversation.test.ts webui/vitest.config.ts
  git commit -m "feat(webui): add isConversationNode predicate and conversation-text helper

  Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
  ```

---

## Task 7: frontend — PayloadPanel renders conversation text vs JSON

**Files:**

- Modify: `webui/web/src/components/PayloadPanel.svelte`
- Modify: `webui/web/src/components/NodeDrawer.svelte`

**Interfaces:**

- Consumes: `isConversationNode`, `conversationText` from `../lib/conversation`; `prettyJSON`, `payloadState` from `../lib/payload-view`.
- Produces: `PayloadPanel` gains a required prop `nodeType: string`. When `isConversationNode(nodeType)` is true and the view is ready/redacted, it renders `Input` and `Output` as conversation text (whitespace-preserving, plain text) under `Prompt` / `Response` labels; otherwise it keeps the existing `<pre class="payload-content mono">{prettyJSON(...)}</pre>` JSON view. `NodeDrawer` passes `nodeType={node.type}`.

Steps:

- [ ] **Rendering decision (locked): plain, whitespace-preserving text — no markdown dependency.** Conversation text renders safely as plain text with `white-space: pre-wrap` (no raw-HTML injection, no XSS surface). This is the final v1 behavior, not a stopgap: full markdown rendering is explicitly out of scope, because a renderer without sanitization would be a security and tech-debt liability. Do not add `snarkdown`, `marked`, `markdown-it`, or `remark` in this workstream.

- [ ] Thread the node type from `NodeDrawer.svelte`. Change the panel invocation (line 128) to pass the type:

  ```svelte
        <PayloadPanel {hash} nodeId={node.id} nodeType={node.type} {token} />
  ```

- [ ] In `PayloadPanel.svelte`, extend the imports and props. Update the import block and `Props`:

  ```svelte
    import { fetchNodePayload, ForbiddenError, NotFoundError } from '../lib/api';
    import { prettyJSON, payloadState, type PayloadState } from '../lib/payload-view';
    import { isConversationNode, conversationText } from '../lib/conversation';
    import type { PayloadView } from '../lib/types';
  ```

  ```svelte
    interface Props {
      hash: string;
      nodeId: string;
      nodeType: string;
      token: string;
    }
    let { hash, nodeId, nodeType, token }: Props = $props();
  ```

- [ ] Add a derived flag near the other `$derived` declarations:

  ```svelte
    const asText = $derived(isConversationNode(nodeType));
  ```

- [ ] Replace the ready/redacted rendering block. The current block (lines 105-144) renders Input/Output as JSON unconditionally. Branch it on `asText`. Replace the two `payload-section` blocks with:

  ```svelte
      {:else if (displayState === 'redacted' || displayState === 'ready') && view}
        {#if view.redacted}
          <div class="redacted-notice" aria-label="Content was redacted">
            <span class="redacted-badge">redacted</span>
            <span class="redacted-count">{view.redactions.length} secret{view.redactions.length === 1 ? '' : 's'} redacted</span>
          </div>
        {/if}

        {#if view.input !== undefined && view.input !== null}
          <div class="payload-section">
            <div class="payload-section-header">
              <span class="payload-section-label">{asText ? 'Prompt' : 'Input'}</span>
              <button
                class="copy-btn"
                onclick={() => copyText(asText ? conversationText(view?.input) : prettyJSON(view?.input))}
                aria-label="Copy input content"
              >
                Copy
              </button>
            </div>
            {#if asText}
              <pre class="payload-content payload-text">{conversationText(view.input)}</pre>
            {:else}
              <pre class="payload-content mono">{prettyJSON(view.input)}</pre>
            {/if}
          </div>
        {/if}

        {#if view.output !== undefined && view.output !== null}
          <div class="payload-section">
            <div class="payload-section-header">
              <span class="payload-section-label">{asText ? 'Response' : 'Output'}</span>
              <button
                class="copy-btn"
                onclick={() => copyText(asText ? conversationText(view?.output) : prettyJSON(view?.output))}
                aria-label="Copy output content"
              >
                Copy
              </button>
            </div>
            {#if asText}
              <pre class="payload-content payload-text">{conversationText(view.output)}</pre>
            {:else}
              <pre class="payload-content mono">{prettyJSON(view.output)}</pre>
            {/if}
          </div>
        {/if}
      {/if}
  ```

- [ ] Add a `.payload-text` style next to `.payload-content` in the `<style>` block (wraps long prose instead of horizontal scroll; the `mono` class stays for JSON):

  ```css
    .payload-text {
      white-space: pre-wrap;
      word-break: break-word;
      font-family: var(--font-ui);
      color: var(--text);
    }
  ```

- [ ] Typecheck the components:

  ```sh
  cd webui && npm run check
  ```

  Expected: 0 errors (the new required `nodeType` prop is supplied by `NodeDrawer`).

- [ ] Run the vitest suite (components are not line-gated, but the run must stay green and thresholds satisfied):

  ```sh
  cd webui && npm run test
  ```

  Expected: PASS.

- [ ] Rebuild the embedded UI and stage the regenerated `dist` (required by `check:dist`):

  ```sh
  cd webui && npm run build
  ```

  Expected: build succeeds; `webui/dist/` changes are produced.

- [ ] Commit:

  ```sh
  git add webui/web/src/components/PayloadPanel.svelte webui/web/src/components/NodeDrawer.svelte webui/dist
  git commit -m "feat(webui): render conversation-text payloads as prose, keep JSON for tools

  Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
  ```

---

## Task 8: frontend — Playwright e2e for the conversation-text branch

**Files:**

- Modify: `webui/e2e/payload.spec.ts`

**Interfaces:**

- Consumes: the Playwright route-mocking harness already in `payload.spec.ts` (`page.route` for `/v1/sessions`, `/v1/subscribe**`, and `/v1/sessions/{hash}/nodes/**`).
- Produces: e2e coverage that an `assistant_turn` node shows its response **as text** (not a JSON `<pre class="mono">`) and that a `tool_call` node still shows JSON.

Steps:

- [ ] Extend the existing SSE fixture so the mocked session contains an `assistant_turn` node in addition to the tool node. In `webui/e2e/payload.spec.ts`, add a node to `sseEvents` (after the existing `node-tool-pl` upsert) and an edge:

  ```ts
    {
      kind: 'node_upsert',
      rev: 4,
      node: {
        id: 'node-turn-pl',
        run_id: 'run-payload',
        type: 'assistant_turn',
        name: 'assistant turn',
        status: 'ok',
        payload_hash: 'turnhash',
        rev: 4,
      },
    },
    {
      kind: 'edge_upsert',
      rev: 5,
      edge: {
        id: 'edge-pl-2',
        run_id: 'run-payload',
        type: 'parent_child',
        src: 'node-session-pl',
        dst: 'node-turn-pl',
        rev: 5,
      },
    },
  ```

- [ ] Add a test that asserts the assistant turn renders as text (`.payload-text`, no `.mono` JSON block) under a `Response` label. The payload mock must return `output` as a string (matching what the redacted endpoint serves for conversation nodes). Append to `webui/e2e/payload.spec.ts`:

  ```ts
  test('content panel: assistant turn renders response as text, not JSON', async ({ page }) => {
    await page.route(`/v1/sessions/${sessionHash}/nodes/**`, async (route) => {
      const turnPayload: PayloadView = {
        node_id: 'node-turn-pl',
        payload_hash: 'turnhash',
        output: 'Here is the **answer** to your question.',
        redactions: [],
        redacted: false,
      };
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(turnPayload) });
    });

    await page.goto(`/?token=test#/s/${sessionHash}`);
    await expect(page.locator('.svelte-flow__node')).toHaveCount(3, { timeout: 8000 });

    const turnNode = page.locator('.svelte-flow__node').filter({ hasText: 'assistant turn' });
    await turnNode.click();

    const drawer = page.locator('.node-drawer');
    await expect(drawer).toBeVisible();
    await drawer.locator('.reveal-btn').click();

    await expect(drawer.locator('.payload-section-label')).toContainText('Response');
    await expect(drawer.locator('.payload-text')).toContainText('Here is the **answer** to your question.');
    await expect(drawer.locator('.payload-content.mono')).toHaveCount(0);
  });
  ```

- [ ] Update the existing tool-node count assertions. The two existing tool tests assert `toHaveCount(2)` for `.svelte-flow__node`; with the added turn node the session now has 3 nodes. Change those two assertions from `toHaveCount(2, …)` to `toHaveCount(3, …)`, and confirm the tool test still finds the JSON branch by adding one assertion to the existing `200 redacted payload` test after its section checks:

  ```ts
    await expect(drawer.locator('.payload-content.mono').first()).toBeVisible();
    await expect(drawer.locator('.payload-text')).toHaveCount(0);
  ```

- [ ] Run the payload e2e spec:

  ```sh
  cd webui && npx playwright test e2e/payload.spec.ts
  ```

  Expected: PASS — the new conversation-text test plus the updated tool-JSON tests all green.

- [ ] Run the full e2e suite to confirm the node-count fixture change broke nothing else:

  ```sh
  cd webui && npm run test:e2e
  ```

  Expected: PASS.

- [ ] Commit:

  ```sh
  git add webui/e2e/payload.spec.ts
  git commit -m "test(webui): e2e for conversation-text vs JSON payload rendering

  Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
  ```

---

## Task 9: full-stack verification + dist sync

**Files:**

- None (verification only; re-stage `webui/dist` if the final build differs).

Steps:

- [ ] Run the complete backend gate:

  ```sh
  make cover && golangci-lint run
  ```

  Expected: 100% coverage, 0 lint issues, no codepolicy comment violations.

- [ ] Run the complete frontend gate:

  ```sh
  cd webui && npm run check && npm run test && npm run test:e2e
  ```

  Expected: typecheck clean, vitest 100% thresholds met, all Playwright specs green.

- [ ] Confirm the committed `dist` is in sync:

  ```sh
  cd webui && npm run check:dist
  ```

  Expected: exits 0 (no stale `dist`). If it reports a diff, run `npm run build`, then `git add webui/dist` and amend the Task 7 commit or add a `chore(webui): rebuild dist` commit with the standard trailer.

- [ ] Markdownlint this plan doc:

  ```sh
  npx --yes markdownlint-cli@0.49.0 docs/superpowers/plans/2026-06-25-w3-conversation-text.md
  ```

  Expected: no output (clean).

---

## Exit Criteria

- [ ] `user_prompt` partials from both `ingest/jsonl` and `ingest/streamjson` carry `Payload.Input` = the JSON-encoded prompt text with a non-empty `Hash`; empty prompt text attaches no payload.
- [ ] `assistant_turn` partials carry `Payload.Output` = the JSON-encoded response text with a non-empty `Hash`; turns with no visible text (tool-use-only, or the streamjson `result` turn) attach no payload.
- [ ] Existing tool payloads on `assistant_tool_use`/`tool_result` (`Payload.Input`/`Payload.Output`) are byte-for-byte unchanged.
- [ ] `reduce` merges text payloads onto `user_prompt` and `assistant_turn` nodes; a later payload-free `assistant_turn` does not clobber previously merged text.
- [ ] The `GET …/payload` endpoint returns conversation text, redacts secrets embedded in message text, and is 403-gated behind `--allow-payload-access` — all proven by daemon tests with no production edit to `daemon/payload.go`.
- [ ] `isConversationNode` and `conversationText` exist, are vitest-covered at 100%, and `conversation.ts` is listed in `webui/vitest.config.ts` `coverage.include`.
- [ ] `PayloadPanel.svelte` renders `user_prompt`/`assistant_turn` payloads as whitespace-preserving prose (`Prompt`/`Response` labels) and keeps the JSON view for `tool_call`/`mcp_call`; `NodeDrawer` passes `nodeType`.
- [ ] Playwright proves the assistant-turn text branch and the tool-JSON branch.
- [ ] `make cover` 100%, `golangci-lint` 0 issues, `npm run check`/`test`/`test:e2e` green, `webui/dist` in sync, and this plan doc is markdownlint-clean.
