package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/realkarych/catacomb/ingest/drift"
	ijsonl "github.com/realkarych/catacomb/ingest/jsonl"
	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/redact"
	"github.com/realkarych/catacomb/reduce"
)

var driftOut io.Writer = os.Stderr

func parseTranscripts(main string, subs []string, executionID string) ([]model.Observation, error) {
	var all []model.Observation
	var counts drift.Counts
	for _, p := range append([]string{main}, subs...) {
		f, err := os.Open(p)
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", p, err)
		}
		var seq uint64
		next := func() uint64 { s := seq; seq++; return s }
		obs, dc, perr := ijsonl.Parse(f, executionID, next, func(ts time.Time) time.Time { return ts })
		if rerr := errors.Join(perr, f.Close()); rerr != nil {
			return nil, fmt.Errorf("read %s: %w", p, rerr)
		}
		counts = counts.Merge(dc)
		all = append(all, obs...)
	}
	for i := range all {
		all[i].Seq = uint64(i + 1)
	}
	warnDrift(counts)
	return all, nil
}

func warnDrift(counts drift.Counts) {
	var total uint64
	for _, n := range counts {
		total += n
	}
	if total == 0 {
		return
	}
	reasons := make([]string, 0, len(counts))
	for r := range counts {
		reasons = append(reasons, r)
	}
	sort.Strings(reasons)
	parts := make([]string, len(reasons))
	for i, r := range reasons {
		parts[i] = fmt.Sprintf("%s=%d", r, counts[r])
	}
	fmt.Fprintf(driftOut, "warning: %d unrecognized transcript record(s) [%s]\n", total, strings.Join(parts, ", "))
}

func boundaryObservations(sessionID, name string, start, end time.Time) []model.Observation {
	return []model.Observation{
		markerObservation(sessionID, name, "start", start),
		markerObservation(sessionID, name, "end", end),
	}
}

func markerObservation(sessionID, name, boundary string, at time.Time) model.Observation {
	return model.Observation{
		ObsID:       ulid.Make().String(),
		RunID:       sessionID,
		Source:      model.SourceHook,
		Kind:        "marker",
		Correlation: model.Correlation{SessionID: sessionID},
		Attrs:       map[string]any{"name": name, "boundary": boundary},
		EventTime:   at.UTC(),
		ObservedAt:  at.UTC(),
	}
}

func loadGraphOffline(main string, subs []string, executionID string, pricer reduce.Pricer, extra []model.Observation) (*reduce.Graph, error) {
	obs, err := parseTranscripts(main, subs, executionID)
	if err != nil {
		return nil, err
	}
	base := len(obs)
	for i := range extra {
		extra[i].ExecutionID = executionID
		extra[i].Seq = uint64(base + i + 1)
		obs = append(obs, extra[i])
	}
	policy := redact.DefaultPolicy()
	for i := range obs {
		obs[i] = policy.Observation(obs[i])
	}
	g := reduce.NewGraph()
	if pricer != nil {
		g = reduce.NewGraphWithPricer(pricer)
	}
	g.ApplyAll(obs)
	return g, nil
}

func sortedGraphSnapshot(g *reduce.Graph) ([]*model.Node, []*model.Edge) {
	nodes, edges := g.Snapshot()
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	sort.Slice(edges, func(i, j int) bool { return edges[i].ID < edges[j].ID })
	return nodes, edges
}

func graphMarkerNames(g *reduce.Graph) map[string]struct{} {
	nodes, _ := g.Snapshot()
	out := make(map[string]struct{}, len(nodes))
	for _, n := range nodes {
		if n.Type == model.NodeMarker {
			out[n.Name] = struct{}{}
		}
	}
	return out
}
