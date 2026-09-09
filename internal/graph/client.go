package graph

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/idoavrah/entra-tui/internal/auth"
)

// DefaultBaseURL is the Graph v1.0 endpoint. Beta is intentionally not used:
// v1.0 is the only surface with stability guarantees.
const DefaultBaseURL = "https://graph.microsoft.com/v1.0"

// DefaultPageSize is the server-side page size requested via $top. Graph caps
// directory collections at 999; 100 keeps the first paint fast while still
// filling a tall terminal in one round trip.
const DefaultPageSize = 100

const (
	// maxPropertyRetries bounds how many rejected properties are dropped
	// before a query is given up on.
	maxPropertyRetries = 6

	maxRetries      = 3
	maxRetryBackoff = 20 * time.Second
	// maxBodyBytes bounds how much of a response is read, so a pathological
	// reply cannot exhaust memory.
	maxBodyBytes = 32 << 20
)

// Client performs read-only Graph requests on behalf of the signed-in user.
type Client struct {
	tokens  auth.Provider
	http    *http.Client
	baseURL string
	// UserAgent identifies the tool in Graph telemetry, which helps tenant
	// admins attribute traffic.
	userAgent string

	// unsupported remembers properties this tenant has rejected, so a
	// property that is absent here is only ever paid for once.
	mu          sync.RWMutex
	unsupported map[string]bool
}

// Option customises a Client.
type Option func(*Client)

// WithBaseURL overrides the Graph endpoint, e.g. for a sovereign cloud.
func WithBaseURL(u string) Option {
	return func(c *Client) {
		if u != "" {
			c.baseURL = strings.TrimRight(u, "/")
		}
	}
}

// WithHTTPClient overrides the underlying HTTP client, mainly for tests.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) {
		if h != nil {
			c.http = h
		}
	}
}

