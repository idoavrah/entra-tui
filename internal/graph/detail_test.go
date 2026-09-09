package graph

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// sectionTitles lists a rendered detail's section headings in order.
func sectionTitles(sections []Section) []string {
	out := make([]string, len(sections))
	for i, s := range sections {
		out[i] = s.Title
	}
	return out
}

// findSection returns the named section.
func findSection(t *testing.T, sections []Section, title string) Section {
	t.Helper()
	for _, s := range sections {
		if s.Title == title {
			return s
		}
	}
	t.Fatalf("no %q section in %v", title, sectionTitles(sections))
	return Section{}
}

// fieldValue returns the value of the named field.
func fieldValue(s Section, label string) string {
	for _, f := range s.Fields {
		if f.Label == label {
			return f.Value
		}
	}
	return ""
}

func mustItem(t *testing.T, raw string) Item {
	t.Helper()
	var i Item
	if err := json.Unmarshal([]byte(raw), &i); err != nil {
		t.Fatalf("bad test fixture: %v", err)
	}
	return i
}

func TestAppRegistrationSectionOrderPutsEssentialsFirst(t *testing.T) {
	app := mustItem(t, `{
		"id":"obj1","appId":"app-guid","displayName":"Contoso API",
		"signInAudience":"AzureADMyOrg","publisherDomain":"contoso.com",
		"web":{"redirectUris":["https://a/callback"],"implicitGrantSettings":{"enableAccessTokenIssuance":true}},
		"passwordCredentials":[{"displayName":"prod","endDateTime":"2030-01-01T00:00:00Z"}],
		"requiredResourceAccess":[{"resourceAppId":"00000003-0000-0000-c000-000000000000",
			"resourceAccess":[{"id":"perm-1","type":"Scope"}]}],
		"appRoles":[{"id":"role-1","displayName":"Reader","value":"Reader",
			"allowedMemberTypes":["User"],"isEnabled":true}]
	}`)

	sections := Sections(Detail{
		Kind:          KindAppRegistrations,
		Object:        app,
		ResourceNames: map[string]string{"00000003-0000-0000-c000-000000000000": "Microsoft Graph"},
		Permissions:   map[string]PermissionInfo{"perm-1": {Value: "User.Read.All"}},
	})

	titles := sectionTitles(sections)
	if titles[0] != "Essentials" {
		t.Errorf("first section = %q, want Essentials", titles[0])
	}
	for _, want := range []string{"Authentication", "Certificates & secrets", "API permissions", "App roles", "Owners"} {
		found := false
		for _, got := range titles {
			if got == want {
				found = true
			}
		}
		if !found {
			t.Errorf("missing section %q; got %v", want, titles)
		}
	}
}

func TestAppRegistrationEssentialsUseEntraTerminology(t *testing.T) {
	app := mustItem(t, `{"id":"obj1","appId":"app-guid","displayName":"Contoso API","signInAudience":"AzureADMyOrg"}`)
	essentials := findSection(t, Sections(Detail{Kind: KindAppRegistrations, Object: app}), "Essentials")

	if got := fieldValue(essentials, "Application (client) ID"); got != "app-guid" {
		t.Errorf("Application (client) ID = %q, want app-guid", got)
	}
	if got := fieldValue(essentials, "Object ID"); got != "obj1" {
		t.Errorf("Object ID = %q, want obj1", got)
	}
	if got := fieldValue(essentials, "Supported account types"); got != "Single tenant" {
		t.Errorf("Supported account types = %q, want Single tenant", got)
	}
}

func TestAuthenticationSectionGathersEveryPlatform(t *testing.T) {
	app := mustItem(t, `{
		"id":"o","displayName":"a",
		"web":{"redirectUris":["https://web/cb"],"homePageUrl":"https://home"},
		"spa":{"redirectUris":["https://spa/cb"]},
		"publicClient":{"redirectUris":["http://localhost"]}
	}`)
	auth := findSection(t, Sections(Detail{Kind: KindAppRegistrations, Object: app}), "Authentication")

	labels := map[string][]string{}
	for _, f := range auth.Fields {
		labels[f.Label] = f.Values
	}
	for _, want := range []string{"Web redirect URIs", "SPA redirect URIs", "Desktop/mobile redirect URIs"} {
		if len(labels[want]) == 0 {
			t.Errorf("missing %q; Graph splits redirect URIs across platform objects", want)
		}
	}
}

