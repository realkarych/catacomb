package daemon

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/cdc"
	"github.com/realkarych/catacomb/model"
)

func i64(v int64) *int64     { return &v }
func f64(v float64) *float64 { return &v }

func seedSubagentGraph(t *testing.T, d *Daemon) {
	t.Helper()
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Task","tool_use_id":"toolu_agent","tool_input":{}}`)))

	d.mu.Lock()
	defer d.mu.Unlock()
	g := d.graphs["exec1"]

	sub := model.SubagentID("exec1", "ag1")
	g.Nodes[sub] = &model.Node{ID: sub, RunID: "s1", Type: model.NodeSubagent, AgentID: "ag1", SubagentType: "researcher", Rev: 10}

	prompt := model.UserPromptID("exec1", "u-inner")
	g.Nodes[prompt] = &model.Node{ID: prompt, RunID: "s1", Type: model.NodeUserPrompt, AgentID: "ag1", Rev: 11}

	turn := model.AssistantTurnID("exec1", "m-inner")
	g.Nodes[turn] = &model.Node{ID: turn, RunID: "s1", Type: model.NodeAssistantTurn, AgentID: "ag1", Rev: 12, TokensIn: i64(100), TokensOut: i64(40), CostUSD: f64(0.5)}

	tool := model.ToolCallID("exec1", "toolu_inner")
	g.Nodes[tool] = &model.Node{ID: tool, RunID: "s1", Type: model.NodeToolCall, AgentID: "ag1", Rev: 13, TokensIn: i64(7), TokensOut: i64(3), CostUSD: f64(0.25)}

	toolCall := model.ToolCallID("exec1", "toolu_agent")
	g.Edges["e-agent"] = &model.Edge{ID: "e-agent", RunID: "s1", Type: model.EdgeParentChild, Src: toolCall, Dst: sub, Rev: 10}
	g.Edges["e-sub-prompt"] = &model.Edge{ID: "e-sub-prompt", RunID: "s1", Type: model.EdgeParentChild, Src: sub, Dst: prompt, Rev: 11}
	g.Edges["e-prompt-turn"] = &model.Edge{ID: "e-prompt-turn", RunID: "s1", Type: model.EdgeParentChild, Src: prompt, Dst: turn, Rev: 12}
	g.Edges["e-turn-tool"] = &model.Edge{ID: "e-turn-tool", RunID: "s1", Type: model.EdgeParentChild, Src: turn, Dst: tool, Rev: 13}
}

func TestIsInnerNode(t *testing.T) {
	sub := &model.Node{ID: model.SubagentID("exec1", "ag1"), AgentID: "ag1", Type: model.NodeSubagent}
	assert.False(t, isInnerNode("exec1", sub), "subagent node itself is not inner")

	inner := &model.Node{ID: model.UserPromptID("exec1", "u1"), AgentID: "ag1", Type: model.NodeUserPrompt}
	assert.True(t, isInnerNode("exec1", inner), "agent-scoped prompt is inner")

	top := &model.Node{ID: model.ToolCallID("exec1", "t1"), Type: model.NodeToolCall}
	assert.False(t, isInnerNode("exec1", top), "top-level node has no agent")

	session := &model.Node{ID: model.SessionNodeID("exec1"), Type: model.NodeSession}
	assert.False(t, isInnerNode("exec1", session))
}

func TestSessionGraphOmitsInnerNodes(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	seedSubagentGraph(t, d)

	d.mu.Lock()
	evs, err := d.sessionGraphDeltas("s1")
	d.mu.Unlock()
	require.NoError(t, err)

	got := map[string]bool{}
	for _, ev := range evs {
		if ev.Node != nil {
			got[ev.Node.ID] = true
		}
	}
	assert.True(t, got[model.SessionNodeID("exec1")], "session spine kept")
	assert.True(t, got[model.ToolCallID("exec1", "toolu_agent")], "agent tool call kept")
	assert.True(t, got[model.SubagentID("exec1", "ag1")], "subagent node kept")
	assert.False(t, got[model.UserPromptID("exec1", "u-inner")], "inner prompt omitted")
	assert.False(t, got[model.AssistantTurnID("exec1", "m-inner")], "inner turn omitted")
	assert.False(t, got[model.ToolCallID("exec1", "toolu_inner")], "inner tool omitted")
}

