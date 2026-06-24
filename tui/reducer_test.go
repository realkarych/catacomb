package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func nodeEv(kind, id, typ, status string, rev uint64) SseEvent {
	return SseEvent{Kind: kind, Rev: rev, Node: &Node{ID: id, RunID: "r", Type: typ, Status: status, Rev: rev}}
}

func TestApplyStatusBeforeUpsertNotClobbered(t *testing.T) {
	g := EmptyGraph()
	Apply(&g, nodeEv("node_status", "n1", "", "ok", 2))
	Apply(&g, nodeEv("node_upsert", "n1", "tool_call", "", 1))
	got := g.Nodes["n1"]
	assert.Equal(t, "tool_call", got.Type)
	assert.Equal(t, "ok", got.Status)
}

func TestApplyDeterministicAnyOrder(t *testing.T) {
	evs := []SseEvent{
		nodeEv("node_upsert", "n1", "session", "running", 1),
		nodeEv("node_upsert", "n2", "tool_call", "ok", 2),
		nodeEv("node_status", "n2", "", "error", 4),
		{Kind: "edge_upsert", Rev: 3, Edge: &Edge{ID: "e1", RunID: "r", Type: "parent_child", Src: "n1", Dst: "n2", Rev: 3}},
	}
	perms := [][]int{{0, 1, 2, 3}, {3, 2, 1, 0}, {2, 0, 3, 1}, {1, 3, 0, 2}}
	var first map[string]Node
	for _, p := range perms {
		g := EmptyGraph()
		for _, i := range p {
			Apply(&g, evs[i])
		}
		if first == nil {
			first = g.Nodes
			continue
		}
		require.Equal(t, len(first), len(g.Nodes))
		for id, n := range first {
			assert.Equal(t, n.Type, g.Nodes[id].Type, id)
			assert.Equal(t, n.Status, g.Nodes[id].Status, id)
		}
		_, ok := g.Edges["e1"]
		assert.True(t, ok)
	}
}

func TestApplyEdgeDeleteAndMerge(t *testing.T) {
	g := EmptyGraph()
	Apply(&g, SseEvent{Kind: "edge_upsert", Rev: 1, Edge: &Edge{ID: "e1", Src: "a", Dst: "b", Rev: 1}})
	Apply(&g, SseEvent{Kind: "edge_delete", Rev: 2, Edge: &Edge{ID: "e1", Rev: 2}})
	_, ok := g.Edges["e1"]
	assert.False(t, ok)
	Apply(&g, nodeEv("node_upsert", "old", "tool_call", "ok", 1))
	Apply(&g, SseEvent{Kind: "node_merge", Rev: 3, OldID: "old", Node: &Node{ID: "new", RunID: "r", Type: "tool_call", Rev: 3}})
	_, hadOld := g.Nodes["old"]
	_, hasNew := g.Nodes["new"]
	assert.False(t, hadOld)
	assert.True(t, hasNew)
}

func TestApplyTombstoneBlocksUpsert(t *testing.T) {
	g := EmptyGraph()
	Apply(&g, SseEvent{Kind: "node_merge", Rev: 10, OldID: "old", Node: &Node{ID: "new", RunID: "r", Rev: 10}})
	Apply(&g, nodeEv("node_upsert", "old", "tool_call", "ok", 5))
	_, ok := g.Nodes["old"]
	assert.False(t, ok)
}

func TestApplyEstablishedRevDrop(t *testing.T) {
	g := EmptyGraph()
	Apply(&g, nodeEv("node_upsert", "n1", "session", "ok", 5))
	Apply(&g, nodeEv("node_upsert", "n1", "tool_call", "error", 3))
	assert.Equal(t, "session", g.Nodes["n1"].Type)
	assert.Equal(t, "ok", g.Nodes["n1"].Status)
}

func TestApplyStatusOnExistingEstablished(t *testing.T) {
	g := EmptyGraph()
	Apply(&g, nodeEv("node_upsert", "n1", "session", "running", 5))
	Apply(&g, nodeEv("node_status", "n1", "", "ok", 10))
	assert.Equal(t, "ok", g.Nodes["n1"].Status)
	Apply(&g, nodeEv("node_status", "n1", "", "error", 3))
	assert.Equal(t, "ok", g.Nodes["n1"].Status)
}

func TestApplyEdgeRevDrop(t *testing.T) {
	g := EmptyGraph()
	Apply(&g, SseEvent{Kind: "edge_upsert", Rev: 5, Edge: &Edge{ID: "e1", Src: "a", Dst: "b", Rev: 5}})
	Apply(&g, SseEvent{Kind: "edge_upsert", Rev: 3, Edge: &Edge{ID: "e1", Src: "x", Dst: "y", Rev: 3}})
	assert.Equal(t, "a", g.Edges["e1"].Src)
}

func TestApplyEdgeTombstoneBlocksUpsert(t *testing.T) {
	g := EmptyGraph()
	Apply(&g, SseEvent{Kind: "edge_delete", Rev: 10, Edge: &Edge{ID: "e1", Rev: 10}})
	Apply(&g, SseEvent{Kind: "edge_upsert", Rev: 5, Edge: &Edge{ID: "e1", Src: "a", Dst: "b", Rev: 5}})
	_, ok := g.Edges["e1"]
	assert.False(t, ok)
}

