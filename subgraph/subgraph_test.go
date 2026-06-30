package subgraph

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/realkarych/catacomb/model"
)

func ts(sec int64) *time.Time {
	t := time.Unix(sec, 0).UTC()
	return &t
}

func node(id string, typ model.NodeType, start *time.Time) *model.Node {
	return &model.Node{ID: id, Type: typ, TStart: start}
}

func TestInWindowClosedInterval(t *testing.T) {
	w := Window{Start: time.Unix(100, 0).UTC(), End: ts(200)}

	assert.False(t, InWindow(node("x", model.NodeToolCall, nil), w))
	assert.False(t, InWindow(node("x", model.NodeToolCall, ts(99)), w))
	assert.True(t, InWindow(node("x", model.NodeToolCall, ts(100)), w))
	assert.True(t, InWindow(node("x", model.NodeToolCall, ts(150)), w))
	assert.True(t, InWindow(node("x", model.NodeToolCall, ts(200)), w))
	assert.False(t, InWindow(node("x", model.NodeToolCall, ts(201)), w))
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
