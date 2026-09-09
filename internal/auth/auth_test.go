package auth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// makeJWT builds an unsigned token whose payload carries the given claims.
// Only the payload is ever read, so the header and signature are filler.
func makeJWT(t *testing.T, payload map[string]any) string {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"none"}`)) + "." + enc(body) + ".sig"
}

func TestDecodeClaimsReadsIdentity(t *testing.T) {
	token := makeJWT(t, map[string]any{
		"upn":  "ada@contoso.com",
		"name": "Ada Lovelace",
		"tid":  "11111111-2222-3333-4444-555555555555",
	})

	c := decodeClaims(token)
	if c.account() != "ada@contoso.com" {
		t.Errorf("account = %q, want ada@contoso.com", c.account())
	}
	if c.TenantID != "11111111-2222-3333-4444-555555555555" {
		t.Errorf("tid = %q", c.TenantID)
	}
}

func TestAccountFallsBackThroughClaimNames(t *testing.T) {
	// Different Entra token versions populate different claims.
	preferred := decodeClaims(makeJWT(t, map[string]any{"preferred_username": "grace@x.com"}))
	if got := preferred.account(); got != "grace@x.com" {
		t.Errorf("account = %q, want the preferred_username fallback", got)
	}
	unique := decodeClaims(makeJWT(t, map[string]any{"unique_name": "alan@x.com"}))
	if got := unique.account(); got != "alan@x.com" {
		t.Errorf("account = %q, want the unique_name fallback", got)
	}
}

func TestDecodeClaimsToleratesGarbage(t *testing.T) {
	// These claims only feed the status bar, so a token that is opaque or
	// malformed must degrade to an empty identity rather than fail the run.
	for _, tok := range []string{
		"", "not-a-jwt", "a.b", "a.b.c.d",
		"x." + base64.RawURLEncoding.EncodeToString([]byte("not json")) + ".y",
		"x.!!!not-base64!!!.y",
	} {
		if got := decodeClaims(tok); got.account() != "" || got.TenantID != "" {
			t.Errorf("decodeClaims(%q) = %+v, want a zero value", tok, got)
		}
	}
}

func TestIdentityLabelPrefersAccountThenName(t *testing.T) {
	if got := (Identity{Account: "a@x.com", Name: "A"}).Label(); got != "a@x.com" {
		t.Errorf("Label = %q, want the account", got)
	}
	if got := (Identity{Name: "Ada"}).Label(); got != "Ada" {
		t.Errorf("Label = %q, want the name fallback", got)
	}
	if got := (Identity{}).Label(); got != "unknown" {
		t.Errorf("Label = %q, want unknown", got)
	}
}

func TestParseMethod(t *testing.T) {
	for in, want := range map[string]Method{
		"":         MethodAuto,
		"auto":     MethodAuto,
		"BROWSER":  MethodBrowser,
		"azurecli": MethodAzureCLI,
		" az ":     MethodAzureCLI,
		"cli":      MethodAzureCLI,
	} {
		got, err := ParseMethod(in)
		if err != nil {
			t.Errorf("ParseMethod(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseMethod(%q) = %q, want %q", in, got, want)
		}
	}
	if _, err := ParseMethod("clientsecret"); err == nil {
		t.Error("ParseMethod accepted an unsupported method")
	}
}

func TestDefaultScopesAreLeastPrivilege(t *testing.T) {
	scopes := DefaultScopes()
	joined := strings.Join(scopes, " ")

	for _, want := range []string{"User.Read.All", "Group.Read.All", "Application.Read.All"} {
		if !strings.Contains(joined, want) {
			t.Errorf("default scopes %v missing %s", scopes, want)
		}
	}
	// Directory.Read.All would grant far more than the four read-only views
	// need; Application.Read.All already covers both app collections.
	if strings.Contains(joined, "Directory.Read.All") {
		t.Errorf("default scopes %v include the broad Directory.Read.All", scopes)
	}
	// Nothing that permits a write may ever appear here.
	for _, forbidden := range []string{"ReadWrite", ".Write", "Directory.AccessAsUser"} {
		if strings.Contains(joined, forbidden) {
			t.Errorf("default scopes %v include a write-capable scope %q", scopes, forbidden)
		}
	}
}

func TestAzTokenExpiryPrefersUnixField(t *testing.T) {
	unix := time.Now().Add(time.Hour).Unix()
	r := azTokenResponse{ExpiresOn: "2020-01-01 00:00:00.000000", ExpiresOnUnix: unix}

	if got := r.expiry().Unix(); got != unix {
		t.Errorf("expiry = %d, want the unambiguous unix field %d", got, unix)
	}
}

func TestAzTokenExpiryParsesLocalTimeString(t *testing.T) {
	r := azTokenResponse{ExpiresOn: "2030-06-01 12:30:00.000000"}
	got := r.expiry()

	if got.IsZero() {
		t.Fatal("expiry is zero, want the local-time string parsed")
	}
	if got.Year() != 2030 || got.Month() != time.June || got.Hour() != 12 {
		t.Errorf("expiry = %v, want 2030-06-01 12:30 local", got)
	}

	// Some CLI versions omit the fractional seconds.
	if r2 := (azTokenResponse{ExpiresOn: "2030-06-01 12:30:00"}); r2.expiry().IsZero() {
		t.Error("expiry without fractional seconds failed to parse")
	}
}

func TestAzTokenExpiryUnparseableIsTreatedAsStale(t *testing.T) {
	// A zero time makes expired() true, forcing a refresh. Treating an
	// unreadable expiry as "valid forever" would strand a dead token.
	r := azTokenResponse{ExpiresOn: "sometime next tuesday"}
	if !r.expiry().IsZero() {
		t.Errorf("expiry = %v, want the zero time", r.expiry())
	}
	if !expired(r.expiry()) {
		t.Error("expired(zero) = false, want a zero expiry to be stale")
	}
}

func TestExpiredAppliesSkew(t *testing.T) {
	// A token dying in two minutes must be refreshed now, not handed to a
	// request that may take longer than that.
	if !expired(time.Now().Add(2 * time.Minute)) {
		t.Error("a token expiring inside the skew window was treated as fresh")
	}
	if expired(time.Now().Add(time.Hour)) {
		t.Error("a token valid for an hour was treated as stale")
	}
}

func TestClassifyAzErrorMarksNotSignedIn(t *testing.T) {
	// A missing session is an ordinary state that auto mode steps over.
	err := classifyAzError(&exec.ExitError{Stderr: []byte("ERROR: Please run 'az login' to setup account.")})
	if !errors.Is(err, ErrNoAzureCLI) {
		t.Errorf("error %v does not wrap ErrNoAzureCLI", err)
	}
}

func TestClassifyAzErrorWrapsUnknownFailures(t *testing.T) {
	err := classifyAzError(&exec.ExitError{Stderr: []byte("ERROR: something exotic\nsecond line")})
	if !errors.Is(err, ErrNoAzureCLI) {
		t.Errorf("error %v does not wrap ErrNoAzureCLI", err)
	}
	if strings.Contains(err.Error(), "second line") {
		t.Errorf("error = %q, want only the first stderr line", err)
	}
}

func TestApplyDefaultsFillsEveryField(t *testing.T) {
	var o Options
	o.applyDefaults()

	if o.ClientID != DefaultClientID || o.TenantID != DefaultTenant {
		t.Errorf("applyDefaults left identity fields empty: %+v", o)
	}
	if o.Method != MethodAuto || len(o.Scopes) == 0 || o.Log == nil {
		t.Errorf("applyDefaults left behaviour fields empty: %+v", o)
	}
	// Log must be safe to call so callers need no nil check.
	o.Log("smoke %s", "test")
}
