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
	// modalConfirm asks for a yes before anything is written.
	modalConfirm
)

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
		// Adding the wrong person is not something a guess should risk, so an
		// ambiguous term is refused rather than resolved by picking the first.
		m.modalError = fmt.Sprintf("%d objects match %q — be more specific.",
			len(msg.items), bidi.Display(msg.term))
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
	if m.modal == modalConfirm {
		return styleHintKey.Render("y") + styleHintDesc.Render(" yes") +
			styleDim.Render("   ") +
			styleHintKey.Render("any other key") + styleHintDesc.Render(" no (default)")
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
