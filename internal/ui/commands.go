package ui

import (
	"context"
	"errors"
	"net/http"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/idoavrah/entra-tui/internal/graph"
)

// requestTimeout bounds a single Graph round trip from the UI's point of
// view. The client retries internally, so this is the outer budget.
const requestTimeout = 90 * time.Second

// pageMsg carries a fetched page back to the update loop.
type pageMsg struct {
	gen  int
	page *graph.Page
	// append distinguishes a nextLink follow from a fresh first page.
	append bool
	// advanced reports whether the query that produced this page needed the
	// "eventual" consistency level. Following its nextLink must repeat that
	// header, so it travels with the page.
	advanced bool
}

// errMsg carries a failed request back to the update loop.
type errMsg struct {
	gen int
	err error
}

// detailMsg carries the fully expanded object for the detail pane.
type detailMsg struct {
	gen  int
	id   string
	item graph.Item
}

// flashExpiredMsg clears a transient status message.
type flashExpiredMsg struct{ seq int }

// query builds the Graph query for the current resource, filter and search.
func (m *Model) query() graph.Query {
	res := m.coll.res
	q := graph.Query{
		Path:    res.Path,
		Select:  res.Select,
		OrderBy: res.OrderBy,
		Top:     m.pageSize,
		// Asking for @odata.count is what lets the status bar say "142 of
		// 3,481" instead of just "142 so far".
		Count: true,
	}
	if m.coll.search != "" {
		q.Search = res.SearchExpr(m.coll.search)
	}
	return q
}

// loadFirst fetches page one of the current resource, replacing any loaded data.
func (m *Model) loadFirst() tea.Cmd {
	gen := m.gen
	q := m.query()
	client := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, requestTimeout)
		defer cancel()
		page, effective, err := listWithFallback(ctx, client, q)
		if err != nil {
			return errMsg{gen: gen, err: err}
		}
		return pageMsg{gen: gen, page: page, advanced: effective.NeedsAdvancedQuery()}
	}
}

// loadNext follows the current @odata.nextLink.
func (m *Model) loadNext() tea.Cmd {
	gen := m.gen
	link := m.coll.nextLink
	advanced := m.coll.advanced
	client := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, requestTimeout)
		defer cancel()
		page, err := client.Next(ctx, link, advanced)
		if err != nil {
			return errMsg{gen: gen, err: err}
		}
		return pageMsg{gen: gen, page: page, append: true, advanced: advanced}
	}
}

// loadDetail fetches an object without a $select projection, so the detail
// pane can show the full default property set rather than only the handful
// of fields the table needed.
func (m *Model) loadDetail(id string) tea.Cmd {
	gen := m.gen
	path := m.coll.res.Path + "/" + id
	client := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, requestTimeout)
		defer cancel()
		item, err := client.Get(ctx, path, nil)
		if err != nil {
			return errMsg{gen: gen, err: err}
		}
		return detailMsg{gen: gen, id: id, item: item}
	}
}

// listWithFallback runs a query, degrading it if Graph rejects the request as
// malformed.
//
// Advanced query options are not uniformly supported across directory
// collections or tenant configurations: $count can be refused, and $orderby
// is rejected outright in combination with some filters. Rather than
// hard-coding which tenant supports what, the richest query is attempted
// first and options are dropped one at a time on a 400. Only 400 triggers a
// retry -- a 403 means missing consent and no amount of degrading will fix
// it, so it surfaces immediately.
func listWithFallback(ctx context.Context, client *graph.Client, q graph.Query) (*graph.Page, graph.Query, error) {
	attempts := []graph.Query{q}
	if q.Count {
		degraded := q
		degraded.Count = false
		attempts = append(attempts, degraded)
	}
	if q.OrderBy != "" {
		degraded := attempts[len(attempts)-1]
		degraded.OrderBy = ""
		attempts = append(attempts, degraded)
	}

	var lastErr error
	for _, attempt := range attempts {
		page, err := client.List(ctx, attempt)
		if err == nil {
			return page, attempt, nil
		}
		lastErr = err
		if !isBadRequest(err) {
			break
		}
	}
	return nil, q, lastErr
}

// isBadRequest reports whether Graph rejected the query itself.
func isBadRequest(err error) bool {
	var api *graph.APIError
	return errors.As(err, &api) && api.Status == http.StatusBadRequest
}

// flashFor shows a transient status message for a fixed duration.
func (m *Model) flashFor(text string) tea.Cmd {
	m.flash = text
	m.flashSeq++
	seq := m.flashSeq
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg {
		return flashExpiredMsg{seq: seq}
	})
}
