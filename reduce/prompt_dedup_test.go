package reduce_test

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/ingest/jsonl"
	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/reduce"
)

func parseJSONL(r io.Reader, executionID string) ([]model.Observation, error) {
	var seq uint64
	obs, _, err := jsonl.Parse(r, executionID, func() uint64 {
		s := seq
		seq++
		return s
	}, func(ts time.Time) time.Time { return ts })
	return obs, err
}

func countPromptNodes(g *reduce.Graph) int {
	c := 0
	for _, n := range g.Nodes {
		if n.Type == model.NodeUserPrompt {
			c++
		}
	}
	return c
}

func countSessionPromptEdges(g *reduce.Graph, execID string) int {
	session := model.SessionNodeID(execID)
	c := 0
	for _, e := range g.Edges {
		if e.Type != model.EdgeParentChild || e.Src != session {
			continue
		}
		if n, ok := g.Nodes[e.Dst]; ok && n.Type == model.NodeUserPrompt {
			c++
		}
	}
	return c
}

const dedupPromptLine = `{"type":"user","uuid":"u1","sessionId":"s1","timestamp":"2026-06-20T10:00:00Z","message":{"role":"user","content":"list files"}}` + "\n"

func TestDuplicatePromptsReconcileToOneNode(t *testing.T) {
	const execID = "e1"
	first, err := parseJSONL(strings.NewReader(dedupPromptLine), execID)
	require.NoError(t, err)
	second, err := parseJSONL(strings.NewReader(dedupPromptLine), execID)
	require.NoError(t, err)

	g := reduce.NewGraph()
	g.ApplyAll(first)
	g.ApplyAll(second)

	assert.Equal(t, 1, countPromptNodes(g), "two observations for one logical prompt must reconcile to exactly one user_prompt node")
	assert.Equal(t, 1, countSessionPromptEdges(g, execID), "must yield exactly one session->prompt edge")
}

func TestSinglePromptCreatesExactlyOneNode(t *testing.T) {
	const execID = "e1"
	obs, err := parseJSONL(strings.NewReader(dedupPromptLine), execID)
	require.NoError(t, err)

	g := reduce.NewGraph()
	g.ApplyAll(obs)

	assert.Equal(t, 1, countPromptNodes(g))
	assert.Equal(t, 1, countSessionPromptEdges(g, execID))
}
