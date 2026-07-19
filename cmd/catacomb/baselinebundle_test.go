package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
)

const tarBlockSize = 512

var errBundleTestWrite = errors.New("bundle test: write failed")

type failingWriter struct {
	allow int
	count int
}

func (w *failingWriter) Write(p []byte) (int, error) {
	w.count++
	if w.count > w.allow {
		return 0, errBundleTestWrite
	}
	return len(p), nil
}

type countingWriter struct {
	writes int
}

func (w *countingWriter) Write(p []byte) (int, error) {
	w.writes++
	return len(p), nil
}

func bundleFixtureBaseline(runsDir string) model.Baseline {
	return model.Baseline{
		Name:      "golden",
		RunIDs:    []string{"run-b", "run-a"},
		Selector:  map[string]string{"variant": "base"},
		CreatedAt: time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC),
		RunsDir:   runsDir,
	}
}

func bundleFixtureFiles() map[string]string {
	return map[string]string{
		"runs/run-a/meta.json":                 `{"run_id":"run-a"}` + "\n",
		"runs/run-a/session.jsonl":             "a-line\n",
		"runs/run-b/session.jsonl":             "b-line\n",
		"runs/run-b/subagents/agent-001.jsonl": "sub-line\n",
	}
}

func writeBundleFixture(t *testing.T, files map[string]string) (model.Baseline, string) {
	t.Helper()
	runsDir := t.TempDir()
	for p, content := range files {
		rel := strings.TrimPrefix(p, "runs/")
		abs := filepath.Join(runsDir, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o750))
		require.NoError(t, os.WriteFile(abs, []byte(content), 0o600))
	}
	return bundleFixtureBaseline(runsDir), runsDir
}

func bundleSHA(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func collectBundleContents(files map[string][]byte) func(string, io.Reader) error {
	return func(p string, r io.Reader) error {
		data, err := io.ReadAll(r)
		if err != nil {
			return err
		}
		files[p] = data
		return nil
	}
}

func discardBundleFile(string, io.Reader) error { return nil }

type bundleTarEntry struct {
	name     string
	typeflag byte
	linkname string
	data     []byte
}

func bundleManifestEntry(t *testing.T, version int) bundleTarEntry {
	t.Helper()
	data, err := json.Marshal(bundleManifest{Version: version, Files: map[string]string{}})
	require.NoError(t, err)
	return bundleTarEntry{name: "bundle.json", typeflag: tar.TypeReg, data: data}
}

func gzipTarBundle(t *testing.T, entries ...bundleTarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		hdr := &tar.Header{
			Name:     e.name,
			Typeflag: e.typeflag,
			Linkname: e.linkname,
			Mode:     0o644,
			Size:     int64(len(e.data)),
			ModTime:  time.Unix(1, 0),
		}
		require.NoError(t, tw.WriteHeader(hdr))
		_, err := tw.Write(e.data)
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return buf.Bytes()
}

func gunzipBundle(t *testing.T, bundle []byte) []byte {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(bundle))
	require.NoError(t, err)
	raw, err := io.ReadAll(gz)
	require.NoError(t, err)
	return raw
}

func gzipBytes(t *testing.T, raw []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	_, err := gz.Write(raw)
	require.NoError(t, err)
	require.NoError(t, gz.Close())
	return buf.Bytes()
}

func TestWriteBundleReadBundleRoundTrip(t *testing.T) {
	files := bundleFixtureFiles()
	b, runsDir := writeBundleFixture(t, files)
	var buf bytes.Buffer
	count, err := writeBundle(&buf, b, runsDir)
	require.NoError(t, err)
	assert.Equal(t, len(files), count)
	got := map[string][]byte{}
	m, err := readBundle(bytes.NewReader(buf.Bytes()), collectBundleContents(got))
	require.NoError(t, err)
	assert.Equal(t, bundleVersion, m.Version)
	assert.Equal(t, b, m.Baseline)
	wantHashes := map[string]string{}
	wantContents := map[string][]byte{}
	for p, content := range files {
		wantHashes[p] = bundleSHA(content)
		wantContents[p] = []byte(content)
	}
	assert.Equal(t, wantHashes, m.Files)
	assert.Equal(t, wantContents, got)
}

func TestWriteBundleGzipHeaderCarriesNoHostState(t *testing.T) {
	b, runsDir := writeBundleFixture(t, bundleFixtureFiles())
	var buf bytes.Buffer
	_, err := writeBundle(&buf, b, runsDir)
	require.NoError(t, err)
	gz, err := gzip.NewReader(bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = gz.Close() })
	assert.True(t, gz.ModTime.IsZero(), "gzip mtime must not leak wall-clock time")
	assert.Equal(t, byte(255), gz.OS, "gzip OS byte must be unknown, not the building host")
	assert.Empty(t, gz.Name)
	assert.Empty(t, gz.Comment)
}