// New builds a Graph client that draws bearer tokens from tokens.
func New(tokens auth.Provider, opts ...Option) *Client {
	c := &Client{
		tokens:      tokens,
		http:        &http.Client{Timeout: 60 * time.Second},
		baseURL:     DefaultBaseURL,
		userAgent:   "entra-tui",
		unsupported: map[string]bool{},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Query describes a single collection request.
type Query struct {
	// Path is the collection path relative to the base URL, e.g. "/users".
	Path string
	// Select limits returned properties. Graph returns a small default set
	// otherwise, so this is how the table gets the columns it needs.
	Select []string
	// Filter is a raw OData $filter expression.
	Filter string
	// SearchFields and SearchTerm drive $search. They are kept apart rather
	// than pre-composed so the client can drop a field the tenant rejects
	// without having to unpick a built expression.
	SearchFields []string
	SearchTerm   string
	// OrderBy is a raw $orderby expression.
	OrderBy string
	// Top is the requested page size; DefaultPageSize when zero.
	Top int
	// Count asks Graph for the total collection size via @odata.count.
	Count bool
}

// NeedsAdvancedQuery reports whether the request requires the "eventual"
// consistency level. Directory objects reject $search and $count on the
// default (session) consistency level.
func (q Query) NeedsAdvancedQuery() bool {
	return q.searching() || q.Count
}

// searching reports whether the query carries a usable $search.
func (q Query) searching() bool {
	return strings.TrimSpace(q.SearchTerm) != "" && len(q.SearchFields) > 0
}

// searchExpr renders the search into Graph's syntax across SearchFields,
// e.g. `"displayName:ada" OR "mail:ada"`.
//
// Double quotes and backslashes are stripped rather than escaped: Graph's
// search grammar has no escape sequence for them inside a quoted term, so
// removing them is the only way to keep a user's stray quote from producing
// a malformed query.
func (q Query) searchExpr() string {
	term := strings.TrimSpace(strings.NewReplacer(`"`, "", `\`, "").Replace(q.SearchTerm))
	if term == "" || len(q.SearchFields) == 0 {
		return ""
	}
	parts := make([]string, 0, len(q.SearchFields))
	for _, f := range q.SearchFields {
		parts = append(parts, fmt.Sprintf("%q", f+":"+term))
	}
	return strings.Join(parts, " OR ")
}

// Page is one page of a collection.
type Page struct {
	Items []Item
	// NextLink is the opaque @odata.nextLink URL, empty on the last page.
	NextLink string
	// TotalCount is @odata.count when requested, or -1 when Graph did not
	// report one. Graph reports the size of the *filtered* collection.
	TotalCount int64
}

// collectionResponse mirrors an OData collection payload.
type collectionResponse struct {
	Value    []Item `json:"value"`
	NextLink string `json:"@odata.nextLink"`
	Count    *int64 `json:"@odata.count"`
}

// buildURL renders a Query into an absolute Graph URL.
func (c *Client) buildURL(q Query) string {
	params := url.Values{}
	if len(q.Select) > 0 {
		params.Set("$select", strings.Join(q.Select, ","))
	}
	if q.Filter != "" {
		params.Set("$filter", q.Filter)
	}
	if expr := q.searchExpr(); expr != "" {
		params.Set("$search", expr)
	}
	// Graph rejects $orderby combined with $search on directory collections,
	// so relevance ordering wins whenever a search is active.
	if q.OrderBy != "" && !q.searching() {
		params.Set("$orderby", q.OrderBy)
	}
	if q.Count {
		params.Set("$count", "true")
	}
	top := q.Top
	if top <= 0 {
		top = DefaultPageSize
	}
	params.Set("$top", strconv.Itoa(top))

	return c.baseURL + "/" + strings.TrimLeft(q.Path, "/") + "?" + params.Encode()
}

// List fetches the first page of a collection.
//
// Graph rejects a request outright when it names a property the tenant does
// not have, and which properties exist varies with tenant configuration and
// with the collection being queried -- servicePrincipal's publisherName is
// one that some tenants refuse. Rather than hard-coding a lowest common
// denominator, the richest query is sent, the rejected property is read out
// of the error, and the request is retried without it. The property is
// remembered, so the cost is one extra round trip per property per session.
func (c *Client) List(ctx context.Context, q Query) (*Page, error) {
	for range maxPropertyRetries {
		pruned := c.prune(q)
		page, err := c.fetchPage(ctx, c.buildURL(pruned), pruned.NeedsAdvancedQuery())
		if err == nil {
			return page, nil
		}

		property, ok := unsupportedProperty(err)
		if !ok || !pruned.mentions(property) {
			// Either not a property complaint, or about something this query
			// does not ask for -- dropping it again would loop forever.
			return nil, err
		}
		c.markUnsupported(property)
	}
	return nil, fmt.Errorf("query still rejected after dropping %d unsupported properties", maxPropertyRetries)
}

// Count reads a collection's total via the $count endpoint, which returns a
// bare integer and is far cheaper than paging the collection to size it.
func (c *Client) Count(ctx context.Context, path string) (int64, error) {
	u := c.baseURL + "/" + strings.Trim(path, "/") + "/$count"
	body, err := c.do(ctx, u, true) // $count always needs eventual consistency
	if err != nil {
		return 0, err
	}
	n, err := strconv.ParseInt(strings.TrimSpace(string(body)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse $count response %q: %w", string(body), err)
	}
	return n, nil
}

// mentions reports whether the query asks for a property by name.
func (q Query) mentions(property string) bool {
	if slices.Contains(q.Select, property) {
		return true
	}
	return slices.Contains(q.SearchFields, property)
}

// prune removes properties this tenant has already rejected.
func (c *Client) prune(q Query) Query {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.unsupported) == 0 {
		return q
	}

	out := q
	out.Select = keepSupported(q.Select, c.unsupported)
	out.SearchFields = keepSupported(q.SearchFields, c.unsupported)
	return out
}

func keepSupported(props []string, unsupported map[string]bool) []string {
	kept := make([]string, 0, len(props))
	for _, p := range props {
		if !unsupported[p] {
			kept = append(kept, p)
		}
	}
	return kept
}

func (c *Client) markUnsupported(property string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.unsupported[property] = true
}

// unsupportedPropertyRe matches the Graph error naming a property the tenant
// does not recognise.
var unsupportedPropertyRe = regexp.MustCompile(`[Pp]roperty '([^']+)' does not exist`)

// unsupportedProperty extracts the offending property from a Graph error.
func unsupportedProperty(err error) (string, bool) {
	var api *APIError
	if !errors.As(err, &api) || api.Status != http.StatusBadRequest {
		return "", false
	}
	m := unsupportedPropertyRe.FindStringSubmatch(api.Message)
	if len(m) != 2 {
		return "", false
	}
	return m[1], true
}

// Next follows an @odata.nextLink. The link already encodes the original
// query options and the server-side skip token, so it is used verbatim --
// rebuilding it would silently restart paging from the top.
func (c *Client) Next(ctx context.Context, nextLink string, advanced bool) (*Page, error) {
	if nextLink == "" {
		return nil, errors.New("no further pages")
	}
	return c.fetchPage(ctx, nextLink, advanced)
}

// Get fetches a single object by absolute or relative path.
func (c *Client) Get(ctx context.Context, path string, sel []string) (Item, error) {
	u := path
	if !strings.HasPrefix(u, "http") {
		u = c.baseURL + "/" + strings.TrimLeft(path, "/")
		if len(sel) > 0 {
			u += "?" + url.Values{"$select": {strings.Join(sel, ",")}}.Encode()
		}
	}
	body, err := c.do(ctx, u, false)
	if err != nil {
		return nil, err
	}
	var item Item
	if err := json.Unmarshal(body, &item); err != nil {
		return nil, fmt.Errorf("decode object: %w", err)
	}
	return item, nil
}

func (c *Client) fetchPage(ctx context.Context, u string, advanced bool) (*Page, error) {
	body, err := c.do(ctx, u, advanced)
	if err != nil {
		return nil, err
	}
	var resp collectionResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decode collection: %w", err)
	}
	page := &Page{Items: resp.Value, NextLink: resp.NextLink, TotalCount: -1}
	if resp.Count != nil {
		page.TotalCount = *resp.Count
	}
	return page, nil
}

// do issues a GET with retries for throttling and transient server errors.
func (c *Client) do(ctx context.Context, u string, advanced bool) ([]byte, error) {
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			delay := retryDelay(attempt, lastErr)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}

		body, err := c.doOnce(ctx, u, advanced)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retryable(err) {
			return nil, err
		}
	}
	return nil, lastErr
}

