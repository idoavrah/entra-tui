package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// quickSearchSlots is how many searches each view keeps. Ten is the ceiling
// because the bar is driven by the digit keys 1-9 and 0.
const quickSearchSlots = 10

// historyFileVersion guards the on-disk format. A file written by a future
// version is ignored rather than misread.
const historyFileVersion = 1

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
// The slots are cached on disk so they survive a restart. Persistence is
// best-effort: this is a convenience, and no failure to read or write it is
// worth interrupting the session for.
type searchHistory struct {
	slots map[string][]string
	// next is the slot the following new term will claim, per view.
	next map[string]int
	// path is where the cache lives, empty when it could not be located.
	path string
}

// historyFile is the on-disk shape.
type historyFile struct {
	Version int                 `json:"version"`
	Slots   map[string][]string `json:"slots"`
	Next    map[string]int      `json:"next"`
}

func newSearchHistory() *searchHistory {
	return &searchHistory{
		slots: map[string][]string{},
		next:  map[string]int{},
	}
}

// loadSearchHistory reads the cached slots from a cache directory. An empty
// dir means the user's own cache directory.
func loadSearchHistory(dir string) *searchHistory {
	return loadHistoryFrom(historyPath(dir))
}

// loadHistoryFrom reads a specific cache file, returning an empty history
// when there is nothing to read or the file cannot be understood.
func loadHistoryFrom(path string) *searchHistory {
	h := newSearchHistory()
	h.path = path
	if h.path == "" {
		return h
	}

	data, err := os.ReadFile(h.path)
	if err != nil {
		return h
	}
	var file historyFile
	if err := json.Unmarshal(data, &file); err != nil || file.Version != historyFileVersion {
		return h
	}

	for kind, slots := range file.Slots {
		// Normalise to the current slot count: a cache written when the
		// ceiling was different must not produce short or over-long rows.
		fixed := make([]string, quickSearchSlots)
		copy(fixed, slots)
		h.slots[kind] = fixed
	}
	for kind, n := range file.Next {
		if n >= 0 && n < quickSearchSlots {
			h.next[kind] = n
		}
	}
	return h
}

// historyPath is the cache file's location, or empty if no cache directory
// can be determined. Passing a directory is what keeps tests off the
// developer's real cache.
func historyPath(dir string) string {
	if dir == "" {
		var err error
		if dir, err = os.UserCacheDir(); err != nil {
			return ""
		}
	}
	return filepath.Join(dir, "entra-tui", "searches.json")
}

// save writes the slots back to disk, ignoring any failure.
//
// The write goes to a temporary file and is renamed into place, so an
// interrupted run leaves the previous cache intact rather than a truncated
// one that the next start would discard.
func (h *searchHistory) save() {
	if h.path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(h.path), 0o755); err != nil {
		return
	}

	data, err := json.Marshal(historyFile{
		Version: historyFileVersion,
		Slots:   h.slots,
		Next:    h.next,
	})
	if err != nil {
		return
	}

	tmp, err := os.CreateTemp(filepath.Dir(h.path), "searches-*.json")
	if err != nil {
		return
	}
	name := tmp.Name()
	// Search terms can name people; keep the cache to the owner.
	if _, err := tmp.Write(data); err != nil || tmp.Chmod(0o600) != nil || tmp.Close() != nil {
		_ = os.Remove(name)
		return
	}
	if err := os.Rename(name, h.path); err != nil {
		_ = os.Remove(name)
	}
}

// record stores term in its view's slots, leaving existing entries where they
// are, and persists the result.
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
	h.save()
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
