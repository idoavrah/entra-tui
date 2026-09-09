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
		Kind:            KindAppRegistrations,
		Object:          app,
		ResourceNames:   map[string]string{"00000003-0000-0000-c000-000000000000": "Microsoft Graph"},
		PermissionNames: map[string]string{"perm-1": "User.Read.All"},
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

func TestAPIPermissionsResolveToNames(t *testing.T) {
	app := mustItem(t, `{"id":"o","displayName":"a","requiredResourceAccess":[
		{"resourceAppId":"graph-app","resourceAccess":[
			{"id":"p1","type":"Scope"},{"id":"p2","type":"Role"}]}]}`)

	perms := findSection(t, Sections(Detail{
		Kind: KindAppRegistrations, Object: app,
		ResourceNames:   map[string]string{"graph-app": "Microsoft Graph"},
		PermissionNames: map[string]string{"p1": "User.Read", "p2": "Directory.Read.All"},
	}), "API permissions")

	if len(perms.Fields) != 1 {
		t.Fatalf("got %d resources, want 1", len(perms.Fields))
	}
	f := perms.Fields[0]
	if f.Label != "Microsoft Graph" {
		t.Errorf("resource label = %q, want the resolved API name", f.Label)
	}
	joined := strings.Join(f.Values, "\n")
	for _, want := range []string{"User.Read", "Directory.Read.All", "Delegated", "Application"} {
		if !strings.Contains(joined, want) {
			t.Errorf("permissions %q missing %q", joined, want)
		}
	}
}

func TestUnresolvedPermissionsFallBackToGUIDs(t *testing.T) {
	// A lookup can fail on a locked-down tenant. Showing the GUID beats
	// showing nothing.
	app := mustItem(t, `{"id":"o","displayName":"a","requiredResourceAccess":[
		{"resourceAppId":"unknown-app","resourceAccess":[{"id":"perm-guid","type":"Scope"}]}]}`)
	perms := findSection(t, Sections(Detail{Kind: KindAppRegistrations, Object: app}), "API permissions")

	joined := strings.Join(perms.Fields[0].Values, "")
	if !strings.Contains(joined, "perm-guid") {
		t.Errorf("values = %q, want the raw permission id", joined)
	}
	if perms.Fields[0].Label != "unknown-app" {
		t.Errorf("label = %q, want the raw resource id", perms.Fields[0].Label)
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
