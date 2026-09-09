package graph

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Field is one labelled value in a detail section. A field carries either a
// single Value or a list of Values; a list renders one entry per line under
// the label, which is how redirect URIs, permissions and roles read best.
type Field struct {
	Label  string
	Value  string
	Values []string
	// Warn marks a value that deserves attention, such as a lapsed credential.
	Warn bool
	// ID is the directory object this field stands for, when the field is a
	// real object rather than a property. Only these can be removed.
	ID string
	// Kind is the view that describes that object, when entra-tui has one.
	// A field carrying both an ID and a Kind can be opened in its own right.
	Kind Kind
	// Cells are the row's values when its section is columnar. Label and
	// Value stay populated for everything that reads a field as a pair --
	// confirmations, for one.
	Cells []string
}

// Empty reports whether the field has nothing worth rendering.
func (f Field) Empty() bool {
	return strings.TrimSpace(f.Value) == "" && len(f.Values) == 0
}

// Section is a titled group of fields in the detail view.
type Section struct {
	Title  string
	Fields []Field
	// Note explains an empty or partial section, typically a permission the
	// signed-in user lacks.
	Note string
	// Relationship names the editable Graph collection this section lists.
	// An empty relationship means the section is read-only.
	Relationship Relationship
	// List marks a section that enumerates objects rather than describing
	// the one on screen. These are shown as tabs below the properties: an
	// unbounded list of members has no business pushing an object's own
	// fields off the top of the pane.
	List bool
	// Columns are the headings of a columnar list. When set, each field
	// carries Cells in the same order.
	Columns []string
}

// PermissionInfo describes one permission an API publishes.
type PermissionInfo struct {
	// Value is the permission's name, such as "User.Read.All".
	Value string
	// Description is what the consent prompt would say it does.
	Description string
	// AdminConsent reports that only an administrator can grant it.
	AdminConsent bool
}

// Detail is everything the detail view knows about one object: the object
// itself plus the follow-up lookups that make an app registration or an
// enterprise application intelligible.
type Detail struct {
	Kind   Kind
	Object Item

	// Owners of the application or service principal.
	Owners []Item
	// OwnersErr records why owners could not be listed, if they could not.
	OwnersErr error

	// Groups are the groups and directory roles the object belongs to.
	Groups          []Item
	GroupsTruncated bool
	GroupsErr       error

	// Members are the direct members of a group.
	Members          []Item
	MembersTruncated bool
	MembersErr       error

	// Assignments are appRoleAssignedTo entries for a service principal --
	// the users and groups the enterprise app is assigned to.
	Assignments []Item
	// AssignmentsTruncated reports that more assignments exist than were read.
	AssignmentsTruncated bool
	AssignmentsErr       error

	// ResourceNames maps a resourceAppId to the API's display name, and
	// Permissions maps a permission id to what it is. Without these, the
	// API permissions section is a wall of GUIDs.
	ResourceNames map[string]string
	Permissions   map[string]PermissionInfo

	// GrantedScopes and GrantedRoles are the permissions actually consented
	// to, keyed by scope value and by app role id respectively -- the two
	// halves of consent are recorded differently by Graph.
	GrantedScopes map[string]bool
	GrantedRoles  map[string]bool
	// GrantsErr records why consent could not be read, if it could not.
	GrantsErr error

	// Counterpart is the paired object: an app registration's service
	// principal, or an enterprise app's application object. Nil when the
	// pairing does not exist in this tenant, which is normal for
	// Microsoft-published apps.
	Counterpart *Counterpart
}

// Counterpart identifies the object on the other side of the
// application / service principal pair.
type Counterpart struct {
	Kind        Kind
	ID          string
	DisplayName string
}

// Sections renders the object into the grouped layout for its kind.
func Sections(d Detail) []Section {
	switch d.Kind {
	case KindAppRegistrations:
		return appRegistrationSections(d)
	case KindEnterpriseApps:
		return enterpriseAppSections(d)
	case KindUsers:
		return userSections(d)
	case KindGroups:
		return groupSections(d)
	case KindDevices:
		return deviceSections(d)
	default:
		return []Section{{Title: "Properties", Fields: allFields(d.Object, nil)}}
	}
}

// ---------------------------------------------------------------- builders

