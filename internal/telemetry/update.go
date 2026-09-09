package telemetry

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

const releasesURL = "https://api.github.com/repos/idoavrah/entra-tui/releases/latest"

// StartUpdateCheck asks GitHub for the newest release, in the background.
// Like everything else here it never blocks and never fails loudly.
func (c *Client) StartUpdateCheck() {
	if c == nil {
		return
	}
	go func() {
		latest := c.latestRelease()
		c.mu.Lock()
		c.latest = latest
		c.mu.Unlock()
	}()
}

// UpdateAvailable reports whether GitHub is genuinely *ahead* of this build.
//
// Comparing for inequality would announce an update to anyone running a
// development build, which is newer than the published one rather than older.
func (c *Client) UpdateAvailable() bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	latest := c.latest
	c.mu.RUnlock()
	return latest != "" && isNewer(latest, c.version)
}

// UpdateSuffix is what the header adds beside the version, or empty.
func (c *Client) UpdateSuffix() string {
	if c.UpdateAvailable() {
		return " (update available)"
	}
	return ""
}

func (c *Client) latestRelease() string {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releasesURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", app+"/"+c.version)

	resp, err := c.http.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return ""
	}
	return release.TagName
}

// isNewer reports whether candidate is a later release than current.
func isNewer(candidate, current string) bool {
	left, right := releaseParts(candidate), releaseParts(current)
	if len(left) == 0 || len(right) == 0 {
		// Unparseable on either side -- a "dev" build, say. Saying nothing is
		// better than announcing an update nobody can act on.
		return false
	}
	for i := range max(len(left), len(right)) {
		l, r := at(left, i), at(right, i)
		if l != r {
			return l > r
		}
	}
	return false
}

// releaseParts is the leading numeric release segment, so "v1.2.3-rc1" is
// (1, 2, 3).
func releaseParts(version string) []int {
	var out []int
	for _, chunk := range strings.Split(strings.TrimPrefix(version, "v"), ".") {
		digits := ""
		for _, r := range chunk {
			if r < '0' || r > '9' {
				break
			}
			digits += string(r)
		}
		if digits == "" {
			break
		}
		n, err := strconv.Atoi(digits)
		if err != nil {
			break
		}
		out = append(out, n)
	}
	return out
}

func at(parts []int, i int) int {
	if i < len(parts) {
		return parts[i]
	}
	return 0
}
