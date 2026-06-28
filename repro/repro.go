package repro

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"sort"
)

const Absent = "absent"

type Hashes struct {
	PromptsHash        string `json:"prompts_hash"`
	SkillsHash         string `json:"skills_hash"`
	SubagentsHash      string `json:"subagents_hash"`
	CatacombConfigHash string `json:"catacomb_config_hash"`
}

type Config struct {
	OTLPEndpoint  string `json:"otlp_endpoint,omitempty"`
	OTLPProject   string `json:"otlp_project,omitempty"`
	TranscriptDir string `json:"transcript_dir,omitempty"`
}

func HashFiles(fsys fs.FS, names []string) string {
	sorted := make([]string, len(names))
	copy(sorted, names)
	sort.Strings(sorted)
	h := sha256.New()
	for _, name := range sorted {
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			continue
		}
		h.Write([]byte(name))
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func HashTree(fsys fs.FS, root string) string {
	if _, err := fs.Stat(fsys, root); err != nil {
		return Absent
	}
	var names []string
	_ = fs.WalkDir(fsys, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		names = append(names, path)
		return nil
	})
	return HashFiles(fsys, names)
}

func ConfigHash(cfg Config) string {
	data, _ := json.Marshal(cfg)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func Capture(fsys fs.FS, cfg Config) Hashes {
	return Hashes{
		PromptsHash:        HashFiles(fsys, []string{"CLAUDE.md"}),
		SkillsHash:         HashTree(fsys, ".claude/commands"),
		SubagentsHash:      HashTree(fsys, ".claude/agents"),
		CatacombConfigHash: ConfigHash(cfg),
	}
}