func userSections(d Detail) []Section {
	o := d.Object
	used := newFieldSet()

	essentials := Section{Title: "Essentials", Fields: fields(o, used,
		f("Display name", "displayName"),
		f("User principal name", "userPrincipalName"),
		f("Mail", "mail"),
		f("Object ID", "id"),
		f("User type", "userType"),
		fn("Account enabled", func(i Item) string { return YesNo(i, "accountEnabled") }, "accountEnabled"),
		fn("Created", func(i Item) string { return dateWithAge(i, "createdDateTime") }, "createdDateTime"),
	)}

	org := Section{Title: "Organisation", Fields: fields(o, used,
		f("Job title", "jobTitle"),
		f("Department", "department"),
		f("Company", "companyName"),
		f("Employee ID", "employeeId"),
		f("Office", "officeLocation"),
		f("Usage location", "usageLocation"),
	)}

	contact := Section{Title: "Contact", Fields: fields(o, used,
		f("Mobile", "mobilePhone"),
		fl("Business phones", "businessPhones"),
		fl("Other mails", "otherMails"),
		fl("Proxy addresses", "proxyAddresses"),
	)}

	sync := Section{Title: "On-premises sync", Fields: fields(o, used,
		fn("Synced", func(i Item) string { return YesNo(i, "onPremisesSyncEnabled") }, "onPremisesSyncEnabled"),
		f("SAM account name", "onPremisesSamAccountName"),
		f("Domain", "onPremisesDomainName"),
		f("Last sync", "onPremisesLastSyncDateTime"),
	)}

	return compact(essentials, org, groupsSection(d), contact, sync,
		Section{Title: "Other properties", Fields: allFields(o, used)})
}

func groupSections(d Detail) []Section {
	o := d.Object
	used := newFieldSet()

	essentials := Section{Title: "Essentials", Fields: fields(o, used,
		f("Display name", "displayName"),
		f("Description", "description"),
		f("Object ID", "id"),
		fn("Group type", GroupType, "mailEnabled", "securityEnabled"),
		fn("Membership", GroupMembership),
		f("Visibility", "visibility"),
		fn("Created", func(i Item) string { return dateWithAge(i, "createdDateTime") }, "createdDateTime"),
	)}

	membership := Section{Title: "Membership", Fields: fields(o, used,
		fl("Group types", "groupTypes"),
		f("Dynamic rule", "membershipRule"),
		f("Rule state", "membershipRuleProcessingState"),
		fn("Role-assignable", func(i Item) string { return YesNo(i, "isAssignableToRole") }, "isAssignableToRole"),
	)}

	mail := Section{Title: "Mail", Fields: fields(o, used,
		f("Mail", "mail"),
		f("Nickname", "mailNickname"),
		fn("Mail enabled", func(i Item) string { return YesNo(i, "mailEnabled") }, "mailEnabled"),
		fn("Security enabled", func(i Item) string { return YesNo(i, "securityEnabled") }, "securityEnabled"),
		fl("Proxy addresses", "proxyAddresses"),
	)}

	sync := Section{Title: "On-premises sync", Fields: fields(o, used,
		fn("Synced", func(i Item) string { return YesNo(i, "onPremisesSyncEnabled") }, "onPremisesSyncEnabled"),
		f("Last sync", "onPremisesLastSyncDateTime"),
	)}

	return compact(essentials, membership, ownersSection(d), membersSection(d), mail, sync,
		Section{Title: "Other properties", Fields: allFields(o, used)})
}

// groupsSection lists what the object is a member of.
func groupsSection(d Detail) Section {
	s := Section{Title: "Groups", List: true, Columns: []string{"NAME", "TYPE", "MAIL"}}
	if d.GroupsErr != nil {
		s.Note = "Could not read group membership: " + shortError(d.GroupsErr)
		return s
	}
	if len(d.Groups) == 0 {
		s.Note = "Not a member of any group or directory role."
		return s
	}
	for _, g := range d.Groups {
		name := orDash(g.String("displayName"))
		s.Fields = append(s.Fields, Field{
			Label: name,
			Value: describeDirectoryObject(g),
			ID:    g.ID(),
			Kind:  ObjectViewKind(g),
			Cells: []string{name, objectKind(g), orDash(g.String("mail"))},
		})
	}
	if d.GroupsTruncated {
		s.Note = fmt.Sprintf("Showing the first %d groups.", len(d.Groups))
	}
	return s
}

