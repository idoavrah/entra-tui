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
	raw := c.buildURL(Query{Path: "/users", OrderBy: "displayName", Search: `"displayName:ada"`})

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
	if api.Hint() == "" {
		t.Error("Hint() is empty; a 403 should explain the consent problem")
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
		{"search", Query{Path: "/users", Search: `"displayName:a"`}, true},
	} {
		if got := tc.q.NeedsAdvancedQuery(); got != tc.want {
			t.Errorf("%s: NeedsAdvancedQuery() = %v, want %v", tc.name, got, tc.want)
		}
	}
}