func TestSessionGraphOmitsInnerEdges(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	seedSubagentGraph(t, d)

	d.mu.Lock()
	evs, err := d.sessionGraphDeltas("s1")
	d.mu.Unlock()
	require.NoError(t, err)

	edges := map[string]bool{}
	for _, ev := range evs {
		if ev.Edge != nil {
			edges[ev.Edge.ID] = true
		}
	}
	assert.True(t, edges["e-agent"], "tool->subagent edge kept")
	assert.False(t, edges["e-sub-prompt"], "subagent->inner edge omitted")
	assert.False(t, edges["e-prompt-turn"], "inner->inner edge omitted")
	assert.False(t, edges["e-turn-tool"], "inner->inner edge omitted")
}

func TestSessionGraphSubagentAggregate(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	seedSubagentGraph(t, d)

	d.mu.Lock()
	evs, err := d.sessionGraphDeltas("s1")
	d.mu.Unlock()
	require.NoError(t, err)

	var sub *model.Node
	for _, ev := range evs {
		if ev.Node != nil && ev.Node.ID == model.SubagentID("exec1", "ag1") {
			sub = ev.Node
		}
	}
	require.NotNil(t, sub)
	require.NotNil(t, sub.Attrs)
	assert.Equal(t, 3, sub.Attrs["descendant_count"])
	assert.Equal(t, int64(107), sub.Attrs["descendant_tokens_in"])
	assert.Equal(t, int64(43), sub.Attrs["descendant_tokens_out"])
	assert.InDelta(t, 0.75, sub.Attrs["descendant_cost_usd"], 1e-9)
}

func TestSessionGraphAggregateDoesNotMutateLiveGraph(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	seedSubagentGraph(t, d)

	d.mu.Lock()
	_, err := d.sessionGraphDeltas("s1")
	live := d.graphs["exec1"].Nodes[model.SubagentID("exec1", "ag1")]
	d.mu.Unlock()
	require.NoError(t, err)
	_, has := live.Attrs["descendant_count"]
	assert.False(t, has, "live graph node must not be mutated")
}

