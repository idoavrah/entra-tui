package ui

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/idoavrah/entra-tui/internal/auth"
	"github.com/idoavrah/entra-tui/internal/graph"
)

// requestTimeout bounds a single Graph round trip from the UI's point of
// view. The client retries internally, so this is the outer budget.
const requestTimeout = 90 * time.Second

// authTimeout bounds an interactive sign-in, which waits on a human.
const authTimeout = 5 * time.Minute

// ------------------------------------------------------------------ messages

// authURLMsg carries the sign-in URL so it can be shown to a user whose
// browser failed to open.
type authURLMsg struct{ url string }

// authDoneMsg reports the outcome of a sign-in attempt.
type authDoneMsg struct {
	attempt  int
	provider auth.Provider
	err      error
}

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

// detailMsg carries the fully expanded object and its follow-up lookups.
type detailMsg struct {
	gen    int
	detail graph.Detail
}

// pairMsg carries the result of an on-demand app registration / enterprise
// app pairing lookup.
type pairMsg struct {
	gen         int
	counterpart *graph.Counterpart
	missing     string
	err         error
}

// principalsMsg carries the objects a membership lookup found.
type principalsMsg struct {
	gen   int
	term  string
	items []graph.Item
	err   error
}

// writeDoneMsg reports the outcome of a membership or ownership change.
type writeDoneMsg struct {
	gen     int
	summary string
	err     error
}

// countMsg carries one view's directory-wide object total.
type countMsg struct {
	kind  graph.Kind
	total int64
	err   error
}

// flashExpiredMsg clears a transient status message.
type flashExpiredMsg struct{ seq int }

// clipboardSentMsg retires a rendered OSC 52 payload.
type clipboardSentMsg struct{}

// ------------------------------------------------------------------- auth

// waitForAuthURL blocks until the interactive flow reports its sign-in URL.
func waitForAuthURL(ch chan string) tea.Cmd {
	return func() tea.Msg {
		return authURLMsg{url: <-ch}
	}
}

// authenticate runs a sign-in in the background.
//
// The browser flow publishes its URL through a channel before launching the
// system browser, so the TUI can display it. Browser launcher output is
// discarded: with the alternate screen buffer active, anything xdg-open
// prints would land in the middle of the frame.
func (m Model) authenticate(method auth.Method) tea.Cmd {
	attempt := m.authAttempt
	ch := m.authURLCh
	opts := m.opts.Auth
	opts.Method = method
	opts.Log = func(string, ...any) {}
	opts.OpenURL = func(url string) error {
		select {
		case ch <- url:
		default: // a URL is already queued; the newest is not more useful
		}
		return auth.OpenInBrowser(url)
	}

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, authTimeout)
		defer cancel()
		provider, err := auth.Resolve(ctx, opts)
		return authDoneMsg{attempt: attempt, provider: provider, err: err}
	}
}

// ------------------------------------------------------------------ paging

// query builds the Graph query for the current view and search term.
func (m *Model) query() graph.Query {
	res := m.coll.res
	q := graph.Query{
		Path:   res.Path,
		Select: res.Select,
		Top:    m.opts.PageSize,
		// Asking for @odata.count is what lets the header say "142 of 3,481"
		// instead of just "142 so far".
		Count: true,
	}
	if m.coll.search != "" {
		q.SearchFields, q.SearchTerm = res.SearchFields, m.coll.search
	}
	return q
}

// loadFirst fetches page one of the current view, replacing loaded data.
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

// ------------------------------------------------------------------ totals

// countTimeout bounds a single $count call. The dashboard is usable without
// its numbers, so a slow tenant should not hold it up for long.
const countTimeout = 20 * time.Second

// loadTotals fetches every view's object count concurrently.
//
// The $count endpoint returns a bare integer for the whole collection, which
// is the only cheap way to size a directory -- paging one to count it would
// cost thousands of requests. Each count stands alone: a view the signed-in
// user cannot enumerate simply shows no number.
func (m *Model) loadTotals() tea.Cmd {
	if m.client == nil {
		return nil
	}
	client := m.client
	ctx := m.ctx

	all := graph.All()
	cmds := make([]tea.Cmd, 0, len(all))
	for _, res := range all {
		cmds = append(cmds, func() tea.Msg {
			reqCtx, cancel := context.WithTimeout(ctx, countTimeout)
			defer cancel()
			total, err := client.Count(reqCtx, res.Path)
			return countMsg{kind: res.Kind, total: total, err: err}
		})
	}
	return tea.Batch(cmds...)
}

