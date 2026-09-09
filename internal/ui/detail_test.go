package ui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/idoavrah/entra-tui/internal/graph"
)

// hebrewOwner is a Hebrew personal name used to check right-to-left handling.
const hebrewOwner = "שרה כהן"

// mustBody renders the detail pane's property region.
func mustBody(m Model) string {
	return strings.Join(m.propertyLines(), "\n")
}

func reverseRunes(s string) string {
	r := []rune(s)
	for lo, hi := 0, len(r)-1; lo < hi; lo, hi = lo+1, hi-1 {
		r[lo], r[hi] = r[hi], r[lo]
	}
	return string(r)
}

func TestFieldLabelsGetRTLTreatment(t *testing.T) {
	// Owner names, app roles and assigned principals all arrive as labels,
	// so a label is data and needs reordering just as a value does.
	out := strings.Join(renderFieldLines(
		graph.Field{Label: hebrewOwner, Value: "he.user@x.com"}, 26, 40), "\n")

	if !strings.Contains(out, reverseRunes(hebrewOwner)) {
		t.Errorf("rendered field %q does not contain the reordered label", out)
	}
	if strings.Contains(out, hebrewOwner) {
		t.Error("the label was left in logical order, which reads backwards in a terminal")
	}
}

func TestFieldValuesGetRTLTreatment(t *testing.T) {
	out := strings.Join(renderFieldLines(
		graph.Field{Label: "Display name", Value: hebrewOwner}, 26, 40), "\n")
	if !strings.Contains(out, reverseRunes(hebrewOwner)) {
		t.Errorf("rendered field %q does not contain the reordered value", out)
	}
}

func TestFieldLeavesLatinAlone(t *testing.T) {
	out := strings.Join(renderFieldLines(
		graph.Field{Label: "Display name", Value: "Ada Lovelace"}, 26, 40), "\n")
	if !strings.Contains(out, "Ada Lovelace") {
		t.Errorf("rendered field %q lost its Latin value", out)
	}
}

func TestMultiValuedFieldListsOnePerLine(t *testing.T) {
	lines := renderFieldLines(graph.Field{
		Label:  "Web redirect URIs",
		Values: []string{"https://a/cb", "https://b/cb"},
	}, 26, 40)

	if len(lines) != 2 {
		t.Fatalf("got %d lines, want one per value:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	if !strings.Contains(lines[0], "Web redirect URIs") {
		t.Error("the first line should carry the label")
	}
	if strings.Contains(lines[1], "Web redirect URIs") {
		t.Error("the label should not repeat on continuation lines")
	}
	if !strings.Contains(lines[1], "https://b/cb") {
		t.Errorf("second value missing from %q", lines[1])
	}
}

func TestLabelWidthFitsTheLongestLabel(t *testing.T) {
	// "Widen if needed": the label column should size to its content rather
	// than wrapping a label that would have fitted.
	sections := []graph.Section{{Title: "S", Fields: []graph.Field{
		{Label: "Application (client) ID", Value: "x"},
		{Label: "ID", Value: "y"},
	}}}

	got := detailLabelWidth(sections, 80)
	if got < lipgloss.Width("Application (client) ID") {
		t.Errorf("label width = %d, too narrow for the longest label", got)
	}
}

func TestLabelWidthStaysWithinBounds(t *testing.T) {
	// A pathological label must not squeeze the values off the pane...
	long := []graph.Section{{Title: "S", Fields: []graph.Field{
		{Label: strings.Repeat("x", 120), Value: "v"},
	}}}
	if got := detailLabelWidth(long, 80); got > detailLabelMax {
		t.Errorf("label width = %d, want it capped at %d", got, detailLabelMax)
	}
	// ...and a narrow column must not let it exceed half the width.
	if got := detailLabelWidth(long, 30); got > 15 {
		t.Errorf("label width = %d in a 30-cell column, want at most half", got)
	}
	// Short labels still get a usable minimum.
	short := []graph.Section{{Title: "S", Fields: []graph.Field{{Label: "a", Value: "v"}}}}
	if got := detailLabelWidth(short, 80); got < detailLabelMin {
		t.Errorf("label width = %d, want at least %d", got, detailLabelMin)
	}
}

func TestWrapLinesPrefersWordBoundaries(t *testing.T) {
	got := wrapLines("the quick brown fox jumps", 12)
	for _, l := range got {
		if lipgloss.Width(l) > 12 {
			t.Errorf("line %q exceeds the width", l)
		}
	}
	if strings.Contains(got[0], "brow\n") || strings.HasSuffix(got[0], "brow") {
		t.Errorf("first line %q broke mid-word unnecessarily", got[0])
	}
}

func TestWrapLinesHardBreaksUnbreakableTokens(t *testing.T) {
	// A GUID or a long URL has no word boundary; it must still be cut rather
	// than bleed into the next column.
	guid := strings.Repeat("a", 50)
	for _, l := range wrapLines(guid, 20) {
		if lipgloss.Width(l) > 20 {
			t.Errorf("line %q exceeds the width", l)
		}
	}
}

func TestWrapLinesLeavesShortTextAlone(t *testing.T) {
	got := wrapLines("short", 20)
	if len(got) != 1 || got[0] != "short" {
		t.Errorf("wrapLines = %v, want the text untouched", got)
	}
}

func TestTwoColumnLayoutBalancesAndKeepsReadingOrder(t *testing.T) {
	blocks := [][]string{
		{"a1", "a2", "a3"},
		{"b1", "b2", "b3"},
		{"c1", "c2"},
	}
	out := twoColumnLayout(blocks, 20)
	lines := strings.Split(out, "\n")

	// Reading order: the left column holds the earlier sections.
	if !strings.HasPrefix(lines[0], "a1") {
		t.Errorf("first line = %q, want the first section top-left", lines[0])
	}
	// The later section moved into the right column rather than continuing down.
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "c1") {
		t.Error("the third section is missing")
	}
	// No section is split across columns: b1..b3 stay contiguous in one.
	if strings.Count(joined, "b1") != 1 {
		t.Error("a section was duplicated across columns")
	}
	// Sections of 3, 3 and 2 lines split best as 3 | 5: cutting at the
	// halfway mark instead would give 6 | 2 and a taller pane.
	if len(lines) != 5 {
		t.Errorf("layout is %d lines tall, want the optimal 5 (columns of 3 and 5)", len(lines))
	}
}

