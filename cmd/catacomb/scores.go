package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/realkarych/catacomb/aggregate"
)

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
		entries = append(entries, e)
	}
	return entries, nil
}

func parseScoreLine(line string) (scoreEntry, error) {
	var l scoreLine
	if err := json.Unmarshal([]byte(line), &l); err != nil {
		return scoreEntry{}, err
	}
	if l.StepKey == "" {
		return scoreEntry{}, errors.New(`missing "step_key"`)
	}
	if !validAnnotationKey(l.Key) {
		return scoreEntry{}, fmt.Errorf("key %q must be owner.key", l.Key)
	}
	if l.Value == nil {
		return scoreEntry{}, errors.New(`missing numeric "value"`)
	}
	return scoreEntry{StepKey: l.StepKey, Key: l.Key, Value: *l.Value, RunID: l.RunID}, nil
}

func applyScores(groups [][]aggregate.RunGraph, entries []scoreEntry) (applied, unmatched int) {
	for _, e := range entries {
		n := applyScore(groups, e)
		if n == 0 {
			unmatched++
			continue
		}
		applied += n
	}
	return applied, unmatched
}

func applyScore(groups [][]aggregate.RunGraph, e scoreEntry) int {
	applied := 0
	for _, group := range groups {
		for _, rg := range group {
			if e.RunID != "" && rg.Run.ID != e.RunID {
				continue
			}
			for _, n := range rg.Nodes {
				if n.StepKey != e.StepKey {
					continue
				}
				if n.Annotations == nil {
					n.Annotations = map[string]any{}
				}
				n.Annotations[e.Key] = e.Value
				applied++
			}
		}
	}
	return applied
}

func applyScoresFile(errOut io.Writer, path string, baseGroup, candGroup []aggregate.RunGraph) error {
	if path == "" {
		return nil
	}
	entries, err := loadScores(path)
	if err != nil {
		return operational(err)
	}
	applied, unmatched := applyScores([][]aggregate.RunGraph{baseGroup, candGroup}, entries)
	if unmatched > 0 {
		fmt.Fprintf(errOut, "warning: scores: %d score entries matched no node (%d values applied)\n", unmatched, applied)
	}
	return nil
}
