package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type treeState struct {
	graph    Graph
	expanded map[string]bool
	cursor   int
}

func newTreeState() treeState {
	return treeState{
		graph:    EmptyGraph(),
		expanded: make(map[string]bool),
	}
}

func viewOffset(cursor, height int) int {
	if height <= 0 {
		return 0
	}
	if cursor < height {
		return 0
	}
	return cursor - height + 1
}

func (ts treeState) seed(evs []SseEvent) treeState {
	g := EmptyGraph()
	for _, ev := range evs {
		Apply(&g, ev)
	}
	ts.graph = g
	ts.cursor = 0
	return ts
}

func (ts treeState) applyEvent(ev SseEvent) treeState {
	rows := Flatten(ts.graph, ts.expanded)
	var curID string
	if ts.cursor < len(rows) {
		curID = rows[ts.cursor].Node.ID
	}
	Apply(&ts.graph, ev)
	if curID != "" {
		newRows := Flatten(ts.graph, ts.expanded)
		for i, r := range newRows {
			if r.Node.ID == curID {
				ts.cursor = i
				break
			}
		}
	}
	return ts
}

func (ts treeState) update(msg tea.Msg) (treeState, *Node) {
	rows := Flatten(ts.graph, ts.expanded)
	m, ok := msg.(tea.KeyMsg)
	if !ok {
		return ts, nil
	}
	switch {
	case m.Type == tea.KeyRunes && string(m.Runes) == "j", m.Type == tea.KeyDown:
		if ts.cursor < len(rows)-1 {
			ts.cursor++
		}
	case m.Type == tea.KeyRunes && string(m.Runes) == "k", m.Type == tea.KeyUp:
		if ts.cursor > 0 {
			ts.cursor--
		}
	case m.Type == tea.KeyRunes && string(m.Runes) == "h":
		if ts.cursor < len(rows) {
			id := rows[ts.cursor].Node.ID
			delete(ts.expanded, id)
		}
	case m.Type == tea.KeyRunes && string(m.Runes) == "l":
		if ts.cursor < len(rows) {
			row := rows[ts.cursor]
			if row.HasKids {
				ts.expanded[row.Node.ID] = true
			}
		}
	case m.Type == tea.KeySpace:
		if ts.cursor < len(rows) {
			row := rows[ts.cursor]
			if row.HasKids {
				if ts.expanded[row.Node.ID] {
					delete(ts.expanded, row.Node.ID)
				} else {
					ts.expanded[row.Node.ID] = true
				}
			}
		}
	case m.Type == tea.KeyEnter:
		if ts.cursor >= len(rows) {
			return ts, nil
		}
		row := rows[ts.cursor]
		if row.HasKids && !ts.expanded[row.Node.ID] {
			ts.expanded[row.Node.ID] = true
		} else {
			n := row.Node
			return ts, &n
		}
	case m.Type == tea.KeyEsc:
		return ts, nil
	}
	return ts, nil
}

func (ts treeState) view(s Styles, height int) string {
	rows := Flatten(ts.graph, ts.expanded)
	if len(rows) == 0 {
		return s.Faint.Render("no nodes")
	}

	if height < 1 {
		height = 10
	}

	offset := viewOffset(ts.cursor, height)
	end := offset + height
	if end > len(rows) {
		end = len(rows)
	}

	var b strings.Builder
	for i := offset; i < end; i++ {
		row := rows[i]
		indent := strings.Repeat("  ", row.Depth)
		var icon string
		if row.HasKids {
			if ts.expanded[row.Node.ID] {
				icon = "▾ "
			} else {
				icon = "▸ "
			}
		} else {
			icon = "  "
		}
		glyph := StatusGlyph(row.Node.Status)
		label := row.Node.Name
		if label == "" {
			label = NodeTypeLabel(row.Node.Type)
		}
		line := fmt.Sprintf("%s%s%s %s", indent, icon, glyph, label)
		if i == ts.cursor {
			line = s.Selected.Render(line)
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}
