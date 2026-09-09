package ui

import "github.com/charmbracelet/lipgloss"

// The ENTRA TUI wordmark, in two sizes.
//
// logoFull is the Figlet "ANSI Shadow" rendering of "entra tui"; logoCompact
// is a three-row box-drawing version for terminals with no room for it. The
// header picks between them, and drops both when even the small one would
// crowd out something that does something.
//
// Every line of a wordmark is padded to the same number of display cells,
// which the header alignment depends on.
var logoFull = []string{
	"███████╗███╗   ██╗████████╗██████╗  █████╗     ████████╗██╗   ██╗██╗",
	"██╔════╝████╗  ██║╚══██╔══╝██╔══██╗██╔══██╗    ╚══██╔══╝██║   ██║██║",
	"█████╗  ██╔██╗ ██║   ██║   ██████╔╝███████║       ██║   ██║   ██║██║",
	"██╔══╝  ██║╚██╗██║   ██║   ██╔══██╗██╔══██║       ██║   ██║   ██║██║",
	"███████╗██║ ╚████║   ██║   ██║  ██║██║  ██║       ██║   ╚██████╔╝██║",
	"╚══════╝╚═╝  ╚═══╝   ╚═╝   ╚═╝  ╚═╝╚═╝  ╚═╝       ╚═╝    ╚═════╝ ╚═╝",
}

var logoCompact = []string{
	"┌─┐┌┐┌┌┬┐┬─┐┌─┐  ┌┬┐┬ ┬┬",
	"├┤ │││ │ ├┬┘├─┤   │ │ ││",
	"└─┘┘└┘ ┴ ┴└─┴ ┴   ┴ └─┘┴",
}

// logoWidth is the display width of a wordmark's widest line.
func logoWidth(logo []string) int {
	w := 0
	for _, l := range logo {
		w = max(w, lipgloss.Width(l))
	}
	return w
}