func TestSessionGraphNoSubagentUnchanged(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_input":{}}`)))
	require.NoError(t, d.Ingest("PostToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_response":{}}`)))

	d.mu.Lock()
	g := d.graphs["exec1"]
	nodes, edges := g.Snapshot()
	evs, err := d.sessionGraphDeltas("s1")
	d.mu.Unlock()
	require.NoError(t, err)

	nodeCount, edgeCount := 0, 0
	for _, ev := range evs {
		if ev.Node != nil {
			nodeCount++
			assert.NotContains(t, ev.Node.Attrs, "descendant_count")
		}
		if ev.Edge != nil {
			edgeCount++
		}
	}
	assert.Equal(t, len(nodes), nodeCount, "every node emitted when no subagents")
	assert.Equal(t, len(edges), edgeCount, "every edge emitted when no subagents")
}

func TestSubscribeSnapshotOmitsInnerNodes(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	seedSubagentGraph(t, d)

	sub := d.SubscribeFiltered(SubFilter{SessionID: "s1"}, 64)
	defer d.Unsubscribe(sub)

	nodes := map[string]bool{}
	var subNode *model.Node
	for _, delta := range sub.Snapshot {
		if delta.Node != nil {
			nodes[delta.Node.ID] = true
			if delta.Node.ID == model.SubagentID("exec1", "ag1") {
				subNode = delta.Node
			}
		}
	}
	assert.True(t, nodes[model.SubagentID("exec1", "ag1")])
	assert.False(t, nodes[model.UserPromptID("exec1", "u-inner")])
	require.NotNil(t, subNode)
	assert.Equal(t, 3, subNode.Attrs["descendant_count"])
}

func TestMatchDropsInnerNodeDelta(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	seedSubagentGraph(t, d)

	sub := d.SubscribeFiltered(SubFilter{SessionID: "s1"}, 64)
	defer d.Unsubscribe(sub)

	inner := &model.Node{ID: model.UserPromptID("exec1", "u-inner"), RunID: "s1", Type: model.NodeUserPrompt, AgentID: "ag1"}
	innerDelta := cdc.GraphDelta{Kind: cdc.DeltaNodeUpsert, ExecutionID: "exec1", RunID: "s1", Node: inner, Rev: 20}
	_, keep := sub.transform(innerDelta)
	assert.False(t, keep, "inner node delta must be dropped")
}

func TestMatchPassesSpineDelta(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	seedSubagentGraph(t, d)

	sub := d.SubscribeFiltered(SubFilter{SessionID: "s1"}, 64)
	defer d.Unsubscribe(sub)

	spine := &model.Node{ID: model.ToolCallID("exec1", "toolu_agent"), RunID: "s1", Type: model.NodeToolCall}
	spineDelta := cdc.GraphDelta{Kind: cdc.DeltaNodeUpsert, ExecutionID: "exec1", RunID: "s1", Node: spine, Rev: 21}
	out, keep := sub.transform(spineDelta)
	assert.True(t, keep, "spine delta must pass")
	assert.Equal(t, model.ToolCallID("exec1", "toolu_agent"), out.Node.ID)
}

func TestMatchSubagentDeltaCarriesAggregate(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	seedSubagentGraph(t, d)

	sub := d.SubscribeFiltered(SubFilter{SessionID: "s1"}, 64)
	defer d.Unsubscribe(sub)

	subID := model.SubagentID("exec1", "ag1")
	subDelta := cdc.GraphDelta{
		Kind:        cdc.DeltaNodeUpsert,
		ExecutionID: "exec1",
		RunID:       "s1",
		Node:        &model.Node{ID: subID, RunID: "s1", Type: model.NodeSubagent, AgentID: "ag1"},
		Rev:         22,
	}
	out, keep := sub.transform(subDelta)
	require.True(t, keep)
	require.NotNil(t, out.Node.Attrs)
	assert.Equal(t, 3, out.Node.Attrs["descendant_count"])
}

func TestMatchSubagentDeltaDoesNotMutateBusCopy(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	seedSubagentGraph(t, d)

	sub := d.SubscribeFiltered(SubFilter{SessionID: "s1"}, 64)
	defer d.Unsubscribe(sub)

	subID := model.SubagentID("exec1", "ag1")
	shared := &model.Node{ID: subID, RunID: "s1", Type: model.NodeSubagent, AgentID: "ag1"}
	subDelta := cdc.GraphDelta{Kind: cdc.DeltaNodeUpsert, ExecutionID: "exec1", RunID: "s1", Node: shared, Rev: 23}
	_, keep := sub.transform(subDelta)
	require.True(t, keep)
	_, mutated := shared.Attrs["descendant_count"]
	assert.False(t, mutated, "the bus-shared node must not be mutated in place")
}

func TestMatchDropsInnerEdgeDelta(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	seedSubagentGraph(t, d)

	sub := d.SubscribeFiltered(SubFilter{SessionID: "s1"}, 64)
	defer d.Unsubscribe(sub)

	innerEdge := &model.Edge{ID: "e-prompt-turn", RunID: "s1", Type: model.EdgeParentChild, Src: model.UserPromptID("exec1", "u-inner"), Dst: model.AssistantTurnID("exec1", "m-inner")}
	delta := cdc.GraphDelta{Kind: cdc.DeltaEdgeUpsert, ExecutionID: "exec1", RunID: "s1", Edge: innerEdge, Rev: 24}
	_, keep := sub.transform(delta)
	assert.False(t, keep, "edge between inner nodes must be dropped")
}

func TestMatchKeepsAgentToolEdgeDelta(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	seedSubagentGraph(t, d)

	sub := d.SubscribeFiltered(SubFilter{SessionID: "s1"}, 64)
	defer d.Unsubscribe(sub)

	edge := &model.Edge{ID: "e-agent", RunID: "s1", Type: model.EdgeParentChild, Src: model.ToolCallID("exec1", "toolu_agent"), Dst: model.SubagentID("exec1", "ag1")}
	delta := cdc.GraphDelta{Kind: cdc.DeltaEdgeUpsert, ExecutionID: "exec1", RunID: "s1", Edge: edge, Rev: 25}
	_, keep := sub.transform(delta)
	assert.True(t, keep, "tool->subagent edge must pass")
}

func TestMatchEdgeDeleteOfInnerDropped(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	seedSubagentGraph(t, d)

	sub := d.SubscribeFiltered(SubFilter{SessionID: "s1"}, 64)
	defer d.Unsubscribe(sub)

	edge := &model.Edge{ID: "e-prompt-turn", RunID: "s1", Type: model.EdgeParentChild, Src: model.UserPromptID("exec1", "u-inner"), Dst: model.AssistantTurnID("exec1", "m-inner")}
	delta := cdc.GraphDelta{Kind: cdc.DeltaEdgeDelete, ExecutionID: "exec1", RunID: "s1", Edge: edge, Rev: 26}
	_, keep := sub.transform(delta)
	assert.False(t, keep, "edge_delete for inner edge must be dropped")
}

func TestMatchLifecycleDeltaPasses(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	seedSubagentGraph(t, d)

	sub := d.SubscribeFiltered(SubFilter{SessionID: "s1"}, 64)
	defer d.Unsubscribe(sub)

	delta := cdc.GraphDelta{Kind: cdc.DeltaRunEnded, ExecutionID: "exec1", RunID: "s1", Rev: 27}
	_, keep := sub.transform(delta)
	assert.True(t, keep)
}

func TestMatchOutOfSessionDropped(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	seedSubagentGraph(t, d)

	sub := d.SubscribeFiltered(SubFilter{SessionID: "s1"}, 64)
	defer d.Unsubscribe(sub)

	delta := cdc.GraphDelta{Kind: cdc.DeltaNodeUpsert, ExecutionID: "exec2", RunID: "s2", Node: &model.Node{ID: "x", Type: model.NodeSession}}
	_, keep := sub.transform(delta)
	assert.False(t, keep)
}

func TestSubagentAggregateUnknownAgentZero(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	seedSubagentGraph(t, d)

	d.mu.Lock()
	g := d.graphs["exec1"]
	agg := subagentAggregate(g, "exec1", "ghost")
	d.mu.Unlock()
	assert.Equal(t, 0, agg.count)
	assert.False(t, agg.hasCost)
}

func TestSubagentAggregateNilGraph(t *testing.T) {
	agg := subagentAggregate(nil, "exec1", "ag1")
	assert.Equal(t, 0, agg.count)
}

func TestChildlessSubagentAggregateZero(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))

	d.mu.Lock()
	g := d.graphs["exec1"]
	sub := model.SubagentID("exec1", "lonely")
	g.Nodes[sub] = &model.Node{ID: sub, RunID: "s1", Type: model.NodeSubagent, AgentID: "lonely", Rev: 5}
	evs, err := d.sessionGraphDeltas("s1")
	d.mu.Unlock()
	require.NoError(t, err)

	var node *model.Node
	for _, ev := range evs {
		if ev.Node != nil && ev.Node.ID == sub {
			node = ev.Node
		}
	}
	require.NotNil(t, node)
	assert.Equal(t, 0, node.Attrs["descendant_count"])
	_, hasCost := node.Attrs["descendant_cost_usd"]
	assert.False(t, hasCost, "no cost attr when subagent has no inner nodes")
}

func TestTransformEdgeNilEdgePasses(t *testing.T) {
	d := New(tempStore(t))
	sub := d.SubscribeFiltered(SubFilter{}, 64)
	defer d.Unsubscribe(sub)

	delta := cdc.GraphDelta{Kind: cdc.DeltaEdgeUpsert, ExecutionID: "exec1", RunID: "s1"}
	_, keep := sub.transform(delta)
	assert.True(t, keep, "edge delta with nil edge passes through")
}

func TestTransformNilNodePasses(t *testing.T) {
	d := New(tempStore(t))
	sub := d.SubscribeFiltered(SubFilter{}, 64)
	defer d.Unsubscribe(sub)

	delta := cdc.GraphDelta{Kind: cdc.DeltaNodeUpsert, ExecutionID: "exec1", RunID: "s1"}
	_, keep := sub.transform(delta)
	assert.True(t, keep, "node delta with nil node passes through")
}

func TestTransformEdgeMissingGraphNotInner(t *testing.T) {
	d := New(tempStore(t))
	sub := d.SubscribeFiltered(SubFilter{}, 64)
	defer d.Unsubscribe(sub)

	edge := &model.Edge{ID: "e", RunID: "s1", Type: model.EdgeParentChild, Src: "a", Dst: "b"}
	delta := cdc.GraphDelta{Kind: cdc.DeltaEdgeUpsert, ExecutionID: "ghost-exec", RunID: "s1", Edge: edge}
	_, keep := sub.transform(delta)
	assert.True(t, keep, "edge for a missing graph is treated as not-inner")
}

func TestTransformSubagentDeltaMissingGraphZero(t *testing.T) {
	d := New(tempStore(t))
	sub := d.SubscribeFiltered(SubFilter{}, 64)
	defer d.Unsubscribe(sub)

	node := &model.Node{ID: model.SubagentID("ghost-exec", "ag1"), RunID: "s1", Type: model.NodeSubagent, AgentID: "ag1"}
	delta := cdc.GraphDelta{Kind: cdc.DeltaNodeUpsert, ExecutionID: "ghost-exec", RunID: "s1", Node: node}
	out, keep := sub.transform(delta)
	require.True(t, keep)
	assert.Equal(t, 0, out.Node.Attrs["descendant_count"])
}

func TestSubagentSubtreeDeltasReturnsInnerNodes(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	seedSubagentGraph(t, d)

	d.mu.Lock()
	evs, err := d.subagentSubtreeDeltas("s1", "ag1")
	d.mu.Unlock()
	require.NoError(t, err)

	nodeIDs := map[string]bool{}
	edgeIDs := map[string]bool{}
	for _, ev := range evs {
		if ev.Node != nil {
			nodeIDs[ev.Node.ID] = true
		}
		if ev.Edge != nil {
			edgeIDs[ev.Edge.ID] = true
		}
	}

	assert.True(t, nodeIDs[model.UserPromptID("exec1", "u-inner")])
	assert.True(t, nodeIDs[model.AssistantTurnID("exec1", "m-inner")])
	assert.True(t, nodeIDs[model.ToolCallID("exec1", "toolu_inner")])

	assert.False(t, nodeIDs[model.SubagentID("exec1", "ag1")])
	assert.False(t, nodeIDs[model.SessionNodeID("exec1")])
	assert.False(t, nodeIDs[model.ToolCallID("exec1", "toolu_agent")])

	assert.True(t, edgeIDs["e-sub-prompt"])
	assert.True(t, edgeIDs["e-prompt-turn"])
	assert.True(t, edgeIDs["e-turn-tool"])

	assert.False(t, edgeIDs["e-agent"])
}

func TestSubagentSubtreeDeltasUnknownSession(t *testing.T) {
	d := New(tempStore(t))
	d.mu.Lock()
	_, err := d.subagentSubtreeDeltas("ghost", "ag1")
	d.mu.Unlock()
	assert.True(t, errors.Is(err, ErrSessionNotFound))
}

func TestSubagentSubtreeDeltasUnknownAgent(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	seedSubagentGraph(t, d)

	d.mu.Lock()
	evs, err := d.subagentSubtreeDeltas("s1", "ghost")
	d.mu.Unlock()
	require.NoError(t, err)
	assert.Empty(t, evs)
}

func TestSubagentSubtreeDeltasExcludesOtherAgent(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	seedSubagentGraph(t, d)

	d.mu.Lock()
	g := d.graphs["exec1"]
	sub2 := model.SubagentID("exec1", "ag2")
	g.Nodes[sub2] = &model.Node{ID: sub2, RunID: "s1", Type: model.NodeSubagent, AgentID: "ag2", Rev: 20}
	inner2 := model.UserPromptID("exec1", "u-ag2")
	g.Nodes[inner2] = &model.Node{ID: inner2, RunID: "s1", Type: model.NodeUserPrompt, AgentID: "ag2", Rev: 21}

	evs, err := d.subagentSubtreeDeltas("s1", "ag1")
	d.mu.Unlock()
	require.NoError(t, err)

	nodeIDs := map[string]bool{}
	for _, ev := range evs {
		if ev.Node != nil {
			nodeIDs[ev.Node.ID] = true
		}
	}

	assert.False(t, nodeIDs[sub2])
	assert.False(t, nodeIDs[inner2])
	assert.True(t, nodeIDs[model.UserPromptID("exec1", "u-inner")])
}

func TestSubagentSubtreeDeltasPayloadNil(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	seedSubagentGraph(t, d)

	d.mu.Lock()
	g := d.graphs["exec1"]
	innerID := model.UserPromptID("exec1", "u-inner")
	g.Nodes[innerID].Payload = &model.Payload{Input: []byte(`"hi"`)}
	evs, err := d.subagentSubtreeDeltas("s1", "ag1")
	d.mu.Unlock()
	require.NoError(t, err)

	for _, ev := range evs {
		if ev.Node != nil {
			assert.Nil(t, ev.Node.Payload)
		}
	}
}

func TestSubagentSubtreeDeltasDoesNotMutateLive(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	seedSubagentGraph(t, d)

	d.mu.Lock()
	g := d.graphs["exec1"]
	innerID := model.UserPromptID("exec1", "u-inner")
	g.Nodes[innerID].Payload = &model.Payload{Input: []byte(`"hi"`)}
	_, err := d.subagentSubtreeDeltas("s1", "ag1")
	live := g.Nodes[innerID]
	d.mu.Unlock()
	require.NoError(t, err)
	assert.NotNil(t, live.Payload)
}

func TestSubagentSubtreeHandlerOK(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	seedSubagentGraph(t, d)
	srv := httptest.NewServer(d.Handler("tok"))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/v1/sessions/s1/subagent/ag1?token=tok")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")

	var evs []sseEvent
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&evs))
	assert.NotEmpty(t, evs)
}

func TestSubagentSubtreeHandlerUnknownSession404(t *testing.T) {
	d := New(tempStore(t))
	srv := httptest.NewServer(d.Handler("tok"))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/v1/sessions/ghost/subagent/ag1?token=tok")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestSubagentSubtreeHandlerUnknownAgent200Empty(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	seedSubagentGraph(t, d)
	srv := httptest.NewServer(d.Handler("tok"))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/v1/sessions/s1/subagent/ghost?token=tok")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var evs []sseEvent
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&evs))
	assert.Empty(t, evs)
}

func TestSubagentSubtreeHandlerAuthRejected(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	srv := httptest.NewServer(d.Handler("tok"))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/v1/sessions/s1/subagent/ag1")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestSubagentSubtreeHandlerBearerAuth(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	seedSubagentGraph(t, d)
	srv := httptest.NewServer(d.Handler("tok"))
	t.Cleanup(srv.Close)

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/v1/sessions/s1/subagent/ag1", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer tok")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}
