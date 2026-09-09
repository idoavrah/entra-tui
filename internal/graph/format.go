package graph

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// Age renders a duration the way k9s does: the two most significant units,
// collapsing to a single unit once the value is large.
func Age(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	if d < 0 {
		return "0s"
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	case d < 365*24*time.Hour:
		days := int(d.Hours()) / 24
		return fmt.Sprintf("%dd", days)
	default:
		years := int(d.Hours()) / 24 / 365
		days := (int(d.Hours()) / 24) % 365
		return fmt.Sprintf("%dy%dd", years, days)
	}
}

// AgeOf renders the age of an ISO-8601 property.
func AgeOf(i Item, key string) string {
	t, ok := i.Time(key)
	if !ok {
		return ""
	}
	return Age(t)
}

// Until renders time remaining until t. Already-elapsed instants render as
// "expired" rather than a negative duration, because for credentials that
// distinction is the whole point.
func Until(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Until(t)
	if d <= 0 {
		return "expired"
	}
	days := int(math.Floor(d.Hours() / 24))
	switch {
	case days >= 365:
		return fmt.Sprintf("%dy", days/365)
	case days >= 1:
		return fmt.Sprintf("%dd", days)
	case d >= time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
}

// YesNo renders a tri-state boolean property, distinguishing "false" from
// "absent" so a missing $select does not read as a real value.
func YesNo(i Item, key string) string {
	v, ok := i.Bool(key)
	if !ok {
		return "-"
	}
	if v {
		return "yes"
	}
	return "no"
}

// Truncate shortens s to width, using a single-rune ellipsis so column
// alignment is preserved.
func Truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= width {
		return s
	}
	if width == 1 {
		return "…"
	}
	return string(runes[:width-1]) + "…"
}

// firstNonEmpty returns the first non-blank string.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
