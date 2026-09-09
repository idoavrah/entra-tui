package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
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

	slotAt := strings.Index(line, "[1]")
	logoAt := strings.Index(line, logo[0])
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

	if strings.Contains(m.renderHeader(), "[1]") {
		t.Error("the grid is drawn in a terminal with no room for it")
	}
}
