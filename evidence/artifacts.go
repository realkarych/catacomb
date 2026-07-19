package evidence

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/realkarych/catacomb/redact"
)

type ArtifactMeta struct {
	Rel    string `json:"rel"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

const (
	ArtifactsDirName   = "artifacts"
	ArtifactPerFileCap = int64(10 << 20)
	ArtifactTotalCap   = int64(50 << 20)
	artifactSniffLen   = 8 << 10
)

func CaptureArtifacts(dir, workdir string, globs []string) ([]ArtifactMeta, string, error) {
	return captureArtifacts(dir, workdir, globs, ArtifactPerFileCap, ArtifactTotalCap)
}

func StampArtifacts(dir string, arts []ArtifactMeta, note string) error {
	m, err := ReadMeta(dir)
	if err != nil {
		return fmt.Errorf("evidence.StampArtifacts: %w", err)
	}
	m.Artifacts = arts
	m.ArtifactsNote = note
	data, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, metaFileName), data, 0o600); err != nil {
		return fmt.Errorf("evidence.StampArtifacts: %w", err)
	}
	return nil
}

func captureArtifacts(dir, workdir string, globs []string, perFileCap, totalCap int64) ([]ArtifactMeta, string, error) {
	var (
		metas []ArtifactMeta
		notes []string
		total int64
		root  *os.Root
	)
	seen := make(map[string]struct{})
	defer func() {
		if root != nil {
			_ = root.Close()
		}
	}()
	realWork := workdir
	if resolved, werr := filepath.EvalSymlinks(workdir); werr == nil {
		realWork = resolved
	}
capture:
	for _, g := range globs {
		matches, gerr := filepath.Glob(filepath.Join(workdir, g))
		if gerr != nil {
			notes = append(notes, fmt.Sprintf("skipped glob %q: %v", g, gerr))
			continue
		}
		for _, src := range matches {
			rel, rerr := filepath.Rel(workdir, src)
			if rerr != nil || !filepath.IsLocal(rel) {
				notes = append(notes, fmt.Sprintf("skipped %q: escapes workdir", g))
				continue
			}
			realSrc, eerr := filepath.EvalSymlinks(src)
			if eerr != nil {
				notes = append(notes, fmt.Sprintf("skipped %q: not a regular file", rel))
				continue
			}
			if rrel, rrerr := filepath.Rel(realWork, realSrc); rrerr != nil || !filepath.IsLocal(rrel) {
				notes = append(notes, fmt.Sprintf("skipped %q: escapes workdir", g))
				continue
			}
			if _, dup := seen[realSrc]; dup {
				continue
			}
			seen[realSrc] = struct{}{}
			info, serr := os.Lstat(src)
			if serr != nil || !info.Mode().IsRegular() {
				notes = append(notes, fmt.Sprintf("skipped %q: not a regular file", rel))
				continue
			}
			if info.Size() > perFileCap {
				notes = append(notes, fmt.Sprintf("skipped %q: exceeds per-file cap", rel))
				continue
			}
			if total+info.Size() > totalCap {
				notes = append(notes, fmt.Sprintf("stopped at %q: total cap reached", rel))
				break capture
			}
			if root == nil {
				r, oerr := openArtifactsRoot(dir)
				if oerr != nil {
					return nil, "", fmt.Errorf("evidence.CaptureArtifacts: %w", oerr)
				}
				root = r
			}
			n, sum, cerr := copyArtifact(root, rel, src)
			if cerr != nil {
				return nil, "", fmt.Errorf("evidence.CaptureArtifacts: %w", cerr)
			}
			total += info.Size()
			metas = append(metas, ArtifactMeta{Rel: rel, SHA256: sum, Bytes: n})
		}
	}
	return metas, strings.Join(notes, "; "), nil
}

func openArtifactsRoot(dir string) (*os.Root, error) {
	base := filepath.Join(dir, ArtifactsDirName)
	if err := os.MkdirAll(base, 0o700); err != nil {
		return nil, err
	}
	return os.OpenRoot(base)
}

func copyArtifact(root *os.Root, rel, src string) (int64, string, error) {
	data, err := os.ReadFile(src)
	if err != nil {
		return 0, "", err
	}
	out := data
	if isTextArtifact(data) {
		out = redactArtifactBytes(data)
	}
	if parent := filepath.Dir(rel); parent != "." {
		if err := root.MkdirAll(parent, 0o700); err != nil {
			return 0, "", err
		}
	}
	if err := root.WriteFile(rel, out, 0o600); err != nil {
		return 0, "", err
	}
	sum := sha256.Sum256(out)
	return int64(len(out)), hex.EncodeToString(sum[:]), nil
}

func redactArtifactBytes(data []byte) []byte {
	var buf bytes.Buffer
	for rest := data; len(rest) > 0; {
		line := rest
		if i := bytes.IndexByte(rest, '\n'); i >= 0 {
			line, rest = rest[:i+1], rest[i+1:]
		} else {
			rest = nil
		}
		body := bytes.TrimSuffix(line, []byte{'\n'})
		buf.Write(redact.RedactPreservingBytes(body).Data)
		buf.Write(line[len(body):])
	}
	return buf.Bytes()
}

func isTextArtifact(data []byte) bool {
	chunk := data
	if len(chunk) > artifactSniffLen {
		chunk = trimPartialRune(chunk[:artifactSniffLen])
	}
	return utf8.Valid(chunk) && !bytes.ContainsRune(chunk, 0)
}

func trimPartialRune(chunk []byte) []byte {
	for i := len(chunk); i > 0 && len(chunk)-i < utf8.UTFMax; i-- {
		if !utf8.RuneStart(chunk[i-1]) {
			continue
		}
		if r, size := utf8.DecodeRune(chunk[i-1:]); r == utf8.RuneError && size == 1 {
			return chunk[:i-1]
		}
		return chunk
	}
	return chunk
}