func TestWriteBundleBytesIdenticalAcrossSourceDirsAndFileMTimes(t *testing.T) {
	files := bundleFixtureFiles()
	b, firstDir := writeBundleFixture(t, files)
	_, secondDir := writeBundleFixture(t, files)
	require.NotEqual(t, firstDir, secondDir)

	skew := time.Date(1999, 4, 5, 6, 7, 8, 0, time.UTC)
	for p := range files {
		abs := filepath.Join(secondDir, filepath.FromSlash(strings.TrimPrefix(p, "runs/")))
		require.NoError(t, os.Chtimes(abs, skew, skew))
	}

	var firstBuf, secondBuf bytes.Buffer
	_, err := writeBundle(&firstBuf, b, firstDir)
	require.NoError(t, err)
	_, err = writeBundle(&secondBuf, b, secondDir)
	require.NoError(t, err)
	assert.Equal(t, firstBuf.Bytes(), secondBuf.Bytes())
}

func TestWriteBundleRunIDOrderDoesNotChangePackedEntries(t *testing.T) {
	files := bundleFixtureFiles()
	b, runsDir := writeBundleFixture(t, files)
	reversed := b
	reversed.RunIDs = slices.Clone(b.RunIDs)
	slices.Reverse(reversed.RunIDs)
	require.NotEqual(t, b.RunIDs, reversed.RunIDs)

	forward := map[string][]byte{}
	var forwardBuf bytes.Buffer
	_, err := writeBundle(&forwardBuf, b, runsDir)
	require.NoError(t, err)
	forwardManifest, err := readBundle(bytes.NewReader(forwardBuf.Bytes()), collectBundleContents(forward))
	require.NoError(t, err)

	backward := map[string][]byte{}
	var backwardBuf bytes.Buffer
	_, err = writeBundle(&backwardBuf, reversed, runsDir)
	require.NoError(t, err)
	backwardManifest, err := readBundle(bytes.NewReader(backwardBuf.Bytes()), collectBundleContents(backward))
	require.NoError(t, err)

	assert.Equal(t, forwardManifest.Files, backwardManifest.Files)
	assert.Equal(t, forward, backward)
	assert.Equal(t, bundleEntryNames(t, forwardBuf.Bytes()), bundleEntryNames(t, backwardBuf.Bytes()))
}

func bundleEntryNames(t *testing.T, bundle []byte) []string {
	t.Helper()
	tr := tar.NewReader(bytes.NewReader(gunzipBundle(t, bundle)))
	names := []string{}
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return names
		}
		require.NoError(t, err)
		names = append(names, hdr.Name)
	}
}

func TestWriteBundleEntryOrderAndNormalizedHeaders(t *testing.T) {
	b, runsDir := writeBundleFixture(t, bundleFixtureFiles())
	var buf bytes.Buffer
	_, err := writeBundle(&buf, b, runsDir)
	require.NoError(t, err)
	raw := gunzipBundle(t, buf.Bytes())
	assert.Zero(t, bytes.Count(raw, []byte("PaxHeaders")))
	tr := tar.NewReader(bytes.NewReader(raw))
	names := []string{}
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		names = append(names, hdr.Name)
		assert.Equal(t, byte(tar.TypeReg), hdr.Typeflag)
		assert.Equal(t, int64(0o644), hdr.Mode)
		assert.Zero(t, hdr.Uid)
		assert.Zero(t, hdr.Gid)
		assert.Empty(t, hdr.Uname)
		assert.Empty(t, hdr.Gname)
		assert.True(t, hdr.ModTime.Equal(b.CreatedAt))
		assert.NotZero(t, hdr.Format&tar.FormatUSTAR)
	}
	assert.Equal(t, []string{
		"bundle.json",
		"runs/run-a/meta.json",
		"runs/run-a/session.jsonl",
		"runs/run-b/session.jsonl",
		"runs/run-b/subagents/agent-001.jsonl",
	}, names)
}

