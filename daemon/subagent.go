package daemon

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/realkarych/catacomb/cdc"
	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/reduce"
)

type subAgg struct {
	count     int
	tokensIn  int64
	tokensOut int64
	cost      float64
	hasCost   bool
}

func isInnerNode(execID string, n *model.Node) bool {
	if n == nil || n.AgentID == "" {
		return false
	}
	return n.ID != model.SubagentID(execID, n.AgentID)
}

func parentEdgeSources(g *reduce.Graph) map[string]string {
	out := map[string]string{}
	if g == nil {
		return out
	}
	for _, e := range g.Edges {
		if e.Type == model.EdgeParentChild {
			out[e.Dst] = e.Src
		}
	}
	return out
}

func parentNode(g *reduce.Graph, parents map[string]string, nodeID string) *model.Node {
	if g == nil {
		return nil
	}
	return g.Nodes[parents[nodeID]]
}

func nestedSubagent(g *reduce.Graph, parents map[string]string, execID string, n *model.Node) bool {
	if n == nil || n.Type != model.NodeSubagent {
		return false
	}
	return isInnerNode(execID, parentNode(g, parents, n.ID))
}

func topLevelExcluded(g *reduce.Graph, parents map[string]string, execID string, n *model.Node) bool {
	return isInnerNode(execID, n) || nestedSubagent(g, parents, execID, n)
}

func subtreeMember(g *reduce.Graph, parents map[string]string, execID, agentID string, n *model.Node) bool {
	if isInnerNode(execID, n) && n.AgentID == agentID {
		return true
	}
	if n.Type == model.NodeSubagent {
		if p := parentNode(g, parents, n.ID); p != nil && p.AgentID == agentID {
			return true
		}
	}
	return false
}

func (a *subAgg) add(n *model.Node) {
	a.count++
	if n.TokensIn != nil {
		a.tokensIn += *n.TokensIn
	}
	if n.TokensOut != nil {
		a.tokensOut += *n.TokensOut
	}
	if n.CostUSD != nil {
		a.hasCost = true
		a.cost += *n.CostUSD
	}
}

func subagentRollups(g *reduce.Graph, execID string) map[string]*subAgg {
	out := map[string]*subAgg{}
	for _, n := range g.Nodes {
		if !isInnerNode(execID, n) {
			continue
		}
		a, ok := out[n.AgentID]
		if !ok {
			a = &subAgg{}
			out[n.AgentID] = a
		}
		a.add(n)
	}
	return out
}

func subagentAggregate(g *reduce.Graph, execID, agentID string) subAgg {
	var a subAgg
	if g == nil {
		return a
	}
	for _, n := range g.Nodes {
		if n.AgentID != agentID || !isInnerNode(execID, n) {
			continue
		}
		a.add(n)
	}
	return a
}

func applyAggregate(nc *model.Node, a *subAgg) {
	if nc.Attrs == nil {
		nc.Attrs = map[string]any{}
	}
	nc.Attrs["descendant_count"] = a.count
	nc.Attrs["descendant_tokens_in"] = a.tokensIn
	nc.Attrs["descendant_tokens_out"] = a.tokensOut
	if a.hasCost {
		nc.Attrs["descendant_cost_usd"] = a.cost
	}
}

func decorateSubagent(nc *model.Node, rollups map[string]*subAgg) {
	if nc.Type != model.NodeSubagent || nc.AgentID == "" {
		return
	}
	a := rollups[nc.AgentID]
	if a == nil {
		a = &subAgg{}
	}
	applyAggregate(nc, a)
}

func (d *Daemon) subagentSubtreeDeltas(hash, agentID string) ([]sseEvent, error) {
	execs := d.executionsForSession(hash)
	if len(execs) == 0 {
		return nil, ErrSessionNotFound
	}
	out := []sseEvent{}
	for _, execID := range execs {
		g := d.graphs[execID]
		subNodeID := model.SubagentID(execID, agentID)
		parents := parentEdgeSources(g)
		rollups := subagentRollups(g, execID)
		included := map[string]bool{}
		for _, n := range g.Nodes {
			if subtreeMember(g, parents, execID, agentID, n) {
				included[n.ID] = true
			}
		}
		for _, n := range g.Nodes {
			if !included[n.ID] {
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
		for _, e := range g.Edges {
			if (included[e.Src] && included[e.Dst]) || e.Src == subNodeID {
				out = append(out, deltaToSSE(cdc.GraphDelta{
					Kind:        cdc.DeltaEdgeUpsert,
					Rev:         e.Rev,
					Edge:        copyEdge(e),
					RunID:       e.RunID,
					ExecutionID: execID,
				}))
			}
		}
	}
	return out, nil
}

func (d *Daemon) handleSubagentSubtree(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	agentID := r.PathValue("agentId")
	d.mu.Lock()
	evs, err := d.subagentSubtreeDeltas(hash, agentID)
	d.mu.Unlock()
	if errors.Is(err, ErrSessionNotFound) {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(evs)
}
