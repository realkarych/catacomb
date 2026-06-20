package reduce

import (
	"strings"

	"github.com/realkarych/catacomb/model"
)

func (g *Graph) ApplyAll(obs []model.Observation) {
	for _, o := range obs {
		g.Apply(o)
	}
}

func (g *Graph) Apply(o model.Observation) {
	g.node(model.SessionNodeID(o.ExecutionID), o.RunID, model.NodeSession)
	switch o.Kind {
	case "user_prompt":
		n := g.node(model.UserPromptID(o.ExecutionID, o.Correlation.UUID), o.RunID, model.NodeUserPrompt)
		g.stamp(n, o)
		g.upsertEdge(o.ExecutionID, o.RunID, model.EdgeParentChild, model.SessionNodeID(o.ExecutionID), n.ID)
	case "assistant_turn":
		n := g.node(model.AssistantTurnID(o.ExecutionID, o.Correlation.MessageID), o.RunID, model.NodeAssistantTurn)
		g.stamp(n, o)
		applyTokens(n, o.Attrs)
	case "assistant_tool_use", "tool_result":
		g.applyTool(o)
	}
}

func (g *Graph) applyTool(o model.Observation) {
	id := model.ToolCallID(o.ExecutionID, o.Correlation.ToolUseID)
	nodeType := model.NodeToolCall
	if name, _ := o.Attrs["name"].(string); isMCP(name) {
		nodeType = model.NodeMCPCall
	}
	n := g.node(id, o.RunID, nodeType)
	g.stamp(n, o)
	if name, ok := o.Attrs["name"].(string); ok && n.Name == "" {
		n.Name = name
	}
	if s, ok := o.Attrs["status"].(string); ok {
		n.Status = model.Status(s)
	}
	mergePayload(n, o.Payload)
	if o.Correlation.MessageID != "" {
		g.upsertEdge(o.ExecutionID, o.RunID, model.EdgeParentChild, model.AssistantTurnID(o.ExecutionID, o.Correlation.MessageID), id)
	}
}

func (g *Graph) stamp(n *model.Node, o model.Observation) {
	if n.TStart == nil || o.EventTime.Before(*n.TStart) {
		ts := o.EventTime
		n.TStart = &ts
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
