package jsonl

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/reduce"
)

type errReader struct{ read bool }

func (r *errReader) Read(p []byte) (int, error) {
	if r.read {
		return 0, errors.New("read fail")
	}
	r.read = true
	p[0] = '\n'
	return 1, nil
}

func parseFixture(t *testing.T) []model.Observation {
	t.Helper()
	f, err := os.Open("testdata/session.jsonl")
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })
	obs, err := ParseReader(f, "exec-T")
	require.NoError(t, err)
	return obs
}

func byKind(obs []model.Observation, kind string) []model.Observation {
	var out []model.Observation
	for _, o := range obs {
		if o.Kind == kind {
			out = append(out, o)
		}
	}
	return out
}

func TestParseReaderShapes(t *testing.T) {
	obs := parseFixture(t)

	require.Len(t, byKind(obs, "user_prompt"), 1)
	require.Len(t, byKind(obs, "assistant_turn"), 2)
	require.Len(t, byKind(obs, "assistant_tool_use"), 2)
	require.Len(t, byKind(obs, "tool_result"), 2)

	seen := map[string]bool{}
	for i, o := range obs {
		assert.Equal(t, "s1", o.RunID)
		assert.Equal(t, "exec-T", o.ExecutionID)
		assert.Equal(t, model.SourceJSONL, o.Source)
		assert.Equal(t, uint64(i), o.Seq)
		assert.Equal(t, o.EventTime, o.ObservedAt)
		assert.NotEmpty(t, o.ObsID)
		assert.False(t, seen[o.ObsID])
		seen[o.ObsID] = true
	}
}

func TestParseReaderToolUsePayload(t *testing.T) {
	tu := byKind(parseFixture(t), "assistant_tool_use")
	require.Len(t, tu, 2)
	assert.Equal(t, "toolu_1", tu[0].Correlation.ToolUseID)
	assert.Equal(t, "Bash", tu[0].Attrs["name"])
	require.NotNil(t, tu[0].Payload)
	assert.NotEmpty(t, tu[0].Payload.Hash)
	assert.Equal(t, "mcp__fs__read", tu[1].Attrs["name"])
}

func TestParseReaderToolResultStatus(t *testing.T) {
	tr := byKind(parseFixture(t), "tool_result")
	require.Len(t, tr, 2)
	assert.Equal(t, string(model.StatusOK), tr[0].Attrs["status"])
	assert.Equal(t, string(model.StatusError), tr[1].Attrs["status"])
}

func TestParseReaderSkipsBlankLines(t *testing.T) {
	obs, err := ParseReader(strings.NewReader("\n   \n"), "exec-T")
	require.NoError(t, err)
	assert.Empty(t, obs)
}

func TestParseReaderMalformedLine(t *testing.T) {
	_, err := ParseReader(strings.NewReader("{not json}\n"), "exec-T")
	require.Error(t, err)
}

func TestParseReaderMalformedMessage(t *testing.T) {
	_, err := ParseReader(strings.NewReader(`{"type":"user","message":123}`+"\n"), "exec-T")
	require.Error(t, err)
}

func TestParseReaderMalformedContent(t *testing.T) {
	_, err := ParseReader(strings.NewReader(`{"type":"user","message":{"role":"user","content":5}}`+"\n"), "exec-T")
	require.Error(t, err)
}

func TestParseReaderScannerError(t *testing.T) {
	_, err := ParseReader(&errReader{}, "exec-T")
	require.Error(t, err)
}

func TestParseReaderEmptyMessage(t *testing.T) {
	obs, err := ParseReader(strings.NewReader(`{"type":"user"}`+"\n"), "exec-T")
	require.NoError(t, err)
	assert.Empty(t, obs)
}

func TestParseReaderUnknownType(t *testing.T) {
	obs, err := ParseReader(strings.NewReader(`{"type":"system","message":{"role":"system","content":"x"}}`+"\n"), "exec-T")
	require.NoError(t, err)
	assert.Empty(t, obs)
}