// ------------------------------------------------------------------ detail

// loadDetail re-reads an object without a $select projection and gathers the
// follow-up lookups its sections need.
//
// The lookups run concurrently: an app registration needs its owners, its
// permission catalogue and its paired service principal, and doing those in
// series would make opening a detail pane feel slow. Each is independently
// optional -- a failure records itself in the bundle and the section explains
// it, rather than failing the whole view.
func (m *Model) loadDetail(res graph.Resource, id string) tea.Cmd {
	gen := m.gen
	client := m.client
	ctx := m.ctx

	return func() tea.Msg {
		reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
		defer cancel()

		object, err := client.Get(reqCtx, res.Path+"/"+id, nil)
		if err != nil {
			return errMsg{gen: gen, err: err}
		}

		d := graph.Detail{Kind: res.Kind, Object: object}
		appID := object.String("appId")

		var wg sync.WaitGroup
		var mu sync.Mutex

		// Owners exist on applications, service principals and groups alike.
		if res.Kind == graph.KindAppRegistrations || res.Kind == graph.KindEnterpriseApps ||
			res.Kind == graph.KindGroups {
			wg.Add(1)
			go func() {
				defer wg.Done()
				owners, ownersErr := client.Owners(reqCtx, res.Path, id)
				mu.Lock()
				defer mu.Unlock()
				d.Owners, d.OwnersErr = owners, ownersErr
			}()
		}

		if res.Kind == graph.KindDevices {
			wg.Add(1)
			go func() {
				defer wg.Done()
				owners, ownersErr := client.RegisteredOwners(reqCtx, id)
				mu.Lock()
				defer mu.Unlock()
				d.Owners, d.OwnersErr = owners, ownersErr
			}()
		}

		// Both users and devices belong to groups.
		if res.Kind == graph.KindUsers || res.Kind == graph.KindDevices {
			wg.Add(1)
			go func() {
				defer wg.Done()
				groups, truncated, groupsErr := client.MemberOf(reqCtx, res.Path, id)
				mu.Lock()
				defer mu.Unlock()
				d.Groups, d.GroupsTruncated, d.GroupsErr = groups, truncated, groupsErr
			}()
		}

		if res.Kind == graph.KindGroups {
			wg.Add(1)
			go func() {
				defer wg.Done()
				members, truncated, membersErr := client.Members(reqCtx, id)
				mu.Lock()
				defer mu.Unlock()
				d.Members, d.MembersTruncated, d.MembersErr = members, truncated, membersErr
			}()
		}

		if res.Kind == graph.KindAppRegistrations || res.Kind == graph.KindEnterpriseApps {
			wg.Add(1)
			go func() {
				defer wg.Done()
				c := resolveCounterpart(reqCtx, client, res.Kind, appID)

				// An app registration's consent lives on its service
				// principal, which is the counterpart just resolved -- so the
				// grant lookup rides along rather than repeating the search.
				var scopes, roles map[string]bool
				var grantsErr error
				if c != nil && res.Kind == graph.KindAppRegistrations {
					scopes, roles, grantsErr = client.GrantedPermissions(reqCtx, c.ID)
				}

				mu.Lock()
				defer mu.Unlock()
				d.Counterpart = c
				d.GrantedScopes, d.GrantedRoles, d.GrantsErr = scopes, roles, grantsErr
			}()
		}

		if res.Kind == graph.KindAppRegistrations {
			wg.Add(1)
			go func() {
				defer wg.Done()
				resources, permissions := client.ResolvePermissions(reqCtx, object)
				mu.Lock()
				defer mu.Unlock()
				d.ResourceNames, d.Permissions = resources, permissions
			}()
		}

		if res.Kind == graph.KindEnterpriseApps {
			wg.Add(1)
			go func() {
				defer wg.Done()
				assignments, truncated, assignErr := client.AppRoleAssignedTo(reqCtx, id)
				mu.Lock()
				defer mu.Unlock()
				d.Assignments, d.AssignmentsTruncated, d.AssignmentsErr = assignments, truncated, assignErr
			}()
		}

		wg.Wait()
		return detailMsg{gen: gen, detail: d}
	}
}

