package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	ttwidget "github.com/dweymouth/fyne-tooltip/widget"

	"github.com/spytify/spytify/internal/bus"
	"github.com/spytify/spytify/internal/settings"
	"github.com/spytify/spytify/internal/ui/link"
	"github.com/spytify/spytify/internal/ui/model"
	"github.com/spytify/spytify/pkg/proto"
)

func event(t *testing.T, method string, payload any) proto.Event {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return proto.Event{Method: method, Params: raw}
}

func defaults() map[string]any {
	values := map[string]any{}
	v := settings.Defaults()
	for _, d := range settings.Table {
		switch d.Kind {
		case settings.KindBool:
			values[d.Key] = v.Bool(d.Key)
		case settings.KindInt, settings.KindIntEnum:
			values[d.Key] = float64(v.Int(d.Key))
		case settings.KindFloat:
			values[d.Key] = v.Float(d.Key)
		default:
			values[d.Key] = v.String(d.Key)
		}
	}
	values["output.dir"] = "/Users/listener/Music/Spytify"
	values["encode.format"] = "flac"
	return values
}

func info() proto.CoreInfoResult {
	return proto.CoreInfoResult{
		Version: "v2.0.0-alpha", Protocol: proto.Protocol, Platform: "darwin/arm64",
		Target: proto.TargetInfo{Process: "Player", BundleID: "org.example.player"},
		Checks: []proto.Check{
			{Name: "target configured", OK: true, Detail: "SPYTIFY_TARGET_PROCESS=Player"},
			{Name: "player running", OK: true, Detail: "pid 4242"},
			{Name: "ffmpeg found", OK: true, Detail: "ffmpeg version 9.0.2"},
			{Name: "capture helper", OK: true, Detail: "/Applications/Spytify.app/Contents/MacOS/spytify-tap"},
		},
		Warnings: []string{"turn crossfade and gapless playback off in the player"},
	}
}

// newTestApp builds the window over a model the test feeds directly.
func newTestApp(t *testing.T, addr string) (*App, *model.Model, *link.Link) {
	t.Helper()
	fa := test.NewTempApp(t)
	fa.Preferences().SetBool(firstRunKey, true)
	m := model.New()
	l := link.New(addr, m)
	a := New(fa, l, m, Options{StateDir: t.TempDir(), Socket: addr, ManualRender: true, NoUpdateCheck: true})
	a.win.Resize(fyne.NewSize(920, 620))
	return a, m, l
}

func feedSession(t *testing.T, m *model.Model) {
	m.SetConn(model.Connected, "")
	m.SetInfo(info())
	m.SetSettings(defaults())
	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateWaiting}))
	m.Apply(event(t, proto.EventTargetState, proto.TargetStateEvent{Running: true, PID: 4242}))
	for i, title := range []string{"Intro", "Reunion", "Wait"} {
		id := "s-00" + string(rune('1'+i))
		m.Apply(event(t, proto.EventTrackStarted, proto.TrackStartedEvent{Track: proto.Track{ID: id, Artist: "M83", Title: title, Album: "Hurry Up, We're Dreaming", LengthMs: 134000 + int64(i)*20000}}))
		switch i {
		case 2:
			m.Apply(event(t, proto.EventTrackSkipped, proto.TrackSkippedEvent{ID: id, Reason: proto.SkipDuplicate}))
		default:
			m.Apply(event(t, proto.EventTrackFinished, proto.TrackFinishedEvent{ID: id, Path: "/Users/listener/Music/Spytify/M83/" + title + ".flac", DurationMs: 134000 + int64(i)*102000, Bytes: 18_200_000 + int64(i)*12_800_000, Tagged: true, TaggedBy: "lastfm"}))
		}
	}
	m.Apply(event(t, proto.EventTrackStarted, proto.TrackStartedEvent{Track: proto.Track{ID: "s-004", Artist: "M83", Title: "Midnight City", Album: "Hurry Up, We're Dreaming", LengthMs: 243000}}))
	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateRecording}))
	m.Apply(event(t, proto.EventLevel, proto.LevelEvent{PeakDBFS: -3, RMSDBFS: -14}))
	m.Apply(event(t, proto.EventLog, proto.LogEvent{Level: "info", Msg: "session 20261023-120000 started", TS: time.Now().Format(time.RFC3339Nano)}))
	m.Apply(event(t, proto.EventLog, proto.LogEvent{Level: "error", Msg: "\"Wait\": already recorded", TS: time.Now().Format(time.RFC3339Nano)}))
}

