package daemon

import (
	"slices"

	"github.com/realkarych/catacomb/cdc"
	"github.com/realkarych/catacomb/model"
)

type SubFilter struct {
	RunID     string
	SessionID string
	NodeTypes []string
	Tiers     []string
}

type Subscription struct {
	Snapshot []cdc.GraphDelta
	Consumer *cdc.Consumer
	filter   SubFilter
	execSet  map[string]bool
	daemon   *Daemon
}

func matchNode(f SubFilter, n *model.Node) bool {
	if len(f.NodeTypes) > 0 && !slices.Contains(f.NodeTypes, string(n.Type)) {
		return false
	}
	if len(f.Tiers) > 0 && !slices.Contains(f.Tiers, n.Tier) {
		return false
	}
	return true
}

func matchEdge(f SubFilter, e *model.Edge) bool {
	if f.RunID == "" || f.RunID == "all" {
		return true
	}
	return e.RunID == f.RunID
}

func matchDelta(f SubFilter, d cdc.GraphDelta) bool {
	if f.RunID != "" && f.RunID != "all" && d.RunID != f.RunID {
		return false
	}
	switch d.Kind {
	case cdc.DeltaNodeUpsert, cdc.DeltaNodeStatus, cdc.DeltaNodeMerge:
		if d.Node != nil {
			return matchNode(f, d.Node)
		}
	case cdc.DeltaEdgeUpsert, cdc.DeltaEdgeDelete:
		if d.Edge != nil {
			return matchEdge(f, d.Edge)
		}
	}
	return true
}

func (d *Daemon) SubscribeFiltered(f SubFilter, bufSize int) *Subscription {
	d.mu.Lock()
	defer d.mu.Unlock()
	var execSet map[string]bool
	if f.SessionID != "" {
		execSet = map[string]bool{}
		for _, e := range d.executionsForSession(f.SessionID) {
			execSet[e] = true
		}
	}
	var snapshot []cdc.GraphDelta
	for execID, g := range d.graphs {
		if execSet != nil && !execSet[execID] {
			continue
		}
		nodes, edges := g.Snapshot()
		parents := parentEdgeSources(g)
		rollups := subagentRollups(g, execID)
		for _, n := range nodes {
			if !matchNode(f, n) || topLevelExcluded(g, parents, execID, n) {
				continue
			}
			nc := copyNode(n)
			decorateSubagent(nc, rollups)
			snapshot = append(snapshot, cdc.GraphDelta{
				Kind:        cdc.DeltaNodeUpsert,
				Rev:         n.Rev,
				Node:        nc,
				RunID:       n.RunID,
				ExecutionID: execID,
			})
		}
		for _, e := range edges {
			if !matchEdge(f, e) || topLevelExcluded(g, parents, execID, g.Nodes[e.Src]) || topLevelExcluded(g, parents, execID, g.Nodes[e.Dst]) {
				continue
			}
			ec := copyEdge(e)
			snapshot = append(snapshot, cdc.GraphDelta{
				Kind:        cdc.DeltaEdgeUpsert,
				Rev:         e.Rev,
				Edge:        ec,
				RunID:       e.RunID,
				ExecutionID: execID,
			})
		}
	}
	consumer := d.bus.Subscribe(bufSize)
	return &Subscription{Snapshot: snapshot, Consumer: consumer, filter: f, execSet: execSet, daemon: d}
}

func (s *Subscription) match(d cdc.GraphDelta) bool {
	if s.execSet != nil && !s.execSet[d.ExecutionID] {
		return false
	}
	return matchDelta(s.filter, d)
}

func (s *Subscription) transform(d cdc.GraphDelta) (cdc.GraphDelta, bool) {
	if !s.match(d) {
		return d, false
	}
	switch d.Kind {
	case cdc.DeltaNodeUpsert, cdc.DeltaNodeStatus, cdc.DeltaNodeMerge:
		if d.Node == nil {
			return d, true
		}
		if isInnerNode(d.ExecutionID, d.Node) {
			return d, false
		}
		if d.Node.Type == model.NodeSubagent && d.Node.AgentID != "" {
			nc, keep := s.subagentTransform(d.ExecutionID, d.Node)
			if !keep {
				return d, false
			}
			d.Node = nc
		}
		return d, true
	case cdc.DeltaEdgeUpsert, cdc.DeltaEdgeDelete:
		if d.Edge == nil {
			return d, true
		}
		if s.edgeIsInner(d.ExecutionID, d.Edge) {
			return d, false
		}
		return d, true
	}
	return d, true
}

func (s *Subscription) subagentTransform(execID string, n *model.Node) (*model.Node, bool) {
	s.daemon.mu.Lock()
	defer s.daemon.mu.Unlock()
	g := s.daemon.graphs[execID]
	if nestedSubagent(g, parentEdgeSources(g), execID, n) {
		return nil, false
	}
	nc := copyNode(n)
	agg := subagentAggregate(g, execID, n.AgentID)
	applyAggregate(nc, &agg)
	return nc, true
}

func (s *Subscription) edgeIsInner(execID string, e *model.Edge) bool {
	s.daemon.mu.Lock()
	defer s.daemon.mu.Unlock()
	g := s.daemon.graphs[execID]
	if g == nil {
		return false
	}
	return isInnerNode(execID, g.Nodes[e.Src]) || isInnerNode(execID, g.Nodes[e.Dst])
}

func (d *Daemon) Unsubscribe(s *Subscription) {
	d.bus.Unsubscribe(s.Consumer)
}
