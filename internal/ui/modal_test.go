package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/idoavrah/entra-tui/internal/auth"
	"github.com/idoavrah/entra-tui/internal/graph"
)

// groupDetail opens a group's detail pane with two members and one owner.
func groupDetail(t *testing.T, _ bool) Model {
	t.Helper()
	res, _ := graph.Lookup("users")
	m := New(t.Context(), Options{
		Auth:     auth.Options{TenantID: "organizations"},
		GraphURL: "http://127.0.0.1:1/v1.0", PageSize: 100, Resource: res,
		CacheDir: t.TempDir(),
	})
	m = send(t, m, tea.WindowSizeMsg{Width: 150, Height: 40})
	m = send(t, m, authDoneMsg{attempt: m.authAttempt, provider: stubProvider{}})

	groups, _ := graph.Lookup("groups")
	next, _ := m.openResource(groups)
	m = next.(Model)
	m.loading = false
	m = send(t, m, pageMsg{gen: m.gen, page: &graph.Page{Items: []graph.Item{
		{"id": "g1", "displayName": "Research"},
	}}})
	m = send(t, m, press("enter"))
	return send(t, m, detailMsg{gen: m.gen, detail: graph.Detail{
		Kind:   graph.KindGroups,
		Object: graph.Item{"id": "g1", "displayName": "Research", "securityEnabled": true},
		Owners: []graph.Item{{"id": "u9", "displayName": "Owner One", "userPrincipalName": "o1@x.com"}},
		Members: []graph.Item{
			{"id": "u1", "displayName": "Ada", "userPrincipalName": "ada@x.com"},
			{"id": "u2", "displayName": "Grace", "userPrincipalName": "grace@x.com"},
		},
	}})
}

func TestListsBecomeTabsBelowTheProperties(t *testing.T) {
	m := groupDetail(t, true)

	// The object's own fields stay above; its collections become tabs.
	for _, s := range m.propertySections() {
		if s.List {
			t.Errorf("section %q is a list but was kept with the properties", s.Title)
		}
	}
	titles := map[string]bool{}
	for _, s := range m.listSections() {
		titles[s.Title] = true
	}
	for _, want := range []string{"Owners", "Members"} {
		if !titles[want] {
			t.Errorf("%q is not one of the tabs", want)
		}
	}

	view := m.View()
	if !strings.Contains(view, "Members (2)") {
		t.Error("the tab strip does not show the list and its size")
	}
}

func TestArrowsWalkTheActiveTab(t *testing.T) {
	m := groupDetail(t, true)

	first, ok := m.selectedEntry()
	if !ok || first.id != "u9" {
		t.Fatalf("first selection = %+v ok=%v, want the first owner", first, ok)
	}

	// Owners has one entry, so the cursor stops rather than wrapping.
	for range 5 {
		m = send(t, m, press("down"))
	}
	if m.tabCursor != 0 {
		t.Errorf("cursor = %d, want it clamped to the single owner", m.tabCursor)
	}
}

func TestLeftRightSwitchTabs(t *testing.T) {
	m := groupDetail(t, true)
	lists := m.listSections()
	if len(lists) < 2 {
		t.Fatalf("only %d tabs, want at least two to switch between", len(lists))
	}

	m = send(t, m, press("right"))
	if m.detailTab != 1 {
		t.Fatalf("tab = %d, want the second", m.detailTab)
	}
	entry, ok := m.selectedEntry()
	if !ok || entry.rel != graph.RelMembers {
		t.Errorf("selection = %+v, want a member from the second tab", entry)
	}

	// Switching starts the new list at the top.
	m = send(t, m, press("down"))
	m = send(t, m, press("left"))
	if m.tabCursor != 0 {
		t.Errorf("cursor = %d, want a fresh tab to start at the top", m.tabCursor)
	}
	if m.detailTab != 0 {
		t.Errorf("tab = %d, want the first", m.detailTab)
	}

	// And stops at the ends.
	for range 10 {
		m = send(t, m, press("left"))
	}
	if m.detailTab != 0 {
		t.Errorf("tab = %d, want it clamped at the first", m.detailTab)
	}
}

