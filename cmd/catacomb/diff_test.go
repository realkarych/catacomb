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

func diffCounts(r catdiff.DiffResult) [4]int {
	return [4]int{len(r.Unchanged), len(r.Changed), len(r.Added), len(r.Removed)}
}

func TestRunDiffCountsAreAsymmetricInTheDirectionOfTheChange(t *testing.T) {
	const (
		base  = "testdata/session.jsonl"
		extra = "testdata/session_added.jsonl"
	)
	cases := []struct {
		name       string
		a, b       string
		want       [4]int
		wantTools  []string
		wantAddedT []string
	}{
		{name: "a file against itself has no change at all", a: base, b: base, want: [4]int{2, 0, 0, 0}},
		{name: "b has one step a lacks: added", a: base, b: extra, want: [4]int{2, 0, 1, 0}, wantAddedT: []string{"Bash"}},
		{name: "a has one step b lacks: removed", a: extra, b: base, want: [4]int{2, 0, 0, 1}, wantTools: []string{"Bash"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := runDiff(diffArgs{a: tc.a, b: tc.b})
			require.NoError(t, err)
			assert.Equal(t, tc.want, diffCounts(result), "unchanged/changed/added/removed")
			added := make([]string, 0, len(result.Added))
			for _, s := range result.Added {
				added = append(added, s.Tool)
			}
			removed := make([]string, 0, len(result.Removed))
			for _, s := range result.Removed {
				removed = append(removed, s.Tool)
			}
			assert.Equal(t, tc.wantAddedT, nilIfEmpty(added))
			assert.Equal(t, tc.wantTools, nilIfEmpty(removed))
		})
	}
}

func nilIfEmpty(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	return s
}

func TestDiffCommandHumanRendersHeadlineThenAddedLines(t *testing.T) {
	root := newRootCmd()
	var sb strings.Builder
	root.SetOut(&sb)
	root.SetArgs([]string{"diff", "testdata/session.jsonl", "testdata/session_added.jsonl"})
	require.NoError(t, root.Execute())
	assert.Equal(t, "unchanged: 2  changed: 0  added: 1  removed: 0\n+ tool_call Bash\n", sb.String())
}

func TestDiffCommandHumanRendersRemovedLinesForTheReverseDirection(t *testing.T) {
	root := newRootCmd()
	var sb strings.Builder
	root.SetOut(&sb)
	root.SetArgs([]string{"diff", "testdata/session_added.jsonl", "testdata/session.jsonl"})
	require.NoError(t, root.Execute())
	assert.Equal(t, "unchanged: 2  changed: 0  added: 0  removed: 1\n- tool_call Bash\n", sb.String())
}

func writeTwoToolTranscript(t *testing.T, secondTool string, input map[string]string) string {
	t.Helper()
	arg, err := json.Marshal(input)
	require.NoError(t, err)
	lines := `{"type":"user","uuid":"u1","sessionId":"s1","timestamp":"2026-06-20T10:00:00Z","message":{"role":"user","content":"go"}}
{"type":"assistant","uuid":"a1","parentUuid":"u1","sessionId":"s1","timestamp":"2026-06-20T10:00:01Z","message":{"role":"assistant","id":"msg_1","model":"m","content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"ls"}}]}}
{"type":"user","uuid":"u2","parentUuid":"a1","sessionId":"s1","timestamp":"2026-06-20T10:00:02Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"ok","is_error":false}]}}
{"type":"assistant","uuid":"a2","parentUuid":"u2","sessionId":"s1","timestamp":"2026-06-20T10:00:03Z","message":{"role":"assistant","id":"msg_2","model":"m","content":[{"type":"tool_use","id":"toolu_2","name":"` + secondTool + `","input":` + string(arg) + `}]}}
{"type":"user","uuid":"u3","parentUuid":"a2","sessionId":"s1","timestamp":"2026-06-20T10:00:04Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_2","content":"ok","is_error":false}]}}
`
	path := filepath.Join(t.TempDir(), "two-tool.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(lines), 0o600))
	return path
}

func TestDiffCommandHumanRendersAddedBeforeRemovedWhenBothArePresent(t *testing.T) {
	a := writeTwoToolTranscript(t, "Read", map[string]string{"file_path": "/x"})
	b := writeTwoToolTranscript(t, "Grep", map[string]string{"pattern": "y"})

	root := newRootCmd()
	var sb strings.Builder
	root.SetOut(&sb)
	root.SetArgs([]string{"diff", a, b})
	require.NoError(t, root.Execute())
	assert.Equal(t,
		"unchanged: 1  changed: 0  added: 1  removed: 1\n+ tool_call Grep\n- tool_call Read\n",
		sb.String())
}

func TestDiffCommandJSONCarriesTheSameCountsAsTheHumanRender(t *testing.T) {
	root := newRootCmd()
	var sb strings.Builder
	root.SetOut(&sb)
	root.SetArgs([]string{"diff", "--json", "testdata/session.jsonl", "testdata/session_added.jsonl"})
	require.NoError(t, root.Execute())
	var result catdiff.DiffResult
	require.NoError(t, json.Unmarshal([]byte(sb.String()), &result))
	assert.Equal(t, [4]int{2, 0, 1, 0}, diffCounts(result))
	require.Len(t, result.Added, 1)
	assert.Equal(t, "Bash", result.Added[0].Tool)
	assert.Equal(t, "tool_call", result.Added[0].Type)
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
	assert.Equal(t, "unchanged: 0  changed: 1  added: 0  removed: 0\n~ tool_call Bash cost\n", sb.String())
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

func TestSummarizeDeltasEmitsAFixedFieldOrderIndependentOfWhichFieldsChanged(t *testing.T) {
	args := &catdiff.StringChange{Before: "a", After: "b"}
	status := &catdiff.StringChange{Before: "ok", After: "error"}
	cost := &catdiff.FloatChange{Before: 0, After: 0.05, Delta: 0.05}
	dur := &catdiff.IntChange{Before: 0, After: 100, Delta: 100}
	tokIn := &catdiff.IntChange{Before: 0, After: 10, Delta: 10}
	tokOut := &catdiff.IntChange{Before: 0, After: 5, Delta: 5}

	cases := []struct {
		name string
		in   catdiff.Deltas
		want string
	}{
		{name: "no deltas", in: catdiff.Deltas{}, want: ""},
		{name: "single delta carries no separator", in: catdiff.Deltas{CostUSD: cost}, want: "cost"},
		{
			name: "sparse deltas keep the declared order",
			in:   catdiff.Deltas{Status: status, TokensOut: tokOut},
			want: "status,tokens_out",
		},
		{
			name: "every delta renders in the declared order",
			in: catdiff.Deltas{
				Args: args, Status: status, CostUSD: cost,
				DurationMS: dur, TokensIn: tokIn, TokensOut: tokOut,
			},
			want: "args,status,cost,duration,tokens_in,tokens_out",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, summarizeDeltas(tc.in))
		})
	}
}
