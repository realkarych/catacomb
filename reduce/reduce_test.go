package reduce

import (
	"encoding/json"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/cdc"
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
	g.upsertEdge(execID, runID, "", "x", 1)
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

func TestRunStatusGenuineSessionEndLatchesOverRunEnded(t *testing.T) {
	fwd := NewGraph()
	fwd.ApplyAll([]model.Observation{
		sessionStartObs("e1", "s1", 1),
		runEndedObs("e1", "s1", "timeout", 2),
		sessionEndObs("e1", "s1", 3),
	})
	rev := NewGraph()
	rev.ApplyAll([]model.Observation{
		sessionStartObs("e2", "s2", 1),
		sessionEndObs("e2", "s2", 3),
		runEndedObs("e2", "s2", "timeout", 4),
	})
	assert.Equal(t, model.StatusOK, fwd.Runs["s1"].Status)
	assert.Equal(t, model.StatusOK, rev.Runs["s2"].Status)
	assert.Equal(t, "session_ended", fwd.Runs["s1"].EndReason)
	assert.Equal(t, "session_ended", rev.Runs["s2"].EndReason)
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

func otelTool(exec, runID, toolUse, span, parentSpan string, seq uint64) model.Observation {
	return model.Observation{
		ObsID: "o" + strconv.FormatUint(seq, 10), RunID: runID, ExecutionID: exec,
		Source: model.SourceOTel, Kind: "assistant_tool_use",
		Correlation: model.Correlation{SessionID: runID, ToolUseID: toolUse, SpanID: span, ParentSpanID: parentSpan},
		Attrs:       map[string]any{"name": "Bash"},
		EventTime:   time.Unix(int64(seq), 0).UTC(), Seq: seq,
	}
}

func TestSpanChildrenRecordedForAnyParentSpan(t *testing.T) {
	g := NewGraph()
	g.Apply(otelTool("e1", "s1", "t1", "spanChild", "spanParent", 1))
	assert.True(t, g.spanChildren["spanParent"])
}

func TestGateAcceptsOTelEdgeWhenToolUseIDPresent(t *testing.T) {
	g := NewGraph()
	o := otelTool("e1", "s1", "tA", "spanA", "spanRoot", 1)
	g.Apply(o)
	tool := model.ToolCallID("e1", "tA")
	require.Contains(t, g.Edges, model.EdgeID("e1", model.EdgeParentChild, model.SessionNodeID("e1"), tool))

	g2 := NewGraph()
	o2 := otelTool("e2", "s2", "tB", "spanB", "spanRoot2", 1)
	g2.Apply(o2)
	tool2 := model.ToolCallID("e2", "tB")
	require.Contains(t, g2.Edges, model.EdgeID("e2", model.EdgeParentChild, model.SessionNodeID("e2"), tool2))
}

func TestGateSkipsOTelEdgeWhenNoChildrenAndNoToolUseID(t *testing.T) {
	g := NewGraph()
	o := model.Observation{
		ObsID: "o1", RunID: "s1", ExecutionID: "e1", Source: model.SourceOTel, Kind: "assistant_tool_use",
		Correlation: model.Correlation{SessionID: "s1", ToolUseID: "", SpanID: "spanFlat", ParentSpanID: "spanRoot"},
		Attrs:       map[string]any{"name": "Bash"}, EventTime: time.Unix(1, 0).UTC(), Seq: 1,
	}
	g.Apply(o)
	tool := model.ToolCallID("e1", "")
	assert.NotContains(t, g.Edges, model.EdgeID("e1", model.EdgeParentChild, model.SessionNodeID("e1"), tool))
}

func TestGateAcceptsOTelEdgeWhenSpanHasObservedChild(t *testing.T) {
	g := NewGraph()
	g.Apply(otelTool("e1", "s1", "tChild", "spanInner", "spanMid", 1))
	o := model.Observation{
		ObsID: "o2", RunID: "s1", ExecutionID: "e1", Source: model.SourceOTel, Kind: "assistant_tool_use",
		Correlation: model.Correlation{SessionID: "s1", ToolUseID: "", SpanID: "spanMid", ParentSpanID: "spanRoot"},
		Attrs:       map[string]any{"name": "Read"}, EventTime: time.Unix(2, 0).UTC(), Seq: 2,
	}
	g.Apply(o)
	tool := model.ToolCallID("e1", "")
	assert.Contains(t, g.Edges, model.EdgeID("e1", model.EdgeParentChild, model.SessionNodeID("e1"), tool))
}

func TestGateNeverAppliesToHookEdges(t *testing.T) {
	g := NewGraph()
	g.Apply(toolObs("e1", "s1", "", "Bash", "running", 1))
	tool := model.ToolCallID("e1", "")
	assert.Contains(t, g.Edges, model.EdgeID("e1", model.EdgeParentChild, model.SessionNodeID("e1"), tool))
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

func TestCascadeStatusHandlesDiamond(t *testing.T) {
	g := NewGraph()
	g.node(model.SessionNodeID("e1"), "s1", model.NodeSession)
	g.node("a", "s1", model.NodeToolCall).Status = model.StatusRunning
	g.node("b", "s1", model.NodeToolCall).Status = model.StatusRunning
	g.node("c", "s1", model.NodeToolCall).Status = model.StatusRunning
	g.upsertEdge("e1", "s1", model.SessionNodeID("e1"), "a", 1)
	g.upsertEdge("e1", "s1", model.SessionNodeID("e1"), "b", 2)
	g.upsertEdge("e1", "s1", "a", "c", 3)
	g.upsertEdge("e1", "s1", "b", "c", 9)
	g.cascadeStatus(model.SessionNodeID("e1"), model.StatusUnknown, 1)
	assert.Equal(t, model.StatusUnknown, g.Nodes["c"].Status)
}

func TestCascadeStatusSkipsMissingNode(t *testing.T) {
	g := NewGraph()
	g.node(model.SessionNodeID("e1"), "s1", model.NodeSession)
	g.upsertEdge("e1", "s1", model.SessionNodeID("e1"), "ghost", 2)
	g.cascadeStatus(model.SessionNodeID("e1"), model.StatusUnknown, 2)
	assert.NotContains(t, g.Nodes, "ghost")
}

func TestCascadeStatusIgnoresNonParentChild(t *testing.T) {
	g := NewGraph()
	g.node(model.SessionNodeID("e1"), "s1", model.NodeSession)
	g.node("x", "s1", model.NodeToolCall).Status = model.StatusRunning
	g.Edges["seq"] = &model.Edge{ID: "seq", RunID: "s1", Type: model.EdgeSequence, Src: model.SessionNodeID("e1"), Dst: "x"}
	g.cascadeStatus(model.SessionNodeID("e1"), model.StatusUnknown, 3)
	assert.Equal(t, model.StatusRunning, g.Nodes["x"].Status)
}

func TestSessionEndLeavesPointInTimeNodes(t *testing.T) {
	g := NewGraph()
	g.ApplyAll([]model.Observation{
		sessionStartObs("e1", "s1", 1),
		{ObsID: "o2", RunID: "s1", ExecutionID: "e1", Source: model.SourceHook, Kind: "user_prompt", Correlation: model.Correlation{SessionID: "s1", UUID: "u1"}, EventTime: time.Unix(2, 0).UTC(), Seq: 2},
		sessionEndObs("e1", "s1", 3),
	})
	up := g.Nodes[model.UserPromptID("e1", "u1")]
	require.NotNil(t, up)
	assert.NotEqual(t, model.StatusUnknown, up.Status)
}

func TestRunEndedClosesSessionNode(t *testing.T) {
	g := NewGraph()
	g.ApplyAll([]model.Observation{
		sessionStartObs("e1", "s1", 1),
		runEndedObs("e1", "s1", "timeout", 2),
	})
	assert.Equal(t, model.StatusAbandoned, g.Runs["s1"].Status)
	assert.Equal(t, model.StatusUnknown, g.Nodes[model.SessionNodeID("e1")].Status)
}

func TestStopDoesNotTerminateRun(t *testing.T) {
	g := NewGraph()
	g.ApplyAll([]model.Observation{
		sessionStartObs("e1", "s1", 1),
		{ObsID: "o2", RunID: "s1", ExecutionID: "e1", Source: model.SourceHook, Kind: "stop", Correlation: model.Correlation{SessionID: "s1"}, EventTime: time.Unix(2, 0).UTC(), Seq: 2},
	})
	assert.Equal(t, model.StatusRunning, g.Runs["s1"].Status)
	assert.Equal(t, model.StatusRunning, g.Nodes[model.SessionNodeID("e1")].Status)
}

func TestRankSupersededAndAbandonedAreProvisional(t *testing.T) {
	assert.Equal(t, 2, rank(model.StatusSuperseded))
	assert.Equal(t, 2, rank(model.StatusAbandoned))
}

func TestResolveStatusSupersededOverRunningButUnderTerminal(t *testing.T) {
	assert.Equal(t, model.StatusSuperseded, resolveStatus(model.StatusRunning, model.StatusSuperseded))
	assert.Equal(t, model.StatusOK, resolveStatus(model.StatusSuperseded, model.StatusOK))
	assert.Equal(t, model.StatusSuperseded, resolveStatus(model.StatusUnknown, model.StatusSuperseded))
}

func TestNodeRevTracksMaxSeq(t *testing.T) {
	g := NewGraph()
	a := toolObs("e1", "s1", "t1", "Bash", "running", 3)
	b := toolObs("e1", "s1", "t1", "Bash", "ok", 7)
	c := toolObs("e1", "s1", "t1", "Bash", "ok", 5)
	g.ApplyAll([]model.Observation{a, b, c})
	assert.Equal(t, uint64(7), g.Nodes[model.ToolCallID("e1", "t1")].Rev)
}

func TestEdgeRevTracksMaxSeq(t *testing.T) {
	g := NewGraph()
	g.upsertEdge("e1", "s1", model.SessionNodeID("e1"), "x", 4)
	g.upsertEdge("e1", "s1", model.SessionNodeID("e1"), "x", 2)
	g.upsertEdge("e1", "s1", model.SessionNodeID("e1"), "x", 9)
	id := model.EdgeID("e1", model.EdgeParentChild, model.SessionNodeID("e1"), "x")
	assert.Equal(t, uint64(9), g.Edges[id].Rev)
}

func TestCascadeStatusCancelsNonTerminalDescendants(t *testing.T) {
	g := NewGraph()
	g.node(model.SessionNodeID("e1"), "s1", model.NodeSession)
	g.node("root", "s1", model.NodeToolCall).Status = model.StatusRunning
	g.node("childRun", "s1", model.NodeToolCall).Status = model.StatusRunning
	g.node("childDone", "s1", model.NodeToolCall).Status = model.StatusOK
	g.upsertEdge("e1", "s1", "root", "childRun", 1)
	g.upsertEdge("e1", "s1", "root", "childDone", 2)
	g.cascadeStatus("root", model.StatusCancelled, 42)
	assert.Equal(t, model.StatusCancelled, g.Nodes["childRun"].Status)
	assert.Equal(t, "root", g.Nodes["childRun"].Attrs["cancel_cause"])
	assert.Equal(t, model.StatusOK, g.Nodes["childDone"].Status)
	_, hasCause := g.Nodes["childDone"].Attrs["cancel_cause"]
	assert.False(t, hasCause)
}

func TestCascadeStatusSupersededSetsCause(t *testing.T) {
	g := NewGraph()
	g.node("root", "s1", model.NodeToolCall).Status = model.StatusRunning
	g.node("child", "s1", model.NodeToolCall).Status = model.StatusRunning
	g.upsertEdge("e1", "s1", "root", "child", 1)
	g.cascadeStatus("root", model.StatusSuperseded, 5)
	assert.Equal(t, model.StatusSuperseded, g.Nodes["child"].Status)
	assert.Equal(t, "root", g.Nodes["child"].Attrs["cancel_cause"])
}

func TestCascadeUnknownPathHasNoCancelCause(t *testing.T) {
	g := NewGraph()
	g.node(model.SessionNodeID("e1"), "s1", model.NodeSession)
	g.node("child", "s1", model.NodeToolCall).Status = model.StatusRunning
	g.upsertEdge("e1", "s1", model.SessionNodeID("e1"), "child", 1)
	g.cascadeStatus(model.SessionNodeID("e1"), model.StatusUnknown, 7)
	assert.Equal(t, model.StatusUnknown, g.Nodes["child"].Status)
	_, hasCause := g.Nodes["child"].Attrs["cancel_cause"]
	assert.False(t, hasCause)
}

func TestToolResultCancelledCascadesToChildren(t *testing.T) {
	g := NewGraph()
	parent := toolObs("e1", "s1", "tp", "Task", "running", 1)
	parent.Correlation.MessageID = "m1"
	g.Apply(parent)
	child := toolObs("e1", "s1", "tc", "Bash", "running", 2)
	child.Correlation.MessageID = ""
	g.Apply(child)
	g.upsertEdge("e1", "s1", model.ToolCallID("e1", "tp"), model.ToolCallID("e1", "tc"), 3)
	cancel := toolObs("e1", "s1", "tp", "Task", string(model.StatusCancelled), 4)
	cancel.Correlation.MessageID = "m1"
	g.Apply(cancel)
	assert.Equal(t, model.StatusCancelled, g.Nodes[model.ToolCallID("e1", "tc")].Status)
	assert.Equal(t, model.ToolCallID("e1", "tp"), g.Nodes[model.ToolCallID("e1", "tc")].Attrs["cancel_cause"])
}

func TestMarkerCreatesNodeAttachedToSession(t *testing.T) {
	g := NewGraph()
	o := model.Observation{
		ObsID: "m1", RunID: "s1", ExecutionID: "e1", Source: model.SourceHook, Kind: "marker",
		Correlation: model.Correlation{SessionID: "s1"},
		Attrs:       map[string]any{"hook_event": "PreCompact", "trigger": "auto"},
		EventTime:   time.Unix(1, 0).UTC(), Seq: 1,
	}
	g.Apply(o)
	n := g.Nodes[model.MarkerID("e1", "m1")]
	require.NotNil(t, n)
	assert.Equal(t, model.NodeMarker, n.Type)
	assert.Equal(t, "auto", n.Attrs["trigger"])
	assert.Contains(t, g.Edges, model.EdgeID("e1", model.EdgeParentChild, model.SessionNodeID("e1"), n.ID))
}

func TestSourceRank(t *testing.T) {
	assert.Equal(t, 3, sourceRank(model.SourceOTel))
	assert.Equal(t, 2, sourceRank(model.SourceHook))
	assert.Equal(t, 1, sourceRank(model.SourceStreamJSON))
	assert.Equal(t, 0, sourceRank(model.SourceJSONL))
}

func TestSetNameEmptyIsNoOp(t *testing.T) {
	g := NewGraph()
	o := toolObs("e1", "s1", "t1", "Bash", "running", 1)
	g.Apply(o)
	id := model.ToolCallID("e1", "t1")
	n := g.Nodes[id]
	o2 := toolObs("e1", "s1", "t1", "", "running", 2)
	o2.Attrs["name"] = ""
	g.Apply(o2)
	assert.Equal(t, "Bash", n.Name)
}

func otelTurn(exec, runID, msg string, tIn, tOut int64, ts time.Time, seq uint64) model.Observation {
	return model.Observation{
		ObsID: "o" + strconv.FormatUint(seq, 10), RunID: runID, ExecutionID: exec,
		Source: model.SourceOTel, Kind: "assistant_turn",
		Correlation: model.Correlation{SessionID: runID, MessageID: msg},
		Attrs:       map[string]any{"tokens_in": tIn, "tokens_out": tOut},
		EventTime:   ts, ObservedAt: ts, Seq: seq,
	}
}

func hookTurn(exec, runID, msg string, tIn, tOut int64, ts time.Time, seq uint64) model.Observation {
	return model.Observation{
		ObsID: "o" + strconv.FormatUint(seq, 10), RunID: runID, ExecutionID: exec,
		Source: model.SourceHook, Kind: "assistant_turn",
		Correlation: model.Correlation{SessionID: runID, MessageID: msg},
		Attrs:       map[string]any{"tokens_in": tIn, "tokens_out": tOut},
		EventTime:   ts, ObservedAt: ts, Seq: seq,
	}
}

func TestTokensOTelWinsRegardlessOfOrder(t *testing.T) {
	t0 := time.Unix(10, 0).UTC()
	h := hookTurn("e1", "s1", "m1", 1, 1, t0, 1)
	o := otelTurn("e1", "s1", "m1", 99, 88, t0, 2)
	fwd := NewGraph()
	fwd.ApplyAll([]model.Observation{h, o})
	rev := NewGraph()
	rev.ApplyAll([]model.Observation{o, h})
	id := model.AssistantTurnID("e1", "m1")
	assert.Equal(t, int64(99), *fwd.Nodes[id].TokensIn)
	assert.Equal(t, int64(99), *rev.Nodes[id].TokensIn)
	assert.Equal(t, int64(88), *rev.Nodes[id].TokensOut)

	h2 := hookTurn("e2", "s2", "m2", 3, 4, t0, 1)
	g2 := NewGraph()
	g2.Apply(h2)
	id2 := model.AssistantTurnID("e2", "m2")
	assert.Equal(t, int64(3), *g2.Nodes[id2].TokensIn)
}

func TestTokensHookKeptWhenNoOTel(t *testing.T) {
	g := NewGraph()
	g.Apply(hookTurn("e1", "s1", "m1", 7, 3, time.Unix(1, 0).UTC(), 1))
	id := model.AssistantTurnID("e1", "m1")
	assert.Equal(t, int64(7), *g.Nodes[id].TokensIn)
}

func TestTimingOTelOutranksHookEitherOrder(t *testing.T) {
	tHook := time.Unix(100, 0).UTC()
	tOTel := time.Unix(200, 0).UTC()
	h := hookTurn("e1", "s1", "m1", 0, 0, tHook, 5)
	o := otelTurn("e1", "s1", "m1", 0, 0, tOTel, 1)
	fwd := NewGraph()
	fwd.ApplyAll([]model.Observation{h, o})
	rev := NewGraph()
	rev.ApplyAll([]model.Observation{o, h})
	id := model.AssistantTurnID("e1", "m1")
	assert.Equal(t, tOTel, *fwd.Nodes[id].TStart)
	assert.Equal(t, tOTel, *rev.Nodes[id].TStart)
}

func TestTimingEqualRankKeepsEarliest(t *testing.T) {
	early := time.Unix(100, 0).UTC()
	late := time.Unix(200, 0).UTC()
	a := hookTurn("e1", "s1", "m1", 0, 0, late, 1)
	b := hookTurn("e1", "s1", "m1", 0, 0, early, 2)
	g := NewGraph()
	g.ApplyAll([]model.Observation{a, b})
	id := model.AssistantTurnID("e1", "m1")
	assert.Equal(t, early, *g.Nodes[id].TStart)
}

func TestNameLowestSeqWinsRegardlessOfOrder(t *testing.T) {
	t0 := time.Unix(1, 0).UTC()
	lo := toolObs("e1", "s1", "t1", "EarlyName", "running", 2)
	hi := toolObs("e1", "s1", "t1", "LateName", "running", 9)
	_ = t0
	fwd := NewGraph()
	fwd.ApplyAll([]model.Observation{hi, lo})
	rev := NewGraph()
	rev.ApplyAll([]model.Observation{lo, hi})
	id := model.ToolCallID("e1", "t1")
	assert.Equal(t, "EarlyName", fwd.Nodes[id].Name)
	assert.Equal(t, "EarlyName", rev.Nodes[id].Name)
}

func permute(obs []model.Observation) [][]model.Observation {
	if len(obs) <= 1 {
		return [][]model.Observation{append([]model.Observation(nil), obs...)}
	}
	var out [][]model.Observation
	for i := range obs {
		rest := append(append([]model.Observation(nil), obs[:i]...), obs[i+1:]...)
		for _, p := range permute(rest) {
			out = append(out, append([]model.Observation{obs[i]}, p...))
		}
	}
	return out
}

func TestReductionCommutativity(t *testing.T) {
	t0 := time.Unix(100, 0).UTC()
	obs := []model.Observation{
		sessionStartObs("e1", "s1", 1),
		hookTurn("e1", "s1", "m1", 5, 2, t0, 2),
		otelTurn("e1", "s1", "m1", 50, 20, t0.Add(time.Second), 3),
		toolObs("e1", "s1", "t1", "Bash", "running", 4),
		toolObs("e1", "s1", "t1", "Bash", string(model.StatusCancelled), 6),
		otelTool("e1", "s1", "t2", "spanLeaf", "spanRoot", 7),
	}
	perms := permute(obs)
	var want string
	for i, p := range perms {
		g := NewGraph()
		g.ApplyAll(p)
		got := canonGraph(g)
		if i == 0 {
			want = got
			continue
		}
		assert.Equal(t, want, got, "permutation %d diverged", i)
	}
}

func canonGraph(g *Graph) string {
	type nodeView struct {
		ID          string
		Type        model.NodeType
		Name        string
		Status      model.Status
		Rev         uint64
		TStart      string
		TIn         string
		TOut        string
		Cause       string
		PayloadHash string
	}
	var nv []nodeView
	for id, n := range g.Nodes {
		v := nodeView{ID: id, Type: n.Type, Name: n.Name, Status: n.Status, Rev: n.Rev}
		if n.TStart != nil {
			v.TStart = n.TStart.UTC().Format(time.RFC3339Nano)
		}
		if n.TokensIn != nil {
			v.TIn = strconv.FormatInt(*n.TokensIn, 10)
		}
		if n.TokensOut != nil {
			v.TOut = strconv.FormatInt(*n.TokensOut, 10)
		}
		if n.Attrs != nil {
			if c, ok := n.Attrs["cancel_cause"].(string); ok {
				v.Cause = c
			}
		}
		v.PayloadHash = n.PayloadHash
		nv = append(nv, v)
	}
	sort.Slice(nv, func(i, j int) bool { return nv[i].ID < nv[j].ID })
	type edgeView struct {
		ID  string
		Rev uint64
	}
	var ev []edgeView
	for id, e := range g.Edges {
		ev = append(ev, edgeView{ID: id, Rev: e.Rev})
	}
	sort.Slice(ev, func(i, j int) bool { return ev[i].ID < ev[j].ID })
	b, _ := json.Marshal(struct {
		Nodes []nodeView
		Edges []edgeView
	}{nv, ev})
	return string(b)
}

func deltaByKind(ds []cdc.GraphDelta, k cdc.GraphDeltaKind) []cdc.GraphDelta {
	var out []cdc.GraphDelta
	for _, d := range ds {
		if d.Kind == k {
			out = append(out, d)
		}
	}
	return out
}

func TestEmitNodeUpsertOnSessionStart(t *testing.T) {
	g := NewGraph()
	g.Apply(sessionStartObs("e1", "s1", 1))
	ds := g.DrainDeltas()
	ups := deltaByKind(ds, cdc.DeltaNodeUpsert)
	require.NotEmpty(t, ups)
	found := false
	for _, d := range ups {
		if d.Node != nil && d.Node.ID == model.SessionNodeID("e1") {
			found = true
			assert.Equal(t, uint64(1), d.Rev)
			assert.Equal(t, "e1", d.ExecutionID)
		}
	}
	assert.True(t, found)
}

func TestDrainDeltasClearsBuffer(t *testing.T) {
	g := NewGraph()
	g.Apply(sessionStartObs("e1", "s1", 1))
	first := g.DrainDeltas()
	require.NotEmpty(t, first)
	second := g.DrainDeltas()
	assert.Empty(t, second)
}

func TestEmitRunStartedOnceOnFirstObs(t *testing.T) {
	g := NewGraph()
	g.Apply(sessionStartObs("e1", "s1", 1))
	g.Apply(toolObs("e1", "s1", "t1", "Bash", "running", 2))
	ds := g.DrainDeltas()
	starts := deltaByKind(ds, cdc.DeltaRunStarted)
	require.Len(t, starts, 1)
	assert.Equal(t, "s1", starts[0].RunID)
}

func TestEmitEdgeUpsertOnNewEdge(t *testing.T) {
	g := NewGraph()
	g.Apply(toolObs("e1", "s1", "t1", "Bash", "running", 1))
	ds := g.DrainDeltas()
	edges := deltaByKind(ds, cdc.DeltaEdgeUpsert)
	require.NotEmpty(t, edges)
	assert.Equal(t, uint64(1), edges[0].Rev)
	assert.NotNil(t, edges[0].Edge)
}

func TestEmitSessionEndedOnSessionEnd(t *testing.T) {
	g := NewGraph()
	g.Apply(sessionStartObs("e1", "s1", 1))
	_ = g.DrainDeltas()
	g.Apply(sessionEndObs("e1", "s1", 2))
	ds := g.DrainDeltas()
	require.Len(t, deltaByKind(ds, cdc.DeltaSessionEnded), 1)
	assert.Equal(t, "s1", deltaByKind(ds, cdc.DeltaSessionEnded)[0].RunID)
}

func TestEmitRunEndedOnRunEnded(t *testing.T) {
	g := NewGraph()
	g.Apply(toolObs("e1", "s1", "t1", "Bash", "running", 1))
	_ = g.DrainDeltas()
	g.Apply(runEndedObs("e1", "s1", "timeout", 2))
	ds := g.DrainDeltas()
	require.Len(t, deltaByKind(ds, cdc.DeltaRunEnded), 1)
}

func TestRunEndedEarlyReturnEmitsNoRunEnded(t *testing.T) {
	g := NewGraph()
	g.ApplyAll([]model.Observation{
		sessionStartObs("e1", "s1", 1),
		sessionEndObs("e1", "s1", 2),
	})
	_ = g.DrainDeltas()
	g.Apply(runEndedObs("e1", "s1", "timeout", 3))
	ds := g.DrainDeltas()
	assert.Empty(t, deltaByKind(ds, cdc.DeltaRunEnded))
}

func TestCascadeEmitsNodeStatusWithTriggeringSeq(t *testing.T) {
	g := NewGraph()
	g.node("root", "s1", model.NodeToolCall).Status = model.StatusRunning
	g.node("child", "s1", model.NodeToolCall).Status = model.StatusRunning
	g.upsertEdge("e1", "s1", "root", "child", 1)
	_ = g.DrainDeltas()
	g.cascadeStatus("root", model.StatusCancelled, 42)
	ds := g.DrainDeltas()
	st := deltaByKind(ds, cdc.DeltaNodeStatus)
	require.Len(t, st, 1)
	assert.Equal(t, "child", st[0].Node.ID)
	assert.Equal(t, model.StatusCancelled, st[0].Node.Status)
	assert.Equal(t, uint64(42), st[0].Rev)
}

func TestCascadeUnknownCloseEmitsNodeStatus(t *testing.T) {
	g := NewGraph()
	g.node(model.SessionNodeID("e1"), "s1", model.NodeSession)
	g.node("child", "s1", model.NodeToolCall).Status = model.StatusRunning
	g.upsertEdge("e1", "s1", model.SessionNodeID("e1"), "child", 1)
	_ = g.DrainDeltas()
	g.cascadeStatus(model.SessionNodeID("e1"), model.StatusUnknown, 7)
	ds := g.DrainDeltas()
	st := deltaByKind(ds, cdc.DeltaNodeStatus)
	require.Len(t, st, 1)
	assert.Equal(t, "child", st[0].Node.ID)
	assert.Equal(t, model.StatusUnknown, st[0].Node.Status)
	assert.Equal(t, uint64(7), st[0].Rev)
}

func TestCloseIfOpenNoChangeEmitsNothing(t *testing.T) {
	g := NewGraph()
	g.node("done", "s1", model.NodeToolCall).Status = model.StatusOK
	_ = g.DrainDeltas()
	g.closeIfOpen("done", model.StatusUnknown, 5)
	ds := g.DrainDeltas()
	assert.Empty(t, deltaByKind(ds, cdc.DeltaNodeStatus))
}

func TestCascadeTerminalDescendantEmitsNoNodeStatus(t *testing.T) {
	g := NewGraph()
	g.node("root", "s1", model.NodeToolCall).Status = model.StatusRunning
	g.node("child", "s1", model.NodeToolCall).Status = model.StatusOK
	g.upsertEdge("e1", "s1", "root", "child", 1)
	_ = g.DrainDeltas()
	g.cascadeStatus("root", model.StatusCancelled, 9)
	ds := g.DrainDeltas()
	assert.Empty(t, deltaByKind(ds, cdc.DeltaNodeStatus))
}

func TestSourceRankThreeLiveTiers(t *testing.T) {
	assert.Equal(t, 3, sourceRank(model.SourceOTel))
	assert.Equal(t, 2, sourceRank(model.SourceHook))
	assert.Equal(t, 1, sourceRank(model.SourceStreamJSON))
}

func TestTimingHookBeatsStreamJSON(t *testing.T) {
	t0 := time.Unix(100, 0).UTC()
	hookEarly := hookTurn("e1", "s1", "m1", 0, 0, t0.Add(time.Hour), 1)
	sjLate := model.Observation{
		RunID: "s1", ExecutionID: "e1", Source: model.SourceStreamJSON,
		Kind: "assistant_turn", Correlation: model.Correlation{SessionID: "s1", MessageID: "m1"},
		Attrs: map[string]any{}, EventTime: t0, ObservedAt: t0, Seq: 2,
	}
	fwd := NewGraph()
	fwd.ApplyAll([]model.Observation{sjLate, hookEarly})
	rev := NewGraph()
	rev.ApplyAll([]model.Observation{hookEarly, sjLate})
	id := model.AssistantTurnID("e1", "m1")
	require.NotNil(t, fwd.Nodes[id].TStart)
	assert.Equal(t, t0.Add(time.Hour), fwd.Nodes[id].TStart.UTC())
	assert.Equal(t, fwd.Nodes[id].TStart.UTC(), rev.Nodes[id].TStart.UTC())
}

func sjTurn(execID, runID, msgID string, tin, tout int64, seq uint64) model.Observation {
	return model.Observation{
		RunID: runID, ExecutionID: execID, Source: model.SourceStreamJSON,
		Kind: "assistant_turn", Correlation: model.Correlation{SessionID: runID, MessageID: msgID},
		Attrs:     map[string]any{"tokens_in": tin, "tokens_out": tout},
		EventTime: time.Unix(100, 0).UTC(), ObservedAt: time.Unix(100, 0).UTC(), Seq: seq,
	}
}

func TestTokenRankOTelBeatsStreamJSON(t *testing.T) {
	otel := otelTurn("e1", "s1", "m1", 50, 20, time.Unix(100, 0).UTC(), 1)
	sj := sjTurn("e1", "s1", "m1", 7, 9, 2)
	fwd := NewGraph()
	fwd.ApplyAll([]model.Observation{otel, sj})
	rev := NewGraph()
	rev.ApplyAll([]model.Observation{sj, otel})
	id := model.AssistantTurnID("e1", "m1")
	require.NotNil(t, fwd.Nodes[id].TokensIn)
	assert.Equal(t, int64(50), *fwd.Nodes[id].TokensIn)
	assert.Equal(t, int64(50), *rev.Nodes[id].TokensIn)
}

func TestTokenStreamJSONSetsWhenNoOTel(t *testing.T) {
	sj := sjTurn("e1", "s1", "m1", 7, 9, 1)
	g := NewGraph()
	g.Apply(sj)
	id := model.AssistantTurnID("e1", "m1")
	require.NotNil(t, g.Nodes[id].TokensIn)
	assert.Equal(t, int64(7), *g.Nodes[id].TokensIn)
}

func TestTokenRankDefaultIsZero(t *testing.T) {
	assert.Equal(t, 0, tokenRank(model.SourceHook))
	assert.Equal(t, 0, tokenRank(model.SourceJSONL))
}

func sjToolInput(execID, runID, tuid, name, input string, seq uint64) model.Observation {
	pl := &model.Payload{Input: json.RawMessage(input)}
	pl.Hash = model.HashPayload(pl)
	return model.Observation{
		RunID: runID, ExecutionID: execID, Source: model.SourceStreamJSON,
		Kind: "assistant_tool_use", Correlation: model.Correlation{SessionID: runID, ToolUseID: tuid},
		Attrs:     map[string]any{"name": name},
		Payload:   pl,
		EventTime: time.Unix(100, 0).UTC(), ObservedAt: time.Unix(100, 0).UTC(), Seq: seq,
	}
}

func hookToolInput(execID, runID, tuid, name, input string, seq uint64) model.Observation {
	pl := &model.Payload{Input: json.RawMessage(input)}
	pl.Hash = model.HashPayload(pl)
	return model.Observation{
		RunID: runID, ExecutionID: execID, Source: model.SourceHook,
		Kind: "assistant_tool_use", Correlation: model.Correlation{SessionID: runID, ToolUseID: tuid},
		Attrs:     map[string]any{"name": name, "status": "running"},
		Payload:   pl,
		EventTime: time.Unix(100, 0).UTC(), ObservedAt: time.Unix(100, 0).UTC(), Seq: seq,
	}
}

func TestPayloadHookBeatsStreamJSONDelta(t *testing.T) {
	hookFull := hookToolInput("e1", "s1", "t1", "Bash", `{"command":"ls -la"}`, 1)
	sjDelta := sjToolInput("e1", "s1", "t1", "Bash", `{"command":"l"}`, 2)
	fwd := NewGraph()
	fwd.ApplyAll([]model.Observation{hookFull, sjDelta})
	rev := NewGraph()
	rev.ApplyAll([]model.Observation{sjDelta, hookFull})
	id := model.ToolCallID("e1", "t1")
	require.NotNil(t, fwd.Nodes[id].Payload)
	assert.JSONEq(t, `{"command":"ls -la"}`, string(fwd.Nodes[id].Payload.Input))
	assert.Equal(t, string(fwd.Nodes[id].Payload.Input), string(rev.Nodes[id].Payload.Input))
}

func TestPayloadStreamJSONSetsWhenAlone(t *testing.T) {
	g := NewGraph()
	g.Apply(sjToolInput("e1", "s1", "t1", "Bash", `{"command":"l"}`, 1))
	id := model.ToolCallID("e1", "t1")
	require.NotNil(t, g.Nodes[id].Payload)
	assert.JSONEq(t, `{"command":"l"}`, string(g.Nodes[id].Payload.Input))
}

func TestPayloadRankStreamJSONIsDefault(t *testing.T) {
	assert.Equal(t, 0, payloadRank(model.SourceStreamJSON))
	assert.Equal(t, 0, payloadRank(model.SourceOTel))
}

func TestReductionCommutativityThreeSources(t *testing.T) {
	t0 := time.Unix(200, 0).UTC()
	obs := []model.Observation{
		sessionStartObs("e2", "s2", 1),
		hookTurn("e2", "s2", "m2", 5, 2, t0.Add(time.Minute), 2),
		otelTurn("e2", "s2", "m2", 50, 20, t0.Add(time.Second), 3),
		sjTurn("e2", "s2", "m2", 7, 9, 4),
		hookToolInput("e2", "s2", "t9", "Bash", `{"command":"ls -la"}`, 5),
		sjToolInput("e2", "s2", "t9", "Bash", `{"command":"l"}`, 6),
	}
	perms := permute(obs)
	var want string
	for i, p := range perms {
		g := NewGraph()
		g.ApplyAll(p)
		got := canonGraph(g)
		if i == 0 {
			want = got
			continue
		}
		assert.Equal(t, want, got, "permutation %d diverged", i)
	}
}

func sjStreamEvent(execID, runID, childTUID, parentTUID string, seq uint64) model.Observation {
	return model.Observation{
		RunID: runID, ExecutionID: execID, Source: model.SourceStreamJSON,
		Kind: "assistant_tool_use", Correlation: model.Correlation{SessionID: runID, ToolUseID: childTUID, ParentToolUseID: parentTUID},
		EventTime: time.Unix(100, 0).UTC(), ObservedAt: time.Unix(100, 0).UTC(), Seq: seq,
	}
}

func TestParentToolUseEdgeCreated(t *testing.T) {
	parent := sjToolInput("e1", "s1", "tparent", "Task", `{}`, 1)
	child := sjStreamEvent("e1", "s1", "tchild", "tparent", 2)
	g := NewGraph()
	g.ApplyAll([]model.Observation{parent, child})
	id := model.EdgeID("e1", model.EdgeParentChild, model.ToolCallID("e1", "tparent"), model.ToolCallID("e1", "tchild"))
	require.NotNil(t, g.Edges[id])
	assert.Equal(t, model.ToolCallID("e1", "tparent"), g.Edges[id].Src)
	assert.Equal(t, model.ToolCallID("e1", "tchild"), g.Edges[id].Dst)
}

func otelChildEdge(execID, runID, childTUID, parentTUID string, seq uint64) model.Observation {
	return model.Observation{
		RunID: runID, ExecutionID: execID, Source: model.SourceOTel,
		Kind: "assistant_tool_use", Correlation: model.Correlation{SessionID: runID, ToolUseID: childTUID, ParentToolUseID: parentTUID},
		Attrs:     map[string]any{"name": "Task"},
		EventTime: time.Unix(100, 0).UTC(), ObservedAt: time.Unix(100, 0).UTC(), Seq: seq,
	}
}

func TestParentToolUseStreamJSONDoesNotOverwriteOTelEdge(t *testing.T) {
	otelEdge := otelChildEdge("e1", "s1", "tchild", "tparentA", 1)
	sjEdge := sjStreamEvent("e1", "s1", "tchild", "tparentB", 2)
	fwd := NewGraph()
	fwd.ApplyAll([]model.Observation{otelEdge, sjEdge})
	rev := NewGraph()
	rev.ApplyAll([]model.Observation{sjEdge, otelEdge})
	wantID := model.EdgeID("e1", model.EdgeParentChild, model.ToolCallID("e1", "tparentA"), model.ToolCallID("e1", "tchild"))
	loseID := model.EdgeID("e1", model.EdgeParentChild, model.ToolCallID("e1", "tparentB"), model.ToolCallID("e1", "tchild"))
	require.NotNil(t, fwd.Edges[wantID])
	require.NotNil(t, rev.Edges[wantID])
	assert.Nil(t, fwd.Edges[loseID])
	assert.Nil(t, rev.Edges[loseID])
}

func TestParentToolUseNoChildID(t *testing.T) {
	o := model.Observation{
		RunID: "s1", ExecutionID: "e1", Source: model.SourceStreamJSON,
		Kind: "assistant_tool_use", Correlation: model.Correlation{SessionID: "s1", ParentToolUseID: "tparent"},
		EventTime: time.Unix(100, 0).UTC(), ObservedAt: time.Unix(100, 0).UTC(), Seq: 1,
	}
	g := NewGraph()
	g.Apply(o)
	loseID := model.EdgeID("e1", model.EdgeParentChild, model.ToolCallID("e1", "tparent"), model.ToolCallID("e1", ""))
	assert.Nil(t, g.Edges[loseID])
}

func TestStructureRankValues(t *testing.T) {
	assert.Equal(t, 2, structureRank(model.SourceOTel))
	assert.Equal(t, 1, structureRank(model.SourceStreamJSON))
	assert.Equal(t, 0, structureRank(model.SourceHook))
}

func TestReductionCommutativityWithParentToolEdge(t *testing.T) {
	obs := []model.Observation{
		sessionStartObs("e3", "s3", 1),
		sjToolInput("e3", "s3", "tp", "Task", `{}`, 2),
		sjStreamEvent("e3", "s3", "tc", "tp", 3),
		otelChildEdge("e3", "s3", "tc", "tp", 4),
	}
	perms := permute(obs)
	var want string
	for i, p := range perms {
		g := NewGraph()
		g.ApplyAll(p)
		got := canonGraph(g)
		if i == 0 {
			want = got
			continue
		}
		assert.Equal(t, want, got, "permutation %d diverged", i)
	}
}
