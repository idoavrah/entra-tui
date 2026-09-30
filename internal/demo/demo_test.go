package demo

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/idoavrah/entra-tui/internal/graph"
)

func TestGenerateIsDeterministic(t *testing.T) {
	// Screenshots are only comparable between runs if the directory is.
	a, b := Generate(7), Generate(7)
	if a.Users[10].String("displayName") != b.Users[10].String("displayName") {
		t.Error("the same seed produced different data")
	}
	if len(a.Users) != UserCount || len(a.Groups) != GroupCount || len(a.Devices) != DeviceCount {
		t.Errorf("sizes = %d/%d/%d, want %d/%d/%d",
			len(a.Users), len(a.Groups), len(a.Devices), UserCount, GroupCount, DeviceCount)
	}
}

func TestGeneratedDirectoryHasVariety(t *testing.T) {
	d := Generate(7)

	var disabled, guests, hebrew int
	for _, u := range d.Users {
		if enabled, ok := u.Bool("accountEnabled"); ok && !enabled {
			disabled++
		}
		if u.String("userType") == "Guest" {
			guests++
		}
		if strings.ContainsAny(u.String("displayName"), "אבגדהוזחטיכלמנסעפצקרשת") {
			hebrew++
		}
	}
	// Every row state the tables colour needs something to colour.
	if disabled == 0 {
		t.Error("no disabled users, so the muted row style is never exercised")
	}
	if guests == 0 {
		t.Error("no guest users")
	}
	if hebrew == 0 {
		t.Error("no right-to-left names, so bidi rendering is never exercised")
	}

	var expired int
	for _, a := range d.Applications {
		if graph.SoonestCredentialExpiry(a) == "expired" {
			expired++
		}
	}
	if expired == 0 {
		t.Error("no lapsed credentials, so the warning row style is never exercised")
	}
}

func TestServerPagesAndCounts(t *testing.T) {
	c := NewServer(7).Client()

	page, err := c.List(context.Background(), graph.Query{Path: "/users", Top: 50, Count: true})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(page.Items) != 50 {
		t.Errorf("got %d items, want a full page of 50", len(page.Items))
	}
	if page.TotalCount != UserCount {
		t.Errorf("count = %d, want %d", page.TotalCount, UserCount)
	}
	if page.NextLink == "" {
		t.Error("no nextLink on a partial listing")
	}

	next, err := c.Next(context.Background(), page.NextLink, true)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if next.Items[0].ID() == page.Items[0].ID() {
		t.Error("the second page repeated the first")
	}

	total, err := c.Count(context.Background(), "/devices")
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if total != DeviceCount {
		t.Errorf("Count = %d, want %d", total, DeviceCount)
	}
}

func TestServerRejectsASelectTheCollectionDoesNotDeclare(t *testing.T) {
	// This is the whole point of the stand-in enforcing $select: a fake that
	// ignores it lets a bad projection through to a real tenant, which is
	// exactly how a group-only property ended up on a directoryObject
	// collection in a shipped build.
	//
	// The request goes straight to the handler: the client would heal a
	// rejected property and retry, which is right in the app and would hide
	// the rejection here.
	s := NewServer(7)

	for _, tc := range []struct {
		name, path, selection string
		wantRejected          bool
	}{
		{"group property on memberOf", "/v1.0/users/u0000/memberOf", "id,displayName,groupTypes", true},
		{"user property on members", "/v1.0/groups/g0000/members", "id,userPrincipalName", true},
		{"user property on owners", "/v1.0/groups/g0000/owners", "id,userPrincipalName", true},
		{"directoryObject properties", "/v1.0/users/u0000/memberOf", "id,displayName", false},
		{"no projection at all", "/v1.0/users/u0000/memberOf", "", false},
		{"real user property on users", "/v1.0/users", "id,userPrincipalName,department", false},
		{"invented property on users", "/v1.0/users", "id,noSuchThing", true},
	} {
		status, body := request(t, s, http.MethodGet, tc.path+"?$select="+url.QueryEscape(tc.selection), "")
		rejected := status == http.StatusBadRequest
		if rejected != tc.wantRejected {
			t.Errorf("%s: status %d (rejected=%v), want rejected=%v\n%s",
				tc.name, status, rejected, tc.wantRejected, body)
		}
		if tc.wantRejected && !strings.Contains(body, "does not exist as a declared property") {
			t.Errorf("%s: body %q does not read like Graph's own refusal", tc.name, body)
		}
	}
}

func TestRelationshipsWorkWithoutAProjection(t *testing.T) {
	c := NewServer(7).Client()
	ctx := context.Background()

	if _, _, err := c.MemberOf(ctx, "/users", "u0000"); err != nil {
		t.Errorf("MemberOf: %v", err)
	}
	if _, _, err := c.Members(ctx, "g0000"); err != nil {
		t.Errorf("Members: %v", err)
	}
	if _, err := c.Owners(ctx, "/groups", "g0000"); err != nil {
		t.Errorf("Owners: %v", err)
	}
}

