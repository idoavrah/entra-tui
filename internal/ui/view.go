package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/idoavrah/entra-tui/internal/graph"
)

// Fixed vertical budgets, subtracted from the terminal height to size the
// scrollable region.
const (
	headerHeight      = logoHeight // context block on the left, wordmark on the right
	promptHeight      = 1          // the ":" / "/" line, kept at the top
	quickHeight       = 1          // recent searches for this view
	resourceBarHeight = 1
	tableHeadHeight   = 1
	footerHeight      = 2 // status line, key hints

	browseChromeHeight = headerHeight + promptHeight + quickHeight +
		resourceBarHeight + tableHeadHeight + footerHeight

	// detailChromeHeight is everything around the detail viewport.
	detailChromeHeight = headerHeight + promptHeight + 1 + footerHeight
)

// tableHeight is how many data rows fit on screen.
func (m Model) tableHeight() int {
	return max(1, m.height-browseChromeHeight)
}

// View renders the current screen.
func (m Model) View() string {
	if m.quitting {
		return ""
	}
	if m.width == 0 {
		// The first frame arrives before the terminal reports its size.
		return "starting entra-tui…"
	}

	var body string
	switch m.screen {
	case screenLogin:
		body = m.renderLogin()
	case screenDashboard:
		body = m.renderDashboard()
	case screenHelp:
		body = m.renderHelp()
	case screenDetail:
		body = m.renderDetail()
	default:
		body = m.renderBrowse()
	}

	// An OSC 52 clipboard payload rides along with the frame so the renderer
	// serialises it rather than it racing writes on stdout.
	if m.pendingClipboard != "" {
		return osc52(m.pendingClipboard) + body
	}
	return body
}

// ------------------------------------------------------------------ chrome

// renderHeader draws the session context on the left and the wordmark on the
// right, occupying exactly logoHeight lines.
func (m Model) renderHeader() string {
	left := m.contextLines()

	showLogo := m.width >= logoMinTerminalWidth
	lines := make([]string, logoHeight)
	for i := range lines {
		text := ""
		if i < len(left) {
			text = left[i]
		}
		if !showLogo {
			lines[i] = graph.Truncate(text, m.width)
			continue
		}
		gap := m.width - lipgloss.Width(text) - logoWidth
		if gap < 1 {
			text = graph.Truncate(text, max(0, m.width-logoWidth-1))
			gap = m.width - lipgloss.Width(text) - logoWidth
		}
		lines[i] = text + strings.Repeat(" ", max(1, gap)) + accentStyle("").Render(logo[i])
	}
	return strings.Join(lines, "\n")
}

// contextLines describe who is signed in and what is happening.
func (m Model) contextLines() []string {
	if m.client == nil {
		return []string{
			styleContextKey.Render("Tenant   ") + styleDim.Render(m.opts.Auth.TenantID),
			styleContextKey.Render("Account  ") + styleDim.Render("not signed in"),
			"",
		}
	}
	tenant := m.identity.TenantID
	if tenant == "" {
		tenant = "unknown"
	}
	method := string(m.identity.Method)
	if method == "" {
		method = "unknown"
	}
	return []string{
		styleContextKey.Render("Tenant   ") + styleContextVal.Render(tenant),
		styleContextKey.Render("Account  ") + styleContextVal.Render(m.identity.Label()) +
			styleDim.Render("  ·  "+method),
		styleContextKey.Render("Status   ") + m.statusIndicator(),
	}
}

// statusIndicator reports in-flight work.
func (m Model) statusIndicator() string {
	switch {
	case m.loading:
		return styleDim.Render(m.spin.View() + " loading")
	case m.loadAll:
		return styleWarn.Render(m.spin.View() + " loading all pages")
	case m.loadingMore:
		return styleDim.Render(m.spin.View() + " fetching page")
	case m.detailLoading:
		return styleDim.Render(m.spin.View() + " describing")
	default:
		return styleOK.Render("connected")
	}
}

// renderPromptLine is the command and search line, kept at the top of the
// screen where the eye already is when typing.
func (m Model) renderPromptLine() string {
	if m.mode != modeNormal {
		return stylePrompt.Render(m.promptPrefix()) + m.input.View()
	}
	parts := []string{
		styleHintKey.Render(":") + styleHintDesc.Render(" view"),
		styleHintKey.Render("/") + styleHintDesc.Render(" search"),
	}
	return styleDim.Render(strings.Join(parts, "   "))
}

