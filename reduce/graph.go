package reduce

import "github.com/realkarych/catacomb/model"

type Graph struct {
	Nodes map[string]*model.Node
	Edges map[string]*model.Edge
}

func NewGraph() *Graph {
	return &Graph{Nodes: map[string]*model.Node{}, Edges: map[string]*model.Edge{}}
}

func (g *Graph) node(id, runID string, t model.NodeType) *model.Node {
	n, ok := g.Nodes[id]
	if !ok {
		n = &model.Node{ID: id, RunID: runID, Type: t, Tier: "core"}
		g.Nodes[id] = n
	}
	return n
}

func (g *Graph) upsertEdge(executionID, runID, src, dst string) {
	if src == "" || dst == "" {
		return
	}
	id := model.EdgeID(executionID, model.EdgeParentChild, src, dst)
	if _, ok := g.Edges[id]; !ok {
		g.Edges[id] = &model.Edge{ID: id, RunID: runID, Type: model.EdgeParentChild, Src: src, Dst: dst}
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
