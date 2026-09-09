package graph

import (
	"context"
	"fmt"
	"strings"
)

// GraphAppID is Microsoft Graph's own application id. It appears in almost
// every app registration's requiredResourceAccess, so naming it without a
// lookup saves the most common round trip.
const GraphAppID = "00000003-0000-0000-c000-000000000000"

// Bounds on how much of an unbounded relationship the detail view reads. A
// widely assigned enterprise app, or a group holding every employee, can have
// tens of thousands of entries; the detail pane is not the place to
// enumerate them, and the section says when it stopped short.
const (
	maxAssignments = 200
	maxMembers     = 200
	maxMemberOf    = 200
)

// collect follows pages of a relationship up to limit, returning the items
// and whether more remain.
//
// A page that fails midway returns what was gathered rather than nothing: a
// partial member list still answers most questions a reader has.
func (c *Client) collect(ctx context.Context, q Query, limit int) ([]Item, bool, error) {
	page, err := c.List(ctx, q)
	if err != nil {
		return nil, false, err
	}
	items := page.Items
	for page.NextLink != "" && len(items) < limit {
		page, err = c.Next(ctx, page.NextLink, false)
		if err != nil {
			return items, true, nil
		}
		items = append(items, page.Items...)
	}
	return items, page.NextLink != "", nil
}

// RegisteredOwners lists the users a device is registered to.
//
// Devices do not use the "owners" relationship that applications and groups
// share; theirs is registeredOwners, which is why it needs its own call.
func (c *Client) RegisteredOwners(ctx context.Context, deviceID string) ([]Item, error) {
	page, err := c.List(ctx, Query{
		Path: fmt.Sprintf("devices/%s/registeredOwners", deviceID),
		Top:  50,
	})
	if err != nil {
		return nil, err
	}
	return page.Items, nil
}

// MemberOf lists the groups and directory roles an object belongs to.
//
// This is the direct membership Graph reports for the object, not the
// transitive closure: showing the groups someone was actually added to is
// what an administrator is looking for, and transitiveMemberOf on a
// well-nested tenant returns an unhelpfully long list.
//
// No $select is sent. memberOf returns a heterogeneous collection -- groups
// alongside directory roles -- typed as directoryObject, and naming a
// group-only property like groupTypes makes Graph reject the whole request.
// The default projection carries displayName and the @odata.type annotation
// that tells the two apart, which is all this needs.
func (c *Client) MemberOf(ctx context.Context, path, id string) ([]Item, bool, error) {
	return c.collect(ctx, Query{
		Path: fmt.Sprintf("%s/%s/memberOf", strings.Trim(path, "/"), id),
		Top:  100,
	}, maxMemberOf)
}

// Members lists the direct members of a group.
//
// As with MemberOf, no $select is sent: members can be users, groups, service
// principals or devices, and a user-only property in the projection fails the
// request for the whole collection.
func (c *Client) Members(ctx context.Context, groupID string) ([]Item, bool, error) {
	return c.collect(ctx, Query{
		Path: fmt.Sprintf("groups/%s/members", groupID),
		Top:  100,
	}, maxMembers)
}

// Owners lists the owners of a directory object. Owners are also a
// heterogeneous collection, so it too goes without a projection.
func (c *Client) Owners(ctx context.Context, path, id string) ([]Item, error) {
	page, err := c.List(ctx, Query{
		Path: fmt.Sprintf("%s/%s/owners", strings.Trim(path, "/"), id),
		Top:  50,
	})
	if err != nil {
		return nil, err
	}
	return page.Items, nil
}

// AppRoleAssignedTo lists the users and groups assigned to an enterprise
// application, following pages up to maxAssignments.
func (c *Client) AppRoleAssignedTo(ctx context.Context, spID string) ([]Item, bool, error) {
	return c.collect(ctx, Query{
		Path: fmt.Sprintf("servicePrincipals/%s/appRoleAssignedTo", spID),
		Select: []string{
			"id", "principalId", "principalDisplayName", "principalType", "appRoleId", "createdDateTime",
		},
		Top: 100,
	}, maxAssignments)
}

// byAppID fetches the single directory object in a collection whose appId
// matches. Graph has no direct-by-appId addressing for these collections, so
// this is a filtered list that returns at most one object.
func (c *Client) byAppID(ctx context.Context, path, appID string, sel []string) (Item, error) {
	if appID == "" {
		return nil, fmt.Errorf("no application id")
	}
	page, err := c.List(ctx, Query{
		Path:   path,
		Select: sel,
		Filter: fmt.Sprintf("appId eq '%s'", escapeODataString(appID)),
		Top:    1,
	})
	if err != nil {
		return nil, err
	}
	if len(page.Items) == 0 {
		return nil, nil
	}
	return page.Items[0], nil
}