// membersSection lists a group's direct members.
func membersSection(d Detail) Section {
	s := Section{Title: "Members", Relationship: RelMembers, List: true,
		Columns: []string{"NAME", "TYPE", "SIGN-IN NAME"}}
	if d.MembersErr != nil {
		s.Note = "Could not read members: " + shortError(d.MembersErr)
		return s
	}
	if len(d.Members) == 0 {
		s.Note = "This group has no direct members."
		return s
	}
	for _, m := range d.Members {
		name := orDash(firstNonEmpty(m.String("displayName"), m.String("userPrincipalName"), m.ID()))
		s.Fields = append(s.Fields, Field{
			Label: name,
			Value: describeDirectoryObject(m),
			ID:    m.ID(),
			Kind:  ObjectViewKind(m),
			Cells: []string{name, objectKind(m),
				orDash(firstNonEmpty(m.String("userPrincipalName"), m.String("mail")))},
		})
	}
	if d.MembersTruncated {
		s.Note = fmt.Sprintf("Showing the first %d members; the group has more.", len(d.Members))
	}
	return s
}

// describeDirectoryObject summarises a membership entry: what kind of object
// it is, and the identifier a reader would recognise it by.
func describeDirectoryObject(i Item) string {
	kind := objectKind(i)
	ident := firstNonEmpty(i.String("userPrincipalName"), i.String("mail"))
	if ident == "" {
		return kind
	}
	return kind + " · " + ident
}

// ObjectViewKind is the view that describes a directory object, or "" when
// entra-tui has none for it -- a directory role, say. It is what decides
// whether a row in a membership list can be opened in its own right.
//
// Graph annotates every object in a heterogeneous collection with
// @odata.type, which is the signal to trust; a projection can suppress it, so
// the object's own shape is the fallback. objectKind names the same decision
// for the reader, and goes through here so the two cannot disagree -- a row
// labelled User that enter refuses to open is worse than either answer.
func ObjectViewKind(i Item) Kind {
	if k := principalViewKind(strings.TrimPrefix(i.String("@odata.type"), "#microsoft.graph.")); k != "" {
		return k
	}
	switch {
	case i.String("userPrincipalName") != "":
		return KindUsers
	case has(i, "securityEnabled"), has(i, "groupTypes"):
		return KindGroups
	case has(i, "trustType"), has(i, "operatingSystem"):
		return KindDevices
	}
	return ""
}

// has reports whether an object carries a property at all, which separates a
// group with nothing set from an object of another type entirely.
func has(i Item, key string) bool {
	_, ok := i[key]
	return ok
}

// principalViewKind maps a Graph type name onto the view for it. Graph spells
// the same type two ways -- "#microsoft.graph.user" on a directory object and
// "User" on an app role assignment -- so both are accepted.
func principalViewKind(t string) Kind {
	switch strings.ToLower(t) {
	case "user":
		return KindUsers
	case "group":
		return KindGroups
	case "serviceprincipal":
		return KindEnterpriseApps
	case "application":
		return KindAppRegistrations
	case "device":
		return KindDevices
	}
	return ""
}

// objectKind turns the @odata.type annotation into a readable noun. Graph
// returns heterogeneous collections from memberOf and members, so the type is
// the only thing distinguishing a nested group from a user.
func objectKind(i Item) string {
	switch t := strings.TrimPrefix(i.String("@odata.type"), "#microsoft.graph."); t {
	case "user":
		return "User"
	case "group":
		return "Group"
	case "servicePrincipal":
		return "Service principal"
	case "device":
		return "Device"
	case "directoryRole":
		return "Directory role"
	case "orgContact":
		return "Contact"
	case "":
		// $select suppresses the annotation on some collections; fall back to
		// the shape of the object, through the same reading enter goes by.
		switch ObjectViewKind(i) {
		case KindUsers:
			return "User"
		case KindGroups:
			return "Group"
		case KindDevices:
			return "Device"
		}
		return "Object"
	default:
		return t
	}
}

