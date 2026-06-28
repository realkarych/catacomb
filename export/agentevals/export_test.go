package agentevals_test

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/export/agentevals"
	"github.com/realkarych/catacomb/model"
)

func mustTime(s string) *time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return &t
}

func sampleNodes() []*model.Node {
	return []*model.Node{
		{
			ID:     "session-1",
			RunID:  "run-1",
			Type:   model.NodeSession,
			TStart: mustTime("2024-01-01T00:00:01Z"),
		},
		{
			ID:     "prompt-1",
			RunID:  "run-1",
			Type:   model.NodeUserPrompt,
			TStart: mustTime("2024-01-01T00:00:02Z"),
			Payload: &model.Payload{
				Input: json.RawMessage(`"hello"`),
			},
		},
		{
			ID:     "turn-1",
			RunID:  "run-1",
			Type:   model.NodeAssistantTurn,
			TStart: mustTime("2024-01-01T00:00:03Z"),
			Payload: &model.Payload{
				Output: json.RawMessage(`"hi there"`),
			},
		},
		{
			ID:     "tool-bash-1",
			RunID:  "run-1",
			Name:   "Bash",
			Type:   model.NodeToolCall,
			TStart: mustTime("2024-01-01T00:00:04Z"),
			Payload: &model.Payload{
				Input:  json.RawMessage(`{"command":"ls"}`),
				Output: json.RawMessage(`"a.txt\nb.txt"`),
			},
		},
		{
			ID:     "tool-mcp-1",
			RunID:  "run-1",
			Name:   "read_file",
			Type:   model.NodeMCPCall,
			TStart: mustTime("2024-01-01T00:00:05Z"),
			Payload: &model.Payload{
				Input:  json.RawMessage(`{"path":"x"}`),
				Output: json.RawMessage(`"file content"`),
			},
		},
	}
}

func sampleEdges() []*model.Edge {
	return []*model.Edge{
		{ID: "e1", RunID: "run-1", Type: model.EdgeParentChild, Src: "session-1", Dst: "prompt-1"},
		{ID: "e2", RunID: "run-1", Type: model.EdgeParentChild, Src: "session-1", Dst: "turn-1"},
		{ID: "e3", RunID: "run-1", Type: model.EdgeParentChild, Src: "turn-1", Dst: "tool-bash-1"},
		{ID: "e4", RunID: "run-1", Type: model.EdgeParentChild, Src: "turn-1", Dst: "tool-mcp-1"},
	}
}

func TestBuildConversation(t *testing.T) {
	msgs := agentevals.Build(sampleNodes(), sampleEdges())
	require.Len(t, msgs, 4)

	assert.Equal(t, "user", msgs[0].Role)
	assert.Equal(t, "hello", msgs[0].Content)

	assert.Equal(t, "assistant", msgs[1].Role)
	assert.Equal(t, "hi there", msgs[1].Content)
	require.Len(t, msgs[1].ToolCalls, 2)
	assert.Equal(t, "tool-bash-1", msgs[1].ToolCalls[0].ID)
	assert.Equal(t, "function", msgs[1].ToolCalls[0].Type)
	assert.Equal(t, "Bash", msgs[1].ToolCalls[0].Function.Name)
	assert.Equal(t, `{"command":"ls"}`, msgs[1].ToolCalls[0].Function.Arguments)
	assert.Equal(t, "tool-mcp-1", msgs[1].ToolCalls[1].ID)
	assert.Equal(t, "read_file", msgs[1].ToolCalls[1].Function.Name)
	assert.Equal(t, `{"path":"x"}`, msgs[1].ToolCalls[1].Function.Arguments)

	assert.Equal(t, "tool", msgs[2].Role)
	assert.Equal(t, "a.txt\nb.txt", msgs[2].Content)
	assert.Equal(t, "tool-bash-1", msgs[2].ToolCallID)

	assert.Equal(t, "tool", msgs[3].Role)
	assert.Equal(t, "file content", msgs[3].Content)
	assert.Equal(t, "tool-mcp-1", msgs[3].ToolCallID)
}

