package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/evidence"
	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/regress"
)

func writeEvidenceRun(t *testing.T, root, id, variant, fixture string) {
	t.Helper()
	m := evidence.Meta{
		RunID:       id,
		Task:        "t1",
		Variant:     variant,
		Rep:         1,
		SessionID:   "s1",
		Labels:      map[string]string{"variant": variant},
		MarkerName:  "task:t1",
		MarkerStart: time.Unix(100, 0).UTC(),
		MarkerEnd:   time.Unix(200, 0).UTC(),
		FinishedAt:  time.Unix(201, 0).UTC(),
	}
	src := filepath.Join("testdata", fixture)
	require.NoError(t, evidence.Write(filepath.Join(root, id), m, []evidence.SourceFile{{Src: src, Rel: "session.jsonl"}}))
}

func evidenceRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeEvidenceRun(t, root, "base-0", "base", "session_marked.jsonl")
	writeEvidenceRun(t, root, "base-1", "base", "session_marked.jsonl")
	writeEvidenceRun(t, root, "cand-0", "cand", "session.jsonl")
	writeEvidenceRun(t, root, "cand-1", "cand", "session.jsonl")
	return root
}

func TestRunsDirResolveGroupsAndFilter(t *testing.T) {
	root := evidenceRoot(t)
	base, err := resolveSelectorRunsDir(root, newPricer(), "label:variant=base")
	require.NoError(t, err)
	require.Len(t, base, 2)
	assert.Equal(t, "base-0", base[0].Run.ID)
	for _, rg := range base {
		assert.Equal(t, "base", rg.Run.Labels["variant"])
		assert.Equal(t, []string{"s1"}, rg.Run.SessionIDs)
		assert.NotEmpty(t, rg.Nodes)
	}
	cand, err := resolveSelectorRunsDir(root, newPricer(), "label:variant=cand")
	require.NoError(t, err)
	assert.Len(t, cand, 2)
}

func TestRunsDirNameSelectorErrorsPV2(t *testing.T) {
	root := evidenceRoot(t)
	_, err := resolveSelectorRunsDir(root, newPricer(), "name:golden")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name:")
	assert.Contains(t, err.Error(), "PV-2")
}

func TestRunsDirEmptyMatchNamesSelector(t *testing.T) {
	root := evidenceRoot(t)
	_, err := resolveSelectorRunsDir(root, newPricer(), "label:variant=none")
	require.ErrorIs(t, err, ErrEmptyGroup)
	assert.Contains(t, err.Error(), "label:variant=none")
}

func TestRunsDirBadSelectorOperational(t *testing.T) {
	root := evidenceRoot(t)
	_, err := resolveSelectorRunsDir(root, newPricer(), "bogus")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid selector")
}

func TestRunsDirBadLabelTermOperational(t *testing.T) {
	root := evidenceRoot(t)
	_, err := resolveSelectorRunsDir(root, newPricer(), "label:BAD=x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid --label")
}

func TestRunsDirScanError(t *testing.T) {
	_, err := resolveSelectorRunsDir(filepath.Join(t.TempDir(), "absent"), newPricer(), "label:variant=base")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "runs-dir")
}

func TestRunsDirEvidenceLoadErrorPropagates(t *testing.T) {
	root := t.TempDir()
	m := evidence.Meta{
		RunID:       "broken",
		Variant:     "base",
		SessionID:   "s1",
		Labels:      map[string]string{"variant": "base"},
		MarkerName:  "task:t1",
		MarkerStart: time.Unix(1, 0).UTC(),
		MarkerEnd:   time.Unix(2, 0).UTC(),
	}
	require.NoError(t, evidence.Write(filepath.Join(root, "broken"), m, nil))
	_, err := resolveSelectorRunsDir(root, newPricer(), "label:variant=base")
	require.Error(t, err)
}

