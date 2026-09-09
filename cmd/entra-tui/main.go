// Command entra-tui is a read-only terminal browser for Microsoft Entra ID.
//
// It signs you in -- through the browser, or by borrowing an existing Azure
// CLI session -- and lists users, groups, app registrations and enterprise
// applications through Microsoft Graph, scoped to your own delegated
// permissions.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/idoavrah/entra-tui/internal/auth"
	"github.com/idoavrah/entra-tui/internal/config"
	"github.com/idoavrah/entra-tui/internal/demo"
	"github.com/idoavrah/entra-tui/internal/ui"
)

// demoSeed fixes the generated directory, so a screenshot taken today
// matches one taken tomorrow.
const demoSeed = 7

// Build information, stamped by the release build. The defaults are what a
// `go build` from a working tree reports.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, config.ErrHelp) {
			return
		}
		fmt.Fprintf(os.Stderr, "entra-tui: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, err := config.Load(args, os.Getenv, os.Stderr)
	if err != nil {
		return err
	}
	if cfg.Version {
		fmt.Printf("entra-tui %s (%s, built %s)\n", version, commit, date)
		return nil
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	opts := ui.Options{
		Auth: auth.Options{
			ClientID: cfg.ClientID,
			TenantID: cfg.TenantID,
			Scopes:   cfg.Scopes,
			Method:   cfg.Method,
		},
		GraphURL: cfg.GraphURL,
		PageSize: cfg.PageSize,
		Resource: cfg.Resource,
		NoDelay:  cfg.NoDelay,
	}

	// Demo mode swaps the tenant for a generated directory served in
	// process, so there is nothing to sign in to and nothing to reach.
	if cfg.Demo {
		server := demo.NewServer(demoSeed)
		opts.Client = server.Client()
		opts.Identity = server.Identity()
	}

	program := tea.NewProgram(ui.New(ctx, opts), tea.WithAltScreen(), tea.WithContext(ctx))
	if _, err := program.Run(); err != nil {
		// A cancelled context is the user pressing Ctrl-C, not a failure.
		if errors.Is(err, tea.ErrProgramKilled) || errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}
	return nil
}
