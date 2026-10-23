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

const (
	releasesURL = "https://api.github.com/repos/spytify/spytify/releases/latest"
	donateURL   = "https://spytify.github.io/spytify/donate.html"
	projectURL  = "https://github.com/spytify/spytify"
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
	var r struct {
		Tag  string `json:"tag_name"`
		Page string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", "", err
	}
	return r.Tag, r.Page, nil
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
	return c.pre != "" && l.pre == ""
}

func at(n []int, i int) int {
	if i < len(n) {
		return n[i]
	}
	return 0
}
