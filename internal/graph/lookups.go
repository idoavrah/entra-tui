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

// maxAssignments bounds how many app role assignments the detail view reads.
// A widely assigned enterprise app can have tens of thousands; the detail
// pane is not the place to enumerate them.
const maxAssignments = 200

// Owners lists the owners of a directory object.
func (c *Client) Owners(ctx context.Context, path, id string) ([]Item, error) {
	page, err := c.List(ctx, Query{
		Path:   fmt.Sprintf("%s/%s/owners", strings.Trim(path, "/"), id),
		Select: []string{"id", "displayName", "userPrincipalName"},
		Top:    50,
	})
	if err != nil {
		return nil, err
	}
	return page.Items, nil
}

// AppRoleAssignedTo lists the users and groups assigned to an enterprise
// application, following pages up to maxAssignments.
func (c *Client) AppRoleAssignedTo(ctx context.Context, spID string) ([]Item, bool, error) {
	page, err := c.List(ctx, Query{
		Path: fmt.Sprintf("servicePrincipals/%s/appRoleAssignedTo", spID),
		Select: []string{
			"id", "principalId", "principalDisplayName", "principalType", "appRoleId", "createdDateTime",
		},
		Top: 100,
	})
	if err != nil {
		return nil, false, err
	}

	items := page.Items
	for page.NextLink != "" && len(items) < maxAssignments {
		page, err = c.Next(ctx, page.NextLink, false)
		if err != nil {
			// Partial results still beat none in a read-only browser.
			return items, true, nil
		}
		items = append(items, page.Items...)
	}
	return items, page.NextLink != "", nil
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
func (c *Client) ResolvePermissions(ctx context.Context, o Item) (resources map[string]string, permissions map[string]string) {
	resources = map[string]string{}
	permissions = map[string]string{}

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
		collectPermissionNames(sp, "oauth2PermissionScopes", permissions)
		collectPermissionNames(sp, "appRoles", permissions)
	}
	return resources, permissions
}

// collectPermissionNames maps permission ids to their values from a service
// principal's published catalogue.
func collectPermissionNames(sp Item, key string, into map[string]string) {
	entries, _ := sp[key].([]any)
	for _, entry := range entries {
		m, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		id, _ := m["id"].(string)
		value, _ := m["value"].(string)
		if id != "" && value != "" {
			into[id] = value
		}
	}
}

// escapeODataString escapes a value for use inside an OData string literal,
// where a single quote is doubled. Directory ids never contain one, but the
// filter is built by string concatenation and must not be breakable.
func escapeODataString(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}
