# PR-R: `catacomb mcp` stdio server (F8) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: use superpowers:subagent-driven-development (one fresh subagent per task, review between tasks) or superpowers:executing-plans. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the MCP server behind the flagship `mcp__catacomb__mark` checkpoint convention so it works out of the box. The convention is documented in `concepts.md:111`, `workflows.md:73,176,181,363`, and `cli.md:551`, and the reducer already synthesizes markers from the tool-call input riding the trace stream (`reduce/marker.go`) — but catacomb ships **no** MCP server, so the tool does not exist client-side and every adopter hand-writes a stub. The V-1 dogfood run (`docs/reviews/2026-07-04-dogfood-calibration.md` §1, §6 F8) had to hand-wire a ~50-line no-op `mark` server via `--mcp-config`. Replace that stub with a shipped `catacomb mcp` subcommand.

**The end-to-end contract (verified, zero reducer change):**

1. An MCP server named `catacomb` exposing a tool `mark` surfaces to the agent as `mcp__catacomb__mark` — Claude Code prefixes MCP tools `mcp__<server>__<tool>` (the same convention `reduce/reduce.go:581` `isMCP` keys on `mcp__`). The reducer's `isMarkerTool` accepts exactly `mcp__catacomb__mark` (and the bare `catacomb__mark`) at `reduce/marker.go:23-25`.
2. When the agent calls the tool, Claude Code emits an assistant `tool_use` block into the stream **verbatim**: `ingest/streamjson/streamjson.go:210-223` maps it to a partial with `attrs["name"] = b.Name` (`mcp__catacomb__mark`) and `Payload.Input = b.Input` (the agent's `arguments` object, byte-for-byte). This happens **regardless of what the tool returns**.
3. `reduce/reduce.go:139-153` `applyTool` reads `name := o.Attrs["name"]`, matches `isMarkerTool`, and `extractMarkerFromPayload` (`reduce/marker.go:34-51`) unmarshals `Payload.Input` into `markerToolInput{ Name string, Boundary string, StateRef string ("state_ref"), Occurrence *int }`, requiring `name != "" && boundary != ""`. The marker node is synthesized from that input.

So the server's **only** jobs are (a) exist and advertise a tool named `mark` whose `inputSchema` makes the agent send `{name, boundary, occurrence?, state_ref?}`, and (b) return a valid MCP result so the agent's turn completes. The `arguments` the agent passes — not anything the server emits — are what reach the reducer. The tool is therefore a **pure ack; it never contacts the daemon.**

**Ack-only vs daemon-hitting — decision: ack-only.** Justification: the marker rides the trace stream (stream-json `tool_use.input` and/or the hook `PreToolUse` trace), and the reducer synthesizes from that input. A daemon round-trip would (a) duplicate what the trace already carries, (b) require discovery/token plumbing inside a process Claude Code spawns per session, and (c) fail closed when no daemon is reachable — the opposite of what a checkpoint tool should do. `catacomb mark` (the CLI, `cmd/catacomb/mark.go`) already covers the deliberate `POST /v1/mark` path for out-of-band marking; the MCP tool is the *in-run agent* channel and must fail open with zero configuration. Ack-only.

**Architecture:** a new stdlib-only `mcp/` package holds a pure JSON-RPC 2.0 loop over an `io.Reader`/`io.Writer` pair (fully testable without real stdio), and a thin `cmd/catacomb/mcp.go` wires `cmd.InOrStdin()`/`cmd.OutOrStdout()` and registers the subcommand under the Advanced group. No new dependencies (`go.mod` has no MCP SDK and must not gain one — `encoding/json` + `bufio` only). No daemon, reducer, ingest, or schema changes.

**Protocol.** MCP over stdio = JSON-RPC 2.0, **newline-delimited** (one message per line, no embedded newlines — `json.Marshal` guarantees this). Methods handled: `initialize` (→ `protocolVersion` + `capabilities.tools` + `serverInfo{name:"catacomb"}`), `notifications/initialized` (a JSON-RPC notification — no `id`, no reply), `tools/list` (→ the `mark` tool), `tools/call` (name `mark` → an ack `content` result). The server **echoes the client's requested `protocolVersion`** (MCP spec: the server responds with the same version when it supports it), falling back to `2025-06-18` (current stable, what Claude Code sends) only when the client omits it — this makes the server forward-compatible as Claude Code's version advances, so the version string is not load-bearing. Errors return JSON-RPC error objects: malformed line → `-32700`, unknown method → `-32601`, bad `tools/call` params / unknown tool → `-32602`; invalid `mark` arguments return a tool result with `isError:true` (agent-visible, non-fatal) rather than crashing the session.

**Tech Stack:** Go 1.26, testify, table-driven tests. Repo rules: NO comments in Go (`internal/codepolicy` fails the build otherwise), 100% coverage TDD-first, gofumpt via `make fmt`, deterministic outputs, one commit per task.

## Global Constraints

- Work in this worktree only (`.claude/worktrees/feat+mcp-server`); never touch the shared checkout.
- No comments in Go — none, not even doc comments (only `//go:build`/`//go:embed`/`//go:generate` directives are allowed).
- 100% coverage (`make cover`); TDD: failing test first, minimal implementation, refactor under green. The stdio loop is testable because `Serve` takes `io.Reader`/`io.Writer`; every branch below has a named test.
- `make lint` 0 issues; `make fmt` before committing; `npx -y markdownlint-cli@0.49.0 'docs/**/*.md'` on touched docs.
- No new `go.mod` dependency. `encoding/json`, `bufio`, `bytes`, `context`, `io` only.
- Deterministic: no wall-clock, no map-order-dependent output in tests (decode responses to structs/maps and assert fields, not raw string equality of marshaled maps).

---

### Task 1: `mcp` package — JSON-RPC loop, `initialize`, notifications, `tools/list`, protocol errors

**Files:**

- Create: `mcp/server.go`
- Create: `mcp/server_test.go`

**Interfaces:**

- `func Serve(ctx context.Context, r io.Reader, w io.Writer) error` — reads newline-delimited JSON-RPC requests from `r`, writes newline-delimited responses to `w`, returns when `r` reaches EOF (nil) or `ctx` is cancelled (nil) or a read/write fails (that error).
- `var Version = "dev"` — surfaced in `serverInfo.version` (mirrors the `cmd` package's `Version`; ldflags wiring is out of scope).

- [ ] **Step 1: Write failing tests** (`mcp/server_test.go`)

Package `mcp`. Helpers + the non-tool-call surface. `decode` unmarshals one response line into a `map[string]any`.

```go
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errBoom = errors.New("boom")

type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, errBoom }

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errBoom }

func serve(t *testing.T, input string) []map[string]any {
	t.Helper()
	var out bytes.Buffer
	require.NoError(t, Serve(context.Background(), strings.NewReader(input), &out))
	var msgs []map[string]any
	for _, l := range strings.Split(strings.TrimRight(out.String(), "\n"), "\n") {
		if l == "" {
			continue
		}
		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(l), &m))
		msgs = append(msgs, m)
	}
	return msgs
}

func TestInitializeEchoesClientProtocolVersion(t *testing.T) {
	msgs := serve(t, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"claude","version":"x"}}}`+"\n")
	require.Len(t, msgs, 1)
	require.Equal(t, "2.0", msgs[0]["jsonrpc"])
	require.EqualValues(t, 1, msgs[0]["id"])
	res := msgs[0]["result"].(map[string]any)
	assert.Equal(t, "2025-03-26", res["protocolVersion"])
	assert.Equal(t, "catacomb", res["serverInfo"].(map[string]any)["name"])
	assert.Contains(t, res["capabilities"].(map[string]any), "tools")
}

