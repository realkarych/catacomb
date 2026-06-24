package tui

type Graph struct {
	Nodes       map[string]Node
	Edges       map[string]Edge
	established map[string]bool
	statusRev   map[string]uint64
	tombstones  map[string]uint64
}

func EmptyGraph() Graph {
	return Graph{
		Nodes:       make(map[string]Node),
		Edges:       make(map[string]Edge),
		established: make(map[string]bool),
		statusRev:   make(map[string]uint64),
		tombstones:  make(map[string]uint64),
	}
}

func applyStatusFields(target, source *Node) {
	if source.Status != "" {
		target.Status = source.Status
	}
	if source.TEnd != nil {
		target.TEnd = source.TEnd
	}
	if source.DurationMS != nil {
		target.DurationMS = source.DurationMS
	}
}

func Apply(g *Graph, ev SseEvent) {
	switch ev.Kind {
	case "node_upsert":
		applyNodeUpsert(g, ev)
	case "node_status":
		applyNodeStatus(g, ev)
	case "node_merge":
		applyNodeMerge(g, ev)
	case "edge_upsert":
		applyEdgeUpsert(g, ev)
	case "edge_delete":
		applyEdgeDelete(g, ev)
	}
}

func applyNodeUpsert(g *Graph, ev SseEvent) {
	if ev.Node == nil {
		return
	}
	n := ev.Node
	tombKey := "node:" + n.ID
	if tomb, ok := g.tombstones[tombKey]; ok && n.Rev <= tomb {
		return
	}
	existing, exists := g.Nodes[n.ID]
	if exists && g.established[n.ID] {
		if n.Rev <= existing.Rev {
			return
		}
	}
	if exists && !g.established[n.ID] {
		merged := *n
		prevStatusRev := g.statusRev[n.ID]
		if prevStatusRev > n.Rev {
			if existing.Status != "" {
				merged.Status = existing.Status
			}
			if existing.TEnd != nil {
				merged.TEnd = existing.TEnd
			}
			if existing.DurationMS != nil {
				merged.DurationMS = existing.DurationMS
			}
		}
		g.Nodes[n.ID] = merged
	} else {
		g.Nodes[n.ID] = *n
	}
	g.established[n.ID] = true
	prev := g.statusRev[n.ID]
	if n.Rev >= prev {
		g.statusRev[n.ID] = n.Rev
	}
}

func applyNodeStatus(g *Graph, ev SseEvent) {
	if ev.Node == nil {
		return
	}
	patch := ev.Node
	tombKey := "node:" + patch.ID
	if tomb, ok := g.tombstones[tombKey]; ok && patch.Rev <= tomb {
		return
	}
	prevStatusRev := g.statusRev[patch.ID]
	existing, exists := g.Nodes[patch.ID]
	if !exists {
		seed := Node{
			ID:    patch.ID,
			RunID: patch.RunID,
			Type:  patch.Type,
			Rev:   patch.Rev,
		}
		applyStatusFields(&seed, patch)
		g.Nodes[patch.ID] = seed
		g.statusRev[patch.ID] = patch.Rev
		return
	}
	if patch.Rev >= prevStatusRev {
		applyStatusFields(&existing, patch)
		g.statusRev[patch.ID] = patch.Rev
		if !g.established[patch.ID] {
			existing.Rev = patch.Rev
		}
		g.Nodes[patch.ID] = existing
	}
}

func applyNodeMerge(g *Graph, ev SseEvent) {
	if ev.Node == nil {
		return
	}
	n := ev.Node
	if ev.OldID != "" && ev.OldID != n.ID {
		delete(g.Nodes, ev.OldID)
		delete(g.established, ev.OldID)
		delete(g.statusRev, ev.OldID)
		tombKey := "node:" + ev.OldID
		existing := g.tombstones[tombKey]
		if ev.Rev > existing {
			g.tombstones[tombKey] = ev.Rev
		}
		for id, e := range g.Edges {
			changed := false
			if e.Src == ev.OldID {
				e.Src = n.ID
				changed = true
			}
			if e.Dst == ev.OldID {
				e.Dst = n.ID
				changed = true
			}
			if changed {
				g.Edges[id] = e
			}
		}
	}
	g.Nodes[n.ID] = *n
	g.established[n.ID] = true
	g.statusRev[n.ID] = n.Rev
}

func applyEdgeUpsert(g *Graph, ev SseEvent) {
	if ev.Edge == nil {
		return
	}
	e := ev.Edge
	if tomb, ok := g.tombstones[e.ID]; ok && e.Rev <= tomb {
		return
	}
	if existing, ok := g.Edges[e.ID]; ok && e.Rev <= existing.Rev {
		return
	}
	g.Edges[e.ID] = *e
}

func applyEdgeDelete(g *Graph, ev SseEvent) {
	if ev.Edge == nil {
		return
	}
	id := ev.Edge.ID
	existing := g.tombstones[id]
	if ev.Rev > existing {
		g.tombstones[id] = ev.Rev
	}
	delete(g.Edges, id)
}
