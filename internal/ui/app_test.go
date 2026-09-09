package ui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/idoavrah/entra-tui/internal/auth"
	"github.com/idoavrah/entra-tui/internal/graph"
)

// stubProvider satisfies auth.Provider without performing any sign-in.
type stubProvider struct{}

func (stubProvider) Token(context.Context) (string, error) { return "tok", nil }
func (stubProvider) Identity() auth.Identity {
	return auth.Identity{Account: "ada@contoso.com", TenantID: "tid", Method: auth.MethodBrowser}
}

// newLoginModel returns a sized model sitting on the login screen.
func newLoginModel(t *testing.T) Model {
	t.Helper()
	res, ok := graph.Lookup("users")
	if !ok {
		t.Fatal("users resource missing")
	}
	m := New(context.Background(), Options{
		Auth:     auth.Options{TenantID: "organizations"},
		GraphURL: "http://127.0.0.1:1/v1.0",
		PageSize: 100,
		Resource: res,
	})
	return send(t, m, tea.WindowSizeMsg{Width: 150, Height: 40})
}

// signedIn advances a model past the login screen without touching a network.
func signedIn(t *testing.T, m Model) Model {
	t.Helper()
	m.authAttempt++
	return send(t, m, authDoneMsg{attempt: m.authAttempt, provider: stubProvider{}})
}

// browsing returns a model with the users view open and no request pending.
func browsing(t *testing.T) Model {
	t.Helper()
	m := signedIn(t, newLoginModel(t))
	res, _ := graph.Lookup("users")
	next, _ := m.openResource(res)
	m = next.(Model)
	m.loading = false
	return m
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
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func typeKeys(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		m = send(t, m, press(string(r)))
	}
	return m
}

// loadUsers pushes a page of users in as if Graph had replied.
func loadUsers(t *testing.T, m Model, names ...string) Model {
	t.Helper()
	items := make([]graph.Item, 0, len(names))
	for _, n := range names {
		items = append(items, graph.Item{
			"id": n, "displayName": n,
			"userPrincipalName": strings.ToLower(n) + "@contoso.com",
			"userType":          "Member", "accountEnabled": true,
		})
	}
	return send(t, m, pageMsg{gen: m.gen, page: &graph.Page{Items: items, TotalCount: int64(len(items))}})
}

// ------------------------------------------------------------ login screen

func TestStartsOnLoginAndQueriesNothing(t *testing.T) {
	m := newLoginModel(t)

	if m.screen != screenLogin {
		t.Fatalf("screen = %v, want screenLogin", m.screen)
	}
	if m.client != nil {
		t.Error("a Graph client exists before sign-in")
	}
	if m.coll != nil {
		t.Error("a collection exists before sign-in; nothing should be queried")
	}
	if !strings.Contains(m.View(), "SIGN IN") {
		t.Error("login screen does not render its title")
	}
}

func TestLoginOffersBothMethods(t *testing.T) {
	view := newLoginModel(t).View()
	for _, want := range []string{"browser", "Azure CLI"} {
		if !strings.Contains(view, want) {
			t.Errorf("login screen missing %q", want)
		}
	}
}

func TestLoginCursorMoves(t *testing.T) {
	m := newLoginModel(t)
	m.authCursor = authOptionBrowser

	m = send(t, m, press("down"))
	if m.authCursor != authOptionAzureCLI {
		t.Errorf("cursor = %d, want the Azure CLI option", m.authCursor)
	}
	m = send(t, m, press("up"))
	m = send(t, m, press("up"))
	if m.authCursor != authOptionBrowser {
		t.Errorf("cursor = %d, want it clamped at the first option", m.authCursor)
	}
}

func TestSuccessfulSignInLandsOnDashboard(t *testing.T) {
	m := signedIn(t, newLoginModel(t))

	if m.screen != screenDashboard {
		t.Fatalf("screen = %v, want screenDashboard", m.screen)
	}
	if m.client == nil {
		t.Error("no Graph client after sign-in")
	}
	if m.coll != nil {
		t.Error("a view was loaded automatically; the dashboard should wait for a choice")
	}
	if m.identity.Account != "ada@contoso.com" {
		t.Errorf("identity = %q, want the provider's account", m.identity.Account)
	}
}

