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
	a := New(fa, l, m, Options{StateDir: t.TempDir(), Socket: addr, ManualRender: true})
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
	for _, want := range []string{"RECORDING", "Midnight City", "M83 · Hurry Up, We're Dreaming", "Reunion", "Tagged", "Skipped: duplicate",
		"2 saved · 1 skipped · 0 ads", "Stop", "● Player running", "FLAC"} {
		if !contains(all, want) {
			t.Errorf("%q not on screen", want)
		}
	}
	if !contains(all, "● 2") {
		t.Error("sidebar has no live count")
	}
	snapshot(t, a, "record")
}

func TestEmptyStateShowsTheChecklist(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	m.SetConn(model.Connected, "")
	m.SetInfo(info())
	m.SetSettings(defaults())
	a.render()
	all := texts(a.win.Content())
	for _, want := range []string{"Ready to record", "player running", "pid 4242", "capture helper", "turn crossfade and gapless playback off", "Record"} {
		if !contains(all, want) {
			t.Errorf("%q not on the empty state", want)
		}
	}
	snapshot(t, a, "empty")
}

func TestOfflineScreen(t *testing.T) {
	a, m, _ := newTestApp(t, "/tmp/spytify.sock")
	m.SetConn(model.Disconnected, "spytifyd is not running")
	a.render()
	all := texts(a.win.Content())
	for _, want := range []string{"spytifyd is not running", "Start spytifyd", "/tmp/spytify.sock", "Player", "Save and restart spytifyd"} {
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
	for i, name := range []string{"library", "settings", "logs", "about"} {
		a.sidebar.Select(i + 1)
		a.render()
		snapshot(t, a, name)
	}
	a.sidebar.Select(2)
	a.render()
	all := texts(a.win.Content())
	for _, want := range []string{"Output", "Output folder", "output.dir", "Encoding", "Tags", "Connect to the Spotify Web API", "Player", "Reset group"} {
		if !contains(all, want) {
			t.Errorf("%q not on the settings screen", want)
		}
	}
	a.sidebar.Select(3)
	a.render()
	if all := texts(a.win.Content()); !contains(all, "already recorded") || !contains(all, "session 20261023-120000 started") {
		t.Errorf("logs screen: %v", all)
	}
	a.sidebar.Select(4)
	a.render()
	if all := texts(a.win.Content()); !contains(all, "core v2.0.0-alpha (darwin/arm64)") || !contains(all, "bundle org.example.player") {
		t.Errorf("about screen: %v", all)
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

	a.sidebar.Select(2)
	a.render()
	s := a.screens[2].(*settingsScreen)
	check := findCheck(s.fields["output.underscores"].row)
	if check == nil {
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

func findCheck(o fyne.CanvasObject) *widget.Check {
	switch w := o.(type) {
	case *widget.Check:
		return w
	case *fyne.Container:
		for _, c := range w.Objects {
			if found := findCheck(c); found != nil {
				return found
			}
		}
	}
	return nil
}
