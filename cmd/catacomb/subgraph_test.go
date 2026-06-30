package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/subgraph"
)

func TestSubgraphCommandJSON(t *testing.T) {
	root := newRootCmd()
	var sb strings.Builder
	root.SetOut(&sb)
	root.SetArgs([]string{"subgraph", "--phase", "plan", "--json", "testdata/session_marked.jsonl"})
	require.NoError(t, root.Execute())
	out := sb.String()
	assert.Contains(t, out, "toolu_2")
	assert.NotContains(t, out, "toolu_1")
	assert.NotContains(t, out, "toolu_3")
}

func TestSubgraphCommandHuman(t *testing.T) {
	root := newRootCmd()
	var sb strings.Builder
	root.SetOut(&sb)
	root.SetArgs([]string{"subgraph", "--phase", "plan", "testdata/session_marked.jsonl"})
	require.NoError(t, root.Execute())
	out := sb.String()
	assert.Contains(t, out, "nodes:")
	assert.Contains(t, out, "toolu_2")
	assert.NotContains(t, out, "toolu_1")
}

func TestSubgraphCommandPhaseNotFound(t *testing.T) {
	root := newRootCmd()
	root.SetOut(&strings.Builder{})
	root.SetArgs([]string{"subgraph", "--phase", "ghost", "testdata/session_marked.jsonl"})
	err := root.Execute()
	assert.ErrorIs(t, err, subgraph.ErrPhaseNotFound)
}

func TestSubgraphCommandInvalidSelector(t *testing.T) {
	root := newRootCmd()
	root.SetOut(&strings.Builder{})
	root.SetArgs([]string{"subgraph", "--from", "plan", "testdata/session_marked.jsonl"})
	err := root.Execute()
	assert.ErrorIs(t, err, subgraph.ErrInvalidSelector)
}

func TestRunSubgraphMissingFile(t *testing.T) {
	_, _, err := runSubgraph(subgraphArgs{input: "/nonexistent/file.jsonl", phase: "plan"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDiffInput)
}

func TestRunSubgraphEmptySpec(t *testing.T) {
	_, _, err := runSubgraph(subgraphArgs{input: "testdata/session_marked.jsonl"})
	assert.ErrorIs(t, err, subgraph.ErrInvalidSelector)
}
