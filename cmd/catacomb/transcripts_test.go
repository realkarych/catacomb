package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveTranscripts(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "-Users-x-proj")
	require.NoError(t, os.MkdirAll(filepath.Join(proj, "sess-1", "subagents"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(proj, "sess-1.jsonl"), []byte("{}\n"), 0o600))
	for _, a := range []string{"agent-b.jsonl", "agent-a.jsonl"} {
		require.NoError(t, os.WriteFile(filepath.Join(proj, "sess-1", "subagents", a), []byte("{}\n"), 0o600))
	}
	ts, err := resolveTranscripts(root, "sess-1")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(proj, "sess-1.jsonl"), ts.Main)
	require.Equal(t, []string{
		filepath.Join(proj, "sess-1", "subagents", "agent-a.jsonl"),
		filepath.Join(proj, "sess-1", "subagents", "agent-b.jsonl"),
	}, ts.Subagents)
}

func TestResolveTranscriptsNotFoundAndAmbiguous(t *testing.T) {
	root := t.TempDir()
	_, err := resolveTranscripts(root, "sess-x")
	require.ErrorContains(t, err, "no transcript")
	for _, p := range []string{"p1", "p2"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, p), 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(root, p, "dup.jsonl"), []byte("{}\n"), 0o600))
	}
	_, err = resolveTranscripts(root, "dup")
	require.ErrorContains(t, err, "ambiguous")
}

func TestResolveTranscriptsRetry(t *testing.T) {
	root := t.TempDir()
	calls := 0
	old := sleepFn
	sleepFn = func(time.Duration) {
		calls++
		if calls == 2 {
			proj := filepath.Join(root, "p")
			require.NoError(t, os.MkdirAll(proj, 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(proj, "late.jsonl"), []byte("{}\n"), 0o600))
		}
	}
	defer func() { sleepFn = old }()
	ts, err := resolveTranscriptsRetry(root, "late", 5, time.Millisecond)
	require.NoError(t, err)
	require.NotEmpty(t, ts.Main)
	_, err = resolveTranscriptsRetry(t.TempDir(), "never", 2, time.Millisecond)
	require.Error(t, err)
}

func TestRealSleep(t *testing.T) {
	start := time.Now()
	realSleep(2 * time.Millisecond)
	assert.GreaterOrEqual(t, time.Since(start), 2*time.Millisecond)
}
