package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/aggregate"
	"github.com/realkarych/catacomb/evidence"
	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/regress"
	"github.com/realkarych/catacomb/store"
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

func writeTokenEvidenceRun(t *testing.T, root, id, variant string, inputTokens int) {
	t.Helper()
	writeLabeledTokenEvidenceRun(t, root, id, variant, inputTokens, map[string]string{"variant": variant})
}

func writeTaskTokenEvidenceRun(t *testing.T, root, id, variant string, inputTokens int) {
	t.Helper()
	writeLabeledTokenEvidenceRun(t, root, id, variant, inputTokens, map[string]string{"variant": variant, "task": "t1"})
}

func writeLabeledTokenEvidenceRun(t *testing.T, root, id, variant string, inputTokens int, labels map[string]string) {
	t.Helper()
	transcript := fmt.Sprintf(`{"type":"assistant","uuid":"a1","sessionId":"s1","timestamp":"2026-06-20T10:00:01Z","message":{"role":"assistant","id":"msg_1","model":"claude-opus-4-8","content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"ls"}}],"usage":{"input_tokens":%d,"output_tokens":5}}}
{"type":"user","uuid":"u2","parentUuid":"a1","sessionId":"s1","timestamp":"2026-06-20T10:00:02Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"ok","is_error":false}]}}
`, inputTokens)
	src := filepath.Join(t.TempDir(), id+".jsonl")
	require.NoError(t, os.WriteFile(src, []byte(transcript), 0o600))
	m := evidence.Meta{
		RunID:       id,
		Task:        "t1",
		Variant:     variant,
		Rep:         1,
		SessionID:   "s1",
		Labels:      labels,
		MarkerName:  "task:t1",
		MarkerStart: time.Unix(100, 0).UTC(),
		MarkerEnd:   time.Unix(200, 0).UTC(),
		FinishedAt:  time.Unix(201, 0).UTC(),
	}
	require.NoError(t, evidence.Write(filepath.Join(root, id), m, []evidence.SourceFile{{Src: src, Rel: "session.jsonl"}}))
}

func TestRunsDirResolveGroupsAndFilter(t *testing.T) {
	root := evidenceRoot(t)
	base, _, err := resolveSelectorRunsDir(io.Discard, "", root, newPricer(), "label:variant=base", loadForAggregation)
	require.NoError(t, err)
	require.Len(t, base, 2)
	assert.Equal(t, "base-0", base[0].Run.ID)
	for _, rg := range base {
		assert.Equal(t, "base", rg.Run.Labels["variant"])
		assert.Equal(t, []string{"s1"}, rg.Run.SessionIDs)
		assert.NotEmpty(t, rg.Nodes)
	}
	cand, _, err := resolveSelectorRunsDir(io.Discard, "", root, newPricer(), "label:variant=cand", loadForAggregation)
	require.NoError(t, err)
	assert.Len(t, cand, 2)
}

func TestRunsDirEmptyMatchNamesSelector(t *testing.T) {
	root := evidenceRoot(t)
	_, _, err := resolveSelectorRunsDir(io.Discard, "", root, newPricer(), "label:variant=none", loadForAggregation)
	require.ErrorIs(t, err, ErrEmptyGroup)
	assert.Contains(t, err.Error(), "label:variant=none")
}

func TestRunsDirBadSelectorOperational(t *testing.T) {
	root := evidenceRoot(t)
	_, _, err := resolveSelectorRunsDir(io.Discard, "", root, newPricer(), "bogus", loadForAggregation)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid selector")
}

func TestRunsDirBadLabelTermOperational(t *testing.T) {
	root := evidenceRoot(t)
	_, _, err := resolveSelectorRunsDir(io.Discard, "", root, newPricer(), "label:BAD=x", loadForAggregation)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid --label")
}

func TestRunsDirScanError(t *testing.T) {
	_, _, err := resolveSelectorRunsDir(io.Discard, "", filepath.Join(t.TempDir(), "absent"), newPricer(), "label:variant=base", loadForAggregation)
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
	_, _, err := resolveSelectorRunsDir(io.Discard, "", root, newPricer(), "label:variant=base", loadForAggregation)
	require.Error(t, err)
}

func upsertBaselineRunsDir(t *testing.T, dbPath string, b model.Baseline) {
	t.Helper()
	s, err := store.OpenSQLite(dbPath)
	require.NoError(t, err)
	require.NoError(t, s.UpsertBaseline(b))
	require.NoError(t, s.Close())
}

