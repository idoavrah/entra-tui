package ui

import "strings"

// maxQuickSearches is how many recent searches are offered per view. Ten is
// the ceiling because the quick-search bar is driven by the digit keys 1-9
// and 0.
const maxQuickSearches = 10

// searchHistory remembers recent search terms per resource, most recent
// first. Searches are scoped to the view they were run in: a term that
// finds a person is rarely the term that finds an app registration.
//
// History lives for the life of the process. Nothing is written to disk.
type searchHistory struct {
	byKind map[string][]string
}

func newSearchHistory() *searchHistory {
	return &searchHistory{byKind: map[string][]string{}}
}

// record moves term to the front of its view's list, de-duplicating
// case-insensitively so repeating a search does not fill the bar with it.
func (h *searchHistory) record(kind, term string) {
	term = strings.TrimSpace(term)
	if term == "" {
		return
	}
	existing := h.byKind[kind]
	out := make([]string, 0, len(existing)+1)
	out = append(out, term)
	for _, t := range existing {
		if strings.EqualFold(t, term) {
			continue
		}
		out = append(out, t)
		if len(out) == maxQuickSearches {
			break
		}
	}
	h.byKind[kind] = out
}

// list returns the recent searches for a view, most recent first.
func (h *searchHistory) list(kind string) []string {
	return h.byKind[kind]
}

// at returns the nth recent search for a view.
func (h *searchHistory) at(kind string, n int) (string, bool) {
	list := h.byKind[kind]
	if n < 0 || n >= len(list) {
		return "", false
	}
	return list[n], true
}