func TestInitializeDefaultsProtocolVersionWhenAbsent(t *testing.T) {
	msgs := serve(t, `{"jsonrpc":"2.0","id":2,"method":"initialize","params":{}}`+"\n")
	assert.Equal(t, defaultProtocolVersion, msgs[0]["result"].(map[string]any)["protocolVersion"])
}

func TestInitializeDefaultsProtocolVersionWhenNoParams(t *testing.T) {
	msgs := serve(t, `{"jsonrpc":"2.0","id":3,"method":"initialize"}`+"\n")
	assert.Equal(t, defaultProtocolVersion, msgs[0]["result"].(map[string]any)["protocolVersion"])
}

func TestInitializedNotificationHasNoReply(t *testing.T) {
	msgs := serve(t, `{"jsonrpc":"2.0","method":"notifications/initialized"}`+"\n")
	assert.Empty(t, msgs)
}

func TestToolsListReturnsMarkTool(t *testing.T) {
	msgs := serve(t, `{"jsonrpc":"2.0","id":4,"method":"tools/list"}`+"\n")
	tools := msgs[0]["result"].(map[string]any)["tools"].([]any)
	require.Len(t, tools, 1)
	mark := tools[0].(map[string]any)
	assert.Equal(t, "mark", mark["name"])
	schema := mark["inputSchema"].(map[string]any)
	assert.ElementsMatch(t, []any{"name", "boundary"}, schema["required"].([]any))
	props := schema["properties"].(map[string]any)
	for _, k := range []string{"name", "boundary", "occurrence", "state_ref"} {
		assert.Contains(t, props, k)
	}
	assert.ElementsMatch(t, []any{"start", "end"}, props["boundary"].(map[string]any)["enum"].([]any))
}

