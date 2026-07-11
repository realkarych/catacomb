package model

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNodeStepKeyJSONTags(t *testing.T) {
	n := Node{ID: "n1", StepKey: "abc123", StepKeyMethod: "heuristic"}
	b, err := json.Marshal(n)
	require.NoError(t, err)
	assert.Contains(t, string(b), `"step_key":"abc123"`)
	assert.Contains(t, string(b), `"step_key_method":"heuristic"`)
	var back Node
	require.NoError(t, json.Unmarshal(b, &back))
	assert.Equal(t, "abc123", back.StepKey)
	assert.Equal(t, "heuristic", back.StepKeyMethod)
}

func TestNodeStepKeyOmitemptyWhenUnset(t *testing.T) {
	b, err := json.Marshal(Node{ID: "n1"})
	require.NoError(t, err)
	assert.NotContains(t, string(b), "step_key")
}
