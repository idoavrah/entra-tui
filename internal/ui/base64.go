package ui

import "encoding/base64"

// base64Encode is the payload encoding required by OSC 52.
func base64Encode(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}
