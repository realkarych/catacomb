//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveCodexTranscriptsWalkError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("permission checks do not apply to root")
	}
	root := t.TempDir()
	stageCodexRollout(t, root, codexMainThread, mainPayload(codexMainThread))
	locked := filepath.Join(root, "locked")
	require.NoError(t, os.MkdirAll(locked, 0o700))
	require.NoError(t, os.Chmod(locked, 0o000))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })

	_, err := resolveCodexTranscripts(root, codexMainThread)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolve transcripts")
}

func TestCodexTranscriptByPathWalkError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("permission checks do not apply to root")
	}
	root := t.TempDir()
	main := filepath.Join(root, codexRolloutName(codexMainThread))
	require.NoError(t, os.WriteFile(main, codexMetaLine(t, mainPayload(codexMainThread)), 0o600))
	locked := filepath.Join(root, "locked")
	require.NoError(t, os.MkdirAll(locked, 0o700))
	require.NoError(t, os.Chmod(locked, 0o000))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })

	_, _, err := codexTranscriptByPath(main)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolve transcripts")
}

func TestResolveCodexTranscriptsSkipsUnreadableFile(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("permission checks do not apply to root")
	}
	root := t.TempDir()
	main := stageCodexRollout(t, root, codexMainThread, mainPayload(codexMainThread))
	child := stageCodexRollout(t, root, codexChildThread, childPayload(codexChildThread, codexMainThread))
	sealed := stageCodexRollout(t, root, codexOtherThread, childPayload(codexOtherThread, codexMainThread))
	require.NoError(t, os.Chmod(sealed, 0o000))
	t.Cleanup(func() { _ = os.Chmod(sealed, 0o600) })

	ts, err := resolveCodexTranscripts(root, codexMainThread)
	require.NoError(t, err)
	assert.Equal(t, main, ts.Main)
	assert.Equal(t, []string{child}, ts.Subagents)
}