func TestStaleSignInResultIsIgnored(t *testing.T) {
	// Cancelling an attempt and starting another must not let the abandoned
	// one land.
	m := newLoginModel(t)
	m.authAttempt = 5
	m = send(t, m, authDoneMsg{attempt: 2, provider: stubProvider{}})

	if m.screen != screenLogin {
		t.Error("an abandoned sign-in attempt was accepted")
	}
}

func TestSignInErrorStaysOnLogin(t *testing.T) {
	m := newLoginModel(t)
	m.authAttempt++
	m = send(t, m, authDoneMsg{attempt: m.authAttempt, err: context.DeadlineExceeded})

	if m.screen != screenLogin {
		t.Errorf("screen = %v, want to stay on login after a failure", m.screen)
	}
	if m.err == nil {
		t.Error("the sign-in error was not recorded")
	}
}

func TestSignInURLIsShownWhenBrowserDoesNotOpen(t *testing.T) {
	m := newLoginModel(t)
	m.authing = true
	m = send(t, m, authURLMsg{url: "https://login.microsoftonline.com/organizations/oauth2/v2.0/authorize?x=1"})

	if !strings.Contains(m.View(), "login.microsoftonline.com") {
		t.Error("the sign-in URL is not offered for manual use")
	}
}

// -------------------------------------------------------------- dashboard

func TestDashboardListsEveryView(t *testing.T) {
	view := signedIn(t, newLoginModel(t)).View()
	for _, want := range []string{"Users", "Groups", "App registrations", "Enterprise apps"} {
		if !strings.Contains(view, want) {
			t.Errorf("dashboard missing %q", want)
		}
	}
}

func TestDashboardDigitOpensView(t *testing.T) {
	m := send(t, signedIn(t, newLoginModel(t)), press("3"))

	if m.screen != screenBrowse {
		t.Fatalf("screen = %v, want screenBrowse", m.screen)
	}
	if m.coll.res.Kind != graph.KindAppRegistrations {
		t.Errorf("view = %s, want app registrations", m.coll.res.Kind)
	}
	if !m.loading {
		t.Error("opening a view did not start a load")
	}
}

func TestEscapeFromBrowseReturnsToDashboard(t *testing.T) {
	m := loadUsers(t, browsing(t), "Ada")
	m = send(t, m, press("esc"))

	if m.screen != screenDashboard {
		t.Errorf("screen = %v, want screenDashboard", m.screen)
	}
}

func TestTildeReturnsToDashboardFromAnywhere(t *testing.T) {
	m := loadUsers(t, browsing(t), "Ada")
	m = send(t, m, press("enter")) // detail
	m = send(t, m, press("~"))

	if m.screen != screenDashboard {
		t.Errorf("screen = %v, want the dashboard", m.screen)
	}
}

func TestReturningToDashboardDropsInFlightPages(t *testing.T) {
	m := browsing(t)
	gen := m.gen
	m = send(t, m, press("esc"))

	m = send(t, m, pageMsg{gen: gen, page: &graph.Page{
		Items: []graph.Item{{"id": "x", "displayName": "Late"}},
	}})
	if m.coll.len() != 0 {
		t.Error("a page that arrived after leaving the view was applied")
	}
}

// ----------------------------------------------------------------- search

func TestSlashRunsAServerSearch(t *testing.T) {
	m := loadUsers(t, browsing(t), "Ada", "Grace")
	m = send(t, m, press("/"))
	if m.mode != modeSearch {
		t.Fatalf("mode = %v, want modeSearch", m.mode)
	}
	m = typeKeys(t, m, "torvalds")
	m = send(t, m, press("enter"))

	if m.coll.search != "torvalds" {
		t.Errorf("search = %q, want the term recorded", m.coll.search)
	}
	if !m.loading {
		t.Error("committing a search did not trigger a requery")
	}
	if m.coll.len() != 0 {
		t.Error("previous rows survived the requery; search must reload from Graph")
	}
}

