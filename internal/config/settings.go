package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/idoavrah/entra-tui/internal/bidi"
)

// settingsFileVersion guards the on-disk format. A file written by a future
// version is ignored rather than misread.
const settingsFileVersion = 1

// Settings are choices made inside the app that outlive the session.
//
// They live in the user's config directory, not beside the search slots in
// the cache: a cache is the system's to clear, and these are decisions
// somebody made. Reading is best-effort -- a file that is missing or cannot
// be understood is simply no settings.
type Settings struct {
	path string
	// bidi is who reorders right-to-left text, per terminal name.
	bidi map[string]bidi.Mode
}

// settingsFile is the on-disk shape.
type settingsFile struct {
	Version int                  `json:"version"`
	Bidi    map[string]bidi.Mode `json:"bidi,omitempty"`
}

// ErrNoTerminalName reports a terminal that does not say what it is, so a
// choice made in it has nothing to be remembered against.
var ErrNoTerminalName = errors.New("the terminal does not say what it is")

// SettingsPath is the settings file's location under dir, or under the
// user's config directory when dir is empty. Empty when neither is known.
func SettingsPath(dir string) string {
	if dir == "" {
		var err error
		if dir, err = os.UserConfigDir(); err != nil {
			return ""
		}
	}
	return filepath.Join(dir, "entra-tui", "settings.json")
}

// LoadSettings reads the settings at path.
func LoadSettings(path string) *Settings {
	s := &Settings{path: path, bidi: map[string]bidi.Mode{}}
	if path == "" {
		return s
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	var file settingsFile
	if err := json.Unmarshal(data, &file); err != nil || file.Version != settingsFileVersion {
		return s
	}
	for terminal, mode := range file.Bidi {
		// Only a real choice is kept. "auto" is not one, and anything else
		// was not written by this program.
		if mode == bidi.ModeOn || mode == bidi.ModeOff {
			s.bidi[terminal] = mode
		}
	}
	return s
}

// ResolveBidi decides whether entra-tui reorders right-to-left text, and why.
//
// An explicit -bidi or ENTRA_TUI_BIDI wins. Under auto, a choice remembered
// for this terminal comes before the guess from its name, because it is what
// somebody saw with their own eyes in this very terminal.
func (s *Settings) ResolveBidi(mode bidi.Mode, getenv func(string) string) (reorder bool, reason string) {
	if mode == bidi.ModeAuto {
		terminal := bidi.TerminalName(getenv)
		if saved, ok := s.bidi[terminal]; ok && terminal != "" {
			return saved == bidi.ModeOn, "remembered for " + terminal
		}
	}
	return mode.Resolve(getenv)
}

// RememberBidi records who reorders right-to-left text in this terminal.
//
// A choice that agrees with the guess is forgotten rather than stored: the
// guess is never written down, so a release that corrects a wrong one still
// reaches everybody who never overrode it -- and pressing b back to the
// guess is how a remembered choice is undone.
func (s *Settings) RememberBidi(reorder bool, getenv func(string) string) error {
	terminal := bidi.TerminalName(getenv)
	if terminal == "" {
		return ErrNoTerminalName
	}
	if guess, _ := bidi.ModeAuto.Resolve(getenv); guess == reorder {
		delete(s.bidi, terminal)
	} else if reorder {
		s.bidi[terminal] = bidi.ModeOn
	} else {
		s.bidi[terminal] = bidi.ModeOff
	}
	return s.save()
}

// save writes the settings back, through a temporary file renamed into place
// so an interrupted write leaves the previous file intact.
func (s *Settings) save() error {
	if s.path == "" {
		return errors.New("no config directory")
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(settingsFile{Version: settingsFileVersion, Bidi: s.bidi}, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), "settings-*.json")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, s.path); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}
