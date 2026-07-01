package subgraph

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
)

func ts(sec int64) *time.Time {
	t := time.Unix(sec, 0).UTC()
	return &t
}

func node(id string, typ model.NodeType, start *time.Time) *model.Node {
	return &model.Node{ID: id, Type: typ, TStart: start}
}

func TestInWindowHalfOpenInterval(t *testing.T) {
	w := Window{Start: time.Unix(100, 0).UTC(), End: ts(200)}

	assert.False(t, InWindow(node("x", model.NodeToolCall, nil), w))
	assert.False(t, InWindow(node("x", model.NodeToolCall, ts(99)), w))
	assert.True(t, InWindow(node("x", model.NodeToolCall, ts(100)), w))
	assert.True(t, InWindow(node("x", model.NodeToolCall, ts(150)), w))
	assert.True(t, InWindow(node("x", model.NodeToolCall, ts(199)), w))
	assert.False(t, InWindow(node("x", model.NodeToolCall, ts(200)), w))
	assert.False(t, InWindow(node("x", model.NodeToolCall, ts(201)), w))
}

func TestPhaseBoundaryHalfOpenNoDoubleCount(t *testing.T) {
	exec := "E"
	a := model.PhaseMarkerID(exec, "a", 0)
	b := model.PhaseMarkerID(exec, "b", 0)
	c := model.PhaseMarkerID(exec, "c", 0)
	nodes := []*model.Node{
		{ID: a, Type: model.NodeMarker, TStart: ts(100), TEnd: ts(200)},
		{ID: b, Type: model.NodeMarker, TStart: ts(200), TEnd: ts(300)},
		{ID: c, Type: model.NodeMarker, TStart: ts(300), TEnd: ts(400)},
		{ID: "nB", Type: model.NodeToolCall, TStart: ts(200)},
		{ID: "nC", Type: model.NodeToolCall, TStart: ts(300)},
	}
	edges := []*model.Edge{}

	wa, ok := PhaseWindow(nodes, exec, "a", 0)
	require.True(t, ok)
	snA, _ := Subgraph(nodes, edges, wa)
	assert.NotContains(t, ids(snA), "nB", "boundary node must not leak into the phase that ends at it")

	wb, ok := PhaseWindow(nodes, exec, "b", 0)
	require.True(t, ok)
	snB, _ := Subgraph(nodes, edges, wb)
	assert.Contains(t, ids(snB), "nB", "boundary node belongs to the phase that starts at it")
	assert.NotContains(t, ids(snB), "nC")

	wc, ok := PhaseWindow(nodes, exec, "c", 0)
	require.True(t, ok)
	snC, _ := Subgraph(nodes, edges, wc)
	assert.Contains(t, ids(snC), "nC")
}

func TestRangeExcludesEndBoundaryNode(t *testing.T) {
	exec := "E"
	a := model.PhaseMarkerID(exec, "a", 0)
	b := model.PhaseMarkerID(exec, "b", 0)
	nodes := []*model.Node{
		{ID: a, Type: model.NodeMarker, TStart: ts(100), TEnd: ts(200)},
		{ID: b, Type: model.NodeMarker, TStart: ts(200), TEnd: ts(300)},
		{ID: "inRange", Type: model.NodeToolCall, TStart: ts(150)},
		{ID: "atB", Type: model.NodeToolCall, TStart: ts(200)},
	}
	edges := []*model.Edge{}

	p, err := ParseSpec(Spec{From: "a", To: "b"})
	require.NoError(t, err)
	sn, _, ok := ScopeExecutionParsed(nodes, edges, exec, p)
	require.True(t, ok)
	assert.Contains(t, ids(sn), "inRange")
	assert.NotContains(t, ids(sn), "atB", "--from a --to b must exclude b's first node")
}

func TestInWindowOpenEnd(t *testing.T) {
	w := Window{Start: time.Unix(100, 0).UTC(), End: nil}
	assert.True(t, InWindow(node("x", model.NodeToolCall, ts(10_000)), w))
	assert.False(t, InWindow(node("x", model.NodeToolCall, ts(99)), w))
}

func TestSubgraphFiltersNodesMarkersAndInducesEdges(t *testing.T) {
	w := Window{Start: time.Unix(100, 0).UTC(), End: ts(200)}
	nodes := []*model.Node{
		node("in1", model.NodeToolCall, ts(110)),
		node("in2", model.NodeToolCall, ts(190)),
		node("out", model.NodeToolCall, ts(500)),
		node("mark", model.NodeMarker, ts(120)),
	}
	edges := []*model.Edge{
		{ID: "e1", Src: "in1", Dst: "in2"},
		{ID: "e2", Src: "in1", Dst: "out"},
		{ID: "e3", Src: "mark", Dst: "in1"},
	}

	sn, se := Subgraph(nodes, edges, w)

	assert.Equal(t, []string{"in1", "in2"}, ids(sn))
	assert.Len(t, se, 1)
	assert.Equal(t, "e1", se[0].ID)
}

func ids(ns []*model.Node) []string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = n.ID
	}
	return out
}
