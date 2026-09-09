package auth

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

// claims is the subset of an access token payload used for display.
type claims struct {
	UPN               string `json:"upn"`
	PreferredUsername string `json:"preferred_username"`
	UniqueName        string `json:"unique_name"`
	Name              string `json:"name"`
	TenantID          string `json:"tid"`
	Scope             string `json:"scp"`
}

func (c claims) account() string {
	for _, v := range []string{c.UPN, c.PreferredUsername, c.UniqueName} {
		if v != "" {
			return v
		}
	}
	return ""
}

// decodeClaims reads the payload of a JWT without verifying its signature.
//
// That is deliberate and safe here: the token came from MSAL or the Azure CLI
// over TLS and is only ever forwarded to Graph, which does verify it. These
// claims drive nothing but the text in the status bar, so a malformed or
// opaque token simply yields an empty Identity rather than an error.
func decodeClaims(token string) claims {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return claims{}
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return claims{}
	}
	var c claims
	if err := json.Unmarshal(payload, &c); err != nil {
		return claims{}
	}
	return c
}

// identityFrom builds a display identity from a raw access token.
func identityFrom(token string, method Method) Identity {
	c := decodeClaims(token)
	return Identity{
		Account:  c.account(),
		Name:     c.Name,
		TenantID: c.TenantID,
		Method:   method,
	}
}
