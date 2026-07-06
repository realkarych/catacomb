package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func rootCommandNames() map[string]bool {
	root := newRootCmd()
	names := make(map[string]bool)
	for _, sub := range root.Commands() {
		names[sub.Name()] = true
	}
	return names
}

func TestRootCommandSetIsExact(t *testing.T) {
	names := rootCommandNames()
	want := []string{
		"bench", "regress", "baseline", "trends", "diff",
		"subgraph", "export", "replay", "mcp", "version",
	}
	require.Len(t, names, len(want))
	for _, name := range want {
		assert.True(t, names[name], "missing command %q", name)
	}
}

func TestRootRemovedCommandsStayRemoved(t *testing.T) {
	names := rootCommandNames()
	for _, gone := range []string{
		"up", "down", "restart", "status", "logs", "daemon", "install-hooks",
		"env", "hook", "mark", "demo", "discovery", "run", "ingest",
		"runs", "inspect", "snapshot", "observe", "ui", "watch",
	} {
		assert.False(t, names[gone], "command %q must not be registered", gone)
	}
}

func TestRootLongMentionsNoTerminalObserver(t *testing.T) {
	root := newRootCmd()
	assert.NotContains(t, root.Long, "terminal observer")
}

func TestRootLongMentionsNoDaemon(t *testing.T) {
	root := newRootCmd()
	assert.NotContains(t, root.Long, "daemon")
}
