package ui

import "testing"

func TestHistoryKeepsMostRecentFirst(t *testing.T) {
	h := newSearchHistory()
	h.record("users", "ada")
	h.record("users", "grace")

	got := h.list("users")
	if len(got) != 2 || got[0] != "grace" || got[1] != "ada" {
		t.Errorf("history = %v, want [grace ada]", got)
	}
}

func TestHistoryDeduplicatesCaseInsensitively(t *testing.T) {
	h := newSearchHistory()
	h.record("users", "Ada")
	h.record("users", "grace")
	h.record("users", "ADA")

	got := h.list("users")
	if len(got) != 2 {
		t.Fatalf("history = %v, want 2 entries", got)
	}
	if got[0] != "ADA" {
		t.Errorf("history[0] = %q, want the re-run term moved to the front", got[0])
	}
}

func TestHistoryIsCappedAtTheDigitKeys(t *testing.T) {
	h := newSearchHistory()
	for i := range 25 {
		h.record("users", string(rune('a'+i)))
	}
	if got := len(h.list("users")); got != maxQuickSearches {
		t.Errorf("history length = %d, want %d (one per digit key)", got, maxQuickSearches)
	}
}

func TestHistoryIgnoresBlankTerms(t *testing.T) {
	h := newSearchHistory()
	h.record("users", "   ")
	h.record("users", "")
	if got := h.list("users"); len(got) != 0 {
		t.Errorf("history = %v, want blank searches ignored", got)
	}
}

func TestHistoryIsPerView(t *testing.T) {
	h := newSearchHistory()
	h.record("users", "ada")
	h.record("groups", "finance")

	if got := h.list("users"); len(got) != 1 || got[0] != "ada" {
		t.Errorf("users history = %v", got)
	}
	if got := h.list("groups"); len(got) != 1 || got[0] != "finance" {
		t.Errorf("groups history = %v", got)
	}
	if got := h.list("applications"); len(got) != 0 {
		t.Errorf("untouched view history = %v, want empty", got)
	}
}

func TestHistoryAtBoundsCheck(t *testing.T) {
	h := newSearchHistory()
	h.record("users", "ada")

	if term, ok := h.at("users", 0); !ok || term != "ada" {
		t.Errorf("at(0) = %q,%v", term, ok)
	}
	if _, ok := h.at("users", 1); ok {
		t.Error("at(1) returned ok for a single-entry history")
	}
	if _, ok := h.at("users", -1); ok {
		t.Error("at(-1) returned ok")
	}
	if _, ok := h.at("groups", 0); ok {
		t.Error("at on an empty view returned ok")
	}
}
