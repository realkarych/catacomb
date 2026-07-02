package daemon

import (
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/realkarych/catacomb/cdc"
	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/redact"
	"github.com/realkarych/catacomb/reduce"
)

var ErrSessionNotFound = errors.New("daemon: session not found")

type SessionSummary struct {
	Session        string            `json:"session"`
	Label          string            `json:"label,omitempty"`
	Status         string            `json:"status"`
	StartedAt      string            `json:"started_at,omitempty"`
	EndedAt        string            `json:"ended_at,omitempty"`
	LastActivity   string            `json:"last_activity,omitempty"`
	DurationMS     *int64            `json:"duration_ms,omitempty"`
	TokensIn       int64             `json:"tokens_in"`
	TokensOut      int64             `json:"tokens_out"`
	CostUSD        *float64          `json:"cost_usd,omitempty"`
	CostSource     string            `json:"cost_source"`
	NodeCount      int               `json:"node_count"`
	ToolCount      int               `json:"tool_count"`
	ErrorCount     int               `json:"error_count"`
	ModelID        string            `json:"model_id,omitempty"`
	RunIDs         []string          `json:"run_ids"`
	CountsByType   map[string]int    `json:"counts_by_type"`
	CountsByStatus map[string]int    `json:"counts_by_status"`
	ErrorRate      float64           `json:"error_rate"`
	Repro          *model.ReproMeta  `json:"repro,omitempty"`
	Labels         map[string]string `json:"labels,omitempty"`
}

func (d *Daemon) sessionGraphDeltas(hash string) ([]sseEvent, error) {
	execs := d.executionsForSession(hash)
	if len(execs) == 0 {
		return nil, ErrSessionNotFound
	}
	out := []sseEvent{}
	for _, execID := range execs {
		g := d.graphs[execID]
		nodes, edges := g.Snapshot()
		parents := parentEdgeSources(g)
		rollups := subagentRollups(g, execID)
		for _, n := range nodes {
			if topLevelExcluded(g, parents, execID, n) {
				continue
			}
			nc := copyNode(n)
			nc.Payload = nil
			decorateSubagent(nc, rollups)
			out = append(out, deltaToSSE(cdc.GraphDelta{
				Kind:        cdc.DeltaNodeUpsert,
				Rev:         n.Rev,
				Node:        nc,
				RunID:       n.RunID,
				ExecutionID: execID,
			}))
		}
		for _, e := range edges {
			if topLevelExcluded(g, parents, execID, g.Nodes[e.Src]) || topLevelExcluded(g, parents, execID, g.Nodes[e.Dst]) {
				continue
			}
			out = append(out, deltaToSSE(cdc.GraphDelta{
				Kind:        cdc.DeltaEdgeUpsert,
				Rev:         e.Rev,
				Edge:        copyEdge(e),
				RunID:       e.RunID,
				ExecutionID: execID,
			}))
		}
	}
	return out, nil
}

func (d *Daemon) sessionSummaries() []SessionSummary {
	hashes := map[string]bool{}
	for _, g := range d.graphs {
		for _, r := range g.Runs {
			for _, h := range r.SessionIDs {
				hashes[h] = true
			}
		}
	}
	out := make([]SessionSummary, 0, len(hashes))
	for h := range hashes {
		out = append(out, d.summarizeSession(h))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Session < out[j].Session })
	return out
}

func statusRank(s model.Status) int {
	switch s {
	case model.StatusError:
		return 3
	case model.StatusRunning:
		return 2
	case model.StatusOK:
		return 1
	default:
		return 0
	}
}

func foldStatus(cur string, s model.Status) string {
	candidate := string(s)
	if statusRank(s) > statusRank(model.Status(cur)) {
		return candidate
	}
	return cur
}

func nodeActivity(n *model.Node) *time.Time {
	if n.TEnd != nil {
		return n.TEnd
	}
	return n.TStart
}

func graphSlice(m map[string]*reduce.Graph) []*reduce.Graph {
	out := make([]*reduce.Graph, 0, len(m))
	for _, g := range m {
		out = append(out, g)
	}
	return out
}

