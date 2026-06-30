package evalview

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
)

func mustTime(s string) *time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return &t
}

func int64Ptr(v int64) *int64 { return &v }

func sampleNodes() []*model.Node {
	return []*model.Node{
		{
			ID:     "session-1",
			RunID:  "run-1",
			Type:   model.NodeSession,
			Status: model.StatusRunning,
			TStart: mustTime("2024-01-01T00:00:01Z"),
		},
		{
			ID:     "prompt-1",
			RunID:  "run-1",
			Type:   model.NodeUserPrompt,
			Status: model.StatusOK,
			TStart: mustTime("2024-01-01T00:00:02Z"),
		},
		{
			ID:         "turn-1",
			RunID:      "run-1",
			Type:       model.NodeAssistantTurn,
			Status:     model.StatusOK,
			TStart:     mustTime("2024-01-01T00:00:03Z"),
			DurationMS: int64Ptr(100),
			TokensIn:   int64Ptr(100),
			TokensOut:  int64Ptr(50),
			Attrs:      map[string]any{"model": "claude-3-haiku"},
		},
		{
			ID:     "tool-bash-1",
			RunID:  "run-1",
			Name:   "Bash",
			Type:   model.NodeToolCall,
			Status: model.StatusOK,
			TStart: mustTime("2024-01-01T00:00:04Z"),
			TEnd:   mustTime("2024-01-01T00:00:05Z"),
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
			Status: model.StatusError,
			TStart: mustTime("2024-01-01T00:00:06Z"),
			Payload: &model.Payload{
				Input:  json.RawMessage(`{"path":"x"}`),
				Output: json.RawMessage(`"boom"`),
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

func TestWriteTraceGolden(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, writeTrace(&buf, "run-1", sampleNodes(), sampleEdges()))

	golden, err := os.ReadFile("testdata/trace.golden.jsonl")
	require.NoError(t, err)

	assert.Equal(t, string(golden), buf.String())
}

func TestWriteTraceShape(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, writeTrace(&buf, "run-1", sampleNodes(), sampleEdges()))

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	require.Len(t, lines, 7)

	var first map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &first))
	assert.Equal(t, "trace_start", first["type"])
	assert.Equal(t, "1.0", first["trace_spec_version"])

	var last map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[6]), &last))
	assert.Equal(t, "trace_end", last["type"])
}

func TestSpanTypeMapping(t *testing.T) {
	tests := []struct {
		nodeType model.NodeType
		want     string
	}{
		{model.NodeAssistantTurn, "llm"},
		{model.NodeToolCall, "tool"},
		{model.NodeMCPCall, "mcp"},
		{model.NodeSkill, "skill"},
		{model.NodeUserPrompt, "agent"},
		{model.NodeSession, "agent"},
	}
	for _, tc := range tests {
		n := &model.Node{Type: tc.nodeType}
		assert.Equal(t, tc.want, spanType(n), "nodeType=%s", tc.nodeType)
	}
}

func TestLatencyAndAttrFallbacks(t *testing.T) {
	t.Run("nil-everything", func(t *testing.T) {
		n := &model.Node{}
		assert.Nil(t, latency(n))
	})

	t.Run("duration-ms", func(t *testing.T) {
		v := int64(42)
		n := &model.Node{DurationMS: &v}
		l := latency(n)
		require.NotNil(t, l)
		assert.Equal(t, 42.0, *l)
	})

	t.Run("tstart-tend", func(t *testing.T) {
		ts := mustTime("2024-01-01T00:00:00Z")
		te := mustTime("2024-01-01T00:00:02Z")
		n := &model.Node{TStart: ts, TEnd: te}
		l := latency(n)
		require.NotNil(t, l)
		assert.Equal(t, 2000.0, *l)
	})

	t.Run("attr-nil-attrs", func(t *testing.T) {
		n := &model.Node{}
		assert.Equal(t, "", attrString(n, "model"))
	})

	t.Run("attr-missing-key", func(t *testing.T) {
		n := &model.Node{Attrs: map[string]any{"model": "claude"}}
		assert.Equal(t, "", attrString(n, "provider"))
	})

	t.Run("attr-non-string", func(t *testing.T) {
		n := &model.Node{Attrs: map[string]any{"model": 42}}
		assert.Equal(t, "", attrString(n, "model"))
	})

	t.Run("attr-string", func(t *testing.T) {
		n := &model.Node{Attrs: map[string]any{"model": "claude-3"}}
		assert.Equal(t, "claude-3", attrString(n, "model"))
	})

	t.Run("redacted-len-nil", func(t *testing.T) {
		assert.Equal(t, 0, redactedLen(nil))
	})

	t.Run("redacted-len-empty", func(t *testing.T) {
		assert.Equal(t, 0, redactedLen([]byte{}))
	})

	t.Run("redacted-len-content", func(t *testing.T) {
		assert.Equal(t, 4, redactedLen([]byte("test")))
	})
}

func TestSpanToolNilPayload(t *testing.T) {
	nodes := []*model.Node{
		{ID: "tool-nil", RunID: "run-1", Name: "Bash", Type: model.NodeToolCall, Status: model.StatusOK},
	}
	var buf bytes.Buffer
	require.NoError(t, writeTrace(&buf, "run-1", nodes, nil))
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	require.Len(t, lines, 3)
	var spanLine map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[1]), &spanLine))
	tool, ok := spanLine["tool"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, float64(0), tool["tool_args_bytes"])
	assert.Equal(t, float64(0), tool["tool_result_bytes"])
}

func TestWriteTraceRedactsToBytes(t *testing.T) {
	nodes := []*model.Node{
		{
			ID:     "tool-1",
			RunID:  "run-1",
			Name:   "Bash",
			Type:   model.NodeToolCall,
			Status: model.StatusOK,
			Payload: &model.Payload{
				Input: json.RawMessage(`"AKIAIOSFODNN7EXAMPLE"`),
			},
		},
	}
	var buf bytes.Buffer
	require.NoError(t, writeTrace(&buf, "run-1", nodes, nil))
	assert.NotContains(t, buf.String(), "AKIAIOSFODNN7EXAMPLE")
}

func TestWriteAllConcatenatesTracesSorted(t *testing.T) {
	t1 := mustTime("2024-01-01T00:00:01Z")
	nodes := []*model.Node{
		{ID: "n-b", RunID: "runB", Type: model.NodeSession, TStart: t1},
		{ID: "n-a", RunID: "runA", Type: model.NodeSession, TStart: t1},
	}
	var buf bytes.Buffer
	require.NoError(t, WriteAll(&buf, nodes, nil))

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	require.GreaterOrEqual(t, len(lines), 2)

	var first map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &first))
	assert.Equal(t, "runA", first["trace_id"])
}

type countWriter struct {
	calls  int
	failAt int
}

func (c *countWriter) Write(p []byte) (int, error) {
	c.calls++
	if c.calls >= c.failAt {
		return 0, errors.New("write fail")
	}
	return len(p), nil
}

func TestWriteAllEncodeErrors(t *testing.T) {
	for _, failAt := range []int{1, 2, 7} {
		cw := &countWriter{failAt: failAt}
		err := WriteAll(cw, sampleNodes(), sampleEdges())
		require.Error(t, err, "failAt=%d", failAt)
		assert.Contains(t, err.Error(), "evalview.writeTrace", "failAt=%d", failAt)
	}
}
