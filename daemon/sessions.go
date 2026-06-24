package daemon

import (
	"errors"
	"slices"
	"sort"
	"time"

	"github.com/realkarych/catacomb/cdc"
	"github.com/realkarych/catacomb/model"
)

var ErrSessionNotFound = errors.New("daemon: session not found")

type SessionSummary struct {
	Session        string         `json:"session"`
	Status         string         `json:"status"`
	StartedAt      string         `json:"started_at,omitempty"`
	EndedAt        string         `json:"ended_at,omitempty"`
	DurationMS     *int64         `json:"duration_ms,omitempty"`
	TokensIn       int64          `json:"tokens_in"`
	TokensOut      int64          `json:"tokens_out"`
	CostUSD        *float64       `json:"cost_usd,omitempty"`
	CostSource     string         `json:"cost_source"`
	NodeCount      int            `json:"node_count"`
	ToolCount      int            `json:"tool_count"`
	ErrorCount     int            `json:"error_count"`
	ModelID        string         `json:"model_id,omitempty"`
	RunIDs         []string       `json:"run_ids"`
	CountsByType   map[string]int `json:"counts_by_type"`
	CountsByStatus map[string]int `json:"counts_by_status"`
	ErrorRate      float64        `json:"error_rate"`
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
		for _, n := range nodes {
			nc := copyNode(n)
			nc.Payload = nil
			out = append(out, deltaToSSE(cdc.GraphDelta{
				Kind:        cdc.DeltaNodeUpsert,
				Rev:         n.Rev,
				Node:        nc,
				RunID:       n.RunID,
				ExecutionID: execID,
			}))
		}
		for _, e := range edges {
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
		hasCost   bool
		totalCost float64
		srcRank   int
		tokensIn  int64
		tokensOut int64
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
			sum.CountsByType[string(n.Type)]++
			sum.CountsByStatus[string(n.Status)]++
			if n.Type == model.NodeToolCall || n.Type == model.NodeMCPCall {
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
