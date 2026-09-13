package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/idoavrah/entra-tui/internal/bidi"
	"github.com/idoavrah/entra-tui/internal/graph"
	"github.com/idoavrah/entra-tui/internal/telemetry"
)

// modalKind is the dialog currently in front of the detail pane.
type modalKind int

const (
	modalNone modalKind = iota
	// modalAddPrompt asks who to add.
	modalAddPrompt
	// modalPick chooses between the candidates an ambiguous term matched.
	modalPick
	// modalConfirm asks for a yes before anything is written.
	modalConfirm
)

// pickerRows is how many candidates the picker shows at once. The rest are
// reachable by scrolling; the dialog stays a dialog rather than growing into
// a second table.
const pickerRows = 8

// modalAction is what a confirmation would carry out.
type modalAction int

const (
	actionAdd modalAction = iota
	actionRemove
)

// modalWidth is the dialog's width, clamped to the frame at render time.
const modalWidth = 66

// openAddModal starts the two-step add: type a name, then confirm the single
// match. Nothing is written until the confirmation is answered.
func (m Model) openAddModal(rel graph.Relationship) (tea.Model, tea.Cmd) {
	if !m.detailHasRelationship(rel) {
		return m, m.flashFor(fmt.Sprintf("this object has no %s to add to", rel.Label()+"s"))
	}

	m.modal = modalAddPrompt
	m.modalRel = rel
	m.modalAction = actionAdd
	m.modalError = ""
	m.input.SetValue("")
	m.input.CursorEnd()
	return m, m.input.Focus()
}

// openRemoveModal asks to confirm removing the selected entry. The default
// answer is no: this is the destructive one.
func (m Model) openRemoveModal() (tea.Model, tea.Cmd) {
	entry, ok := m.selectedEntry()
	if !ok {
		return m, m.flashFor("select a member or an owner first")
	}

	m.modal = modalConfirm
	m.modalAction = actionRemove
	m.modalRel = entry.rel
	m.modalEntry = entry
	m.modalError = ""
	return m, nil
}

// closeModal dismisses the dialog without writing anything.
func (m Model) closeModal() Model {
	m.modal = modalNone
	m.modalTarget = nil
	m.modalItems = nil
	m.modalCursor = 0
	m.modalError = ""
	m.modalBusy = false
	m.input.Blur()
	return m
}

// handleModalKey routes keys while a dialog is open.
func (m Model) handleModalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// While a write is in flight the only sensible key is the one that
	// abandons waiting for it.
	if m.modalBusy {
		if msg.Type == tea.KeyEsc {
			return m.closeModal(), nil
		}
		return m, nil
	}

	switch m.modal {
	case modalAddPrompt:
		switch msg.Type {
		case tea.KeyEsc:
			return m.closeModal(), nil
		case tea.KeyEnter:
			term := strings.TrimSpace(m.input.Value())
			if term == "" {
				return m, nil
			}
			m.modalBusy = true
			m.modalError = ""
			return m, m.findPrincipal(m.modalRel, term)
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd

	case modalPick:
		return m.handlePickKey(msg)

	case modalConfirm:
		// Only an explicit yes proceeds; every other key, Enter included,
		// cancels. A confirmation that commits on Enter is one held-down
		// key away from a change nobody meant to make.
		switch strings.ToLower(msg.String()) {
		case "y":
			m.modalBusy = true
			return m, m.applyMembershipChange()
		}
		return m.closeModal(), nil
	}
	return m, nil
}

