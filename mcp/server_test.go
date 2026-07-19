package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"

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
	assert.Equal(t, []any{"name", "boundary"}, schema["required"].([]any))
	props := schema["properties"].(map[string]any)
	for _, k := range []string{"name", "boundary", "occurrence", "state_ref"} {
		assert.Contains(t, props, k)
	}
	assert.Equal(t, []any{"start", "end"}, props["boundary"].(map[string]any)["enum"].([]any))
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

func markCall(args string) string {
	return `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"mark","arguments":` + args + `}}` + "\n"
}

func markResult(t *testing.T, args string) (string, bool) {
	t.Helper()
	msgs := serve(t, markCall(args))
	require.Len(t, msgs, 1)
	require.NotContains(t, msgs[0], "error", "argument validation must be reported in-band, not as a JSON-RPC error")
	res := msgs[0]["result"].(map[string]any)
	content := res["content"].([]any)
	require.Len(t, content, 1)
	first := content[0].(map[string]any)
	assert.Equal(t, "text", first["type"])
	return first["text"].(string), res["isError"].(bool)
}

func TestToolsCallMarkEchoesBoundaryAndName(t *testing.T) {
	tests := []struct {
		name string
		args string
		want string
	}{
		{
			"start with optional fields",
			`{"name":"impl","boundary":"start","occurrence":0,"state_ref":"c1"}`,
			"marked start impl",
		},
		{"end without optional fields", `{"name":"verify","boundary":"end"}`, "marked end verify"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, isErr := markResult(t, tt.args)
			assert.False(t, isErr)
			assert.Equal(t, tt.want, text)
		})
	}
}

func TestToolsCallMarkRejectsInvalidArguments(t *testing.T) {
	const badArgs = "mark: name is required and boundary must be start or end"
	tests := []struct {
		name string
		args string
		want string
	}{
		{"missing name", `{"boundary":"start"}`, badArgs},
		{"empty name", `{"name":"","boundary":"start"}`, badArgs},
		{"missing boundary", `{"name":"impl"}`, badArgs},
		{"boundary not start or end", `{"name":"impl","boundary":"sideways"}`, badArgs},
		{"boundary wrong case", `{"name":"impl","boundary":"START"}`, badArgs},
		{"arguments not an object", `"not-an-object"`, "mark: invalid arguments"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, isErr := markResult(t, tt.args)
			assert.True(t, isErr)
			assert.Equal(t, tt.want, text)
		})
	}
}

func TestToolsCallUnknownToolIsInvalidParams(t *testing.T) {
	msgs := serve(t, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"nope","arguments":{}}}`+"\n")
	assert.EqualValues(t, codeInvalidParams, msgs[0]["error"].(map[string]any)["code"])
}

func TestToolsCallBadParamsIsInvalidParams(t *testing.T) {
	msgs := serve(t, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":5}`+"\n")
	assert.EqualValues(t, codeInvalidParams, msgs[0]["error"].(map[string]any)["code"])
}

func TestFinalMessageWithoutTrailingNewlineIsStillServed(t *testing.T) {
	msgs := serve(t, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`+"\n"+`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	require.Len(t, msgs, 2, "a final line ending at EOF instead of a newline must not be dropped")
	assert.EqualValues(t, 1, msgs[0]["id"])
	assert.EqualValues(t, 2, msgs[1]["id"])
}

func TestMessagesSplitAcrossManyPartialReadsAreReassembledInOrder(t *testing.T) {
	input := markCall(`{"name":"first","boundary":"start"}`) +
		markCall(`{"name":"second","boundary":"end"}`)

	var out bytes.Buffer
	require.NoError(t, Serve(context.Background(), iotest.OneByteReader(strings.NewReader(input)), &out))

	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	require.Len(t, lines, 2)
	texts := make([]string, 0, 2)
	for _, l := range lines {
		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(l), &m))
		res := m["result"].(map[string]any)
		texts = append(texts, res["content"].([]any)[0].(map[string]any)["text"].(string))
	}
	assert.Equal(t, []string{"marked start first", "marked end second"}, texts)
}

func TestOversizedMessageIsFramedWholeAndNotTruncated(t *testing.T) {
	huge := strings.Repeat("p", 512*1024)
	text, isErr := markResult(t, `{"name":"`+huge+`","boundary":"start"}`)
	assert.False(t, isErr)
	assert.Equal(t, "marked start "+huge, text,
		"a message far larger than the read buffer must survive framing intact")
}

func TestOversizedMessageIsNotSplitIntoSeveralRequests(t *testing.T) {
	huge := strings.Repeat("q", 512*1024)
	msgs := serve(t, markCall(`{"name":"`+huge+`","boundary":"start"}`))
	assert.Len(t, msgs, 1, "one oversized line must produce exactly one response")
}

type cancelAfterFirstRead struct {
	chunks [][]byte
	cancel context.CancelFunc
}

func (r *cancelAfterFirstRead) Read(p []byte) (int, error) {
	if len(r.chunks) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.chunks[0])
	r.chunks = r.chunks[1:]
	r.cancel()
	return n, nil
}

func TestServeStopsBeforeHandlingMessagesQueuedAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reader := &cancelAfterFirstRead{
		chunks: [][]byte{
			[]byte(markCall(`{"name":"first","boundary":"start"}`)),
			[]byte(markCall(`{"name":"second","boundary":"start"}`)),
		},
		cancel: cancel,
	}

	var out bytes.Buffer
	require.NoError(t, Serve(ctx, reader, &out))

	assert.Equal(t, 1, strings.Count(out.String(), "\n"),
		"the in-flight message is answered, the queued one is not")
	assert.Contains(t, out.String(), "marked start first")
	assert.NotContains(t, out.String(), "second")
}

func TestRequestIDTypeIsEchoedVerbatim(t *testing.T) {
	msgs := serve(t, `{"jsonrpc":"2.0","id":"req-7","method":"tools/list"}`+"\n")
	require.Len(t, msgs, 1)
	assert.Equal(t, "req-7", msgs[0]["id"], "a string id must not be coerced to a number")
}