// shortError renders an error for a section note, preferring the Graph code
// over a long message.
func shortError(err error) string {
	var api *APIError
	if errors.As(err, &api) {
		if api.Code != "" {
			return fmt.Sprintf("%s (%d)", api.Code, api.Status)
		}
		return fmt.Sprintf("HTTP %d", api.Status)
	}
	return err.Error()
}

func deviceSections(d Detail) []Section {
	o := d.Object
	used := newFieldSet()

	essentials := Section{Title: "Essentials", Fields: fields(o, used,
		f("Display name", "displayName"),
		f("Device ID", "deviceId"),
		f("Object ID", "id"),
		fn("Join type", TrustType, "trustType"),
		fn("Enabled", func(i Item) string { return YesNo(i, "accountEnabled") }, "accountEnabled"),
		f("Profile type", "profileType"),
	)}

	platform := Section{Title: "Platform", Fields: fields(o, used,
		f("Operating system", "operatingSystem"),
		f("Version", "operatingSystemVersion"),
		f("Manufacturer", "manufacturer"),
		f("Model", "model"),
	)}

	compliance := Section{Title: "Compliance & management", Fields: fields(o, used,
		fn("Compliant", func(i Item) string { return YesNo(i, "isCompliant") }, "isCompliant"),
		fn("Managed", func(i Item) string { return YesNo(i, "isManaged") }, "isManaged"),
		f("Enrollment type", "enrollmentType"),
		f("Management type", "managementType"),
		fn("Synced from on-premises", func(i Item) string {
			return YesNo(i, "onPremisesSyncEnabled")
		}, "onPremisesSyncEnabled"),
	)}

	activity := Section{Title: "Activity", Fields: fields(o, used,
		fn("Registered", func(i Item) string { return dateWithAge(i, "registrationDateTime") }, "registrationDateTime"),
		fn("Last sign-in", func(i Item) string {
			return dateWithAge(i, "approximateLastSignInDateTime")
		}, "approximateLastSignInDateTime"),
	)}

	owners := ownersSection(d)
	owners.Title = "Registered owners"
	// Graph keeps a device's owners under a different relationship name from
	// everything else, and the write path needs the right one.
	owners.Relationship = RelRegisteredOwners
	if len(owners.Fields) == 0 && d.OwnersErr == nil {
		owners.Note = "This device has no registered owner."
	}

	return compact(essentials, platform, compliance, activity, owners, groupsSection(d),
		Section{Title: "Other properties", Fields: allFields(o, used)})
}

func appRegistrationSections(d Detail) []Section {
	o := d.Object
	used := newFieldSet()

	essentials := Section{Title: "Essentials", Fields: fields(o, used,
		f("Display name", "displayName"),
		f("Application (client) ID", "appId"),
		f("Object ID", "id"),
		fn("Supported account types", SignInAudience, "signInAudience"),
		f("Publisher domain", "publisherDomain"),
		fl("Application ID URI", "identifierUris"),
		f("Description", "description"),
		fn("Created", func(i Item) string { return dateWithAge(i, "createdDateTime") }, "createdDateTime"),
		fl("Tags", "tags"),
	)}
	if d.Counterpart != nil {
		essentials.Fields = append(essentials.Fields, Field{
			Label: "Enterprise app", Value: d.Counterpart.DisplayName + "  (press x to open)",
		})
	}

	auth := Section{Title: "Authentication", Fields: authenticationFields(o, used)}

	creds := Section{Title: "Certificates & secrets", List: true,
		Columns: []string{"KIND", "NAME", "EXPIRES"}, Fields: credentialFields(o, used)}
	if len(creds.Fields) == 0 {
		creds.Note = "No client secrets or certificates are configured."
	}

	perms := Section{Title: "API permissions", List: true,
		Columns: []string{"API", "PERMISSION", "TYPE", "ADMIN CONSENT", "STATUS", "DESCRIPTION"},
		Fields:  apiPermissionFields(d, used)}
	if len(perms.Fields) == 0 {
		perms.Note = "No delegated or application permissions are requested."
	}

	roles := Section{Title: "App roles", List: true,
		Columns: []string{"ROLE", "VALUE", "MEMBER TYPES", "STATE"}, Fields: appRoleFields(o, used)}
	if len(roles.Fields) == 0 {
		roles.Note = "This application defines no app roles."
	}

	exposed := Section{Title: "Exposed API", List: true,
		Columns: []string{"SCOPE", "CONSENT", "DESCRIPTION"}, Fields: exposedScopeFields(o, used)}

	owners := ownersSection(d)

	return compact(essentials, auth, creds, perms, roles, exposed, owners,
		Section{Title: "Other properties", Fields: allFields(o, used)})
}

