package graph

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/idoavrah/entra-tui/internal/auth"
)

// stubProvider is a fixed-token auth.Provider for tests.
type stubProvider struct{ token string }

func (s stubProvider) Token(context.Context) (string, error) { return s.token, nil }
func (s stubProvider) Identity() auth.Identity               { return auth.Identity{Account: "test@example.com"} }

func newTestClient(t *testing.T, h http.Handler) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return New(stubProvider{token: "tok"}, WithBaseURL(srv.URL+"/v1.0"), WithHTTPClient(srv.Client())), srv
}

func TestBuildURLIncludesQueryOptions(t *testing.T) {
	c := New(stubProvider{}, WithBaseURL("https://graph.example/v1.0"))
	raw := c.buildURL(Query{
		Path:    "/users",
		Select:  []string{"id", "displayName"},
		Filter:  "accountEnabled eq true",
		OrderBy: "displayName",
		Top:     42,
		Count:   true,
	})

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("buildURL produced an unparseable URL: %v", err)
	}
	if got, want := u.Path, "/v1.0/users"; got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
	q := u.Query()
	for key, want := range map[string]string{
		"$select":  "id,displayName",
		"$filter":  "accountEnabled eq true",
		"$orderby": "displayName",
		"$top":     "42",
		"$count":   "true",
	} {
		if got := q.Get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestBuildURLDropsOrderByWhenSearching(t *testing.T) {
	// Graph rejects $orderby combined with $search on directory collections,
	// so the client must drop the ordering rather than let the request 400.
	c := New(stubProvider{}, WithBaseURL("https://graph.example/v1.0"))
	raw := c.buildURL(Query{
		Path: "/users", OrderBy: "displayName",
		SearchFields: []string{"displayName"}, SearchTerm: "ada",
	})

	u, _ := url.Parse(raw)
	if got := u.Query().Get("$orderby"); got != "" {
		t.Errorf("$orderby = %q, want it dropped alongside $search", got)
	}
	if got := u.Query().Get("$search"); got != `"displayName:ada"` {
		t.Errorf("$search = %q, want it preserved", got)
	}
}

func TestListPagesThroughNextLink(t *testing.T) {
	var srvURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/v1.0/users", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("missing bearer token, got %q", r.Header.Get("Authorization"))
		}
		// $count requires the eventual consistency level.
		if r.Header.Get("ConsistencyLevel") != "eventual" {
			t.Errorf("ConsistencyLevel = %q, want eventual", r.Header.Get("ConsistencyLevel"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"@odata.count":    int64(3),
			"@odata.nextLink": srvURL + "/v1.0/users?page=2",
			"value":           []map[string]any{{"id": "1"}, {"id": "2"}},
		})
	})
	c, srv := newTestClient(t, mux)
	srvURL = srv.URL

	page, err := c.List(context.Background(), Query{Path: "/users", Count: true})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("got %d items, want 2", len(page.Items))
	}
	if page.TotalCount != 3 {
		t.Errorf("TotalCount = %d, want 3", page.TotalCount)
	}
	if page.NextLink == "" {
		t.Error("NextLink is empty, want the server-provided link")
	}
	if page.Items[0].ID() != "1" {
		t.Errorf("first id = %q, want 1", page.Items[0].ID())
	}
}

func TestNextFollowsLinkVerbatim(t *testing.T) {
	// The skip token lives in the nextLink; rebuilding the URL would restart
	// paging from the top and loop forever.
	var gotQuery string
	mux := http.NewServeMux()
	mux.HandleFunc("/v1.0/users", func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(map[string]any{"value": []map[string]any{{"id": "9"}}})
	})
	c, srv := newTestClient(t, mux)

	link := srv.URL + "/v1.0/users?%24skiptoken=abc123&%24top=100"
	page, err := c.Next(context.Background(), link, true)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if !strings.Contains(gotQuery, "skiptoken") {
		t.Errorf("query = %q, want the skiptoken preserved", gotQuery)
	}
	if page.TotalCount != -1 {
		t.Errorf("TotalCount = %d, want -1 when the server reports no count", page.TotalCount)
	}
}

func TestNextRejectsEmptyLink(t *testing.T) {
	c := New(stubProvider{})
	if _, err := c.Next(context.Background(), "", false); err == nil {
		t.Fatal("Next(\"\") returned nil error, want a failure")
	}
}

func TestRetriesOnThrottleThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/v1.0/groups", func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"code":"throttled","message":"slow down"}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"value": []map[string]any{{"id": "g1"}}})
	})
	c, _ := newTestClient(t, mux)

	page, err := c.List(context.Background(), Query{Path: "/groups"})
	if err != nil {
		t.Fatalf("List after throttle: %v", err)
	}
	if len(page.Items) != 1 {
		t.Errorf("got %d items, want 1", len(page.Items))
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("server saw %d calls, want 2 (one throttled, one retried)", got)
	}
}

func TestDoesNotRetryForbidden(t *testing.T) {
	// A 403 means missing consent; retrying only delays the real message.
	var calls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/v1.0/applications", func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":"Authorization_RequestDenied","message":"Insufficient privileges"}}`))
	})
	c, _ := newTestClient(t, mux)

	_, err := c.List(context.Background(), Query{Path: "/applications"})
	if err == nil {
		t.Fatal("List returned nil error for a 403")
	}
	var api *APIError
	if !errors.As(err, &api) {
		t.Fatalf("error %v is not an *APIError", err)
	}
	if api.Status != http.StatusForbidden {
		t.Errorf("status = %d, want 403", api.Status)
	}
	if api.Code != "Authorization_RequestDenied" {
		t.Errorf("code = %q, want Authorization_RequestDenied", api.Code)
	}
	// A permission failure says three words and stops. Graph's own message
	// is boilerplate that repeats the status code, and an essay about
	// consent models is not what someone wants mid-task.
	if got := api.Error(); got != "no permission" {
		t.Errorf("Error() = %q, want a succinct refusal", got)
	}
	if api.Hint() != "" {
		t.Errorf("Hint() = %q, want nothing added to it", api.Hint())
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("server saw %d calls, want exactly 1 (no retry on 403)", got)
	}
}

func TestGetFetchesSingleObject(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1.0/users/abc", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("$select") != "" {
			t.Errorf("detail fetch sent a $select, want the full default projection")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "abc", "displayName": "Ada"})
	})
	c, _ := newTestClient(t, mux)

	item, err := c.Get(context.Background(), "/users/abc", nil)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if item.String("displayName") != "Ada" {
		t.Errorf("displayName = %q, want Ada", item.String("displayName"))
	}
}

func TestContextCancellationStopsRetries(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1.0/users", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	c, _ := newTestClient(t, mux)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	if _, err := c.List(ctx, Query{Path: "/users"}); err == nil {
		t.Fatal("List returned nil error despite a cancelled context")
	}
	// Without honouring the context the backoff chain would take seconds.
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("List took %v, want it to abort promptly on cancellation", elapsed)
	}
}

func TestParseAPIErrorFallsBackToRawBody(t *testing.T) {
	e := parseAPIError(http.StatusBadGateway, []byte("upstream exploded"))
	if !strings.Contains(e.Error(), "upstream exploded") {
		t.Errorf("Error() = %q, want it to include the raw body", e.Error())
	}
}

func TestNeedsAdvancedQuery(t *testing.T) {
	for _, tc := range []struct {
		name string
		q    Query
		want bool
	}{
		{"plain", Query{Path: "/users"}, false},
		{"count", Query{Path: "/users", Count: true}, true},
		{"search", Query{Path: "/users", SearchFields: []string{"displayName"}, SearchTerm: "a"}, true},
		{"search with no fields", Query{Path: "/users", SearchTerm: "a"}, false},
	} {
		if got := tc.q.NeedsAdvancedQuery(); got != tc.want {
			t.Errorf("%s: NeedsAdvancedQuery() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestListDropsAPropertyTheTenantRejects(t *testing.T) {
	// The failure reported from a real tenant: servicePrincipal's
	// publisherName is not a declared property there, and Graph rejects the
	// whole request rather than ignoring the field.
	var selects []string
	mux := http.NewServeMux()
	mux.HandleFunc("/v1.0/servicePrincipals", func(w http.ResponseWriter, r *http.Request) {
		sel := r.URL.Query().Get("$select")
		selects = append(selects, sel)
		if strings.Contains(sel, "publisherName") {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"code":"Request_UnsupportedQuery","message":` +
				`"Property 'publisherName' does not exist as a declared property or extension property."}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"value": []map[string]any{{"id": "sp1"}}})
	})
	c, _ := newTestClient(t, mux)

	page, err := c.List(context.Background(), Query{
		Path:   "/servicePrincipals",
		Select: []string{"id", "displayName", "publisherName"},
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(page.Items) != 1 {
		t.Errorf("got %d items, want 1", len(page.Items))
	}
	if len(selects) != 2 {
		t.Fatalf("server saw %d attempts, want 2 (rejected then pruned)", len(selects))
	}
	if !strings.Contains(selects[1], "displayName") {
		t.Errorf("retry $select = %q, want the other properties kept", selects[1])
	}
}

func TestRejectedPropertyIsRememberedForLaterQueries(t *testing.T) {
	// Paying one round trip per property is acceptable; paying it on every
	// request is not.
	var attempts int
	mux := http.NewServeMux()
	mux.HandleFunc("/v1.0/servicePrincipals", func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if strings.Contains(r.URL.Query().Get("$select"), "publisherName") {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"code":"Request_UnsupportedQuery","message":` +
				`"Property 'publisherName' does not exist as a declared property or extension property."}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"value": []map[string]any{}})
	})
	c, _ := newTestClient(t, mux)

	q := Query{Path: "/servicePrincipals", Select: []string{"id", "publisherName"}}
	for range 3 {
		if _, err := c.List(context.Background(), q); err != nil {
			t.Fatalf("List: %v", err)
		}
	}
	if attempts != 4 {
		t.Errorf("server saw %d requests over 3 calls, want 4 (one wasted, then never again)", attempts)
	}
}

func TestUnsearchablePropertyIsDroppedFromSearch(t *testing.T) {
	var searches []string
	mux := http.NewServeMux()
	mux.HandleFunc("/v1.0/servicePrincipals", func(w http.ResponseWriter, r *http.Request) {
		search := r.URL.Query().Get("$search")
		searches = append(searches, search)
		if strings.Contains(search, "publisherName") {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"code":"Request_UnsupportedQuery","message":` +
				`"Property 'publisherName' does not exist as a declared property or extension property."}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"value": []map[string]any{}})
	})
	c, _ := newTestClient(t, mux)

	if _, err := c.List(context.Background(), Query{
		Path:         "/servicePrincipals",
		SearchFields: []string{"displayName", "publisherName"},
		SearchTerm:   "contoso",
	}); err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(searches) != 2 {
		t.Fatalf("server saw %d attempts, want 2", len(searches))
	}
	if !strings.Contains(searches[1], "displayName:contoso") {
		t.Errorf("retry $search = %q, want the searchable field kept", searches[1])
	}
}

func TestUnrelatedBadRequestIsNotRetried(t *testing.T) {
	// A property complaint about something the query does not ask for cannot
	// be fixed by dropping anything, and must not loop.
	var attempts int
	mux := http.NewServeMux()
	mux.HandleFunc("/v1.0/users", func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":"Request_UnsupportedQuery","message":` +
			`"Property 'somethingElse' does not exist as a declared property or extension property."}}`))
	})
	c, _ := newTestClient(t, mux)

	if _, err := c.List(context.Background(), Query{Path: "/users", Select: []string{"id"}}); err == nil {
		t.Fatal("List returned nil error")
	}
	if attempts != 1 {
		t.Errorf("server saw %d attempts, want exactly 1", attempts)
	}
}

