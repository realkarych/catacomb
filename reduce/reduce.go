package reduce

import (
	"slices"
	"strings"

	"github.com/realkarych/catacomb/cdc"
	"github.com/realkarych/catacomb/model"
)

func (g *Graph) emitNode(n *model.Node, o model.Observation) {
	g.emit(cdc.GraphDelta{Kind: cdc.DeltaNodeUpsert, Rev: o.Seq, Node: n, RunID: o.RunID, ExecutionID: o.ExecutionID})
}

func (g *Graph) ApplyAll(obs []model.Observation) {
	for _, o := range obs {
		g.Apply(o)
	}
}

func (g *Graph) Apply(o model.Observation) {
	g.ensureRun(o)
	if o.Correlation.ParentSpanID != "" {
		g.spanChildren[o.Correlation.ParentSpanID] = true
	}
	g.node(model.SessionNodeID(o.ExecutionID), o.RunID, model.NodeSession)
	switch o.Kind {
	case "session_start":
		n := g.node(model.SessionNodeID(o.ExecutionID), o.RunID, model.NodeSession)
		g.stamp(n, o)
		n.Status = resolveStatus(n.Status, model.StatusRunning)
		g.emitNode(n, o)
	case "session_end":
		n := g.node(model.SessionNodeID(o.ExecutionID), o.RunID, model.NodeSession)
		g.stamp(n, o)
		g.stampEnd(n, o)
		n.Status = resolveStatus(n.Status, model.StatusOK)
		g.emitNode(n, o)
		g.cascadeStatus(n.ID, model.StatusUnknown, o.Seq)
		r := g.Runs[o.RunID]
		r.Status = model.StatusOK
		ended := o.EventTime
		r.EndedAt = &ended
		r.EndReason = "session_ended"
		g.emit(cdc.GraphDelta{Kind: cdc.DeltaSessionEnded, Rev: o.Seq, RunID: o.RunID, ExecutionID: o.ExecutionID})
	case "user_prompt":
		n := g.node(model.UserPromptID(o.ExecutionID, o.Correlation.UUID), o.RunID, model.NodeUserPrompt)
		g.stamp(n, o)
		g.emitNode(n, o)
		g.upsertEdge(o.ExecutionID, o.RunID, model.SessionNodeID(o.ExecutionID), n.ID, o.Seq)
	case "assistant_turn":
		n := g.node(model.AssistantTurnID(o.ExecutionID, o.Correlation.MessageID), o.RunID, model.NodeAssistantTurn)
		g.stamp(n, o)
		g.stampEnd(n, o)
		if g.applyTokens(n, o.Attrs, o.Source) {
			g.applyCost(n, o.Attrs)
		}
		g.emitNode(n, o)
	case "assistant_tool_use", "tool_result":
		g.applyTool(o)
	case "subagent_stop":
		g.applySubagent(o)
	case "marker":
		n := g.node(model.MarkerID(o.ExecutionID, o.ObsID), o.RunID, model.NodeMarker)
		g.stamp(n, o)
		n.Attrs = o.Attrs
		g.emitNode(n, o)
		g.upsertEdge(o.ExecutionID, o.RunID, model.SessionNodeID(o.ExecutionID), n.ID, o.Seq)
	case "run_ended":
		g.applyRunEnded(o)
	}
}

func (g *Graph) upsertEdgeGated(o model.Observation, src, dst string) {
	if o.Source == model.SourceOTel && o.Correlation.ParentSpanID != "" {
		if !g.spanChildren[o.Correlation.SpanID] && o.Correlation.ToolUseID == "" {
			return
		}
	}
	g.upsertEdge(o.ExecutionID, o.RunID, src, dst, o.Seq)
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
	if o.Kind == "tool_result" {
		g.stampEnd(n, o)
	}
	if name, ok := o.Attrs["name"].(string); ok {
		g.setName(n, o, name)
	}
	if s, ok := o.Attrs["status"].(string); ok {
		n.Status = resolveStatus(n.Status, model.Status(s))
		if n.Status == model.StatusCancelled || n.Status == model.StatusSuperseded {
			g.cascadeStatus(n.ID, n.Status, o.Seq)
		}
	}
	g.mergePayload(n, o.Payload, o.Source)
	g.emitNode(n, o)
	parent := model.SessionNodeID(o.ExecutionID)
	if o.Correlation.MessageID != "" {
		parent = model.AssistantTurnID(o.ExecutionID, o.Correlation.MessageID)
	}
	g.upsertEdgeGated(o, parent, id)
	g.upsertParentToolEdge(o)
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
	g.stampEnd(n, o)
	n.Status = resolveStatus(n.Status, model.StatusOK)
	g.emitNode(n, o)
	g.upsertEdge(o.ExecutionID, o.RunID, model.SessionNodeID(o.ExecutionID), n.ID, o.Seq)
}

