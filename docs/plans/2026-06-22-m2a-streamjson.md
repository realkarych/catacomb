# M2a — stream-json ingest source + reducer 4-source precedence generalization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add stream-json (`claude --output-format stream-json`) as Catacomb's third live ingest source and generalize the reducer's OTel-vs-rest binary precedence into a per-field rank over the three live sources, plus a `parent_tool_use_id` subagent-edge consumer.

**Architecture:** A new pure `ingest/streamjson.Parse` maps one NDJSON envelope line to zero-or-more `model.Observation{Source: SourceStreamJSON}` reusing the existing reducer kinds. A new token-gated `POST /v1/stream-json` handler reads the streamed NDJSON request body line-by-line through `Daemon.IngestStreamJSON` (mirroring `IngestOTLP`). The reducer's `(rank, seq)` stamp mechanism is extended with three live-source rank functions and a structure-rank guard; commutativity is preserved because every guard is a pure function of `(rank, seq)`. Two thin fail-open CLI forwarders (`catacomb ingest stream-json` and `catacomb run -- <cmd...>`) feed the endpoint; the daemon never spawns Claude.

**Tech Stack:** Go 1.26 pure-Go; stdlib only (`encoding/json`, `bufio`, `os/exec`, `io.TeeReader`, `net/http`); `github.com/oklog/ulid/v2` (already vendored); `github.com/spf13/cobra` (already vendored); `github.com/stretchr/testify` for tests. NO new dependencies.

## Global Constraints

- **Go 1.26, pure-Go, no cgo.** `modernc.org/sqlite` only; single static cross-platform binary.
- **NO new dependencies.** M2a needs none (stdlib NDJSON + `os/exec` + `io.TeeReader`). **NEVER `go mod tidy`**; if a dep were ever needed it is `go get <pkg>@latest` one at a time (it is not needed here).
- **NO comments in Go code** (`internal/codepolicy`). The only allowed comments are the `//go:build`, `//go:embed`, `//go:generate` directives; `// Code generated … DO NOT EDIT.` files are skipped wholesale.
- **100% line coverage under `-race`** (`make cover`), TDD-first, threshold never lowered. Every branch must be reached by a genuine assertion.
- **NO dead code.** In M2a there are exactly THREE live sources: hook, OTel, stream-json. **JSONL is NOT live until M2b** — do NOT add a `SourceJSONL` branch to `sourceRank` / `tokenRank` / `payloadRank` / the structure guard in M2a; an unreachable JSONL branch would fail 100% genuine coverage. M2b inserts the JSONL tier.
- **`golangci-lint v2` clean** (gofumpt, goimports local-prefix `github.com/realkarych/catacomb`, govet shadow, **forbidigo bans `time.Sleep`**, unparam, errcheck, rowserrcheck, bodyclose). **unparam gotcha:** vary args/sources/tokens across call sites so a parameter is never seen as constant.
- **Single-mutex daemon discipline.** `d.graphs` / `d.execBySession` / `d.lastSeen` / bus / metrics under `d.mu`. `IngestStreamJSON` takes `d.mu` and ingests per line. The handler reads the body (I/O) outside the lock and calls `IngestStreamJSON` per line.
- **Observation log is the system of record** (ADR-0002); the reducer is a deterministic **COMMUTATIVE** projection (original spec §16). The precedence extension stays a pure function of `(rank, seq)` — extend the M1b `(rank, seq)` stamp mechanism, do not replace it.
- **`seq` is the global gap-free total order** (ADR-0010/0018), stamped once at the daemon's serialized append boundary (`daemon.go:312` `next()`). The live stream-json path takes `seq` from `d.next` via the `nextSeq` seam — never a parser-local counter.
- **`nowFn` injection** (ADR-0018). No `time.Now()` in non-test code; `ingest/streamjson` uses the `var nowFn = time.Now` seam.
- **ADR-0019 fault isolation.** The handler and `IngestStreamJSON` each run under `recover()`; one bad line → a quarantined poison observation + HTTP 200, never a daemon crash. Reuse `d.quarantine`.
- **ADR-0013 loopback + bearer.** `POST /v1/stream-json` sits behind the existing `authed` middleware, like `/hook/{type}` and `/v1/traces`.
- **Ingest sources are pure `Parse(...) ([]model.Observation, error)`** mirroring `ingest/hook` and `ingest/otel`. White-box test files (same package). **Fixtures-as-contract:** construct the NDJSON in tests (as `ingest/otel` builds proto structs), not golden files on disk.
- **[VERIFY] markers.** The stream-json envelope is officially undocumented; the best-known field names (spec §4.5) are recorded in this plan's prose and in a doc note, NOT in Go comments (which are banned).
- **markdownlint** (CI, `npx markdownlint-cli@0.49.0`, config `.markdownlint.json`): blank lines around every list and fenced code block (MD031/MD032 enforced). Run `npx markdownlint-cli@0.49.0 --fix` on this plan before committing.

---

## Architecture notes for the implementer

These are facts about the existing codebase the tasks below rely on. Read them before Task 1.

- **`model.SourceStreamJSON` already exists** (`model/model.go:13` = `"stream_json"`). **`model.Correlation.ParentToolUseID` already exists** (`model/model.go:56`) but currently has NO consumer. No new `model` consts are needed; Task 3 adds the first consumer of `ParentToolUseID`.
- **Existing reducer kinds** the parser reuses (string literals in `reduce/reduce.go:27-69`): `session_start`, `session_end`, `user_prompt`, `assistant_turn`, `assistant_tool_use`, `tool_result`, `subagent_stop`, `marker`, `run_ended`. M2a adds NO new kinds.
- **Existing reducer precedence mechanism (exact, read in full):**
  - `func sourceRank(s model.Source) int` (`reduce.go:126-131`) — returns `1` for `SourceOTel`, `0` otherwise.
  - `type fieldStamps struct { timingRank int; haveTiming bool; nameSeq uint64; haveName bool; haveOTelTokens bool }` (`reduce.go:133-139`); `func (g *Graph) stampsFor(id string) *fieldStamps` (`reduce.go:141-148`).
  - `func (g *Graph) stamp(n *model.Node, o model.Observation)` (`reduce.go:150-166`) — timing: `r := sourceRank(o.Source)`; higher rank wins (`!fs.haveTiming || r > fs.timingRank`), equal rank earliest-`EventTime` wins, lower rank blocked; then `n.Rev = max(n.Rev, o.Seq)`; then appends `model.SourceRef`.
  - `func (g *Graph) setName(n *model.Node, o model.Observation, name string)` (`reduce.go:168-178`) — first non-empty by lowest `Seq`; source-agnostic. **No change in M2a.**
  - `func mergePayload(n *model.Node, p *model.Payload)` (`reduce.go:180-195`) — package-level (NOT a `*Graph` method); last-writer-wins per non-empty field, no rank today.
  - `func (g *Graph) applyTokens(n *model.Node, attrs map[string]any, src model.Source)` (`reduce.go:197-211`) — gated by the `haveOTelTokens` bool: non-OTel returns early once OTel tokens seen; sets `n.TokensIn`/`n.TokensOut` from `toInt64`.
  - `func (g *Graph) upsertEdgeGated(o model.Observation, src, dst string)` (`reduce.go:72-79`) — the #53954 OTel gate: an OTel `parent_child` edge is skipped unless the span has children (`g.spanChildren[o.Correlation.SpanID]`) or `o.Correlation.ToolUseID != ""`; otherwise calls `upsertEdge`.
  - `func (g *Graph) upsertEdge(executionID, runID, src, dst string, seq uint64)` (`reduce/graph.go:54-70`) — creates the `EdgeParentChild` edge (`Rev = seq`) or raises `Rev` on a higher `seq`, emitting `cdc.DeltaEdgeUpsert`.
  - `func (g *Graph) applyTool(o model.Observation)` (`reduce.go:81-108`) — routes `assistant_tool_use`/`tool_result`; parent is the assistant turn (if `MessageID != ""`) else the session node; calls `g.upsertEdgeGated(o, parent, id)`.
- **Daemon ingest seam pattern** (`daemon/daemon.go`): package vars `nowFn`, `applyFn = func(g,o){g.Apply(o)}`, `parseFn = otelingest.Parse` (lines 28-32). `IngestOTLP` (lines 175-229) takes `d.mu`, installs a `recover()` that quarantines and sets `err = nil`, resolves `sessionID → execID` via `d.execBySession` (minting via `d.newExecID`), calls `parseFn(req, execID, d.next)` (quarantine on error), lazy-reloads the shard from `d.store.ObservationsForExecution` when `known && !inMem`, then `d.applyAndPersist(g, o)` + `d.lastSeen[o.RunID] = o.ObservedAt` per obs. `func (d *Daemon) next() uint64` (line 312) increments and returns `d.seq`. `func (d *Daemon) quarantine(hookType string, payload []byte, msg string)` (line 304).
- **HTTP handler pattern** (`daemon/server.go`): `d.Handler(token)` registers routes on a `http.ServeMux`; `d.authed(token, h)` (line 55) is the bearer middleware. `handleOTLP` (lines 37-53) and `handleHook` (lines 65-73) read the body and return a status code. The mux uses Go 1.22 method-prefixed patterns (`"POST /v1/traces"`).
- **CLI forwarder pattern** (`cmd/catacomb/hook.go`): `forward(warn io.Writer, discoveryPath, hookType string, stdin io.Reader)` reads stdin, `daemon.ReadDiscovery(discoveryPath)` for `{Addr, Token}`, POSTs with `Authorization: Bearer <token>`, fails open by writing to `warn` (never returns an error). `newHookCmd()` wires it via cobra; `newRootCmd()` (`cmd/catacomb/root.go:5`) registers commands.
- **Test helpers** (`daemon/testsupport.go`): `d.GraphsForTest() map[string]*reduce.Graph`, `d.execForTest(runID) string`, `d.dropShardForTest(runID)`, `d.QuarantinedForTest() int64`, `d.EvictedForTest() int64`. `fixedExecID(d)` (in `daemon_test.go:32`) makes `d.newExecID` deterministic (`exec1`, `exec2`, …). `tempStore(t)` opens a temp SQLite store.

---

### Task 1: `ingest/streamjson.Parse` — NDJSON envelope → observations

**Files:**

- Create: `ingest/streamjson/streamjson.go`
- Test: `ingest/streamjson/streamjson_test.go`

**Interfaces:**

- Consumes: `model.Observation`, `model.Correlation`, `model.Payload`, `model.SourceStreamJSON`, `model.HashPayload`, `model.StatusOK`, `model.StatusError` (all existing in `model`); `github.com/oklog/ulid/v2`.
- Produces: `func Parse(line []byte, executionID string, nextSeq func() uint64) ([]model.Observation, error)` — pure, no I/O, mirrors `ingest/otel.Parse` and `ingest/hook.Parse`. One NDJSON line yields zero-or-more observations, each `Source: model.SourceStreamJSON`, `RunID = session_id` from the envelope, `ExecutionID = executionID`, `Seq = nextSeq()`, `EventTime = ObservedAt = nowFn().UTC()`. An unknown/unrecognized `type` yields zero observations and no error. A line that is not valid JSON returns an error (so the daemon can quarantine it). Also produces the package seam `var nowFn = time.Now`.