func TestPagesScrollThePropertiesNotTheList(t *testing.T) {
	m := groupDetail(t, true)
	before := m.tabCursor

	m = send(t, m, tea.KeyMsg{Type: tea.KeyPgDown})
	if m.tabCursor != before {
		t.Error("paging moved the list selection; pages belong to the properties")
	}
	m = send(t, m, tea.KeyMsg{Type: tea.KeyPgUp})
	if m.detailVP.YOffset != 0 {
		t.Errorf("YOffset = %d, want page up to return to the top", m.detailVP.YOffset)
	}
}

func TestAddFlowNeedsExactlyOneMatchThenAConfirmation(t *testing.T) {
	// "a" adds to the tab in front, so the members tab is where a member is
	// added from.
	m := tabTitled(t, groupDetail(t, true), "Members")
	m = send(t, m, press("a"))
	if m.modal != modalAddPrompt {
		t.Fatalf("modal = %v, want the add prompt", m.modal)
	}
	m = typeKeys(t, m, "ada")
	m = send(t, m, press("enter"))
	if !m.modalBusy {
		t.Error("committing the prompt did not start a lookup")
	}

	// Two matches is not a guess worth making.
	ambiguous := send(t, m, principalsMsg{gen: m.gen, term: "ada", items: []graph.Item{
		{"id": "u1", "displayName": "Ada Lovelace"},
		{"id": "u2", "displayName": "Ada Byron"},
	}})
	if ambiguous.modal != modalAddPrompt {
		t.Error("an ambiguous match moved on to the confirmation")
	}
	if !strings.Contains(ambiguous.modalError, "be more specific") {
		t.Errorf("modalError = %q, want it to ask for a narrower term", ambiguous.modalError)
	}

	// Nothing found says so.
	none := send(t, m, principalsMsg{gen: m.gen, term: "nobody"})
	if !strings.Contains(none.modalError, "Nothing in the directory") {
		t.Errorf("modalError = %q, want a no-match message", none.modalError)
	}

	// Exactly one moves to the confirmation, naming who and where.
	m = send(t, m, principalsMsg{gen: m.gen, term: "ada", items: []graph.Item{
		{"id": "u7", "displayName": "Ada Lovelace", "userPrincipalName": "ada@x.com"},
	}})
	if m.modal != modalConfirm || m.modalAction != actionAdd {
		t.Fatalf("modal = %v action = %v, want an add confirmation", m.modal, m.modalAction)
	}
	view := m.View()
	for _, want := range []string{"Ada Lovelace", "ada@x.com", "Research", "Add member"} {
		if !strings.Contains(view, want) {
			t.Errorf("the confirmation does not mention %q", want)
		}
	}
}

func TestConfirmationDefaultsToNo(t *testing.T) {
	m := groupDetail(t, true)
	m = send(t, m, press("d"))
	if m.modal != modalConfirm || m.modalAction != actionRemove {
		t.Fatalf("modal = %v action = %v, want a remove confirmation", m.modal, m.modalAction)
	}

	// Enter is the key a held-down finger hits; it must not commit.
	for _, k := range []tea.KeyMsg{
		{Type: tea.KeyEnter}, {Type: tea.KeyEsc},
		{Type: tea.KeyRunes, Runes: []rune("n")},
		{Type: tea.KeyRunes, Runes: []rune("x")},
	} {
		got := send(t, groupDetail(t, true), press("d"))
		got = send(t, got, k)
		if got.modal != modalNone {
			t.Errorf("key %v left the dialog open", k)
		}
		if got.modalBusy {
			t.Errorf("key %v started a write", k)
		}
	}

	// Only y proceeds.
	confirmed := send(t, m, press("y"))
	if !confirmed.modalBusy {
		t.Error("y did not start the removal")
	}
}

func TestDeleteNamesTheSelectedEntry(t *testing.T) {
	m := groupDetail(t, true)
	m = send(t, m, press("right")) // members tab
	m = send(t, m, press("d"))

	view := m.View()
	if !strings.Contains(view, "Delete member") {
		t.Error("the confirmation does not say what it would do")
	}
	if !strings.Contains(view, "Ada") {
		t.Error("the confirmation does not name the selected member")
	}
}

