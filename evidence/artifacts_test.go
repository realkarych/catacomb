package evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const bigCap = int64(1 << 30)

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, data, 0o600))
}

func sha(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestCaptureArtifactsTextRedaction(t *testing.T) {
	work := t.TempDir()
	src := []byte("safe line\ntoken=AKIAIOSFODNN7EXAMPLE\n")
	writeFile(t, filepath.Join(work, "log.txt"), src)
	dir := filepath.Join(t.TempDir(), "run")

	metas, note, err := CaptureArtifacts(dir, work, []string{"log.txt"})
	require.NoError(t, err)
	require.Empty(t, note)
	require.Len(t, metas, 1)

	written, rerr := os.ReadFile(filepath.Join(dir, ArtifactsDirName, "log.txt"))
	require.NoError(t, rerr)
	assert.NotEqual(t, src, written, "redacted copy must differ from source")
	assert.Contains(t, string(written), "‹redacted:aws-key›")
	assert.NotContains(t, string(written), "AKIAIOSFODNN7EXAMPLE")

	assert.Equal(t, "log.txt", metas[0].Rel)
	assert.Equal(t, int64(len(written)), metas[0].Bytes)
	assert.Equal(t, sha(written), metas[0].SHA256)
}

func TestCaptureArtifactsBinaryIdentical(t *testing.T) {
	work := t.TempDir()
	src := []byte{'a', 0x00, 0xff, 'z', '\n', 0x01}
	writeFile(t, filepath.Join(work, "blob.bin"), src)
	dir := filepath.Join(t.TempDir(), "run")

	metas, note, err := CaptureArtifacts(dir, work, []string{"blob.bin"})
	require.NoError(t, err)
	require.Empty(t, note)
	require.Len(t, metas, 1)

	written, rerr := os.ReadFile(filepath.Join(dir, ArtifactsDirName, "blob.bin"))
	require.NoError(t, rerr)
	assert.Equal(t, src, written, "binary copy must be byte-identical")
	assert.Equal(t, int64(len(src)), metas[0].Bytes)
	assert.Equal(t, sha(src), metas[0].SHA256)
}

func TestCaptureArtifactsNestedRel(t *testing.T) {
	work := t.TempDir()
	writeFile(t, filepath.Join(work, "out", "sub", "x.csv"), []byte("a,b,c\n"))
	dir := filepath.Join(t.TempDir(), "run")

	metas, note, err := CaptureArtifacts(dir, work, []string{filepath.Join("out", "sub", "*.csv")})
	require.NoError(t, err)
	require.Empty(t, note)
	require.Len(t, metas, 1)
	assert.Equal(t, filepath.Join("out", "sub", "x.csv"), metas[0].Rel)
	_, serr := os.Stat(filepath.Join(dir, ArtifactsDirName, "out", "sub", "x.csv"))
	require.NoError(t, serr)
}

func TestCaptureArtifactsNoMatches(t *testing.T) {
	work := t.TempDir()
	dir := filepath.Join(t.TempDir(), "run")
	metas, note, err := CaptureArtifacts(dir, work, []string{"nothing-*.log"})
	require.NoError(t, err)
	assert.Empty(t, metas)
	assert.Empty(t, note)
}

func TestCaptureArtifactsPerFileCap(t *testing.T) {
	work := t.TempDir()
	writeFile(t, filepath.Join(work, "small.bin"), []byte{0, 1, 2})
	writeFile(t, filepath.Join(work, "big.bin"), []byte{0, 1, 2, 3})
	dir := filepath.Join(t.TempDir(), "run")

	metas, note, err := captureArtifacts(dir, work, []string{"*.bin"}, int64(3), bigCap)
	require.NoError(t, err)
	require.Len(t, metas, 1)
	assert.Equal(t, "small.bin", metas[0].Rel)
	assert.Contains(t, note, "big.bin")
	assert.Contains(t, note, "per-file cap")
}

func TestCaptureArtifactsTotalCap(t *testing.T) {
	work := t.TempDir()
	writeFile(t, filepath.Join(work, "a.bin"), []byte{0, 1, 2, 3})
	writeFile(t, filepath.Join(work, "b.bin"), []byte{0, 1, 2, 3})
	dir := filepath.Join(t.TempDir(), "run")

	metas, note, err := captureArtifacts(dir, work, []string{"*.bin"}, bigCap, int64(7))
	require.NoError(t, err)
	require.Len(t, metas, 1)
	assert.Equal(t, "a.bin", metas[0].Rel)
	assert.Contains(t, note, "b.bin")
	assert.Contains(t, note, "total cap")
}

func TestCaptureArtifactsEscapeSkipped(t *testing.T) {
	base := t.TempDir()
	work := filepath.Join(base, "work")
	require.NoError(t, os.MkdirAll(work, 0o700))
	writeFile(t, filepath.Join(base, "secret.txt"), []byte("outside-secret\n"))
	dir := filepath.Join(t.TempDir(), "run")

	escape := filepath.Join("*", "..", "..", "secret.txt")
	metas, note, err := CaptureArtifacts(dir, work, []string{escape})
	require.NoError(t, err)
	assert.Empty(t, metas)
	assert.Contains(t, note, "escapes workdir")

	entries, _ := os.ReadDir(filepath.Join(dir, ArtifactsDirName))
	assert.Empty(t, entries, "no outside file may be captured")
}

func TestCaptureArtifactsNonRegularSkipped(t *testing.T) {
	work := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(work, "adir"), 0o700))
	writeFile(t, filepath.Join(work, "afile"), []byte{0, 1})
	dir := filepath.Join(t.TempDir(), "run")

	metas, note, err := CaptureArtifacts(dir, work, []string{"a*"})
	require.NoError(t, err)
	require.Len(t, metas, 1)
	assert.Equal(t, "afile", metas[0].Rel)
	assert.Contains(t, note, "adir")
	assert.Contains(t, note, "not a regular file")
}

