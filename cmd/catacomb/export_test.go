package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/evidence"
	"github.com/realkarych/catacomb/model"
)

func decodeSnapshotLines(t *testing.T, out string) []map[string]any {
	t.Helper()
	var lines []map[string]any
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		var line map[string]any
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &line))
		lines = append(lines, line)
	}
	return lines
}

var ulidPattern = regexp.MustCompile(`[0-9A-HJKMNP-TV-Z]{26}`)

func withoutExecutionIDs(snapshot string) string {
	return ulidPattern.ReplaceAllString(snapshot, "EXEC")
}

func countKinds(lines []map[string]any) map[string]int {
	counts := map[string]int{}
	for _, l := range lines {
		kind, _ := l["kind"].(string)
		counts[kind]++
	}
	return counts
}

func TestExportTranscriptViaRootEmitsKindLines(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"export", "testdata/session.jsonl"})
	var buf strings.Builder
	root.SetOut(&buf)
	require.NoError(t, root.Execute())

	lines := decodeSnapshotLines(t, buf.String())
	counts := countKinds(lines)
	assert.Positive(t, counts["node"])
	assert.Positive(t, counts["edge"])
	assert.Positive(t, counts["run"])

	var prev string
	for _, l := range lines {
		if l["kind"] != "node" {
			continue
		}
		id, _ := l["id"].(string)
		assert.LessOrEqual(t, prev, id)
		prev = id
	}
}

func TestExportExplicitJSONLMatchesTheDefaultSinkModuloExecutionIDs(t *testing.T) {
	var explicit strings.Builder
	require.NoError(t, runExport(&explicit, exportArgs{input: "testdata/session.jsonl", to: "jsonl"}))

	root := newRootCmd()
	var defaulted strings.Builder
	root.SetOut(&defaulted)
	root.SetArgs([]string{"export", "testdata/session.jsonl"})
	require.NoError(t, root.Execute())

	counts := countKinds(decodeSnapshotLines(t, explicit.String()))
	require.Positive(t, counts["node"])
	require.Positive(t, counts["run"])
	assert.Equal(t, withoutExecutionIDs(defaulted.String()), withoutExecutionIDs(explicit.String()))
}

func writeExportEvidenceDir(t *testing.T, subSessionIDs ...string) string {
	t.Helper()
	if len(subSessionIDs) == 0 {
		subSessionIDs = []string{"sub1"}
	}
	src := t.TempDir()
	dir := filepath.Join(t.TempDir(), "run-ev1")
	m := evidence.Meta{
		RunID:       "run-ev1",
		Task:        "t1",
		Variant:     "base",
		Rep:         1,
		SessionID:   "s1",
		Labels:      map[string]string{"variant": "base"},
		MarkerName:  "task:t1",
		MarkerStart: time.Unix(100, 0).UTC(),
		MarkerEnd:   time.Unix(200, 0).UTC(),
		FinishedAt:  time.Unix(201, 0).UTC(),
	}
	files := []evidence.SourceFile{{Src: filepath.Join("testdata", "session.jsonl"), Rel: "session.jsonl"}}
	for i, sessionID := range subSessionIDs {
		name := fmt.Sprintf("agent-%03d.jsonl", i+1)
		p := filepath.Join(src, name)
		writeSubagentTranscript(t, p, sessionID)
		files = append(files, evidence.SourceFile{Src: p, Rel: filepath.Join("subagents", name)})
	}
	require.NoError(t, evidence.Write(dir, m, files))
	return dir
}

func TestExportEvidenceDirSynthesizesBoundaryAndMetaRun(t *testing.T) {
	dir := writeExportEvidenceDir(t)
	var buf strings.Builder
	require.NoError(t, runExport(&buf, exportArgs{input: dir, to: "jsonl"}))

	lines := decodeSnapshotLines(t, buf.String())
	var sawMarker, sawSubagentNode, sawMetaRun bool
	var runCount int
	for _, l := range lines {
		switch l["kind"] {
		case "node":
			if l["type"] == "marker" && l["name"] == "task:t1" {
				sawMarker = true
			}
			if l["name"] == "Read" {
				sawSubagentNode = true
			}
		case "run":
			runCount++
			if l["id"] == "run-ev1" {
				sawMetaRun = true
				labels, ok := l["labels"].(map[string]any)
				require.True(t, ok)
				assert.Equal(t, "base", labels["variant"])
				assert.Equal(t, []any{"s1", "sub1"}, l["session_ids"])
			}
		}
	}
	assert.True(t, sawMarker)
	assert.True(t, sawSubagentNode)
	assert.True(t, sawMetaRun)
	assert.Equal(t, 1, runCount)
}

