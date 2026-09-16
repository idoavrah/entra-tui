package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/idoavrah/entra-tui/internal/bidi"
)

var (
	iterm  = env(map[string]string{"TERM_PROGRAM": "iTerm.app", "TERM_PROGRAM_VERSION": "3.7.1"})
	vscode = env(map[string]string{"TERM_PROGRAM": "vscode"})
)

func TestBidiChoiceIsRememberedPerTerminal(t *testing.T) {
	path := SettingsPath(t.TempDir())

	// iTerm2 3.7 is guessed to reorder by itself; somebody who switched that
	// off in iTerm2 wants entra-tui to do it.
	if err := LoadSettings(path).RememberBidi(true, iterm); err != nil {
		t.Fatalf("RememberBidi: %v", err)
	}

	next := LoadSettings(path)
	if reorder, reason := next.ResolveBidi(bidi.ModeAuto, iterm); !reorder || reason != "remembered for iTerm2" {
		t.Errorf("iTerm2: reorder = %v (%s), want the remembered choice", reorder, reason)
	}
	// The same person's VS Code terminal needs the opposite, and gets its
	// own guess.
	if reorder, reason := next.ResolveBidi(bidi.ModeAuto, vscode); !reorder || reason == "remembered for iTerm2" {
		t.Errorf("VS Code: reorder = %v (%s), want the guess, not iTerm2's choice", reorder, reason)
	}
}

func TestExplicitBidiModeBeatsARememberedChoice(t *testing.T) {
	path := SettingsPath(t.TempDir())
	s := LoadSettings(path)
	if err := s.RememberBidi(true, iterm); err != nil {
		t.Fatal(err)
	}
	if reorder, _ := s.ResolveBidi(bidi.ModeOff, iterm); reorder {
		t.Error("-bidi off lost to a remembered choice")
	}
}

func TestChoosingTheGuessForgetsTheChoice(t *testing.T) {
	path := SettingsPath(t.TempDir())
	s := LoadSettings(path)
	if err := s.RememberBidi(true, iterm); err != nil {
		t.Fatal(err)
	}
	// Pressing b back to what auto would pick is how a choice is undone, so
	// a later release that corrects the guess reaches this user too.
	if err := s.RememberBidi(false, iterm); err != nil {
		t.Fatal(err)
	}
	if _, reason := LoadSettings(path).ResolveBidi(bidi.ModeAuto, iterm); reason == "remembered for iTerm2" {
		t.Error("a choice matching the guess was stored instead of forgotten")
	}
}

func TestSettingsFileIsPrivate(t *testing.T) {
	path := SettingsPath(t.TempDir())
	if err := LoadSettings(path).RememberBidi(true, iterm); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("settings file mode = %o, want 600", perm)
	}
}

func TestUnnamedTerminalRemembersNothing(t *testing.T) {
	err := LoadSettings(SettingsPath(t.TempDir())).RememberBidi(true, env(nil))
	if !errors.Is(err, ErrNoTerminalName) {
		t.Errorf("RememberBidi = %v, want ErrNoTerminalName", err)
	}
}

func TestUnreadableSettingsAreNoSettings(t *testing.T) {
	path := SettingsPath(t.TempDir())
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, content := range []string{
		"not json",
		`{"version": 99, "bidi": {"iTerm2": "on"}}`,
		`{"version": 1, "bidi": {"iTerm2": "sideways"}}`,
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, reason := LoadSettings(path).ResolveBidi(bidi.ModeAuto, iterm); reason == "remembered for iTerm2" {
			t.Errorf("settings %q were trusted", content)
		}
	}
}
