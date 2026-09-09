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
	// PermissionNames maps a permission id to its value (e.g. "User.Read").
	// Without these, the API permissions section is a wall of GUIDs.
	ResourceNames   map[string]string
	PermissionNames map[string]string

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
	s := Section{Title: "Groups"}
	if d.GroupsErr != nil {
		s.Note = "Could not read group membership: " + shortError(d.GroupsErr)
		return s
	}
	if len(d.Groups) == 0 {
		s.Note = "Not a member of any group or directory role."
		return s
	}
	for _, g := range d.Groups {
		s.Fields = append(s.Fields, Field{
			Label: orDash(g.String("displayName")),
			Value: describeDirectoryObject(g),
		})
	}
	if d.GroupsTruncated {
		s.Note = fmt.Sprintf("Showing the first %d groups.", len(d.Groups))
	}
	return s
}

// membersSection lists a group's direct members.
func membersSection(d Detail) Section {
	s := Section{Title: "Members"}
	if d.MembersErr != nil {
		s.Note = "Could not read members: " + shortError(d.MembersErr)
		return s
	}
	if len(d.Members) == 0 {
		s.Note = "This group has no direct members."
		return s
	}
	for _, m := range d.Members {
		s.Fields = append(s.Fields, Field{
			Label: orDash(firstNonEmpty(m.String("displayName"), m.String("userPrincipalName"), m.ID())),
			Value: describeDirectoryObject(m),
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
		// the shape of the object.
		if i.String("userPrincipalName") != "" {
			return "User"
		}
		if _, ok := i["securityEnabled"]; ok {
			return "Group"
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

	creds := Section{Title: "Certificates & secrets", Fields: credentialFields(o, used)}
	if len(creds.Fields) == 0 {
		creds.Note = "No client secrets or certificates are configured."
	}

	perms := Section{Title: "API permissions", Fields: apiPermissionFields(d, used)}
	if len(perms.Fields) == 0 {
		perms.Note = "No delegated or application permissions are requested."
	}

	roles := Section{Title: "App roles", Fields: appRoleFields(o, used)}
	if len(roles.Fields) == 0 {
		roles.Note = "This application defines no app roles."
	}

	exposed := Section{Title: "Exposed API", Fields: exposedScopeFields(o, used)}

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
	roles := Section{Title: "App roles", Fields: appRoleFields(o, used)}
	if len(roles.Fields) == 0 {
		roles.Note = "This application defines no app roles; assignments use the default access role."
	}
	exposed := Section{Title: "Exposed permissions", Fields: exposedScopeFields(o, used)}
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
	if web, ok := o["web"].(map[string]any); ok {
		if ig, ok := web["implicitGrantSettings"].(map[string]any); ok {
			out = append(out,
				Field{Label: "Implicit access tokens", Value: boolLabel(ig["enableAccessTokenIssuance"]),
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
			})
		}
	}
	return out
}

// apiPermissionFields renders requiredResourceAccess, resolving the GUIDs to
// names where the lookup succeeded.
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
		resourceName := d.ResourceNames[resourceAppID]
		if resourceName == "" {
			resourceName = resourceAppID
		}

		access, _ := ra["resourceAccess"].([]any)
		var perms []string
		for _, a := range access {
			am, ok := a.(map[string]any)
			if !ok {
				continue
			}
			id, _ := am["id"].(string)
			kind, _ := am["type"].(string)
			label := d.PermissionNames[id]
			if label == "" {
				label = id
			}
			switch kind {
			case "Scope":
				kind = "Delegated"
			case "Role":
				kind = "Application"
			}
			perms = append(perms, fmt.Sprintf("%-13s %s", kind, label))
		}
		sort.Strings(perms)
		out = append(out, Field{Label: resourceName, Values: perms})
	}
	return out
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
		detail := fmt.Sprintf("%s · %s · %s", orDash(value), strings.Join(members, "/"), state)
		out = append(out, Field{Label: orDash(name), Value: detail, Warn: !enabled})
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
		})
	}
	return out
}

// assignmentsSection renders the users and groups an enterprise app is
// assigned to, mapping each assignment back to the role it grants.
func assignmentsSection(d Detail, o Item) Section {
	s := Section{Title: "Users and groups"}
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
		name := a.String("principalDisplayName")
		kind := a.String("principalType")
		role := roleNames[a.String("appRoleId")]
		if role == "" {
			role = "Default Access"
		}
		s.Fields = append(s.Fields, Field{
			Label: orDash(name),
			Value: fmt.Sprintf("%-6s · %s", orDash(kind), role),
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
	s := Section{Title: "Owners"}
	if d.OwnersErr != nil {
		s.Note = "Could not read owners: " + shortError(d.OwnersErr)
		return s
	}
	if len(d.Owners) == 0 {
		s.Note = "This object has no owners, which means only administrators can manage it."
		return s
	}
	for _, o := range d.Owners {
		name := firstNonEmpty(o.String("displayName"), o.String("userPrincipalName"), o.ID())
		s.Fields = append(s.Fields, Field{
			Label: name,
			Value: firstNonEmpty(o.String("userPrincipalName"), o.String("@odata.type"), ""),
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

// allFields renders every property not already shown, so nothing Graph
// returned is silently hidden.
func allFields(o Item, used fieldSet) []Field {
	var out []Field
	for _, k := range o.Keys() {
		if strings.HasPrefix(k, "@odata") || used.has(k) {
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