func (c *Client) doOnce(ctx context.Context, u string, advanced bool) ([]byte, error) {
	token, err := c.tokens.Token(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire token: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	// $count answers text/plain; every other endpoint answers JSON.
	req.Header.Set("Accept", "application/json, text/plain")
	req.Header.Set("User-Agent", c.userAgent)
	if advanced {
		req.Header.Set("ConsistencyLevel", "eventual")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, &transientError{err: err}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, &transientError{err: fmt.Errorf("read response: %w", err)}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		apiErr := parseAPIError(resp.StatusCode, body)
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if secs, convErr := strconv.Atoi(ra); convErr == nil {
				return nil, &throttleError{APIError: apiErr, retryAfter: time.Duration(secs) * time.Second}
			}
		}
		return nil, apiErr
	}
	return body, nil
}

// transientError wraps a network-level failure worth retrying.
type transientError struct{ err error }

func (e *transientError) Error() string { return e.err.Error() }
func (e *transientError) Unwrap() error { return e.err }

// throttleError carries a server-provided Retry-After delay.
type throttleError struct {
	*APIError
	retryAfter time.Duration
}

// retryable reports whether another attempt could plausibly succeed. A 403 or
// 404 never will, and retrying them just delays the error the user needs.
func retryable(err error) bool {
	var t *transientError
	if errors.As(err, &t) {
		return true
	}
	var th *throttleError
	if errors.As(err, &th) {
		return true
	}
	var api *APIError
	if errors.As(err, &api) {
		return api.Status == http.StatusTooManyRequests || api.Status >= 500
	}
	return false
}

// retryDelay honours a server Retry-After when present, else backs off
// exponentially.
func retryDelay(attempt int, err error) time.Duration {
	var th *throttleError
	if errors.As(err, &th) && th.retryAfter > 0 {
		return min(th.retryAfter, maxRetryBackoff)
	}
	return min(time.Duration(1<<attempt)*time.Second, maxRetryBackoff)
}
