package graph

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestItemStringFlattensNestedValues(t *testing.T) {
	var item Item
	if err := json.Unmarshal([]byte(`{
		"id":"1","count":42,"ratio":1.5,"enabled":true,
		"tags":["a","b"],"web":{"homePageUrl":"https://x"},"missing":null
	}`), &item); err != nil {
		t.Fatal(err)
	}

	for key, want := range map[string]string{
		"id":      "1",
		"count":   "42", // JSON numbers decode as float64; no ".0" suffix
		"ratio":   "1.5",
		"enabled": "true",
		"tags":    "a, b",
		"missing": "",
		"absent":  "",
	} {
		if got := item.String(key); got != want {
			t.Errorf("String(%q) = %q, want %q", key, got, want)
		}
	}
	if got := item.String("web"); !strings.Contains(got, "homePageUrl") {
		t.Errorf("String(web) = %q, want the nested object rendered", got)
	}
}

func TestItemBoolDistinguishesFalseFromAbsent(t *testing.T) {
	item := Item{"on": true, "off": false, "text": "no"}

	if v, ok := item.Bool("off"); !ok || v {
		t.Errorf("Bool(off) = (%v,%v), want (false,true)", v, ok)
	}
	if _, ok := item.Bool("absent"); ok {
		t.Error("Bool(absent) reported present")
	}
	if _, ok := item.Bool("text"); ok {
		t.Error("Bool(text) reported a string as boolean")
	}
}

func TestYesNoRendersTriState(t *testing.T) {
	item := Item{"a": true, "b": false}
	if got := YesNo(item, "a"); got != "yes" {
		t.Errorf("YesNo(true) = %q", got)
	}
	if got := YesNo(item, "b"); got != "no" {
		t.Errorf("YesNo(false) = %q", got)
	}
	// An unselected property must not read as a real "no".
	if got := YesNo(item, "c"); got != "-" {
		t.Errorf("YesNo(absent) = %q, want -", got)
	}
}

func TestItemStringsIgnoresNonStrings(t *testing.T) {
	item := Item{"mixed": []any{"a", 1, "b", nil}}
	got := item.Strings("mixed")
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("Strings(mixed) = %v, want [a b]", got)
	}
	if got := item.Strings("absent"); got != nil {
		t.Errorf("Strings(absent) = %v, want nil", got)
	}
}

func TestItemKeysOrdersForDisplay(t *testing.T) {
	item := Item{"zebra": 1, "displayName": "n", "id": "x", "@odata.type": "t", "alpha": 2}
	keys := item.Keys()

	if keys[0] != "id" || keys[1] != "displayName" {
		t.Errorf("keys start with %v, want id then displayName", keys[:2])
	}
	if keys[len(keys)-1] != "@odata.type" {
		t.Errorf("last key = %q, want the OData annotation sunk to the bottom", keys[len(keys)-1])
	}
	if keys[2] != "alpha" || keys[3] != "zebra" {
		t.Errorf("middle keys = %v, want alphabetical", keys[2:4])
	}
}

func TestTruncatePreservesWidth(t *testing.T) {
	for _, tc := range []struct {
		in    string
		width int
		want  string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello", 4, "hel…"},
		{"hello", 1, "…"},
		{"hello", 0, ""},
		{"héllo", 3, "hé…"},
	} {
		got := Truncate(tc.in, tc.width)
		if got != tc.want {
			t.Errorf("Truncate(%q,%d) = %q, want %q", tc.in, tc.width, got, tc.want)
		}
		if n := len([]rune(got)); n > tc.width {
			t.Errorf("Truncate(%q,%d) is %d runes, over the limit", tc.in, tc.width, n)
		}
	}
}

func TestAgeUnits(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name string
		at   time.Time
		want string
	}{
		{"seconds", now.Add(-30 * time.Second), "30s"},
		{"minutes", now.Add(-5 * time.Minute), "5m"},
		{"days", now.Add(-3 * 24 * time.Hour), "3d"},
		{"zero", time.Time{}, ""},
		{"future", now.Add(time.Hour), "0s"},
	} {
		if got := Age(tc.at); got != tc.want {
			t.Errorf("%s: Age = %q, want %q", tc.name, got, tc.want)
		}
	}
	if got := Age(now.Add(-800 * 24 * time.Hour)); !strings.HasPrefix(got, "2y") {
		t.Errorf("Age(2 years) = %q, want a 2y prefix", got)
	}
}

func TestUntilFlagsExpiry(t *testing.T) {
	if got := Until(time.Now().Add(-time.Minute)); got != "expired" {
		t.Errorf("Until(past) = %q, want expired", got)
	}
	if got := Until(time.Time{}); got != "" {
		t.Errorf("Until(zero) = %q, want empty", got)
	}
	if got := Until(time.Now().Add(10 * 24 * time.Hour)); got != "9d" && got != "10d" {
		t.Errorf("Until(10 days) = %q, want ~10d", got)
	}
}

func TestItemJSONRoundTrips(t *testing.T) {
	item := Item{"id": "1", "displayName": "Ada"}
	out := item.JSON()
	var back map[string]any
	if err := json.Unmarshal([]byte(out), &back); err != nil {
		t.Fatalf("JSON() produced invalid JSON: %v", err)
	}
	if back["displayName"] != "Ada" {
		t.Errorf("round trip lost displayName: %v", back)
	}
}

func TestNilItemIsSafe(t *testing.T) {
	var item Item
	if got := item.String("anything"); got != "" {
		t.Errorf("nil Item String = %q, want empty", got)
	}
	if _, ok := item.Bool("anything"); ok {
		t.Error("nil Item Bool reported present")
	}
	if got := item.Strings("anything"); got != nil {
		t.Errorf("nil Item Strings = %v, want nil", got)
	}
}
