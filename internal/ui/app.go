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
	screenDashboard screen = iota
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

// Options configures the root model.
type Options struct {
	Auth     auth.Options
	GraphURL string
	PageSize int
	// Resource is the view opened when the user picks one from the dashboard
	// first; it seeds the dashboard cursor.
	Resource graph.Resource
	// CacheDir holds the quick-search cache. Empty means the user's own
	// cache directory; tests point it somewhere disposable.
	CacheDir string
	// NoDelay opens an object's pane on the row's own columns and fills the
	// rest in when the read lands. The default waits, so the pane is drawn
	// once rather than flickering as every value is replaced.
	NoDelay bool

	// Client and Identity bypass sign-in when supplied, which is how demo
	// mode runs with no tenant behind it.
	Client   *graph.Client
	Identity auth.Identity
}

// pendingOpen is an object being read for a pane that is not on screen yet.
type pendingOpen struct {
	res graph.Resource
	id  string
	// link marks an open reached from a list inside another pane, which is
	// pushed onto the stack so esc comes back to it.
	link bool
}

// detailFrame is a pane esc can back out to: what it showed, and where the
// reader had got to in it.
type detailFrame struct {
	res      graph.Resource
	id       string
	raw      bool
	detail   graph.Detail
	sections []graph.Section
	tab      int
	cursor   int
	offset   int
	vpOffset int
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
	authing     bool
	authURL     string
	authURLCh   chan string
	provider    auth.Provider
	identity    auth.Identity
	client      *graph.Client
	authAttempt int

	// --- dashboard ----------------------------------------------------
	dashCursor int
	// totals are directory-wide object counts, read from the $count
	// endpoint. A missing entry has not arrived; an entry in totalErrs
	// could not be read.
	totals    map[graph.Kind]int64
	totalErrs map[graph.Kind]error

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
	history     *searchHistory

	// --- detail -------------------------------------------------------
	detail         graph.Detail
	detailID       string
	detailSections []graph.Section
	detailRaw      bool
	detailLoading  bool
	// pending is the object an open is waiting on. Until the read lands the
	// screen still shows what it showed before, so a pane is drawn once,
	// already populated, rather than filling in under the reader.
	pending pendingOpen
	// detailRes is the view describing the object in the pane, which is the
	// browse collection's until a link is followed out of it.
	detailRes graph.Resource
	// detailStack is the panes esc backs out to, innermost last.
	detailStack []detailFrame
	detailVP    viewport.Model
	// detailTab is the list tab in front; tabCursor and tabOffset are the
	// selection and scroll position within it.
	detailTab int
	tabCursor int
	tabOffset int

	// --- modal --------------------------------------------------------
	modal       modalKind
	modalAction modalAction
	modalRel    graph.Relationship
	modalTarget graph.Item
	modalEntry  detailEntry
	modalError  string
	modalBusy   bool

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
		screen:    screenDashboard,
		authURLCh: make(chan string, 1),
		history:   loadSearchHistory(opts.CacheDir),
		totals:    map[graph.Kind]int64{},
		totalErrs: map[graph.Kind]error{},
		input:     ti,
		spin:      sp,
	}
	// A supplied client means there is nothing to sign in to -- demo mode.
	if opts.Client != nil {
		m.client = opts.Client
		m.identity = opts.Identity
	} else {
		// Sign-in starts immediately and unattended: there is nothing to
		// ask, so there is no screen to flash. The dashboard shows the
		// attempt in its status line and fills in as the tokens and counts
		// arrive.
		//
		// The attempt is numbered here, not in Init: Init takes the model by
		// value, so an increment there would be discarded and the reply
		// dropped as stale.
		m.authing = true
		m.authAttempt = 1
	}
	m.dashCursor = dashboardIndexOf(opts.Resource.Kind)
	return m
}

// signInMethod is how this session authenticates.
//
// The browser flow is still implemented and reachable with -auth browser,
// but the Azure CLI is the default and the only one the interface offers:
// an existing az session is a decision the user already made, and a screen
// that flashes past before anyone can read it is not a choice.
func (m Model) signInMethod() auth.Method {
	if m.opts.Auth.Method == auth.MethodBrowser {
		return auth.MethodBrowser
	}
	return auth.MethodAzureCLI
}