**[VERIFY] field names used (undocumented envelope, spec §4.5):** discriminator top-level `type` ∈ {`system`,`assistant`,`user`,`stream_event`,`result`}; `subtype` (for `system` → `init`); top-level `session_id`; top-level `parent_tool_use_id` and `uuid` (on `stream_event`); `message.id`, `message.model`, `message.usage.input_tokens`/`output_tokens`; `content[]` blocks `{type,id,name,input}` (tool_use) and `{type,tool_use_id,content,is_error}` (tool_result); `result.usage.input_tokens`/`output_tokens` and `result.total_cost_usd`. Every field access is type-guarded via `encoding/json` struct tags + presence checks; missing fields degrade to null, never a hard error.

- [ ] **Step 1: Write the failing test for the `system.init` envelope**

```go
package streamjson

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
)

func seq() func() uint64 {
	var n uint64
	return func() uint64 {
		n++
		return n
	}
}

func fixedNow(t time.Time) {
	nowFn = func() time.Time { return t }
}

func TestParseSystemInit(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	fixedNow(now)
	line := []byte(`{"type":"system","subtype":"init","session_id":"sess_1","model":"claude-opus-4-8"}`)

	obs, err := Parse(line, "exec1", seq())
	require.NoError(t, err)
	require.Len(t, obs, 1)

	o := obs[0]
	assert.Equal(t, "session_start", o.Kind)
	assert.Equal(t, model.SourceStreamJSON, o.Source)
	assert.Equal(t, "exec1", o.ExecutionID)
	assert.Equal(t, "sess_1", o.RunID)
	assert.Equal(t, "sess_1", o.Correlation.SessionID)
	assert.Equal(t, "claude-opus-4-8", o.Attrs["model"])
	assert.Equal(t, now, o.ObservedAt)
	assert.Equal(t, now, o.EventTime)
	assert.Equal(t, uint64(1), o.Seq)
	assert.NotEmpty(t, o.ObsID)
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./ingest/streamjson/ -run TestParseSystemInit`
Expected: FAIL — `undefined: Parse` / no such package.

- [ ] **Step 3: Write the minimal `streamjson.go` to map `system.init`**

```go
package streamjson

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/realkarych/catacomb/model"
)

var nowFn = time.Now

type envelope struct {
	Type            string          `json:"type"`
	Subtype         string          `json:"subtype"`
	SessionID       string          `json:"session_id"`
	Model           string          `json:"model"`
	ParentToolUseID string          `json:"parent_tool_use_id"`
	UUID            string          `json:"uuid"`
	Message         json.RawMessage `json:"message"`
	Usage           *usage          `json:"usage"`
	TotalCostUSD    *float64        `json:"total_cost_usd"`
}

type message struct {
	ID      string          `json:"id"`
	Model   string          `json:"model"`
	Content json.RawMessage `json:"content"`
	Usage   *usage          `json:"usage"`
}

type usage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

type block struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

type partial struct {
	kind        string
	correlation model.Correlation
	attrs       map[string]any
	payload     *model.Payload
}

func Parse(line []byte, executionID string, nextSeq func() uint64) ([]model.Observation, error) {
	var e envelope
	if err := json.Unmarshal(line, &e); err != nil {
		return nil, fmt.Errorf("streamjson.Parse: %w", err)
	}
	parts, err := build(e)
	if err != nil {
		return nil, err
	}
	if len(parts) == 0 {
		return []model.Observation{}, nil
	}
	ts := nowFn().UTC()
	out := make([]model.Observation, 0, len(parts))
	for _, p := range parts {
		out = append(out, model.Observation{
			ObsID:       ulid.Make().String(),
			RunID:       e.SessionID,
			ExecutionID: executionID,
			Source:      model.SourceStreamJSON,
			Kind:        p.kind,
			Correlation: p.correlation,
			Attrs:       p.attrs,
			Payload:     p.payload,
			EventTime:   ts,
			ObservedAt:  ts,
			Seq:         nextSeq(),
		})
	}
	return out, nil
}

func build(e envelope) ([]partial, error) {
	base := model.Correlation{SessionID: e.SessionID}
	switch e.Type {
	case "system":
		if e.Subtype != "init" {
			return nil, nil
		}
		return []partial{{kind: "session_start", correlation: base, attrs: map[string]any{"model": e.Model}}}, nil
	default:
		return nil, nil
	}
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./ingest/streamjson/ -run TestParseSystemInit`
Expected: PASS.

- [ ] **Step 5: Write the failing test for the `assistant` envelope (turn + per-block tool_use)**

```go
func TestParseAssistantTurnAndToolUse(t *testing.T) {
	fixedNow(time.Now())
	line := []byte(`{"type":"assistant","session_id":"sess_2","message":{"id":"msg_a","model":"claude-opus-4-8","usage":{"input_tokens":100,"output_tokens":50},"content":[{"type":"text","text":"hi"},{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"ls"}}]}}`)

	obs, err := Parse(line, "exec2", seq())
	require.NoError(t, err)
	require.Len(t, obs, 2)

	turn := obs[0]
	assert.Equal(t, "assistant_turn", turn.Kind)
	assert.Equal(t, "msg_a", turn.Correlation.MessageID)
	assert.Equal(t, "claude-opus-4-8", turn.Attrs["model"])
	assert.Equal(t, int64(100), turn.Attrs["tokens_in"])
	assert.Equal(t, int64(50), turn.Attrs["tokens_out"])

	tu := obs[1]
	assert.Equal(t, "assistant_tool_use", tu.Kind)
	assert.Equal(t, "toolu_1", tu.Correlation.ToolUseID)
	assert.Equal(t, "msg_a", tu.Correlation.MessageID)
	assert.Equal(t, "Bash", tu.Attrs["name"])
	require.NotNil(t, tu.Payload)
	assert.JSONEq(t, `{"command":"ls"}`, string(tu.Payload.Input))
}
```

- [ ] **Step 6: Run the test to verify it fails**

Run: `go test ./ingest/streamjson/ -run TestParseAssistantTurnAndToolUse`
Expected: FAIL — `build` returns nil for `assistant` (got 0 observations, want 2).

- [ ] **Step 7: Add the `assistant`/`user` cases + content decoder**

Add to the `switch e.Type` in `build` (before `default`):

```go
	case "assistant":
		var msg message
		if len(e.Message) > 0 {
			if err := json.Unmarshal(e.Message, &msg); err != nil {
				return nil, fmt.Errorf("streamjson.build.assistant: %w", err)
			}
		}
		_, blocks, err := decodeContent(msg.Content)
		if err != nil {
			return nil, err
		}
		return assistantParts(base, msg, blocks), nil
	case "user":
		var msg message
		if len(e.Message) > 0 {
			if err := json.Unmarshal(e.Message, &msg); err != nil {
				return nil, fmt.Errorf("streamjson.build.user: %w", err)
			}
		}
		text, blocks, err := decodeContent(msg.Content)
		if err != nil {
			return nil, err
		}
		return userParts(base, text, blocks), nil
```

Add these package-level functions:

```go
func decodeContent(raw json.RawMessage) (string, []block, error) {
	if len(raw) == 0 {
		return "", nil, nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text, nil, nil
	}
	var blocks []block
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return "", nil, fmt.Errorf("streamjson.decodeContent: %w", err)
	}
	return "", blocks, nil
}

func assistantParts(base model.Correlation, msg message, blocks []block) []partial {
	turn := base
	turn.MessageID = msg.ID
	attrs := map[string]any{"model": msg.Model}
	if msg.Usage != nil {
		attrs["tokens_in"] = msg.Usage.InputTokens
		attrs["tokens_out"] = msg.Usage.OutputTokens
	}
	parts := []partial{{kind: "assistant_turn", correlation: turn, attrs: attrs}}
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

func userParts(base model.Correlation, text string, blocks []block) []partial {
	var parts []partial
	if text != "" {
		parts = append(parts, partial{kind: "user_prompt", correlation: base, attrs: map[string]any{"prompt": text}})
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

- [ ] **Step 8: Run the test to verify it passes**

Run: `go test ./ingest/streamjson/ -run TestParseAssistantTurnAndToolUse`
Expected: PASS.

- [ ] **Step 9: Write the failing tests for `user`/`stream_event`/`result`/unknown/bad-JSON**

```go
func TestParseUserToolResult(t *testing.T) {
	fixedNow(time.Now())
	line := []byte(`{"type":"user","session_id":"sess_3","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"done","is_error":false}]}}`)
	obs, err := Parse(line, "exec3", seq())
	require.NoError(t, err)
	require.Len(t, obs, 1)
	o := obs[0]
	assert.Equal(t, "tool_result", o.Kind)
	assert.Equal(t, "toolu_1", o.Correlation.ToolUseID)
	assert.Equal(t, "ok", o.Attrs["status"])
	assert.JSONEq(t, `"done"`, string(o.Payload.Output))
}

func TestParseUserToolResultError(t *testing.T) {
	fixedNow(time.Now())
	line := []byte(`{"type":"user","session_id":"s","message":{"content":[{"type":"tool_result","tool_use_id":"t","content":"boom","is_error":true}]}}`)
	obs, err := Parse(line, "e", seq())
	require.NoError(t, err)
	require.Len(t, obs, 1)
	assert.Equal(t, "error", obs[0].Attrs["status"])
}

func TestParseUserPromptText(t *testing.T) {
	fixedNow(time.Now())
	line := []byte(`{"type":"user","session_id":"s","message":{"content":"hello there"}}`)
	obs, err := Parse(line, "e", seq())
	require.NoError(t, err)
	require.Len(t, obs, 1)
	assert.Equal(t, "user_prompt", obs[0].Kind)
	assert.Equal(t, "hello there", obs[0].Attrs["prompt"])
}

func TestParseStreamEventParentToolUseID(t *testing.T) {
	fixedNow(time.Now())
	line := []byte(`{"type":"stream_event","session_id":"s","parent_tool_use_id":"toolu_parent","uuid":"u1"}`)
	obs, err := Parse(line, "e", seq())
	require.NoError(t, err)
	require.Len(t, obs, 1)
	o := obs[0]
	assert.Equal(t, "assistant_tool_use", o.Kind)
	assert.Equal(t, "toolu_parent", o.Correlation.ParentToolUseID)
	assert.Equal(t, "u1", o.Correlation.UUID)
}

func TestParseResultEnrichment(t *testing.T) {
	fixedNow(time.Now())
	line := []byte(`{"type":"result","session_id":"s","usage":{"input_tokens":7,"output_tokens":9},"total_cost_usd":0.0123}`)
	obs, err := Parse(line, "e", seq())
	require.NoError(t, err)
	require.Len(t, obs, 1)
	o := obs[0]
	assert.Equal(t, "assistant_turn", o.Kind)
	assert.Equal(t, int64(7), o.Attrs["tokens_in"])
	assert.Equal(t, int64(9), o.Attrs["tokens_out"])
	assert.Equal(t, 0.0123, o.Attrs["cost_usd"])
}

