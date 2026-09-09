package auth

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// azTimeout bounds a single `az account get-access-token` invocation. The CLI
// is a Python program and can take a couple of seconds cold, but it should
// never take thirty.
const azTimeout = 30 * time.Second

// azureCLIProvider borrows the Graph token from an existing `az login`
// session. It shells out to the CLI exactly as azure-identity's
// AzureCliCredential does, and caches the result until it nears expiry so a
// busy TUI does not spawn a subprocess per request.
type azureCLIProvider struct {
	tenantID string

	mu       sync.Mutex
	token    string
	expires  time.Time
	identity Identity
}

// azTokenResponse mirrors `az account get-access-token -o json`.
type azTokenResponse struct {
	AccessToken string `json:"accessToken"`
	// ExpiresOn is a local-time string with no zone ("2026-01-01 12:00:00.000000").
	ExpiresOn string `json:"expiresOn"`
	// ExpiresOnUnix is emitted by newer CLI versions and is unambiguous, so it
	// is preferred whenever present.
	ExpiresOnUnix int64  `json:"expires_on"`
	Tenant        string `json:"tenant"`
}

func (r azTokenResponse) expiry() time.Time {
	if r.ExpiresOnUnix > 0 {
		return time.Unix(r.ExpiresOnUnix, 0)
	}
	if t, err := time.ParseInLocation("2006-01-02 15:04:05.000000", r.ExpiresOn, time.Local); err == nil {
		return t
	}
	if t, err := time.ParseInLocation("2006-01-02 15:04:05", r.ExpiresOn, time.Local); err == nil {
		return t
	}
	// An unparseable expiry must not be treated as "valid forever"; a zero
	// time makes expired() true and forces a refresh on next use.
	return time.Time{}
}

func newAzureCLI(ctx context.Context, opts Options) (Provider, error) {
	p := &azureCLIProvider{tenantID: opts.TenantID}
	if _, err := p.refresh(ctx); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *azureCLIProvider) Token(ctx context.Context) (string, error) {
	p.mu.Lock()
	if !expired(p.expires) {
		token := p.token
		p.mu.Unlock()
		return token, nil
	}
	p.mu.Unlock()
	return p.refresh(ctx)
}

func (p *azureCLIProvider) Identity() Identity {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.identity
}

func (p *azureCLIProvider) refresh(ctx context.Context) (string, error) {
	res, err := runAzTokenCommand(ctx, p.tenantID)
	if err != nil {
		return "", err
	}
	ident := identityFrom(res.AccessToken, MethodAzureCLI)
	if ident.TenantID == "" {
		ident.TenantID = res.Tenant
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	p.token = res.AccessToken
	p.expires = res.expiry()
	p.identity = ident
	return res.AccessToken, nil
}

// azBinary locates the CLI. On Windows `az` is a batch shim, so the bare name
// is not executable via exec.LookPath in every shell configuration.
func azBinary() (string, error) {
	names := []string{"az"}
	if runtime.GOOS == "windows" {
		names = []string{"az.cmd", "az.bat", "az"}
	}
	for _, n := range names {
		if path, err := exec.LookPath(n); err == nil {
			return path, nil
		}
	}
	return "", &SignInError{
		Reason: "the Azure CLI is not installed",
		Detail: "`az` is not on PATH",
		Remedy: installRemedy,
	}
}

func runAzTokenCommand(ctx context.Context, tenantID string) (azTokenResponse, error) {
	var out azTokenResponse

	bin, err := azBinary()
	if err != nil {
		return out, err
	}

	ctx, cancel := context.WithTimeout(ctx, azTimeout)
	defer cancel()

	args := []string{"account", "get-access-token", "--resource", GraphResource, "--output", "json"}
	if tenantID != "" {
		args = append(args, "--tenant", tenantID)
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	// The CLI otherwise decorates output with ANSI colour codes that break
	// JSON parsing on some terminals.
	cmd.Env = append(cmd.Environ(), "AZURE_CORE_NO_COLOR=true", "AZURE_CORE_ONLY_SHOW_ERRORS=true")

	stdout, err := cmd.Output()
	if err != nil {
		return out, classifyAzError(err, tenantID)
	}
	if err := json.Unmarshal(stdout, &out); err != nil {
		return out, &SignInError{
			Reason: "could not read the Azure CLI's reply",
			Detail: err.Error(),
			Remedy: loginRemedy(tenantID),
		}
	}
	if out.AccessToken == "" {
		return out, &SignInError{
			Reason: "the Azure CLI returned an empty access token",
			Remedy: loginRemedy(tenantID),
		}
	}
	return out, nil
}

// classifyAzError turns a failed CLI invocation into an error that says what
// to run. A missing session is by far the most common cause, so it gets its
// own wording; anything else keeps the CLI's own first line, which usually
// names the real problem (an expired refresh token, a tenant the account
// cannot reach).
func classifyAzError(err error, tenantID string) error {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		msg := strings.TrimSpace(string(exitErr.Stderr))
		lower := strings.ToLower(msg)
		switch {
		case strings.Contains(lower, "az login"),
			strings.Contains(lower, "please run"),
			strings.Contains(lower, "no subscription"),
			strings.Contains(lower, "not logged in"),
			strings.Contains(lower, "refresh token has expired"),
			strings.Contains(lower, "interactive authentication is needed"):
			return &SignInError{
				Reason: "not signed in to the Azure CLI",
				Remedy: loginRemedy(tenantID),
			}
		}
		if msg != "" {
			return &SignInError{
				Reason: "the Azure CLI could not get a Graph token",
				Detail: firstLine(msg),
				Remedy: loginRemedy(tenantID),
			}
		}
	}
	return &SignInError{
		Reason: "the Azure CLI could not get a Graph token",
		Detail: err.Error(),
		Remedy: loginRemedy(tenantID),
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}
