package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/idoavrah/entra-tui/internal/bidi"
	"github.com/idoavrah/entra-tui/internal/graph"
)

// Fixed vertical budgets. The header and the footer never change height, so
// the framed content area is the only thing that grows and shrinks with the
// terminal.
const (
	footerHeight = 3 // status line, key hints, breadcrumb
	boxChrome    = 2 // top and bottom border
	// tableHeadHeight is the column header row, drawn inside the frame.
	tableHeadHeight = 1
	// tableMargin is the blank column either side of a table inside its
	// frame, so the text is not flush against the border.
	tableMargin = 1
)

// quickSearchRows is the height of the quick-search grid: ten slots in two
// columns.
const quickSearchRows = quickSearchSlots / 2

// Layout thresholds.
const (
	// quickEntryWidth is the fixed width of one quick-search cell. Fixing it
	// keeps the grid compact and stops the entries drifting apart as the
	// terminal widens.
	quickEntryWidth = 21
	// quickBlockWidth is the whole two-column grid.
	quickBlockWidth = quickEntryWidth*2 + 2
	// shortcutColumnGap separates the legend's two columns.
	shortcutColumnGap = 2
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

// headerHeight is the height of the header block: the context lines, the
// quick-search grid and the wordmark sit side by side, so the tallest of them
// sets it. Only the wordmark varies, and only with the terminal width.
func (m Model) headerHeight() int {
	return max(quickSearchRows, len(m.headerLogo()))
}

// contentHeight is the number of body rows inside the frame.
func (m Model) contentHeight() int {
	return max(1, m.height-m.headerHeight()-m.promptLines()-boxChrome-footerHeight)
}

// tableHeight is how many data rows fit, once the column header is deducted.
func (m Model) tableHeight() int {
	return max(1, m.contentHeight()-tableHeadHeight)
}

// detailBodyHeight sizes the detail viewport. It takes the terminal height as
// an argument rather than reading it off the model because the viewport must
// be sized in Update, on a resize, before the model carries the new height.
func (m Model) detailBodyHeight(height int) int {
	return max(1, height-m.headerHeight()-boxChrome-footerHeight)
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

// contextBlockWidth is the width the session context occupies on the left.
func (m Model) contextBlockWidth() int {
	w := 0
	for _, l := range m.contextLines() {
		w = max(w, lipgloss.Width(l))
	}
	return min(w, contextBlockMaxWidth)
}

// headerLogo picks the largest wordmark the terminal has room for.
//
// The wordmark is decoration, so it yields rather than displaces: it is only
// drawn in the space left over once the context block, the key legend and the
// quick searches have taken theirs. On a wide terminal that is the full ANSI
// Shadow mark; on an ordinary one the compact mark; on a narrow one nothing.
//
// The quick-search grid is counted whether or not there is anything in it, so
// that recording a first search cannot change the wordmark -- and with it the
// height of the header and the number of rows in the table below.
func (m Model) headerLogo() []string {
	need := m.contextBlockWidth() +
		headerBlockGap + shortcutBlockWidth() +
		headerBlockGap + quickBlockWidth
	for _, logo := range [][]string{logoFull, logoCompact} {
		if need+headerBlockGap+logoWidth(logo) <= m.width {
			return logo
		}
	}
	return nil
}

// renderHeader lays out three blocks across the header rows: session context
// on the left, the quick-search grid in the middle, the wordmark on the
// right. Blocks are dropped from the right as the terminal narrows.
func (m Model) renderHeader() string {
	ctx := m.contextLines()
	contextWidth := m.contextBlockWidth()

	// Blocks are placed from both edges inward and dropped as the terminal
	// narrows: the wordmark first, then the quick searches, then the key
	// legend. The context block always survives.
	logo := m.headerLogo()
	logoAt := -1
	if len(logo) > 0 {
		logoAt = m.width - logoWidth(logo)
	}

	rightEdge := m.width
	if logoAt >= 0 {
		rightEdge = logoAt - headerBlockGap
	}

	// The general keys sit next to the context block, k9s-style: they apply
	// everywhere, so they belong with the session information rather than in
	// the footer where the screen-specific keys live. They are placed before
	// the quick searches because they are the only thing on screen saying
	// how to get anywhere.
	shortcuts := generalShortcuts()
	shortcutsAt := -1
	if contextWidth+headerBlockGap+shortcutBlockWidth() <= rightEdge {
		shortcutsAt = contextWidth + headerBlockGap
	}

	usedLeft := contextWidth
	if shortcutsAt >= 0 {
		usedLeft = shortcutsAt + shortcutBlockWidth()
	}
	quick := m.quickSearchBlock()
	quickAt := -1
	if len(quick) > 0 && rightEdge-quickBlockWidth >= usedLeft+headerBlockGap {
		quickAt = rightEdge - quickBlockWidth
	}

	height := m.headerHeight()
	lines := make([]string, height)
	for i := range height {
		var b strings.Builder
		if i < len(ctx) {
			b.WriteString(ctx[i])
		}
		if shortcutsAt >= 0 && i < len(shortcuts) {
			b.WriteString(spaces(shortcutsAt - lipgloss.Width(b.String())))
			b.WriteString(shortcuts[i])
		}
		if quickAt >= 0 && i < len(quick) {
			b.WriteString(spaces(quickAt - lipgloss.Width(b.String())))
			b.WriteString(quick[i])
		}
		if logoAt >= 0 && i < len(logo) {
			b.WriteString(spaces(max(1, logoAt-lipgloss.Width(b.String()))))
			b.WriteString(accentStyle("").Render(logo[i]))
		}
		lines[i] = strings.TrimRight(b.String(), " ")
	}
	return strings.Join(lines, "\n")
}

// generalShortcutPairs is what works on every screen: changing view,
// searching, moving around and backing out. Anything a particular screen
// offers -- adding an owner, jumping to a paired object -- stays in that
// screen's footer, so the two legends never say the same thing twice.
//
// The two columns are read down, not across: the first is what changes
// screen, the second is what moves within one.
var generalShortcutPairs = [2][][2]string{
	{
		{":", "view"},
		{"/", "search"},
		{"~", "home"},
		{"?", "help"},
		{"q", "quit"},
	},
	{
		{"esc", "back"},
		{"c", "copy"},
		{"↑↓", "move"},
		{"←→", "tab"},
		{"pg↑↓", "page"},
	},
}

// generalShortcuts renders the legend as rows of a fixed-width block.
func generalShortcuts() []string {
	widths := [2]int{}
	rows := 0
	for c, col := range generalShortcutPairs {
		rows = max(rows, len(col))
		for _, p := range col {
			widths[c] = max(widths[c], lipgloss.Width(p[0])+1+lipgloss.Width(p[1]))
		}
	}

	out := make([]string, rows)
	for i := range rows {
		var b strings.Builder
		for c, col := range generalShortcutPairs {
			if c > 0 {
				b.WriteString(spaces(shortcutColumnGap))
			}
			entry := ""
			if i < len(col) {
				entry = styleHintKey.Render(col[i][0]) + styleHintDesc.Render(" "+col[i][1])
			}
			b.WriteString(entry + spaces(widths[c]-lipgloss.Width(entry)))
		}
		out[i] = b.String()
	}
	return out
}

// shortcutBlockWidth is the width generalShortcuts renders to.
func shortcutBlockWidth() int {
	w := 0
	for _, l := range generalShortcuts() {
		w = max(w, lipgloss.Width(l))
	}
	return w
}

// contextLines describe who is signed in and what is happening.
func (m Model) contextLines() []string {
	tenant := m.identity.TenantID
	if tenant == "" {
		tenant = "unknown"
	}
	method := string(m.identity.Method)
	if method == "" {
		method = "unknown"
	}
	return []string{
		styleContextKey.Render("Version  ") + styleDim.Render(m.versionLabel()) +
			styleOK.Render(m.opts.Telemetry.UpdateSuffix()),
		styleContextKey.Render("Tenant   ") + styleContextVal.Render(graph.Truncate(tenant, 36)),
		styleContextKey.Render("Account  ") + styleContextVal.Render(graph.Truncate(m.identity.Label(), 36)),
		styleContextKey.Render("Signed   ") + styleDim.Render(method),
		styleContextKey.Render("Status   ") + m.statusIndicator(),
	}
}

// versionLabel is the build this is, for the header.
func (m Model) versionLabel() string {
	if m.opts.Version == "" {
		return "dev"
	}
	return graph.Truncate(m.opts.Version, 24)
}

// statusIndicator reports in-flight work.
func (m Model) statusIndicator() string {
	switch {
	case m.loading:
		return styleDim.Render(m.spin.View() + " loading")
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
func (m Model) quickSearchBlock() []string {
	// The slots replay searches over a table, so the table is the only place
	// they mean anything. On the dashboard the digits open views instead, and
	// a detail pane has nothing to search at all.
	if m.coll == nil || m.screen != screenBrowse {
		return nil
	}
	kind := string(m.coll.res.Kind)

	// Before anything has been searched there is nothing to show: the key
	// legend beside it already says how to search.
	if len(m.history.list(kind)) == 0 {
		return nil
	}

	slots := m.history.slotsFor(kind)
	lines := make([]string, quickSearchRows)
	for r := range quickSearchRows {
		lines[r] = m.quickEntry(slots, r) + "  " + m.quickEntry(slots, r+quickSearchRows)
	}
	return lines
}

// quickEntry renders one slot as "[n] term", padded to a fixed width so the
// two columns stay aligned.
func (m Model) quickEntry(slots []string, index int) string {
	// "[n] " is four cells; the rest is the term. Getting this arithmetic
	// wrong by one shifts the whole right-hand column.
	const labelWidth = 4

	if index >= len(slots) {
		return spaces(quickEntryWidth)
	}
	if term := slots[index]; term == "" {
		// An unused slot shows its digit and nothing else: the position is
		// worth advertising, a placeholder glyph is not.
		return styleDim.Render("["+quickDigit(index)+"]") + spaces(quickEntryWidth-3)
	}

	style := styleHintDesc
	if slots[index] == m.coll.search {
		style = styleOK
	}
	text := bidi.Display(graph.Truncate(slots[index], quickEntryWidth-labelWidth))
	return styleHintKey.Render("["+quickDigit(index)+"]") + style.Render(" "+text) +
		spaces(quickEntryWidth-labelWidth-lipgloss.Width(text))
}

// quickDigit maps a slot index to the key that replays it. The slots are
// numbered from zero so the digit on a slot is the digit you press: "1-0"
// as a range read as a countdown, and left readers hunting for slot ten.
func quickDigit(index int) string {
	return itoa(index)
}

// ------------------------------------------------------------------ prompt

// renderPromptLine draws the command or search line. It returns empty while
// no prompt is open, and the caller omits the row entirely.
func (m Model) renderPromptLine() string {
	if m.mode == modeNormal {
		return ""
	}
	line := stylePrompt.Render(m.promptPrefix()) + m.promptValue()
	if m.mode == modeCommand {
		line += m.renderCommandChoices(lipgloss.Width(line))
	}
	return line
}

// promptValue draws what has been typed.
//
// The input's own view puts the string on screen in logical order, which
// shows a right-to-left term reversed -- searching for a Hebrew name meant
// typing it and watching it come out backwards. Reordering costs the cursor
// its place mid-string, so it is only done when there is right-to-left text
// to fix, and the cursor is drawn at the end, where typing leaves it.
func (m Model) promptValue() string {
	value := m.input.Value()
	if !bidi.Contains(value) {
		return m.input.View()
	}
	return styleContextVal.Render(bidi.Display(value)) + styleDim.Render("▌")
}

// renderCommandChoices lists the views the typed prefix still matches, with
// the one enter would open marked. Names are dropped from the right as the
// line fills rather than the whole strip being cut, which would sever the
// styling on the last one drawn.
func (m Model) renderCommandChoices(used int) string {
	matches := m.commandMatches()
	if len(matches) == 0 {
		return "  " + styleWarn.Render("no such view")
	}
	chosen, _ := m.selectedCommand()

	var b strings.Builder
	room := m.width - used
	for _, name := range matches {
		entry := "  " + name
		if lipgloss.Width(entry) > room {
			break
		}
		if name == chosen {
			b.WriteString("  " + styleRowSelected.Render(name))
		} else {
			b.WriteString("  " + styleDim.Render(name))
		}
		room -= lipgloss.Width(entry)
	}
	if hint := "   ↑↓"; len(matches) > 1 && lipgloss.Width(hint) <= room {
		b.WriteString(styleDim.Render(hint))
	}
	return b.String()
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

// renderFooter is the status line, the key hints, and the trail showing
// where in the directory the screen is.
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
	// The hints sit against the frame's bottom edge, a rule separates them
	// from the trail, and the trail shares its row with whatever the app has
	// to say -- an error or a confirmation -- pushed to the right.
	rule := styleBorder.Render(strings.Repeat(boxHorizontal, max(0, m.width)))
	return second + "\n" + rule + "\n" + m.renderTrailLine()
}

// renderTrailLine is the bottom row: the trail on the left, the status on the
// right. They share a row because the status is usually empty, and a footer
// that changes height would move the frame above it.
func (m Model) renderTrailLine() string {
	status := m.renderStatusLine()
	trail := m.renderBreadcrumb(max(0, m.width-lipgloss.Width(status)-2))
	gap := m.width - lipgloss.Width(trail) - lipgloss.Width(status)
	if gap < 1 {
		return trail
	}
	return trail + spaces(gap) + status
}

// renderBreadcrumb draws the trail to what is on screen, fitted to width.
//
// Following a link out of a membership list can go several objects deep, and
// the pane's own title only names the object in front of you; the trail is
// what says which group you reached this user through, and how far esc has
// to take you back.
func (m Model) renderBreadcrumb(width int) string {
	trail := m.breadcrumb()
	if len(trail) == 0 || width <= 0 {
		return ""
	}
	const separator = " › "

	// Drop leading steps rather than the object in front: which group you
	// came through is worth losing before what you are looking at. An
	// ellipsis stands in for whatever was dropped.
	steps := trail
	for start := 1; len(steps) > 1 && trailWidth(steps, separator) > width; start++ {
		steps = append([]crumb{{text: "…", style: styleDim}}, trail[start:]...)
	}

	// A step wider than the terminal is cut before it is styled. Cutting the
	// finished line instead severs an escape sequence -- and counts those
	// escapes as characters, which is what once ate the object's name and
	// left the trail ending in a separator.
	last := len(steps) - 1
	steps[last].text = graph.Truncate(steps[last].text, width)

	var b strings.Builder
	for i, step := range steps {
		if i > 0 {
			b.WriteString(styleDim.Render(separator))
		}
		b.WriteString(step.style.Render(bidi.Display(step.text)))
	}
	return b.String()
}

// crumb is one step of the trail, and how it is drawn. The steps are tinted
// by what they are: the view in its own accent, the objects linked through in
// passing, and what is in front of you brightest.
type crumb struct {
	text  string
	style lipgloss.Style
}

// trailWidth is what a trail costs on screen, separators included.
func trailWidth(trail []crumb, separator string) int {
	w := (len(trail) - 1) * lipgloss.Width(separator)
	for _, s := range trail {
		w += lipgloss.Width(s.text)
	}
	return w
}

// breadcrumb is the trail as steps, outermost first.
//
// The dashboard is not a step: it is where you start, so naming it on every
// screen says nothing. It appears only when it is what you are looking at.
func (m Model) breadcrumb() []crumb {
	switch {
	case m.screen == screenDashboard:
		return []crumb{{text: "Dashboard", style: styleContextVal}}
	case m.screen == screenHelp:
		return []crumb{{text: "Help", style: styleContextVal}}
	case m.coll == nil:
		return nil
	}

	view := m.coll.res.Title
	if m.coll.search != "" {
		view += ` "` + m.coll.search + `"`
	}
	trail := []crumb{{text: view, style: accentStyle(m.coll.res.Accent)}}
	if m.screen != screenDetail {
		return trail
	}

	for _, f := range m.detailStack {
		trail = append(trail, crumb{text: objectLabel(f.detail, f.id), style: styleDim})
	}
	trail = append(trail, crumb{text: objectLabel(m.detail, m.detailID), style: styleContextVal})

	// Only the object in front is bright; the one before it is the pane esc
	// goes back to, which is worth reading over the rest.
	if n := len(trail); n > 2 {
		trail[n-2].style = styleDetailVal
	}
	return trail
}

// objectLabel is what to call an object in the trail.
func objectLabel(d graph.Detail, id string) string {
	if name := d.Object.String("displayName"); name != "" {
		return name
	}
	return id
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
	// The table is inset by a space on each side, so its text does not sit
	// flush against the frame. The margin is part of the row rather than
	// outside it, which keeps the selection bar spanning the full width.
	inner := boxInnerWidth(m.width) - 2*tableMargin
	cols := m.coll.res.Columns
	widths := layout(cols, inner)

	pad := func(line string) string {
		if fill := inner - lipgloss.Width(line); fill > 0 {
			line += spaces(fill)
		}
		return spaces(tableMargin) + line + spaces(tableMargin)
	}

	body := make([]string, 0, m.contentHeight())
	body = append(body, styleTableHead.Render(pad(headerCells(cols, widths))))

	if m.coll.len() == 0 {
		body = append(body, spaces(tableMargin)+styleDim.Render(m.emptyMessage()))
	}

	height := m.tableHeight()
	for i := m.offset; i < m.coll.len() && len(body) <= height; i++ {
		item, cells, ok := m.coll.at(i)
		if !ok {
			break
		}
		style := rowStyle(m.coll.res, item)
		if i == m.cursor {
			style = selected(style)
		}
		body = append(body, style.Render(pad(renderCells(cells, widths))))
	}

	hints := hintBar(m.width,
		[2]string{"enter", "describe"},
		[2]string{"0-9", "replay a search"},
		[2]string{"r", "refresh"},
	)
	return m.chrome(m.tableCaption(), m.tableFooterCaption(), body, hints)
}

// rowStyle tints a whole row by the object's state, so a disabled account or
// a lapsed credential is visible without reading the column that says so.
func rowStyle(res graph.Resource, item graph.Item) lipgloss.Style {
	if res.State == nil {
		return styleRow
	}
	switch res.State(item) {
	case graph.RowMuted:
		return styleRowMuted
	case graph.RowWarn:
		return styleRowWarn
	default:
		return styleRow
	}
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

	// Only a searched view is sorted, so only a searched view says so.
	if m.coll.search != "" {
		parts = append(parts, styleOK.Render("search: "+bidi.Display(m.coll.search)))
		parts = append(parts, styleDim.Render("↑name"))
	}
	return strings.Join(parts, styleDim.Render(" · "))
}

// tableFooterCaption notes that the view is not fully loaded.
//
// There is no key to page: scrolling past the last loaded row fetches the
// next page, so the caption reports state rather than offering a command.
func (m Model) tableFooterCaption() string {
	if !m.coll.hasMore() {
		return ""
	}
	return styleDim.Render("more below — scroll to load")
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
		[2]string{":  ↑/↓", "pick from what a prefix matches"},
	)

	cols := lipgloss.JoinHorizontal(lipgloss.Top,
		section("NAVIGATION", [][2]string{
			{"↑/k ↓/j", "move cursor, or walk the list in front"},
			{"←/→", "switch list tab"},
			{"pgup/pgdn", "page the list"},
			{"g / G", "top / bottom of the list"},
			{"enter", "describe, or open a linked object"},
			{"esc", "back one layer"},
			{"x", "app reg ⇄ ent app"},
			{"q", "quit"},
		}),
		"    ",
		section("VIEWS", viewRows),
	)

	cols2 := lipgloss.JoinHorizontal(lipgloss.Top,
		section("SEARCH", [][2]string{
			{"/", "search the directory"},
			{"0-9", "replay a slot"},
			{":", "command prompt"},
		}),
		"    ",
		section("DATA", [][2]string{
			{"r", "refresh from Graph"},
			{"R", "raw json (detail)"},
			{"c", "copy object id"},
			{"a", "add to the list in front"},
			{"d", "delete the selected row"},
		}),
	)

	note := styleDim.Render(
		"Search runs against Microsoft Graph, not just the rows on screen.\n" +
			"esc backs out; it does not clear a search. Run an empty search to do that.\n" +
			"Slots keep fixed positions, so a digit always replays the same term. A search\n" +
			"that finds nothing keeps no slot.\n" +
			"a adds to whichever list is in front, so it means member on one tab and owner\n" +
			"on the next; d deletes the row under the cursor from that same list.\n" +
			"entra-tui reads the directory and edits only members and owners.")

	body := strings.Split(strings.Join([]string{cols, "", cols2, "", note}, "\n"), "\n")
	return m.chrome(styleHelpTitle.Render("HELP"), "", body,
		hintBar(m.width, [2]string{"any key", "back"}))
}

// ----------------------------------------------------------------- helpers

// itoa renders a small non-negative integer without pulling in strconv at
// every call site.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

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