func TestUnknownMethodReturnsMethodNotFound(t *testing.T) {
	msgs := serve(t, `{"jsonrpc":"2.0","id":5,"method":"resources/list"}`+"\n")
	assert.EqualValues(t, codeMethodNotFound, msgs[0]["error"].(map[string]any)["code"])
}

func TestMalformedLineReturnsParseErrorWithNullID(t *testing.T) {
	msgs := serve(t, "not json\n")
	assert.Nil(t, msgs[0]["id"])
	assert.EqualValues(t, codeParse, msgs[0]["error"].(map[string]any)["code"])
}

func TestBlankLinesAndMultipleMessages(t *testing.T) {
	msgs := serve(t, "\n"+`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`+"\n\n"+`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`+"\n")
	require.Len(t, msgs, 2)
	assert.EqualValues(t, 1, msgs[0]["id"])
	assert.EqualValues(t, 2, msgs[1]["id"])
}

func TestServeStopsOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out bytes.Buffer
	require.NoError(t, Serve(ctx, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`+"\n"), &out))
	assert.Empty(t, out.String())
}

func TestServeReturnsReaderError(t *testing.T) {
	require.ErrorIs(t, Serve(context.Background(), errReader{}, &bytes.Buffer{}), errBoom)
}

func TestServeReturnsWriterError(t *testing.T) {
	err := Serve(context.Background(), strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`+"\n"), errWriter{})
	require.ErrorIs(t, err, errBoom)
}
```

- [ ] **Step 2: Run tests, confirm they fail to compile** (no `mcp/server.go` yet). Expected: build error / undefined `Serve`.

- [ ] **Step 3: Implement** (`mcp/server.go`)

```go
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
)

var Version = "dev"

const (
	defaultProtocolVersion = "2025-06-18"
	codeParse              = -32700
	codeMethodNotFound     = -32601
	codeInvalidParams      = -32602
)

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

