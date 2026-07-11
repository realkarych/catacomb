package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/aggregate"
	"github.com/realkarych/catacomb/model"
)

func writeScoresFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "scores.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestScoresLoadValid(t *testing.T) {
	path := writeScoresFile(t, "\n"+
		`{"step_key":"sk1","key":"owner.quality","value":1}`+"\n"+
		"   \n"+
		`{"step_key":"sk2","key":"owner.quality","value":0.5,"run_id":"r1"}`+"\n")
	entries, err := loadScores(path)
	require.NoError(t, err)
	assert.Equal(t, []scoreEntry{
		{StepKey: "sk1", Key: "owner.quality", Value: 1},
		{StepKey: "sk2", Key: "owner.quality", Value: 0.5, RunID: "r1"},
	}, entries)
}

func TestScoresLoadErrors(t *testing.T) {
	valid := `{"step_key":"sk","key":"owner.quality","value":1}`
	cases := []struct {
		name string
		body string
		want []string
	}{
		{"malformed json", valid + "\n{nope", []string{"line 2"}},
		{"bad key form no dot", `{"step_key":"sk","key":"nodot","value":1}`, []string{"line 1", "owner.key"}},
		{"bad key form double dot", `{"step_key":"sk","key":"a.b.c","value":1}`, []string{"line 1", "owner.key"}},
		{"missing key", `{"step_key":"sk","value":1}`, []string{"line 1", "owner.key"}},
		{"run-level without run_id", "\n\n" + `{"key":"owner.quality","value":1}`, []string{"line 3", "run_id"}},
		{"missing value", `{"step_key":"sk","key":"owner.quality"}`, []string{"line 1", "value"}},
		{"non-numeric value", `{"step_key":"sk","key":"owner.quality","value":"high"}`, []string{"line 1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeScoresFile(t, tc.body)
			_, err := loadScores(path)
			require.Error(t, err)
			assert.Contains(t, err.Error(), path)
			for _, w := range tc.want {
				assert.Contains(t, err.Error(), w)
			}
		})
	}
}