func TestCaptureArtifactsBadGlob(t *testing.T) {
	work := t.TempDir()
	writeFile(t, filepath.Join(work, "keep.txt"), []byte{0, 9})
	dir := filepath.Join(t.TempDir(), "run")

	metas, note, err := CaptureArtifacts(dir, work, []string{"[", "keep.txt"})
	require.NoError(t, err)
	require.Len(t, metas, 1)
	assert.Equal(t, "keep.txt", metas[0].Rel)
	assert.Contains(t, note, "glob")
}

func TestCaptureArtifactsRootOpenError(t *testing.T) {
	work := t.TempDir()
	writeFile(t, filepath.Join(work, "keep.txt"), []byte{0, 9})
	dir := filepath.Join(t.TempDir(), "run")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ArtifactsDirName), []byte("x"), 0o600))

	_, _, err := CaptureArtifacts(dir, work, []string{"keep.txt"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CaptureArtifacts")
}

func TestCaptureArtifactsWriteError(t *testing.T) {
	work := t.TempDir()
	writeFile(t, filepath.Join(work, "keep.txt"), []byte{0, 9})
	dir := filepath.Join(t.TempDir(), "run")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ArtifactsDirName, "keep.txt"), 0o700))

	_, _, err := CaptureArtifacts(dir, work, []string{"keep.txt"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CaptureArtifacts")
}

func TestCopyArtifactIOErrors(t *testing.T) {
	realFile := filepath.Join(t.TempDir(), "real.txt")
	require.NoError(t, os.WriteFile(realFile, []byte("hi\n"), 0o600))

	root, err := openArtifactsRoot(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { _ = root.Close() })
	require.NoError(t, root.Mkdir("existing-dir", 0o700))
	require.NoError(t, root.WriteFile("file-parent", []byte("x"), 0o600))

	tests := []struct {
		name string
		rel  string
		src  string
	}{
		{"read error on directory source", "ok.txt", t.TempDir()},
		{"write error on directory dest", "existing-dir", realFile},
		{"mkdir error on file parent", filepath.Join("file-parent", "sub", "x"), realFile},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, cerr := copyArtifact(root, tc.rel, tc.src)
			require.Error(t, cerr)
		})
	}
}

