package ui

import "github.com/charmbracelet/bubbles/key"

// keyMap holds every binding. The vocabulary is k9s's: ":" changes view, "/"
// searches, Esc backs out one layer, "?" explains.
type keyMap struct {
	Up        key.Binding
	Down      key.Binding
	Left      key.Binding
	Right     key.Binding
	PageUp    key.Binding
	PageDown  key.Binding
	Home      key.Binding
	End       key.Binding
	Enter     key.Binding
	Back      key.Binding
	Dashboard key.Binding
	Command   key.Binding
	Search    key.Binding
	Refresh   key.Binding
	Copy      key.Binding
	RawToggle key.Binding
	Pair      key.Binding
	Help      key.Binding
	Quit      key.Binding
}

var keys = keyMap{
	Up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
	Down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
	Left:     key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "left")),
	Right:    key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "right")),
	PageUp:   key.NewBinding(key.WithKeys("pgup", "ctrl+b"), key.WithHelp("pgup", "page up")),
	PageDown: key.NewBinding(key.WithKeys("pgdown", "ctrl+f"), key.WithHelp("pgdn", "page down")),
	Home:     key.NewBinding(key.WithKeys("home", "g"), key.WithHelp("g", "top")),
	End:      key.NewBinding(key.WithKeys("end", "G"), key.WithHelp("G", "bottom")),
	Enter:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
	Back:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	// The dashboard is reachable directly as well as by backing out, because
	// backing out of a searched view takes two presses.
	Dashboard: key.NewBinding(key.WithKeys("~"), key.WithHelp("~", "dashboard")),
	Command:   key.NewBinding(key.WithKeys(":"), key.WithHelp(":", "command")),
	Search:    key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
	Refresh:   key.NewBinding(key.WithKeys("r", "ctrl+r"), key.WithHelp("r", "refresh")),
	// "c" for copy is what someone guesses first; "y" stays bound because
	// vi's yank is what the fingers of anyone coming from k9s will reach for.
	Copy:      key.NewBinding(key.WithKeys("c", "y"), key.WithHelp("c", "copy id")),
	RawToggle: key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "raw json")),
	// x jumps between an app registration and its enterprise application,
	// which are two halves of the same thing in Entra.
	Pair: key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "app reg ⇄ ent app")),
	Help: key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	Quit: key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
}
