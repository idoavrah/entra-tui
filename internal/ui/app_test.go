package ui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/idoavrah/entra-tui/internal/auth"
	"github.com/idoavrah/entra-tui/internal/graph"
)

// stubProvider satisfies auth.Provider without performing any sign-in.
type stubProvider struct{}

func (stubProvider) Token(context.Context) (string, error) { return "tok", nil }
func (stubProvider) Identity() auth.Identity {
	return auth.Identity{Account: "ada@contoso.com", TenantID: "tid", Method: auth.MethodBrowser}
}

// newTestModel builds a model sized for a terminal, with no request in
// flight. Commands returned by Update are deliberately not executed, so no
// test here touches the network.
func newTestModel(t *testing.T) Model {
	t.Helper()
	res, ok := graph.Lookup("users")
	if !ok {
		t.Fatal("users resource missing")
	}
	client := graph.New(stubProvider{}, graph.WithBaseURL("http://127.0.0.1:1/v1.0"))
	m := New(context.Background(), client, stubProvider{}.Identity(), res, 100)
	m.loading = false
	return send(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
}

func send(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	next, _ := m.Update(msg)
	out, ok := next.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want ui.Model", next)
	}
	return out
}

// press builds a KeyMsg for a literal keystroke.
func press(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

// typeKeys feeds a string one keystroke at a time.
func typeKeys(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		m = send(t, m, press(string(r)))
	}
	return m
}

// loadUsers pushes a page of users into the model as if Graph had replied.
func loadUsers(t *testing.T, m Model, names ...string) Model {
	t.Helper()
	items := make([]graph.Item, 0, len(names))
	for _, n := range names {
		items = append(items, graph.Item{
			"id": n, "displayName": n, "userPrincipalName": strings.ToLower(n) + "@contoso.com",
			"userType": "Member", "accountEnabled": true,
		})
	}
	return send(t, m, pageMsg{
		gen:  m.gen,
		page: &graph.Page{Items: items, TotalCount: int64(len(items))},
	})
}

func TestPageLoadPopulatesTable(t *testing.T) {
	m := loadUsers(t, newTestModel(t), "Ada", "Grace", "Alan")

	if m.coll.len() != 3 {
		t.Fatalf("len = %d, want 3", m.coll.len())
	}
	if m.loading {
		t.Error("loading is still set after a page arrived")
	}
	if !strings.Contains(m.View(), "Ada") {
		t.Error("View does not show the loaded rows")
	}
}

func TestStalePageIsDropped(t *testing.T) {
	// A reply for a resource the user has already navigated away from must
	// not land in the new table.
	m := loadUsers(t, newTestModel(t), "Ada")
	m = send(t, m, press(":"))
	m = typeKeys(t, m, "groups")
	m = send(t, m, press("enter"))

	if m.coll.res.Kind != graph.KindGroups {
		t.Fatalf("resource = %s, want groups", m.coll.res.Kind)
	}

	stale := send(t, m, pageMsg{gen: 0, page: &graph.Page{
		Items: []graph.Item{{"id": "x", "displayName": "Leftover"}},
	}})
	if stale.coll.len() != 0 {
		t.Errorf("len = %d, want the stale page ignored", stale.coll.len())
	}
}

func TestStaleErrorIsDropped(t *testing.T) {
	m := newTestModel(t)
	m.gen = 5
	m = send(t, m, errMsg{gen: 1, err: context.Canceled})
	if m.err != nil {
		t.Errorf("err = %v, want the stale error ignored", m.err)
	}
}

func TestCommandPromptSwitchesResource(t *testing.T) {
	m := newTestModel(t)
	m = send(t, m, press(":"))
	if m.mode != modeCommand {
		t.Fatalf("mode = %v, want modeCommand", m.mode)
	}
	m = typeKeys(t, m, "sp")
	m = send(t, m, press("enter"))

	if m.mode != modeNormal {
		t.Errorf("mode = %v, want modeNormal after commit", m.mode)
	}
	if m.coll.res.Kind != graph.KindEnterpriseApps {
		t.Errorf("resource = %s, want servicePrincipals", m.coll.res.Kind)
	}
}

func TestUnknownCommandFlashesInsteadOfSwitching(t *testing.T) {
	m := newTestModel(t)
	m = send(t, m, press(":"))
	m = typeKeys(t, m, "devices")
	m = send(t, m, press("enter"))

	if m.coll.res.Kind != graph.KindUsers {
		t.Errorf("resource = %s, want users unchanged", m.coll.res.Kind)
	}
	if !strings.Contains(m.flash, "devices") {
		t.Errorf("flash = %q, want it to name the bad command", m.flash)
	}
}