func sourceRank(s model.Source) int {
	switch s {
	case model.SourceOTel:
		return 3
	case model.SourceHook:
		return 2
	case model.SourceJSONL:
		return 1
	default:
		return 0
	}
}

func tokenRank(s model.Source) int {
	switch s {
	case model.SourceOTel:
		return 2
	case model.SourceStreamJSON:
		return 1
	default:
		return 0
	}
}

func payloadRank(s model.Source) int {
	switch s {
	case model.SourceHook, model.SourceJSONL:
		return 1
	default:
		return 0
	}
}

func structureRank(s model.Source) int {
	switch s {
	case model.SourceJSONL:
		return 3
	case model.SourceOTel:
		return 2
	case model.SourceStreamJSON:
		return 1
	default:
		return 0
	}
}

func (g *Graph) upsertParentToolEdge(o model.Observation) {
	if o.Correlation.ParentToolUseID == "" || o.Correlation.ToolUseID == "" {
		return
	}
	src := model.ToolCallID(o.ExecutionID, o.Correlation.ParentToolUseID)
	dst := model.ToolCallID(o.ExecutionID, o.Correlation.ToolUseID)
	fs := g.stampsFor(dst)
	r := structureRank(o.Source)
	if fs.haveStruct && r < fs.structRank {
		return
	}
	if fs.haveStruct && fs.structSrc != src {
		oldID := model.EdgeID(o.ExecutionID, model.EdgeParentChild, fs.structSrc, dst)
		if old, ok := g.Edges[oldID]; ok {
			delete(g.Edges, oldID)
			g.emit(cdc.GraphDelta{Kind: cdc.DeltaEdgeDelete, Rev: o.Seq, Edge: old, RunID: old.RunID, ExecutionID: o.ExecutionID})
		}
	}
	fs.structRank = r
	fs.haveStruct = true
	fs.structSrc = src
	g.upsertEdge(o.ExecutionID, o.RunID, src, dst, o.Seq)
}

type fieldStamps struct {
	timingRank  int
	haveTiming  bool
	nameSeq     uint64
	haveName    bool
	tokenRank   int
	haveToken   bool
	payloadRank int
	havePayload bool
	structRank  int
	haveStruct  bool
	structSrc   string
	endRank     int
	haveEnd     bool
}

func (g *Graph) stampsFor(id string) *fieldStamps {
	fs, ok := g.stamps[id]
	if !ok {
		fs = &fieldStamps{}
		g.stamps[id] = fs
	}
	return fs
}

func setDuration(n *model.Node) {
	if n.TStart == nil || n.TEnd == nil {
		return
	}
	ms := n.TEnd.Sub(*n.TStart).Milliseconds()
	n.DurationMS = &ms
}

func (g *Graph) stampEnd(n *model.Node, o model.Observation) {
	fs := g.stampsFor(n.ID)
	r := sourceRank(o.Source)
	switch {
	case !fs.haveEnd || r > fs.endRank:
		ts := o.EventTime
		n.TEnd = &ts
		fs.endRank = r
		fs.haveEnd = true
	case r == fs.endRank && (n.TEnd == nil || o.EventTime.After(*n.TEnd)):
		ts := o.EventTime
		n.TEnd = &ts
	}
	setDuration(n)
}

func (g *Graph) stamp(n *model.Node, o model.Observation) {
	fs := g.stampsFor(n.ID)
	r := sourceRank(o.Source)
	if !fs.haveTiming || r > fs.timingRank {
		ts := o.EventTime
		n.TStart = &ts
		fs.timingRank = r
		fs.haveTiming = true
		setDuration(n)
	} else if r == fs.timingRank && (n.TStart == nil || o.EventTime.Before(*n.TStart)) {
		ts := o.EventTime
		n.TStart = &ts
		setDuration(n)
	}
	if o.Seq > n.Rev {
		n.Rev = o.Seq
	}
	n.Sources = append(n.Sources, model.SourceRef{Source: o.Source, ObsID: o.ObsID, ObservedAt: o.ObservedAt})
}

