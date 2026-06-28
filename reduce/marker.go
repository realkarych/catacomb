package reduce

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/phasekey"
)

type markerBound struct {
	name     string
	boundary string
	occ      int
	stateRef string
	agentID  string
	ts       time.Time
	seq      uint64
}

func isMarkerTool(name string) bool {
	return name == "mcp__catacomb__mark" || name == "catacomb__mark"
}

type markerToolInput struct {
	Name       string `json:"name"`
	Boundary   string `json:"boundary"`
	StateRef   string `json:"state_ref"`
	Occurrence *int   `json:"occurrence"`
}

func extractMarkerFromPayload(o model.Observation) (name, boundary, stateRef string, occ int, ok bool) {
	occ = -1
	if o.Payload == nil || len(o.Payload.Input) == 0 {
		return name, boundary, stateRef, occ, ok
	}
	var in markerToolInput
	if err := json.Unmarshal(o.Payload.Input, &in); err != nil {
		return name, boundary, stateRef, occ, ok
	}
	name = in.Name
	boundary = in.Boundary
	stateRef = in.StateRef
	if in.Occurrence != nil {
		occ = *in.Occurrence
	}
	ok = name != "" && boundary != ""
	return name, boundary, stateRef, occ, ok
}

func extractMarkerFromAttrs(o model.Observation) (name, boundary, stateRef string, occ int, ok bool) {
	occ = -1
	name, _ = o.Attrs["name"].(string)
	boundary, _ = o.Attrs["boundary"].(string)
	stateRef, _ = o.Attrs["state_ref"].(string)
	if ov, hasOcc := o.Attrs["occurrence"].(float64); hasOcc {
		occ = int(ov)
	}
	ok = name != "" && boundary != ""
	return name, boundary, stateRef, occ, ok
}

func appendMarkerBound(s *execState, name, boundary, stateRef string, occ int, agentID string, ts time.Time, seq uint64) {
	s.markerBounds = append(s.markerBounds, markerBound{
		name:     name,
		boundary: boundary,
		occ:      occ,
		stateRef: stateRef,
		agentID:  agentID,
		ts:       ts,
		seq:      seq,
	})
}

func (g *Graph) synthesizeMarkers() {
	for execID, s := range g.execs {
		if len(s.markerBounds) > 0 {
			g.synthesizeExecMarkers(execID, s)
		}
	}
}

func (g *Graph) synthesizeExecMarkers(execID string, s *execState) {
	sessNode := g.Nodes[model.SessionNodeID(execID)]
	if sessNode == nil {
		return
	}

	sorted := make([]markerBound, len(s.markerBounds))
	copy(sorted, s.markerBounds)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].ts.Equal(sorted[j].ts) {
			return sorted[i].seq < sorted[j].seq
		}
		return sorted[i].ts.Before(sorted[j].ts)
	})

	byName := map[string][]markerBound{}
	var names []string
	for _, b := range sorted {
		if _, seen := byName[b.name]; !seen {
			names = append(names, b.name)
		}
		byName[b.name] = append(byName[b.name], b)
	}
	sort.Strings(names)

	for _, name := range names {
		bounds := byName[name]
		var starts, ends []markerBound
		for _, b := range bounds {
			switch b.boundary {
			case "start":
				starts = append(starts, b)
			case "end":
				ends = append(ends, b)
			}
		}
		for i, start := range starts {
			occ := i
			if start.occ >= 0 {
				occ = start.occ
			}
			g.buildMarker(execID, sessNode, name, occ, start, ends, i)
		}
	}
}

func (g *Graph) buildMarker(execID string, sessNode *model.Node, name string, occ int, start markerBound, ends []markerBound, idx int) {
	id := model.PhaseMarkerID(execID, name, occ)
	runID := sessNode.RunID

	attrs := map[string]any{}
	if start.stateRef != "" {
		attrs["state_ref"] = start.stateRef
	}

	var tEnd *time.Time
	if idx < len(ends) {
		t := ends[idx].ts
		tEnd = &t
	} else {
		attrs["open"] = true
		if sessNode.TEnd != nil {
			tEnd = sessNode.TEnd
		}
	}

	enclosingStepKey := ""
	if start.agentID != "" {
		agentNode := g.Nodes[model.SubagentID(execID, start.agentID)]
		if agentNode != nil {
			enclosingStepKey = agentNode.StepKey
		}
	}
	pk := phasekey.Compute(enclosingStepKey, name, occ)

	tStart := start.ts
	n := &model.Node{
		ID:       id,
		RunID:    runID,
		Type:     model.NodeMarker,
		PhaseKey: pk,
		TStart:   &tStart,
		Attrs:    attrs,
	}
	if len(attrs) == 0 {
		n.Attrs = nil
	}
	if tEnd != nil {
		n.TEnd = tEnd
		ms := n.TEnd.Sub(*n.TStart).Milliseconds()
		n.DurationMS = &ms
	}

	g.Nodes[id] = n
	g.synthMarkerNodes[id] = true

	sessEdgeID := model.EdgeID(execID, model.EdgeParentChild, model.SessionNodeID(execID), id)
	g.Edges[sessEdgeID] = &model.Edge{
		ID:    sessEdgeID,
		RunID: runID,
		Type:  model.EdgeParentChild,
		Src:   model.SessionNodeID(execID),
		Dst:   id,
	}
	g.synthMarkerEdges[sessEdgeID] = true

	g.addMarkerSpans(execID, runID, id, tStart, tEnd)
}

func (g *Graph) addMarkerSpans(execID, runID, markerID string, tStart time.Time, tEnd *time.Time) {
	for nodeID, n := range g.Nodes {
		if nodeID == markerID {
			continue
		}
		if n.Type == model.NodeMarker {
			continue
		}
		if n.TStart == nil {
			continue
		}
		if n.TStart.Before(tStart) {
			continue
		}
		if tEnd != nil && n.TStart.After(*tEnd) {
			continue
		}
		edgeID := model.EdgeID(execID, model.EdgeMarkerSpan, markerID, nodeID)
		g.Edges[edgeID] = &model.Edge{
			ID:    edgeID,
			RunID: runID,
			Type:  model.EdgeMarkerSpan,
			Src:   markerID,
			Dst:   nodeID,
		}
		g.synthMarkerEdges[edgeID] = true
	}
}
