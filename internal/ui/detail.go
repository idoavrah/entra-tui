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
	// columns of sections. Below it everything stacks in one.
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
)

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

	hintPairs := [][2]string{
		{"↑/↓", "scroll"},
		{"R", "raw json"},
	}
	if m.detail.Kind == graph.KindAppRegistrations || m.detail.Kind == graph.KindEnterpriseApps {
		hintPairs = append(hintPairs, [2]string{"x", m.pairHint()})
	}
	hintPairs = append(hintPairs,
		[2]string{"c", "copy id"},
		[2]string{"esc", "back"},
		[2]string{"~", "dashboard"},
	)

	body := strings.Split(m.detailVP.View(), "\n")
	return m.chrome(caption, m.detailFooterCaption(), body, hintBar(m.width, hintPairs...))
}

// detailFooterCaption shows scroll position when the object does not fit.
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

// renderDetailBody formats the selected object for the viewport, spreading
// sections across two columns when the frame is wide enough to hold them.
func (m Model) renderDetailBody() string {
	if m.detail.Object == nil {
		return styleDim.Render("nothing selected")
	}
	if m.detailRaw {
		return m.detail.Object.JSON()
	}

	inner := boxInnerWidth(m.width)
	columns := 1
	if inner >= twoColumnMinWidth {
		columns = 2
	}
	columnWidth := (inner - (columns-1)*detailColumnGap) / columns
	labelWidth := detailLabelWidth(m.detailSections, columnWidth)

	blocks := make([][]string, 0, len(m.detailSections))
	for _, section := range m.detailSections {
		blocks = append(blocks, renderSection(section, labelWidth, columnWidth))
	}

	var out string
	if columns == 1 {
		out = strings.Join(flatten(blocks), "\n")
	} else {
		out = twoColumnLayout(blocks, columnWidth)
	}

	if m.detailLoading {
		out += "\n" + styleDim.Render(m.spin.View()+" gathering related objects…")
	}
	return out
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

// renderSection renders one titled group, returning its lines.
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