func TestImplicitGrantIsFlaggedAsAWarning(t *testing.T) {
	app := mustItem(t, `{"id":"o","displayName":"a",
		"web":{"implicitGrantSettings":{"enableAccessTokenIssuance":true,"enableIdTokenIssuance":false}}}`)
	auth := findSection(t, Sections(Detail{Kind: KindAppRegistrations, Object: app}), "Authentication")

	for _, f := range auth.Fields {
		if f.Label == "Implicit access tokens" {
			if f.Value != "yes" {
				t.Errorf("value = %q, want yes", f.Value)
			}
			if !f.Warn {
				t.Error("implicit access token issuance should be flagged")
			}
			return
		}
	}
	t.Error("implicit grant settings were not reported")
}

func TestCredentialsShowExpiryAndWarnWhenClose(t *testing.T) {
	soon := time.Now().Add(5 * 24 * time.Hour).UTC().Format(time.RFC3339)
	far := time.Now().Add(500 * 24 * time.Hour).UTC().Format(time.RFC3339)
	app := Item{
		"id": "o", "displayName": "a",
		"passwordCredentials": []any{map[string]any{"displayName": "expiring", "endDateTime": soon}},
		"keyCredentials":      []any{map[string]any{"displayName": "fine", "endDateTime": far}},
	}
	creds := findSection(t, Sections(Detail{Kind: KindAppRegistrations, Object: app}), "Certificates & secrets")

	if len(creds.Fields) != 2 {
		t.Fatalf("got %d credentials, want 2", len(creds.Fields))
	}
	var warned, calm int
	for _, f := range creds.Fields {
		if !strings.Contains(f.Value, "expires") {
			t.Errorf("field %q value = %q, want an expiry", f.Label, f.Value)
		}
		if f.Warn {
			warned++
		} else {
			calm++
		}
	}
	if warned != 1 || calm != 1 {
		t.Errorf("warned=%d calm=%d, want exactly the near-expiry one flagged", warned, calm)
	}
}

func TestAPIPermissionsAreOneRowPerPermission(t *testing.T) {
	// The nested shape Graph returns reads terribly as a tree; what an
	// administrator wants is per permission.
	app := mustItem(t, `{"id":"o","displayName":"a","requiredResourceAccess":[
		{"resourceAppId":"graph-app","resourceAccess":[
			{"id":"p1","type":"Scope"},{"id":"p2","type":"Role"}]}]}`)

	perms := findSection(t, Sections(Detail{
		Kind: KindAppRegistrations, Object: app,
		ResourceNames: map[string]string{"graph-app": "Microsoft Graph"},
		Permissions: map[string]PermissionInfo{
			"p1": {Value: "User.Read", Description: "Sign in and read profile"},
			"p2": {Value: "Directory.Read.All", Description: "Read directory data", AdminConsent: true},
		},
		GrantedScopes: map[string]bool{"User.Read": true},
		GrantedRoles:  map[string]bool{},
	}), "API permissions")

	if len(perms.Fields) != 2 {
		t.Fatalf("got %d rows, want one per permission", len(perms.Fields))
	}
	if got := perms.Columns; len(got) != 6 {
		t.Fatalf("columns = %v, want six", got)
	}

	byName := map[string][]string{}
	for _, f := range perms.Fields {
		if len(f.Cells) != len(perms.Columns) {
			t.Fatalf("row %q has %d cells for %d columns", f.Label, len(f.Cells), len(perms.Columns))
		}
		byName[f.Cells[1]] = f.Cells
	}

	delegated := byName["User.Read"]
	if delegated[0] != "Microsoft Graph" {
		t.Errorf("API column = %q, want the resolved name", delegated[0])
	}
	if delegated[2] != "Delegated" {
		t.Errorf("type = %q, want Delegated", delegated[2])
	}
	if delegated[3] != "no" {
		t.Errorf("admin consent = %q, want no for a user-consentable scope", delegated[3])
	}
	if delegated[4] != "Granted" {
		t.Errorf("status = %q, want Granted", delegated[4])
	}
	if delegated[5] != "Sign in and read profile" {
		t.Errorf("description = %q", delegated[5])
	}

	// An application permission is always admin-only, and this one has not
	// been consented to.
	role := byName["Directory.Read.All"]
	if role[2] != "Application" || role[3] != "yes" {
		t.Errorf("application row = %v, want Application/yes", role)
	}
	if role[4] != "Not granted" {
		t.Errorf("status = %q, want Not granted", role[4])
	}
}