// handlePickKey moves through the candidates and chooses one.
//
// Choosing still lands on the confirmation rather than writing: the picker
// narrows who is meant, it does not decide that the change should happen.
func (m Model) handlePickKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	last := len(m.modalItems) - 1

	switch msg.Type {
	case tea.KeyEsc:
		// Back to the prompt with the term still in it, so a near miss can be
		// narrowed instead of retyped from scratch.
		m.modal = modalAddPrompt
		m.modalItems = nil
		m.modalCursor = 0
		m.modalError = ""
		m.input.CursorEnd()
		return m, m.input.Focus()
	case tea.KeyEnter:
		m.modalTarget = m.modalItems[m.modalCursor]
		m.modal = modalConfirm
		m.modalAction = actionAdd
		m.modalItems = nil
		m.modalCursor = 0
		return m, nil
	case tea.KeyUp:
		m.modalCursor = max(0, m.modalCursor-1)
		return m, nil
	case tea.KeyDown:
		m.modalCursor = min(last, m.modalCursor+1)
		return m, nil
	case tea.KeyHome:
		m.modalCursor = 0
		return m, nil
	case tea.KeyEnd:
		m.modalCursor = last
		return m, nil
	case tea.KeyPgUp:
		m.modalCursor = max(0, m.modalCursor-pickerRows)
		return m, nil
	case tea.KeyPgDown:
		m.modalCursor = min(last, m.modalCursor+pickerRows)
		return m, nil
	}

	// The vi keys work here as they do in the tables.
	switch msg.String() {
	case "k":
		m.modalCursor = max(0, m.modalCursor-1)
	case "j":
		m.modalCursor = min(last, m.modalCursor+1)
	}
	return m, nil
}

// handlePrincipals turns a lookup into a confirmation, or explains why it
// cannot.
func (m Model) handlePrincipals(msg principalsMsg) (tea.Model, tea.Cmd) {
	if msg.gen != m.gen || m.modal != modalAddPrompt {
		return m, nil
	}
	m.modalBusy = false

	switch {
	case msg.err != nil:
		m.modalError = "Search failed: " + msg.err.Error()
	case len(msg.items) == 0:
		m.modalError = fmt.Sprintf("Nothing in the directory matches %q.", bidi.Display(msg.term))
	case len(msg.items) > 1:
		// Adding the wrong person is not a risk a guess should take, so an
		// ambiguous term is never resolved by picking the first. It is handed
		// back as a list instead: the user knows which one they meant, and
		// making them guess a narrower term for a name two people share is
		// asking them to solve a problem they cannot see.
		m.modal = modalPick
		m.modalItems = msg.items
		m.modalCursor = 0
		m.input.Blur()
	default:
		m.modal = modalConfirm
		m.modalAction = actionAdd
		m.modalTarget = msg.items[0]
		m.input.Blur()
	}
	return m, nil
}

// handleWriteDone reports the outcome and re-reads the object, so the pane
// shows what Graph actually holds rather than what was asked for.
func (m Model) handleWriteDone(msg writeDoneMsg) (tea.Model, tea.Cmd) {
	if msg.gen != m.gen {
		return m, nil
	}
	m.modalBusy = false
	if msg.err != nil {
		m.modalError = msg.err.Error()
		if hint := apiHint(msg.err); hint != "" {
			m.modalError += "\n" + hint
		}
		return m, nil
	}

	m.track("changed membership", telemetry.Properties{
		"action":       map[bool]string{true: "add", false: "delete"}[m.modalAction == actionAdd],
		"relationship": string(m.modalRel),
	})

	m = m.closeModal()
	m.detailLoading = true
	return m, tea.Batch(m.flashFor(msg.summary), m.loadDetail(m.detailRes, m.detailID))
}

// ---------------------------------------------------------------- rendering

// renderModal draws the dialog centred in the content area.
func (m Model) renderModal(height int) []string {
	width := min(modalWidth, max(20, boxInnerWidth(m.width)-4))

	var title string
	var body []string
	switch m.modal {
	case modalAddPrompt:
		title = "Add " + m.modalRel.Label()
		body = []string{
			styleDim.Render(graph.Truncate(
				"Name, sign-in name or email of the "+m.modalRel.Label()+" to add:", width-4)),
			"",
			stylePrompt.Render("› ") + m.input.View(),
		}
	case modalPick:
		title = "Add " + m.modalRel.Label()
		body = m.pickLines(width)
	case modalConfirm:
		title = "Confirm"
		body = m.confirmLines(width)
	}

	if m.modalError != "" {
		body = append(body, "")
		for _, l := range strings.Split(m.modalError, "\n") {
			for _, wrapped := range wrapLines(l, width-4) {
				body = append(body, styleErr.Render(wrapped))
			}
		}
	}
	if m.modalBusy {
		body = append(body, "", styleWarn.Render(m.spin.View()+" working…"))
	}
	body = append(body, "", m.modalHints())

	// Frame the dialog, then float it in the middle of the pane.
	dialog := boxFrame(width, styleHelpTitle.Render(title), "", padBody(body, width))
	return floatCentre(dialog, boxInnerWidth(m.width), height)
}

