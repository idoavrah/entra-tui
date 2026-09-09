// Package ui implements the entra-tui terminal interface: a k9s-style
// browser over the read-only Microsoft Graph directory collections.
package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/idoavrah/entra-tui/internal/auth"
	"github.com/idoavrah/entra-tui/internal/graph"
)

// screen is the top-level view.
type screen int

const (
	screenLogin screen = iota
	screenDashboard
	screenBrowse
	screenDetail
	screenHelp
)

// inputMode is the active prompt, if any.
type inputMode int

const (
	modeNormal inputMode = iota
	modeCommand
	modeSearch
)

// prefetchRows is how close to the bottom the cursor gets before the next
// page is fetched, keeping scrolling continuous instead of stalling at each
// page boundary.
const prefetchRows = 10

// maxAutoPages caps a single "load all" so an unbounded tenant cannot pin the
// UI fetching forever. The user can press A again to continue.
const maxAutoPages = 50

// Options configures the root model.
type Options struct {
	Auth     auth.Options
	GraphURL string
	PageSize int
	// Resource is the view opened when the user picks one from the dashboard
	// first; it seeds the dashboard cursor.
	Resource graph.Resource
}

// Model is the root Bubble Tea model.
type Model struct {
	ctx  context.Context
	opts Options

	width, height int
	screen        screen
	// helpReturn is the screen "?" was opened from.
	helpReturn screen
	mode       inputMode

	// --- authentication -----------------------------------------------
	authCursor  int
	authing     bool
	authURL     string
	authURLCh   chan string
	provider    auth.Provider
	identity    auth.Identity
	client      *graph.Client
	authAttempt int

	// --- dashboard ----------------------------------------------------
	dashCursor int

	// --- browse -------------------------------------------------------
	coll   *collection
	cursor int
	offset int

	// gen invalidates in-flight requests. Every response carries the
	// generation it was issued under and is dropped if the user has since
	// switched view, searched, or refreshed -- without it, a slow reply for
	// /users could land in the /groups table.
	gen         int
	loading     bool
	loadingMore bool
	loadAll     bool
	autoPages   int
	history     *searchHistory

	// --- detail -------------------------------------------------------
	detail         graph.Detail
	detailID       string
	detailSections []graph.Section
	detailRaw      bool
	detailLoading  bool
	detailVP       viewport.Model

	// --- chrome -------------------------------------------------------
	err      error
	flash    string
	flashSeq int
	input    textinput.Model
	spin     spinner.Model

	// pendingClipboard is emitted into the next rendered frame as an OSC 52
	// sequence. Routing it through View keeps it synchronised with the
	// renderer instead of racing it on stdout.
	pendingClipboard string

	quitting bool
}

// New builds the root model on the login screen. Nothing is queried until the
// user signs in and picks a view.
func New(ctx context.Context, opts Options) Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.CharLimit = 200

	sp := spinner.New()
	sp.Spinner = spinner.Dot

	m := Model{
		ctx:       ctx,
		opts:      opts,
		screen:    screenLogin,
		authURLCh: make(chan string, 1),
		history:   newSearchHistory(),
		input:     ti,
		spin:      sp,
	}
	// Preselect the option that will not need a browser round trip.
	if auth.AzureCLIAvailable() {
		m.authCursor = authOptionAzureCLI
	}
	if opts.Auth.Method == auth.MethodBrowser {
		m.authCursor = authOptionBrowser
	} else if opts.Auth.Method == auth.MethodAzureCLI {
		m.authCursor = authOptionAzureCLI
	}
	m.dashCursor = dashboardIndexOf(opts.Resource.Kind)
	return m
}

// Init starts the spinner and the listener for the sign-in URL.
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.spin.Tick, waitForAuthURL(m.authURLCh))
}

