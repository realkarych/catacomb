package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mkGraph() Graph {
	g := EmptyGraph()
	Apply(&g, nodeEv("node_upsert", "s", "session", "running", 1))
	Apply(&g, nodeEv("node_upsert", "a", "user_prompt", "ok", 2))
	Apply(&g, nodeEv("node_upsert", "b", "assistant_turn", "ok", 3))
	Apply(&g, SseEvent{Kind: "edge_upsert", Rev: 4, Edge: &Edge{ID: "e1", Type: "parent_child", Src: "s", Dst: "a", Rev: 4}})
	Apply(&g, SseEvent{Kind: "edge_upsert", Rev: 5, Edge: &Edge{ID: "e2", Type: "parent_child", Src: "s", Dst: "b", Rev: 5}})
	Apply(&g, SseEvent{Kind: "edge_upsert", Rev: 6, Edge: &Edge{ID: "e3", Type: "sequence", Src: "a", Dst: "b", Rev: 6}})
	return g
}

func TestBuildTreeNestsChildren(t *testing.T) {
	rows := BuildTree(mkGraph())
	require.Len(t, rows, 3)
	assert.Equal(t, "s", rows[0].Node.ID)
	assert.Equal(t, 0, rows[0].Depth)
	assert.True(t, rows[0].HasKids)
	assert.Equal(t, "a", rows[1].Node.ID)
	assert.Equal(t, 1, rows[1].Depth)
	assert.Equal(t, "b", rows[2].Node.ID)
}

func TestFlattenRespectsCollapse(t *testing.T) {
	g := mkGraph()
	rows := Flatten(g, map[string]bool{})
	require.Len(t, rows, 1)
	assert.Equal(t, "s", rows[0].Node.ID)
	rows = Flatten(g, map[string]bool{"s": true})
	assert.Len(t, rows, 3)
}

func TestBuildTreeOrphanAttachesAtRoot(t *testing.T) {
	g := EmptyGraph()
	Apply(&g, nodeEv("node_upsert", "x", "tool_call", "ok", 1))
	rows := BuildTree(g)
	require.Len(t, rows, 1)
	assert.Equal(t, "x", rows[0].Node.ID)
	assert.Equal(t, 0, rows[0].Depth)
}

func TestBuildTreeEmpty(t *testing.T) {
	assert.Empty(t, BuildTree(EmptyGraph()))
}

func TestBuildTreeMultipleRoots(t *testing.T) {
	g := EmptyGraph()
	Apply(&g, nodeEv("node_upsert", "x", "tool_call", "ok", 1))
	Apply(&g, nodeEv("node_upsert", "y", "user_prompt", "ok", 2))
	rows := BuildTree(g)
	require.Len(t, rows, 2)
	assert.Equal(t, 0, rows[0].Depth)
	assert.Equal(t, 0, rows[1].Depth)
	ids := []string{rows[0].Node.ID, rows[1].Node.ID}
	assert.Contains(t, ids, "x")
	assert.Contains(t, ids, "y")
}

func TestBuildTreeTStartOrdering(t *testing.T) {
	g := EmptyGraph()
	Apply(&g, nodeEv("node_upsert", "p", "session", "ok", 1))
	t1 := "2026-01-01T00:00:01Z"
	t2 := "2026-01-01T00:00:02Z"
	Apply(&g, SseEvent{Kind: "node_upsert", Rev: 2, Node: &Node{ID: "c2", RunID: "r", Type: "tool_call", Status: "ok", TStart: &t2, Rev: 2}})
	Apply(&g, SseEvent{Kind: "node_upsert", Rev: 3, Node: &Node{ID: "c1", RunID: "r", Type: "tool_call", Status: "ok", TStart: &t1, Rev: 3}})
	Apply(&g, SseEvent{Kind: "edge_upsert", Rev: 4, Edge: &Edge{ID: "e1", Type: "parent_child", Src: "p", Dst: "c1", Rev: 4}})
	Apply(&g, SseEvent{Kind: "edge_upsert", Rev: 5, Edge: &Edge{ID: "e2", Type: "parent_child", Src: "p", Dst: "c2", Rev: 5}})
	rows := BuildTree(g)
	require.Len(t, rows, 3)
	assert.Equal(t, "c1", rows[1].Node.ID)
	assert.Equal(t, "c2", rows[2].Node.ID)
}