// retryAuth starts a fresh sign-in attempt, abandoning any in flight.
func (m Model) retryAuth() (Model, tea.Cmd) {
	m.authAttempt++
	m.authing = true
	m.err = nil
	m.authURL = ""
	return m, m.authenticate(m.signInMethod())
}

// Init starts the spinner, the listener for the sign-in URL, and the
// unattended sign-in.
func (m Model) Init() tea.Cmd {
	if m.client != nil {
		return tea.Batch(m.spin.Tick, m.loadTotals())
	}
	return tea.Batch(
		m.spin.Tick,
		waitForAuthURL(m.authURLCh),
		m.authenticate(m.signInMethod()),
	)
}

// Update is the Bubble Tea event loop.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.detailVP.Width = boxInnerWidth(msg.Width)
		m.detailVP.Height = m.detailBodyHeight(msg.Height)
		if m.screen == screenDetail {
			// Re-wrap: the section layout depends on the frame's width, so a
			// resize changes the content, not just the window onto it.
			m = m.refreshDetail()
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

	case countMsg:
		// Counts are keyed by view and idempotent, so they are accepted
		// whenever they land rather than being tied to a request generation.
		return m.handleCount(msg)

	case errMsg:
		if msg.gen != m.gen {
			return m, nil
		}
		m.loading, m.loadingMore, m.detailLoading = false, false, false
		m.err = msg.err
		return m, nil

	case principalsMsg:
		return m.handlePrincipals(msg)

	case writeDoneMsg:
		return m.handleWriteDone(msg)

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// refreshDetail re-renders the property region and keeps the tab selection
// within range of whatever the object turned out to have.
func (m Model) refreshDetail() Model {
	m.detailVP.SetContent(m.propertyContent())

	lists := m.listSections()
	if len(lists) == 0 {
		m.detailTab, m.tabCursor, m.tabOffset = 0, 0, 0
		return m
	}
	m.detailTab = clamp(m.detailTab, 0, len(lists)-1)

	rows := len(lists[m.detailTab].Fields)
	m.tabCursor = clamp(m.tabCursor, 0, max(0, rows-1))
	return m.scrollTab()
}

// scrollTab keeps the selected row inside the visible slice of the tab.
func (m Model) scrollTab() Model {
	height := m.tabRowsHeight()
	if height <= 0 {
		return m
	}
	if m.tabCursor < m.tabOffset {
		m.tabOffset = m.tabCursor
	}
	if m.tabCursor >= m.tabOffset+height {
		m.tabOffset = m.tabCursor - height + 1
	}
	m.tabOffset = max(0, m.tabOffset)
	return m
}

// tabRowsHeight is how many list rows fit below the properties.
//
// The table's own furniture -- its border, its column titles and the rule
// under them -- comes out of that space. Leaving it in is what let the
// selection walk four rows past the last row actually drawn.
func (m Model) tabRowsHeight() int {
	total := m.contentHeight()
	return max(0, total-m.propertyHeight(total)-tabBarHeight-tabChromeHeight)
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
	return m, m.loadTotals()
}

// handleCount records a directory total for the dashboard.
func (m Model) handleCount(msg countMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.totalErrs[msg.kind] = msg.err
		return m, nil
	}
	delete(m.totalErrs, msg.kind)
	m.totals[msg.kind] = msg.total
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
	return m, nil
}