func TestQuickSearchSlotsKeepTheirDigits(t *testing.T) {
	// The rendered bar must show each term against a stable digit.
	m := browsing(t)
	m.history.record(string(graph.KindUsers), "alpha")
	m.history.record(string(graph.KindUsers), "beta")

	view := m.View()
	if !strings.Contains(view, "[1]") || !strings.Contains(view, "[2]") {
		t.Error("the quick-search bar does not label its slots")
	}
	if !strings.Contains(view, "alpha") || !strings.Contains(view, "beta") {
		t.Error("the quick-search bar does not show both terms")
	}
}

func TestSearchIsRecordedAsAQuickSearch(t *testing.T) {
	m := browsing(t)
	m = send(t, m, press("/"))
	m = typeKeys(t, m, "finance")
	m = send(t, m, press("enter"))

	recent := m.history.list(string(graph.KindUsers))
	if len(recent) != 1 || recent[0] != "finance" {
		t.Fatalf("history = %v, want [finance]", recent)
	}
	if !strings.Contains(m.View(), "finance") {
		t.Error("the quick-search bar does not show the recent search")
	}
}

func TestQuickSearchesAreScopedToTheView(t *testing.T) {
	m := browsing(t)
	m.history.record(string(graph.KindUsers), "ada")
	m.history.record(string(graph.KindGroups), "finance")

	if got := m.history.list(string(graph.KindUsers)); len(got) != 1 || got[0] != "ada" {
		t.Errorf("users history = %v, want [ada]", got)
	}
	if got := m.history.list(string(graph.KindGroups)); len(got) != 1 || got[0] != "finance" {
		t.Errorf("groups history = %v, want [finance]", got)
	}
}

func TestDigitReplaysItsFixedSlot(t *testing.T) {
	// Slots are positional, not most-recently-used: the second term recorded
	// lives in slot 2 and stays there, so "2" always replays it.
	m := browsing(t)
	m.history.record(string(graph.KindUsers), "alpha")
	m.history.record(string(graph.KindUsers), "beta")

	m = send(t, m, press("2"))
	if m.coll.search != "beta" {
		t.Errorf("search = %q, want the term in slot 2", m.coll.search)
	}
	if !m.loading {
		t.Error("replaying a search did not requery")
	}

	// Running a third search must not disturb what "2" means.
	m.loading = false
	m.history.record(string(graph.KindUsers), "gamma")
	m = send(t, m, press("2"))
	if m.coll.search != "beta" {
		t.Errorf("search = %q, want slot 2 unchanged by a later search", m.coll.search)
	}
}

func TestDigitWithNoHistoryIsIgnored(t *testing.T) {
	m := loadUsers(t, browsing(t), "Ada")
	m = send(t, m, press("5"))

	if m.coll.search != "" || m.loading {
		t.Error("an empty quick-search slot triggered a query")
	}
	if m.coll.len() != 1 {
		t.Error("the loaded rows were disturbed")
	}
}

func TestEscapeClearsSearchBeforeLeavingTheView(t *testing.T) {
	m := loadUsers(t, browsing(t), "Ada")
	m.coll.search = "ada"

	m = send(t, m, press("esc"))
	if m.screen != screenBrowse {
		t.Fatalf("screen = %v, want to stay in the view while a search is set", m.screen)
	}
	if m.coll.search != "" {
		t.Errorf("search = %q, want it cleared", m.coll.search)
	}

	m.loading = false
	m = send(t, m, press("esc"))
	if m.screen != screenDashboard {
		t.Errorf("screen = %v, want the dashboard on the second esc", m.screen)
	}
}

func TestRepeatingTheSameSearchDoesNotRequery(t *testing.T) {
	m := browsing(t)
	m.coll.search = "ada"
	m.loading = false

	m = send(t, m, press("/"))
	m = send(t, m, press("enter")) // prompt pre-filled with "ada"
	if m.loading {
		t.Error("an unchanged search term triggered a redundant requery")
	}
}

// ------------------------------------------------------------------ views

