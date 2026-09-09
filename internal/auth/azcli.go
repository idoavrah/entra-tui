package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	opts.applyDefaults()
	p := &azureCLIProvider{}
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
	res, err := runAzTokenCommand(ctx)
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
	return "", fmt.Errorf("%w: `az` not found on PATH", ErrNoAzureCLI)
}

func runAzTokenCommand(ctx context.Context) (azTokenResponse, error) {
	var out azTokenResponse

	bin, err := azBinary()
	if err != nil {
		return out, err
	}

	ctx, cancel := context.WithTimeout(ctx, azTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin,
		"account", "get-access-token",
		"--resource", GraphResource,
		"--output", "json")
	// The CLI otherwise decorates output with ANSI colour codes that break
	// JSON parsing on some terminals.
	cmd.Env = append(cmd.Environ(), "AZURE_CORE_NO_COLOR=true", "AZURE_CORE_ONLY_SHOW_ERRORS=true")

	stdout, err := cmd.Output()
	if err != nil {
		return out, classifyAzError(err)
	}
	if err := json.Unmarshal(stdout, &out); err != nil {
		return out, fmt.Errorf("%w: parsing az output: %v", ErrNoAzureCLI, err)
	}
	if out.AccessToken == "" {
		return out, fmt.Errorf("%w: az returned an empty access token", ErrNoAzureCLI)
	}
	return out, nil
}

// classifyAzError turns a failed CLI invocation into either ErrNoAzureCLI
// (an ordinary "not signed in" state that auto mode should step over) or a
// real error worth reporting.
func classifyAzError(err error) error {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		msg := strings.TrimSpace(string(exitErr.Stderr))
		lower := strings.ToLower(msg)
		switch {
		case strings.Contains(lower, "az login"),
			strings.Contains(lower, "please run"),
			strings.Contains(lower, "no subscription"),
			strings.Contains(lower, "not logged in"),
			strings.Contains(lower, "interactive authentication is needed"):
			return fmt.Errorf("%w: not signed in to the Azure CLI", ErrNoAzureCLI)
		}
		if msg != "" {
			return fmt.Errorf("%w: az failed: %s", ErrNoAzureCLI, firstLine(msg))
		}
	}
	return fmt.Errorf("%w: %v", ErrNoAzureCLI, err)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}
