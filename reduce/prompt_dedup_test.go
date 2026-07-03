package reduce_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/ingest/hook"
	"github.com/realkarych/catacomb/ingest/jsonl"
	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/reduce"
)

func promptSeq() func() uint64 {
	var n uint64
	return func() uint64 { n++; return n }
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

func TestHookAndJSONLPromptReconcileToOneNode(t *testing.T) {
	const execID = "e1"
	hookObs, _, err := hook.Parse("UserPromptSubmit", []byte(`{"session_id":"s1","prompt":"list files"}`), execID, promptSeq())
	require.NoError(t, err)
	jsonlLine := `{"type":"user","uuid":"u1","sessionId":"s1","timestamp":"2026-06-20T10:00:00Z","message":{"role":"user","content":"list files"}}` + "\n"
	jsonlObs, err := jsonl.ParseReader(strings.NewReader(jsonlLine), execID)
	require.NoError(t, err)

	g := reduce.NewGraph()
	g.ApplyAll(hookObs)
	g.ApplyAll(jsonlObs)

	assert.Equal(t, 1, countPromptNodes(g), "hook + jsonl for one logical prompt must reconcile to exactly one user_prompt node")
	assert.Equal(t, 1, countSessionPromptEdges(g, execID), "must yield exactly one session->prompt edge")
}

func TestHookOnlyPromptCreatesExactlyOneNode(t *testing.T) {
	const execID = "e1"
	hookObs, _, err := hook.Parse("UserPromptSubmit", []byte(`{"session_id":"s1","prompt":"list files"}`), execID, promptSeq())
	require.NoError(t, err)

	g := reduce.NewGraph()
	g.ApplyAll(hookObs)

	assert.Equal(t, 1, countPromptNodes(g))
	assert.Equal(t, 1, countSessionPromptEdges(g, execID))
}
