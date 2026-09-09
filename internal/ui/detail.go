package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/idoavrah/entra-tui/internal/bidi"
	"github.com/idoavrah/entra-tui/internal/graph"
)

// Detail pane layout.
const (
	// twoColumnMinWidth is the narrowest frame that fits two readable
	// columns of property sections. Below it everything stacks in one.
	twoColumnMinWidth = 132
	// detailColumnGap separates the two section columns.
	detailColumnGap = 4
	// The label column sizes itself to its content between these bounds, so
	// short labels do not leave a gutter and long ones do not wrap.
	detailLabelMin = 12
	detailLabelMax = 30
	// detailIndent is the left inset of a field inside its section.
	detailIndent = 2
	// detailGutter separates a label from its value.
	detailGutter = 2

	// tabBarHeight is the strip naming the lists.
	tabBarHeight = 1
	// tabChromeHeight is the table's own furniture: top rule, header,
	// divider, bottom rule.
	tabChromeHeight = 4
	// tabMinRows is the smallest useful list pane: its furniture plus two
	// rows of content.
	tabMinRows = tabChromeHeight + 2
	// tabCellPadding is the blank cell either side of a column's text.
	tabCellPadding = 1
	// tabMinColumnWidth stops a squeezed column collapsing to nothing.
	tabMinColumnWidth = 4
)

// detailEntry is one selectable object in a list tab -- a member or an owner.
type detailEntry struct {
	rel  graph.Relationship
	id   string
	name string
	// detail is the entry's secondary text -- the sign-in name or object
	// kind -- carried so a confirmation can identify it without guessing.
	detail string
}

// propertySections are the sections describing the object itself.
func (m Model) propertySections() []graph.Section {
	var out []graph.Section
	for _, s := range m.detailSections {
		if !s.List {
			out = append(out, s)
		}
	}
	return out
}

// listSections are the sections enumerating other objects, shown as tabs.
func (m Model) listSections() []graph.Section {
	var out []graph.Section
	for _, s := range m.detailSections {
		if s.List {
			out = append(out, s)
		}
	}
	return out
}

// activeSection is the list tab in front.
func (m Model) activeSection() (graph.Section, bool) {
	lists := m.listSections()
	if len(lists) == 0 {
		return graph.Section{}, false
	}
	return lists[clamp(m.detailTab, 0, len(lists)-1)], true
}

// selectedEntry is the object under the cursor in the active tab, when that
// tab lists something that can be added to or removed.
func (m Model) selectedEntry() (detailEntry, bool) {
	section, ok := m.activeSection()
	if !ok || section.Relationship == "" {
		return detailEntry{}, false
	}
	if m.tabCursor < 0 || m.tabCursor >= len(section.Fields) {
		return detailEntry{}, false
	}
	f := section.Fields[m.tabCursor]
	if f.ID == "" {
		return detailEntry{}, false
	}
	return detailEntry{
		rel: section.Relationship, id: f.ID, name: f.Label, detail: f.Value,
	}, true
}

