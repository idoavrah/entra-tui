package ui

import "testing"

func TestSlotsFillInOrder(t *testing.T) {
	h := newSearchHistory()
	h.record("users", "ada")
	h.record("users", "grace")

	slots := h.slotsFor("users")
	if slots[0] != "ada" || slots[1] != "grace" {
		t.Errorf("slots = %v, want ada then grace in the first two positions", slots[:2])
	}
}

func TestExistingTermKeepsItsSlot(t *testing.T) {
	// The whole point of fixed slots: a digit must keep meaning the same
	// search, so re-running one must not promote it.
	h := newSearchHistory()
	h.record("users", "ada")
	h.record("users", "grace")
	h.record("users", "ada")

	slots := h.slotsFor("users")
	if slots[0] != "ada" || slots[1] != "grace" {
		t.Errorf("slots = %v, want the order unchanged by the repeat", slots[:2])
	}
	if slots[2] != "" {
		t.Errorf("slots[2] = %q, want the repeat not to consume a new slot", slots[2])
	}
}

func TestRepeatIsCaseInsensitive(t *testing.T) {
	h := newSearchHistory()
	h.record("users", "Ada")
	h.record("users", "ADA")

	if got := len(h.list("users")); got != 1 {
		t.Errorf("filled slots = %d, want the case variant treated as the same term", got)
	}
	if h.slotsFor("users")[0] != "Ada" {
		t.Errorf("slot 0 = %q, want the original spelling kept", h.slotsFor("users")[0])
	}
}

func TestSlotsCycleWhenFull(t *testing.T) {
	h := newSearchHistory()
	for i := range quickSearchSlots {
		h.record("users", string(rune('a'+i)))
	}
	// The eleventh term overwrites the oldest slot, in place.
	h.record("users", "new")

	slots := h.slotsFor("users")
	if slots[0] != "new" {
		t.Errorf("slots[0] = %q, want the oldest slot recycled", slots[0])
	}
	if slots[1] != "b" {
		t.Errorf("slots[1] = %q, want every other slot undisturbed", slots[1])
	}
	if len(h.list("users")) != quickSearchSlots {
		t.Errorf("filled = %d, want the list to stay at capacity", len(h.list("users")))
	}

	// And it keeps going round rather than stopping at the first slot.
	h.record("users", "newer")
	if got := h.slotsFor("users")[1]; got != "newer" {
		t.Errorf("slots[1] = %q, want the cycle to advance", got)
	}
}

func TestBlankTermsAreIgnored(t *testing.T) {
	h := newSearchHistory()
	h.record("users", "   ")
	h.record("users", "")
	if got := h.list("users"); len(got) != 0 {
		t.Errorf("list = %v, want blank searches ignored", got)
	}
}

func TestSlotsArePerView(t *testing.T) {
	h := newSearchHistory()
	h.record("users", "ada")
	h.record("groups", "finance")

	if got := h.list("users"); len(got) != 1 || got[0] != "ada" {
		t.Errorf("users = %v", got)
	}
	if got := h.list("groups"); len(got) != 1 || got[0] != "finance" {
		t.Errorf("groups = %v", got)
	}
	if got := h.list("applications"); len(got) != 0 {
		t.Errorf("untouched view = %v, want empty", got)
	}
}

func TestAtSkipsEmptySlots(t *testing.T) {
	h := newSearchHistory()
	h.record("users", "ada")

	if term, ok := h.at("users", 0); !ok || term != "ada" {
		t.Errorf("at(0) = %q,%v", term, ok)
	}
	if _, ok := h.at("users", 1); ok {
		t.Error("at(1) returned ok for an empty slot")
	}
	if _, ok := h.at("users", -1); ok {
		t.Error("at(-1) returned ok")
	}
	if _, ok := h.at("users", quickSearchSlots); ok {
		t.Error("at(out of range) returned ok")
	}
	if _, ok := h.at("groups", 0); ok {
		t.Error("at on an untouched view returned ok")
	}
}

func TestSlotsForIsAlwaysFullLength(t *testing.T) {
	// Rendering depends on a stable slot count even before anything is stored.
	if got := len(newSearchHistory().slotsFor("users")); got != quickSearchSlots {
		t.Errorf("slotsFor = %d entries, want %d", got, quickSearchSlots)
	}
}
