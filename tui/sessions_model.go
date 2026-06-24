package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type sessionsState struct {
	all       []SessionSummary
	filtered  []SessionSummary
	cursor    int
	filtering bool
	query     string
}

func newSessionsState() sessionsState {
	return sessionsState{}
}

func (ss sessionsState) withSessions(rows []SessionSummary) sessionsState {
	sorted := make([]SessionSummary, len(rows))
	copy(sorted, rows)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Session < sorted[j].Session
	})
	ss.all = sorted
	ss.query = ""
	ss.filtering = false
	ss.cursor = 0
	ss.filtered = sorted
	return ss
}

func applyFilter(all []SessionSummary, q string) []SessionSummary {
	if q == "" {
		return all
	}
	var out []SessionSummary
	for _, s := range all {
		if strings.Contains(s.Session, q) {
			out = append(out, s)
		}
	}
	return out
}

func (ss *sessionsState) clampCursor() {
	if ss.cursor >= len(ss.filtered) {
		ss.cursor = 0
	}
}

func (ss sessionsState) update(msg tea.Msg) (sessionsState, string) {
	m, ok := msg.(tea.KeyMsg)
	if !ok {
		return ss, ""
	}
	if ss.filtering {
		switch m.Type {
		case tea.KeyEsc:
			ss.filtering = false
			ss.filtered = applyFilter(ss.all, ss.query)
			ss.clampCursor()
		case tea.KeyEnter:
			ss.filtering = false
			ss.filtered = applyFilter(ss.all, ss.query)
			ss.clampCursor()
		case tea.KeyBackspace:
			if len(ss.query) > 0 {
				runes := []rune(ss.query)
				ss.query = string(runes[:len(runes)-1])
				ss.filtered = applyFilter(ss.all, ss.query)
				ss.clampCursor()
			}
		case tea.KeyRunes:
			ss.query += string(m.Runes)
			ss.filtered = applyFilter(ss.all, ss.query)
			ss.clampCursor()
		}
		return ss, ""
	}
	switch {
	case m.Type == tea.KeyRunes && string(m.Runes) == "/":
		ss.filtering = true
	case m.Type == tea.KeyRunes && string(m.Runes) == "j", m.Type == tea.KeyDown:
		if ss.cursor < len(ss.filtered)-1 {
			ss.cursor++
		}
	case m.Type == tea.KeyRunes && string(m.Runes) == "k", m.Type == tea.KeyUp:
		if ss.cursor > 0 {
			ss.cursor--
		}
	case m.Type == tea.KeyEnter:
		if len(ss.filtered) == 0 {
			return ss, ""
		}
		return ss, ss.filtered[ss.cursor].Session
	}
	return ss, ""
}

func (ss sessionsState) view(s Styles, width int) string {
	if len(ss.filtered) == 0 {
		msg := "no sessions"
		if ss.query != "" {
			msg = fmt.Sprintf("no sessions match %q", ss.query)
		}
		return s.Faint.Render(msg)
	}

	_ = width
	var b strings.Builder
	for i, row := range ss.filtered {
		glyph := StatusGlyph(row.Status)
		hash := ShortHash(row.Session, 8)
		nodeCount := fmt.Sprintf("%d", row.NodeCount)
		tIn := Tokens(&row.TokensIn)
		tOut := Tokens(&row.TokensOut)
		cost := Cost(row.CostUSD)
		dur := Duration(row.DurationMS)
		line := fmt.Sprintf("%s %s  nodes:%-4s  tok %s→%s  %s  %s", glyph, hash, nodeCount, tIn, tOut, cost, dur)
		if i == ss.cursor {
			line = s.Selected.Render(line)
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}