func enterpriseAppSections(d Detail) []Section {
	o := d.Object
	used := newFieldSet()

	essentials := Section{Title: "Essentials", Fields: fields(o, used,
		f("Display name", "displayName"),
		f("Application ID", "appId"),
		f("Object ID", "id"),
		f("Type", "servicePrincipalType"),
		f("Publisher", "publisherName"),
		f("Home page", "homepage"),
		f("Owner tenant", "appOwnerOrganizationId"),
		fn("Created", func(i Item) string { return dateWithAge(i, "createdDateTime") }, "createdDateTime"),
	)}
	if d.Counterpart != nil {
		essentials.Fields = append(essentials.Fields, Field{
			Label: "App registration", Value: d.Counterpart.DisplayName + "  (press x to open)",
		})
	}

	props := Section{Title: "Properties", Fields: fields(o, used,
		fn("Enabled for sign-in", func(i Item) string { return YesNo(i, "accountEnabled") }, "accountEnabled"),
		fn("Assignment required", func(i Item) string { return YesNo(i, "appRoleAssignmentRequired") }, "appRoleAssignmentRequired"),
		f("Single sign-on mode", "preferredSingleSignOnMode"),
		f("Login URL", "loginUrl"),
		f("Logout URL", "logoutUrl"),
		fl("Reply URLs", "replyUrls"),
		fl("Service principal names", "servicePrincipalNames"),
		fl("Notification emails", "notificationEmailAddresses"),
		fn("Supported account types", SignInAudience, "signInAudience"),
		fl("Tags", "tags"),
	)}

	assignments := assignmentsSection(d, o)
	roles := Section{Title: "App roles", List: true,
		Columns: []string{"ROLE", "VALUE", "MEMBER TYPES", "STATE"}, Fields: appRoleFields(o, used)}
	if len(roles.Fields) == 0 {
		roles.Note = "This application defines no app roles; assignments use the default access role."
	}
	exposed := Section{Title: "Exposed permissions", List: true,
		Columns: []string{"SCOPE", "CONSENT", "DESCRIPTION"}, Fields: exposedScopeFields(o, used)}
	owners := ownersSection(d)

	return compact(essentials, props, assignments, roles, exposed, owners,
		Section{Title: "Other properties", Fields: allFields(o, used)})
}

// ----------------------------------------------------------- section pieces

// authenticationFields gathers the redirect URIs, which Graph splits across
// three platform-specific objects, plus the implicit-grant toggles.
func authenticationFields(o Item, used fieldSet) []Field {
	used.add("web", "spa", "publicClient", "isFallbackPublicClient")

	var out []Field
	for _, platform := range []struct{ key, label string }{
		{"web", "Web redirect URIs"},
		{"spa", "SPA redirect URIs"},
		{"publicClient", "Desktop/mobile redirect URIs"},
	} {
		if uris := nestedStrings(o, platform.key, "redirectUris"); len(uris) > 0 {
			out = append(out, Field{Label: platform.label, Values: uris})
		}
	}
	if v := nestedString(o, "web", "homePageUrl"); v != "" {
		out = append(out, Field{Label: "Home page URL", Value: v})
	}
	if v := nestedString(o, "web", "logoutUrl"); v != "" {
		out = append(out, Field{Label: "Front-channel logout", Value: v})
	}

	// Implicit grant is a security-relevant setting, so report it explicitly
	// rather than letting an absent value read as "off".
	//
	// Only the access-token half is flagged. The implicit flow returns
	// tokens in the URL fragment, where they reach browser history and
	// referrers, cannot be bound with PKCE, and come without refresh tokens
	// -- so apps ask for long-lived ones. Microsoft's guidance is the
	// authorization code flow with PKCE instead. Implicit ID tokens are not
	// flagged: an ID token is not a credential for calling an API, and the
	// hybrid flow that issues one is still a supported pattern.
	if web, ok := o["web"].(map[string]any); ok {
		if ig, ok := web["implicitGrantSettings"].(map[string]any); ok {
			access := boolLabel(ig["enableAccessTokenIssuance"])
			if ig["enableAccessTokenIssuance"] == true {
				// The tint says something is wrong; this says what.
				access += " (legacy, prefer PKCE)"
			}
			out = append(out,
				Field{Label: "Implicit access tokens", Value: access,
					Warn: ig["enableAccessTokenIssuance"] == true},
				Field{Label: "Implicit ID tokens", Value: boolLabel(ig["enableIdTokenIssuance"])})
		}
	}
	if v, ok := o.Bool("isFallbackPublicClient"); ok {
		out = append(out, Field{Label: "Allow public client flows", Value: boolLabel(v)})
	}
	return out
}

