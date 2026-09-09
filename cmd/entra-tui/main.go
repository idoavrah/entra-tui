// Command entra-tui is a read-only terminal browser for Microsoft Entra ID.
//
// It signs in as you -- through the browser, or by borrowing an existing
// Azure CLI session -- and lists users, groups, app registrations and
// enterprise applications through Microsoft Graph, scoped to your own
// delegated permissions.
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
	"github.com/idoavrah/entra-tui/internal/graph"
	"github.com/idoavrah/entra-tui/internal/ui"
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

	// Ctrl-C during the sign-in phase must abort cleanly, before Bubble Tea
	// installs its own signal handling.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Authentication happens before the TUI starts. The interactive flow opens
	// a browser and prints progress, which would be invisible (and would
	// corrupt the display) once the alternate screen buffer is in use.
	provider, err := auth.Resolve(ctx, auth.Options{
		ClientID: cfg.ClientID,
		TenantID: cfg.TenantID,
		Scopes:   cfg.Scopes,
		Method:   cfg.Method,
		Log: func(format string, a ...any) {
			fmt.Fprintf(os.Stderr, format+"\n", a...)
		},
	})
	if err != nil {
		return fmt.Errorf("sign-in failed: %w", err)
	}

	client := graph.New(provider, graph.WithBaseURL(cfg.GraphURL))
	model := ui.New(ctx, client, provider.Identity(), cfg.Resource, cfg.PageSize)

	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithContext(ctx))
	if _, err := program.Run(); err != nil {
		// A cancelled context is the user pressing Ctrl-C, not a failure.
		if errors.Is(err, tea.ErrProgramKilled) || errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}
	return nil
}
