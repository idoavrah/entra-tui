package ui

import (
	"testing"

	"github.com/idoavrah/entra-tui/internal/graph"
)

func usersCollection(t *testing.T) *collection {
	t.Helper()
	res, ok := graph.Lookup("users")
	if !ok {
		t.Fatal("users resource missing")
	}
	return newCollection(res)
}

func page(items []graph.Item, next string, total int64) *graph.Page {
	return &graph.Page{Items: items, NextLink: next, TotalCount: total}
}

func user(name, upn, dept string) graph.Item {
	return graph.Item{
		"id": name, "displayName": name, "userPrincipalName": upn,
		"department": dept, "accountEnabled": true, "userType": "Member",
	}
}

func TestAppendPageAccumulatesAndTracksPaging(t *testing.T) {
	c := usersCollection(t)
	c.appendPage(page([]graph.Item{user("Ada", "ada@x.com", "R&D")}, "next-1", 7))

	if c.loaded() != 1 || c.len() != 1 {
		t.Fatalf("loaded=%d len=%d, want 1 and 1", c.loaded(), c.len())
	}
	if !c.hasMore() {
		t.Error("hasMore = false, want true while a nextLink is present")
	}
	if c.total != 7 {
		t.Errorf("total = %d, want 7", c.total)
	}

	c.appendPage(page([]graph.Item{user("Grace", "grace@x.com", "Ops")}, "", 7))
	if c.loaded() != 2 {
		t.Errorf("loaded = %d, want 2 after the second page", c.loaded())
	}
	if c.hasMore() {
		t.Error("hasMore = true after an empty nextLink")
	}
}

func TestAppendPagePreservesTotalWhenServerOmitsIt(t *testing.T) {
	c := usersCollection(t)
	c.appendPage(page([]graph.Item{user("Ada", "a@x", "R&D")}, "n", 7))
	// Following pages carry no @odata.count; the known total must survive.
	c.appendPage(page([]graph.Item{user("Bob", "b@x", "R&D")}, "", -1))

	if c.total != 7 {
		t.Errorf("total = %d, want the earlier count retained", c.total)
	}
}

func TestFilterMatchesAcrossAllColumns(t *testing.T) {
	c := usersCollection(t)
	c.appendPage(page([]graph.Item{
		user("Ada Lovelace", "ada@example.com", "Research"),
		user("Grace Hopper", "grace@example.com", "Operations"),
	}, "", 2))

	// Matching a department the user never has to locate by column.
	c.setFilter("research")
	if c.len() != 1 {
		t.Fatalf("len = %d, want 1 match for \"research\"", c.len())
	}
	item, _, _ := c.at(0)
	if item.String("displayName") != "Ada Lovelace" {
		t.Errorf("matched %q, want Ada Lovelace", item.String("displayName"))
	}
}

func TestFilterIsCaseInsensitiveAndAndsTerms(t *testing.T) {
	c := usersCollection(t)
	c.appendPage(page([]graph.Item{
		user("Ada Lovelace", "ada@example.com", "Research"),
		user("Ada Byron", "byron@example.com", "Operations"),
	}, "", 2))

	c.setFilter("ADA research")
	if c.len() != 1 {
		t.Errorf("len = %d, want both terms required", c.len())
	}
}

func TestFilterNegation(t *testing.T) {
	c := usersCollection(t)
	c.appendPage(page([]graph.Item{
		user("Ada Lovelace", "ada@example.com", "Research"),
		user("Ada Byron", "byron@example.com", "Operations"),
	}, "", 2))

	c.setFilter("ada !operations")
	if c.len() != 1 {
		t.Fatalf("len = %d, want the negated row excluded", c.len())
	}
	item, _, _ := c.at(0)
	if item.String("displayName") != "Ada Lovelace" {
		t.Errorf("matched %q, want Ada Lovelace", item.String("displayName"))
	}
}

func TestClearingFilterRestoresEveryRow(t *testing.T) {
	c := usersCollection(t)
	c.appendPage(page([]graph.Item{
		user("Ada", "a@x", "R"), user("Grace", "g@x", "O"),
	}, "", 2))

	c.setFilter("ada")
	c.setFilter("")
	if c.len() != 2 {
		t.Errorf("len = %d, want all rows back", c.len())
	}
}

func TestFilterSurvivesLaterPages(t *testing.T) {
	c := usersCollection(t)
	c.appendPage(page([]graph.Item{user("Ada", "ada@x", "Research")}, "n", -1))
	c.setFilter("research")

	// A page fetched while a filter is active must be filtered too.
	c.appendPage(page([]graph.Item{
		user("Grace", "grace@x", "Operations"),
		user("Alan", "alan@x", "Research"),
	}, "", -1))

	if c.len() != 2 {
		t.Errorf("len = %d, want the new matching row folded in", c.len())
	}
	if c.loaded() != 3 {
		t.Errorf("loaded = %d, want 3", c.loaded())
	}
}

func TestAtBoundsCheck(t *testing.T) {
	c := usersCollection(t)
	c.appendPage(page([]graph.Item{user("Ada", "a@x", "R")}, "", 1))

	if _, _, ok := c.at(-1); ok {
		t.Error("at(-1) returned ok")
	}
	if _, _, ok := c.at(5); ok {
		t.Error("at(5) returned ok")
	}
	if _, _, ok := c.at(0); !ok {
		t.Error("at(0) returned !ok for a present row")
	}
}

func TestParseFilterIgnoresLoneBang(t *testing.T) {
	// A bare "!" would otherwise become an empty negated term matching
	// everything, blanking the table mid-keystroke.
	terms := parseFilter("!")
	if len(terms) != 0 {
		t.Errorf("parseFilter(\"!\") = %v, want no terms", terms)
	}
}

func TestMatchesRowJoinsCells(t *testing.T) {
	row := []string{"Ada Lovelace", "ada@example.com", "Member"}
	if !matches(row, parseFilter("lovelace member")) {
		t.Error("expected a match across separate cells")
	}
	if matches(row, parseFilter("lovelace !member")) {
		t.Error("negation did not exclude the row")
	}
}
