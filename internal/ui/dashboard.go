package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/idoavrah/entra-tui/internal/graph"
)

// Dashboard tile geometry.
const (
	tileGap = 2
	// tileBodyHeight is the big number plus the description line.
	tileBodyHeight = bigDigitHeight + 1
	// maxDashboardColumns caps the grid. Past three the tiles are narrower
	// than the thing they describe.
	maxDashboardColumns = 3
	// twoColumnDashboard is the width below which one tile per row is the
	// only thing that reads, whatever the descriptions would like.
	twoColumnDashboard = 88
	// tileMinWidth keeps a tile wide enough for a seven-digit count. It is
	// the floor, not the goal -- see tilePreferredWidth.
	tileMinWidth = 34
)

// tilePreferredWidth is wide enough for the longest description a tile can
// carry, plus its border and the cell Truncate leaves either side. Measured
// rather than written down, so adding a wordier view widens the tiles instead
// of quietly clipping one.
func tilePreferredWidth() int {
	widest := 0
	for _, r := range graph.All() {
		widest = max(widest, lipgloss.Width(r.Description))
	}
	return widest + 4
}

// tileWidthFor is how wide each tile is when the row holds this many.
func tileWidthFor(inner, columns int) int {
	return (inner - (columns-1)*tileGap) / columns
}

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
	columns := m.dashboardColumns()

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
	case key.Matches(msg, keys.Refresh):
		return m, m.loadTotals()

	// The tiles are a grid, so the cursor moves in two dimensions.
	case key.Matches(msg, keys.Up):
		m.dashCursor = clamp(m.dashCursor-columns, 0, len(all)-1)
		return m, nil
	case key.Matches(msg, keys.Down):
		m.dashCursor = clamp(m.dashCursor+columns, 0, len(all)-1)
		return m, nil
	case key.Matches(msg, keys.Left):
		m.dashCursor = clamp(m.dashCursor-1, 0, len(all)-1)
		return m, nil
	case key.Matches(msg, keys.Right):
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

// dashboardColumns is how many tiles fit across.
//
// Fewer, wider tiles beat more, narrower ones: a column count chosen purely
// on available width gave three tiles at 150 columns, each four cells too
// narrow for the longest description, which then lost its last few words to
// an ellipsis on every launch.
func (m Model) dashboardColumns() int {
	inner := boxInnerWidth(m.width)

	// As many across as can each still show a description in full.
	preferred := tilePreferredWidth()
	for columns := maxDashboardColumns; columns > 1; columns-- {
		if tileWidthFor(inner, columns) >= preferred {
			return columns
		}
	}
	// Below that, the choice stops being wide against wider and becomes
	// cramped against unreadable, so a narrow terminal keeps the layout it
	// already had.
	if inner >= twoColumnDashboard {
		return 2
	}
	return 1
}

func (m Model) renderDashboard() string {
	all := graph.All()
	columns := m.dashboardColumns()
	inner := boxInnerWidth(m.width)
	tileWidth := max(tileMinWidth, tileWidthFor(inner, columns))

	var body []string
	for start := 0; start < len(all); start += columns {
		end := min(start+columns, len(all))

		row := make([][]string, 0, columns)
		for i := start; i < end; i++ {
			row = append(row, m.renderTile(all[i], i, tileWidth))
		}
		body = append(body, joinTilesAcross(row, tileWidth)...)
		body = append(body, "")
	}

	if m.err != nil {
		body = append(body, styleErr.Render("✗ "+m.err.Error()))
	}

	hints := hintBar(m.width,
		[2]string{"enter", "open"},
		[2]string{"1-5", "open directly"},
		[2]string{"r", "refresh totals"},
	)
	return m.chrome(styleHelpTitle.Render("DIRECTORY"), m.totalsCaption(), body, hints)
}