func TestWriteBundleLongPathFallsBackToPAX(t *testing.T) {
	longRel := strings.Repeat("d", 20) + "/" + strings.Repeat("f", 120) + ".jsonl"
	longPath := "runs/run-a/" + longRel
	files := map[string]string{
		"runs/run-a/session.jsonl": "a-line\n",
		longPath:                   "deep\n",
	}
	b, runsDir := writeBundleFixture(t, files)
	b.RunIDs = []string{"run-a"}
	var first, second bytes.Buffer
	_, err := writeBundle(&first, b, runsDir)
	require.NoError(t, err)
	_, err = writeBundle(&second, b, runsDir)
	require.NoError(t, err)
	assert.Equal(t, first.Bytes(), second.Bytes())
	raw := gunzipBundle(t, first.Bytes())
	assert.Equal(t, 1, bytes.Count(raw, []byte("PaxHeaders")))
	formats := map[string]tar.Format{}
	tr := tar.NewReader(bytes.NewReader(raw))
	for {
		hdr, nextErr := tr.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		require.NoError(t, nextErr)
		formats[hdr.Name] = hdr.Format
	}
	require.Contains(t, formats, longPath)
	assert.Equal(t, tar.FormatPAX, formats[longPath])
	assert.NotZero(t, formats["bundle.json"]&tar.FormatUSTAR)
	got := map[string][]byte{}
	m, err := readBundle(bytes.NewReader(first.Bytes()), collectBundleContents(got))
	require.NoError(t, err)
	assert.Equal(t, bundleSHA("deep\n"), m.Files[longPath])
	assert.Equal(t, []byte("deep\n"), got[longPath])
}

func TestWriteBundleEmptyRunSet(t *testing.T) {
	b := bundleFixtureBaseline("")
	b.RunIDs = nil
	var buf bytes.Buffer
	count, err := writeBundle(&buf, b, t.TempDir())
	require.NoError(t, err)
	assert.Zero(t, count)
	got := map[string][]byte{}
	m, err := readBundle(bytes.NewReader(buf.Bytes()), collectBundleContents(got))
	require.NoError(t, err)
	assert.Equal(t, b, m.Baseline)
	assert.Equal(t, map[string]string{}, m.Files)
	assert.Empty(t, got)
}

func TestWriteBundleDuplicateRunIDsPackedOnce(t *testing.T) {
	files := map[string]string{"runs/run-a/session.jsonl": "a-line\n"}
	b, runsDir := writeBundleFixture(t, files)
	b.RunIDs = []string{"run-a", "run-a"}
	var buf bytes.Buffer
	count, err := writeBundle(&buf, b, runsDir)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	tr := tar.NewReader(bytes.NewReader(gunzipBundle(t, buf.Bytes())))
	names := []string{}
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		names = append(names, hdr.Name)
	}
	assert.Equal(t, []string{"bundle.json", "runs/run-a/session.jsonl"}, names)
}

func TestWriteBundleRunIDEscapes(t *testing.T) {
	for _, id := range []string{"../esc", ".", "", "nested/run", `nested\run`} {
		t.Run(fmt.Sprintf("id %q", id), func(t *testing.T) {
			b := bundleFixtureBaseline("")
			b.RunIDs = []string{id}
			_, err := writeBundle(io.Discard, b, t.TempDir())
			require.ErrorIs(t, err, errBundleRunID)
			assert.Contains(t, err.Error(), fmt.Sprintf("%q", id))
			assert.Contains(t, err.Error(), "escapes the runs dir")
		})
	}
}

func TestWriteBundleMissingRunDir(t *testing.T) {
	b := bundleFixtureBaseline("")
	b.RunIDs = []string{"ghost"}
	_, err := writeBundle(io.Discard, b, t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), `walk run "ghost"`)
}

func TestWriteBundleSymlinkRefused(t *testing.T) {
	files := map[string]string{"runs/run-a/session.jsonl": "a-line\n"}
	b, runsDir := writeBundleFixture(t, files)
	b.RunIDs = []string{"run-a"}
	link := filepath.Join(runsDir, "run-a", "loop.jsonl")
	if err := os.Symlink(filepath.Join(runsDir, "run-a", "session.jsonl"), link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, err := writeBundle(io.Discard, b, runsDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "symlink")
}

func TestWriteBundleManifestMarshalError(t *testing.T) {
	b := bundleFixtureBaseline("")
	b.RunIDs = nil
	b.CreatedAt = time.Date(10001, 1, 1, 0, 0, 0, 0, time.UTC)
	_, err := writeBundle(io.Discard, b, t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "encode manifest")
}

func TestBundleWalkEntryEscapesRunDir(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "evil.txt"), []byte("x"), 0o600))
	entries, err := os.ReadDir(tmp)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	_, _, werr := bundleWalkEntry(filepath.Join(tmp, "sub"), "r1", filepath.Join(tmp, "evil.txt"), entries[0])
	require.Error(t, werr)
	assert.Contains(t, werr.Error(), "escapes run dir")
}

