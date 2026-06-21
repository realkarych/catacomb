package streamjson

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
)

func seq() func() uint64 {
	var n uint64
	return func() uint64 {
		n++
		return n
	}
}

func fixedNow(t time.Time) {
	nowFn = func() time.Time { return t }
}

func TestParseSystemInit(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	fixedNow(now)
	line := []byte(`{"type":"system","subtype":"init","session_id":"sess_1","model":"claude-opus-4-8"}`)

	obs, err := Parse(line, "exec1", seq())
	require.NoError(t, err)
	require.Len(t, obs, 1)

	o := obs[0]
	assert.Equal(t, "session_start", o.Kind)
	assert.Equal(t, model.SourceStreamJSON, o.Source)
	assert.Equal(t, "exec1", o.ExecutionID)
	assert.Equal(t, "sess_1", o.RunID)
	assert.Equal(t, "sess_1", o.Correlation.SessionID)
	assert.Equal(t, "claude-opus-4-8", o.Attrs["model"])
	assert.Equal(t, now, o.ObservedAt)
	assert.Equal(t, now, o.EventTime)
	assert.Equal(t, uint64(1), o.Seq)
	assert.NotEmpty(t, o.ObsID)
}

func TestParseAssistantTurnAndToolUse(t *testing.T) {
	fixedNow(time.Now())
	line := []byte(`{"type":"assistant","session_id":"sess_2","message":{"id":"msg_a","model":"claude-opus-4-8","usage":{"input_tokens":100,"output_tokens":50},"content":[{"type":"text","text":"hi"},{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"ls"}}]}}`)

	obs, err := Parse(line, "exec2", seq())
	require.NoError(t, err)
	require.Len(t, obs, 2)

	turn := obs[0]
	assert.Equal(t, "assistant_turn", turn.Kind)
	assert.Equal(t, "msg_a", turn.Correlation.MessageID)
	assert.Equal(t, "claude-opus-4-8", turn.Attrs["model"])
	assert.Equal(t, int64(100), turn.Attrs["tokens_in"])
	assert.Equal(t, int64(50), turn.Attrs["tokens_out"])

	tu := obs[1]
	assert.Equal(t, "assistant_tool_use", tu.Kind)
	assert.Equal(t, "toolu_1", tu.Correlation.ToolUseID)
	assert.Equal(t, "msg_a", tu.Correlation.MessageID)
	assert.Equal(t, "Bash", tu.Attrs["name"])
	require.NotNil(t, tu.Payload)
	assert.JSONEq(t, `{"command":"ls"}`, string(tu.Payload.Input))
}

func TestParseUserToolResult(t *testing.T) {
	fixedNow(time.Now())
	line := []byte(`{"type":"user","session_id":"sess_3","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"done","is_error":false}]}}`)
	obs, err := Parse(line, "exec3", seq())
	require.NoError(t, err)
	require.Len(t, obs, 1)
	o := obs[0]
	assert.Equal(t, "tool_result", o.Kind)
	assert.Equal(t, "toolu_1", o.Correlation.ToolUseID)
	assert.Equal(t, "ok", o.Attrs["status"])
	assert.JSONEq(t, `"done"`, string(o.Payload.Output))
}

func TestParseUserToolResultError(t *testing.T) {
	fixedNow(time.Now())
	line := []byte(`{"type":"user","session_id":"s","message":{"content":[{"type":"tool_result","tool_use_id":"t","content":"boom","is_error":true}]}}`)
	obs, err := Parse(line, "e", seq())
	require.NoError(t, err)
	require.Len(t, obs, 1)
	assert.Equal(t, "error", obs[0].Attrs["status"])
}

func TestParseUserPromptText(t *testing.T) {
	fixedNow(time.Now())
	line := []byte(`{"type":"user","session_id":"s","message":{"content":"hello there"}}`)
	obs, err := Parse(line, "e", seq())
	require.NoError(t, err)
	require.Len(t, obs, 1)
	assert.Equal(t, "user_prompt", obs[0].Kind)
	assert.Equal(t, "hello there", obs[0].Attrs["prompt"])
}

