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
