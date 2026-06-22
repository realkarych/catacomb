package daemon

import (
	"github.com/realkarych/catacomb/cdc"
	"github.com/realkarych/catacomb/model"
)

type SubFilter struct {
	RunID     string
	NodeTypes []string
	Tiers     []string
}

type Subscription struct {
	Snapshot []cdc.GraphDelta
	Consumer *cdc.Consumer
	filter   SubFilter
}

func matchNode(f SubFilter, n *model.Node) bool {
	if len(f.NodeTypes) > 0 {
		found := false
		for _, t := range f.NodeTypes {
			if string(n.Type) == t {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if len(f.Tiers) > 0 {
		found := false
		for _, tier := range f.Tiers {
			if n.Tier == tier {
				found = true
				break
			}
		}
		if !found {
			return false
		}
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
	var snapshot []cdc.GraphDelta
	for execID, g := range d.graphs {
		nodes, edges := g.Snapshot()
		for _, n := range nodes {
			if !matchNode(f, n) {
				continue
			}
			nc := copyNode(n)
			snapshot = append(snapshot, cdc.GraphDelta{
				Kind:        cdc.DeltaNodeUpsert,
				Rev:         n.Rev,
				Node:        nc,
				RunID:       n.RunID,
				ExecutionID: execID,
			})
		}
		for _, e := range edges {
			if !matchEdge(f, e) {
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
	return &Subscription{Snapshot: snapshot, Consumer: consumer, filter: f}
}

func (d *Daemon) Unsubscribe(s *Subscription) {
	d.bus.Unsubscribe(s.Consumer)
}