func TestBuildGolden(t *testing.T) {
	msgs := agentevals.Build(sampleNodes(), sampleEdges())
	got, err := json.Marshal(msgs)
	require.NoError(t, err)

	golden, err := os.ReadFile("testdata/conversation.golden.json")
	require.NoError(t, err)

	assert.JSONEq(t, string(golden), string(got))
}

func TestBuildEdgeCases(t *testing.T) {
	t.Run("assistant-no-text-only-tool", func(t *testing.T) {
		nodes := []*model.Node{
			{ID: "turn-x", RunID: "r", Type: model.NodeAssistantTurn},
			{ID: "tool-x", RunID: "r", Name: "Run", Type: model.NodeToolCall, Payload: &model.Payload{Output: json.RawMessage(`"result"`)}},
		}
		edges := []*model.Edge{
			{ID: "e1", RunID: "r", Type: model.EdgeParentChild, Src: "turn-x", Dst: "tool-x"},
			{ID: "e2", RunID: "r", Type: model.EdgeSequence, Src: "turn-x", Dst: "tool-x"},
		}
		msgs := agentevals.Build(nodes, edges)
		require.Len(t, msgs, 2)
		assert.Equal(t, "assistant", msgs[0].Role)
		assert.Equal(t, "", msgs[0].Content)
		require.Len(t, msgs[0].ToolCalls, 1)
		assert.Equal(t, "{}", msgs[0].ToolCalls[0].Function.Arguments)
		assert.Equal(t, "tool", msgs[1].Role)
		assert.Equal(t, "tool-x", msgs[1].ToolCallID)
	})

	t.Run("tool-nil-payload-in-turn", func(t *testing.T) {
		nodes := []*model.Node{
			{ID: "turn-np", RunID: "r", Type: model.NodeAssistantTurn},
			{ID: "tool-np", RunID: "r", Name: "Run", Type: model.NodeToolCall},
		}
		edges := []*model.Edge{
			{ID: "e1", RunID: "r", Type: model.EdgeParentChild, Src: "turn-np", Dst: "tool-np"},
		}
		msgs := agentevals.Build(nodes, edges)
		require.Len(t, msgs, 2)
		assert.Equal(t, "assistant", msgs[0].Role)
		assert.Equal(t, "", msgs[1].Content)
	})

	t.Run("orphan-tool-parented-to-session", func(t *testing.T) {
		nodes := []*model.Node{
			{ID: "sess-y", RunID: "r", Type: model.NodeSession},
			{ID: "tool-y", RunID: "r", Name: "Bash", Type: model.NodeToolCall, Payload: &model.Payload{Output: json.RawMessage(`"done"`)}},
		}
		edges := []*model.Edge{
			{ID: "e1", RunID: "r", Type: model.EdgeParentChild, Src: "sess-y", Dst: "tool-y"},
		}
		msgs := agentevals.Build(nodes, edges)
		require.Len(t, msgs, 1)
		assert.Equal(t, "tool", msgs[0].Role)
		assert.Equal(t, "tool-y", msgs[0].ToolCallID)
	})

	t.Run("non-string-user-payload", func(t *testing.T) {
		nodes := []*model.Node{
			{ID: "p1", RunID: "r", Type: model.NodeUserPrompt, Payload: &model.Payload{Input: json.RawMessage(`{"complex":"data"}`)}},
		}
		msgs := agentevals.Build(nodes, nil)
		require.Len(t, msgs, 1)
		assert.Equal(t, `{"complex":"data"}`, msgs[0].Content)
	})

	t.Run("skips-session-subagent-marker", func(t *testing.T) {
		nodes := []*model.Node{
			{ID: "s1", RunID: "r", Type: model.NodeSession},
			{ID: "sa1", RunID: "r", Type: model.NodeSubagent},
			{ID: "m1", RunID: "r", Type: model.NodeMarker},
		}
		msgs := agentevals.Build(nodes, nil)
		assert.Empty(t, msgs)
	})

	t.Run("sort-tiebreaker-by-id", func(t *testing.T) {
		sameTime := mustTime("2024-01-01T00:00:01Z")
		nodes := []*model.Node{
			{ID: "p-z", RunID: "r", Type: model.NodeUserPrompt, TStart: sameTime, Payload: &model.Payload{Input: json.RawMessage(`"second"`)}},
			{ID: "p-a", RunID: "r", Type: model.NodeUserPrompt, TStart: sameTime, Payload: &model.Payload{Input: json.RawMessage(`"first"`)}},
		}
		msgs := agentevals.Build(nodes, nil)
		require.Len(t, msgs, 2)
		assert.Equal(t, "first", msgs[0].Content)
		assert.Equal(t, "second", msgs[1].Content)
	})

	t.Run("nil-tstart-node", func(t *testing.T) {
		nodes := []*model.Node{
			{ID: "p-nil", RunID: "r", Type: model.NodeUserPrompt, Payload: &model.Payload{Input: json.RawMessage(`"hello"`)}},
			{ID: "p-set", RunID: "r", Type: model.NodeUserPrompt, TStart: mustTime("2024-01-01T00:00:01Z"), Payload: &model.Payload{Input: json.RawMessage(`"world"`)}},
		}
		msgs := agentevals.Build(nodes, nil)
		require.Len(t, msgs, 2)
	})
}

