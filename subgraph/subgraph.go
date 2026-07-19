package subgraph

import (
	"time"

	"github.com/realkarych/catacomb/model"
)

type Window struct {
	Start time.Time
	End   *time.Time
}

func InWindow(n *model.Node, w Window) bool {
	if n.TStart == nil {
		return false
	}
	if n.TStart.Before(w.Start) {
		return false
	}
	if w.End != nil && !n.TStart.Before(*w.End) {
		return false
	}
	return true
}

func Subgraph(nodes []*model.Node, edges []*model.Edge, w Window) ([]*model.Node, []*model.Edge) {
	return subgraphWith(nodes, edges, w, false)
}

func SubgraphAnchored(nodes []*model.Node, edges []*model.Edge, w Window) ([]*model.Node, []*model.Edge) {
	return subgraphWith(nodes, edges, w, true)
}

func keepNode(n *model.Node, w Window, anchor bool) bool {
	if n.Type == model.NodeMarker {
		return false
	}
	if anchor && n.Type == model.NodeSession {
		return true
	}
	return InWindow(n, w)
}

func subgraphWith(nodes []*model.Node, edges []*model.Edge, w Window, anchor bool) ([]*model.Node, []*model.Edge) {
	included := make(map[string]bool, len(nodes))
	var sn []*model.Node
	for _, n := range nodes {
		if !keepNode(n, w, anchor) {
			continue
		}
		included[n.ID] = true
		sn = append(sn, n)
	}
	var se []*model.Edge
	for _, e := range edges {
		if included[e.Src] && included[e.Dst] {
			se = append(se, e)
		}
	}
	return sn, se
}
