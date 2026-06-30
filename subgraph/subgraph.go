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
	if w.End != nil && n.TStart.After(*w.End) {
		return false
	}
	return true
}

func Subgraph(nodes []*model.Node, edges []*model.Edge, w Window) ([]*model.Node, []*model.Edge) {
	included := make(map[string]bool, len(nodes))
	var sn []*model.Node
	for _, n := range nodes {
		if n.Type == model.NodeMarker {
			continue
		}
		if !InWindow(n, w) {
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