// Update is the Bubble Tea event loop.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.detailVP.Width = boxInnerWidth(msg.Width)
		m.detailVP.Height = detailBodyHeight(msg.Height)
		if m.screen == screenDetail {
			// Re-wrap: the section layout depends on the frame's width, so a
			// resize changes the content, not just the window onto it.
			m.detailVP.SetContent(m.renderDetailBody())
		}
		m.clampCursor()
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case flashExpiredMsg:
		// Only the newest flash may clear the line; an older timer firing
		// late must not wipe a message the user just triggered.
		if msg.seq == m.flashSeq {
			m.flash = ""
		}
		return m, nil

	case clipboardSentMsg:
		m.pendingClipboard = ""
		return m, nil

	case authURLMsg:
		m.authURL = msg.url
		// Keep listening: a retry produces another URL.
		return m, waitForAuthURL(m.authURLCh)

	case authDoneMsg:
		return m.handleAuthDone(msg)

	case pageMsg:
		return m.handlePage(msg)

	case detailMsg:
		return m.handleDetail(msg)

	case pairMsg:
		return m.handlePair(msg)

	case errMsg:
		if msg.gen != m.gen {
			return m, nil
		}
		m.loading, m.loadingMore, m.loadAll, m.detailLoading = false, false, false, false
		m.err = msg.err
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// ---------------------------------------------------------------- handlers

func (m Model) handleAuthDone(msg authDoneMsg) (tea.Model, tea.Cmd) {
	if msg.attempt != m.authAttempt {
		return m, nil
	}
	m.authing = false
	m.authURL = ""
	if msg.err != nil {
		m.err = msg.err
		return m, nil
	}
	m.err = nil
	m.provider = msg.provider
	m.identity = msg.provider.Identity()
	m.client = graph.New(msg.provider, graph.WithBaseURL(m.opts.GraphURL))
	m.screen = screenDashboard
	return m, nil
}

func (m Model) handlePage(msg pageMsg) (tea.Model, tea.Cmd) {
	if msg.gen != m.gen {
		return m, nil
	}
	m.err = nil

	// Remember what was selected: appending a page re-sorts the collection,
	// which can move the current row somewhere else entirely.
	var selectedID string
	if item, _, ok := m.coll.at(m.cursor); ok {
		selectedID = item.ID()
	}

	if !msg.append {
		m.coll.entries = nil
		m.coll.total = -1
		m.cursor, m.offset = 0, 0
		m.coll.advanced = msg.advanced
		selectedID = ""
	}
	m.coll.appendPage(msg.page)
	m.loading, m.loadingMore = false, false

	if idx := m.coll.indexOf(selectedID); idx >= 0 {
		m.cursor = idx
	}
	m.clampCursor()

	if m.loadAll && m.coll.hasMore() {
		if m.autoPages >= maxAutoPages {
			m.loadAll = false
			return m, m.flashFor(fmt.Sprintf("stopped after %d pages (%d objects) — press A to continue",
				m.autoPages, m.coll.len()))
		}
		m.autoPages++
		m.loadingMore = true
		return m, m.loadNext()
	}
	m.loadAll = false
	return m, nil
}

func (m Model) handleDetail(msg detailMsg) (tea.Model, tea.Cmd) {
	if msg.gen != m.gen || msg.detail.Object.ID() != m.detailID {
		return m, nil
	}
	m.detailLoading = false
	m.detail = msg.detail
	m.detailSections = graph.Sections(msg.detail)
	m.detailVP.SetContent(m.renderDetailBody())
	return m, nil
}

func (m Model) handlePair(msg pairMsg) (tea.Model, tea.Cmd) {
	if msg.gen != m.gen {
		return m, nil
	}
	m.detailLoading = false
	if msg.err != nil {
		m.err = msg.err
		return m, nil
	}
	if msg.counterpart == nil {
		return m, m.flashFor(msg.missing)
	}
	return m.openPaired(*msg.counterpart)
}

// ---------------------------------------------------------------- key input

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// A prompt swallows keys until it is committed or cancelled.
	if m.mode != modeNormal {
		return m.handlePromptKey(msg)
	}
	switch m.screen {
	case screenHelp:
		m.screen = m.helpReturn
		return m, nil
	case screenLogin:
		return m.handleLoginKey(msg)
	case screenDashboard:
		return m.handleDashboardKey(msg)
	case screenDetail:
		return m.handleDetailKey(msg)
	default:
		return m.handleBrowseKey(msg)
	}
}

