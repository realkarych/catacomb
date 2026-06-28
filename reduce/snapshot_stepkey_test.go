package reduce

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/stepkey"
)

func TestSnapshotPopulatesStepKeyOnToolCall(t *testing.T) {
	t0 := time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC)
	use := ob("assistant_tool_use", "toolu_sk1", t0)
	use.Correlation.MessageID = "msg_sk1"
	use.Attrs = map[string]any{"name": "Bash"}
	use.Payload = &model.Payload{Input: []byte(`{"command":"ls"}`)}

	res := ob("tool_result", "toolu_sk1", t0.Add(time.Second))
	res.Attrs = map[string]any{"status": string(model.StatusOK)}

	g := NewGraph()
	g.ApplyAll([]model.Observation{use, res})

	nodes, _ := g.Snapshot()

	toolID := model.ToolCallID(execID, "toolu_sk1")
	var toolNode *model.Node
	for _, n := range nodes {
		if n.ID == toolID {
			toolNode = n
			break
		}
	}
	require.NotNil(t, toolNode)
	assert.NotEmpty(t, toolNode.StepKey)
	assert.Equal(t, stepkey.Method, toolNode.StepKeyMethod)
	assert.Len(t, toolNode.StepKey, 32)
}

func TestSnapshotSessionNodeHasNoStepKey(t *testing.T) {
	t0 := time.Unix(0, 0).UTC()
	up := ob("user_prompt", "", t0)
	up.Correlation.UUID = "u_sk1"

	g := NewGraph()
	g.Apply(up)

	nodes, _ := g.Snapshot()

	sessID := model.SessionNodeID(execID)
	for _, n := range nodes {
		if n.ID == sessID {
			assert.Empty(t, n.StepKey)
			assert.Empty(t, n.StepKeyMethod)
			return
		}
	}
	t.Fatal("session node not found")
}

func TestSnapshotStepKeyStableAcrossIdenticalGraphs(t *testing.T) {
	t0 := time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC)

	buildGraph := func() *Graph {
		use := ob("assistant_tool_use", "toolu_stable", t0)
		use.Correlation.MessageID = "msg_stable"
		use.Attrs = map[string]any{"name": "Read"}
		use.Payload = &model.Payload{Input: []byte(`{"file_path":"a.go"}`)}
		res := ob("tool_result", "toolu_stable", t0.Add(time.Second))
		res.Attrs = map[string]any{"status": string(model.StatusOK)}
		g := NewGraph()
		g.ApplyAll([]model.Observation{use, res})
		return g
	}

	g1 := buildGraph()
	g2 := buildGraph()

	nodes1, _ := g1.Snapshot()
	nodes2, _ := g2.Snapshot()

	toolID := model.ToolCallID(execID, "toolu_stable")
	var k1, k2 string
	for _, n := range nodes1 {
		if n.ID == toolID {
			k1 = n.StepKey
		}
	}
	for _, n := range nodes2 {
		if n.ID == toolID {
			k2 = n.StepKey
		}
	}
	require.NotEmpty(t, k1)
	assert.Equal(t, k1, k2)
}