func summarizeGraphs(key string, graphs []*reduce.Graph, match func(*model.Run) bool) SessionSummary {
	sum := SessionSummary{
		Session:        key,
		RunIDs:         []string{},
		CountsByType:   map[string]int{},
		CountsByStatus: map[string]int{},
	}
	var (
		tStart    *time.Time
		tEnd      *time.Time
		lastAct   *time.Time
		hasCost   bool
		totalCost float64
		srcRank   int
		tokensIn  int64
		tokensOut int64
	)
	runSeen := map[string]bool{}
	for _, g := range graphs {
		for runID, r := range g.Runs {
			if !match(r) {
				continue
			}
			if !runSeen[runID] {
				runSeen[runID] = true
				sum.RunIDs = append(sum.RunIDs, runID)
			}
			if sum.ModelID == "" && r.ModelID != "" {
				sum.ModelID = r.ModelID
			}
			if sum.Labels == nil && len(r.Labels) > 0 {
				sum.Labels = r.Labels
			}
			if sum.Repro == nil && r.Repro != nil {
				sum.Repro = r.Repro
			}
			sum.Status = foldStatus(sum.Status, r.Status)
			if r.StartedAt != nil {
				if tStart == nil || r.StartedAt.Before(*tStart) {
					t := *r.StartedAt
					tStart = &t
				}
			}
			if r.EndedAt != nil {
				if tEnd == nil || r.EndedAt.After(*tEnd) {
					t := *r.EndedAt
					tEnd = &t
				}
			}
		}
		for _, n := range g.Nodes {
			r, ok := g.Runs[n.RunID]
			if !ok || !match(r) {
				continue
			}
			sum.NodeCount++
			if act := nodeActivity(n); act != nil && (lastAct == nil || act.After(*lastAct)) {
				lastAct = act
			}
			sum.CountsByType[string(n.Type)]++
			sum.CountsByStatus[string(n.Status)]++
			if n.Type == model.NodeToolCall || n.Type == model.NodeMCPCall || n.Type == model.NodeSkill {
				sum.ToolCount++
			}
			if n.Status == model.StatusError {
				sum.ErrorCount++
			}
			if n.TokensIn != nil {
				tokensIn += *n.TokensIn
			}
			if n.TokensOut != nil {
				tokensOut += *n.TokensOut
			}
			if n.CostUSD != nil {
				hasCost = true
				totalCost += *n.CostUSD
				src, _ := n.Attrs["cost_source"].(string)
				var rank int
				switch src {
				case "reported":
					rank = 2
				case "estimated":
					rank = 1
				}
				if rank > srcRank {
					srcRank = rank
					sum.CostSource = src
				}
			}
		}
	}
	if sum.NodeCount > 0 {
		sum.ErrorRate = float64(sum.ErrorCount) / float64(sum.NodeCount)
	}
	if tStart != nil {
		sum.StartedAt = tStart.UTC().Format(time.RFC3339)
	}
	if tEnd != nil {
		sum.EndedAt = tEnd.UTC().Format(time.RFC3339)
	}
	if lastAct != nil {
		sum.LastActivity = lastAct.UTC().Format(time.RFC3339)
	}
	if tStart != nil && tEnd != nil {
		ms := tEnd.Sub(*tStart).Milliseconds()
		sum.DurationMS = &ms
	}
	if hasCost {
		sum.CostUSD = &totalCost
	}
	sum.TokensIn = tokensIn
	sum.TokensOut = tokensOut
	sort.Strings(sum.RunIDs)
	return sum
}

func SummarizeRun(runID string, graphs []*reduce.Graph) SessionSummary {
	return summarizeGraphs(runID, graphs, func(r *model.Run) bool { return r.ID == runID })
}

func SummarizeSession(hash string, graphs []*reduce.Graph) SessionSummary {
	return summarizeGraphs(hash, graphs, func(r *model.Run) bool { return slices.Contains(r.SessionIDs, hash) })
}

