package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/realkarych/catacomb/model"
)

const (
	bundleVersion      = 1
	bundleManifestName = "bundle.json"
	bundleRunsPrefix   = "runs"
)

var (
	errBundleVersion   = errors.New("baseline bundle: format version newer than this catacomb supports")
	errBundleEntry     = errors.New("baseline bundle: entry escapes bundle root or is not a regular file")
	errBundleRunID     = errors.New("baseline bundle: run id is not a clean local name")
	errBundleHash      = errors.New("baseline bundle: file hash mismatch")
	errBundleCollision = errors.New("baseline bundle: run dir exists with different content")
)

func validBundleRunID(id string) bool {
	return filepath.IsLocal(id) && !strings.ContainsAny(id, `/\`) && id == path.Clean(id) && id != "."
}

type bundleManifest struct {
	Version  int               `json:"version"`
	Baseline model.Baseline    `json:"baseline"`
	Files    map[string]string `json:"files"`
}

type bundleFile struct {
	path string
	data []byte
}

func writeBundle(w io.Writer, b model.Baseline, runsDir string) (int, error) {
	files, err := collectBundleFiles(runsDir, b.RunIDs)
	if err != nil {
		return 0, err
	}
	gz := gzip.NewWriter(w)
	gz.ModTime = time.Time{}
	gz.OS = 255
	if terr := writeBundleTar(gz, b, files); terr != nil {
		return 0, terr
	}
	if cerr := gz.Close(); cerr != nil {
		return 0, fmt.Errorf("baseline bundle: close gzip: %w", cerr)
	}
	return len(files), nil
}

func collectBundleFiles(runsDir string, runIDs []string) ([]bundleFile, error) {
	ids := slices.Clone(runIDs)
	slices.Sort(ids)
	ids = slices.Compact(ids)
	files := []bundleFile{}
	for _, id := range ids {
		if !validBundleRunID(id) {
			return nil, fmt.Errorf("baseline bundle: run id %q escapes the runs dir", id)
		}
		runDir := filepath.Join(runsDir, id)
		walkErr := filepath.WalkDir(runDir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			f, ok, entryErr := bundleWalkEntry(runDir, id, p, d)
			if entryErr != nil {
				return entryErr
			}
			if ok {
				files = append(files, f)
			}
			return nil
		})
		if walkErr != nil {
			return nil, fmt.Errorf("baseline bundle: walk run %q: %w", id, walkErr)
		}
	}
	return files, nil
}

func bundleWalkEntry(runDir, id, p string, d fs.DirEntry) (bundleFile, bool, error) {
	if d.Type()&fs.ModeSymlink != 0 {
		return bundleFile{}, false, fmt.Errorf("refusing to follow symlink %q", p)
	}
	if d.IsDir() {
		return bundleFile{}, false, nil
	}
	rel, relErr := filepath.Rel(runDir, p)
	if relErr != nil || !filepath.IsLocal(rel) {
		return bundleFile{}, false, fmt.Errorf("entry %q escapes run dir %q", p, runDir)
	}
	data, readErr := os.ReadFile(p)
	if readErr != nil {
		return bundleFile{}, false, readErr
	}
	return bundleFile{path: path.Join(bundleRunsPrefix, id, filepath.ToSlash(rel)), data: data}, true, nil
}

func writeBundleTar(w io.Writer, b model.Baseline, files []bundleFile) error {
	manifest := bundleManifest{Version: bundleVersion, Baseline: b, Files: make(map[string]string, len(files))}
	for _, f := range files {
		sum := sha256.Sum256(f.data)
		manifest.Files[f.path] = hex.EncodeToString(sum[:])
	}
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("baseline bundle: encode manifest: %w", err)
	}
	entries := append([]bundleFile{{path: bundleManifestName, data: append(manifestData, '\n')}}, files...)
	tw := tar.NewWriter(w)
	modTime := b.CreatedAt.UTC().Truncate(time.Second)
	for _, e := range entries {
		if werr := writeBundleEntry(tw, e, modTime); werr != nil {
			return werr
		}
	}
	if cerr := tw.Close(); cerr != nil {
		return fmt.Errorf("baseline bundle: close tar: %w", cerr)
	}
	return nil
}

func writeBundleEntry(tw *tar.Writer, f bundleFile, modTime time.Time) error {
	hdr := &tar.Header{
		Name:    f.path,
		Mode:    0o644,
		Size:    int64(len(f.data)),
		ModTime: modTime,
		Format:  tar.FormatUSTAR | tar.FormatPAX,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("baseline bundle: write header %q: %w", f.path, err)
	}
	if _, err := tw.Write(f.data); err != nil {
		return fmt.Errorf("baseline bundle: write %q: %w", f.path, err)
	}
	return nil
}

func readBundle(r io.Reader, onFile func(path string, r io.Reader) error) (bundleManifest, error) {
	return readBundleWith(r, func(bundleManifest) error { return nil }, onFile)
}

func readBundleWith(r io.Reader, onManifest func(m bundleManifest) error, onFile func(path string, r io.Reader) error) (bundleManifest, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return bundleManifest{}, fmt.Errorf("baseline bundle: open gzip: %w", err)
	}
	tr := tar.NewReader(gz)
	manifest, err := readBundleManifest(tr)
	if err != nil {
		return bundleManifest{}, err
	}
	if merr := onManifest(manifest); merr != nil {
		return bundleManifest{}, merr
	}
	if serr := streamBundleFiles(tr, onFile); serr != nil {
		return bundleManifest{}, serr
	}
	return manifest, nil
}

func readBundleManifest(tr *tar.Reader) (bundleManifest, error) {
	hdr, err := tr.Next()
	if err != nil {
		return bundleManifest{}, fmt.Errorf("baseline bundle: read first entry: %w", err)
	}
	if hdr.Name != bundleManifestName || hdr.Typeflag != tar.TypeReg {
		return bundleManifest{}, fmt.Errorf("baseline bundle: first entry %q is not %s: %w", hdr.Name, bundleManifestName, errBundleEntry)
	}
	data, err := io.ReadAll(tr)
	if err != nil {
		return bundleManifest{}, fmt.Errorf("baseline bundle: read manifest: %w", err)
	}
	var manifest bundleManifest
	if uerr := json.Unmarshal(data, &manifest); uerr != nil {
		return bundleManifest{}, fmt.Errorf("baseline bundle: decode manifest: %w", uerr)
	}
	if manifest.Version > bundleVersion {
		return bundleManifest{}, fmt.Errorf("baseline bundle: manifest version %d: %w", manifest.Version, errBundleVersion)
	}
	return manifest, nil
}

func streamBundleFiles(tr *tar.Reader, onFile func(path string, r io.Reader) error) error {
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("baseline bundle: read entry: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg || !bundleRunPath(hdr.Name) {
			return fmt.Errorf("baseline bundle: entry %q: %w", hdr.Name, errBundleEntry)
		}
		if ferr := onFile(hdr.Name, tr); ferr != nil {
			return fmt.Errorf("baseline bundle: entry %q: %w", hdr.Name, ferr)
		}
	}
}

func bundleRunPath(name string) bool {
	if name != path.Clean(name) || !filepath.IsLocal(name) {
		return false
	}
	parts := strings.Split(name, "/")
	return len(parts) >= 3 && parts[0] == bundleRunsPrefix
}
