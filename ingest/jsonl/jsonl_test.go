package jsonl

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/ingest/drift"
	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/reduce"
)

func seqFor(t *testing.T) func() uint64 {
	t.Helper()
	var n uint64
	return func() uint64 {
		s := n
		n++
		return s
	}
}

func parseReader(r io.Reader, executionID string) ([]model.Observation, error) {
	var seq uint64
	obs, _, err := Parse(r, executionID, func() uint64 {
		s := seq
		seq++
		return s
	}, func(ts time.Time) time.Time { return ts })
	return obs, err
}

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
	obs, err := parseReader(f, "exec-T")
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

func kinds(obs []model.Observation) []string {
	out := make([]string, 0, len(obs))
	for _, o := range obs {
		out = append(out, o.Kind)
	}
	return out
}

func TestParseReaderEmitsKindsInTranscriptOrderWithTurnBeforeItsToolUses(t *testing.T) {
	assert.Equal(t, []string{
		"user_prompt",
		"assistant_turn", "assistant_tool_use", "tool_result",
		"assistant_turn", "assistant_tool_use", "tool_result",
	}, kinds(parseFixture(t)))
}

func TestParseReaderEventTimesAreNonDecreasingAcrossTheTranscript(t *testing.T) {
	obs := parseFixture(t)
	require.NotEmpty(t, obs)
	for i := 1; i < len(obs); i++ {
		assert.Falsef(t, obs[i].EventTime.Before(obs[i-1].EventTime),
			"observation %d (%s) went back in time", i, obs[i].Kind)
	}
	assert.Equal(t, "2026-06-20T10:00:00Z", obs[0].EventTime.Format(time.RFC3339))
	assert.Equal(t, "2026-06-20T10:00:04Z", obs[len(obs)-1].EventTime.Format(time.RFC3339))
}

func TestParseReaderStampsEveryObservationWithRunAndExecutionIdentity(t *testing.T) {
	obs := parseFixture(t)
	require.Len(t, obs, 7)
	seen := map[string]bool{}
	for _, o := range obs {
		assert.Equal(t, "s1", o.RunID)
		assert.Equal(t, "exec-T", o.ExecutionID)
		assert.Equal(t, model.SourceJSONL, o.Source)
		assert.Equal(t, o.EventTime, o.ObservedAt)
		assert.NotEmpty(t, o.ObsID)
		assert.False(t, seen[o.ObsID], "duplicate obs id %q", o.ObsID)
		seen[o.ObsID] = true
	}
}

func TestParseReaderSeqIsDenseAndAscendingFromTheInjectedCounter(t *testing.T) {
	f, err := os.Open("testdata/session.jsonl")
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })
	n := uint64(100)
	obs, _, err := Parse(f, "exec-T", func() uint64 { n++; return n }, func(ts time.Time) time.Time { return ts })
	require.NoError(t, err)
	require.Len(t, obs, 7)
	var got []uint64
	for _, o := range obs {
		got = append(got, o.Seq)
	}
	assert.Equal(t, []uint64{101, 102, 103, 104, 105, 106, 107}, got)
}

func TestParseReaderUserPromptTextPayload(t *testing.T) {
	up := byKind(parseFixture(t), "user_prompt")
	require.Len(t, up, 1)
	require.NotNil(t, up[0].Payload)
	assert.JSONEq(t, `"list files"`, string(up[0].Payload.Input))
	assert.Empty(t, up[0].Payload.Output)
	assert.Equal(t, model.HashPayload(up[0].Payload), up[0].Payload.Hash)
}

func TestParseReaderUserPromptCorrelatesByContentHashNotByLineUUID(t *testing.T) {
	up := byKind(parseFixture(t), "user_prompt")
	require.Len(t, up, 1)
	assert.Equal(t, model.PromptUUID("s1", "list files"), up[0].Correlation.UUID)
	assert.NotEqual(t, "u1", up[0].Correlation.UUID, "the transcript line uuid must not become the prompt identity")
	assert.Equal(t, "s1", up[0].Correlation.SessionID)
}