func TestDigitKeysJumpBetweenViews(t *testing.T) {
	m := newTestModel(t)
	m = send(t, m, press("2"))
	if m.coll.res.Kind != graph.KindGroups {
		t.Errorf("after '2' resource = %s, want groups", m.coll.res.Kind)
	}
	// Out-of-range digits must be ignored, not panic.
	m = send(t, m, press("9"))
	if m.coll.res.Kind != graph.KindGroups {
		t.Errorf("after '9' resource = %s, want groups unchanged", m.coll.res.Kind)
	}
}

func TestFilterNarrowsLiveWhileTyping(t *testing.T) {
	m := loadUsers(t, newTestModel(t), "Ada", "Grace", "Alan")
	m = send(t, m, press("/"))
	m = typeKeys(t, m, "a")

	// "Ada", "Grace" and "Alan" all contain an "a" somewhere.
	if m.coll.len() == 0 {
		t.Fatal("filter hid every row")
	}
	m = typeKeys(t, m, "l")
	if m.coll.len() != 1 {
		t.Fatalf("len = %d, want only Alan to match \"al\"", m.coll.len())
	}
	m = send(t, m, press("enter"))
	if m.mode != modeNormal || m.coll.filter != "al" {
		t.Errorf("mode=%v filter=%q, want the filter committed", m.mode, m.coll.filter)
	}
}

func TestCancellingFilterRestoresThePreviousOne(t *testing.T) {
	m := loadUsers(t, newTestModel(t), "Ada", "Grace", "Alan")
	m = send(t, m, press("/"))
	m = typeKeys(t, m, "ada")
	m = send(t, m, press("enter"))

	// Reopen, type something else, then abandon it.
	m = send(t, m, press("/"))
	m = typeKeys(t, m, "zzz")
	if m.coll.len() != 0 {
		t.Fatalf("len = %d, want the live filter applied", m.coll.len())
	}
	m = send(t, m, press("esc"))

	if m.coll.filter != "ada" {
		t.Errorf("filter = %q, want the committed filter restored", m.coll.filter)
	}
	if m.coll.len() != 1 {
		t.Errorf("len = %d, want the restored filter's single match", m.coll.len())
	}
}

func TestEscapePeelsFilterThenSearch(t *testing.T) {
	m := loadUsers(t, newTestModel(t), "Ada", "Grace")
	m.coll.search = "ada"
	m = send(t, m, press("/"))
	m = typeKeys(t, m, "ada")
	m = send(t, m, press("enter"))

	m = send(t, m, press("esc"))
	if m.coll.filter != "" {
		t.Errorf("filter = %q, want it cleared first", m.coll.filter)
	}
	if m.coll.search != "ada" {
		t.Errorf("search = %q, want it still set after one esc", m.coll.search)
	}

	m = send(t, m, press("esc"))
	if m.coll.search != "" {
		t.Errorf("search = %q, want it cleared by the second esc", m.coll.search)
	}
}

func TestCursorMovementClampsToBounds(t *testing.T) {
	m := loadUsers(t, newTestModel(t), "Ada", "Grace", "Alan")

	m = send(t, m, press("up"))
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want it clamped at the top", m.cursor)
	}
	for range 10 {
		m = send(t, m, press("down"))
	}
	if m.cursor != 2 {
		t.Errorf("cursor = %d, want it clamped at the last row", m.cursor)
	}
	m = send(t, m, press("g"))
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want g to jump to the top", m.cursor)
	}
	m = send(t, m, press("G"))
	if m.cursor != 2 {
		t.Errorf("cursor = %d, want G to jump to the bottom", m.cursor)
	}
}

func TestCursorMovementOnEmptyTableIsSafe(t *testing.T) {
	m := newTestModel(t)
	m = send(t, m, press("down"))
	m = send(t, m, press("G"))
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0 on an empty table", m.cursor)
	}
	if strings.Contains(m.View(), "panic") {
		t.Error("View reported a panic")
	}
}

func TestFilterSuppressesAutomaticPrefetch(t *testing.T) {
	// With a filter active, nearing the bottom says nothing about how much of
	// the tenant is loaded; auto-fetching would silently walk the directory.
	m := loadUsers(t, newTestModel(t), "Ada", "Grace")
	m.coll.nextLink = "https://graph.example/next"
	m.coll.setFilter("ada")

	m = send(t, m, press("G"))
	if m.loadingMore {
		t.Error("a filtered view triggered an automatic page fetch")
	}
}