func (m Model) handleBrowseKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Quit):
		m.quitting = true
		return m, tea.Quit

	case key.Matches(msg, keys.Help):
		m.helpReturn = m.screen
		m.screen = screenHelp
		return m, nil

	case key.Matches(msg, keys.Command):
		return m.openPrompt(modeCommand, "")

	case key.Matches(msg, keys.Search):
		return m.openPrompt(modeSearch, m.coll.search)

	case key.Matches(msg, keys.Dashboard):
		return m.toDashboard()

	case key.Matches(msg, keys.Back):
		// Esc peels back one layer: an active search first, then the view
		// itself, landing on the dashboard.
		if m.coll.search != "" {
			m.coll.search = ""
			return m.reload()
		}
		return m.toDashboard()

	case key.Matches(msg, keys.Enter):
		return m.openDetail()

	case key.Matches(msg, keys.Refresh):
		return m.reload()

	case key.Matches(msg, keys.NextPage):
		return m.fetchMore(false)

	case key.Matches(msg, keys.LoadAll):
		return m.fetchMore(true)

	case key.Matches(msg, keys.Yank):
		return m.yankCurrentID()

	case key.Matches(msg, keys.Up):
		return m.moveCursor(-1)
	case key.Matches(msg, keys.Down):
		return m.moveCursor(1)
	case key.Matches(msg, keys.PageUp):
		return m.moveCursor(-m.tableHeight())
	case key.Matches(msg, keys.PageDown):
		return m.moveCursor(m.tableHeight())
	case key.Matches(msg, keys.Home):
		return m.moveCursor(-m.coll.len())
	case key.Matches(msg, keys.End):
		// Routed through moveCursor rather than setting the cursor directly,
		// so jumping to the bottom prefetches the next page like scrolling.
		return m.moveCursor(m.coll.len())
	}

	// Digits replay this view's recent searches. Views are changed with ":"
	// or from the dashboard, which leaves the number row free for the thing
	// a directory admin repeats most: the same lookups.
	if n, ok := digitIndex(msg.String()); ok {
		return m.replaySearch(n)
	}
	return m, nil
}

func (m Model) handleDetailKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Back):
		m.screen = screenBrowse
		m.detail = graph.Detail{}
		m.detailSections = nil
		m.detailID = ""
		return m, nil
	case key.Matches(msg, keys.Quit):
		m.quitting = true
		return m, tea.Quit
	case key.Matches(msg, keys.Dashboard):
		return m.toDashboard()
	case key.Matches(msg, keys.RawToggle):
		m.detailRaw = !m.detailRaw
		m.detailVP.SetContent(m.renderDetailBody())
		m.detailVP.GotoTop()
		return m, nil
	case key.Matches(msg, keys.Pair):
		return m.jumpToPair()
	case key.Matches(msg, keys.Yank):
		return m.yankCurrentID()
	case key.Matches(msg, keys.Help):
		m.helpReturn = m.screen
		m.screen = screenHelp
		return m, nil
	}
	var cmd tea.Cmd
	m.detailVP, cmd = m.detailVP.Update(msg)
	return m, cmd
}

func (m Model) handlePromptKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.mode = modeNormal
		m.input.Blur()
		return m, nil
	case tea.KeyEnter:
		return m.commitPrompt()
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) commitPrompt() (tea.Model, tea.Cmd) {
	value := strings.TrimSpace(m.input.Value())
	mode := m.mode
	m.mode = modeNormal
	m.input.Blur()

	switch mode {
	case modeSearch:
		return m.runSearch(value)
	case modeCommand:
		return m.runCommand(value)
	}
	return m, nil
}

func (m Model) openPrompt(mode inputMode, initial string) (tea.Model, tea.Cmd) {
	m.mode = mode
	m.input.SetValue(initial)
	m.input.CursorEnd()
	m.err = nil
	return m, m.input.Focus()
}

