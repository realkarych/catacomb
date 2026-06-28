package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCanonicalIDs(t *testing.T) {
	assert.Equal(t, "session:exec1", SessionNodeID("exec1"))
	assert.Equal(t, "exec1:prompt:u1", UserPromptID("exec1", "u1"))
	assert.Equal(t, "exec1:turn:m1", AssistantTurnID("exec1", "m1"))
	assert.Equal(t, "exec1:tool:toolu_1", ToolCallID("exec1", "toolu_1"))
	assert.Equal(t, "exec1:agent:a1", SubagentID("exec1", "a1"))
	assert.Equal(t, "exec1:parent_child:a>b", EdgeID("exec1", EdgeParentChild, "a", "b"))
}

func TestPhaseMarkerID(t *testing.T) {
	assert.Equal(t, "exec1:marker:foo:0", PhaseMarkerID("exec1", "foo", 0))
	assert.Equal(t, "exec1:marker:bar:3", PhaseMarkerID("exec1", "bar", 3))
}
