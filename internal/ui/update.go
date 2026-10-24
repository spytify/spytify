package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// releasesURL lists the newest releases, pre-releases included: GitHub's
// "latest" leaves those out, and the alphas are pre-releases.
var releasesURL = "https://api.github.com/repos/spytify/spytify/releases?per_page=10"

const (
	donateURL = "https://spytify.github.io/spytify/v2/donate.html"
	siteURL   = "https://spytify.github.io/spytify/v2/overview.html"
	sourceURL = "https://github.com/spytify/spytify"
)

// latestRelease asks GitHub for the newest published release. It is the
// only outbound call the UI makes, and only with app.check_updates on.
func latestRelease(ctx context.Context) (tag, page string, err error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releasesURL, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", "", nil
	}
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("GitHub answered %s", resp.Status)
	}
	var list []struct {
		Tag   string `json:"tag_name"`
		Page  string `json:"html_url"`
		Draft bool   `json:"draft"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return "", "", err
	}
	// Newest first; a draft is not published yet.
	for _, r := range list {
		if !r.Draft {
			return r.Tag, r.Page, nil
		}
	}
	return "", "", nil
}

type semver struct {
	nums []int
	pre  string
}

func parseVersion(v string) semver {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	var s semver
	if i := strings.IndexByte(v, '-'); i >= 0 {
		v, s.pre = v[:i], v[i+1:]
	}
	for _, part := range strings.Split(v, ".") {
		n, err := strconv.Atoi(part)
		if err != nil {
			break
		}
		s.nums = append(s.nums, n)
	}
	return s
}

// newer reports whether latest is a later version than current; a release
// is later than a pre-release of the same number.
func newer(latest, current string) bool {
	l, c := parseVersion(latest), parseVersion(current)
	if len(l.nums) == 0 || len(c.nums) == 0 {
		return false
	}
	for i := 0; i < 3; i++ {
		a, b := at(l.nums, i), at(c.nums, i)
		if a != b {
			return a > b
		}
	}
	switch {
	case l.pre == c.pre:
		return false
	case l.pre == "":
		return true // a release beats its pre-releases
	case c.pre == "":
		return false
	}
	return prereleaseAfter(l.pre, c.pre)
}

// prereleaseAfter orders pre-release tags as semver does: dot-separated
// fields left to right, numbers by value and below words, words as text,
// and a longer tag after its own prefix (alpha.1 after alpha).
func prereleaseAfter(a, b string) bool {
	x, y := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(x) && i < len(y); i++ {
		if x[i] == y[i] {
			continue
		}
		m, errM := strconv.Atoi(x[i])
		n, errN := strconv.Atoi(y[i])
		switch {
		case errM == nil && errN == nil:
			return m > n
		case errM == nil:
			return false
		case errN == nil:
			return true
		}
		return x[i] > y[i]
	}
	return len(x) > len(y)
}

func at(n []int, i int) int {
	if i < len(n) {
		return n[i]
	}
	return 0
}