func TestBuildTreeDeepNesting(t *testing.T) {
	g := EmptyGraph()
	Apply(&g, nodeEv("node_upsert", "a", "session", "ok", 1))
	Apply(&g, nodeEv("node_upsert", "b", "tool_call", "ok", 2))
	Apply(&g, nodeEv("node_upsert", "c", "user_prompt", "ok", 3))
	Apply(&g, SseEvent{Kind: "edge_upsert", Rev: 4, Edge: &Edge{ID: "e1", Type: "parent_child", Src: "a", Dst: "b", Rev: 4}})
	Apply(&g, SseEvent{Kind: "edge_upsert", Rev: 5, Edge: &Edge{ID: "e2", Type: "parent_child", Src: "b", Dst: "c", Rev: 5}})
	rows := BuildTree(g)
	require.Len(t, rows, 3)
	assert.Equal(t, 0, rows[0].Depth)
	assert.True(t, rows[0].HasKids)
	assert.Equal(t, 1, rows[1].Depth)
	assert.True(t, rows[1].HasKids)
	assert.Equal(t, 2, rows[2].Depth)
	assert.False(t, rows[2].HasKids)
}

func TestBuildTreeSessionFirstOrdering(t *testing.T) {
	g := EmptyGraph()
	Apply(&g, nodeEv("node_upsert", "o", "tool_call", "ok", 1))
	Apply(&g, nodeEv("node_upsert", "s", "session", "ok", 2))
	rows := BuildTree(g)
	require.Len(t, rows, 2)
	assert.Equal(t, "s", rows[0].Node.ID)
	assert.Equal(t, "o", rows[1].Node.ID)
}

func TestFlattenNestedExpansion(t *testing.T) {
	g := EmptyGraph()
	Apply(&g, nodeEv("node_upsert", "root", "session", "ok", 1))
	Apply(&g, nodeEv("node_upsert", "child", "tool_call", "ok", 2))
	Apply(&g, nodeEv("node_upsert", "grand", "user_prompt", "ok", 3))
	Apply(&g, SseEvent{Kind: "edge_upsert", Rev: 4, Edge: &Edge{ID: "e1", Type: "parent_child", Src: "root", Dst: "child", Rev: 4}})
	Apply(&g, SseEvent{Kind: "edge_upsert", Rev: 5, Edge: &Edge{ID: "e2", Type: "parent_child", Src: "child", Dst: "grand", Rev: 5}})

	rows := Flatten(g, map[string]bool{"root": true})
	require.Len(t, rows, 2)
	assert.Equal(t, "root", rows[0].Node.ID)
	assert.Equal(t, "child", rows[1].Node.ID)

	rows = Flatten(g, map[string]bool{"root": true, "child": true})
	require.Len(t, rows, 3)
	assert.Equal(t, "root", rows[0].Node.ID)
	assert.Equal(t, "child", rows[1].Node.ID)
	assert.Equal(t, "grand", rows[2].Node.ID)
}

func TestFlattenDepthInRows(t *testing.T) {
	g := EmptyGraph()
	Apply(&g, nodeEv("node_upsert", "root", "session", "ok", 1))
	Apply(&g, nodeEv("node_upsert", "child", "tool_call", "ok", 2))
	Apply(&g, SseEvent{Kind: "edge_upsert", Rev: 3, Edge: &Edge{ID: "e1", Type: "parent_child", Src: "root", Dst: "child", Rev: 3}})
	rows := Flatten(g, map[string]bool{"root": true})
	require.Len(t, rows, 2)
	assert.Equal(t, 0, rows[0].Depth)
	assert.Equal(t, 1, rows[1].Depth)
}

func TestFlattenNoKidsNode(t *testing.T) {
	g := EmptyGraph()
	Apply(&g, nodeEv("node_upsert", "leaf", "tool_call", "ok", 1))
	rows := Flatten(g, map[string]bool{"leaf": true})
	require.Len(t, rows, 1)
	assert.Equal(t, "leaf", rows[0].Node.ID)
}