// runCommand handles the ":" prompt.
func (m Model) runCommand(cmd string) (tea.Model, tea.Cmd) {
	cmd = strings.ToLower(strings.TrimSpace(cmd))
	switch cmd {
	case "":
		return m, nil
	case "q", "quit", "exit":
		m.quitting = true
		return m, tea.Quit
	case "?", "h", "help":
		m.helpReturn = m.screen
		m.screen = screenHelp
		return m, nil
	case "dash", "dashboard", "home":
		return m.toDashboard()
	}
	if res, ok := graph.Lookup(cmd); ok {
		return m.openResource(res)
	}
	return m, m.flashFor(fmt.Sprintf("unknown command %q — try :users, :groups, :appregs, :entapps", cmd))
}

// runSearch re-queries Graph with a new search term. Unlike a local filter,
// this reaches objects that have not been paged in yet, which is the only way
// to find anything in a tenant of any size.
func (m Model) runSearch(term string) (tea.Model, tea.Cmd) {
	if m.client == nil || m.coll == nil {
		return m, nil
	}
	if term == m.coll.search {
		return m, nil
	}
	if term != "" {
		m.history.record(string(m.coll.res.Kind), term)
	}
	m.coll.search = term
	return m.reload()
}

// replaySearch re-runs the nth recent search for this view.
func (m Model) replaySearch(n int) (tea.Model, tea.Cmd) {
	term, ok := m.history.at(string(m.coll.res.Kind), n)
	if !ok {
		return m, nil
	}
	return m.runSearch(term)
}

// ------------------------------------------------------------- data actions

// openResource switches to a view and loads its first page.
func (m Model) openResource(res graph.Resource) (tea.Model, tea.Cmd) {
	if m.client == nil {
		return m, m.flashFor("sign in first")
	}
	if m.coll != nil && res.Kind == m.coll.res.Kind && m.screen == screenBrowse {
		return m, nil
	}
	m.gen++
	m.coll = newCollection(res)
	m.cursor, m.offset = 0, 0
	m.loading, m.loadingMore, m.loadAll = true, false, false
	m.autoPages = 0
	m.err = nil
	m.screen = screenBrowse
	m.dashCursor = dashboardIndexOf(res.Kind)
	return m, m.loadFirst()
}

// toDashboard returns to the view picker without discarding the session.
func (m Model) toDashboard() (tea.Model, tea.Cmd) {
	if m.client == nil {
		return m, nil
	}
	// Bumping the generation drops any page still in flight, so a reply for
	// the view just left cannot repopulate it behind the dashboard.
	m.gen++
	m.loading, m.loadingMore, m.loadAll = false, false, false
	m.screen = screenDashboard
	m.err = nil
	return m, nil
}

// reload re-runs the current query from page one.
func (m Model) reload() (tea.Model, tea.Cmd) {
	if m.coll == nil {
		return m, nil
	}
	m.gen++
	search := m.coll.search
	m.coll = newCollection(m.coll.res)
	m.coll.search = search
	m.cursor, m.offset = 0, 0
	m.loading, m.loadingMore, m.loadAll = true, false, false
	m.autoPages = 0
	m.err = nil
	return m, m.loadFirst()
}

// fetchMore pulls the next page, or every remaining page when all is set.
func (m Model) fetchMore(all bool) (tea.Model, tea.Cmd) {
	if m.loading || m.loadingMore {
		return m, nil
	}
	if !m.coll.hasMore() {
		return m, m.flashFor("all objects loaded")
	}
	m.loadingMore = true
	m.loadAll = all
	if all {
		m.autoPages = 1
	}
	return m, m.loadNext()
}

func (m Model) openDetail() (tea.Model, tea.Cmd) {
	item, _, ok := m.coll.at(m.cursor)
	if !ok {
		return m, nil
	}
	m.screen = screenDetail
	m.detailID = item.ID()
	m.detailRaw = false
	m.detail = graph.Detail{Kind: m.coll.res.Kind, Object: item}
	m.detailSections = graph.Sections(m.detail)
	m.detailVP = viewport.New(boxInnerWidth(m.width), detailBodyHeight(m.height))
	m.detailVP.SetContent(m.renderDetailBody())

	if m.detailID == "" {
		return m, nil
	}
	// The table only $selects the columns it needs. The detail view re-reads
	// the object without a projection and gathers the follow-up lookups --
	// owners, assignments, permission names -- that make it intelligible.
	m.detailLoading = true
	return m, m.loadDetail(m.coll.res, m.detailID)
}

