package reduce

import (
	"strconv"
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

func TestResolveStatusGenuineLatches(t *testing.T) {
	assert.Equal(t, model.StatusOK, resolveStatus(model.StatusOK, model.StatusRunning))
	assert.Equal(t, model.StatusError, resolveStatus(model.StatusError, model.StatusPending))
}

func TestResolveStatusGenuineSupersedesProvisional(t *testing.T) {
	assert.Equal(t, model.StatusOK, resolveStatus(model.StatusUnknown, model.StatusOK))
	assert.Equal(t, model.StatusError, resolveStatus(model.StatusCancelled, model.StatusError))
}

func TestResolveStatusProvisionalOverRunning(t *testing.T) {
	assert.Equal(t, model.StatusUnknown, resolveStatus(model.StatusRunning, model.StatusUnknown))
}

func TestResolveStatusProvisionalNotRevertedByRunning(t *testing.T) {
	assert.Equal(t, model.StatusUnknown, resolveStatus(model.StatusUnknown, model.StatusRunning))
}

func TestResolveStatusRunningOverPending(t *testing.T) {
	assert.Equal(t, model.StatusRunning, resolveStatus(model.StatusPending, model.StatusRunning))
}

func TestResolveStatusTieTakesNext(t *testing.T) {
	assert.Equal(t, model.StatusError, resolveStatus(model.StatusOK, model.StatusError))
	assert.Equal(t, model.StatusRunning, resolveStatus(model.StatusRunning, model.StatusRunning))
}

func sessionStartObs(exec, runID string, seq uint64) model.Observation {
	return model.Observation{
		ObsID: "o" + strconv.FormatUint(seq, 10), RunID: runID, ExecutionID: exec,
		Source: model.SourceHook, Kind: "session_start",
		Correlation: model.Correlation{SessionID: runID},
		EventTime:   time.Unix(int64(seq), 0).UTC(), Seq: seq,
	}
}

func runEndedObs(exec, runID, reason string, seq uint64) model.Observation {
	attrs := map[string]any{}
	if reason != "" {
		attrs["reason"] = reason
	}
	return model.Observation{
		ObsID: "o" + strconv.FormatUint(seq, 10), RunID: runID, ExecutionID: exec,
		Source: model.SourceHook, Kind: "run_ended",
		Correlation: model.Correlation{}, Attrs: attrs,
		EventTime: time.Unix(int64(seq), 0).UTC(), Seq: seq,
	}
}

func TestRunOpensOnFirstObs(t *testing.T) {
	g := NewGraph()
	g.Apply(sessionStartObs("e1", "s1", 1))
	r := g.Runs["s1"]
	require.NotNil(t, r)
	assert.Equal(t, model.StatusRunning, r.Status)
	require.NotNil(t, r.StartedAt)
	assert.Equal(t, []string{"s1"}, r.SessionIDs)
}

func TestRunLastSeqTracksMaxIgnoringOutOfOrder(t *testing.T) {
	g := NewGraph()
	g.ApplyAll([]model.Observation{
		sessionStartObs("e1", "s1", 5),
		toolObs("e1", "s1", "t1", "Bash", "running", 3),
	})
	assert.Equal(t, uint64(5), g.Runs["s1"].LastSeq)
}

func TestSessionEndEndsRunOK(t *testing.T) {
	g := NewGraph()
	g.ApplyAll([]model.Observation{
		sessionStartObs("e1", "s1", 1),
		sessionEndObs("e1", "s1", 2),
	})
	r := g.Runs["s1"]
	assert.Equal(t, model.StatusOK, r.Status)
	assert.Equal(t, "session_ended", r.EndReason)
	require.NotNil(t, r.EndedAt)
}

func TestRunEndedAbandonsRunAndClosesDescendants(t *testing.T) {
	g := NewGraph()
	g.ApplyAll([]model.Observation{
		toolObs("e1", "s1", "t1", "Bash", "running", 1),
		runEndedObs("e1", "s1", "timeout", 2),
	})
	assert.Equal(t, model.StatusAbandoned, g.Runs["s1"].Status)
	assert.Equal(t, "timeout", g.Runs["s1"].EndReason)
	assert.Equal(t, model.StatusUnknown, g.Nodes[model.ToolCallID("e1", "t1")].Status)
}

func TestRunEndedNoReason(t *testing.T) {
	g := NewGraph()
	g.ApplyAll([]model.Observation{
		sessionStartObs("e1", "s1", 1),
		runEndedObs("e1", "s1", "", 2),
	})
	assert.Equal(t, model.StatusAbandoned, g.Runs["s1"].Status)
	assert.Equal(t, "", g.Runs["s1"].EndReason)
}

func TestRunEndedClearsStaleEndReason(t *testing.T) {
	g := NewGraph()
	g.ApplyAll([]model.Observation{
		sessionStartObs("e3", "s3", 1),
		sessionEndObs("e3", "s3", 2),
		runEndedObs("e3", "s3", "", 3),
	})
	r := g.Runs["s3"]
	assert.Equal(t, model.StatusAbandoned, r.Status)
	assert.Equal(t, "", r.EndReason)
}

func TestRunReawakenFromAbandoned(t *testing.T) {
	g := NewGraph()
	g.ApplyAll([]model.Observation{
		sessionStartObs("e1", "s1", 1),
		runEndedObs("e1", "s1", "timeout", 2),
		toolObs("e1", "s1", "t1", "Bash", "running", 3),
	})
	r := g.Runs["s1"]
	assert.Equal(t, model.StatusRunning, r.Status)
	assert.Nil(t, r.EndedAt)
	assert.Equal(t, "", r.EndReason)
}

func TestRunsSnapshot(t *testing.T) {
	g := NewGraph()
	g.Apply(sessionStartObs("e1", "s1", 1))
	snap := g.RunsSnapshot()
	require.Len(t, snap, 1)
	assert.Equal(t, "s1", snap[0].ID)
}

func TestRunsSnapshotMultipleRuns(t *testing.T) {
	g := NewGraph()
	g.Apply(sessionStartObs("e1", "s1", 1))
	g.Apply(sessionStartObs("e2", "s2", 2))
	snap := g.RunsSnapshot()
	require.Len(t, snap, 2)
}

func toolObs(exec, runID, toolUseID, name, status string, seq uint64) model.Observation {
	return model.Observation{
		ObsID: "o" + strconv.FormatUint(seq, 10), RunID: runID, ExecutionID: exec,
		Source: model.SourceHook, Kind: "assistant_tool_use",
		Correlation: model.Correlation{SessionID: runID, ToolUseID: toolUseID},
		Attrs:       map[string]any{"name": name, "status": status},
		EventTime:   time.Unix(int64(seq), 0).UTC(), Seq: seq,
	}
}

func sessionEndObs(exec, runID string, seq uint64) model.Observation {
	return model.Observation{
		ObsID: "o" + strconv.FormatUint(seq, 10), RunID: runID, ExecutionID: exec,
		Source: model.SourceHook, Kind: "session_end",
		Correlation: model.Correlation{SessionID: runID},
		EventTime:   time.Unix(int64(seq), 0).UTC(), Seq: seq,
	}
}

func TestSessionEndClosesRunningDescendant(t *testing.T) {
	g := NewGraph()
	g.ApplyAll([]model.Observation{
		toolObs("e1", "s1", "t1", "Bash", "running", 1),
		sessionEndObs("e1", "s1", 2),
	})
	assert.Equal(t, model.StatusUnknown, g.Nodes[model.ToolCallID("e1", "t1")].Status)
}

func TestSessionEndLeavesGenuineTerminal(t *testing.T) {
	g := NewGraph()
	g.ApplyAll([]model.Observation{
		toolObs("e2", "s2", "t1", "Read", "ok", 1),
		sessionEndObs("e2", "s2", 2),
	})
	assert.Equal(t, model.StatusOK, g.Nodes[model.ToolCallID("e2", "t1")].Status)
}

func TestSessionEndLateGenuineSupersedesUnknown(t *testing.T) {
	fwd := NewGraph()
	fwd.ApplyAll([]model.Observation{
		toolObs("e1", "s1", "t2", "Bash", "running", 1),
		sessionEndObs("e1", "s1", 4),
		toolObs("e1", "s1", "t2", "Bash", "ok", 5),
	})
	rev := NewGraph()
	rev.ApplyAll([]model.Observation{
		toolObs("e1", "s1", "t2", "Bash", "ok", 5),
		toolObs("e1", "s1", "t2", "Bash", "running", 1),
		sessionEndObs("e1", "s1", 4),
	})
	assert.Equal(t, model.StatusOK, fwd.Nodes[model.ToolCallID("e1", "t2")].Status)
	assert.Equal(t, model.StatusOK, rev.Nodes[model.ToolCallID("e1", "t2")].Status)
}

func TestCloseOpenDescendantsHandlesDiamond(t *testing.T) {
	g := NewGraph()
	g.node(model.SessionNodeID("e1"), "s1", model.NodeSession)
	g.node("a", "s1", model.NodeToolCall).Status = model.StatusRunning
	g.node("b", "s1", model.NodeToolCall).Status = model.StatusRunning
	g.node("c", "s1", model.NodeToolCall).Status = model.StatusRunning
	g.upsertEdge("e1", "s1", model.SessionNodeID("e1"), "a")
	g.upsertEdge("e1", "s1", model.SessionNodeID("e1"), "b")
	g.upsertEdge("e1", "s1", "a", "c")
	g.upsertEdge("e1", "s1", "b", "c")
	g.closeOpenDescendants(model.SessionNodeID("e1"))
	assert.Equal(t, model.StatusUnknown, g.Nodes["c"].Status)
}

func TestCloseOpenDescendantsSkipsMissingNode(t *testing.T) {
	g := NewGraph()
	g.node(model.SessionNodeID("e1"), "s1", model.NodeSession)
	g.upsertEdge("e1", "s1", model.SessionNodeID("e1"), "ghost")
	g.closeOpenDescendants(model.SessionNodeID("e1"))
	assert.NotContains(t, g.Nodes, "ghost")
}

func TestCloseOpenDescendantsIgnoresNonParentChild(t *testing.T) {
	g := NewGraph()
	g.node(model.SessionNodeID("e1"), "s1", model.NodeSession)
	g.node("x", "s1", model.NodeToolCall).Status = model.StatusRunning
	g.Edges["seq"] = &model.Edge{ID: "seq", RunID: "s1", Type: model.EdgeSequence, Src: model.SessionNodeID("e1"), Dst: "x"}
	g.closeOpenDescendants(model.SessionNodeID("e1"))
	assert.Equal(t, model.StatusRunning, g.Nodes["x"].Status)
}
