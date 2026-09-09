package ui

import "strings"

// bigDigitHeight is the number of rows a large numeral occupies.
const bigDigitHeight = 3

// bigDigitWidth is the cell width of one numeral, including its trailing gap.
const bigDigitWidth = 4

// bigDigits renders counts large enough to read at a glance, which is the
// point of a dashboard tile. Each glyph is three rows of three cells.
// Solid blocks rather than box-drawing lines: at three rows a line-drawn "3"
// and "8" are hard to tell apart, and a count nobody can read at a glance
// defeats the purpose of showing it large.
var bigDigits = map[rune][bigDigitHeight]string{
	'0': {"█▀█", "█ █", "█▄█"},
	'1': {" ▀█", "  █", "  █"},
	'2': {"▀▀█", "█▀▀", "█▄▄"},
	'3': {"▀▀█", " ▀█", "▄▄█"},
	'4': {"█ █", "▀▀█", "  █"},
	'5': {"█▀▀", "▀▀█", "▄▄█"},
	'6': {"█▀▀", "█▀█", "█▄█"},
	'7': {"▀▀█", "  █", "  █"},
	'8': {"█▀█", "█▀█", "█▄█"},
	'9': {"█▀█", "▀▀█", "▄▄█"},
	',': {"   ", "   ", "▗  "},
	'?': {"▀▀█", " ▀▀", " ▄ "},
	'—': {"   ", "▄▄▄", "   "},
}

// bigNumber renders s as large numerals, returning bigDigitHeight lines.
// Unknown characters fall back to a question mark so a surprise never
// collapses the tile's height.
func bigNumber(s string) []string {
	rows := make([]strings.Builder, bigDigitHeight)
	for _, r := range s {
		glyph, ok := bigDigits[r]
		if !ok {
			glyph = bigDigits['?']
		}
		for i := range bigDigitHeight {
			rows[i].WriteString(glyph[i])
			rows[i].WriteByte(' ')
		}
	}

	// Rows are padded to a common width rather than trimmed: a glyph whose
	// top row is all spaces -- the placeholder dash, for one -- would
	// otherwise come back shorter than its siblings and shear the tile.
	width := bigNumberWidth(s)
	out := make([]string, bigDigitHeight)
	for i := range bigDigitHeight {
		line := rows[i].String()
		if len(line) > 0 {
			line = line[:len(line)-1] // drop the trailing inter-glyph gap
		}
		out[i] = line + strings.Repeat(" ", max(0, width-len([]rune(line))))
	}
	return out
}

// bigNumberWidth is the cell width bigNumber will produce for s.
func bigNumberWidth(s string) int {
	if s == "" {
		return 0
	}
	return len([]rune(s))*bigDigitWidth - 1
}
