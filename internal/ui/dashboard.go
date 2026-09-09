package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/idoavrah/entra-tui/internal/graph"
)

// dashboardIndexOf finds a resource's position on the dashboard.
func dashboardIndexOf(kind graph.Kind) int {
	for i, r := range graph.All() {
		if r.Kind == kind {
			return i
		}
	}
	return 0
}

func (m Model) handleDashboardKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	all := graph.All()
	switch {
	case key.Matches(msg, keys.Quit):
		m.quitting = true
		return m, tea.Quit
	case key.Matches(msg, keys.Help):
		m.helpReturn = m.screen
		m.screen = screenHelp
		return m, nil
	case key.Matches(msg, keys.Command):
		return m.openPrompt(modeCommand, "")
	case key.Matches(msg, keys.Up):
		m.dashCursor = clamp(m.dashCursor-1, 0, len(all)-1)
		return m, nil
	case key.Matches(msg, keys.Down):
		m.dashCursor = clamp(m.dashCursor+1, 0, len(all)-1)
		return m, nil
	case key.Matches(msg, keys.Enter):
		return m.openResource(all[m.dashCursor])
	}
	if n, ok := digitIndex(msg.String()); ok && n < len(all) {
		return m.openResource(all[n])
	}
	return m, nil
}

func (m Model) renderDashboard() string {
	var b strings.Builder

	for i, res := range graph.All() {
		marker := "  "
		title := styleContextVal.Render(res.Title)
		if i == m.dashCursor {
			marker = styleHintKey.Render("▸ ")
			title = accentStyle(res.Accent).Render(res.Title)
		}
		alias := ":" + res.Aliases[0]
		b.WriteString(marker +
			styleHintKey.Render(itoa(i+1)+". ") + title +
			styleDim.Render("   "+alias) + "\n")
		b.WriteString("     " + styleDim.Render(res.Description) + "\n")

		// Show this view's recent searches so a repeat lookup is one key away
		// even before the view is open.
		if recent := m.history.list(string(res.Kind)); len(recent) > 0 {
			b.WriteString("     " + styleHintDesc.Render("recent: "+
				graph.Truncate(strings.Join(recent, " · "), max(20, m.width-14))) + "\n")
		}
		b.WriteString("\n")
	}

	b.WriteString(styleDim.Render(
		"Nothing is fetched until you open a view. Every request is a read.") + "\n")

	if m.err != nil {
		b.WriteString("\n" + styleErr.Render("✗ "+m.err.Error()) + "\n")
	}

	hints := hintBar(m.width,
		[2]string{"↑/↓", "choose"},
		[2]string{"enter", "open"},
		[2]string{"1-4", "open directly"},
		[2]string{":", "command"},
		[2]string{"?", "help"},
		[2]string{"q", "quit"},
	)

	return m.chrome(styleHelpTitle.Render("VIEWS"), "",
		strings.Split(b.String(), "\n"), hints)
}
