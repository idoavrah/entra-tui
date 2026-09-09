package text

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestSanitizeDropsWhatATerminalWouldActOn(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{"plain text is untouched", "Ada Lovelace", "Ada Lovelace"},
		{"Hebrew is untouched", "שרה", "שרה"},
		{"CSI clear screen", "Ada\x1b[2J\x1b[HGOTCHA", "Ada[2J[HGOTCHA"},
		{"OSC 52 clipboard write", "Bob\x1b]52;c;cGF5bG9hZA==\x07", "Bob]52;c;cGF5bG9hZA=="},
		{"carriage return overwrite", "safe\rEVIL", "safeEVIL"},
		{"bell", "ding\a", "ding"},
		{"DEL", "a\x7fb", "ab"},
		{"C1 control", "amb", "amb"},
		{"right-to-left override", "cod‮gnp.exe", "codgnp.exe"},
		{"isolates", "⁦spoof⁩", "spoof"},
		{"line separator", "one two", "onetwo"},
	} {
		if got := Sanitize(tc.in); got != tc.want {
			t.Errorf("%s: Sanitize(%q) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}
}

func TestSanitizeReturnsCleanTextUnchanged(t *testing.T) {
	// The common case scans and returns the original rather than rebuilding
	// it, which is why the scan comes first.
	const in = "Ada Lovelace"
	if got := Sanitize(in); got != in {
		t.Errorf("Sanitize(%q) = %q", in, got)
	}
}

func TestEscapeKeepsTheTruthWithoutTheEffect(t *testing.T) {
	// The raw view promises the object exactly as Graph returned it, so the
	// characters Sanitize drops are spelled out there rather than removed.
	in := "cod‮gnp.exe"
	got := Escape(in)
	if got != `cod\u202egnp.exe` {
		t.Errorf("Escape(%q) = %q", in, got)
	}
	if got == in {
		t.Error("the override survived into text a terminal would act on")
	}
	if clean := "Ada Lovelace"; Escape(clean) != clean {
		t.Error("clean text was rewritten")
	}
}

func TestEscapeLeavesStructureAlone(t *testing.T) {
	// Escape is handed encoded documents whose own newlines and indentation
	// are structure. Rewriting those broke the JSON it was meant to protect.
	const doc = "{\n  \"displayName\": \"Ada\"\n}"
	if got := Escape(doc); got != doc {
		t.Errorf("Escape rewrote a document's own formatting: %q", got)
	}
}

func TestTruncateCountsColumnsNotRunes(t *testing.T) {
	// A CJK ideograph is two columns wide. Counting runes let eight of them
	// claim a column sized for eight cells and occupy sixteen, which pushed
	// the frame's border off the end of the row.
	for _, tc := range []struct {
		name  string
		in    string
		width int
		want  int // display columns the result may occupy
	}{
		{"short text is untouched", "Ada", 10, 3},
		{"exact fit", "Ada", 3, 3},
		{"plain text is cut", "Ada Lovelace", 6, 6},
		{"wide glyphs are cut by width", "東京東京東京東京", 8, 8},
		{"an odd budget drops the wide glyph whole", "東京東京", 5, 5},
		{"mixed", "Ada東京Lovelace", 9, 9},
		{"no room", "Ada", 0, 0},
		{"one column", "Ada", 1, 1},
	} {
		got := Truncate(tc.in, tc.width)
		if w := lipgloss.Width(got); w > tc.want {
			t.Errorf("%s: Truncate(%q, %d) = %q, %d columns, want at most %d",
				tc.name, tc.in, tc.width, got, w, tc.want)
		}
	}
}

func TestTruncateNeverExceedsItsBudget(t *testing.T) {
	// The property that matters: whatever comes back fits.
	for _, s := range []string{"Ada", "東京東京東京", "שרה כהן", "a東b京c", ""} {
		for width := range 12 {
			if w := lipgloss.Width(Truncate(s, width)); w > width {
				t.Errorf("Truncate(%q, %d) is %d columns wide", s, width, w)
			}
		}
	}
}