func TestColonAliasesSwitchViews(t *testing.T) {
	for alias, want := range map[string]graph.Kind{
		"users":   graph.KindUsers,
		"groups":  graph.KindGroups,
		"appregs": graph.KindAppRegistrations,
		"entapps": graph.KindEnterpriseApps,
	} {
		m := browsing(t)
		m = send(t, m, press(":"))
		m = typeKeys(t, m, alias)
		m = send(t, m, press("enter"))

		if m.coll.res.Kind != want {
			t.Errorf(":%s opened %s, want %s", alias, m.coll.res.Kind, want)
		}
		if m.screen != screenBrowse {
			t.Errorf(":%s left screen = %v, want screenBrowse", alias, m.screen)
		}
	}
}

func TestColonDashboardReturnsHome(t *testing.T) {
	m := browsing(t)
	m = send(t, m, press(":"))
	m = typeKeys(t, m, "dash")
	m = send(t, m, press("enter"))

	if m.screen != screenDashboard {
		t.Errorf("screen = %v, want screenDashboard", m.screen)
	}
}

func TestUnknownCommandFlashes(t *testing.T) {
	m := browsing(t)
	m = send(t, m, press(":"))
	m = typeKeys(t, m, "devices")
	m = send(t, m, press("enter"))

	if !strings.Contains(m.flash, "devices") {
		t.Errorf("flash = %q, want it to name the bad command", m.flash)
	}
}

// ------------------------------------------------------------------- table

func TestRowsAreSortedByName(t *testing.T) {
	m := loadUsers(t, browsing(t), "Zoe", "ada", "Mike")

	var names []string
	for i := 0; i < m.coll.len(); i++ {
		item, _, _ := m.coll.at(i)
		names = append(names, item.String("displayName"))
	}
	want := []string{"ada", "Mike", "Zoe"}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("order = %v, want %v (case-insensitive by name)", names, want)
		}
	}
}

func TestLaterPagesAreMergedInNameOrder(t *testing.T) {
	m := loadUsers(t, browsing(t), "Bob", "Dave")
	m = send(t, m, pageMsg{gen: m.gen, append: true, page: &graph.Page{
		Items: []graph.Item{
			{"id": "c", "displayName": "Carol"},
			{"id": "a", "displayName": "Alice"},
		},
	}})

	var names []string
	for i := 0; i < m.coll.len(); i++ {
		item, _, _ := m.coll.at(i)
		names = append(names, item.String("displayName"))
	}
	want := []string{"Alice", "Bob", "Carol", "Dave"}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("order = %v, want %v", names, want)
		}
	}
}

func TestSelectionSurvivesAResortingPage(t *testing.T) {
	// Appending a page re-sorts the collection; the cursor must follow the
	// object it was on rather than staying at a row index.
	m := loadUsers(t, browsing(t), "Bob", "Dave")
	m = send(t, m, press("down")) // select Dave
	selected, _, _ := m.coll.at(m.cursor)
	if selected.String("displayName") != "Dave" {
		t.Fatalf("selected %q, want Dave", selected.String("displayName"))
	}

	m = send(t, m, pageMsg{gen: m.gen, append: true, page: &graph.Page{
		Items: []graph.Item{{"id": "a", "displayName": "Alice"}},
	}})

	after, _, _ := m.coll.at(m.cursor)
	if after.String("displayName") != "Dave" {
		t.Errorf("selection moved to %q after a re-sort, want Dave", after.String("displayName"))
	}
}

func TestCursorClampsToBounds(t *testing.T) {
	m := loadUsers(t, browsing(t), "Ada", "Bob", "Cal")

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
}

func TestPrefetchTriggersAtTheBottom(t *testing.T) {
	m := loadUsers(t, browsing(t), "Ada", "Bob")
	m.coll.nextLink = "https://graph.example/next"

	m = send(t, m, press("G"))
	if !m.loadingMore {
		t.Error("reaching the last row did not prefetch the next page")
	}
}