// credentialFields lists secrets and certificates with their expiry.
func credentialFields(o Item, used fieldSet) []Field {
	used.add("passwordCredentials", "keyCredentials")

	var out []Field
	for _, group := range []struct{ key, label string }{
		{"passwordCredentials", "Secret"},
		{"keyCredentials", "Certificate"},
	} {
		entries, _ := o[group.key].([]any)
		for _, entry := range entries {
			cred, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			name, _ := cred["displayName"].(string)
			if name == "" {
				name = "(unnamed)"
			}
			end, _ := cred["endDateTime"].(string)
			expiry := "no expiry"
			warn := false
			if t, err := time.Parse(time.RFC3339, end); err == nil {
				expiry = t.Format("2006-01-02") + " (" + Until(t) + ")"
				warn = time.Until(t) < 30*24*time.Hour
			}
			out = append(out, Field{
				Label: group.label + " · " + name,
				Value: "expires " + expiry,
				Warn:  warn,
				Cells: []string{group.label, name, expiry},
			})
		}
	}
	return out
}

// apiPermissionFields flattens requiredResourceAccess into one row per
// permission, resolving GUIDs to names and reporting whether each has
// actually been consented to.
//
// The nested shape Graph returns -- APIs, each holding a list of permission
// ids -- reads terribly as an indented tree, and the thing an administrator
// wants to know is per permission: what it is, whether it needs an admin, and
// whether anyone has said yes.
func apiPermissionFields(d Detail, used fieldSet) []Field {
	used.add("requiredResourceAccess")

	entries, _ := d.Object["requiredResourceAccess"].([]any)
	var out []Field
	for _, entry := range entries {
		ra, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		resourceAppID, _ := ra["resourceAppId"].(string)
		api := d.ResourceNames[resourceAppID]
		if api == "" {
			api = resourceAppID
		}

		access, _ := ra["resourceAccess"].([]any)
		for _, a := range access {
			am, ok := a.(map[string]any)
			if !ok {
				continue
			}
			id, _ := am["id"].(string)
			kind, _ := am["type"].(string)

			info, known := d.Permissions[id]
			name := info.Value
			if name == "" {
				name = id
			}

			delegated := kind == "Scope"
			typeLabel := "Application"
			if delegated {
				typeLabel = "Delegated"
			}

			consent := "-"
			if known {
				consent = "no"
				if info.AdminConsent || !delegated {
					consent = "yes"
				}
			}

			out = append(out, Field{
				Label: name,
				Value: typeLabel + " · " + api,
				Warn:  d.GrantsErr == nil && permissionStatus(d, info, id, delegated) == "Not granted",
				Cells: []string{
					api, name, typeLabel, consent,
					permissionStatus(d, info, id, delegated),
					orDash(info.Description),
				},
			})
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Cells[0] != out[j].Cells[0] {
			return out[i].Cells[0] < out[j].Cells[0]
		}
		return out[i].Cells[1] < out[j].Cells[1]
	})
	return out
}

// permissionStatus reports whether a requested permission has been consented
// to. Delegated consent is recorded against the scope's value and application
// consent against the role's id, so the two are looked up differently.
func permissionStatus(d Detail, info PermissionInfo, id string, delegated bool) string {
	if d.GrantsErr != nil || (d.GrantedScopes == nil && d.GrantedRoles == nil) {
		return "-"
	}
	if delegated {
		if info.Value != "" && d.GrantedScopes[info.Value] {
			return "Granted"
		}
		return "Not granted"
	}
	if d.GrantedRoles[id] {
		return "Granted"
	}
	return "Not granted"
}

