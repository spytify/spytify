package ui

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"

	"github.com/spytify/spytify/internal/ui/lang"
	"github.com/spytify/spytify/internal/ui/link"
	"github.com/spytify/spytify/internal/ui/model"
	"github.com/spytify/spytify/pkg/proto"
)

// With app.check_updates off the app never asks GitHub for releases; on,
// it asks once it is connected. The release is an older one, so the check
// touches no widget from its goroutine (TestANewerReleaseShowsTheBadge
// covers the badge): Fyne's test driver runs fyne.Do on the spot, which
// would race with the render that started the check.
func TestTheUpdateCheckFollowsTheSetting(t *testing.T) {
	var asked atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		asked.Add(1)
		fmt.Fprint(w, `[{"tag_name":"v1.0.0","html_url":"https://x/v1","draft":false}]`)
	}))
	defer srv.Close()
	defer func(u string) { releasesURL = u }(releasesURL)
	releasesURL = srv.URL
	defer func(v string) { proto.Version = v }(proto.Version)
	proto.Version = "v2.0.0"
	for _, on := range []bool{false, true} {
		fa := test.NewTempApp(t)
		fa.Preferences().SetBool(firstRunKey, true)
		m := model.New()
		addr := filepath.Join(t.TempDir(), "absent.sock")
		a := New(fa, link.New(addr, m), m, Options{StateDir: t.TempDir(), Socket: addr, ManualRender: true})
		feedSession(t, m)
		values := defaults()
		values["app.check_updates"] = on
		m.SetSettings(values)
		before := asked.Load()
		a.render()
		deadline := time.Now().Add(3 * time.Second)
		for on && asked.Load() == before && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		if !on {
			time.Sleep(300 * time.Millisecond)
		}
		if got := asked.Load() > before; got != on {
			t.Errorf("check_updates %v: releases asked %v", on, got)
		}
	}
}

func TestNewerVersion(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"v2.0.0", "v2.0.0-alpha+f5dd96e", true},
		{"v2.0.1", "v2.0.0", true},
		{"v2.0.0", "v2.0.0", false},
		{"v2.0.0-beta", "v2.0.0-alpha", true},
		{"v2.0.0-alpha", "v2.0.0-beta", false},
		{"v2.0.0-alpha.10", "v2.0.0-alpha.9", true},
		{"v2.0.0-alpha.1", "v2.0.0-alpha", true},
		{"v2.0.0-alpha", "v2.0.0-alpha+f5dd96e", false},
		{"1.12", "v2.0.0-alpha", false},
		{"", "v2.0.0", false},
		// A development build is after the tag it was made from (one
		// patch on), so it is never offered that tag, nor the release the
		// tag led to; the next patch release is newer.
		{"v2.0.0-alpha.1", "v2.0.1-dev.12", false},
		{"v2.0.0", "v2.0.1-dev.1", false},
		{"v2.0.1", "v2.0.1-dev.12", true},
		{"v2.0.0-alpha.1", "v0.0.0-dev.0", true},
	}
	for _, c := range cases {
		if got := newer(c.latest, c.current); got != c.want {
			t.Errorf("newer(%q, %q) = %v", c.latest, c.current, got)
		}
	}
}

func TestLatestReleaseCountsPreReleases(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `[{"tag_name":"v2.1.0","html_url":"https://x/draft","draft":true},
			{"tag_name":"v2.0.0-alpha.2","html_url":"https://x/a2","draft":false,"prerelease":true},
			{"tag_name":"v2.0.0-alpha.1","html_url":"https://x/a1","draft":false,"prerelease":true}]`)
	}))
	defer srv.Close()
	defer func(u string) { releasesURL = u }(releasesURL)
	releasesURL = srv.URL
	tag, page, err := latestRelease(context.Background())
	if err != nil || tag != "v2.0.0-alpha.2" || page != "https://x/a2" {
		t.Fatalf("latest = %q %q %v", tag, page, err)
	}
	if !newer(tag, "v2.0.0-alpha.1+f5dd96e") {
		t.Fatal("alpha.2 is not newer than alpha.1")
	}
}

func TestANewerReleaseShowsTheBadge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `[{"tag_name":"v9.0.0","html_url":"https://x/v9","draft":false}]`)
	}))
	defer srv.Close()
	defer func(u string) { releasesURL = u }(releasesURL)
	releasesURL = srv.URL
	defer func(v string) { proto.Version = v }(proto.Version)
	proto.Version = "v2.0.0"
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	feedSession(t, m)
	a.render()
	if a.update.Visible() {
		t.Fatal("Update shows before any release was found")
	}
	a.checkUpdate()
	if !a.update.Visible() || a.update.Icon != newReleasesIcon || a.update.Text != fmt.Sprintf(lang.UpdateTo, "v9.0.0") {
		t.Fatalf("Update: visible %v, text %q", a.update.Visible(), a.update.Text)
	}
	snapshot(t, a, "update")
	light := defaults()
	light["app.theme"] = "light"
	m.SetSettings(light)
	a.render()
	snapshot(t, a, "update-light")
}

// A development build, local or from CI, does not look for releases.
func TestADevelopmentBuildShowsNoBadge(t *testing.T) {
	var asked atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		asked.Store(true)
		fmt.Fprint(w, `[{"tag_name":"v9.0.0","html_url":"https://x/v9","draft":false}]`)
	}))
	defer srv.Close()
	defer func(u string) { releasesURL = u }(releasesURL)
	releasesURL = srv.URL
	for _, v := range []string{"v2.0.1-dev.12", "v0.0.0-dev"} {
		func() {
			defer func(old string) { proto.Version = old }(proto.Version)
			proto.Version = v
			a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
			feedSession(t, m)
			a.checkUpdate()
			if a.update.Visible() || asked.Load() {
				t.Errorf("%s: Update visible %v, releases asked %v", v, a.update.Visible(), asked.Load())
			}
		}()
	}
}
