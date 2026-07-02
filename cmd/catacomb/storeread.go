package main

import (
	"errors"
	"fmt"
	"os"
	"sort"

	"github.com/realkarych/catacomb/aggregate"
	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/pricing"
	"github.com/realkarych/catacomb/reduce"
	"github.com/realkarych/catacomb/store"
)

func openReadStore(open storeOpener, dbPath string) (store.Store, error) {
	if _, err := os.Stat(dbPath); errors.Is(err, os.ErrNotExist) {
		return nil, ErrStoreNotFound
	}
	s, err := open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("store open: %w", err)
	}
	return s, nil
}

func storeGraphs(s store.Store, pricer reduce.Pricer) ([]*reduce.Graph, error) {
	graphs, _, err := storeGraphsWithIDs(s, pricer)
	return graphs, err
}

func storeGraphsWithIDs(s store.Store, pricer reduce.Pricer) ([]*reduce.Graph, []string, error) {
	obs, err := s.ObservationsSince(0)
	if err != nil {
		return nil, nil, fmt.Errorf("store read: %w", err)
	}

	var order []string
	groups := map[string][]model.Observation{}
	for _, o := range obs {
		if _, seen := groups[o.ExecutionID]; !seen {
			order = append(order, o.ExecutionID)
		}
		groups[o.ExecutionID] = append(groups[o.ExecutionID], o)
	}

	graphs := make([]*reduce.Graph, 0, len(order))
	for _, id := range order {
		g := reduce.NewGraphWithPricer(pricer)
		g.ApplyAll(groups[id])
		graphs = append(graphs, g)
	}
	return graphs, order, nil
}

func loadRunGroup(s store.Store, pricer reduce.Pricer, selector map[string]string) ([]aggregate.RunGraph, error) {
	graphs, ids, err := storeGraphsWithIDs(s, pricer)
	if err != nil {
		return nil, err
	}
	for i, g := range graphs {
		anns, err := s.AnnotationsForExecution(ids[i])
		if err != nil {
			return nil, fmt.Errorf("store annotations: %w", err)
		}
		g.ApplyAnnotations(anns)
	}
	var out []aggregate.RunGraph
	for _, r := range collectRuns(graphs) {
		if !model.MatchLabels(r.Labels, selector) {
			continue
		}
		nodes, edges := collectSnapshot(graphs, r.ID)
		out = append(out, aggregate.RunGraph{Run: r, Nodes: nodes, Edges: edges})
	}
	return out, nil
}

func collectRuns(graphs []*reduce.Graph) []model.Run {
	seen := map[string]bool{}
	var out []model.Run
	for _, g := range graphs {
		for _, r := range g.RunsSnapshot() {
			if !seen[r.ID] {
				seen[r.ID] = true
				out = append(out, r)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func collectRunsFor(graphs []*reduce.Graph, runID string) []model.Run {
	all := collectRuns(graphs)
	if runID == "" {
		return all
	}
	var out []model.Run
	for _, r := range all {
		if r.ID == runID {
			out = append(out, r)
		}
	}
	return out
}

func collectSnapshot(graphs []*reduce.Graph, runID string) ([]*model.Node, []*model.Edge) {
	var nodes []*model.Node
	var edges []*model.Edge
	for _, g := range graphs {
		ns, es := g.Snapshot()
		for _, n := range ns {
			if runID == "" || n.RunID == runID {
				nodes = append(nodes, n)
			}
		}
		for _, e := range es {
			if runID == "" || e.RunID == runID {
				edges = append(edges, e)
			}
		}
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	sort.Slice(edges, func(i, j int) bool { return edges[i].ID < edges[j].ID })
	return nodes, edges
}

func newPricer() reduce.Pricer {
	eng := pricing.New()
	return reduce.PricerFunc(func(in reduce.PriceInputs) (reduce.PriceResult, bool) {
		r, ok := eng.Cost(pricing.Inputs{
			ModelID:     in.ModelID,
			TokensIn:    in.TokensIn,
			TokensOut:   in.TokensOut,
			CacheReadIn: in.CacheReadIn,
			CacheWrite:  in.CacheWrite,
			ReportedUSD: in.ReportedUSD,
		})
		return reduce.PriceResult{USD: r.USD, Source: r.Source}, ok
	})
}

func formatCost(c *float64) string {
	if c == nil {
		return "-"
	}
	return fmt.Sprintf("$%.4f", *c)
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