func TestLoadAllStopsAtTheSafetyCap(t *testing.T) {
	m := loadUsers(t, browsing(t), "Ada")
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

// ------------------------------------------------------------------ layout

func TestPromptAndQuickSearchesRenderAboveTheTable(t *testing.T) {
	m := loadUsers(t, browsing(t), "Ada")
	m.history.record(string(graph.KindUsers), "finance")

	lines := strings.Split(m.View(), "\n")
	idxOf := func(sub string) int {
		for i, l := range lines {
			if strings.Contains(l, sub) {
				return i
			}
		}
		return -1
	}

	quick := idxOf("[1]")
	header := idxOf("USER PRINCIPAL NAME")
	row := idxOf("Ada")
	if quick < 0 || header < 0 || row < 0 {
		t.Fatalf("missing landmarks: quick=%d header=%d row=%d", quick, header, row)
	}
	if !(quick < header && header < row) {
		t.Errorf("quick searches must sit above the table: quick=%d header=%d row=%d", quick, header, row)
	}
}

func TestWordmarkRendersOnTheRight(t *testing.T) {
	m := browsing(t)
	lines := strings.Split(m.View(), "\n")

	found := false
	for _, l := range lines[:logoHeight] {
		if strings.Contains(l, logo[0]) {
			found = true
			// The logo must sit in the right half of a wide terminal.
			if idx := strings.Index(l, logo[0]); idx < m.width/2 {
				t.Errorf("wordmark starts at column %d, want it right-aligned", idx)
			}
		}
	}
	if !found {
		t.Error("wordmark is missing from the header")
	}
}

func TestWordmarkIsDroppedInNarrowTerminals(t *testing.T) {
	m := send(t, browsing(t), tea.WindowSizeMsg{Width: 70, Height: 30})
	if strings.Contains(m.View(), logo[0]) {
		t.Error("the wordmark is rendered in a terminal too narrow for it")
	}
}

func TestViewSurvivesEverySizeAndScreen(t *testing.T) {
	base := loadUsers(t, browsing(t), "Ada", "Bob")
	for _, size := range []tea.WindowSizeMsg{
		{Width: 20, Height: 12}, {Width: 5, Height: 10}, {Width: 200, Height: 60},
	} {
		m := send(t, base, size)
		for _, s := range []screen{screenLogin, screenDashboard, screenBrowse, screenDetail, screenHelp} {
			m.screen = s
			if got := m.View(); got == "" {
				t.Errorf("View at %dx%d screen %v returned empty", size.Width, size.Height, s)
			}
		}
	}
}

// ------------------------------------------------------------------ detail

func TestDetailShowsSections(t *testing.T) {
	m := loadUsers(t, browsing(t), "Ada")
	m = send(t, m, press("enter"))

	if m.screen != screenDetail {
		t.Fatalf("screen = %v, want screenDetail", m.screen)
	}
	view := m.View()
	if !strings.Contains(view, "ESSENTIALS") {
		t.Error("detail view has no Essentials section")
	}
	if !strings.Contains(view, "Display name") {
		t.Error("detail view does not label its fields")
	}
}

func TestDetailRawTogglesToJSON(t *testing.T) {
	m := loadUsers(t, browsing(t), "Ada")
	m = send(t, m, press("enter"))
	m = send(t, m, press("R"))

	if !strings.Contains(m.View(), "\"displayName\"") {
		t.Error("raw mode does not render JSON")
	}
}

func TestDetailReplyForAnotherObjectIsIgnored(t *testing.T) {
	m := loadUsers(t, browsing(t), "Ada")
	m = send(t, m, press("enter"))
	m.detailLoading = true

	m = send(t, m, detailMsg{gen: m.gen, detail: graph.Detail{
		Kind: graph.KindUsers, Object: graph.Item{"id": "someone-else", "displayName": "Wrong"},
	}})
	if !m.detailLoading {
		t.Error("a reply for a different object was accepted")
	}
}

func TestPairJumpOpensTheCounterpart(t *testing.T) {
	m := browsing(t)
	res, _ := graph.Lookup("appregs")
	next, _ := m.openResource(res)
	m = next.(Model)
	m.loading = false
	m = send(t, m, pageMsg{gen: m.gen, page: &graph.Page{Items: []graph.Item{
		{"id": "app1", "displayName": "Contoso", "appId": "aaaa"},
	}}})
	m = send(t, m, press("enter"))

	m = send(t, m, detailMsg{gen: m.gen, detail: graph.Detail{
		Kind:   graph.KindAppRegistrations,
		Object: graph.Item{"id": "app1", "displayName": "Contoso", "appId": "aaaa"},
		Counterpart: &graph.Counterpart{
			Kind: graph.KindEnterpriseApps, ID: "sp1", DisplayName: "Contoso",
		},
	}})

	m = send(t, m, press("x"))
	if m.coll.res.Kind != graph.KindEnterpriseApps {
		t.Errorf("view = %s, want the enterprise apps view", m.coll.res.Kind)
	}
	if m.detailID != "sp1" {
		t.Errorf("detailID = %q, want the paired service principal", m.detailID)
	}
	if m.screen != screenDetail {
		t.Errorf("screen = %v, want to land in the paired detail view", m.screen)
	}
}

func TestPairJumpExplainsAMissingCounterpart(t *testing.T) {
	m := browsing(t)
	m.screen = screenDetail
	m.detail = graph.Detail{Kind: graph.KindEnterpriseApps, Object: graph.Item{"id": "sp1"}}

	m = send(t, m, pairMsg{gen: m.gen, missing: "no app registration in this tenant"})
	if !strings.Contains(m.flash, "no app registration") {
		t.Errorf("flash = %q, want an explanation", m.flash)
	}
}

func TestPairKeyOnAUserViewSaysSo(t *testing.T) {
	m := loadUsers(t, browsing(t), "Ada")
	m = send(t, m, press("enter"))
	m = send(t, m, press("x"))

	if !strings.Contains(m.flash, "no paired object") {
		t.Errorf("flash = %q, want it to explain there is no pairing", m.flash)
	}
}

// ------------------------------------------------------------------- misc

func TestYankEmitsClipboardSequence(t *testing.T) {
	m := loadUsers(t, browsing(t), "Ada")
	m = send(t, m, press("y"))

	if m.pendingClipboard != "Ada" {
		t.Errorf("pendingClipboard = %q, want the object id", m.pendingClipboard)
	}
	if !strings.Contains(m.View(), "\x1b]52;c;") {
		t.Error("View does not carry the OSC 52 clipboard sequence")
	}
	m = send(t, m, clipboardSentMsg{})
	if strings.Contains(m.View(), "\x1b]52;c;") {
		t.Error("clipboard sequence still emitted after it was sent")
	}
}

func TestPromptKeysDoNotTriggerShortcuts(t *testing.T) {
	m := browsing(t)
	m = send(t, m, press("/"))
	m = typeKeys(t, m, "q:x")

	if m.quitting {
		t.Error("typing q in a prompt quit the app")
	}
	if m.mode != modeSearch {
		t.Errorf("mode = %v, want the search prompt to stay open", m.mode)
	}
	if m.input.Value() != "q:x" {
		t.Errorf("input = %q, want the literal keystrokes", m.input.Value())
	}
}

func TestHelpReturnsToTheScreenItWasOpenedFrom(t *testing.T) {
	m := signedIn(t, newLoginModel(t))
	m = send(t, m, press("?"))
	if m.screen != screenHelp {
		t.Fatalf("screen = %v, want screenHelp", m.screen)
	}
	m = send(t, m, press("x"))
	if m.screen != screenDashboard {
		t.Errorf("screen = %v, want to return to the dashboard", m.screen)
	}
}

func TestErrorRendersWithItsHint(t *testing.T) {
	m := browsing(t)
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

func TestFlashOnlyClearedByItsOwnTimer(t *testing.T) {
	m := loadUsers(t, browsing(t), "Ada")
	m = send(t, m, press("n"))
	first := m.flashSeq
	m = send(t, m, press("n"))

	m = send(t, m, flashExpiredMsg{seq: first})
	if m.flash == "" {
		t.Error("an older flash timer cleared the current message")
	}
	m = send(t, m, flashExpiredMsg{seq: m.flashSeq})
	if m.flash != "" {
		t.Error("the matching timer did not clear the message")
	}
}

// ------------------------------------------------------- layout invariants

func TestPromptRowAppearsOnlyWhileAPromptIsOpen(t *testing.T) {
	m := loadUsers(t, browsing(t), "Ada")

	idle := strings.Split(m.View(), "\n")
	open := strings.Split(send(t, m, press("/")).View(), "\n")

	// The screen always fills the terminal; the prompt displaces a content
	// row rather than growing the layout.
	if len(open) != len(idle) {
		t.Errorf("view is %d lines with a prompt open and %d idle, want a constant height",
			len(open), len(idle))
	}
	// Idle: the frame starts straight after the header, with no reserved row.
	if !strings.HasPrefix(idle[headerHeight], boxTopLeft) {
		t.Errorf("idle line %q should be the frame, not a reserved prompt row", idle[headerHeight])
	}
	// Open: the prompt takes that row and the frame moves down one.
	if !strings.HasPrefix(strings.TrimSpace(open[headerHeight]), "/") {
		t.Errorf("line %q is not the open search prompt", open[headerHeight])
	}
	if !strings.HasPrefix(open[headerHeight+1], boxTopLeft) {
		t.Errorf("line %q should be the frame under an open prompt", open[headerHeight+1])
	}
}

func TestHeaderHeightIsFixedAcrossScreens(t *testing.T) {
	base := loadUsers(t, browsing(t), "Ada")
	for _, s := range []screen{screenDashboard, screenBrowse, screenDetail, screenHelp} {
		m := base
		m.screen = s
		lines := strings.Split(m.View(), "\n")
		// The frame's top border always sits immediately after the header.
		if !strings.HasPrefix(lines[headerHeight], boxTopLeft) {
			t.Errorf("screen %v: line %d = %q, want the frame's top border", s, headerHeight, lines[headerHeight])
		}
	}
}

func TestContentIsFramedAndTheFrameCarriesTheTitle(t *testing.T) {
	m := loadUsers(t, browsing(t), "Ada")
	lines := strings.Split(m.View(), "\n")

	top := lines[headerHeight]
	if !strings.Contains(top, "Users") {
		t.Errorf("top border %q does not carry the screen title", top)
	}
	if !strings.Contains(top, "50") && !strings.Contains(top, "1") {
		t.Errorf("top border %q does not carry the row count", top)
	}
	// Data rows sit inside vertical borders.
	row := lines[headerHeight+2]
	if !strings.HasPrefix(row, boxVertical) || !strings.HasSuffix(row, boxVertical) {
		t.Errorf("content row %q is not framed", row)
	}
}

func TestEveryRenderedLineFitsTheTerminal(t *testing.T) {
	base := loadUsers(t, browsing(t), "Ada", "Bob")
	for _, size := range []tea.WindowSizeMsg{
		{Width: 80, Height: 24}, {Width: 120, Height: 40}, {Width: 200, Height: 60},
	} {
		m := send(t, base, size)
		for _, s := range []screen{screenLogin, screenDashboard, screenBrowse, screenDetail, screenHelp} {
			m.screen = s
			for i, line := range strings.Split(m.View(), "\n") {
				if w := lipgloss.Width(line); w > size.Width {
					t.Errorf("screen %v at %dx%d: line %d is %d cells, over the %d-cell terminal",
						s, size.Width, size.Height, i, w, size.Width)
				}
			}
		}
	}
}

func TestFooterStaysTwoLinesAtTheBottom(t *testing.T) {
	m := loadUsers(t, browsing(t), "Ada")
	lines := strings.Split(m.View(), "\n")

	// Last line is the hint bar; the one before it is the status line.
	if !strings.Contains(lines[len(lines)-1], "help") {
		t.Errorf("last line = %q, want the key hints", lines[len(lines)-1])
	}
	if !strings.HasPrefix(lines[len(lines)-3], boxBottomLeft) {
		t.Errorf("line %q should be the frame's bottom border", lines[len(lines)-3])
	}
}

func TestUserTableShowsTheRequestedColumns(t *testing.T) {
	m := loadUsers(t, browsing(t), "Ada")
	view := m.View()

	for _, want := range []string{"NAME", "USER PRINCIPAL NAME", "TYPE", "ENABLED", "DEPARTMENT"} {
		if !strings.Contains(view, want) {
			t.Errorf("users table is missing the %s column", want)
		}
	}
	for _, unwanted := range []string{"JOB TITLE", "AGE"} {
		if strings.Contains(view, unwanted) {
			t.Errorf("users table still shows the %s column", unwanted)
		}
	}
}