func Serve(ctx context.Context, r io.Reader, w io.Writer) error {
	br := bufio.NewReader(r)
	for {
		if ctx.Err() != nil {
			return nil
		}
		line, err := br.ReadBytes('\n')
		if trimmed := bytes.TrimSpace(line); len(trimmed) > 0 {
			if werr := handleLine(trimmed, w); werr != nil {
				return werr
			}
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

func handleLine(line []byte, w io.Writer) error {
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		return writeMessage(w, errorResponse(json.RawMessage("null"), codeParse, "parse error"))
	}
	if len(req.ID) == 0 {
		return nil
	}
	result, rpcErr := dispatch(req)
	if rpcErr != nil {
		return writeMessage(w, errorResponse(req.ID, rpcErr.Code, rpcErr.Message))
	}
	return writeMessage(w, resultResponse(req.ID, result))
}

func dispatch(req request) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		return initializeResult(req.Params), nil
	case "tools/list":
		return toolsListResult(), nil
	default:
		return nil, &rpcError{Code: codeMethodNotFound, Message: "method not found: " + req.Method}
	}
}

func initializeResult(params json.RawMessage) map[string]any {
	version := defaultProtocolVersion
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := json.Unmarshal(params, &p); err == nil && p.ProtocolVersion != "" {
		version = p.ProtocolVersion
	}
	return map[string]any{
		"protocolVersion": version,
		"capabilities":    map[string]any{"tools": map[string]any{}},
		"serverInfo":      map[string]any{"name": "catacomb", "version": Version},
	}
}

func toolsListResult() map[string]any {
	return map[string]any{"tools": []any{markToolSpec()}}
}

func markToolSpec() map[string]any {
	return map[string]any{
		"name":        "mark",
		"description": "Record a phase-boundary checkpoint in the current catacomb session. Call at the start and end of each named phase (for example impl, verify) so regress and diff can scope to it.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":       map[string]any{"type": "string", "description": "phase label, for example impl or verify"},
				"boundary":   map[string]any{"type": "string", "enum": []any{"start", "end"}, "description": "start or end of the phase"},
				"occurrence": map[string]any{"type": "integer", "description": "explicit occurrence ordinal for repeated same-name phases (optional)"},
				"state_ref":  map[string]any{"type": "string", "description": "opaque state reference (optional)"},
			},
			"required": []any{"name", "boundary"},
		},
	}
}

func writeMessage(w io.Writer, v any) error {
	b, _ := json.Marshal(v)
	_, err := w.Write(append(b, '\n'))
	return err
}

func errorResponse(id json.RawMessage, code int, msg string) response {
	return response{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}}
}

func resultResponse(id json.RawMessage, result any) response {
	return response{JSONRPC: "2.0", ID: id, Result: result}
}
```

- [ ] **Step 4: Green + coverage.** Run `go test -race ./mcp/ && go test ./mcp/ -cover`. Expected: PASS, coverage **100.0%** (the `tools/call` case arrives in Task 2; the `default` branch, both `initialize` version branches, notification skip, blank-line skip, parse error, cancelled-ctx, reader error, and writer error are all covered by the Step 1 tests).

- [ ] **Step 5: Commit**

```bash
git add mcp/server.go mcp/server_test.go
git commit -m "feat(mcp): JSON-RPC stdio loop — initialize, tools/list, notifications, errors (F8)"
```

---

### Task 2: `tools/call` for `mark` (ack-only, with agent-visible validation)

**Files:**

- Modify: `mcp/server.go` (add the `tools/call` dispatch case + handlers)
- Modify: `mcp/server_test.go` (add `tools/call` tests)

- [ ] **Step 1: Write failing tests** (append to `mcp/server_test.go`)

```go
func TestToolsCallMarkOK(t *testing.T) {
	msgs := serve(t, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"mark","arguments":{"name":"impl","boundary":"start","occurrence":0,"state_ref":"c1"}}}`+"\n")
	res := msgs[0]["result"].(map[string]any)
	assert.Equal(t, false, res["isError"])
	content := res["content"].([]any)[0].(map[string]any)
	assert.Equal(t, "text", content["type"])
	assert.Contains(t, content["text"], "impl")
}

func TestToolsCallMarkMissingFieldsIsError(t *testing.T) {
	msgs := serve(t, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"mark","arguments":{"boundary":"sideways"}}}`+"\n")
	res := msgs[0]["result"].(map[string]any)
	assert.Equal(t, true, res["isError"])
}