func TestPrefetchTriggersNearTheEndWhenUnfiltered(t *testing.T) {
	m := loadUsers(t, newTestModel(t), "Ada", "Grace")
	m.coll.nextLink = "https://graph.example/next"

	m = send(t, m, press("G"))
	if !m.loadingMore {
		t.Error("reaching the last row did not trigger a prefetch")
	}
}

func TestNextPageFlashesWhenEverythingIsLoaded(t *testing.T) {
	m := loadUsers(t, newTestModel(t), "Ada")
	m = send(t, m, press("n"))

	if m.loadingMore {
		t.Error("requested another page with no nextLink")
	}
	if !strings.Contains(m.flash, "loaded") {
		t.Errorf("flash = %q, want it to say everything is loaded", m.flash)
	}
}

func TestLoadAllStopsAtTheSafetyCap(t *testing.T) {
	m := loadUsers(t, newTestModel(t), "Ada")
	m.coll.nextLink = "https://graph.example/next"
	m.loadAll = true
	m.autoPages = maxAutoPages

	m = send(t, m, pageMsg{gen: m.gen, append: true, page: &graph.Page{
		Items:    []graph.Item{{"id": "b", "displayName": "Bob"}},
		NextLink: "https://graph.example/next2",
	}})

	if m.loadAll {
		t.Error("loadAll is still set past the cap")
	}
	if !strings.Contains(m.flash, "stopped after") {
		t.Errorf("flash = %q, want the cap explained", m.flash)
	}
}

func TestAppendPageKeepsExistingRows(t *testing.T) {
	m := loadUsers(t, newTestModel(t), "Ada")
	m = send(t, m, pageMsg{gen: m.gen, append: true, page: &graph.Page{
		Items: []graph.Item{{"id": "b", "displayName": "Bob"}},
	}})
	if m.coll.loaded() != 2 {
		t.Errorf("loaded = %d, want the appended page added to the first", m.coll.loaded())
	}
}

func TestFirstPageReplacesExistingRows(t *testing.T) {
	m := loadUsers(t, newTestModel(t), "Ada", "Grace")
	m = send(t, m, pageMsg{gen: m.gen, page: &graph.Page{
		Items: []graph.Item{{"id": "b", "displayName": "Bob"}},
	}})
	if m.coll.loaded() != 1 {
		t.Errorf("loaded = %d, want the reload to replace, not append", m.coll.loaded())
	}
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want it reset on reload", m.cursor)
	}
}

func TestDetailViewOpensAndReturns(t *testing.T) {
	m := loadUsers(t, newTestModel(t), "Ada")
	m = send(t, m, press("enter"))

	if m.view != viewDetail {
		t.Fatalf("view = %v, want viewDetail", m.view)
	}
	if !strings.Contains(m.View(), "Ada") {
		t.Error("detail view does not name the object")
	}

	m = send(t, m, press("R"))
	if !m.detailRaw {
		t.Error("R did not toggle raw json")
	}
	if !strings.Contains(m.View(), "\"displayName\"") {
		t.Error("raw view does not render JSON")
	}

	m = send(t, m, press("esc"))
	if m.view != viewBrowse {
		t.Errorf("view = %v, want viewBrowse after esc", m.view)
	}
}

func TestDetailMsgForAnotherObjectIsIgnored(t *testing.T) {
	m := loadUsers(t, newTestModel(t), "Ada")
	m = send(t, m, press("enter"))
	m.detailLoading = true

	m = send(t, m, detailMsg{gen: m.gen, id: "someone-else", item: graph.Item{"displayName": "Wrong"}})
	if !m.detailLoading {
		t.Error("a reply for a different object was accepted")
	}
	if m.detailItem.String("displayName") == "Wrong" {
		t.Error("detail pane shows another object's data")
	}
}

func TestDetailMsgEnrichesTheSelectedObject(t *testing.T) {
	m := loadUsers(t, newTestModel(t), "Ada")
	m = send(t, m, press("enter"))

	m = send(t, m, detailMsg{gen: m.gen, id: "Ada", item: graph.Item{
		"id": "Ada", "displayName": "Ada", "officeLocation": "Building 7",
	}})
	if m.detailLoading {
		t.Error("detailLoading still set after the object arrived")
	}
	if !strings.Contains(m.View(), "Building 7") {
		t.Error("detail view does not show the enriched properties")
	}
}

