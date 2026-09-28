// Package updatecheck checks for a newer stable linear-tui release at startup.
package updatecheck

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	latestReleaseAPI = "https://api.github.com/repos/roeyazroel/linear-tui/releases/latest"
	releaseURLBase   = "https://github.com/roeyazroel/linear-tui/releases/tag/"
	requestTimeout   = 3 * time.Second
	maxResponseBody  = 1 << 20
	maxVersionLen    = 128
)

var versionPattern = regexp.MustCompile(`^(?:v)?([0-9]+)\.([0-9]+)\.([0-9]+)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$`)

// Check returns a user-facing notice when a newer stable release is available.
// It returns an empty string for development builds, unavailable releases, and
// any network or response error.
func Check(ctx context.Context, currentVersion string) string {
	return check(ctx, currentVersion, http.DefaultClient, latestReleaseAPI)
}

type release struct {
	TagName    string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
}

type version struct {
	major, minor, patch uint64
	prerelease          bool
	tag                 string
}

func check(parent context.Context, currentVersion string, client *http.Client, endpoint string) string {
	current, ok := parseVersion(currentVersion)
	if !ok || client == nil {
		return ""
	}
	if parent == nil {
		parent = context.Background()
	}

	ctx, cancel := context.WithTimeout(parent, requestTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ""
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "linear-tui")

	response, err := client.Do(request)
	if err != nil {
		return ""
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return ""
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBody+1))
	if err != nil || len(body) > maxResponseBody {
		return ""
	}
	var latestRelease release
	if err := json.Unmarshal(body, &latestRelease); err != nil {
		return ""
	}
	if latestRelease.Draft || latestRelease.Prerelease {
		return ""
	}
	latest, ok := parseVersion(latestRelease.TagName)
	if !ok || latest.prerelease || compareVersions(latest, current) <= 0 {
		return ""
	}

	return fmt.Sprintf("New linear-tui version %s available — %s", latest.display(), releaseURL(latest))
}

func releaseURL(v version) string {
	// parseVersion only permits SemVer characters, so the tag cannot inject a
	// path or query component into this fixed, trusted repository URL.
	return releaseURLBase + v.tag
}

func parseVersion(raw string) (version, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > maxVersionLen || strings.EqualFold(raw, "dev") || strings.EqualFold(raw, "unknown") {
		return version{}, false
	}
	match := versionPattern.FindStringSubmatch(raw)
	if match == nil || !validPrerelease(match[4]) {
		return version{}, false
	}
	numbers := [3]uint64{}
	for i, text := range match[1:4] {
		number, ok := parseCoreNumber(text)
		if !ok {
			return version{}, false
		}
		numbers[i] = number
	}
	return version{major: numbers[0], minor: numbers[1], patch: numbers[2], prerelease: match[4] != "", tag: raw}, true
}

func parseCoreNumber(text string) (uint64, bool) {
	if text == "" || (len(text) > 1 && text[0] == '0') {
		return 0, false
	}
	number, err := strconv.ParseUint(text, 10, 64)
	return number, err == nil
}

func validPrerelease(text string) bool {
	if text == "" {
		return true
	}
	for _, part := range strings.Split(text, ".") {
		if len(part) > 1 && part[0] == '0' {
			numeric := true
			for _, char := range part {
				if char < '0' || char > '9' {
					numeric = false
					break
				}
			}
			if numeric {
				return false
			}
		}
	}
	return true
}

func compareVersions(a, b version) int {
	if a.major != b.major {
		if a.major < b.major {
			return -1
		}
		return 1
	}
	if a.minor != b.minor {
		if a.minor < b.minor {
			return -1
		}
		return 1
	}
	if a.patch != b.patch {
		if a.patch < b.patch {
			return -1
		}
		return 1
	}
	if a.prerelease == b.prerelease {
		return 0
	}
	if a.prerelease {
		return -1
	}
	return 1
}

func (v version) display() string {
	return fmt.Sprintf("v%d.%d.%d", v.major, v.minor, v.patch)
}
