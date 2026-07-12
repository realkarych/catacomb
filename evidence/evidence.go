package evidence

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/realkarych/catacomb/redact"
)

const metaFileName = "meta.json"

type Meta struct {
	RunID       string            `json:"run_id"`
	Task        string            `json:"task"`
	Variant     string            `json:"variant"`
	Rep         int               `json:"rep"`
	SessionID   string            `json:"session_id"`
	Labels      map[string]string `json:"labels,omitempty"`
	ExitCode    int               `json:"exit_code"`
	CostUSD     *float64          `json:"cost_usd,omitempty"`
	BasketHash  string            `json:"basket_hash"`
	MarkerName  string            `json:"marker_name"`
	MarkerStart time.Time         `json:"marker_start"`
	MarkerEnd   time.Time         `json:"marker_end"`
	FinishedAt  time.Time         `json:"finished_at"`

	Artifacts     []ArtifactMeta `json:"artifacts,omitempty"`
	ArtifactsNote string         `json:"artifacts_note,omitempty"`
}

type SourceFile struct {
	Src string
	Rel string
}

type Run struct {
	Dir  string
	Meta Meta
}

func Write(dir string, m Meta, files []SourceFile) error {
	for _, f := range files {
		if !filepath.IsLocal(f.Rel) {
			return fmt.Errorf("evidence.Write: rel %q escapes evidence dir", f.Rel)
		}
	}
	if err := replaceDir(dir); err != nil {
		return fmt.Errorf("evidence.Write: %w", err)
	}
	for _, f := range files {
		if err := copyRedacted(f.Src, filepath.Join(dir, f.Rel)); err != nil {
			return fmt.Errorf("evidence.Write: %w", err)
		}
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("evidence.Write: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, metaFileName), data, 0o600); err != nil {
		return fmt.Errorf("evidence.Write: %w", err)
	}
	return nil
}

func replaceDir(dir string) error {
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	return os.MkdirAll(dir, 0o700)
}

func copyRedacted(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	if derr := os.MkdirAll(filepath.Dir(dst), 0o700); derr != nil {
		return derr
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	werr := redactLines(in, out)
	cerr := out.Close()
	if werr != nil {
		return werr
	}
	return cerr
}

func redactLines(in io.Reader, out io.Writer) error {
	r := bufio.NewReader(in)
	for {
		line, err := r.ReadBytes('\n')
		trimmed := bytes.TrimSuffix(line, []byte{'\n'})
		if len(trimmed) > 0 {
			if _, werr := out.Write(append(redact.Redact(trimmed).Data, '\n')); werr != nil {
				return werr
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func ReadMeta(dir string) (Meta, error) {
	data, err := os.ReadFile(filepath.Join(dir, metaFileName))
	if err != nil {
		return Meta{}, fmt.Errorf("evidence.ReadMeta: %w", err)
	}
	var m Meta
	if err := json.Unmarshal(data, &m); err != nil {
		return Meta{}, fmt.Errorf("evidence.ReadMeta: %w", err)
	}
	return m, nil
}

func ScanRuns(root string) ([]Run, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("evidence.ScanRuns: %w", err)
	}
	var out []Run
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		m, merr := ReadMeta(dir)
		if merr != nil {
			continue
		}
		out = append(out, Run{Dir: dir, Meta: m})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Meta.RunID < out[j].Meta.RunID })
	return out, nil
}

func MatchLabels(have, want map[string]string) bool {
	for k, v := range want {
		if have[k] != v {
			return false
		}
	}
	return true
}
