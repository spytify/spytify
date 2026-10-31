package ui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/spytify/spytify/internal/telemetry"
	"github.com/spytify/spytify/internal/telemetry/fakesentry"
	"github.com/spytify/spytify/internal/ui/link"
	"github.com/spytify/spytify/internal/ui/model"
	"github.com/spytify/spytify/pkg/proto"
)

// privacyApp is a test app past its first-run notice, whose bug tracker,
// on or off, reports to a fake Sentry.
func privacyApp(t *testing.T, srv *fakesentry.Server, on bool) (*App, *model.Model) {
	t.Helper()
	fa := test.NewTempApp(t)
	fa.Preferences().SetBool(firstRunKey, true)
	fa.Preferences().SetBool(privacySeenKey, true)
	m := model.New()
	l := link.New(filepath.Join(t.TempDir(), "absent.sock"), m)
	tel := telemetry.New(telemetry.Options{DSN: srv.DSN(), Prefs: fa.Preferences(), TracesSampleRate: 1, ProblemSampleRate: 1})
	t.Cleanup(tel.Close)
	a := New(fa, l, m, Options{StateDir: t.TempDir(), ManualRender: true, NoUpdateCheck: true, AutoStart: true, Telemetry: tel,
		Privacy: map[string]any{trackerKey: on}})
	a.shown = true
	t.Cleanup(a.reports.Wait)
	return a, m
}

// connect gives the app a recorder with the bug tracker on or off.
func connect(t *testing.T, a *App, m *model.Model, on bool) {
	t.Helper()
	m.SetConn(model.Connected, "")
	m.SetInfo(info())
	values := defaults()
	values[trackerKey] = on
	m.SetSettings(values)
	a.render()
}

// failCheck makes ffmpeg's check fail.
func failCheck(a *App, m *model.Model) {
	in := info()
	in.Checks = append(in.Checks, proto.Check{Name: "ffmpeg found", Detail: "not on PATH"})
	m.SetInfo(in)
	a.render()
}

// overlay is the dialog on top of the window.
func overlay(t *testing.T, a *App) fyne.CanvasObject {
	t.Helper()
	top := a.win.Canvas().Overlays().Top()
	if top == nil {
		t.Fatal("no dialog is shown")
	}
	return top
}

func tapButton(t *testing.T, o fyne.CanvasObject, text string) {
	t.Helper()
	b := buttons(o, text)
	if len(b) != 1 {
		t.Fatalf("%d %q buttons", len(b), text)
	}
	test.Tap(b[0])
}

// checkBoxes are the visible check boxes with this label.
func checkBoxes(o fyne.CanvasObject, label string) []*widget.Check {
	var out []*widget.Check
	var walk func(fyne.CanvasObject)
	walk = func(o fyne.CanvasObject) {
		if o == nil || !o.Visible() {
			return
		}
		switch w := o.(type) {
		case *widget.Check:
			if w.Text == label {
				out = append(out, w)
			}
			return
		case *fyne.Container:
			for _, child := range w.Objects {
				walk(child)
			}
			return
		}
		if w, ok := o.(fyne.Widget); ok {
			for _, child := range test.WidgetRenderer(w).Objects() {
				walk(child)
			}
		}
	}
	walk(o)
	return out
}

// quiet waits for anything queued, then says how many requests reached
// the server.
func quiet(a *App, srv *fakesentry.Server) int {
	a.tel.Flush(2 * time.Second)
	time.Sleep(200 * time.Millisecond)
	return srv.Requests()
}

// With the bug tracker off, the app sends nothing at all, not at launch,
// not while recording, not on a problem, a failed start or a crash: the
// crash only shows, and the diagnostics' button says why it is off.
func TestNothingLeavesWhenTheBugTrackerIsOff(t *testing.T) {
	srv := fakesentry.New(t)
	a, m := privacyApp(t, srv, false)
	leaveACrash(t, a, "boom")
	connect(t, a, m, false)
	a.checkCrashes()
	tapButton(t, overlay(t, a), "Close")
	a.launchFailed(errors.New("spytifyd started but does not answer; see its log in Logs"))
	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateRecording}))
	m.Apply(event(t, proto.EventLevel, proto.LevelEvent{PeakDBFS: -12}))
	m.Apply(event(t, proto.EventTrackStarted, proto.TrackStartedEvent{Track: proto.Track{ID: "s-1", Artist: "Neon Harbor", Title: "Midnight Ferry", LengthMs: 200000},
		Cut: &proto.TrackCut{Kind: "boundary", By: "silence", OffsetMs: -480}}))
	m.Apply(event(t, proto.EventTrackFinished, proto.TrackFinishedEvent{ID: "s-1", Path: "/x.flac", Timings: &proto.FinishTimings{Format: "flac", EncodeMs: 900, TotalMs: 1000}}))
	m.Apply(event(t, proto.EventTrackSkipped, proto.TrackSkippedEvent{ID: "s-2", Reason: proto.SkipIncomplete}))
	m.Apply(event(t, proto.EventError, proto.ErrorEvent{Code: "capture_stalled", Msg: "No audio"}))
	a.showTab(tabLogs)
	failCheck(a, m)
	if a.win.Canvas().Overlays().Top() != nil {
		t.Error("a dialog shows with the bug tracker off")
	}
	send := sendButton(t, a)
	if !send.Disabled() || send.ToolTip() != optedOut {
		t.Errorf("Send to the developers: disabled %v, tooltip %q", send.Disabled(), send.ToolTip())
	}
	snapshot(t, a, "privacy-diagnostics-off")
	// The tooltip shows through fyne.Do, which the test driver runs on the
	// tooltip's timer goroutine, not a main loop: hovering races here, so
	// only for the picture, made without -race.
	if os.Getenv("SPYTIFY_UI_SNAPSHOTS") != "" {
		test.MoveMouse(a.win.Canvas(), a.fa.Driver().AbsolutePositionForObject(send).AddXY(20, 10))
		time.Sleep(time.Second)
		snapshot(t, a, "privacy-diagnostics-off-tooltip")
	}
	if n := quiet(a, srv); n != 0 {
		t.Fatalf("%d requests reached Sentry with the bug tracker off", n)
	}
	if left := telemetry.FindCrashes(a.opts.StateDir, a.fa.Preferences(), time.Now()); len(left) != 0 {
		t.Error("the crash would leave once the bug tracker is on")
	}
}