// pickLines renders the candidates an ambiguous term matched.
//
// Each row carries the kind as well as the name, because a member may be a
// user or a group and "Ada" can be both. The object id is deliberately not
// here -- it would crowd out the detail that actually tells two people apart,
// and the confirmation that follows still names it.
func (m Model) pickLines(width int) []string {
	inner := width - 4
	start, end := m.pickWindow()

	lines := []string{
		styleDim.Render(graph.Truncate(fmt.Sprintf(
			"%d matches — choose the %s to add:", len(m.modalItems), m.modalRel.Label()), inner)),
		"",
	}
	for i := start; i < end; i++ {
		lines = append(lines, m.pickRow(i, inner))
	}
	if hidden := len(m.modalItems) - end + start; hidden > 0 {
		lines = append(lines, styleDim.Render(graph.Truncate(
			fmt.Sprintf("  … %d more", hidden), inner)))
	}
	// A full result set is a truncated one: say so rather than let somebody
	// conclude the name they wanted is not in the directory.
	if len(m.modalItems) >= graph.PrincipalSearchLimit {
		lines = append(lines, "", styleDim.Render(graph.Truncate(fmt.Sprintf(
			"Showing the first %d — narrow the term to see others.", len(m.modalItems)), inner)))
	}
	return lines
}

// pickWindow is the slice of candidates on screen, kept around the cursor so
// moving past the edge scrolls rather than stops.
func (m Model) pickWindow() (start, end int) {
	if len(m.modalItems) <= pickerRows {
		return 0, len(m.modalItems)
	}
	start = min(max(0, m.modalCursor-pickerRows/2), len(m.modalItems)-pickerRows)
	return start, start + pickerRows
}

// pickRow renders one candidate, fitted to the width before it is styled.
func (m Model) pickRow(i, width int) string {
	item := m.modalItems[i]

	// The kind sits in a fixed column on the right so the names above and
	// below it stay aligned.
	const kindWidth = 6
	body := max(8, width-2-kindWidth-1)
	nameWidth := m.nameColumnWidth(body)
	detailWidth := max(0, body-nameWidth-1)

	name := bidi.Display(graph.Truncate(item.String("displayName"), nameWidth))
	detail := bidi.Display(graph.Truncate(principalDetail(item), detailWidth))
	row := padRight(name, nameWidth) + " " + padRight(detail, detailWidth) +
		" " + padRight(principalKind(item), kindWidth)

	if i == m.modalCursor {
		return styleSelectedBase.Render(padRight("› "+row, width))
	}
	return styleDim.Render("  ") + styleDetailVal.Render(padRight(name, nameWidth)) +
		styleDim.Render(" "+padRight(detail, detailWidth)+" "+padRight(principalKind(item), kindWidth))
}

// nameColumnWidth fits the names to the longest one on offer rather than to a
// fixed share of the dialog.
//
// The detail beside it is what actually tells two people called Ada Lovelace
// apart, so padding the name column past what it needs and cutting the
// sign-in name to pay for it defeats the point of the list. The width is
// taken over every candidate, not just the visible ones, so the column does
// not shift about while scrolling.
func (m Model) nameColumnWidth(body int) int {
	widest := 0
	for _, it := range m.modalItems {
		widest = max(widest, lipgloss.Width(it.String("displayName")))
	}
	return max(6, min(widest, body*3/5))
}

// principalDetail is whatever tells two objects of the same name apart: a
// sign-in name for a user, a mail address for a group.
func principalDetail(i graph.Item) string {
	for _, key := range []string{"userPrincipalName", "mail"} {
		if v := i.String(key); v != "" {
			return v
		}
	}
	return i.ID()
}

