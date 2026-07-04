package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
