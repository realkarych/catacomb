package aggregate

import (
	"sort"

	"github.com/realkarych/catacomb/model"
)

type Cell struct {
	RunID      string            `json:"run_id"`
	Labels     map[string]string `json:"labels,omitempty"`
	DurationMS float64           `json:"duration_ms"`
	CostUSD    float64           `json:"cost_usd"`
	TokensIn   float64           `json:"tokens_in"`
	TokensOut  float64           `json:"tokens_out"`
	Turns      float64           `json:"turns"`
}

func Cells(group []RunGraph) []Cell {
	cells := make([]Cell, 0, len(group))
	for _, rg := range group {
		sums := runNodeSums(rg)
		duration, _ := runDuration(rg.Run)
		cells = append(cells, Cell{
			RunID:      rg.Run.ID,
			Labels:     rg.Run.Labels,
			DurationMS: duration,
			CostUSD:    sums.cost,
			TokensIn:   sums.tokensIn,
			TokensOut:  sums.tokensOut,
			Turns:      countAssistantTurns(rg),
		})
	}
	sort.Slice(cells, func(i, j int) bool { return cells[i].RunID < cells[j].RunID })
	return cells
}

func countAssistantTurns(rg RunGraph) float64 {
	var turns float64
	for _, n := range rg.Nodes {
		if n.Type == model.NodeAssistantTurn {
			turns++
		}
	}
	return turns
}
