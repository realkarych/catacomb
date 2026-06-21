package reduce

import (
	"slices"
	"strings"

	"github.com/realkarych/catacomb/model"
)

func (g *Graph) ApplyAll(obs []model.Observation) {
	for _, o := range obs {
		g.Apply(o)
	}
}

func (g *Graph) Apply(o model.Observation) {
	g.ensureRun(o)
	g.node(model.SessionNodeID(o.ExecutionID), o.RunID, model.NodeSession)
	switch o.Kind {
	case "session_start":
		n := g.node(model.SessionNodeID(o.ExecutionID), o.RunID, model.NodeSession)
		g.stamp(n, o)
		n.Status = resolveStatus(n.Status, model.StatusRunning)
	case "session_end":
		n := g.node(model.SessionNodeID(o.ExecutionID), o.RunID, model.NodeSession)
		g.stamp(n, o)
		ts := o.EventTime
		n.TEnd = &ts
		n.Status = resolveStatus(n.Status, model.StatusOK)
		g.closeOpenDescendants(n.ID)
		r := g.Runs[o.RunID]
		r.Status = model.StatusOK
		ended := o.EventTime
		r.EndedAt = &ended
		r.EndReason = "session_ended"
	case "user_prompt":
		n := g.node(model.UserPromptID(o.ExecutionID, o.Correlation.UUID), o.RunID, model.NodeUserPrompt)
		g.stamp(n, o)
		g.upsertEdge(o.ExecutionID, o.RunID, model.SessionNodeID(o.ExecutionID), n.ID, o.Seq)
	case "assistant_turn":
		n := g.node(model.AssistantTurnID(o.ExecutionID, o.Correlation.MessageID), o.RunID, model.NodeAssistantTurn)
		g.stamp(n, o)
		applyTokens(n, o.Attrs)
	case "assistant_tool_use", "tool_result":
		g.applyTool(o)
	case "subagent_stop":
		g.applySubagent(o)
	case "marker":
		n := g.node(model.MarkerID(o.ExecutionID, o.ObsID), o.RunID, model.NodeMarker)
		g.stamp(n, o)
		n.Attrs = o.Attrs
		g.upsertEdge(o.ExecutionID, o.RunID, model.SessionNodeID(o.ExecutionID), n.ID, o.Seq)
	case "run_ended":
		g.applyRunEnded(o)
	}
}

func (g *Graph) applyTool(o model.Observation) {
	id := model.ToolCallID(o.ExecutionID, o.Correlation.ToolUseID)
	nodeType := model.NodeToolCall
	if name, _ := o.Attrs["name"].(string); isMCP(name) {
		nodeType = model.NodeMCPCall
	}
	n := g.node(id, o.RunID, nodeType)
	if n.Type == model.NodeToolCall && nodeType == model.NodeMCPCall {
		n.Type = model.NodeMCPCall
	}
	g.stamp(n, o)
	if name, ok := o.Attrs["name"].(string); ok && n.Name == "" {
		n.Name = name
	}
	if s, ok := o.Attrs["status"].(string); ok {
		n.Status = resolveStatus(n.Status, model.Status(s))
	}
	mergePayload(n, o.Payload)
	parent := model.SessionNodeID(o.ExecutionID)
	if o.Correlation.MessageID != "" {
		parent = model.AssistantTurnID(o.ExecutionID, o.Correlation.MessageID)
	}
	g.upsertEdge(o.ExecutionID, o.RunID, parent, id, o.Seq)
}

func (g *Graph) applySubagent(o model.Observation) {
	n := g.node(model.SubagentID(o.ExecutionID, o.Correlation.AgentID), o.RunID, model.NodeSubagent)
	g.stamp(n, o)
	if o.Correlation.AgentID != "" {
		n.AgentID = o.Correlation.AgentID
	}
	if t, ok := o.Attrs["subagent_type"].(string); ok && n.SubagentType == "" {
		n.SubagentType = t
	}
	ts := o.EventTime
	n.TEnd = &ts
	n.Status = resolveStatus(n.Status, model.StatusOK)
	g.upsertEdge(o.ExecutionID, o.RunID, model.SessionNodeID(o.ExecutionID), n.ID, o.Seq)
}

