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
	"github.com/realkarych/catacomb/ingest/streamjson"
	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/pricing"
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

func TestAssistantTurnModelAttr(t *testing.T) {
	t0 := time.Unix(0, 0).UTC()
	turn := ob("assistant_turn", "", t0)
	turn.Correlation.MessageID = "msg_2"
	turn.Attrs = map[string]any{"model": "claude-opus-4-8", "tokens_in": int64(10), "tokens_out": int64(5)}

	g := NewGraph()
	g.Apply(turn)

	turnNode := g.Nodes[model.AssistantTurnID(execID, "msg_2")]
	require.NotNil(t, turnNode)
	assert.Equal(t, "claude-opus-4-8", turnNode.Attrs["model"])
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

func TestToFloat64(t *testing.T) {
	cases := []struct {
		in any
		ok bool
		f  float64
	}{
		{float64(1.5), true, 1.5},
		{float32(2.5), true, float64(float32(2.5))},
		{int64(3), true, 3.0},
		{int(4), true, 4.0},
		{"x", false, 0},
	}
	for _, c := range cases {
		f, ok := toFloat64(c.in)
		assert.Equal(t, c.ok, ok)
		assert.InDelta(t, c.f, f, 1e-6)
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

func TestEmptyToolUseIDMarkerDoesNotDropLaterTool(t *testing.T) {
	t0 := time.Unix(0, 0).UTC()
	mark := model.Observation{
		ObsID:       "mark_obs",
		RunID:       runID,
		ExecutionID: execID,
		Source:      model.SourceOTel,
		Kind:        "assistant_tool_use",
		Correlation: model.Correlation{SessionID: runID},
		Attrs:       map[string]any{"name": "mcp__catacomb__mark"},
		Payload:     &model.Payload{Input: []byte(`{"name":"phase1","boundary":"start"}`)},
		EventTime:   t0,
		ObservedAt:  t0,
		Seq:         1,
	}
	tool := model.Observation{
		ObsID:       "tool_obs",
		RunID:       runID,
		ExecutionID: execID,
		Source:      model.SourceOTel,
		Kind:        "assistant_tool_use",
		Correlation: model.Correlation{SessionID: runID},
		Attrs:       map[string]any{"name": "Bash"},
		EventTime:   t0.Add(time.Second),
		ObservedAt:  t0.Add(time.Second),
		Seq:         2,
	}

	g := NewGraph()
	g.ApplyAll([]model.Observation{mark, tool})

	id := model.ToolCallID(execID, nodeKey("", "", "tool_obs"))
	assert.NotNil(t, g.Nodes[id], "ordinary tool with empty ToolUseID must not be dropped by empty-key marker poison")
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

func TestUserPromptAttrsPromptKindSystem(t *testing.T) {
	o := ob("user_prompt", "", time.Unix(0, 0).UTC())
	o.Correlation.UUID = "u2"
	o.Attrs = map[string]any{"prompt_kind": "system"}
	g := NewGraph()
	g.Apply(o)
	n := g.Nodes[model.UserPromptID(execID, "u2")]
	require.NotNil(t, n)
	assert.Equal(t, "system", n.Attrs["prompt_kind"])
}

func TestUserPromptAttrsPromptKindHuman(t *testing.T) {
	o := ob("user_prompt", "", time.Unix(0, 0).UTC())
	o.Correlation.UUID = "u3"
	o.Attrs = map[string]any{"prompt_kind": "human"}
	g := NewGraph()
	g.Apply(o)
	n := g.Nodes[model.UserPromptID(execID, "u3")]
	require.NotNil(t, n)
	assert.Equal(t, "human", n.Attrs["prompt_kind"])
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

func TestResolveStatusTerminalPrecedence(t *testing.T) {
	cases := []struct {
		name string
		cur  model.Status
		next model.Status
		want model.Status
	}{
		{"error then ok stays error", model.StatusError, model.StatusOK, model.StatusError},
		{"ok then error becomes error", model.StatusOK, model.StatusError, model.StatusError},
		{"running then ok becomes ok", model.StatusRunning, model.StatusOK, model.StatusOK},
		{"blocked then ok stays blocked", model.StatusBlocked, model.StatusOK, model.StatusBlocked},
		{"ok then blocked becomes blocked", model.StatusOK, model.StatusBlocked, model.StatusBlocked},
		{"blocked then error becomes error", model.StatusBlocked, model.StatusError, model.StatusError},
		{"error then blocked stays error", model.StatusError, model.StatusBlocked, model.StatusError},
		{"ok then ok stays ok", model.StatusOK, model.StatusOK, model.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, resolveStatus(tc.cur, tc.next))
		})
	}
}

func TestToolStatusErrorNotMaskedByLaterOK(t *testing.T) {
	t0 := time.Unix(0, 0).UTC()
	errRes := ob("tool_result", "toolu_err", t0)
	errRes.Attrs = map[string]any{"status": string(model.StatusError)}
	okHook := ob("tool_result", "toolu_err", t0.Add(time.Second))
	okHook.Attrs = map[string]any{"status": string(model.StatusOK)}

	g := NewGraph()
	g.ApplyAll([]model.Observation{errRes, okHook})

	assert.Equal(t, model.StatusError, g.Nodes[model.ToolCallID(execID, "toolu_err")].Status)
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
	tool := model.ToolCallID("e1", "span:spanMid")
	assert.Contains(t, g.Edges, model.EdgeID("e1", model.EdgeParentChild, model.SessionNodeID("e1"), tool))
}

func TestGateNeverAppliesToHookEdges(t *testing.T) {
	g := NewGraph()
	g.Apply(toolObs("e1", "s1", "", "Bash", "running", 1))
	tool := model.ToolCallID("e1", "obs:o1")
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
	assert.Equal(t, 1, sourceRank(model.SourceJSONL))
	assert.Equal(t, 0, sourceRank(model.SourceStreamJSON))
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

func TestSourceRankFourLiveTiers(t *testing.T) {
	assert.Equal(t, 3, sourceRank(model.SourceOTel))
	assert.Equal(t, 2, sourceRank(model.SourceHook))
	assert.Equal(t, 1, sourceRank(model.SourceJSONL))
	assert.Equal(t, 0, sourceRank(model.SourceStreamJSON))
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

func TestPayloadRankJSONLIsFull(t *testing.T) {
	assert.Equal(t, 1, payloadRank(model.SourceHook))
	assert.Equal(t, 1, payloadRank(model.SourceJSONL))
	assert.Equal(t, 0, payloadRank(model.SourceStreamJSON))
	assert.Equal(t, 0, payloadRank(model.SourceOTel))
}

func TestTokenRankJSONLIsLowest(t *testing.T) {
	assert.Equal(t, 2, tokenRank(model.SourceOTel))
	assert.Equal(t, 1, tokenRank(model.SourceStreamJSON))
	assert.Equal(t, 0, tokenRank(model.SourceJSONL))
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
	assert.Equal(t, 3, structureRank(model.SourceJSONL))
	assert.Equal(t, 2, structureRank(model.SourceOTel))
	assert.Equal(t, 1, structureRank(model.SourceStreamJSON))
	assert.Equal(t, 0, structureRank(model.SourceHook))
}

func jsonlToolInput(execID, runID, tuid, name, input string, seq uint64) model.Observation {
	pl := &model.Payload{Input: json.RawMessage(input)}
	pl.Hash = model.HashPayload(pl)
	return model.Observation{
		ObsID: "o" + strconv.FormatUint(seq, 10), RunID: runID, ExecutionID: execID,
		Source: model.SourceJSONL, Kind: "assistant_tool_use",
		Correlation: model.Correlation{SessionID: runID, ToolUseID: tuid},
		Attrs:       map[string]any{"name": name},
		Payload:     pl,
		EventTime:   time.Unix(int64(seq), 0).UTC(), ObservedAt: time.Unix(int64(seq), 0).UTC(), Seq: seq,
	}
}

func jsonlChildEdge(execID, runID, childTUID, parentTUID string, seq uint64) model.Observation {
	return model.Observation{
		ObsID: "o" + strconv.FormatUint(seq, 10), RunID: runID, ExecutionID: execID,
		Source: model.SourceJSONL, Kind: "assistant_tool_use",
		Correlation: model.Correlation{SessionID: runID, ToolUseID: childTUID, ParentToolUseID: parentTUID},
		Attrs:       map[string]any{"name": "Task"},
		EventTime:   time.Unix(int64(seq), 0).UTC(), ObservedAt: time.Unix(int64(seq), 0).UTC(), Seq: seq,
	}
}

func jsonlTurn(execID, runID, msgID string, tin, tout int64, seq uint64) model.Observation {
	return model.Observation{
		ObsID: "o" + strconv.FormatUint(seq, 10), RunID: runID, ExecutionID: execID,
		Source: model.SourceJSONL, Kind: "assistant_turn",
		Correlation: model.Correlation{SessionID: runID, MessageID: msgID},
		Attrs:       map[string]any{"tokens_in": tin, "tokens_out": tout},
		EventTime:   time.Unix(int64(seq), 0).UTC(), ObservedAt: time.Unix(int64(seq), 0).UTC(), Seq: seq,
	}
}

func TestJSONLStructureOutranksOTelEdgeBothOrders(t *testing.T) {
	otelEdge := otelChildEdge("e1", "s1", "tchild2", "tparentOTEL", 1)
	jsonlEdge := jsonlChildEdge("e1", "s1", "tchild2", "tparentJSONL", 2)
	fwd := NewGraph()
	fwd.ApplyAll([]model.Observation{otelEdge, jsonlEdge})
	rev := NewGraph()
	rev.ApplyAll([]model.Observation{jsonlEdge, otelEdge})
	wantID := model.EdgeID("e1", model.EdgeParentChild, model.ToolCallID("e1", "tparentJSONL"), model.ToolCallID("e1", "tchild2"))
	loseID := model.EdgeID("e1", model.EdgeParentChild, model.ToolCallID("e1", "tparentOTEL"), model.ToolCallID("e1", "tchild2"))
	require.NotNil(t, fwd.Edges[wantID])
	require.NotNil(t, rev.Edges[wantID])
	assert.Nil(t, fwd.Edges[loseID])
	assert.Nil(t, rev.Edges[loseID])
}

func TestJSONLStructureOutranksOTelEdge(t *testing.T) {
	g := NewGraph()
	otel := model.Observation{
		ObsID: "o1", RunID: "s1", ExecutionID: "e1", Source: model.SourceOTel,
		Kind: "assistant_tool_use", Seq: 1, EventTime: time.Unix(1, 0).UTC(),
		Attrs:       map[string]any{"name": "Task"},
		Correlation: model.Correlation{ToolUseID: "child", ParentToolUseID: "pOTEL"},
	}
	g.Apply(otel)
	require.Contains(t, g.Edges, model.EdgeID("e1", model.EdgeParentChild,
		model.ToolCallID("e1", "pOTEL"), model.ToolCallID("e1", "child")))

	jsonl := model.Observation{
		ObsID: "o2", RunID: "s1", ExecutionID: "e1", Source: model.SourceJSONL,
		Kind: "assistant_tool_use", Seq: 2, EventTime: time.Unix(2, 0).UTC(),
		Attrs:       map[string]any{"name": "Task"},
		Correlation: model.Correlation{ToolUseID: "child", ParentToolUseID: "pJSONL"},
	}
	g.Apply(jsonl)

	assert.NotContains(t, g.Edges, model.EdgeID("e1", model.EdgeParentChild,
		model.ToolCallID("e1", "pOTEL"), model.ToolCallID("e1", "child")))
	assert.Contains(t, g.Edges, model.EdgeID("e1", model.EdgeParentChild,
		model.ToolCallID("e1", "pJSONL"), model.ToolCallID("e1", "child")))
}

func TestReparentEmitsEdgeDelete(t *testing.T) {
	g := NewGraph()
	g.Apply(model.Observation{
		ObsID: "o1", RunID: "s1", ExecutionID: "e1", Source: model.SourceOTel,
		Kind: "assistant_tool_use", Seq: 1, EventTime: time.Unix(1, 0).UTC(),
		Attrs:       map[string]any{"name": "Task"},
		Correlation: model.Correlation{ToolUseID: "child", ParentToolUseID: "pOTEL"},
	})
	_ = g.DrainDeltas()
	g.Apply(model.Observation{
		ObsID: "o2", RunID: "s1", ExecutionID: "e1", Source: model.SourceJSONL,
		Kind: "assistant_tool_use", Seq: 2, EventTime: time.Unix(2, 0).UTC(),
		Attrs:       map[string]any{"name": "Task"},
		Correlation: model.Correlation{ToolUseID: "child", ParentToolUseID: "pJSONL"},
	})
	oldID := model.EdgeID("e1", model.EdgeParentChild, model.ToolCallID("e1", "pOTEL"), model.ToolCallID("e1", "child"))
	deletedOld := false
	for _, d := range g.DrainDeltas() {
		if d.Kind == cdc.DeltaEdgeDelete && d.Edge != nil && d.Edge.ID == oldID {
			deletedOld = true
		}
	}
	assert.True(t, deletedOld, "re-parent must emit DeltaEdgeDelete for the superseded edge")
}

func hookToolNoMessage(seq uint64) model.Observation {
	return model.Observation{
		ObsID: "o" + strconv.FormatUint(seq, 10), RunID: "s1", ExecutionID: "e1",
		Source: model.SourceHook, Kind: "assistant_tool_use",
		Correlation: model.Correlation{SessionID: "s1", ToolUseID: "t1"},
		Attrs:       map[string]any{"name": "Bash", "status": "running"},
		EventTime:   time.Unix(int64(seq), 0).UTC(), ObservedAt: time.Unix(int64(seq), 0).UTC(), Seq: seq,
	}
}

func jsonlToolTurn(seq uint64) model.Observation {
	return model.Observation{
		ObsID: "o" + strconv.FormatUint(seq, 10), RunID: "s1", ExecutionID: "e1",
		Source: model.SourceJSONL, Kind: "assistant_tool_use",
		Correlation: model.Correlation{SessionID: "s1", ToolUseID: "t1", MessageID: "m1"},
		Attrs:       map[string]any{"name": "Bash"},
		EventTime:   time.Unix(int64(seq), 0).UTC(), ObservedAt: time.Unix(int64(seq), 0).UTC(), Seq: seq,
	}
}

func TestHookSessionReparentedToTurnByJSONL(t *testing.T) {
	g := NewGraph()
	g.Apply(hookToolNoMessage(1))
	_ = g.DrainDeltas()
	g.Apply(jsonlToolTurn(2))

	tool := model.ToolCallID("e1", "t1")
	turnEdge := model.EdgeID("e1", model.EdgeParentChild, model.AssistantTurnID("e1", "m1"), tool)
	sessionEdge := model.EdgeID("e1", model.EdgeParentChild, model.SessionNodeID("e1"), tool)
	require.Contains(t, g.Edges, turnEdge)
	assert.NotContains(t, g.Edges, sessionEdge)

	deletedSession := false
	for _, d := range g.DrainDeltas() {
		if d.Kind == cdc.DeltaEdgeDelete && d.Edge != nil && d.Edge.ID == sessionEdge {
			deletedSession = true
		}
	}
	assert.True(t, deletedSession, "session edge must be deleted when tool is reparented to its turn")
}

func TestStructReparentDeleteRevNotBelowDeletedEdgeRev(t *testing.T) {
	g := NewGraph()
	g.Apply(hookToolNoMessage(5))
	_ = g.DrainDeltas()

	tool := model.ToolCallID("e1", "t1")
	sessionEdge := model.EdgeID("e1", model.EdgeParentChild, model.SessionNodeID("e1"), tool)
	deletedRev := g.Edges[sessionEdge].Rev

	g.Apply(jsonlToolTurn(2))

	var del *cdc.GraphDelta
	for _, d := range g.DrainDeltas() {
		if d.Kind == cdc.DeltaEdgeDelete && d.Edge != nil && d.Edge.ID == sessionEdge {
			dd := d
			del = &dd
		}
	}
	require.NotNil(t, del, "reparent must emit DeltaEdgeDelete for the superseded session edge")
	assert.GreaterOrEqual(t, del.Rev, deletedRev, "delete rev must not be below the deleted edge's last upsert rev")
	assert.GreaterOrEqual(t, del.Rev, uint64(2), "delete rev must be at least the establishing observation seq")
}

func TestJSONLTurnNotReplacedByHookSessionFallback(t *testing.T) {
	g := NewGraph()
	g.Apply(jsonlToolTurn(1))
	g.Apply(hookToolNoMessage(2))

	tool := model.ToolCallID("e1", "t1")
	turnEdge := model.EdgeID("e1", model.EdgeParentChild, model.AssistantTurnID("e1", "m1"), tool)
	sessionEdge := model.EdgeID("e1", model.EdgeParentChild, model.SessionNodeID("e1"), tool)
	require.Contains(t, g.Edges, turnEdge)
	assert.NotContains(t, g.Edges, sessionEdge)
}

func TestParentToolUseIDIsSoleStructuralParent(t *testing.T) {
	g := NewGraph()
	o := model.Observation{
		ObsID: "o1", RunID: "s1", ExecutionID: "e1", Source: model.SourceJSONL,
		Kind: "assistant_tool_use", Seq: 1, EventTime: time.Unix(1, 0).UTC(),
		Attrs:       map[string]any{"name": "Bash"},
		Correlation: model.Correlation{SessionID: "s1", ToolUseID: "tc", ParentToolUseID: "tp", MessageID: "m1"},
	}
	g.Apply(o)

	tool := model.ToolCallID("e1", "tc")
	parentToolEdge := model.EdgeID("e1", model.EdgeParentChild, model.ToolCallID("e1", "tp"), tool)
	turnEdge := model.EdgeID("e1", model.EdgeParentChild, model.AssistantTurnID("e1", "m1"), tool)
	sessionEdge := model.EdgeID("e1", model.EdgeParentChild, model.SessionNodeID("e1"), tool)
	require.Contains(t, g.Edges, parentToolEdge)
	assert.NotContains(t, g.Edges, turnEdge)
	assert.NotContains(t, g.Edges, sessionEdge)
}

func TestPlainToolTurnParentOnly(t *testing.T) {
	g := NewGraph()
	g.Apply(jsonlToolTurn(1))

	tool := model.ToolCallID("e1", "t1")
	turnEdge := model.EdgeID("e1", model.EdgeParentChild, model.AssistantTurnID("e1", "m1"), tool)
	sessionEdge := model.EdgeID("e1", model.EdgeParentChild, model.SessionNodeID("e1"), tool)
	require.Contains(t, g.Edges, turnEdge)
	assert.NotContains(t, g.Edges, sessionEdge)
}

func TestHookOnlyToolKeepsSessionParent(t *testing.T) {
	g := NewGraph()
	g.Apply(hookToolNoMessage(1))
	g.Apply(hookToolNoMessage(2))

	tool := model.ToolCallID("e1", "t1")
	sessionEdge := model.EdgeID("e1", model.EdgeParentChild, model.SessionNodeID("e1"), tool)
	require.Contains(t, g.Edges, sessionEdge)
}

func TestJSONLPayloadBeatsStreamJSON(t *testing.T) {
	jsonlFull := jsonlToolInput("e1", "s1", "t1", "Bash", `{"command":"ls -la"}`, 1)
	sjDelta := sjToolInput("e1", "s1", "t1", "Bash", `{"command":"l"}`, 2)
	fwd := NewGraph()
	fwd.ApplyAll([]model.Observation{jsonlFull, sjDelta})
	rev := NewGraph()
	rev.ApplyAll([]model.Observation{sjDelta, jsonlFull})
	id := model.ToolCallID("e1", "t1")
	require.NotNil(t, fwd.Nodes[id].Payload)
	assert.JSONEq(t, `{"command":"ls -la"}`, string(fwd.Nodes[id].Payload.Input))
	assert.Equal(t, string(fwd.Nodes[id].Payload.Input), string(rev.Nodes[id].Payload.Input))
}

func TestStreamJSONTokensOutrankJSONLTokens(t *testing.T) {
	g := NewGraph()
	mkTurn := func(src model.Source, in, out int64, seq uint64) model.Observation {
		return model.Observation{
			ObsID: "o" + strconv.FormatUint(seq, 10), RunID: "s1", ExecutionID: "e1",
			Source: src, Kind: "assistant_turn", Seq: seq, EventTime: time.Unix(int64(seq), 0).UTC(),
			Correlation: model.Correlation{MessageID: "m1"},
			Attrs:       map[string]any{"tokens_in": in, "tokens_out": out},
		}
	}
	g.Apply(mkTurn(model.SourceStreamJSON, 11, 22, 1))
	g.Apply(mkTurn(model.SourceJSONL, 99, 99, 2))
	n := g.Nodes[model.AssistantTurnID("e1", "m1")]
	require.NotNil(t, n)
	assert.Equal(t, int64(11), *n.TokensIn)
	assert.Equal(t, int64(22), *n.TokensOut)
}

func TestJSONLTimingBeatsStreamJSON(t *testing.T) {
	tJSONL := time.Unix(200, 0).UTC()
	tSJ := time.Unix(100, 0).UTC()
	jsonlObs := jsonlTurn("e1", "s1", "m1", 0, 0, 1)
	jsonlObs.EventTime = tJSONL
	sjObs := sjTurn("e1", "s1", "m1", 0, 0, 2)
	sjObs.EventTime = tSJ
	fwd := NewGraph()
	fwd.ApplyAll([]model.Observation{sjObs, jsonlObs})
	rev := NewGraph()
	rev.ApplyAll([]model.Observation{jsonlObs, sjObs})
	id := model.AssistantTurnID("e1", "m1")
	require.NotNil(t, fwd.Nodes[id].TStart)
	assert.Equal(t, tJSONL, *fwd.Nodes[id].TStart)
	assert.Equal(t, tJSONL, *rev.Nodes[id].TStart)
}

func TestJSONLTimingLosesToHook(t *testing.T) {
	tHook := time.Unix(300, 0).UTC()
	tJSONL := time.Unix(100, 0).UTC()
	hookObs := hookTurn("e1", "s1", "m1", 0, 0, tHook, 1)
	jsonlObs := jsonlTurn("e1", "s1", "m1", 0, 0, 2)
	jsonlObs.EventTime = tJSONL
	fwd := NewGraph()
	fwd.ApplyAll([]model.Observation{hookObs, jsonlObs})
	rev := NewGraph()
	rev.ApplyAll([]model.Observation{jsonlObs, hookObs})
	id := model.AssistantTurnID("e1", "m1")
	require.NotNil(t, fwd.Nodes[id].TStart)
	assert.Equal(t, tHook, *fwd.Nodes[id].TStart)
	assert.Equal(t, tHook, *rev.Nodes[id].TStart)
}

func TestReductionCommutativityWithJSONL(t *testing.T) {
	base := []model.Observation{
		otelTool("e4", "s4", "tu1", "sp1", "", 1),
		toolObs("e4", "s4", "tu1", "Bash", "ok", 2),
		{
			ObsID: "o3", RunID: "s4", ExecutionID: "e4", Source: model.SourceJSONL,
			Kind: "assistant_tool_use", Seq: 3, EventTime: time.Unix(3, 0).UTC(),
			Correlation: model.Correlation{ToolUseID: "tu1", ParentToolUseID: "tu0"},
			Attrs:       map[string]any{"name": "Bash"},
			Payload:     &model.Payload{Input: json.RawMessage(`{"command":"ls"}`)},
		},
		{
			ObsID: "o4", RunID: "s4", ExecutionID: "e4", Source: model.SourceStreamJSON,
			Kind: "assistant_turn", Seq: 4, EventTime: time.Unix(4, 0).UTC(),
			Correlation: model.Correlation{MessageID: "m1"},
			Attrs:       map[string]any{"tokens_in": int64(5), "tokens_out": int64(6)},
		},
	}
	g0 := NewGraph()
	g0.ApplyAll(base)
	want := canonGraph(g0)
	perms := permute(base)
	for i, p := range perms {
		g := NewGraph()
		g.ApplyAll(p)
		got := canonGraph(g)
		assert.Equal(t, want, got, "permutation %d diverged", i)
	}
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

func seq() func() uint64 {
	var n uint64
	return func() uint64 {
		n++
		return n
	}
}

func TestParentChildEdgeReachableFromRealAssistantEnvelope(t *testing.T) {
	parentLine := []byte(`{"type":"assistant","session_id":"s1","message":{"id":"msg_parent","content":[{"type":"tool_use","id":"toolu_parent","name":"Task","input":{}}]}}`)
	childLine := []byte(`{"type":"assistant","session_id":"s1","parent_tool_use_id":"toolu_parent","message":{"id":"msg_child","content":[{"type":"tool_use","id":"toolu_child","name":"Bash","input":{"command":"ls"}}]}}`)

	sq := seq()
	parentObs, _, err := streamjson.Parse(parentLine, "exec_i1", sq)
	require.NoError(t, err)
	childObs, _, err := streamjson.Parse(childLine, "exec_i1", sq)
	require.NoError(t, err)

	g := NewGraph()
	g.ApplyAll(parentObs)
	g.ApplyAll(childObs)

	childToolObs := childObs[1]
	assert.Equal(t, "toolu_parent", childToolObs.Correlation.ParentToolUseID)
	assert.Equal(t, "toolu_child", childToolObs.Correlation.ToolUseID)

	edgeID := model.EdgeID("exec_i1", model.EdgeParentChild, model.ToolCallID("exec_i1", "toolu_parent"), model.ToolCallID("exec_i1", "toolu_child"))
	require.NotNil(t, g.Edges[edgeID], "parent_child edge must be created from real assistant envelope")
	assert.Equal(t, model.ToolCallID("exec_i1", "toolu_parent"), g.Edges[edgeID].Src)
	assert.Equal(t, model.ToolCallID("exec_i1", "toolu_child"), g.Edges[edgeID].Dst)
}

func TestStreamEventCreatesNoJunkEmptyToolNode(t *testing.T) {
	line := []byte(`{"type":"stream_event","session_id":"s1","parent_tool_use_id":"toolu_parent","uuid":"u1"}`)

	sq := seq()
	obs, _, err := streamjson.Parse(line, "exec_i2", sq)
	require.NoError(t, err)
	require.Empty(t, obs, "stream_event must yield zero observations")

	g := NewGraph()
	g.ApplyAll(obs)
	junkID := model.ToolCallID("exec_i2", "")
	assert.NotContains(t, g.Nodes, junkID, "no junk empty-tool node must exist after stream_event")
}

func TestToolCallStampsEndAndDuration(t *testing.T) {
	t0 := time.Date(2026, 6, 20, 10, 0, 1, 0, time.UTC)
	t1 := t0.Add(2 * time.Second)
	use := ob("assistant_tool_use", "toolu_d1", t0)
	use.Attrs = map[string]any{"name": "Bash"}
	res := ob("tool_result", "toolu_d1", t1)
	res.Attrs = map[string]any{"status": string(model.StatusOK)}

	g := NewGraph()
	g.ApplyAll([]model.Observation{use, res})

	n := g.Nodes[model.ToolCallID(execID, "toolu_d1")]
	require.NotNil(t, n.TStart)
	require.NotNil(t, n.TEnd)
	require.NotNil(t, n.DurationMS)
	assert.Equal(t, t1, *n.TEnd)
	assert.Equal(t, int64(2000), *n.DurationMS)
}

func TestDurationStampOrderIndependent(t *testing.T) {
	t0 := time.Date(2026, 6, 20, 10, 0, 1, 0, time.UTC)
	t1 := t0.Add(3 * time.Second)
	use := ob("assistant_tool_use", "toolu_d2", t0)
	use.Attrs = map[string]any{"name": "Bash"}
	res := ob("tool_result", "toolu_d2", t1)
	res.Attrs = map[string]any{"status": string(model.StatusOK)}

	fwd := NewGraph()
	fwd.ApplyAll([]model.Observation{use, res})
	rev := NewGraph()
	rev.ApplyAll([]model.Observation{res, use})

	id := model.ToolCallID(execID, "toolu_d2")
	assert.Equal(t, *fwd.Nodes[id].TEnd, *rev.Nodes[id].TEnd)
	assert.Equal(t, *fwd.Nodes[id].DurationMS, *rev.Nodes[id].DurationMS)
}

func TestAssistantTurnStampsDurationWhenStartAndEndKnown(t *testing.T) {
	t0 := time.Date(2026, 6, 20, 10, 0, 1, 0, time.UTC)
	t1 := t0.Add(time.Second)
	first := ob("assistant_turn", "", t0)
	first.Correlation.MessageID = "m1"
	first.Attrs = map[string]any{"tokens_in": int64(1)}
	last := ob("assistant_turn", "", t1)
	last.Correlation.MessageID = "m1"
	last.Attrs = map[string]any{"tokens_in": int64(1)}

	g := NewGraph()
	g.ApplyAll([]model.Observation{first, last})

	n := g.Nodes[model.AssistantTurnID(execID, "m1")]
	require.NotNil(t, n.TEnd)
	require.NotNil(t, n.DurationMS)
	assert.Equal(t, int64(1000), *n.DurationMS)
}

func TestEndRankHigherSourceWins(t *testing.T) {
	t0 := time.Date(2026, 6, 20, 10, 0, 1, 0, time.UTC)
	jsonlEnd := t0.Add(time.Second)
	otelEnd := t0.Add(5 * time.Second)

	use := ob("assistant_tool_use", "toolu_d3", t0)
	use.Attrs = map[string]any{"name": "Bash"}

	resJSONL := ob("tool_result", "toolu_d3", jsonlEnd)
	resJSONL.Source = model.SourceJSONL
	resJSONL.Attrs = map[string]any{"status": string(model.StatusOK)}

	resOTel := ob("tool_result", "toolu_d3", otelEnd)
	resOTel.Source = model.SourceOTel
	resOTel.Attrs = map[string]any{"status": string(model.StatusOK)}

	g := NewGraph()
	g.ApplyAll([]model.Observation{use, resJSONL, resOTel})
	rg := NewGraph()
	rg.ApplyAll([]model.Observation{resOTel, resJSONL, use})

	id := model.ToolCallID(execID, "toolu_d3")
	assert.Equal(t, otelEnd, *g.Nodes[id].TEnd)
	assert.Equal(t, otelEnd, *rg.Nodes[id].TEnd)
}

func TestSessionEndGetsDurationMS(t *testing.T) {
	t0 := time.Unix(0, 0).UTC()
	t1 := t0.Add(5 * time.Second)
	g := NewGraph()
	g.Apply(ob("session_start", "", t0))
	g.Apply(ob("session_end", "", t1))
	n := g.Nodes[model.SessionNodeID(execID)]
	require.NotNil(t, n.TEnd)
	require.NotNil(t, n.DurationMS)
	assert.Equal(t, int64(5000), *n.DurationMS)
}

func TestSubagentStopGetsDurationMS(t *testing.T) {
	t0 := time.Unix(0, 0).UTC()
	t1 := t0.Add(3 * time.Second)
	first := ob("subagent_stop", "", t0)
	first.Correlation.AgentID = "a2"
	first.EventTime = t0
	second := ob("subagent_stop", "", t1)
	second.Correlation.AgentID = "a2"
	second.EventTime = t1
	g := NewGraph()
	g.ApplyAll([]model.Observation{first, second})
	n := g.Nodes[model.SubagentID(execID, "a2")]
	require.NotNil(t, n.TEnd)
	require.NotNil(t, n.DurationMS)
	assert.Equal(t, int64(3000), *n.DurationMS)
}

func TestEndRankEqualRankLatestTimeWins(t *testing.T) {
	t0 := time.Date(2026, 6, 20, 10, 0, 1, 0, time.UTC)
	earlier := t0.Add(time.Second)
	later := t0.Add(2 * time.Second)

	use := ob("assistant_tool_use", "toolu_d4", t0)
	use.Attrs = map[string]any{"name": "Bash"}

	resEarly := ob("tool_result", "toolu_d4", earlier)
	resEarly.Source = model.SourceJSONL
	resEarly.Attrs = map[string]any{"status": string(model.StatusOK)}

	resLate := ob("tool_result", "toolu_d4", later)
	resLate.Source = model.SourceJSONL
	resLate.Attrs = map[string]any{"status": string(model.StatusOK)}

	g := NewGraph()
	g.ApplyAll([]model.Observation{use, resEarly, resLate})
	rg := NewGraph()
	rg.ApplyAll([]model.Observation{resLate, resEarly, use})

	id := model.ToolCallID(execID, "toolu_d4")
	assert.Equal(t, later, *g.Nodes[id].TEnd)
	assert.Equal(t, later, *rg.Nodes[id].TEnd)
}

func TestAssistantTurnCostReportedProvenance(t *testing.T) {
	p := PricerFunc(func(in PriceInputs) (PriceResult, bool) {
		if in.ReportedUSD != nil {
			return PriceResult{USD: *in.ReportedUSD, Source: "reported"}, true
		}
		return PriceResult{USD: float64(in.TokensIn+in.TokensOut) / 1000, Source: "estimated"}, true
	})
	o := ob("assistant_turn", "", time.Unix(0, 0).UTC())
	o.Correlation.MessageID = "mc1"
	o.Attrs = map[string]any{"model": "model-x", "tokens_in": int64(10), "tokens_out": int64(5), "cost_usd": float64(0.25)}

	g := NewGraphWithPricer(p)
	g.Apply(o)

	n := g.Nodes[model.AssistantTurnID(execID, "mc1")]
	require.NotNil(t, n.CostUSD)
	assert.InDelta(t, 0.25, *n.CostUSD, 1e-9)
	assert.Equal(t, "reported", n.Attrs["cost_source"])
}

func TestAssistantTurnCostEstimatedProvenance(t *testing.T) {
	p := PricerFunc(func(in PriceInputs) (PriceResult, bool) {
		return PriceResult{USD: float64(in.TokensIn+in.TokensOut) / 1000, Source: "estimated"}, true
	})
	o := ob("assistant_turn", "", time.Unix(0, 0).UTC())
	o.Correlation.MessageID = "mc2"
	o.Attrs = map[string]any{"model": "model-x", "tokens_in": int64(1000), "tokens_out": int64(0)}

	g := NewGraphWithPricer(p)
	g.Apply(o)

	n := g.Nodes[model.AssistantTurnID(execID, "mc2")]
	require.NotNil(t, n.CostUSD)
	assert.InDelta(t, 1.0, *n.CostUSD, 1e-9)
	assert.Equal(t, "estimated", n.Attrs["cost_source"])
}

func TestAssistantTurnCostIncludesCacheTokens(t *testing.T) {
	p := PricerFunc(func(in PriceInputs) (PriceResult, bool) {
		usd := float64(in.TokensIn+in.TokensOut+in.CacheReadIn+in.CacheWrite) / 1000
		return PriceResult{USD: usd, Source: "estimated"}, true
	})
	o := ob("assistant_turn", "", time.Unix(0, 0).UTC())
	o.Correlation.MessageID = "mc_cache"
	o.Attrs = map[string]any{
		"model":         "model-x",
		"tokens_in":     int64(10),
		"tokens_out":    int64(5),
		"cache_read_in": int64(1000),
		"cache_write":   int64(2000),
	}

	g := NewGraphWithPricer(p)
	g.Apply(o)

	n := g.Nodes[model.AssistantTurnID(execID, "mc_cache")]
	require.NotNil(t, n.CostUSD)
	assert.InDelta(t, float64(10+5+1000+2000)/1000, *n.CostUSD, 1e-9)
}

func TestAssistantTurnReportedCostWinsOverCacheEstimate(t *testing.T) {
	eng := pricing.New()
	p := PricerFunc(func(in PriceInputs) (PriceResult, bool) {
		r, ok := eng.Cost(pricing.Inputs{
			ModelID:     in.ModelID,
			TokensIn:    in.TokensIn,
			TokensOut:   in.TokensOut,
			CacheReadIn: in.CacheReadIn,
			CacheWrite:  in.CacheWrite,
			ReportedUSD: in.ReportedUSD,
		})
		return PriceResult{USD: r.USD, Source: r.Source}, ok
	})
	o := ob("assistant_turn", "", time.Unix(0, 0).UTC())
	o.Source = model.SourceStreamJSON
	o.Correlation.MessageID = "mc_reported"
	o.Attrs = map[string]any{
		"model":         "claude-opus-4-8",
		"tokens_in":     int64(1000),
		"tokens_out":    int64(500),
		"cache_read_in": int64(1_000_000),
		"cache_write":   int64(1_000_000),
		"cost_usd":      float64(0.25),
	}

	g := NewGraphWithPricer(p)
	g.Apply(o)

	n := g.Nodes[model.AssistantTurnID(execID, "mc_reported")]
	require.NotNil(t, n.CostUSD)
	assert.InDelta(t, 0.25, *n.CostUSD, 1e-9)
	assert.Equal(t, "reported", n.Attrs["cost_source"])
}

func TestAssistantTurnCostUnavailableLeavesNil(t *testing.T) {
	p := PricerFunc(func(in PriceInputs) (PriceResult, bool) {
		return PriceResult{}, false
	})
	o := ob("assistant_turn", "", time.Unix(0, 0).UTC())
	o.Correlation.MessageID = "mc3"
	o.Attrs = map[string]any{"model": "unknown", "tokens_in": int64(5)}

	g := NewGraphWithPricer(p)
	g.Apply(o)

	n := g.Nodes[model.AssistantTurnID(execID, "mc3")]
	assert.Nil(t, n.CostUSD)
	_, has := n.Attrs["cost_source"]
	assert.False(t, has)
}

func TestCostAttributionOrderIndependent(t *testing.T) {
	p := PricerFunc(func(in PriceInputs) (PriceResult, bool) {
		return PriceResult{USD: float64(in.TokensIn), Source: "estimated"}, true
	})
	t0 := time.Unix(0, 0).UTC()
	a := ob("assistant_turn", "", t0)
	a.Source = model.SourceStreamJSON
	a.Correlation.MessageID = "mc4"
	a.Attrs = map[string]any{"model": "model-x", "tokens_in": int64(7)}
	b := ob("assistant_turn", "", t0.Add(time.Second))
	b.Source = model.SourceOTel
	b.Correlation.MessageID = "mc4"
	b.Attrs = map[string]any{"model": "model-x", "tokens_in": int64(11)}

	fwd := NewGraphWithPricer(p)
	fwd.ApplyAll([]model.Observation{a, b})
	rev := NewGraphWithPricer(p)
	rev.ApplyAll([]model.Observation{b, a})

	id := model.AssistantTurnID(execID, "mc4")
	require.NotNil(t, fwd.Nodes[id].CostUSD)
	assert.Equal(t, *fwd.Nodes[id].CostUSD, *rev.Nodes[id].CostUSD)
}

func TestNewGraphNoPricerNoCost(t *testing.T) {
	o := ob("assistant_turn", "", time.Unix(0, 0).UTC())
	o.Correlation.MessageID = "mc5"
	o.Attrs = map[string]any{"model": "model-x", "tokens_in": int64(5)}
	g := NewGraph()
	g.Apply(o)
	assert.Nil(t, g.Nodes[model.AssistantTurnID(execID, "mc5")].CostUSD)
}

func TestEnsureRunPopulatesModelID(t *testing.T) {
	o := ob("assistant_turn", "", time.Unix(0, 0).UTC())
	o.Correlation.MessageID = "mc6"
	o.Attrs = map[string]any{"model": "claude-sonnet-4-6", "tokens_in": int64(10)}

	g := NewGraph()
	g.Apply(o)

	r := g.Runs[runID]
	require.NotNil(t, r)
	assert.Equal(t, "claude-sonnet-4-6", r.ModelID)
}

func TestEnsureRunModelIDFirstNonEmptyWins(t *testing.T) {
	t0 := time.Unix(0, 0).UTC()
	first := ob("assistant_turn", "", t0)
	first.Correlation.MessageID = "mc7a"
	first.Attrs = map[string]any{"model": "claude-opus-4-8"}
	second := ob("assistant_turn", "", t0.Add(time.Second))
	second.Correlation.MessageID = "mc7b"
	second.Attrs = map[string]any{"model": "claude-sonnet-4-6"}

	g := NewGraph()
	g.ApplyAll([]model.Observation{first, second})

	r := g.Runs[runID]
	require.NotNil(t, r)
	assert.Equal(t, "claude-opus-4-8", r.ModelID)
}

func TestApplyUserPromptMergesTextPayload(t *testing.T) {
	g := NewGraph()
	o := model.Observation{
		ObsID: "o1", RunID: "s1", ExecutionID: "e1", Source: model.SourceJSONL,
		Kind: "user_prompt", Correlation: model.Correlation{SessionID: "s1", UUID: "u1"},
		Payload:   &model.Payload{Input: json.RawMessage(`"list files"`)},
		EventTime: time.Unix(1, 0).UTC(), Seq: 1,
	}
	o.Payload.Hash = model.HashPayload(o.Payload)
	g.ApplyAll([]model.Observation{o})
	n := g.Nodes[model.UserPromptID("e1", "u1")]
	require.NotNil(t, n)
	require.NotNil(t, n.Payload)
	assert.JSONEq(t, `"list files"`, string(n.Payload.Input))
	assert.NotEmpty(t, n.PayloadHash)
}

func TestApplyAssistantTurnMergesTextPayload(t *testing.T) {
	g := NewGraph()
	o := model.Observation{
		ObsID: "o1", RunID: "s1", ExecutionID: "e1", Source: model.SourceJSONL,
		Kind: "assistant_turn", Correlation: model.Correlation{SessionID: "s1", MessageID: "m1"},
		Payload:   &model.Payload{Output: json.RawMessage(`"the reply"`)},
		EventTime: time.Unix(1, 0).UTC(), Seq: 1,
	}
	o.Payload.Hash = model.HashPayload(o.Payload)
	g.ApplyAll([]model.Observation{o})
	n := g.Nodes[model.AssistantTurnID("e1", "m1")]
	require.NotNil(t, n)
	require.NotNil(t, n.Payload)
	assert.JSONEq(t, `"the reply"`, string(n.Payload.Output))
	assert.NotEmpty(t, n.PayloadHash)
}

func TestApplyAssistantTurnNilPayloadNoPanic(t *testing.T) {
	g := NewGraph()
	o := model.Observation{
		ObsID: "o1", RunID: "s1", ExecutionID: "e1", Source: model.SourceStreamJSON,
		Kind: "assistant_turn", Correlation: model.Correlation{SessionID: "s1", MessageID: "m2"},
		EventTime: time.Unix(1, 0).UTC(), Seq: 1,
	}
	g.ApplyAll([]model.Observation{o})
	n := g.Nodes[model.AssistantTurnID("e1", "m2")]
	require.NotNil(t, n)
	assert.Nil(t, n.Payload)
}

func TestApplyAssistantTurnResultDoesNotClobberText(t *testing.T) {
	g := NewGraph()
	first := model.Observation{
		ObsID: "o1", RunID: "s1", ExecutionID: "e1", Source: model.SourceStreamJSON,
		Kind: "assistant_turn", Correlation: model.Correlation{SessionID: "s1", MessageID: "m1"},
		Payload:   &model.Payload{Output: json.RawMessage(`"keep me"`)},
		EventTime: time.Unix(1, 0).UTC(), Seq: 1,
	}
	first.Payload.Hash = model.HashPayload(first.Payload)
	second := model.Observation{
		ObsID: "o2", RunID: "s1", ExecutionID: "e1", Source: model.SourceStreamJSON,
		Kind: "assistant_turn", Correlation: model.Correlation{SessionID: "s1", MessageID: "m1"},
		EventTime: time.Unix(2, 0).UTC(), Seq: 2,
	}
	g.ApplyAll([]model.Observation{first, second})
	n := g.Nodes[model.AssistantTurnID("e1", "m1")]
	require.NotNil(t, n.Payload)
	assert.JSONEq(t, `"keep me"`, string(n.Payload.Output))
}

func TestCumulativeCostNoDoubleCount(t *testing.T) {
	sq := seq()
	line1 := []byte(`{"type":"result","session_id":"s1","total_cost_usd":0.10}`)
	line2 := []byte(`{"type":"result","session_id":"s1","total_cost_usd":0.30}`)
	obs1, _, err := streamjson.Parse(line1, execID, sq)
	require.NoError(t, err)
	obs2, _, err := streamjson.Parse(line2, execID, sq)
	require.NoError(t, err)

	p := PricerFunc(func(in PriceInputs) (PriceResult, bool) {
		if in.ReportedUSD != nil {
			return PriceResult{USD: *in.ReportedUSD, Source: "reported"}, true
		}
		return PriceResult{}, false
	})
	g := NewGraphWithPricer(p)
	g.ApplyAll(obs1)
	g.ApplyAll(obs2)

	var costUSD float64
	var found bool
	for _, n := range g.Nodes {
		if n.Type == model.NodeAssistantTurn && n.CostUSD != nil {
			costUSD += *n.CostUSD
			found = true
		}
	}
	require.True(t, found, "expected at least one assistant_turn node with cost")
	assert.InDelta(t, 0.30, costUSD, 1e-9)
}

func promptObs(uuid string, seq uint64) model.Observation {
	return model.Observation{
		ObsID: "o" + strconv.FormatUint(seq, 10), RunID: "s1", ExecutionID: "e1",
		Source: model.SourceHook, Kind: "user_prompt",
		Correlation: model.Correlation{SessionID: "s1", UUID: uuid},
		EventTime:   time.Unix(int64(seq), 0).UTC(), ObservedAt: time.Unix(int64(seq), 0).UTC(), Seq: seq,
	}
}

func turnObs(msg string, seq uint64) model.Observation {
	return model.Observation{
		ObsID: "o" + strconv.FormatUint(seq, 10), RunID: "s1", ExecutionID: "e1",
		Source: model.SourceHook, Kind: "assistant_turn",
		Correlation: model.Correlation{SessionID: "s1", MessageID: msg},
		EventTime:   time.Unix(int64(seq), 0).UTC(), ObservedAt: time.Unix(int64(seq), 0).UTC(), Seq: seq,
	}
}

func TestTurnParentsToPrecedingPrompt(t *testing.T) {
	g := NewGraph()
	g.ApplyAll([]model.Observation{
		promptObs("u1", 1),
		turnObs("m1", 2),
	})
	prompt := model.UserPromptID("e1", "u1")
	turn := model.AssistantTurnID("e1", "m1")
	assert.Contains(t, g.Edges, model.EdgeID("e1", model.EdgeParentChild, prompt, turn))
	assert.NotContains(t, g.Edges, model.EdgeID("e1", model.EdgeParentChild, model.SessionNodeID("e1"), turn))
}

func TestTurnParentsToGreatestPrecedingPrompt(t *testing.T) {
	g := NewGraph()
	g.ApplyAll([]model.Observation{
		promptObs("u1", 1),
		promptObs("u2", 3),
		turnObs("m1", 4),
	})
	turn := model.AssistantTurnID("e1", "m1")
	assert.Contains(t, g.Edges, model.EdgeID("e1", model.EdgeParentChild, model.UserPromptID("e1", "u2"), turn))
	assert.NotContains(t, g.Edges, model.EdgeID("e1", model.EdgeParentChild, model.UserPromptID("e1", "u1"), turn))
}

func TestTurnWithoutPrecedingPromptParentsToSession(t *testing.T) {
	g := NewGraph()
	g.Apply(turnObs("m1", 2))
	turn := model.AssistantTurnID("e1", "m1")
	assert.Contains(t, g.Edges, model.EdgeID("e1", model.EdgeParentChild, model.SessionNodeID("e1"), turn))
}

func TestLatePromptReparentsTurnFromSession(t *testing.T) {
	g := NewGraph()
	g.Apply(turnObs("m1", 2))
	turn := model.AssistantTurnID("e1", "m1")
	require.Contains(t, g.Edges, model.EdgeID("e1", model.EdgeParentChild, model.SessionNodeID("e1"), turn))
	_ = g.DrainDeltas()

	g.Apply(promptObs("u1", 1))
	prompt := model.UserPromptID("e1", "u1")
	assert.NotContains(t, g.Edges, model.EdgeID("e1", model.EdgeParentChild, model.SessionNodeID("e1"), turn))
	assert.Contains(t, g.Edges, model.EdgeID("e1", model.EdgeParentChild, prompt, turn))

	ds := g.DrainDeltas()
	dels := deltaByKind(ds, cdc.DeltaEdgeDelete)
	require.Len(t, dels, 1)
	require.NotNil(t, dels[0].Edge)
	assert.Equal(t, model.EdgeID("e1", model.EdgeParentChild, model.SessionNodeID("e1"), turn), dels[0].Edge.ID)
}

func TestTurnReparentDeleteRevNotBelowDeletedEdgeRev(t *testing.T) {
	g := NewGraph()
	g.Apply(turnObs("m1", 2))
	turn := model.AssistantTurnID("e1", "m1")
	sessionEdge := model.EdgeID("e1", model.EdgeParentChild, model.SessionNodeID("e1"), turn)
	require.Contains(t, g.Edges, sessionEdge)
	deletedRev := g.Edges[sessionEdge].Rev
	_ = g.DrainDeltas()

	g.Apply(promptObs("u1", 1))

	dels := deltaByKind(g.DrainDeltas(), cdc.DeltaEdgeDelete)
	require.Len(t, dels, 1)
	require.NotNil(t, dels[0].Edge)
	require.Equal(t, sessionEdge, dels[0].Edge.ID)
	assert.GreaterOrEqual(t, dels[0].Rev, deletedRev, "delete rev must not be below the deleted edge's last upsert rev")
	assert.GreaterOrEqual(t, dels[0].Rev, uint64(1), "delete rev must be at least the establishing observation seq")
}

func TestLatePromptReparentsTurnFromEarlierPrompt(t *testing.T) {
	g := NewGraph()
	g.ApplyAll([]model.Observation{
		promptObs("u1", 1),
		turnObs("m1", 4),
	})
	turn := model.AssistantTurnID("e1", "m1")
	require.Contains(t, g.Edges, model.EdgeID("e1", model.EdgeParentChild, model.UserPromptID("e1", "u1"), turn))
	_ = g.DrainDeltas()

	g.Apply(promptObs("u2", 3))
	assert.NotContains(t, g.Edges, model.EdgeID("e1", model.EdgeParentChild, model.UserPromptID("e1", "u1"), turn))
	assert.Contains(t, g.Edges, model.EdgeID("e1", model.EdgeParentChild, model.UserPromptID("e1", "u2"), turn))

	dels := deltaByKind(g.DrainDeltas(), cdc.DeltaEdgeDelete)
	require.Len(t, dels, 1)
	assert.Equal(t, model.EdgeID("e1", model.EdgeParentChild, model.UserPromptID("e1", "u1"), turn), dels[0].Edge.ID)
}

func TestLatePromptAfterTurnDoesNotReparentEarlierTurn(t *testing.T) {
	g := NewGraph()
	g.ApplyAll([]model.Observation{
		promptObs("u1", 1),
		turnObs("m1", 2),
	})
	turn := model.AssistantTurnID("e1", "m1")
	_ = g.DrainDeltas()

	g.Apply(promptObs("u2", 5))
	assert.Contains(t, g.Edges, model.EdgeID("e1", model.EdgeParentChild, model.UserPromptID("e1", "u1"), turn))
	assert.Empty(t, deltaByKind(g.DrainDeltas(), cdc.DeltaEdgeDelete))
}

func TestToolStillParentsToTurnUnderReparentedTurn(t *testing.T) {
	g := NewGraph()
	prompt := promptObs("u1", 1)
	turn := turnObs("m1", 2)
	use := toolObs("e1", "s1", "tA", "Bash", "running", 3)
	use.Correlation.MessageID = "m1"
	g.ApplyAll([]model.Observation{prompt, turn, use})

	turnID := model.AssistantTurnID("e1", "m1")
	tool := model.ToolCallID("e1", "tA")
	assert.Contains(t, g.Edges, model.EdgeID("e1", model.EdgeParentChild, model.UserPromptID("e1", "u1"), turnID))
	assert.Contains(t, g.Edges, model.EdgeID("e1", model.EdgeParentChild, turnID, tool))
}

func TestTurnParentDeterministicAcrossOrder(t *testing.T) {
	obs := []model.Observation{
		promptObs("u1", 1),
		promptObs("u2", 3),
		turnObs("m1", 2),
		turnObs("m2", 4),
	}
	var want string
	for i, p := range permute(obs) {
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

func TestMultiSourceTurnSelectsByFirstAppearanceDeterministic(t *testing.T) {
	obs := []model.Observation{
		turnObs("m1", 2),
		promptObs("u1", 3),
		hookTurn("e1", "s1", "m1", 5, 2, time.Unix(4, 0).UTC(), 4),
	}
	var want string
	for i, p := range permute(obs) {
		g := NewGraph()
		g.ApplyAll(p)
		turn := model.AssistantTurnID("e1", "m1")
		assert.Contains(t, g.Edges, model.EdgeID("e1", model.EdgeParentChild, model.SessionNodeID("e1"), turn), "permutation %d", i)
		assert.NotContains(t, g.Edges, model.EdgeID("e1", model.EdgeParentChild, model.UserPromptID("e1", "u1"), turn), "permutation %d", i)
		got := canonGraph(g)
		if i == 0 {
			want = got
			continue
		}
		assert.Equal(t, want, got, "permutation %d diverged", i)
	}
}

func agentPromptObs(uuid, agentID string, seq uint64) model.Observation {
	o := promptObs(uuid, seq)
	o.Correlation.AgentID = agentID
	return o
}

func agentTurnObs(msg, agentID string, seq uint64) model.Observation {
	o := turnObs(msg, seq)
	o.Correlation.AgentID = agentID
	return o
}

func subagentStopObs(parentToolUseID, subagentType, description string, seq uint64) model.Observation {
	o := model.Observation{
		ObsID: "o" + strconv.FormatUint(seq, 10), RunID: "s1", ExecutionID: "e1",
		Source: model.SourceJSONL, Kind: "subagent_stop",
		Correlation: model.Correlation{SessionID: "s1", AgentID: "ag1", ParentToolUseID: parentToolUseID},
		EventTime:   time.Unix(int64(seq), 0).UTC(), ObservedAt: time.Unix(int64(seq), 0).UTC(), Seq: seq,
	}
	attrs := map[string]any{}
	if subagentType != "" {
		attrs["subagent_type"] = subagentType
	}
	if description != "" {
		attrs["description"] = description
	}
	if len(attrs) > 0 {
		o.Attrs = attrs
	}
	return o
}

func pcEdge(src, dst string) string {
	return model.EdgeID("e1", model.EdgeParentChild, src, dst)
}

func TestInnerRootPromptParentsUnderSubagent(t *testing.T) {
	g := NewGraph()
	g.Apply(agentPromptObs("u1", "ag1", 1))

	prompt := model.UserPromptID("e1", "u1")
	assert.Contains(t, g.Edges, pcEdge(model.SubagentID("e1", "ag1"), prompt))
	assert.NotContains(t, g.Edges, pcEdge(model.SessionNodeID("e1"), prompt))
}

func TestMainPromptStillParentsUnderSession(t *testing.T) {
	g := NewGraph()
	g.Apply(promptObs("u1", 1))
	prompt := model.UserPromptID("e1", "u1")
	assert.Contains(t, g.Edges, pcEdge(model.SessionNodeID("e1"), prompt))
}

func TestSubagentParentsUnderAgentToolCall(t *testing.T) {
	g := NewGraph()
	g.Apply(subagentStopObs("toolu_agent", "researcher", "Review PR1 reparent", 5))

	sub := model.SubagentID("e1", "ag1")
	n := g.Nodes[sub]
	require.NotNil(t, n)
	assert.Equal(t, "researcher", n.SubagentType)
	assert.Equal(t, "Review PR1 reparent", n.Name)
	assert.Contains(t, g.Edges, pcEdge(model.ToolCallID("e1", "toolu_agent"), sub))
	assert.NotContains(t, g.Edges, pcEdge(model.SessionNodeID("e1"), sub))
}

func TestSubagentMetaUpgradesSessionParentDropsStaleEdge(t *testing.T) {
	g := NewGraph()
	g.Apply(subagentStopObs("", "", "", 1))
	sub := model.SubagentID("e1", "ag1")
	sessionEdge := pcEdge(model.SessionNodeID("e1"), sub)
	require.Contains(t, g.Edges, sessionEdge)
	deletedRev := g.Edges[sessionEdge].Rev
	_ = g.DrainDeltas()

	g.Apply(subagentStopObs("toolu_agent", "researcher", "desc", 4))

	toolEdge := pcEdge(model.ToolCallID("e1", "toolu_agent"), sub)
	assert.Contains(t, g.Edges, toolEdge)
	assert.NotContains(t, g.Edges, sessionEdge)

	dels := deltaByKind(g.DrainDeltas(), cdc.DeltaEdgeDelete)
	require.Len(t, dels, 1)
	require.NotNil(t, dels[0].Edge)
	assert.Equal(t, sessionEdge, dels[0].Edge.ID)
	assert.GreaterOrEqual(t, dels[0].Rev, deletedRev)
	assert.GreaterOrEqual(t, dels[0].Rev, uint64(4))

	n := g.Nodes[sub]
	assert.Equal(t, "researcher", n.SubagentType)
	assert.Equal(t, "desc", n.Name)
}

func TestSubagentSessionStopAfterMetaLeavesNoStaleEdge(t *testing.T) {
	g := NewGraph()
	g.Apply(subagentStopObs("toolu_agent", "researcher", "desc", 1))
	sub := model.SubagentID("e1", "ag1")
	toolEdge := pcEdge(model.ToolCallID("e1", "toolu_agent"), sub)
	require.Contains(t, g.Edges, toolEdge)
	_ = g.DrainDeltas()

	g.Apply(subagentStopObs("", "", "", 4))

	assert.Contains(t, g.Edges, toolEdge)
	assert.NotContains(t, g.Edges, pcEdge(model.SessionNodeID("e1"), sub))
	assert.Empty(t, deltaByKind(g.DrainDeltas(), cdc.DeltaEdgeDelete))

	edges := 0
	for id, e := range g.Edges {
		if e.Type == model.EdgeParentChild && e.Dst == sub {
			assert.Equal(t, toolEdge, id)
			edges++
		}
	}
	assert.Equal(t, 1, edges)
}

func TestInnerPromptCarriesAgentID(t *testing.T) {
	g := NewGraph()
	g.Apply(agentPromptObs("u1", "ag1", 1))
	n := g.Nodes[model.UserPromptID("e1", "u1")]
	require.NotNil(t, n)
	assert.Equal(t, "ag1", n.AgentID)
}

func TestInnerTurnCarriesAgentID(t *testing.T) {
	g := NewGraph()
	g.Apply(agentTurnObs("m1", "ag1", 1))
	n := g.Nodes[model.AssistantTurnID("e1", "m1")]
	require.NotNil(t, n)
	assert.Equal(t, "ag1", n.AgentID)
}

func TestInnerToolCarriesAgentID(t *testing.T) {
	o := toolObs("e1", "s1", "toolu_1", "Bash", string(model.StatusOK), 1)
	o.Correlation.AgentID = "ag1"
	g := NewGraph()
	g.Apply(o)
	n := g.Nodes[model.ToolCallID("e1", "toolu_1")]
	require.NotNil(t, n)
	assert.Equal(t, "ag1", n.AgentID)
}

func TestTopLevelNodesHaveNoAgentID(t *testing.T) {
	g := NewGraph()
	g.Apply(promptObs("u1", 1))
	g.Apply(turnObs("m1", 2))
	g.Apply(toolObs("e1", "s1", "toolu_1", "Bash", string(model.StatusOK), 3))
	assert.Empty(t, g.Nodes[model.UserPromptID("e1", "u1")].AgentID)
	assert.Empty(t, g.Nodes[model.AssistantTurnID("e1", "m1")].AgentID)
	assert.Empty(t, g.Nodes[model.ToolCallID("e1", "toolu_1")].AgentID)
}

func TestSubagentParentOrderIndependent(t *testing.T) {
	obs := []model.Observation{
		subagentStopObs("", "", "", 1),
		subagentStopObs("toolu_agent", "researcher", "desc", 2),
		subagentStopObs("", "", "", 3),
	}
	sub := model.SubagentID("e1", "ag1")
	toolEdge := pcEdge(model.ToolCallID("e1", "toolu_agent"), sub)
	sessionEdge := pcEdge(model.SessionNodeID("e1"), sub)
	for i, p := range permute(obs) {
		g := NewGraph()
		g.ApplyAll(p)
		assert.Contains(t, g.Edges, toolEdge, "permutation %d", i)
		assert.NotContains(t, g.Edges, sessionEdge, "permutation %d", i)
		n := g.Nodes[sub]
		assert.Equal(t, "researcher", n.SubagentType, "permutation %d", i)
		assert.Equal(t, "desc", n.Name, "permutation %d", i)
	}
}

func TestSubagentMetaDescriptionFirstNonEmptyWins(t *testing.T) {
	g := NewGraph()
	g.Apply(subagentStopObs("toolu_agent", "researcher", "first", 1))
	g.Apply(subagentStopObs("toolu_agent", "other", "second", 2))
	n := g.Nodes[model.SubagentID("e1", "ag1")]
	assert.Equal(t, "researcher", n.SubagentType)
	assert.Equal(t, "first", n.Name)
}

func TestSubagentTurnBeforeInnerPromptHangsUnderSubagent(t *testing.T) {
	g := NewGraph()
	g.Apply(agentTurnObs("m1", "ag1", 2))

	sub := model.SubagentID("e1", "ag1")
	turn := model.AssistantTurnID("e1", "m1")
	assert.Contains(t, g.Edges, pcEdge(sub, turn))
	assert.NotContains(t, g.Edges, pcEdge(model.SessionNodeID("e1"), turn))
}

func TestSubagentTurnReparentsToInnerPrompt(t *testing.T) {
	g := NewGraph()
	g.Apply(agentTurnObs("m1", "ag1", 2))
	sub := model.SubagentID("e1", "ag1")
	turn := model.AssistantTurnID("e1", "m1")
	subEdge := pcEdge(sub, turn)
	require.Contains(t, g.Edges, subEdge)
	_ = g.DrainDeltas()

	g.Apply(agentPromptObs("u1", "ag1", 1))

	prompt := model.UserPromptID("e1", "u1")
	assert.Contains(t, g.Edges, pcEdge(prompt, turn))
	assert.NotContains(t, g.Edges, subEdge)

	dels := deltaByKind(g.DrainDeltas(), cdc.DeltaEdgeDelete)
	require.Len(t, dels, 1)
	assert.Equal(t, subEdge, dels[0].Edge.ID)
}

func TestAgentScopedTurnIgnoresForeignPrompt(t *testing.T) {
	g := NewGraph()
	g.ApplyAll([]model.Observation{
		promptObs("uMain", 1),
		agentTurnObs("mAg", "ag1", 2),
	})
	turn := model.AssistantTurnID("e1", "mAg")
	assert.Contains(t, g.Edges, pcEdge(model.SubagentID("e1", "ag1"), turn))
	assert.NotContains(t, g.Edges, pcEdge(model.UserPromptID("e1", "uMain"), turn))
}

func TestMainTurnIgnoresSubagentPrompt(t *testing.T) {
	g := NewGraph()
	g.ApplyAll([]model.Observation{
		agentPromptObs("uAg", "ag1", 1),
		turnObs("mMain", 2),
	})
	turn := model.AssistantTurnID("e1", "mMain")
	assert.Contains(t, g.Edges, pcEdge(model.SessionNodeID("e1"), turn))
	assert.NotContains(t, g.Edges, pcEdge(model.UserPromptID("e1", "uAg"), turn))
}

func TestAgentIsolationInterleavedExactEdgeSet(t *testing.T) {
	obs := []model.Observation{
		promptObs("uM1", 1),
		agentPromptObs("uA1", "ag1", 2),
		turnObs("mM1", 3),
		agentTurnObs("mA1", "ag1", 4),
		agentPromptObs("uB1", "ag2", 5),
		agentTurnObs("mB1", "ag2", 6),
		promptObs("uM2", 7),
		turnObs("mM2", 8),
		agentTurnObs("mA2", "ag1", 9),
		agentTurnObs("mB2", "ag2", 10),
	}
	want := map[string]bool{
		pcEdge(model.UserPromptID("e1", "uM1"), model.AssistantTurnID("e1", "mM1")): true,
		pcEdge(model.UserPromptID("e1", "uM2"), model.AssistantTurnID("e1", "mM2")): true,
		pcEdge(model.UserPromptID("e1", "uA1"), model.AssistantTurnID("e1", "mA1")): true,
		pcEdge(model.UserPromptID("e1", "uA1"), model.AssistantTurnID("e1", "mA2")): true,
		pcEdge(model.UserPromptID("e1", "uB1"), model.AssistantTurnID("e1", "mB1")): true,
		pcEdge(model.UserPromptID("e1", "uB1"), model.AssistantTurnID("e1", "mB2")): true,
		pcEdge(model.SessionNodeID("e1"), model.UserPromptID("e1", "uM1")):          true,
		pcEdge(model.SessionNodeID("e1"), model.UserPromptID("e1", "uM2")):          true,
		pcEdge(model.SubagentID("e1", "ag1"), model.UserPromptID("e1", "uA1")):      true,
		pcEdge(model.SubagentID("e1", "ag2"), model.UserPromptID("e1", "uB1")):      true,
	}
	for i, p := range sampleOrders(obs, 64) {
		g := NewGraph()
		g.ApplyAll(p)
		got := map[string]bool{}
		for id, e := range g.Edges {
			if e.Type == model.EdgeParentChild {
				got[id] = true
			}
		}
		assert.Equal(t, want, got, "ordering %d edge set mismatch", i)
	}
}

func sampleOrders(obs []model.Observation, n int) [][]model.Observation {
	out := [][]model.Observation{append([]model.Observation(nil), obs...)}
	state := uint64(0x9e3779b97f4a7c15)
	next := func() uint64 {
		state ^= state << 13
		state ^= state >> 7
		state ^= state << 17
		return state
	}
	for k := 1; k < n; k++ {
		p := append([]model.Observation(nil), obs...)
		for i := len(p) - 1; i > 0; i-- {
			j := int(next() % uint64(i+1))
			p[i], p[j] = p[j], p[i]
		}
		out = append(out, p)
	}
	return out
}

func TestMainSessionRegressionByteIdentical(t *testing.T) {
	g := NewGraph()
	g.ApplyAll([]model.Observation{
		turnObs("m1", 2),
		promptObs("u1", 1),
		promptObs("u2", 5),
		turnObs("m2", 3),
		turnObs("m3", 6),
		turnObs("m4", 4),
	})

	wantEdges := map[string]uint64{
		pcEdge(model.SessionNodeID("e1"), model.UserPromptID("e1", "u1")):         1,
		pcEdge(model.SessionNodeID("e1"), model.UserPromptID("e1", "u2")):         5,
		pcEdge(model.UserPromptID("e1", "u1"), model.AssistantTurnID("e1", "m1")): 2,
		pcEdge(model.UserPromptID("e1", "u1"), model.AssistantTurnID("e1", "m2")): 3,
		pcEdge(model.UserPromptID("e1", "u1"), model.AssistantTurnID("e1", "m4")): 4,
		pcEdge(model.UserPromptID("e1", "u2"), model.AssistantTurnID("e1", "m3")): 6,
	}
	gotEdges := map[string]uint64{}
	for id, e := range g.Edges {
		if e.Type == model.EdgeParentChild {
			gotEdges[id] = e.Rev
		}
	}
	assert.Equal(t, wantEdges, gotEdges)

	wantDeletes := map[string]uint64{
		pcEdge(model.SessionNodeID("e1"), model.AssistantTurnID("e1", "m1")): 2,
	}
	gotDeletes := map[string]uint64{}
	for _, d := range deltaByKind(g.DrainDeltas(), cdc.DeltaEdgeDelete) {
		gotDeletes[d.Edge.ID] = d.Rev
	}
	assert.Equal(t, wantDeletes, gotDeletes)
}

func TestLargeInterleavedParentInvariant(t *testing.T) {
	groups := []string{"", "ag1", "ag2", "ag3"}
	type rec struct {
		isPrompt bool
		group    string
		seq      uint64
	}
	var obs []model.Observation
	var recs []rec
	var seq uint64
	for i := 0; i < 240; i++ {
		seq++
		grp := groups[i%len(groups)]
		if i%3 == 0 {
			obs = append(obs, agentPromptObs("u"+strconv.Itoa(i), grp, seq))
			recs = append(recs, rec{isPrompt: true, group: grp, seq: seq})
		} else {
			obs = append(obs, agentTurnObs("m"+strconv.Itoa(i), grp, seq))
			recs = append(recs, rec{isPrompt: false, group: grp, seq: seq})
		}
	}
	rng := func(n int) int { return (n*1103515245 + 12345) & 0x7fffffff }
	for i := len(obs) - 1; i > 0; i-- {
		j := rng(i) % (i + 1)
		obs[i], obs[j] = obs[j], obs[i]
	}

	g := NewGraph()
	g.ApplyAll(obs)

	promptSeqs := map[string][]uint64{}
	promptID := map[string]map[uint64]string{}
	for i, r := range recs {
		if !r.isPrompt {
			continue
		}
		promptSeqs[r.group] = append(promptSeqs[r.group], r.seq)
		if promptID[r.group] == nil {
			promptID[r.group] = map[uint64]string{}
		}
		promptID[r.group][r.seq] = model.UserPromptID("e1", "u"+strconv.Itoa(i))
	}
	for _, s := range promptSeqs {
		sort.Slice(s, func(a, b int) bool { return s[a] < s[b] })
	}
	root := func(group string) string {
		if group == "" {
			return model.SessionNodeID("e1")
		}
		return model.SubagentID("e1", group)
	}
	expectedParent := func(group string, turnSeq uint64) string {
		seqs := promptSeqs[group]
		var best uint64
		found := false
		for _, ps := range seqs {
			if ps < turnSeq && (!found || ps > best) {
				best = ps
				found = true
			}
		}
		if !found {
			return root(group)
		}
		return promptID[group][best]
	}
	for i, r := range recs {
		if r.isPrompt {
			continue
		}
		turn := model.AssistantTurnID("e1", "m"+strconv.Itoa(i))
		parent := expectedParent(r.group, r.seq)
		assert.Contains(t, g.Edges, pcEdge(parent, turn), "turn %d (group %q seq %d)", i, r.group, r.seq)
		childEdges := 0
		for _, e := range g.Edges {
			if e.Type == model.EdgeParentChild && e.Dst == turn {
				childEdges++
			}
		}
		assert.Equal(t, 1, childEdges, "turn %d must have exactly one parent", i)
	}
}

func TestNearLinearReparentsOnlyAffectedTurns(t *testing.T) {
	g := NewGraph()
	g.ApplyAll([]model.Observation{
		promptObs("u1", 10),
		turnObs("m1", 11),
		turnObs("m2", 12),
		turnObs("m3", 30),
	})
	_ = g.DrainDeltas()

	m1EdgeID := pcEdge(model.UserPromptID("e1", "u1"), model.AssistantTurnID("e1", "m1"))
	revBeforeM1 := g.Edges[m1EdgeID].Rev

	g.Apply(promptObs("u2", 20))

	assert.Contains(t, g.Edges, pcEdge(model.UserPromptID("e1", "u1"), model.AssistantTurnID("e1", "m1")))
	assert.Contains(t, g.Edges, pcEdge(model.UserPromptID("e1", "u1"), model.AssistantTurnID("e1", "m2")))
	assert.Contains(t, g.Edges, pcEdge(model.UserPromptID("e1", "u2"), model.AssistantTurnID("e1", "m3")))

	assert.Equal(t, revBeforeM1, g.Edges[m1EdgeID].Rev, "m1 edge Rev must not change when turn is not in affected interval")

	dels := deltaByKind(g.DrainDeltas(), cdc.DeltaEdgeDelete)
	require.Len(t, dels, 1)
	assert.Equal(t, pcEdge(model.UserPromptID("e1", "u1"), model.AssistantTurnID("e1", "m3")), dels[0].Edge.ID)
}

func TestTurnSeqDecreaseRepositionsAndReparents(t *testing.T) {
	g := NewGraph()
	g.ApplyAll([]model.Observation{
		promptObs("u1", 5),
		turnObs("m2", 7),
		turnObs("m1", 10),
	})
	require.Contains(t, g.Edges, pcEdge(model.UserPromptID("e1", "u1"), model.AssistantTurnID("e1", "m1")))
	_ = g.DrainDeltas()

	g.Apply(turnObs("m1", 3))

	turn := model.AssistantTurnID("e1", "m1")
	assert.Contains(t, g.Edges, pcEdge(model.SessionNodeID("e1"), turn))
	assert.NotContains(t, g.Edges, pcEdge(model.UserPromptID("e1", "u1"), turn))

	g.Apply(promptObs("u2", 8))
	assert.Contains(t, g.Edges, pcEdge(model.SessionNodeID("e1"), turn))
	assert.NotContains(t, g.Edges, pcEdge(model.UserPromptID("e1", "u2"), turn))

	_ = g.DrainDeltas()
	g.Apply(promptObs("u0", 1))
	u0 := model.UserPromptID("e1", "u0")
	assert.Contains(t, g.Edges, pcEdge(u0, turn), "turn must reparent to prompt before it after repositionTurn")
	dels := deltaByKind(g.DrainDeltas(), cdc.DeltaEdgeDelete)
	require.Len(t, dels, 1)
	assert.Equal(t, pcEdge(model.SessionNodeID("e1"), turn), dels[0].Edge.ID)
}

func TestSetIfEmpty(t *testing.T) {
	tests := []struct {
		name    string
		initial string
		src     any
		want    string
	}{
		{"fills empty from string", "", "hello", "hello"},
		{"skips non-empty dst", "existing", "new", "existing"},
		{"skips empty string src", "", "", ""},
		{"skips nil src", "", nil, ""},
		{"skips int src", "", 42, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.initial
			setIfEmpty(&got, tc.src)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestApplyReproMetaNilRun(t *testing.T) {
	applyReproMeta(nil, map[string]any{"prompts_hash": "abc"})
}

func TestApplyReproMetaSetsAllFields(t *testing.T) {
	r := &model.Run{}
	applyReproMeta(r, map[string]any{
		"prompts_hash":         "ph",
		"skills_hash":          "sh",
		"subagents_hash":       "suh",
		"catacomb_config_hash": "cch",
		"catacomb_version":     "cv",
		"claude_code_version":  "ccv",
		"cwd":                  "/home",
	})
	require.NotNil(t, r.Repro)
	assert.Equal(t, "ph", r.Repro.PromptsHash)
	assert.Equal(t, "sh", r.Repro.SkillsHash)
	assert.Equal(t, "suh", r.Repro.SubagentsHash)
	assert.Equal(t, "cch", r.Repro.CatacombConfigHash)
	assert.Equal(t, "cv", r.Repro.CatacombVersion)
	assert.Equal(t, "ccv", r.Repro.ClaudeCodeVersion)
	assert.Equal(t, "/home", r.Repro.Cwd)
}

func TestApplyReproMetaNoOverwrite(t *testing.T) {
	r := &model.Run{Repro: &model.ReproMeta{PromptsHash: "original"}}
	applyReproMeta(r, map[string]any{"prompts_hash": "new"})
	assert.Equal(t, "original", r.Repro.PromptsHash)
}

func TestApplyReproMetaKindInGraph(t *testing.T) {
	g := NewGraph()
	g.Apply(model.Observation{
		RunID:       "s1",
		ExecutionID: "exec1",
		Source:      model.SourceHook,
		Kind:        "session_start",
		Correlation: model.Correlation{SessionID: "s1"},
		Attrs:       map[string]any{},
	})
	g.Apply(model.Observation{
		RunID:       "s1",
		ExecutionID: "exec1",
		Source:      model.SourceHook,
		Kind:        "repro_meta",
		Correlation: model.Correlation{SessionID: "s1"},
		Attrs: map[string]any{
			"prompts_hash":     "ph",
			"catacomb_version": "cv",
		},
	})
	r := g.Runs["s1"]
	require.NotNil(t, r)
	require.NotNil(t, r.Repro)
	assert.Equal(t, "ph", r.Repro.PromptsHash)
	assert.Equal(t, "cv", r.Repro.CatacombVersion)
}

func TestBareRunHasNoReproField(t *testing.T) {
	g := NewGraph()
	g.Apply(sessionStartObs("e1", "s1", 1))
	r := g.Runs["s1"]
	require.NotNil(t, r)
	assert.Nil(t, r.Repro)
	b, err := json.Marshal(r)
	require.NoError(t, err)
	assert.NotContains(t, string(b), `"repro"`)
}

func TestEnsureRunHarvestsClaudeCodeVersionAndCwd(t *testing.T) {
	g := NewGraph()
	g.Apply(model.Observation{
		RunID:       "s1",
		ExecutionID: "exec1",
		Source:      model.SourceHook,
		Kind:        "session_start",
		Correlation: model.Correlation{SessionID: "s1"},
		Attrs: map[string]any{
			"claude_code_version": "1.2.3",
			"cwd":                 "/project",
		},
	})
	r := g.Runs["s1"]
	require.NotNil(t, r)
	require.NotNil(t, r.Repro)
	assert.Equal(t, "1.2.3", r.Repro.ClaudeCodeVersion)
	assert.Equal(t, "/project", r.Repro.Cwd)
}

func TestEmitRunStartedDeltaCarriesRun(t *testing.T) {
	g := NewGraph()
	g.Apply(sessionStartObs("e1", "s1", 1))
	ds := g.DrainDeltas()
	starts := deltaByKind(ds, cdc.DeltaRunStarted)
	require.Len(t, starts, 1)
	require.NotNil(t, starts[0].Run)
	assert.Equal(t, "s1", starts[0].Run.ID)
	assert.Equal(t, model.StatusRunning, starts[0].Run.Status)
}

func TestEmitRunEndedDeltaCarriesRun(t *testing.T) {
	g := NewGraph()
	g.Apply(toolObs("e1", "s1", "t1", "Bash", "running", 1))
	_ = g.DrainDeltas()
	g.Apply(runEndedObs("e1", "s1", "timeout", 2))
	ds := g.DrainDeltas()
	ended := deltaByKind(ds, cdc.DeltaRunEnded)
	require.Len(t, ended, 1)
	require.NotNil(t, ended[0].Run)
	assert.Equal(t, "s1", ended[0].Run.ID)
	assert.Equal(t, model.StatusAbandoned, ended[0].Run.Status)
	assert.NotNil(t, ended[0].Run.EndedAt)
}

func TestAgentScopedDeterministicAcrossOrder(t *testing.T) {
	obs := []model.Observation{
		promptObs("uM", 1),
		agentPromptObs("uA", "ag1", 2),
		turnObs("mM", 3),
		agentTurnObs("mA", "ag1", 4),
		subagentStopObs("toolu_agent", "researcher", "desc", 5),
		agentTurnObs("mA2", "ag1", 6),
	}
	var want string
	for i, p := range permute(obs) {
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

func TestNodeKey(t *testing.T) {
	assert.Equal(t, "abc", nodeKey("abc", "span1", "obs1"))
	assert.Equal(t, "span:span1", nodeKey("", "span1", "obs1"))
	assert.Equal(t, "obs:obs1", nodeKey("", "", "obs1"))
}

func TestToolSpanFallbackDistinctSpanIDs(t *testing.T) {
	t0 := time.Unix(0, 0).UTC()
	o1 := model.Observation{
		ObsID: "obs1", RunID: runID, ExecutionID: execID,
		Source: model.SourceOTel, Kind: "assistant_tool_use",
		Correlation: model.Correlation{SessionID: runID, ToolUseID: "", SpanID: "s1"},
		Attrs:       map[string]any{"name": "Bash"},
		EventTime:   t0, ObservedAt: t0, Seq: 1,
	}
	o2 := model.Observation{
		ObsID: "obs2", RunID: runID, ExecutionID: execID,
		Source: model.SourceOTel, Kind: "assistant_tool_use",
		Correlation: model.Correlation{SessionID: runID, ToolUseID: "", SpanID: "s2"},
		Attrs:       map[string]any{"name": "Read"},
		EventTime:   t0, ObservedAt: t0, Seq: 2,
	}
	g := NewGraph()
	g.ApplyAll([]model.Observation{o1, o2})
	n1 := g.Nodes[model.ToolCallID(execID, "span:s1")]
	n2 := g.Nodes[model.ToolCallID(execID, "span:s2")]
	require.NotNil(t, n1)
	require.NotNil(t, n2)
	assert.NotEqual(t, n1.ID, n2.ID)
	assert.Nil(t, g.Nodes[model.ToolCallID(execID, "")])
}

func TestToolObsFallbackDistinctObsIDs(t *testing.T) {
	t0 := time.Unix(0, 0).UTC()
	o1 := model.Observation{
		ObsID: "o1", RunID: runID, ExecutionID: execID,
		Source: model.SourceOTel, Kind: "assistant_tool_use",
		Correlation: model.Correlation{SessionID: runID, ToolUseID: "", SpanID: ""},
		Attrs:       map[string]any{"name": "Bash"},
		EventTime:   t0, ObservedAt: t0, Seq: 1,
	}
	o2 := model.Observation{
		ObsID: "o2", RunID: runID, ExecutionID: execID,
		Source: model.SourceOTel, Kind: "assistant_tool_use",
		Correlation: model.Correlation{SessionID: runID, ToolUseID: "", SpanID: ""},
		Attrs:       map[string]any{"name": "Read"},
		EventTime:   t0, ObservedAt: t0, Seq: 2,
	}
	g := NewGraph()
	g.ApplyAll([]model.Observation{o1, o2})
	n1 := g.Nodes[model.ToolCallID(execID, "obs:o1")]
	n2 := g.Nodes[model.ToolCallID(execID, "obs:o2")]
	require.NotNil(t, n1)
	require.NotNil(t, n2)
	assert.NotEqual(t, n1.ID, n2.ID)
	assert.Nil(t, g.Nodes[model.ToolCallID(execID, "")])
}

func TestTurnSpanFallbackDistinctSpanIDs(t *testing.T) {
	t0 := time.Unix(0, 0).UTC()
	o1 := model.Observation{
		ObsID: "obs1", RunID: runID, ExecutionID: execID,
		Source: model.SourceOTel, Kind: "assistant_turn",
		Correlation: model.Correlation{SessionID: runID, MessageID: "", SpanID: "sp1"},
		Attrs:       map[string]any{},
		EventTime:   t0, ObservedAt: t0, Seq: 1,
	}
	o2 := model.Observation{
		ObsID: "obs2", RunID: runID, ExecutionID: execID,
		Source: model.SourceOTel, Kind: "assistant_turn",
		Correlation: model.Correlation{SessionID: runID, MessageID: "", SpanID: "sp2"},
		Attrs:       map[string]any{},
		EventTime:   t0, ObservedAt: t0, Seq: 2,
	}
	g := NewGraph()
	g.ApplyAll([]model.Observation{o1, o2})
	n1 := g.Nodes[model.AssistantTurnID(execID, "span:sp1")]
	n2 := g.Nodes[model.AssistantTurnID(execID, "span:sp2")]
	require.NotNil(t, n1)
	require.NotNil(t, n2)
	assert.NotEqual(t, n1.ID, n2.ID)
	assert.Nil(t, g.Nodes[model.AssistantTurnID(execID, "")])
}

func TestTurnNoMessageNoSpanCollapsesToOneNode(t *testing.T) {
	t0 := time.Unix(0, 0).UTC()
	o1 := model.Observation{
		ObsID: "ob1", RunID: runID, ExecutionID: execID,
		Source: model.SourceStreamJSON, Kind: "assistant_turn",
		Correlation: model.Correlation{SessionID: runID, MessageID: "", SpanID: ""},
		Attrs:       map[string]any{},
		EventTime:   t0, ObservedAt: t0, Seq: 1,
	}
	o2 := model.Observation{
		ObsID: "ob2", RunID: runID, ExecutionID: execID,
		Source: model.SourceStreamJSON, Kind: "assistant_turn",
		Correlation: model.Correlation{SessionID: runID, MessageID: "", SpanID: ""},
		Attrs:       map[string]any{},
		EventTime:   t0, ObservedAt: t0, Seq: 2,
	}
	g := NewGraph()
	g.ApplyAll([]model.Observation{o1, o2})
	collapsed := g.Nodes[model.AssistantTurnID(execID, "")]
	require.NotNil(t, collapsed)
	turns := 0
	for _, n := range g.Nodes {
		if n.Type == model.NodeAssistantTurn {
			turns++
		}
	}
	assert.Equal(t, 1, turns)
}

func TestPromptObsFallbackDistinctObsIDs(t *testing.T) {
	t0 := time.Unix(0, 0).UTC()
	o1 := model.Observation{
		ObsID: "po1", RunID: runID, ExecutionID: execID,
		Source: model.SourceOTel, Kind: "user_prompt",
		Correlation: model.Correlation{SessionID: runID, UUID: ""},
		Attrs:       map[string]any{},
		EventTime:   t0, ObservedAt: t0, Seq: 1,
	}
	o2 := model.Observation{
		ObsID: "po2", RunID: runID, ExecutionID: execID,
		Source: model.SourceOTel, Kind: "user_prompt",
		Correlation: model.Correlation{SessionID: runID, UUID: ""},
		Attrs:       map[string]any{},
		EventTime:   t0, ObservedAt: t0, Seq: 2,
	}
	g := NewGraph()
	g.ApplyAll([]model.Observation{o1, o2})
	n1 := g.Nodes[model.UserPromptID(execID, "obs:po1")]
	n2 := g.Nodes[model.UserPromptID(execID, "obs:po2")]
	require.NotNil(t, n1)
	require.NotNil(t, n2)
	assert.NotEqual(t, n1.ID, n2.ID)
	assert.Nil(t, g.Nodes[model.UserPromptID(execID, "")])
}

func TestResultObservationTagsSessionTotalNode(t *testing.T) {
	o := ob("assistant_turn", "", time.Unix(0, 0).UTC())
	o.Source = model.SourceStreamJSON
	o.Attrs = map[string]any{"session_total": true, "model": "m", "tokens_in": int64(7), "tokens_out": int64(9), "cost_usd": 0.5}

	g := NewGraph()
	g.Apply(o)

	n := g.Nodes[model.AssistantTurnID(execID, "")]
	require.NotNil(t, n)
	assert.True(t, n.SessionTotal())
}

func TestLegacyResultObservationTagsSessionTotalNode(t *testing.T) {
	o := ob("assistant_turn", "", time.Unix(0, 0).UTC())
	o.Source = model.SourceStreamJSON
	o.Attrs = map[string]any{"tokens_in": int64(7), "cost_usd": 0.5}

	g := NewGraph()
	g.Apply(o)

	assert.True(t, g.Nodes[model.AssistantTurnID(execID, "")].SessionTotal())
}

func TestMessageTurnWithReportedCostNotSessionTotal(t *testing.T) {
	o := ob("assistant_turn", "", time.Unix(0, 0).UTC())
	o.Source = model.SourceStreamJSON
	o.Correlation.MessageID = "m1"
	o.Attrs = map[string]any{"cost_usd": 0.5}

	g := NewGraph()
	g.Apply(o)

	assert.False(t, g.Nodes[model.AssistantTurnID(execID, "m1")].SessionTotal())
}

func TestNonStreamTurnWithCostNotSessionTotal(t *testing.T) {
	o := ob("assistant_turn", "", time.Unix(0, 0).UTC())
	o.Attrs = map[string]any{"cost_usd": 0.5}

	g := NewGraph()
	g.Apply(o)

	assert.False(t, g.Nodes[model.AssistantTurnID(execID, "")].SessionTotal())
}

func TestSessionTotalTagSurvivesStoreRoundTrip(t *testing.T) {
	o := ob("assistant_turn", "", time.Unix(0, 0).UTC())
	o.Source = model.SourceStreamJSON
	o.Attrs = map[string]any{"session_total": true, "cost_usd": 0.5}

	raw, err := json.Marshal(o)
	require.NoError(t, err)
	var rt model.Observation
	require.NoError(t, json.Unmarshal(raw, &rt))

	g := NewGraph()
	g.Apply(rt)

	assert.True(t, g.Nodes[model.AssistantTurnID(execID, "")].SessionTotal())
}