func TestWriteFailureStaysInTheDialog(t *testing.T) {
	m := groupDetail(t, true)
	m = send(t, m, press("d"))
	m = send(t, m, press("y"))

	m = send(t, m, writeDoneMsg{gen: m.gen, err: &graph.APIError{
		Status: 403, Code: "Authorization_RequestDenied", Message: "Insufficient privileges",
	}})
	if m.modal != modalConfirm {
		t.Error("a failed write closed the dialog, hiding what went wrong")
	}
	if !strings.Contains(m.modalError, "no permission") {
		t.Errorf("modalError = %q, want a succinct refusal", m.modalError)
	}
	if len(m.modalError) > 80 {
		t.Errorf("modalError = %q, want a short refusal rather than an essay", m.modalError)
	}
}

func TestSuccessfulWriteClosesAndReloads(t *testing.T) {
	m := groupDetail(t, true)
	m = send(t, m, press("d"))
	m = send(t, m, press("y"))

	m = send(t, m, writeDoneMsg{gen: m.gen, summary: "removed Owner One as owner"})
	if m.modal != modalNone {
		t.Error("the dialog stayed open after a successful write")
	}
	if !strings.Contains(m.flash, "removed Owner One") {
		t.Errorf("flash = %q, want the change reported", m.flash)
	}
	// The pane is re-read rather than patched locally, so it shows what
	// Graph holds rather than what was asked for.
	if !m.detailLoading {
		t.Error("the object was not re-read after the change")
	}
}

// tabTitled moves the tab strip to the named list.
func tabTitled(t *testing.T, m Model, title string) Model {
	t.Helper()
	for i, s := range m.listSections() {
		if s.Title == title {
			m.detailTab, m.tabCursor, m.tabOffset = i, 0, 0
			return m.refreshDetail()
		}
	}
	t.Fatalf("no %q tab; have %d lists", title, len(m.listSections()))
	return m
}

func TestAddOnTheOwnersTabAddsAnOwner(t *testing.T) {
	m := send(t, tabTitled(t, groupDetail(t, true), "Owners"), press("a"))
	if m.modalRel != graph.RelOwners {
		t.Errorf("modalRel = %q, want owners", m.modalRel)
	}
}

func TestOwnersTabOfADeviceWritesRegisteredOwners(t *testing.T) {
	// Devices keep their owners under registeredOwners; using "owners"
	// would write to a collection that does not exist. The tab carries the
	// right name, which is what the add key now goes by.
	m := groupDetail(t, true)
	m.detail = graph.Detail{
		Kind:   graph.KindDevices,
		Object: graph.Item{"id": "d1", "displayName": "Rig"},
		Owners: []graph.Item{{"id": "u9", "displayName": "Owner One"}},
	}
	m.detailSections = graph.Sections(m.detail)
	m = send(t, tabTitled(t, m.refreshDetail(), "Registered owners"), press("a"))

	if m.modalRel != graph.RelRegisteredOwners {
		t.Errorf("modalRel = %q, want registeredOwners", m.modalRel)
	}
}

func TestAddRefusedOnAListThatCannotBeWritten(t *testing.T) {
	m := groupDetail(t, true)
	m.detail = graph.Detail{
		Kind:   graph.KindUsers,
		Object: graph.Item{"id": "u1", "displayName": "Ada"},
		Groups: []graph.Item{{"id": "g1", "displayName": "One"}},
	}
	m.detailSections = graph.Sections(m.detail)
	m = send(t, tabTitled(t, m.refreshDetail(), "Groups"), press("a"))

	if m.modal != modalNone {
		t.Error("a read-only list offered an add dialog")
	}
	if !strings.Contains(strings.ToLower(m.flash), "groups") {
		t.Errorf("flash = %q, want it to name the list it refused", m.flash)
	}
}

func TestModalSwallowsKeysMeantForTheScreenBehind(t *testing.T) {
	m := groupDetail(t, true)
	m = send(t, m, press("a"))
	m = typeKeys(t, m, "q:x")

	if m.quitting {
		t.Error("typing q in a dialog quit the app")
	}
	if m.input.Value() != "q:x" {
		t.Errorf("input = %q, want the literal keystrokes", m.input.Value())
	}
}