func TestEvidenceRunGraphNoMarker(t *testing.T) {
	root := t.TempDir()
	m := evidence.Meta{RunID: "r0", SessionID: "s1", Labels: map[string]string{"variant": "base"}}
	require.NoError(t, evidence.Write(filepath.Join(root, "r0"), m, []evidence.SourceFile{{Src: filepath.Join("testdata", "session.jsonl"), Rel: "session.jsonl"}}))
	rg, err := evidenceRunGraph(filepath.Join(root, "r0"), m, newPricer())
	require.NoError(t, err)
	assert.Equal(t, "r0", rg.Run.ID)
	assert.Equal(t, []string{"s1"}, rg.Run.SessionIDs)
	assert.NotEmpty(t, rg.Nodes)
	names := map[string]struct{}{}
	for _, n := range rg.Nodes {
		names[n.Name] = struct{}{}
	}
	_, ok := names["task:t1"]
	assert.False(t, ok, "no synthesized marker when MarkerName empty")
	assert.Nil(t, rg.Run.StartedAt)
	assert.Nil(t, rg.Run.EndedAt)
}

func TestEvidenceRunGraphRunWindowFromMarkerTimes(t *testing.T) {
	root := t.TempDir()
	m := evidence.Meta{
		RunID:       "r2",
		SessionID:   "s1",
		Labels:      map[string]string{"variant": "base"},
		MarkerName:  "task:t1",
		MarkerStart: time.Unix(100, 0).UTC(),
		MarkerEnd:   time.Unix(200, 0).UTC(),
		FinishedAt:  time.Unix(201, 0).UTC(),
	}
	require.NoError(t, evidence.Write(filepath.Join(root, "r2"), m, []evidence.SourceFile{{Src: filepath.Join("testdata", "session.jsonl"), Rel: "session.jsonl"}}))
	rg, err := evidenceRunGraph(filepath.Join(root, "r2"), m, newPricer())
	require.NoError(t, err)
	require.NotNil(t, rg.Run.StartedAt)
	require.NotNil(t, rg.Run.EndedAt)
	assert.Equal(t, m.MarkerStart, *rg.Run.StartedAt)
	assert.Equal(t, m.MarkerEnd, *rg.Run.EndedAt)
}

func TestEvidenceRunGraphOverlaysStatusAndModel(t *testing.T) {
	root := t.TempDir()
	m := evidence.Meta{
		RunID:       "r3",
		SessionID:   "s1",
		Labels:      map[string]string{"variant": "base"},
		MarkerName:  "task:t1",
		MarkerStart: time.Unix(100, 0).UTC(),
		MarkerEnd:   time.Unix(200, 0).UTC(),
		FinishedAt:  time.Unix(201, 0).UTC(),
	}
	require.NoError(t, evidence.Write(filepath.Join(root, "r3"), m, []evidence.SourceFile{{Src: filepath.Join("testdata", "session_marked.jsonl"), Rel: "session.jsonl"}}))
	rg, err := evidenceRunGraph(filepath.Join(root, "r3"), m, newPricer())
	require.NoError(t, err)
	assert.Equal(t, model.StatusRunning, rg.Run.Status)
	assert.Equal(t, "claude-opus-4-8", rg.Run.ModelID)
	require.NotNil(t, rg.Run.StartedAt)
	require.NotNil(t, rg.Run.EndedAt)
	assert.Equal(t, m.MarkerStart, *rg.Run.StartedAt)
	assert.Equal(t, m.MarkerEnd, *rg.Run.EndedAt)
}

func TestEvidenceRunGraphEmptySnapshotLeavesRunZero(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "r4")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "session.jsonl"), nil, 0o600))
	m := evidence.Meta{RunID: "r4", SessionID: "s1"}
	rg, err := evidenceRunGraph(dir, m, newPricer())
	require.NoError(t, err)
	assert.Empty(t, rg.Run.Status)
	assert.Empty(t, rg.Run.ModelID)
	assert.Nil(t, rg.Run.StartedAt)
	assert.Nil(t, rg.Run.EndedAt)
}

