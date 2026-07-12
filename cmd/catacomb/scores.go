package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/realkarych/catacomb/aggregate"
)

var errRunLevelNeedsRunID = errors.New(`run-level score requires "run_id"`)

type scoreEntry struct {
	StepKey string
	Key     string
	Value   float64
	RunID   string
}

type scoreLine struct {
	StepKey string   `json:"step_key"`
	Key     string   `json:"key"`
	Value   *float64 `json:"value"`
	RunID   string   `json:"run_id"`
}

func loadScores(path string) ([]scoreEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("scores: %w", err)
	}
	var entries []scoreEntry
	for i, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		e, perr := parseScoreLine(line)
		if perr != nil {
			return nil, fmt.Errorf("scores %s line %d: %w", path, i+1, perr)
		}
		if e.StepKey == "" && e.RunID == "" {
			return nil, fmt.Errorf("scores %s line %d: %w", path, i+1, errRunLevelNeedsRunID)
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func parseScoreLine(line string) (scoreEntry, error) {
	var l scoreLine
	if err := json.Unmarshal([]byte(line), &l); err != nil {
		return scoreEntry{}, err
	}
	if !validAnnotationKey(l.Key) {
		return scoreEntry{}, fmt.Errorf("key %q must be owner.key", l.Key)
	}
	if l.Value == nil {
		return scoreEntry{}, errors.New(`missing numeric "value"`)
	}
	return scoreEntry{StepKey: l.StepKey, Key: l.Key, Value: *l.Value, RunID: l.RunID}, nil
}

func loadEvidenceScores(dir, runID string) ([]scoreEntry, error) {
	path := filepath.Join(dir, "scores.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("scores: %w", err)
	}
	var entries []scoreEntry
	for i, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		e, perr := parseScoreLine(line)
		if perr != nil {
			return nil, fmt.Errorf("scores %s line %d: %w", path, i+1, perr)
		}
		if e.RunID == "" {
			e.RunID = runID
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func applyScores(groups [][]aggregate.RunGraph, entries []scoreEntry) (applied, unmatched, overridden int) {
	for _, e := range entries {
		a, o := 0, 0
		for gi := range groups {
			for ri := range groups[gi] {
				na, no := applyEntryToGraph(&groups[gi][ri], e, true)
				a += na
				o += no
			}
		}
		overridden += o
		if a == 0 {
			unmatched++
			continue
		}
		applied += a
	}
	return applied, unmatched, overridden
}

func applyEntriesToRunGraph(rg *aggregate.RunGraph, entries []scoreEntry) (applied, unmatched int) {
	for _, e := range entries {
		if a, _ := applyEntryToGraph(rg, e, false); a == 0 {
			unmatched++
		} else {
			applied += a
		}
	}
	return applied, unmatched
}

func applyEntryToGraph(rg *aggregate.RunGraph, e scoreEntry, countOverride bool) (applied, overridden int) {
	if e.StepKey == "" {
		if rg.Run.ID != e.RunID {
			return 0, 0
		}
		if rg.Annotations == nil {
			rg.Annotations = map[string]float64{}
		}
		if _, ok := rg.Annotations[e.Key]; ok && countOverride {
			overridden++
		}
		rg.Annotations[e.Key] = e.Value
		return 1, overridden
	}
	if e.RunID != "" && rg.Run.ID != e.RunID {
		return 0, 0
	}
	for _, n := range rg.Nodes {
		if n.StepKey != e.StepKey {
			continue
		}
		if n.Annotations == nil {
			n.Annotations = map[string]any{}
		}
		if _, ok := n.Annotations[e.Key]; ok && countOverride {
			overridden++
		}
		n.Annotations[e.Key] = e.Value
		applied++
	}
	return applied, overridden
}

func applyScoresFile(errOut io.Writer, path string, baseGroup, candGroup []aggregate.RunGraph) error {
	if path == "" {
		return nil
	}
	entries, err := loadScores(path)
	if err != nil {
		return operational(err)
	}
	applied, unmatched, overridden := applyScores([][]aggregate.RunGraph{baseGroup, candGroup}, entries)
	if unmatched > 0 {
		fmt.Fprintf(errOut, "warning: scores: %d score entries matched no node (%d values applied)\n", unmatched, applied)
	}
	if overridden > 0 {
		fmt.Fprintf(errOut, "warning: scores: %d entries overrode evidence-provided values\n", overridden)
	}
	return nil
}