func (m Model) handleDetail(msg detailMsg) (tea.Model, tea.Cmd) {
	id := msg.detail.Object.ID()
	if msg.gen != m.gen || (id != m.detailID && id != m.pending.id) {
		return m, nil
	}
	m.detailLoading = false
	// An open that was waiting for its object: this is where the pane is
	// built, already populated.
	if m.pending.id == id {
		if m.pending.link {
			m.detailStack = append(m.detailStack, m.detailFrame())
		}
		m.detailRes = m.pending.res
		return m.enterDetail(msg.detail), nil
	}
	m.detail = msg.detail
	m.detailSections = graph.Sections(msg.detail)
	return m.refreshDetail(), nil
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
	// A dialog takes precedence over everything: it is asking a question and
	// nothing else should act until it is answered.
	if m.modal != modalNone {
		return m.handleModalKey(msg)
	}
	// A prompt swallows keys until it is committed or cancelled.
	if m.mode != modeNormal {
		return m.handlePromptKey(msg)
	}
	switch m.screen {
	case screenHelp:
		m.screen = m.helpReturn
		return m, nil
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
		// Opening search starts empty: the common case is looking for
		// something new, and the previous term is a keystroke away in its
		// quick-search slot.
		return m.openPrompt(modeSearch, "")

	case key.Matches(msg, keys.Dashboard):
		return m.toDashboard()

	case key.Matches(msg, keys.Back):
		// Esc leaves, it does not unpick. Clearing a search on the way out
		// meant two presses to get home and a wasted round trip in between;
		// the search is cleared by running an empty one.
		return m.toDashboard()

	case key.Matches(msg, keys.Enter):
		return m.openDetail()

	case key.Matches(msg, keys.Refresh):
		return m.reload()

	case key.Matches(msg, keys.Copy):
		return m.copyCurrentID()

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
	if n, ok := slotIndex(msg.String()); ok {
		return m.replaySearch(n)
	}
	return m, nil
}

func (m Model) handleDetailKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	// Arrows walk the members and owners; pages scroll the whole pane. A
	// long object is read by paging, and its people are picked by arrowing.
	// Arrows work the lists below; pages scroll the properties above.
	case key.Matches(msg, keys.Up):
		return m.moveTabCursor(-1)
	case key.Matches(msg, keys.Down):
		return m.moveTabCursor(1)
	case key.Matches(msg, keys.Left):
		return m.moveTab(-1)
	case key.Matches(msg, keys.Right):
		return m.moveTab(1)
	case key.Matches(msg, keys.PageUp):
		return m.pageDetail(-1)
	case key.Matches(msg, keys.PageDown):
		return m.pageDetail(1)
	case key.Matches(msg, keys.Home):
		m.detailVP.GotoTop()
		return m, nil
	case key.Matches(msg, keys.End):
		m.detailVP.GotoBottom()
		return m, nil

	case key.Matches(msg, keys.Add):
		return m.addToActiveTab()
	case key.Matches(msg, keys.Delete):
		return m.openRemoveModal()

	case key.Matches(msg, keys.Enter):
		return m.followLink()

	case key.Matches(msg, keys.Back):
		// Esc unwinds one link at a time, and only leaves the pane once
		// there is nothing left to come back to.
		if popped, ok := m.popDetail(); ok {
			return popped, nil
		}
		m.cancelPendingDetail()
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
		m.detailVP.GotoTop()
		return m.refreshDetail(), nil
	case key.Matches(msg, keys.Pair):
		return m.jumpToPair()
	case key.Matches(msg, keys.Copy):
		return m.copyCurrentID()
	case key.Matches(msg, keys.Help):
		m.helpReturn = m.screen
		m.screen = screenHelp
		return m, nil
	}
	return m, nil
}

// moveTabCursor walks the active list, scrolling the properties instead when
// there is no list to walk.
func (m Model) moveTabCursor(delta int) (tea.Model, tea.Cmd) {
	section, ok := m.activeSection()
	if !ok || len(section.Fields) == 0 {
		m.detailVP.SetYOffset(max(0, m.detailVP.YOffset+delta))
		return m, nil
	}
	m.tabCursor = clamp(m.tabCursor+delta, 0, len(section.Fields)-1)
	return m.scrollTab(), nil
}

// pageDetail moves a whole screenful.
//
// The lists are what run past their space, so a page moves the list in
// front. Only an object with no lists at all pages its properties.
func (m Model) pageDetail(direction int) (tea.Model, tea.Cmd) {
	if _, ok := m.activeSection(); !ok {
		step := direction * m.propertyHeight(m.contentHeight())
		m.detailVP.SetYOffset(max(0, m.detailVP.YOffset+step))
		return m, nil
	}
	return m.moveTabCursor(direction * max(1, m.tabRowsHeight()))
}

