// Package config resolves entra-tui's settings from command-line flags and
// environment variables, with flags taking precedence.
package config

import (
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/idoavrah/entra-tui/internal/graph"
)

// Config is the fully resolved runtime configuration.
type Config struct {
	// TenantID picks which tenant to ask the Azure CLI for a token for, for
	// an account signed in to more than one. Empty means the CLI's active one.
	TenantID string
	PageSize int
	GraphURL string
	Resource graph.Resource
	// Demo runs against a generated directory instead of a real tenant.
	Demo bool
	// NoDelay opens an object's pane on the row's own columns and fills the
	// rest in afterwards, instead of waiting for the full read.
	NoDelay bool
	// DisableUsageTracking turns off anonymous usage tracking and the update
	// check. Tracking is on by default; this is the opt-out.
	DisableUsageTracking bool
	// Version asks for the build stamp and nothing else.
	Version bool
}

// Env var names, all prefixed so they cannot collide with the Azure CLI's own.
const (
	EnvTenantID = "ENTRA_TUI_TENANT_ID"
	EnvPageSize = "ENTRA_TUI_PAGE_SIZE"
	EnvGraphURL = "ENTRA_TUI_GRAPH_URL"
	// EnvDisableUsageTracking opts out of usage tracking, the same as -d.
	EnvDisableUsageTracking = "ENTRA_TUI_DISABLE_USAGE_TRACKING"
)

// isTruthy reads an opt-out environment variable. Anything set to something
// other than an explicit "no" counts, because somebody who sets a variable
// named DISABLE_USAGE_TRACKING at all has said what they mean.
func isTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "0", "false", "no", "off":
		return false
	}
	return true
}

// ErrHelp reports that usage was requested, so the caller exits zero.
var ErrHelp = flag.ErrHelp

// Load parses args (excluding the program name) and the environment.
//
// getenv is injected rather than read from the process so the precedence
// rules can be tested without mutating global state.
func Load(args []string, getenv func(string) string, out io.Writer) (Config, error) {
	fs := flag.NewFlagSet("entra-tui", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.Usage = func() {
		fmt.Fprint(out, usage)
		fs.PrintDefaults()
		fmt.Fprint(out, envHelp)
	}

	var (
		tenantID = fs.String("tenant", "", "tenant id or domain to get the token for (default: the Azure CLI's active tenant)")
		pageSize = fs.Int("page-size", 0, "objects requested per Graph page (1-999)")
		graphURL = fs.String("graph-url", "", "Graph endpoint, for sovereign clouds")
		resource = fs.String("view", "users", "view to open on: users, groups, appregs, entapps or devices")
		demo     = fs.Bool("demo", false, "run against a generated directory, with no tenant and no sign-in")
		nodelay  = fs.Bool("nodelay", false, "open an object's pane before it has loaded, filling the rest in afterwards")
		notrack  = fs.Bool("disable-usage-tracking", false, "turn off anonymous usage tracking and the update check (default enabled)")
		notrackD = fs.Bool("d", false, "shorthand for -disable-usage-tracking")
		version  = fs.Bool("version", false, "print the build version and exit")
	)

	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}

	cfg := Config{
		TenantID: firstNonEmpty(*tenantID, getenv(EnvTenantID)),
		GraphURL: firstNonEmpty(*graphURL, getenv(EnvGraphURL), graph.DefaultBaseURL),
	}

	var err error
	cfg.PageSize, err = parsePageSize(*pageSize, getenv(EnvPageSize))
	if err != nil {
		return Config{}, err
	}

	cfg.Demo = *demo
	cfg.NoDelay = *nodelay
	// Demo mode promises no network at all, and that promise is worth more
	// than a statistic, so it opts out on its own behalf.
	cfg.DisableUsageTracking = *notrack || *notrackD ||
		isTruthy(getenv(EnvDisableUsageTracking)) || cfg.Demo
	cfg.Version = *version

	res, ok := graph.Lookup(*resource)
	if !ok {
		return Config{}, fmt.Errorf("unknown view %q (want users, groups, apps or sp)", *resource)
	}
	cfg.Resource = res

	return cfg, nil
}

// parsePageSize validates the requested page size against Graph's ceiling.
func parsePageSize(flagVal int, envVal string) (int, error) {
	size := flagVal
	if size == 0 && strings.TrimSpace(envVal) != "" {
		parsed, err := strconv.Atoi(strings.TrimSpace(envVal))
		if err != nil {
			return 0, fmt.Errorf("invalid %s: %v", EnvPageSize, err)
		}
		size = parsed
	}
	if size == 0 {
		return graph.DefaultPageSize, nil
	}
	// Graph caps $top at 999 for directory collections and rejects anything
	// larger outright, so catch it here rather than on the first request.
	if size < 1 || size > 999 {
		return 0, fmt.Errorf("page size %d out of range (want 1-999)", size)
	}
	return size, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

const usage = `entra-tui — a read-only terminal browser for Microsoft Entra ID.

Usage:
  entra-tui [flags]

Flags:
`

const envHelp = `
Environment:
  ENTRA_TUI_TENANT_ID   same as -tenant
  ENTRA_TUI_PAGE_SIZE   same as -page-size
  ENTRA_TUI_GRAPH_URL   same as -graph-url
  ENTRA_TUI_DISABLE_USAGE_TRACKING  same as -d

Sign-in is the Azure CLI and nothing else: run "az login" first, and
entra-tui borrows that session's Microsoft Graph token.

Use -demo to explore the interface against a generated directory, with no
tenant, no sign-in and no network.

entra-tui works with your own delegated permissions. It reads the directory
and can add and remove members and owners; it creates, deletes and renames
nothing.
`
