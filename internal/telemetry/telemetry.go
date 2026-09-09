// Package telemetry is anonymous, opt-out usage tracking, and the update
// check that runs alongside it.
//
// It mirrors what terraform-tui does, down to the vocabulary of the machine
// handle, and posts to the same PostHog project. Every event carries
// app="entra-tui", which is what separates the two tools in the logs.
//
// Three rules hold everywhere in this package:
//
//   - Nothing here can block the interface. Every call returns immediately;
//     work happens on one worker goroutine.
//   - Nothing here can take the application down. Failures are dropped.
//   - Nothing here carries directory data. See the note on Capture.
package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	posthogHost = "https://app.posthog.com"
	// posthogKey is a project write key. It is public by design -- it can
	// send events and read nothing -- which is why it lives in the source
	// rather than in a secret.
	posthogKey = "phc_tjGzx7V6Y85JdNfOFWxQLXo5wtUs6MeVLvoVfybqz09"

	// app separates entra-tui from terraform-tui, which posts to the same
	// project. Every event carries it.
	app = "entra-tui"

	requestTimeout = 5 * time.Second
	// queueDepth is how many events may be waiting. Beyond it events are
	// dropped rather than made to wait: a keystroke is worth more than a
	// statistic.
	queueDepth = 32
)

// token is a run of non-whitespace, which is as much of a path as a
// traceback ever puts in one piece.
var token = regexp.MustCompile(`\S+`)

// Redact strips directory names out of text before it is sent anywhere.
//
// Only the intermediate components go: a redacted error keeps its shape and
// the name of the failing file, without carrying the directory structure of
// somebody's machine. Go's regexp has no lookaround, so the walk is explicit
// rather than a pattern.
func Redact(text string) string {
	return token.ReplaceAllStringFunc(text, func(t string) string {
		if !strings.ContainsAny(t, `/\`) {
			return t
		}
		return redactPath(t)
	})
}

func redactPath(s string) string {
	var out strings.Builder
	start := 0
	for i := 0; i <= len(s); i++ {
		if i < len(s) && s[i] != '/' && s[i] != '\\' {
			continue
		}
		component := s[start:i]
		// A component is intermediate when a separator both precedes and
		// follows it, which is exactly the directory names.
		if component != "" && start > 0 && i < len(s) {
			out.WriteString("***")
		} else {
			out.WriteString(component)
		}
		if i < len(s) {
			out.WriteByte(s[i])
		}
		start = i + 1
	}
	return out.String()
}

// Properties are the fields sent with an event.
//
// Only ever put in here things that describe the *shape* of what somebody did
// -- which view, which relationship, how many results, whether it worked.
// Never a display name, an object id, a tenant, a user principal name or a
// search term: this tool browses a directory, and none of it belongs in
// anybody's analytics.
type Properties map[string]any

// Client sends events. The zero value is unusable; use New or Disabled.
type Client struct {
	enabled bool
	version string
	handle  string
	http    *http.Client

	queue chan event
	once  sync.Once
	done  chan struct{}

	mu     sync.RWMutex
	latest string
}

type event struct {
	name  string
	props Properties
}

// New returns a client. When enabled is false nothing is ever sent and no
// worker is started, so opting out costs nothing at all.
func New(enabled bool, version string) *Client {
	c := &Client{
		enabled: enabled,
		version: version,
		http:    &http.Client{Timeout: requestTimeout},
		queue:   make(chan event, queueDepth),
		done:    make(chan struct{}),
	}
	if !enabled {
		close(c.done)
		return c
	}
	c.handle = MachineHandle()
	go c.run()
	return c
}

// Disabled is a client that does nothing. Demo mode and the tests use it.
func Disabled() *Client { return New(false, "") }

// Enabled reports whether anything is being sent.
func (c *Client) Enabled() bool { return c != nil && c.enabled }

// Handle is the two-word name this machine is known by, for the help screen.
func (c *Client) Handle() string {
	if c == nil {
		return ""
	}
	return c.handle
}

// Capture records a usage event. It never blocks and never fails: if the queue
// is full the event is dropped, because a statistic is not worth a keystroke.
//
// Read the note on Properties before adding a field.
func (c *Client) Capture(name string, props Properties) {
	if !c.Enabled() {
		return
	}
	select {
	case c.queue <- event{name: name, props: props}:
	default:
	}
}

// CaptureError records a failure, with file paths redacted first.
func (c *Client) CaptureError(name, detail string) {
	c.Capture(name, Properties{"error_message": Redact(detail)})
}

// run is the single worker. Events keep their order and none of them touches
// the goroutine that draws the screen.
func (c *Client) run() {
	defer close(c.done)
	for e := range c.queue {
		c.send(e)
	}
}

func (c *Client) send(e event) {
	props := Properties{
		"app":               app,
		"entra_tui_version": c.version,
		"platform":          runtime.GOOS + "/" + runtime.GOARCH,
	}
	for k, v := range e.props {
		props[k] = v
	}

	body, err := json.Marshal(map[string]any{
		"api_key":     posthogKey,
		"event":       e.name,
		"distinct_id": c.handle,
		"properties":  props,
	})
	if err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, posthogHost+"/capture/", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")

	// Whatever comes back, there is nothing useful to do about it: this is
	// telemetry, and the user is in the middle of something else.
	resp, err := c.http.Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()
}

// Shutdown stops accepting events and waits, briefly, for the queue to drain.
// A slow network must not hold up quitting.
func (c *Client) Shutdown(timeout time.Duration) {
	if c == nil {
		return
	}
	c.once.Do(func() { close(c.queue) })
	select {
	case <-c.done:
	case <-time.After(timeout):
	}
}