func TestUnsupportedPropertyExtraction(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
		ok   bool
	}{
		{"graph wording", &APIError{Status: 400, Message: "Property 'publisherName' does not exist as a declared property or extension property."}, "publisherName", true},
		{"lowercase", &APIError{Status: 400, Message: "property 'foo' does not exist"}, "foo", true},
		{"not a 400", &APIError{Status: 403, Message: "Property 'x' does not exist"}, "", false},
		{"different 400", &APIError{Status: 400, Message: "Invalid filter clause"}, "", false},
	} {
		got, ok := unsupportedProperty(tc.err)
		if ok != tc.ok || got != tc.want {
			t.Errorf("%s: unsupportedProperty = %q,%v want %q,%v", tc.name, got, ok, tc.want, tc.ok)
		}
	}
}

func TestCountReadsThePlainIntegerEndpoint(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1.0/users/$count", func(w http.ResponseWriter, r *http.Request) {
		// $count is refused without the eventual consistency level.
		if r.Header.Get("ConsistencyLevel") != "eventual" {
			t.Errorf("ConsistencyLevel = %q, want eventual", r.Header.Get("ConsistencyLevel"))
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("1707600\n"))
	})
	c, _ := newTestClient(t, mux)

	got, err := c.Count(context.Background(), "/users")
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if got != 1707600 {
		t.Errorf("Count = %d, want 1707600", got)
	}
}

func TestCountReportsAnUnparseableBody(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1.0/users/$count", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not a number"))
	})
	c, _ := newTestClient(t, mux)

	if _, err := c.Count(context.Background(), "/users"); err == nil {
		t.Fatal("Count accepted a non-numeric body")
	}
}