func TestEvidenceRunGraphOverlayKeyedByMetaSessionID(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "r5")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "subagents"), 0o700))
	data, err := os.ReadFile(filepath.Join("testdata", "session.jsonl"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "session.jsonl"), data, 0o600))
	for i := 0; i < 8; i++ {
		sub := strings.ReplaceAll(string(data), `"sessionId":"s1"`, fmt.Sprintf(`"sessionId":"sub-%d"`, i))
		sub = strings.ReplaceAll(sub, "claude-opus-4-8", "claude-subagent-model")
		require.NoError(t, os.WriteFile(filepath.Join(dir, "subagents", fmt.Sprintf("agent-%d.jsonl", i)), []byte(sub), 0o600))
	}
	m := evidence.Meta{RunID: "r5", SessionID: "s1", Labels: map[string]string{"variant": "base"}}
	for i := 0; i < 20; i++ {
		rg, gerr := evidenceRunGraph(dir, m, newPricer())
		require.NoError(t, gerr)
		require.Equal(t, "claude-opus-4-8", rg.Run.ModelID)
		require.Equal(t, model.StatusRunning, rg.Run.Status)
	}
}

func TestEvidenceRunGraphOverlayNoSessionMatchLeavesRunZero(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "r6")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	data, err := os.ReadFile(filepath.Join("testdata", "session.jsonl"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "session.jsonl"), data, 0o600))
	m := evidence.Meta{RunID: "r6", SessionID: "other-session"}
	rg, err := evidenceRunGraph(dir, m, newPricer())
	require.NoError(t, err)
	assert.Empty(t, rg.Run.Status)
	assert.Empty(t, rg.Run.ModelID)
}

func TestEvidenceRunGraphMergesSubagents(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "r1")
	data, err := os.ReadFile(filepath.Join("testdata", "session.jsonl"))
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "subagents"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "session.jsonl"), data, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "subagents", "agent-a.jsonl"), data, 0o600))
	m := evidence.Meta{RunID: "r1", SessionID: "s1", MarkerName: "task:t1", MarkerStart: time.Unix(1, 0).UTC(), MarkerEnd: time.Unix(2, 0).UTC()}
	rg, err := evidenceRunGraph(dir, m, newPricer())
	require.NoError(t, err)
	assert.NotEmpty(t, rg.Nodes)
}

func TestRegressRunsDirFullOffline(t *testing.T) {
	root := evidenceRoot(t)
	var out, errBuf bytes.Buffer
	code := run([]string{
		"regress", "--runs-dir", root,
		"--db", filepath.Join(t.TempDir(), "nope.db"),
		"--baseline", "label:variant=base", "--candidate", "label:variant=cand",
	}, &out, &errBuf)
	assert.Equal(t, 0, code, errBuf.String())
	assert.Contains(t, out.String(), "overall")
	assert.Empty(t, errBuf.String())
}

func TestRegressRunsDirRecordConflict(t *testing.T) {
	root := evidenceRoot(t)
	var out, errBuf bytes.Buffer
	code := run([]string{
		"regress", "--runs-dir", root, "--record",
		"--baseline", "label:variant=base", "--candidate", "label:variant=cand",
	}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), "offline baselines land in PV-2")
}

func TestRegressRunsDirEmptyBaselineExitTwo(t *testing.T) {
	root := evidenceRoot(t)
	var out, errBuf bytes.Buffer
	code := run([]string{
		"regress", "--runs-dir", root,
		"--baseline", "label:variant=none", "--candidate", "label:variant=cand",
	}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), "matched no runs")
}

func TestRegressRunsDirEmptyCandidateExitTwo(t *testing.T) {
	root := evidenceRoot(t)
	var out, errBuf bytes.Buffer
	code := run([]string{
		"regress", "--runs-dir", root,
		"--baseline", "label:variant=base", "--candidate", "label:variant=none",
	}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), "matched no runs")
}

func TestRegressRunsDirRenderErrorOperational(t *testing.T) {
	root := evidenceRoot(t)
	f := regressFlags{
		runsDir:    root,
		baseline:   "label:variant=base",
		candidate:  "label:variant=cand",
		asJSON:     true,
		thresholds: regress.DefaultThresholds(),
	}
	err := runRegress(failWriter{}, io.Discard, openStore(nil), newPricer, f)
	require.Error(t, err)
	assert.NotErrorIs(t, err, errRegressionDetected)
	var opErr *operationalError
	require.ErrorAs(t, err, &opErr)
}
