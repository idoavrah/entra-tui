package config

import (
	"errors"
	"io"
	"testing"

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
	// An empty tenant means "whichever one the Azure CLI is signed in to",
	// which is the right default for the overwhelming majority of accounts.
	if cfg.TenantID != "" {
		t.Errorf("TenantID = %q, want it left to the Azure CLI", cfg.TenantID)
	}
	if cfg.PageSize != graph.DefaultPageSize {
		t.Errorf("PageSize = %d, want %d", cfg.PageSize, graph.DefaultPageSize)
	}
	if cfg.Resource.Kind != graph.KindUsers {
		t.Errorf("Resource = %s, want users", cfg.Resource.Kind)
	}
}

func TestFlagsBeatEnvironment(t *testing.T) {
	vars := map[string]string{
		EnvTenantID: "env-tenant",
		EnvGraphURL: "https://env.example/v1.0",
		EnvPageSize: "50",
	}
	cfg, err := Load([]string{
		"-tenant", "flag-tenant",
		"-graph-url", "https://flag.example/v1.0",
		"-page-size", "25",
	}, env(vars), io.Discard)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.TenantID != "flag-tenant" {
		t.Errorf("TenantID = %q, want the flag to win", cfg.TenantID)
	}
	if cfg.GraphURL != "https://flag.example/v1.0" {
		t.Errorf("GraphURL = %q, want the flag to win", cfg.GraphURL)
	}
	if cfg.PageSize != 25 {
		t.Errorf("PageSize = %d, want the flag to win", cfg.PageSize)
	}
}

func TestEnvironmentUsedWhenNoFlag(t *testing.T) {
	cfg, err := Load(nil, env(map[string]string{
		EnvTenantID: "env-tenant",
		EnvPageSize: "999",
	}), io.Discard)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.TenantID != "env-tenant" {
		t.Errorf("TenantID = %q, want env-tenant", cfg.TenantID)
	}
	if cfg.PageSize != 999 {
		t.Errorf("PageSize = %d, want 999", cfg.PageSize)
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

func TestTheOldSignInFlagsAreGone(t *testing.T) {
	// The Azure CLI mints the token from its own first-party client, so a
	// client id, a scope list and a choice of method have nothing left to
	// configure. Accepting them silently would be worse than refusing them.
	for _, flag := range []string{"-auth", "-client-id", "-scopes"} {
		if _, err := Load([]string{flag, "whatever"}, env(nil), io.Discard); err == nil {
			t.Errorf("Load still accepts %s", flag)
		}
	}
}

func TestUnknownViewIsRejected(t *testing.T) {
	if _, err := Load([]string{"-view", "printers"}, env(nil), io.Discard); err == nil {
		t.Fatal("Load accepted an unknown view")
	}
}

func TestViewAliasesResolve(t *testing.T) {
	for alias, want := range map[string]graph.Kind{
		"groups":  graph.KindGroups,
		"apps":    graph.KindAppRegistrations,
		"sp":      graph.KindEnterpriseApps,
		"devices": graph.KindDevices,
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
