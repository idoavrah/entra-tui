// Package config resolves entra-tui's settings from command-line flags and
// environment variables, with flags taking precedence.
package config

import (
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/idoavrah/entra-tui/internal/auth"
	"github.com/idoavrah/entra-tui/internal/graph"
)

// Config is the fully resolved runtime configuration.
type Config struct {
	ClientID string
	TenantID string
	Scopes   []string
	Method   auth.Method
	PageSize int
	GraphURL string
	Resource graph.Resource
	// Demo runs against a generated directory instead of a real tenant.
	Demo bool
	// NoDelay opens an object's pane on the row's own columns and fills the
	// rest in afterwards, instead of waiting for the full read.
	NoDelay bool
	// Version asks for the build stamp and nothing else.
	Version bool
}

// Env var names, all prefixed so they cannot collide with the Azure CLI's own.
const (
	EnvClientID = "ENTRA_TUI_CLIENT_ID"
	EnvTenantID = "ENTRA_TUI_TENANT_ID"
	EnvAuth     = "ENTRA_TUI_AUTH"
	EnvScopes   = "ENTRA_TUI_SCOPES"
	EnvPageSize = "ENTRA_TUI_PAGE_SIZE"
	EnvGraphURL = "ENTRA_TUI_GRAPH_URL"
)

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
		clientID = fs.String("client-id", "", "OAuth client id of the app registration to sign in with")
		tenantID = fs.String("tenant", "", "tenant id or domain to sign in against (default \"organizations\")")
		method   = fs.String("auth", "", "sign-in method: auto, browser or azurecli (default \"auto\")")
		scopes   = fs.String("scopes", "", "comma-separated delegated Graph scopes to request")
		pageSize = fs.Int("page-size", 0, "objects requested per Graph page (1-999)")
		graphURL = fs.String("graph-url", "", "Graph endpoint, for sovereign clouds")
		resource = fs.String("view", "users", "view to open on: users, groups, appregs, entapps or devices")
		demo     = fs.Bool("demo", false, "run against a generated directory, with no tenant and no sign-in")
		nodelay  = fs.Bool("nodelay", false, "open an object's pane before it has loaded, filling the rest in afterwards")
		version  = fs.Bool("version", false, "print the build version and exit")
	)

	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}

	cfg := Config{
		ClientID: firstNonEmpty(*clientID, getenv(EnvClientID), auth.DefaultClientID),
		TenantID: firstNonEmpty(*tenantID, getenv(EnvTenantID), auth.DefaultTenant),
		GraphURL: firstNonEmpty(*graphURL, getenv(EnvGraphURL), graph.DefaultBaseURL),
	}

	parsedMethod, err := auth.ParseMethod(firstNonEmpty(*method, getenv(EnvAuth)))
	if err != nil {
		return Config{}, err
	}
	cfg.Method = parsedMethod

	cfg.Scopes = parseScopes(firstNonEmpty(*scopes, getenv(EnvScopes)))
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = auth.DefaultScopes()
	}

	cfg.PageSize, err = parsePageSize(*pageSize, getenv(EnvPageSize))
	if err != nil {
		return Config{}, err
	}

	cfg.Demo = *demo
	cfg.NoDelay = *nodelay
	cfg.Version = *version

	res, ok := graph.Lookup(*resource)
	if !ok {
		return Config{}, fmt.Errorf("unknown view %q (want users, groups, apps or sp)", *resource)
	}
	cfg.Resource = res

	return cfg, nil
}

// parseScopes splits a comma-separated scope list, qualifying bare permission
// names with the Graph resource URI so that "User.Read.All" works as well as
// the fully qualified form.
func parseScopes(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	for _, raw := range strings.Split(s, ",") {
		scope := strings.TrimSpace(raw)
		if scope == "" {
			continue
		}
		if !strings.Contains(scope, "/") {
			scope = auth.GraphResource + "/" + scope
		}
		out = append(out, scope)
	}
	return out
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
  ENTRA_TUI_CLIENT_ID   same as -client-id
  ENTRA_TUI_TENANT_ID   same as -tenant
  ENTRA_TUI_AUTH        same as -auth
  ENTRA_TUI_SCOPES      same as -scopes
  ENTRA_TUI_PAGE_SIZE   same as -page-size
  ENTRA_TUI_GRAPH_URL   same as -graph-url

Use -demo to explore the interface against a generated directory, with no
tenant, no sign-in and no network.

entra-tui works with your own delegated permissions. It reads the directory
and can add and remove members and owners; it creates, deletes and renames
nothing.
`
