package bidi

import "testing"

// Hebrew strings are written here as explicit runes so the test source
// itself is unambiguous regardless of how an editor renders it.
const (
	shalom = "שלום" // שלום, logical order
	rehov  = "רחוב" // רחוב ("street")
)

// reversed returns the visual form of a pure-Hebrew word.
func reversed(s string) string {
	r := []rune(s)
	for lo, hi := 0, len(r)-1; lo < hi; lo, hi = lo+1, hi-1 {
		r[lo], r[hi] = r[hi], r[lo]
	}
	return string(r)
}

func TestDisplayLeavesLatinUntouched(t *testing.T) {
	for _, s := range []string{"", "Ada Lovelace", "ada@contoso.com", "123", "a-b_c.d"} {
		if got := Display(s); got != s {
			t.Errorf("Display(%q) = %q, want it unchanged", s, got)
		}
	}
}

func TestDisplayReversesPureHebrew(t *testing.T) {
	if got, want := Display(shalom), reversed(shalom); got != want {
		t.Errorf("Display(shalom) = %q, want %q", got, want)
	}
}

func TestDisplayHebrewThenLatin(t *testing.T) {
	// Base direction is right-to-left, so the Latin word is drawn leftmost
	// and the Hebrew word occupies the right of the field.
	got := Display(shalom + " Ada")
	want := "Ada " + reversed(shalom)
	if got != want {
		t.Errorf("Display = %q, want %q", got, want)
	}
}

func TestDisplayLatinThenHebrew(t *testing.T) {
	// Base direction is left-to-right: the Latin stays put and only the
	// Hebrew run is reordered.
	got := Display("Ada " + shalom)
	want := "Ada " + reversed(shalom)
	if got != want {
		t.Errorf("Display = %q, want %q", got, want)
	}
}

func TestDisplayLatinEmbeddedInHebrew(t *testing.T) {
	got := Display(shalom + " Ada " + shalom)
	want := reversed(shalom) + " Ada " + reversed(shalom)
	if got != want {
		t.Errorf("Display = %q, want %q", got, want)
	}
}

func TestDisplayKeepsNumbersReadable(t *testing.T) {
	// A multi-digit number inside Hebrew must not come out reversed.
	got := Display(rehov + " 15")
	want := "15 " + reversed(rehov)
	if got != want {
		t.Errorf("Display = %q, want %q (numbers must stay in reading order)", got, want)
	}
}

func TestDisplayMirrorsBrackets(t *testing.T) {
	// An opening parenthesis in right-to-left text is drawn as a closing one.
	got := Display("(" + shalom + ")")
	want := "(" + reversed(shalom) + ")"
	if got != want {
		t.Errorf("Display = %q, want %q", got, want)
	}
}

func TestIsRTLUsesFirstStrongCharacter(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{shalom, true},
		{shalom + " Ada", true},
		{"Ada " + shalom, false},
		{"Ada", false},
		{"", false},
		{"123 " + shalom, true}, // digits are not strong, so Hebrew decides
		{"  " + shalom, true},
	} {
		if got := IsRTL(tc.in); got != tc.want {
			t.Errorf("IsRTL(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestContains(t *testing.T) {
	if !Contains("Ada " + shalom) {
		t.Error("Contains missed embedded Hebrew")
	}
	if Contains("Ada Lovelace") {
		t.Error("Contains reported Hebrew in pure Latin text")
	}
	// Arabic uses the same machinery.
	if !Contains("مرحبا") {
		t.Error("Contains missed Arabic")
	}
}

func TestDisplayIsLengthPreserving(t *testing.T) {
	// Column layout depends on reordering never changing the cell's width.
	for _, s := range []string{
		shalom, shalom + " Ada", "Ada " + shalom, rehov + " 15",
		"(" + shalom + ")", shalom + " Ada " + shalom,
	} {
		if got, want := len([]rune(Display(s))), len([]rune(s)); got != want {
			t.Errorf("Display(%q) changed rune count: %d, want %d", s, got, want)
		}
	}
}

func TestDisplayIsIdempotentForLatin(t *testing.T) {
	s := "Ada Lovelace"
	if Display(Display(s)) != s {
		t.Error("Display is not stable over Latin text")
	}
}
