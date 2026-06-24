package tui

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitNoHashLoadsSessions(t *testing.T) {
	f := &fakeClient{sessions: []SessionSummary{{Session: "s1"}}}
	m := NewModel(t.Context(), f, "", true)
	cmd := m.Init()
	require.NotNil(t, cmd)
	msg := cmd()
	_, ok := msg.(sessionsLoadedMsg)
	assert.True(t, ok)
}

func TestInitWithHashLoadsGraph(t *testing.T) {
	f := &fakeClient{graph: []SseEvent{{Kind: "node_upsert"}}}
	m := NewModel(t.Context(), f, "s1", true)
	msg := m.Init()()
	g, ok := msg.(graphLoadedMsg)
	require.True(t, ok)
	assert.Equal(t, "s1", g.hash)
}

func TestUpdateWindowSize(t *testing.T) {
	m := NewModel(t.Context(), &fakeClient{}, "", true)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	mm := updated.(Model)
	assert.Equal(t, 120, mm.width)
	assert.Equal(t, 40, mm.height)
}

func TestUpdateQuit(t *testing.T) {
	m := NewModel(t.Context(), &fakeClient{}, "", true)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	require.NotNil(t, cmd)
	assert.Equal(t, tea.Quit(), cmd())
}

func TestUpdateCtrlCQuit(t *testing.T) {
	m := NewModel(t.Context(), &fakeClient{}, "", true)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	require.NotNil(t, cmd)
	assert.Equal(t, tea.Quit(), cmd())
}

func TestSessionsLoadedThenSelectLoadsGraph(t *testing.T) {
	f := &fakeClient{sessions: []SessionSummary{{Session: "s1"}}, graph: []SseEvent{{Kind: "node_upsert"}}}
	m := NewModel(t.Context(), f, "", true)
	u, _ := m.Update(sessionsLoadedMsg{sessions: f.sessions})
	m = u.(Model)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	g, ok := cmd().(graphLoadedMsg)
	require.True(t, ok)
	assert.Equal(t, "s1", g.hash)
}

func TestGraphLoadedSeedsTreeAndSubscribes(t *testing.T) {
	ch := make(chan StreamMsg, 1)
	f := &fakeClient{ch: ch}
	m := NewModel(t.Context(), f, "s1", true)
	u, cmd := m.Update(graphLoadedMsg{hash: "s1", events: []SseEvent{nodeEv("node_upsert", "s", "session", "running", 1)}})
	m = u.(Model)
	assert.Contains(t, m.View(), "session")
	require.NotNil(t, cmd)
}

func TestStreamReadyMsgStoresCh(t *testing.T) {
	ch := make(chan StreamMsg, 1)
	m := NewModel(t.Context(), &fakeClient{ch: ch}, "s1", true)
	u, cmd := m.Update(streamReadyMsg{ch: ch})
	m = u.(Model)
	assert.NotNil(t, m.subCh)
	require.NotNil(t, cmd)
}

func TestStreamEventAppliesAndReissues(t *testing.T) {
	ch := make(chan StreamMsg, 1)
	m := NewModel(t.Context(), &fakeClient{ch: ch}, "s1", true)
	u, _ := m.Update(graphLoadedMsg{hash: "s1", events: []SseEvent{nodeEv("node_upsert", "s", "session", "running", 1)}})
	m = u.(Model)
	u, _ = m.Update(streamReadyMsg{ch: ch})
	m = u.(Model)
	u2, cmd := m.Update(streamEventMsg{msg: StreamMsg{Event: nodeEv("node_upsert", "a", "tool_call", "ok", 2)}})
	m = u2.(Model)
	require.NotNil(t, cmd)
	assert.Contains(t, m.View(), "session")
}

func TestStreamEventNoChannelNoCmd(t *testing.T) {
	m := NewModel(t.Context(), &fakeClient{}, "s1", true)
	u, _ := m.Update(graphLoadedMsg{hash: "s1", events: []SseEvent{nodeEv("node_upsert", "s", "session", "running", 1)}})
	m = u.(Model)
	m.subCh = nil
	u2, cmd := m.Update(streamEventMsg{msg: StreamMsg{Event: nodeEv("node_status", "s", "", "ok", 3)}})
	_ = u2.(Model)
	assert.Nil(t, cmd)
}

func TestStreamClosedMsg(t *testing.T) {
	m := NewModel(t.Context(), &fakeClient{}, "s1", true)
	u, cmd := m.Update(streamClosedMsg{})
	_ = u.(Model)
	assert.Nil(t, cmd)
}

