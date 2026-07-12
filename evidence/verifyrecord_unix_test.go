//go:build !windows

package evidence_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/evidence"
)

func TestWriteVerifyFileMode(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, evidence.WriteVerify(dir, sampleVerify()))

	info, err := os.Stat(filepath.Join(dir, "verify.json"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}
