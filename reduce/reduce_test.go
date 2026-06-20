package reduce

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
)

const (
	execID = "exec1"
	runID  = "s1"
)

func ob(kind, toolUse string, ts time.Time) model.Observation {
	return model.Observation{
		RunID:       runID,
		ExecutionID: execID,
		Source:      model.SourceJSONL,
		Kind:        kind,
		EventTime:   ts,
		ObservedAt:  ts,
		Correlation: model.Correlation{SessionID: runID, ToolUseID: toolUse},
	}
}

func TestToolCallMergesUseAndResult(t *testing.T) {
	t0 := time.Date(2026, 6, 20, 10, 0, 1, 0, time.UTC)
	t1 := t0.Add(time.Second)

	use := ob("assistant_tool_use", "toolu_1", t0)
	use.Correlation.MessageID = "msg_1"
	use.Attrs = map[string]any{"name": "Bash"}
	use.Payload = &model.Payload{Input: []byte(`{"command":"ls"}`)}

	res := ob("tool_result", "toolu_1", t1)
	res.Attrs = map[string]any{"status": string(model.StatusOK)}
	res.Payload = &model.Payload{Output: []byte(`"a.txt"`)}

	g := NewGraph()
	g.ApplyAll([]model.Observation{use, res})

	n := g.Nodes[model.ToolCallID(execID, "toolu_1")]
	require.NotNil(t, n)
	assert.Equal(t, model.NodeToolCall, n.Type)
	assert.Equal(t, runID, n.RunID)
	assert.Equal(t, "Bash", n.Name)
	assert.Equal(t, model.StatusOK, n.Status)
	require.NotNil(t, n.Payload)
	assert.NotEmpty(t, n.Payload.Input)
	assert.NotEmpty(t, n.Payload.Output)
	assert.Len(t, n.Sources, 2)
	require.NotNil(t, n.TStart)
	assert.Equal(t, t0, *n.TStart)

	require.NotNil(t, g.Edges[model.EdgeID(execID, model.EdgeParentChild, model.AssistantTurnID(execID, "msg_1"), n.ID)])
}

func TestMCPNodeType(t *testing.T) {
	use := ob("assistant_tool_use", "toolu_2", time.Unix(0, 0).UTC())
	use.Attrs = map[string]any{"name": "mcp__fs__read"}
	g := NewGraph()
	g.Apply(use)
	assert.Equal(t, model.NodeMCPCall, g.Nodes[model.ToolCallID(execID, "toolu_2")].Type)
}

func TestUserPromptAndAssistantTurn(t *testing.T) {
	t0 := time.Unix(0, 0).UTC()
	up := ob("user_prompt", "", t0)
	up.Correlation.UUID = "u1"
	turn := ob("assistant_turn", "", t0)
	turn.Correlation.MessageID = "msg_1"
	turn.Attrs = map[string]any{"model": "claude-opus-4-8", "tokens_in": int64(10), "tokens_out": int64(5)}

	g := NewGraph()
	g.ApplyAll([]model.Observation{up, turn})

	require.NotNil(t, g.Nodes[model.UserPromptID(execID, "u1")])
	require.NotNil(t, g.Edges[model.EdgeID(execID, model.EdgeParentChild, model.SessionNodeID(execID), model.UserPromptID(execID, "u1"))])
	turnNode := g.Nodes[model.AssistantTurnID(execID, "msg_1")]
	require.NotNil(t, turnNode.TokensIn)
	assert.Equal(t, int64(10), *turnNode.TokensIn)
	assert.Equal(t, int64(5), *turnNode.TokensOut)
}

func TestApplyUnknownKind(t *testing.T) {
	g := NewGraph()
	g.Apply(ob("mystery", "", time.Unix(0, 0).UTC()))
	require.NotNil(t, g.Nodes[model.SessionNodeID(execID)])
	assert.Len(t, g.Nodes, 1)
}