func TestErrMsgRenders(t *testing.T) {
	m := NewModel(t.Context(), &fakeClient{}, "", true)
	u, _ := m.Update(errMsg{err: ErrDaemonRestarted})
	assert.Contains(t, u.(Model).View(), "restarted")
}

func TestErrMsgUnreachableRenders(t *testing.T) {
	m := NewModel(t.Context(), &fakeClient{}, "", true)
	u, _ := m.Update(errMsg{err: ErrDaemonUnreachable})
	assert.Contains(t, u.(Model).View(), "unreachable")
}

func TestErrMsgGenericRenders(t *testing.T) {
	m := NewModel(t.Context(), &fakeClient{}, "", true)
	u, _ := m.Update(errMsg{err: ErrSessionNotFound})
	assert.Contains(t, u.(Model).View(), "error:")
}

func TestTabCyclesFocusAndDebugToggle(t *testing.T) {
	m := NewModel(t.Context(), &fakeClient{}, "", true)
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	assert.NotEqual(t, m.focus, u.(Model).focus)
	u2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	assert.True(t, u2.(Model).debug)
}

func TestTabCyclesAllFoci(t *testing.T) {
	m := NewModel(t.Context(), &fakeClient{}, "", true)
	assert.Equal(t, focusList, m.focus)
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = u.(Model)
	assert.Equal(t, focusTree, m.focus)
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = u.(Model)
	assert.Equal(t, focusDetail, m.focus)
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = u.(Model)
	assert.Equal(t, focusList, m.focus)
}

func TestViewLoadingAndEmpty(t *testing.T) {
	m := NewModel(t.Context(), &fakeClient{}, "", true)
	assert.NotEmpty(t, m.View())
	u, _ := m.Update(sessionsLoadedMsg{sessions: nil})
	assert.NotEmpty(t, u.(Model).View())
}

func TestViewSessionFocusLayout(t *testing.T) {
	f := &fakeClient{sessions: []SessionSummary{sess("s1")}}
	m := NewModel(t.Context(), f, "", true)
	u, _ := m.Update(sessionsLoadedMsg{sessions: f.sessions})
	m = u.(Model)
	m.width = 200
	m.height = 40
	v := m.View()
	assert.NotEmpty(t, v)
}

func TestViewTreeFocusLayout(t *testing.T) {
	ch := make(chan StreamMsg, 1)
	f := &fakeClient{ch: ch}
	m := NewModel(t.Context(), f, "s1", true)
	u, _ := m.Update(graphLoadedMsg{hash: "s1", events: []SseEvent{nodeEv("node_upsert", "s", "session", "running", 1)}})
	m = u.(Model)
	m.width = 200
	m.height = 40
	v := m.View()
	assert.NotEmpty(t, v)
}

func TestTreeEscGoesBackToList(t *testing.T) {
	m := NewModel(t.Context(), &fakeClient{}, "", true)
	u, _ := m.Update(sessionsLoadedMsg{sessions: []SessionSummary{sess("s1")}})
	m = u.(Model)
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = u.(Model)
	assert.Equal(t, focusTree, m.focus)
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = u.(Model)
	assert.Equal(t, focusList, m.focus)
}

func TestDetailEscGoesBackToTree(t *testing.T) {
	m := NewModel(t.Context(), &fakeClient{}, "", true)
	m.focus = focusDetail
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = u.(Model)
	assert.Equal(t, focusTree, m.focus)
}

func TestDetailKeyDelegated(t *testing.T) {
	m := NewModel(t.Context(), &fakeClient{}, "", true)
	m.focus = focusDetail
	m.detail = m.detail.withNode(Node{ID: "n1", Type: "tool_call"})
	u, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	_ = u.(Model)
	require.NotNil(t, cmd)
}

func TestPayloadLoadedDelegated(t *testing.T) {
	m := NewModel(t.Context(), &fakeClient{}, "", true)
	m.detail = m.detail.withNode(Node{ID: "n1", Type: "tool_call"})
	u, _ := m.Update(payloadLoadedMsg{err: ErrContentDisabled})
	m = u.(Model)
	assert.True(t, m.detail.disabled)
}