func TestToolsCallMarkBadArgumentsShapeIsError(t *testing.T) {
	msgs := serve(t, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"mark","arguments":"not-an-object"}}`+"\n")
	assert.Equal(t, true, msgs[0]["result"].(map[string]any)["isError"])
}

func TestToolsCallUnknownToolIsInvalidParams(t *testing.T) {
	msgs := serve(t, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"nope","arguments":{}}}`+"\n")
	assert.EqualValues(t, codeInvalidParams, msgs[0]["error"].(map[string]any)["code"])
}

func TestToolsCallBadParamsIsInvalidParams(t *testing.T) {
	msgs := serve(t, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":5}`+"\n")
	assert.EqualValues(t, codeInvalidParams, msgs[0]["error"].(map[string]any)["code"])
}
```

- [ ] **Step 2: Run tests, confirm the five new ones fail** (`tools/call` currently hits `default` → `-32601`, so the OK/isError assertions fail and the two `-32602` tests get `-32601`).

- [ ] **Step 3: Implement.** Add the case to `dispatch` (between `tools/list` and `default`):

```go
	case "tools/call":
		return toolsCallResult(req.Params)
```

Append the handlers:

```go
func toolsCallResult(params json.RawMessage) (any, *rpcError) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &rpcError{Code: codeInvalidParams, Message: "invalid params"}
	}
	if p.Name != "mark" {
		return nil, &rpcError{Code: codeInvalidParams, Message: "unknown tool: " + p.Name}
	}
	var a struct {
		Name     string `json:"name"`
		Boundary string `json:"boundary"`
	}
	if err := json.Unmarshal(p.Arguments, &a); err != nil {
		return toolResult("mark: invalid arguments", true), nil
	}
	if a.Name == "" || (a.Boundary != "start" && a.Boundary != "end") {
		return toolResult("mark: name is required and boundary must be start or end", true), nil
	}
	return toolResult("marked "+a.Boundary+" "+a.Name, false), nil
}

func toolResult(text string, isErr bool) map[string]any {
	return map[string]any{
		"content": []any{map[string]any{"type": "text", "text": text}},
		"isError": isErr,
	}
}
```

Note: `toolsCallResult` deliberately validates only `name`/`boundary` (the reducer's own requirement); `occurrence`/`state_ref` are not parsed here because the server forwards nothing — the agent's full `arguments` object reaches the reducer through the stream `tool_use.input`, not through this ack.

- [ ] **Step 4: Green + coverage.** `go test -race ./mcp/ && go test ./mcp/ -cover`. Expected PASS, 100.0% (five branches: bad params, unknown tool, bad-argument-shape, validation failure, OK).

- [ ] **Step 5: Commit**

```bash
git add mcp/server.go mcp/server_test.go
git commit -m "feat(mcp): tools/call mark — ack-only checkpoint tool with agent-visible validation (F8)"
```

---

### Task 3: `catacomb mcp` subcommand wiring

**Files:**

- Create: `cmd/catacomb/mcp.go`
- Create: `cmd/catacomb/mcp_test.go`
- Modify: `cmd/catacomb/root.go` (register under the Advanced group)

- [ ] **Step 1: Write failing test** (`cmd/catacomb/mcp_test.go`)

Drive the command end-to-end through the root: feed an `initialize` line via `SetIn`, capture `SetOut`, and rely on EOF to terminate `Serve`.

```go
package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMCPCommandServesOverStdio(t *testing.T) {
	root := newRootCmd()
	root.SetIn(strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}` + "\n"))
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"mcp"})
	require.NoError(t, root.Execute())
	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out.String())), &m))
	assert.Equal(t, "catacomb", m["result"].(map[string]any)["serverInfo"].(map[string]any)["name"])
}
```

- [ ] **Step 2: Run, confirm failure** (`unknown command "mcp"`).

- [ ] **Step 3: Implement** (`cmd/catacomb/mcp.go`) — mirror `daemon.go`'s signal wiring; stdin EOF is the primary shutdown, SIGINT/SIGTERM the secondary (checked between messages):

```go
package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/realkarych/catacomb/mcp"
)

func newMCPCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Run the catacomb MCP stdio server (exposes the mark checkpoint tool)",
		Long: `Run the catacomb MCP server over stdio (JSON-RPC 2.0, newline-delimited).

Wire it into Claude Code with --mcp-config so the agent can call the
mcp__catacomb__mark checkpoint tool during a run:

  {"mcpServers":{"catacomb":{"command":"catacomb","args":["mcp"]}}}

The tool is a pure acknowledgement: the phase marker rides the trace stream and
the catacomb reducer synthesizes it from the tool-call input, so the server needs
no daemon and no configuration. It exits when its stdin is closed.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return mcp.Serve(ctx, cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}
}
```

Register in `root.go` beside the other advanced commands (e.g. after `newMarkCmd()`):

```go
	root.AddCommand(advanced(newMCPCmd()))
