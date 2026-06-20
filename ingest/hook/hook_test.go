package hook

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
)

func seqGen() func() uint64 {
	var n uint64
	return func() uint64 {
		n++
		return n
	}
}

func parseFixture(t *testing.T, hookType, file string) []model.Observation {
	t.Helper()
	payload, err := os.ReadFile(filepath.Join("testdata", file))
	require.NoError(t, err)
	obs, err := Parse(hookType, payload, "exec-T", seqGen())
	require.NoError(t, err)
	return obs
}

func TestParsePreToolUse(t *testing.T) {
	obs := parseFixture(t, "PreToolUse", "pretooluse.json")
	require.Len(t, obs, 1)
	o := obs[0]
	assert.Equal(t, "assistant_tool_use", o.Kind)
	assert.Equal(t, model.SourceHook, o.Source)
	assert.Equal(t, "s1", o.RunID)
	assert.Equal(t, "exec-T", o.ExecutionID)
	assert.Equal(t, "toolu_1", o.Correlation.ToolUseID)
	assert.Equal(t, "Bash", o.Attrs["name"])
	assert.Equal(t, string(model.StatusRunning), o.Attrs["status"])
	require.NotNil(t, o.Payload)
	assert.NotEmpty(t, o.Payload.Input)
	assert.NotEmpty(t, o.Payload.Hash)
	assert.NotEmpty(t, o.ObsID)
	assert.Equal(t, uint64(1), o.Seq)
}

func TestParsePostToolUse(t *testing.T) {
	obs := parseFixture(t, "PostToolUse", "posttooluse.json")
	require.Len(t, obs, 1)
	o := obs[0]
	assert.Equal(t, "tool_result", o.Kind)
	assert.Equal(t, "toolu_1", o.Correlation.ToolUseID)
	assert.Equal(t, string(model.StatusOK), o.Attrs["status"])
	require.NotNil(t, o.Payload)
	assert.NotEmpty(t, o.Payload.Output)
}

func TestParseUserPromptSubmit(t *testing.T) {
	obs := parseFixture(t, "UserPromptSubmit", "userpromptsubmit.json")
	require.Len(t, obs, 1)
	o := obs[0]
	assert.Equal(t, "user_prompt", o.Kind)
	assert.NotEmpty(t, o.Correlation.UUID)
	assert.Equal(t, "list files", o.Attrs["prompt"])
}

func TestParseSessionStart(t *testing.T) {
	obs := parseFixture(t, "SessionStart", "sessionstart.json")
	require.Len(t, obs, 1)
	assert.Equal(t, "session_start", obs[0].Kind)
	assert.Equal(t, "startup", obs[0].Attrs["source"])
}

func TestParseSessionEnd(t *testing.T) {
	obs := parseFixture(t, "SessionEnd", "sessionend.json")
	require.Len(t, obs, 1)
	assert.Equal(t, "session_end", obs[0].Kind)
	assert.Equal(t, "clear", obs[0].Attrs["reason"])
}

func TestParseSubagentStop(t *testing.T) {
	obs := parseFixture(t, "SubagentStop", "subagentstop.json")
	require.Len(t, obs, 1)
	o := obs[0]
	assert.Equal(t, "subagent_stop", o.Kind)
	assert.Equal(t, "a1", o.Correlation.AgentID)
	assert.Equal(t, "researcher", o.Attrs["subagent_type"])
}

func TestParseStop(t *testing.T) {
	obs := parseFixture(t, "Stop", "stop.json")
	require.Len(t, obs, 1)
	assert.Equal(t, "session_end", obs[0].Kind)
}

func TestParsePreCompactIsMarker(t *testing.T) {
	obs := parseFixture(t, "PreCompact", "precompact.json")
	require.Len(t, obs, 1)
	assert.Equal(t, "marker", obs[0].Kind)
	assert.Equal(t, "PreCompact", obs[0].Attrs["hook_event"])
}

func TestParseNotificationIsMarker(t *testing.T) {
	obs := parseFixture(t, "Notification", "notification.json")
	require.Len(t, obs, 1)
	assert.Equal(t, "marker", obs[0].Kind)
}

func TestParseUnknownType(t *testing.T) {
	obs, err := Parse("Mystery", []byte(`{"hook_event_name":"Mystery","session_id":"s1"}`), "exec-T", seqGen())
	require.NoError(t, err)
	assert.Empty(t, obs)
}

func TestParseMalformed(t *testing.T) {
	_, err := Parse("PreToolUse", []byte("{not json}"), "exec-T", seqGen())
	require.Error(t, err)
}

func TestParsePreToolUseBlocked(t *testing.T) {
	payload := []byte(`{"hook_event_name":"PreToolUse","session_id":"s1","tool_name":"Bash","tool_use_id":"t2","tool_input":{},"permission_decision":"deny"}`)
	obs, err := Parse("PreToolUse", payload, "exec-T", seqGen())
	require.NoError(t, err)
	require.Len(t, obs, 1)
	assert.Equal(t, string(model.StatusBlocked), obs[0].Attrs["status"])
}
