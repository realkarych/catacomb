package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	catdiff "github.com/realkarych/catacomb/diff"
	"github.com/realkarych/catacomb/subgraph"
)

func TestRunDiffIdentical(t *testing.T) {
	result, err := runDiff(diffArgs{a: "testdata/session.jsonl", b: "testdata/session.jsonl"})
	require.NoError(t, err)
	assert.NotEmpty(t, result.Unchanged)
	assert.Empty(t, result.Added)
	assert.Empty(t, result.Removed)
}

func TestRunDiffAddedFixture(t *testing.T) {
	result, err := runDiff(diffArgs{a: "testdata/session.jsonl", b: "testdata/session_added.jsonl"})
	require.NoError(t, err)
	assert.NotEmpty(t, result.Added)
}

func TestDiffCommandHuman(t *testing.T) {
	root := newRootCmd()
	var sb strings.Builder
	root.SetOut(&sb)
	root.SetArgs([]string{"diff", "testdata/session.jsonl", "testdata/session.jsonl"})
	require.NoError(t, root.Execute())
	assert.Contains(t, sb.String(), "unchanged:")
}

func TestDiffCommandJSON(t *testing.T) {
	root := newRootCmd()
	var sb strings.Builder
	root.SetOut(&sb)
	root.SetArgs([]string{"diff", "--json", "testdata/session.jsonl", "testdata/session.jsonl"})
	require.NoError(t, root.Execute())
	var result catdiff.DiffResult
	require.NoError(t, json.Unmarshal([]byte(sb.String()), &result))
	assert.NotEmpty(t, result.Unchanged)
}

func TestRunDiffMissingA(t *testing.T) {
	_, err := runDiff(diffArgs{a: filepath.Join(t.TempDir(), "nope.jsonl"), b: "testdata/session.jsonl"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrDiffInput))
}

func TestRunDiffMissingB(t *testing.T) {
	_, err := runDiff(diffArgs{a: "testdata/session.jsonl", b: filepath.Join(t.TempDir(), "nope.jsonl")})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrDiffInput))
}

func TestRunDiffMalformedB(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.jsonl")
	require.NoError(t, os.WriteFile(bad, []byte("{not json}\n"), 0o644))
	_, err := runDiff(diffArgs{a: "testdata/session.jsonl", b: bad})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrDiffInput))
}

func TestDiffCommandErrorPropagated(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"diff", filepath.Join(t.TempDir(), "nope.jsonl"), "testdata/session.jsonl"})
	err := root.Execute()
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrDiffInput))
}

func TestDiffWarnsOnUnknownRecords(t *testing.T) {
	buf := captureDriftOut(t)
	drifty := writeDriftyCopy(t, filepath.Join("testdata", "session.jsonl"))
	root := newRootCmd()
	root.SetOut(&strings.Builder{})
	root.SetArgs([]string{"diff", drifty, "testdata/session.jsonl"})
	require.NoError(t, root.Execute())
	assert.Contains(t, buf.String(), "unrecognized transcript record")
}

