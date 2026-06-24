package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type focus int

const (
	focusList focus = iota
	focusTree
	focusDetail
)

type Model struct {
	ctx      context.Context
	client   Client
	hash     string
	focus    focus
	width    int
	height   int
	debug    bool
	styles   Styles
	loading  bool
	err      error
	sessions sessionsState
	tree     treeState
	detail   detailState
	subCh    <-chan StreamMsg
}

func NewModel(ctx context.Context, client Client, hash string, noColor bool) Model {
	return Model{
		ctx:     ctx,
		client:  client,
		hash:    hash,
		styles:  NewStyles(noColor),
		loading: true,
		focus:   focusList,
		tree:    newTreeState(),
		detail:  newDetailState(),
	}
}

func (m Model) Init() tea.Cmd {
	if m.hash != "" {
		return loadGraphCmd(m.client, m.hash)
	}
	return loadSessionsCmd(m.client)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = v.Width
		m.height = v.Height
		return m, nil

	case tea.KeyMsg:
		switch {
		case v.Type == tea.KeyRunes && string(v.Runes) == "q":
			return m, tea.Quit
		case v.Type == tea.KeyCtrlC:
			return m, tea.Quit
		case v.Type == tea.KeyRunes && string(v.Runes) == "d":
			m.debug = !m.debug
			return m, nil
		case v.Type == tea.KeyTab:
			m.focus = nextFocus(m.focus)
			return m, nil
		default:
			return m.delegateKey(v)
		}

	case sessionsLoadedMsg:
		m.sessions = m.sessions.withSessions(v.sessions)
		m.loading = false
		return m, nil

	case graphLoadedMsg:
		m.hash = v.hash
		m.tree = m.tree.seed(v.events)
		m.focus = focusTree
		m.loading = false
		lastRev := maxRev(v.events)
		return m, subscribeCmd(m.ctx, m.client, v.hash, lastRev)

	case streamReadyMsg:
		m.subCh = v.ch
		return m, waitForEventCmd(v.ch)

	case streamEventMsg:
		m.tree = m.tree.applyEvent(v.msg.Event)
		if m.detail.node != nil {
			if n, ok := m.tree.graph.Nodes[m.detail.node.ID]; ok {
				m.detail = m.detail.withNode(n)
			}
		}
		var cmd tea.Cmd
		if m.subCh != nil {
			cmd = waitForEventCmd(m.subCh)
		}
		return m, cmd

	case streamClosedMsg:
		return m, nil

	case payloadLoadedMsg:
		var cmd tea.Cmd
		m.detail, cmd = m.detail.update(msg, m.client, m.hash)
		return m, cmd

	case errMsg:
		m.err = v.err
		m.loading = false
		return m, nil
	}
	return m, nil
}

func nextFocus(f focus) focus {
	switch f {
	case focusList:
		return focusTree
	case focusTree:
		return focusDetail
	default:
		return focusList
	}
}

func maxRev(evs []SseEvent) uint64 {
	var max uint64
	for _, ev := range evs {
		if ev.Rev > max {
			max = ev.Rev
		}
	}
	return max
}

func (m Model) delegateKey(v tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.focus {
	case focusList:
		ss, chosen := m.sessions.update(v)
		m.sessions = ss
		if chosen != "" {
			return m, loadGraphCmd(m.client, chosen)
		}

	case focusTree:
		if v.Type == tea.KeyEsc {
			m.focus = focusList
			return m, nil
		}
		ts, sel := m.tree.update(v)
		m.tree = ts
		if sel != nil {
			m.detail = m.detail.withNode(*sel)
			m.focus = focusDetail
		}

	default:
		if v.Type == tea.KeyEsc {
			m.focus = focusTree
			return m, nil
		}
		d, cmd := m.detail.update(v, m.client, m.hash)
		m.detail = d
		return m, cmd
	}
	return m, nil
}

func (m Model) View() string {
	if m.err != nil {
		return renderTUIErr(m.err)
	}
	if m.loading {
		return "loading…"
	}
	if m.focus == focusList {
		return m.sessions.view(m.styles, m.width)
	}
	sessionW := m.width / 4
	if sessionW < 20 {
		sessionW = 20
	}
	sessView := m.sessions.view(m.styles, sessionW)
	treeView := m.tree.view(m.styles, m.height)
	detailView := m.detail.view(m.styles, m.debug)
	return fmt.Sprintf("%s\n%s\n%s",
		strings.TrimRight(sessView, "\n"),
		strings.TrimRight(treeView, "\n"),
		strings.TrimRight(detailView, "\n"),
	)
}

func renderTUIErr(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if strings.Contains(msg, "restarted") {
		return "daemon restarted — re-run catacomb observe"
	}
	if strings.Contains(msg, "unreachable") {
		return "daemon is unreachable — is catacomb up running?"
	}
	return "error: " + msg
}