func TestApplyOrderIndependentFields(t *testing.T) {
	t0 := time.Date(2026, 6, 20, 10, 0, 1, 0, time.UTC)
	t1 := t0.Add(time.Second)
	use := ob("assistant_tool_use", "toolu_1", t0)
	use.Attrs = map[string]any{"name": "Bash"}
	res := ob("tool_result", "toolu_1", t1)
	res.Attrs = map[string]any{"status": string(model.StatusOK)}

	fwd := NewGraph()
	fwd.ApplyAll([]model.Observation{use, res})
	rev := NewGraph()
	rev.ApplyAll([]model.Observation{res, use})

	id := model.ToolCallID(execID, "toolu_1")
	assert.Equal(t, fwd.Nodes[id].Name, rev.Nodes[id].Name)
	assert.Equal(t, fwd.Nodes[id].Status, rev.Nodes[id].Status)
	assert.Equal(t, *fwd.Nodes[id].TStart, *rev.Nodes[id].TStart)
	assert.Equal(t, fwd.Nodes[id].Type, rev.Nodes[id].Type)
}

func TestApplyToolTypeUpgradeReversedOrder(t *testing.T) {
	t0 := time.Date(2026, 6, 20, 10, 0, 1, 0, time.UTC)
	t1 := t0.Add(time.Second)
	res := ob("tool_result", "toolu_3", t0)
	res.Attrs = map[string]any{"status": string(model.StatusOK)}
	use := ob("assistant_tool_use", "toolu_3", t1)
	use.Attrs = map[string]any{"name": "mcp__fs__read"}

	g := NewGraph()
	g.ApplyAll([]model.Observation{res, use})

	assert.Equal(t, model.NodeMCPCall, g.Nodes[model.ToolCallID(execID, "toolu_3")].Type)
}

func TestToInt64(t *testing.T) {
	cases := []struct {
		in any
		ok bool
		n  int64
	}{
		{int64(7), true, 7},
		{int(7), true, 7},
		{float64(7), true, 7},
		{"7", false, 0},
	}
	for _, c := range cases {
		n, ok := toInt64(c.in)
		assert.Equal(t, c.ok, ok)
		assert.Equal(t, c.n, n)
	}
}

func TestIsMCP(t *testing.T) {
	assert.True(t, isMCP("mcp__fs__read"))
	assert.False(t, isMCP("Bash"))
}

func TestUpsertEdgeEmptyGuard(t *testing.T) {
	g := NewGraph()
	g.upsertEdge(execID, runID, "", "x")
	assert.Empty(t, g.Edges)
}

func TestMergePayloadNil(t *testing.T) {
	g := NewGraph()
	o := ob("assistant_tool_use", "toolu_9", time.Unix(0, 0).UTC())
	o.Attrs = map[string]any{"name": "Bash"}
	g.Apply(o)
	assert.Nil(t, g.Nodes[model.ToolCallID(execID, "toolu_9")].Payload)
}

func TestMergePayloadOutputOnly(t *testing.T) {
	g := NewGraph()
	o := ob("tool_result", "toolu_10", time.Unix(0, 0).UTC())
	o.Payload = &model.Payload{Output: []byte(`"done"`)}
	g.Apply(o)
	n := g.Nodes[model.ToolCallID(execID, "toolu_10")]
	require.NotNil(t, n.Payload)
	assert.Empty(t, n.Payload.Input)
	assert.NotEmpty(t, n.Payload.Output)
}

func TestStatusLatticeTerminalNotOverwritten(t *testing.T) {
	t0 := time.Unix(0, 0).UTC()
	res := ob("tool_result", "toolu_x", t0)
	res.Attrs = map[string]any{"status": string(model.StatusOK)}
	use := ob("assistant_tool_use", "toolu_x", t0)
	use.Attrs = map[string]any{"name": "Bash", "status": string(model.StatusRunning)}

	g := NewGraph()
	g.ApplyAll([]model.Observation{res, use})

	assert.Equal(t, model.StatusOK, g.Nodes[model.ToolCallID(execID, "toolu_x")].Status)
}

