package ui

import "strings"

// quickSearchSlots is how many searches each view keeps. Ten is the ceiling
// because the bar is driven by the digit keys 1-9 and 0.
const quickSearchSlots = 10

// searchHistory remembers recent search terms per view in fixed slots.
//
// Slots are deliberately stable: a new term fills the next free slot and,
// once all are taken, overwrites the oldest one in place. Re-running an
// existing term does not move it. A most-recently-used list would reshuffle
// the bar on every search, so the digit that ran "finance" a moment ago would
// run something else the next time -- which makes the shortcuts unusable from
// memory. Cycling costs nothing and keeps each digit meaning one thing for as
// long as the term lives.
//
// History lives for the life of the process. Nothing is written to disk.
type searchHistory struct {
	slots map[string][]string
	// next is the slot the following new term will claim, per view.
	next map[string]int
}

func newSearchHistory() *searchHistory {
	return &searchHistory{
		slots: map[string][]string{},
		next:  map[string]int{},
	}
}

// record stores term in its view's slots, leaving existing entries where they
// are.
func (h *searchHistory) record(kind, term string) {
	term = strings.TrimSpace(term)
	if term == "" {
		return
	}
	slots := h.ensure(kind)

	// An existing term keeps its slot, so its digit keeps working.
	for _, existing := range slots {
		if strings.EqualFold(existing, term) {
			return
		}
	}

	slots[h.next[kind]] = term
	h.next[kind] = (h.next[kind] + 1) % quickSearchSlots
}

// ensure returns the view's slot array, allocating it on first use.
func (h *searchHistory) ensure(kind string) []string {
	slots, ok := h.slots[kind]
	if !ok {
		slots = make([]string, quickSearchSlots)
		h.slots[kind] = slots
	}
	return slots
}

// slotsFor returns the view's slots in position order. Empty slots are
// returned as empty strings so the caller can render stable positions.
func (h *searchHistory) slotsFor(kind string) []string {
	if slots, ok := h.slots[kind]; ok {
		return slots
	}
	return make([]string, quickSearchSlots)
}

// list returns only the filled slots, in position order, for places that just
// want to show what has been searched.
func (h *searchHistory) list(kind string) []string {
	var out []string
	for _, t := range h.slotsFor(kind) {
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

// at returns the term in the nth slot.
func (h *searchHistory) at(kind string, n int) (string, bool) {
	slots := h.slotsFor(kind)
	if n < 0 || n >= len(slots) || slots[n] == "" {
		return "", false
	}
	return slots[n], true
}