func TestScoresLoadFileAbsent(t *testing.T) {
	_, err := loadScores(filepath.Join(t.TempDir(), "absent.jsonl"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scores")
}

func TestParseScoreLineRunLevel(t *testing.T) {
	e, err := parseScoreLine(`{"key":"verifier.pass","value":1,"run_id":"r1"}`)
	require.NoError(t, err)
	assert.Equal(t, scoreEntry{Key: "verifier.pass", Value: 1, RunID: "r1"}, e)
}

func TestParseScoreLineToleratesProvenanceFields(t *testing.T) {
	e, err := parseScoreLine(`{"key":"judge.groundedness","value":0.8,"run_id":"r1","tool":"deepeval","tool_version":"3.1","prompt_hash":"abc"}`)
	require.NoError(t, err)
	assert.InDelta(t, 0.8, e.Value, 1e-9)
}

func TestLoadScoresRunLevelRequiresRunID(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	require.NoError(t, os.WriteFile(p, []byte(`{"key":"verifier.pass","value":1}`+"\n"), 0o600))
	_, err := loadScores(p)
	require.ErrorContains(t, err, `run-level score requires "run_id"`)
}

func TestRunRegressScoresMissingFileNamesPath(t *testing.T) {
	root := evidenceRoot(t)
	missing := filepath.Join(t.TempDir(), "typo.jsonl")
	var out, errBuf bytes.Buffer
	code := run([]string{
		"regress", "--runs-dir", root,
		"--baseline", "label:variant=base", "--candidate", "label:variant=cand",
		"--scores", missing,
	}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), missing)
	assert.NotContains(t, errBuf.String(), "daemon")
}

func scoreGraph(runID string, stepKeys ...string) aggregate.RunGraph {
	nodes := make([]*model.Node, 0, len(stepKeys))
	for i, k := range stepKeys {
		nodes = append(nodes, &model.Node{ID: fmt.Sprintf("%s-n%d", runID, i), StepKey: k})
	}
	return aggregate.RunGraph{Run: model.Run{ID: runID}, Nodes: nodes}
}

func TestScoresApplyMatrix(t *testing.T) {
	base := []aggregate.RunGraph{scoreGraph("b0", "sk1", "sk2"), scoreGraph("b1", "sk1")}
	cand := []aggregate.RunGraph{scoreGraph("c0", "sk1")}
	entries := []scoreEntry{
		{StepKey: "sk1", Key: "owner.q", Value: 1},
		{StepKey: "sk2", Key: "owner.q", Value: 0.25, RunID: "b0"},
		{StepKey: "sk2", Key: "owner.q", Value: 0.5, RunID: "c0"},
		{StepKey: "ghost", Key: "owner.q", Value: 1},
	}
	applied, unmatched, _ := applyScores([][]aggregate.RunGraph{base, cand}, entries)
	assert.Equal(t, 4, applied)
	assert.Equal(t, 2, unmatched)
	assert.Equal(t, float64(1), base[0].Nodes[0].Annotations["owner.q"])
	assert.Equal(t, 0.25, base[0].Nodes[1].Annotations["owner.q"])
	assert.Equal(t, float64(1), base[1].Nodes[0].Annotations["owner.q"])
	assert.Equal(t, float64(1), cand[0].Nodes[0].Annotations["owner.q"])
}

func TestScoresApplyRunScopedSkipsOtherRuns(t *testing.T) {
	base := []aggregate.RunGraph{scoreGraph("b0", "sk1"), scoreGraph("b1", "sk1")}
	applied, unmatched, _ := applyScores([][]aggregate.RunGraph{base}, []scoreEntry{
		{StepKey: "sk1", Key: "owner.q", Value: 0.75, RunID: "b1"},
	})
	assert.Equal(t, 1, applied)
	assert.Equal(t, 0, unmatched)
	assert.Nil(t, base[0].Nodes[0].Annotations)
	assert.Equal(t, 0.75, base[1].Nodes[0].Annotations["owner.q"])
}

func TestScoresApplyPreservesExistingAnnotations(t *testing.T) {
	g := scoreGraph("r0", "sk1")
	g.Nodes[0].Annotations = map[string]any{"other.metric": 2.0}
	applied, unmatched, _ := applyScores([][]aggregate.RunGraph{{g}}, []scoreEntry{
		{StepKey: "sk1", Key: "owner.q", Value: 1},
	})
	assert.Equal(t, 1, applied)
	assert.Equal(t, 0, unmatched)
	assert.Equal(t, 2.0, g.Nodes[0].Annotations["other.metric"])
	assert.Equal(t, float64(1), g.Nodes[0].Annotations["owner.q"])
}

func TestScoresApplyEmptyEntries(t *testing.T) {
	applied, unmatched, _ := applyScores([][]aggregate.RunGraph{{scoreGraph("r0", "sk1")}}, nil)
	assert.Zero(t, applied)
	assert.Zero(t, unmatched)
}

func TestScoresFileFlagEmptyNoop(t *testing.T) {
	var errBuf bytes.Buffer
	require.NoError(t, applyScoresFile(&errBuf, "", nil, nil))
	assert.Empty(t, errBuf.String())
}

func TestScoresFileLoadErrorOperational(t *testing.T) {
	err := applyScoresFile(io.Discard, filepath.Join(t.TempDir(), "absent.jsonl"), nil, nil)
	require.Error(t, err)
	var opErr *operationalError
	require.ErrorAs(t, err, &opErr)
}

func TestScoresFileUnmatchedWarnsOnce(t *testing.T) {
	path := writeScoresFile(t, `{"step_key":"sk1","key":"owner.q","value":1}`+"\n"+
		`{"step_key":"ghost","key":"owner.q","value":1}`+"\n"+
		`{"step_key":"ghost2","key":"owner.q","value":1}`)
	base := []aggregate.RunGraph{scoreGraph("b0", "sk1")}
	var errBuf bytes.Buffer
	require.NoError(t, applyScoresFile(&errBuf, path, base, nil))
	assert.Equal(t, 1, strings.Count(errBuf.String(), "warning:"))
	assert.Contains(t, errBuf.String(), "2 score entries matched no node")
	assert.Contains(t, errBuf.String(), "1 value")
}

func TestScoresFileAllMatchedNoWarning(t *testing.T) {
	path := writeScoresFile(t, `{"step_key":"sk1","key":"owner.q","value":1}`)
	base := []aggregate.RunGraph{scoreGraph("b0", "sk1")}
	cand := []aggregate.RunGraph{scoreGraph("c0", "sk1")}
	var errBuf bytes.Buffer
	require.NoError(t, applyScoresFile(&errBuf, path, base, cand))
	assert.Empty(t, errBuf.String())
	assert.Equal(t, float64(1), base[0].Nodes[0].Annotations["owner.q"])
	assert.Equal(t, float64(1), cand[0].Nodes[0].Annotations["owner.q"])
}

func TestApplyEntriesToRunGraphRunLevel(t *testing.T) {
	rg := aggregate.RunGraph{Run: model.Run{ID: "r1"}}
	applied, unmatched := applyEntriesToRunGraph(&rg, []scoreEntry{
		{Key: "verifier.pass", Value: 1, RunID: "r1"},
		{Key: "verifier.pass", Value: 0, RunID: "other"},
	})
	assert.Equal(t, 1, applied)
	assert.Equal(t, 1, unmatched)
	assert.Equal(t, map[string]float64{"verifier.pass": 1}, rg.Annotations)
}

func TestLoadEvidenceScoresFillsRunID(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "scores.jsonl"),
		[]byte(`{"key":"verifier.pass","value":1}`+"\n"), 0o600))
	entries, err := loadEvidenceScores(dir, "r9")
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "r9", entries[0].RunID)
}

