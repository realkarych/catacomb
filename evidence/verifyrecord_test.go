package evidence_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/evidence"
)

func sampleVerify() evidence.VerifyRecord {
	return evidence.VerifyRecord{
		Cmd:        []string{"go", "test", "./..."},
		SHA256:     "abc123",
		ExitCode:   0,
		DurationMS: 4200,
		Mode:       "required",
		FinishedAt: time.Unix(1700, 0).UTC(),
	}
}

func TestWriteReadVerifyRoundtrip(t *testing.T) {
	dir := t.TempDir()
	r := sampleVerify()
	r.Error = "boom"
	require.NoError(t, evidence.WriteVerify(dir, r))

	got, ok, err := evidence.ReadVerify(dir)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, r, got)
}

func TestReadVerifyAbsent(t *testing.T) {
	got, ok, err := evidence.ReadVerify(t.TempDir())
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Equal(t, evidence.VerifyRecord{}, got)
}

func TestWriteVerifyRewriteReplaces(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, evidence.WriteVerify(dir, sampleVerify()))

	second := sampleVerify()
	second.ExitCode = 1
	second.Mode = "advisory"
	second.Error = "failed"
	require.NoError(t, evidence.WriteVerify(dir, second))

	got, ok, err := evidence.ReadVerify(dir)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, second, got)
}

func TestVerifyConfigSHA256(t *testing.T) {
	cmd := []string{"go", "test", "./..."}
	envA := map[string]string{"A": "1", "B": "2", "C": "3"}
	envReordered := map[string]string{"C": "3", "A": "1", "B": "2"}
	assert.Equal(t, evidence.VerifyConfigSHA256(cmd, envA), evidence.VerifyConfigSHA256(cmd, envReordered))

	envDiff := map[string]string{"A": "1", "B": "2", "C": "9"}
	assert.NotEqual(t, evidence.VerifyConfigSHA256(cmd, envA), evidence.VerifyConfigSHA256(cmd, envDiff))

	assert.NotEqual(t, evidence.VerifyConfigSHA256(cmd, envA), evidence.VerifyConfigSHA256([]string{"make", "verify"}, envA))
}

func TestWriteVerifyMarshalError(t *testing.T) {
	r := sampleVerify()
	r.FinishedAt = time.Date(10001, 1, 1, 0, 0, 0, 0, time.UTC)
	require.Error(t, evidence.WriteVerify(t.TempDir(), r))
}

func TestWriteVerifyWriteError(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing")
	require.Error(t, evidence.WriteVerify(dir, sampleVerify()))
}

func TestReadVerifyReadError(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "verify.json"), 0o700))
	_, ok, err := evidence.ReadVerify(dir)
	require.Error(t, err)
	assert.False(t, ok)
}

func TestReadVerifyUnmarshalError(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "verify.json"), []byte("{not json"), 0o600))
	_, ok, err := evidence.ReadVerify(dir)
	require.Error(t, err)
	assert.False(t, ok)
}
