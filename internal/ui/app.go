// Package ui implements the entra-tui terminal interface: a k9s-style
// resource browser over the read-only Microsoft Graph directory collections.
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

// viewState is the top-level screen.
type viewState int

const (
	viewBrowse viewState = iota
	viewDetail
	viewHelp
)

// inputMode is the active prompt, if any.
type inputMode int

const (
	modeNormal inputMode = iota
	modeCommand
	modeFilter
	modeSearch
)

// prefetchRows is how close to the bottom the cursor gets before the next
// page is fetched, which keeps scrolling continuous instead of stalling at
// each page boundary.
const prefetchRows = 10

// maxAutoPages caps a single "load all" so an unbounded tenant cannot pin the
// UI fetching forever. The user can press A again to continue.
const maxAutoPages = 50

// Model is the root Bubble Tea model.
type Model struct {
	ctx      context.Context
	client   *graph.Client
	identity auth.Identity
	pageSize int

	width, height int

	view viewState
	mode inputMode

	coll   *collection
	cursor int
	offset int

	// filterBeforePrompt remembers the filter in force when the "/" prompt
	// opened, so cancelling with Esc restores it rather than leaving the
	// half-typed narrowing applied.
	filterBeforePrompt string

	// gen invalidates in-flight requests. Every response carries the
	// generation it was issued under and is dropped if the user has since
	// switched resource, searched, or refreshed -- without it, a slow reply
	// for /users could land in the /groups table.
	gen         int
	loading     bool
	loadingMore bool
	loadAll     bool
	autoPages   int

	err      error
	flash    string
	flashSeq int

	input textinput.Model
	spin  spinner.Model

	detailVP      viewport.Model
	detailItem    graph.Item
	detailID      string
	detailRaw     bool
	detailLoading bool

	// pendingClipboard is emitted into the next rendered frame as an OSC 52
	// sequence. Routing it through View keeps it synchronised with the
	// renderer instead of racing it on stdout.
	pendingClipboard string

	quitting bool
}

// New builds the root model positioned on the given resource.
func New(ctx context.Context, client *graph.Client, identity auth.Identity, res graph.Resource, pageSize int) Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.CharLimit = 200

	sp := spinner.New()
	sp.Spinner = spinner.Dot

	return Model{
		ctx:      ctx,
		client:   client,
		identity: identity,
		pageSize: pageSize,
		coll:     newCollection(res),
		input:    ti,
		spin:     sp,
		loading:  true,
	}
}

// Init kicks off the first page load.
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.spin.Tick, m.loadFirst())
}

// Update is the Bubble Tea event loop.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.detailVP.Width = msg.Width
		m.detailVP.Height = max(1, msg.Height-detailChromeHeight)
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

	case pageMsg:
		return m.handlePage(msg)

	case detailMsg:
		return m.handleDetail(msg)

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

func (m Model) handlePage(msg pageMsg) (tea.Model, tea.Cmd) {
	if msg.gen != m.gen {
		return m, nil
	}
	m.err = nil
	if !msg.append {
		m.coll.items = nil
		m.coll.rows = nil
		m.coll.view = nil
		m.coll.total = -1
		m.cursor, m.offset = 0, 0
		m.coll.advanced = msg.advanced
	}
	m.coll.appendPage(msg.page)
	m.loading, m.loadingMore = false, false
	m.clampCursor()

	if m.loadAll && m.coll.hasMore() {
		if m.autoPages >= maxAutoPages {
			m.loadAll = false
			return m, m.flashFor(fmt.Sprintf("stopped after %d pages (%d objects) - press A to continue",
				m.autoPages, m.coll.loaded()))
		}
		m.autoPages++
		m.loadingMore = true
		return m, m.loadNext()
	}
	m.loadAll = false
	return m, nil
}

func (m Model) handleDetail(msg detailMsg) (tea.Model, tea.Cmd) {
	if msg.gen != m.gen || msg.id != m.detailID {
		return m, nil
	}
	m.detailLoading = false
	m.detailItem = msg.item
	m.detailVP.SetContent(m.renderDetailBody())
	return m, nil
}

// clipboardSentMsg retires a rendered OSC 52 payload.
type clipboardSentMsg struct{}

// ---------------------------------------------------------------- key input

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// A prompt swallows keys until it is committed or cancelled.
	if m.mode != modeNormal {
		return m.handlePromptKey(msg)
	}
	switch m.view {
	case viewHelp:
		// Any key dismisses help, matching k9s.
		m.view = viewBrowse
		return m, nil
	case viewDetail:
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
		m.view = viewHelp
		return m, nil

	case key.Matches(msg, keys.Command):
		return m.openPrompt(modeCommand, "")

	case key.Matches(msg, keys.Filter):
		return m.openPrompt(modeFilter, m.coll.filter)

	case key.Matches(msg, keys.Search):
		return m.openPrompt(modeSearch, m.coll.search)

	case key.Matches(msg, keys.Back):
		// Esc peels back one layer of narrowing at a time: local filter
		// first, then the server-side search.
		switch {
		case m.coll.filter != "":
			m.coll.setFilter("")
			m.cursor, m.offset = 0, 0
			return m, nil
		case m.coll.search != "":
			m.coll.search = ""
			return m.reload()
		}
		return m, nil

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
		// so jumping to the bottom prefetches the next page like scrolling does.
		return m.moveCursor(m.coll.len())
	}

	// Digits jump straight to a resource, the way k9s numbers its views.
	if n, ok := digitIndex(msg.String()); ok {
		all := graph.All()
		if n < len(all) {
			return m.switchResource(all[n])
		}
	}
	return m, nil
}

