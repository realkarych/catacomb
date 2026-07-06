package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRootHasThreeGroups(t *testing.T) {
	root := newRootCmd()
	groups := root.Groups()
	require.Len(t, groups, 3)
	ids := make([]string, len(groups))
	for i, g := range groups {
		ids[i] = g.ID
	}
	assert.Contains(t, ids, "observe")
	assert.Contains(t, ids, "setup")
	assert.Contains(t, ids, "advanced")
}

func TestRootGroupTitles(t *testing.T) {
	root := newRootCmd()
	titles := make(map[string]string)
	for _, g := range root.Groups() {
		titles[g.ID] = g.Title
	}
	assert.Equal(t, "Observe:", titles["observe"])
	assert.Equal(t, "Setup:", titles["setup"])
	assert.Equal(t, "Advanced:", titles["advanced"])
}

func TestAllCommandsHaveGroupID(t *testing.T) {
	root := newRootCmd()
	for _, sub := range root.Commands() {
		assert.NotEmpty(t, sub.GroupID, "command %q has no GroupID", sub.Name())
	}
}

func TestCommandGroupAssignments(t *testing.T) {
	root := newRootCmd()
	groups := make(map[string]string)
	for _, sub := range root.Commands() {
		groups[sub.Name()] = sub.GroupID
	}
	assert.Equal(t, "observe", groups["up"])
	assert.Equal(t, "observe", groups["status"])
	assert.Equal(t, "setup", groups["daemon"])
	assert.Equal(t, "setup", groups["install-hooks"])
	assert.Equal(t, "setup", groups["env"])
	assert.Equal(t, "advanced", groups["hook"])
	assert.Equal(t, "advanced", groups["replay"])
	assert.Equal(t, "advanced", groups["demo"])
	assert.Equal(t, "advanced", groups["runs"])
	assert.Equal(t, "advanced", groups["snapshot"])
	assert.Equal(t, "advanced", groups["inspect"])
	assert.Equal(t, "advanced", groups["version"])
	assert.Equal(t, "advanced", groups["export"])
}

func TestViewerCommandsRemoved(t *testing.T) {
	root := newRootCmd()
	names := make(map[string]bool)
	for _, sub := range root.Commands() {
		names[sub.Name()] = true
	}
	for _, gone := range []string{"observe", "ui", "watch"} {
		assert.False(t, names[gone], "command %q must not be registered", gone)
	}
}

func TestRunIngestCommandsRemoved(t *testing.T) {
	root := newRootCmd()
	names := make(map[string]bool)
	for _, sub := range root.Commands() {
		names[sub.Name()] = true
	}
	for _, gone := range []string{"run", "ingest"} {
		assert.False(t, names[gone], "command %q must not be registered", gone)
	}
}

func TestRootLongMentionsNoTerminalObserver(t *testing.T) {
	root := newRootCmd()
	assert.NotContains(t, root.Long, "terminal observer")
}

func TestRootHelpContainsGroupTitles(t *testing.T) {
	root := newRootCmd()
	usage := root.UsageString()
	assert.Contains(t, usage, "Observe:")
	assert.Contains(t, usage, "Setup:")
	assert.Contains(t, usage, "Advanced:")
}
