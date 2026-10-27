package ui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"

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
