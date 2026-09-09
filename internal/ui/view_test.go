package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/idoavrah/entra-tui/internal/bidi"
	"github.com/idoavrah/entra-tui/internal/graph"
)

func TestQuickEntriesAreExactlyOneCellWide(t *testing.T) {
	// An entry a single cell too wide shifts the whole right-hand column,
	// which is exactly what a fixed grid must not do.
	m := browsing(t)
	slots := []string{"ada", "", "a much longer search term than fits", "x"}

	for i := range slots {
		got := m.quickEntry(slots, i)
		if w := lipgloss.Width(got); w != quickEntryWidth {
			t.Errorf("slot %d (%q) rendered %d cells, want %d", i, slots[i], w, quickEntryWidth)
		}
	}
	// Past the end of the slot array too.
	if w := lipgloss.Width(m.quickEntry(slots, 9)); w != quickEntryWidth {
		t.Errorf("empty slot rendered %d cells, want %d", w, quickEntryWidth)
	}
}

func TestQuickSearchColumnsAlignAcrossRows(t *testing.T) {
	m := browsing(t)
	for _, term := range []string{"ada", "finance", "a", "guest", "contoso", "x"} {
		m.history.record(string(graph.KindUsers), term)
	}

	lines := m.quickSearchBlock()
	if len(lines) != quickSearchRows {
		t.Fatalf("got %d rows, want %d", len(lines), quickSearchRows)
	}

	// Every row must put its second column at the same offset.
	var offsets []int
	for _, l := range lines {
		idx := strings.Index(l, "[")
		second := strings.Index(l[idx+1:], "[")
		if second < 0 {
			t.Fatalf("row %q has no second column", l)
		}
		offsets = append(offsets, lipgloss.Width(l[:idx+1+second]))
	}
	for i, o := range offsets {
		if o != offsets[0] {
			t.Errorf("row %d starts its second column at %d, row 0 at %d", i, o, offsets[0])
		}
	}
}

func TestQuickSearchGridIsFixedWidthRegardlessOfTerminal(t *testing.T) {
	m := browsing(t)
	m.history.record(string(graph.KindUsers), "ada")

	narrow := m.quickSearchBlock()
	m.width = 300
	wide := m.quickSearchBlock()

	if lipgloss.Width(narrow[0]) != lipgloss.Width(wide[0]) {
		t.Errorf("grid width changed with the terminal: %d vs %d",
			lipgloss.Width(narrow[0]), lipgloss.Width(wide[0]))
	}
	if lipgloss.Width(wide[0]) != quickBlockWidth {
		t.Errorf("grid is %d cells, want the fixed %d", lipgloss.Width(wide[0]), quickBlockWidth)
	}
}

func TestQuickSearchGridSitsBesideTheWordmark(t *testing.T) {
	m := browsing(t)
	m.history.record(string(graph.KindUsers), "ada")

	header := strings.Split(m.renderHeader(), "\n")
	line := header[0]

	slotAt := strings.Index(line, "[0]")
	logoAt := strings.Index(line, m.headerLogo()[0])
	if slotAt < 0 || logoAt < 0 {
		t.Fatalf("header %q is missing the grid or the wordmark", line)
	}
	if slotAt < len(line)/3 {
		t.Errorf("the grid starts at byte %d of %d; it should sit to the right", slotAt, len(line))
	}
	if slotAt > logoAt {
		t.Error("the grid is drawn after the wordmark")
	}
}

func TestQuickSearchGridDroppedWhenThereIsNoRoom(t *testing.T) {
	m := browsing(t)
	m.history.record(string(graph.KindUsers), "ada")
	m.width = 60

	if strings.Contains(m.renderHeader(), "[0]") {
		t.Error("the grid is drawn in a terminal with no room for it")
	}
}

func TestRowsAreTintedByState(t *testing.T) {
	// Reading one flag out of a column of fifty is what a colour is for, so
	// the state colours the whole line rather than the cell that carries it.
	users, _ := graph.Lookup("users")
	devices, _ := graph.Lookup("devices")
	appregs, _ := graph.Lookup("appregs")
	entapps, _ := graph.Lookup("entapps")

	for _, tc := range []struct {
		name string
		res  graph.Resource
		item graph.Item
		want lipgloss.Style
	}{
		{"enabled user", users, graph.Item{"accountEnabled": true}, styleRow},
		{"disabled user", users, graph.Item{"accountEnabled": false}, styleRowMuted},
		{"user with no flag", users, graph.Item{}, styleRow},
		{"disabled service principal", entapps, graph.Item{"accountEnabled": false}, styleRowMuted},
		{"compliant device", devices, graph.Item{"accountEnabled": true, "isCompliant": true}, styleRow},
		{"non-compliant device", devices, graph.Item{"accountEnabled": true, "isCompliant": false}, styleRowWarn},
		// A disabled device is inert, so being non-compliant is moot.
		{"disabled device", devices, graph.Item{"accountEnabled": false, "isCompliant": false}, styleRowMuted},
		{"app with a live secret", appregs, graph.Item{
			"passwordCredentials": []any{map[string]any{"endDateTime": "2099-01-01T00:00:00Z"}},
		}, styleRow},
		{"app with a lapsed secret", appregs, graph.Item{
			"passwordCredentials": []any{map[string]any{"endDateTime": "2001-01-01T00:00:00Z"}},
		}, styleRowWarn},
	} {
		if got := rowStyle(tc.res, tc.item); got.GetForeground() != tc.want.GetForeground() {
			t.Errorf("%s: row style = %v, want %v", tc.name, got.GetForeground(), tc.want.GetForeground())
		}
	}
}

