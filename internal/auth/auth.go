// Package auth borrows the Microsoft Graph access token from an existing
// `az login` session.
//
// That is the only way in. There is no app-only or service-principal path by
// design, and no interactive flow of entra-tui's own: the Azure CLI already
// handles device codes, MFA, Conditional Access, WAM and every broker quirk
// on every platform, and doing it a second time badly helps nobody. A machine
// without a signed-in CLI is told to run `az login`, not offered a fallback.
package auth

import (
	"context"
	"strings"
	"time"
)

// GraphResource is the Microsoft Graph audience all tokens are minted for.
const GraphResource = "https://graph.microsoft.com"

// Method names where a token came from, for the header. There is only one
// real source; demo mode supplies its own.
type Method string

// MethodAzureCLI is the Azure CLI's Graph token.
const MethodAzureCLI Method = "azurecli"

// Identity describes who the tokens belong to, for display in the TUI header.
type Identity struct {
	Account  string // user principal name
	Name     string // display name, when the token carries one
	TenantID string
	Method   Method
}

// Label renders the identity for the status bar.
func (i Identity) Label() string {
	who := i.Account
	if who == "" {
		who = i.Name
	}
	if who == "" {
		who = "unknown"
	}
	return who
}

// Provider hands out a valid Graph access token, refreshing as needed.
// Implementations must be safe for concurrent use.
type Provider interface {
	// Token returns a bearer token valid for at least a few more minutes.
	Token(ctx context.Context) (string, error)
	// Identity describes the signed-in user.
	Identity() Identity
}

// Options configures token acquisition.
type Options struct {
	// TenantID picks which tenant's token to ask the CLI for, for an account
	// signed in to more than one. Empty means whichever the CLI has active.
	TenantID string
}

// SignInError is a sign-in failure together with the steps that fix it.
//
// Nearly every one of these is a machine that needs `az login` rather than a
// bug, so the remedy travels with the error instead of being left for the
// user to guess at.
type SignInError struct {
	// Reason is the one-line summary, safe to log.
	Reason string
	// Detail is whatever the CLI said, when it said anything useful.
	Detail string
	// Remedy is the commands that get the user signed in.
	Remedy string
}

func (e *SignInError) Error() string {
	msg := e.Reason
	if e.Detail != "" {
		msg += ": " + e.Detail
	}
	if e.Remedy != "" {
		msg += "\n\n" + e.Remedy
	}
	return msg
}

// installRemedy is for a machine with no Azure CLI at all.
const installRemedy = `entra-tui signs in by borrowing the Graph token from an ` + "`az login`" + ` session,
which is the only supported sign-in method.

  Install the Azure CLI   https://aka.ms/azure-cli
  Then sign in            az login`

// loginRemedy is for a CLI that is installed but has no usable session.
func loginRemedy(tenantID string) string {
	cmd := "az login"
	if tenantID != "" {
		cmd += " --tenant " + tenantID
	}
	return "Sign in and try again:\n\n  " + cmd
}

// Resolve signs in and returns a token provider.
//
// There is exactly one way for this to succeed: an Azure CLI on PATH with a
// live session that can mint a Graph token. Everything else comes back as a
// *SignInError carrying what to run.
func Resolve(ctx context.Context, opts Options) (Provider, error) {
	opts.TenantID = strings.TrimSpace(opts.TenantID)
	return newAzureCLI(ctx, opts)
}

// expirySkew is how long before true expiry a token is treated as stale, so a
// long-running Graph call never starts with a token about to die.
const expirySkew = 5 * time.Minute

func expired(t time.Time) bool {
	return t.IsZero() || time.Now().Add(expirySkew).After(t)
}
