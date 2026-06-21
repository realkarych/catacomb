package reduce

import (
	"github.com/realkarych/catacomb/cdc"
	"github.com/realkarych/catacomb/model"
)

type Graph struct {
	Nodes        map[string]*model.Node
	Edges        map[string]*model.Edge
	Runs         map[string]*model.Run
	spanChildren map[string]bool
	stamps       map[string]*fieldStamps
	deltas       []cdc.GraphDelta
}

func NewGraph() *Graph {
	return &Graph{
		Nodes:        map[string]*model.Node{},
		Edges:        map[string]*model.Edge{},
		Runs:         map[string]*model.Run{},
		spanChildren: map[string]bool{},
		stamps:       map[string]*fieldStamps{},
	}
}

func (g *Graph) RunsSnapshot() []model.Run {
	out := make([]model.Run, 0, len(g.Runs))
	for _, r := range g.Runs {
		out = append(out, *r)
	}
	return out
}

func (g *Graph) node(id, runID string, t model.NodeType) *model.Node {
	n, ok := g.Nodes[id]
	if !ok {
		n = &model.Node{ID: id, RunID: runID, Type: t, Tier: "core"}
		g.Nodes[id] = n
	}
	return n
}

func (g *Graph) emit(d cdc.GraphDelta) {
	g.deltas = append(g.deltas, d)
}

func (g *Graph) DrainDeltas() []cdc.GraphDelta {
	d := g.deltas
	g.deltas = nil
	return d
}

func (g *Graph) upsertEdge(executionID, runID, src, dst string, seq uint64) {
	if src == "" || dst == "" {
		return
	}
	id := model.EdgeID(executionID, model.EdgeParentChild, src, dst)
	e, ok := g.Edges[id]
	if !ok {
		e = &model.Edge{ID: id, RunID: runID, Type: model.EdgeParentChild, Src: src, Dst: dst, Rev: seq}
		g.Edges[id] = e
		g.emit(cdc.GraphDelta{Kind: cdc.DeltaEdgeUpsert, Rev: seq, Edge: e, RunID: runID, ExecutionID: executionID})
		return
	}
	if seq > e.Rev {
		e.Rev = seq
		g.emit(cdc.GraphDelta{Kind: cdc.DeltaEdgeUpsert, Rev: seq, Edge: e, RunID: runID, ExecutionID: executionID})
	}
}

func (g *Graph) Snapshot() ([]*model.Node, []*model.Edge) {
	nodes := make([]*model.Node, 0, len(g.Nodes))
	for _, n := range g.Nodes {
		nodes = append(nodes, n)
	}
	edges := make([]*model.Edge, 0, len(g.Edges))
	for _, e := range g.Edges {
		edges = append(edges, e)
	}
	return nodes, edges
}