// snapshot saves the window as PNG when SPYTIFY_UI_SNAPSHOTS names a folder.
func snapshot(t *testing.T, a *App, name string) {
	t.Helper()
	dir := os.Getenv("SPYTIFY_UI_SNAPSHOTS")
	if dir == "" {
		return
	}
	img := a.win.Canvas().Capture()
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

// texts collects every label and button text under o.
func texts(o fyne.CanvasObject) []string {
	var out []string
	var walk func(fyne.CanvasObject)
	walk = func(o fyne.CanvasObject) {
		if o == nil || !o.Visible() {
			return
		}
		switch w := o.(type) {
		case *widget.Label:
			out = append(out, w.Text)
		case *widget.RichText:
			for _, seg := range w.Segments {
				if ts, ok := seg.(*widget.TextSegment); ok {
					out = append(out, ts.Text)
				}
			}
		case *canvas.Text:
			out = append(out, w.Text)
		case *widget.Hyperlink:
			out = append(out, w.Text)
		case *widget.Button:
			out = append(out, w.Text)
		case *widget.Card:
			out = append(out, w.Title)
			walk(w.Content)
			return
		case *fyne.Container:
			for _, c := range w.Objects {
				walk(c)
			}
			return
		}
		if w, ok := o.(fyne.Widget); ok {
			r := test.WidgetRenderer(w)
			for _, c := range r.Objects() {
				walk(c)
			}
		}
	}
	walk(o)
	return out
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if strings.Contains(s, want) {
			return true
		}
	}
	return false
}

func TestRecordScreenShowsTheSession(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	feedSession(t, m)
	a.render()
	all := texts(a.win.Content())
	for _, want := range []string{"● RECORDING", "Midnight City", "M83 · Hurry Up, We're Dreaming", "Reunion", "Tagged", "Skipped: duplicate",
		"2 saved · 1 skipped · 0 ads", "Stop", "Player is running", "FLAC", "Skip"} {
		if !contains(all, want) {
			t.Errorf("%q not on screen", want)
		}
	}
	// The app bar's Donate is a heart, as in v1; About has the worded one.
	if contains(all, "♥ Donate") || !a.donate.Visible() || a.donate.Icon != heartIcon || a.donate.ToolTip() != "Donate" {
		t.Error("the app bar's Donate is not a heart alone")
	}
	if a.tabs.Items[tabRecord].Text != "Record · 2" {
		t.Errorf("Record tab title %q", a.tabs.Items[tabRecord].Text)
	}
	r := a.screens[tabRecord].(*recordScreen)
	if r.clear.Disabled() {
		t.Error("Clear list is disabled with rows in the list")
	}
	if a.action.Text != "Stop" || a.action.Importance != widget.DangerImportance {
		t.Errorf("app bar action %q", a.action.Text)
	}
	snapshot(t, a, "record")

	test.Tap(r.clear)
	a.render()
	if s := m.Snapshot(); len(s.Rows) != 0 || s.Current == nil {
		t.Errorf("after Clear list: %d rows, current %v", len(s.Rows), s.Current)
	}
	if !r.clear.Disabled() || !contains(texts(a.win.Content()), "No files yet") {
		t.Error("the cleared list does not say it is empty")
	}
}

func TestWaitingOffersToRestartTheTrack(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	m.SetConn(model.Connected, "")
	m.SetInfo(info())
	m.SetSettings(defaults())
	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateWaiting}))
	a.render()
	all := texts(a.win.Content())
	if !contains(all, "Waiting for the next track to start.") || !contains(all, "Record this track from the start") {
		t.Fatalf("waiting state: %v", all)
	}
	snapshot(t, a, "waiting")
}

