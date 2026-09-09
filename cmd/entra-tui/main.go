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
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/idoavrah/entra-tui/internal/auth"
	"github.com/idoavrah/entra-tui/internal/config"
	"github.com/idoavrah/entra-tui/internal/demo"
	"github.com/idoavrah/entra-tui/internal/graph"
	"github.com/idoavrah/entra-tui/internal/telemetry"
	"github.com/idoavrah/entra-tui/internal/ui"
)

// demoSeed fixes the generated directory, so a screenshot taken today
// matches one taken tomorrow.
const demoSeed = 7

// signInTimeout bounds sign-in. The browser flow waits on a human, so it is
// generous; the Azure CLI path returns in under a second either way.
const signInTimeout = 5 * time.Minute

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

// signIn resolves a delegated token, bounded so a browser flow that nobody
// completes cannot hang forever.
func signIn(ctx context.Context, cfg config.Config) (auth.Provider, error) {
	ctx, cancel := context.WithTimeout(ctx, signInTimeout)
	defer cancel()

	opts := auth.Options{
		ClientID: cfg.ClientID,
		TenantID: cfg.TenantID,
		Scopes:   cfg.Scopes,
		Method:   cfg.Method,
	}
	provider, err := auth.Resolve(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("sign-in failed: %w\n\nRun `az login` first, or use -auth browser on a machine with a browser", err)
	}
	return provider, nil
}

func run(args []string) error {
	cfg, err := config.Load(args, os.Getenv, os.Stderr)
	if err != nil {
		return err
	}
	track := telemetry.New(!cfg.DisableUsageTracking, version)
	defer track.Shutdown(2 * time.Second)
	track.StartUpdateCheck()

	if cfg.Version {
		fmt.Printf("entra-tui %s (%s, built %s)\n", version, commit, date)
		return nil
	}
	track.Capture("started application", nil)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	opts := ui.Options{
		Auth: auth.Options{
			ClientID: cfg.ClientID,
			TenantID: cfg.TenantID,
			Scopes:   cfg.Scopes,
			Method:   cfg.Method,
		},
		GraphURL:  cfg.GraphURL,
		PageSize:  cfg.PageSize,
		Resource:  cfg.Resource,
		NoDelay:   cfg.NoDelay,
		Version:   version,
		Telemetry: track,
	}

	// Demo mode swaps the tenant for a generated directory served in
	// process, so there is nothing to sign in to and nothing to reach.
	if cfg.Demo {
		server := demo.NewServer(demoSeed)
		opts.Client = server.Client()
		opts.Identity = server.Identity()
	} else {
		// Sign in before the interface opens, and fail here if it does not
		// work. A dashboard that has drawn itself, reported "connected" and
		// then admits in a corner that it never signed in is worse than no
		// dashboard: it looks like the tool is working.
		//
		// This is also why the browser flow belongs out here. Its handoff
		// prints and waits, and the alternate screen buffer is no place for
		// either.
		provider, err := signIn(ctx, cfg)
		if err != nil {
			track.CaptureError("sign-in failed", err.Error())
			return err
		}
		opts.Client = graph.New(provider, graph.WithBaseURL(cfg.GraphURL))
		opts.Identity = provider.Identity()
	}

	program := tea.NewProgram(ui.New(ctx, opts), tea.WithAltScreen(), tea.WithContext(ctx))
	if _, err := program.Run(); err != nil {
		// A cancelled context is the user pressing Ctrl-C, not a failure.
		if errors.Is(err, tea.ErrProgramKilled) || errors.Is(err, context.Canceled) {
			track.Capture("exited successfully", nil)
			return nil
		}
		track.CaptureError("exited unsuccessfully", err.Error())
		return err
	}
	track.Capture("exited successfully", nil)
	return nil
}
