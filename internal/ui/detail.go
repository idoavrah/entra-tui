package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/idoavrah/entra-tui/internal/bidi"
	"github.com/idoavrah/entra-tui/internal/graph"
)

// detailLabelWidth is the width of the label column in the detail pane,
// clamped against narrow terminals at render time.
const detailLabelWidth = 26

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

	bar := accentStyle(m.coll.res.Accent).Render(m.detail.Kind.Title()+" › ") +
		styleContextVal.Render(bidi.Display(name)) +
		styleDim.Render("  ("+mode+")")

	hintPairs := [][2]string{
		{"↑/↓", "scroll"},
		{"R", "raw json"},
	}
	if m.detail.Kind == graph.KindAppRegistrations || m.detail.Kind == graph.KindEnterpriseApps {
		hintPairs = append(hintPairs, [2]string{"x", m.pairHint()})
	}
	hintPairs = append(hintPairs,
		[2]string{"y", "yank id"},
		[2]string{"esc", "back"},
		[2]string{"~", "dashboard"},
	)

	return strings.Join([]string{
		m.renderHeader(),
		m.renderPromptLine(),
		bar,
		m.detailVP.View(),
		m.renderFooter(hintBar(hintPairs...)),
	}, "\n")
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

// renderDetailBody formats the selected object for the viewport.
func (m Model) renderDetailBody() string {
	if m.detail.Object == nil {
		return styleDim.Render("nothing selected")
	}
	if m.detailRaw {
		return m.detail.Object.JSON()
	}

	labelWidth := min(detailLabelWidth, max(10, m.width/3))
	valueWidth := max(20, m.width-labelWidth-3)

	var b strings.Builder
	for i, section := range m.detailSections {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(styleSectionTitle.Render("▌ "+strings.ToUpper(section.Title)) + "\n")

		if section.Note != "" {
			b.WriteString("  " + styleDim.Render(section.Note) + "\n")
		}
		for _, field := range section.Fields {
			b.WriteString(renderField(field, labelWidth, valueWidth))
		}
	}

	if m.detailLoading {
		b.WriteString("\n" + styleDim.Render(m.spin.View()+" gathering owners, permissions and related objects…"))
	}
	return b.String()
}

// renderField renders one label/value pair, wrapping long values and listing
// multi-valued fields one per line under the same label.
func renderField(f graph.Field, labelWidth, valueWidth int) string {
	// Labels are not always static text: an owner's name, an app role and an
	// assigned principal all arrive as labels, so they need the same
	// right-to-left treatment as values. Truncation runs first, while the
	// text is still in logical order.
	label := bidi.Display(graph.Truncate(f.Label, labelWidth))
	pad := max(0, labelWidth-lipgloss.Width(label))
	indent := strings.Repeat(" ", labelWidth+4)

	valueStyle := styleDetailVal
	if f.Warn {
		valueStyle = styleWarn
	}

	var b strings.Builder
	b.WriteString("  " + styleDetailKey.Render(label) + strings.Repeat(" ", pad+2))

	values := f.Values
	if len(values) == 0 {
		values = []string{f.Value}
	}
	for i, v := range values {
		if i > 0 {
			b.WriteString(indent)
		}
		b.WriteString(valueStyle.Render(wrapValue(displayValue(v), valueWidth, labelWidth+4)))
		b.WriteByte('\n')
	}
	return b.String()
}

// displayValue reorders right-to-left text for a terminal without bidi
// support. Values are left where they are rather than right-aligned: the
// detail pane has no fixed right edge to align against, and reordering alone
// is what makes the text readable.
func displayValue(s string) string {
	return bidi.Display(s)
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