func TestDiffMissingInputIsOperational(t *testing.T) {
	var out, errBuf bytes.Buffer
	code := run([]string{"diff", filepath.Join(t.TempDir(), "nope.jsonl"), "testdata/session.jsonl"}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.NotEmpty(t, errBuf.String())
}

func TestDiffMalformedInputIsOperational(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "bad.jsonl")
	require.NoError(t, os.WriteFile(bad, []byte("{not json}\n"), 0o600))
	var out, errBuf bytes.Buffer
	code := run([]string{"diff", "testdata/session.jsonl", bad}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.NotEmpty(t, errBuf.String())
}

func TestDiffCommandShowsAddedRemoved(t *testing.T) {
	root := newRootCmd()
	var sb strings.Builder
	root.SetOut(&sb)
	root.SetArgs([]string{"diff", "testdata/session.jsonl", "testdata/session_added.jsonl"})
	require.NoError(t, root.Execute())
	assert.Contains(t, sb.String(), "+")
}

func TestRenderDiffRemoved(t *testing.T) {
	result, err := runDiff(diffArgs{a: "testdata/session_added.jsonl", b: "testdata/session.jsonl"})
	require.NoError(t, err)
	cmd := newDiffCmd()
	var sb strings.Builder
	cmd.SetOut(&sb)
	renderDiff(cmd, result)
	assert.Contains(t, sb.String(), "-")
}

func TestRenderDiffChanged(t *testing.T) {
	cost := 0.05
	result := catdiff.DiffResult{
		Added:   make([]catdiff.Step, 0),
		Removed: make([]catdiff.Step, 0),
		Changed: []catdiff.ChangedStep{
			{
				Match: catdiff.Match{Type: "tool_call", Tool: "Bash", Tier: "step_key"},
				Deltas: catdiff.Deltas{
					CostUSD: &catdiff.FloatChange{Before: 0, After: cost, Delta: cost},
				},
			},
		},
		Unchanged: make([]catdiff.Match, 0),
	}
	cmd := newDiffCmd()
	var sb strings.Builder
	cmd.SetOut(&sb)
	renderDiff(cmd, result)
	assert.Contains(t, sb.String(), "~")
	assert.Contains(t, sb.String(), "cost")
}

func TestRunDiffPhaseScopeReducesSet(t *testing.T) {
	whole, err := runDiff(diffArgs{a: "testdata/session_marked.jsonl", b: "testdata/session_marked.jsonl"})
	require.NoError(t, err)
	scoped, err := runDiff(diffArgs{a: "testdata/session_marked.jsonl", b: "testdata/session_marked.jsonl", phase: "plan"})
	require.NoError(t, err)
	assert.Len(t, whole.Unchanged, 3)
	assert.Len(t, scoped.Unchanged, 1)
}

func TestRunDiffAPhaseOnly(t *testing.T) {
	result, err := runDiff(diffArgs{a: "testdata/session_marked.jsonl", b: "testdata/session_marked.jsonl", aPhase: "plan"})
	require.NoError(t, err)
	assert.Len(t, result.Unchanged, 1)
	assert.Len(t, result.Added, 2)
}

func TestRunDiffBPhaseNotFound(t *testing.T) {
	_, err := runDiff(diffArgs{a: "testdata/session_marked.jsonl", b: "testdata/session_marked.jsonl", bPhase: "ghost"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestRunDiffInvalidSelector(t *testing.T) {
	_, err := runDiff(diffArgs{a: "testdata/session_marked.jsonl", b: "testdata/session_marked.jsonl", phase: "plan,x"})
	assert.ErrorIs(t, err, subgraph.ErrInvalidSelector)
}

func TestDiffCommandPhaseFlag(t *testing.T) {
	root := newRootCmd()
	var sb strings.Builder
	root.SetOut(&sb)
	root.SetArgs([]string{"diff", "--phase", "plan", "testdata/session_marked.jsonl", "testdata/session_marked.jsonl"})
	require.NoError(t, root.Execute())
	assert.Contains(t, sb.String(), "unchanged: 1")
}

func TestRunDiffRange(t *testing.T) {
	result, err := runDiff(diffArgs{
		a: "testdata/session_marked.jsonl", b: "testdata/session_marked.jsonl",
		aFrom: "plan", aTo: "plan", bFrom: "plan", bTo: "plan",
	})
	require.NoError(t, err)
	assert.Empty(t, result.Added)
	assert.Empty(t, result.Removed)
	assert.Empty(t, result.Changed)
}

func TestRunDiffRangeRequiresBoth(t *testing.T) {
	_, err := runDiff(diffArgs{
		a: "testdata/session_marked.jsonl", b: "testdata/session_marked.jsonl",
		aFrom: "plan",
	})
	assert.ErrorIs(t, err, subgraph.ErrInvalidSelector)
}

func TestSummarizeDeltas(t *testing.T) {
	d := catdiff.Deltas{}
	assert.Equal(t, "", summarizeDeltas(d))

	cost := 0.05
	dur := int64(100)
	tokIn := int64(10)
	tokOut := int64(5)
	d2 := catdiff.Deltas{
		Args:       &catdiff.StringChange{Before: "a", After: "b"},
		Status:     &catdiff.StringChange{Before: "ok", After: "error"},
		CostUSD:    &catdiff.FloatChange{Before: 0, After: cost, Delta: cost},
		DurationMS: &catdiff.IntChange{Before: 0, After: dur, Delta: dur},
		TokensIn:   &catdiff.IntChange{Before: 0, After: tokIn, Delta: tokIn},
		TokensOut:  &catdiff.IntChange{Before: 0, After: tokOut, Delta: tokOut},
	}
	r := summarizeDeltas(d2)
	assert.Contains(t, r, "args")
	assert.Contains(t, r, "status")
	assert.Contains(t, r, "cost")
	assert.Contains(t, r, "duration")
	assert.Contains(t, r, "tokens_in")
	assert.Contains(t, r, "tokens_out")
}
