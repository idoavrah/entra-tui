// Package bidi renders right-to-left text for terminals that do not
// implement the Unicode bidirectional algorithm themselves.
//
// Almost no terminal emulator applies UAX #9. A Hebrew display name arrives
// from Microsoft Graph in *logical* order (the order the characters were
// typed) and, without reordering, is drawn left to right -- which reads
// backwards. This package reorders logical text into the visual order a
// dumb terminal needs, so that Hebrew data reads correctly while staying
// inside the column it belongs to.
//
// The implementation is a deliberate simplification of UAX #9: it resolves
// neutral characters from their surrounding strong context, reverses
// right-to-left runs, and mirrors paired punctuation. It does not implement
// explicit embedding controls or the full weak-type rules, which directory
// data does not exercise.
package bidi

import (
	"strings"
	"unicode"
)

// direction classifies a rune's bidirectional behaviour.
type direction int

const (
	dirNeutral direction = iota
	dirLTR
	dirRTL
)

// classify assigns a rune its bidi direction.
//
// Digits are treated as neutral rather than left-to-right: a house number or
// a year embedded in Hebrew text should travel with the surrounding run, and
// resolveNeutrals keeps the digits themselves in reading order.
func classify(r rune) direction {
	switch {
	case isRTLRune(r):
		return dirRTL
	case unicode.IsLetter(r):
		return dirLTR
	default:
		return dirNeutral
	}
}

// isRTLRune reports whether r belongs to a right-to-left script.
func isRTLRune(r rune) bool {
	switch {
	case r >= 0x0590 && r <= 0x05FF: // Hebrew
		return true
	case r >= 0xFB1D && r <= 0xFB4F: // Hebrew presentation forms
		return true
	case r >= 0x0600 && r <= 0x06FF: // Arabic
		return true
	case r >= 0x0750 && r <= 0x077F: // Arabic supplement
		return true
	case r >= 0x08A0 && r <= 0x08FF: // Arabic extended-A
		return true
	case r >= 0xFB50 && r <= 0xFDFF: // Arabic presentation forms-A
		return true
	case r >= 0xFE70 && r <= 0xFEFF: // Arabic presentation forms-B
		return true
	case r >= 0x0700 && r <= 0x074F: // Syriac
		return true
	case r >= 0x07C0 && r <= 0x07FF: // NKo, Samaritan area
		return true
	}
	return false
}

// Contains reports whether s holds any right-to-left character.
func Contains(s string) bool {
	for _, r := range s {
		if isRTLRune(r) {
			return true
		}
	}
	return false
}

// IsRTL reports whether s reads right-to-left overall, which UAX #9 decides
// from the first strong character. A Hebrew name with a Latin suffix is
// right-to-left; an English name mentioning a Hebrew word is not.
func IsRTL(s string) bool {
	for _, r := range s {
		switch classify(r) {
		case dirRTL:
			return true
		case dirLTR:
			return false
		}
	}
	return false
}

// mirrored maps the paired punctuation that swaps sides in right-to-left
// text. An opening parenthesis in Hebrew is drawn as a closing one.
var mirrored = map[rune]rune{
	'(': ')', ')': '(',
	'[': ']', ']': '[',
	'{': '}', '}': '{',
	'<': '>', '>': '<',
	'«': '»', '»': '«', // guillemets
	'‹': '›', '›': '‹',
}

// Display converts logical-order text into the visual order a terminal
// without bidi support must be handed. Text with no right-to-left content is
// returned unchanged, so this is safe to call on every cell.
func Display(s string) string {
	if !Contains(s) {
		return s
	}

	runes := []rune(s)
	dirs := resolveNeutrals(runes, IsRTL(s))

	// Group consecutive runes sharing a resolved direction.
	type run struct {
		dir   direction
		runes []rune
	}
	var runs []run
	for i, r := range runes {
		if len(runs) == 0 || runs[len(runs)-1].dir != dirs[i] {
			runs = append(runs, run{dir: dirs[i]})
		}
		runs[len(runs)-1].runes = append(runs[len(runs)-1].runes, r)
	}

	// Right-to-left runs are stored logically but must be drawn reversed.
	for i := range runs {
		if runs[i].dir == dirRTL {
			runs[i].runes = reverseAndMirror(runs[i].runes)
		}
	}

	// In a right-to-left paragraph the runs themselves also run right to
	// left, so the whole sequence is laid out back to front.
	var b strings.Builder
	if IsRTL(s) {
		for i := len(runs) - 1; i >= 0; i-- {
			b.WriteString(string(runs[i].runes))
		}
	} else {
		for _, r := range runs {
			b.WriteString(string(r.runes))
		}
	}
	return b.String()
}

// resolveNeutrals assigns every rune a direction, giving neutral characters
// the direction of the strong text around them. A neutral between two runs
// of the same direction joins them; one that sits between opposing
// directions -- or at either edge -- takes the paragraph direction.
func resolveNeutrals(runes []rune, baseRTL bool) []direction {
	base := dirLTR
	if baseRTL {
		base = dirRTL
	}

	dirs := make([]direction, len(runes))
	for i, r := range runes {
		dirs[i] = classify(r)
	}

	for i := 0; i < len(dirs); i++ {
		if dirs[i] != dirNeutral {
			continue
		}
		// Span the whole neutral stretch at once so a run of spaces and
		// punctuation resolves consistently.
		j := i
		for j < len(dirs) && dirs[j] == dirNeutral {
			j++
		}

		before, after := base, base
		if i > 0 {
			before = dirs[i-1]
		}
		if j < len(dirs) {
			after = dirs[j]
		}

		resolved := base
		if before == after {
			resolved = before
		}
		for k := i; k < j; k++ {
			dirs[k] = resolved
		}
		i = j - 1
	}
	return dirs
}

// reverseAndMirror reverses a right-to-left run, swaps its paired
// punctuation, and restores the reading order of any embedded numbers.
func reverseAndMirror(runes []rune) []rune {
	out := make([]rune, len(runes))
	for i, r := range runes {
		if m, ok := mirrored[r]; ok {
			r = m
		}
		out[len(runes)-1-i] = r
	}
	return restoreNumbers(out)
}

// restoreNumbers un-reverses digit sequences inside an already-reversed
// right-to-left run.
//
// Numbers are the one thing that stays left-to-right inside Hebrew text: a
// street number written 15 must still read 15, not 51. UAX #9 achieves this
// by giving European digits their own weak type; reversing them back after
// the fact is the equivalent for this simplified implementation.
func restoreNumbers(runes []rune) []rune {
	for i := 0; i < len(runes); i++ {
		if !unicode.IsDigit(runes[i]) {
			continue
		}
		j := i
		for j < len(runes) && unicode.IsDigit(runes[j]) {
			j++
		}
		for lo, hi := i, j-1; lo < hi; lo, hi = lo+1, hi-1 {
			runes[lo], runes[hi] = runes[hi], runes[lo]
		}
		i = j - 1
	}
	return runes
}
