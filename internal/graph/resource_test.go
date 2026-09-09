package graph

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestLookupResolvesAliasesAndKinds(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want Kind
	}{
		{"users", KindUsers},
		{"u", KindUsers},
		{"USERS", KindUsers},
		{"groups", KindGroups},
		{"g", KindGroups},
		{"apps", KindAppRegistrations},
		{"appreg", KindAppRegistrations},
		{"applications", KindAppRegistrations},
		{"sp", KindEnterpriseApps},
		{"enterprise", KindEnterpriseApps},
		{"  ent  ", KindEnterpriseApps},
		{"devices", KindDevices},
		{"dev", KindDevices},
	} {
		got, ok := Lookup(tc.in)
		if !ok {
			t.Errorf("Lookup(%q) not found", tc.in)
			continue
		}
		if got.Kind != tc.want {
			t.Errorf("Lookup(%q) = %s, want %s", tc.in, got.Kind, tc.want)
		}
	}

	if _, ok := Lookup("printers"); ok {
		t.Error("Lookup(\"printers\") resolved, want not found")
	}
}

func TestEveryColumnPropertyIsSelected(t *testing.T) {
	// A column reading a property absent from $select renders blank forever,
	// which is invisible at runtime. Catch it here instead: feed each
	// resource an object carrying only its selected properties and assert the
	// columns still produce output.
	for _, res := range All() {
		selected := make(map[string]bool, len(res.Select))
		for _, s := range res.Select {
			selected[s] = true
		}
		if !selected["id"] {
			t.Errorf("%s: $select omits id, which the detail view needs", res.Kind)
		}
		for _, f := range res.SearchFields {
			if !selected[f] {
				t.Errorf("%s: search field %q is not in $select", res.Kind, f)
			}
		}
		if len(res.Columns) == 0 {
			t.Errorf("%s: no columns defined", res.Kind)
		}
		for _, c := range res.Columns {
			if c.MinWidth <= 0 {
				t.Errorf("%s: column %q has non-positive MinWidth", res.Kind, c.Title)
			}
			if c.Value == nil {
				t.Errorf("%s: column %q has no accessor", res.Kind, c.Title)
			}
		}
	}
}

func TestSearchExprBuildsGraphSyntax(t *testing.T) {
	users, _ := Lookup("users")
	got := Query{SearchFields: users.SearchFields, SearchTerm: "ada"}.searchExpr()

	for _, want := range []string{`"displayName:ada"`, `"userPrincipalName:ada"`, " OR "} {
		if !strings.Contains(got, want) {
			t.Errorf("searchExpr = %q, want it to contain %q", got, want)
		}
	}
}

