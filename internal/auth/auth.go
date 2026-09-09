// Package auth resolves a delegated Microsoft Graph access token for the
// signed-in user. Every request entra-tui makes runs under that user's own
// permissions -- there is no app-only/service-principal path by design.
package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// GraphResource is the Microsoft Graph audience all tokens are minted for.
const GraphResource = "https://graph.microsoft.com"

// DefaultClientID is the first-party "Microsoft Graph Command Line Tools"
// public client. It already carries the loopback redirect URIs an interactive
// PKCE flow needs, so most tenants work with no setup at all. Tenants that
// block it can point entra-tui at their own app registration with
// ENTRA_TUI_CLIENT_ID (see README).
const DefaultClientID = "14d82eec-204b-4c2f-b7e8-296a70dab67e"

// DefaultTenant targets the "organizations" endpoint, which accepts any work
// or school account but rejects personal Microsoft accounts (which have no
// directory to browse).
const DefaultTenant = "organizations"

// DefaultScopes are the least-privilege delegated scopes that cover the
// read-only views.
//
// Application.Read.All covers both app registrations (/applications) and
// enterprise apps (/servicePrincipals). GroupMember.Read.All is what lets the
// detail panes show a user's groups and a group's members; without it those
// sections report that they could not be read and everything else still
// works. None of this needs the far broader Directory.Read.All.
func DefaultScopes() []string {
	return []string{
		GraphResource + "/User.Read.All",
		GraphResource + "/Group.Read.All",
		GraphResource + "/GroupMember.Read.All",
		GraphResource + "/Application.Read.All",
	}
}

// Method identifies how a token was obtained.
type Method string

const (
	// MethodAuto prefers an existing Azure CLI session and falls back to the
	// browser. Because entra-tui keeps no on-disk token cache, reusing an
	// `az login` session is what saves you a browser popup on every launch.
	MethodAuto Method = "auto"
	// MethodBrowser always runs interactive auth code + PKCE.
	MethodBrowser Method = "browser"
	// MethodAzureCLI always borrows the Azure CLI's Graph token.
	MethodAzureCLI Method = "azurecli"
)

// ParseMethod validates a user-supplied auth method.
func ParseMethod(s string) (Method, error) {
	switch Method(strings.ToLower(strings.TrimSpace(s))) {
	case "", MethodAuto:
		return MethodAuto, nil
	case MethodBrowser:
		return MethodBrowser, nil
	case MethodAzureCLI, "az", "cli":
		return MethodAzureCLI, nil
	default:
		return "", fmt.Errorf("unknown auth method %q (want auto, browser or azurecli)", s)
	}
}

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
	ClientID string
	TenantID string
	Scopes   []string
	Method   Method
	// Log receives human-readable progress ("opening browser...").
	Log func(format string, args ...any)
	// OpenURL is called with the sign-in URL before the browser is launched.
	// The TUI uses it to display the URL, so a user whose browser did not
	// open has something to copy. Returning an error aborts the flow.
	//
	// When nil, the default system browser launcher is used.
	OpenURL func(url string) error
}

func (o *Options) applyDefaults() {
	if o.ClientID == "" {
		o.ClientID = DefaultClientID
	}
	if o.TenantID == "" {
		o.TenantID = DefaultTenant
	}
	if len(o.Scopes) == 0 {
		o.Scopes = DefaultScopes()
	}
	if o.Method == "" {
		o.Method = MethodAuto
	}
	if o.Log == nil {
		o.Log = func(string, ...any) {}
	}
}

// ErrNoAzureCLI reports that the Azure CLI path is unavailable, which in
// MethodAuto is an ordinary condition rather than a failure.
var ErrNoAzureCLI = errors.New("azure cli credential unavailable")

// Resolve obtains a token provider according to opts.Method, performing the
// first token acquisition eagerly so that failures surface before the TUI
// starts and the browser handoff is not fighting the alternate screen buffer.
func Resolve(ctx context.Context, opts Options) (Provider, error) {
	opts.applyDefaults()

	switch opts.Method {
	case MethodAzureCLI:
		return newAzureCLI(ctx, opts)
	case MethodBrowser:
		return newInteractive(ctx, opts)
	}

	// MethodAuto: a working `az login` costs one subprocess call and skips the
	// browser entirely, so try it first and fall through quietly.
	p, err := newAzureCLI(ctx, opts)
	if err == nil {
		opts.Log("using Azure CLI credentials for %s", p.Identity().Label())
		return p, nil
	}
	if !errors.Is(err, ErrNoAzureCLI) {
		opts.Log("azure cli sign-in not usable (%v), falling back to browser", err)
	}
	return newInteractive(ctx, opts)
}

// expirySkew is how long before true expiry a token is treated as stale, so a
// long-running Graph call never starts with a token about to die.
const expirySkew = 5 * time.Minute

func expired(t time.Time) bool {
	return t.IsZero() || time.Now().Add(expirySkew).After(t)
}
