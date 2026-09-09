package config

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/idoavrah/entra-tui/internal/auth"
	"github.com/idoavrah/entra-tui/internal/graph"
)

// env builds a getenv function from a map.
func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestDefaults(t *testing.T) {
	cfg, err := Load(nil, env(nil), io.Discard)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ClientID != auth.DefaultClientID {
		t.Errorf("ClientID = %q, want the well-known Graph CLI client", cfg.ClientID)
	}
	if cfg.TenantID != auth.DefaultTenant {
		t.Errorf("TenantID = %q, want %q", cfg.TenantID, auth.DefaultTenant)
	}
	if cfg.Method != auth.MethodAuto {
		t.Errorf("Method = %q, want auto", cfg.Method)
	}
	if cfg.PageSize != graph.DefaultPageSize {
		t.Errorf("PageSize = %d, want %d", cfg.PageSize, graph.DefaultPageSize)
	}
	if cfg.Resource.Kind != graph.KindUsers {
		t.Errorf("Resource = %s, want users", cfg.Resource.Kind)
	}
	if len(cfg.Scopes) != len(auth.DefaultScopes()) {
		t.Errorf("Scopes = %v, want the defaults", cfg.Scopes)
	}
}

func TestFlagsBeatEnvironment(t *testing.T) {
	vars := map[string]string{
		EnvClientID: "from-env",
		EnvTenantID: "env-tenant",
		EnvAuth:     "azurecli",
		EnvPageSize: "50",
	}
	cfg, err := Load([]string{
		"-client-id", "from-flag",
		"-tenant", "flag-tenant",
		"-auth", "browser",
		"-page-size", "25",
	}, env(vars), io.Discard)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.ClientID != "from-flag" {
		t.Errorf("ClientID = %q, want the flag to win", cfg.ClientID)
	}
	if cfg.TenantID != "flag-tenant" {
		t.Errorf("TenantID = %q, want the flag to win", cfg.TenantID)
	}
	if cfg.Method != auth.MethodBrowser {
		t.Errorf("Method = %q, want the flag to win", cfg.Method)
	}
	if cfg.PageSize != 25 {
		t.Errorf("PageSize = %d, want the flag to win", cfg.PageSize)
	}
}

func TestEnvironmentUsedWhenNoFlag(t *testing.T) {
	cfg, err := Load(nil, env(map[string]string{
		EnvClientID: "env-client",
		EnvAuth:     "azurecli",
		EnvPageSize: "999",
	}), io.Discard)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ClientID != "env-client" {
		t.Errorf("ClientID = %q, want env-client", cfg.ClientID)
	}
	if cfg.Method != auth.MethodAzureCLI {
		t.Errorf("Method = %q, want azurecli", cfg.Method)
	}
	if cfg.PageSize != 999 {
		t.Errorf("PageSize = %d, want 999", cfg.PageSize)
	}
}

func TestScopesAreQualifiedWithTheGraphResource(t *testing.T) {
	cfg, err := Load([]string{"-scopes", "User.Read.All, Group.Read.All"}, env(nil), io.Discard)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []string{
		auth.GraphResource + "/User.Read.All",
		auth.GraphResource + "/Group.Read.All",
	}
	if len(cfg.Scopes) != len(want) {
		t.Fatalf("Scopes = %v, want %v", cfg.Scopes, want)
	}
	for i := range want {
		if cfg.Scopes[i] != want[i] {
			t.Errorf("Scopes[%d] = %q, want %q", i, cfg.Scopes[i], want[i])
		}
	}
}

func TestFullyQualifiedScopesArePassedThrough(t *testing.T) {
	custom := "api://contoso/Directory.Read"
	cfg, err := Load([]string{"-scopes", custom}, env(nil), io.Discard)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Scopes) != 1 || cfg.Scopes[0] != custom {
		t.Errorf("Scopes = %v, want [%s] untouched", cfg.Scopes, custom)
	}
}

func TestPageSizeValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		vars map[string]string
	}{
		{"too large", []string{"-page-size", "1000"}, nil},
		{"negative", []string{"-page-size", "-1"}, nil},
		{"non-numeric env", nil, map[string]string{EnvPageSize: "many"}},
	} {
		if _, err := Load(tc.args, env(tc.vars), io.Discard); err == nil {
			t.Errorf("%s: Load returned nil error, want a validation failure", tc.name)
		}
	}
}

func TestUnknownAuthMethodIsRejected(t *testing.T) {
	_, err := Load([]string{"-auth", "certificate"}, env(nil), io.Discard)
	if err == nil {
		t.Fatal("Load accepted an unknown auth method")
	}
	if !strings.Contains(err.Error(), "certificate") {
		t.Errorf("error = %v, want it to name the bad value", err)
	}
}

func TestUnknownViewIsRejected(t *testing.T) {
	if _, err := Load([]string{"-view", "devices"}, env(nil), io.Discard); err == nil {
		t.Fatal("Load accepted an unknown view")
	}
}

func TestViewAliasesResolve(t *testing.T) {
	for alias, want := range map[string]graph.Kind{
		"groups": graph.KindGroups,
		"apps":   graph.KindAppRegistrations,
		"sp":     graph.KindEnterpriseApps,
	} {
		cfg, err := Load([]string{"-view", alias}, env(nil), io.Discard)
		if err != nil {
			t.Fatalf("Load(-view %s): %v", alias, err)
		}
		if cfg.Resource.Kind != want {
			t.Errorf("-view %s = %s, want %s", alias, cfg.Resource.Kind, want)
		}
	}
}

func TestHelpIsNotAnError(t *testing.T) {
	_, err := Load([]string{"-h"}, env(nil), io.Discard)
	if !errors.Is(err, ErrHelp) {
		t.Errorf("Load(-h) = %v, want ErrHelp so the caller exits zero", err)
	}
}
