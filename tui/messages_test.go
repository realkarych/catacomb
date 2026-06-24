package tui

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakeClient struct {
	sessions []SessionSummary
	graph    []SseEvent
	ch       chan StreamMsg
	payload  PayloadView
	err      error
}

func (f *fakeClient) Sessions(context.Context) ([]SessionSummary, error) { return f.sessions, f.err }
func (f *fakeClient) Graph(context.Context, string) ([]SseEvent, error)   { return f.graph, f.err }
func (f *fakeClient) Subscribe(context.Context, string, uint64) (<-chan StreamMsg, error) {
	return f.ch, f.err
}
func (f *fakeClient) Payload(context.Context, string, string) (PayloadView, error) {
	return f.payload, f.err
}

func TestLoadSessionsCmd(t *testing.T) {
	want := []SessionSummary{{Session: "s1", Status: "ok"}}
	fc := &fakeClient{sessions: want}
	msg := loadSessionsCmd(fc)()
	got, ok := msg.(sessionsLoadedMsg)
	require.True(t, ok)
	require.Equal(t, want, got.sessions)
}

func TestLoadSessionsCmdError(t *testing.T) {
	fc := &fakeClient{err: ErrDaemonUnreachable}
	msg := loadSessionsCmd(fc)()
	got, ok := msg.(errMsg)
	require.True(t, ok)
	require.ErrorIs(t, got.err, ErrDaemonUnreachable)
}

func TestLoadGraphCmd(t *testing.T) {
	evs := []SseEvent{{Kind: "node_upsert", Rev: 1}}
	fc := &fakeClient{graph: evs}
	msg := loadGraphCmd(fc, "myhash")()
	got, ok := msg.(graphLoadedMsg)
	require.True(t, ok)
	require.Equal(t, "myhash", got.hash)
	require.Equal(t, evs, got.events)
}

func TestLoadGraphCmdError(t *testing.T) {
	fc := &fakeClient{err: ErrSessionNotFound}
	msg := loadGraphCmd(fc, "h")()
	got, ok := msg.(errMsg)
	require.True(t, ok)
	require.ErrorIs(t, got.err, ErrSessionNotFound)
}

func TestSubscribeCmd(t *testing.T) {
	ch := make(chan StreamMsg, 1)
	fc := &fakeClient{ch: ch}
	msg := subscribeCmd(t.Context(), fc, "h", 0)()
	got, ok := msg.(streamReadyMsg)
	require.True(t, ok)
	require.Equal(t, (<-chan StreamMsg)(ch), got.ch)
}

func TestSubscribeCmdError(t *testing.T) {
	fc := &fakeClient{err: ErrDaemonRestarted}
	msg := subscribeCmd(t.Context(), fc, "h", 0)()
	got, ok := msg.(errMsg)
	require.True(t, ok)
	require.ErrorIs(t, got.err, ErrDaemonRestarted)
}

func TestWaitForEventCmd(t *testing.T) {
	ch := make(chan StreamMsg, 1)
	ev := SseEvent{Kind: "node_upsert", Rev: 5}
	ch <- StreamMsg{Event: ev}
	msg := waitForEventCmd(ch)()
	got, ok := msg.(streamEventMsg)
	require.True(t, ok)
	require.Equal(t, ev.Kind, got.msg.Event.Kind)
}

func TestWaitForEventCmdChannelClosed(t *testing.T) {
	ch := make(chan StreamMsg)
	close(ch)
	msg := waitForEventCmd(ch)()
	got, ok := msg.(streamClosedMsg)
	require.True(t, ok)
	require.NoError(t, got.err)
}

func TestWaitForEventCmdDoneMsg(t *testing.T) {
	ch := make(chan StreamMsg, 1)
	ch <- StreamMsg{Done: true}
	msg := waitForEventCmd(ch)()
	got, ok := msg.(streamClosedMsg)
	require.True(t, ok)
	require.NoError(t, got.err)
}

func TestWaitForEventCmdErrMsg(t *testing.T) {
	ch := make(chan StreamMsg, 1)
	someErr := errors.New("stream error")
	ch <- StreamMsg{Err: someErr}
	msg := waitForEventCmd(ch)()
	got, ok := msg.(streamClosedMsg)
	require.True(t, ok)
	require.ErrorIs(t, got.err, someErr)
}

func TestLoadPayloadCmd(t *testing.T) {
	want := PayloadView{NodeID: "n1", Redacted: false}
	fc := &fakeClient{payload: want}
	msg := loadPayloadCmd(fc, "hash", "n1")()
	got, ok := msg.(payloadLoadedMsg)
	require.True(t, ok)
	require.Equal(t, want, got.view)
	require.NoError(t, got.err)
}

func TestLoadPayloadCmdError(t *testing.T) {
	wantErr := ErrPayloadNotFound
	fc := &fakeClient{err: wantErr}
	msg := loadPayloadCmd(fc, "hash", "n1")()
	got, ok := msg.(payloadLoadedMsg)
	require.True(t, ok)
	require.ErrorIs(t, got.err, wantErr)
}