func TestYankSetsClipboardPayloadInTheFrame(t *testing.T) {
	m := loadUsers(t, newTestModel(t), "Ada")
	m = send(t, m, press("y"))

	if m.pendingClipboard != "Ada" {
		t.Errorf("pendingClipboard = %q, want the object id", m.pendingClipboard)
	}
	if !strings.Contains(m.View(), "\x1b]52;c;") {
		t.Error("View does not carry the OSC 52 clipboard sequence")
	}

	m = send(t, m, clipboardSentMsg{})
	if m.pendingClipboard != "" {
		t.Error("clipboard payload was not retired")
	}
	if strings.Contains(m.View(), "\x1b]52;c;") {
		t.Error("View still emits the clipboard sequence after it was sent")
	}
}

func TestHelpOverlayToggles(t *testing.T) {
	m := newTestModel(t)
	m = send(t, m, press("?"))
	if m.view != viewHelp {
		t.Fatalf("view = %v, want viewHelp", m.view)
	}
	if !strings.Contains(m.View(), "NAVIGATION") {
		t.Error("help view is missing its key reference")
	}
	m = send(t, m, press("x"))
	if m.view != viewBrowse {
		t.Errorf("view = %v, want any key to dismiss help", m.view)
	}
}

func TestPromptKeysDoNotTriggerShortcuts(t *testing.T) {
	// Typing "q" into a prompt must not quit, and ":" must not re-open the
	// command bar.
	m := newTestModel(t)
	m = send(t, m, press("/"))
	m = typeKeys(t, m, "q:s")

	if m.quitting {
		t.Error("typing q in a prompt quit the app")
	}
	if m.mode != modeFilter {
		t.Errorf("mode = %v, want the filter prompt to stay open", m.mode)
	}
	if m.input.Value() != "q:s" {
		t.Errorf("input = %q, want the literal keystrokes", m.input.Value())
	}
}

func TestErrorIsRenderedWithItsHint(t *testing.T) {
	m := newTestModel(t)
	m = send(t, m, errMsg{gen: m.gen, err: &graph.APIError{
		Status: 403, Code: "Authorization_RequestDenied", Message: "Insufficient privileges",
	}})

	view := m.View()
	if !strings.Contains(view, "Insufficient privileges") {
		t.Error("View does not show the Graph error message")
	}
	if !strings.Contains(view, "consent") {
		t.Error("View does not show the actionable hint for a 403")
	}
}

func TestViewSurvivesTinyTerminals(t *testing.T) {
	m := loadUsers(t, newTestModel(t), "Ada", "Grace")
	for _, size := range []tea.WindowSizeMsg{
		{Width: 20, Height: 10},
		{Width: 5, Height: 8},
		{Width: 200, Height: 60},
	} {
		m = send(t, m, size)
		for _, v := range []viewState{viewBrowse, viewDetail, viewHelp} {
			m.view = v
			if got := m.View(); got == "" {
				t.Errorf("View at %dx%d state %v returned empty", size.Width, size.Height, v)
			}
		}
		m.view = viewBrowse
	}
}

func TestFlashOnlyClearedByItsOwnTimer(t *testing.T) {
	m := newTestModel(t)
	m = send(t, m, press("n")) // sets a flash, seq 1
	first := m.flashSeq
	m = send(t, m, press("n")) // replaces it, seq 2

	// The first timer firing late must not wipe the newer message.
	m = send(t, m, flashExpiredMsg{seq: first})
	if m.flash == "" {
		t.Error("an older flash timer cleared the current message")
	}
	m = send(t, m, flashExpiredMsg{seq: m.flashSeq})
	if m.flash != "" {
		t.Error("the matching timer did not clear the message")
	}
}

func TestSwitchingToTheSameResourceIsANoop(t *testing.T) {
	m := loadUsers(t, newTestModel(t), "Ada")
	before := m.gen
	m = send(t, m, press("1"))

	if m.gen != before {
		t.Error("re-selecting the current view discarded loaded data")
	}
	if m.coll.len() != 1 {
		t.Errorf("len = %d, want the rows kept", m.coll.len())
	}
}

func TestQuitKeySetsQuitting(t *testing.T) {
	m := send(t, newTestModel(t), press("q"))
	if !m.quitting {
		t.Error("q did not set quitting")
	}
	if m.View() != "" {
		t.Error("View still renders while quitting")
	}
}
