package ui

import (
	"context"
	"fmt"
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

// newLoginModel returns a sized model sitting on the login screen, mid
// unattended Azure CLI attempt.
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

// pickerModel returns a login screen after the unattended Azure CLI attempt
// has failed, which is when the method picker is shown.
func pickerModel(t *testing.T) Model {
	t.Helper()
	m := newLoginModel(t)
	return send(t, m, authDoneMsg{attempt: m.authAttempt,
		err: fmt.Errorf("%w: not signed in to the Azure CLI", auth.ErrNoAzureCLI)})
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
	view := pickerModel(t).View()
	for _, want := range []string{"browser", "Azure CLI"} {
		if !strings.Contains(view, want) {
			t.Errorf("login screen missing %q", want)
		}
	}
}

func TestLoginCursorMoves(t *testing.T) {
	m := pickerModel(t)
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
	m := pickerModel(t)
	m.authAttempt++
	m = send(t, m, authDoneMsg{attempt: m.authAttempt, err: context.DeadlineExceeded})

	if m.screen != screenLogin {
		t.Errorf("screen = %v, want to stay on login after a failure", m.screen)
	}
	if m.err == nil {
		t.Error("the sign-in error was not recorded")
	}
}

func TestStartupTriesTheAzureCLIWithoutAsking(t *testing.T) {
	// An existing az session is enough to get going; making the user pick it
	// is asking them to confirm something they already decided.
	m := newLoginModel(t)
	if !m.autoSignIn || !m.authing {
		t.Fatalf("autoSignIn=%v authing=%v, want an unattended attempt in flight",
			m.autoSignIn, m.authing)
	}
	if !strings.Contains(m.View(), "signing in") {
		t.Error("the login screen does not show the attempt in progress")
	}
}

func TestUnattendedFailureFallsBackToThePickerQuietly(t *testing.T) {
	// "Not signed in to az" is the ordinary reason the attempt fails, and
	// falling back is the whole point -- so it is not reported as an error.
	m := pickerModel(t)

	if m.authing || m.autoSignIn {
		t.Error("the attempt is still marked in flight")
	}
	if m.err != nil {
		t.Errorf("err = %v, want a missing az session handled silently", m.err)
	}
	if !strings.Contains(m.View(), "Sign in with your browser") {
		t.Error("the method picker was not shown")
	}
}

func TestUnattendedRealFailureIsReported(t *testing.T) {
	// Anything other than a missing session is worth showing.
	m := newLoginModel(t)
	m = send(t, m, authDoneMsg{attempt: m.authAttempt, err: context.DeadlineExceeded})

	if m.err == nil {
		t.Error("a genuine sign-in failure was swallowed")
	}
}

func TestBrowserMethodSkipsTheUnattendedAttempt(t *testing.T) {
	res, _ := graph.Lookup("users")
	m := New(context.Background(), Options{
		Auth:     auth.Options{TenantID: "organizations", Method: auth.MethodBrowser},
		GraphURL: "http://127.0.0.1:1/v1.0", PageSize: 100, Resource: res,
	})
	if m.autoSignIn || m.authing {
		t.Error("-auth browser should go straight to the picker")
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
	for _, want := range []string{"Users", "Groups", "App registrations", "Enterprise apps", "Devices"} {
		if !strings.Contains(view, want) {
			t.Errorf("dashboard missing %q", want)
		}
	}
}

func TestDashboardShowsDirectoryTotals(t *testing.T) {
	m := signedIn(t, newLoginModel(t))
	m = send(t, m, countMsg{kind: graph.KindUsers, total: 1707600})

	if got := m.totals[graph.KindUsers]; got != 1707600 {
		t.Fatalf("total = %d, want it recorded", got)
	}
	// Rendered as large numerals, so the digits appear as glyph rows rather
	// than literal text; check the tile carries the count's shape.
	view := m.View()
	if !strings.Contains(view, bigNumber("1,707,600")[0]) {
		t.Error("the users tile does not show its total")
	}
}

func TestDashboardTotalFailureShowsAPlaceholder(t *testing.T) {
	m := signedIn(t, newLoginModel(t))
	m = send(t, m, countMsg{kind: graph.KindDevices, err: &graph.APIError{Status: 403}})

	if got := m.totalText(graph.KindDevices); got != "—" {
		t.Errorf("totalText = %q, want a placeholder", got)
	}
	if !strings.Contains(m.View(), "some totals unavailable") {
		t.Error("the dashboard does not say a total could not be read")
	}
}

func TestDashboardCursorMovesInTwoDimensions(t *testing.T) {
	m := signedIn(t, newLoginModel(t))
	columns := m.dashboardColumns()

	m = send(t, m, press("right"))
	if m.dashCursor != 1 {
		t.Errorf("cursor = %d, want 1 after moving right", m.dashCursor)
	}
	m = send(t, m, press("down"))
	if m.dashCursor != clamp(1+columns, 0, len(graph.All())-1) {
		t.Errorf("cursor = %d, want a row down", m.dashCursor)
	}
	m = send(t, m, press("left"))
	m = send(t, m, press("up"))
	if m.dashCursor != 0 {
		t.Errorf("cursor = %d, want to be back at the first tile", m.dashCursor)
	}
}

func TestDashboardDoesNotShowRecentSearches(t *testing.T) {
	m := signedIn(t, newLoginModel(t))
	m.history.record(string(graph.KindUsers), "finance")

	if strings.Contains(m.View(), "finance") {
		t.Error("the dashboard still lists recent searches")
	}
}

func TestDevicesViewIsReachable(t *testing.T) {
	m := send(t, signedIn(t, newLoginModel(t)), press("5"))

	if m.coll.res.Kind != graph.KindDevices {
		t.Fatalf("view = %s, want devices", m.coll.res.Kind)
	}
	m.loading = false
	m = send(t, m, pageMsg{gen: m.gen, page: &graph.Page{Items: []graph.Item{{
		"id": "d1", "displayName": "LAPTOP-01", "operatingSystem": "Windows",
		"operatingSystemVersion": "10.0.22631", "trustType": "AzureAd",
		"isCompliant": true, "isManaged": true, "accountEnabled": true,
	}}}})

	view := m.View()
	for _, want := range []string{"LAPTOP-01", "Windows", "OS", "JOIN TYPE", "COMPLIANT"} {
		if !strings.Contains(view, want) {
			t.Errorf("devices table missing %q", want)
		}
	}
	if !strings.Contains(view, "Microsoft Entra joined") {
		t.Error("trustType was not rendered as a readable join type")
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

func TestOpeningSearchStartsFromAnEmptyPattern(t *testing.T) {
	// The common case is looking for something new; the previous term is one
	// keystroke away in its quick-search slot.
	m := browsing(t)
	m.coll.search = "ada"
	m.loading = false

	m = send(t, m, press("/"))
	if m.input.Value() != "" {
		t.Errorf("prompt pre-filled with %q, want it cleared", m.input.Value())
	}

	// Committing the empty prompt therefore clears the search.
	m = send(t, m, press("enter"))
	if m.coll.search != "" {
		t.Errorf("search = %q, want it cleared", m.coll.search)
	}
	if !m.loading {
		t.Error("clearing the search did not requery the unfiltered view")
	}
}

func TestReplayingTheActiveSlotDoesNotRequery(t *testing.T) {
	m := browsing(t)
	m.history.record(string(graph.KindUsers), "ada")
	m.coll.search = "ada"
	m.loading = false

	m = send(t, m, press("1"))
	if m.loading {
		t.Error("replaying the term already in force triggered a redundant requery")
	}
}

// ------------------------------------------------------------------ views

func TestColonAliasesSwitchViews(t *testing.T) {
	for alias, want := range map[string]graph.Kind{
		"users":   graph.KindUsers,
		"groups":  graph.KindGroups,
		"appregs": graph.KindAppRegistrations,
		"entapps": graph.KindEnterpriseApps,
		"devices": graph.KindDevices,
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
	m = typeKeys(t, m, "printers")
	m = send(t, m, press("enter"))

	if !strings.Contains(m.flash, "printers") {
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

func TestScrollingIsTheOnlyWayToLoadMorePages(t *testing.T) {
	// There is no paging key: reaching the end of the loaded rows fetches
	// the next page, and there is no way to pull the whole tenant at once.
	m := loadUsers(t, browsing(t), "Ada", "Bob")
	m.coll.nextLink = "https://graph.example/next"

	for _, k := range []string{"n", "A"} {
		got := send(t, m, press(k))
		if got.loadingMore {
			t.Errorf("%q triggered a fetch; paging keys should be gone", k)
		}
	}

	view := m.View()
	for _, gone := range []string{"n/A", "load all"} {
		if strings.Contains(view, gone) {
			t.Errorf("the view still advertises %q", gone)
		}
	}
	if !strings.Contains(view, "more below") {
		t.Error("the frame does not report that more rows exist")
	}

	// Scrolling to the end still fetches.
	if !send(t, m, press("G")).loadingMore {
		t.Error("reaching the last row did not fetch the next page")
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

func TestCopyEmitsClipboardSequence(t *testing.T) {
	m := loadUsers(t, browsing(t), "Ada")
	m = send(t, m, press("c"))

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

func TestYankStillWorksForViFingers(t *testing.T) {
	// "c" is what a newcomer guesses; "y" is what anyone coming from vi or
	// k9s will press. Both are bound.
	m := loadUsers(t, browsing(t), "Ada")
	if got := send(t, m, press("y")); got.pendingClipboard != "Ada" {
		t.Errorf("pendingClipboard = %q, want y to copy as well", got.pendingClipboard)
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
	m = send(t, m, press("enter")) // detail
	m = send(t, m, press("x"))     // flashes: users have no paired object
	first := m.flashSeq
	m = send(t, m, press("x"))

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

func TestUnattendedAttemptIsNumberedBeforeInit(t *testing.T) {
	// Init takes the model by value, so it cannot number the attempt itself:
	// the increment would be discarded and the reply dropped as stale. This
	// is the bug that left the login screen spinning forever.
	m := newLoginModel(t)
	if m.authAttempt == 0 {
		t.Fatal("the unattended attempt has no number, so its result will be ignored")
	}

	// The number New assigned must be the one the reply is accepted under.
	m.Init()
	done := send(t, m, authDoneMsg{attempt: m.authAttempt, provider: stubProvider{}})
	if done.screen != screenDashboard {
		t.Errorf("screen = %v, want the sign-in to have been accepted", done.screen)
	}
}