func TestLoadEvidenceScoresMissingFile(t *testing.T) {
	entries, err := loadEvidenceScores(t.TempDir(), "r9")
	require.NoError(t, err)
	assert.Nil(t, entries)
}

func TestLoadEvidenceScoresParseError(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "scores.jsonl"), []byte("{bad\n"), 0o600))
	_, err := loadEvidenceScores(dir, "r9")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "line 1")
}

func TestLoadEvidenceScoresReadError(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "scores.jsonl"), 0o700))
	_, err := loadEvidenceScores(dir, "r9")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scores")
}

func TestApplyScoresExternalOverridesEvidence(t *testing.T) {
	groups := [][]aggregate.RunGraph{{{Run: model.Run{ID: "r1"}, Annotations: map[string]float64{"verifier.pass": 1}}}}
	applied, unmatched, overridden := applyScores(groups, []scoreEntry{{Key: "verifier.pass", Value: 0, RunID: "r1"}})
	assert.Equal(t, 1, applied)
	assert.Equal(t, 0, unmatched)
	assert.Equal(t, 1, overridden)
	assert.InDelta(t, 0.0, groups[0][0].Annotations["verifier.pass"], 1e-9)
}

func TestApplyScoresExternalOverridesNodeAnnotation(t *testing.T) {
	g := scoreGraph("r1", "sk1")
	g.Nodes[0].Annotations = map[string]any{"owner.q": 0.5}
	applied, unmatched, overridden := applyScores([][]aggregate.RunGraph{{g}}, []scoreEntry{
		{StepKey: "sk1", Key: "owner.q", Value: 1},
	})
	assert.Equal(t, 1, applied)
	assert.Equal(t, 0, unmatched)
	assert.Equal(t, 1, overridden)
	assert.Equal(t, float64(1), g.Nodes[0].Annotations["owner.q"])
}

func TestScoresFileOverrideWarns(t *testing.T) {
	path := writeScoresFile(t, `{"key":"verifier.pass","value":0,"run_id":"b0"}`)
	base := []aggregate.RunGraph{{Run: model.Run{ID: "b0"}, Annotations: map[string]float64{"verifier.pass": 1}}}
	var errBuf bytes.Buffer
	require.NoError(t, applyScoresFile(&errBuf, path, base, nil))
	assert.Contains(t, errBuf.String(), "1 entries overrode evidence-provided values")
}
