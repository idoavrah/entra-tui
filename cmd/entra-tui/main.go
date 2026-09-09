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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Sign-in happens inside the TUI, on its own screen: the user picks a
	// method and nothing is queried until they choose a view.
	model := ui.New(ctx, ui.Options{
		Auth: auth.Options{
			ClientID: cfg.ClientID,
			TenantID: cfg.TenantID,
			Scopes:   cfg.Scopes,
			Method:   cfg.Method,
		},
		GraphURL: cfg.GraphURL,
		PageSize: cfg.PageSize,
		Resource: cfg.Resource,
	})

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
