package graph

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// APIError is a structured Microsoft Graph error response.
type APIError struct {
	Status    int
	Code      string
	Message   string
	RequestID string
}

func (e *APIError) Error() string {
	// A permission failure needs three words, not a paragraph. Graph's own
	// message for a 403 is a boilerplate sentence that says nothing the
	// status code did not, and repeating it crowds out whatever the user was
	// actually doing.
	if e.Status == http.StatusForbidden {
		return "no permission"
	}

	msg := e.Message
	if msg == "" {
		msg = http.StatusText(e.Status)
	}
	if e.Code != "" && !strings.EqualFold(e.Code, msg) {
		return fmt.Sprintf("graph %d %s: %s", e.Status, e.Code, msg)
	}
	return fmt.Sprintf("graph %d: %s", e.Status, msg)
}

// Hint returns actionable guidance for the errors a read-only directory
// browser actually hits, so the TUI can show something better than a bare
// status code.
func (e *APIError) Hint() string {
	switch e.Status {
	case http.StatusUnauthorized:
		return "Your session expired or the token was rejected. Restart entra-tui to sign in again."
	case http.StatusForbidden:
		// Deliberately silent: "no permission" is the whole story, and an
		// explanation of consent models is not what someone wants mid-task.
		return ""
	case http.StatusNotFound:
		return "That object no longer exists, or it is not visible to your account."
	case http.StatusTooManyRequests:
		return "Microsoft Graph is throttling this tenant. Wait a moment before retrying."
	}
	if e.Status >= 500 {
		return "Microsoft Graph returned a server error. This is usually transient."
	}
	return ""
}

// graphErrorEnvelope mirrors the Graph error payload shape.
type graphErrorEnvelope struct {
	Error struct {
		Code       string `json:"code"`
		Message    string `json:"message"`
		InnerError struct {
			RequestID string `json:"request-id"`
		} `json:"innerError"`
	} `json:"error"`
}

// parseAPIError builds an APIError from a non-2xx response body, degrading
// gracefully when the body is empty or not the expected JSON shape.
func parseAPIError(status int, body []byte) *APIError {
	e := &APIError{Status: status}
	var env graphErrorEnvelope
	if err := json.Unmarshal(body, &env); err == nil {
		e.Code = env.Error.Code
		e.Message = env.Error.Message
		e.RequestID = env.Error.InnerError.RequestID
	}
	if e.Message == "" {
		if s := strings.TrimSpace(string(body)); s != "" && len(s) < 400 {
			e.Message = s
		}
	}
	return e
}
