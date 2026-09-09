// Package graph is a small read-only Microsoft Graph client built around
// OData paging. It deliberately avoids the generated Graph SDK: entra-tui
// reads a handful of directory collections, and keeping objects as loosely
// typed maps lets the detail view show every property Graph returns without
// a struct having to know about it in advance.
package graph

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/idoavrah/entra-tui/internal/text"
)

// Item is a single directory object as returned by Graph.
type Item map[string]any

// ID returns the object's directory id.
func (i Item) ID() string { return i.String("id") }

// String renders the named property as display text. Nested objects and
// arrays are flattened rather than omitted, so the detail view never shows a
// blank line where data exists.
func (i Item) String(key string) string {
	if i == nil {
		return ""
	}
	return renderValue(i[key])
}

// Bool reports a boolean property and whether it was present and boolean.
func (i Item) Bool(key string) (bool, bool) {
	if i == nil {
		return false, false
	}
	b, ok := i[key].(bool)
	return b, ok
}

// Strings returns a string-slice property, ignoring non-string elements.
func (i Item) Strings(key string) []string {
	if i == nil {
		return nil
	}
	raw, ok := i[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, text.Sanitize(s))
		}
	}
	return out
}

// Time parses an ISO-8601 property, reporting whether it was present.
func (i Item) Time(key string) (time.Time, bool) {
	s, ok := i[key].(string)
	if !ok || s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// Keys lists the object's properties in a stable display order: OData
// annotations (@odata.*) sink to the bottom, everything else is alphabetical
// with "id" and "displayName" pulled to the top.
func (i Item) Keys() []string {
	keys := make([]string, 0, len(i))
	for k := range i {
		keys = append(keys, k)
	}
	rank := func(k string) int {
		switch {
		case strings.HasPrefix(k, "@"):
			return 3
		case k == "id":
			return 0
		case k == "displayName":
			return 1
		default:
			return 2
		}
	}
	sort.Slice(keys, func(a, b int) bool {
		ra, rb := rank(keys[a]), rank(keys[b])
		if ra != rb {
			return ra < rb
		}
		return keys[a] < keys[b]
	})
	return keys
}

// JSON renders the object as indented JSON for the raw detail view.
//
// This is the one screen that shows the object exactly as Graph returned it,
// so nothing is dropped. encoding/json escapes the control characters
// already; the directionality overrides it passes through get the same
// treatment, so the document says what is really there without the terminal
// acting on it.
func (i Item) JSON() string {
	b, err := json.MarshalIndent(i, "", "  ")
	if err != nil {
		return fmt.Sprintf("<unrenderable object: %v>", err)
	}
	return text.Escape(string(b))
}

// renderValue flattens an arbitrary decoded JSON value to one display line.
func renderValue(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return text.Sanitize(t)
	case bool:
		return strconv.FormatBool(t)
	case float64:
		// encoding/json decodes every number as float64; render integral
		// values without a misleading ".0" suffix.
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case []any:
		parts := make([]string, 0, len(t))
		for _, e := range t {
			parts = append(parts, renderValue(e))
		}
		return strings.Join(parts, ", ")
	case map[string]any:
		b, err := json.Marshal(t)
		if err != nil {
			return "{...}"
		}
		return string(b)
	default:
		return text.Sanitize(fmt.Sprint(t))
	}
}
