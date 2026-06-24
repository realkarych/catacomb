package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seededTree() treeState {
	evs := []SseEvent{
		nodeEv("node_upsert", "s", "session", "running", 1),
		nodeEv("node_upsert", "a", "tool_call", "ok", 2),
		{Kind: "edge_upsert", Rev: 3, Edge: &Edge{ID: "e1", Type: "parent_child", Src: "s", Dst: "a", Rev: 3}},
	}
	return newTreeState().seed(evs)
}

func TestTreeMoveAndClamp(t *testing.T) {
	ts := seededTree()
	ts, _ = ts.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	assert.Equal(t, 0, ts.cursor)
	ts2, _ := ts.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	assert.Equal(t, 0, ts2.cursor)
}

func TestTreeExpandRevealsChildren(t *testing.T) {
	ts := seededTree()
	v0 := ts.view(NewStyles(true), 10)
	assert.NotContains(t, v0, "tool call")
	ts, _ = ts.update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Contains(t, ts.view(NewStyles(true), 10), "tool call")
}

func TestTreeSelectNodeSignals(t *testing.T) {
	ts := seededTree()
	ts, _ = ts.update(tea.KeyMsg{Type: tea.KeyEnter})
	ts, _ = ts.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	_, sel := ts.update(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, sel)
	assert.Equal(t, "a", sel.ID)
}

func TestTreeLiveDeltaUpdatesInPlace(t *testing.T) {
	ts := seededTree()
	ts, _ = ts.update(tea.KeyMsg{Type: tea.KeyEnter})
	ts = ts.applyEvent(nodeEv("node_status", "a", "", "error", 9))
	assert.Contains(t, ts.view(NewStyles(true), 10), StatusGlyph("error"))
}

func TestTreeViewEmpty(t *testing.T) {
	assert.NotEmpty(t, newTreeState().view(NewStyles(true), 10))
}

func TestTreeMoveDown(t *testing.T) {
	ts := seededTree()
	ts, _ = ts.update(tea.KeyMsg{Type: tea.KeyEnter})
	ts, _ = ts.update(tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, 1, ts.cursor)
}

func TestTreeMoveUp(t *testing.T) {
	ts := seededTree()
	ts, _ = ts.update(tea.KeyMsg{Type: tea.KeyEnter})
	ts, _ = ts.update(tea.KeyMsg{Type: tea.KeyDown})
	ts, _ = ts.update(tea.KeyMsg{Type: tea.KeyUp})
	assert.Equal(t, 0, ts.cursor)
}

func TestTreeCollapseWithH(t *testing.T) {
	ts := seededTree()
	ts, _ = ts.update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Contains(t, ts.view(NewStyles(true), 10), "tool call")
	ts, _ = ts.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	assert.NotContains(t, ts.view(NewStyles(true), 10), "tool call")
}

func TestTreeSpaceExpandCollapse(t *testing.T) {
	ts := seededTree()
	ts, _ = ts.update(tea.KeyMsg{Type: tea.KeySpace})
	assert.Contains(t, ts.view(NewStyles(true), 10), "tool call")
	ts, _ = ts.update(tea.KeyMsg{Type: tea.KeySpace})
	assert.NotContains(t, ts.view(NewStyles(true), 10), "tool call")
}

func TestTreeLExpand(t *testing.T) {
	ts := seededTree()
	ts, _ = ts.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	assert.Contains(t, ts.view(NewStyles(true), 10), "tool call")
}

func TestTreeEscReturnsNilNodeNoSignal(t *testing.T) {
	ts := seededTree()
	_, sel := ts.update(tea.KeyMsg{Type: tea.KeyEsc})
	assert.Nil(t, sel)
}

func TestTreeScrollOffset(t *testing.T) {
	var evs []SseEvent
	evs = append(evs, nodeEv("node_upsert", "root", "session", "ok", 1))
	for i := 0; i < 20; i++ {
		id := strings.Repeat("x", i+1)
		evs = append(evs, nodeEv("node_upsert", id, "tool_call", "ok", uint64(i+2)))
		evs = append(evs, SseEvent{Kind: "edge_upsert", Rev: uint64(100 + i), Edge: &Edge{ID: "e" + id, Type: "parent_child", Src: "root", Dst: id, Rev: uint64(100 + i)}})
	}
	ts := newTreeState().seed(evs)
	ts, _ = ts.update(tea.KeyMsg{Type: tea.KeyEnter})
	for i := 0; i < 15; i++ {
		ts, _ = ts.update(tea.KeyMsg{Type: tea.KeyDown})
	}
	assert.Equal(t, 15, ts.cursor)
	v := ts.view(NewStyles(true), 5)
	assert.NotEmpty(t, v)
}

func TestTreeUnknownMsgNoOp(t *testing.T) {
	ts := seededTree()
	ts2, sel := ts.update("random-msg")
	assert.Nil(t, sel)
	assert.Equal(t, ts.cursor, ts2.cursor)
}

func TestTreeSeedKeepsCursorOnSameID(t *testing.T) {
	ts := seededTree()
	ts, _ = ts.update(tea.KeyMsg{Type: tea.KeyEnter})
	ts, _ = ts.update(tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, 1, ts.cursor)
	ts = ts.applyEvent(nodeEv("node_status", "s", "", "ok", 10))
	assert.Equal(t, 1, ts.cursor)
}

func TestTreeEnterEmptyRowsReturnsNil(t *testing.T) {
	ts := newTreeState()
	_, sel := ts.update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Nil(t, sel)
}

func TestTreeViewZeroHeightDefaultsTen(t *testing.T) {
	ts := seededTree()
	v := ts.view(NewStyles(true), 0)
	assert.NotEmpty(t, v)
}

func TestViewOffset(t *testing.T) {
	assert.Equal(t, 0, viewOffset(0, 10))
	assert.Equal(t, 0, viewOffset(5, 10))
	assert.Equal(t, 1, viewOffset(10, 10))
	assert.Equal(t, 5, viewOffset(14, 10))
	assert.Equal(t, 0, viewOffset(5, 0))
}

func TestTreeScrollOffsetBackwards(t *testing.T) {
	var evs []SseEvent
	evs = append(evs, nodeEv("node_upsert", "root", "session", "ok", 1))
	for i := 0; i < 20; i++ {
		id := strings.Repeat("x", i+1)
		evs = append(evs, nodeEv("node_upsert", id, "tool_call", "ok", uint64(i+2)))
		evs = append(evs, SseEvent{Kind: "edge_upsert", Rev: uint64(100 + i), Edge: &Edge{ID: "e" + id, Type: "parent_child", Src: "root", Dst: id, Rev: uint64(100 + i)}})
	}
	ts := newTreeState().seed(evs)
	ts, _ = ts.update(tea.KeyMsg{Type: tea.KeyEnter})
	for i := 0; i < 15; i++ {
		ts, _ = ts.update(tea.KeyMsg{Type: tea.KeyDown})
	}
	for i := 0; i < 10; i++ {
		ts, _ = ts.update(tea.KeyMsg{Type: tea.KeyUp})
	}
	assert.Equal(t, 5, ts.cursor)
}
