package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestBigNumberIsThreeRows(t *testing.T) {
	got := bigNumber("1,707,600")
	if len(got) != bigDigitHeight {
		t.Fatalf("got %d rows, want %d", len(got), bigDigitHeight)
	}
}

func TestBigNumberRowsAreEqualWidth(t *testing.T) {
	// Ragged rows would shear the tile they sit in.
	for _, s := range []string{"0", "1,707,600", "55,387", "—"} {
		rows := bigNumber(s)
		for i, r := range rows {
			if lipgloss.Width(r) != lipgloss.Width(rows[0]) {
				t.Errorf("%q row %d is %d cells, row 0 is %d",
					s, i, lipgloss.Width(r), lipgloss.Width(rows[0]))
			}
		}
	}
}

func TestBigNumberWidthMatchesWhatIsRendered(t *testing.T) {
	for _, s := range []string{"5", "13,823", "1,707,600"} {
		want := bigNumberWidth(s)
		if got := lipgloss.Width(bigNumber(s)[0]); got != want {
			t.Errorf("%q rendered %d cells, bigNumberWidth said %d", s, got, want)
		}
	}
	if bigNumberWidth("") != 0 {
		t.Error("an empty count should have zero width")
	}
}

func TestEveryDigitHasAGlyph(t *testing.T) {
	// A missing glyph would fall back to "?" and silently misreport a count.
	for _, r := range "0123456789," {
		if _, ok := bigDigits[r]; !ok {
			t.Errorf("no glyph for %q", r)
		}
	}
	// Every digit must render as something distinct, so a count cannot be
	// misread as another number.
	seen := map[string]rune{}
	for _, r := range "0123456789" {
		glyph := strings.Join(bigNumber(string(r)), "\n")
		if other, clash := seen[glyph]; clash {
			t.Errorf("%q and %q render identically", r, other)
		}
		seen[glyph] = r
	}
}

func TestUnknownCharacterDoesNotCollapseTheHeight(t *testing.T) {
	got := bigNumber("1x2")
	if len(got) != bigDigitHeight {
		t.Fatalf("got %d rows, want %d", len(got), bigDigitHeight)
	}
	if lipgloss.Width(got[0]) != bigNumberWidth("1x2") {
		t.Error("an unknown character changed the rendered width")
	}
}

func TestGlyphsAreAllThreeCellsWide(t *testing.T) {
	for r, glyph := range bigDigits {
		for i, row := range glyph {
			if lipgloss.Width(row) != 3 {
				t.Errorf("glyph %q row %d is %d cells, want 3", r, i, lipgloss.Width(row))
			}
		}
	}
}