// appRoleFields lists the roles an application defines.
func appRoleFields(o Item, used fieldSet) []Field {
	used.add("appRoles")

	entries, _ := o["appRoles"].([]any)
	var out []Field
	for _, entry := range entries {
		role, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		name, _ := role["displayName"].(string)
		value, _ := role["value"].(string)
		enabled, _ := role["isEnabled"].(bool)
		var members []string
		if raw, ok := role["allowedMemberTypes"].([]any); ok {
			for _, m := range raw {
				if s, ok := m.(string); ok {
					members = append(members, s)
				}
			}
		}
		state := "enabled"
		if !enabled {
			state = "disabled"
		}
		out = append(out, Field{
			Label: orDash(name),
			Value: orDash(value) + " · " + state,
			Warn:  !enabled,
			Cells: []string{orDash(name), orDash(value), orDash(strings.Join(members, ", ")), state},
		})
	}
	return out
}

// exposedScopeFields lists the delegated scopes an application exposes.
func exposedScopeFields(o Item, used fieldSet) []Field {
	used.add("api", "oauth2PermissionScopes")

	scopes, _ := o["oauth2PermissionScopes"].([]any)
	if scopes == nil {
		if api, ok := o["api"].(map[string]any); ok {
			scopes, _ = api["oauth2PermissionScopes"].([]any)
		}
	}
	var out []Field
	for _, entry := range scopes {
		scope, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		value, _ := scope["value"].(string)
		consent, _ := scope["type"].(string)
		desc, _ := scope["adminConsentDisplayName"].(string)
		out = append(out, Field{
			Label: orDash(value),
			Value: strings.TrimSpace(consentLabel(consent) + " · " + desc),
			Cells: []string{orDash(value), consentLabel(consent), orDash(desc)},
		})
	}
	return out
}

// assignmentsSection renders the users and groups an enterprise app is
// assigned to, mapping each assignment back to the role it grants.
func assignmentsSection(d Detail, o Item) Section {
	s := Section{Title: "Users and groups", List: true,
		Columns: []string{"PRINCIPAL", "TYPE", "ROLE"}}
	if d.AssignmentsErr != nil {
		s.Note = "Could not read assignments: " + shortError(d.AssignmentsErr)
		return s
	}
	if len(d.Assignments) == 0 {
		s.Note = "No users or groups are assigned to this application."
		return s
	}

	roleNames := appRoleNames(o)
	for _, a := range d.Assignments {
		name := orDash(a.String("principalDisplayName"))
		kind := orDash(a.String("principalType"))
		role := roleNames[a.String("appRoleId")]
		if role == "" {
			role = "Default Access"
		}
		s.Fields = append(s.Fields, Field{
			Label: name,
			Value: kind + " · " + role,
			ID:    a.String("principalId"),
			Kind:  principalViewKind(a.String("principalType")),
			Cells: []string{name, kind, role},
		})
	}
	if d.AssignmentsTruncated {
		s.Note = fmt.Sprintf("Showing the first %d assignments.", len(d.Assignments))
	}
	return s
}

// appRoleNames maps appRole ids to their display names.
func appRoleNames(o Item) map[string]string {
	out := map[string]string{}
	entries, _ := o["appRoles"].([]any)
	for _, entry := range entries {
		role, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		id, _ := role["id"].(string)
		name, _ := role["displayName"].(string)
		if id != "" {
			out[id] = name
		}
	}
	return out
}

func ownersSection(d Detail) Section {
	s := Section{Title: "Owners", Relationship: RelOwners, List: true,
		Columns: []string{"NAME", "TYPE", "SIGN-IN NAME"}}
	if d.OwnersErr != nil {
		s.Note = "Could not read owners: " + shortError(d.OwnersErr)
		return s
	}
	if len(d.Owners) == 0 {
		s.Note = "This object has no owners, which means only administrators can manage it."
		return s
	}
	for _, o := range d.Owners {
		name := orDash(firstNonEmpty(o.String("displayName"), o.String("userPrincipalName"), o.ID()))
		s.Fields = append(s.Fields, Field{
			Label: name,
			Value: firstNonEmpty(o.String("userPrincipalName"), objectKind(o), ""),
			ID:    o.ID(),
			Kind:  ObjectViewKind(o),
			Cells: []string{name, objectKind(o),
				orDash(firstNonEmpty(o.String("userPrincipalName"), o.String("mail")))},
		})
	}
	return s
}

