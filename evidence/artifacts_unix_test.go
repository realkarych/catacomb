//go:build !windows

package evidence

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCaptureArtifactsIntermediateSymlinkSkipped(t *testing.T) {
	for _, pattern := range []string{filepath.Join("sub", "*.txt"), filepath.Join("*", "secret.txt")} {
		t.Run(pattern, func(t *testing.T) {
			base := t.TempDir()
			work := filepath.Join(base, "work")
			require.NoError(t, os.MkdirAll(work, 0o700))
			outside := filepath.Join(base, "outside")
			require.NoError(t, os.MkdirAll(outside, 0o700))
			writeFile(t, filepath.Join(outside, "secret.txt"), []byte("outside-secret\n"))
			require.NoError(t, os.Symlink(outside, filepath.Join(work, "sub")))
			dir := filepath.Join(t.TempDir(), "run")

			metas, note, err := CaptureArtifacts(dir, work, []string{pattern})
			require.NoError(t, err)
			assert.Empty(t, metas, "outside file behind an intermediate symlink must not be captured")
			assert.Contains(t, note, "escapes workdir")

			entries, _ := os.ReadDir(filepath.Join(dir, ArtifactsDirName))
			assert.Empty(t, entries, "no outside file may be written under artifacts")
		})
	}
}

func TestCaptureArtifactsFinalSymlinkToOutsideSkipped(t *testing.T) {
	base := t.TempDir()
	work := filepath.Join(base, "work")
	require.NoError(t, os.MkdirAll(work, 0o700))
	writeFile(t, filepath.Join(base, "secret.txt"), []byte("outside-secret\n"))
	require.NoError(t, os.Symlink(filepath.Join(base, "secret.txt"), filepath.Join(work, "link.txt")))
	dir := filepath.Join(t.TempDir(), "run")

	metas, note, err := CaptureArtifacts(dir, work, []string{"link.txt"})
	require.NoError(t, err)
	assert.Empty(t, metas, "a terminal symlink to an outside file must not be captured")
	assert.Contains(t, note, "escapes workdir")

	entries, _ := os.ReadDir(filepath.Join(dir, ArtifactsDirName))
	assert.Empty(t, entries)
}

func TestCaptureArtifactsFinalSymlinkToInsideSkipped(t *testing.T) {
	work := t.TempDir()
	writeFile(t, filepath.Join(work, "real.txt"), []byte("inside\n"))
	require.NoError(t, os.Symlink(filepath.Join(work, "real.txt"), filepath.Join(work, "inlink.txt")))
	dir := filepath.Join(t.TempDir(), "run")

	metas, note, err := CaptureArtifacts(dir, work, []string{"inlink.txt"})
	require.NoError(t, err)
	assert.Empty(t, metas, "a symlink is never a regular file, even when it points inside the workdir")
	assert.Contains(t, note, "not a regular file")
}

func TestCaptureArtifactsDanglingSymlinkSkipped(t *testing.T) {
	work := t.TempDir()
	require.NoError(t, os.Symlink(filepath.Join(work, "nonexistent"), filepath.Join(work, "dangling")))
	dir := filepath.Join(t.TempDir(), "run")

	metas, note, err := CaptureArtifacts(dir, work, []string{"dangling"})
	require.NoError(t, err)
	assert.Empty(t, metas, "an unresolvable symlink must not be captured")
	assert.Contains(t, note, "not a regular file")
}

func TestCaptureArtifactsWorkdirUnderSymlinkedParent(t *testing.T) {
	base := t.TempDir()
	realParent := filepath.Join(base, "real")
	require.NoError(t, os.MkdirAll(realParent, 0o700))
	linkParent := filepath.Join(base, "link")
	require.NoError(t, os.Symlink(realParent, linkParent))

	work := filepath.Join(linkParent, "work")
	require.NoError(t, os.MkdirAll(work, 0o700))
	writeFile(t, filepath.Join(work, "own.txt"), []byte("mine\n"))
	dir := filepath.Join(t.TempDir(), "run")

	metas, note, err := CaptureArtifacts(dir, work, []string{"own.txt"})
	require.NoError(t, err)
	require.Empty(t, note)
	require.Len(t, metas, 1, "a workdir living under a symlinked parent must still capture its own files")
	assert.Equal(t, "own.txt", metas[0].Rel)
	_, serr := os.Stat(filepath.Join(dir, ArtifactsDirName, "own.txt"))
	require.NoError(t, serr)
}
