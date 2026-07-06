package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/realkarych/catacomb/aggregate"
	"github.com/realkarych/catacomb/evidence"
	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/reduce"
)

func resolveSelectorRunsDir(runsDir string, pricer reduce.Pricer, sel string) ([]aggregate.RunGraph, error) {
	kind, val, err := parseSelector(sel)
	if err != nil {
		return nil, operational(err)
	}
	if kind == selectorName {
		return nil, operational(errors.New("name: selectors require the store; use label: with --runs-dir (baselines migrate in PV-2)"))
	}
	if verr := validateLabelTerms(strings.Split(val, ",")); verr != nil {
		return nil, operational(verr)
	}
	runs, err := evidence.ScanRuns(runsDir)
	if err != nil {
		return nil, operational(fmt.Errorf("regress --runs-dir: %w", err))
	}
	want := model.ParseLabels(val)
	var group []aggregate.RunGraph
	for _, r := range runs {
		if !evidence.MatchLabels(r.Meta.Labels, want) {
			continue
		}
		rg, rerr := evidenceRunGraph(r.Dir, r.Meta, pricer)
		if rerr != nil {
			return nil, operational(rerr)
		}
		group = append(group, rg)
	}
	if len(group) == 0 {
		return nil, operational(fmt.Errorf("regress selector %q: %w", sel, ErrEmptyGroup))
	}
	return group, nil
}

func evidenceRunGraph(dir string, m evidence.Meta, pricer reduce.Pricer) (aggregate.RunGraph, error) {
	main := filepath.Join(dir, "session.jsonl")
	subs, _ := filepath.Glob(filepath.Join(dir, "subagents", "agent-*.jsonl"))
	sort.Strings(subs)
	var extra []model.Observation
	if m.MarkerName != "" {
		extra = boundaryObservations(m.SessionID, m.MarkerName, m.MarkerStart, m.MarkerEnd)
	}
	g, err := loadGraphOffline(main, subs, newExecutionID(), pricer, extra)
	if err != nil {
		return aggregate.RunGraph{}, err
	}
	nodes, edges := g.Snapshot()
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	sort.Slice(edges, func(i, j int) bool { return edges[i].ID < edges[j].ID })
	run := model.Run{ID: m.RunID, SessionIDs: []string{m.SessionID}, Labels: m.Labels}
	if snap := g.RunsSnapshot(); len(snap) > 0 {
		run.Status = snap[0].Status
		run.ModelID = snap[0].ModelID
	}
	if !m.MarkerStart.IsZero() {
		run.StartedAt = &m.MarkerStart
	}
	if !m.MarkerEnd.IsZero() {
		run.EndedAt = &m.MarkerEnd
	}
	return aggregate.RunGraph{
		Run:   run,
		Nodes: nodes,
		Edges: edges,
	}, nil
}