```

- [ ] **Step 4: Green + coverage.** `go test -race ./cmd/catacomb/ -run TestMCP && go test ./cmd/catacomb/ -cover`. Expected PASS; the one command test exercises the whole `mcp.go` file (RunE closure, NotifyContext, `defer stop()`). If the package coverage line dropped, it is unrelated to this file — confirm `mcp.go` itself is fully covered via `go test ./cmd/catacomb/ -coverprofile=/tmp/c.out && go tool cover -func=/tmp/c.out | grep mcp.go`.

- [ ] **Step 5: Commit**

```bash
git add cmd/catacomb/mcp.go cmd/catacomb/mcp_test.go cmd/catacomb/root.go
git commit -m "feat(cmd): catacomb mcp subcommand wires the stdio MCP server (F8)"
```

---

### Task 4: Docs — config recipe, CLI reference, bench note

**Files:**

- Modify: `docs/guide/workflows.md`
- Modify: `docs/guide/cli.md`
- Modify: `docs/guide/concepts.md`

- [ ] **Step 1: `workflows.md`** — replace the bare line at `:73` ("The agent can also call `mcp__catacomb__mark` directly, which rides the trace stream.") with a pointer to the shipped server plus the config block:

```markdown
The agent can also call `mcp__catacomb__mark` directly, which rides the trace
stream. That tool is served by `catacomb mcp` — a stdlib stdio MCP server that
ships with catacomb (no hand-rolled stub needed). Wire it in with `--mcp-config`:

    {"mcpServers":{"catacomb":{"command":"catacomb","args":["mcp"]}}}

The server named `catacomb` exposing the `mark` tool surfaces to the agent as
`mcp__catacomb__mark`. The call is a pure acknowledgement — the marker is
synthesized from the tool-call input on the trace stream, so no daemon
connection is required and the tool fails open.
```

- [ ] **Step 2: `cli.md`** — add a `### catacomb mcp` subsection near the `catacomb mark` reference (~`:309`). Document: stdio JSON-RPC 2.0 server; one `mark` tool with `name` (required), `boundary` (`start|end`, required), `occurrence` (optional int), `state_ref` (optional); ack-only, no daemon; the `--mcp-config` snippet; and — matching the V-1 dogfood setup — that bench cells pass it with `--strict-mcp-config` so only the catacomb server is loaded. Cross-link the checkpoints workflow.

- [ ] **Step 3: `concepts.md`** — at `:111` ("Or from inside an agent session via the `mcp__catacomb__mark` tool …"), append a half-sentence that this tool is served by `catacomb mcp` (link to the cli.md section). Keep it to one line.

- [ ] **Step 4: markdownlint.** `npx -y markdownlint-cli@0.49.0 'docs/**/*.md'`. Expected 0 errors. Fix wrapping/list issues if any.

- [ ] **Step 5: Commit**

```bash
git add docs/guide/workflows.md docs/guide/cli.md docs/guide/concepts.md
git commit -m "docs: catacomb mcp server — config recipe, CLI reference, bench note (F8)"
```