func TestIdleRecordScreenIsJustAMessage(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	m.SetConn(model.Connected, "")
	m.SetInfo(info())
	m.SetSettings(defaults())
	a.render()
	all := texts(a.win.Content())
	for _, want := range []string{"Ready to record", "Press Record, then play music in Player."} {
		if !contains(all, want) {
			t.Errorf("%q not on the idle screen", want)
		}
	}
	for _, unwanted := range []string{"pid 4242", "capture helper", "Open diagnostics"} {
		if contains(all, unwanted) {
			t.Errorf("%q is on the idle Record screen; the checks live in Logs", unwanted)
		}
	}
	if n := len(buttons(a.win.Content(), "Record")); n != 1 {
		t.Errorf("%d Record buttons; want the app bar's only", n)
	}
	snapshot(t, a, "idle")

	// A player that is not running is offered to open, not diagnosed.
	down := info()
	down.Checks[1].OK = false
	m.SetInfo(down)
	a.render()
	if all := texts(a.win.Content()); !contains(all, "Open Player") || contains(all, "Open diagnostics") {
		t.Errorf("player not running: %v", all)
	}
	// It starts: the check follows the target's state, and the offer goes.
	m.Apply(event(t, proto.EventTargetState, proto.TargetStateEvent{Running: true, PID: 99}))
	a.render()
	if all := texts(a.win.Content()); contains(all, "Open Player") || !checkOK(m.Snapshot().Info.Checks, "player running") {
		t.Errorf("player started: %v", all)
	}
	// Any other failing check sends the user to the diagnostics.
	failing := info()
	failing.Checks[2].OK = false
	m.SetInfo(failing)
	a.render()
	if !contains(texts(a.win.Content()), "Open diagnostics") {
		t.Error("no way to the diagnostics while a check fails")
	}
}

func TestOfflineScreen(t *testing.T) {
	a, m, _ := newTestApp(t, "/tmp/spytify.sock")
	m.SetConn(model.Disconnected, "spytifyd is not running")
	a.render()
	all := texts(a.win.Content())
	for _, want := range []string{"The recorder is not running", "spytifyd is not running", "Start the recorder", "/tmp/spytify.sock", "Player process", "Save and restart spytifyd"} {
		if !contains(all, want) {
			t.Errorf("%q not on the offline screen", want)
		}
	}
	if !a.action.Disabled() {
		t.Error("Record is enabled without a daemon")
	}
	snapshot(t, a, "offline")
}

func TestOtherScreensRender(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	feedSession(t, m)
	a.render()
	for i, name := range map[int]string{tabLibrary: "library", tabSettings: "settings", tabLogs: "logs", tabAbout: "about"} {
		a.showTab(i)
		a.render()
		snapshot(t, a, name)
	}

	a.showTab(tabSettings)
	a.render()
	all := texts(a.win.Content())
	for _, want := range []string{"App", "Minimize to tray", "Output", "Output folder", "Group by folders", "Encoding", "Output format",
		"Recording", "Normalize volume", "Ads", "Mute ads", "Advanced", "Reset"} {
		if !contains(all, want) {
			t.Errorf("%q not on the settings screen", want)
		}
	}
	// Advanced settings are folded, and no field carries a caption.
	for _, unwanted := range []string{"File name template", "Player process", "Embed album cover", "output.dir", "Log level", "Language"} {
		if contains(all, unwanted) {
			t.Errorf("%q shows on the settings screen", unwanted)
		}
	}
	if g := groups(); g[0] != "App" {
		t.Errorf("sections %v: App is not first", g)
	}
	s := a.screens[tabSettings].(*settingsScreen)
	s.search.SetText("cover")
	a.render()
	if all := texts(a.win.Content()); !contains(all, "Embed album cover") || contains(all, "Output folder") {
		t.Errorf("search for cover: %v", all)
	}
	s.search.SetText("player")
	a.render()
	if all := texts(a.win.Content()); !contains(all, "Player process") || !contains(all, "Save and restart spytifyd") {
		t.Errorf("search for player: %v", all)
	}
	s.search.SetText("")

	a.showTab(tabLogs)
	a.render()
	all = texts(a.win.Content())
	for _, want := range []string{"Diagnostics · all 4 checks pass", "already recorded", "session 20261023-120000 started"} {
		if !contains(all, want) {
			t.Errorf("%q not on the logs screen", want)
		}
	}

	a.showTab(tabAbout)
	a.render()
	all = texts(a.win.Content())
	for _, want := range []string{"recorder v2.0.0-alpha on darwin/arm64", "Source code on GitHub", "Project page", "Check for updates", "♥ Donate"} {
		if !contains(all, want) {
			t.Errorf("%q not on the about screen", want)
		}
	}
}

