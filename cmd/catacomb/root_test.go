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
	assert.Equal(t, "observe", groups["ui"])
	assert.Equal(t, "observe", groups["watch"])
	assert.Equal(t, "observe", groups["status"])
	assert.Equal(t, "observe", groups["observe"])
	assert.Equal(t, "setup", groups["daemon"])
	assert.Equal(t, "setup", groups["install-hooks"])
	assert.Equal(t, "setup", groups["env"])
	assert.Equal(t, "advanced", groups["hook"])
	assert.Equal(t, "advanced", groups["ingest"])
	assert.Equal(t, "advanced", groups["run"])
	assert.Equal(t, "advanced", groups["replay"])
	assert.Equal(t, "advanced", groups["demo"])
	assert.Equal(t, "advanced", groups["runs"])
	assert.Equal(t, "advanced", groups["inspect"])
	assert.Equal(t, "advanced", groups["version"])
}

func TestRootHelpContainsGroupTitles(t *testing.T) {
	root := newRootCmd()
	usage := root.UsageString()
	assert.Contains(t, usage, "Observe:")
	assert.Contains(t, usage, "Setup:")
	assert.Contains(t, usage, "Advanced:")
}