func TestStatusLatticeProvisionalThenTerminal(t *testing.T) {
	t0 := time.Unix(0, 0).UTC()
	use := ob("assistant_tool_use", "toolu_y", t0)
	use.Attrs = map[string]any{"name": "Bash", "status": string(model.StatusRunning)}
	res := ob("tool_result", "toolu_y", t0)
	res.Attrs = map[string]any{"status": string(model.StatusError)}

	g := NewGraph()
	g.ApplyAll([]model.Observation{use, res})

	assert.Equal(t, model.StatusError, g.Nodes[model.ToolCallID(execID, "toolu_y")].Status)
}

func TestToolWithoutMessageIDAttachesToSession(t *testing.T) {
	use := ob("assistant_tool_use", "toolu_z", time.Unix(0, 0).UTC())
	use.Attrs = map[string]any{"name": "Bash"}

	g := NewGraph()
	g.Apply(use)

	tool := model.ToolCallID(execID, "toolu_z")
	require.NotNil(t, g.Edges[model.EdgeID(execID, model.EdgeParentChild, model.SessionNodeID(execID), tool)])
}

func TestSessionStart(t *testing.T) {
	o := ob("session_start", "", time.Unix(0, 0).UTC())
	g := NewGraph()
	g.Apply(o)
	n := g.Nodes[model.SessionNodeID(execID)]
	require.NotNil(t, n)
	assert.Equal(t, model.StatusRunning, n.Status)
	require.NotNil(t, n.TStart)
}

func TestSessionEnd(t *testing.T) {
	t0 := time.Unix(0, 0).UTC()
	g := NewGraph()
	g.Apply(ob("session_start", "", t0))
	g.Apply(ob("session_end", "", t0.Add(time.Second)))
	n := g.Nodes[model.SessionNodeID(execID)]
	require.NotNil(t, n.TEnd)
	assert.Equal(t, model.StatusOK, n.Status)
}

func TestSubagentStop(t *testing.T) {
	o := ob("subagent_stop", "", time.Unix(0, 0).UTC())
	o.Correlation.AgentID = "a1"
	o.Attrs = map[string]any{"subagent_type": "researcher"}
	g := NewGraph()
	g.Apply(o)
	n := g.Nodes[model.SubagentID(execID, "a1")]
	require.NotNil(t, n)
	assert.Equal(t, model.NodeSubagent, n.Type)
	assert.Equal(t, "a1", n.AgentID)
	assert.Equal(t, "researcher", n.SubagentType)
	require.NotNil(t, n.TEnd)
	assert.Equal(t, model.StatusOK, n.Status)
	require.NotNil(t, g.Edges[model.EdgeID(execID, model.EdgeParentChild, model.SessionNodeID(execID), n.ID)])
}

func TestSubagentStopMinimal(t *testing.T) {
	o := ob("subagent_stop", "", time.Unix(0, 0).UTC())
	g := NewGraph()
	g.Apply(o)
	n := g.Nodes[model.SubagentID(execID, "")]
	require.NotNil(t, n)
	assert.Empty(t, n.AgentID)
	assert.Empty(t, n.SubagentType)
}

func TestSnapshotReturnsAllNodesAndEdges(t *testing.T) {
	t0 := time.Unix(0, 0).UTC()
	up := ob("user_prompt", "", t0)
	up.Correlation.UUID = "u1"
	g := NewGraph()
	g.Apply(up)
	nodes, edges := g.Snapshot()
	assert.Len(t, nodes, 2)
	assert.Len(t, edges, 1)
}

func TestIsTerminal(t *testing.T) {
	assert.True(t, isTerminal(model.StatusOK))
	assert.True(t, isTerminal(model.StatusError))
	assert.True(t, isTerminal(model.StatusBlocked))
	assert.True(t, isTerminal(model.StatusCancelled))
	assert.False(t, isTerminal(model.StatusRunning))
	assert.False(t, isTerminal(model.StatusPending))
}