func TestParseReaderNonToolResultBlock(t *testing.T) {
	obs, err := ParseReader(strings.NewReader(`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"hi"}]}}`+"\n"), "exec-T")
	require.NoError(t, err)
	assert.Empty(t, obs)
}

func TestParseReaderAssistantTextOnly(t *testing.T) {
	obs, err := ParseReader(strings.NewReader(`{"type":"assistant","message":{"role":"assistant","id":"m","content":[{"type":"text","text":"hi"}]}}`+"\n"), "exec-T")
	require.NoError(t, err)
	require.Len(t, byKind(obs, "assistant_turn"), 1)
	assert.Empty(t, byKind(obs, "assistant_tool_use"))
}

func TestParseReaderMessageWithoutContent(t *testing.T) {
	obs, err := ParseReader(strings.NewReader(`{"type":"user","message":{"role":"user"}}`+"\n"), "exec-T")
	require.NoError(t, err)
	assert.Empty(t, obs)
}

func TestParseUsesInjectedSeqAndObservedAt(t *testing.T) {
	var n uint64 = 40
	next := func() uint64 { n++; return n }
	at := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	obs, err := Parse(strings.NewReader(
		`{"type":"assistant","timestamp":"2026-06-22T10:00:00Z","message":{"role":"assistant","id":"m1","content":[{"type":"text","text":"hi"}]}}`+"\n"),
		"exec-Z", next, func(time.Time) time.Time { return at })
	require.NoError(t, err)
	require.Len(t, obs, 1)
	assert.Equal(t, uint64(41), obs[0].Seq)
	assert.Equal(t, at, obs[0].ObservedAt)
	assert.Equal(t, "2026-06-22T10:00:00Z", obs[0].EventTime.Format(time.RFC3339))
	assert.Equal(t, model.SourceJSONL, obs[0].Source)
}

func TestNowFnSeamDefaultsToTimeNow(t *testing.T) {
	before := time.Now().Add(-time.Second)
	got := nowFn()
	assert.False(t, got.Before(before))
}

func TestParseThreadsParentToolUseID(t *testing.T) {
	f, err := os.Open("testdata/subagent.jsonl")
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })
	obs, err := ParseReader(f, "exec-S")
	require.NoError(t, err)
	tu := byKind(obs, "assistant_tool_use")
	require.Len(t, tu, 1)
	assert.Equal(t, "toolu_child", tu[0].Correlation.ToolUseID)
	assert.Equal(t, "toolu_parent", tu[0].Correlation.ParentToolUseID)
}

func TestParseEmitsSubagentForSidechain(t *testing.T) {
	f, err := os.Open("testdata/subagent.jsonl")
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })
	obs, err := ParseReader(f, "exec-S")
	require.NoError(t, err)
	sa := byKind(obs, "subagent_stop")
	require.Len(t, sa, 1)
	assert.Equal(t, "agent_42", sa[0].Correlation.AgentID)
	assert.Equal(t, "toolu_parent", sa[0].Correlation.ParentToolUseID)
}

func TestParseNoSidechainNoSubagent(t *testing.T) {
	obs, err := ParseReader(strings.NewReader(
		`{"type":"assistant","message":{"role":"assistant","id":"m","content":[{"type":"text","text":"hi"}]}}`+"\n"), "e")
	require.NoError(t, err)
	assert.Empty(t, byKind(obs, "subagent_stop"))
}

func TestSubagentTranscriptBuildsNodeAndEdge(t *testing.T) {
	f, err := os.Open("testdata/subagent.jsonl")
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })
	obs, err := ParseReader(f, "e1")
	require.NoError(t, err)
	g := reduce.NewGraph()
	g.ApplyAll(obs)
	_, edges := g.Snapshot()
	require.NotNil(t, g.Nodes[model.SubagentID("e1", "agent_42")])
	assert.Contains(t, g.Edges, model.EdgeID("e1", model.EdgeParentChild,
		model.ToolCallID("e1", "toolu_parent"), model.ToolCallID("e1", "toolu_child")))
	_ = edges
}
