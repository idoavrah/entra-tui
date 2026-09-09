package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestBoxTopCentresItsCaption(t *testing.T) {
	got := boxTop(40, "Users")

	if lipgloss.Width(got) != 40 {
		t.Fatalf("width = %d, want 40", lipgloss.Width(got))
	}
	if !strings.HasPrefix(got, boxTopLeft) || !strings.HasSuffix(got, boxTopRight) {
		t.Errorf("border = %q, want the corner characters", got)
	}

	// Equal rule on either side, within the one cell an odd remainder leaves.
	// Measured in runes: the border glyphs are multi-byte.
	runes := []rune(got)
	caption := []rune("Users")
	start := -1
	for i := 0; i+len(caption) <= len(runes); i++ {
		if string(runes[i:i+len(caption)]) == "Users" {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("caption missing from %q", got)
	}
	left := start - 1 // rule before the leading space
	right := len(runes) - (start + len(caption) + 1) - 1
	if left-right > 1 || right-left > 1 {
		t.Errorf("caption sits %d cells from the left and %d from the right, want it centred", left, right)
	}
}

func TestBoxTopWithoutCaptionIsAPlainRule(t *testing.T) {
	got := boxTop(20, "")
	want := boxTopLeft + strings.Repeat(boxHorizontal, 18) + boxTopRight
	if got != want {
		t.Errorf("border = %q, want %q", got, want)
	}
}

func TestBoxTopMeasuresStyledCaptionsByPrintableWidth(t *testing.T) {
	// A styled caption carries ANSI; centring must not count those bytes.
	plain := boxTop(60, "Users")
	styled := boxTop(60, styleContextVal.Render("Users"))

	if lipgloss.Width(plain) != lipgloss.Width(styled) {
		t.Errorf("styled border width %d != plain %d", lipgloss.Width(styled), lipgloss.Width(plain))
	}
	if lipgloss.Width(styled) != 60 {
		t.Errorf("width = %d, want 60", lipgloss.Width(styled))
	}
}

func TestBoxTopDropsACaptionTooWideToFit(t *testing.T) {
	// A caption wider than the frame must not break the border.
	got := boxTop(12, "an extremely long screen caption")
	if lipgloss.Width(got) != 12 {
		t.Errorf("width = %d, want the frame intact at 12", lipgloss.Width(got))
	}
	if strings.Contains(got, "extremely") {
		t.Error("the oversized caption was rendered anyway")
	}
}

func TestBoxRowPadsAndFrames(t *testing.T) {
	got := boxRow(20, "abc")
	if lipgloss.Width(got) != 20 {
		t.Errorf("width = %d, want 20", lipgloss.Width(got))
	}
	if !strings.HasPrefix(got, boxVertical) || !strings.HasSuffix(got, boxVertical) {
		t.Errorf("row = %q, want vertical borders on both sides", got)
	}
}

func TestBoxRowTruncatesPlainOverflow(t *testing.T) {
	got := boxRow(12, strings.Repeat("x", 40))
	if lipgloss.Width(got) != 12 {
		t.Errorf("width = %d, want 12", lipgloss.Width(got))
	}
}

func TestTruncateStyledKeepsEscapeSequencesIntact(t *testing.T) {
	// Cutting a styled string by rune index risks severing an escape
	// sequence and bleeding colour across the rest of the screen, so styled
	// content is passed through rather than corrupted.
	//
	// The literal escapes here matter: lipgloss emits none under a test
	// binary's colour profile, so a styled fixture would not exercise this.
	styled := "\x1b[7m" + strings.Repeat("x", 40) + "\x1b[0m"

	got := truncateStyled(styled, 12)
	if !strings.HasPrefix(got, "\x1b[7m") {
		t.Error("the opening escape sequence was damaged")
	}
	if !strings.HasSuffix(got, "\x1b[0m") {
		t.Error("the style reset was lost, which would bleed colour onward")
	}
}

func TestTruncateStyledCutsPlainText(t *testing.T) {
	got := truncateStyled(strings.Repeat("x", 40), 12)
	if lipgloss.Width(got) != 12 {
		t.Errorf("width = %d, want plain text truncated to 12", lipgloss.Width(got))
	}
}

func TestBoxFrameHasExactlyTwoMoreLinesThanItsBody(t *testing.T) {
	body := []string{"a", "b", "c"}
	got := boxFrame(30, "Title", "footer", body)

	if len(got) != len(body)+2 {
		t.Fatalf("got %d lines, want %d", len(got), len(body)+2)
	}
	for i, line := range got {
		if lipgloss.Width(line) != 30 {
			t.Errorf("line %d width = %d, want 30", i, lipgloss.Width(line))
		}
	}
	if !strings.Contains(got[0], "Title") {
		t.Error("the top border lost its caption")
	}
	if !strings.Contains(got[len(got)-1], "footer") {
		t.Error("the bottom border lost its caption")
	}
}

func TestBoxSurvivesDegenerateWidths(t *testing.T) {
	for _, w := range []int{0, 1, 2, 3} {
		if got := boxTop(w, "Users"); got == "" {
			t.Errorf("boxTop(%d) returned empty", w)
		}
		if got := boxRow(w, "content"); got == "" {
			t.Errorf("boxRow(%d) returned empty", w)
		}
	}
}