func TestBundleWalkEntryReadError(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "real.txt"), []byte("x"), 0o600))
	entries, err := os.ReadDir(tmp)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	_, _, werr := bundleWalkEntry(tmp, "r1", filepath.Join(tmp, "ghost.txt"), entries[0])
	require.ErrorIs(t, werr, fs.ErrNotExist)
}

func TestWriteBundleTarWriteFailures(t *testing.T) {
	files := []bundleFile{{path: "runs/r1/f.txt", data: []byte("hello")}}
	b := bundleFixtureBaseline("")
	counter := &countingWriter{}
	require.NoError(t, writeBundleTar(counter, b, files))
	require.Positive(t, counter.writes)
	for n := 0; n < counter.writes; n++ {
		t.Run(fmt.Sprintf("fail write %d", n+1), func(t *testing.T) {
			err := writeBundleTar(&failingWriter{allow: n}, b, files)
			require.ErrorIs(t, err, errBundleTestWrite)
		})
	}
}

func TestWriteBundleWriterFailures(t *testing.T) {
	files := map[string]string{"runs/run-a/session.jsonl": "a-line\n"}
	b, runsDir := writeBundleFixture(t, files)
	b.RunIDs = []string{"run-a"}
	cases := []struct {
		name  string
		allow int
		want  string
	}{
		{"first write fails inside tar", 0, "write header"},
		{"close fails", 1, "close gzip"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := writeBundle(&failingWriter{allow: tc.allow}, b, runsDir)
			require.ErrorIs(t, err, errBundleTestWrite)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestReadBundleGarbageGzip(t *testing.T) {
	_, err := readBundle(bytes.NewReader([]byte("not a gzip stream")), discardBundleFile)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "open gzip")
}

func TestReadBundleTruncatedGzip(t *testing.T) {
	b, runsDir := writeBundleFixture(t, bundleFixtureFiles())
	var buf bytes.Buffer
	_, err := writeBundle(&buf, b, runsDir)
	require.NoError(t, err)
	bundle := buf.Bytes()
	_, err = readBundle(bytes.NewReader(bundle[:len(bundle)/2]), discardBundleFile)
	require.Error(t, err)
}

func TestReadBundleEmptyTar(t *testing.T) {
	_, err := readBundle(bytes.NewReader(gzipBytes(t, nil)), discardBundleFile)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read first entry")
}

func TestReadBundleFirstEntryNotManifest(t *testing.T) {
	bundle := gzipTarBundle(t, bundleTarEntry{name: "evil.txt", typeflag: tar.TypeReg, data: []byte("x")})
	_, err := readBundle(bytes.NewReader(bundle), discardBundleFile)
	require.ErrorIs(t, err, errBundleEntry)
	assert.Contains(t, err.Error(), "bundle.json")
}

func TestReadBundleManifestTruncatedBody(t *testing.T) {
	b, runsDir := writeBundleFixture(t, bundleFixtureFiles())
	var buf bytes.Buffer
	_, err := writeBundle(&buf, b, runsDir)
	require.NoError(t, err)
	raw := gunzipBundle(t, buf.Bytes())
	hdr, err := tar.NewReader(bytes.NewReader(raw)).Next()
	require.NoError(t, err)
	require.Equal(t, "bundle.json", hdr.Name)
	require.Greater(t, hdr.Size, int64(1))
	cut := tarBlockSize + int(hdr.Size)/2
	require.Less(t, cut, len(raw))
	_, err = readBundle(bytes.NewReader(gzipBytes(t, raw[:cut])), discardBundleFile)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read manifest")
}

func TestReadBundleManifestNotJSON(t *testing.T) {
	bundle := gzipTarBundle(t, bundleTarEntry{name: "bundle.json", typeflag: tar.TypeReg, data: []byte("not json")})
	_, err := readBundle(bytes.NewReader(bundle), discardBundleFile)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode manifest")
}

func TestReadBundleManifestTooLarge(t *testing.T) {
	cases := []struct {
		name string
		data []byte
	}{
		{"valid json padded past the cap", append([]byte(`{"version":1}`), bytes.Repeat([]byte(" "), maxBundleManifestBytes)...)},
		{"garbage past the cap", bytes.Repeat([]byte("x"), maxBundleManifestBytes+1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bundle := gzipTarBundle(t, bundleTarEntry{name: bundleManifestName, typeflag: tar.TypeReg, data: tc.data})
			_, err := readBundle(bytes.NewReader(bundle), discardBundleFile)
			require.ErrorIs(t, err, errBundleManifestTooLarge)
			assert.NotContains(t, err.Error(), "decode manifest")
		})
	}
}

func TestReadBundleManifestAtCapAccepted(t *testing.T) {
	manifest := []byte(`{"version":1,"files":{}}`)
	manifest = append(manifest, bytes.Repeat([]byte(" "), maxBundleManifestBytes-len(manifest))...)
	require.Len(t, manifest, maxBundleManifestBytes)
	bundle := gzipTarBundle(t, bundleTarEntry{name: bundleManifestName, typeflag: tar.TypeReg, data: manifest})
	m, err := readBundle(bytes.NewReader(bundle), discardBundleFile)
	require.NoError(t, err)
	assert.Equal(t, bundleVersion, m.Version)
}

func TestReadBundleVersionTooNew(t *testing.T) {
	bundle := gzipTarBundle(t, bundleManifestEntry(t, 2))
	_, err := readBundle(bytes.NewReader(bundle), discardBundleFile)
	require.ErrorIs(t, err, errBundleVersion)
}

func TestReadBundleHostileEntries(t *testing.T) {
	cases := []struct {
		name  string
		entry bundleTarEntry
	}{
		{"dot dot escape", bundleTarEntry{name: "../evil", typeflag: tar.TypeReg, data: []byte("x")}},
		{"absolute path", bundleTarEntry{name: "/evil", typeflag: tar.TypeReg, data: []byte("x")}},
		{"symlink", bundleTarEntry{name: "runs/r1/link", typeflag: tar.TypeSymlink, linkname: "../../etc/passwd"}},
		{"hardlink", bundleTarEntry{name: "runs/r1/hard", typeflag: tar.TypeLink, linkname: "bundle.json"}},
		{"directory", bundleTarEntry{name: "runs/r1/dir", typeflag: tar.TypeDir}},
		{"outside runs", bundleTarEntry{name: "notes/readme.txt", typeflag: tar.TypeReg, data: []byte("x")}},
		{"shallow run path", bundleTarEntry{name: "runs/r1", typeflag: tar.TypeReg, data: []byte("x")}},
		{"unclean path", bundleTarEntry{name: "runs/r1/../r2/f", typeflag: tar.TypeReg, data: []byte("x")}},
		{"second manifest", bundleTarEntry{name: "bundle.json", typeflag: tar.TypeReg, data: []byte("{}")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bundle := gzipTarBundle(t, bundleManifestEntry(t, 1), tc.entry)
			_, err := readBundle(bytes.NewReader(bundle), discardBundleFile)
			require.ErrorIs(t, err, errBundleEntry)
		})
	}
}

func TestReadBundleCorruptSecondHeader(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	entry := bundleManifestEntry(t, 1)
	hdr := &tar.Header{
		Name:     entry.name,
		Typeflag: entry.typeflag,
		Mode:     0o644,
		Size:     int64(len(entry.data)),
		ModTime:  time.Unix(1, 0),
	}
	require.NoError(t, tw.WriteHeader(hdr))
	_, err := tw.Write(entry.data)
	require.NoError(t, err)
	require.NoError(t, tw.Flush())
	_, err = gz.Write(bytes.Repeat([]byte{0xff}, 512))
	require.NoError(t, err)
	require.NoError(t, gz.Close())
	_, rerr := readBundle(bytes.NewReader(buf.Bytes()), discardBundleFile)
	require.Error(t, rerr)
	assert.Contains(t, rerr.Error(), "read entry")
}

func TestReadBundleCallbackError(t *testing.T) {
	b, runsDir := writeBundleFixture(t, bundleFixtureFiles())
	var buf bytes.Buffer
	_, err := writeBundle(&buf, b, runsDir)
	require.NoError(t, err)
	errCallback := errors.New("bundle test: callback refused")
	_, err = readBundle(bytes.NewReader(buf.Bytes()), func(string, io.Reader) error { return errCallback })
	require.ErrorIs(t, err, errCallback)
}

func TestBundleSentinelsDistinct(t *testing.T) {
	sentinels := []error{errBundleVersion, errBundleEntry, errBundleHash, errBundleCollision, errBundleManifestTooLarge, errBundleRunSet}
	for i, first := range sentinels {
		require.Error(t, first)
		for j, second := range sentinels {
			if i != j {
				assert.NotErrorIs(t, first, second)
			}
		}
	}
}
