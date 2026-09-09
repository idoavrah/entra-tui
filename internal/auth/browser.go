package auth

import (
	"io"

	"github.com/pkg/browser"
)

// OpenInBrowser launches the system browser without letting the launcher
// write to the terminal.
//
// xdg-open and its equivalents print diagnostics on stdout and stderr. With
// the TUI holding the alternate screen buffer, that output lands in the
// middle of the rendered frame, so it is discarded here.
func OpenInBrowser(url string) error {
	browser.Stdout = io.Discard
	browser.Stderr = io.Discard
	return browser.OpenURL(url)
}
