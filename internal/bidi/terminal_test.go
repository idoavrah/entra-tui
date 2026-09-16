package bidi

import "testing"

func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func TestParseMode(t *testing.T) {
	for in, want := range map[string]Mode{
		"": ModeAuto, "auto": ModeAuto, " ON ": ModeOn, "off": ModeOff,
	} {
		if got, err := ParseMode(in); err != nil || got != want {
			t.Errorf("ParseMode(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := ParseMode("yes"); err == nil {
		t.Error("ParseMode accepted a value that says nothing about who reorders")
	}
}

func TestExplicitModesIgnoreTheEnvironment(t *testing.T) {
	iterm := env(map[string]string{"TERM_PROGRAM": "iTerm.app", "TERM_PROGRAM_VERSION": "3.7.1"})
	if reorder, _ := ModeOn.Resolve(iterm); !reorder {
		t.Error("-bidi on did not reorder in a terminal auto would have left it to")
	}
	if reorder, _ := ModeOff.Resolve(env(nil)); reorder {
		t.Error("-bidi off still reorders")
	}
}

func TestAutoRecognisesTerminalsThatReorder(t *testing.T) {
	for _, tc := range []struct {
		name    string
		vars    map[string]string
		reorder bool
	}{
		{"iTerm2 with bidi", map[string]string{"TERM_PROGRAM": "iTerm.app", "TERM_PROGRAM_VERSION": "3.7.1"}, false},
		{"iTerm2 before bidi", map[string]string{"TERM_PROGRAM": "iTerm.app", "TERM_PROGRAM_VERSION": "3.5.14"}, true},
		// tmux replaces TERM_PROGRAM, but iTerm2 is still what draws the text.
		{"iTerm2 under tmux", map[string]string{"TERM_PROGRAM": "tmux", "LC_TERMINAL": "iTerm2", "LC_TERMINAL_VERSION": "3.6.0"}, false},
		{"iTerm2 with no version", map[string]string{"TERM_PROGRAM": "iTerm.app"}, true},
		{"Terminal.app", map[string]string{"TERM_PROGRAM": "Apple_Terminal"}, false},
		{"Konsole", map[string]string{"KONSOLE_VERSION": "240202"}, false},
		{"mlterm", map[string]string{"MLTERM": "3.9.3"}, false},
		{"VS Code", map[string]string{"TERM_PROGRAM": "vscode"}, true},
		{"nothing known", nil, true},
	} {
		if got, reason := ModeAuto.Resolve(env(tc.vars)); got != tc.reorder {
			t.Errorf("%s: reorder = %v (%s), want %v", tc.name, got, reason, tc.reorder)
		}
	}
}

func TestTerminalNameIgnoresTheVersion(t *testing.T) {
	for _, tc := range []struct {
		vars map[string]string
		want string
	}{
		{map[string]string{"TERM_PROGRAM": "iTerm.app", "TERM_PROGRAM_VERSION": "3.7.1"}, "iTerm2"},
		{map[string]string{"TERM_PROGRAM": "tmux", "LC_TERMINAL": "iTerm2"}, "iTerm2"},
		{map[string]string{"TERM_PROGRAM": "vscode", "TERM": "xterm-256color"}, "vscode"},
		{map[string]string{"VTE_VERSION": "7600", "TERM": "xterm-256color"}, "VTE"},
		{map[string]string{"TERM": "alacritty"}, "alacritty"},
		{map[string]string{"TERM_PROGRAM": "evil\x1b[2J", "TERM": "xterm"}, "xterm"},
		{nil, ""},
	} {
		if got := TerminalName(env(tc.vars)); got != tc.want {
			t.Errorf("TerminalName(%v) = %q, want %q", tc.vars, got, tc.want)
		}
	}
}

func TestReasonNamesTheTerminalWithoutEchoingTheEnvironment(t *testing.T) {
	_, reason := ModeAuto.Resolve(env(map[string]string{
		"TERM_PROGRAM": "iTerm.app", "TERM_PROGRAM_VERSION": "3.7.1\x1b[2J",
	}))
	if want := "auto: iTerm2 3.7 reorders it itself"; reason != want {
		t.Errorf("reason = %q, want %q", reason, want)
	}
}

func TestDisplayLeavesTextAloneWhenNotReordering(t *testing.T) {
	SetReordering(false)
	t.Cleanup(func() { SetReordering(true) })

	if got := Display(shalom + " Ada"); got != shalom+" Ada" {
		t.Errorf("Display = %q, want the logical order handed to the terminal", got)
	}
	if TerminalMode() != ImplicitMode {
		t.Error("the terminal is not told to reorder when entra-tui does not")
	}
}

func TestTerminalIsToldWhenTheAppReorders(t *testing.T) {
	if !Reordering() || TerminalMode() != ExplicitMode {
		t.Error("reordering is on by default, and the terminal should be told to stand down")
	}
}
