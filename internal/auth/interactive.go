package auth

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/public"
)

// interactiveProvider runs the OAuth 2.0 authorization code flow with PKCE
// against a loopback redirect: MSAL starts a listener on 127.0.0.1, opens the
// system browser at the real Entra sign-in page, and collects the code from
// the redirect. Because the browser does the talking, MFA and Conditional
// Access policies work exactly as they do on the web.
type interactiveProvider struct {
	client  public.Client
	scopes  []string
	account public.Account

	mu       sync.Mutex
	token    string
	expires  time.Time
	identity Identity
}

func newInteractive(ctx context.Context, opts Options) (Provider, error) {
	opts.applyDefaults()

	authority := fmt.Sprintf("https://login.microsoftonline.com/%s", opts.TenantID)
	client, err := public.New(opts.ClientID, public.WithAuthority(authority))
	if err != nil {
		return nil, fmt.Errorf("configure public client: %w", err)
	}

	p := &interactiveProvider{client: client, scopes: opts.Scopes}

	opts.Log("opening your browser to sign in to %s...", authority)

	// An empty host lets MSAL pick a free loopback port, which matches the
	// wildcard http://localhost redirect registered on public clients.
	args := []public.AcquireInteractiveOption{public.WithRedirectURI("http://localhost")}
	if opts.OpenURL != nil {
		args = append(args, public.WithOpenURL(opts.OpenURL))
	}
	res, err := client.AcquireTokenInteractive(ctx, opts.Scopes, args...)
	if err != nil {
		return nil, fmt.Errorf("interactive sign-in: %w", err)
	}

	p.store(res.AccessToken, res.ExpiresOn)
	p.account = res.Account
	opts.Log("signed in as %s", p.Identity().Label())
	return p, nil
}

func (p *interactiveProvider) store(token string, expires time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.token = token
	p.expires = expires
	p.identity = identityFrom(token, MethodBrowser)
}

func (p *interactiveProvider) Token(ctx context.Context) (string, error) {
	p.mu.Lock()
	if !expired(p.expires) {
		token := p.token
		p.mu.Unlock()
		return token, nil
	}
	p.mu.Unlock()

	// MSAL keeps an in-memory cache holding the refresh token for this
	// process, so a silent renewal normally succeeds without user interaction.
	res, err := p.client.AcquireTokenSilent(ctx, p.scopes,
		public.WithSilentAccount(p.account))
	if err != nil {
		return "", fmt.Errorf("refresh access token (sign in again): %w", err)
	}
	p.store(res.AccessToken, res.ExpiresOn)
	return res.AccessToken, nil
}

func (p *interactiveProvider) Identity() Identity {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.identity
}
