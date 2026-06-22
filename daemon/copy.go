package daemon

import "github.com/realkarych/catacomb/model"

func copyNode(n *model.Node) *model.Node {
	nc := *n
	if n.Attrs != nil {
		nc.Attrs = make(map[string]any, len(n.Attrs))
		for k, v := range n.Attrs {
			nc.Attrs[k] = v
		}
	}
	if n.Annotations != nil {
		nc.Annotations = make(map[string]any, len(n.Annotations))
		for k, v := range n.Annotations {
			nc.Annotations[k] = v
		}
	}
	if n.Sources != nil {
		nc.Sources = make([]model.SourceRef, len(n.Sources))
		copy(nc.Sources, n.Sources)
	}
	return &nc
}

func copyEdge(e *model.Edge) *model.Edge {
	ec := *e
	if e.Attrs != nil {
		ec.Attrs = make(map[string]any, len(e.Attrs))
		for k, v := range e.Attrs {
			ec.Attrs[k] = v
		}
	}
	return &ec
}