// addToActiveTab adds to whichever list is in front, so one key covers
// members and owners alike and always means "add to what I am looking at".
//
// The tab also settles which collection is written: a device's owners live
// under registeredOwners rather than owners, and the section carries that.
func (m Model) addToActiveTab() (tea.Model, tea.Cmd) {
	section, ok := m.activeSection()
	if !ok {
		return m, m.flashFor("this object has no lists to add to")
	}
	if section.Relationship == "" {
		return m, m.flashFor(strings.ToLower(section.Title) + " is not a list you can add to")
	}
	return m.openAddModal(section.Relationship)
}

// moveTab switches between lists, starting the new one from the top.
func (m Model) moveTab(delta int) (tea.Model, tea.Cmd) {
	lists := m.listSections()
	if len(lists) < 2 {
		return m, nil
	}
	m.detailTab = clamp(m.detailTab+delta, 0, len(lists)-1)
	m.tabCursor, m.tabOffset = 0, 0
	return m, nil
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
	m.loading, m.loadingMore = true, false
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
	m.cancelPendingDetail()
	m.loading, m.loadingMore = false, false
	m.screen = screenDashboard
	m.err = nil
	return m, m.loadTotals()
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
	m.loading, m.loadingMore = true, false
	m.err = nil
	return m, m.loadFirst()
}

// enterDetail installs an object in the detail pane and shows it.
func (m Model) enterDetail(d graph.Detail) Model {
	m.screen = screenDetail
	m.pending = pendingOpen{}
	m.detailID = d.Object.ID()
	m.detailRaw = false
	m.detail = d
	m.detailSections = graph.Sections(d)
	m.detailVP = viewport.New(boxInnerWidth(m.width), m.detailBodyHeight(m.height))
	m.detailTab, m.tabCursor, m.tabOffset = 0, 0, 0
	return m.refreshDetail()
}

func (m Model) openDetail() (tea.Model, tea.Cmd) {
	item, _, ok := m.coll.at(m.cursor)
	if !ok {
		return m, nil
	}

	// The table only $selects the columns it needs. The detail view re-reads
	// the object without a projection and gathers the follow-up lookups --
	// owners, assignments, permission names -- that make it intelligible.
	id := item.ID()
	m.detailRes = m.coll.res
	m.detailStack = nil
	if id == "" {
		return m.enterDetail(graph.Detail{Kind: m.coll.res.Kind, Object: item}), nil
	}
	m.detailLoading = true

	// The table stays on screen until that read lands, so the pane is drawn
	// once. -nodelay opens it on the row's handful of columns instead, and
	// replaces every value a moment later.
	if !m.opts.NoDelay {
		m.pending = pendingOpen{res: m.coll.res, id: id}
		return m, m.loadDetail(m.coll.res, id)
	}

	m = m.enterDetail(graph.Detail{Kind: m.coll.res.Kind, Object: item})
	return m, m.loadDetail(m.coll.res, id)
}

// followLink opens the object under the tab cursor in a pane of its own,
// keeping the pane it came from on the stack for esc to return to.
//
// The whole point of a membership list is the objects in it; reading one had
// meant going back to its own view and searching for it by name.
func (m Model) followLink() (tea.Model, tea.Cmd) {
	res, f, ok := m.linkTarget()
	if !ok {
		return m, nil
	}

	m.detailLoading = true
	stub := graph.Detail{Kind: f.Kind, Object: graph.Item{"id": f.ID, "displayName": f.Label}}
	if m.opts.NoDelay {
		m.detailStack = append(m.detailStack, m.detailFrame())
		m.detailRes = res
		return m.enterDetail(stub), m.loadDetail(res, f.ID)
	}
	// The pane on screen stays put, and stays usable, until the read lands.
	m.pending = pendingOpen{res: res, id: f.ID, link: true}
	return m, m.loadDetail(res, f.ID)
}