func (g *Graph) stamp(n *model.Node, o model.Observation) {
	if n.TStart == nil || o.EventTime.Before(*n.TStart) {
		ts := o.EventTime
		n.TStart = &ts
	}
	if o.Seq > n.Rev {
		n.Rev = o.Seq
	}
	n.Sources = append(n.Sources, model.SourceRef{Source: o.Source, ObsID: o.ObsID, ObservedAt: o.ObservedAt})
}

func mergePayload(n *model.Node, p *model.Payload) {
	if p == nil {
		return
	}
	if n.Payload == nil {
		n.Payload = &model.Payload{}
	}
	if len(p.Input) > 0 {
		n.Payload.Input = p.Input
	}
	if len(p.Output) > 0 {
		n.Payload.Output = p.Output
	}
	n.Payload.Hash = model.HashPayload(n.Payload)
	n.PayloadHash = n.Payload.Hash
}

func applyTokens(n *model.Node, attrs map[string]any) {
	if v, ok := toInt64(attrs["tokens_in"]); ok {
		n.TokensIn = &v
	}
	if v, ok := toInt64(attrs["tokens_out"]); ok {
		n.TokensOut = &v
	}
}

func toInt64(v any) (int64, bool) {
	switch x := v.(type) {
	case int64:
		return x, true
	case int:
		return int64(x), true
	case float64:
		return int64(x), true
	default:
		return 0, false
	}
}

func isMCP(name string) bool {
	return strings.HasPrefix(name, "mcp__")
}

func (g *Graph) ensureRun(o model.Observation) {
	r, ok := g.Runs[o.RunID]
	if !ok {
		started := o.EventTime
		r = &model.Run{ID: o.RunID, Status: model.StatusRunning, StartedAt: &started}
		g.Runs[o.RunID] = r
	}
	if r.Status == model.StatusAbandoned {
		r.Status = model.StatusRunning
		r.EndedAt = nil
		r.EndReason = ""
	}
	if o.Seq > r.LastSeq {
		r.LastSeq = o.Seq
	}
	r.SessionIDs = appendUnique(r.SessionIDs, o.Correlation.SessionID)
}

func (g *Graph) applyRunEnded(o model.Observation) {
	r := g.Runs[o.RunID]
	if r.Status == model.StatusOK {
		return
	}
	r.Status = model.StatusAbandoned
	ended := o.EventTime
	r.EndedAt = &ended
	r.EndReason = ""
	if reason, ok := o.Attrs["reason"].(string); ok {
		r.EndReason = reason
	}
	g.closeIfOpen(model.SessionNodeID(o.ExecutionID), model.StatusUnknown)
	g.closeOpenDescendants(model.SessionNodeID(o.ExecutionID))
}

func appendUnique(xs []string, x string) []string {
	if x == "" {
		return xs
	}
	if slices.Contains(xs, x) {
		return xs
	}
	return append(xs, x)
}

func (g *Graph) closeOpenDescendants(rootID string) {
	children := map[string][]string{}
	for _, e := range g.Edges {
		if e.Type == model.EdgeParentChild {
			children[e.Src] = append(children[e.Src], e.Dst)
		}
	}
	seen := map[string]bool{rootID: true}
	queue := []string{rootID}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, c := range children[cur] {
			if seen[c] {
				continue
			}
			seen[c] = true
			queue = append(queue, c)
			g.closeIfOpen(c, model.StatusUnknown)
		}
	}
}

func (g *Graph) closeIfOpen(id string, status model.Status) {
	n := g.Nodes[id]
	if n == nil {
		return
	}
	if n.Status == model.StatusRunning || n.Status == model.StatusPending {
		n.Status = resolveStatus(n.Status, status)
	}
}

func rank(s model.Status) int {
	switch s {
	case model.StatusOK, model.StatusError, model.StatusBlocked:
		return 3
	case model.StatusCancelled, model.StatusUnknown, model.StatusSuperseded, model.StatusAbandoned:
		return 2
	case model.StatusRunning:
		return 1
	default:
		return 0
	}
}

func resolveStatus(cur, next model.Status) model.Status {
	rc, rn := rank(cur), rank(next)
	if rc == 3 && rn < 3 {
		return cur
	}
	if rn >= rc {
		return next
	}
	return cur
}
