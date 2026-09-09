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

func signInError(t *testing.T, err error) *SignInError {
	t.Helper()
	var e *SignInError
	if !errors.As(err, &e) {
		t.Fatalf("error %v is not a *SignInError", err)
	}
	return e
}

func TestClassifyAzErrorMarksNotSignedIn(t *testing.T) {
	// The single most common failure, and the one whose whole value is the
	// remedy it comes with.
	err := classifyAzError(&exec.ExitError{Stderr: []byte("ERROR: Please run 'az login' to setup account.")}, "")
	e := signInError(t, err)

	if !strings.Contains(e.Reason, "not signed in") {
		t.Errorf("reason = %q, want it to say the session is missing", e.Reason)
	}
	if !strings.Contains(e.Error(), "az login") {
		t.Errorf("error = %q, want it to name the command that fixes it", e)
	}
}

func TestNotSignedInRemedyCarriesTheChosenTenant(t *testing.T) {
	// Signing in to the wrong tenant fixes nothing, so the suggested command
	// repeats whichever tenant was asked for.
	err := classifyAzError(&exec.ExitError{Stderr: []byte("ERROR: Please run 'az login'")}, "contoso.com")
	if got := err.Error(); !strings.Contains(got, "az login --tenant contoso.com") {
		t.Errorf("error = %q, want the tenant carried into the remedy", got)
	}
}

func TestClassifyAzErrorKeepsOnlyTheFirstStderrLine(t *testing.T) {
	err := classifyAzError(&exec.ExitError{Stderr: []byte("ERROR: something exotic\nsecond line")}, "")
	e := signInError(t, err)

	if !strings.Contains(e.Detail, "something exotic") {
		t.Errorf("detail = %q, want the CLI's own message", e.Detail)
	}
	if strings.Contains(e.Detail, "second line") {
		t.Errorf("detail = %q, want only the first stderr line", e.Detail)
	}
	if !strings.Contains(e.Error(), "az login") {
		t.Errorf("error = %q, want a remedy even on an unrecognised failure", e)
	}
}

func TestSignInErrorReadsAsThreeParts(t *testing.T) {
	e := &SignInError{Reason: "the sky fell", Detail: "on tuesday", Remedy: "wait"}
	if got, want := e.Error(), "the sky fell: on tuesday\n\nwait"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	// Reason alone is what telemetry sends, so it must stand on its own.
	bare := &SignInError{Reason: "the sky fell"}
	if got := bare.Error(); got != "the sky fell" {
		t.Errorf("Error() = %q, want just the reason", got)
	}
}
