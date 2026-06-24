package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
)

type sessionsLoadedMsg struct{ sessions []SessionSummary }

type graphLoadedMsg struct {
	hash   string
	events []SseEvent
}

type streamReadyMsg struct{ ch <-chan StreamMsg }

type streamEventMsg struct{ msg StreamMsg }

type streamClosedMsg struct{ err error }

type payloadLoadedMsg struct {
	view PayloadView
	err  error
}

type errMsg struct{ err error }

func loadSessionsCmd(c Client) tea.Cmd {
	return func() tea.Msg {
		ss, err := c.Sessions(context.Background())
		if err != nil {
			return errMsg{err: err}
		}
		return sessionsLoadedMsg{sessions: ss}
	}
}

func loadGraphCmd(c Client, hash string) tea.Cmd {
	return func() tea.Msg {
		evs, err := c.Graph(context.Background(), hash)
		if err != nil {
			return errMsg{err: err}
		}
		return graphLoadedMsg{hash: hash, events: evs}
	}
}

func subscribeCmd(ctx context.Context, c Client, hash string, sinceRev uint64) tea.Cmd {
	return func() tea.Msg {
		ch, err := c.Subscribe(ctx, hash, sinceRev)
		if err != nil {
			return errMsg{err: err}
		}
		return streamReadyMsg{ch: ch}
	}
}

func waitForEventCmd(ch <-chan StreamMsg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return streamClosedMsg{}
		}
		if msg.Done {
			return streamClosedMsg{}
		}
		if msg.Err != nil {
			return streamClosedMsg{err: msg.Err}
		}
		return streamEventMsg{msg: msg}
	}
}

func loadPayloadCmd(c Client, hash, nodeID string) tea.Cmd {
	return func() tea.Msg {
		view, err := c.Payload(context.Background(), hash, nodeID)
		return payloadLoadedMsg{view: view, err: err}
	}
}