func (d *Daemon) summarizeSession(hash string) SessionSummary {
	sum := SessionSummary{
		Session:        hash,
		RunIDs:         []string{},
		CountsByType:   map[string]int{},
		CountsByStatus: map[string]int{},
	}
	var (
		tStart    *time.Time
		tEnd      *time.Time
		lastAct   *time.Time
		hasCost   bool
		totalCost float64
		srcRank   int
		tokensIn  int64
		tokensOut int64
		labelNode *model.Node
	)
	runSeen := map[string]bool{}
	for _, execID := range d.executionsForSession(hash) {
		g := d.graphs[execID]
		for runID, r := range g.Runs {
			if !slices.Contains(r.SessionIDs, hash) {
				continue
			}
			if !runSeen[runID] {
				runSeen[runID] = true
				sum.RunIDs = append(sum.RunIDs, runID)
			}
			if sum.ModelID == "" && r.ModelID != "" {
				sum.ModelID = r.ModelID
			}
			if sum.Repro == nil && r.Repro != nil {
				sum.Repro = r.Repro
			}
			sum.Status = foldStatus(sum.Status, r.Status)
			if r.StartedAt != nil {
				if tStart == nil || r.StartedAt.Before(*tStart) {
					t := *r.StartedAt
					tStart = &t
				}
			}
			if r.EndedAt != nil {
				if tEnd == nil || r.EndedAt.After(*tEnd) {
					t := *r.EndedAt
					tEnd = &t
				}
			}
		}
		for _, n := range g.Nodes {
			r, ok := g.Runs[n.RunID]
			if !ok || !slices.Contains(r.SessionIDs, hash) {
				continue
			}
			sum.NodeCount++
			if d.allowPayloadAccess && isPromptLabelCandidate(n) && promptNodeBefore(n, labelNode) {
				labelNode = n
			}
			if act := nodeActivity(n); act != nil && (lastAct == nil || act.After(*lastAct)) {
				lastAct = act
			}
			sum.CountsByType[string(n.Type)]++
			sum.CountsByStatus[string(n.Status)]++
			if n.Type == model.NodeToolCall || n.Type == model.NodeMCPCall || n.Type == model.NodeSkill {
				sum.ToolCount++
			}
			if n.Status == model.StatusError {
				sum.ErrorCount++
			}
			if n.TokensIn != nil {
				tokensIn += *n.TokensIn
			}
			if n.TokensOut != nil {
				tokensOut += *n.TokensOut
			}
			if n.CostUSD != nil {
				hasCost = true
				totalCost += *n.CostUSD
				src, _ := n.Attrs["cost_source"].(string)
				var rank int
				switch src {
				case "reported":
					rank = 2
				case "estimated":
					rank = 1
				}
				if rank > srcRank {
					srcRank = rank
					sum.CostSource = src
				}
			}
		}
	}
	if sum.NodeCount > 0 {
		sum.ErrorRate = float64(sum.ErrorCount) / float64(sum.NodeCount)
	}
	if tStart != nil {
		sum.StartedAt = tStart.UTC().Format(time.RFC3339)
	}
	if tEnd != nil {
		sum.EndedAt = tEnd.UTC().Format(time.RFC3339)
	}
	if lastAct != nil {
		sum.LastActivity = lastAct.UTC().Format(time.RFC3339)
	}
	if tStart != nil && tEnd != nil {
		ms := tEnd.Sub(*tStart).Milliseconds()
		sum.DurationMS = &ms
	}
	if hasCost {
		sum.CostUSD = &totalCost
	}
	sum.TokensIn = tokensIn
	sum.TokensOut = tokensOut
	if labelNode != nil {
		sum.Label = sessionLabelFromPayload(labelNode.Payload.Input)
	}
	sort.Strings(sum.RunIDs)
	return sum
}

const sessionLabelMaxRunes = 60

func isPromptLabelCandidate(n *model.Node) bool {
	if n.Type != model.NodeUserPrompt {
		return false
	}
	if pk, _ := n.Attrs["prompt_kind"].(string); pk == "system" {
		return false
	}
	return n.Payload != nil && len(n.Payload.Input) > 0
}

func promptSortKey(n *model.Node) string {
	if n.TStart == nil {
		return "\uffff" + n.ID
	}
	return n.TStart.UTC().Format(time.RFC3339Nano) + "\x00" + n.ID
}

func promptNodeBefore(n, cur *model.Node) bool {
	if cur == nil {
		return true
	}
	return promptSortKey(n) < promptSortKey(cur)
}

func sessionLabelFromPayload(raw json.RawMessage) string {
	text := collapseWhitespace(promptLabelText(redact.Redact(raw).Data))
	if text == "" {
		return ""
	}
	return truncateLabel(text)
}

func promptLabelText(raw json.RawMessage) string {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func truncateLabel(s string) string {
	r := []rune(s)
	if len(r) <= sessionLabelMaxRunes {
		return s
	}
	return string(r[:sessionLabelMaxRunes]) + "…"
}