func (m Model) promptPrefix() string {
	switch m.mode {
	case modeCommand:
		return ":"
	case modeSearch:
		return "/"
	default:
		return ""
	}
}

// renderQuickSearches lists this view's recent searches, replayable with the
// number keys.
func (m Model) renderQuickSearches() string {
	if m.coll == nil {
		return ""
	}
	recent := m.history.list(string(m.coll.res.Kind))
	if len(recent) == 0 {
		return styleDim.Render("recent searches appear here — press / to run one")
	}

	var parts []string
	for i, term := range recent {
		digit := itoa(i + 1)
		if i == 9 {
			digit = "0"
		}
		style := styleHintDesc
		if term == m.coll.search {
			style = styleOK
		}
		parts = append(parts, styleHintKey.Render("["+digit+"]")+style.Render(" "+term))
	}
	return graph.Truncate(strings.Join(parts, "  "), m.width)
}

// renderResourceBar shows the active view, how much of it is loaded, and any
// search in force.
func (m Model) renderResourceBar() string {
	res := m.coll.res
	parts := []string{accentStyle(res.Accent).Render(res.Title)}

	count := "[" + formatInt(m.coll.len())
	if m.coll.total >= 0 {
		count += " of " + formatInt(int(m.coll.total))
	}
	count += "]"
	parts = append(parts, styleDim.Render(count))
	parts = append(parts, styleDim.Render("↑name"))

	if m.coll.search != "" {
		parts = append(parts, styleOK.Render("search:"+m.coll.search))
	}
	if m.coll.hasMore() {
		parts = append(parts, styleDim.Render("(more — n / A)"))
	}
	return styleResourceBar.Render(strings.Join(parts, "  "))
}

// renderFooter is the status line plus a second line carrying either an
// error's remedy or the key hints.
//
// When a request fails, the hint explaining what to do about it displaces the
// key hints: the two together overflow one line and the truncation would cut
// off exactly the part the user needs.
func (m Model) renderFooter(hints string) string {
	second := hints
	if m.err != nil {
		if hint := apiHint(m.err); hint != "" {
			second = styleWarn.Render(graph.Truncate(hint, m.width))
		}
	}
	return m.renderStatusLine() + "\n" + second
}

func (m Model) renderStatusLine() string {
	switch {
	case m.err != nil:
		return styleErr.Render(graph.Truncate("✗ "+m.err.Error(), m.width))
	case m.flash != "":
		return styleFlash.Render(graph.Truncate("• "+m.flash, m.width))
	default:
		return ""
	}
}

// hintBar renders "key description" pairs.
func hintBar(pairs ...[2]string) string {
	out := make([]string, 0, len(pairs))
	for _, p := range pairs {
		out = append(out, styleHintKey.Render(p[0])+styleHintDesc.Render(" "+p[1]))
	}
	return styleHintDesc.Render(strings.Join(out, styleHintDesc.Render("  ·  ")))
}

// ------------------------------------------------------------ browse screen

func (m Model) renderBrowse() string {
	cols := m.coll.res.Columns
	widths := layout(cols, m.width)

	head := styleTableHead.Render(headerCells(cols, widths))

	height := m.tableHeight()
	lines := make([]string, 0, height)

	if m.coll.len() == 0 {
		lines = append(lines, styleDim.Render(m.emptyMessage()))
	}

	for i := m.offset; i < m.coll.len() && len(lines) < height; i++ {
		_, cells, ok := m.coll.at(i)
		if !ok {
			break
		}
		line := renderCells(cells, widths)
		if i == m.cursor {
			// Pad to full width so the selection bar spans the table rather
			// than stopping at the last non-blank character.
			if pad := m.width - lipgloss.Width(line); pad > 0 {
				line += strings.Repeat(" ", pad)
			}
			lines = append(lines, styleRowSelected.Render(line))
		} else {
			lines = append(lines, styleRow.Render(line))
		}
	}
	for len(lines) < height {
		lines = append(lines, "")
	}

	hints := hintBar(
		[2]string{"enter", "describe"},
		[2]string{"/", "search"},
		[2]string{"1-0", "recent"},
		[2]string{":", "view"},
		[2]string{"n/A", "more"},
		[2]string{"r", "refresh"},
		[2]string{"y", "yank"},
		[2]string{"esc", "dashboard"},
		[2]string{"?", "help"},
	)

	return strings.Join([]string{
		m.renderHeader(),
		m.renderPromptLine(),
		m.renderQuickSearches(),
		m.renderResourceBar(),
		head,
		strings.Join(lines, "\n"),
		m.renderFooter(hints),
	}, "\n")
}