func TestPermissionStatusUnknownWhenConsentCannotBeRead(t *testing.T) {
	// DelegatedPermissionGrant.Read.All can be refused; the rest of the tab
	// is still worth showing.
	app := mustItem(t, `{"id":"o","displayName":"a","requiredResourceAccess":[
		{"resourceAppId":"g","resourceAccess":[{"id":"p1","type":"Scope"}]}]}`)
	perms := findSection(t, Sections(Detail{
		Kind: KindAppRegistrations, Object: app,
		Permissions: map[string]PermissionInfo{"p1": {Value: "User.Read"}},
		GrantsErr:   &APIError{Status: 403},
	}), "API permissions")

	if got := perms.Fields[0].Cells[4]; got != "-" {
		t.Errorf("status = %q, want it unknown rather than guessed", got)
	}
}

func TestUnresolvedPermissionsFallBackToGUIDs(t *testing.T) {
	// A lookup can fail on a locked-down tenant. Showing the GUID beats
	// showing nothing.
	app := mustItem(t, `{"id":"o","displayName":"a","requiredResourceAccess":[
		{"resourceAppId":"unknown-app","resourceAccess":[{"id":"perm-guid","type":"Scope"}]}]}`)
	perms := findSection(t, Sections(Detail{Kind: KindAppRegistrations, Object: app}), "API permissions")

	row := perms.Fields[0].Cells
	if row[0] != "unknown-app" {
		t.Errorf("API = %q, want the raw resource id", row[0])
	}
	if row[1] != "perm-guid" {
		t.Errorf("permission = %q, want the raw permission id", row[1])
	}
}

func TestStructuredPropertiesAreLeftToTheRawView(t *testing.T) {
	// A nested object can only render here as a line of JSON, which is
	// unreadable in a key-and-value pane.
	app := mustItem(t, `{"id":"o","displayName":"a",
		"someNested":{"a":1},"someList":[{"b":2}],"plain":"text","plainList":["x","y"]}`)

	other := findSection(t, Sections(Detail{Kind: KindAppRegistrations, Object: app}), "Other properties")
	for _, f := range other.Fields {
		if strings.Contains(f.Value, "{") || strings.Contains(f.Value, "}") {
			t.Errorf("field %q renders JSON: %q", f.Label, f.Value)
		}
	}

	labels := map[string]string{}
	for _, f := range other.Fields {
		labels[f.Label] = f.Value
	}
	if labels["plain"] != "text" {
		t.Error("a plain property was dropped along with the structured ones")
	}
	if labels["plainList"] != "x, y" {
		t.Errorf("plainList = %q, want a list of strings kept", labels["plainList"])
	}
	if _, shown := labels["someNested"]; shown {
		t.Error("a nested object was rendered as JSON")
	}
}

