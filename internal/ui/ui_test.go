package ui

import (
	"context"
	"encoding/json"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

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
		case settings.KindSecret:
			values[d.Key] = ""
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
			out = append(out, w.String())
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
		"2 saved · 1 skipped · 0 ads", "Stop", "Player is running", "FLAC", "♥ Donate", "Skip"} {
		if !contains(all, want) {
			t.Errorf("%q not on screen", want)
		}
	}
	if a.tabs.Items[tabRecord].Text != "Record · 2" {
		t.Errorf("Record tab title %q", a.tabs.Items[tabRecord].Text)
	}
	if a.action.Text != "Stop" || a.action.Importance != widget.DangerImportance {
		t.Errorf("app bar action %q", a.action.Text)
	}
	snapshot(t, a, "record")
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

	// A failing check sends the user to the diagnostics.
	failing := info()
	failing.Checks[1].OK = false
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
	for _, want := range []string{"spytifyd is not running", "Start spytifyd", "/tmp/spytify.sock", "Player process", "Save and restart spytifyd"} {
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
	for _, want := range []string{"Player", "Player process", "Output", "Output folder", "output.dir", "Encoding", "Tags", "Reset output settings"} {
		if !contains(all, want) {
			t.Errorf("%q not on the settings screen", want)
		}
	}
	s := a.screens[tabSettings].(*settingsScreen)
	s.search.SetText("spotify")
	a.render()
	if all := texts(a.win.Content()); !contains(all, "Connect to the Spotify Web API") || !contains(all, "tags.spotify.client_id") || contains(all, "output.dir") {
		t.Errorf("search for spotify: %v", all)
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
	for _, want := range []string{"recorder v2.0.0-alpha on darwin/arm64", "Source code on GitHub", "Project page", "Check for updates"} {
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

func TestNewerVersion(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"v2.0.0", "v2.0.0-alpha+f5dd96e", true},
		{"v2.0.1", "v2.0.0", true},
		{"v2.0.0", "v2.0.0", false},
		{"v2.0.0-beta", "v2.0.0-alpha", false},
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
}

func (c *recordingCore) handle(ctx context.Context, method string, params json.RawMessage) (any, error) {
	c.mu.Lock()
	c.calls = append(c.calls, method)
	if method == proto.MethodSettingsSet {
		var p proto.SettingsSetParams
		_ = json.Unmarshal(params, &p)
		c.sets = append(c.sets, p.Values)
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
	check, ok := s.fields["output.underscores"].item.Widget.(*widget.Check)
	if !ok {
		t.Fatal("no checkbox for output.underscores")
	}
	test.Tap(check)
	waitCall(t, core, proto.MethodSettingsSet)
	core.mu.Lock()
	last := core.sets[len(core.sets)-1]
	core.mu.Unlock()
	if last["output.underscores"] != true {
		t.Fatalf("settings.set sent %v", last)
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
