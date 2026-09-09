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

func user(id, name string) graph.Item {
	return graph.Item{"id": id, "displayName": name, "userPrincipalName": id + "@x.com"}
}

func names(c *collection) []string {
	out := make([]string, 0, c.len())
	for i := 0; i < c.len(); i++ {
		item, _, _ := c.at(i)
		out = append(out, item.String("displayName"))
	}
	return out
}

func TestAppendPageTracksPagingState(t *testing.T) {
	c := usersCollection(t)
	c.appendPage(page([]graph.Item{user("a", "Ada")}, "next-1", 7))

	if c.len() != 1 {
		t.Fatalf("len = %d, want 1", c.len())
	}
	if !c.hasMore() {
		t.Error("hasMore = false while a nextLink is present")
	}
	if c.total != 7 {
		t.Errorf("total = %d, want 7", c.total)
	}

	c.appendPage(page([]graph.Item{user("g", "Grace")}, "", -1))
	if c.len() != 2 {
		t.Errorf("len = %d, want 2", c.len())
	}
	if c.hasMore() {
		t.Error("hasMore = true after an empty nextLink")
	}
	// A later page carries no @odata.count; the known total must survive.
	if c.total != 7 {
		t.Errorf("total = %d, want the earlier count retained", c.total)
	}
}

func TestCollectionSortsByNameCaseInsensitively(t *testing.T) {
	c := usersCollection(t)
	c.appendPage(page([]graph.Item{
		user("z", "Zoe"), user("a", "ada"), user("m", "Mike"),
	}, "", 3))

	want := []string{"ada", "Mike", "Zoe"}
	got := names(c)
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestLaterPagesMergeIntoSortOrder(t *testing.T) {
	c := usersCollection(t)
	c.appendPage(page([]graph.Item{user("b", "Bob"), user("d", "Dave")}, "n", -1))
	c.appendPage(page([]graph.Item{user("c", "Carol"), user("a", "Alice")}, "", -1))

	want := []string{"Alice", "Bob", "Carol", "Dave"}
	got := names(c)
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestSortKeyFallsBackWhenThereIsNoDisplayName(t *testing.T) {
	// Service principals and some directory objects can arrive without a
	// display name; they must still land somewhere deterministic.
	for _, tc := range []struct {
		name string
		item graph.Item
		want string
	}{
		{"upn", graph.Item{"id": "1", "userPrincipalName": "Zed@x.com"}, "zed@x.com"},
		{"mail", graph.Item{"id": "1", "mail": "Ann@x.com"}, "ann@x.com"},
		{"appId", graph.Item{"id": "1", "appId": "AAAA"}, "aaaa"},
		{"id only", graph.Item{"id": "ABC"}, "abc"},
	} {
		if got := sortKeyFor(tc.item); got != tc.want {
			t.Errorf("%s: sortKeyFor = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestSortIsStableForDuplicateNames(t *testing.T) {
	c := usersCollection(t)
	c.appendPage(page([]graph.Item{
		user("b", "Same"), user("a", "Same"), user("c", "Same"),
	}, "", 3))

	// Equal names fall back to the id, so ordering is total and does not
	// shuffle between reloads.
	var ids []string
	for i := 0; i < c.len(); i++ {
		item, _, _ := c.at(i)
		ids = append(ids, item.ID())
	}
	want := []string{"a", "b", "c"}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("ids = %v, want %v", ids, want)
		}
	}
}

func TestIndexOfFindsARowByIdentity(t *testing.T) {
	c := usersCollection(t)
	c.appendPage(page([]graph.Item{user("b", "Bob"), user("a", "Alice")}, "", 2))

	if got := c.indexOf("b"); got != 1 {
		t.Errorf("indexOf(b) = %d, want 1 after sorting", got)
	}
	if got := c.indexOf("missing"); got != -1 {
		t.Errorf("indexOf(missing) = %d, want -1", got)
	}
	if got := c.indexOf(""); got != -1 {
		t.Errorf("indexOf(\"\") = %d, want -1", got)
	}
}

func TestCellsStayWithTheirItemAcrossSorting(t *testing.T) {
	// Cells are rendered at load time; a re-sort must move them with their
	// object rather than leaving the two misaligned.
	c := usersCollection(t)
	c.appendPage(page([]graph.Item{user("z", "Zoe")}, "n", -1))
	c.appendPage(page([]graph.Item{user("a", "Ada")}, "", -1))

	for i := 0; i < c.len(); i++ {
		item, cells, _ := c.at(i)
		if cells[0] != item.String("displayName") {
			t.Errorf("row %d: cells %v do not match item %q", i, cells, item.String("displayName"))
		}
	}
}

func TestAtBoundsCheck(t *testing.T) {
	c := usersCollection(t)
	c.appendPage(page([]graph.Item{user("a", "Ada")}, "", 1))

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
