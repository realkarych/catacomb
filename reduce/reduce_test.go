package reduce

import (
	"encoding/json"
	"io"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/ingest/jsonl"
	"github.com/realkarych/catacomb/model"
)

const (
	execID = "exec1"
	runID  = "s1"
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
	assert.JSONEq(t, `{"command":"ls"}`, string(n.Payload.Input))
	assert.JSONEq(t, `"a.txt"`, string(n.Payload.Output))
	assert.Equal(t, model.HashPayload(n.Payload), n.PayloadHash)
	assert.Equal(t, []model.SourceRef{
		{Source: model.SourceJSONL, ObsID: use.ObsID, ObservedAt: t0},
		{Source: model.SourceJSONL, ObsID: res.ObsID, ObservedAt: t1},
	}, n.Sources)
	require.NotNil(t, n.TStart)
	assert.Equal(t, t0, *n.TStart)
	require.NotNil(t, n.TEnd)
	assert.Equal(t, t1, *n.TEnd)
	require.NotNil(t, n.DurationMS)
	assert.Equal(t, int64(1000), *n.DurationMS)

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
	n := fwd.Nodes[id]
	require.NotNil(t, n)
	assert.Equal(t, "Bash", n.Name)
	assert.Equal(t, model.StatusOK, n.Status)
	assert.Equal(t, model.NodeToolCall, n.Type)
	require.NotNil(t, n.TStart)
	assert.Equal(t, t0, *n.TStart)
	assert.Equal(t, canonGraph(fwd), canonGraph(rev),
		"use-then-result and result-then-use must converge on every field, not just the ones spelled out above")
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

func TestEmptyToolUseIDMarkerDoesNotDropLaterTool(t *testing.T) {
	t0 := time.Unix(0, 0).UTC()
	mark := model.Observation{
		ObsID:       "mark_obs",
		RunID:       runID,
		ExecutionID: execID,
		Source:      model.SourceJSONL,
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
		Source:      model.SourceJSONL,
		Kind:        "assistant_tool_use",
		Correlation: model.Correlation{SessionID: runID},
		Attrs:       map[string]any{"name": "Bash"},
		EventTime:   t0.Add(time.Second),
		ObservedAt:  t0.Add(time.Second),
		Seq:         2,
	}

	g := NewGraph()
	g.ApplyAll([]model.Observation{mark, tool})

	id := model.ToolCallID(execID, nodeKey("", "tool_obs"))
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
	assert.Equal(t, time.Unix(0, 0).UTC(), *n.TEnd)
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
	assert.ElementsMatch(t, []string{model.SessionNodeID(execID), model.UserPromptID(execID, "u1")}, ids(nodes))
	assert.Equal(t, []string{model.EdgeID(execID, model.EdgeParentChild, model.SessionNodeID(execID), model.UserPromptID(execID, "u1"))}, edgeIDs(edges))
}

func ids(ns []*model.Node) []string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = n.ID
	}
	return out
}

func edgeIDs(es []*model.Edge) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.ID
	}
	return out
}

func TestResolveStatusGenuineLatches(t *testing.T) {
	assert.Equal(t, model.StatusOK, resolveStatus(model.StatusOK, model.StatusRunning))
	assert.Equal(t, model.StatusError, resolveStatus(model.StatusError, model.StatusPending))
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
		{"running then pending stays running", model.StatusRunning, model.StatusPending, model.StatusRunning},
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

func unknownKindObs(exec, runID, kind string, seq uint64) model.Observation {
	return model.Observation{
		ObsID: "o" + strconv.FormatUint(seq, 10), RunID: runID, ExecutionID: exec,
		Source: model.SourceHook, Kind: kind,
		Correlation: model.Correlation{SessionID: runID},
		EventTime:   time.Unix(int64(seq), 0).UTC(), Seq: seq,
	}
}

func TestAppendUnique(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		add  string
		want []string
	}{
		{"empty string is ignored", []string{"a"}, "", []string{"a"}},
		{"duplicate is not appended", []string{"a", "b"}, "a", []string{"a", "b"}},
		{"new value is appended", []string{"a"}, "b", []string{"a", "b"}},
		{"appends to nil", nil, "a", []string{"a"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, appendUnique(tc.in, tc.add))
		})
	}
}

