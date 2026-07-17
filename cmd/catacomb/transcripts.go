package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"time"
)

var sleepFn = realSleep

func realSleep(d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	<-timer.C
}

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
	return resolveWithRetry(attempts, delay, func() (transcriptSet, error) {
		return resolveTranscripts(root, sessionID)
	})
}

func resolveWithRetry(attempts int, delay time.Duration, resolve func() (transcriptSet, error)) (transcriptSet, error) {
	if attempts < 1 {
		attempts = 1
	}
	var last error
	for i := 0; i < attempts; i++ {
		ts, err := resolve()
		if err == nil {
			return ts, nil
		}
		last = err
		if i < attempts-1 {
			sleepFn(delay)
		}
	}
	return transcriptSet{}, last
}