func TestResourcesWithoutStateRenderNormally(t *testing.T) {
	groups, _ := graph.Lookup("groups")
	if groups.State != nil {
		t.Skip("groups gained a state function")
	}
	if got := rowStyle(groups, graph.Item{}); got.GetForeground() != styleRow.GetForeground() {
		t.Error("a resource with no state classifier should render normally")
	}
}

func TestQuickSearchBlockIsEmptyBeforeAnySearch(t *testing.T) {
	// The header's key legend already says how to search; a placeholder
	// repeating it is just noise.
	m := browsing(t)
	if got := m.quickSearchBlock(); got != nil {
		t.Errorf("quickSearchBlock = %v, want nothing before a search is run", got)
	}
	if strings.Contains(m.renderHeader(), "press / to search") {
		t.Error("the header still carries the placeholder hint")
	}
}

func TestQuickSearchGridOnlyAppearsOverTheTable(t *testing.T) {
	// The slots replay searches over a table; nowhere else can act on one.
	m := browsing(t)
	m.history.record(string(graph.KindUsers), "ada")
	if len(m.quickSearchBlock()) == 0 {
		t.Fatal("the table with a recorded search has no grid")
	}

	for _, s := range []screen{screenDashboard, screenDetail, screenHelp} {
		m := m
		m.screen = s
		if len(m.quickSearchBlock()) != 0 {
			t.Errorf("screen %v draws the quick-search grid", s)
		}
		if strings.Contains(m.View(), "[0] ada") {
			t.Errorf("screen %v shows a quick search", s)
		}
	}
}

func TestBreadcrumbKeepsTheObjectWhenItCannotFit(t *testing.T) {
	m := browsing(t)
	m.width = 30
	m.screen = screenDetail
	m.detailID = "u1"
	m.detail = graph.Detail{Kind: graph.KindUsers,
		Object: graph.Item{"id": "u1", "displayName": "Ada Lovelace"}}

	line := m.renderBreadcrumb(m.width)
	if !strings.Contains(line, "Ada Lovelace") {
		t.Errorf("trail %q dropped the object rather than the steps before it", line)
	}
	if lipgloss.Width(line) > m.width {
		t.Errorf("trail is %d cells wide, want at most %d", lipgloss.Width(line), m.width)
	}
}

func TestBreadcrumbDrawsEveryStep(t *testing.T) {
	// Styling made the line far longer in bytes than in cells, and cutting
	// the finished string to the terminal width ate the object's name and
	// left the trail ending in a separator.
	m := browsing(t)
	m.screen = screenDetail
	m.detailID = "u1"
	m.detail = graph.Detail{Kind: graph.KindUsers,
		Object: graph.Item{"id": "u1", "displayName": "Zephyr Silverbrook"}}

	line := m.renderBreadcrumb(m.width)
	plain := ansiPattern.ReplaceAllString(line, "")
	for _, want := range []string{"Users", "Zephyr Silverbrook"} {
		if !strings.Contains(plain, want) {
			t.Errorf("trail %q is missing %q", plain, want)
		}
	}
	if strings.HasSuffix(strings.TrimSpace(plain), "›") {
		t.Errorf("trail %q ends in a separator", plain)
	}
}

func TestRightToLeftSearchTermsAreReadable(t *testing.T) {
	// Typing a Hebrew name and watching it come out backwards is what this
	// is about: the term is reordered for display everywhere it is shown.
	const term = "שרה"
	reordered := bidi.Display(term)
	if reordered == term {
		t.Fatal("the test term is not reordered, so it proves nothing")
	}

	m := browsing(t)
	m.history.record(string(graph.KindUsers), term)
	m.coll.search = term

	grid := strings.Join(m.quickSearchBlock(), "\n")
	if !strings.Contains(grid, reordered) {
		t.Errorf("the quick-search slot shows %q in logical order", term)
	}
	if !strings.Contains(m.tableCaption(), reordered) {
		t.Errorf("the frame's title shows %q in logical order", term)
	}

	// ...and while it is being typed.
	m.mode = modeSearch
	m.input.SetValue(term)
	if !strings.Contains(m.renderPromptLine(), reordered) {
		t.Errorf("the prompt shows %q in logical order", term)
	}
}

func TestPlainPromptTextKeepsTheRealCursor(t *testing.T) {
	// Reordering costs the cursor its place, so it is only done where there
	// is right-to-left text to fix.
	m := browsing(t)
	m.mode = modeSearch
	m.input.SetValue("ada")
	m.input.Focus()

	if got, want := m.renderPromptLine(), stylePrompt.Render("/")+m.input.View(); got != want {
		t.Errorf("prompt = %q, want the input's own view for plain text", got)
	}
}

func TestSelectionKeepsARowsStateColour(t *testing.T) {
	// The cursor is exactly when a reader is looking hardest at a row, so
	// that is the worst moment to repaint a disabled account in the same
	// colour as everything else.
	warn, plain := selected(styleRowWarn), selected(styleRow)
	if warn.GetForeground() == plain.GetForeground() {
		t.Error("a flagged row and a plain one look the same under the cursor")
	}
	if warn.GetForeground() != styleRowWarn.GetForeground() {
		t.Error("the selection repainted what the row's state says about it")
	}
	if warn.GetBackground() != styleRowSelected.GetBackground() {
		t.Error("the selected row is not marked as selected at all")
	}
}