func TestExportTranscriptSortsRuns(t *testing.T) {
	transcript := filepath.Join(t.TempDir(), "two.jsonl")
	lines := `{"type":"user","uuid":"u1","sessionId":"sB","timestamp":"2026-06-20T10:00:00Z","message":{"role":"user","content":"hi"}}
{"type":"user","uuid":"u2","sessionId":"sA","timestamp":"2026-06-20T10:00:01Z","message":{"role":"user","content":"hi"}}
`
	require.NoError(t, os.WriteFile(transcript, []byte(lines), 0o600))

	var buf strings.Builder
	require.NoError(t, runExport(&buf, exportArgs{input: transcript, to: "jsonl"}))

	var runIDs []string
	for _, l := range decodeSnapshotLines(t, buf.String()) {
		if l["kind"] == "run" {
			id, _ := l["id"].(string)
			runIDs = append(runIDs, id)
		}
	}
	assert.Equal(t, []string{"sA", "sB"}, runIDs)
}

func TestExportEvidenceDirWithoutMetaErrors(t *testing.T) {
	err := runExport(io.Discard, exportArgs{input: t.TempDir(), to: "jsonl"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "evidence.ReadMeta")
}

func TestExportEvidenceDirMissingSessionErrors(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "broken")
	m := evidence.Meta{RunID: "broken", SessionID: "s1"}
	require.NoError(t, evidence.Write(dir, m, nil))
	err := runExport(io.Discard, exportArgs{input: dir, to: "jsonl"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "session.jsonl")
}

func TestExportUnknownFormatRejected(t *testing.T) {
	err := runExport(io.Discard, exportArgs{input: "testdata/session.jsonl", to: "otlp"})
	assert.True(t, errors.Is(err, ErrUnknownSink))
}

func TestExportMissingInput(t *testing.T) {
	err := runExport(io.Discard, exportArgs{input: filepath.Join(t.TempDir(), "nope.jsonl"), to: "jsonl"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "export input")
}

func TestExportMalformedTranscript(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "bad.jsonl")
	require.NoError(t, os.WriteFile(bad, []byte("{not json}\n"), 0o600))
	err := runExport(io.Discard, exportArgs{input: bad, to: "jsonl"})
	require.Error(t, err)
}

func TestExportOutFileHoldsExactlyWhatStdoutWouldHaveHeld(t *testing.T) {
	var stdout strings.Builder
	require.NoError(t, runExport(&stdout, exportArgs{input: "testdata/session.jsonl", to: "jsonl"}))
	require.NotEmpty(t, stdout.String())

	outPath := filepath.Join(t.TempDir(), "out.jsonl")
	root := newRootCmd()
	var viaRoot strings.Builder
	root.SetArgs([]string{"export", "testdata/session.jsonl", "--to", "jsonl", "--out", outPath})
	root.SetOut(&viaRoot)
	require.NoError(t, root.Execute())

	data, err := os.ReadFile(outPath)
	require.NoError(t, err)
	assert.Equal(t, withoutExecutionIDs(stdout.String()), withoutExecutionIDs(string(data)))
	assert.Empty(t, viaRoot.String(), "--out must divert the snapshot away from stdout entirely")
}

func TestExportBadOutPath(t *testing.T) {
	err := runExport(io.Discard, exportArgs{
		input: "testdata/session.jsonl",
		to:    "jsonl",
		out:   filepath.Join(t.TempDir(), "nodir", "x.jsonl"),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "export create")
}

func TestExportRedactsSecretBearingTranscript(t *testing.T) {
	transcript := filepath.Join(t.TempDir(), "s.jsonl")
	line := `{"type":"assistant","sessionId":"exp-s","message":{"id":"m1","model":"m","role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"psql postgres://kesha:kesha_dev_password@localhost/appdb"}}]}}`
	require.NoError(t, os.WriteFile(transcript, []byte(line+"\n"), 0o600))

	var buf strings.Builder
	require.NoError(t, runExport(&buf, exportArgs{input: transcript, to: "jsonl"}))
	out := buf.String()
	assert.NotContains(t, out, "kesha_dev_password")
	assert.Contains(t, out, "‹redacted:connection-string›")
}

func TestExportMissingInputIsOperational(t *testing.T) {
	var out, errBuf bytes.Buffer
	code := run([]string{"export", filepath.Join(t.TempDir(), "nope.jsonl")}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), "export input")
}

func TestExportUnknownSinkIsOperational(t *testing.T) {
	var out, errBuf bytes.Buffer
	code := run([]string{"export", "testdata/session.jsonl", "--to", "otlp"}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), "unknown export format")
}

