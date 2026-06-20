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

func (g *Graph) upsertEdge(executionID, runID string, t model.EdgeType, src, dst string) {
	if src == "" || dst == "" {
		return
	}
	id := model.EdgeID(executionID, t, src, dst)
	if _, ok := g.Edges[id]; !ok {
		g.Edges[id] = &model.Edge{ID: id, RunID: runID, Type: t, Src: src, Dst: dst}
	}
}
