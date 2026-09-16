package bidi

import (
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
)

// reordering is whether Display reorders at all.
//
// It is process-wide rather than threaded through the renderer because it
// describes the terminal, and there is one terminal: every cell, label and
// prompt is drawn onto the same one, and a table that reordered while its
// breadcrumb did not would read backwards in exactly one of the two places.
var reordering atomic.Bool

func init() { reordering.Store(true) }

// SetReordering says whether Display should reorder. Off means the terminal
// applies the bidirectional algorithm itself, and reordering here as well
// would reverse the text a second time.
func SetReordering(on bool) { reordering.Store(on) }

// Reordering reports whether Display reorders.
func Reordering() bool { return reordering.Load() }

// Mode is who reorders right-to-left text: entra-tui ("on"), the terminal
// ("off"), or whichever the environment suggests ("auto").
type Mode string

const (
	ModeAuto Mode = "auto"
	ModeOn   Mode = "on"
	ModeOff  Mode = "off"
)

// ParseMode reads a -bidi value. Empty is auto.
func ParseMode(s string) (Mode, error) {
	switch Mode(strings.ToLower(strings.TrimSpace(s))) {
	case "", ModeAuto:
		return ModeAuto, nil
	case ModeOn:
		return ModeOn, nil
	case ModeOff:
		return ModeOff, nil
	}
	return "", fmt.Errorf("unknown bidi mode %q (want auto, on or off)", s)
}

// Resolve decides whether entra-tui should reorder, and says why in words a
// reader can check against the terminal in front of them.
//
// No terminal can be asked whether it reorders: a cursor position report is
// in logical columns either way, so a probe cannot tell the two apart. Auto
// therefore goes by name, and knows only terminals that reorder by default.
// Anything it does not recognise is assumed not to, which is what almost
// every terminal emulator does.
func (mode Mode) Resolve(getenv func(string) string) (reorder bool, reason string) {
	switch mode {
	case ModeOn:
		return true, "-bidi on"
	case ModeOff:
		return false, "-bidi off"
	}
	if name, ok := selfReorderingTerminal(getenv); ok {
		return false, "auto: " + name + " reorders it itself"
	}
	return true, "auto: the terminal is not known to reorder it"
}

// iTermBidiVersion is the first iTerm2 release that reorders right-to-left
// text on its own.
var iTermBidiVersion = [2]int{3, 6}

// selfReorderingTerminal names the terminal when it is one known to apply the
// bidirectional algorithm by default.
func selfReorderingTerminal(getenv func(string) string) (string, bool) {
	switch name := TerminalName(getenv); name {
	case "iTerm2":
		version := getenv("TERM_PROGRAM_VERSION")
		if getenv("LC_TERMINAL") == "iTerm2" {
			version = getenv("LC_TERMINAL_VERSION")
		}
		major, minor, ok := parseVersion(version)
		if ok && (major > iTermBidiVersion[0] ||
			major == iTermBidiVersion[0] && minor >= iTermBidiVersion[1]) {
			return fmt.Sprintf("iTerm2 %d.%d", major, minor), true
		}
		return "", false
	case "Terminal.app", "Konsole", "mlterm":
		return name, true
	}
	return "", false
}

// TerminalName is what the terminal drawing the text calls itself, or empty
// when it says nothing. It is a key for a choice remembered per terminal, so
// it leaves the version out: an upgrade must not forget the choice.
func TerminalName(getenv func(string) string) string {
	switch {
	// iTerm2 sets LC_TERMINAL as well as TERM_PROGRAM, and LC_* variables
	// survive ssh and tmux where TERM_PROGRAM does not.
	case getenv("LC_TERMINAL") == "iTerm2", getenv("TERM_PROGRAM") == "iTerm.app":
		return "iTerm2"
	case getenv("TERM_PROGRAM") == "Apple_Terminal":
		return "Terminal.app"
	case getenv("KONSOLE_VERSION") != "":
		return "Konsole"
	case getenv("MLTERM") != "":
		return "mlterm"
	case getenv("VTE_VERSION") != "":
		return "VTE"
	}
	if name := plainName(getenv("TERM_PROGRAM")); name != "" {
		return name
	}
	return plainName(getenv("TERM"))
}

// plainName keeps a terminal name that is safe to show and to store, and
// drops anything else. The environment is the user's own, but it is still
// text from outside reaching the screen.
func plainName(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 40 {
		return ""
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '-', r == '_', r == '+':
		default:
			return ""
		}
	}
	return s
}

// parseVersion reads the major and minor numbers from a version string such
// as "3.7.1". Only the numbers are kept, so nothing from the environment
// reaches the screen verbatim.
func parseVersion(v string) (major, minor int, ok bool) {
	parts := strings.SplitN(strings.TrimSpace(v), ".", 3)
	if len(parts) < 2 {
		return 0, 0, false
	}
	major, errMajor := strconv.Atoi(parts[0])
	minor, errMinor := strconv.Atoi(parts[1])
	if errMajor != nil || errMinor != nil {
		return 0, 0, false
	}
	return major, minor, true
}

// Terminals that implement the "BiDi in Terminal Emulators" recommendation --
// VTE, and so GNOME Terminal -- take their instructions from ECMA-48's
// Bi-Directional Support Mode (BDSM, mode 8). On those, telling the terminal
// who reorders makes the choice exact rather than a guess. Every other
// terminal ignores a mode it does not know.
const (
	// ExplicitMode says the application has already laid the text out.
	ExplicitMode = "\x1b[8l"
	// ImplicitMode says the terminal should reorder. It is the default, so it
	// is also what is left behind on exit.
	ImplicitMode = "\x1b[8h"
)

// TerminalMode is the BDSM sequence matching the current choice.
func TerminalMode() string {
	if Reordering() {
		return ExplicitMode
	}
	return ImplicitMode
}
