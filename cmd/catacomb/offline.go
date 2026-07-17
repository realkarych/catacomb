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

	"github.com/realkarych/catacomb/ingest/codex"
	"github.com/realkarych/catacomb/ingest/drift"
	ijsonl "github.com/realkarych/catacomb/ingest/jsonl"
	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/redact"
	"github.com/realkarych/catacomb/reduce"
)

var driftOut io.Writer = os.Stderr

var driftSeen = map[string]struct{}{}

func resetDriftWarnings() { driftSeen = map[string]struct{}{} }

func parseTranscripts(main string, subs []string, executionID string) ([]model.Observation, error) {
	return parseTranscriptsFor(drift.RuntimeClaudeCode, main, subs, "", executionID)
}

func parseTranscriptsFor(rt, main string, subs []string, mainRunID, executionID string) ([]model.Observation, error) {
	var all []model.Observation
	var counts drift.Counts
	for _, p := range append([]string{main}, subs...) {
		obs, dc, err := parseTranscriptFile(rt, p, mainRunID, executionID)
		if err != nil {
			return nil, err
		}
		counts = counts.Merge(dc)
		all = append(all, obs...)
	}
	for i := range all {
		all[i].Seq = uint64(i + 1)
	}
	warnDrift(counts)
	warnVersionFor(rt, maxObservedVersionFor(rt, all))
	return all, nil
}

func parseTranscriptFile(rt, path, mainRunID, executionID string) ([]model.Observation, drift.Counts, error) {
	r, err := openTranscript(rt, path)
	if err != nil {
		return nil, nil, fmt.Errorf("open %s: %w", path, err)
	}
	var seq uint64
	next := func() uint64 { s := seq; seq++; return s }
	keepEventTime := func(ts time.Time) time.Time { return ts }
	var obs []model.Observation
	var dc drift.Counts
	var perr error
	if rt == drift.RuntimeCodex {
		obs, dc, perr = codex.Parse(r, mainRunID, executionID, next, keepEventTime)
	} else {
		obs, dc, perr = ijsonl.Parse(r, executionID, next, keepEventTime)
	}
	if rerr := errors.Join(perr, r.Close()); rerr != nil {
		return nil, nil, fmt.Errorf("read %s: %w", path, rerr)
	}
	return obs, dc, nil
}

func openTranscript(rt, path string) (io.ReadCloser, error) {
	if rt == drift.RuntimeCodex {
		return codex.Open(path)
	}
	return os.Open(path)
}

func runtimeVersionAttr(rt string) string {
	if rt == drift.RuntimeCodex {
		return "codex_version"
	}
	return "claude_code_version"
}

func maxObservedVersionFor(rt string, obs []model.Observation) string {
	attr := runtimeVersionAttr(rt)
	newest := ""
	for _, o := range obs {
		v, ok := o.Attrs[attr].(string)
		if !ok {
			continue
		}
		if newest == "" || drift.CompareVersions(v, newest) > 0 {
			newest = v
		}
	}
	return newest
}

func runtimeNameAndCeiling(rt string) (string, string) {
	if rt == drift.RuntimeCodex {
		return "Codex", drift.TestedCodexVersion
	}
	return "Claude Code", drift.TestedClaudeCodeVersion
}

func warnVersionFor(rt, observed string) {
	if !drift.NewerThanTestedFor(rt, observed) {
		return
	}
	key := rt + " " + observed
	if _, ok := driftSeen[key]; ok {
		return
	}
	driftSeen[key] = struct{}{}
	name, tested := runtimeNameAndCeiling(rt)
	fmt.Fprintf(driftOut, "warning: transcript %s version %s is newer than tested %s\n", name, observed, tested)
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
	return loadGraphOfflineFor(drift.RuntimeClaudeCode, main, subs, "", executionID, pricer, extra)
}

func loadGraphOfflineFor(rt, main string, subs []string, mainRunID, executionID string, pricer reduce.Pricer, extra []model.Observation) (*reduce.Graph, error) {
	obs, err := parseTranscriptsFor(rt, main, subs, mainRunID, executionID)
	if err != nil {
		return nil, err
	}
	return graphFromObservations(obs, executionID, pricer, extra), nil
}

func graphFromObservations(obs []model.Observation, executionID string, pricer reduce.Pricer, extra []model.Observation) *reduce.Graph {
	base := len(obs)
	for i := range extra {
		e := extra[i]
		e.ExecutionID = executionID
		e.Seq = uint64(base + i + 1)
		obs = append(obs, e)
	}
	policy := redact.DefaultPolicy()
	for i := range obs {
		obs[i] = policy.Observation(obs[i])
	}
	var g *reduce.Graph
	if pricer != nil {
		g = reduce.NewGraphWithPricer(pricer)
	} else {
		g = reduce.NewGraph()
	}
	g.ApplyAll(obs)
	return g
}

func transcriptTimeBounds(obs []model.Observation) (time.Time, time.Time, bool) {
	var start, end time.Time
	found := false
	for _, o := range obs {
		if o.EventTime.IsZero() {
			continue
		}
		if !found {
			start, end, found = o.EventTime, o.EventTime, true
			continue
		}
		if o.EventTime.Before(start) {
			start = o.EventTime
		}
		if o.EventTime.After(end) {
			end = o.EventTime
		}
	}
	return start, end, found
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