func (g *Graph) setName(n *model.Node, o model.Observation, name string) {
	if name == "" {
		return
	}
	fs := g.stampsFor(n.ID)
	if !fs.haveName || o.Seq < fs.nameSeq {
		n.Name = name
		fs.nameSeq = o.Seq
		fs.haveName = true
	}
}

func (g *Graph) mergePayload(n *model.Node, p *model.Payload, src model.Source) {
	if p == nil {
		return
	}
	fs := g.stampsFor(n.ID)
	r := payloadRank(src)
	if fs.havePayload && r < fs.payloadRank {
		return
	}
	fs.payloadRank = r
	fs.havePayload = true
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

func (g *Graph) applyTokens(n *model.Node, attrs map[string]any, src model.Source) bool {
	fs := g.stampsFor(n.ID)
	r := tokenRank(src)
	if fs.haveToken && r < fs.tokenRank {
		return false
	}
	fs.tokenRank = r
	fs.haveToken = true
	if v, ok := toInt64(attrs["tokens_in"]); ok {
		n.TokensIn = &v
	}
	if v, ok := toInt64(attrs["tokens_out"]); ok {
		n.TokensOut = &v
	}
	return true
}

func (g *Graph) applyCost(n *model.Node, attrs map[string]any) {
	if g.pricer == nil {
		return
	}
	in := PriceInputs{}
	if m, ok := attrs["model"].(string); ok {
		in.ModelID = m
	}
	if n.TokensIn != nil {
		in.TokensIn = *n.TokensIn
	}
	if n.TokensOut != nil {
		in.TokensOut = *n.TokensOut
	}
	if v, ok := toFloat64(attrs["cost_usd"]); ok {
		in.ReportedUSD = &v
	}
	res, ok := g.pricer.Cost(in)
	if !ok {
		return
	}
	usd := res.USD
	n.CostUSD = &usd
	if n.Attrs == nil {
		n.Attrs = map[string]any{}
	}
	n.Attrs["cost_source"] = res.Source
}

func toFloat64(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int64:
		return float64(x), true
	case int:
		return float64(x), true
	default:
		return 0, false
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
		g.emit(cdc.GraphDelta{Kind: cdc.DeltaRunStarted, Rev: o.Seq, RunID: o.RunID, ExecutionID: o.ExecutionID})
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
	g.closeIfOpen(model.SessionNodeID(o.ExecutionID), model.StatusUnknown, o.Seq)
	g.cascadeStatus(model.SessionNodeID(o.ExecutionID), model.StatusUnknown, o.Seq)
	g.emit(cdc.GraphDelta{Kind: cdc.DeltaRunEnded, Rev: o.Seq, RunID: o.RunID, ExecutionID: o.ExecutionID})
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

func (g *Graph) cascadeStatus(rootID string, status model.Status, seq uint64) {
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
			g.applyCascade(c, rootID, status, seq)
		}
	}
}

func (g *Graph) applyCascade(id, rootID string, status model.Status, seq uint64) {
	if status == model.StatusUnknown {
		g.closeIfOpen(id, status, seq)
		return
	}
	n := g.Nodes[id]
	if n == nil || rank(n.Status) >= 3 {
		return
	}
	n.Status = resolveStatus(n.Status, status)
	if n.Attrs == nil {
		n.Attrs = map[string]any{}
	}
	n.Attrs["cancel_cause"] = rootID
	g.emit(cdc.GraphDelta{Kind: cdc.DeltaNodeStatus, Rev: seq, Node: n, RunID: n.RunID})
}

func (g *Graph) closeIfOpen(id string, status model.Status, seq uint64) {
	n := g.Nodes[id]
	if n == nil {
		return
	}
	if n.Status == model.StatusRunning || n.Status == model.StatusPending {
		n.Status = resolveStatus(n.Status, status)
		g.emit(cdc.GraphDelta{Kind: cdc.DeltaNodeStatus, Rev: seq, Node: n, RunID: n.RunID})
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
