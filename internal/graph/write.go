package graph

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Relationship names an editable collection on a directory object.
type Relationship string

const (
	// RelMembers is a group's members.
	RelMembers Relationship = "members"
	// RelOwners is the owners of a group, application or service principal.
	RelOwners Relationship = "owners"
	// RelRegisteredOwners is a device's owners, which Graph keeps under a
	// different name from everything else.
	RelRegisteredOwners Relationship = "registeredOwners"
)

// Label renders a relationship for a confirmation prompt.
func (r Relationship) Label() string {
	switch r {
	case RelMembers:
		return "member"
	case RelRegisteredOwners:
		return "registered owner"
	default:
		return "owner"
	}
}

// AddRef adds a directory object to a relationship.
//
// Graph edits these collections through a $ref sub-resource whose body is a
// single @odata.id pointing at the object being added -- there is no way to
// pass an id directly.
func (c *Client) AddRef(ctx context.Context, path, id string, rel Relationship, principalID string) error {
	if err := validRefArgs(id, principalID); err != nil {
		return err
	}
	body, err := json.Marshal(map[string]string{
		"@odata.id": c.baseURL + "/directoryObjects/" + principalID,
	})
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	u := fmt.Sprintf("%s/%s/%s/%s/$ref", c.baseURL, strings.Trim(path, "/"), id, rel)
	return c.write(ctx, http.MethodPost, u, body)
}

// RemoveRef removes a directory object from a relationship.
func (c *Client) RemoveRef(ctx context.Context, path, id string, rel Relationship, principalID string) error {
	if err := validRefArgs(id, principalID); err != nil {
		return err
	}
	u := fmt.Sprintf("%s/%s/%s/%s/%s/$ref", c.baseURL, strings.Trim(path, "/"), id, rel, principalID)
	return c.write(ctx, http.MethodDelete, u, nil)
}

// validRefArgs rejects ids that would produce a malformed or, worse, a
// differently-targeted URL. Directory ids are opaque GUIDs; anything with a
// slash or a query character in it did not come from Graph.
func validRefArgs(ids ...string) error {
	for _, id := range ids {
		if strings.TrimSpace(id) == "" {
			return fmt.Errorf("missing object id")
		}
		if strings.ContainsAny(id, "/?#&%") {
			return fmt.Errorf("refusing to use %q as an object id", id)
		}
	}
	return nil
}

// write performs a mutating request. Unlike reads it is not retried on a
// server error: a POST that may or may not have been applied should be
// reported, not repeated.
func (c *Client) write(ctx context.Context, method, u string, body []byte) error {
	token, err := c.tokens.Token(ctx)
	if err != nil {
		return fmt.Errorf("acquire token: %w", err)
	}

	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, u, reader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	payload := make([]byte, 8<<10)
	n, _ := resp.Body.Read(payload)
	return parseAPIError(resp.StatusCode, payload[:n])
}

// FindPrincipals searches for the objects that can be added to a
// relationship, by display name or sign-in name.
//
// Owners must be users; members may also be groups, so a group is searched
// for only when the relationship allows one. The caller decides what to do
// with an ambiguous result -- this returns everything it found.
func (c *Client) FindPrincipals(ctx context.Context, rel Relationship, term string) ([]Item, error) {
	term = strings.TrimSpace(term)
	if term == "" {
		return nil, fmt.Errorf("nothing to search for")
	}

	found, err := c.searchPrincipals(ctx, "/users",
		[]string{"displayName", "userPrincipalName", "mail"},
		[]string{"id", "displayName", "userPrincipalName", "accountEnabled"}, term)
	if err != nil {
		return nil, err
	}
	if rel != RelMembers {
		return found, nil
	}

	groups, err := c.searchPrincipals(ctx, "/groups",
		[]string{"displayName", "mail"},
		[]string{"id", "displayName", "mail"}, term)
	if err != nil {
		// A user match is still useful when the group lookup is refused.
		return found, nil
	}
	return append(found, groups...), nil
}

// searchPrincipals runs one bounded search. The cap is deliberately small:
// the add flow only proceeds on a single match, so a long list is only ever
// reported as "too many".
func (c *Client) searchPrincipals(ctx context.Context, path string, searchFields, sel []string, term string) ([]Item, error) {
	page, err := c.List(ctx, Query{
		Path:         path,
		Select:       sel,
		SearchFields: searchFields,
		SearchTerm:   term,
		Top:          5,
	})
	if err != nil {
		return nil, err
	}
	return page.Items, nil
}