// ----------------------------------------------------------------- helpers

// fieldSpec describes one field to pull out of an object.
type fieldSpec struct {
	label string
	key   string
	fn    func(Item) string
	list  bool
	// consumes are extra raw properties a computed field covers.
	consumes []string
}

func f(label, key string) fieldSpec  { return fieldSpec{label: label, key: key} }
func fl(label, key string) fieldSpec { return fieldSpec{label: label, key: key, list: true} }

// fn builds a computed field. consumes names the raw properties the
// computation reads, so the catch-all "Other properties" section does not
// repeat them underneath in their unprocessed form.
func fn(label string, get func(Item) string, consumes ...string) fieldSpec {
	return fieldSpec{label: label, fn: get, consumes: consumes}
}

// fields extracts the given specs, marking each key as consumed so the
// catch-all "Other properties" section does not repeat it.
func fields(o Item, used fieldSet, specs ...fieldSpec) []Field {
	var out []Field
	for _, spec := range specs {
		if spec.key != "" {
			used.add(spec.key)
		}
		used.add(spec.consumes...)
		var fld Field
		switch {
		case spec.fn != nil:
			fld = Field{Label: spec.label, Value: spec.fn(o)}
		case spec.list:
			fld = Field{Label: spec.label, Values: o.Strings(spec.key)}
		default:
			fld = Field{Label: spec.label, Value: o.String(spec.key)}
		}
		if !fld.Empty() {
			out = append(out, fld)
		}
	}
	return out
}

// allFields renders every simple property not already shown.
//
// Structured values are left out. A nested object or a list of them can only
// be rendered here as a line of raw JSON, which is unreadable in a
// key-and-value pane and belongs to whichever section understands its shape.
// The raw JSON view still shows everything.
func allFields(o Item, used fieldSet) []Field {
	var out []Field
	for _, k := range o.Keys() {
		if strings.HasPrefix(k, "@odata") || used.has(k) || isStructured(o[k]) {
			continue
		}
		v := o.String(k)
		if strings.TrimSpace(v) == "" {
			continue
		}
		out = append(out, Field{Label: k, Value: v})
	}
	return out
}

// isStructured reports whether a value would render as JSON rather than as
// text: a nested object, or a list holding them.
func isStructured(v any) bool {
	switch t := v.(type) {
	case map[string]any:
		return true
	case []any:
		for _, e := range t {
			switch e.(type) {
			case map[string]any, []any:
				return true
			}
		}
	}
	return false
}

// compact drops sections that ended up with neither fields nor a note.
func compact(sections ...Section) []Section {
	var out []Section
	for _, s := range sections {
		if len(s.Fields) == 0 && s.Note == "" {
			continue
		}
		out = append(out, s)
	}
	return out
}

// fieldSet tracks which properties a section already consumed.
type fieldSet map[string]bool

func newFieldSet() fieldSet { return fieldSet{} }

func (s fieldSet) add(keys ...string) {
	for _, k := range keys {
		s[k] = true
	}
}

func (s fieldSet) has(k string) bool { return s[k] }

// nestedString reads a string from a nested object, e.g. web.homePageUrl.
func nestedString(o Item, parent, key string) string {
	m, ok := o[parent].(map[string]any)
	if !ok {
		return ""
	}
	s, _ := m[key].(string)
	return s
}

// nestedStrings reads a string slice from a nested object.
func nestedStrings(o Item, parent, key string) []string {
	m, ok := o[parent].(map[string]any)
	if !ok {
		return nil
	}
	raw, ok := m[key].([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func boolLabel(v any) string {
	b, ok := v.(bool)
	if !ok {
		return "-"
	}
	if b {
		return "yes"
	}
	return "no"
}

func consentLabel(t string) string {
	switch t {
	case "User":
		return "User consent"
	case "Admin":
		return "Admin consent"
	default:
		return t
	}
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

// dateWithAge renders a timestamp alongside how long ago it was.
func dateWithAge(i Item, key string) string {
	t, ok := i.Time(key)
	if !ok {
		return i.String(key)
	}
	return t.Format("2006-01-02") + " (" + Age(t) + " ago)"
}