// detailFrame snapshots the pane on screen so esc can come back to it.
func (m Model) detailFrame() detailFrame {
	return detailFrame{
		res: m.detailRes, id: m.detailID, raw: m.detailRaw,
		detail: m.detail, sections: m.detailSections,
		tab: m.detailTab, cursor: m.tabCursor, offset: m.tabOffset,
		vpOffset: m.detailVP.YOffset,
	}
}

// popDetail returns to the pane esc was pressed from, and reports whether
// there was one.
func (m Model) popDetail() (Model, bool) {
	if len(m.detailStack) == 0 {
		return m, false
	}
	f := m.detailStack[len(m.detailStack)-1]
	m.detailStack = m.detailStack[:len(m.detailStack)-1]

	m.pending = pendingOpen{}
	m.detailLoading = false
	m.detailRes, m.detailID, m.detailRaw = f.res, f.id, f.raw
	m.detail, m.detailSections = f.detail, f.sections
	m.detailVP = viewport.New(boxInnerWidth(m.width), m.detailBodyHeight(m.height))
	m.detailTab, m.tabCursor, m.tabOffset = f.tab, f.cursor, f.offset
	m = m.refreshDetail()
	m.detailVP.SetYOffset(f.vpOffset)
	return m, true
}

// cancelPendingDetail abandons an open that has not arrived. Moving off the
// row or leaving the view means the pane is no longer wanted, and it must not
// spring open when the read finally lands.
func (m *Model) cancelPendingDetail() {
	if m.pending.id != "" {
		m.pending = pendingOpen{}
		m.detailLoading = false
	}
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
	m.detailRes = res
	m.detailStack = nil
	m = m.enterDetail(graph.Detail{Kind: c.Kind, Object: graph.Item{"id": c.ID, "displayName": c.DisplayName}})
	m.detailLoading = true
	m.err = nil
	m.loading = true

	// The table behind the pane is loaded too. Without it, backing out of a
	// paired object landed on an empty list: the collection had been swapped
	// for the counterpart's view but never populated.
	return m, tea.Batch(m.loadDetail(res, c.ID), m.loadFirst())
}

func (m Model) copyCurrentID() (tea.Model, tea.Cmd) {
	payload, what := "", ""
	switch {
	// In the raw view the whole object is what is on screen, so that is what
	// copying it should hand over.
	case m.screen == screenDetail && m.detailRaw && m.detail.Object != nil:
		payload, what = m.detail.Object.JSON(), "copied the full JSON"
	case m.screen == screenDetail:
		payload, what = m.detailID, "copied "+m.detailID
	default:
		if item, _, ok := m.coll.at(m.cursor); ok {
			payload, what = item.ID(), "copied "+item.ID()
		}
	}
	if payload == "" {
		return m, nil
	}

	m.pendingClipboard = payload
	return m, tea.Batch(
		m.flashFor(what),
		func() tea.Msg { return clipboardSentMsg{} },
	)
}

// ------------------------------------------------------------ cursor motion

func (m Model) moveCursor(delta int) (tea.Model, tea.Cmd) {
	if m.coll == nil || m.coll.len() == 0 {
		return m, nil
	}
	m.cancelPendingDetail()
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

// digitIndex maps "1".."9" onto a zero-based position, for the dashboard
// tiles, which are numbered from one because that is how they are labelled.
func digitIndex(s string) (int, bool) {
	if len(s) != 1 || s[0] < '1' || s[0] > '9' {
		return 0, false
	}
	return int(s[0] - '1'), true
}

// slotIndex maps a digit onto the quick-search slot it replays. The slots are
// numbered from zero, so the digit is the slot: no arithmetic, and no slot
// ten hiding behind the "0" key.
func slotIndex(s string) (int, bool) {
	if len(s) != 1 || s[0] < '0' || s[0] > '9' {
		return 0, false
	}
	return int(s[0] - '0'), true
}

// apiHint returns the remedy for a Graph error, or "" when there is none.
func apiHint(err error) string {
	var api *graph.APIError
	if errors.As(err, &api) {
		return api.Hint()
	}
	return ""
}