func TestLibraryRescansWhenShown(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	dir := t.TempDir()
	values := defaults()
	values["output.dir"] = dir
	m.SetConn(model.Connected, "")
	m.SetInfo(info())
	m.SetSettings(values)
	a.render()
	lib := a.screens[tabLibrary].(*libraryScreen)

	a.showTab(tabLibrary)
	a.render()
	if len(lib.shown) != 0 {
		t.Fatalf("%d files in an empty folder", len(lib.shown))
	}
	// A file lands while another tab is open; coming back lists it.
	a.showTab(tabRecord)
	if err := os.WriteFile(filepath.Join(dir, "Artist - Title.flac"), []byte("fLaC"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.showTab(tabLibrary)
	a.render()
	if len(lib.shown) != 1 || !contains(texts(a.win.Content()), "Artist - Title") {
		t.Errorf("library after coming back: %d files", len(lib.shown))
	}
}

func TestThemeFollowsTheSetting(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	values := defaults()
	values["app.theme"] = "light"
	m.SetConn(model.Connected, "")
	m.SetInfo(info())
	m.SetSettings(values)
	a.render()
	th := a.fa.Settings().Theme()
	// The green replaces the blue, whatever variant the OS asks for.
	if got := th.Color(theme.ColorNamePrimary, theme.VariantDark); got != greenLight {
		t.Errorf("light theme primary = %v", got)
	}
	values["app.theme"] = "dark"
	m.SetSettings(values)
	a.render()
	if got := a.fa.Settings().Theme().Color(theme.ColorNamePrimary, theme.VariantLight); got != greenDark {
		t.Errorf("dark theme primary = %v", got)
	}
	values["app.theme"] = "system"
	m.SetSettings(values)
	a.render()
	if got := a.fa.Settings().Theme().Color(theme.ColorNamePrimary, theme.VariantLight); got != greenLight {
		t.Errorf("system theme on a light OS = %v", got)
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
	}
	for _, c := range cases {
		if got := newer(c.latest, c.current); got != c.want {
			t.Errorf("newer(%q, %q) = %v", c.latest, c.current, got)
		}
	}
}

// recordingCore is a fake daemon that remembers what the UI asked.
type recordingCore struct {
	mu    sync.Mutex
	calls []string
	sets  []map[string]any
	stops []proto.SessionStopParams
}

func (c *recordingCore) handle(ctx context.Context, method string, params json.RawMessage) (any, error) {
	c.mu.Lock()
	c.calls = append(c.calls, method)
	if method == proto.MethodSettingsSet {
		var p proto.SettingsSetParams
		_ = json.Unmarshal(params, &p)
		c.sets = append(c.sets, p.Values)
	}
	if method == proto.MethodSessionStop {
		var p proto.SessionStopParams
		_ = json.Unmarshal(params, &p)
		c.stops = append(c.stops, p)
	}
	c.mu.Unlock()
	switch method {
	case proto.MethodCoreInfo:
		return info(), nil
	case proto.MethodSettingsGet:
		return proto.SettingsValues{Values: defaults()}, nil
	case proto.MethodLogTail:
		return proto.LogTailResult{}, nil
	case proto.MethodEventsRecent:
		return proto.EventsRecentResult{}, nil
	case proto.MethodSessionStatus:
		return proto.SessionStatusResult{State: proto.StateIdle}, nil
	case proto.MethodSettingsSet:
		return proto.SettingsSetResult{Applied: []string{"x"}, Rejected: map[string]string{}}, nil
	}
	return proto.Empty{}, nil
}

func (c *recordingCore) called(method string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, m := range c.calls {
		if m == method {
			return true
		}
	}
	return false
}

func TestButtonsSendActions(t *testing.T) {
	dir, err := os.MkdirTemp("", "spyui")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	core := &recordingCore{}
	srv := bus.NewServer(core.handle, nil)
	sock := filepath.Join(dir, "s.sock")
	if err := srv.ListenUnix(sock); err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	a, m, l := newTestApp(t, sock)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go l.Run(ctx)
	deadline := time.Now().Add(5 * time.Second)
	for m.Snapshot().Conn != model.Connected {
		if time.Now().After(deadline) {
			t.Fatal("UI did not connect")
		}
		time.Sleep(20 * time.Millisecond)
	}
	a.render()

	test.Tap(a.action)
	waitCall(t, core, proto.MethodSessionStart)

	a.showTab(tabSettings)
	a.render()
	s := a.screens[tabSettings].(*settingsScreen)
	sw, ok := s.fields["output.underscores"].item.Widget.(*toggle)
	if !ok {
		t.Fatal("no switch for output.underscores")
	}
	test.Tap(sw)
	waitCall(t, core, proto.MethodSettingsSet)
	core.mu.Lock()
	last := core.sets[len(core.sets)-1]
	core.mu.Unlock()
	if last["output.underscores"] != true {
		t.Fatalf("settings.set sent %v", last)
	}

	// A text setting applies when it loses focus, once, if it changed.
	e := s.fields["output.template"].input.(*applyEntry)
	e.FocusLost()
	e.SetText("{title}")
	e.FocusLost()
	e.FocusLost()
	waitSets(t, core, 2)
	core.mu.Lock()
	sets := append([]map[string]any(nil), core.sets...)
	core.mu.Unlock()
	if len(sets) != 2 || sets[1]["output.template"] != "{title}" {
		t.Fatalf("settings.set calls %v", sets)
	}

	// Quitting the app stops the recorder too.
	a.stopDaemon()
	waitCall(t, core, proto.MethodCoreShutdown)
}

func TestQuitWhileRecordingAsksFirst(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	feedSession(t, m)
	a.render()
	a.quit()
	top := a.win.Canvas().Overlays().Top()
	if top == nil || !contains(texts(top), "Quit Spytify?") {
		t.Fatal("quitting during a recording did not ask")
	}
}

func waitSets(t *testing.T, c *recordingCore, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		c.mu.Lock()
		got := len(c.sets)
		c.mu.Unlock()
		if got >= n {
			time.Sleep(100 * time.Millisecond)
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d settings.set calls, want %d", got, n)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func waitCall(t *testing.T, c *recordingCore, method string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !c.called(method) {
		if time.Now().After(deadline) {
			t.Fatalf("%s was not sent; calls: %v", method, c.calls)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func buttons(o fyne.CanvasObject, text string) []*widget.Button {
	var out []*widget.Button
	var walk func(fyne.CanvasObject)
	walk = func(o fyne.CanvasObject) {
		if o == nil || !o.Visible() {
			return
		}
		switch w := o.(type) {
		case *widget.Button:
			if w.Text == text {
				out = append(out, w)
			}
			return
		case *ttwidget.Button:
			if w.Text == text {
				out = append(out, &w.Button)
			}
			return
		case *fyne.Container:
			for _, c := range w.Objects {
				walk(c)
			}
			return
		}
		if w, ok := o.(fyne.Widget); ok {
			for _, c := range test.WidgetRenderer(w).Objects() {
				walk(c)
			}
		}
	}
	walk(o)
	return out
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
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	feedSession(t, m)
	a.render()
	if a.update.Visible() {
		t.Fatal("Update shows before any release was found")
	}
	a.checkUpdate()
	if !a.update.Visible() || a.update.Icon != newReleasesIcon || a.update.Text != "Update to v9.0.0" {
		t.Fatalf("Update: visible %v, text %q", a.update.Visible(), a.update.Text)
	}
	snapshot(t, a, "update")
}

func TestLibraryShowsEmbeddedCovers(t *testing.T) {
	ff, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	var picture bytes.Buffer
	if err := png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	cover := filepath.Join(t.TempDir(), "cover.png")
	if err := os.WriteFile(cover, picture.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	track := filepath.Join(dir, "Artist - Song.flac")
	cmd := exec.Command(ff, "-v", "error", "-nostdin", "-f", "lavfi", "-t", "0.5", "-i", "anullsrc", "-i", cover,
		"-map", "0:a", "-map", "1:v", "-c:a", "flac", "-c:v", "copy", "-disposition:v", "attached_pic", track)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("make the FLAC: %v: %s", err, b)
	}
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	values := defaults()
	values["output.dir"] = dir
	m.SetConn(model.Connected, "")
	m.SetInfo(info())
	m.SetSettings(values)
	a.showTab(tabLibrary)
	a.render()
	lib := a.screens[tabLibrary].(*libraryScreen)
	if len(lib.shown) != 1 {
		t.Fatalf("%d files listed", len(lib.shown))
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if res := lib.cover(lib.shown[0]); res != nil {
			if !bytes.Equal(res.Content(), picture.Bytes()) {
				t.Fatal("the row shows another picture")
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the cover never loaded")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Hover and selection are drawn over buttons: an opaque colour would hide
// a red Stop under the pointer.
func TestHoverAndSelectionAreTranslucent(t *testing.T) {
	th := spytifyTheme{mode: "system"}
	for _, v := range []fyne.ThemeVariant{theme.VariantLight, theme.VariantDark} {
		for _, name := range []fyne.ThemeColorName{theme.ColorNameHover, theme.ColorNameSelection} {
			if _, _, _, a := th.Color(name, v).RGBA(); a >= 0xffff {
				t.Errorf("%s is opaque in variant %d", name, v)
			}
		}
	}
}

func TestSourceText(t *testing.T) {
	for _, c := range []struct {
		rate, ch int
		want     string
	}{{44100, 2, "44.1 kHz stereo"}, {48000, 1, "48 kHz mono"}, {96000, 6, "96 kHz 6 channels"}, {0, 2, ""}} {
		if got := sourceText(c.rate, c.ch); got != c.want {
			t.Errorf("sourceText(%d, %d) = %q, want %q", c.rate, c.ch, got, c.want)
		}
	}
}

func TestAFailedCheckReadsAsWhatIsWrong(t *testing.T) {
	for name, want := range map[string]string{
		"ffmpeg found": "ffmpeg not found", "player running": "player not running",
		"output folder writable": "output folder not writable", "capture helper": "capture helper",
	} {
		if got := checkLabel(proto.Check{Name: name}); got != want {
			t.Errorf("checkLabel(%q) = %q, want %q", name, got, want)
		}
		if got := checkLabel(proto.Check{Name: name, OK: true}); got != name {
			t.Errorf("a passing %q reads %q", name, got)
		}
	}
}

// Under a dialog the meter stays still: moving it there repaints the whole
// dimmed window, two thirds of a core with the first-run dialog open.
func TestTheMeterWaitsUnderADialog(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	feedSession(t, m)
	a.render()
	meter := a.screens[tabRecord].(*recordScreen).level
	before := meter.value
	m.Apply(event(t, proto.EventLevel, proto.LevelEvent{RMSDBFS: -3}))
	d := dialog.NewInformation("A dialog", "covering the meter", a.win)
	d.Show()
	a.renderLevel()
	if meter.value != before {
		t.Fatalf("the meter moved under a dialog: %v to %v", before, meter.value)
	}
	d.Hide()
	a.renderLevel()
	if meter.value == before {
		t.Fatal("the meter did not move once the dialog closed")
	}
}

// QA found closing the window on macOS left the app running with Minimize
// to tray off. Closing quits unless the setting is on and there is a tray.
func TestClosingTheWindowQuitsUnlessMinimizedToTray(t *testing.T) {
	for _, c := range []struct {
		name       string
		tray, mini bool
		quits      bool
	}{
		{"setting on, tray", true, true, false},
		{"setting off, tray", true, false, true},
		{"setting on, no tray", false, true, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
			values := defaults()
			values["app.minimize_to_tray"] = c.mini
			m.SetConn(model.Connected, "")
			m.SetSettings(values)
			a.render()
			quits := 0
			a.tray, a.quitApp = c.tray, func() { quits++ }
			a.closeWindow()
			if (quits == 1) != c.quits {
				t.Fatalf("quit %d times, want quitting %v", quits, c.quits)
			}
		})
	}
}

// With a player set, the app starts the recorder itself: the tabs show,
// saying so, instead of the offline screen, which only a start that failed
// shows, with the reason.
func TestTheAppStartsTheRecorderItself(t *testing.T) {
	fa := test.NewTempApp(t)
	fa.Preferences().SetBool(firstRunKey, true)
	m := model.New()
	l := link.New(filepath.Join(t.TempDir(), "absent.sock"), m)
	a := New(fa, l, m, Options{StateDir: t.TempDir(), ManualRender: true, NoUpdateCheck: true, AutoStart: true})
	m.SetConn(model.Disconnected, "spytifyd is not running")
	a.render()
	snapshot(t, a, "starting")
	all := texts(a.win.Content())
	if !contains(all, "Starting the recorder…") || contains(all, "Start the recorder") || contains(all, "The recorder is not running") {
		t.Fatalf("starting: %v", all)
	}
	if a.center.Objects[0] != a.tabs || a.tabs.SelectedIndex() != tabRecord {
		t.Fatal("the Record tab is not shown while the recorder starts")
	}
	for i := range a.tabs.Items {
		a.showTab(i)
		a.render()
	}
	a.showTab(tabRecord)
	a.startFailed(errors.New("spytifyd not found next to Spytify"))
	all = texts(a.win.Content())
	for _, want := range []string{"The recorder is not running", "spytifyd not found next to Spytify", "Start the recorder"} {
		if !contains(all, want) {
			t.Errorf("%q not shown after a failed start: %v", want, all)
		}
	}
	m.SetConn(model.Connected, "")
	m.SetInfo(info())
	a.render()
	if contains(texts(a.win.Content()), "Starting the recorder…") {
		t.Error("still starting once connected")
	}
}

// Record needs the player: with it closed the button is off, and the app
// bar says why. A recording under way keeps its Stop.
func TestRecordWaitsForThePlayer(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	m.SetConn(model.Connected, "")
	m.SetInfo(info())
	m.SetSettings(defaults())
	m.Apply(event(t, proto.EventTargetState, proto.TargetStateEvent{Running: false}))
	a.render()
	if !a.action.Disabled() || !strings.Contains(a.status.Text, "is not running") {
		t.Fatalf("player closed: Record disabled %v, status %q", a.action.Disabled(), a.status.Text)
	}
	m.Apply(event(t, proto.EventTargetState, proto.TargetStateEvent{Running: true, PID: 7}))
	a.render()
	if a.action.Disabled() {
		t.Fatal("Record stays disabled once the player runs")
	}
	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateRecording}))
	m.Apply(event(t, proto.EventTargetState, proto.TargetStateEvent{Running: false}))
	a.render()
	if a.action.Disabled() || a.action.Text != "Stop" {
		t.Fatalf("player quit while recording: %q disabled %v", a.action.Text, a.action.Disabled())
	}
}

// A player that is off is in the app bar and offered to open; the
// diagnostics are not, even while its check still says so.
func TestNoDiagnosticsForAPlayerThatIsOff(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	m.SetConn(model.Connected, "")
	stale := info()
	stale.Checks[1].OK = false
	m.SetInfo(stale)
	m.SetSettings(defaults())
	a.render()
	all := texts(a.win.Content())
	if !contains(all, "Open Player") || contains(all, "Open diagnostics") {
		t.Errorf("player off: %v", all)
	}
	// The check can lag behind the player's state, which the app bar
	// shows: it sends nobody to the diagnostics either.
	m.Apply(event(t, proto.EventTargetState, proto.TargetStateEvent{Running: true, PID: 7}))
	m.SetInfo(stale)
	a.render()
	if all := texts(a.win.Content()); contains(all, "Open diagnostics") {
		t.Errorf("a stale player check sends to the diagnostics: %v", all)
	}
}
