package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/idoavrah/entra-tui/internal/bidi"
	"github.com/idoavrah/entra-tui/internal/graph"
)

// columnGap is the blank space rendered between adjacent columns.
const columnGap = 1

// layout negotiates column widths for the available terminal width.
//
// Columns are admitted left to right while their MinWidth still fits, so a
// narrow terminal loses the rightmost (least important) columns rather than
// squeezing every column into illegibility. Whatever width remains is shared
// among the admitted columns in proportion to Weight; zero-weight columns
// stay pinned at MinWidth, which suits fixed-shape fields like an id or a
// yes/no flag.
//
// It returns one width per admitted column; the caller renders only that many.
func layout(cols []graph.Column, avail int) []int {
	if len(cols) == 0 || avail <= 0 {
		return nil
	}

	widths := make([]int, 0, len(cols))
	used := 0
	for n, c := range cols {
		gap := 0
		if n > 0 {
			gap = columnGap
		}
		next := used + gap + c.MinWidth
		if next > avail && len(widths) > 0 {
			break
		}
		used = next
		widths = append(widths, c.MinWidth)
	}

	// The first column is always shown, even in a terminal too narrow for its
	// minimum, so the view is never blank.
	if len(widths) == 0 {
		return []int{avail}
	}
	if used > avail {
		widths[0] = max(1, avail)
		return widths
	}

	leftover := avail - used
	if leftover <= 0 {
		return widths
	}

	totalWeight := 0
	for i := range widths {
		totalWeight += cols[i].Weight
	}
	if totalWeight == 0 {
		// Nothing asked to grow; give the slack to the first column so the
		// table still fills the pane instead of leaving a ragged right edge.
		widths[0] += leftover
		return widths
	}

	handed := 0
	for i := range widths {
		if cols[i].Weight == 0 {
			continue
		}
		share := leftover * cols[i].Weight / totalWeight
		widths[i] += share
		handed += share
	}
	// Integer division loses a few cells; award the remainder to the widest
	// growing column so totals land exactly on avail.
	if rem := leftover - handed; rem > 0 {
		best, bestWeight := -1, 0
		for i := range widths {
			if cols[i].Weight > bestWeight {
				best, bestWeight = i, cols[i].Weight
			}
		}
		if best >= 0 {
			widths[best] += rem
		}
	}
	return widths
}

// renderCells joins pre-rendered cell text into one fixed-width line,
// truncating any cell that overflows its column.
//
// Right-to-left values are reordered so they read correctly in a terminal
// without bidi support, but they stay left-aligned like every other cell.
// Reordering is what makes the text readable; alignment is a separate
// question, and a ragged left edge costs more than the typographic nicety of
// right-aligning is worth in a dense table.
func renderCells(cells []string, widths []int) string {
	var b strings.Builder
	for i, w := range widths {
		if i > 0 {
			b.WriteString(strings.Repeat(" ", columnGap))
		}
		cell := ""
		if i < len(cells) {
			cell = cells[i]
		}

		// Truncate while the text is still in logical order, so an
		// over-long Hebrew name loses its tail rather than its beginning.
		cell = bidi.Display(graph.Truncate(cell, w))

		// Pad by display width rather than byte or rune count so that wide
		// (CJK) glyphs in a display name do not shear the columns to the right.
		b.WriteString(cell)
		if pad := w - lipgloss.Width(cell); pad > 0 {
			b.WriteString(strings.Repeat(" ", pad))
		}
	}
	return b.String()
}

// headerCells renders the column titles for the admitted columns.
func headerCells(cols []graph.Column, widths []int) string {
	titles := make([]string, len(widths))
	for i := range widths {
		titles[i] = cols[i].Title
	}
	return renderCells(titles, widths)
}
