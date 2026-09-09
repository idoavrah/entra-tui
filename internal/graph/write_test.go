package graph

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// recorded is one request the fake Graph saw.
type recorded struct {
	method string
	path   string
	body   string
}

func recordingClient(t *testing.T, status int) (*Client, *[]recorded) {
	t.Helper()
	var seen []recorded
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen = append(seen, recorded{method: r.Method, path: r.URL.Path, body: string(body)})
		w.WriteHeader(status)
		if status >= 400 {
			_, _ = w.Write([]byte(`{"error":{"code":"Authorization_RequestDenied","message":"Insufficient privileges"}}`))
		}
	}))
	t.Cleanup(srv.Close)
	return New(stubProvider{token: "tok"}, WithBaseURL(srv.URL+"/v1.0"), WithHTTPClient(srv.Client())), &seen
}

func TestAddRefPostsAnODataReference(t *testing.T) {
	// Graph edits these collections through a $ref sub-resource whose body
	// is an @odata.id; there is no way to pass a bare id.
	c, seen := recordingClient(t, http.StatusNoContent)

	if err := c.AddRef(context.Background(), "/groups", "g1", RelMembers, "u1"); err != nil {
		t.Fatalf("AddRef: %v", err)
	}
	if len(*seen) != 1 {
		t.Fatalf("got %d requests, want 1", len(*seen))
	}
	got := (*seen)[0]
	if got.method != http.MethodPost {
		t.Errorf("method = %s, want POST", got.method)
	}
	if want := "/v1.0/groups/g1/members/$ref"; got.path != want {
		t.Errorf("path = %q, want %q", got.path, want)
	}

	var body map[string]string
	if err := json.Unmarshal([]byte(got.body), &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if !strings.HasSuffix(body["@odata.id"], "/directoryObjects/u1") {
		t.Errorf("@odata.id = %q, want it to point at the object", body["@odata.id"])
	}
}

func TestRemoveRefDeletesTheReference(t *testing.T) {
	c, seen := recordingClient(t, http.StatusNoContent)

	if err := c.RemoveRef(context.Background(), "/groups", "g1", RelOwners, "u1"); err != nil {
		t.Fatalf("RemoveRef: %v", err)
	}
	got := (*seen)[0]
	if got.method != http.MethodDelete {
		t.Errorf("method = %s, want DELETE", got.method)
	}
	if want := "/v1.0/groups/g1/owners/u1/$ref"; got.path != want {
		t.Errorf("path = %q, want %q", got.path, want)
	}
	if got.body != "" {
		t.Errorf("body = %q, want none", got.body)
	}
}

func TestDeviceOwnersUseTheirOwnRelationship(t *testing.T) {
	// Graph keeps a device's owners under registeredOwners, not owners.
	c, seen := recordingClient(t, http.StatusNoContent)

	if err := c.AddRef(context.Background(), "/devices", "d1", RelRegisteredOwners, "u1"); err != nil {
		t.Fatalf("AddRef: %v", err)
	}
	if want := "/v1.0/devices/d1/registeredOwners/$ref"; (*seen)[0].path != want {
		t.Errorf("path = %q, want %q", (*seen)[0].path, want)
	}
}

func TestWriteReportsGraphErrors(t *testing.T) {
	c, _ := recordingClient(t, http.StatusForbidden)

	err := c.AddRef(context.Background(), "/groups", "g1", RelMembers, "u1")
	if err == nil {
		t.Fatal("AddRef returned nil for a 403")
	}
	var api *APIError
	if !asAPIErr(err, &api) {
		t.Fatalf("error %v is not an *APIError", err)
	}
	if api.Status != http.StatusForbidden || api.Code != "Authorization_RequestDenied" {
		t.Errorf("error = %+v, want the Graph code preserved", api)
	}
	if api.Hint() == "" {
		t.Error("a 403 on a write should explain the missing permission")
	}
}

func TestWritesAreNotRetried(t *testing.T) {
	// A POST that may or may not have been applied must be reported, not
	// repeated: retrying could add the same principal twice or mask a
	// partial success.
	c, seen := recordingClient(t, http.StatusServiceUnavailable)

	if err := c.AddRef(context.Background(), "/groups", "g1", RelMembers, "u1"); err == nil {
		t.Fatal("AddRef returned nil for a 503")
	}
	if len(*seen) != 1 {
		t.Errorf("server saw %d requests, want exactly 1", len(*seen))
	}
}

func TestRefArgumentsAreValidated(t *testing.T) {
	// Ids come from Graph and are opaque GUIDs. Anything carrying a path or
	// query character did not, and must never be pasted into a URL.
	c, seen := recordingClient(t, http.StatusNoContent)

	for _, tc := range []struct{ object, principal string }{
		{"", "u1"},
		{"g1", ""},
		{"g1/../../users", "u1"},
		{"g1", "u1?$select=x"},
		{"g1", "u1&x=1"},
		{"g1", "u1#frag"},
	} {
		if err := c.AddRef(context.Background(), "/groups", tc.object, RelMembers, tc.principal); err == nil {
			t.Errorf("AddRef(%q,%q) was accepted", tc.object, tc.principal)
		}
		if err := c.RemoveRef(context.Background(), "/groups", tc.object, RelMembers, tc.principal); err == nil {
			t.Errorf("RemoveRef(%q,%q) was accepted", tc.object, tc.principal)
		}
	}
	if len(*seen) != 0 {
		t.Errorf("server saw %d requests, want none to have been sent", len(*seen))
	}
}

func TestRelationshipLabels(t *testing.T) {
	for rel, want := range map[Relationship]string{
		RelMembers:          "member",
		RelOwners:           "owner",
		RelRegisteredOwners: "registered owner",
	} {
		if got := rel.Label(); got != want {
			t.Errorf("%s.Label() = %q, want %q", rel, got, want)
		}
	}
}

func TestFindPrincipalsSearchesGroupsOnlyForMembers(t *testing.T) {
	// An owner must be a user; a member may also be a group. Searching
	// groups for an owner would offer a choice that cannot be applied.
	var paths []string
	mux := http.NewServeMux()
	handler := func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{"value": []map[string]any{{"id": "x"}}})
	}
	mux.HandleFunc("/v1.0/users", handler)
	mux.HandleFunc("/v1.0/groups", handler)
	c, _ := newTestClient(t, mux)

	if _, err := c.FindPrincipals(context.Background(), RelOwners, "ada"); err != nil {
		t.Fatalf("FindPrincipals: %v", err)
	}
	if len(paths) != 1 || !strings.HasSuffix(paths[0], "/users") {
		t.Errorf("owner lookup hit %v, want users only", paths)
	}

	paths = nil
	if _, err := c.FindPrincipals(context.Background(), RelMembers, "ada"); err != nil {
		t.Fatalf("FindPrincipals: %v", err)
	}
	if len(paths) != 2 {
		t.Errorf("member lookup hit %v, want users and groups", paths)
	}
}

func TestFindPrincipalsRejectsABlankTerm(t *testing.T) {
	c := New(stubProvider{})
	if _, err := c.FindPrincipals(context.Background(), RelMembers, "   "); err == nil {
		t.Error("a blank search term was accepted")
	}
}

// asAPIErr is errors.As specialised, kept local to this file.
func asAPIErr(err error, target **APIError) bool {
	for err != nil {
		if e, ok := err.(*APIError); ok {
			*target = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
