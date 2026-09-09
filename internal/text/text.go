// Package text holds the one thing every string from outside needs before it
// reaches a terminal.
package text

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Sanitize drops the characters a terminal would act on rather than draw.
//
// Directory data is not under the reader's control: a display name is
// whatever somebody typed into it, and entra-tui draws it straight into a
// terminal. An escape sequence in one would let whoever set that name move
// the cursor and repaint the screen -- including over a confirmation dialog,
// which exists precisely so that a change can be trusted to say what it does.
// Bidirectional overrides go the same way: they reorder everything after
// them, this renderer does not implement them, and a terminal that does would
// then disagree with what was measured and drawn.
//
// Nothing is hidden by this. The raw view renders the object as JSON, which
// escapes these characters rather than emitting them, so the true value is
// always one keystroke away.
func Sanitize(s string) string {
	if strings.IndexFunc(s, unsafeForTerminal) < 0 {
		return s
	}
	return strings.Map(func(r rune) rune {
		if unsafeForTerminal(r) {
			return -1
		}
		return r
	}, s)
}

// unsafeForTerminal reports whether a rune steers the terminal instead of
// putting a glyph on it.
func unsafeForTerminal(r rune) bool {
	switch {
	case r < 0x20 || r == 0x7f:
		return true // C0 controls and DEL, escape among them
	case r >= 0x80 && r <= 0x9f:
		return true // C1 controls
	}
	return invisible(r)
}

// invisible reports the characters that reorder or hide text around them
// while drawing nothing themselves. They are the half of the problem that
// survives a JSON encoder, which escapes the control characters and emits
// these as they are.
func invisible(r rune) bool {
	switch {
	case r == 0x200e || r == 0x200f:
		return true // left-to-right and right-to-left marks
	case r >= 0x202a && r <= 0x202e:
		return true // embeddings and overrides
	case r >= 0x2066 && r <= 0x2069:
		return true // isolates
	case r == 0x2028 || r == 0x2029:
		return true // line and paragraph separators
	}
	return false
}

// Escape rewrites the invisible characters as \uXXXX, for the places that
// have to stay faithful to what the directory holds.
//
// The raw view is the one screen that promises the object exactly as Graph
// returned it, so dropping anything there would be a lie. It is given an
// encoded document, whose own newlines and indentation are structure and must
// survive; the encoder has already escaped the control characters, and these
// are the ones it emits as themselves. A right-to-left override reverses a
// JSON document just as readily as it reverses a table.
func Escape(s string) string {
	if strings.IndexFunc(s, invisible) < 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if invisible(r) {
			fmt.Fprintf(&b, "\\u%04x", r)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Truncate shortens text to fit a number of terminal columns, marking the cut
// with an ellipsis.
//
// Columns, not runes. One CJK ideograph occupies two of them, so a name
// counted by rune overflows its column, pushes the frame's border off the end
// of the row and wraps the line -- and a display name is free to be nothing
// but wide glyphs on purpose. Width is measured the same way the padding
// measures it, so the two cannot disagree.
func Truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	if width == 1 {
		return "…"
	}

	// Built up to the budget rather than cut down to it: a wide glyph is
	// dropped whole instead of leaving half a cell behind.
	room := width - 1 // the ellipsis takes one
	var b strings.Builder
	for _, r := range s {
		w := lipgloss.Width(string(r))
		if w > room {
			break
		}
		b.WriteRune(r)
		room -= w
	}
	return b.String() + "…"
}
