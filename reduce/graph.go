package reduce

import (
	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/stepkey"
)

type promptRef struct {
	seq     uint64
	id      string
	agentID string
}

type turnRef struct {
	seq     uint64
	rev     uint64
	id      string
	parent  string
	agentID string
}

type agentGroup struct {
	root    string
	prompts []promptRef
	turns   []*turnRef
}

type execState struct {
	executionID  string
	turnsByID    map[string]*turnRef
	groups       map[string]*agentGroup
	markerBounds map[string]markerBound
	markerTools  map[string]bool
}

func (s *execState) group(agentID string) *agentGroup {
	gp, ok := s.groups[agentID]
	if !ok {
		gp = &agentGroup{root: groupRoot(s.executionID, agentID)}
		s.groups[agentID] = gp
	}
	return gp
}

func groupRoot(executionID, agentID string) string {
	if agentID == "" {
		return model.SessionNodeID(executionID)
	}
	return model.SubagentID(executionID, agentID)
}

type Graph struct {
	Nodes            map[string]*model.Node
	Edges            map[string]*model.Edge
	Runs             map[string]*model.Run
	stamps           map[string]*fieldStamps
	execs            map[string]*execState
	pricer           Pricer
	synthMarkerNodes map[string]bool
	synthMarkerEdges map[string]bool
}

func NewGraph() *Graph {
	return newGraph(nil)
}

func NewGraphWithPricer(p Pricer) *Graph {
	return newGraph(p)
}

func newGraph(p Pricer) *Graph {
	return &Graph{
		Nodes:            map[string]*model.Node{},
		Edges:            map[string]*model.Edge{},
		Runs:             map[string]*model.Run{},
		stamps:           map[string]*fieldStamps{},
		execs:            map[string]*execState{},
		pricer:           p,
		synthMarkerNodes: map[string]bool{},
		synthMarkerEdges: map[string]bool{},
	}
}

func (g *Graph) execState(executionID string) *execState {
	s, ok := g.execs[executionID]
	if !ok {
		s = &execState{
			executionID:  executionID,
			turnsByID:    map[string]*turnRef{},
			groups:       map[string]*agentGroup{},
			markerBounds: map[string]markerBound{},
			markerTools:  map[string]bool{},
		}
		g.execs[executionID] = s
	}
	return s
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

func (g *Graph) upsertEdge(executionID, runID, src, dst string, seq uint64) {
	if src == "" || dst == "" {
		return
	}
	id := model.EdgeID(executionID, model.EdgeParentChild, src, dst)
	e, ok := g.Edges[id]
	if !ok {
		g.Edges[id] = &model.Edge{ID: id, RunID: runID, Type: model.EdgeParentChild, Src: src, Dst: dst, Rev: seq}
		return
	}
	if seq > e.Rev {
		e.Rev = seq
	}
}

func (g *Graph) removeToolArtifacts(id string) {
	delete(g.Nodes, id)
	delete(g.stamps, id)
	for eid, e := range g.Edges {
		if e.Src == id || e.Dst == id {
			delete(g.Edges, eid)
		}
	}
}

func (g *Graph) clearSynthMarkers() {
	for id := range g.synthMarkerNodes {
		delete(g.Nodes, id)
	}
	for id := range g.synthMarkerEdges {
		delete(g.Edges, id)
	}
	g.synthMarkerNodes = map[string]bool{}
	g.synthMarkerEdges = map[string]bool{}
}

func (g *Graph) Snapshot() ([]*model.Node, []*model.Edge) {
	g.clearSynthMarkers()
	baseNodes := make([]*model.Node, 0, len(g.Nodes))
	for _, n := range g.Nodes {
		baseNodes = append(baseNodes, n)
	}
	baseEdges := make([]*model.Edge, 0, len(g.Edges))
	for _, e := range g.Edges {
		baseEdges = append(baseEdges, e)
	}
	for id, k := range stepkey.Compute(baseNodes, baseEdges) {
		n := g.Nodes[id]
		n.StepKey = k.Key
		n.StepKeyMethod = k.Method
	}
	g.synthesizeMarkers()
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
