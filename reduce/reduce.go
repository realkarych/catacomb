package reduce

import (
	"slices"
	"sort"
	"strings"

	"github.com/realkarych/catacomb/model"
)

func setAgentID(n *model.Node, o model.Observation) {
	if o.Correlation.AgentID != "" {
		n.AgentID = o.Correlation.AgentID
	}
}

func (g *Graph) ApplyAll(obs []model.Observation) {
	for _, o := range obs {
		g.Apply(o)
	}
}

func (g *Graph) Apply(o model.Observation) {
	g.ensureRun(o)
	g.node(model.SessionNodeID(o.ExecutionID), o.RunID, model.NodeSession)
	switch o.Kind {
	case "user_prompt":
		n := g.node(model.UserPromptID(o.ExecutionID, nodeKey(o.Correlation.UUID, o.ObsID)), o.RunID, model.NodeUserPrompt)
		g.stamp(n, o)
		setAgentID(n, o)
		if pk, ok := o.Attrs["prompt_kind"].(string); ok && pk != "" {
			if n.Attrs == nil {
				n.Attrs = map[string]any{}
			}
			n.Attrs["prompt_kind"] = pk
		}
		g.mergePayload(n, o.Payload, o.Source)
		g.upsertEdge(o.ExecutionID, o.RunID, groupRoot(o.ExecutionID, o.Correlation.AgentID), n.ID, o.Seq)
		g.recordPrompt(o, n.ID)
	case "assistant_turn":
		turnKey := o.Correlation.MessageID
		n := g.node(model.AssistantTurnID(o.ExecutionID, turnKey), o.RunID, model.NodeAssistantTurn)
		g.stamp(n, o)
		setAgentID(n, o)
		g.stampEnd(n, o)
		g.applyTokens(n, o.Attrs)
		g.applyCost(n, o.Attrs)
		if m, ok := o.Attrs["model"].(string); ok && m != "" {
			if n.Attrs == nil {
				n.Attrs = map[string]any{}
			}
			n.Attrs["model"] = m
		}
		g.mergePayload(n, o.Payload, o.Source)
		g.parentTurn(o, n.ID)
	case "assistant_tool_use", "tool_result":
		g.applyTool(o)
	case "subagent_stop":
		g.applySubagent(o)
	case "marker":
		if mn, mb, ms, mocc, mok := extractMarkerFromAttrs(o); mok {
			s := g.execState(o.ExecutionID)
			mergeMarkerBound(s, o.ObsID, mn, mb, ms, mocc, o.Correlation.AgentID, o.EventTime, o.Seq)
		} else {
			n := g.node(model.MarkerID(o.ExecutionID, o.ObsID), o.RunID, model.NodeMarker)
			g.stamp(n, o)
			n.Attrs = o.Attrs
			g.upsertEdge(o.ExecutionID, o.RunID, model.SessionNodeID(o.ExecutionID), n.ID, o.Seq)
		}
	}
}

func nodeKey(primary, obs string) string {
	if primary != "" {
		return primary
	}
	return "obs:" + obs
}

func (g *Graph) applyTool(o model.Observation) {
	name, _ := o.Attrs["name"].(string)
	s := g.execState(o.ExecutionID)
	if isMarkerTool(name) {
		key := o.Correlation.ToolUseID
		if key == "" {
			key = o.ObsID
		}
		if mn, mb, ms, mocc, mok := extractMarkerFromPayload(o); mok {
			mergeMarkerBound(s, key, mn, mb, ms, mocc, o.Correlation.AgentID, o.EventTime, o.Seq)
		}
		if o.Correlation.ToolUseID != "" {
			s.markerTools[o.Correlation.ToolUseID] = true
		}
		return
	}
	if o.Correlation.ToolUseID != "" && s.markerTools[o.Correlation.ToolUseID] {
		return
	}

	id := model.ToolCallID(o.ExecutionID, nodeKey(o.Correlation.ToolUseID, o.ObsID))
	nodeType := model.NodeToolCall
	switch {
	case isMCP(name):
		nodeType = model.NodeMCPCall
	case isSkill(name):
		nodeType = model.NodeSkill
	}
	n := g.node(id, o.RunID, nodeType)
	if n.Type == model.NodeToolCall && nodeType != model.NodeToolCall {
		n.Type = nodeType
	}
	g.stamp(n, o)
	setAgentID(n, o)
	if o.Kind == "tool_result" {
		g.stampEnd(n, o)
	}
	if dn, strong := toolDisplayName(o, name); dn != "" {
		g.setName(n, o, dn, strong)
	}
	if s, ok := o.Attrs["status"].(string); ok {
		n.Status = resolveStatus(n.Status, model.Status(s))
	}
	g.mergePayload(n, o.Payload, o.Source)
	switch {
	case o.Correlation.ParentToolUseID != "":
		g.upsertParentToolEdge(o)
	case o.Correlation.MessageID != "":
		g.setStructParent(o, structKindTurn, model.AssistantTurnID(o.ExecutionID, o.Correlation.MessageID), id)
	default:
		g.setStructParent(o, structKindSession, model.SessionNodeID(o.ExecutionID), id)
	}
}