func (m Model) handleDetailKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Back), key.Matches(msg, keys.Quit):
		m.view = viewBrowse
		m.detailItem = nil
		m.detailID = ""
		return m, nil
	case key.Matches(msg, keys.RawToggle):
		m.detailRaw = !m.detailRaw
		m.detailVP.SetContent(m.renderDetailBody())
		m.detailVP.GotoTop()
		return m, nil
	case key.Matches(msg, keys.Yank):
		return m.yankCurrentID()
	case key.Matches(msg, keys.Help):
		m.view = viewHelp
		return m, nil
	}
	var cmd tea.Cmd
	m.detailVP, cmd = m.detailVP.Update(msg)
	return m, cmd
}

func (m Model) handlePromptKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		mode := m.mode
		m.mode = modeNormal
		m.input.Blur()
		// A cancelled filter restores the unfiltered view; a cancelled
		// search leaves the server query alone because it was never re-run.
		if mode == modeFilter {
			m.coll.setFilter(m.filterBeforePrompt)
			m.clampCursor()
		}
		return m, nil

	case tea.KeyEnter:
		return m.commitPrompt()
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	// The local filter applies live, so the table narrows as you type.
	if m.mode == modeFilter {
		m.coll.setFilter(m.input.Value())
		m.cursor, m.offset = 0, 0
	}
	return m, cmd
}

func (m Model) commitPrompt() (tea.Model, tea.Cmd) {
	value := strings.TrimSpace(m.input.Value())
	mode := m.mode
	m.mode = modeNormal
	m.input.Blur()

	switch mode {
	case modeFilter:
		m.coll.setFilter(value)
		m.clampCursor()
		return m, nil

	case modeSearch:
		if value == m.coll.search {
			return m, nil
		}
		m.coll.search = value
		return m.reload()

	case modeCommand:
		return m.runCommand(value)
	}
	return m, nil
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
		m.view = viewHelp
		return m, nil
	}
	if res, ok := graph.Lookup(cmd); ok {
		return m.switchResource(res)
	}
	return m, m.flashFor(fmt.Sprintf("unknown command %q - try :users, :groups, :apps, :sp", cmd))
}

func (m Model) openPrompt(mode inputMode, initial string) (tea.Model, tea.Cmd) {
	m.mode = mode
	m.filterBeforePrompt = m.coll.filter
	m.input.SetValue(initial)
	m.input.CursorEnd()
	m.err = nil
	return m, m.input.Focus()
}

// ------------------------------------------------------------- data actions

// switchResource replaces the current view, discarding loaded data. The
// generation bump means any page still in flight for the old resource is
// dropped when it arrives.
func (m Model) switchResource(res graph.Resource) (tea.Model, tea.Cmd) {
	if res.Kind == m.coll.res.Kind {
		return m, nil
	}
	m.gen++
	m.coll = newCollection(res)
	m.cursor, m.offset = 0, 0
	m.loading, m.loadingMore, m.loadAll = true, false, false
	m.autoPages = 0
	m.err = nil
	m.view = viewBrowse
	return m, m.loadFirst()
}

// reload re-runs the current query from page one.
func (m Model) reload() (tea.Model, tea.Cmd) {
	m.gen++
	filter, search := m.coll.filter, m.coll.search
	m.coll = newCollection(m.coll.res)
	m.coll.filter, m.coll.search = filter, search
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
	m.view = viewDetail
	m.detailItem = item
	m.detailID = item.ID()
	m.detailRaw = false
	m.detailVP = viewport.New(m.width, max(1, m.height-detailChromeHeight))
	m.detailVP.SetContent(m.renderDetailBody())

	// The table only $selects the columns it needs; re-fetching without a
	// projection fills the detail pane with everything Graph returns by
	// default. Until it lands, the row data already on screen is shown.
	if m.detailID == "" {
		return m, nil
	}
	m.detailLoading = true
	return m, m.loadDetail(m.detailID)
}

func (m Model) yankCurrentID() (tea.Model, tea.Cmd) {
	var id string
	if m.view == viewDetail && m.detailItem != nil {
		id = m.detailItem.ID()
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
	if m.coll.len() == 0 {
		return m, nil
	}
	m.cursor = clamp(m.cursor+delta, 0, m.coll.len()-1)
	m.ensureVisible()

	// Fetch ahead so scrolling does not stall at each page boundary. This is
	// suppressed while a local filter is active: the filter hides rows, so
	// nearing the bottom says nothing about how much of the tenant is loaded,
	// and auto-fetching would silently walk the whole directory.
	if m.coll.filter == "" && m.coll.hasMore() && !m.loading && !m.loadingMore &&
		m.cursor >= m.coll.len()-prefetchRows {
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
	n := m.coll.len()
	if n == 0 {
		m.cursor, m.offset = 0, 0
		return
	}
	m.cursor = clamp(m.cursor, 0, n-1)
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

// digitIndex maps "1".."9" to a zero-based resource index.
func digitIndex(s string) (int, bool) {
	if len(s) != 1 || s[0] < '1' || s[0] > '9' {
		return 0, false
	}
	return int(s[0] - '1'), true
}

// apiHint returns the remedy for a Graph error, or "" when there is none.
func apiHint(err error) string {
	var api *graph.APIError
	if errors.As(err, &api) {
		return api.Hint()
	}
	return ""
}