func TestEnterpriseAppSectionsIncludeUsersAndGroups(t *testing.T) {
	sp := mustItem(t, `{"id":"sp1","appId":"a","displayName":"Contoso",
		"servicePrincipalType":"Application","accountEnabled":true,
		"appRoles":[{"id":"r1","displayName":"Admin","value":"Admin","allowedMemberTypes":["User"],"isEnabled":true}]}`)

	sections := Sections(Detail{
		Kind: KindEnterpriseApps, Object: sp,
		Assignments: []Item{
			{"principalDisplayName": "Ada", "principalType": "User", "appRoleId": "r1"},
			{"principalDisplayName": "Finance", "principalType": "Group", "appRoleId": "unknown"},
		},
	})

	titles := sectionTitles(sections)
	for _, want := range []string{"Essentials", "Properties", "Users and groups"} {
		found := false
		for _, got := range titles {
			if got == want {
				found = true
			}
		}
		if !found {
			t.Errorf("missing section %q; got %v", want, titles)
		}
	}

	assigned := findSection(t, sections, "Users and groups")
	if len(assigned.Fields) != 2 {
		t.Fatalf("got %d assignments, want 2", len(assigned.Fields))
	}
	if !strings.Contains(fieldValue(assigned, "Ada"), "Admin") {
		t.Errorf("Ada's assignment = %q, want the app role name resolved", fieldValue(assigned, "Ada"))
	}
	// An assignment with no matching app role is the portal's "Default Access".
	if !strings.Contains(fieldValue(assigned, "Finance"), "Default Access") {
		t.Errorf("Finance's assignment = %q, want Default Access", fieldValue(assigned, "Finance"))
	}
}

func TestEmptySectionsExplainThemselves(t *testing.T) {
	sp := mustItem(t, `{"id":"sp1","displayName":"Contoso"}`)
	sections := Sections(Detail{Kind: KindEnterpriseApps, Object: sp})

	assigned := findSection(t, sections, "Users and groups")
	if len(assigned.Fields) != 0 || assigned.Note == "" {
		t.Error("an empty assignments section should carry an explanatory note")
	}
	owners := findSection(t, sections, "Owners")
	if owners.Note == "" {
		t.Error("an ownerless object should say so rather than showing a blank section")
	}
}

func TestFailedLookupsSurfaceAsNotes(t *testing.T) {
	sp := mustItem(t, `{"id":"sp1","displayName":"Contoso"}`)
	sections := Sections(Detail{
		Kind: KindEnterpriseApps, Object: sp,
		OwnersErr:      &APIError{Status: 403, Code: "Authorization_RequestDenied"},
		AssignmentsErr: &APIError{Status: 403, Code: "Authorization_RequestDenied"},
	})

	if note := findSection(t, sections, "Owners").Note; !strings.Contains(note, "Could not read owners") {
		t.Errorf("owners note = %q, want the failure explained", note)
	}
	if note := findSection(t, sections, "Users and groups").Note; !strings.Contains(note, "Could not read assignments") {
		t.Errorf("assignments note = %q, want the failure explained", note)
	}
}

func TestCounterpartIsAdvertisedInEssentials(t *testing.T) {
	app := mustItem(t, `{"id":"o","appId":"a","displayName":"Contoso"}`)
	essentials := findSection(t, Sections(Detail{
		Kind: KindAppRegistrations, Object: app,
		Counterpart: &Counterpart{Kind: KindEnterpriseApps, ID: "sp1", DisplayName: "Contoso"},
	}), "Essentials")

	v := fieldValue(essentials, "Enterprise app")
	if !strings.Contains(v, "Contoso") || !strings.Contains(v, "x") {
		t.Errorf("Enterprise app field = %q, want the name and the key hint", v)
	}
}

func TestNothingIsSilentlyDropped(t *testing.T) {
	// Any property not claimed by a named section must still appear, so the
	// grouped view never hides data the raw view would show.
	app := mustItem(t, `{"id":"o","displayName":"a","someUnknownFutureProperty":"keep me"}`)
	sections := Sections(Detail{Kind: KindAppRegistrations, Object: app})

	var all strings.Builder
	for _, s := range sections {
		for _, f := range s.Fields {
			all.WriteString(f.Label + " " + f.Value + " " + strings.Join(f.Values, " "))
		}
	}
	if !strings.Contains(all.String(), "keep me") {
		t.Error("an unrecognised property was dropped from the sectioned view")
	}
}