func TestStreamEventUpdatesDetailNode(t *testing.T) {
	ch := make(chan StreamMsg, 1)
	m := NewModel(t.Context(), &fakeClient{ch: ch}, "s1", true)
	u, _ := m.Update(graphLoadedMsg{hash: "s1", events: []SseEvent{nodeEv("node_upsert", "s", "session", "running", 1)}})
	m = u.(Model)
	m.detail = m.detail.withNode(m.tree.graph.Nodes["s"])
	u, _ = m.Update(streamReadyMsg{ch: ch})
	m = u.(Model)
	u, _ = m.Update(streamEventMsg{msg: StreamMsg{Event: nodeEv("node_status", "s", "", "ok", 5)}})
	m = u.(Model)
	require.NotNil(t, m.detail.node)
	assert.Equal(t, "ok", m.detail.node.Status)
}

func TestTreeEnterSelectsNodeAndFocusesDetail(t *testing.T) {
	ch := make(chan StreamMsg, 1)
	f := &fakeClient{ch: ch}
	m := NewModel(t.Context(), f, "s1", true)
	u, _ := m.Update(graphLoadedMsg{hash: "s1", events: []SseEvent{
		nodeEv("node_upsert", "s", "session", "running", 1),
		nodeEv("node_upsert", "a", "tool_call", "ok", 2),
		{Kind: "edge_upsert", Rev: 3, Edge: &Edge{ID: "e1", Type: "parent_child", Src: "s", Dst: "a", Rev: 3}},
	}})
	m = u.(Model)
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = u.(Model)
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = u.(Model)
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = u.(Model)
	assert.Equal(t, focusDetail, m.focus)
	require.NotNil(t, m.detail.node)
	assert.Equal(t, "a", m.detail.node.ID)
}

func TestUnknownMsgNoOp(t *testing.T) {
	m := NewModel(t.Context(), &fakeClient{}, "", true)
	u, cmd := m.Update("some-unknown-msg")
	assert.Equal(t, m.focus, u.(Model).focus)
	assert.Nil(t, cmd)
}

func TestMaxRev(t *testing.T) {
	assert.Equal(t, uint64(0), maxRev(nil))
	assert.Equal(t, uint64(5), maxRev([]SseEvent{{Rev: 3}, {Rev: 5}, {Rev: 1}}))
}

func TestNextFocus(t *testing.T) {
	assert.Equal(t, focusTree, nextFocus(focusList))
	assert.Equal(t, focusDetail, nextFocus(focusTree))
	assert.Equal(t, focusList, nextFocus(focusDetail))
}

func TestRenderTUIErrNil(t *testing.T) {
	assert.Equal(t, "", renderTUIErr(nil))
}

func TestViewWithWidthZero(t *testing.T) {
	ch := make(chan StreamMsg, 1)
	f := &fakeClient{ch: ch}
	m := NewModel(t.Context(), f, "s1", true)
	u, _ := m.Update(graphLoadedMsg{hash: "s1", events: []SseEvent{nodeEv("node_upsert", "s", "session", "running", 1)}})
	m = u.(Model)
	m.width = 0
	v := m.View()
	assert.NotEmpty(t, v)
}

func TestDebugToggleOff(t *testing.T) {
	m := NewModel(t.Context(), &fakeClient{}, "", true)
	m.debug = true
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	assert.False(t, u.(Model).debug)
}

func TestSessionListKeyDelegatedWhenFocusList(t *testing.T) {
	f := &fakeClient{sessions: []SessionSummary{sess("s1"), sess("s2")}}
	m := NewModel(t.Context(), f, "", true)
	u, _ := m.Update(sessionsLoadedMsg{sessions: f.sessions})
	m = u.(Model)
	assert.Equal(t, focusList, m.focus)
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = u.(Model)
	assert.Equal(t, 1, m.sessions.cursor)
}

func TestTreeKeyDelegatedWhenFocusTree(t *testing.T) {
	ch := make(chan StreamMsg, 1)
	f := &fakeClient{ch: ch}
	m := NewModel(t.Context(), f, "s1", true)
	u, _ := m.Update(graphLoadedMsg{hash: "s1", events: []SseEvent{
		nodeEv("node_upsert", "s", "session", "running", 1),
		nodeEv("node_upsert", "a", "tool_call", "ok", 2),
		{Kind: "edge_upsert", Rev: 3, Edge: &Edge{ID: "e1", Type: "parent_child", Src: "s", Dst: "a", Rev: 3}},
	}})
	m = u.(Model)
	assert.Equal(t, focusTree, m.focus)
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = u.(Model)
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = u.(Model)
	assert.Equal(t, 1, m.tree.cursor)
}

func TestInitContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := NewModel(ctx, &fakeClient{}, "", true)
	assert.NotNil(t, m.ctx)
}
