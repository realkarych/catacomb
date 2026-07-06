package main

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/oklog/ulid/v2"

	ijsonl "github.com/realkarych/catacomb/ingest/jsonl"
	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/redact"
	"github.com/realkarych/catacomb/reduce"
)

func parseTranscripts(main string, subs []string, executionID string) ([]model.Observation, error) {
	var all []model.Observation
	for _, p := range append([]string{main}, subs...) {
		f, err := os.Open(p)
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", p, err)
		}
		obs, perr := ijsonl.ParseReader(f, executionID)
		if rerr := errors.Join(perr, f.Close()); rerr != nil {
			return nil, fmt.Errorf("read %s: %w", p, rerr)
		}
		all = append(all, obs...)
	}
	for i := range all {
		all[i].Seq = uint64(i + 1)
	}
	return all, nil
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
