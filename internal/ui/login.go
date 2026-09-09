package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/idoavrah/entra-tui/internal/auth"
	"github.com/idoavrah/entra-tui/internal/graph"
)

// Indices into authOptions, named so the preselection logic reads clearly.
const (
	authOptionBrowser = iota
	authOptionAzureCLI
)

// authOption is one sign-in method offered on the login screen.
type authOption struct {
	Title  string
	Detail string
	Method auth.Method
}

func authOptions() []authOption {
	return []authOption{
		{
			Title:  "Sign in with your browser",
			Detail: "OAuth 2.0 authorization code with PKCE. Opens the Entra sign-in page; MFA and Conditional Access apply as usual.",
			Method: auth.MethodBrowser,
		},
		{
			Title:  "Use the Azure CLI session",
			Detail: "Borrows the Microsoft Graph token from an existing `az login`. No prompt, but only if you are already signed in.",
			Method: auth.MethodAzureCLI,
		},
	}
}

func (m Model) handleLoginKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// While a sign-in is running the only useful keys are quit and cancel.
	if m.authing {
		switch {
		case key.Matches(msg, keys.Quit):
			m.quitting = true
			return m, tea.Quit
		case key.Matches(msg, keys.Back):
			// The in-flight attempt is abandoned by bumping the counter; its
			// result is discarded when it eventually arrives.
			m.authAttempt++
			m.authing = false
			m.authURL = ""
			return m, nil
		}
		return m, nil
	}

	opts := authOptions()
	switch {
	case key.Matches(msg, keys.Quit):
		m.quitting = true
		return m, tea.Quit
	case key.Matches(msg, keys.Help):
		m.helpReturn = m.screen
		m.screen = screenHelp
		return m, nil
	case key.Matches(msg, keys.Up):
		m.authCursor = clamp(m.authCursor-1, 0, len(opts)-1)
		return m, nil
	case key.Matches(msg, keys.Down):
		m.authCursor = clamp(m.authCursor+1, 0, len(opts)-1)
		return m, nil
	case key.Matches(msg, keys.Enter):
		m.authAttempt++
		m.authing = true
		m.err = nil
		m.authURL = ""
		return m, m.authenticate(opts[m.authCursor].Method)
	}

	// Digits pick an option directly.
	if n, ok := digitIndex(msg.String()); ok && n < len(opts) {
		m.authCursor = n
		m.authAttempt++
		m.authing = true
		m.err = nil
		m.authURL = ""
		return m, m.authenticate(opts[n].Method)
	}
	return m, nil
}

func (m Model) renderLogin() string {
	var b strings.Builder

	b.WriteString(styleDim.Render(
		"entra-tui reads your directory as you, using delegated permissions.\n"+
			"Nothing is queried until you sign in and pick a view.") + "\n\n")

	for i, opt := range authOptions() {
		marker := "  "
		title := styleContextVal.Render(opt.Title)
		if i == m.authCursor {
			marker = styleHintKey.Render("▸ ")
			title = accentStyle("").Render(opt.Title)
		}
		b.WriteString(marker + styleHintKey.Render(itoa(i+1)+". ") + title + "\n")
		b.WriteString("     " + styleDim.Render(wrapText(opt.Detail, max(30, m.width-8))) + "\n\n")
	}

	switch {
	case m.authing:
		b.WriteString(styleWarn.Render(m.spin.View()+" signing in…") + "\n")
		if m.authURL != "" {
			b.WriteString("\n" + styleDim.Render("If your browser did not open, visit:") + "\n")
			b.WriteString(styleContextVal.Render(graph.Truncate(m.authURL, max(20, m.width-2))) + "\n")
		}
		b.WriteString("\n" + styleHintDesc.Render("esc cancels this attempt."))
	case m.err != nil:
		b.WriteString(styleErr.Render("✗ "+m.err.Error()) + "\n")
		if hint := apiHint(m.err); hint != "" {
			b.WriteString(styleWarn.Render(hint) + "\n")
		}
		b.WriteString("\n" + styleHintDesc.Render("Pick a method to try again."))
	}

	hints := hintBar(m.width,
		[2]string{"↑/↓", "choose"},
		[2]string{"enter", "sign in"},
		[2]string{"1/2", "pick directly"},
		[2]string{"?", "help"},
		[2]string{"q", "quit"},
	)

	return m.chrome(styleHelpTitle.Render("SIGN IN"), "",
		strings.Split(b.String(), "\n"), hints)
}

// itoa renders a small non-negative integer without pulling in strconv at
// every call site.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// wrapText breaks text onto lines of at most width cells, indenting
// continuation lines to line up under the first.
func wrapText(s string, width int) string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return ""
	}
	var lines []string
	line := words[0]
	for _, w := range words[1:] {
		if len(line)+1+len(w) > width {
			lines = append(lines, line)
			line = w
			continue
		}
		line += " " + w
	}
	lines = append(lines, line)
	return strings.Join(lines, "\n     ")
}
