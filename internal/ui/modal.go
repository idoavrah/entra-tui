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
	// modalRole chooses which role an app role assignment grants.
	modalRole
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
// Wide enough that a sign-in name and a role description sit beside a display
// name without either being cut.
const modalWidth = 86

// defaultAccessLabel names the roleless assignment, matching what the portal
// calls it so the two agree.
const defaultAccessLabel = "Default Access"

// openAddModal starts the two-step add: type a name, then confirm the single
// match. Nothing is written until the confirmation is answered.
func (m Model) openAddModal(rel graph.Relationship) (tea.Model, tea.Cmd) {
	if !m.detailHasRelationship(rel) {
		return m, m.flashFor(fmt.Sprintf("this object has no %s to add to", rel.Plural()))
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
		return m, m.flashFor("select a row in the list first")
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
	m.modalRoles = nil
	m.modalRoleID = ""
	m.modalRoleName = ""
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

	case modalPick, modalRole:
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
	last := len(m.pickOptions()) - 1

	switch msg.Type {
	case tea.KeyEsc:
		return m.pickBack()
	case tea.KeyEnter:
		return m.pickChoose()
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

// pickBack steps one stage back rather than abandoning the dialog, so a wrong
// turn costs a keystroke instead of the whole answer.
func (m Model) pickBack() (tea.Model, tea.Cmd) {
	// From the role, back to the candidates -- but only if there were any to
	// choose between; a single match never showed a list to return to.
	if m.modal == modalRole && len(m.modalItems) > 1 {
		m.modal = modalPick
		m.modalCursor = 0
		return m, nil
	}
	// Otherwise back to the prompt with the term still in it, so a near miss
	// can be narrowed instead of retyped from scratch.
	m.modal = modalAddPrompt
	m.modalItems = nil
	m.modalRoles = nil
	m.modalCursor = 0
	m.modalError = ""
	m.input.CursorEnd()
	return m, m.input.Focus()
}

// pickChoose takes the row under the cursor.
func (m Model) pickChoose() (tea.Model, tea.Cmd) {
	if m.modal == modalRole {
		role := m.modalRoles[m.modalCursor]
		m.modalRoleID, m.modalRoleName = role.ID, role.DisplayName
		m.modal = modalConfirm
		return m, nil
	}

	m.modalTarget = m.modalItems[m.modalCursor]
	m.modalAction = actionAdd
	return m.afterPrincipalChosen(), nil
}

// afterPrincipalChosen moves on to the role when there is a role to choose,
// and straight to the confirmation when there is not.
//
// Only an enterprise application has roles at all, and one that publishes
// none can offer nothing but Default Access -- a list of one is a question
// with a single answer, so it is answered rather than asked.
func (m Model) afterPrincipalChosen() Model {
	m.modalRoleID, m.modalRoleName = "", ""
	if m.modalRel != graph.RelAppRoleAssignments {
		m.modal = modalConfirm
		return m
	}

	roles := assignableRoles(m.detail.Object)
	if len(roles) < 2 {
		m.modalRoleID, m.modalRoleName = graph.DefaultAppRoleID, defaultAccessLabel
		m.modal = modalConfirm
		return m
	}
	m.modalRoles = roles
	m.modalCursor = 0
	m.modal = modalRole
	return m
}

// assignableRoles is what an assignment may grant, with Default Access first
// so the cursor starts on it: it is both the commonest answer and the only
// one every application can offer.
func assignableRoles(o graph.Item) []graph.AppRole {
	roles := []graph.AppRole{{
		ID:          graph.DefaultAppRoleID,
		DisplayName: defaultAccessLabel,
		Description: "Admits them to the application without granting a role.",
	}}
	for _, r := range graph.AppRoles(o) {
		if r.AssignableToPrincipals() {
			roles = append(roles, r)
		}
	}
	return roles
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
		m.modalAction = actionAdd
		m.modalTarget = msg.items[0]
		m.modalItems = msg.items
		m.input.Blur()
		m = m.afterPrincipalChosen()
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
	case modalPick, modalRole:
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

// pickOption is one row of a picker, whatever is being chosen.
type pickOption struct {
	name   string
	detail string
	// tag is a short classifier in a column of its own. Empty everywhere it
	// would say the same thing on every row, which is most places.
	tag string
}

// pickOptions is whatever the open picker is choosing between. Principals and
// roles are different things, but they are read the same way -- a name, what
// tells it apart, and sometimes a kind -- so one widget renders both.
func (m Model) pickOptions() []pickOption {
	if m.modal == modalRole {
		out := make([]pickOption, len(m.modalRoles))
		for i, r := range m.modalRoles {
			out[i] = pickOption{name: r.DisplayName, detail: r.Description}
		}
		return out
	}
	out := make([]pickOption, len(m.modalItems))
	for i, it := range m.modalItems {
		out[i] = pickOption{
			name:   it.String("displayName"),
			detail: principalDetail(it),
			tag:    principalKind(it),
		}
	}
	return out
}

// pickPrompt says what is being chosen and why there is a choice at all.
func (m Model) pickPrompt() string {
	if m.modal == modalRole {
		return "Choose the role to grant " + m.modalTarget.String("displayName") + ":"
	}
	return fmt.Sprintf("%d matches — choose the %s to add:",
		len(m.modalItems), m.modalRel.Label())
}

// pickLines renders the open picker.
//
// A principal row carries its kind as well as its name, because a member may
// be a user or a group and "Ada" can be both. The object id is deliberately
// not here -- it would crowd out the detail that actually tells two people
// apart, and the confirmation that follows still names it.
func (m Model) pickLines(width int) []string {
	inner := width - 4
	opts := m.pickOptions()
	start, end := m.pickWindow()

	lines := []string{styleDim.Render(graph.Truncate(m.pickPrompt(), inner)), ""}

	nameWidth, detailWidth, tagWidth := pickColumns(opts, inner)
	for i := start; i < end; i++ {
		lines = append(lines, m.pickRow(opts[i], i == m.modalCursor, inner, nameWidth, detailWidth, tagWidth))
	}
	if hidden := len(opts) - end + start; hidden > 0 {
		lines = append(lines, styleDim.Render(graph.Truncate(
			fmt.Sprintf("  … %d more", hidden), inner)))
	}
	// A full result set is a truncated one: say so rather than let somebody
	// conclude the name they wanted is not in the directory. Roles are never
	// truncated -- an application publishes what it publishes.
	if m.modal == modalPick && len(opts) >= graph.PrincipalSearchLimit {
		lines = append(lines, "", styleDim.Render(graph.Truncate(fmt.Sprintf(
			"Showing the first %d — narrow the term to see others.", len(opts)), inner)))
	}
	return lines
}

// pickColumns fits the columns to their content rather than to fixed shares.
//
// The detail beside a name is what actually tells two people called Ada
// Lovelace apart, so padding the name column past what it needs and cutting
// the sign-in name to pay for it defeats the point of the list. Widths are
// taken over every option, not just the visible ones, so nothing shifts about
// while scrolling, and a tag column that would be empty on every row takes no
// space at all.
func pickColumns(opts []pickOption, inner int) (nameWidth, detailWidth, tagWidth int) {
	widest := func(of func(pickOption) string) int {
		w := 0
		for _, o := range opts {
			w = max(w, lipgloss.Width(of(o)))
		}
		return w
	}

	tagWidth = widest(func(o pickOption) string { return o.tag })
	body := max(8, inner-2-tagWidth)
	if tagWidth > 0 {
		body = max(8, body-1)
	}
	nameWidth = max(6, min(widest(func(o pickOption) string { return o.name }), body*3/5))
	detailWidth = max(0, body-nameWidth-1)
	return nameWidth, detailWidth, tagWidth
}

// pickWindow is the slice of options on screen, kept around the cursor so
// moving past the edge scrolls rather than stops.
func (m Model) pickWindow() (start, end int) {
	n := len(m.pickOptions())
	if n <= pickerRows {
		return 0, n
	}
	start = min(max(0, m.modalCursor-pickerRows/2), n-pickerRows)
	return start, start + pickerRows
}

// pickRow renders one option, fitted to the width before it is styled.
func (m Model) pickRow(o pickOption, current bool, width, nameWidth, detailWidth, tagWidth int) string {
	name := bidi.Display(graph.Truncate(o.name, nameWidth))
	detail := bidi.Display(graph.Truncate(o.detail, detailWidth))

	rest := padRight(detail, detailWidth)
	if tagWidth > 0 {
		rest += " " + padRight(o.tag, tagWidth)
	}
	if current {
		return styleSelectedBase.Render(padRight("› "+padRight(name, nameWidth)+" "+rest, width))
	}
	return styleDim.Render("  ") + styleDetailVal.Render(padRight(name, nameWidth)) +
		styleDim.Render(" "+rest)
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
		styleContextVal.Render(m.confirmHeading(what)),
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
	// An assignment grants something; saying what is the point of having
	// asked. A removal takes the role the entry already showed.
	if m.modalRel == graph.RelAppRoleAssignments && m.modalAction == actionAdd {
		lines = append(lines, row("Role", m.modalRoleName))
	}
	return lines
}

// confirmHeading says what is about to happen.
//
// An app role assignment needs wording of its own: "Delete user or group"
// reads as deleting the person from the directory, when all that goes is
// their assignment to this one application.
func (m Model) confirmHeading(what string) string {
	if m.modalRel != graph.RelAppRoleAssignments {
		return what + " " + m.modalRel.Label()
	}
	if m.modalAction == actionAdd {
		return "Assign user or group"
	}
	return "Delete assignment"
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
	case modalPick, modalRole:
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