func TestParseStreamEventParentToolUseID(t *testing.T) {
	fixedNow(time.Now())
	line := []byte(`{"type":"stream_event","session_id":"s","parent_tool_use_id":"toolu_parent","uuid":"u1"}`)
	obs, err := Parse(line, "e", seq())
	require.NoError(t, err)
	require.Len(t, obs, 1)
	o := obs[0]
	assert.Equal(t, "assistant_tool_use", o.Kind)
	assert.Equal(t, "toolu_parent", o.Correlation.ParentToolUseID)
	assert.Equal(t, "u1", o.Correlation.UUID)
}

func TestParseResultEnrichment(t *testing.T) {
	fixedNow(time.Now())
	line := []byte(`{"type":"result","session_id":"s","usage":{"input_tokens":7,"output_tokens":9},"total_cost_usd":0.0123}`)
	obs, err := Parse(line, "e", seq())
	require.NoError(t, err)
	require.Len(t, obs, 1)
	o := obs[0]
	assert.Equal(t, "assistant_turn", o.Kind)
	assert.Equal(t, int64(7), o.Attrs["tokens_in"])
	assert.Equal(t, int64(9), o.Attrs["tokens_out"])
	assert.Equal(t, 0.0123, o.Attrs["cost_usd"])
}

func TestParseUnknownTypeSkipped(t *testing.T) {
	fixedNow(time.Now())
	obs, err := Parse([]byte(`{"type":"mystery","session_id":"s"}`), "e", seq())
	require.NoError(t, err)
	assert.Empty(t, obs)
}

func TestParseSystemNonInitSkipped(t *testing.T) {
	fixedNow(time.Now())
	obs, err := Parse([]byte(`{"type":"system","subtype":"other","session_id":"s"}`), "e", seq())
	require.NoError(t, err)
	assert.Empty(t, obs)
}

func TestParseBadJSON(t *testing.T) {
	fixedNow(time.Now())
	_, err := Parse([]byte(`{not json`), "e", seq())
	require.Error(t, err)
}

func TestParseBadMessage(t *testing.T) {
	fixedNow(time.Now())
	_, err := Parse([]byte(`{"type":"assistant","session_id":"s","message":123}`), "e", seq())
	require.Error(t, err)
}

func TestParseBadContent(t *testing.T) {
	fixedNow(time.Now())
	_, err := Parse([]byte(`{"type":"user","session_id":"s","message":{"content":123}}`), "e", seq())
	require.Error(t, err)
}

func TestParseAssistantNoMessageNoTokens(t *testing.T) {
	fixedNow(time.Now())
	obs, err := Parse([]byte(`{"type":"assistant","session_id":"s"}`), "e", seq())
	require.NoError(t, err)
	require.Len(t, obs, 1)
	_, ok := obs[0].Attrs["tokens_in"]
	assert.False(t, ok)
}

func TestParseResultNoUsageNoCost(t *testing.T) {
	fixedNow(time.Now())
	obs, err := Parse([]byte(`{"type":"result","session_id":"s"}`), "e2", seq())
	require.NoError(t, err)
	require.Len(t, obs, 1)
	assert.Equal(t, "assistant_turn", obs[0].Kind)
	_, hasTokensIn := obs[0].Attrs["tokens_in"]
	assert.False(t, hasTokensIn)
	_, hasCost := obs[0].Attrs["cost_usd"]
	assert.False(t, hasCost)
}

func TestParseUserBadMessageField(t *testing.T) {
	fixedNow(time.Now())
	_, err := Parse([]byte(`{"type":"user","session_id":"s","message":123}`), "e3", seq())
	require.Error(t, err)
}

func TestParseAssistantBadContent(t *testing.T) {
	fixedNow(time.Now())
	_, err := Parse([]byte(`{"type":"assistant","session_id":"s","message":{"id":"m","content":123}}`), "e4", seq())
	require.Error(t, err)
}

func TestParseUserNonToolResultBlockSkipped(t *testing.T) {
	fixedNow(time.Now())
	line := []byte(`{"type":"user","session_id":"s","message":{"content":[{"type":"text","text":"hi"},{"type":"tool_result","tool_use_id":"t2","content":"ok","is_error":false}]}}`)
	obs, err := Parse(line, "e5", seq())
	require.NoError(t, err)
	require.Len(t, obs, 1)
	assert.Equal(t, "tool_result", obs[0].Kind)
	assert.Equal(t, "t2", obs[0].Correlation.ToolUseID)
}
