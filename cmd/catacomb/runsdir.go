package main

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/realkarych/catacomb/aggregate"
	"github.com/realkarych/catacomb/evidence"
	"github.com/realkarych/catacomb/ingest/drift"
	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/reduce"
	"github.com/realkarych/catacomb/store"
)

type runGraphLoadMode int

const (
	loadFullGraphs runGraphLoadMode = iota
	loadForAggregation
)

func (mode runGraphLoadMode) prune(rg *aggregate.RunGraph) {
	if mode != loadForAggregation {
		return
	}
	for _, n := range rg.Nodes {
		n.Payload = nil
		n.Attrs = nil
		n.Sources = nil
	}
}

func validateLabelTerms(terms []string) error {
	for _, term := range terms {
		for _, seg := range strings.Split(term, ",") {
			if len(model.ParseLabels(seg)) != 1 {
				return fmt.Errorf("invalid --label %q: expected k=v (key [a-z0-9_.-]{1,64}, value ≤256 bytes)", term)
			}
		}
	}
	return nil
}

func resolveSelectorRunsDir(errOut io.Writer, verb, dbPath, runsDir string, pricer reduce.Pricer, sel string, mode runGraphLoadMode) ([]aggregate.RunGraph, model.Baseline, error) {
	kind, val, err := parseSelector(sel)
	if err != nil {
		return nil, model.Baseline{}, operational(err)
	}
	if kind == selectorName {
		return resolveNameSelectorRunsDir(errOut, verb, dbPath, runsDir, pricer, val, mode)
	}
	if verr := validateLabelTerms(strings.Split(val, ",")); verr != nil {
		return nil, model.Baseline{}, operational(verr)
	}
	runs, err := evidence.ScanRuns(runsDir)
	if err != nil {
		return nil, model.Baseline{}, operational(fmt.Errorf("%s --runs-dir: %w", verb, err))
	}
	want := model.ParseLabels(val)
	var group []aggregate.RunGraph
	for _, r := range runs {
		if !evidence.MatchLabels(r.Meta.Labels, want) {
			continue
		}
		rg, rerr := evidenceRunGraph(r.Dir, r.Meta, pricer)
		if rerr != nil {
			return nil, model.Baseline{}, operational(rerr)
		}
		mode.prune(&rg)
		group = append(group, rg)
	}
	if len(group) == 0 {
		return nil, model.Baseline{}, operational(fmt.Errorf("%s selector %q: %w", verb, sel, ErrEmptyGroup))
	}
	return group, model.Baseline{}, nil
}

func resolveNameSelectorRunsDir(errOut io.Writer, verb, dbPath, runsDir string, pricer reduce.Pricer, name string, mode runGraphLoadMode) ([]aggregate.RunGraph, model.Baseline, error) {
	s, err := openReadStore(store.OpenSQLiteReadOnly, dbPath)
	if err != nil {
		return nil, model.Baseline{}, operational(err)
	}
	defer func() { _ = s.Close() }()
	b, ok, err := s.GetBaseline(name)
	if err != nil {
		if errors.Is(err, store.ErrSchemaOutdated) {
			return nil, model.Baseline{}, operational(store.ErrSchemaOutdated)
		}
		return nil, model.Baseline{}, operational(fmt.Errorf("%s get baseline %q: %w", verb, name, err))
	}
	if !ok {
		return nil, model.Baseline{}, operational(fmt.Errorf("%w: %q", ErrBaselineNotFound, name))
	}
	if b.RunsDir != "" && b.RunsDir != runsDir {
		fmt.Fprintf(errOut, "warning: baseline %q recorded runs-dir %q; using --runs-dir %q\n", name, b.RunsDir, runsDir)
	}
	group, err := runGroupFromDirs(verb, runsDir, name, b.RunIDs, pricer, mode)
	if err != nil {
		return nil, model.Baseline{}, err
	}
	if len(group) == 0 {
		return nil, model.Baseline{}, operational(fmt.Errorf("%s name:%s: %w", verb, name, ErrEmptyGroup))
	}
	return group, b, nil
}

func runGroupFromDirs(verb, runsDir, name string, ids []string, pricer reduce.Pricer, mode runGraphLoadMode) ([]aggregate.RunGraph, error) {
	group := make([]aggregate.RunGraph, 0, len(ids))
	for _, id := range ids {
		dir := filepath.Join(runsDir, id)
		m, err := evidence.ReadMeta(dir)
		if err != nil {
			return nil, operational(fmt.Errorf("%s name:%s: run %q dir %q: %w", verb, name, id, dir, err))
		}
		rg, err := evidenceRunGraph(dir, m, pricer)
		if err != nil {
			return nil, operational(fmt.Errorf("%s name:%s: run %q dir %q: %w", verb, name, id, dir, err))
		}
		mode.prune(&rg)
		group = append(group, rg)
	}
	return group, nil
}

func metaRuntime(m evidence.Meta) string {
	if m.Env != nil && m.Env.AgentRuntime != "" {
		return m.Env.AgentRuntime
	}
	return drift.RuntimeClaudeCode
}

func evidenceRunGraph(dir string, m evidence.Meta, pricer reduce.Pricer) (aggregate.RunGraph, error) {
	main := filepath.Join(dir, "session.jsonl")
	subs, _ := filepath.Glob(filepath.Join(dir, "subagents", "agent-*.jsonl"))
	sort.Strings(subs)
	var extra []model.Observation
	if m.MarkerName != "" {
		extra = boundaryObservations(m.SessionID, m.MarkerName, m.MarkerStart, m.MarkerEnd)
	}
	g, err := loadGraphOfflineFor(metaRuntime(m), main, subs, m.SessionID, newExecutionID(), pricer, extra)
	if err != nil {
		return aggregate.RunGraph{}, err
	}
	nodes, edges := sortedGraphSnapshot(g)
	run := model.Run{ID: m.RunID, SessionIDs: []string{m.SessionID}, Labels: m.Labels}
	for _, sr := range g.RunsSnapshot() {
		if sr.ID == m.SessionID {
			run.Status = sr.Status
			run.ModelID = sr.ModelID
			break
		}
	}
	if m.ExitCode != 0 {
		run.Status = model.StatusError
	}
	if !m.MarkerStart.IsZero() {
		run.StartedAt = &m.MarkerStart
	}
	if !m.MarkerEnd.IsZero() {
		run.EndedAt = &m.MarkerEnd
	}
	rg := aggregate.RunGraph{
		Run:   run,
		Nodes: nodes,
		Edges: edges,
	}
	scores, err := loadEvidenceScores(dir, m.RunID)
	if err != nil {
		return aggregate.RunGraph{}, operational(err)
	}
	applyEntriesToRunGraph(&rg, scores)
	return rg, nil
}