func TestApplyNodeMergeSameID(t *testing.T) {
	g := EmptyGraph()
	Apply(&g, SseEvent{Kind: "node_merge", Rev: 5, OldID: "n1", Node: &Node{ID: "n1", RunID: "r", Type: "session", Rev: 5}})
	n, ok := g.Nodes["n1"]
	assert.True(t, ok)
	assert.Equal(t, "session", n.Type)
}

func TestApplyUnknownKind(t *testing.T) {
	g := EmptyGraph()
	Apply(&g, SseEvent{Kind: "mystery", Rev: 1})
	assert.Empty(t, g.Nodes)
	assert.Empty(t, g.Edges)
}

func TestApplyNodeStatusTombstoned(t *testing.T) {
	g := EmptyGraph()
	Apply(&g, SseEvent{Kind: "node_merge", Rev: 10, OldID: "old", Node: &Node{ID: "new", RunID: "r", Rev: 10}})
	Apply(&g, nodeEv("node_status", "old", "", "ok", 5))
	_, ok := g.Nodes["old"]
	assert.False(t, ok)
}

func TestApplyNodeUpsertNilNode(t *testing.T) {
	g := EmptyGraph()
	Apply(&g, SseEvent{Kind: "node_upsert", Rev: 1, Node: nil})
	assert.Empty(t, g.Nodes)
}

func TestApplyNodeStatusNilNode(t *testing.T) {
	g := EmptyGraph()
	Apply(&g, SseEvent{Kind: "node_status", Rev: 1, Node: nil})
	assert.Empty(t, g.Nodes)
}

func TestApplyNodeMergeNilNode(t *testing.T) {
	g := EmptyGraph()
	Apply(&g, SseEvent{Kind: "node_merge", Rev: 1, Node: nil})
	assert.Empty(t, g.Nodes)
}

func TestApplyEdgeUpsertNilEdge(t *testing.T) {
	g := EmptyGraph()
	Apply(&g, SseEvent{Kind: "edge_upsert", Rev: 1, Edge: nil})
	assert.Empty(t, g.Edges)
}

func TestApplyEdgeDeleteNilEdge(t *testing.T) {
	g := EmptyGraph()
	Apply(&g, SseEvent{Kind: "edge_delete", Rev: 1, Edge: nil})
	assert.Empty(t, g.Edges)
}

func TestApplyEdgeDeleteUpdatesTombstone(t *testing.T) {
	g := EmptyGraph()
	Apply(&g, SseEvent{Kind: "edge_delete", Rev: 5, Edge: &Edge{ID: "e1", Rev: 5}})
	Apply(&g, SseEvent{Kind: "edge_delete", Rev: 3, Edge: &Edge{ID: "e1", Rev: 3}})
	Apply(&g, SseEvent{Kind: "edge_upsert", Rev: 4, Edge: &Edge{ID: "e1", Src: "a", Dst: "b", Rev: 4}})
	_, ok := g.Edges["e1"]
	assert.False(t, ok)
}

func TestApplyStatusOnUnestablishedUpdatesRev(t *testing.T) {
	g := EmptyGraph()
	Apply(&g, nodeEv("node_status", "n1", "session", "running", 3))
	Apply(&g, nodeEv("node_status", "n1", "session", "ok", 7))
	assert.Equal(t, "ok", g.Nodes["n1"].Status)
	assert.Equal(t, uint64(7), g.Nodes["n1"].Rev)
}

func TestApplyNodeUpsertMergesStatusFromSeedWithHigherRev(t *testing.T) {
	g := EmptyGraph()
	tEnd := "2026-01-01T01:00:00Z"
	dur := int64(3600000)
	Apply(&g, SseEvent{Kind: "node_status", Rev: 10, Node: &Node{ID: "n1", RunID: "r", Status: "ok", TEnd: &tEnd, DurationMS: &dur, Rev: 10}})
	Apply(&g, nodeEv("node_upsert", "n1", "session", "", 5))
	n := g.Nodes["n1"]
	assert.Equal(t, "session", n.Type)
	assert.Equal(t, "ok", n.Status)
	require.NotNil(t, n.TEnd)
	assert.Equal(t, tEnd, *n.TEnd)
	require.NotNil(t, n.DurationMS)
	assert.Equal(t, dur, *n.DurationMS)
}

func TestApplyNodeMergeRewireEdges(t *testing.T) {
	g := EmptyGraph()
	Apply(&g, nodeEv("node_upsert", "old", "session", "running", 1))
	Apply(&g, SseEvent{Kind: "edge_upsert", Rev: 2, Edge: &Edge{ID: "e1", Src: "old", Dst: "x", Rev: 2}})
	Apply(&g, SseEvent{Kind: "edge_upsert", Rev: 3, Edge: &Edge{ID: "e2", Src: "y", Dst: "old", Rev: 3}})
	Apply(&g, SseEvent{Kind: "node_merge", Rev: 10, OldID: "old", Node: &Node{ID: "new", RunID: "r", Rev: 10}})
	assert.Equal(t, "new", g.Edges["e1"].Src)
	assert.Equal(t, "new", g.Edges["e2"].Dst)
}

func TestApplyNodeMergeNoOldID(t *testing.T) {
	g := EmptyGraph()
	Apply(&g, SseEvent{Kind: "node_merge", Rev: 5, OldID: "", Node: &Node{ID: "n1", RunID: "r", Type: "session", Rev: 5}})
	n, ok := g.Nodes["n1"]
	assert.True(t, ok)
	assert.Equal(t, "session", n.Type)
}
