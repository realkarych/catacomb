package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/mcp"
)

func TestMCPCommandServesAWellFormedInitializeResponseOverStdio(t *testing.T) {
	root := newRootCmd()
	root.SetIn(strings.NewReader(`{"jsonrpc":"2.0","id":7,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}` + "\n"))
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"mcp"})
	require.NoError(t, root.Execute())

	replies := strings.Split(strings.TrimSpace(out.String()), "\n")
	require.Len(t, replies, 1, "one request must produce exactly one reply")

	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(replies[0]), &m))
	assert.Equal(t, "2.0", m["jsonrpc"])
	assert.InDelta(t, 7.0, m["id"], 1e-9, "the reply must echo the request id")
	assert.NotContains(t, m, "error")

	result, ok := m["result"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "2025-06-18", result["protocolVersion"])
	assert.Equal(t, map[string]any{"name": "catacomb", "version": mcp.Version}, result["serverInfo"])
}

func TestMCPCommandExitsSilentlyWhenStdinClosesWithoutARequest(t *testing.T) {
	root := newRootCmd()
	root.SetIn(strings.NewReader(""))
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"mcp"})
	require.NoError(t, root.Execute())
	assert.Empty(t, out.String())
}