func TestBuildTreeTStartNilVsNonNil(t *testing.T) {
	g := EmptyGraph()
	Apply(&g, nodeEv("node_upsert", "p", "session", "ok", 1))
	t1 := "2026-01-01T00:00:01Z"
	t2 := "2026-01-01T00:00:02Z"
	t3 := "2026-01-01T00:00:03Z"
	Apply(&g, SseEvent{Kind: "node_upsert", Rev: 2, Node: &Node{ID: "c1", RunID: "r", Type: "tool_call", Status: "ok", TStart: &t1, Rev: 2}})
	Apply(&g, SseEvent{Kind: "node_upsert", Rev: 3, Node: &Node{ID: "c2", RunID: "r", Type: "tool_call", Status: "ok", TStart: nil, Rev: 3}})
	Apply(&g, SseEvent{Kind: "node_upsert", Rev: 4, Node: &Node{ID: "c3", RunID: "r", Type: "tool_call", Status: "ok", TStart: &t2, Rev: 4}})
	Apply(&g, SseEvent{Kind: "node_upsert", Rev: 5, Node: &Node{ID: "c4", RunID: "r", Type: "tool_call", Status: "ok", TStart: &t3, Rev: 5}})
	Apply(&g, SseEvent{Kind: "edge_upsert", Rev: 6, Edge: &Edge{ID: "e1", Type: "parent_child", Src: "p", Dst: "c1", Rev: 6}})
	Apply(&g, SseEvent{Kind: "edge_upsert", Rev: 7, Edge: &Edge{ID: "e2", Type: "parent_child", Src: "p", Dst: "c2", Rev: 7}})
	Apply(&g, SseEvent{Kind: "edge_upsert", Rev: 8, Edge: &Edge{ID: "e3", Type: "parent_child", Src: "p", Dst: "c3", Rev: 8}})
	Apply(&g, SseEvent{Kind: "edge_upsert", Rev: 9, Edge: &Edge{ID: "e4", Type: "parent_child", Src: "p", Dst: "c4", Rev: 9}})
	rows := BuildTree(g)
	require.Len(t, rows, 5)
	assert.Equal(t, "c1", rows[1].Node.ID)
	assert.Equal(t, "c3", rows[2].Node.ID)
	assert.Equal(t, "c4", rows[3].Node.ID)
	assert.Equal(t, "c2", rows[4].Node.ID)
}

func TestBuildTreeRootTStartNilVsNonNil(t *testing.T) {
	g := EmptyGraph()
	t1 := "2026-01-01T00:00:01Z"
	Apply(&g, SseEvent{Kind: "node_upsert", Rev: 1, Node: &Node{ID: "x", RunID: "r", Type: "tool_call", Status: "ok", TStart: &t1, Rev: 1}})
	Apply(&g, SseEvent{Kind: "node_upsert", Rev: 2, Node: &Node{ID: "y", RunID: "r", Type: "tool_call", Status: "ok", TStart: nil, Rev: 2}})
	rows := BuildTree(g)
	require.Len(t, rows, 2)
	assert.Equal(t, "x", rows[0].Node.ID)
	assert.Equal(t, "y", rows[1].Node.ID)
}

func TestBuildTreeSequenceCycleFallsBack(t *testing.T) {
	g := EmptyGraph()
	Apply(&g, nodeEv("node_upsert", "p", "session", "ok", 1))
	Apply(&g, nodeEv("node_upsert", "a", "tool_call", "ok", 2))
	Apply(&g, nodeEv("node_upsert", "b", "user_prompt", "ok", 3))
	Apply(&g, SseEvent{Kind: "edge_upsert", Rev: 4, Edge: &Edge{ID: "e1", Type: "parent_child", Src: "p", Dst: "a", Rev: 4}})
	Apply(&g, SseEvent{Kind: "edge_upsert", Rev: 5, Edge: &Edge{ID: "e2", Type: "parent_child", Src: "p", Dst: "b", Rev: 5}})
	Apply(&g, SseEvent{Kind: "edge_upsert", Rev: 6, Edge: &Edge{ID: "e3", Type: "sequence", Src: "a", Dst: "b", Rev: 6}})
	Apply(&g, SseEvent{Kind: "edge_upsert", Rev: 7, Edge: &Edge{ID: "e4", Type: "sequence", Src: "b", Dst: "a", Rev: 7}})
	rows := BuildTree(g)
	require.Len(t, rows, 3)
	assert.Equal(t, "p", rows[0].Node.ID)
	childIDs := []string{rows[1].Node.ID, rows[2].Node.ID}
	assert.Contains(t, childIDs, "a")
	assert.Contains(t, childIDs, "b")
}