func TestRunOpensOnFirstObs(t *testing.T) {
	g := NewGraph()
	g.Apply(toolObs("e1", "s1", "t1", "Bash", "running", 1))
	r := g.Runs["s1"]
	require.NotNil(t, r)
	assert.Equal(t, model.StatusRunning, r.Status)
	require.NotNil(t, r.StartedAt)
	assert.Equal(t, time.Unix(1, 0).UTC(), *r.StartedAt)
	assert.Equal(t, uint64(1), r.LastSeq)
	assert.Equal(t, []string{"s1"}, r.SessionIDs)
}

func TestRunStartedAtSkipsZeroEventTime(t *testing.T) {
	g := NewGraph()
	timeless := toolObs("e1", "s1", "t1", "Bash", "running", 1)
	timeless.EventTime = time.Time{}
	g.Apply(timeless)
	r := g.Runs["s1"]
	require.NotNil(t, r)
	assert.Nil(t, r.StartedAt)

	g.Apply(toolObs("e1", "s1", "t2", "Bash", "running", 2))
	require.NotNil(t, r.StartedAt)
	assert.Equal(t, time.Unix(2, 0).UTC(), *r.StartedAt)
}

func TestRunStartedAtIsEarliestEventTimeInAnyArrivalOrder(t *testing.T) {
	early := toolObs("e1", "s1", "t1", "Bash", "running", 5)
	late := toolObs("e1", "s1", "t2", "Bash", "running", 9)
	middle := toolObs("e1", "s1", "t3", "Bash", "running", 7)

	for i, p := range permute([]model.Observation{early, late, middle}) {
		g := NewGraph()
		g.ApplyAll(p)
		r := g.Runs["s1"]
		require.NotNil(t, r.StartedAt)
		assert.Equal(t, time.Unix(5, 0).UTC(), *r.StartedAt,
			"the run starts at its earliest observation, whatever order they arrive in (permutation %d)", i)
	}
}

func TestRunLastSeqTracksMaxIgnoringOutOfOrder(t *testing.T) {
	g := NewGraph()
	g.ApplyAll([]model.Observation{
		toolObs("e1", "s1", "t1", "Bash", "running", 5),
		toolObs("e1", "s1", "t2", "Bash", "running", 3),
	})
	assert.Equal(t, uint64(5), g.Runs["s1"].LastSeq)
}

func TestRunsSnapshot(t *testing.T) {
	g := NewGraph()
	g.Apply(toolObs("e1", "s1", "t1", "Bash", "running", 1))
	snap := g.RunsSnapshot()
	require.Len(t, snap, 1)
	assert.Equal(t, "s1", snap[0].ID)
	assert.Equal(t, *g.Runs["s1"], snap[0])

	snap[0].Status = model.StatusError
	assert.Equal(t, model.StatusRunning, g.Runs["s1"].Status,
		"the snapshot is a copy: mutating it must not reach back into the graph")
}