// jumpToPair moves between an app registration and its enterprise
// application. The pairing is resolved when the detail loaded, so this is
// usually instant; it falls back to a lookup when it is not.
func (m Model) jumpToPair() (tea.Model, tea.Cmd) {
	if m.detail.Kind != graph.KindAppRegistrations && m.detail.Kind != graph.KindEnterpriseApps {
		return m, m.flashFor("no paired object for this view")
	}
	if m.detail.Counterpart != nil {
		return m.openPaired(*m.detail.Counterpart)
	}
	if m.detailLoading {
		return m, m.flashFor("still loading — try again in a moment")
	}
	appID := m.detail.Object.String("appId")
	if appID == "" {
		return m, m.flashFor("this object has no application id")
	}
	m.detailLoading = true
	return m, m.loadPair(m.detail.Kind, appID)
}

// openPaired switches to the counterpart's view and opens it directly.
func (m Model) openPaired(c graph.Counterpart) (tea.Model, tea.Cmd) {
	res, ok := graph.Lookup(string(c.Kind))
	if !ok {
		return m, nil
	}
	m.gen++
	m.coll = newCollection(res)
	m.cursor, m.offset = 0, 0
	m.dashCursor = dashboardIndexOf(res.Kind)
	m.screen = screenDetail
	m.detailID = c.ID
	m.detailRaw = false
	m.detailLoading = true
	m.detail = graph.Detail{Kind: c.Kind, Object: graph.Item{"id": c.ID, "displayName": c.DisplayName}}
	m.detailSections = graph.Sections(m.detail)
	m.detailVP = viewport.New(boxInnerWidth(m.width), detailBodyHeight(m.height))
	m.detailVP.SetContent(m.renderDetailBody())
	m.err = nil
	return m, m.loadDetail(res, c.ID)
}

func (m Model) yankCurrentID() (tea.Model, tea.Cmd) {
	var id string
	if m.screen == screenDetail {
		id = m.detailID
	} else if item, _, ok := m.coll.at(m.cursor); ok {
		id = item.ID()
	}
	if id == "" {
		return m, nil
	}
	m.pendingClipboard = id
	return m, tea.Batch(
		m.flashFor("copied "+id),
		func() tea.Msg { return clipboardSentMsg{} },
	)
}

// ------------------------------------------------------------ cursor motion

func (m Model) moveCursor(delta int) (tea.Model, tea.Cmd) {
	if m.coll == nil || m.coll.len() == 0 {
		return m, nil
	}
	m.cursor = clamp(m.cursor+delta, 0, m.coll.len()-1)
	m.ensureVisible()

	if m.coll.hasMore() && !m.loading && !m.loadingMore && m.cursor >= m.coll.len()-prefetchRows {
		m.loadingMore = true
		return m, m.loadNext()
	}
	return m, nil
}

func (m *Model) ensureVisible() {
	h := m.tableHeight()
	if h <= 0 {
		return
	}
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+h {
		m.offset = m.cursor - h + 1
	}
	m.offset = max(0, m.offset)
}

func (m *Model) clampCursor() {
	if m.coll == nil || m.coll.len() == 0 {
		m.cursor, m.offset = 0, 0
		return
	}
	m.cursor = clamp(m.cursor, 0, m.coll.len()-1)
	m.ensureVisible()
}

// ----------------------------------------------------------------- helpers

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// digitIndex maps "1".."9" and "0" to a zero-based slot, so the number row
// addresses ten quick searches in keyboard order.
func digitIndex(s string) (int, bool) {
	if len(s) != 1 {
		return 0, false
	}
	switch c := s[0]; {
	case c >= '1' && c <= '9':
		return int(c - '1'), true
	case c == '0':
		return 9, true
	}
	return 0, false
}

// apiHint returns the remedy for a Graph error, or "" when there is none.
func apiHint(err error) string {
	var api *graph.APIError
	if errors.As(err, &api) {
		return api.Hint()
	}
	return ""
}
