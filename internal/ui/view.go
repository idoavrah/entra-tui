package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/idoavrah/entra-tui/internal/graph"
)

// Fixed vertical budgets. The header and the footer never change height, so
// the framed content area is the only thing that grows and shrinks with the
// terminal.
const (
	// headerHeight holds the context block, the quick-search grid and the
	// wordmark side by side. The grid is the tallest of the three.
	headerHeight = quickSearchRows
	footerHeight = 2 // status line, key hints
	boxChrome    = 2 // top and bottom border
	// tableHeadHeight is the column header row, drawn inside the frame.
	tableHeadHeight = 1
)

// quickSearchRows is the height of the quick-search grid: ten slots in two
// columns.
const quickSearchRows = quickSearchSlots / 2

// Layout thresholds.
const (
	// quickSearchMinWidth is the narrowest middle column that still fits two
	// readable quick-search columns. Below it the grid is dropped; the digit
	// keys keep working.
	quickSearchMinWidth = 40
	// headerBlockGap separates the header's three blocks.
	headerBlockGap = 3
	// contextBlockMaxWidth caps the left block so a long tenant id cannot
	// crowd out the quick searches.
	contextBlockMaxWidth = 46
)

// promptLines is 1 while a prompt is open and 0 otherwise. The search line is
// only worth a row when it is actually being used.
func (m Model) promptLines() int {
	if m.mode != modeNormal {
		return 1
	}
	return 0
}

// contentHeight is the number of body rows inside the frame.
func (m Model) contentHeight() int {
	return max(1, m.height-headerHeight-m.promptLines()-boxChrome-footerHeight)
}

// tableHeight is how many data rows fit, once the column header is deducted.
func (m Model) tableHeight() int {
	return max(1, m.contentHeight()-tableHeadHeight)
}

