package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/idoavrah/entra-tui/internal/graph"
)

func testColumns() []graph.Column {
	return []graph.Column{
		{Title: "NAME", MinWidth: 10, Weight: 3},
		{Title: "UPN", MinWidth: 10, Weight: 1},
		{Title: "AGE", MinWidth: 5},
	}
}

func TestLayoutFillsAvailableWidthExactly(t *testing.T) {
	cols := testColumns()
	avail := 80
	widths := layout(cols, avail)

	if len(widths) != 3 {
		t.Fatalf("got %d columns, want all 3 at width %d", len(widths), avail)
	}
	total := 0
	for _, w := range widths {
		total += w
	}
	total += columnGap * (len(widths) - 1)
	if total != avail {
		t.Errorf("columns total %d, want exactly %d", total, avail)
	}
}

func TestLayoutRespectsMinWidthsAndWeights(t *testing.T) {
	cols := testColumns()
	widths := layout(cols, 80)

	for i, w := range widths {
		if w < cols[i].MinWidth {
			t.Errorf("column %d width %d below MinWidth %d", i, w, cols[i].MinWidth)
		}
	}
	// The zero-weight column must not grow.
	if widths[2] != 5 {
		t.Errorf("zero-weight column = %d, want it pinned at 5", widths[2])
	}
	// Weight 3 must receive more slack than weight 1.
	if widths[0]-cols[0].MinWidth <= widths[1]-cols[1].MinWidth {
		t.Errorf("growth was %d vs %d; the heavier column should grow more",
			widths[0]-cols[0].MinWidth, widths[1]-cols[1].MinWidth)
	}
}

func TestLayoutDropsTrailingColumnsWhenNarrow(t *testing.T) {
	cols := testColumns()
	// Room for the first two minimums (10 + 1 + 10 = 21) but not the third.
	widths := layout(cols, 22)

	if len(widths) != 2 {
		t.Fatalf("got %d columns at width 22, want 2 (rightmost dropped)", len(widths))
	}
}

func TestLayoutAlwaysKeepsOneColumn(t *testing.T) {
	widths := layout(testColumns(), 4)
	if len(widths) != 1 {
		t.Fatalf("got %d columns in a 4-cell terminal, want 1", len(widths))
	}
	if widths[0] != 4 {
		t.Errorf("width = %d, want it clamped to the terminal width", widths[0])
	}
}

func TestLayoutHandlesDegenerateInput(t *testing.T) {
	if got := layout(nil, 80); got != nil {
		t.Errorf("layout(nil) = %v, want nil", got)
	}
	if got := layout(testColumns(), 0); got != nil {
		t.Errorf("layout(width 0) = %v, want nil", got)
	}
}

func TestLayoutAllZeroWeightStillFillsWidth(t *testing.T) {
	cols := []graph.Column{
		{Title: "A", MinWidth: 5},
		{Title: "B", MinWidth: 5},
	}
	widths := layout(cols, 40)
	total := widths[0] + widths[1] + columnGap
	if total != 40 {
		t.Errorf("total = %d, want 40 even with no growing columns", total)
	}
}

func TestRenderCellsPadsAndTruncates(t *testing.T) {
	widths := []int{6, 4}
	got := renderCells([]string{"abc", "toolong"}, widths)

	want := "abc   " + strings.Repeat(" ", columnGap) + "too…"
	if got != want {
		t.Errorf("renderCells = %q, want %q", got, want)
	}
	if lipgloss.Width(got) != 6+columnGap+4 {
		t.Errorf("rendered width = %d, want %d", lipgloss.Width(got), 6+columnGap+4)
	}
}

func TestRenderCellsHandlesMissingCells(t *testing.T) {
	// A row shorter than the admitted column count must pad, not panic.
	got := renderCells([]string{"only"}, []int{6, 4})
	if lipgloss.Width(got) != 11 {
		t.Errorf("width = %d, want 11", lipgloss.Width(got))
	}
}

func TestRenderCellsKeepsWideGlyphsAligned(t *testing.T) {
	// CJK glyphs occupy two cells; padding by rune count would shear the
	// columns to their right.
	got := renderCells([]string{"日本", "x"}, []int{6, 3})
	if lipgloss.Width(got) != 6+columnGap+3 {
		t.Errorf("display width = %d, want %d", lipgloss.Width(got), 6+columnGap+3)
	}
}

func TestHeaderCellsMatchesAdmittedColumns(t *testing.T) {
	cols := testColumns()
	widths := layout(cols, 22)
	head := headerCells(cols, widths)

	if !strings.Contains(head, "NAME") || !strings.Contains(head, "UPN") {
		t.Errorf("header = %q, want the admitted titles", head)
	}
	if strings.Contains(head, "AGE") {
		t.Errorf("header = %q, want the dropped column omitted", head)
	}
}

// Hebrew fixture, written as an explicit constant so the test source is
// unambiguous regardless of editor rendering.
const hebrewName = "שרה כהן"

func TestRenderCellsLeftAlignsHebrew(t *testing.T) {
	widths := []int{12, 6}
	got := renderCells([]string{hebrewName, "Member"}, widths)

	// The cell keeps its column width...
	if lipgloss.Width(got) != 12+columnGap+6 {
		t.Fatalf("row width = %d, want %d", lipgloss.Width(got), 12+columnGap+6)
	}
	// ...and Hebrew starts at the left edge like every other cell. Only the
	// character order is adjusted, not the alignment.
	if []rune(got)[0] == ' ' {
		t.Error("the Hebrew cell is indented; it should be left-aligned like any other")
	}
	if !strings.Contains(got, "Member") {
		t.Error("the Latin cell was disturbed")
	}
}

func TestRenderCellsLeavesLatinLeftAligned(t *testing.T) {
	got := renderCells([]string{"Ada", "Member"}, []int{12, 6})
	if !strings.HasPrefix(got, "Ada ") {
		t.Errorf("row = %q, want the Latin cell left-aligned", got)
	}
}

func TestRenderCellsReordersHebrewForDisplay(t *testing.T) {
	got := renderCells([]string{hebrewName}, []int{20})
	// Within the cell's text, the first logical character must come last:
	// that is what makes it read right-to-left on a terminal with no bidi.
	logical := []rune(hebrewName)
	visual := []rune(strings.TrimRight(got, " "))
	if visual[len(visual)-1] != logical[0] {
		t.Errorf("last rune of the cell text = %q, want the first logical rune %q",
			visual[len(visual)-1], logical[0])
	}
	if visual[0] != logical[len(logical)-1] {
		t.Errorf("first rune of the cell text = %q, want the last logical rune %q",
			visual[0], logical[len(logical)-1])
	}
}

func TestRenderCellsTruncatesHebrewFromItsTail(t *testing.T) {
	// Truncation happens in logical order, so an over-long name loses its
	// end rather than its beginning.
	got := renderCells([]string{hebrewName}, []int{4})
	if lipgloss.Width(got) != 4 {
		t.Fatalf("width = %d, want 4", lipgloss.Width(got))
	}
	if !strings.Contains(got, "…") {
		t.Errorf("row = %q, want a truncation ellipsis", got)
	}
	logical := []rune(hebrewName)
	if !strings.ContainsRune(got, logical[0]) {
		t.Error("truncation dropped the beginning of the name")
	}
}