// emptyMessage explains an empty table, distinguishing "still loading" from
// "your search matched nothing" from "the tenant really has none".
func (m Model) emptyMessage() string {
	switch {
	case m.loading:
		return "loading…"
	case m.err != nil:
		return "request failed — see the message below"
	case m.coll.search != "":
		return fmt.Sprintf("no results for %q (esc clears the search)", m.coll.search)
	default:
		return "no objects returned"
	}
}

// -------------------------------------------------------------- help screen

func (m Model) renderHelp() string {
	section := func(title string, rows [][2]string) string {
		var b strings.Builder
		b.WriteString(styleHelpTitle.Render(title) + "\n")
		for _, r := range rows {
			b.WriteString("  " + styleHintKey.Render(padRight(r[0], 12)) + styleContextVal.Render(r[1]) + "\n")
		}
		return b.String()
	}

	var viewRows [][2]string
	for i, r := range graph.All() {
		viewRows = append(viewRows, [2]string{
			fmt.Sprintf("%d  :%s", i+1, r.Aliases[0]), r.Title,
		})
	}
	viewRows = append(viewRows,
		[2]string{"~  :dash", "Dashboard"},
		[2]string{"esc", "back one layer"},
	)

	cols := lipgloss.JoinHorizontal(lipgloss.Top,
		section("NAVIGATION", [][2]string{
			{"↑/k ↓/j", "move cursor"},
			{"pgup/pgdn", "page"},
			{"g / G", "top / bottom"},
			{"enter", "describe object"},
			{"x", "app reg ⇄ ent app"},
			{"q", "quit"},
		}),
		"    ",
		section("VIEWS", viewRows),
	)

	cols2 := lipgloss.JoinHorizontal(lipgloss.Top,
		section("SEARCH", [][2]string{
			{"/", "search the directory"},
			{"1-0", "replay a recent search"},
			{"esc", "clear the search"},
			{":", "command prompt"},
		}),
		"    ",
		section("DATA", [][2]string{
			{"n", "load next page"},
			{"A", "load all pages"},
			{"r", "refresh from Graph"},
			{"R", "raw json (detail)"},
			{"y", "copy object id"},
		}),
	)

	note := styleDim.Render(
		"Search runs against Microsoft Graph, not just the rows on screen.\n" +
			"entra-tui is strictly read-only: it issues GET requests and nothing else.\n" +
			"Right-to-left names are reordered for display; the underlying data is untouched.")

	body := strings.Join([]string{cols, "", cols2, "", note}, "\n")

	return strings.Join([]string{
		m.renderHeader(),
		styleHelpTitle.Render("HELP"),
		body,
		m.renderFooter(hintBar([2]string{"any key", "back"})),
	}, "\n")
}

// ----------------------------------------------------------------- helpers

// padRight pads to a display width. Byte length would be wrong here: the
// help screen's arrow glyphs are multi-byte but single-cell.
func padRight(s string, w int) string {
	pad := w - lipgloss.Width(s)
	if pad <= 0 {
		return s
	}
	return s + strings.Repeat(" ", pad)
}

// formatInt renders a count with thousands separators, because directory
// sizes are routinely five or six digits.
func formatInt(n int) string {
	s := fmt.Sprint(n)
	if n < 0 {
		return s
	}
	var parts []string
	for len(s) > 3 {
		parts = append([]string{s[len(s)-3:]}, parts...)
		s = s[:len(s)-3]
	}
	parts = append([]string{s}, parts...)
	return strings.Join(parts, ",")
}

// osc52 builds the terminal escape sequence that copies text to the system
// clipboard. It works over SSH, where a local clipboard library cannot reach
// the user's actual machine.
func osc52(s string) string {
	return "\x1b]52;c;" + base64Encode(s) + "\a"
}