func TestRunsDirNameSelectorResolvesOffline(t *testing.T) {
	root := evidenceRoot(t)
	dbPath := filepath.Join(t.TempDir(), "b.db")
	require.NoError(t, runBaselineSet(io.Discard, store.OpenSQLite, dbPath, "golden", []string{"variant=base"}, root))

	var out, errBuf bytes.Buffer
	code := run([]string{
		"regress", "--runs-dir", root, "--db", dbPath,
		"--baseline", "name:golden", "--candidate", "label:variant=cand",
	}, &out, &errBuf)
	assert.Equal(t, 0, code, errBuf.String())
	assert.Contains(t, out.String(), "baseline runs 2")
	assert.Empty(t, errBuf.String())
}

func TestRunsDirNameSelectorNotFound(t *testing.T) {
	root := evidenceRoot(t)
	dbPath := emptyStoreDB(t)
	var out, errBuf bytes.Buffer
	code := run([]string{
		"regress", "--runs-dir", root, "--db", dbPath,
		"--baseline", "name:nope", "--candidate", "label:variant=cand",
	}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), "baseline not found")
}

func TestRunsDirNameSelectorStoreMissing(t *testing.T) {
	root := evidenceRoot(t)
	missing := filepath.Join(t.TempDir(), "nope.db")
	var out, errBuf bytes.Buffer
	code := run([]string{
		"regress", "--runs-dir", root, "--db", missing,
		"--baseline", "name:golden", "--candidate", "label:variant=cand",
	}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), "no catacomb store")
}

