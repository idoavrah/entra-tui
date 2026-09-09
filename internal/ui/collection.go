package ui

import (
	"sort"
	"strings"

	"github.com/idoavrah/entra-tui/internal/graph"
)

// entry pairs an object with its rendered cells so the two cannot drift
// apart when the collection is re-sorted.
type entry struct {
	item  graph.Item
	cells []string
	// sortKey is the lowercased display name used for ordering.
	sortKey string
}

// collection holds the pages loaded so far for one view, kept sorted by name.
//
// Cells are rendered once at load time rather than per frame: a tenant with
// thousands of loaded objects would otherwise re-run every column accessor
// sixty times a second just to scroll.
type collection struct {
	res     entryResource
	entries []entry

	nextLink string
	// total is @odata.count for the server-side query, or -1 when unknown.
	total int64
	// advanced records whether the active query needs the "eventual"
	// consistency level, which must persist across nextLink follows.
	advanced bool

	// search is the server-side search term currently narrowing this view.
	search string
}

// entryResource is the subset of graph.Resource a collection needs. Naming it
// separately keeps the sort key definition next to the data it orders.
type entryResource = graph.Resource

func newCollection(res graph.Resource) *collection {
	return &collection{res: res, total: -1}
}

// appendPage adds a freshly fetched page and restores name ordering.
func (c *collection) appendPage(p *graph.Page) {
	for _, it := range p.Items {
		c.entries = append(c.entries, entry{
			item:    it,
			cells:   c.res.Row(it),
			sortKey: sortKeyFor(it),
		})
	}
	c.nextLink = p.NextLink
	if p.TotalCount >= 0 {
		c.total = p.TotalCount
	}
	c.sort()
}

// sort orders a searched collection by display name.
//
// Only a searched one. Graph refuses $orderby alongside $search, so results
// would otherwise arrive in relevance order, which is not an order anyone can
// scan; a handful of matches is worth sorting client-side. An unfiltered view
// is left in the order the directory returned it: sorting it would mean
// re-ordering the rows already on screen every time a page arrives, moving
// the row under the cursor while it is being read.
func (c *collection) sort() {
	if c.search == "" {
		return
	}
	sort.SliceStable(c.entries, func(i, j int) bool {
		if c.entries[i].sortKey != c.entries[j].sortKey {
			return c.entries[i].sortKey < c.entries[j].sortKey
		}
		// Fall back to the id so ordering is total and stable across reloads.
		return c.entries[i].item.ID() < c.entries[j].item.ID()
	})
}

// sortKeyFor picks the name to order an object by, falling back through the
// identifiers a directory object might carry instead of a display name.
func sortKeyFor(it graph.Item) string {
	for _, key := range []string{"displayName", "userPrincipalName", "mail", "appId"} {
		if v := strings.TrimSpace(it.String(key)); v != "" {
			return strings.ToLower(v)
		}
	}
	return strings.ToLower(it.ID())
}

// hasMore reports whether Graph offered another page.
func (c *collection) hasMore() bool { return c.nextLink != "" }

// len is the number of rows loaded.
func (c *collection) len() int { return len(c.entries) }

// at returns the item and its rendered cells for a row index.
func (c *collection) at(i int) (graph.Item, []string, bool) {
	if i < 0 || i >= len(c.entries) {
		return nil, nil, false
	}
	return c.entries[i].item, c.entries[i].cells, true
}

// indexOf finds the row holding the given object id, or -1.
//
// Re-sorting after a page arrives can move the selected row, so the cursor is
// restored by identity rather than by position.
func (c *collection) indexOf(id string) int {
	if id == "" {
		return -1
	}
	for i := range c.entries {
		if c.entries[i].item.ID() == id {
			return i
		}
	}
	return -1
}
