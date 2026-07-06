//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveTranscriptsBadPattern(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "p")
	require.NoError(t, os.MkdirAll(proj, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(proj, "esc.jsonl"), []byte("{}\n"), 0o600))
	_, err := resolveTranscripts(root, "[a")
	require.ErrorIs(t, err, filepath.ErrBadPattern)
	_, err = resolveTranscripts(root, `esc\`)
	require.ErrorIs(t, err, filepath.ErrBadPattern)
}