// The first-run notice shows the bug tracker ticked; nothing starts
// before it closes, and unticking it keeps it off.
func TestFirstRunShowsTheBugTrackerTicked(t *testing.T) {
	srv := fakesentry.New(t)
	a, _ := privacyApp(t, srv, true)
	a.shown = false
	a.fa.Preferences().SetBool(firstRunKey, false)
	a.fa.Preferences().SetBool(privacySeenKey, false)
	closed := false
	a.maybeShowFirstRun(func() {
		closed = true
		a.shown = true
		a.renderPrivacy(a.last)
	})
	d := overlay(t, a)
	snapshot(t, a, "privacy-first-run")
	box := checkBoxes(d, trackerLabel)
	if len(box) != 1 || !box[0].Checked {
		t.Fatalf("the notice has %d bug tracker boxes, ticked %v", len(box), len(box) == 1 && box[0].Checked)
	}
	if a.tel.Enabled() {
		t.Fatal("the bug tracker started before the notice closed")
	}
	box[0].SetChecked(false)
	tapButton(t, d, "OK")
	if !closed || a.tel.Enabled() {
		t.Fatalf("closed %v, bug tracker on %v", closed, a.tel.Enabled())
	}
	if !a.fa.Preferences().Bool(privacySeenKey) {
		t.Error("the choice would be shown again")
	}
	if n := quiet(a, srv); n != 0 {
		t.Errorf("%d requests after opting out at first run", n)
	}
}

// The bug tracker follows the setting; on, the recorder's problems leave
// with the steps and clicks that led there, a full disk excepted.
func TestTheBugTrackerFollowsTheSetting(t *testing.T) {
	srv := fakesentry.New(t)
	a, m := privacyApp(t, srv, false)
	connect(t, a, m, false)
	if a.tel.Enabled() {
		t.Fatal("the bug tracker is on with the setting off")
	}
	a.showTab(tabSettings)
	a.render()
	set := a.screens[tabSettings].(*settingsScreen)
	set.search.SetText("privacy")
	if all := texts(set.root); !contains(all, trackerLabel) {
		t.Errorf("the Privacy section shows %v", all)
	}
	snapshot(t, a, "privacy-settings")
	set.search.SetText("")
	m.Apply(event(t, proto.EventSettingsChanged, proto.SettingsValues{Values: map[string]any{trackerKey: true}}))
	a.render()
	if !a.tel.Enabled() {
		t.Fatal("the bug tracker is off with the setting on")
	}
	a.showTab(tabLogs)
	failCheck(a, m)
	tapButton(t, a.win.Content(), "Run the checks again")
	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateRecording}))
	m.Apply(event(t, proto.EventError, proto.ErrorEvent{Code: "disk_full", Msg: "only 10 MB free"}))
	m.Apply(event(t, proto.EventError, proto.ErrorEvent{Code: "capture_stalled", Msg: "No audio from the player for 10 s"}))
	ev := srv.Wait(t, "event", 1)[0].JSON()
	if ev["message"] != "blocked: capture_stalled" {
		t.Errorf("problem %v", ev["message"])
	}
	crumbs, _ := ev["breadcrumbs"].([]any)
	var trail []string
	for _, c := range crumbs {
		trail = append(trail, c.(map[string]any)["message"].(string))
	}
	if all := strings.Join(trail, " | "); !strings.Contains(all, "run the checks again") || !strings.Contains(all, "recording") {
		t.Errorf("breadcrumbs %v", trail)
	}
	m.Apply(event(t, proto.EventSettingsChanged, proto.SettingsValues{Values: map[string]any{trackerKey: false}}))
	a.render()
	if a.tel.Enabled() {
		t.Fatal("the bug tracker is still on")
	}
}