func (g *Graph) applySubagent(o model.Observation) {
	id := model.SubagentID(o.ExecutionID, o.Correlation.AgentID)
	n := g.node(id, o.RunID, model.NodeSubagent)
	g.stamp(n, o)
	if o.Correlation.AgentID != "" {
		n.AgentID = o.Correlation.AgentID
	}
	if t, ok := o.Attrs["subagent_type"].(string); ok && n.SubagentType == "" {
		n.SubagentType = t
	}
	if d, ok := o.Attrs["description"].(string); ok && d != "" && n.Name == "" {
		n.Name = d
	}
	g.stampEnd(n, o)
	n.Status = resolveStatus(n.Status, model.StatusOK)
	if o.Correlation.ParentToolUseID != "" {
		g.setStructParent(o, structKindParentTool, model.ToolCallID(o.ExecutionID, o.Correlation.ParentToolUseID), id)
		return
	}
	g.setStructParent(o, structKindSession, model.SessionNodeID(o.ExecutionID), id)
}

func sourceRank(s model.Source) int {
	switch s {
	case model.SourceHook:
		return 2
	case model.SourceJSONL:
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
	if s == model.SourceJSONL {
		return 3
	}
	return 0
}

const (
	structKindSession    = 0
	structKindTurn       = 1
	structKindParentTool = 2
)

func (g *Graph) upsertParentToolEdge(o model.Observation) {
	if o.Correlation.ParentToolUseID == "" || o.Correlation.ToolUseID == "" {
		return
	}
	src := model.ToolCallID(o.ExecutionID, o.Correlation.ParentToolUseID)
	dst := model.ToolCallID(o.ExecutionID, o.Correlation.ToolUseID)
	g.setStructParent(o, structKindParentTool, src, dst)
}

func (g *Graph) setStructParent(o model.Observation, kind int, src, dst string) {
	fs := g.stampsFor(dst)
	r := structureRank(o.Source)
	if fs.haveStruct && (kind < fs.structKind || (kind == fs.structKind && r < fs.structRank)) {
		return
	}
	if fs.haveStruct && fs.structSrc != src {
		oldID := model.EdgeID(o.ExecutionID, model.EdgeParentChild, fs.structSrc, dst)
		delete(g.Edges, oldID)
	}
	fs.structKind = kind
	fs.structRank = r
	fs.haveStruct = true
	fs.structSrc = src
	g.upsertEdge(o.ExecutionID, o.RunID, src, dst, o.Seq)
}

func precedingPromptID(gp *agentGroup, seq uint64) string {
	i := sort.Search(len(gp.prompts), func(i int) bool { return gp.prompts[i].seq >= seq })
	if i == 0 {
		return gp.root
	}
	return gp.prompts[i-1].id
}

func (g *Graph) parentTurn(o model.Observation, turnID string) {
	s := g.execState(o.ExecutionID)
	t, ok := s.turnsByID[turnID]
	if !ok {
		t = &turnRef{seq: o.Seq, rev: o.Seq, id: turnID, agentID: o.Correlation.AgentID}
		s.turnsByID[turnID] = t
		gp := s.group(t.agentID)
		gp.turns = insertTurnSorted(gp.turns, t)
	}
	if o.Seq > t.rev {
		t.rev = o.Seq
	}
	if o.Seq < t.seq {
		t.seq = o.Seq
		gp := s.group(t.agentID)
		gp.turns = repositionTurn(gp.turns, t)
	}
	gp := s.group(t.agentID)
	g.setTurnParent(o, t, precedingPromptID(gp, t.seq))
}

func (g *Graph) recordPrompt(o model.Observation, promptID string) {
	s := g.execState(o.ExecutionID)
	gp := s.group(o.Correlation.AgentID)
	p := promptRef{seq: o.Seq, id: promptID, agentID: o.Correlation.AgentID}
	i := sort.Search(len(gp.prompts), func(i int) bool { return gp.prompts[i].seq >= p.seq })
	gp.prompts = append(gp.prompts, promptRef{})
	copy(gp.prompts[i+1:], gp.prompts[i:])
	gp.prompts[i] = p
	next := ^uint64(0)
	if i+1 < len(gp.prompts) {
		next = gp.prompts[i+1].seq
	}
	lo := sort.Search(len(gp.turns), func(j int) bool { return gp.turns[j].seq > p.seq })
	for j := lo; j < len(gp.turns) && gp.turns[j].seq < next; j++ {
		g.setTurnParent(o, gp.turns[j], precedingPromptID(gp, gp.turns[j].seq))
	}
}

func insertTurnSorted(turns []*turnRef, t *turnRef) []*turnRef {
	i := sort.Search(len(turns), func(i int) bool { return turns[i].seq >= t.seq })
	turns = append(turns, nil)
	copy(turns[i+1:], turns[i:])
	turns[i] = t
	return turns
}

func repositionTurn(turns []*turnRef, t *turnRef) []*turnRef {
	var idx int
	for i, x := range turns {
		if x == t {
			idx = i
			break
		}
	}
	turns = append(turns[:idx], turns[idx+1:]...)
	return insertTurnSorted(turns, t)
}

func (g *Graph) setTurnParent(o model.Observation, t *turnRef, parent string) {
	if t.parent == parent {
		g.upsertEdge(o.ExecutionID, o.RunID, parent, t.id, t.rev)
		return
	}
	if t.parent != "" {
		oldID := model.EdgeID(o.ExecutionID, model.EdgeParentChild, t.parent, t.id)
		delete(g.Edges, oldID)
	}
	t.parent = parent
	g.upsertEdge(o.ExecutionID, o.RunID, parent, t.id, t.rev)
}

type fieldStamps struct {
	timingRank  int
	haveTiming  bool
	nameSeq     uint64
	haveName    bool
	nameStrong  bool
	payloadRank int
	havePayload bool
	structRank  int
	structKind  int
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
	case r == fs.endRank && o.EventTime.After(*n.TEnd):
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

func (g *Graph) setName(n *model.Node, o model.Observation, name string, strong bool) {
	fs := g.stampsFor(n.ID)
	if !fs.haveName || (strong && !fs.nameStrong) || (strong == fs.nameStrong && o.Seq < fs.nameSeq) {
		n.Name = name
		fs.nameSeq = o.Seq
		fs.haveName = true
		fs.nameStrong = strong
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

func (g *Graph) applyTokens(n *model.Node, attrs map[string]any) {
	if v, ok := toInt64(attrs["tokens_in"]); ok {
		n.TokensIn = &v
	}
	if v, ok := toInt64(attrs["tokens_out"]); ok {
		n.TokensOut = &v
	}
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
	if v, ok := toInt64(attrs["cache_read_in"]); ok {
		in.CacheReadIn = v
	}
	if v, ok := toInt64(attrs["cache_write"]); ok {
		in.CacheWrite = v
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
	if o.Seq > r.LastSeq {
		r.LastSeq = o.Seq
	}
	r.SessionIDs = appendUnique(r.SessionIDs, o.Correlation.SessionID)
	if r.ModelID == "" {
		if m, ok := o.Attrs["model"].(string); ok && m != "" {
			r.ModelID = m
		}
	}
	if v, ok := o.Attrs["claude_code_version"].(string); ok && v != "" {
		if r.Repro == nil {
			r.Repro = &model.ReproMeta{}
		}
		if r.Repro.ClaudeCodeVersion == "" {
			r.Repro.ClaudeCodeVersion = v
		}
	}
	if v, ok := o.Attrs["cwd"].(string); ok && v != "" {
		if r.Repro == nil {
			r.Repro = &model.ReproMeta{}
		}
		if r.Repro.Cwd == "" {
			r.Repro.Cwd = v
		}
	}
	if raw, ok := o.Attrs["catacomb.labels"].(string); ok && raw != "" {
		r.Labels = model.MergeLabels(r.Labels, model.ParseLabels(raw))
	}
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

func rank(s model.Status) int {
	switch s {
	case model.StatusOK, model.StatusError:
		return 3
	case model.StatusRunning:
		return 1
	default:
		return 0
	}
}

func terminalRank(s model.Status) int {
	if s == model.StatusError {
		return 2
	}
	return 0
}

func resolveStatus(cur, next model.Status) model.Status {
	rc, rn := rank(cur), rank(next)
	if rc == 3 && rn == 3 {
		if terminalRank(next) > terminalRank(cur) {
			return next
		}
		return cur
	}
	if rc == 3 && rn < 3 {
		return cur
	}
	if rn >= rc {
		return next
	}
	return cur
}