// totalsCaption reports on the count queries along the frame's bottom edge.
func (m Model) totalsCaption() string {
	if len(m.totalErrs) > 0 {
		return styleWarn.Render("some totals unavailable — r retries")
	}
	if len(m.totals) < len(graph.All()) {
		return styleDim.Render(m.spin.View() + " counting")
	}
	return styleDim.Render("totals from Microsoft Graph — r refreshes")
}

// renderTile draws one resource as a bordered card: its name and key on the
// border, its total in large numerals, and what it holds underneath.
func (m Model) renderTile(res graph.Resource, index, width int) []string {
	selected := index == m.dashCursor

	accent := accentStyle(res.Accent)
	border := styleBorder
	if selected {
		border = accent
	}

	caption := styleHintKey.Render(itoa(index+1)) + styleDim.Render(" · ") + accent.Render(res.Title)
	inner := max(1, width-2)

	lines := []string{tileBorder(width, caption, border, boxTopLeft, boxTopRight)}

	numberStyle := accent
	if _, failed := m.totalErrs[res.Kind]; failed {
		numberStyle = styleDim
	}
	// The card reads as one centred column: caption, count, and what the
	// count is of, each balanced against the same axis.
	total := m.totalText(res.Kind)
	numberWidth := bigNumberWidth(total)
	for _, l := range bigNumber(total) {
		lines = append(lines, tileRow(inner, centreIn(numberStyle.Render(l), numberWidth, inner), border))
	}

	description := graph.Truncate(res.Description, inner-2)
	lines = append(lines, tileRow(inner,
		centreIn(styleDim.Render(description), lipgloss.Width(description), inner), border))
	lines = append(lines, tileBorder(width, "", border, boxBottomLeft, boxBottomRight))
	return lines
}

// centreIn pads styled content of a known printable width to sit centred in
// a field. The width is passed in because measuring styled text at every
// call site is what lets a stray escape sequence skew the alignment.
func centreIn(content string, contentWidth, field int) string {
	if contentWidth >= field {
		return content
	}
	return spaces((field-contentWidth)/2) + content
}

// totalText is the count to display, or a placeholder while it is unknown.
func (m Model) totalText(kind graph.Kind) string {
	if _, failed := m.totalErrs[kind]; failed {
		return "—"
	}
	total, ok := m.totals[kind]
	if !ok {
		return "—"
	}
	return formatInt(int(total))
}

// tileBorder draws a tile's top or bottom edge in the given style, with the
// caption centred on it.
func tileBorder(width int, caption string, style lipgloss.Style, left, right string) string {
	inner := max(1, width-2)
	if lipgloss.Width(caption) == 0 || lipgloss.Width(caption)+2 > inner {
		return style.Render(left + strings.Repeat(boxHorizontal, inner) + right)
	}
	captionWidth := lipgloss.Width(caption) + 2
	leftRule := (inner - captionWidth) / 2
	rightRule := inner - captionWidth - leftRule
	return style.Render(left+strings.Repeat(boxHorizontal, leftRule)) +
		" " + caption + " " +
		style.Render(strings.Repeat(boxHorizontal, rightRule)+right)
}

// tileRow frames one line of a tile's body.
func tileRow(inner int, content string, style lipgloss.Style) string {
	if pad := inner - lipgloss.Width(content); pad > 0 {
		content += spaces(pad)
	}
	return style.Render(boxVertical) + content + style.Render(boxVertical)
}

// joinTilesAcross places a row of equal-height tiles side by side.
func joinTilesAcross(tiles [][]string, tileWidth int) []string {
	if len(tiles) == 0 {
		return nil
	}
	height := 0
	for _, t := range tiles {
		height = max(height, len(t))
	}

	out := make([]string, height)
	for row := range height {
		var b strings.Builder
		for i, tile := range tiles {
			if i > 0 {
				b.WriteString(spaces(tileGap))
			}
			line := ""
			if row < len(tile) {
				line = tile[row]
			}
			b.WriteString(line)
			b.WriteString(spaces(tileWidth - lipgloss.Width(line)))
		}
		out[row] = strings.TrimRight(b.String(), " ")
	}
	return out
}