func TestUserAndGroupSectionsAreGrouped(t *testing.T) {
	u := mustItem(t, `{"id":"u","displayName":"Ada","userPrincipalName":"ada@x.com",
		"jobTitle":"Engineer","department":"R&D","mobilePhone":"+1"}`)
	titles := sectionTitles(Sections(Detail{Kind: KindUsers, Object: u}))
	for _, want := range []string{"Essentials", "Organisation", "Contact"} {
		found := false
		for _, got := range titles {
			if got == want {
				found = true
			}
		}
		if !found {
			t.Errorf("user detail missing %q; got %v", want, titles)
		}
	}

	g := mustItem(t, `{"id":"g","displayName":"Team","mailEnabled":true,"securityEnabled":false,
		"groupTypes":["Unified","DynamicMembership"],"membershipRule":"user.department -eq \"R&D\""}`)
	gs := Sections(Detail{Kind: KindGroups, Object: g})
	essentials := findSection(t, gs, "Essentials")
	if got := fieldValue(essentials, "Group type"); got != "Microsoft 365" {
		t.Errorf("Group type = %q, want Microsoft 365", got)
	}
	if got := fieldValue(essentials, "Membership"); got != "Dynamic" {
		t.Errorf("Membership = %q, want Dynamic", got)
	}
}

func TestComputedFieldsDoNotDuplicateTheirSources(t *testing.T) {
	// A computed field like "Account enabled" reads accountEnabled. If the
	// raw property is not marked consumed, it reappears verbatim under
	// "Other properties" and the pane shows the same fact twice.
	sp := mustItem(t, `{"id":"sp1","displayName":"Contoso","appId":"a",
		"accountEnabled":false,"appRoleAssignmentRequired":true,
		"createdDateTime":"2020-02-02T02:00:00Z","signInAudience":"AzureADMyOrg"}`)

	sections := Sections(Detail{Kind: KindEnterpriseApps, Object: sp})
	for _, s := range sections {
		if s.Title != "Other properties" {
			continue
		}
		for _, f := range s.Fields {
			switch f.Label {
			case "accountEnabled", "appRoleAssignmentRequired", "createdDateTime", "signInAudience":
				t.Errorf("%q is shown raw in Other properties as well as in its own section", f.Label)
			}
		}
	}

	u := mustItem(t, `{"id":"u","displayName":"Ada","accountEnabled":true,
		"createdDateTime":"2023-01-01T00:00:00Z","onPremisesSyncEnabled":true}`)
	for _, s := range Sections(Detail{Kind: KindUsers, Object: u}) {
		if s.Title != "Other properties" {
			continue
		}
		for _, f := range s.Fields {
			switch f.Label {
			case "accountEnabled", "createdDateTime", "onPremisesSyncEnabled":
				t.Errorf("%q is duplicated in Other properties", f.Label)
			}
		}
	}

	g := mustItem(t, `{"id":"g","displayName":"Team","mailEnabled":true,
		"securityEnabled":false,"groupTypes":["Unified"]}`)
	for _, s := range Sections(Detail{Kind: KindGroups, Object: g}) {
		if s.Title != "Other properties" {
			continue
		}
		for _, f := range s.Fields {
			switch f.Label {
			case "mailEnabled", "securityEnabled":
				t.Errorf("%q is duplicated in Other properties", f.Label)
			}
		}
	}
}

func TestUserDetailShowsGroupMembership(t *testing.T) {
	u := mustItem(t, `{"id":"u1","displayName":"Ada","userPrincipalName":"ada@x.com"}`)
	sections := Sections(Detail{
		Kind: KindUsers, Object: u,
		Groups: []Item{
			{"id": "g1", "displayName": "Research", "mail": "research@x.com",
				"@odata.type": "#microsoft.graph.group"},
			{"id": "r1", "displayName": "Global Reader",
				"@odata.type": "#microsoft.graph.directoryRole"},
		},
	})

	groups := findSection(t, sections, "Groups")
	if len(groups.Fields) != 2 {
		t.Fatalf("got %d entries, want 2", len(groups.Fields))
	}
	if got := fieldValue(groups, "Research"); !strings.Contains(got, "Group") {
		t.Errorf("Research = %q, want it identified as a group", got)
	}
	// memberOf returns a heterogeneous collection; a directory role is not a
	// group and should not be labelled as one.
	if got := fieldValue(groups, "Global Reader"); !strings.Contains(got, "Directory role") {
		t.Errorf("Global Reader = %q, want it identified as a directory role", got)
	}
}

