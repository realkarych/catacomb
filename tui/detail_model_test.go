package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetailViewRendersMetricsWithDash(t *testing.T) {
	s := NewStyles(true)
	d := newDetailState().withNode(Node{ID: "n1", Type: "tool_call", Name: "Bash", Status: "ok"})
	out := d.view(s, false)
	assert.Contains(t, out, "tool call")
	assert.Contains(t, out, "Bash")
	assert.Contains(t, out, "ok")
	assert.Contains(t, out, "—")
}

func TestDetailViewShowsTokensCostDuration(t *testing.T) {
	s := NewStyles(true)
	in64, out64, cost, dur := int64(100), int64(50), 0.12, int64(1400)
	d := newDetailState().withNode(Node{ID: "n1", Type: "assistant_turn", TokensIn: &in64, TokensOut: &out64, CostUSD: &cost, DurationMS: &dur, Attrs: map[string]any{"cost_source": "reported"}})
	v := d.view(s, false)
	assert.Contains(t, v, "100")
	assert.Contains(t, v, "50")
	assert.Contains(t, v, "$0.12")
	assert.Contains(t, v, "1.4s")
	assert.Contains(t, v, "reported")
}

func TestDetailDebugTogglesIDs(t *testing.T) {
	s := NewStyles(true)
	d := newDetailState().withNode(Node{ID: "node-12345", Type: "tool_call", PayloadHash: "deadbeefcafe"})
	assert.NotContains(t, d.view(s, false), "node-12345")
	assert.Contains(t, d.view(s, true), "node-12345")
}

func TestDetailContentRequestAndDisabled(t *testing.T) {
	d := newDetailState().withNode(Node{ID: "n1", Type: "tool_call"})
	d2, cmd := d.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")}, &fakeClient{}, "s1")
	require.NotNil(t, cmd)
	_ = d2
	d3, _ := d.update(payloadLoadedMsg{err: ErrContentDisabled}, &fakeClient{}, "s1")
	assert.Contains(t, d3.view(NewStyles(true), false), "disabled")
}

func TestDetailContentRendersRedaction(t *testing.T) {
	d := newDetailState().withNode(Node{ID: "n1", Type: "tool_call"})
	d2, _ := d.update(payloadLoadedMsg{view: PayloadView{NodeID: "n1", Input: []byte(`{"a":1}`), Redacted: true, Redactions: []RedactionFinding{{Path: "$.x", Reason: "secret"}}}}, &fakeClient{}, "s1")
	v := d2.view(NewStyles(true), false)
	assert.True(t, strings.Contains(v, "redact") || strings.Contains(v, "Redact"))
}

func TestDetailEmptyNoNode(t *testing.T) {
	out := newDetailState().view(NewStyles(true), false)
	assert.NotEmpty(t, out)
}

func TestDetailViewShowsModel(t *testing.T) {
	s := NewStyles(true)
	d := newDetailState().withNode(Node{ID: "n1", Type: "assistant_turn", Attrs: map[string]any{"model": "claude-3-opus"}})
	v := d.view(s, false)
	assert.Contains(t, v, "claude-3-opus")
}

func TestDetailViewShowsModelDashWhenMissing(t *testing.T) {
	s := NewStyles(true)
	d := newDetailState().withNode(Node{ID: "n1", Type: "tool_call"})
	v := d.view(s, false)
	assert.Contains(t, v, "—")
}

func TestDetailDebugShowsSourcesAndPayloadHash(t *testing.T) {
	s := NewStyles(true)
	d := newDetailState().withNode(Node{
		ID:          "node-xyz",
		Type:        "tool_call",
		PayloadHash: "deadbeef",
		Sources:     []SourceRef{{Source: "cdc", ObsID: "o1"}},
	})
	v := d.view(s, true)
	assert.Contains(t, v, "node-xyz")
	assert.Contains(t, v, "deadbeef")
	assert.Contains(t, v, "cdc")
}

func TestDetailPayloadLoadedRendersJSON(t *testing.T) {
	d := newDetailState().withNode(Node{ID: "n1", Type: "tool_call"})
	d2, _ := d.update(payloadLoadedMsg{view: PayloadView{NodeID: "n1", Input: []byte(`{"key":"val"}`)}}, &fakeClient{}, "s1")
	v := d2.view(NewStyles(true), false)
	assert.Contains(t, v, "key")
}

func TestDetailUnknownMsgNoOp(t *testing.T) {
	d := newDetailState().withNode(Node{ID: "n1", Type: "tool_call"})
	d2, cmd := d.update("some-random-msg", &fakeClient{}, "s1")
	assert.Nil(t, cmd)
	assert.Equal(t, d, d2)
}

func TestDetailNodeNilNoPayloadCrash(t *testing.T) {
	d := newDetailState()
	d2, cmd := d.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")}, &fakeClient{}, "s1")
	assert.Nil(t, cmd)
	assert.Equal(t, d, d2)
}

func TestDetailPayloadLoadedWithNonDisabledError(t *testing.T) {
	d := newDetailState().withNode(Node{ID: "n1", Type: "tool_call"})
	d2, _ := d.update(payloadLoadedMsg{err: ErrPayloadNotFound}, &fakeClient{}, "other-hash")
	v := d2.view(NewStyles(true), false)
	assert.Contains(t, v, "error")
}

func TestDetailPayloadRendersOutput(t *testing.T) {
	d := newDetailState().withNode(Node{ID: "n1", Type: "tool_call"})
	d2, _ := d.update(payloadLoadedMsg{view: PayloadView{NodeID: "n1", Output: []byte(`{"out":"val"}`)}}, &fakeClient{}, "s1")
	v := d2.view(NewStyles(true), false)
	assert.Contains(t, v, "out")
}

func TestDetailRedactionMultiplePaths(t *testing.T) {
	d := newDetailState().withNode(Node{ID: "n1", Type: "tool_call"})
	d2, _ := d.update(payloadLoadedMsg{view: PayloadView{
		NodeID:     "n1",
		Input:      []byte(`{"a":1}`),
		Redacted:   true,
		Redactions: []RedactionFinding{{Path: "$.x", Reason: "secret"}, {Path: "$.y", Reason: "pii"}},
	}}, &fakeClient{}, "s1")
	v := d2.view(NewStyles(true), false)
	assert.Contains(t, v, "$.x")
	assert.Contains(t, v, "$.y")
}

func TestPrettyJSONInvalidFallsBack(t *testing.T) {
	result := prettyJSON([]byte("not-json{"))
	assert.Equal(t, "not-json{", result)
}