func TestParseReaderSamePromptInTwoSessionsGetsDistinctPromptUUIDs(t *testing.T) {
	line := func(session string) string {
		return `{"type":"user","uuid":"u1","sessionId":"` + session + `","message":{"role":"user","content":"same words"}}` + "\n"
	}
	first, err := parseReader(strings.NewReader(line("s1")), "e")
	require.NoError(t, err)
	second, err := parseReader(strings.NewReader(line("s2")), "e")
	require.NoError(t, err)
	require.Len(t, first, 1)
	require.Len(t, second, 1)
	assert.NotEqual(t, first[0].Correlation.UUID, second[0].Correlation.UUID)
}

func TestParseReaderSamePromptTextRepeatedInOneSessionSharesPromptUUID(t *testing.T) {
	in := `{"type":"user","uuid":"u1","sessionId":"s1","message":{"role":"user","content":"repeat me"}}` + "\n" +
		`{"type":"user","uuid":"u2","sessionId":"s1","message":{"role":"user","content":" repeat me "}}` + "\n"
	obs, err := parseReader(strings.NewReader(in), "e")
	require.NoError(t, err)
	up := byKind(obs, "user_prompt")
	require.Len(t, up, 2)
	assert.Equal(t, up[0].Correlation.UUID, up[1].Correlation.UUID)
}