func TestCaptureArtifactsOverlappingGlobsDedup(t *testing.T) {
	work := t.TempDir()
	writeFile(t, filepath.Join(work, "results", "final.csv"), []byte{0, 1, 2, 3})
	dir := filepath.Join(t.TempDir(), "run")

	globs := []string{filepath.Join("results", "*.csv"), filepath.Join("results", "final.csv")}
	metas, note, err := captureArtifacts(dir, work, globs, bigCap, int64(4))
	require.NoError(t, err)
	require.Len(t, metas, 1)
	assert.Empty(t, note)
	assert.Equal(t, filepath.Join("results", "final.csv"), metas[0].Rel)
}

func TestCaptureArtifactsMultipleFiles(t *testing.T) {
	work := t.TempDir()
	writeFile(t, filepath.Join(work, "a.txt"), []byte("aaa\n"))
	writeFile(t, filepath.Join(work, "b.txt"), []byte("bbb\n"))
	dir := filepath.Join(t.TempDir(), "run")

	metas, note, err := CaptureArtifacts(dir, work, []string{"*.txt"})
	require.NoError(t, err)
	require.Empty(t, note)
	require.Len(t, metas, 2)
	for _, m := range metas {
		_, serr := os.Stat(filepath.Join(dir, ArtifactsDirName, m.Rel))
		require.NoError(t, serr)
	}
}

func TestIsTextArtifact(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{"ascii text", []byte("hello world\n"), true},
		{"empty", nil, true},
		{"nul byte", []byte{'a', 0x00, 'b'}, false},
		{"invalid utf8", []byte{0xff, 0xfe, 0xfd}, false},
		{"large text truncated to sniff window", append([]byte(strings.Repeat("x", artifactSniffLen+16)), 0x00), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isTextArtifact(tc.data))
		})
	}
}

func TestCaptureArtifactsMetaRoundtrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "run")
	m := sampleArtifactMeta()
	require.NoError(t, Write(dir, m, nil))
	got, err := ReadMeta(dir)
	require.NoError(t, err)
	assert.Equal(t, m.Artifacts, got.Artifacts)
	assert.Equal(t, m.ArtifactsNote, got.ArtifactsNote)
}

func sampleArtifactMeta() Meta {
	return Meta{
		RunID: "run-art", Task: "t", Variant: "base", Rep: 1, SessionID: "s",
		BasketHash: "h", MarkerName: "task:t",
		Artifacts:     []ArtifactMeta{{Rel: "out.csv", SHA256: "abc", Bytes: 12}},
		ArtifactsNote: "skipped \"big.bin\": exceeds per-file cap",
	}
}

func TestStampArtifacts(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "run")
	require.NoError(t, Write(dir, Meta{RunID: "r1", Task: "t1", MarkerName: "task:t1"}, nil))
	arts := []ArtifactMeta{{Rel: "out/result.csv", SHA256: "deadbeef", Bytes: 7}}
	require.NoError(t, StampArtifacts(dir, arts, "note-x"))
	got, err := ReadMeta(dir)
	require.NoError(t, err)
	assert.Equal(t, arts, got.Artifacts)
	assert.Equal(t, "note-x", got.ArtifactsNote)
}

func TestStampArtifactsPreservesEnv(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "run")
	m := Meta{
		RunID: "r1", Task: "t1", MarkerName: "task:t1",
		Env: &EnvStamps{
			CatacombVersion:   "1.0.0",
			ModelID:           "m-x",
			ClaudeCodeVersion: "2.0.0",
			Resources:         Resources{OS: "linux", Arch: "amd64", CPUs: 2},
		},
	}
	require.NoError(t, Write(dir, m, nil))
	before := rawMetaEnv(t, dir)
	require.NoError(t, StampArtifacts(dir, []ArtifactMeta{{Rel: "a.txt", SHA256: "x", Bytes: 1}}, "note"))
	after := rawMetaEnv(t, dir)
	assert.Equal(t, string(before), string(after))
	got, err := ReadMeta(dir)
	require.NoError(t, err)
	assert.Equal(t, m.Env, got.Env)
	assert.Len(t, got.Artifacts, 1)
}

func rawMetaEnv(t *testing.T, dir string) json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	require.NoError(t, err)
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &raw))
	v, ok := raw["env"]
	require.True(t, ok)
	return v
}

func TestStampArtifactsReadMetaError(t *testing.T) {
	err := StampArtifacts(t.TempDir(), nil, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "evidence.StampArtifacts")
}
