package ui

import (
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