func TestParseReaderUserPromptEmptyTextNoPayload(t *testing.T) {
	obs, err := parseReader(strings.NewReader(
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"x","is_error":false}]}}`+"\n"), "e")
	require.NoError(t, err)
	assert.Empty(t, byKind(obs, "user_prompt"))
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
	obs, err := parseReader(strings.NewReader("\n   \n"), "exec-T")
	require.NoError(t, err)
	assert.Empty(t, obs)
}

func TestParseReaderMalformedLine(t *testing.T) {
	_, err := parseReader(strings.NewReader("{not json}\n"), "exec-T")
	require.Error(t, err)
}

func TestParseReaderMalformedMessage(t *testing.T) {
	_, err := parseReader(strings.NewReader(`{"type":"user","message":123}`+"\n"), "exec-T")
	require.Error(t, err)
}

func TestParseReaderMalformedContent(t *testing.T) {
	_, err := parseReader(strings.NewReader(`{"type":"user","message":{"role":"user","content":5}}`+"\n"), "exec-T")
	require.Error(t, err)
}

func TestParseReaderScannerError(t *testing.T) {
	_, err := parseReader(&errReader{}, "exec-T")
	require.Error(t, err)
}

func TestParseReaderEmptyMessage(t *testing.T) {
	obs, err := parseReader(strings.NewReader(`{"type":"user"}`+"\n"), "exec-T")
	require.NoError(t, err)
	assert.Empty(t, obs)
}

func TestParseReaderUnknownType(t *testing.T) {
	obs, err := parseReader(strings.NewReader(`{"type":"system","message":{"role":"system","content":"x"}}`+"\n"), "exec-T")
	require.NoError(t, err)
	assert.Empty(t, obs)
}

func TestParseReaderNonToolResultBlock(t *testing.T) {
	obs, err := parseReader(strings.NewReader(`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"hi"}]}}`+"\n"), "exec-T")
	require.NoError(t, err)
	assert.Empty(t, obs)
}

func TestSidechainLineCorrelationAgentIDSet(t *testing.T) {
	obs, err := parseReader(strings.NewReader(
		`{"type":"assistant","sessionId":"s1","agentId":"agent_99","isSidechain":true,"timestamp":"2026-06-22T10:00:00Z","message":{"role":"assistant","id":"m1","content":[{"type":"text","text":"hi"}]}}`+"\n"), "e")
	require.NoError(t, err)
	turns := byKind(obs, "assistant_turn")
	require.Len(t, turns, 1)
	assert.Equal(t, "agent_99", turns[0].Correlation.AgentID)
}

func TestMainLineCorrelationAgentIDEmpty(t *testing.T) {
	obs, err := parseReader(strings.NewReader(
		`{"type":"assistant","sessionId":"s1","timestamp":"2026-06-22T10:00:00Z","message":{"role":"assistant","id":"m1","content":[{"type":"text","text":"hi"}]}}`+"\n"), "e")
	require.NoError(t, err)
	turns := byKind(obs, "assistant_turn")
	require.Len(t, turns, 1)
	assert.Empty(t, turns[0].Correlation.AgentID)
}

func TestParseReaderAssistantTextOnly(t *testing.T) {
	obs, err := parseReader(strings.NewReader(`{"type":"assistant","message":{"role":"assistant","id":"m","content":[{"type":"text","text":"hi"}]}}`+"\n"), "exec-T")
	require.NoError(t, err)
	require.Len(t, byKind(obs, "assistant_turn"), 1)
	assert.Empty(t, byKind(obs, "assistant_tool_use"))
}

func TestParseReaderMessageWithoutContent(t *testing.T) {
	obs, err := parseReader(strings.NewReader(`{"type":"user","message":{"role":"user"}}`+"\n"), "exec-T")
	require.NoError(t, err)
	assert.Empty(t, obs)
}

func TestParseUsesInjectedSeqAndObservedAt(t *testing.T) {
	var n uint64 = 40
	next := func() uint64 { n++; return n }
	at := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	obs, _, err := Parse(strings.NewReader(
		`{"type":"assistant","timestamp":"2026-06-22T10:00:00Z","message":{"role":"assistant","id":"m1","content":[{"type":"text","text":"hi"}]}}`+"\n"),
		"exec-Z", next, func(time.Time) time.Time { return at })
	require.NoError(t, err)
	require.Len(t, obs, 1)
	assert.Equal(t, uint64(41), obs[0].Seq)
	assert.Equal(t, at, obs[0].ObservedAt)
	assert.Equal(t, "2026-06-22T10:00:00Z", obs[0].EventTime.Format(time.RFC3339))
	assert.Equal(t, model.SourceJSONL, obs[0].Source)
}

func TestParseThreadsParentToolUseID(t *testing.T) {
	f, err := os.Open("testdata/subagent.jsonl")
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })
	obs, err := parseReader(f, "exec-S")
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
	obs, err := parseReader(f, "exec-S")
	require.NoError(t, err)
	sa := byKind(obs, "subagent_stop")
	require.Len(t, sa, 1)
	assert.Equal(t, "agent_42", sa[0].Correlation.AgentID)
	assert.Equal(t, "toolu_parent", sa[0].Correlation.ParentToolUseID)
}

func TestParseSubagentStopCarriesSubagentType(t *testing.T) {
	obs, err := parseReader(strings.NewReader(
		`{"type":"assistant","sessionId":"s1","agentId":"agent_7","isSidechain":true,"subagent_type":"general-purpose","timestamp":"2026-06-22T10:00:00Z","message":{"role":"assistant","id":"m1","content":[{"type":"text","text":"hi"}]}}`+"\n"), "e")
	require.NoError(t, err)
	sa := byKind(obs, "subagent_stop")
	require.Len(t, sa, 1)
	assert.Equal(t, "general-purpose", sa[0].Attrs["subagent_type"])
}

func TestParseSubagentStopWithoutSubagentTypeHasNoAttr(t *testing.T) {
	obs, err := parseReader(strings.NewReader(
		`{"type":"assistant","sessionId":"s1","agentId":"agent_7","isSidechain":true,"timestamp":"2026-06-22T10:00:00Z","message":{"role":"assistant","id":"m1","content":[{"type":"text","text":"hi"}]}}`+"\n"), "e")
	require.NoError(t, err)
	sa := byKind(obs, "subagent_stop")
	require.Len(t, sa, 1)
	_, ok := sa[0].Attrs["subagent_type"]
	assert.False(t, ok)
}

func TestParseNoSidechainNoSubagent(t *testing.T) {
	obs, err := parseReader(strings.NewReader(
		`{"type":"assistant","message":{"role":"assistant","id":"m","content":[{"type":"text","text":"hi"}]}}`+"\n"), "e")
	require.NoError(t, err)
	assert.Empty(t, byKind(obs, "subagent_stop"))
}

func TestParseReaderAssistantCacheTokens(t *testing.T) {
	obs, err := parseReader(strings.NewReader(
		`{"type":"assistant","message":{"role":"assistant","id":"m","content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":10,"output_tokens":5,"cache_read_input_tokens":321,"cache_creation_input_tokens":99}}}`+"\n"), "e")
	require.NoError(t, err)
	turn := byKind(obs, "assistant_turn")
	require.Len(t, turn, 1)
	assert.Equal(t, int64(321), turn[0].Attrs["cache_read_in"])
	assert.Equal(t, int64(99), turn[0].Attrs["cache_write"])
}

func TestParseReaderAssistantTextPayload(t *testing.T) {
	obs, err := parseReader(strings.NewReader(
		`{"type":"assistant","message":{"role":"assistant","id":"m","content":[{"type":"text","text":"here is the answer"}]}}`+"\n"), "e")
	require.NoError(t, err)
	turn := byKind(obs, "assistant_turn")
	require.Len(t, turn, 1)
	require.NotNil(t, turn[0].Payload)
	assert.JSONEq(t, `"here is the answer"`, string(turn[0].Payload.Output))
	assert.Empty(t, turn[0].Payload.Input)
	assert.NotEmpty(t, turn[0].Payload.Hash)
}

func TestParseReaderAssistantToolUsePayloadUntouched(t *testing.T) {
	tu := byKind(parseFixture(t), "assistant_tool_use")
	require.Len(t, tu, 2)
	require.NotNil(t, tu[0].Payload)
	assert.JSONEq(t, `{"command":"ls"}`, string(tu[0].Payload.Input))
	assert.Empty(t, tu[0].Payload.Output)
}

func TestParseReaderAssistantNoTextNoTurnPayload(t *testing.T) {
	turn := byKind(parseFixture(t), "assistant_turn")
	require.Len(t, turn, 2)
	assert.Nil(t, turn[0].Payload)
	assert.Nil(t, turn[1].Payload)
}

func TestParseReaderUserPromptSyntheticKind(t *testing.T) {
	obs, err := parseReader(strings.NewReader(
		`{"type":"user","message":{"role":"user","content":"<system-reminder>foo"}}`+"\n"), "e")
	require.NoError(t, err)
	up := byKind(obs, "user_prompt")
	require.Len(t, up, 1)
	assert.Equal(t, "system", up[0].Attrs["prompt_kind"])
}

func TestParseReaderUserPromptHumanKind(t *testing.T) {
	obs, err := parseReader(strings.NewReader(
		`{"type":"user","message":{"role":"user","content":"hello friend"}}`+"\n"), "e")
	require.NoError(t, err)
	up := byKind(obs, "user_prompt")
	require.Len(t, up, 1)
	assert.Equal(t, "human", up[0].Attrs["prompt_kind"])
}

func TestDecodeLineVersionAndCwdInjected(t *testing.T) {
	raw := `{"type":"assistant","sessionId":"s1","isSidechain":true,"version":"1.2.3","cwd":"/home","message":{"id":"m1","model":"claude-opus-4-8","role":"assistant","content":[{"type":"text","text":"hello"}]}}` + "\n"
	obs, err := parseReader(strings.NewReader(raw), "exec1")
	require.NoError(t, err)

	byKindMap := map[string]*model.Observation{}
	for i := range obs {
		byKindMap[obs[i].Kind] = &obs[i]
	}

	turn, ok := byKindMap["assistant_turn"]
	require.True(t, ok, "expected assistant_turn observation")
	assert.Equal(t, "1.2.3", turn.Attrs["claude_code_version"])
	assert.Equal(t, "/home", turn.Attrs["cwd"])

	stop, ok := byKindMap["subagent_stop"]
	require.True(t, ok, "expected subagent_stop observation")
	assert.Equal(t, "1.2.3", stop.Attrs["claude_code_version"])
	assert.Equal(t, "/home", stop.Attrs["cwd"])
}

func TestParseUnknownRecordTypeCountsDrift(t *testing.T) {
	in := `{"type":"checkpoint_v9","sessionId":"s1"}` + "\n" + `{"type":"summary","summary":"s"}` + "\n"
	obs, dc, err := Parse(strings.NewReader(in), "exec-T", seqFor(t), func(ts time.Time) time.Time { return ts })
	require.NoError(t, err)
	assert.Empty(t, obs)
	assert.Equal(t, drift.Counts{drift.ReasonUnknownRecordType: 1}, dc)
}

func TestParseUnknownContentBlockCountsDrift(t *testing.T) {
	in := `{"type":"user","sessionId":"s1","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"},{"type":"video_frame"}]}}` + "\n"
	obs, dc, err := Parse(strings.NewReader(in), "exec-T", seqFor(t), func(ts time.Time) time.Time { return ts })
	require.NoError(t, err)
	require.Len(t, obs, 1)
	assert.Equal(t, drift.Counts{drift.ReasonUnknownContentBlock: 1}, dc)
}

func TestParseAssistantUnknownContentBlockCountsDrift(t *testing.T) {
	in := `{"type":"assistant","sessionId":"s1","message":{"role":"assistant","id":"m1","content":[{"type":"text","text":"hi"},{"type":"hologram"}]}}` + "\n"
	obs, dc, err := Parse(strings.NewReader(in), "exec-T", seqFor(t), func(ts time.Time) time.Time { return ts })
	require.NoError(t, err)
	require.Len(t, obs, 1)
	assert.Equal(t, drift.Counts{drift.ReasonUnknownContentBlock: 1}, dc)
}

func TestParseKnownIgnoredRecordTypesNoDrift(t *testing.T) {
	in := `{"type":"summary","summary":"s"}` + "\n" +
		`{"type":"system","sessionId":"s1"}` + "\n" +
		`{"type":"file-history-snapshot","sessionId":"s1"}` + "\n" +
		`{"type":"attachment","sessionId":"s1"}` + "\n" +
		`{"type":"last-prompt","sessionId":"s1"}` + "\n" +
		`{"type":"mode","sessionId":"s1"}` + "\n" +
		`{"type":"ai-title","sessionId":"s1"}` + "\n" +
		`{"type":"permission-mode","sessionId":"s1"}` + "\n" +
		`{"type":"pr-link","sessionId":"s1"}` + "\n" +
		`{"type":"queue-operation","sessionId":"s1"}` + "\n" +
		`{"type":"worktree-state","sessionId":"s1"}` + "\n" +
		`{"type":"relocated","sessionId":"s1"}` + "\n"
	obs, dc, err := Parse(strings.NewReader(in), "exec-T", seqFor(t), func(ts time.Time) time.Time { return ts })
	require.NoError(t, err)
	assert.Empty(t, obs)
	assert.Empty(t, dc)
}

func TestParseUserDocumentBlockNoDrift(t *testing.T) {
	in := `{"type":"user","sessionId":"s1","message":{"role":"user","content":[{"type":"document"},{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}}` + "\n"
	obs, dc, err := Parse(strings.NewReader(in), "exec-T", seqFor(t), func(ts time.Time) time.Time { return ts })
	require.NoError(t, err)
	require.Len(t, obs, 1)
	assert.Empty(t, dc)
}

func TestParseReaderDiscardsDriftCounts(t *testing.T) {
	obs, err := parseReader(strings.NewReader(`{"type":"checkpoint_v9","sessionId":"s1"}`+"\n"), "exec-T")
	require.NoError(t, err)
	assert.Empty(t, obs)
}

func TestParseBadTimestampCountsDrift(t *testing.T) {
	in := `{"type":"assistant","sessionId":"s1","timestamp":"not-a-timestamp","message":{"role":"assistant","id":"m1","content":[{"type":"text","text":"hi"}]}}` + "\n"
	obs, dc, err := Parse(strings.NewReader(in), "exec-T", seqFor(t), func(ts time.Time) time.Time { return ts })
	require.NoError(t, err)
	require.Len(t, obs, 1)
	assert.True(t, obs[0].EventTime.IsZero())
	assert.Equal(t, drift.Counts{drift.ReasonBadTimestamp: 1}, dc)
}

func TestParseMissingTimestampNoDrift(t *testing.T) {
	in := `{"type":"assistant","sessionId":"s1","message":{"role":"assistant","id":"m1","content":[{"type":"text","text":"hi"}]}}` + "\n"
	obs, dc, err := Parse(strings.NewReader(in), "exec-T", seqFor(t), func(ts time.Time) time.Time { return ts })
	require.NoError(t, err)
	require.Len(t, obs, 1)
	assert.True(t, obs[0].EventTime.IsZero())
	assert.Empty(t, dc)
}

func TestParseBadTimestampDoesNotSaturateDuration(t *testing.T) {
	in := `{"type":"assistant","uuid":"a1","sessionId":"s1","timestamp":"2026-06-20T10:00:00Z","message":{"role":"assistant","id":"m1","content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"ls"}}]}}` + "\n" +
		`{"type":"user","uuid":"u1","sessionId":"s1","timestamp":"20XX-13-99T99:99:99","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"ok","is_error":false}]}}` + "\n"
	obs, dc, err := Parse(strings.NewReader(in), "e1", seqFor(t), func(ts time.Time) time.Time { return ts })
	require.NoError(t, err)
	assert.Equal(t, drift.Counts{drift.ReasonBadTimestamp: 1}, dc)

	g := reduce.NewGraph()
	g.ApplyAll(obs)
	n := g.Nodes[model.ToolCallID("e1", "toolu_1")]
	require.NotNil(t, n)
	require.NotNil(t, n.TStart)
	assert.Equal(t, "2026-06-20T10:00:00Z", n.TStart.Format(time.RFC3339))
	assert.Nil(t, n.TEnd)
	assert.Nil(t, n.DurationMS)
}

func TestSubagentTranscriptBuildsNodeAndEdge(t *testing.T) {
	f, err := os.Open("testdata/subagent.jsonl")
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })
	obs, err := parseReader(f, "e1")
	require.NoError(t, err)
	g := reduce.NewGraph()
	g.ApplyAll(obs)
	require.NotNil(t, g.Nodes[model.SubagentID("e1", "agent_42")])
	assert.Contains(t, g.Edges, model.EdgeID("e1", model.EdgeParentChild,
		model.ToolCallID("e1", "toolu_parent"), model.ToolCallID("e1", "toolu_child")))
}

func TestParseLineTooLongWrapsErrTooLong(t *testing.T) {
	r := strings.NewReader(strings.Repeat("a", 16*1024*1024+1))
	_, err := parseReader(r, "exec-T")
	require.Error(t, err)
	assert.ErrorIs(t, err, bufio.ErrTooLong)
	assert.Contains(t, err.Error(), "jsonl.Parse:")
	assert.Contains(t, err.Error(), "token too long")
}

func longPromptLine(t *testing.T, promptBytes int) (string, string) {
	t.Helper()
	prompt := strings.Repeat("x", promptBytes)
	raw, err := json.Marshal(map[string]any{
		"type":      "user",
		"sessionId": "s1",
		"message":   map[string]any{"role": "user", "content": prompt},
	})
	require.NoError(t, err)
	return string(raw), prompt
}

func TestParseAcceptsLinesFarBeyondTheDefaultScannerTokenLimit(t *testing.T) {
	for _, promptBytes := range []int{64 * 1024, 1024 * 1024, 4 * 1024 * 1024} {
		t.Run(strconv.Itoa(promptBytes), func(t *testing.T) {
			raw, prompt := longPromptLine(t, promptBytes)
			require.Greater(t, len(raw), bufio.MaxScanTokenSize)
			obs, err := parseReader(strings.NewReader(raw+"\n"), "exec-T")
			require.NoError(t, err)
			up := byKind(obs, "user_prompt")
			require.Len(t, up, 1)
			require.NotNil(t, up[0].Payload)
			var got string
			require.NoError(t, json.Unmarshal(up[0].Payload.Input, &got))
			assert.Equal(t, prompt, got)
		})
	}
}

func TestParseAcceptsFinalLineWithoutTrailingNewline(t *testing.T) {
	in := `{"type":"user","sessionId":"s1","message":{"role":"user","content":"first"}}` + "\n" +
		`{"type":"user","sessionId":"s1","message":{"role":"user","content":"last"}}`
	obs, err := parseReader(strings.NewReader(in), "exec-T")
	require.NoError(t, err)
	require.Len(t, byKind(obs, "user_prompt"), 2)
}

func TestParseRejectsTruncatedFinalLineRatherThanSilentlyDroppingIt(t *testing.T) {
	in := `{"type":"user","sessionId":"s1","message":{"role":"user","content":"first"}}` + "\n" +
		`{"type":"user","sessionId":"s1","message":{"role":"user","conte`
	obs, err := parseReader(strings.NewReader(in), "exec-T")
	require.Error(t, err)
	assert.Nil(t, obs)
}

func TestParseAbandonsEarlierObservationsWhenALaterLineIsMalformed(t *testing.T) {
	in := `{"type":"user","sessionId":"s1","message":{"role":"user","content":"good"}}` + "\n" +
		"{not json}\n"
	obs, err := parseReader(strings.NewReader(in), "exec-T")
	require.Error(t, err)
	assert.Nil(t, obs)
}

func TestParseInvalidUTF8InStringPayload(t *testing.T) {
	in := "{\"type\":\"user\",\"sessionId\":\"s1\",\"message\":{\"role\":\"user\",\"content\":\"h\xffi\"}}\n"
	obs, err := parseReader(strings.NewReader(in), "exec-T")
	require.NoError(t, err)
	up := byKind(obs, "user_prompt")
	require.Len(t, up, 1)
	require.NotNil(t, up[0].Payload)
	var got string
	require.NoError(t, json.Unmarshal(up[0].Payload.Input, &got))
	assert.Equal(t, "h�i", got)
	assert.True(t, utf8.ValidString(got))
}

func TestParseEmbeddedNULByteErrors(t *testing.T) {
	in := "{\"type\":\"user\",\"uuid\":\"a\x00b\",\"sessionId\":\"s1\"}\n"
	_, err := parseReader(strings.NewReader(in), "exec-T")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "jsonl.decodeLine")
}

func TestParseEscapedNULAccepted(t *testing.T) {
	in := `{"type":"user","sessionId":"s1","message":{"role":"user","content":"a\u0000b"}}` + "\n"
	obs, err := parseReader(strings.NewReader(in), "exec-T")
	require.NoError(t, err)
	require.Len(t, byKind(obs, "user_prompt"), 1)
}

func TestParseDeeplyNestedJSONErrors(t *testing.T) {
	depth := 100000
	in := `{"type":"user","sessionId":"s1","message":{"role":"user","content":` +
		strings.Repeat("[", depth) + strings.Repeat("]", depth) + `}}` + "\n"
	_, err := parseReader(strings.NewReader(in), "exec-T")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "jsonl.decodeLine")
}

func TestSkillToolReducesToNodeSkill(t *testing.T) {
	f, err := os.Open("testdata/skill.jsonl")
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })
	obs, err := parseReader(f, "e1")
	require.NoError(t, err)
	g := reduce.NewGraph()
	g.ApplyAll(obs)
	n := g.Nodes[model.ToolCallID("e1", "toolu_sk")]
	require.NotNil(t, n)
	assert.Equal(t, model.NodeSkill, n.Type)
	assert.Equal(t, "superpowers:verify", n.Name)
}
