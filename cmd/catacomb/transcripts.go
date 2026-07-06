package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"time"
)

var sleepFn = realSleep

type transcriptSet struct {
	Main      string
	Subagents []string
}

func resolveTranscripts(root, sessionID string) (transcriptSet, error) {
	mains, err := filepath.Glob(filepath.Join(root, "*", sessionID+".jsonl"))
	if err != nil {
		return transcriptSet{}, fmt.Errorf("resolve transcripts: %w", err)
	}
	if len(mains) == 0 {
		return transcriptSet{}, fmt.Errorf("resolve transcripts: no transcript for session %s under %s", sessionID, root)
	}
	if len(mains) > 1 {
		return transcriptSet{}, fmt.Errorf("resolve transcripts: ambiguous session %s: %d matches", sessionID, len(mains))
	}
	subs, err := filepath.Glob(filepath.Join(root, "*", sessionID, "subagents", "agent-*.jsonl"))
	if err != nil {
		return transcriptSet{}, fmt.Errorf("resolve transcripts: %w", err)
	}
	sort.Strings(subs)
	return transcriptSet{Main: mains[0], Subagents: subs}, nil
}

func resolveTranscriptsRetry(root, sessionID string, attempts int, delay time.Duration) (transcriptSet, error) {
	var last error
	for i := 0; i < attempts; i++ {
		ts, err := resolveTranscripts(root, sessionID)
		if err == nil {
			return ts, nil
		}
		last = err
		sleepFn(delay)
	}
	return transcriptSet{}, last
}