func TestGroupDetailShowsOwnersAndMembers(t *testing.T) {
	g := mustItem(t, `{"id":"g1","displayName":"Research","securityEnabled":true}`)
	sections := Sections(Detail{
		Kind: KindGroups, Object: g,
		Owners: []Item{{"id": "u1", "displayName": "Ada", "userPrincipalName": "ada@x.com"}},
		Members: []Item{
			{"id": "u2", "displayName": "Grace", "userPrincipalName": "grace@x.com",
				"@odata.type": "#microsoft.graph.user"},
			{"id": "g2", "displayName": "Nested", "@odata.type": "#microsoft.graph.group"},
		},
	})

	owners := findSection(t, sections, "Owners")
	if len(owners.Fields) != 1 {
		t.Errorf("got %d owners, want 1", len(owners.Fields))
	}
	members := findSection(t, sections, "Members")
	if len(members.Fields) != 2 {
		t.Fatalf("got %d members, want 2", len(members.Fields))
	}
	if got := fieldValue(members, "Nested"); !strings.Contains(got, "Group") {
		t.Errorf("nested group = %q, want it identified as a group", got)
	}
	if got := fieldValue(members, "Grace"); !strings.Contains(got, "grace@x.com") {
		t.Errorf("Grace = %q, want the UPN shown", got)
	}
}

func TestEmptyMembershipSectionsExplainThemselves(t *testing.T) {
	u := mustItem(t, `{"id":"u1","displayName":"Ada"}`)
	if note := findSection(t, Sections(Detail{Kind: KindUsers, Object: u}), "Groups").Note; note == "" {
		t.Error("a user in no groups should say so rather than render blank")
	}

	g := mustItem(t, `{"id":"g1","displayName":"Team"}`)
	gs := Sections(Detail{Kind: KindGroups, Object: g})
	if note := findSection(t, gs, "Members").Note; note == "" {
		t.Error("an empty group should say so")
	}
}

func TestTruncatedMembershipSaysSo(t *testing.T) {
	g := mustItem(t, `{"id":"g1","displayName":"Everyone"}`)
	members := make([]Item, 3)
	for i := range members {
		members[i] = Item{"id": "u", "displayName": "member"}
	}
	s := findSection(t, Sections(Detail{
		Kind: KindGroups, Object: g, Members: members, MembersTruncated: true,
	}), "Members")

	if !strings.Contains(s.Note, "the group has more") {
		t.Errorf("note = %q, want it to admit the list is partial", s.Note)
	}
}

func TestMembershipFailuresUseTheShortErrorForm(t *testing.T) {
	// A section note is one line on a crowded pane; a Graph code says as much
	// as the full message.
	u := mustItem(t, `{"id":"u1","displayName":"Ada"}`)
	s := findSection(t, Sections(Detail{
		Kind: KindUsers, Object: u,
		GroupsErr: &APIError{Status: 403, Code: "Authorization_RequestDenied",
			Message: "Insufficient privileges to complete the operation."},
	}), "Groups")

	if !strings.Contains(s.Note, "Authorization_RequestDenied") {
		t.Errorf("note = %q, want the Graph code", s.Note)
	}
	if strings.Contains(s.Note, "Insufficient privileges to complete") {
		t.Errorf("note = %q, want the long message left out", s.Note)
	}
}

func TestObjectKindFallsBackWhenODataTypeIsAbsent(t *testing.T) {
	// $select suppresses the annotation on some collections, so the shape of
	// the object has to decide.
	for _, tc := range []struct {
		name string
		item Item
		want string
	}{
		{"user by upn", Item{"userPrincipalName": "a@x.com"}, "User"},
		{"group by flag", Item{"securityEnabled": true}, "Group"},
		{"unknown", Item{"id": "x"}, "Object"},
		{"annotated", Item{"@odata.type": "#microsoft.graph.servicePrincipal"}, "Service principal"},
	} {
		if got := objectKind(tc.item); got != tc.want {
			t.Errorf("%s: objectKind = %q, want %q", tc.name, got, tc.want)
		}
	}
}
