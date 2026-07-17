package codex

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenPlainFile(t *testing.T) {
	rc, err := Open(basicFixture)
	require.NoError(t, err)
	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.NoError(t, rc.Close())
	want, err := os.ReadFile(basicFixture)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestOpenZstRoundTrip(t *testing.T) {
	want, err := os.ReadFile(basicFixture)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "basic.jsonl.zst")
	f, err := os.Create(path)
	require.NoError(t, err)
	enc, err := zstd.NewWriter(f)
	require.NoError(t, err)
	_, err = enc.Write(want)
	require.NoError(t, err)
	require.NoError(t, enc.Close())
	require.NoError(t, f.Close())
	rc, err := Open(path)
	require.NoError(t, err)
	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.NoError(t, rc.Close())
	assert.Equal(t, want, got)
}

func TestOpenMissingFile(t *testing.T) {
	rc, err := Open(filepath.Join(t.TempDir(), "absent.jsonl"))
	require.Error(t, err)
	assert.Nil(t, rc)
	assert.ErrorIs(t, err, os.ErrNotExist)
	assert.Contains(t, err.Error(), "codex.Open:")
}

func TestOpenZstInvalidContentFailsOnRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "junk.jsonl.zst")
	require.NoError(t, os.WriteFile(path, []byte("not zstd"), 0o600))
	rc, err := Open(path)
	require.NoError(t, err)
	_, err = io.ReadAll(rc)
	require.Error(t, err)
	require.NoError(t, rc.Close())
}