// request drives the handler directly and returns what it answered.
func request(t *testing.T, s *Server, method, target, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, baseURL+strings.TrimPrefix(target, "/v1.0"), strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.URL.Path = strings.SplitN(target, "?", 2)[0]
	req.Header.Set("ConsistencyLevel", "eventual")

	rec := &recorder{header: http.Header{}, status: http.StatusOK}
	s.ServeHTTP(rec, req)
	return rec.status, rec.body.String()
}

func TestServerSelfHealingStripsARejectedProperty(t *testing.T) {
	// The client drops a property Graph refuses and retries; against the
	// stand-in that should end in a successful listing.
	c := NewServer(7).Client()

	page, err := c.List(context.Background(), graph.Query{
		Path:   "/users",
		Select: []string{"id", "displayName", "noSuchProperty"},
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(page.Items) == 0 {
		t.Error("no items after the unsupported property was dropped")
	}
}

func TestServerRoundTripsAMembershipChange(t *testing.T) {
	s := NewServer(7)
	c := s.Client()
	ctx := context.Background()

	group := s.Data().Groups[0].ID()
	before, _, err := c.Members(ctx, group)
	if err != nil {
		t.Fatalf("Members: %v", err)
	}

	newcomer := s.Data().Users[len(s.Data().Users)-1].ID()
	if err := c.AddRef(ctx, "/groups", group, graph.RelMembers, newcomer); err != nil {
		t.Fatalf("AddRef: %v", err)
	}
	after, _, _ := c.Members(ctx, group)
	if len(after) != len(before)+1 {
		t.Fatalf("members went from %d to %d, want one more", len(before), len(after))
	}

	// Adding the same principal twice is refused, as Graph refuses it.
	if err := c.AddRef(ctx, "/groups", group, graph.RelMembers, newcomer); err == nil {
		t.Error("a duplicate membership was accepted")
	}

	if err := c.RemoveRef(ctx, "/groups", group, graph.RelMembers, newcomer); err != nil {
		t.Fatalf("RemoveRef: %v", err)
	}
	final, _, _ := c.Members(ctx, group)
	if len(final) != len(before) {
		t.Errorf("members = %d after removal, want %d", len(final), len(before))
	}
}

func TestServerResolvesPermissionsAndConsent(t *testing.T) {
	s := NewServer(7)
	c := s.Client()
	ctx := context.Background()

	app := s.Data().Applications[0]
	resources, permissions := c.ResolvePermissions(ctx, app)
	if resources[GraphAppID] != "Microsoft Graph" {
		t.Errorf("resource name = %q, want Microsoft Graph", resources[GraphAppID])
	}
	if len(permissions) == 0 {
		t.Fatal("no permissions resolved")
	}
	for id, info := range permissions {
		if info.Value == "" {
			t.Errorf("permission %s resolved to an empty value", id)
		}
	}

	sp, err := c.ServicePrincipalByAppID(ctx, app.String("appId"))
	if err != nil || sp == nil {
		t.Fatalf("ServicePrincipalByAppID: %v", err)
	}
	scopes, roles, err := c.GrantedPermissions(ctx, sp.ID())
	if err != nil {
		t.Fatalf("GrantedPermissions: %v", err)
	}
	if len(scopes)+len(roles) == 0 {
		t.Error("nothing is consented anywhere, so the permissions tab has one state only")
	}
}

func TestHeterogeneousCollectionsCarryTheirType(t *testing.T) {
	// Graph annotates every object in a members or owners collection with
	// its type, and it is the only thing telling a nested group from a user
	// there -- the properties that would give it away are not selectable.
	c := NewServer(1).Client()
	ctx := context.Background()

	members, _, err := c.Members(ctx, "g0000")
	if err != nil {
		t.Fatalf("Members: %v", err)
	}
	if len(members) == 0 {
		t.Fatal("the first group has no members")
	}
	seen := map[string]bool{}
	for _, m := range members {
		got := m.String("@odata.type")
		if got == "" {
			t.Fatalf("member %q carries no @odata.type", m.ID())
		}
		seen[got] = true
	}
	// The generator nests a group in every members list, so both types are
	// there to tell apart.
	for _, want := range []string{"#microsoft.graph.user", "#microsoft.graph.group"} {
		if !seen[want] {
			t.Errorf("no member of type %s; saw %v", want, seen)
		}
	}
}

func TestListingsAreNotAnnotated(t *testing.T) {
	// A homogeneous collection does not carry the annotation, and code that
	// reads one must not come to depend on it.
	c := NewServer(1).Client()
	page, err := c.List(context.Background(), graph.Query{Path: "/users", Top: 1})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(page.Items) == 0 {
		t.Fatal("no users")
	}
	if got := page.Items[0].String("@odata.type"); got != "" {
		t.Errorf("a plain listing carries @odata.type = %q", got)
	}
}

func TestSearchingByAnIDFindsTheObject(t *testing.T) {
	// End to end: a GUID pasted into the search box reaches Graph as a
	// filter and comes back with one row.
	//
	// The demo writes appId and deviceId as real GUIDs but gives objects
	// short readable ids ("u0092"), so only the former are exercised here --
	// a term that is not written like a GUID is deliberately not treated as
	// one. TestTheDemoFilterMatchesOnObjectID covers the other half.
	c := NewServer(7).Client()
	ctx := context.Background()

	exercised := 0
	for _, res := range graph.All() {
		if len(res.IDFields) == 0 {
			t.Errorf("%s has no id fields, so its ids cannot be searched", res.Kind)
			continue
		}

		first, err := c.List(ctx, graph.Query{Path: res.Path, Select: res.Select, Top: 1})
		if err != nil || len(first.Items) == 0 {
			t.Fatalf("%s: listing the collection: %v", res.Kind, err)
		}
		want := first.Items[0].ID()

		for _, field := range res.IDFields {
			if _, ok := graph.AsGUID(first.Items[0].String(field.Name)); !ok {
				continue
			}
			page, err := c.List(ctx, graph.Query{
				Path: res.Path, Select: res.Select,
				SearchFields: res.SearchFields, IDFields: res.IDFields,
				SearchTerm: first.Items[0].String(field.Name),
			})
			if err != nil {
				t.Errorf("%s by %s: %v", res.Kind, field.Name, err)
				continue
			}
			if len(page.Items) != 1 || page.Items[0].ID() != want {
				t.Errorf("%s by %s = %d items, want just %s", res.Kind, field.Name, len(page.Items), want)
			}
			exercised++
		}
	}
	if exercised == 0 {
		t.Error("no id property was exercised; the test proved nothing")
	}
}

func TestTheDemoFilterMatchesOnObjectID(t *testing.T) {
	// The stand-in has to understand a filter on "id" as well as on "appId",
	// which is the half the demo's own readable ids cannot demonstrate
	// through the search box.
	c := NewServer(7).Client()
	ctx := context.Background()

	first, err := c.List(ctx, graph.Query{Path: "/users", Select: []string{"id"}, Top: 1})
	if err != nil || len(first.Items) == 0 {
		t.Fatalf("listing users: %v", err)
	}
	id := first.Items[0].ID()

	page, err := c.List(ctx, graph.Query{
		Path: "/users", Select: []string{"id"},
		Filter: "id eq '" + id + "' or appId eq '" + id + "'",
	})
	if err != nil {
		t.Fatalf("filtering by id: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].ID() != id {
		t.Errorf("got %d items, want just %s", len(page.Items), id)
	}
}

func TestAnUnknownFilterIsStillRefused(t *testing.T) {
	// The stand-in must keep failing loudly on a filter it cannot honour.
	// One that quietly matched everything would answer a question nobody
	// asked and let a real bug through to a tenant.
	c := NewServer(7).Client()
	_, err := c.List(context.Background(), graph.Query{
		Path: "/users", Filter: "startswith(displayName, 'A')",
	})
	if err == nil {
		t.Error("the demo accepted a filter it does not implement")
	}
}

func TestAPartialIDMatchesWhereGraphAllowsIt(t *testing.T) {
	// Enterprise apps are one of the two places a partly-typed id can be
	// matched at all: servicePrincipal.appId is documented as supporting
	// startsWith, where an object id supports only eq.
	c := NewServer(7).Client()
	ctx := context.Background()

	res, _ := graph.Lookup("entapps")
	first, err := c.List(ctx, graph.Query{Path: res.Path, Select: res.Select, Top: 1})
	if err != nil || len(first.Items) == 0 {
		t.Fatalf("listing enterprise apps: %v", err)
	}
	appID := first.Items[0].String("appId")
	if _, ok := graph.AsGUID(appID); !ok {
		t.Fatalf("appId %q is not a GUID, so this proves nothing", appID)
	}

	page, err := c.List(ctx, graph.Query{
		Path: res.Path, Select: res.Select,
		SearchFields: res.SearchFields, IDFields: res.IDFields,
		SearchTerm: appID[:8],
	})
	if err != nil {
		t.Fatalf("partial id search: %v", err)
	}

	var found bool
	for _, it := range page.Items {
		if it.ID() == first.Items[0].ID() {
			found = true
		}
	}
	if !found {
		t.Errorf("the first 8 characters of %s found %d rows, none of them it",
			appID, len(page.Items))
	}
}

func TestSearchingByIDStillFindsThingsByName(t *testing.T) {
	// The union: asking by id must never cost the rows that asking by name
	// would have found. A search term is both questions at once.
	c := NewServer(7).Client()
	ctx := context.Background()
	res, _ := graph.Lookup("entapps")

	// "0000" is too short to be an id fragment, so this is a pure name
	// search; the demo names contain no digits, so it finds nothing.
	byName, err := c.List(ctx, graph.Query{
		Path: res.Path, Select: res.Select, Count: true,
		SearchFields: res.SearchFields, IDFields: res.IDFields, SearchTerm: "Ember",
	})
	if err != nil || len(byName.Items) == 0 {
		t.Fatalf("name search: %v (%d items)", err, len(byName.Items))
	}
	if byName.TotalCount <= 0 {
		t.Errorf("TotalCount = %d, want the search's own count", byName.TotalCount)
	}
}