func TestBuildRedactsSecret(t *testing.T) {
	nodes := []*model.Node{
		{
			ID:    "p1",
			RunID: "r",
			Type:  model.NodeUserPrompt,
			Payload: &model.Payload{
				Input: json.RawMessage(`"my key is AKIAIOSFODNN7EXAMPLE ok"`),
			},
		},
	}
	msgs := agentevals.Build(nodes, nil)
	require.Len(t, msgs, 1)
	assert.NotContains(t, msgs[0].Content, "AKIAIOSFODNN7EXAMPLE")
	assert.Contains(t, msgs[0].Content, "redacted:aws-key")
}

func TestWriteAllArrayOfArrays(t *testing.T) {
	t1 := mustTime("2024-01-01T00:00:01Z")
	nodes := []*model.Node{
		{ID: "p-b", RunID: "runB", Type: model.NodeUserPrompt, TStart: t1, Payload: &model.Payload{Input: json.RawMessage(`"from B"`)}},
		{ID: "p-a", RunID: "runA", Type: model.NodeUserPrompt, TStart: t1, Payload: &model.Payload{Input: json.RawMessage(`"from A"`)}},
	}
	var buf strings.Builder
	require.NoError(t, agentevals.WriteAll(&buf, nodes, nil))

	var result [][]agentevals.Message
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &result))
	require.Len(t, result, 2)
	require.Len(t, result[0], 1)
	assert.Equal(t, "from A", result[0][0].Content)
	require.Len(t, result[1], 1)
	assert.Equal(t, "from B", result[1][0].Content)
}

func TestWriteAllEmpty(t *testing.T) {
	var buf strings.Builder
	require.NoError(t, agentevals.WriteAll(&buf, nil, nil))
	assert.Equal(t, "[]\n", buf.String())
}

func TestWriteAllSkippedNodesRunProducesEmptyConversation(t *testing.T) {
	nodes := []*model.Node{
		{ID: "s1", RunID: "run-skip", Type: model.NodeSession},
		{ID: "m1", RunID: "run-skip", Type: model.NodeMarker},
	}
	var buf strings.Builder
	require.NoError(t, agentevals.WriteAll(&buf, nodes, nil))
	var result [][]agentevals.Message
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &result))
	require.Len(t, result, 1)
	assert.Empty(t, result[0])
}

type errWriter struct{}

func (e *errWriter) Write(_ []byte) (int, error) { return 0, errors.New("write fail") }

func TestWriteAllEncodeError(t *testing.T) {
	err := agentevals.WriteAll(&errWriter{}, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agentevals.WriteAll")
}