func TestBalancedSplitMinimisesTheTallerColumn(t *testing.T) {
	for _, tc := range []struct {
		name  string
		sizes []int
		want  int
	}{
		{"3-3-2 cuts early", []int{3, 3, 2}, 1},
		{"equal halves", []int{4, 4}, 1},
		{"one big then small", []int{10, 1, 1}, 1},
		{"many small", []int{1, 1, 1, 1, 1, 1}, 3},
		{"single block stays left", []int{5}, 1},
	} {
		blocks := make([][]string, len(tc.sizes))
		for i, n := range tc.sizes {
			blocks[i] = make([]string, n)
		}
		if got := balancedSplit(blocks); got != tc.want {
			t.Errorf("%s: balancedSplit = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestTwoColumnLayoutHandlesASingleBlock(t *testing.T) {
	out := twoColumnLayout([][]string{{"only"}}, 20)
	if strings.TrimSpace(out) != "only" {
		t.Errorf("out = %q, want the single block rendered alone", out)
	}
}

func TestDetailBodyUsesOneColumnWhenNarrow(t *testing.T) {
	m := browsing(t)
	m.width = 80
	m.detail = graph.Detail{Kind: graph.KindUsers, Object: graph.Item{
		"id": "u", "displayName": "Ada", "userPrincipalName": "ada@x.com", "jobTitle": "Engineer",
	}}
	m.detailSections = graph.Sections(m.detail)

	for _, line := range strings.Split(mustBody(m), "\n") {
		if lipgloss.Width(line) > boxInnerWidth(80) {
			t.Errorf("line %q overflows the frame at width 80", line)
		}
	}
}

func TestDetailBodySpreadsToTwoColumnsWhenWide(t *testing.T) {
	m := browsing(t)
	m.width = 180
	m.detail = graph.Detail{
		Kind: graph.KindAppRegistrations,
		Object: graph.Item{
			"id": "a", "displayName": "Contoso", "appId": "guid",
			"signInAudience": "AzureADMyOrg", "publisherDomain": "contoso.com",
			// A second property section, so there is something to balance.
			"web": map[string]any{"redirectUris": []any{"https://a/cb"}, "homePageUrl": "https://a"},
		},
	}
	m.detailSections = graph.Sections(m.detail)

	body := mustBody(m)
	// Two section headings on one line is the signature of a two-column pane.
	found := false
	for _, line := range strings.Split(body, "\n") {
		if strings.Count(line, "▌") == 2 {
			found = true
		}
		if lipgloss.Width(line) > boxInnerWidth(180) {
			t.Errorf("line %q overflows the frame at width 180", line)
		}
	}
	if !found {
		t.Error("no line carries two section headings; the pane did not use two columns")
	}
}

func TestPropertiesRenderHeadingsAndListsExplainEmptiness(t *testing.T) {
	m := browsing(t)
	m.detail = graph.Detail{
		Kind:   graph.KindEnterpriseApps,
		Object: graph.Item{"id": "sp1", "displayName": "Contoso"},
	}
	m.detailSections = graph.Sections(m.detail)
	m = m.refreshDetail()

	if body := mustBody(m); !strings.Contains(body, "ESSENTIALS") {
		t.Error("the property region has no Essentials heading")
	}

	// An empty list explains itself in its own tab rather than rendering
	// blank, and no longer crowds the properties above.
	for i := range m.listSections() {
		m.detailTab = i
		if strings.Contains(strings.Join(m.renderTabRows(10), "\n"), "No users or groups are assigned") {
			return
		}
	}
	t.Error("no tab explains that nothing is assigned")
}

func TestPairHintNamesTheTarget(t *testing.T) {
	m := browsing(t)
	m.detail = graph.Detail{
		Kind: graph.KindAppRegistrations,
		Counterpart: &graph.Counterpart{
			Kind: graph.KindEnterpriseApps, ID: "sp1", DisplayName: "Contoso",
		},
	}
	if got := m.pairHint(); got != "enterprise app" {
		t.Errorf("pairHint = %q, want it to name the target", got)
	}

	m.detail.Counterpart = nil
	m.detailLoading = false
	if got := m.pairHint(); !strings.Contains(got, "find") {
		t.Errorf("pairHint = %q, want it to offer a lookup", got)
	}
}

func TestTabBarShowsEveryListWithItsSize(t *testing.T) {
	// The bar is styled, so it must be built to fit rather than truncated
	// afterwards: cutting a styled string severs an escape sequence, which
	// once cost the bar a whole tab and left a stray border character.
	m := browsing(t)
	m.detail = graph.Detail{
		Kind:   graph.KindGroups,
		Object: graph.Item{"id": "g1", "displayName": "Research", "securityEnabled": true},
		Owners: []graph.Item{{"id": "u9", "displayName": "Owner"}},
		Members: []graph.Item{
			{"id": "u1", "displayName": "Ada"}, {"id": "u2", "displayName": "Grace"},
		},
	}
	m.detailSections = graph.Sections(m.detail)
	m = m.refreshDetail()

	bar := m.renderTabBar()
	for _, want := range []string{"Owners (1)", "Members (2)"} {
		if !strings.Contains(bar, want) {
			t.Errorf("tab bar %q is missing %q", bar, want)
		}
	}
	if w := lipgloss.Width(bar); w > boxInnerWidth(m.width) {
		t.Errorf("tab bar is %d cells, over the %d-cell pane", w, boxInnerWidth(m.width))
	}
}

func TestTabBarFitsANarrowPane(t *testing.T) {
	m := browsing(t)
	m.detail = graph.Detail{
		Kind:    graph.KindGroups,
		Object:  graph.Item{"id": "g1", "displayName": "Research"},
		Owners:  []graph.Item{{"id": "u9", "displayName": "Owner"}},
		Members: []graph.Item{{"id": "u1", "displayName": "Ada"}},
	}
	m.detailSections = graph.Sections(m.detail)
	m = m.refreshDetail()

	for _, width := range []int{20, 30, 40, 200} {
		m.width = width
		if w := lipgloss.Width(m.renderTabBar()); w > boxInnerWidth(width) {
			t.Errorf("at width %d the bar is %d cells, over the %d-cell pane",
				width, w, boxInnerWidth(width))
		}
	}
}

func TestListsDoNotCrowdOutTheProperties(t *testing.T) {
	// A group with hundreds of members must not push the object's own
	// fields off the top of the pane.
	m := browsing(t)
	members := make([]graph.Item, 300)
	for i := range members {
		members[i] = graph.Item{"id": "u" + itoa(i), "displayName": "Member " + itoa(i)}
	}
	m.detail = graph.Detail{
		Kind:    graph.KindGroups,
		Object:  graph.Item{"id": "g1", "displayName": "Everyone", "securityEnabled": true},
		Members: members,
	}
	m.detailSections = graph.Sections(m.detail)
	m = m.refreshDetail()

	body := strings.Join(m.detailBody(), "\n")
	if !strings.Contains(body, "ESSENTIALS") {
		t.Error("the properties were pushed off the pane by the member list")
	}
	if !strings.Contains(body, "Members (300)") {
		t.Error("the tab bar does not report the list size")
	}
	if len(m.detailBody()) > m.contentHeight() {
		t.Errorf("the pane is %d lines, over the %d-line content area",
			len(m.detailBody()), m.contentHeight())
	}
}

// listSection builds a columnar section for the table tests.
func listSection(columns []string, rows ...[]string) graph.Section {
	s := graph.Section{Title: "T", List: true, Columns: columns}
	for _, r := range rows {
		s.Fields = append(s.Fields, graph.Field{Label: r[0], Cells: r})
	}
	return s
}

func TestTabTableIsBorderedWithAHeader(t *testing.T) {
	m := browsing(t)
	m.detailSections = []graph.Section{listSection(
		[]string{"NAME", "TYPE", "MAIL"},
		[]string{"Ada", "User", "ada@x.com"},
		[]string{"Grace", "User", "grace@x.com"},
	)}
	m = m.refreshDetail()

	rows := m.renderTabRows(10)
	if len(rows) < tabChromeHeight {
		t.Fatalf("got %d lines, want at least the table furniture", len(rows))
	}
	if !strings.HasPrefix(rows[0], boxTopLeft) || !strings.Contains(rows[0], "┬") {
		t.Errorf("first line %q is not a bordered rule with column joins", rows[0])
	}
	if !strings.Contains(rows[1], "NAME") || !strings.Contains(rows[1], "MAIL") {
		t.Errorf("second line %q is not the header row", rows[1])
	}
	if !strings.Contains(rows[2], "┼") {
		t.Errorf("third line %q is not the header divider", rows[2])
	}
	if !strings.HasPrefix(rows[len(rows)-1], boxBottomLeft) {
		t.Errorf("last line %q does not close the table", rows[len(rows)-1])
	}
	for i, r := range rows {
		if !strings.Contains(r, boxVertical) && !strings.Contains(r, boxHorizontal) {
			t.Errorf("line %d has no border: %q", i, r)
		}
	}
}

func TestTabTableRowsAllFitThePane(t *testing.T) {
	m := browsing(t)
	m.detailSections = []graph.Section{listSection(
		[]string{"NAME", "TYPE", "DESCRIPTION"},
		[]string{"Ada", "User", strings.Repeat("a very long description ", 20)},
	)}
	m = m.refreshDetail()

	for _, width := range []int{40, 80, 150, 220} {
		m.width = width
		for i, r := range m.renderTabRows(10) {
			if w := lipgloss.Width(r); w > boxInnerWidth(width) {
				t.Errorf("at width %d line %d is %d cells, over the %d-cell pane",
					width, i, w, boxInnerWidth(width))
			}
		}
	}
}

func TestTabColumnSlackIsSharedNotDumped(t *testing.T) {
	// Handing every spare cell to the widest column turned a one-word MAIL
	// column into half the pane.
	columns := []string{"NAME", "TYPE", "MAIL"}
	fields := []graph.Field{{Cells: []string{"Ada Lovelace", "User", "a@x.com"}}}

	widths := tabColumnWidths(columns, fields, 200)
	widest, narrowest := widths[0], widths[0]
	for _, w := range widths {
		widest = max(widest, w)
		narrowest = min(narrowest, w)
	}
	if widest > narrowest*6 {
		t.Errorf("widths %v: one column swallowed the slack", widths)
	}
}

func TestEveryListIsWalkableNotJustEditableOnes(t *testing.T) {
	// Reading a long list is reason enough to move through it, whether or
	// not its rows can be acted on.
	m := browsing(t)
	m.detail = graph.Detail{
		Kind:   graph.KindUsers,
		Object: graph.Item{"id": "u1", "displayName": "Ada"},
		Groups: []graph.Item{
			{"id": "g1", "displayName": "One"},
			{"id": "g2", "displayName": "Two"},
			{"id": "g3", "displayName": "Three"},
		},
	}
	m.screen = screenDetail
	m.detailID = "u1"
	m.detailSections = graph.Sections(m.detail)
	m = m.refreshDetail()

	groups, ok := m.activeSection()
	if !ok || groups.Title != "Groups" {
		t.Fatalf("active section = %+v, want Groups", groups)
	}
	if groups.Relationship != "" {
		t.Skip("group membership became editable")
	}

	m = send(t, m, press("down"))
	if m.tabCursor != 1 {
		t.Errorf("cursor = %d, want a read-only list to still walk", m.tabCursor)
	}
	// ...but there is nothing to remove from it.
	if _, ok := m.selectedEntry(); ok {
		t.Error("a read-only list offered an entry to remove")
	}
}

func TestRawViewCopiesTheWholeObject(t *testing.T) {
	m := browsing(t)
	m.screen = screenDetail
	m.detailID = "u1"
	m.detail = graph.Detail{Kind: graph.KindUsers,
		Object: graph.Item{"id": "u1", "displayName": "Ada"}}
	m.detailSections = graph.Sections(m.detail)
	m = m.refreshDetail()

	// The sectioned view copies the id...
	copied := send(t, m, press("c"))
	if copied.pendingClipboard != "u1" {
		t.Errorf("pendingClipboard = %q, want the object id", copied.pendingClipboard)
	}

	// ...and the raw view copies what it is showing.
	m.detailRaw = true
	copied = send(t, m, press("c"))
	if !strings.Contains(copied.pendingClipboard, `"displayName"`) {
		t.Errorf("pendingClipboard = %q, want the full JSON", copied.pendingClipboard)
	}
	if !strings.Contains(copied.flash, "JSON") {
		t.Errorf("flash = %q, want it to say what was copied", copied.flash)
	}
}

func TestEmptyListKeepsItsTable(t *testing.T) {
	// A group with no owners must look different from one whose owners have
	// not arrived, and the columns are what say so.
	m := browsing(t)
	m.detail = graph.Detail{
		Kind:   graph.KindGroups,
		Object: graph.Item{"id": "g1", "displayName": "Empty"},
	}
	m.screen = screenDetail
	m.detailID = "g1"
	m.detailSections = graph.Sections(m.detail)
	m = m.refreshDetail()

	section, ok := m.activeSection()
	if !ok {
		t.Fatal("a group with no members has no list tabs")
	}
	if len(section.Fields) != 0 {
		t.Fatalf("section %q has %d entries, want an empty one", section.Title, len(section.Fields))
	}

	rows := m.renderTabRows(12)
	if len(rows) < 4 {
		t.Fatalf("empty list drew %d rows, want a full table", len(rows))
	}
	plainFirst := ansiPattern.ReplaceAllString(rows[0], "")
	plainLast := ansiPattern.ReplaceAllString(rows[len(rows)-1], "")
	if !strings.HasPrefix(plainFirst, "┌") || !strings.HasPrefix(plainLast, "└") {
		t.Errorf("empty list is not boxed: first %q, last %q", rows[0], rows[len(rows)-1])
	}
	if !strings.Contains(ansiPattern.ReplaceAllString(rows[1], ""), "NAME") {
		t.Errorf("empty list lost its column titles: %q", rows[1])
	}
	// The note sits inside the box, not loose above it.
	note := ansiPattern.ReplaceAllString(rows[3], "")
	if !strings.HasPrefix(note, boxVertical) || !strings.HasSuffix(note, boxVertical) {
		t.Errorf("the empty note %q is not inside the table", note)
	}
	if plain := ansiPattern.ReplaceAllString(note, ""); strings.TrimSpace(plain) == boxVertical+boxVertical {
		t.Errorf("the empty note row %q says nothing", note)
	}
	// Every row of the table is the same width.
	for i, r := range rows {
		if got, want := lipgloss.Width(r), lipgloss.Width(rows[0]); got != want {
			t.Errorf("row %d is %d cells, want %d", i, got, want)
		}
	}

	// The tab strip counts an empty list rather than going silent.
	if bar := ansiPattern.ReplaceAllString(m.renderTabBar(), ""); !strings.Contains(bar, "(0)") {
		t.Errorf("tab bar %q does not count an empty list", bar)
	}
}

func TestTabTitlesAreNotUnderlined(t *testing.T) {
	// The strip's dividers and the table's border already put each title in
	// a cell; an underline on top of that is one rule too many.
	const underline = "\x1b[4m"
	for name, got := range map[string]string{
		"active tab":   styleTabActive.Render("Owners"),
		"column title": styleTabHead.Render("NAME"),
	} {
		if strings.Contains(got, underline) {
			t.Errorf("%s is underlined: %q", name, got)
		}
	}
}

// ansiPattern strips styling so a test can assert on the characters a reader
// sees rather than on the escape sequences around them.
var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// ------------------------------------------------- links between objects

// linkedGroup is a group whose members can be opened in their own right.
func linkedGroup(t *testing.T) Model {
	t.Helper()
	m := browsing(t)
	groups, _ := graph.Lookup("groups")
	next, _ := m.openResource(groups)
	m = next.(Model)
	m.loading = false
	m.detailRes = groups
	m.detail = graph.Detail{
		Kind:   graph.KindGroups,
		Object: graph.Item{"id": "g1", "displayName": "Research"},
		Members: []graph.Item{
			{"id": "u1", "displayName": "Ada", "@odata.type": "#microsoft.graph.user"},
			{"id": "g2", "displayName": "Nested", "@odata.type": "#microsoft.graph.group"},
		},
	}
	m.screen = screenDetail
	m.detailID = "g1"
	m.detailSections = graph.Sections(m.detail)
	return tabTitled(t, m.refreshDetail(), "Members")
}

func TestEnterOpensTheObjectUnderTheCursor(t *testing.T) {
	m := send(t, linkedGroup(t), press("enter"))

	// The group stays on screen until the member has been read.
	if m.screen != screenDetail || m.detailID != "g1" {
		t.Fatalf("screen = %v id = %q, want the group still shown", m.screen, m.detailID)
	}
	if m.pending.id != "u1" || !m.pending.link {
		t.Fatalf("pending = %+v, want a linked open of u1", m.pending)
	}

	m = send(t, m, detailMsg{gen: m.gen, detail: graph.Detail{
		Kind:   graph.KindUsers,
		Object: graph.Item{"id": "u1", "displayName": "Ada", "department": "Engine"},
	}})
	if m.detailID != "u1" {
		t.Fatalf("detailID = %q, want the member's pane", m.detailID)
	}
	if m.detailRes.Kind != graph.KindUsers {
		t.Errorf("detailRes = %s, want the users view", m.detailRes.Kind)
	}
	if !strings.Contains(mustBody(m), "Engine") {
		t.Error("the member's pane does not show the member")
	}

	// Esc unwinds one link, back to the group and the row it was left on.
	m = send(t, m, press("esc"))
	if m.screen != screenDetail || m.detailID != "g1" {
		t.Fatalf("screen = %v id = %q, want the group back", m.screen, m.detailID)
	}
	if m.detailRes.Kind != graph.KindGroups {
		t.Errorf("detailRes = %s, want the groups view restored", m.detailRes.Kind)
	}
	if section, _ := m.activeSection(); section.Title != "Members" {
		t.Errorf("came back to the %q tab, want Members", section.Title)
	}

	// ...and again leaves the pane entirely.
	m = send(t, m, press("esc"))
	if m.screen != screenBrowse {
		t.Errorf("screen = %v, want the table", m.screen)
	}
}

func TestEnterIgnoresRowsWithNoViewOfTheirOwn(t *testing.T) {
	// An API permission is a row, not an object entra-tui can open.
	m := browsing(t)
	appregs, _ := graph.Lookup("appregs")
	m.detailRes = appregs
	m.detail = graph.Detail{
		Kind:   graph.KindAppRegistrations,
		Object: graph.Item{"id": "a1", "displayName": "App"},
	}
	m.screen = screenDetail
	m.detailID = "a1"
	m.detailSections = graph.Sections(m.detail)
	m = m.refreshDetail()

	before := m
	m = send(t, m, press("enter"))
	if m.detailID != before.detailID || m.pending.id != "" {
		t.Error("enter opened something that has no view")
	}
	if strings.Contains(m.detailHints(), "enter") {
		t.Error("the footer offers enter on a row that cannot be opened")
	}
}

func TestBreadcrumbFollowsTheTrail(t *testing.T) {
	m := linkedGroup(t)
	if got := crumbText(m.breadcrumb()); len(got) != 2 || got[0] != "Groups" || got[1] != "Research" {
		t.Fatalf("breadcrumb = %v, want the view then the object", got)
	}

	m = send(t, m, press("enter"))
	m = send(t, m, detailMsg{gen: m.gen, detail: graph.Detail{
		Kind: graph.KindUsers, Object: graph.Item{"id": "u1", "displayName": "Ada"},
	}})

	got := crumbText(m.breadcrumb())
	want := []string{"Groups", "Research", "Ada"}
	if len(got) != len(want) {
		t.Fatalf("breadcrumb = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("breadcrumb = %v, want %v", got, want)
		}
	}
	if !strings.Contains(m.View(), "Research") {
		t.Error("the trail is not on screen")
	}
}

// crumbText is the trail's steps as plain strings.
func crumbText(trail []crumb) []string {
	out := make([]string, len(trail))
	for i, c := range trail {
		out[i] = c.text
	}
	return out
}

func TestDashboardIsNotAStepInTheTrail(t *testing.T) {
	// It is where you start, so naming it on every screen says nothing. It
	// shows only when it is what you are looking at.
	m := browsing(t)
	m.screen = screenDashboard
	if got := crumbText(m.breadcrumb()); len(got) != 1 || got[0] != "Dashboard" {
		t.Errorf("dashboard trail = %v, want just the dashboard", got)
	}

	m.screen = screenBrowse
	for _, step := range crumbText(m.breadcrumb()) {
		if step == "Dashboard" {
			t.Error("the dashboard is a step in a view's trail")
		}
	}
}

func TestHomeAndEndJumpTheListInFront(t *testing.T) {
	// Same split as paging: the list is the thing that runs past its space,
	// so the ends belong to it.
	m := linkedGroup(t)
	section, _ := m.activeSection()
	if len(section.Fields) < 2 {
		t.Fatalf("the members tab has %d rows, too few to jump", len(section.Fields))
	}

	m = send(t, m, press("G"))
	if m.tabCursor != len(section.Fields)-1 {
		t.Errorf("tabCursor = %d after G, want the last row", m.tabCursor)
	}
	m = send(t, m, press("g"))
	if m.tabCursor != 0 {
		t.Errorf("tabCursor = %d after g, want the first row", m.tabCursor)
	}
}

func TestRawViewScrollsAndBacksOutToTheObject(t *testing.T) {
	m := linkedGroup(t)
	// An object long enough to have somewhere to scroll to.
	for i := range 60 {
		m.detail.Object["property"+itoa(i)] = "value"
	}
	m = send(t, m, press("R"))
	if !m.detailRaw {
		t.Fatal("R did not open the raw view")
	}

	// The keys move the document, not a tab strip that is not on screen.
	before := m.detailVP.YOffset
	m = send(t, m, press("down"))
	if m.detailVP.YOffset == before && m.tabCursor != 0 {
		t.Error("the arrows moved the tab cursor instead of the document")
	}
	m = send(t, m, press("pgdown"))
	if m.detailVP.YOffset == 0 {
		t.Error("the raw view does not scroll")
	}
	if strings.Contains(m.detailHints(), "add") {
		t.Error("the raw view offers keys that act on a list it is not showing")
	}

	// Esc backs out of the raw view, not out of the object.
	m = send(t, m, press("esc"))
	if m.detailRaw {
		t.Error("esc left the raw view open")
	}
	if m.screen != screenDetail || m.detailID != "g1" {
		t.Errorf("screen = %v id = %q, want the object still open", m.screen, m.detailID)
	}
	if m.detailVP.YOffset != 0 {
		t.Errorf("came back scrolled to %d, want the top of the object", m.detailVP.YOffset)
	}
}