// principalKind labels a candidate, going through the same decision the rest
// of the interface uses so a row cannot be labelled one thing here and
// another in the table it came from.
func principalKind(i graph.Item) string {
	switch graph.ObjectViewKind(i) {
	case graph.KindUsers:
		return "User"
	case graph.KindGroups:
		return "Group"
	}
	return ""
}

// confirmLines states exactly what is about to change.
//
// Values are cut to fit before they are styled: a styled line cannot be
// truncated afterwards without risking a severed escape sequence, so an
// over-long name would otherwise punch through the dialog's border.
func (m Model) confirmLines(width int) []string {
	const labelWidth = 8
	valueWidth := max(10, width-4-labelWidth)

	what, preposition := "Add", "To"
	var who, whoID, whoDetail string
	if m.modalAction == actionAdd {
		who = m.modalTarget.String("displayName")
		whoID = m.modalTarget.ID()
		whoDetail = m.modalTarget.String("userPrincipalName")
	} else {
		what, preposition = "Delete", "From"
		who, whoID, whoDetail = m.modalEntry.name, m.modalEntry.id, m.modalEntry.detail
	}

	object := m.detail.Object.String("displayName")
	if object == "" {
		object = m.detailID
	}

	// Names reach the dialog in logical order like everywhere else, so a
	// Hebrew display name needs the same reordering here as in the table.
	row := func(label, value string) string {
		return styleDetailKey.Render(padRight(label, labelWidth)) +
			styleDetailVal.Render(bidi.Display(graph.Truncate(value, valueWidth)))
	}

	// Every party is named with its object id. Display names are not unique
	// in a directory -- two people can share one, and confirming a change
	// against the wrong one is exactly the mistake this dialog exists to
	// prevent -- so the id, which is unique, always appears.
	lines := []string{
		styleContextVal.Render(what + " " + m.modalRel.Label()),
		"",
		row("Who", identify(who, whoID)),
	}
	if whoDetail != "" {
		lines = append(lines, row("", whoDetail))
	}
	lines = append(lines,
		row(preposition, identify(object, m.detailID)),
		row("", m.detail.Kind.Title()),
	)
	return lines
}

// identify renders a directory object as its name followed by its unique id.
func identify(name, id string) string {
	switch {
	case name == "" && id == "":
		return "(unidentified)"
	case name == "":
		return id
	case id == "":
		return name
	default:
		return name + "  (" + id + ")"
	}
}

// modalHints spell out the answer keys, with the safe one first.
func (m Model) modalHints() string {
	switch m.modal {
	case modalConfirm:
		return styleHintKey.Render("y") + styleHintDesc.Render(" yes") +
			styleDim.Render("   ") +
			styleHintKey.Render("any other key") + styleHintDesc.Render(" no (default)")
	case modalPick:
		return styleHintKey.Render("↑↓") + styleHintDesc.Render(" move") +
			styleDim.Render("   ") +
			styleHintKey.Render("enter") + styleHintDesc.Render(" choose") +
			styleDim.Render("   ") +
			styleHintKey.Render("esc") + styleHintDesc.Render(" back")
	}
	return styleHintKey.Render("enter") + styleHintDesc.Render(" search") +
		styleDim.Render("   ") +
		styleHintKey.Render("esc") + styleHintDesc.Render(" cancel")
}

// padBody insets dialog content by one cell.
//
// Nothing is truncated here: styled text cannot be cut without risking a
// severed escape sequence, so every producer of dialog content fits its own
// lines to the width first.
func padBody(lines []string, width int) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = " " + l
	}
	return out
}

// floatCentre places a dialog in the middle of an area of the given size.
func floatCentre(dialog []string, width, height int) []string {
	top := max(0, (height-len(dialog))/2)
	left := max(0, (width-lipgloss.Width(dialog[0]))/2)

	out := make([]string, 0, height)
	for range top {
		out = append(out, "")
	}
	for _, l := range dialog {
		out = append(out, spaces(left)+l)
	}
	return out
}
