package repro_test

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/repro"
)

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
		".claude/commands/a.md": &fstest.MapFile{Data: []byte("skill-a")},
		".claude/commands/b.md": &fstest.MapFile{Data: []byte("skill-b")},
	}
	h1 := repro.HashTree(fsys, ".claude/commands")
	h2 := repro.HashTree(fsys, ".claude/commands")
	assert.Equal(t, h1, h2)
}

func TestConfigHashDeterministic(t *testing.T) {
	cfg := repro.Config{OTLPEndpoint: "grpc://localhost:4317", OTLPProject: "proj"}
	assert.Equal(t, repro.ConfigHash(cfg), repro.ConfigHash(cfg))
}

func TestConfigHashDistinct(t *testing.T) {
	c1 := repro.Config{OTLPEndpoint: "a"}
	c2 := repro.Config{OTLPEndpoint: "b"}
	assert.NotEqual(t, repro.ConfigHash(c1), repro.ConfigHash(c2))
}

func TestCaptureFullFS(t *testing.T) {
	fsys := fstest.MapFS{
		"CLAUDE.md":               &fstest.MapFile{Data: []byte("prompts")},
		".claude/commands/s.md":   &fstest.MapFile{Data: []byte("skill")},
		".claude/agents/agent.md": &fstest.MapFile{Data: []byte("agent")},
	}
	cfg := repro.Config{OTLPEndpoint: "x"}
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

func TestCaptureSameConfigEqualHashes(t *testing.T) {
	fsys := fstest.MapFS{
		"CLAUDE.md": &fstest.MapFile{Data: []byte("x")},
	}
	cfg := repro.Config{OTLPEndpoint: "ep"}
	h1 := repro.Capture(fsys, cfg)
	h2 := repro.Capture(fsys, cfg)
	assert.Equal(t, h1, h2)
}
