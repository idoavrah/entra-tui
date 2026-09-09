package ui

import "github.com/charmbracelet/lipgloss"

// Palette is a dark, Tokyo-Night-adjacent scheme. Every colour is given as a
// 256-colour/TrueColor pair so the UI degrades sanely on limited terminals
// rather than washing out to white on white.
var (
	colBase    = lipgloss.AdaptiveColor{Light: "#1a1b26", Dark: "#c0caf5"}
	colDim     = lipgloss.AdaptiveColor{Light: "#6c7086", Dark: "#565f89"}
	colAccent  = lipgloss.AdaptiveColor{Light: "#2e5eaa", Dark: "#7aa2f7"}
	colOK      = lipgloss.AdaptiveColor{Light: "#2c7a39", Dark: "#9ece6a"}
	colWarn    = lipgloss.AdaptiveColor{Light: "#a06000", Dark: "#e0af68"}
	colErr     = lipgloss.AdaptiveColor{Light: "#b3261e", Dark: "#f7768e"}
	colSelBg   = lipgloss.AdaptiveColor{Light: "#d6e0ff", Dark: "#283457"}
	colHeadFg  = lipgloss.AdaptiveColor{Light: "#1a1b26", Dark: "#7dcfff"}
	colKeyName = lipgloss.AdaptiveColor{Light: "#5a4a9f", Dark: "#bb9af7"}
)

var (
	styleTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#1a1b26")).
			Background(colAccent).
			Padding(0, 1)

	styleContextKey = lipgloss.NewStyle().Foreground(colDim)
	styleContextVal = lipgloss.NewStyle().Foreground(colBase).Bold(true)

	styleResourceBar = lipgloss.NewStyle().Bold(true)

	styleTableHead = lipgloss.NewStyle().
			Foreground(colHeadFg).
			Bold(true).
			Underline(true)

	styleRow = lipgloss.NewStyle().Foreground(colBase)

	// Whole-row tints. Reading one flag out of a column of fifty is what a
	// colour is for, so the state colours the line rather than the cell.
	styleRowMuted = lipgloss.NewStyle().Foreground(colDim)
	styleRowWarn  = lipgloss.NewStyle().Foreground(colWarn)

	styleRowSelected = lipgloss.NewStyle().
				Foreground(colBase).
				Background(colSelBg).
				Bold(true)

	styleDim   = lipgloss.NewStyle().Foreground(colDim)
	styleOK    = lipgloss.NewStyle().Foreground(colOK)
	styleWarn  = lipgloss.NewStyle().Foreground(colWarn)
	styleErr   = lipgloss.NewStyle().Foreground(colErr).Bold(true)
	styleFlash = lipgloss.NewStyle().Foreground(colOK).Bold(true)

	styleHintKey  = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	styleHintDesc = lipgloss.NewStyle().Foreground(colDim)

	stylePrompt = lipgloss.NewStyle().Foreground(colWarn).Bold(true)

	styleDetailKey = lipgloss.NewStyle().Foreground(colKeyName).Bold(true)
	styleDetailVal = lipgloss.NewStyle().Foreground(colBase)

	styleHelpTitle = lipgloss.NewStyle().Foreground(colAccent).Bold(true).Underline(true)

	// styleSectionTitle heads each group in the detail pane.
	styleSectionTitle = lipgloss.NewStyle().Foreground(colHeadFg).Bold(true)

	// styleBorder draws the frame around the content area.
	styleBorder = lipgloss.NewStyle().Foreground(colDim)

	// Tab strip below the properties in a detail pane.
	styleTabActive = lipgloss.NewStyle().Foreground(colHeadFg).Bold(true).Underline(true)
	styleTabIdle   = lipgloss.NewStyle().Foreground(colDim)
)

// accentStyle tints text with a resource's own accent colour, matching the way
// k9s colours each resource view.
func accentStyle(hex string) lipgloss.Style {
	if hex == "" {
		return lipgloss.NewStyle().Foreground(colAccent)
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(hex)).Bold(true)
}
