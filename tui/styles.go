package tui

import "github.com/charmbracelet/lipgloss"

type Styles struct {
	Pane          lipgloss.Style
	Selected      lipgloss.Style
	Faint         lipgloss.Style
	StatusOK      lipgloss.Style
	StatusRunning lipgloss.Style
	StatusError   lipgloss.Style
	Header        lipgloss.Style
	Bold          lipgloss.Style
}

func NewStyles(noColor bool) Styles {
	if noColor {
		return Styles{
			Pane:          lipgloss.NewStyle(),
			Selected:      lipgloss.NewStyle(),
			Faint:         lipgloss.NewStyle(),
			StatusOK:      lipgloss.NewStyle(),
			StatusRunning: lipgloss.NewStyle(),
			StatusError:   lipgloss.NewStyle(),
			Header:        lipgloss.NewStyle(),
			Bold:          lipgloss.NewStyle(),
		}
	}
	return Styles{
		Pane:          lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("240")),
		Selected:      lipgloss.NewStyle().Background(lipgloss.Color("236")).Foreground(lipgloss.Color("255")).Bold(true),
		Faint:         lipgloss.NewStyle().Foreground(lipgloss.Color("240")),
		StatusOK:      lipgloss.NewStyle().Foreground(lipgloss.Color("40")),
		StatusRunning: lipgloss.NewStyle().Foreground(lipgloss.Color("220")),
		StatusError:   lipgloss.NewStyle().Foreground(lipgloss.Color("196")),
		Header:        lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("255")),
		Bold:          lipgloss.NewStyle().Bold(true),
	}
}