// detailBodyHeight sizes the detail viewport. It is computed from the raw
// terminal height because the viewport must be sized in Update, before the
// frame is drawn.
func detailBodyHeight(height int) int {
	return max(1, height-headerHeight-boxChrome-footerHeight)
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

// chrome assembles a screen: fixed header, the prompt when open, the framed
// body, and the fixed footer. Body lines are padded or trimmed to the exact
// content height so the frame never drifts.
func (m Model) chrome(caption, bottomCaption string, body []string, hints string) string {
	h := m.contentHeight()
	fitted := make([]string, h)
	copy(fitted, body)

	parts := []string{m.renderHeader()}
	if p := m.renderPromptLine(); p != "" {
		parts = append(parts, p)
	}
	parts = append(parts, strings.Join(boxFrame(m.width, caption, bottomCaption, fitted), "\n"))
	parts = append(parts, m.renderFooter(hints))
	return strings.Join(parts, "\n")
}

// ------------------------------------------------------------------ header

// renderHeader lays out three blocks across a fixed number of rows: session
// context on the left, the quick-search grid in the middle, the wordmark on
// the right. Blocks are dropped from the right as the terminal narrows.
func (m Model) renderHeader() string {
	ctx := m.contextLines()
	leftWidth := 0
	for _, l := range ctx {
		leftWidth = max(leftWidth, lipgloss.Width(l))
	}
	leftWidth = min(leftWidth, contextBlockMaxWidth)

	showLogo := m.width >= logoMinTerminalWidth
	logoSpace := 0
	if showLogo {
		logoSpace = logoWidth + headerBlockGap
	}

	middleWidth := m.width - leftWidth - headerBlockGap - logoSpace
	quick := m.quickSearchBlock(middleWidth)

	lines := make([]string, headerHeight)
	for i := range headerHeight {
		var b strings.Builder

		left := ""
		if i < len(ctx) {
			left = ctx[i]
		}
		b.WriteString(left)
		b.WriteString(spaces(leftWidth - lipgloss.Width(left)))

		if len(quick) > 0 {
			b.WriteString(spaces(headerBlockGap))
			mid := ""
			if i < len(quick) {
				mid = quick[i]
			}
			b.WriteString(mid)
			b.WriteString(spaces(middleWidth - lipgloss.Width(mid)))
		}

		if showLogo && i < logoHeight {
			b.WriteString(spaces(max(1, m.width-lipgloss.Width(b.String())-logoWidth)))
			b.WriteString(accentStyle("").Render(logo[i]))
		}
		lines[i] = strings.TrimRight(b.String(), " ")
	}
	return strings.Join(lines, "\n")
}

// contextLines describe who is signed in and what is happening.
func (m Model) contextLines() []string {
	if m.client == nil {
		return []string{
			styleContextKey.Render("Tenant   ") + styleDim.Render(m.opts.Auth.TenantID),
			styleContextKey.Render("Account  ") + styleDim.Render("not signed in"),
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
		styleContextKey.Render("Tenant   ") + styleContextVal.Render(graph.Truncate(tenant, 36)),
		styleContextKey.Render("Account  ") + styleContextVal.Render(graph.Truncate(m.identity.Label(), 36)),
		styleContextKey.Render("Signed   ") + styleDim.Render(method),
		styleContextKey.Render("Status   ") + m.statusIndicator(),
	}
}

// statusIndicator reports in-flight work.
func (m Model) statusIndicator() string {
	switch {
	case m.authing:
		return styleWarn.Render(m.spin.View() + " signing in")
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

// quickSearchBlock renders this view's search slots as two columns. Slots
// keep fixed positions, so each digit keeps meaning the same search.
func (m Model) quickSearchBlock(width int) []string {
	if m.coll == nil || width < quickSearchMinWidth {
		return nil
	}
	slots := m.history.slotsFor(string(m.coll.res.Kind))

	// Before anything has been searched, ten empty slots are just noise.
	if len(m.history.list(string(m.coll.res.Kind))) == 0 {
		lines := make([]string, quickSearchRows)
		lines[0] = styleDim.Render(graph.Truncate("press / to search this view", width))
		return lines
	}

	columnWidth := (width - 2) / 2
	lines := make([]string, quickSearchRows)
	for r := range quickSearchRows {
		left := m.quickEntry(slots, r, columnWidth)
		right := m.quickEntry(slots, r+quickSearchRows, columnWidth)
		lines[r] = left + "  " + right
	}
	return lines
}

// quickEntry renders one slot as "[n] term", padded to a fixed width so the
// two columns stay aligned.
func (m Model) quickEntry(slots []string, index, width int) string {
	if index >= len(slots) {
		return spaces(width)
	}
	label := styleHintKey.Render("[" + quickDigit(index) + "]")

	term := slots[index]
	if term == "" {
		// An unused slot shows its digit and nothing else: the position is
		// worth advertising, a placeholder glyph is not.
		return styleDim.Render("["+quickDigit(index)+"]") + spaces(max(0, width-3))
	}

	style := styleHintDesc
	if term == m.coll.search {
		style = styleOK
	}
	text := graph.Truncate(term, max(1, width-4))
	return label + style.Render(" "+text) + spaces(max(0, width-3-lipgloss.Width(text)))
}

// quickDigit maps a slot index to the key that replays it: 1-9 then 0.
func quickDigit(index int) string {
	if index == 9 {
		return "0"
	}
	return itoa(index + 1)
}

// ------------------------------------------------------------------ prompt

// renderPromptLine draws the command or search line. It returns empty while
// no prompt is open, and the caller omits the row entirely.
func (m Model) renderPromptLine() string {
	if m.mode == modeNormal {
		return ""
	}
	return stylePrompt.Render(m.promptPrefix()) + m.input.View()
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

// ------------------------------------------------------------------ footer

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

// hintBar renders "key description" pairs, dropping the ones that do not fit.
//
// The footer is a fixed two rows, so an over-long hint bar would wrap and
// push the frame off the bottom of the screen. Truncating the rendered string
// is not an option either -- it would cut an escape sequence -- so hints are
// added whole while there is room and the rest are simply left out. They are
// ordered most useful first, and the full set is always in the help screen.
func hintBar(width int, pairs ...[2]string) string {
	const separator = "  ·  "

	var out []string
	used := 0
	for _, p := range pairs {
		entry := styleHintKey.Render(p[0]) + styleHintDesc.Render(" "+p[1])
		cost := lipgloss.Width(entry)
		if len(out) > 0 {
			cost += len(separator)
		}
		if used+cost > width {
			break
		}
		out = append(out, entry)
		used += cost
	}
	return strings.Join(out, styleHintDesc.Render(separator))
}

// ------------------------------------------------------------ browse screen

func (m Model) renderBrowse() string {
	inner := boxInnerWidth(m.width)
	cols := m.coll.res.Columns
	widths := layout(cols, inner)

	body := make([]string, 0, m.contentHeight())
	body = append(body, styleTableHead.Render(headerCells(cols, widths)))

	if m.coll.len() == 0 {
		body = append(body, styleDim.Render(m.emptyMessage()))
	}

	height := m.tableHeight()
	for i := m.offset; i < m.coll.len() && len(body) <= height; i++ {
		_, cells, ok := m.coll.at(i)
		if !ok {
			break
		}
		line := renderCells(cells, widths)
		if i == m.cursor {
			// Pad to the frame's inner width so the selection bar spans the
			// row rather than stopping at the last non-blank character.
			if pad := inner - lipgloss.Width(line); pad > 0 {
				line += spaces(pad)
			}
			line = styleRowSelected.Render(line)
		} else {
			line = styleRow.Render(line)
		}
		body = append(body, line)
	}

	hints := hintBar(m.width,
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
	return m.chrome(m.tableCaption(), m.tableFooterCaption(), body, hints)
}

// tableCaption is the centred caption on the frame's top edge: what this view
// is, how much of it is loaded, and any search narrowing it.
func (m Model) tableCaption() string {
	res := m.coll.res
	parts := []string{accentStyle(res.Accent).Render(res.Title)}

	count := formatInt(m.coll.len())
	if m.coll.total >= 0 {
		count += " of " + formatInt(int(m.coll.total))
	}
	parts = append(parts, styleDim.Render(count))
	parts = append(parts, styleDim.Render("↑name"))

	if m.coll.search != "" {
		parts = append(parts, styleOK.Render("search: "+m.coll.search))
	}
	return strings.Join(parts, styleDim.Render(" · "))
}

// tableFooterCaption advertises unfetched pages on the frame's bottom edge.
func (m Model) tableFooterCaption() string {
	if !m.coll.hasMore() {
		return ""
	}
	return styleDim.Render("more — n / A")
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
	for _, r := range graph.All() {
		viewRows = append(viewRows, [2]string{":" + r.Aliases[0], r.Title})
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
			{"1-0", "replay a slot"},
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
			"Quick-search slots keep fixed positions, so a digit always replays the same term.\n" +
			"entra-tui is read-only: it issues GET requests and nothing else.")

	body := strings.Split(strings.Join([]string{cols, "", cols2, "", note}, "\n"), "\n")
	return m.chrome(styleHelpTitle.Render("HELP"), "", body,
		hintBar(m.width, [2]string{"any key", "back"}))
}

// ----------------------------------------------------------------- helpers

// spaces returns n blanks, tolerating a negative count.
func spaces(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(" ", n)
}

// padRight pads to a display width. Byte length would be wrong here: the
// help screen's arrow glyphs are multi-byte but single-cell.
func padRight(s string, w int) string {
	return s + spaces(w-lipgloss.Width(s))
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