func TestRunsDirNameSelectorGetBaselineError(t *testing.T) {
	root := evidenceRoot(t)
	dbPath := emptyStoreDB(t)
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO baselines(name, body) VALUES('golden','not-json')`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	var out, errBuf bytes.Buffer
	code := run([]string{
		"regress", "--runs-dir", root, "--db", dbPath,
		"--baseline", "name:golden", "--candidate", "label:variant=cand",
	}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), "get baseline")
}

func TestRunsDirNameSelectorMissingBaselinesTable(t *testing.T) {
	root := evidenceRoot(t)
	dbPath := seedCurrentVersionDropTable(t, "baselines")
	var out, errBuf bytes.Buffer
	code := run([]string{
		"regress", "--runs-dir", root, "--db", dbPath,
		"--baseline", "name:golden", "--candidate", "label:variant=cand",
	}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), "older than this binary")
}

func TestRunsDirNameSelectorEmptyRunIDs(t *testing.T) {
	root := evidenceRoot(t)
	dbPath := filepath.Join(t.TempDir(), "b.db")
	upsertBaselineRunsDir(t, dbPath, model.Baseline{Name: "empty", RunsDir: root, Stamps: currentStamps()})

	var out, errBuf bytes.Buffer
	code := run([]string{
		"regress", "--runs-dir", root, "--db", dbPath,
		"--baseline", "name:empty", "--candidate", "label:variant=cand",
	}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), "matched no runs")
}

func TestRunsDirNameSelectorMissingRunDir(t *testing.T) {
	root := evidenceRoot(t)
	dbPath := filepath.Join(t.TempDir(), "b.db")
	upsertBaselineRunsDir(t, dbPath, model.Baseline{
		Name: "golden", RunIDs: []string{"base-0", "ghost-99"}, RunsDir: root, Stamps: currentStamps(),
	})

	_, _, err := resolveSelectorRunsDir(io.Discard, dbPath, root, newPricer(), "name:golden", loadForAggregation)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ghost-99")
	assert.Contains(t, err.Error(), filepath.Join(root, "ghost-99"))
	var opErr *operationalError
	require.ErrorAs(t, err, &opErr)
}

func TestRunRegressRunsDirDeletedRunDirNamesRunAndDir(t *testing.T) {
	root := evidenceRoot(t)
	dbPath := filepath.Join(t.TempDir(), "b.db")
	require.NoError(t, runBaselineSet(io.Discard, store.OpenSQLite, dbPath, "golden", []string{"variant=base"}, root))
	require.NoError(t, os.RemoveAll(filepath.Join(root, "base-1")))

	var out, errBuf bytes.Buffer
	code := run([]string{
		"regress", "--runs-dir", root, "--db", dbPath,
		"--baseline", "name:golden", "--candidate", "label:variant=cand",
	}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), `run "base-1"`)
	assert.Contains(t, errBuf.String(), filepath.Join(root, "base-1"))
	assert.NotContains(t, errBuf.String(), "daemon")
}

func TestRunsDirNameSelectorBrokenEvidence(t *testing.T) {
	root := evidenceRoot(t)
	broken := evidence.Meta{RunID: "broken", SessionID: "s1", Labels: map[string]string{"variant": "base"}}
	require.NoError(t, evidence.Write(filepath.Join(root, "broken"), broken, nil))
	dbPath := filepath.Join(t.TempDir(), "b.db")
	upsertBaselineRunsDir(t, dbPath, model.Baseline{
		Name: "golden", RunIDs: []string{"broken"}, RunsDir: root, Stamps: currentStamps(),
	})

	_, _, err := resolveSelectorRunsDir(io.Discard, dbPath, root, newPricer(), "name:golden", loadForAggregation)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "broken")
	var opErr *operationalError
	require.ErrorAs(t, err, &opErr)
}

func TestRunsDirNameSelectorRunsDirPrecedenceWarns(t *testing.T) {
	root := evidenceRoot(t)
	dbPath := filepath.Join(t.TempDir(), "b.db")
	upsertBaselineRunsDir(t, dbPath, model.Baseline{
		Name: "golden", RunIDs: []string{"base-0", "base-1"}, RunsDir: "/recorded/elsewhere", Stamps: currentStamps(),
	})

	var out, errBuf bytes.Buffer
	code := run([]string{
		"regress", "--runs-dir", root, "--db", dbPath,
		"--baseline", "name:golden", "--candidate", "label:variant=cand",
	}, &out, &errBuf)
	assert.Equal(t, 0, code, errBuf.String())
	assert.Contains(t, errBuf.String(), "recorded runs-dir")
	assert.Contains(t, errBuf.String(), "/recorded/elsewhere")
	assert.Contains(t, errBuf.String(), fmt.Sprintf("%q", root))
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

func TestRunsDirAppliesEvidenceScores(t *testing.T) {
	root := t.TempDir()
	writeEvidenceRun(t, root, "base-0", "base", "session_marked.jsonl")
	require.NoError(t, os.WriteFile(filepath.Join(root, "base-0", "scores.jsonl"),
		[]byte(`{"key":"verifier.pass","value":1}`+"\n"), 0o600))
	base, _, err := resolveSelectorRunsDir(io.Discard, "", root, newPricer(), "label:variant=base", loadForAggregation)
	require.NoError(t, err)
	require.Len(t, base, 1)
	assert.InDelta(t, 1.0, base[0].Annotations["verifier.pass"], 1e-9)
}

func TestEvidenceRunGraphScoresParseError(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "r0")
	m := evidence.Meta{RunID: "r0", SessionID: "s1"}
	require.NoError(t, evidence.Write(dir, m, []evidence.SourceFile{{Src: filepath.Join("testdata", "session.jsonl"), Rel: "session.jsonl"}}))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "scores.jsonl"), []byte("{nope\n"), 0o600))
	_, err := evidenceRunGraph(dir, m, newPricer())
	require.Error(t, err)
	var opErr *operationalError
	require.ErrorAs(t, err, &opErr)
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

func TestRegressRunsDirRecordRequiresNameSelector(t *testing.T) {
	root := evidenceRoot(t)
	var out, errBuf bytes.Buffer
	code := run([]string{
		"regress", "--runs-dir", root, "--record",
		"--db", filepath.Join(t.TempDir(), "b.db"),
		"--baseline", "label:variant=base", "--candidate", "label:variant=cand",
	}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), "name:")
}

func TestRegressRunsDirRecordRoundtrip(t *testing.T) {
	root := evidenceRoot(t)
	dbPath := filepath.Join(t.TempDir(), "b.db")
	pinBaselineNow(t)
	require.NoError(t, runBaselineSet(io.Discard, store.OpenSQLite, dbPath, "golden", []string{"variant=base"}, root))

	var out, errBuf bytes.Buffer
	code := run([]string{
		"regress", "--runs-dir", root, "--db", dbPath, "--record",
		"--baseline", "name:golden", "--candidate", "label:variant=cand",
	}, &out, &errBuf)
	require.Equal(t, 0, code, errBuf.String())

	s, err := store.OpenSQLiteReadOnly(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	res, err := s.RegressResultsFor("golden")
	require.NoError(t, err)
	require.Len(t, res, 1)
	var rec regress.Record
	require.NoError(t, json.Unmarshal(res[0].Body, &rec))
	assert.Equal(t, model.Stamps{CatacombVersion: "dev", StepKeyScheme: "stepkey/v1"}, rec.Stamps)
	assert.Equal(t, "label:variant=cand", rec.CandidateSelector)
	assert.Equal(t, regress.RecordVersion, rec.V)
	assert.Empty(t, rec.Project)
	assert.NotContains(t, string(res[0].Body), `"project"`)
}

func TestRegressRunsDirRecordProjectRoundtrip(t *testing.T) {
	root := evidenceRoot(t)
	dbPath := filepath.Join(t.TempDir(), "b.db")
	pinBaselineNow(t)
	require.NoError(t, runBaselineSet(io.Discard, store.OpenSQLite, dbPath, "golden", []string{"variant=base"}, root))

	var out, errBuf bytes.Buffer
	code := run([]string{
		"regress", "--runs-dir", root, "--db", dbPath, "--record", "--project", "payments-api",
		"--baseline", "name:golden", "--candidate", "label:variant=cand",
	}, &out, &errBuf)
	require.Equal(t, 0, code, errBuf.String())

	s, err := store.OpenSQLiteReadOnly(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	res, err := s.RegressResultsFor("golden")
	require.NoError(t, err)
	require.Len(t, res, 1)
	var rec regress.Record
	require.NoError(t, json.Unmarshal(res[0].Body, &rec))
	assert.Equal(t, "payments-api", rec.Project)
	assert.Equal(t, regress.RecordVersion, rec.V)
	assert.Contains(t, string(res[0].Body), `"project":"payments-api"`)
}

func TestRegressRunsDirRecordAppendError(t *testing.T) {
	root := evidenceRoot(t)
	dbPath := filepath.Join(t.TempDir(), "b.db")
	require.NoError(t, runBaselineSet(io.Discard, store.OpenSQLite, dbPath, "golden", []string{"variant=base"}, root))
	opener := func(path string) (store.Store, error) {
		s, err := store.OpenSQLite(path)
		if err != nil {
			return nil, err
		}
		return &appendErrStore{Store: s}, nil
	}

	f := regressFlags{
		runsDir: root, dbPath: dbPath,
		baseline: "name:golden", candidate: "label:variant=cand",
		record: true, thresholds: regress.DefaultThresholds(),
	}
	err := runRegress(io.Discard, io.Discard, opener, newPricer, f)
	require.Error(t, err)
	var opErr *operationalError
	require.ErrorAs(t, err, &opErr)
	assert.Contains(t, err.Error(), "boom-append")
}

func TestAppendRecordOfflineOpenError(t *testing.T) {
	failing := func(string) (store.Store, error) { return nil, errors.New("boom-open") }
	err := appendRecordOffline(failing, regressFlags{dbPath: "x"}, "golden", time.Now(), nil, regress.Report{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom-open")
}

func TestRegressRunsDirNameStampsZeroWarns(t *testing.T) {
	root := evidenceRoot(t)
	dbPath := filepath.Join(t.TempDir(), "b.db")
	upsertBaselineRunsDir(t, dbPath, model.Baseline{Name: "golden", RunIDs: []string{"base-0", "base-1"}, RunsDir: root})

	var out, errBuf bytes.Buffer
	code := run([]string{
		"regress", "--runs-dir", root, "--db", dbPath,
		"--baseline", "name:golden", "--candidate", "label:variant=cand",
	}, &out, &errBuf)
	assert.Equal(t, 0, code, errBuf.String())
	assert.Contains(t, errBuf.String(), "baseline golden has no version stamps (pre-PV-2)")
}

func TestRegressRunsDirNameStampsZeroStrictRefuses(t *testing.T) {
	root := evidenceRoot(t)
	dbPath := filepath.Join(t.TempDir(), "b.db")
	upsertBaselineRunsDir(t, dbPath, model.Baseline{Name: "golden", RunIDs: []string{"base-0", "base-1"}, RunsDir: root})

	var out, errBuf bytes.Buffer
	code := run([]string{
		"regress", "--runs-dir", root, "--db", dbPath, "--strict",
		"--baseline", "name:golden", "--candidate", "label:variant=cand",
	}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), "no version stamps")
}

func TestRegressRunsDirNameStampsMismatchWarns(t *testing.T) {
	root := evidenceRoot(t)
	dbPath := filepath.Join(t.TempDir(), "b.db")
	upsertBaselineRunsDir(t, dbPath, model.Baseline{
		Name: "golden", RunIDs: []string{"base-0", "base-1"}, RunsDir: root,
		Stamps: model.Stamps{CatacombVersion: "old", StepKeyScheme: "stepkey/v0"},
	})

	var out, errBuf bytes.Buffer
	code := run([]string{
		"regress", "--runs-dir", root, "--db", dbPath,
		"--baseline", "name:golden", "--candidate", "label:variant=cand",
	}, &out, &errBuf)
	assert.Equal(t, 0, code, errBuf.String())
	assert.Contains(t, errBuf.String(), "version stamps differ")
	assert.Contains(t, errBuf.String(), "old")
	assert.Contains(t, errBuf.String(), "dev")
}

func TestRegressRunsDirNameStampsMismatchStrictRefuses(t *testing.T) {
	root := evidenceRoot(t)
	dbPath := filepath.Join(t.TempDir(), "b.db")
	upsertBaselineRunsDir(t, dbPath, model.Baseline{
		Name: "golden", RunIDs: []string{"base-0", "base-1"}, RunsDir: root,
		Stamps: model.Stamps{CatacombVersion: "old", StepKeyScheme: "stepkey/v1"},
	})

	var out, errBuf bytes.Buffer
	code := run([]string{
		"regress", "--runs-dir", root, "--db", dbPath, "--strict",
		"--baseline", "name:golden", "--candidate", "label:variant=cand",
	}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), "version stamps differ")
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

type failWriter struct{}

func (failWriter) Write(_ []byte) (int, error) {
	return 0, errors.New("write failed")
}

func countHeavyNodeData(group []aggregate.RunGraph) (payloads, attrs, sources int) {
	for _, rg := range group {
		for _, n := range rg.Nodes {
			if n.Payload != nil {
				payloads++
			}
			if n.Attrs != nil {
				attrs++
			}
			if n.Sources != nil {
				sources++
			}
		}
	}
	return payloads, attrs, sources
}

func TestResolveSelectorRunsDirForAggregationStripsHeavyNodeData(t *testing.T) {
	root := evidenceRoot(t)
	full, _, err := resolveSelectorRunsDir(io.Discard, "", root, newPricer(), "label:variant=base", loadFullGraphs)
	require.NoError(t, err)
	require.NotEmpty(t, full)
	payloads, attrs, sources := countHeavyNodeData(full)
	require.Positive(t, payloads)
	require.Positive(t, attrs)
	require.Positive(t, sources)

	stripped, _, err := resolveSelectorRunsDir(io.Discard, "", root, newPricer(), "label:variant=base", loadForAggregation)
	require.NoError(t, err)
	require.Len(t, stripped, len(full))
	payloads, attrs, sources = countHeavyNodeData(stripped)
	assert.Zero(t, payloads)
	assert.Zero(t, attrs)
	assert.Zero(t, sources)
}

func TestRunGroupFromDirsForAggregationStripsHeavyNodeData(t *testing.T) {
	root := evidenceRoot(t)
	ids := []string{"base-0", "base-1"}
	full, err := runGroupFromDirs(root, "golden", ids, newPricer(), loadFullGraphs)
	require.NoError(t, err)
	payloads, _, _ := countHeavyNodeData(full)
	require.Positive(t, payloads)

	stripped, err := runGroupFromDirs(root, "golden", ids, newPricer(), loadForAggregation)
	require.NoError(t, err)
	require.Len(t, stripped, len(full))
	payloads, attrs, sources := countHeavyNodeData(stripped)
	assert.Zero(t, payloads)
	assert.Zero(t, attrs)
	assert.Zero(t, sources)
}

func TestRegressReportUnchangedByAggregationStrip(t *testing.T) {
	root := evidenceRoot(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "base-0", "scores.jsonl"),
		[]byte(`{"key":"verifier.pass","value":1}`+"\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "cand-0", "scores.jsonl"),
		[]byte(`{"key":"verifier.pass","value":0}`+"\n"), 0o600))
	reportJSON := func(mode runGraphLoadMode) string {
		base, _, err := resolveSelectorRunsDir(io.Discard, "", root, newPricer(), "label:variant=base", mode)
		require.NoError(t, err)
		cand, _, err := resolveSelectorRunsDir(io.Discard, "", root, newPricer(), "label:variant=cand", mode)
		require.NoError(t, err)
		opts := aggregate.Options{AnnotationKeys: []string{"verifier.pass"}}
		rep := regress.Compare(regress.Input{
			Baseline:       aggregate.Aggregate(base, opts),
			Candidate:      aggregate.Aggregate(cand, opts),
			Annotations:    []regress.AnnotationSpec{{Key: "verifier.pass", HigherBetter: true}},
			BaselineCells:  aggregate.Cells(base),
			CandidateCells: aggregate.Cells(cand),
		}, regress.DefaultThresholds())
		b, merr := json.Marshal(rep)
		require.NoError(t, merr)
		return string(b)
	}
	assert.Equal(t, reportJSON(loadFullGraphs), reportJSON(loadForAggregation))
}
