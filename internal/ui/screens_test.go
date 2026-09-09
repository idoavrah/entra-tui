package ui

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/idoavrah/entra-tui/internal/demo"
	"github.com/idoavrah/entra-tui/internal/graph"
)

// update regenerates the committed screen captures instead of checking them.
var update = flag.Bool("update", false, "rewrite the golden screen captures")

// screensDir holds the captures. They are committed, so a change to the
// layout shows up as a readable diff in review rather than as nothing at all.
const screensDir = "../../docs/screens"

// screenSize is the terminal the captures are taken at: wide enough for two
// columns and the wordmark, tall enough for a full page of rows.
const (
	screenWidth  = 150
	screenHeight = 42
)

// demoSeed matches the seed the -demo flag uses, so the captures show what a
// reader will actually see.
const demoSeed = 7

func TestScreenCaptures(t *testing.T) {
	server := demo.NewServer(demoSeed)

	for _, tc := range []struct {
		name  string
		build func(*testing.T, Model) Model
	}{
		{"dashboard", func(t *testing.T, m Model) Model { return m }},
		{"users", func(t *testing.T, m Model) Model { return openView(t, server, m, "users") }},
		{"groups", func(t *testing.T, m Model) Model { return openView(t, server, m, "groups") }},
		{"app-registrations", func(t *testing.T, m Model) Model { return openView(t, server, m, "appregs") }},
		{"enterprise-apps", func(t *testing.T, m Model) Model { return openView(t, server, m, "entapps") }},
		{"devices", func(t *testing.T, m Model) Model { return openView(t, server, m, "devices") }},
		{"user-detail", func(t *testing.T, m Model) Model {
			return describe(t, server, openView(t, server, m, "users"))
		}},
		{"group-detail", func(t *testing.T, m Model) Model {
			return describe(t, server, openView(t, server, m, "groups"))
		}},
		{"app-registration-detail", func(t *testing.T, m Model) Model {
			return describe(t, server, openView(t, server, m, "appregs"))
		}},
		{"api-permissions", func(t *testing.T, m Model) Model {
			m = describe(t, server, openView(t, server, m, "appregs"))
			// Walk to the API permissions tab.
			for i, s := range m.listSections() {
				if s.Title == "API permissions" {
					m.detailTab = i
				}
			}
			return m.refreshDetail()
		}},
		{"search", func(t *testing.T, m Model) Model {
			m = openView(t, server, m, "users")
			m.coll.search = "storm"
			return reloadWith(t, server, m)
		}},
		{"help", func(t *testing.T, m Model) Model {
			m.screen = screenHelp
			return m
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.build(t, newDemoModel(t, server))
			compareGolden(t, tc.name, stabilise(m.View()))
		})
	}
}

// newDemoModel builds a signed-in model over the demo directory, with the
// totals already delivered.
func newDemoModel(t *testing.T, server *demo.Server) Model {
	t.Helper()
	res, _ := graph.Lookup("users")
	m := New(context.Background(), Options{
		GraphURL: "https://demo.invalid/v1.0",
		PageSize: 100,
		Resource: res,
		CacheDir: t.TempDir(),
		Client:   server.Client(),
		Identity: server.Identity(),
	})
	m = send(t, m, tea.WindowSizeMsg{Width: screenWidth, Height: screenHeight})

	for _, r := range graph.All() {
		total, err := server.Client().Count(context.Background(), r.Path)
		if err != nil {
			t.Fatalf("Count(%s): %v", r.Path, err)
		}
		m = send(t, m, countMsg{kind: r.Kind, total: total})
	}
	return m
}

// openView switches to a view and delivers its first page.
func openView(t *testing.T, server *demo.Server, m Model, alias string) Model {
	t.Helper()
	res, ok := graph.Lookup(alias)
	if !ok {
		t.Fatalf("no such view %q", alias)
	}
	next, _ := m.openResource(res)
	return reloadWith(t, server, next.(Model))
}

// reloadWith runs the model's current query and delivers the page.
func reloadWith(t *testing.T, server *demo.Server, m Model) Model {
	t.Helper()
	page, effective, err := listWithFallback(context.Background(), server.Client(), m.query())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	m.loading = false
	return send(t, m, pageMsg{gen: m.gen, page: page, advanced: effective.NeedsAdvancedQuery()})
}

// describe opens the first row's detail pane, fully loaded.
func describe(t *testing.T, server *demo.Server, m Model) Model {
	t.Helper()
	next, _ := m.openDetail()
	m = next.(Model)

	client := server.Client()
	ctx := context.Background()
	object, err := client.Get(ctx, m.coll.res.Path+"/"+m.detailID, nil)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	d := graph.Detail{Kind: m.coll.res.Kind, Object: object}
	d.Owners, _ = client.Owners(ctx, m.coll.res.Path, m.detailID)
	switch d.Kind {
	case graph.KindUsers:
		d.Groups, d.GroupsTruncated, d.GroupsErr = client.MemberOf(ctx, m.coll.res.Path, m.detailID)
	case graph.KindGroups:
		d.Members, d.MembersTruncated, d.MembersErr = client.Members(ctx, m.detailID)
	case graph.KindAppRegistrations:
		d.ResourceNames, d.Permissions = client.ResolvePermissions(ctx, object)
		if sp, err := client.ServicePrincipalByAppID(ctx, object.String("appId")); err == nil && sp != nil {
			d.Counterpart = &graph.Counterpart{
				Kind: graph.KindEnterpriseApps, ID: sp.ID(), DisplayName: sp.String("displayName"),
			}
			d.GrantedScopes, d.GrantedRoles, d.GrantsErr = client.GrantedPermissions(ctx, sp.ID())
		}
	}
	return send(t, m, detailMsg{gen: m.gen, detail: d})
}

// Patterns that move with the wall clock. The captures are about layout, so
// anything that would differ tomorrow is replaced rather than allowed to
// churn the diff every day.
var (
	// Word boundaries rather than whole tokens: an age cell can sit flush
	// against a table border, with no space to split on.
	agePattern  = regexp.MustCompile(`\b(\d+y\d+d|\d+d|\d+h\d+m|\d+m|\d+s)\b`)
	datePattern = regexp.MustCompile(`\d{4}-\d{2}-\d{2}`)
	expiredWord = regexp.MustCompile(`\bexpired\b`)
)

// stabilise blanks out anything time-dependent, preserving the width of what
// it replaces so the layout still reads true.
//
// The captures exist to make layout changes visible in review. A relative age
// would differ every day and churn the diff without ever saying anything.
func stabilise(view string) string {
	view = datePattern.ReplaceAllString(view, "0000-00-00")
	for _, p := range []*regexp.Regexp{agePattern, expiredWord} {
		view = p.ReplaceAllStringFunc(view, func(m string) string {
			return strings.Repeat("·", len([]rune(m)))
		})
	}
	return view
}

// compareGolden checks a capture against its committed copy, or rewrites it
// under -update.
func compareGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join(screensDir, name+".txt")

	if *update {
		if err := os.MkdirAll(screensDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no capture for %q: %v\nrun: go test ./internal/ui -update", name, err)
	}
	if strings.TrimRight(string(want), "\n") != strings.TrimRight(got, "\n") {
		t.Errorf("screen %q changed.\nRun `go test ./internal/ui -update` and review the diff.\n\n--- got ---\n%s", name, got)
	}
}
