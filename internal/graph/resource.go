package graph

import (
	"fmt"
	"strings"
	"time"
)

// Kind identifies one browsable directory collection.
type Kind string

const (
	KindUsers            Kind = "users"
	KindGroups           Kind = "groups"
	KindAppRegistrations Kind = "applications"
	KindEnterpriseApps   Kind = "servicePrincipals"
)

// Title returns the human name of a collection.
func (k Kind) Title() string {
	for _, r := range All() {
		if r.Kind == k {
			return r.Title
		}
	}
	return string(k)
}

// Column is one table column. Width is negotiated at render time: every
// column is guaranteed MinWidth, and any leftover terminal width is shared
// out in proportion to Weight. A Weight of zero pins the column to MinWidth,
// which is what fixed-shape fields like "enabled" or an age want.
type Column struct {
	Title    string
	MinWidth int
	Weight   int
	Value    func(Item) string
}

// Resource describes how to list, search and display one collection.
type Resource struct {
	Kind Kind
	// Title is the human name shown in the header.
	Title string
	// Description is the one-line explanation shown on the dashboard.
	Description string
	// Aliases are what the user can type after ":" to reach this view.
	Aliases []string
	// Path is the Graph collection path.
	Path string
	// Select is the $select projection. Every property a Column reads must
	// appear here, or the column silently renders blank.
	Select []string
	// OrderBy is the default server-side sort, dropped automatically while a
	// server search is active because Graph rejects the combination.
	OrderBy string
	// SearchFields are the properties a server-side search covers.
	SearchFields []string
	// Columns are the table columns, left to right.
	Columns []Column
	// Accent tints the resource in the header, the way k9s colours contexts.
	Accent string
}

