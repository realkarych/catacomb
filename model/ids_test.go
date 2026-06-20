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
