package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/idoavrah/entra-tui/internal/graph"
)

// hostileName is a display name of the kind anyone who can create a directory
// object could set: an OSC 52 clipboard write, a screen clear, a cursor jump
// and a right-to-left override, wrapped around something that looks ordinary.
const hostileName = "Payroll\x1b]52;c;Y3VybCBldmlsLnNoIHwgc2gK\x07\x1b[2J\x1b[3A‮ Sync"

// asGraphSaw is what the string looks like once it has come off the wire,
// which is the only way the app ever sees directory data.
func asGraphSaw(s string) string {
	return graph.Item{"v": s}.String("v")
}

// assertNoInjectedControls fails if a rendered screen carries anything the
// data could have used to steer the terminal. Everything the app draws is
// styled, so the frame is full of escape sequences of its own; what must
// never appear is one a display name brought with it.
func assertNoInjectedControls(t *testing.T, view, where string) {
	t.Helper()
	for _, bad := range []struct{ what, seq string }{
		{"an OSC 52 clipboard write", "\x1b]52"},
		{"an operating system command", "\x1b]"},
		{"a screen clear", "\x1b[2J"},
		{"a cursor move", "\x1b[3A"},
		{"a bell", "\a"},
		{"a carriage return", "\r"},
		{"a right-to-left override", "‮"},
	} {
		if strings.Contains(view, bad.seq) {
			t.Errorf("%s: the rendered screen carries %s from a display name", where, bad.what)
		}
	}
}

func TestHostileNamesCannotDriveTheTerminal(t *testing.T) {
	m := loadUsers(t, browsing(t), asGraphSaw(hostileName))
	m = describeRow(t, m)

	m.detail.Object = graph.Item{"id": "u1", "displayName": hostileName,
		"jobTitle": hostileName, "userPrincipalName": hostileName}
	m.detail.Owners = []graph.Item{{"id": "u2", "displayName": hostileName}}
	m.detail.Groups = []graph.Item{{"id": "g1", "displayName": hostileName}}
	m.detailSections = graph.Sections(m.detail)
	m = m.refreshDetail()

	for _, s := range []screen{screenDashboard, screenBrowse, screenDetail, screenHelp} {
		m := m
		m.screen = s
		assertNoInjectedControls(t, m.View(), "a screen")
	}

	// The dialog that authorises a write is the one that most has to say
	// what it does, so it gets its own check: a name that can repaint the
	// lines above it turns a confirmation into a decoy.
	m.modal = modalConfirm
	m.modalAction = actionRemove
	m.modalEntry = detailEntry{id: "u2", name: asGraphSaw(hostileName), detail: asGraphSaw(hostileName)}
	m.modalRel = graph.RelOwners
	assertNoInjectedControls(t, m.View(), "the confirmation dialog")

	// ...as does the raw view, which shows the object whole.
	m.modal = modalNone
	m.detailRaw = true
	m = m.refreshDetail()
	assertNoInjectedControls(t, m.View(), "the raw view")
}

func TestHostileTextIsSafeEverywhereElseItAppears(t *testing.T) {
	m := browsing(t)
	term := asGraphSaw(hostileName)
	m.history.record(string(graph.KindUsers), term)
	m.coll.search = term
	m.flash = term
	m.mode = modeSearch
	m.input.SetValue(term)

	assertNoInjectedControls(t, m.View(), "the quick searches, prompt, title and flash")

	// A Graph error carries text from a server, and it echoes the query back.
	m.mode = modeNormal
	m.err = &graph.APIError{Status: 400, Code: "Request_BadRequest", Message: hostileName}
	assertNoInjectedControls(t, m.View(), "the error status line")
}

func TestHostileNamesStaySafeAtEveryWidth(t *testing.T) {
	// Truncation is where a half-written escape sequence would come from, so
	// the widths that cut a cell matter as much as the ones that do not.
	m := loadUsers(t, browsing(t), asGraphSaw(hostileName))
	for _, size := range []tea.WindowSizeMsg{
		{Width: 40, Height: 12}, {Width: 80, Height: 24}, {Width: 200, Height: 50},
	} {
		assertNoInjectedControls(t, send(t, m, size).View(), "a resized screen")
	}
}
