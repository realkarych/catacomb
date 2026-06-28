package model

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNodeSourceKey(t *testing.T) {
	tests := []struct {
		name   string
		nodeID string
		want   string
	}{
		{"session node", "session:exec1", "exec1"},
		{"tool call", "exec1:tool:t1", "t1"},
		{"assistant turn", "exec1:turn:msg1", "msg1"},
		{"user prompt", "exec1:prompt:uuid1", "uuid1"},
		{"subagent", "exec1:agent:ag1", "ag1"},
		{"marker", "exec1:marker:m1", "m1"},
		{"empty", "", ""},
		{"no colon", "nocoion", ""},
		{"only one colon", "exec1:onlyone", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, NodeSourceKey(tc.nodeID))
		})
	}
}

func TestSetAnnotationFlatUnionAndLWW(t *testing.T) {
	result := SetAnnotation(nil, "eval", "score", json.RawMessage(`"9"`))
	require.NotNil(t, result)
	assert.Equal(t, json.RawMessage(`"9"`), result["eval.score"])

	result = SetAnnotation(result, "other", "score", json.RawMessage(`"2"`))
	assert.Len(t, result, 2)

	result = SetAnnotation(result, "eval", "score", json.RawMessage(`"10"`))
	assert.Len(t, result, 2)
	assert.Equal(t, json.RawMessage(`"10"`), result["eval.score"])
}