func TestParseUnknownTypeSkipped(t *testing.T) {
	fixedNow(time.Now())
	obs, err := Parse([]byte(`{"type":"mystery","session_id":"s"}`), "e", seq())
	require.NoError(t, err)
	assert.Empty(t, obs)
}

func TestParseSystemNonInitSkipped(t *testing.T) {
	fixedNow(time.Now())
	obs, err := Parse([]byte(`{"type":"system","subtype":"other","session_id":"s"}`), "e", seq())
	require.NoError(t, err)
	assert.Empty(t, obs)
}

func TestParseBadJSON(t *testing.T) {
	fixedNow(time.Now())
	_, err := Parse([]byte(`{not json`), "e", seq())
	require.Error(t, err)
}

func TestParseBadMessage(t *testing.T) {
	fixedNow(time.Now())
	_, err := Parse([]byte(`{"type":"assistant","session_id":"s","message":123}`), "e", seq())
	require.Error(t, err)
}

func TestParseBadContent(t *testing.T) {
	fixedNow(time.Now())
	_, err := Parse([]byte(`{"type":"user","session_id":"s","message":{"content":123}}`), "e", seq())
	require.Error(t, err)
}

func TestParseAssistantNoMessageNoTokens(t *testing.T) {
	fixedNow(time.Now())
	obs, err := Parse([]byte(`{"type":"assistant","session_id":"s"}`), "e", seq())
	require.NoError(t, err)
	require.Len(t, obs, 1)
	_, ok := obs[0].Attrs["tokens_in"]
	assert.False(t, ok)
}
```

- [ ] **Step 10: Run the tests to verify they fail**

Run: `go test ./ingest/streamjson/`
Expected: FAIL — `stream_event` and `result` cases unhandled (skipped), `TestParseBadMessage`/`TestParseBadContent` may already pass.

- [ ] **Step 11: Add the `stream_event` and `result` cases**

Add to the `switch e.Type` in `build` (before `default`):

```go
	case "stream_event":
		c := base
		c.ParentToolUseID = e.ParentToolUseID
		c.UUID = e.UUID
		return []partial{{kind: "assistant_tool_use", correlation: c}}, nil
	case "result":
		attrs := map[string]any{}
		if e.Usage != nil {
			attrs["tokens_in"] = e.Usage.InputTokens
			attrs["tokens_out"] = e.Usage.OutputTokens
		}
		if e.TotalCostUSD != nil {
			attrs["cost_usd"] = *e.TotalCostUSD
		}
		return []partial{{kind: "assistant_turn", correlation: base, attrs: attrs}}, nil
```

- [ ] **Step 12: Run the full package test suite under -race**

Run: `go test ./ingest/streamjson/ -race`
Expected: PASS (all tests).

- [ ] **Step 13: Verify 100% coverage + lint**

Run: `go test ./ingest/streamjson/ -cover` then `golangci-lint run ./ingest/streamjson/...`
Expected: `coverage: 100.0% of statements`; lint 0 issues. If any line is uncovered, add a targeted assertion (do not delete reachable code).

- [ ] **Step 14: Commit**

```bash
git add ingest/streamjson/streamjson.go ingest/streamjson/streamjson_test.go
git commit -m "feat(ingest/streamjson): NDJSON envelope parser to observations

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 2: Reducer 3-live-source precedence generalization (timing + tokens + payload)

**Files:**

- Modify: `reduce/reduce.go` (`sourceRank`, `fieldStamps`, `applyTokens`, `mergePayload`, `applyTool`)
- Test: `reduce/reduce_test.go`

**Interfaces:**

- Consumes: existing reducer internals (see Architecture notes); `model.SourceOTel`, `model.SourceHook`, `model.SourceStreamJSON`.
- Produces (used by Task 3 + M2b):
  - `func sourceRank(s model.Source) int` — full timing order over the three live sources: `SourceOTel=3`, `SourceHook=2`, `SourceStreamJSON=1`, default `0`.
  - `func tokenRank(s model.Source) int` — `SourceOTel=2`, `SourceStreamJSON=1`, default `0`.
  - `func payloadRank(s model.Source) int` — `SourceHook=1`, `SourceStreamJSON=0`, default `0`. (`SourceHook` and the future JSONL share the full-payload tier; stream-json is the delta tier.)
  - `fieldStamps` gains `tokenRank int`, `haveToken bool`, `payloadRank int`, `havePayload bool` (the `haveOTelTokens` bool is removed).
  - `applyTokens` becomes `func (g *Graph) applyTokens(n *model.Node, attrs map[string]any, src model.Source)` (signature unchanged) but ranks via `tokenRank`.
  - `mergePayload` becomes a `*Graph` method: `func (g *Graph) mergePayload(n *model.Node, p *model.Payload, src model.Source)` ranked via `payloadRank`.

**NO-DEAD-CODE NOTE:** Do NOT add a `SourceJSONL` case to `sourceRank`, `tokenRank`, or `payloadRank` in this task. JSONL is not a live producer until M2b; an explicit JSONL branch would be unreachable and fail genuine 100% coverage. The `default: return 0` arm is the correct, covered home for any non-live source today. **M2b inserts the JSONL tier** (timing `OTel>hook>JSONL>stream-json`, tokens `OTel>stream-json>JSONL`, payload `hook=JSONL>stream-json`) by editing these three switches — the shapes below are written so that insertion is a one-line-per-switch change.

- [ ] **Step 1: Write the failing test for the 3-tier timing rank**

```go
func TestSourceRankThreeLiveTiers(t *testing.T) {
	assert.Equal(t, 3, sourceRank(model.SourceOTel))
	assert.Equal(t, 2, sourceRank(model.SourceHook))
	assert.Equal(t, 1, sourceRank(model.SourceStreamJSON))
}

func TestTimingHookBeatsStreamJSON(t *testing.T) {
	t0 := time.Unix(100, 0).UTC()
	hookEarly := hookTurn("e1", "s1", "m1", 0, 0, t0.Add(time.Hour), 1)
	sjLate := model.Observation{
		RunID: "s1", ExecutionID: "e1", Source: model.SourceStreamJSON,
		Kind: "assistant_turn", Correlation: model.Correlation{SessionID: "s1", MessageID: "m1"},
		Attrs: map[string]any{}, EventTime: t0, ObservedAt: t0, Seq: 2,
	}
	fwd := NewGraph()
	fwd.ApplyAll([]model.Observation{sjLate, hookEarly})
	rev := NewGraph()
	rev.ApplyAll([]model.Observation{hookEarly, sjLate})
	id := model.AssistantTurnID("e1", "m1")
	require.NotNil(t, fwd.Nodes[id].TStart)
	assert.Equal(t, t0.Add(time.Hour), fwd.Nodes[id].TStart.UTC())
	assert.Equal(t, fwd.Nodes[id].TStart.UTC(), rev.Nodes[id].TStart.UTC())
}
```

