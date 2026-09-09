package ui

import (
	"strings"
	"testing"

	"github.com/idoavrah/entra-tui/internal/graph"
)

// hebrewOwner is a Hebrew personal name used to check right-to-left handling.
const hebrewOwner = "שרה כהן"

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
	out := renderField(graph.Field{Label: hebrewOwner, Value: "he.user@x.com"}, 26, 60)

	if !strings.Contains(out, reverseRunes(hebrewOwner)) {
		t.Errorf("rendered field %q does not contain the reordered label", out)
	}
	if strings.Contains(out, hebrewOwner) {
		t.Error("the label was left in logical order, which reads backwards in a terminal")
	}
}

func TestFieldValuesGetRTLTreatment(t *testing.T) {
	out := renderField(graph.Field{Label: "Display name", Value: hebrewOwner}, 26, 60)
	if !strings.Contains(out, reverseRunes(hebrewOwner)) {
		t.Errorf("rendered field %q does not contain the reordered value", out)
	}
}

func TestFieldLeavesLatinAlone(t *testing.T) {
	out := renderField(graph.Field{Label: "Display name", Value: "Ada Lovelace"}, 26, 60)
	if !strings.Contains(out, "Ada Lovelace") {
		t.Errorf("rendered field %q lost its Latin value", out)
	}
}

func TestMultiValuedFieldListsOnePerLine(t *testing.T) {
	out := renderField(graph.Field{
		Label:  "Web redirect URIs",
		Values: []string{"https://a/cb", "https://b/cb"},
	}, 26, 60)

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want one per value:\n%s", len(lines), out)
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

func TestDetailBodyRendersSectionHeadings(t *testing.T) {
	m := browsing(t)
	m.detail = graph.Detail{
		Kind:   graph.KindAppRegistrations,
		Object: graph.Item{"id": "a1", "displayName": "Contoso", "appId": "guid"},
	}
	m.detailSections = graph.Sections(m.detail)

	body := m.renderDetailBody()
	if !strings.Contains(body, "ESSENTIALS") {
		t.Error("detail body has no Essentials heading")
	}
	if !strings.Contains(body, "Application (client) ID") {
		t.Error("detail body is missing the app id field")
	}
}

func TestDetailBodyShowsSectionNotes(t *testing.T) {
	m := browsing(t)
	m.detail = graph.Detail{
		Kind:   graph.KindEnterpriseApps,
		Object: graph.Item{"id": "sp1", "displayName": "Contoso"},
	}
	m.detailSections = graph.Sections(m.detail)

	body := m.renderDetailBody()
	if !strings.Contains(body, "No users or groups are assigned") {
		t.Error("an empty section should explain itself rather than render blank")
	}
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