---

### Task 5: Full gates + live verify (replace the hand-written stub)

**Files:** none new (verification only).

- [ ] **Step 1: Full gates.** `make fmt && make lint && make cover && npx -y markdownlint-cli@0.49.0 'docs/**/*.md'`. Expected: fmt clean, lint 0, coverage total **100%** (`mcp` package and `cmd/catacomb/mcp.go` at 100%), markdownlint 0.

- [ ] **Step 2: Protocol smoke (no network, no credits).** Confirm the framing and handshake with a hand-fed session:

```bash
make build
printf '%s\n%s\n%s\n%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}' \
  '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' \
  '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"mark","arguments":{"name":"impl","boundary":"start"}}}' \
  | bin/catacomb mcp
```

Expected: exactly three response lines (the notification produces none) — an `initialize` result echoing `2025-06-18` with `serverInfo.name":"catacomb"`, a `tools/list` result carrying the `mark` tool, and a `tools/call` result with `"isError":false`. Process exits on stdin EOF.

- [ ] **Step 3: Live verify against a real agent (network + credits).** Reproduce the V-1 dogfood setup with the **shipped** server instead of the stub, in a throwaway dir, against a throwaway daemon (never touch `~/.catacomb`):

```bash
TMP=$(mktemp -d); BIN=$(pwd)/bin/catacomb
"$BIN" daemon --db "$TMP/cat.db" --discovery "$TMP/disc.json" > "$TMP/daemon.log" 2>&1 &
DPID=$!; until [ -f "$TMP/disc.json" ]; do sleep 0.2; done
printf '{"mcpServers":{"catacomb":{"command":"%s","args":["mcp"]}}}' "$BIN" > "$TMP/mcp.json"
mkdir -p "$TMP/proj" && cd "$TMP/proj"
CATACOMB_DISCOVERY="$TMP/disc.json" "$BIN" install-hooks --project
claude -p 'Call the mcp__catacomb__mark tool with {"name":"impl","boundary":"start"}, then again with {"name":"impl","boundary":"end"}. Then stop.' \
  --model haiku --output-format stream-json --verbose --max-turns 6 \
  --permission-mode bypassPermissions --mcp-config "$TMP/mcp.json" --strict-mcp-config
# then confirm a closed `impl` phase marker landed:
"$BIN" runs --db "$TMP/cat.db" --json | python3 -c 'import sys,json; print(json.load(sys.stdin))'
kill $DPID
```

Expected: the agent invokes `mcp__catacomb__mark` (no "tool not found"), and the finalized session graph carries a closed `impl` phase marker (a `marker` node paired start→end) — the same result the V-1 hand-wired stub produced, now with zero hand-wiring. Capture the marker evidence for the PR description. If `claude` is unavailable, Step 2 plus the reducer contract (`reduce/marker.go`, exercised by existing `reduce` tests) stand in; note the substitution in the PR body.

- [ ] **Step 4: Commit any gate-driven changes.** If `make fmt` dirtied the tree:

```bash
git add -A && git commit -m "chore: gofumpt after PR-R mcp server"
```

## Definition of done

- `catacomb mcp` speaks newline-delimited JSON-RPC 2.0 on stdio: `initialize` (echoing the client protocol version, `2025-06-18` default), `notifications/initialized` (no reply), `tools/list` (the `mark` tool), `tools/call` (ack), `-32700/-32601/-32602` on malformed/unknown/bad input.
- The `mark` `inputSchema` produces exactly `{name, boundary, occurrence?, state_ref?}`, matching `markerToolInput` (`reduce/marker.go:27-32`); the server named `catacomb` surfaces the tool as `mcp__catacomb__mark`, which `isMarkerTool` accepts — zero reducer change.
- Ack-only: no daemon dependency; fails open.
- No new `go.mod` dependency; no Go comments; 100% coverage; lint/fmt/markdownlint clean.
- A real `claude -p … --mcp-config` cell yields a closed phase marker in the graph, replacing the hand-written stub.
