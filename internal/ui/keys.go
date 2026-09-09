package ui

import "github.com/charmbracelet/bubbles/key"

// keyMap holds every binding. The vocabulary is deliberately k9s's: ":" to
// change view, "/" to filter, Esc to back out, "?" for help.
type keyMap struct {
	Up        key.Binding
	Down      key.Binding
	PageUp    key.Binding
	PageDown  key.Binding
	Home      key.Binding
	End       key.Binding
	Enter     key.Binding
	Back      key.Binding
	Command   key.Binding
	Filter    key.Binding
	Search    key.Binding
	NextPage  key.Binding
	LoadAll   key.Binding
	Refresh   key.Binding
	Yank      key.Binding
	RawToggle key.Binding
	Help      key.Binding
	Quit      key.Binding
}

var keys = keyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "down"),
	),
	PageUp: key.NewBinding(
		key.WithKeys("pgup", "ctrl+b"),
		key.WithHelp("pgup", "page up"),
	),
	PageDown: key.NewBinding(
		key.WithKeys("pgdown", "ctrl+f"),
		key.WithHelp("pgdn", "page down"),
	),
	Home: key.NewBinding(
		key.WithKeys("home", "g"),
		key.WithHelp("g", "top"),
	),
	End: key.NewBinding(
		key.WithKeys("end", "G"),
		key.WithHelp("G", "bottom"),
	),
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "describe"),
	),
	Back: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "back"),
	),
	Command: key.NewBinding(
		key.WithKeys(":"),
		key.WithHelp(":", "command"),
	),
	Filter: key.NewBinding(
		key.WithKeys("/"),
		key.WithHelp("/", "filter loaded"),
	),
	Search: key.NewBinding(
		key.WithKeys("s"),
		key.WithHelp("s", "server search"),
	),
	NextPage: key.NewBinding(
		key.WithKeys("n"),
		key.WithHelp("n", "next page"),
	),
	LoadAll: key.NewBinding(
		key.WithKeys("A"),
		key.WithHelp("A", "load all"),
	),
	Refresh: key.NewBinding(
		key.WithKeys("r", "ctrl+r"),
		key.WithHelp("r", "refresh"),
	),
	Yank: key.NewBinding(
		key.WithKeys("y"),
		key.WithHelp("y", "yank id"),
	),
	RawToggle: key.NewBinding(
		key.WithKeys("R"),
		key.WithHelp("R", "raw json"),
	),
	Help: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "help"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c"),
		key.WithHelp("q", "quit"),
	),
}
