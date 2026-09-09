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
	headerHeight      = 4 // title, tenant, account, spacer
	resourceBarHeight = 1
	tableHeadHeight   = 1
	footerHeight      = 2 // status/prompt line, key hints
	// detailChromeHeight is everything around the detail viewport.
	detailChromeHeight = headerHeight + 1 + footerHeight
)

// tableHeight is how many data rows fit on screen.
func (m Model) tableHeight() int {
	return max(1, m.height-headerHeight-resourceBarHeight-tableHeadHeight-footerHeight)
}

// View renders the current screen.
func (m Model) View() string {
	if m.quitting {
		return ""
	}
	if m.width == 0 {
		// The first frame arrives before the terminal reports its size.
		return "starting entra-tui..."
	}

	var body string
	switch m.view {
	case viewHelp:
		body = m.renderHelp()
	case viewDetail:
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

func (m Model) renderHeader() string {
	title := styleTitle.Render("ENTRA-TUI")

	right := m.headerStatus()
	gap := m.width - lipgloss.Width(title) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	line1 := title + strings.Repeat(" ", gap) + right

	tenant := m.identity.TenantID
	if tenant == "" {
		tenant = "unknown"
	}
	method := string(m.identity.Method)
	if method == "" {
		method = "unknown"
	}

	line2 := styleContextKey.Render("Tenant   ") + styleContextVal.Render(tenant)
	line3 := styleContextKey.Render("Account  ") + styleContextVal.Render(m.identity.Label()) +
		styleDim.Render("  via "+method)

	return strings.Join([]string{line1, line2, line3, ""}, "\n")
}

// headerStatus is the top-right indicator: in-flight work, or a reminder that
// help exists.
func (m Model) headerStatus() string {
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
		return styleHintDesc.Render("? help")
	}
}

// renderResourceBar shows the active view, how much of it is loaded, and any
// narrowing in force.
func (m Model) renderResourceBar() string {
	res := m.coll.res
	parts := []string{accentStyle(res.Accent).Render(res.Title)}

	count := fmt.Sprintf("[%s", formatInt(m.coll.len()))
	if m.coll.filter != "" && m.coll.len() != m.coll.loaded() {
		count += "/" + formatInt(m.coll.loaded())
	}
	if m.coll.total >= 0 {
		count += " of " + formatInt(int(m.coll.total))
	}
	count += "]"
	parts = append(parts, styleDim.Render(count))

	if m.coll.search != "" {
		parts = append(parts, styleWarn.Render("search:"+m.coll.search))
	}
	if m.coll.filter != "" {
		parts = append(parts, styleOK.Render("filter:"+m.coll.filter))
	}
	if m.coll.hasMore() {
		parts = append(parts, styleDim.Render("(more — n / A)"))
	}
	return styleResourceBar.Render(strings.Join(parts, "  "))
}

// renderFooter is the status/prompt line plus a second line that carries
// either an error's remedy or the key hints.
//
// When a request fails, the hint explaining what to do about it displaces the
// key hints: the two together overflow one line and the truncation would cut
// off exactly the part the user needs.
func (m Model) renderFooter(hints string) string {
	second := hints
	if m.mode == modeNormal && m.err != nil {
		if hint := apiHint(m.err); hint != "" {
			second = styleWarn.Render(graph.Truncate(hint, m.width))
		}
	}
	return m.renderStatusLine() + "\n" + second
}

func (m Model) renderStatusLine() string {
	if m.mode != modeNormal {
		return stylePrompt.Render(m.promptPrefix()) + m.input.View()
	}
	switch {
	case m.err != nil:
		return styleErr.Render(graph.Truncate("✗ "+m.err.Error(), m.width))
	case m.flash != "":
		return styleFlash.Render(graph.Truncate("• "+m.flash, m.width))
	default:
		return ""
	}
}

func (m Model) promptPrefix() string {
	switch m.mode {
	case modeCommand:
		return ":"
	case modeFilter:
		return "/"
	case modeSearch:
		return "search> "
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
		[2]string{":", "cmd"},
		[2]string{"/", "filter"},
		[2]string{"s", "search"},
		[2]string{"enter", "describe"},
		[2]string{"n/A", "more"},
		[2]string{"r", "refresh"},
		[2]string{"y", "yank"},
		[2]string{"?", "help"},
		[2]string{"q", "quit"},
	)

	return strings.Join([]string{
		m.renderHeader(),
		m.renderResourceBar(),
		head,
		strings.Join(lines, "\n"),
		m.renderFooter(hints),
	}, "\n")
}

// emptyMessage explains an empty table, distinguishing "still loading" from
// "your filter hid everything" from "the tenant really has none".
func (m Model) emptyMessage() string {
	switch {
	case m.loading:
		return "loading…"
	case m.err != nil:
		return "request failed — see the message below"
	case m.coll.filter != "" && m.coll.loaded() > 0:
		return fmt.Sprintf("no loaded object matches %q (esc to clear, A to load more)", m.coll.filter)
	case m.coll.search != "":
		return fmt.Sprintf("no results for search %q (esc to clear)", m.coll.search)
	default:
		return "no objects returned"
	}
}

// ------------------------------------------------------------ detail screen

func (m Model) renderDetail() string {
	title := m.coll.res.Title
	name := ""
	if m.detailItem != nil {
		name = m.detailItem.String("displayName")
		if name == "" {
			name = m.detailItem.ID()
		}
	}
	mode := "properties"
	if m.detailRaw {
		mode = "raw json"
	}
	bar := accentStyle(m.coll.res.Accent).Render(title+" › ") +
		styleContextVal.Render(name) + "  " + styleDim.Render("("+mode+")")

	hints := hintBar(
		[2]string{"↑/↓", "scroll"},
		[2]string{"R", "toggle raw json"},
		[2]string{"y", "yank id"},
		[2]string{"esc", "back"},
		[2]string{"q", "quit"},
	)

	return strings.Join([]string{
		m.renderHeader(),
		bar,
		m.detailVP.View(),
		m.renderFooter(hints),
	}, "\n")
}

// renderDetailBody formats the selected object for the viewport.
func (m Model) renderDetailBody() string {
	if m.detailItem == nil {
		return styleDim.Render("nothing selected")
	}
	if m.detailRaw {
		return m.detailItem.JSON()
	}

	keys := m.detailItem.Keys()
	// Align values into a column, but never let one pathological key name
	// push the values off the right edge.
	keyWidth := 0
	for _, k := range keys {
		keyWidth = max(keyWidth, len(k))
	}
	keyWidth = min(keyWidth, 34)

	valWidth := max(20, m.width-keyWidth-3)

	var b strings.Builder
	for _, k := range keys {
		if strings.HasPrefix(k, "@odata") {
			continue
		}
		val := m.detailItem.String(k)
		if val == "" {
			val = "-"
		}
		label := graph.Truncate(k, keyWidth)
		// Pad to the key column, then a fixed two-space gutter. Clamping the
		// padding to a minimum of one would push the longest key -- the one
		// that needs no padding -- a cell further right than every other row.
		pad := max(0, keyWidth-lipgloss.Width(label))
		b.WriteString(styleDetailKey.Render(label))
		b.WriteString(strings.Repeat(" ", pad+2))
		b.WriteString(styleDetailVal.Render(wrapValue(val, valWidth, keyWidth+2)))
		b.WriteByte('\n')
	}
	if m.detailLoading {
		b.WriteString("\n" + styleDim.Render("fetching full object…"))
	}
	return b.String()
}

// wrapValue hard-wraps a long property value, indenting continuation lines to
// stay under the value column.
func wrapValue(s string, width, indent int) string {
	if width <= 0 || len([]rune(s)) <= width {
		return s
	}
	pad := strings.Repeat(" ", indent)
	runes := []rune(s)
	var b strings.Builder
	for i := 0; i < len(runes); i += width {
		if i > 0 {
			b.WriteString("\n" + pad)
		}
		b.WriteString(string(runes[i:min(i+width, len(runes))]))
	}
	return b.String()
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

	resourceRows := [][2]string{}
	for i, r := range graph.All() {
		alias := ":" + string(r.Kind)
		if len(r.Aliases) > 0 {
			alias = ":" + r.Aliases[0]
		}
		resourceRows = append(resourceRows, [2]string{
			fmt.Sprintf("%d  %s", i+1, alias), r.Title,
		})
	}

	cols := lipgloss.JoinHorizontal(lipgloss.Top,
		section("NAVIGATION", [][2]string{
			{"↑/k ↓/j", "move cursor"},
			{"pgup/pgdn", "page"},
			{"g / G", "top / bottom"},
			{"enter", "describe object"},
			{"esc", "back / clear filter"},
			{"q", "quit"},
		}),
		"    ",
		section("VIEWS", resourceRows),
	)

	cols2 := lipgloss.JoinHorizontal(lipgloss.Top,
		section("FIND", [][2]string{
			{"/", "filter loaded rows"},
			{"  term term", "all terms must match"},
			{"  !term", "exclude matches"},
			{"s", "server-side search"},
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
		"entra-tui is strictly read-only: it issues GET requests to Microsoft Graph v1.0 and nothing else.\n" +
			"Everything you see is scoped to your own delegated permissions.")

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