func TestSearchExprStripsQuotesThatWouldBreakTheQuery(t *testing.T) {
	users, _ := Lookup("users")
	got := Query{SearchFields: users.SearchFields, SearchTerm: `ad"a\b`}.searchExpr()

	// Exactly two quotes per term -- the delimiters -- and no stray backslash.
	if strings.Count(got, `\`) != 0 {
		t.Errorf("searchExpr = %q, want backslashes stripped", got)
	}
	if !strings.Contains(got, `"displayName:adab"`) {
		t.Errorf("searchExpr = %q, want the embedded quote removed", got)
	}
}

func TestSearchExprEmptyForBlankTerm(t *testing.T) {
	users, _ := Lookup("users")
	if got := (Query{SearchFields: users.SearchFields, SearchTerm: "   "}).searchExpr(); got != "" {
		t.Errorf("searchExpr(blank) = %q, want empty", got)
	}
	if got := (Query{SearchTerm: "ada"}).searchExpr(); got != "" {
		t.Errorf("searchExpr with no fields = %q, want empty", got)
	}
}

func TestGroupTypeMatchesPortalLabels(t *testing.T) {
	for _, tc := range []struct {
		name string
		item Item
		want string
	}{
		{"unified", Item{"groupTypes": []any{"Unified"}}, "Microsoft 365"},
		{"security", Item{"securityEnabled": true, "mailEnabled": false}, "Security"},
		{"distribution", Item{"securityEnabled": false, "mailEnabled": true}, "Distribution"},
		{"mail-enabled security", Item{"securityEnabled": true, "mailEnabled": true}, "Mail-enabled security"},
		{"neither", Item{}, "Unknown"},
	} {
		if got := GroupType(tc.item); got != tc.want {
			t.Errorf("%s: GroupType = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestGroupMembership(t *testing.T) {
	dynamic := Item{"groupTypes": []any{"Unified", "DynamicMembership"}}
	if got := GroupMembership(dynamic); got != "Dynamic" {
		t.Errorf("GroupMembership(dynamic) = %q, want Dynamic", got)
	}
	if got := GroupMembership(Item{"groupTypes": []any{"Unified"}}); got != "Assigned" {
		t.Errorf("GroupMembership(static) = %q, want Assigned", got)
	}
}

func TestSignInAudienceIsShortened(t *testing.T) {
	for in, want := range map[string]string{
		"AzureADMyOrg":                       "Single tenant",
		"AzureADMultipleOrgs":                "Multitenant",
		"AzureADandPersonalMicrosoftAccount": "Multi + personal",
		"":                                   "",
		"SomethingNew":                       "SomethingNew",
	} {
		if got := SignInAudience(Item{"signInAudience": in}); got != want {
			t.Errorf("SignInAudience(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSoonestCredentialExpiryPicksTheEarliest(t *testing.T) {
	soon := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	later := time.Now().Add(400 * 24 * time.Hour).UTC().Format(time.RFC3339)

	item := Item{
		"passwordCredentials": []any{
			map[string]any{"endDateTime": later},
			map[string]any{"endDateTime": soon},
		},
		"keyCredentials": []any{
			map[string]any{"endDateTime": later},
		},
	}
	if got := SoonestCredentialExpiry(item); got != "1d" && got != "2d" {
		t.Errorf("SoonestCredentialExpiry = %q, want roughly 2d", got)
	}
}

func TestSoonestCredentialExpiryFlagsExpired(t *testing.T) {
	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	item := Item{"passwordCredentials": []any{map[string]any{"endDateTime": past}}}
	if got := SoonestCredentialExpiry(item); got != "expired" {
		t.Errorf("SoonestCredentialExpiry = %q, want expired", got)
	}
}

func TestSoonestCredentialExpiryHandlesNoCredentials(t *testing.T) {
	if got := SoonestCredentialExpiry(Item{}); got != "-" {
		t.Errorf("SoonestCredentialExpiry(empty) = %q, want -", got)
	}
	// Malformed entries must not panic or be mistaken for a real expiry.
	bad := Item{"passwordCredentials": []any{"not-an-object", map[string]any{"endDateTime": "garbage"}}}
	if got := SoonestCredentialExpiry(bad); got != "-" {
		t.Errorf("SoonestCredentialExpiry(malformed) = %q, want -", got)
	}
}

func TestRowRendersOneCellPerColumn(t *testing.T) {
	users, _ := Lookup("users")
	var item Item
	if err := json.Unmarshal([]byte(`{
		"id":"1","displayName":"Ada Lovelace","userPrincipalName":"ada@example.com",
		"userType":"Member","accountEnabled":true,"jobTitle":"Engineer","department":"R&D"
	}`), &item); err != nil {
		t.Fatal(err)
	}

	row := users.Row(item)
	if len(row) != len(users.Columns) {
		t.Fatalf("row has %d cells, want %d", len(row), len(users.Columns))
	}
	if row[0] != "Ada Lovelace" {
		t.Errorf("row[0] = %q, want Ada Lovelace", row[0])
	}
	if row[3] != "yes" {
		t.Errorf("enabled cell = %q, want yes", row[3])
	}
}
