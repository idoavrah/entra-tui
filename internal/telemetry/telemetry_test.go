package telemetry

import (
	"strings"
	"testing"
	"time"
)

func TestRedactKeepsTheFileAndDropsTheDirectories(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{
			"a unix path",
			"open /home/ada/projects/secret-client/main.go: no such file",
			"open /***/***/***/***/main.go: no such file",
		},
		{
			"a windows path",
			`C:\Users\ada\Documents\work\notes.txt`,
			`C:\***\***\***\***\notes.txt`,
		},
		{"prose is untouched", "the request was refused", "the request was refused"},
		{"a bare filename is untouched", "main.go:42", "main.go:42"},
		{"a URL keeps its last segment", "https://graph.microsoft.com/v1.0/users", "https://***/***/users"},
	} {
		if got := Redact(tc.in); got != tc.want {
			t.Errorf("%s: Redact(%q) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}
}

func TestRedactLeavesNoUsernameBehind(t *testing.T) {
	// The point of the whole exercise: a home directory names a person.
	const home = "ada.lovelace"
	got := Redact("panic at /Users/" + home + "/src/entra-tui/internal/ui/view.go:120")
	if strings.Contains(got, home) {
		t.Errorf("Redact left the username in: %q", got)
	}
	if !strings.Contains(got, "view.go") {
		t.Errorf("Redact took the filename too: %q", got)
	}
}

func TestOptingOutSendsNothingAndStartsNothing(t *testing.T) {
	// A disabled client must not even resolve the machine handle: opting out
	// should cost nothing and touch nothing.
	c := Disabled()
	if c.Enabled() {
		t.Error("a disabled client reports itself enabled")
	}
	if c.Handle() != "" {
		t.Errorf("a disabled client derived a handle: %q", c.Handle())
	}

	// Capturing is a no-op rather than a panic, and shutting down returns at
	// once because there is no worker to wait for.
	c.Capture("started application", Properties{"view": "users"})
	c.CaptureError("exited unsuccessfully", "boom")

	done := make(chan struct{})
	go func() { c.Shutdown(2 * time.Second); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("Shutdown on a disabled client waited for something")
	}
}

func TestNilClientIsSafe(t *testing.T) {
	// The UI holds this as a plain field; a zero Options must not panic.
	var c *Client
	c.Capture("started application", nil)
	c.CaptureError("exited unsuccessfully", "boom")
	c.StartUpdateCheck()
	c.Shutdown(time.Second)
	if c.Enabled() || c.UpdateAvailable() || c.UpdateSuffix() != "" || c.Handle() != "" {
		t.Error("a nil client claimed to be doing something")
	}
}

func TestCaptureNeverBlocks(t *testing.T) {
	// The queue is bounded and full events are dropped, so a stalled network
	// cannot back up into a keystroke.
	c := &Client{enabled: true, queue: make(chan event, 2), done: make(chan struct{})}
	done := make(chan struct{})
	go func() {
		for range queueDepth * 4 {
			c.Capture("started application", nil)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Capture blocked when the queue was full")
	}
}

func TestHandleIsStableAndTwoWords(t *testing.T) {
	const fingerprint = "linux-somehost-amd64"
	first, second := HandleFor(fingerprint), HandleFor(fingerprint)
	if first != second {
		t.Errorf("the same fingerprint gave %q then %q", first, second)
	}
	if len(strings.Fields(first)) != 2 {
		t.Errorf("handle %q is not two words", first)
	}
	if other := HandleFor("linux-otherhost-amd64"); other == first {
		t.Errorf("two machines share the handle %q", first)
	}
	if strings.Contains(first, "somehost") {
		t.Errorf("handle %q carries the fingerprint it came from", first)
	}
}

func TestUpdateIsOnlyAnnouncedWhenGitHubIsAhead(t *testing.T) {
	for _, tc := range []struct {
		latest, current string
		want            bool
	}{
		{"v0.2.0", "0.1.0", true},
		{"v0.1.1", "0.1.0", true},
		{"v1.0.0", "0.9.9", true},
		{"v0.1.0", "0.1.0", false},
		{"v0.1.0", "0.2.0", false},
		// A development build is newer than the published one, not older.
		{"v0.1.0", "dev", false},
		{"", "0.1.0", false},
	} {
		if got := isNewer(tc.latest, tc.current); got != tc.want {
			t.Errorf("isNewer(%q, %q) = %v, want %v", tc.latest, tc.current, got, tc.want)
		}
	}
}