func TestRunsSnapshotMultipleRuns(t *testing.T) {
	g := NewGraph()
	g.Apply(toolObs("e1", "s1", "t1", "Bash", "running", 1))
	g.Apply(toolObs("e2", "s2", "t2", "Bash", "running", 2))
	snap := g.RunsSnapshot()
	got := make([]string, len(snap))
	for i, r := range snap {
		got[i] = r.ID
	}
	assert.ElementsMatch(t, []string{"s1", "s2"}, got)
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

func jsonlTool(exec, runID, toolUse string, seq uint64) model.Observation {
	return model.Observation{
		ObsID: "o" + strconv.FormatUint(seq, 10), RunID: runID, ExecutionID: exec,
		Source: model.SourceJSONL, Kind: "assistant_tool_use",
		Correlation: model.Correlation{SessionID: runID, ToolUseID: toolUse},
		Attrs:       map[string]any{"name": "Bash"},
		EventTime:   time.Unix(int64(seq), 0).UTC(), Seq: seq,
	}
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
	assert.Equal(t, 2, sourceRank(model.SourceHook))
	assert.Equal(t, 1, sourceRank(model.SourceJSONL))
	assert.Equal(t, 0, sourceRank(model.Source("other")))
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

func jsonlTurnNoTokens(exec, runID, msg string, ts time.Time, seq uint64) model.Observation {
	return model.Observation{
		ObsID: "o" + strconv.FormatUint(seq, 10), RunID: runID, ExecutionID: exec,
		Source: model.SourceJSONL, Kind: "assistant_turn",
		Correlation: model.Correlation{SessionID: runID, MessageID: msg},
		Attrs:       map[string]any{},
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

func TestTokensHookKeptWhenNoOTel(t *testing.T) {
	g := NewGraph()
	g.Apply(hookTurn("e1", "s1", "m1", 7, 3, time.Unix(1, 0).UTC(), 1))
	id := model.AssistantTurnID("e1", "m1")
	assert.Equal(t, int64(7), *g.Nodes[id].TokensIn)
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
		unknownKindObs("e9", "s9", "checkpoint", 10),
		hookTurn("e9", "s9", "m9", 7, 3, t0, 11),
		jsonlTurnNoTokens("e9", "s9", "m9", t0.Add(time.Second), 12),
		toolObs("e9", "s9", "tA", "Read", "running", 13),
		toolObs("e9", "s9", "tA", "Read", string(model.StatusOK), 15),
		jsonlTool("e9", "s9", "tB", 16),
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

func sourcesAsMultiset(refs []model.SourceRef) []model.SourceRef {
	out := append([]model.SourceRef(nil), refs...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Source != out[j].Source {
			return out[i].Source < out[j].Source
		}
		if out[i].ObsID != out[j].ObsID {
			return out[i].ObsID < out[j].ObsID
		}
		return out[i].ObservedAt.Before(out[j].ObservedAt)
	})
	return out
}

func canonNode(n *model.Node) *model.Node {
	c := *n
	c.Sources = sourcesAsMultiset(n.Sources)
	c.TStart = utcPtr(n.TStart)
	c.TEnd = utcPtr(n.TEnd)
	return &c
}

func canonRun(r *model.Run) *model.Run {
	c := *r
	c.StartedAt = utcPtr(r.StartedAt)
	c.EndedAt = utcPtr(r.EndedAt)
	return &c
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

func canonGraph(g *Graph) string {
	nodes := make([]*model.Node, 0, len(g.Nodes))
	for _, n := range g.Nodes {
		nodes = append(nodes, canonNode(n))
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	edges := make([]*model.Edge, 0, len(g.Edges))
	for _, e := range g.Edges {
		edges = append(edges, e)
	}
	sort.Slice(edges, func(i, j int) bool { return edges[i].ID < edges[j].ID })
	runs := make([]*model.Run, 0, len(g.Runs))
	for _, r := range g.Runs {
		runs = append(runs, canonRun(r))
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].ID < runs[j].ID })
	b, err := json.Marshal(struct {
		Nodes []*model.Node
		Edges []*model.Edge
		Runs  []*model.Run
	}{nodes, edges, runs})
	if err != nil {
		panic(err)
	}
	return string(b)
}

func TestPayloadRankJSONLIsFull(t *testing.T) {
	assert.Equal(t, 1, payloadRank(model.SourceHook))
	assert.Equal(t, 1, payloadRank(model.SourceJSONL))
	assert.Equal(t, 0, payloadRank(model.Source("other")))
}

func TestPayloadLowerRankIgnored(t *testing.T) {
	full := jsonlToolInput("e1", "s1", "t1", "Bash", `{"command":"ls -la"}`, 1)
	lower := model.Observation{
		ObsID: "o2", RunID: "s1", ExecutionID: "e1", Source: model.Source("other"),
		Kind: "assistant_tool_use", Correlation: model.Correlation{SessionID: "s1", ToolUseID: "t1"},
		Attrs:     map[string]any{"name": "Bash"},
		Payload:   &model.Payload{Input: json.RawMessage(`{"command":"l"}`)},
		EventTime: time.Unix(2, 0).UTC(), ObservedAt: time.Unix(2, 0).UTC(), Seq: 2,
	}
	fwd := NewGraph()
	fwd.ApplyAll([]model.Observation{full, lower})
	rev := NewGraph()
	rev.ApplyAll([]model.Observation{lower, full})
	id := model.ToolCallID("e1", "t1")
	require.NotNil(t, fwd.Nodes[id].Payload)
	assert.JSONEq(t, `{"command":"ls -la"}`, string(fwd.Nodes[id].Payload.Input))
	assert.Equal(t, string(fwd.Nodes[id].Payload.Input), string(rev.Nodes[id].Payload.Input))
}

func TestParentToolUseEdgeCreated(t *testing.T) {
	parent := jsonlToolInput("e1", "s1", "tparent", "Task", `{}`, 1)
	child := jsonlChildEdge("e1", "s1", "tchild", "tparent", 2)
	g := NewGraph()
	g.ApplyAll([]model.Observation{parent, child})
	id := model.EdgeID("e1", model.EdgeParentChild, model.ToolCallID("e1", "tparent"), model.ToolCallID("e1", "tchild"))
	require.NotNil(t, g.Edges[id])
	assert.Equal(t, model.ToolCallID("e1", "tparent"), g.Edges[id].Src)
	assert.Equal(t, model.ToolCallID("e1", "tchild"), g.Edges[id].Dst)
}

func hookChildEdge(execID, runID, childTUID, parentTUID string, seq uint64) model.Observation {
	return model.Observation{
		ObsID: "o" + strconv.FormatUint(seq, 10), RunID: runID, ExecutionID: execID, Source: model.SourceHook,
		Kind: "assistant_tool_use", Correlation: model.Correlation{SessionID: runID, ToolUseID: childTUID, ParentToolUseID: parentTUID},
		Attrs:     map[string]any{"name": "Task"},
		EventTime: time.Unix(100, 0).UTC(), ObservedAt: time.Unix(100, 0).UTC(), Seq: seq,
	}
}

func TestParentToolUseNoChildID(t *testing.T) {
	o := model.Observation{
		RunID: "s1", ExecutionID: "e1", Source: model.SourceJSONL,
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
	assert.Equal(t, 0, structureRank(model.SourceHook))
	assert.Equal(t, 0, structureRank(model.Source("other")))
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

func TestJSONLStructureOutranksHookEdgeBothOrders(t *testing.T) {
	hookEdge := hookChildEdge("e1", "s1", "tchild2", "tparentHOOK", 1)
	jsonlEdge := jsonlChildEdge("e1", "s1", "tchild2", "tparentJSONL", 2)
	fwd := NewGraph()
	fwd.ApplyAll([]model.Observation{hookEdge, jsonlEdge})
	rev := NewGraph()
	rev.ApplyAll([]model.Observation{jsonlEdge, hookEdge})
	wantID := model.EdgeID("e1", model.EdgeParentChild, model.ToolCallID("e1", "tparentJSONL"), model.ToolCallID("e1", "tchild2"))
	loseID := model.EdgeID("e1", model.EdgeParentChild, model.ToolCallID("e1", "tparentHOOK"), model.ToolCallID("e1", "tchild2"))
	require.NotNil(t, fwd.Edges[wantID])
	require.NotNil(t, rev.Edges[wantID])
	assert.Nil(t, fwd.Edges[loseID])
	assert.Nil(t, rev.Edges[loseID])
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
	g.Apply(jsonlToolTurn(2))

	tool := model.ToolCallID("e1", "t1")
	turnEdge := model.EdgeID("e1", model.EdgeParentChild, model.AssistantTurnID("e1", "m1"), tool)
	sessionEdge := model.EdgeID("e1", model.EdgeParentChild, model.SessionNodeID("e1"), tool)
	require.Contains(t, g.Edges, turnEdge)
	assert.NotContains(t, g.Edges, sessionEdge)
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

func TestParentChildEdgeReachableFromRealAssistantEnvelope(t *testing.T) {
	parentLine := `{"type":"assistant","sessionId":"s1","message":{"role":"assistant","id":"msg_parent","content":[{"type":"tool_use","id":"toolu_parent","name":"Task","input":{}}]}}` + "\n"
	childLine := `{"type":"assistant","sessionId":"s1","parent_tool_use_id":"toolu_parent","message":{"role":"assistant","id":"msg_child","content":[{"type":"tool_use","id":"toolu_child","name":"Bash","input":{"command":"ls"}}]}}` + "\n"

	parentObs, err := parseJSONL(strings.NewReader(parentLine), "exec_i1")
	require.NoError(t, err)
	childObs, err := parseJSONL(strings.NewReader(childLine), "exec_i1")
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

func TestStampIgnoresZeroEventTime(t *testing.T) {
	use := ob("assistant_tool_use", "toolu_z1", time.Time{})
	use.Attrs = map[string]any{"name": "Bash"}
	use.Seq = 7

	g := NewGraph()
	g.Apply(use)

	n := g.Nodes[model.ToolCallID(execID, "toolu_z1")]
	require.NotNil(t, n)
	assert.Nil(t, n.TStart)
	assert.Nil(t, n.TEnd)
	assert.Nil(t, n.DurationMS)
	assert.Len(t, n.Sources, 1)
	assert.Equal(t, uint64(7), n.Rev)
}

func TestStampEndIgnoresZeroEventTime(t *testing.T) {
	t0 := time.Date(2026, 6, 20, 10, 0, 1, 0, time.UTC)
	use := ob("assistant_tool_use", "toolu_z2", t0)
	use.Attrs = map[string]any{"name": "Bash"}
	res := ob("tool_result", "toolu_z2", time.Time{})
	res.Attrs = map[string]any{"status": string(model.StatusOK)}

	g := NewGraph()
	g.ApplyAll([]model.Observation{use, res})

	n := g.Nodes[model.ToolCallID(execID, "toolu_z2")]
	require.NotNil(t, n)
	require.NotNil(t, n.TStart)
	assert.Equal(t, t0, *n.TStart)
	assert.Nil(t, n.TEnd)
	assert.Nil(t, n.DurationMS)
}

func TestStampZeroThenRealEventTime(t *testing.T) {
	t1 := time.Date(2026, 6, 20, 10, 0, 5, 0, time.UTC)
	use := ob("assistant_tool_use", "toolu_z3", time.Time{})
	use.Attrs = map[string]any{"name": "Bash"}
	res := ob("tool_result", "toolu_z3", t1)
	res.Attrs = map[string]any{"status": string(model.StatusOK)}

	g := NewGraph()
	g.ApplyAll([]model.Observation{use, res})

	n := g.Nodes[model.ToolCallID(execID, "toolu_z3")]
	require.NotNil(t, n)
	require.NotNil(t, n.TStart)
	assert.Equal(t, t1, *n.TStart)
	require.NotNil(t, n.TEnd)
	assert.Equal(t, t1, *n.TEnd)
	require.NotNil(t, n.DurationMS)
	assert.Equal(t, int64(0), *n.DurationMS)
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
	hookEnd := t0.Add(5 * time.Second)

	use := ob("assistant_tool_use", "toolu_d3", t0)
	use.Attrs = map[string]any{"name": "Bash"}

	resJSONL := ob("tool_result", "toolu_d3", jsonlEnd)
	resJSONL.Source = model.SourceJSONL
	resJSONL.Attrs = map[string]any{"status": string(model.StatusOK)}

	resHook := ob("tool_result", "toolu_d3", hookEnd)
	resHook.Source = model.SourceHook
	resHook.Attrs = map[string]any{"status": string(model.StatusOK)}

	g := NewGraph()
	g.ApplyAll([]model.Observation{use, resJSONL, resHook})
	rg := NewGraph()
	rg.ApplyAll([]model.Observation{resHook, resJSONL, use})

	id := model.ToolCallID(execID, "toolu_d3")
	assert.Equal(t, hookEnd, *g.Nodes[id].TEnd)
	assert.Equal(t, hookEnd, *rg.Nodes[id].TEnd)
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
	assert.Equal(t, "unpriced", n.Attrs["cost_source"])
}

func TestAssistantTurnUnpriceableZeroTokensNotFlagged(t *testing.T) {
	p := PricerFunc(func(in PriceInputs) (PriceResult, bool) {
		return PriceResult{}, false
	})
	o := ob("assistant_turn", "", time.Unix(0, 0).UTC())
	o.Correlation.MessageID = "mc4"
	o.Attrs = map[string]any{"model": "unknown"}

	g := NewGraphWithPricer(p)
	g.Apply(o)

	n := g.Nodes[model.AssistantTurnID(execID, "mc4")]
	assert.Nil(t, n.CostUSD)
	_, has := n.Attrs["cost_source"]
	assert.False(t, has)
}

func TestAssistantTurnUnpriceableCacheTokensFlagged(t *testing.T) {
	p := PricerFunc(func(in PriceInputs) (PriceResult, bool) {
		return PriceResult{}, false
	})
	o := ob("assistant_turn", "", time.Unix(0, 0).UTC())
	o.Correlation.MessageID = "mc8"
	o.Attrs = map[string]any{"model": "unknown", "cache_read_in": int64(100)}

	g := NewGraphWithPricer(p)
	g.Apply(o)

	n := g.Nodes[model.AssistantTurnID(execID, "mc8")]
	assert.Nil(t, n.CostUSD)
	assert.Equal(t, "unpriced", n.Attrs["cost_source"])
}

func TestAssistantTurnPricedThenUnpriceableKeepsEstimated(t *testing.T) {
	p := PricerFunc(func(in PriceInputs) (PriceResult, bool) {
		if in.ModelID != "model-x" {
			return PriceResult{}, false
		}
		return PriceResult{USD: 1.5, Source: "estimated"}, true
	})
	t0 := time.Unix(0, 0).UTC()
	priced := ob("assistant_turn", "", t0)
	priced.Correlation.MessageID = "mc9"
	priced.Attrs = map[string]any{"model": "model-x", "tokens_in": int64(1000)}
	unpriceable := ob("assistant_turn", "", t0.Add(time.Second))
	unpriceable.Correlation.MessageID = "mc9"
	unpriceable.Attrs = map[string]any{"model": "unknown", "tokens_in": int64(1000)}

	g := NewGraphWithPricer(p)
	g.ApplyAll([]model.Observation{priced, unpriceable})
	rg := NewGraphWithPricer(p)
	rg.ApplyAll([]model.Observation{unpriceable, priced})

	for _, gr := range []*Graph{g, rg} {
		n := gr.Nodes[model.AssistantTurnID(execID, "mc9")]
		require.NotNil(t, n.CostUSD)
		assert.InDelta(t, 1.5, *n.CostUSD, 1e-9)
		assert.Equal(t, "estimated", n.Attrs["cost_source"])
	}
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

func TestEnsureRunModelIDLowestSeqWinsInAnyArrivalOrder(t *testing.T) {
	t0 := time.Unix(0, 0).UTC()
	first := ob("assistant_turn", "", t0)
	first.Correlation.MessageID = "mc7a"
	first.Attrs = map[string]any{"model": "claude-opus-4-8"}
	first.Seq = 1
	second := ob("assistant_turn", "", t0.Add(time.Second))
	second.Correlation.MessageID = "mc7b"
	second.Attrs = map[string]any{"model": "claude-sonnet-4-6"}
	second.Seq = 2
	third := ob("assistant_turn", "", t0.Add(2*time.Second))
	third.Correlation.MessageID = "mc7c"
	third.Attrs = map[string]any{"model": "claude-haiku-4-5"}
	third.Seq = 3

	for i, p := range permute([]model.Observation{first, second, third}) {
		g := NewGraph()
		g.ApplyAll(p)
		r := g.Runs[runID]
		require.NotNil(t, r)
		assert.Equal(t, "claude-opus-4-8", r.ModelID,
			"the run model is the one on the lowest-seq observation carrying it, whatever order they arrive in (permutation %d)", i)
	}
}

func TestEnsureRunReproLowestSeqWinsInAnyArrivalOrder(t *testing.T) {
	reproObs := func(seq uint64, version, cwd string) model.Observation {
		o := ob("assistant_turn", "", time.Unix(int64(seq), 0).UTC())
		o.Correlation.MessageID = "mrepro" + strconv.FormatUint(seq, 10)
		o.Attrs = map[string]any{"claude_code_version": version, "cwd": cwd}
		o.Seq = seq
		return o
	}
	obs := []model.Observation{
		reproObs(1, "1.2.3", "/project"),
		reproObs(2, "9.9.9", "/elsewhere"),
		reproObs(3, "0.0.1", "/third"),
	}

	for i, p := range permute(obs) {
		g := NewGraph()
		g.ApplyAll(p)
		r := g.Runs[runID]
		require.NotNil(t, r.Repro)
		assert.Equal(t, "1.2.3", r.Repro.ClaudeCodeVersion, "permutation %d", i)
		assert.Equal(t, "/project", r.Repro.Cwd, "permutation %d", i)
	}
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
		ObsID: "o1", RunID: "s1", ExecutionID: "e1", Source: model.SourceJSONL,
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
		ObsID: "o1", RunID: "s1", ExecutionID: "e1", Source: model.SourceJSONL,
		Kind: "assistant_turn", Correlation: model.Correlation{SessionID: "s1", MessageID: "m1"},
		Payload:   &model.Payload{Output: json.RawMessage(`"keep me"`)},
		EventTime: time.Unix(1, 0).UTC(), Seq: 1,
	}
	first.Payload.Hash = model.HashPayload(first.Payload)
	second := model.Observation{
		ObsID: "o2", RunID: "s1", ExecutionID: "e1", Source: model.SourceJSONL,
		Kind: "assistant_turn", Correlation: model.Correlation{SessionID: "s1", MessageID: "m1"},
		EventTime: time.Unix(2, 0).UTC(), Seq: 2,
	}
	g.ApplyAll([]model.Observation{first, second})
	n := g.Nodes[model.AssistantTurnID("e1", "m1")]
	require.NotNil(t, n.Payload)
	assert.JSONEq(t, `"keep me"`, string(n.Payload.Output))
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

	g.Apply(promptObs("u1", 1))
	prompt := model.UserPromptID("e1", "u1")
	assert.NotContains(t, g.Edges, model.EdgeID("e1", model.EdgeParentChild, model.SessionNodeID("e1"), turn))
	assert.Contains(t, g.Edges, model.EdgeID("e1", model.EdgeParentChild, prompt, turn))
}

func TestLatePromptReparentsTurnFromEarlierPrompt(t *testing.T) {
	g := NewGraph()
	g.ApplyAll([]model.Observation{
		promptObs("u1", 1),
		turnObs("m1", 4),
	})
	turn := model.AssistantTurnID("e1", "m1")
	require.Contains(t, g.Edges, model.EdgeID("e1", model.EdgeParentChild, model.UserPromptID("e1", "u1"), turn))

	g.Apply(promptObs("u2", 3))
	assert.NotContains(t, g.Edges, model.EdgeID("e1", model.EdgeParentChild, model.UserPromptID("e1", "u1"), turn))
	assert.Contains(t, g.Edges, model.EdgeID("e1", model.EdgeParentChild, model.UserPromptID("e1", "u2"), turn))
}

func TestLatePromptAfterTurnDoesNotReparentEarlierTurn(t *testing.T) {
	g := NewGraph()
	g.ApplyAll([]model.Observation{
		promptObs("u1", 1),
		turnObs("m1", 2),
	})
	turn := model.AssistantTurnID("e1", "m1")

	g.Apply(promptObs("u2", 5))
	assert.Contains(t, g.Edges, model.EdgeID("e1", model.EdgeParentChild, model.UserPromptID("e1", "u1"), turn))
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

	g.Apply(subagentStopObs("toolu_agent", "researcher", "desc", 4))

	toolEdge := pcEdge(model.ToolCallID("e1", "toolu_agent"), sub)
	assert.Contains(t, g.Edges, toolEdge)
	assert.NotContains(t, g.Edges, sessionEdge)

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

	g.Apply(subagentStopObs("", "", "", 4))

	assert.Contains(t, g.Edges, toolEdge)
	assert.NotContains(t, g.Edges, pcEdge(model.SessionNodeID("e1"), sub))

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

	g.Apply(agentPromptObs("u1", "ag1", 1))

	prompt := model.UserPromptID("e1", "u1")
	assert.Contains(t, g.Edges, pcEdge(prompt, turn))
	assert.NotContains(t, g.Edges, subEdge)
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

	m1EdgeID := pcEdge(model.UserPromptID("e1", "u1"), model.AssistantTurnID("e1", "m1"))
	revBeforeM1 := g.Edges[m1EdgeID].Rev

	g.Apply(promptObs("u2", 20))

	assert.Contains(t, g.Edges, pcEdge(model.UserPromptID("e1", "u1"), model.AssistantTurnID("e1", "m1")))
	assert.Contains(t, g.Edges, pcEdge(model.UserPromptID("e1", "u1"), model.AssistantTurnID("e1", "m2")))
	assert.Contains(t, g.Edges, pcEdge(model.UserPromptID("e1", "u2"), model.AssistantTurnID("e1", "m3")))
	assert.NotContains(t, g.Edges, pcEdge(model.UserPromptID("e1", "u1"), model.AssistantTurnID("e1", "m3")))

	assert.Equal(t, revBeforeM1, g.Edges[m1EdgeID].Rev, "m1 edge Rev must not change when turn is not in affected interval")
}

func TestTurnSeqDecreaseRepositionsAndReparents(t *testing.T) {
	g := NewGraph()
	g.ApplyAll([]model.Observation{
		promptObs("u1", 5),
		turnObs("m2", 7),
		turnObs("m1", 10),
	})
	require.Contains(t, g.Edges, pcEdge(model.UserPromptID("e1", "u1"), model.AssistantTurnID("e1", "m1")))

	g.Apply(turnObs("m1", 3))

	turn := model.AssistantTurnID("e1", "m1")
	assert.Contains(t, g.Edges, pcEdge(model.SessionNodeID("e1"), turn))
	assert.NotContains(t, g.Edges, pcEdge(model.UserPromptID("e1", "u1"), turn))

	g.Apply(promptObs("u2", 8))
	assert.Contains(t, g.Edges, pcEdge(model.SessionNodeID("e1"), turn))
	assert.NotContains(t, g.Edges, pcEdge(model.UserPromptID("e1", "u2"), turn))

	g.Apply(promptObs("u0", 1))
	u0 := model.UserPromptID("e1", "u0")
	assert.Contains(t, g.Edges, pcEdge(u0, turn), "turn must reparent to prompt before it after repositionTurn")
	assert.NotContains(t, g.Edges, pcEdge(model.SessionNodeID("e1"), turn))
}

func TestEnsureRunCwdWithoutVersionInitsRepro(t *testing.T) {
	g := NewGraph()
	g.ApplyAll([]model.Observation{{
		ObsID: "o1", RunID: "s1", ExecutionID: "e1", Source: model.SourceJSONL,
		Kind: "assistant_turn", Correlation: model.Correlation{SessionID: "s1", MessageID: "m1"},
		Attrs: map[string]any{"cwd": "/work"}, Seq: 1,
	}})
	r := g.Runs["s1"]
	require.NotNil(t, r)
	require.NotNil(t, r.Repro)
	assert.Equal(t, "/work", r.Repro.Cwd)
}

func TestBareRunHasNoReproField(t *testing.T) {
	g := NewGraph()
	g.Apply(toolObs("e1", "s1", "t1", "Bash", "running", 1))
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
		Kind:        "assistant_turn",
		Correlation: model.Correlation{SessionID: "s1", MessageID: "m1"},
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

func TestRunStartedRunIsRunning(t *testing.T) {
	g := NewGraph()
	g.Apply(toolObs("e1", "s1", "t1", "Bash", "running", 1))
	r := g.Runs["s1"]
	require.NotNil(t, r)
	assert.Equal(t, "s1", r.ID)
	assert.Equal(t, model.StatusRunning, r.Status)
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
	assert.Equal(t, "abc", nodeKey("abc", "obs1"))
	assert.Equal(t, "obs:obs1", nodeKey("", "obs1"))
}

func TestToolObsFallbackDistinctObsIDs(t *testing.T) {
	t0 := time.Unix(0, 0).UTC()
	o1 := model.Observation{
		ObsID: "o1", RunID: runID, ExecutionID: execID,
		Source: model.SourceJSONL, Kind: "assistant_tool_use",
		Correlation: model.Correlation{SessionID: runID, ToolUseID: ""},
		Attrs:       map[string]any{"name": "Bash"},
		EventTime:   t0, ObservedAt: t0, Seq: 1,
	}
	o2 := model.Observation{
		ObsID: "o2", RunID: runID, ExecutionID: execID,
		Source: model.SourceJSONL, Kind: "assistant_tool_use",
		Correlation: model.Correlation{SessionID: runID, ToolUseID: ""},
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

func TestTurnNoMessageCollapsesToOneNode(t *testing.T) {
	t0 := time.Unix(0, 0).UTC()
	o1 := model.Observation{
		ObsID: "ob1", RunID: runID, ExecutionID: execID,
		Source: model.SourceJSONL, Kind: "assistant_turn",
		Correlation: model.Correlation{SessionID: runID, MessageID: ""},
		Attrs:       map[string]any{},
		EventTime:   t0, ObservedAt: t0, Seq: 1,
	}
	o2 := model.Observation{
		ObsID: "ob2", RunID: runID, ExecutionID: execID,
		Source: model.SourceJSONL, Kind: "assistant_turn",
		Correlation: model.Correlation{SessionID: runID, MessageID: ""},
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
		Source: model.SourceJSONL, Kind: "user_prompt",
		Correlation: model.Correlation{SessionID: runID, UUID: ""},
		Attrs:       map[string]any{},
		EventTime:   t0, ObservedAt: t0, Seq: 1,
	}
	o2 := model.Observation{
		ObsID: "po2", RunID: runID, ExecutionID: execID,
		Source: model.SourceJSONL, Kind: "user_prompt",
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
