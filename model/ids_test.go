package model

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPromptUUIDIsPrefixedSHA256OfSessionNULContent(t *testing.T) {
	sum := sha256.Sum256([]byte("s1\x00list files"))
	assert.Equal(t, "pc-"+hex.EncodeToString(sum[:]), PromptUUID("s1", "list files"))
}

func TestPromptUUIDTrimsSurroundingWhitespaceOnly(t *testing.T) {
	a := PromptUUID("s1", "list files")
	assert.Equal(t, a, PromptUUID("s1", "  list files\n"))
	assert.NotEqual(t, a, PromptUUID("s1", "list  files"))
}

func TestPromptUUIDDistinguishesContentAndSession(t *testing.T) {
	a := PromptUUID("s1", "list files")
	assert.NotEqual(t, a, PromptUUID("s1", "list dirs"))
	assert.NotEqual(t, a, PromptUUID("s2", "list files"))
}

func TestPromptUUIDSeparatorPreventsSessionContentBoundaryCollision(t *testing.T) {
	assert.NotEqual(t, PromptUUID("s1x", "y"), PromptUUID("s1", "xy"))
	assert.NotEqual(t, PromptUUID("", "s1list files"), PromptUUID("s1", "list files"))
}

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
