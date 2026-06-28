package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNodeTypeLabel(t *testing.T) {
	cases := map[string]string{
		"session":        "session",
		"user_prompt":    "user prompt",
		"assistant_turn": "assistant turn",
		"tool_call":      "tool call",
		"subagent":       "subagent",
		"mcp_call":       "mcp call",
		"hook_event":     "hook event",
		"marker":         "marker",
		"something_else": "marker",
	}
	for in, want := range cases {
		assert.Equal(t, want, NodeTypeLabel(in), "NodeTypeLabel(%q)", in)
	}
}

func TestProvenance(t *testing.T) {
	assert.Equal(t, "unknown", Provenance(Node{}))
	c := 0.5
	assert.Equal(t, "estimated", Provenance(Node{CostUSD: &c}))
	assert.Equal(t, "reported", Provenance(Node{CostUSD: &c, Attrs: map[string]any{"cost_source": "reported"}}))
	assert.Equal(t, "estimated", Provenance(Node{CostUSD: &c, Attrs: map[string]any{"cost_source": "estimated"}}))
	assert.Equal(t, "estimated", Provenance(Node{CostUSD: &c, Attrs: map[string]any{}}))
	assert.Equal(t, "estimated", Provenance(Node{CostUSD: &c, Attrs: map[string]any{"cost_source": 42}}))
}

func TestStatusLabel(t *testing.T) {
	assert.Equal(t, "finished", StatusLabel("ok"))
	assert.Equal(t, "live", StatusLabel("live"))
	assert.Equal(t, "running", StatusLabel("running"))
	assert.Equal(t, "error", StatusLabel("error"))
	assert.Equal(t, "blocked", StatusLabel("blocked"))
	assert.Equal(t, "—", StatusLabel(""))
}

func TestStatusGlyph(t *testing.T) {
	assert.NotEmpty(t, StatusGlyph("ok"))
	assert.NotEmpty(t, StatusGlyph("live"))
	assert.NotEmpty(t, StatusGlyph("running"))
	assert.NotEmpty(t, StatusGlyph("error"))
	assert.NotEmpty(t, StatusGlyph("pending"))
	assert.NotEmpty(t, StatusGlyph("blocked"))
	assert.NotEmpty(t, StatusGlyph("cancelled"))
	assert.NotEmpty(t, StatusGlyph("unknown-status"))
	assert.Equal(t, "◐", StatusGlyph("live"))
	assert.Equal(t, "·", StatusGlyph(""))
}
