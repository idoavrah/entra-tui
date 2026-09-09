package ui

import (
	"strings"

	"github.com/idoavrah/entra-tui/internal/graph"
)

// collection holds the pages loaded so far for one resource, along with the
// local filter applied on top of them.
//
// Cells are rendered once at load time rather than on every frame: a tenant
// with thousands of loaded objects would otherwise re-run every column
// accessor 60 times a second just to scroll.
type collection struct {
	res   graph.Resource
	items []graph.Item
	rows  [][]string
	// view indexes items that pass the current filter, in display order.
	view []int

	nextLink string
	// total is @odata.count for the server-side query, or -1 when unknown.
	total int64
	// advanced records whether the active query needs the "eventual"
	// consistency level, which must persist across nextLink follows.
	advanced bool

	filter string
	search string
}

func newCollection(res graph.Resource) *collection {
	return &collection{res: res, total: -1}
}

// appendPage adds a freshly fetched page and refreshes the filtered view.
func (c *collection) appendPage(p *graph.Page) {
	for _, it := range p.Items {
		c.items = append(c.items, it)
		c.rows = append(c.rows, c.res.Row(it))
	}
	c.nextLink = p.NextLink
	if p.TotalCount >= 0 {
		c.total = p.TotalCount
	}
	c.refilter()
}

// hasMore reports whether Graph offered another page.
func (c *collection) hasMore() bool { return c.nextLink != "" }

// loaded is the number of objects fetched so far, ignoring the filter.
func (c *collection) loaded() int { return len(c.items) }

// len is the number of rows currently visible.
func (c *collection) len() int { return len(c.view) }

// at returns the item and its rendered cells for a visible row index.
func (c *collection) at(i int) (graph.Item, []string, bool) {
	if i < 0 || i >= len(c.view) {
		return nil, nil, false
	}
	n := c.view[i]
	return c.items[n], c.rows[n], true
}

// setFilter replaces the local filter expression.
func (c *collection) setFilter(f string) {
	c.filter = f
	c.refilter()
}

// refilter recomputes the visible row set.
func (c *collection) refilter() {
	terms := parseFilter(c.filter)
	if len(terms) == 0 {
		c.view = c.view[:0]
		for i := range c.items {
			c.view = append(c.view, i)
		}
		return
	}
	c.view = c.view[:0]
	for i, row := range c.rows {
		if matches(row, terms) {
			c.view = append(c.view, i)
		}
	}
}

// filterTerm is one space-separated token of a filter expression.
type filterTerm struct {
	text   string
	negate bool
}

// parseFilter splits a filter into lowercase terms. Terms are ANDed, and a
// leading "!" negates one, so `admin !guest` reads as "contains admin and
// does not contain guest".
func parseFilter(s string) []filterTerm {
	fields := strings.Fields(strings.ToLower(s))
	terms := make([]filterTerm, 0, len(fields))
	for _, f := range fields {
		t := filterTerm{text: f}
		if strings.HasPrefix(f, "!") {
			t.negate = true
			t.text = strings.TrimPrefix(f, "!")
		}
		if t.text == "" {
			continue
		}
		terms = append(terms, t)
	}
	return terms
}

// matches reports whether a rendered row satisfies every term. Matching runs
// across the whole row rather than per column, so typing a department name
// finds it without the user having to know which column it lives in.
func matches(row []string, terms []filterTerm) bool {
	haystack := strings.ToLower(strings.Join(row, " "))
	for _, t := range terms {
		hit := strings.Contains(haystack, t.text)
		if hit == t.negate {
			return false
		}
	}
	return true
}