// SearchExpr renders term into Graph's $search syntax across SearchFields,
// e.g. `"displayName:ada" OR "mail:ada"`.
//
// Double quotes and backslashes are stripped rather than escaped: Graph's
// search grammar has no escape sequence for them inside a quoted term, so
// removing them is the only way to keep a user's stray quote from producing
// a malformed query.
func (r Resource) SearchExpr(term string) string {
	term = strings.TrimSpace(strings.NewReplacer(`"`, "", `\`, "").Replace(term))
	if term == "" || len(r.SearchFields) == 0 {
		return ""
	}
	parts := make([]string, 0, len(r.SearchFields))
	for _, f := range r.SearchFields {
		parts = append(parts, fmt.Sprintf("%q", f+":"+term))
	}
	return strings.Join(parts, " OR ")
}

// Row renders one item into cell strings, one per column.
func (r Resource) Row(i Item) []string {
	cells := make([]string, len(r.Columns))
	for n, c := range r.Columns {
		cells[n] = c.Value(i)
	}
	return cells
}

// All returns the browsable resources in display order.
func All() []Resource {
	return []Resource{usersResource(), groupsResource(), appRegistrationsResource(), enterpriseAppsResource()}
}

// Lookup resolves a resource by kind, title or alias, case-insensitively.
func Lookup(name string) (Resource, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, r := range All() {
		if strings.ToLower(string(r.Kind)) == name || strings.ToLower(r.Title) == name {
			return r, true
		}
		for _, a := range r.Aliases {
			if a == name {
				return r, true
			}
		}
	}
	return Resource{}, false
}

func usersResource() Resource {
	return Resource{
		Kind:        KindUsers,
		Title:       "Users",
		Description: "People and guests in the directory",
		Aliases:     []string{"users", "user", "u"},
		Path:        "/users",
		Select: []string{
			"id", "displayName", "userPrincipalName", "mail", "userType",
			"accountEnabled", "jobTitle", "department", "officeLocation",
			"mobilePhone", "createdDateTime", "onPremisesSyncEnabled",
		},
		OrderBy:      "displayName",
		SearchFields: []string{"displayName", "userPrincipalName", "mail"},
		Accent:       "#7aa2f7",
		Columns: []Column{
			{Title: "NAME", MinWidth: 16, Weight: 3, Value: func(i Item) string { return i.String("displayName") }},
			{Title: "USER PRINCIPAL NAME", MinWidth: 20, Weight: 4, Value: func(i Item) string { return i.String("userPrincipalName") }},
			{Title: "TYPE", MinWidth: 6, Value: func(i Item) string { return i.String("userType") }},
			{Title: "ENABLED", MinWidth: 7, Value: func(i Item) string { return YesNo(i, "accountEnabled") }},
			{Title: "JOB TITLE", MinWidth: 10, Weight: 2, Value: func(i Item) string { return i.String("jobTitle") }},
			{Title: "DEPARTMENT", MinWidth: 10, Weight: 2, Value: func(i Item) string { return i.String("department") }},
			{Title: "AGE", MinWidth: 6, Value: func(i Item) string { return AgeOf(i, "createdDateTime") }},
		},
	}
}

func groupsResource() Resource {
	return Resource{
		Kind:        KindGroups,
		Title:       "Groups",
		Description: "Security and Microsoft 365 groups",
		Aliases:     []string{"groups", "group", "g"},
		Path:        "/groups",
		Select: []string{
			"id", "displayName", "description", "mail", "mailNickname",
			"mailEnabled", "securityEnabled", "groupTypes", "visibility",
			"createdDateTime", "membershipRule", "onPremisesSyncEnabled",
			"isAssignableToRole",
		},
		OrderBy:      "displayName",
		SearchFields: []string{"displayName", "mail", "description"},
		Accent:       "#9ece6a",
		Columns: []Column{
			{Title: "NAME", MinWidth: 16, Weight: 3, Value: func(i Item) string { return i.String("displayName") }},
			{Title: "TYPE", MinWidth: 12, Weight: 1, Value: GroupType},
			{Title: "MEMBERSHIP", MinWidth: 10, Value: GroupMembership},
			{Title: "MAIL", MinWidth: 14, Weight: 3, Value: func(i Item) string { return i.String("mail") }},
			{Title: "VISIBILITY", MinWidth: 10, Value: func(i Item) string { return i.String("visibility") }},
			{Title: "SYNCED", MinWidth: 6, Value: func(i Item) string { return YesNo(i, "onPremisesSyncEnabled") }},
			{Title: "AGE", MinWidth: 6, Value: func(i Item) string { return AgeOf(i, "createdDateTime") }},
		},
	}
}

// GroupType collapses the mailEnabled/securityEnabled/groupTypes triple into
// the single label the Entra portal shows.
func GroupType(i Item) string {
	types := i.Strings("groupTypes")
	for _, t := range types {
		if strings.EqualFold(t, "Unified") {
			return "Microsoft 365"
		}
	}
	mail, _ := i.Bool("mailEnabled")
	sec, _ := i.Bool("securityEnabled")
	switch {
	case mail && sec:
		return "Mail-enabled security"
	case sec:
		return "Security"
	case mail:
		return "Distribution"
	default:
		return "Unknown"
	}
}

// GroupMembership reports whether membership is rule-driven or assigned.
func GroupMembership(i Item) string {
	for _, t := range i.Strings("groupTypes") {
		if strings.EqualFold(t, "DynamicMembership") {
			return "Dynamic"
		}
	}
	return "Assigned"
}

func appRegistrationsResource() Resource {
	return Resource{
		Kind:        KindAppRegistrations,
		Title:       "App registrations",
		Description: "Applications defined in this tenant",
		Aliases:     []string{"appregs", "appreg", "apps", "app", "applications", "a"},
		Path:        "/applications",
		Select: []string{
			"id", "appId", "displayName", "signInAudience", "createdDateTime",
			"publisherDomain", "description", "identifierUris", "tags",
			"passwordCredentials", "keyCredentials", "web", "api",
		},
		OrderBy:      "displayName",
		SearchFields: []string{"displayName", "description"},
		Accent:       "#e0af68",
		Columns: []Column{
			{Title: "NAME", MinWidth: 16, Weight: 4, Value: func(i Item) string { return i.String("displayName") }},
			{Title: "APP ID", MinWidth: 36, Value: func(i Item) string { return i.String("appId") }},
			{Title: "AUDIENCE", MinWidth: 12, Weight: 1, Value: SignInAudience},
			{Title: "SECRETS", MinWidth: 7, Value: func(i Item) string { return credentialCount(i, "passwordCredentials") }},
			{Title: "CERTS", MinWidth: 5, Value: func(i Item) string { return credentialCount(i, "keyCredentials") }},
			{Title: "CRED EXP", MinWidth: 8, Value: SoonestCredentialExpiry},
			{Title: "AGE", MinWidth: 6, Value: func(i Item) string { return AgeOf(i, "createdDateTime") }},
		},
	}
}

func enterpriseAppsResource() Resource {
	return Resource{
		Kind:        KindEnterpriseApps,
		Title:       "Enterprise apps",
		Description: "Service principals: apps that can sign in here",
		Aliases:     []string{"entapps", "entapp", "ent", "enterprise", "sp", "sps", "serviceprincipal", "serviceprincipals", "e"},
		Path:        "/servicePrincipals",
		Select: []string{
			"id", "appId", "displayName", "accountEnabled", "servicePrincipalType",
			"appRoleAssignmentRequired", "publisherName", "signInAudience",
			"homepage", "tags", "appOwnerOrganizationId", "createdDateTime",
			"loginUrl", "preferredSingleSignOnMode", "servicePrincipalNames",
		},
		OrderBy:      "displayName",
		SearchFields: []string{"displayName", "publisherName"},
		Accent:       "#bb9af7",
		Columns: []Column{
			{Title: "NAME", MinWidth: 16, Weight: 4, Value: func(i Item) string { return i.String("displayName") }},
			{Title: "APP ID", MinWidth: 36, Value: func(i Item) string { return i.String("appId") }},
			{Title: "TYPE", MinWidth: 12, Weight: 1, Value: func(i Item) string { return i.String("servicePrincipalType") }},
			{Title: "ENABLED", MinWidth: 7, Value: func(i Item) string { return YesNo(i, "accountEnabled") }},
			{Title: "ASSIGN REQ", MinWidth: 10, Value: func(i Item) string { return YesNo(i, "appRoleAssignmentRequired") }},
			{Title: "SSO", MinWidth: 10, Weight: 1, Value: func(i Item) string {
				return firstNonEmpty(i.String("preferredSingleSignOnMode"), "-")
			}},
			{Title: "PUBLISHER", MinWidth: 12, Weight: 2, Value: func(i Item) string { return i.String("publisherName") }},
		},
	}
}

// SignInAudience shortens Graph's verbose audience constants to something
// that fits a column.
func SignInAudience(i Item) string {
	switch i.String("signInAudience") {
	case "AzureADMyOrg":
		return "Single tenant"
	case "AzureADMultipleOrgs":
		return "Multitenant"
	case "AzureADandPersonalMicrosoftAccount":
		return "Multi + personal"
	case "PersonalMicrosoftAccount":
		return "Personal"
	case "":
		return ""
	default:
		return i.String("signInAudience")
	}
}

// credentialCount counts entries in a credential collection.
func credentialCount(i Item, key string) string {
	raw, ok := i[key].([]any)
	if !ok {
		return "-"
	}
	return fmt.Sprint(len(raw))
}

// SoonestCredentialExpiry reports when the app registration's next secret or
// certificate lapses. Expiring credentials are the most common cause of a
// production outage traced back to an app registration, so this is worth a
// column of its own rather than being buried in the detail view.
func SoonestCredentialExpiry(i Item) string {
	var soonest time.Time
	for _, key := range []string{"passwordCredentials", "keyCredentials"} {
		raw, ok := i[key].([]any)
		if !ok {
			continue
		}
		for _, entry := range raw {
			cred, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			end, ok := cred["endDateTime"].(string)
			if !ok {
				continue
			}
			t, err := time.Parse(time.RFC3339, end)
			if err != nil {
				continue
			}
			if soonest.IsZero() || t.Before(soonest) {
				soonest = t
			}
		}
	}
	if soonest.IsZero() {
		return "-"
	}
	return Until(soonest)
}