// detailHasRelationship reports whether the open object has an editable
// collection of the given kind.
func (m Model) detailHasRelationship(rel graph.Relationship) bool {
	for _, s := range m.detailSections {
		if s.Relationship == rel {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------- geometry

// propertyHeight is how many rows the property region gets.
//
// The lists get whatever the properties do not need, down to a floor: an
// object with two fields should not reserve half the pane for them, and one
// with thirty should not squeeze the lists out entirely.
func (m Model) propertyHeight(total int) int {
	if len(m.listSections()) == 0 {
		return total
	}
	natural := len(m.propertyLines())
	room := total - tabBarHeight - tabMinRows
	if room < 1 {
		return max(1, total)
	}
	return clamp(natural, 1, min(room, max(1, total/2)))
}

// ---------------------------------------------------------------- rendering

// renderDetail draws the whole detail screen.
func (m Model) renderDetail() string {
	name := m.detail.Object.String("displayName")
	if name == "" {
		name = m.detailID
	}
	mode := "sections"
	if m.detailRaw {
		mode = "raw json"
	}

	caption := accentStyle(m.coll.res.Accent).Render(m.detail.Kind.Title()) +
		styleDim.Render(" · ") + styleContextVal.Render(bidi.Display(name)) +
		styleDim.Render(" · "+mode)

	if m.modal != modalNone {
		return m.chrome(caption, "", m.renderModal(m.contentHeight()), m.detailHints())
	}
	return m.chrome(caption, m.detailFooterCaption(), m.detailBody(), m.detailHints())
}

// detailBody assembles the property region, the tab strip and the active
// list into one pane-height block.
func (m Model) detailBody() []string {
	height := m.contentHeight()
	// The viewport is sized here rather than in Update because how much room
	// the properties get depends on whether this object has any lists.
	vp := m.detailVP
	if m.detailRaw {
		vp.Height = height
		return strings.Split(vp.View(), "\n")
	}

	propHeight := m.propertyHeight(height)
	vp.Height = propHeight
	body := strings.Split(vp.View(), "\n")

	lists := m.listSections()
	if len(lists) == 0 {
		return body
	}

	body = append(body, m.renderTabBar())
	return append(body, m.renderTabRows(max(0, height-propHeight-tabBarHeight))...)
}

// renderTabBar names each list and how much is in it.
//
// Width is tracked on the plain labels as the bar is built, never by
// truncating the finished string: the tabs are styled, and cutting a styled
// string by rune index severs an escape sequence -- which is exactly how the
// bar lost a tab and gained a stray border character.
func (m Model) renderTabBar() string {
	lists := m.listSections()
	if len(lists) == 0 {
		return ""
	}
	active := clamp(m.detailTab, 0, len(lists)-1)

	const separator = "│"
	budget := boxInnerWidth(m.width)

	var bar strings.Builder
	used := 0
	for i, s := range lists {
		label := " " + s.Title
		if n := len(s.Fields); n > 0 {
			label += " (" + itoa(n) + ")"
		}
		label += " "

		cost := len([]rune(label))
		if i > 0 {
			cost++ // the separator
		}
		if used+cost > budget {
			break
		}
		if i > 0 {
			bar.WriteString(styleDim.Render(separator))
		}
		if i == active {
			bar.WriteString(styleTabActive.Render(label))
		} else {
			bar.WriteString(styleTabIdle.Render(label))
		}
		used += cost
	}

	if hint := "  ←/→"; len(lists) > 1 && used+len(hint) <= budget {
		bar.WriteString(styleDim.Render(hint))
	}
	return bar.String()
}

// renderTabRows draws the active list as a bordered table.
func (m Model) renderTabRows(height int) []string {
	section, ok := m.activeSection()
	if !ok || height <= 0 {
		return nil
	}

	if len(section.Fields) == 0 {
		note := section.Note
		if note == "" {
			note = "Nothing here."
		}
		return []string{spaces(detailIndent) +
			styleDim.Render(graph.Truncate(note, boxInnerWidth(m.width)-detailIndent))}
	}

	columns := section.Columns
	if len(columns) == 0 {
		// A section that predates columns still renders as two.
		columns = []string{"NAME", "DETAIL"}
	}

	width := boxInnerWidth(m.width)
	widths := tabColumnWidths(columns, section.Fields, width)
	rows := max(0, height-tabChromeHeight)

	out := []string{
		tabRule(widths, "┌", "┬", "┐"),
		tabRow(headerCellsFor(columns, widths), widths, styleTableHead),
		tabRule(widths, "├", "┼", "┤"),
	}

	offset := clamp(m.tabOffset, 0, max(0, len(section.Fields)-1))
	for i := offset; i < len(section.Fields) && len(out)-3 < rows; i++ {
		f := section.Fields[i]

		style := styleRow
		if f.Warn {
			style = styleRowWarn
		}
		// Every list is walkable, whether or not its rows can be acted on:
		// reading a long list is reason enough to move through it.
		if i == m.tabCursor {
			style = styleRowSelected
		}
		out = append(out, tabRow(cellsFor(f, columns), widths, style))
	}
	return append(out, tabRule(widths, "└", "┴", "┘"))
}

// cellsFor returns a field's cells, padded or trimmed to the column count.
func cellsFor(f graph.Field, columns []string) []string {
	cells := f.Cells
	if len(cells) == 0 {
		cells = []string{f.Label, f.Value}
	}
	out := make([]string, len(columns))
	copy(out, cells)
	return out
}

func headerCellsFor(columns []string, widths []int) []string {
	out := make([]string, len(widths))
	copy(out, columns)
	return out
}

// tabColumnWidths sizes a list table's columns to their content, then fits
// the result to the pane.
//
// Columns start at what they need and are shrunk proportionally when that
// does not fit, so a wide description gives ground before a short status
// column does.
func tabColumnWidths(columns []string, fields []graph.Field, width int) []int {
	n := len(columns)
	natural := make([]int, n)
	for i, c := range columns {
		natural[i] = lipgloss.Width(c)
	}
	for _, f := range fields {
		for i, cell := range cellsFor(f, columns) {
			natural[i] = max(natural[i], lipgloss.Width(cell))
		}
	}

	// Outer borders, one divider between each pair, and padding either side
	// of every cell.
	chrome := 2 + (n - 1) + 2*tabCellPadding*n
	avail := max(n*tabMinColumnWidth, width-chrome)

	total := 0
	for _, w := range natural {
		total += w
	}

	switch {
	case total <= avail:
		// Share the slack in proportion to what each column already needs.
		// Handing it all to the widest turned a one-word MAIL column into
		// half the pane.
		slack := avail - total
		handed := 0
		for i := range natural {
			share := slack * natural[i] / max(1, total)
			natural[i] += share
			handed += share
		}
		if rem := slack - handed; rem > 0 {
			natural[len(natural)-1] += rem
		}
	default:
		assigned := 0
		for i := range natural {
			natural[i] = max(tabMinColumnWidth, natural[i]*avail/total)
			assigned += natural[i]
		}
		// Trim any overshoot from the widest column rather than everywhere.
		for over := assigned - avail; over > 0; {
			widest := 0
			for i, w := range natural {
				if w > natural[widest] {
					widest = i
				}
			}
			if natural[widest] <= tabMinColumnWidth {
				break
			}
			take := min(over, natural[widest]-tabMinColumnWidth)
			natural[widest] -= take
			over -= take
		}
	}
	return natural
}

// tabRule draws a horizontal border across the column layout.
func tabRule(widths []int, left, join, right string) string {
	parts := make([]string, len(widths))
	for i, w := range widths {
		parts[i] = strings.Repeat(boxHorizontal, w+2*tabCellPadding)
	}
	return styleBorder.Render(left + strings.Join(parts, join) + right)
}

// tabRow draws one row of cells between vertical borders.
//
// Each cell is fitted before it is styled: the row style is applied to the
// finished line, and truncating afterwards would cut an escape sequence.
func tabRow(cells []string, widths []int, style lipgloss.Style) string {
	var b strings.Builder
	b.WriteString(styleBorder.Render(boxVertical))
	for i, w := range widths {
		cell := ""
		if i < len(cells) {
			cell = bidi.Display(graph.Truncate(cells[i], w))
		}
		b.WriteString(spaces(tabCellPadding))
		b.WriteString(style.Render(cell + spaces(w-lipgloss.Width(cell))))
		b.WriteString(spaces(tabCellPadding))
		if i < len(widths)-1 {
			b.WriteString(styleBorder.Render(boxVertical))
		}
	}
	b.WriteString(styleBorder.Render(boxVertical))
	return b.String()
}

// detailHints are the keys this screen offers; the general ones live in the
// header.
func (m Model) detailHints() string {
	pairs := [][2]string{{"pgup/pgdn", "properties"}}
	if len(m.listSections()) > 1 {
		pairs = append(pairs, [2]string{"←/→", "tab"})
	}
	if _, ok := m.activeSection(); ok {
		pairs = append(pairs, [2]string{"↑/↓", "list"})
	}
	pairs = append(pairs, [2]string{"R", "raw json"})

	if m.detail.Kind == graph.KindAppRegistrations || m.detail.Kind == graph.KindEnterpriseApps {
		pairs = append(pairs, [2]string{"x", m.pairHint()})
	}
	if m.detailHasRelationship(graph.RelMembers) {
		pairs = append(pairs, [2]string{"a", "add member"})
	}
	if m.detailHasRelationship(m.ownerRelationship()) {
		pairs = append(pairs, [2]string{"o", "add owner"})
	}
	if _, ok := m.selectedEntry(); ok {
		pairs = append(pairs, [2]string{"d", "remove"})
	}
	pairs = append(pairs, [2]string{"c", "copy id"}, [2]string{"esc", "back"})
	return hintBar(m.width, pairs...)
}

// detailFooterCaption shows scroll position when the properties do not fit.
func (m Model) detailFooterCaption() string {
	if m.detailVP.AtTop() && m.detailVP.AtBottom() {
		return ""
	}
	return styleDim.Render(itoa(int(m.detailVP.ScrollPercent()*100)) + "%")
}

// pairHint names what the x key would open, so the binding is meaningful
// before it is pressed.
func (m Model) pairHint() string {
	if m.detail.Counterpart == nil {
		if m.detailLoading {
			return "paired object…"
		}
		return "find paired object"
	}
	if m.detail.Counterpart.Kind == graph.KindEnterpriseApps {
		return "enterprise app"
	}
	return "app registration"
}

// ------------------------------------------------------- property sections

// propertyLines renders the object's own fields, in two columns when the
// frame is wide enough to hold them.
func (m Model) propertyLines() []string {
	if m.detail.Object == nil {
		return []string{styleDim.Render("nothing selected")}
	}

	sections := m.propertySections()
	if len(sections) == 0 {
		return nil
	}

	inner := boxInnerWidth(m.width)
	columns := 1
	if inner >= twoColumnMinWidth {
		columns = 2
	}
	columnWidth := (inner - (columns-1)*detailColumnGap) / columns
	labelWidth := detailLabelWidth(sections, columnWidth)

	blocks := make([][]string, 0, len(sections))
	for _, section := range sections {
		blocks = append(blocks, renderSection(section, labelWidth, columnWidth))
	}

	var out []string
	if columns == 1 {
		out = flatten(blocks)
	} else {
		out = strings.Split(twoColumnLayout(blocks, columnWidth), "\n")
	}
	if m.detailLoading {
		out = append(out, styleDim.Render(m.spin.View()+" gathering related objects…"))
	}
	return out
}

// propertyContent is what the property viewport scrolls over.
func (m Model) propertyContent() string {
	if m.detailRaw {
		if m.detail.Object == nil {
			return ""
		}
		return m.detail.Object.JSON()
	}
	return strings.Join(m.propertyLines(), "\n")
}

// detailLabelWidth sizes the label column to the longest label actually
// present, so short-labelled objects do not leave a gutter and long ones are
// not wrapped. It is bounded so one pathological label cannot squeeze the
// values off the pane.
func detailLabelWidth(sections []graph.Section, columnWidth int) int {
	longest := 0
	for _, s := range sections {
		for _, f := range s.Fields {
			longest = max(longest, lipgloss.Width(f.Label))
		}
	}
	upper := min(detailLabelMax, max(detailLabelMin, columnWidth/2))
	return clamp(longest, detailLabelMin, upper)
}

// renderSection renders one titled group of properties.
func renderSection(s graph.Section, labelWidth, columnWidth int) []string {
	valueWidth := max(10, columnWidth-labelWidth-detailIndent-detailGutter)

	lines := []string{styleSectionTitle.Render("▌ " + strings.ToUpper(s.Title))}
	if s.Note != "" {
		for _, l := range wrapLines(s.Note, columnWidth-detailIndent) {
			lines = append(lines, spaces(detailIndent)+styleDim.Render(l))
		}
	}
	for _, f := range s.Fields {
		lines = append(lines, renderFieldLines(f, labelWidth, valueWidth)...)
	}
	return append(lines, "")
}

// renderFieldLines renders one label/value pair. Multi-valued fields list one
// entry per line under the same label.
func renderFieldLines(f graph.Field, labelWidth, valueWidth int) []string {
	// Labels are not always static text: an owner's name, an app role and an
	// assigned principal all arrive as labels, so they need the same
	// right-to-left treatment as values. Truncation runs first, while the
	// text is still in logical order.
	label := bidi.Display(graph.Truncate(f.Label, labelWidth))
	head := spaces(detailIndent) + styleDetailKey.Render(label) +
		spaces(labelWidth-lipgloss.Width(label)+detailGutter)
	indent := spaces(detailIndent + labelWidth + detailGutter)

	valueStyle := styleDetailVal
	if f.Warn {
		valueStyle = styleWarn
	}

	values := f.Values
	if len(values) == 0 {
		values = []string{f.Value}
	}

	var lines []string
	for _, v := range values {
		for _, wrapped := range wrapLines(bidi.Display(v), valueWidth) {
			prefix := indent
			if len(lines) == 0 {
				prefix = head
			}
			lines = append(lines, prefix+valueStyle.Render(wrapped))
		}
	}
	if len(lines) == 0 {
		lines = append(lines, head)
	}
	return lines
}

// wrapLines breaks text to a width, preferring word boundaries and falling
// back to a hard break for anything unbreakable, such as a URL or a GUID.
func wrapLines(s string, width int) []string {
	if width <= 0 {
		return []string{s}
	}
	if lipgloss.Width(s) <= width {
		return []string{s}
	}

	var out []string
	for _, word := range strings.Fields(s) {
		switch {
		case len(out) == 0:
			out = append(out, word)
		case lipgloss.Width(out[len(out)-1])+1+lipgloss.Width(word) <= width:
			out[len(out)-1] += " " + word
		default:
			out = append(out, word)
		}
	}

	// Any single token still too long is cut rather than allowed to bleed
	// into the neighbouring column.
	var final []string
	for _, line := range out {
		for lipgloss.Width(line) > width {
			runes := []rune(line)
			final = append(final, string(runes[:width]))
			line = string(runes[width:])
		}
		final = append(final, line)
	}
	if len(final) == 0 {
		return []string{""}
	}
	return final
}

// twoColumnLayout distributes section blocks across two columns, keeping
// reading order down the left column and then the right.
//
// A section is never split across the boundary -- one broken over two columns
// is harder to read than an uneven pair -- so the only choice is where to cut
// the list. The cut that minimises the taller column is used, which is not
// the same as the first cut that passes the halfway mark: with sections of 3,
// 3 and 2 lines, stopping at halfway gives columns of 6 and 2, while cutting
// one section earlier gives 3 and 5.
func twoColumnLayout(blocks [][]string, columnWidth int) string {
	split := balancedSplit(blocks)
	leftLines, rightLines := flatten(blocks[:split]), flatten(blocks[split:])
	height := max(len(leftLines), len(rightLines))

	var out strings.Builder
	for i := range height {
		l, r := "", ""
		if i < len(leftLines) {
			l = leftLines[i]
		}
		if i < len(rightLines) {
			r = rightLines[i]
		}
		out.WriteString(l)
		if r != "" {
			out.WriteString(spaces(columnWidth - lipgloss.Width(l) + detailColumnGap))
			out.WriteString(r)
		}
		if i < height-1 {
			out.WriteByte('\n')
		}
	}
	return out.String()
}

// balancedSplit picks the index at which to move to the second column so the
// taller column is as short as possible. It always leaves at least one block
// on the left.
func balancedSplit(blocks [][]string) int {
	total := 0
	for _, b := range blocks {
		total += len(b)
	}

	best, bestHeight := 1, total
	running := 0
	for i, b := range blocks {
		running += len(b)
		split := i + 1
		if split >= len(blocks) {
			break
		}
		if height := max(running, total-running); height < bestHeight {
			best, bestHeight = split, height
		}
	}
	return best
}

// flatten concatenates blocks of lines.
func flatten(blocks [][]string) []string {
	var out []string
	for _, b := range blocks {
		out = append(out, b...)
	}
	return out
}
