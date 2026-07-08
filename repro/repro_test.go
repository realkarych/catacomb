package repro_test

import (
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/repro"
)

type readErrFS struct{ name string }

func (e readErrFS) Open(name string) (fs.File, error) {
	if name != e.name {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return &readErrFile{}, nil
}

type readErrFile struct{}

func (f *readErrFile) Read([]byte) (int, error)   { return 0, errors.New("read") }
func (f *readErrFile) Close() error               { return nil }
func (f *readErrFile) Stat() (fs.FileInfo, error) { return readErrFileInfo{}, nil }

type readErrFileInfo struct{}

func (readErrFileInfo) Name() string       { return "bad.md" }
func (readErrFileInfo) Size() int64        { return 1 }
func (readErrFileInfo) Mode() fs.FileMode  { return 0o444 }
func (readErrFileInfo) ModTime() time.Time { return time.Time{} }
func (readErrFileInfo) IsDir() bool        { return false }
func (readErrFileInfo) Sys() any           { return nil }

func TestHashFilesEmptyNames(t *testing.T) {
	fsys := fstest.MapFS{}
	h := repro.HashFiles(fsys, nil)
	assert.NotEmpty(t, h)
	assert.Len(t, h, 64)
}

func TestHashFilesMissingFile(t *testing.T) {
	fsys := fstest.MapFS{}
	h1 := repro.HashFiles(fsys, []string{"missing.md"})
	h2 := repro.HashFiles(fsys, nil)
	assert.Equal(t, h1, h2)
}

func TestHashFilesSingleFile(t *testing.T) {
	fsys := fstest.MapFS{
		"CLAUDE.md": &fstest.MapFile{Data: []byte("hello")},
	}
	h := repro.HashFiles(fsys, []string{"CLAUDE.md"})
	assert.Len(t, h, 64)
}

func TestHashFilesDeterministic(t *testing.T) {
	fsys := fstest.MapFS{
		"a.md": &fstest.MapFile{Data: []byte("aa")},
		"b.md": &fstest.MapFile{Data: []byte("bb")},
	}
	h1 := repro.HashFiles(fsys, []string{"a.md", "b.md"})
	h2 := repro.HashFiles(fsys, []string{"b.md", "a.md"})
	assert.Equal(t, h1, h2)
}

func TestHashFilesContentSensitive(t *testing.T) {
	fsys1 := fstest.MapFS{
		"CLAUDE.md": &fstest.MapFile{Data: []byte("v1")},
	}
	fsys2 := fstest.MapFS{
		"CLAUDE.md": &fstest.MapFile{Data: []byte("v2")},
	}
	assert.NotEqual(t, repro.HashFiles(fsys1, []string{"CLAUDE.md"}), repro.HashFiles(fsys2, []string{"CLAUDE.md"}))
}

func TestHashTreeMissingRoot(t *testing.T) {
	fsys := fstest.MapFS{}
	assert.Equal(t, repro.Absent, repro.HashTree(fsys, "nodir"))
}

func TestHashTreeEmptyDir(t *testing.T) {
	fsys := fstest.MapFS{
		"skills/.keep": &fstest.MapFile{Data: []byte("")},
	}
	h := repro.HashTree(fsys, "skills")
	assert.NotEmpty(t, h)
	assert.NotEqual(t, repro.Absent, h)
}

func TestHashTreeDeterministic(t *testing.T) {
	fsys := fstest.MapFS{
		".claude/skills/a.md": &fstest.MapFile{Data: []byte("skill-a")},
		".claude/skills/b.md": &fstest.MapFile{Data: []byte("skill-b")},
	}
	h1 := repro.HashTree(fsys, ".claude/skills")
	h2 := repro.HashTree(fsys, ".claude/skills")
	assert.Equal(t, h1, h2)
}

func TestConfigHashDeterministic(t *testing.T) {
	cfg := repro.Config{TranscriptDir: "runs"}
	assert.Equal(t, repro.ConfigHash(cfg), repro.ConfigHash(cfg))
}

func TestConfigHashDistinct(t *testing.T) {
	c1 := repro.Config{TranscriptDir: "a"}
	c2 := repro.Config{TranscriptDir: "b"}
	assert.NotEqual(t, repro.ConfigHash(c1), repro.ConfigHash(c2))
}

func TestCaptureFullFS(t *testing.T) {
	fsys := fstest.MapFS{
		"CLAUDE.md":               &fstest.MapFile{Data: []byte("prompts")},
		".claude/skills/s.md":     &fstest.MapFile{Data: []byte("skill")},
		".claude/agents/agent.md": &fstest.MapFile{Data: []byte("agent")},
	}
	cfg := repro.Config{TranscriptDir: "x"}
	h := repro.Capture(fsys, cfg)
	assert.NotEmpty(t, h.PromptsHash)
	assert.NotEmpty(t, h.SkillsHash)
	assert.NotEmpty(t, h.SubagentsHash)
	assert.NotEmpty(t, h.CatacombConfigHash)
	assert.NotEqual(t, repro.Absent, h.SkillsHash)
	assert.NotEqual(t, repro.Absent, h.SubagentsHash)
}

func TestCaptureMissingDirs(t *testing.T) {
	fsys := fstest.MapFS{
		"CLAUDE.md": &fstest.MapFile{Data: []byte("x")},
	}
	h := repro.Capture(fsys, repro.Config{})
	assert.Equal(t, repro.Absent, h.SkillsHash)
	assert.Equal(t, repro.Absent, h.SubagentsHash)
	require.NotEmpty(t, h.PromptsHash)
}

func TestHashFilesReadErrorSkipsFile(t *testing.T) {
	fsys := readErrFS{name: "bad.md"}
	h1 := repro.HashFiles(fsys, []string{"bad.md"})
	h2 := repro.HashFiles(fsys, nil)
	assert.Equal(t, h1, h2, "file with read error should be skipped like a missing file")
}

func TestHashFilesNoCollisionWithLengthPrefix(t *testing.T) {
	fsys1 := fstest.MapFS{
		"a": &fstest.MapFile{Data: []byte("b.mdX")},
	}
	fsys2 := fstest.MapFS{
		"ab.md": &fstest.MapFile{Data: []byte("X")},
	}
	h1 := repro.HashFiles(fsys1, []string{"a"})
	h2 := repro.HashFiles(fsys2, []string{"ab.md"})
	assert.NotEqual(t, h1, h2, "distinct (name,data) pairs that share the same concatenation must not collide")
}

func TestHashFilesLargeFileCapped(t *testing.T) {
	big := make([]byte, 1<<20+1)
	for i := range big {
		big[i] = byte(i % 251)
	}
	fsys := fstest.MapFS{
		"big.bin": &fstest.MapFile{Data: big},
	}
	h1 := repro.HashFiles(fsys, []string{"big.bin"})
	h2 := repro.HashFiles(fsys, []string{"big.bin"})
	assert.Equal(t, h1, h2, "hash of over-cap file must be deterministic")
	assert.Len(t, h1, 64)
}

func TestCaptureSameConfigEqualHashes(t *testing.T) {
	fsys := fstest.MapFS{
		"CLAUDE.md": &fstest.MapFile{Data: []byte("x")},
	}
	cfg := repro.Config{TranscriptDir: "ep"}
	h1 := repro.Capture(fsys, cfg)
	h2 := repro.Capture(fsys, cfg)
	assert.Equal(t, h1, h2)
}