func TestExportWarnsOnNewerVersion(t *testing.T) {
	resetDriftWarnings()
	buf := captureDriftOut(t)
	versioned := writeVersionedCopy(t, filepath.Join("testdata", "session.jsonl"), "9.9.9")
	root := newRootCmd()
	root.SetArgs([]string{"export", versioned})
	root.SetOut(io.Discard)
	require.NoError(t, root.Execute())
	assert.Contains(t, buf.String(), "9.9.9")
	assert.Contains(t, buf.String(), "newer than tested")
}

type closeErrWriter struct {
	buf      bytes.Buffer
	writeErr error
	closeErr error
}

func (w *closeErrWriter) Write(p []byte) (int, error) {
	if w.writeErr != nil {
		return 0, w.writeErr
	}
	return w.buf.Write(p)
}

func (w *closeErrWriter) Close() error { return w.closeErr }

func TestSnapshotAndCloseReportsCloseError(t *testing.T) {
	errClose := errors.New("flush on close failed")
	w := &closeErrWriter{closeErr: errClose}
	err := snapshotAndClose(w, []*model.Node{{ID: "n1"}}, nil, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, errClose)
}

func TestSnapshotAndCloseJoinsWriteAndCloseErrors(t *testing.T) {
	errWrite := errors.New("write failed")
	errClose := errors.New("close failed")
	w := &closeErrWriter{writeErr: errWrite, closeErr: errClose}
	err := snapshotAndClose(w, []*model.Node{{ID: "n1"}}, nil, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, errWrite)
	assert.ErrorIs(t, err, errClose)
}

func TestSnapshotAndCloseSucceeds(t *testing.T) {
	w := &closeErrWriter{}
	require.NoError(t, snapshotAndClose(w, []*model.Node{{ID: "n1"}}, nil, nil))
	assert.Contains(t, w.buf.String(), `"n1"`)
}

func TestExportEvidenceDirNodesJoinToRunRecord(t *testing.T) {
	dir := writeExportEvidenceDir(t)
	var buf strings.Builder
	require.NoError(t, runExport(&buf, exportArgs{input: dir, to: "jsonl"}))

	lines := decodeSnapshotLines(t, buf.String())
	var runIDs []string
	var sessionIDs []any
	nodeRunIDs := map[string]int{}
	edgeRunIDs := map[string]int{}
	for _, l := range lines {
		switch l["kind"] {
		case "node":
			nodeRunIDs[l["run_id"].(string)]++
		case "edge":
			edgeRunIDs[l["run_id"].(string)]++
		case "run":
			runIDs = append(runIDs, l["id"].(string))
			sessionIDs, _ = l["session_ids"].([]any)
		}
	}
	require.Equal(t, []string{"run-ev1"}, runIDs)
	require.NotEmpty(t, nodeRunIDs)
	require.NotEmpty(t, edgeRunIDs)
	assert.Equal(t, []string{"run-ev1"}, sortedKeys(nodeRunIDs), "every node must join the run record")
	assert.Equal(t, []string{"run-ev1"}, sortedKeys(edgeRunIDs), "every edge must join the run record")
	assert.Equal(t, []any{"s1", "sub1"}, sessionIDs)
}

func TestExportEvidenceDirSessionIDsSortedAcrossSubagents(t *testing.T) {
	dir := writeExportEvidenceDir(t, "sub3", "sub1", "sub2")
	for i := 0; i < 20; i++ {
		var buf strings.Builder
		require.NoError(t, runExport(&buf, exportArgs{input: dir, to: "jsonl"}))
		var seen int
		for _, l := range decodeSnapshotLines(t, buf.String()) {
			if l["kind"] != "run" {
				continue
			}
			seen++
			require.Equal(t, []any{"s1", "sub1", "sub2", "sub3"}, l["session_ids"],
				"session_ids must be stably ordered across identical invocations")
		}
		require.Equal(t, 1, seen)
	}
}

func sortedKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