func TestDialogNeverOverflowsItsBorder(t *testing.T) {
	// A styled line cannot be truncated after the fact without severing an
	// escape sequence, so over-long content has to be cut before styling --
	// otherwise a long display name punches through the dialog's frame.
	m := groupDetail(t, true)
	m = send(t, m, press("a"))
	m = send(t, m, principalsMsg{gen: m.gen, term: "x", items: []graph.Item{{
		"id":                "u7",
		"displayName":       strings.Repeat("Very Long Display Name ", 6),
		"userPrincipalName": strings.Repeat("long", 20) + "@contoso.onmicrosoft.com",
	}}})

	for _, line := range m.renderModal(m.contentHeight()) {
		if w := lipgloss.Width(line); w > boxInnerWidth(m.width) {
			t.Fatalf("dialog line is %d cells, over the %d-cell pane:\n%s",
				w, boxInnerWidth(m.width), line)
		}
	}

	// Every framed row must be the same width, or the border is broken.
	var widths []int
	for _, line := range m.renderModal(m.contentHeight()) {
		if strings.Contains(line, boxVertical) || strings.Contains(line, boxTopLeft) {
			widths = append(widths, lipgloss.Width(line))
		}
	}
	for i, w := range widths {
		if w != widths[0] {
			t.Errorf("framed line %d is %d cells, the first is %d", i, w, widths[0])
		}
	}
}

func TestLongErrorMessagesWrapInsideTheDialog(t *testing.T) {
	m := groupDetail(t, true)
	m = send(t, m, press("d"))
	m = send(t, m, press("y"))
	m = send(t, m, writeDoneMsg{gen: m.gen, err: &graph.APIError{
		Status: 403, Code: "Authorization_RequestDenied",
		Message: strings.Repeat("Insufficient privileges to complete the operation. ", 4),
	}})

	for _, line := range m.renderModal(m.contentHeight()) {
		if w := lipgloss.Width(line); w > boxInnerWidth(m.width) {
			t.Fatalf("error line is %d cells, over the pane: %s", w, line)
		}
	}
}

func TestConfirmationsIdentifyEveryPartyByID(t *testing.T) {
	// Display names are not unique in a directory. Confirming against the
	// wrong "Ada Lovelace" is exactly the mistake this dialog exists to
	// prevent, so every party carries its object id.
	m := groupDetail(t, true)
	m = send(t, m, press("d")) // remove the owner

	view := m.View()
	for _, want := range []string{
		"Owner One", "u9", // the principal, by name and id
		"Research", "g1", // the object it is being removed from
	} {
		if !strings.Contains(view, want) {
			t.Errorf("the remove confirmation does not show %q", want)
		}
	}

	// And the same for an add.
	m = groupDetail(t, true)
	m = send(t, m, press("a"))
	m = send(t, m, principalsMsg{gen: m.gen, term: "ada", items: []graph.Item{
		{"id": "u7", "displayName": "Ada Lovelace", "userPrincipalName": "ada@x.com"},
	}})
	view = m.View()
	for _, want := range []string{"Ada Lovelace", "u7", "ada@x.com", "Research", "g1"} {
		if !strings.Contains(view, want) {
			t.Errorf("the add confirmation does not show %q", want)
		}
	}
}

func TestIdentifyDegradesWhenSomethingIsMissing(t *testing.T) {
	for _, tc := range []struct{ name, id, want string }{
		{"Ada", "u1", "Ada  (u1)"},
		{"", "u1", "u1"},
		{"Ada", "", "Ada"},
		{"", "", "(unidentified)"},
	} {
		if got := identify(tc.name, tc.id); got != tc.want {
			t.Errorf("identify(%q,%q) = %q, want %q", tc.name, tc.id, got, tc.want)
		}
	}
}

func TestChangeSummaryCarriesTheID(t *testing.T) {
	m := groupDetail(t, true)
	m = send(t, m, press("d"))
	m = send(t, m, press("y"))
	m = send(t, m, writeDoneMsg{gen: m.gen, summary: "removed Owner One  (u9) as owner"})

	if !strings.Contains(m.flash, "u9") {
		t.Errorf("flash = %q, want the id in the record of what happened", m.flash)
	}
}
