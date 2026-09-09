package graph

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
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
		tokens:    tokens,
		http:      &http.Client{Timeout: 60 * time.Second},
		baseURL:   DefaultBaseURL,
		userAgent: "entra-tui",
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
	// Search is a raw $search expression, already in Graph's
	// `"field:term"` form. It is mutually exclusive with OrderBy.
	Search string
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
	return q.Search != "" || q.Count
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
	if q.Search != "" {
		params.Set("$search", q.Search)
	}
	// Graph rejects $orderby combined with $search on directory collections,
	// so relevance ordering wins whenever a search is active.
	if q.OrderBy != "" && q.Search == "" {
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
func (c *Client) List(ctx context.Context, q Query) (*Page, error) {
	return c.fetchPage(ctx, c.buildURL(q), q.NeedsAdvancedQuery())
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
	req.Header.Set("Accept", "application/json")
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
