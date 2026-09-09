package ui

// logo is the ENTRA TUI wordmark shown at the top right of every screen.
// Each line is exactly logoWidth display cells wide, which the header
// alignment depends on.
var logo = []string{
	"┌─┐┌┐┌┌┬┐┬─┐┌─┐  ┌┬┐┬ ┬┬",
	"├┤ │││ │ ├┬┘├─┤   │ │ ││",
	"└─┘┘└┘ ┴ ┴└─┴ ┴   ┴ └─┘┴",
}

const (
	logoWidth  = 24
	logoHeight = 3
	// logoMinTerminalWidth is the narrowest terminal that still has room for
	// the wordmark beside the context block. Below it the logo is dropped
	// rather than wrapped.
	logoMinTerminalWidth = 90
)