Note: `hookTurn(execID, runID, msgID, tin, tout, eventTime, seq)` and `otelTurn(...)` already exist as helpers in `reduce_test.go` (used by `TestReductionCommutativity`). Reuse them; do not redefine.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./reduce/ -run 'TestSourceRankThreeLiveTiers|TestTimingHookBeatsStreamJSON'`
Expected: FAIL — `sourceRank(SourceHook)` returns `0`, not `2`; timing tie lets the later/earlier wrong value win.

- [ ] **Step 3: Generalize `sourceRank`**

Replace `sourceRank` (`reduce.go:126-131`) with:

```go
func sourceRank(s model.Source) int {
	switch s {
	case model.SourceOTel:
		return 3
	case model.SourceHook:
		return 2
	case model.SourceStreamJSON:
		return 1
	default:
		return 0
	}
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./reduce/ -run 'TestSourceRankThreeLiveTiers|TestTimingHookBeatsStreamJSON'`
Expected: PASS.

- [ ] **Step 5: Write the failing test for the token rank (OTel > stream-json, stream-json sets when no OTel)**

```go
func sjTurn(execID, runID, msgID string, tin, tout int64, seq uint64) model.Observation {
	return model.Observation{
		RunID: runID, ExecutionID: execID, Source: model.SourceStreamJSON,
		Kind: "assistant_turn", Correlation: model.Correlation{SessionID: runID, MessageID: msgID},
		Attrs: map[string]any{"tokens_in": tin, "tokens_out": tout},
		EventTime: time.Unix(100, 0).UTC(), ObservedAt: time.Unix(100, 0).UTC(), Seq: seq,
	}
}

func TestTokenRankOTelBeatsStreamJSON(t *testing.T) {
	otel := otelTurn("e1", "s1", "m1", 50, 20, time.Unix(100, 0).UTC(), 1)
	sj := sjTurn("e1", "s1", "m1", 7, 9, 2)
	fwd := NewGraph()
	fwd.ApplyAll([]model.Observation{otel, sj})
	rev := NewGraph()
	rev.ApplyAll([]model.Observation{sj, otel})
	id := model.AssistantTurnID("e1", "m1")
	require.NotNil(t, fwd.Nodes[id].TokensIn)
	assert.Equal(t, int64(50), *fwd.Nodes[id].TokensIn)
	assert.Equal(t, int64(50), *rev.Nodes[id].TokensIn)
}

func TestTokenStreamJSONSetsWhenNoOTel(t *testing.T) {
	sj := sjTurn("e1", "s1", "m1", 7, 9, 1)
	g := NewGraph()
	g.Apply(sj)
	id := model.AssistantTurnID("e1", "m1")
	require.NotNil(t, g.Nodes[id].TokensIn)
	assert.Equal(t, int64(7), *g.Nodes[id].TokensIn)
}
```

- [ ] **Step 6: Run the test to verify it fails**

Run: `go test ./reduce/ -run 'TestTokenRankOTelBeatsStreamJSON|TestTokenStreamJSONSetsWhenNoOTel'`
Expected: FAIL — current `applyTokens` lets a non-OTel source overwrite OTel tokens unless `haveOTelTokens` was set in that exact order, and there is no `tokenRank`.

- [ ] **Step 7: Replace the token-stamp fields and `applyTokens`**

In `fieldStamps` (`reduce.go:133-139`) replace `haveOTelTokens bool` with:

```go
	tokenRank   int
	haveToken   bool
```

Replace `applyTokens` (`reduce.go:197-211`) with:

```go
func (g *Graph) applyTokens(n *model.Node, attrs map[string]any, src model.Source) {
	fs := g.stampsFor(n.ID)
	r := tokenRank(src)
	if fs.haveToken && r < fs.tokenRank {
		return
	}
	fs.tokenRank = r
	fs.haveToken = true
	if v, ok := toInt64(attrs["tokens_in"]); ok {
		n.TokensIn = &v
	}
	if v, ok := toInt64(attrs["tokens_out"]); ok {
		n.TokensOut = &v
	}
}

func tokenRank(s model.Source) int {
	switch s {
	case model.SourceOTel:
		return 2
	case model.SourceStreamJSON:
		return 1
	default:
		return 0
	}
}
```

- [ ] **Step 8: Run the token tests to verify they pass**

Run: `go test ./reduce/ -run 'TestTokenRankOTelBeatsStreamJSON|TestTokenStreamJSONSetsWhenNoOTel'`
Expected: PASS.

- [ ] **Step 9: Write the failing test for the payload rank (hook full beats stream-json delta)**

```go
func sjToolInput(execID, runID, tuid, name, input string, seq uint64) model.Observation {
	pl := &model.Payload{Input: json.RawMessage(input)}
	pl.Hash = model.HashPayload(pl)
	return model.Observation{
		RunID: runID, ExecutionID: execID, Source: model.SourceStreamJSON,
		Kind: "assistant_tool_use", Correlation: model.Correlation{SessionID: runID, ToolUseID: tuid},
		Attrs: map[string]any{"name": name}, Payload: pl,
		EventTime: time.Unix(100, 0).UTC(), ObservedAt: time.Unix(100, 0).UTC(), Seq: seq,
	}
}

func hookToolInput(execID, runID, tuid, name, input string, seq uint64) model.Observation {
	pl := &model.Payload{Input: json.RawMessage(input)}
	pl.Hash = model.HashPayload(pl)
	return model.Observation{
		RunID: runID, ExecutionID: execID, Source: model.SourceHook,
		Kind: "assistant_tool_use", Correlation: model.Correlation{SessionID: runID, ToolUseID: tuid},
		Attrs: map[string]any{"name": name, "status": "running"}, Payload: pl,
		EventTime: time.Unix(100, 0).UTC(), ObservedAt: time.Unix(100, 0).UTC(), Seq: seq,
	}
}

func TestPayloadHookBeatsStreamJSONDelta(t *testing.T) {
	hookFull := hookToolInput("e1", "s1", "t1", "Bash", `{"command":"ls -la"}`, 1)
	sjDelta := sjToolInput("e1", "s1", "t1", "Bash", `{"command":"l"}`, 2)
	fwd := NewGraph()
	fwd.ApplyAll([]model.Observation{hookFull, sjDelta})
	rev := NewGraph()
	rev.ApplyAll([]model.Observation{sjDelta, hookFull})
	id := model.ToolCallID("e1", "t1")
	require.NotNil(t, fwd.Nodes[id].Payload)
	assert.JSONEq(t, `{"command":"ls -la"}`, string(fwd.Nodes[id].Payload.Input))
	assert.Equal(t, string(fwd.Nodes[id].Payload.Input), string(rev.Nodes[id].Payload.Input))
}

func TestPayloadStreamJSONSetsWhenAlone(t *testing.T) {
	g := NewGraph()
	g.Apply(sjToolInput("e1", "s1", "t1", "Bash", `{"command":"l"}`, 1))
	id := model.ToolCallID("e1", "t1")
	require.NotNil(t, g.Nodes[id].Payload)
	assert.JSONEq(t, `{"command":"l"}`, string(g.Nodes[id].Payload.Input))
}
```

`json` is already imported in `reduce_test.go` (used by `canonGraph`).

- [ ] **Step 10: Run the test to verify it fails**

Run: `go test ./reduce/ -run 'TestPayloadHookBeatsStreamJSONDelta|TestPayloadStreamJSONSetsWhenAlone'`
Expected: FAIL — `mergePayload` is last-writer-wins, so the lower-seq hook full payload is overwritten by the higher-seq stream-json delta in forward order.

- [ ] **Step 11: Make `mergePayload` a ranked `*Graph` method**

In `fieldStamps` add:

```go
	payloadRank int
	havePayload bool
```

Replace `mergePayload` (`reduce.go:180-195`) with:

```go
func (g *Graph) mergePayload(n *model.Node, p *model.Payload, src model.Source) {
	if p == nil {
		return
	}
	fs := g.stampsFor(n.ID)
	r := payloadRank(src)
	if fs.havePayload && r < fs.payloadRank {
		return
	}
	fs.payloadRank = r
	fs.havePayload = true
	if n.Payload == nil {
		n.Payload = &model.Payload{}
	}
	if len(p.Input) > 0 {
		n.Payload.Input = p.Input
	}
	if len(p.Output) > 0 {
		n.Payload.Output = p.Output
	}
	n.Payload.Hash = model.HashPayload(n.Payload)
	n.PayloadHash = n.Payload.Hash
}

func payloadRank(s model.Source) int {
	switch s {
	case model.SourceHook:
		return 1
	case model.SourceStreamJSON:
		return 0
	default:
		return 0
	}
}
```

Note: `payloadRank` returns `0` for both `SourceStreamJSON` and `default`; both are reachable in M2a (stream-json explicitly; default via OTel — OTel produces no payload today but `applyTool` is source-agnostic, and a future caller may pass OTel). Keep the explicit `SourceStreamJSON` case so M2b's `hook=JSONL=1, stream-json=0` insertion is a one-line edit and the intent is self-documenting. If `golangci-lint` `gocritic`/`unparam` flags the duplicate `0` arms as a single-case-collapse, collapse to `case model.SourceHook: return 1` + `default: return 0` and add a `TestPayloadRankStreamJSONIsDefault` asserting `payloadRank(model.SourceStreamJSON) == 0` to keep the stream-json value pinned.

Update the call site in `applyTool` (`reduce.go:101`): change `mergePayload(n, o.Payload)` to `g.mergePayload(n, o.Payload, o.Source)`.

- [ ] **Step 12: Run the payload tests to verify they pass**

Run: `go test ./reduce/ -run 'TestPayloadHookBeatsStreamJSONDelta|TestPayloadStreamJSONSetsWhenAlone'`
Expected: PASS.

- [ ] **Step 13: Extend the commutativity property test to all three live sources**

Add a new commutativity test that includes a stream-json observation conflicting on every ranked field group, asserting all permutations converge:

```go
func TestReductionCommutativityThreeSources(t *testing.T) {
	t0 := time.Unix(200, 0).UTC()
	obs := []model.Observation{
		sessionStartObs("e2", "s2", 1),
		hookTurn("e2", "s2", "m2", 5, 2, t0.Add(time.Minute), 2),
		otelTurn("e2", "s2", "m2", 50, 20, t0.Add(time.Second), 3),
		sjTurn("e2", "s2", "m2", 7, 9, 4),
		hookToolInput("e2", "s2", "t9", "Bash", `{"command":"ls -la"}`, 5),
		sjToolInput("e2", "s2", "t9", "Bash", `{"command":"l"}`, 6),
	}
	perms := permute(obs)
	var want string
	for i, p := range perms {
		g := NewGraph()
		g.ApplyAll(p)
		got := canonGraph(g)
		if i == 0 {
			want = got
			continue
		}
		assert.Equal(t, want, got, "permutation %d diverged", i)
	}
}
```

`sessionStartObs`, `hookTurn`, `otelTurn`, `permute`, `canonGraph` already exist in `reduce_test.go`. To exercise the payload-rank branch inside `canonGraph`, extend `canonGraph`'s `nodeView` with a `PayloadHash string` field set from `n.PayloadHash` (so a divergent payload would surface as a diff); add the assignment `v.PayloadHash = n.PayloadHash` in the node loop. This strengthens the existing `TestReductionCommutativity` too (still passes — those obs carry no conflicting payload).

- [ ] **Step 14: Run the test to verify it passes**

Run: `go test ./reduce/ -run TestReductionCommutativity -race -count=2`
Expected: PASS (both `TestReductionCommutativity` and `TestReductionCommutativityThreeSources`).

- [ ] **Step 15: Run the full reduce suite under -race + coverage + lint**

Run: `go test ./reduce/ -race -count=2` then `go test ./reduce/ -cover` then `golangci-lint run ./reduce/...`
Expected: all pass; `coverage: 100.0% of statements`; lint 0 issues. If a rank `default` arm is uncovered, add a one-line assertion test (e.g. `assert.Equal(t, 0, tokenRank(model.SourceHook))`), since hook tokens are legitimately rank-0.

- [ ] **Step 16: Commit**

```bash
git add reduce/reduce.go reduce/reduce_test.go
git commit -m "feat(reduce): per-field precedence over three live sources

Generalize sourceRank to a 3-tier timing order, add tokenRank and a
ranked mergePayload; no JSONL tier yet (added in M2b). Merge stays
commutative: every guard is a pure function of (rank, seq).

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 3: `parent_tool_use_id` → `parent_child` edge consumer (structure rank)

**Files:**

- Modify: `reduce/reduce.go` (`Apply` for `assistant_tool_use`/`tool_result` routing, `applyTool`, `upsertEdgeGated`, `fieldStamps`)
- Test: `reduce/reduce_test.go`

**Interfaces:**

- Consumes: `model.Correlation.ParentToolUseID` (existing, `model/model.go:56`); `model.ToolCallID`; `sourceRank` (Task 2); `model.SourceStreamJSON`, `model.SourceOTel`.
- Produces:
  - `func structureRank(s model.Source) int` — `SourceOTel=2`, `SourceStreamJSON=1`, default `0`. (M2b inserts JSONL above OTel; do NOT add JSONL here.)
  - `fieldStamps` gains `edgeStructRank map[string]int` keyed by edge id, recording the rank of the source that created/last-promoted a `parent_child` edge.
  - `func (g *Graph) upsertParentToolEdge(o model.Observation)` — when `o.Correlation.ParentToolUseID != ""`, upserts a `parent_child` edge from `model.ToolCallID(execID, parentToolUseID)` to the child `tool_call` node, subject to `structureRank`: a lower-ranked source never re-parents over an edge created by a higher-ranked source.

**WHY:** This is the stream-json structural contribution (spec §6.4). stream-json `stream_event` carries `parent_tool_use_id` (the SDK subagent boundary). In M2a the live structure producers are stream-json (rank 1) and any OTel parent (rank 2, via the existing `upsertEdgeGated` #53954 gate). M2b adds JSONL structure (the #53954 truth) ABOVE OTel. The guard ensures a stream-json edge never overwrites a higher-ranked edge but is created when none exists.

- [ ] **Step 1: Write the failing test — stream-json creates a parent_child edge**

```go
func sjStreamEvent(execID, runID, childTUID, parentTUID string, seq uint64) model.Observation {
	return model.Observation{
		RunID: runID, ExecutionID: execID, Source: model.SourceStreamJSON,
		Kind: "assistant_tool_use",
		Correlation: model.Correlation{SessionID: runID, ToolUseID: childTUID, ParentToolUseID: parentTUID},
		EventTime: time.Unix(100, 0).UTC(), ObservedAt: time.Unix(100, 0).UTC(), Seq: seq,
	}
}

func TestParentToolUseEdgeCreated(t *testing.T) {
	parent := sjToolInput("e1", "s1", "tparent", "Task", `{}`, 1)
	child := sjStreamEvent("e1", "s1", "tchild", "tparent", 2)
	g := NewGraph()
	g.ApplyAll([]model.Observation{parent, child})
	id := model.EdgeID("e1", model.EdgeParentChild, model.ToolCallID("e1", "tparent"), model.ToolCallID("e1", "tchild"))
	require.NotNil(t, g.Edges[id])
	assert.Equal(t, model.ToolCallID("e1", "tparent"), g.Edges[id].Src)
	assert.Equal(t, model.ToolCallID("e1", "tchild"), g.Edges[id].Dst)
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./reduce/ -run TestParentToolUseEdgeCreated`
Expected: FAIL — `ParentToolUseID` has no consumer; the edge is never created.

- [ ] **Step 3: Add `structureRank`, the edge-rank stamp map, and `upsertParentToolEdge`; call it from `applyTool`**

Add to `NewGraph` initialization is NOT needed (the map lives inside the lazily-created `fieldStamps`; initialize it on first use). In `fieldStamps` add:

```go
	edgeStructRank map[string]int
```

Add these functions:

```go
func structureRank(s model.Source) int {
	switch s {
	case model.SourceOTel:
		return 2
	case model.SourceStreamJSON:
		return 1
	default:
		return 0
	}
}

func (g *Graph) upsertParentToolEdge(o model.Observation) {
	if o.Correlation.ParentToolUseID == "" {
		return
	}
	src := model.ToolCallID(o.ExecutionID, o.Correlation.ParentToolUseID)
	dst := model.ToolCallID(o.ExecutionID, o.Correlation.ToolUseID)
	if src == "" || dst == "" || o.Correlation.ToolUseID == "" {
		return
	}
	fs := g.stampsFor(dst)
	if fs.edgeStructRank == nil {
		fs.edgeStructRank = map[string]int{}
	}
	id := model.EdgeID(o.ExecutionID, model.EdgeParentChild, src, dst)
	r := structureRank(o.Source)
	if cur, ok := fs.edgeStructRank[id]; ok && r < cur {
		return
	}
	fs.edgeStructRank[id] = r
	g.upsertEdge(o.ExecutionID, o.RunID, src, dst, o.Seq)
}
```

In `applyTool` (`reduce.go`), after the existing `g.upsertEdgeGated(o, parent, id)` call (line 107), add:

```go
	g.upsertParentToolEdge(o)
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./reduce/ -run TestParentToolUseEdgeCreated`
Expected: PASS.

- [ ] **Step 5: Write the failing tests — rank guard (lower never overwrites higher; higher promotes; no-op when child has no tool_use_id)**

```go
func otelChildEdge(execID, runID, childTUID, parentTUID string, seq uint64) model.Observation {
	return model.Observation{
		RunID: runID, ExecutionID: execID, Source: model.SourceOTel,
		Kind: "assistant_tool_use",
		Correlation: model.Correlation{SessionID: runID, ToolUseID: childTUID, ParentToolUseID: parentTUID},
		Attrs: map[string]any{"name": "Task"},
		EventTime: time.Unix(100, 0).UTC(), ObservedAt: time.Unix(100, 0).UTC(), Seq: seq,
	}
}

func TestParentToolUseStreamJSONDoesNotOverwriteOTelEdge(t *testing.T) {
	otelEdge := otelChildEdge("e1", "s1", "tchild", "tparentA", 1)
	sjEdge := sjStreamEvent("e1", "s1", "tchild", "tparentB", 2)
	fwd := NewGraph()
	fwd.ApplyAll([]model.Observation{otelEdge, sjEdge})
	rev := NewGraph()
	rev.ApplyAll([]model.Observation{sjEdge, otelEdge})
	wantID := model.EdgeID("e1", model.EdgeParentChild, model.ToolCallID("e1", "tparentA"), model.ToolCallID("e1", "tchild"))
	loseID := model.EdgeID("e1", model.EdgeParentChild, model.ToolCallID("e1", "tparentB"), model.ToolCallID("e1", "tchild"))
	require.NotNil(t, fwd.Edges[wantID])
	require.NotNil(t, rev.Edges[wantID])
	assert.Nil(t, fwd.Edges[loseID])
	assert.Nil(t, rev.Edges[loseID])
}

func TestParentToolUseNoChildID(t *testing.T) {
	o := model.Observation{
		RunID: "s1", ExecutionID: "e1", Source: model.SourceStreamJSON,
		Kind: "assistant_tool_use",
		Correlation: model.Correlation{SessionID: "s1", ParentToolUseID: "tparent"},
		EventTime: time.Unix(100, 0).UTC(), ObservedAt: time.Unix(100, 0).UTC(), Seq: 1,
	}
	g := NewGraph()
	g.Apply(o)
	loseID := model.EdgeID("e1", model.EdgeParentChild, model.ToolCallID("e1", "tparent"), model.ToolCallID("e1", ""))
	assert.Nil(t, g.Edges[loseID])
}
```

Note: `TestParentToolUseStreamJSONDoesNotOverwriteOTelEdge` uses two DIFFERENT parents (A from OTel, B from stream-json) so the guard is genuinely exercised — the OTel edge to A must win and the stream-json edge to B must be absent regardless of order. The OTel observation also carries `tool_use_id` so its own `upsertEdgeGated` #53954 gate (which is separate, parent=turn/session) is not what is under test; the parent-tool edge is the subject.

- [ ] **Step 6: Run the tests to verify they fail**

Run: `go test ./reduce/ -run 'TestParentToolUseStreamJSONDoesNotOverwriteOTelEdge|TestParentToolUseNoChildID'`
Expected: FAIL on the overwrite test (stream-json edge to B is created because there is no rank guard against a *different* edge id). `TestParentToolUseNoChildID` already passes (guard `o.Correlation.ToolUseID == ""` exists).

- [ ] **Step 7: Verify the guard semantics handle the different-parent case**

The guard in Step 3 keys `edgeStructRank` by the FULL edge id (`src→dst`), so a stream-json edge to a *different* parent B is a *different* id and would NOT be blocked by the A-edge stamp. To make "highest-ranked source owns the child's parent" hold, change `edgeStructRank` to be keyed by the CHILD node id (one parent per child by precedence). Replace the map type usage: `fs.edgeStructRank` becomes a single `int` + `bool` per child stamp instead of a map. Update `fieldStamps`:

```go
	structRank int
	haveStruct bool
```

Remove the `edgeStructRank map[string]int` field. Rewrite `upsertParentToolEdge`:

```go
func (g *Graph) upsertParentToolEdge(o model.Observation) {
	if o.Correlation.ParentToolUseID == "" || o.Correlation.ToolUseID == "" {
		return
	}
	src := model.ToolCallID(o.ExecutionID, o.Correlation.ParentToolUseID)
	dst := model.ToolCallID(o.ExecutionID, o.Correlation.ToolUseID)
	fs := g.stampsFor(dst)
	r := structureRank(o.Source)
	if fs.haveStruct && r < fs.structRank {
		return
	}
	fs.structRank = r
	fs.haveStruct = true
	g.upsertEdge(o.ExecutionID, o.RunID, src, dst, o.Seq)
}
```

This makes the child's parent edge source-precedence-aware: the highest-ranked source's parent wins; a lower-ranked later source is blocked; equal rank re-upserts (idempotent, `Rev` raised by `seq` — commutative). Note: a lower-ranked source pointing at a *different* parent is now correctly blocked (the child already has a higher-ranked parent stamp), and the stale edge to B is never created in either order.

- [ ] **Step 8: Run the tests to verify they pass**

Run: `go test ./reduce/ -run 'TestParentToolUseEdgeCreated|TestParentToolUseStreamJSONDoesNotOverwriteOTelEdge|TestParentToolUseNoChildID'`
Expected: PASS.

- [ ] **Step 9: Add a commutativity test including the structure edge**

```go
func TestReductionCommutativityWithParentToolEdge(t *testing.T) {
	obs := []model.Observation{
		sessionStartObs("e3", "s3", 1),
		sjToolInput("e3", "s3", "tp", "Task", `{}`, 2),
		sjStreamEvent("e3", "s3", "tc", "tp", 3),
		otelChildEdge("e3", "s3", "tc", "tp", 4),
	}
	perms := permute(obs)
	var want string
	for i, p := range perms {
		g := NewGraph()
		g.ApplyAll(p)
		got := canonGraph(g)
		if i == 0 {
			want = got
			continue
		}
		assert.Equal(t, want, got, "permutation %d diverged", i)
	}
}
```

This converges because the OTel structure rank (2) always wins over stream-json (1) regardless of arrival order, and the edge `Rev` is `max(seq)` — order-independent.

- [ ] **Step 10: Run the full reduce suite under -race + coverage + lint**

Run: `go test ./reduce/ -race -count=2` then `go test ./reduce/ -cover` then `golangci-lint run ./reduce/...`
Expected: all pass; `coverage: 100.0% of statements`; lint 0. If `structureRank`'s `default` arm is uncovered, add `assert.Equal(t, 0, structureRank(model.SourceHook))`.

- [ ] **Step 11: Commit**

```bash
git add reduce/reduce.go reduce/reduce_test.go
git commit -m "feat(reduce): consume parent_tool_use_id into a precedence-ranked parent_child edge

stream-json structure (rank 1) creates a tool->tool parent edge unless a
higher-ranked source (OTel rank 2; JSONL added in M2b) already owns the
child's parent. Commutative: highest-rank source wins, Rev=max(seq).

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 4: `POST /v1/stream-json` handler + `Daemon.IngestStreamJSON`

**Files:**

- Modify: `daemon/daemon.go` (add `streamParseFn` seam + `IngestStreamJSON`)
- Modify: `daemon/server.go` (register route + `handleStreamJSON`)
- Test: `daemon/daemon_test.go`, `daemon/server_test.go`

**Interfaces:**

- Consumes: `ingest/streamjson.Parse` (Task 1); `d.execBySession`, `d.newExecID`, `d.next`, `d.applyAndPersist`, `d.quarantine`, `d.store.ObservationsForExecution` (existing daemon internals); `d.authed` (existing middleware).
- Produces:
  - `func (d *Daemon) IngestStreamJSON(line []byte, sessionID string) error` — mirrors `IngestOTLP`: under `d.mu` with a `recover()` quarantine/fail-open; resolves `sessionID → execID` (minting via `d.newExecID`); calls `streamParseFn(line, execID, d.next)` (quarantine on error → return nil); lazy-reloads the shard when `known && !inMem`; `applyAndPersist` per obs + `d.lastSeen` update.
  - package var `streamParseFn = streamjson.Parse` (the parse-error seam, mirroring `parseFn`).
  - `func (d *Daemon) handleStreamJSON(w http.ResponseWriter, r *http.Request)` — reads the streamed NDJSON body with `bufio.Scanner` (1 MiB initial / 16 MiB max buffer, matching `ingest/jsonl.go:56`), extracts `session_id` per line, calls `d.IngestStreamJSON(line, sessionID)` per non-empty line; HTTP 200 on clean end / body scan error → it has already ingested what it read, still 200 with the consumed lines (a body read error mid-stream is logged, not a crash); a per-request `recover()` guards the whole handler.

**NOTE on session_id resolution:** stream-json repeats `session_id` on every envelope (spec §4.1), so the handler extracts it per line via a tiny local struct (mirror `sessionIDOf` in `daemon.go:317`). A line with an empty `session_id` resolves to the empty-string key (a new minted exec), exactly as the hook path does for a missing `session_id`. This is acceptable: the first `system.init` line carries it and subsequent lines repeat it.

- [ ] **Step 1: Write the failing test for `IngestStreamJSON` happy path**

```go
func TestIngestStreamJSONSessionInit(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.IngestStreamJSON([]byte(`{"type":"system","subtype":"init","session_id":"s1","model":"claude-opus-4-8"}`), "s1"))
	execID := d.execForTest("s1")
	require.Equal(t, "exec1", execID)
	n := d.GraphsForTest()[execID].Nodes[model.SessionNodeID(execID)]
	require.NotNil(t, n)
	assert.Equal(t, model.StatusRunning, n.Status)
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./daemon/ -run TestIngestStreamJSONSessionInit`
Expected: FAIL — `d.IngestStreamJSON` undefined.

- [ ] **Step 3: Add the `streamParseFn` seam + `IngestStreamJSON`**

In `daemon/daemon.go`, add the import `streamjsoningest "github.com/realkarych/catacomb/ingest/streamjson"` and the seam in the `var (...)` block (after `parseFn`):

```go
	streamParseFn = streamjsoningest.Parse
```

Add the method (mirror `IngestOTLP`):

```go
func (d *Daemon) IngestStreamJSON(line []byte, sessionID string) (err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	defer func() {
		if r := recover(); r != nil {
			d.quarantine("stream-json", line, fmt.Sprintf("panic: %v", r))
			err = nil
		}
	}()
	execID, known := d.execBySession[sessionID]
	if !known {
		execID = d.newExecID()
		d.execBySession[sessionID] = execID
	}
	obs, err := streamParseFn(line, execID, d.next)
	if err != nil {
		d.quarantine("stream-json", line, err.Error())
		return nil
	}
	g, inMem := d.graphs[execID]
	if !inMem {
		g = reduce.NewGraph()
		if known {
			prior, loadErr := d.store.ObservationsForExecution(execID)
			if loadErr != nil {
				d.quarantine("stream-json", line, loadErr.Error())
				return nil
			}
			g.ApplyAll(prior)
			_ = g.DrainDeltas()
		}
		d.graphs[execID] = g
	}
	for _, o := range obs {
		if err := d.applyAndPersist(g, o); err != nil {
			d.quarantine("stream-json", line, err.Error())
			return nil
		}
		d.lastSeen[o.RunID] = o.ObservedAt
	}
	return nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./daemon/ -run TestIngestStreamJSONSessionInit`
Expected: PASS.

- [ ] **Step 5: Write the failing tests for the cross-source merge + parse-error seam + reload + panic + persist-error**

```go
func TestIngestStreamJSONMergesByToolUseIDWithHook(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"toolu_x","tool_input":{}}`)))
	line := []byte(`{"type":"assistant","session_id":"s1","message":{"id":"m1","content":[{"type":"tool_use","id":"toolu_x","name":"Bash","input":{"command":"ls"}}]}}`)
	require.NoError(t, d.IngestStreamJSON(line, "s1"))
	execID := d.execForTest("s1")
	n := d.GraphsForTest()[execID].Nodes[model.ToolCallID(execID, "toolu_x")]
	require.NotNil(t, n)
	assert.Len(t, n.Sources, 2)
}

func TestIngestStreamJSONParseErrorViaSeam(t *testing.T) {
	orig := streamParseFn
	streamParseFn = func(_ []byte, _ string, _ func() uint64) ([]model.Observation, error) {
		return nil, errors.New("parse fail")
	}
	t.Cleanup(func() { streamParseFn = orig })
	s := tempStore(t)
	d := New(s)
	require.NoError(t, d.IngestStreamJSON([]byte(`{"type":"system","subtype":"init","session_id":"s1"}`), "s1"))
	n, err := s.QuarantineCount()
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
}

func TestIngestStreamJSONBadJSONQuarantines(t *testing.T) {
	s := tempStore(t)
	d := New(s)
	require.NoError(t, d.IngestStreamJSON([]byte(`{not json`), "s1"))
	n, err := s.QuarantineCount()
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
}

func TestIngestStreamJSONPanic(t *testing.T) {
	orig := applyFn
	applyFn = func(*reduce.Graph, model.Observation) { panic("sj-boom") }
	t.Cleanup(func() { applyFn = orig })
	s := tempStore(t)
	d := New(s)
	require.NoError(t, d.IngestStreamJSON([]byte(`{"type":"system","subtype":"init","session_id":"s1"}`), "s1"))
	n, err := s.QuarantineCount()
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
}

func TestIngestStreamJSONPersistError(t *testing.T) {
	s := &appendErrStore{Store: tempStore(t)}
	d := New(s)
	require.NoError(t, d.IngestStreamJSON([]byte(`{"type":"system","subtype":"init","session_id":"s1"}`), "s1"))
	n, err := s.QuarantineCount()
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
}

func TestIngestStreamJSONReloadsEvictedShard(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.IngestStreamJSON([]byte(`{"type":"system","subtype":"init","session_id":"s1"}`), "s1"))
	execID := d.execForTest("s1")
	d.dropShardForTest("s1")
	require.NoError(t, d.IngestStreamJSON([]byte(`{"type":"assistant","session_id":"s1","message":{"id":"m1","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{}}]}}`), "s1"))
	g := d.GraphsForTest()[execID]
	require.NotNil(t, g)
	require.NotNil(t, g.Nodes[model.SessionNodeID(execID)])
	require.NotNil(t, g.Nodes[model.ToolCallID(execID, "t1")])
}

func TestIngestStreamJSONReloadError(t *testing.T) {
	base := tempStore(t)
	d := New(base)
	fixedExecID(d)
	require.NoError(t, d.IngestStreamJSON([]byte(`{"type":"system","subtype":"init","session_id":"s1"}`), "s1"))
	d.dropShardForTest("s1")
	d.store = &reloadErrStore{Store: base}
	require.NoError(t, d.IngestStreamJSON([]byte(`{"type":"assistant","session_id":"s1","message":{"id":"m1"}}`), "s1"))
	assert.Equal(t, int64(1), d.QuarantinedForTest())
}
```

`appendErrStore`, `quarantineErrStore`, and `reloadErrStore` already exist in `daemon_test.go` (used by the OTLP tests). Reuse them; do not redefine.

- [ ] **Step 6: Run the tests to verify they fail/pass appropriately**

Run: `go test ./daemon/ -run 'TestIngestStreamJSON'`
Expected: the merge / parse-error-seam / reload tests pass once the method exists; confirm all green (these exercise branches already implemented in Step 3).

- [ ] **Step 7: Write the failing test for the HTTP handler (streamed NDJSON body)**

```go
func TestStreamJSONHTTPEndpoint(t *testing.T) {
	d := New(tempStore(t))
	body := strings.NewReader(`{"type":"system","subtype":"init","session_id":"s1","model":"m"}
{"type":"assistant","session_id":"s1","message":{"id":"m1","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{}}]}}
`)
	r := httptest.NewRequest(http.MethodPost, "/v1/stream-json", body)
	r.Header.Set("Authorization", "Bearer tok")
	rec := httptest.NewRecorder()
	d.Handler("tok").ServeHTTP(rec, r)
	assert.Equal(t, http.StatusOK, rec.Code)
	require.Eventually(t, func() bool { return len(d.GraphsForTest()) == 1 }, time.Second, 10*time.Millisecond)
}

func TestStreamJSONHTTPUnauthorized(t *testing.T) {
	d := New(tempStore(t))
	r := httptest.NewRequest(http.MethodPost, "/v1/stream-json", strings.NewReader(""))
	rec := httptest.NewRecorder()
	d.Handler("tok").ServeHTTP(rec, r)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestStreamJSONHTTPBlankLinesSkipped(t *testing.T) {
	d := New(tempStore(t))
	body := strings.NewReader("\n\n{\"type\":\"system\",\"subtype\":\"init\",\"session_id\":\"s1\"}\n\n")
	r := httptest.NewRequest(http.MethodPost, "/v1/stream-json", body)
	r.Header.Set("Authorization", "Bearer tok")
	rec := httptest.NewRecorder()
	d.Handler("tok").ServeHTTP(rec, r)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Len(t, d.GraphsForTest(), 1)
}

func TestStreamJSONHTTPBodyReadError(t *testing.T) {
	d := New(tempStore(t))
	r := httptest.NewRequest(http.MethodPost, "/v1/stream-json", errReadCloser{})
	r.Header.Set("Authorization", "Bearer tok")
	rec := httptest.NewRecorder()
	d.Handler("tok").ServeHTTP(rec, r)
	assert.Equal(t, http.StatusOK, rec.Code)
}
```

`errReadCloser{}` already exists in `server_test.go` (used by `TestOTLPHTTPBodyReadError`); reuse it. A scan error from `errReadCloser` is swallowed by the handler (it already ingested zero lines) and returns 200 — fail-open. `strings` and `time` are already imported in `server_test.go`.

- [ ] **Step 8: Run the handler tests to verify they fail**

Run: `go test ./daemon/ -run 'TestStreamJSONHTTP'`
Expected: FAIL — no `/v1/stream-json` route (401/404).

- [ ] **Step 9: Register the route + add `handleStreamJSON`**

In `daemon/server.go`, add to the imports `"bufio"` and `"github.com/realkarych/catacomb/model"` is NOT needed (use a local struct). Register the route in `Handler` (after the `/v1/traces` line):

```go
	mux.HandleFunc("POST /v1/stream-json", d.authed(token, d.handleStreamJSON))
```

Add the handler + a per-line session-id extractor:

```go
func (d *Daemon) handleStreamJSON(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("catacomb: stream-json handler recovered: %v", rec)
		}
	}()
	sc := bufio.NewScanner(r.Body)
	sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			continue
		}
		buf := make([]byte, len(trimmed))
		copy(buf, trimmed)
		_ = d.IngestStreamJSON(buf, streamSessionID(buf))
	}
	if err := sc.Err(); err != nil {
		log.Printf("catacomb: stream-json scan: %v", err)
	}
	w.WriteHeader(http.StatusOK)
}

func streamSessionID(line []byte) string {
	var e struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(line, &e); err != nil {
		return ""
	}
	return e.SessionID
}
```

Add `"bytes"` to the imports if not present (it is not currently imported in `server.go` — add it; `encoding/json` and `log` already are). The `buf := make + copy` is required because `sc.Bytes()` reuses its backing array across iterations and `IngestStreamJSON` quarantines the raw bytes.

- [ ] **Step 10: Run the handler tests to verify they pass**

Run: `go test ./daemon/ -run 'TestStreamJSONHTTP'`
Expected: PASS.

- [ ] **Step 11: Run the full daemon suite under -race + coverage + lint**

Run: `go test ./daemon/ -race -count=2` then `go test ./daemon/ -cover` then `golangci-lint run ./daemon/...`
Expected: all pass; `coverage: 100.0% of statements`; lint 0. If `streamSessionID`'s error branch is uncovered, add a handler test feeding a non-JSON line (it ingests with an empty session id and 200s). If `sc.Err()` log branch is uncovered, the `errReadCloser` test in Step 7 covers it.

- [ ] **Step 12: Commit**

```bash
git add daemon/daemon.go daemon/server.go daemon/daemon_test.go daemon/server_test.go
git commit -m "feat(daemon): POST /v1/stream-json + IngestStreamJSON (mirrors IngestOTLP)

Streamed NDJSON body read line-by-line under recover/quarantine fail-open;
session_id resolved per line; lazy shard reload; streamParseFn seam.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 5: `catacomb ingest stream-json` + `catacomb run -- <cmd...>` CLI verbs

**Files:**

- Create: `cmd/catacomb/streamjson.go` (both verbs + shared streamed-POST forwarder)
- Modify: `cmd/catacomb/root.go` (register the two commands)
- Test: `cmd/catacomb/streamjson_test.go`

**Interfaces:**

- Consumes: `daemon.ReadDiscovery`, `daemon.DiscoveryPath` (existing); the running test daemon via `runTestDaemon(t)` (exists in `hook_test.go`); `github.com/spf13/cobra`.
- Produces:
  - `func newIngestCmd() *cobra.Command` — parent `ingest` with subcommand `stream-json` that reads stdin NDJSON and streamed-POSTs to `/v1/stream-json`.
  - `func newRunCmd() *cobra.Command` — `run -- <cmd...>` that execs `<cmd...>`, tees the child's stdout to `os.Stdout` AND forwards it to `/v1/stream-json`, passes through stderr/stdin, sets `CATACOMB_RUN_ID`, exits with the child's exit code.
  - `func streamForward(warn io.Writer, discoveryPath string, body io.Reader)` — the shared fail-open streamed-POST helper (mirrors `forward` in `hook.go`).
  - seams for tests: `var execCommand = exec.Command` (subprocess factory) and `var streamHTTPClient = &http.Client{}` (so tests run without a real `claude` or, for fail-open, without a daemon).

**NOTE — daemon never spawns:** `catacomb run` is the user-invoked wrapper. The child's stdout is wrapped in an `io.TeeReader(childStdout, pipeW)`; a goroutine drains the tee to `os.Stdout` (so the user sees output live) while `streamForward` reads the other end of the pipe into the streamed POST body. If the daemon is down, `streamForward` writes a warning to stderr and returns; the child still runs and tees to the terminal (fail-open). The wrapper's exit code is the child's.

- [ ] **Step 1: Write the failing test for `ingest stream-json` delivery + fail-open**

```go
package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/daemon"
)

func TestStreamForwardDelivers(t *testing.T) {
	d, discovery := runTestDaemon(t)
	var warn bytes.Buffer
	body := bytes.NewReader([]byte(`{"type":"system","subtype":"init","session_id":"s1"}` + "\n"))
	streamForward(&warn, discovery, body)
	assert.Empty(t, warn.String())
	require.Eventually(t, func() bool { return len(d.GraphsForTest()) == 1 }, 2*time.Second, 10*time.Millisecond)
}

func TestStreamForwardMissingDiscovery(t *testing.T) {
	var warn bytes.Buffer
	streamForward(&warn, filepath.Join(t.TempDir(), "nope.json"), bytes.NewReader([]byte(`{}`)))
	assert.Contains(t, warn.String(), "discovery")
}

func TestStreamForwardDaemonDown(t *testing.T) {
	ln, err := daemon.ListenLoopback()
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())
	discovery := filepath.Join(t.TempDir(), "d.json")
	require.NoError(t, daemon.WriteDiscovery(discovery, daemon.Discovery{Addr: addr, Token: "t"}))
	var warn bytes.Buffer
	streamForward(&warn, discovery, bytes.NewReader([]byte(`{}`)))
	assert.Contains(t, warn.String(), "forward")
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./cmd/catacomb/ -run TestStreamForward`
Expected: FAIL — `streamForward` undefined.

- [ ] **Step 3: Write `streamForward` + `newIngestCmd`**

Create `cmd/catacomb/streamjson.go`:

```go
package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/realkarych/catacomb/daemon"
)

var (
	execCommand      = exec.Command
	streamHTTPClient = &http.Client{}
)

func streamForward(warn io.Writer, discoveryPath string, body io.Reader) {
	d, err := daemon.ReadDiscovery(discoveryPath)
	if err != nil {
		fmt.Fprintf(warn, "catacomb stream-json: discovery: %v\n", err)
		return
	}
	req, err := http.NewRequest(http.MethodPost, "http://"+d.Addr+"/v1/stream-json", body)
	if err != nil {
		fmt.Fprintf(warn, "catacomb stream-json: request: %v\n", err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+d.Token)
	req.Header.Set("Content-Type", "application/x-ndjson")
	resp, err := streamHTTPClient.Do(req)
	if err != nil {
		fmt.Fprintf(warn, "catacomb stream-json: forward to %s: %v\n", d.Addr, err)
		return
	}
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		fmt.Fprintf(warn, "catacomb stream-json: forward to %s: status %d\n", d.Addr, resp.StatusCode)
	}
}

func newIngestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ingest",
		Short: "Forward Claude Code output to the catacomb daemon",
	}
	sj := &cobra.Command{
		Use:   "stream-json",
		Short: "Forward stream-json NDJSON from stdin to the catacomb daemon",
		RunE: func(cmd *cobra.Command, _ []string) error {
			streamForward(cmd.ErrOrStderr(), daemon.DiscoveryPath(), cmd.InOrStdin())
			return nil
		},
	}
	cmd.AddCommand(sj)
	return cmd
}

var _ = os.Stdout
```

Note: the `var _ = os.Stdout` placeholder keeps `os` imported until Step 6 adds `newRunCmd` (which uses `os.Stdout`/`os.Environ`); remove it in Step 6. Likewise `execCommand` is unused until Step 6 — to avoid an `unused`/`unparam` lint failure between commits, write Steps 3 and 6 in the SAME commit (commit only at Step 11). Do not run `golangci-lint` between Step 3 and Step 6.

- [ ] **Step 4: Run the `streamForward` tests to verify they pass**

Run: `go test ./cmd/catacomb/ -run TestStreamForward`
Expected: PASS.

- [ ] **Step 5: Write the failing test for `catacomb run` (tee + forward + exit code + fail-open)**

The test uses a fake subprocess via the standard `TestHelperProcess` trick (no real `claude`):

```go
func TestRunTeesAndForwards(t *testing.T) {
	d, discovery := runTestDaemon(t)
	t.Setenv("CATACOMB_DISCOVERY", discovery)
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		cs := append([]string{"-test.run=TestHelperProcess", "--", name}, args...)
		c := exec.Command(os.Args[0], cs...)
		c.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
		return c
	}
	t.Cleanup(func() { execCommand = orig })

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"run", "--", "claude", "-p"})
	require.NoError(t, root.Execute())
	require.Eventually(t, func() bool { return len(d.GraphsForTest()) == 1 }, 2*time.Second, 10*time.Millisecond)
}

func TestRunExitCodePropagates(t *testing.T) {
	discovery := filepath.Join(t.TempDir(), "d.json")
	t.Setenv("CATACOMB_DISCOVERY", discovery)
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		cs := append([]string{"-test.run=TestHelperProcess", "--", "FAIL", name}, args...)
		c := exec.Command(os.Args[0], cs...)
		c.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
		return c
	}
	t.Cleanup(func() { execCommand = orig })
	err := runChild(io.Discard, io.Discard, discovery, "", []string{"claude"})
	var ee *exec.ExitError
	require.ErrorAs(t, err, &ee)
	assert.Equal(t, 7, ee.ExitCode())
}

func TestRunSetsRunID(t *testing.T) {
	discovery := filepath.Join(t.TempDir(), "d.json")
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		cs := append([]string{"-test.run=TestHelperProcess", "--", "ENV", name}, args...)
		c := exec.Command(os.Args[0], cs...)
		c.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
		return c
	}
	t.Cleanup(func() { execCommand = orig })
	var out bytes.Buffer
	_ = runChild(&out, io.Discard, discovery, "run-42", []string{"claude"})
	assert.Contains(t, out.String(), "CATACOMB_RUN_ID=run-42")
}

func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	args := os.Args
	for i, a := range args {
		if a == "--" {
			args = args[i+1:]
			break
		}
	}
	switch {
	case len(args) > 0 && args[0] == "FAIL":
		os.Exit(7)
	case len(args) > 0 && args[0] == "ENV":
		for _, e := range os.Environ() {
			if len(e) >= 16 && e[:15] == "CATACOMB_RUN_ID" {
				fmt.Fprintln(os.Stdout, e)
			}
		}
		os.Exit(0)
	default:
		fmt.Fprintln(os.Stdout, `{"type":"system","subtype":"init","session_id":"s1"}`)
		os.Exit(0)
	}
}
```

- [ ] **Step 6: Run the run-tests to verify they fail**

Run: `go test ./cmd/catacomb/ -run 'TestRun|TestHelperProcess'`
Expected: FAIL — `newRunCmd`/`runChild` undefined.

- [ ] **Step 7: Implement `newRunCmd` + `runChild`; remove the `os` placeholder**

Replace `var _ = os.Stdout` with the run command. Add to `cmd/catacomb/streamjson.go`:

```go
func newRunCmd() *cobra.Command {
	var runID string
	cmd := &cobra.Command{
		Use:                "run -- <cmd...>",
		Short:              "Run a Claude Code command, tee its stream-json to the terminal and the daemon",
		Args:               cobra.MinimumNArgs(1),
		DisableFlagParsing: false,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runChild(cmd.OutOrStdout(), cmd.ErrOrStderr(), daemon.DiscoveryPath(), runID, args)
		},
	}
	cmd.Flags().StringVar(&runID, "run-id", "", "CATACOMB_RUN_ID value exported to the child for multi-session grouping")
	return cmd
}

func runChild(stdout, stderr io.Writer, discoveryPath, runID string, args []string) error {
	child := execCommand(args[0], args[1:]...)
	child.Stderr = stderr
	child.Stdin = os.Stdin
	child.Env = os.Environ()
	if runID != "" {
		child.Env = append(child.Env, "CATACOMB_RUN_ID="+runID)
	}
	pipe, err := child.StdoutPipe()
	if err != nil {
		return err
	}
	pr, pw := io.Pipe()
	tee := io.TeeReader(pipe, pw)
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(stdout, tee)
		_ = pw.Close()
		close(done)
	}()
	go streamForward(stderr, discoveryPath, pr)
	if err := child.Start(); err != nil {
		_ = pw.Close()
		return err
	}
	waitErr := child.Wait()
	<-done
	_ = pr.Close()
	return waitErr
}
```

Now `os` and `execCommand` are both used; delete the `var _ = os.Stdout` line.

- [ ] **Step 8: Register the commands in `root.go`**

In `cmd/catacomb/root.go`, add to `newRootCmd`:

```go
	root.AddCommand(newIngestCmd())
	root.AddCommand(newRunCmd())
```

- [ ] **Step 9: Run the run-tests to verify they pass**

Run: `go test ./cmd/catacomb/ -run 'TestRun|TestStreamForward'`
Expected: PASS.

- [ ] **Step 10: Add a command-wiring test for `ingest stream-json` and verify full-package coverage**

```go
func TestIngestStreamJSONCommandWiring(t *testing.T) {
	_, discovery := runTestDaemon(t)
	t.Setenv("CATACOMB_DISCOVERY", discovery)
	root := newRootCmd()
	root.SetArgs([]string{"ingest", "stream-json"})
	root.SetIn(bytes.NewReader([]byte(`{"type":"system","subtype":"init","session_id":"s9"}` + "\n")))
	var errOut bytes.Buffer
	root.SetErr(&errOut)
	require.NoError(t, root.Execute())
}

func TestRunChildStartError(t *testing.T) {
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		return exec.Command(filepath.Join(t.TempDir(), "does-not-exist-binary"))
	}
	t.Cleanup(func() { execCommand = orig })
	err := runChild(io.Discard, io.Discard, filepath.Join(t.TempDir(), "d.json"), "", []string{"nope"})
	require.Error(t, err)
}
```

`TestRunChildStartError` covers the `child.Start()` error branch (a non-existent binary fails to start). The `child.StdoutPipe()` error branch is not reachable on a fresh `*exec.Cmd` (only errors if Stdout was already set), so do NOT write an unreachable test for it — instead set `child.Stdout` is never done, leaving `StdoutPipe` infallible here; if `go test -cover` reports the `StdoutPipe` error `return err` as uncovered, refactor to drop that branch by asserting `pipe, _ := child.StdoutPipe()` is NOT acceptable (errcheck bans it). Instead keep the check and cover it: add a test that pre-sets `child.Stdout` via a seam. Simplest covered design: change `runChild` to build the pipe with `io.Pipe()` and assign `child.Stdout = pw` directly (no `StdoutPipe`), eliminating the fallible call entirely:

```go
	pr, pw := io.Pipe()
	child.Stdout = io.MultiWriter(stdout, pw)
	go streamForward(stderr, discoveryPath, pr)
	if err := child.Start(); err != nil {
		_ = pw.Close()
		_ = pr.Close()
		return err
	}
	waitErr := child.Wait()
	_ = pw.Close()
	_ = pr.Close()
	return waitErr
```

Adopt THIS form in Step 7 (it is simpler and fully coverable: `child.Stdout = io.MultiWriter(os.Stdout-equivalent, pw)` tees to the terminal and the forward pipe in one writer, with no fallible `StdoutPipe` and no drain goroutine/`done` channel). Update the Step 7 code block accordingly and drop the `pipe`/`tee`/`done` machinery. Re-run the Step 5/9 tests after switching — they still pass (stdout still receives the child output; the daemon still receives the forwarded NDJSON).

- [ ] **Step 11: Run the full cmd suite under -race + coverage + lint, then commit**

Run: `go test ./cmd/catacomb/ -race -count=2` then `go test ./cmd/catacomb/ -cover` then `golangci-lint run ./cmd/catacomb/...`
Expected: all pass; `coverage: 100.0% of statements`; lint 0. `execCommand` varies its name/args across tests (claude/nope/FAIL/ENV), so unparam is satisfied.

```bash
git add cmd/catacomb/streamjson.go cmd/catacomb/streamjson_test.go cmd/catacomb/root.go
git commit -m "feat(cmd): catacomb ingest stream-json + catacomb run verbs

Thin fail-open forwarders: ingest streams stdin NDJSON to /v1/stream-json;
run execs the child, tees stdout to the terminal and the daemon via an
io.MultiWriter+pipe, sets CATACOMB_RUN_ID, propagates the exit code.
The daemon never spawns anything.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 6: Cross-cutting verification + [VERIFY] doc note

**Files:**

- Modify: `docs/specs/2026-06-22-m2-streamjson-tailer-design.md` (append a short "M2a [VERIFY] status" note) OR create `docs/notes/2026-06-22-streamjson-verify.md` if the spec should stay frozen — prefer appending to the spec's §4.5 a one-line "implemented in M2a; field names still [VERIFY] pending Step 7 operator capture".
- No code changes.

**Interfaces:** none (documentation + gate run only).

- [ ] **Step 1: Run the whole-repo coverage gate**

Run: `make cover`
Expected: the gate passes at 100% (`.testcoverage.yml` threshold). If any package dropped below, fix the uncovered line with a genuine assertion before proceeding.

- [ ] **Step 2: Run the whole-repo test suite under -race**

Run: `go test ./... -race -count=1`
Expected: all packages PASS, no race.

- [ ] **Step 3: Run the linter across the repo**

Run: `golangci-lint run ./...`
Expected: 0 issues. Pay attention to unparam (vary tokens/sources/args), forbidigo (no `time.Sleep` — none introduced), errcheck/bodyclose (the `resp.Body.Close()` calls are present).

- [ ] **Step 4: Verify the cross-platform build**

Run: `GOOS=windows GOARCH=amd64 go build ./...`
Expected: builds clean (no unix-only syscalls were introduced in M2a; that risk belongs to M2b's inode/dev rotation detection).

- [ ] **Step 5: Append the [VERIFY] note + run markdownlint**

Append to the spec §4.5 (or a new note) a single line: "M2a implements this mapping; the stream-json field names above remain [VERIFY] pending an operator capture in Step 7 (`claude -p --output-format stream-json --verbose --include-partial-messages`)." Then:

Run: `npx markdownlint-cli@0.49.0 --fix docs/plans/2026-06-22-m2a-streamjson.md docs/specs/2026-06-22-m2-streamjson-tailer-design.md`
Expected: no remaining errors.

- [ ] **Step 6: Commit**

```bash
git add docs/
git commit -m "docs(m2a): note stream-json mapping implemented; field names still [VERIFY]

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Self-Review

**1. Spec coverage (spec §4 + §6):**

- §4.1 transport `POST /v1/stream-json` token-gated, streamed NDJSON body, per-request recover, 200 fail-open → Task 4 (handler) + Task 5 (forwarders).
- §4.2 parser contract `Parse(line, executionID, nextSeq)` pure, `nowFn` seam, unknown→zero-obs-no-error → Task 1.
- §4.3 envelope→kind mapping (system.init/assistant/user/stream_event/result) → Task 1 Steps 3/7/11.
- §4.4 `IngestStreamJSON` mirrors `IngestOTLP` (recover/quarantine, execBySession, lazy reload, streamParseFn seam) → Task 4.
- §4.5 [VERIFY] field names → recorded in Task 1 prose + Task 6 doc note (no Go comments).
- §6.1/§6.2/§6.3 precedence generalization (sourceRank 3-tier, tokenRank, payloadRank, commutativity) → Task 2; NO JSONL branch (explicitly noted, M2b inserts it).
- §6.4 `parent_tool_use_id`→`parent_child` edge, structure-rank-aware → Task 3.
- §8.1 M2a deliverables (parser+fixtures, handler, IngestStreamJSON, both verbs, precedence) → Tasks 1-5. Independence test (hook+stream-json same tool_use_id → one node; stream_event edge not overwriting OTel) → Task 4 Step 5 (`TestIngestStreamJSONMergesByToolUseIDWithHook`) + Task 3 Step 5.
- §9 constraints (no deps, no comments, 100% cover, lint, single-mutex, nowFn, ADR-0019, ADR-0013) → Global Constraints + per-task gate steps.

**2. No-dead-code / no-JSONL-branch check:** `sourceRank`/`tokenRank`/`payloadRank`/`structureRank` each have only OTel/hook/stream-json explicit arms + a covered `default: return 0`. No `SourceJSONL` case anywhere in M2a. Each `default` arm is covered by a genuine assertion (hook tokens rank-0, hook structure rank-0). M2b's JSONL insertion is called out at each switch. Confirmed: no unreachable envelope branch — every `build` case has a fixture; bad-JSON/bad-message/bad-content error branches are tested.

**3. Commutativity covered:** Task 2 Step 13 (`TestReductionCommutativityThreeSources`, conflicting timing+tokens+payload) and Task 3 Step 9 (`TestReductionCommutativityWithParentToolEdge`, conflicting structure). `canonGraph` extended with `PayloadHash` so payload divergence would surface. Every new guard is a pure function of `(rank, seq)`; ties break by `seq`/earliest-`EventTime` (existing M1b mechanism), so the extension stays order-independent.

**4. Placeholder scan:** No "TBD"/"implement later"/"similar to Task N"/"add error handling". The `var _ = os.Stdout` in Task 5 Step 3 is an explicit transient kept-import marker, removed in Step 7, with the same-commit instruction so it never lands. Task 5 Step 10 resolves the `StdoutPipe` coverage concern by switching the Step 7 implementation to `io.MultiWriter` + `io.Pipe` (fully coverable, no fallible call) — the final `runChild` form is the MultiWriter one.

**5. Type consistency:** `Parse(line []byte, executionID string, nextSeq func() uint64) ([]model.Observation, error)` is identical across Task 1 (def), Task 4 (`streamParseFn` seam type), and the seam-override test. `IngestStreamJSON(line []byte, sessionID string) error` consistent across Task 4 def + handler call + tests. `streamForward(warn io.Writer, discoveryPath string, body io.Reader)` and `runChild(stdout, stderr io.Writer, discoveryPath, runID string, args []string) error` consistent across Task 5. `sourceRank`/`tokenRank`/`payloadRank`/`structureRank` all `func(model.Source) int`. `mergePayload` consistently the `*Graph` method `(n *model.Node, p *model.Payload, src model.Source)` at its call site in `applyTool`. `fieldStamps` field set is consistent: timing (`timingRank`,`haveTiming`,`nameSeq`,`haveName`) + tokens (`tokenRank`,`haveToken`) + payload (`payloadRank`,`havePayload`) + structure (`structRank`,`haveStruct`); the original `haveOTelTokens` is removed and the transient `edgeStructRank map` is replaced by `structRank`/`haveStruct` within Task 3.

**Spec/code mismatch resolved:** The spec §6.1 and §6.3 describe `mergePayload` as a free function gaining a rank guard and `applyTokens` losing its `haveOTelTokens` bool; the actual code has `mergePayload` as a package function (`reduce.go:180`) and `applyTokens` already a `*Graph` method (`reduce.go:197`). Task 2 promotes `mergePayload` to a `*Graph` method (it needs `g.stampsFor`) and updates its single call site — recorded explicitly so the implementer is not surprised. Also: spec §6.3 says `sourceRank` becomes `OTel=3,hook=2,JSONL=1,stream_json=0`, but that 4-tier order includes the not-yet-live JSONL; M2a uses `OTel=3,hook=2,stream_json=1,default=0` (three live tiers) and M2b shifts stream-json to 0 and inserts JSONL=1 — this deliberate divergence (live-only ranks) is the no-dead-code requirement and is called out in Task 2's NO-DEAD-CODE note.