// loadPair resolves the counterpart on demand, for the case where the eager
// lookup during detail load did not run or did not finish.
func (m *Model) loadPair(kind graph.Kind, appID string) tea.Cmd {
	gen := m.gen
	client := m.client
	ctx := m.ctx

	return func() tea.Msg {
		reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
		defer cancel()

		c := resolveCounterpart(reqCtx, client, kind, appID)
		if c == nil {
			return pairMsg{gen: gen, missing: missingPairMessage(kind)}
		}
		return pairMsg{gen: gen, counterpart: c}
	}
}

// resolveCounterpart looks up the other half of the application /
// service principal pair. A nil result is an ordinary outcome.
func resolveCounterpart(ctx context.Context, client *graph.Client, kind graph.Kind, appID string) *graph.Counterpart {
	if appID == "" {
		return nil
	}
	switch kind {
	case graph.KindAppRegistrations:
		sp, err := client.ServicePrincipalByAppID(ctx, appID)
		if err != nil || sp == nil {
			return nil
		}
		return &graph.Counterpart{
			Kind: graph.KindEnterpriseApps, ID: sp.ID(), DisplayName: sp.String("displayName"),
		}
	case graph.KindEnterpriseApps:
		app, err := client.ApplicationByAppID(ctx, appID)
		if err != nil || app == nil {
			return nil
		}
		return &graph.Counterpart{
			Kind: graph.KindAppRegistrations, ID: app.ID(), DisplayName: app.String("displayName"),
		}
	}
	return nil
}

// missingPairMessage explains why there is nothing to jump to.
func missingPairMessage(kind graph.Kind) string {
	if kind == graph.KindEnterpriseApps {
		return "no app registration in this tenant — the app is published by another organisation"
	}
	return "no enterprise application — this registration has no service principal here"
}

// ------------------------------------------------------------------ writes

// writeTimeout bounds a single mutating request. It is not retried: a POST
// that may or may not have been applied should be reported, not repeated.
const writeTimeout = 30 * time.Second

// findPrincipal looks up who the user means before anything is written.
func (m *Model) findPrincipal(rel graph.Relationship, term string) tea.Cmd {
	gen := m.gen
	client := m.client
	ctx := m.ctx

	return func() tea.Msg {
		reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
		defer cancel()
		items, err := client.FindPrincipals(reqCtx, rel, term)
		return principalsMsg{gen: gen, term: term, items: items, err: err}
	}
}

// applyMembershipChange performs the confirmed add or removal.
func (m *Model) applyMembershipChange() tea.Cmd {
	gen := m.gen
	client := m.client
	ctx := m.ctx
	path := m.coll.res.Path
	objectID := m.detailID
	rel := m.modalRel
	action := m.modalAction

	// The summary names the object the same way the confirmation did, id
	// included, so the record of what happened is as unambiguous as the
	// question that authorised it.
	principalID, who := m.modalEntry.id, m.modalEntry.name
	if action == actionAdd {
		principalID = m.modalTarget.ID()
		who = m.modalTarget.String("displayName")
	}
	who = identify(who, principalID)

	return func() tea.Msg {
		reqCtx, cancel := context.WithTimeout(ctx, writeTimeout)
		defer cancel()

		var err error
		var summary string
		if action == actionAdd {
			err = client.AddRef(reqCtx, path, objectID, rel, principalID)
			summary = fmt.Sprintf("added %s as %s", who, rel.Label())
		} else {
			err = client.RemoveRef(reqCtx, path, objectID, rel, principalID)
			summary = fmt.Sprintf("removed %s as %s", who, rel.Label())
		}
		return writeDoneMsg{gen: gen, summary: summary, err: err}
	}
}

// ------------------------------------------------------------------ chrome

// flashFor shows a transient status message for a fixed duration.
func (m *Model) flashFor(text string) tea.Cmd {
	m.flash = text
	m.flashSeq++
	seq := m.flashSeq
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg {
		return flashExpiredMsg{seq: seq}
	})
}