// ServicePrincipalByAppID finds the enterprise application paired with an app
// registration.
func (c *Client) ServicePrincipalByAppID(ctx context.Context, appID string) (Item, error) {
	return c.byAppID(ctx, "/servicePrincipals", appID, []string{"id", "appId", "displayName"})
}

// ApplicationByAppID finds the app registration behind an enterprise
// application. Many enterprise apps -- every Microsoft-published one -- have
// no application object in the tenant, so a nil result is normal.
func (c *Client) ApplicationByAppID(ctx context.Context, appID string) (Item, error) {
	return c.byAppID(ctx, "/applications", appID, []string{"id", "appId", "displayName"})
}

// ResolvePermissions turns the GUIDs in an app registration's
// requiredResourceAccess into readable names.
//
// Each referenced API is itself a service principal that publishes the
// permission catalogue, so one lookup per distinct API resolves every
// permission it grants. Failures are swallowed: an unresolvable API leaves
// its GUIDs on screen, which is worse than names but better than an error
// where the detail view should be.
func (c *Client) ResolvePermissions(ctx context.Context, o Item) (resources map[string]string, permissions map[string]PermissionInfo) {
	resources = map[string]string{}
	permissions = map[string]PermissionInfo{}

	entries, _ := o["requiredResourceAccess"].([]any)
	seen := map[string]bool{}
	for _, entry := range entries {
		ra, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		appID, _ := ra["resourceAppId"].(string)
		if appID == "" || seen[appID] {
			continue
		}
		seen[appID] = true

		sp, err := c.byAppID(ctx, "/servicePrincipals", appID,
			[]string{"id", "appId", "displayName", "appRoles", "oauth2PermissionScopes"})
		if err != nil || sp == nil {
			if appID == GraphAppID {
				resources[appID] = "Microsoft Graph"
			}
			continue
		}

		resources[appID] = sp.String("displayName")
		collectPermissions(sp, "oauth2PermissionScopes", permissions)
		collectPermissions(sp, "appRoles", permissions)
	}
	return resources, permissions
}

// collectPermissions maps permission ids to what they are, from a service
// principal's published catalogue.
//
// Application permissions (appRoles) always require an administrator;
// delegated ones (oauth2PermissionScopes) say so in their type.
func collectPermissions(sp Item, key string, into map[string]PermissionInfo) {
	entries, _ := sp[key].([]any)
	for _, entry := range entries {
		m, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		id, _ := m["id"].(string)
		if id == "" {
			continue
		}
		value, _ := m["value"].(string)
		description, _ := m["adminConsentDisplayName"].(string)
		if description == "" {
			description, _ = m["displayName"].(string)
		}

		adminConsent := key == "appRoles"
		if consent, ok := m["type"].(string); ok && consent == "Admin" {
			adminConsent = true
		}
		into[id] = PermissionInfo{Value: value, Description: description, AdminConsent: adminConsent}
	}
}

// GrantedPermissions reports which permissions have actually been consented
// to for an application's service principal.
//
// Delegated consent lives in oauth2PermissionGrants as space-separated scope
// values; application consent lives in appRoleAssignments as role ids. The
// two are keyed differently, so they are returned separately rather than
// merged into a lie.
func (c *Client) GrantedPermissions(ctx context.Context, spID string) (scopes, roles map[string]bool, err error) {
	scopes, roles = map[string]bool{}, map[string]bool{}

	grants, _, err := c.collect(ctx, Query{
		Path: fmt.Sprintf("servicePrincipals/%s/oauth2PermissionGrants", spID),
		Top:  100,
	}, 200)
	if err != nil {
		return nil, nil, err
	}
	for _, g := range grants {
		for _, scope := range strings.Fields(g.String("scope")) {
			scopes[scope] = true
		}
	}

	assignments, _, err := c.collect(ctx, Query{
		Path: fmt.Sprintf("servicePrincipals/%s/appRoleAssignments", spID),
		Top:  100,
	}, 200)
	if err != nil {
		// Delegated consent is still worth reporting on its own.
		return scopes, roles, nil
	}
	for _, a := range assignments {
		if id := a.String("appRoleId"); id != "" {
			roles[id] = true
		}
	}
	return scopes, roles, nil
}

// escapeODataString escapes a value for use inside an OData string literal,
// where a single quote is doubled. Directory ids never contain one, but the
// filter is built by string concatenation and must not be breakable.
func escapeODataString(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}
