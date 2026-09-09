package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/idoavrah/entra-tui/internal/graph"
)

// Box drawing characters for the content frame.
const (
	boxTopLeft     = "┌"
	boxTopRight    = "┐"
	boxBottomLeft  = "└"
	boxBottomRight = "┘"
	boxHorizontal  = "─"
	boxVertical    = "│"
)

// boxInnerWidth is the usable width inside a frame of the given outer width.
func boxInnerWidth(width int) int { return max(1, width-2) }

// boxTop renders the frame's top edge with a caption centred on it, the way
// a titled fieldset reads: the screen's identity sits on the frame rather
// than consuming a row of content.
//
// caption may carry styling; its printable width is measured, not its byte
// length, so ANSI sequences do not skew the centring.
func boxTop(width int, caption string) string {
	return borderWithCaption(width, caption, boxTopLeft, boxTopRight)
}

// boxBottom renders the frame's bottom edge, optionally captioned.
func boxBottom(width int, caption string) string {
	return borderWithCaption(width, caption, boxBottomLeft, boxBottomRight)
}

func borderWithCaption(width int, caption, left, right string) string {
	inner := boxInnerWidth(width)
	if strings.TrimSpace(stripCaption(caption)) == "" {
		return styleBorder.Render(left + strings.Repeat(boxHorizontal, inner) + right)
	}

	// A space either side lifts the caption off the rule.
	captionWidth := lipgloss.Width(caption) + 2
	if captionWidth > inner {
		// Too narrow for the caption: keep the frame intact and drop it.
		return styleBorder.Render(left + strings.Repeat(boxHorizontal, inner) + right)
	}

	leftRule := (inner - captionWidth) / 2
	rightRule := inner - captionWidth - leftRule
	return styleBorder.Render(left+strings.Repeat(boxHorizontal, leftRule)) +
		" " + caption + " " +
		styleBorder.Render(strings.Repeat(boxHorizontal, rightRule)+right)
}

// stripCaption is a cheap emptiness check that ignores styling. lipgloss.Width
// already discards ANSI, so a zero width means there is nothing to show.
func stripCaption(s string) string {
	if lipgloss.Width(s) == 0 {
		return ""
	}
	return s
}

// boxRow frames one line of content, padding or truncating it to fit.
func boxRow(width int, content string) string {
	inner := boxInnerWidth(width)
	w := lipgloss.Width(content)
	switch {
	case w > inner:
		content = truncateStyled(content, inner)
	case w < inner:
		content += strings.Repeat(" ", inner-w)
	}
	return styleBorder.Render(boxVertical) + content + styleBorder.Render(boxVertical)
}

// truncateStyled shortens rendered content to a printable width.
//
// Styled text cannot be cut by rune index without risking a severed escape
// sequence, so anything carrying ANSI is passed through and allowed to
// overflow by the caller's arrangement instead of being corrupted. Plain text
// -- which is what the table produces, since styling is applied to whole
// rows -- is truncated normally.
func truncateStyled(s string, width int) string {
	if !strings.Contains(s, "\x1b") {
		return graph.Truncate(s, width)
	}
	return s
}

// boxFrame wraps body lines in a frame with the given captions, returning
// exactly len(body)+2 lines.
func boxFrame(width int, topCaption, bottomCaption string, body []string) []string {
	out := make([]string, 0, len(body)+2)
	out = append(out, boxTop(width, topCaption))
	for _, line := range body {
		out = append(out, boxRow(width, line))
	}
	out = append(out, boxBottom(width, bottomCaption))
	return out
}
