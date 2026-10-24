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

// privacyApp is a test app whose reports go to a fake Sentry, with the
// privacy settings given (anonymous stats and crash reports).
func privacyApp(t *testing.T, srv *fakesentry.Server, stats bool, crashes string) (*App, *model.Model) {
	t.Helper()
	fa := test.NewTempApp(t)
	fa.Preferences().SetBool(firstRunKey, true)
	fa.Preferences().SetBool(privacySeenKey, true)
	m := model.New()
	l := link.New(filepath.Join(t.TempDir(), "absent.sock"), m)
	tel := telemetry.New(telemetry.Options{DSN: srv.DSN(), Prefs: fa.Preferences(), Names: m.Names, TracesSampleRate: 1, ProblemSampleRate: 1})
	t.Cleanup(tel.Close)
	a := New(fa, l, m, Options{StateDir: t.TempDir(), ManualRender: true, NoUpdateCheck: true, Telemetry: tel,
		Privacy: map[string]any{statsKey: stats, crashesKey: crashes}})
	return a, m
}

// connect gives the app a recorder with these privacy settings.
func connect(t *testing.T, a *App, m *model.Model, stats bool, crashes string) {
	t.Helper()
	m.SetConn(model.Connected, "")
	m.SetInfo(info())
	values := defaults()
	values[statsKey], values[crashesKey] = stats, crashes
	m.SetSettings(values)
	a.render()
}

// leaveACrash writes a trace where spytifyd keeps the previous run's,
// after the app first looked.
func leaveACrash(t *testing.T, a *App, msg string) {
	t.Helper()
	telemetry.FindCrashes(a.opts.StateDir, a.fa.Preferences(), time.Now().Add(-time.Hour))
	trace := "panic: " + msg + "\n\ngoroutine 9 [running]:\ngithub.com/spytify/spytify/internal/core.(*Session).run(0x1)\n\tgithub.com/spytify/spytify/internal/core/session.go:300 +0x10\n"
	if err := os.WriteFile(filepath.Join(a.opts.StateDir, "spytifyd.crash.log.1"), []byte(trace), 0o600); err != nil {
		t.Fatal(err)
	}
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

// With anonymous stats off and crash reports set to never, the app sends
// nothing at all: not at launch, not while recording, not on a problem, a
// failed start or a crash.
func TestNothingLeavesWhenEverythingIsOff(t *testing.T) {
	srv := fakesentry.New(t)
	a, m := privacyApp(t, srv, false, "never")
	leaveACrash(t, a, "boom")
	connect(t, a, m, false, "never")
	a.checkCrashes()
	a.launchFailed(errors.New("spytifyd started but does not answer; see its log in Logs"))
	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateRecording}))
	m.Apply(event(t, proto.EventLevel, proto.LevelEvent{PeakDBFS: -12}))
	m.Apply(event(t, proto.EventTrackStarted, proto.TrackStartedEvent{Track: proto.Track{ID: "s-1", Artist: "Neon Harbor", Title: "Midnight Ferry", LengthMs: 200000}}))
	m.Apply(event(t, proto.EventTrackFinished, proto.TrackFinishedEvent{ID: "s-1", Path: "/x.flac", Timings: &proto.FinishTimings{Format: "flac", EncodeMs: 900, TotalMs: 1000}}))
	for i := 0; i < 3; i++ {
		m.Apply(event(t, proto.EventTrackSkipped, proto.TrackSkippedEvent{ID: "s-2", Reason: proto.SkipIncomplete}))
	}
	m.Apply(event(t, proto.EventError, proto.ErrorEvent{Code: "capture_stalled", Msg: "No audio"}))
	a.showTab(tabLogs)
	a.render()
	if a.win.Canvas().Overlays().Top() != nil {
		t.Error("a dialog asks about the crash although crash reports are set to never")
	}
	if n := quiet(a, srv); n != 0 {
		t.Fatalf("%d requests reached Sentry with everything off", n)
	}
	if left := telemetry.FindCrashes(a.opts.StateDir, a.fa.Preferences(), time.Now()); len(left) != 0 {
		t.Error("the crash is still waiting to be offered")
	}
}

// A crash is offered with what would leave; Don't send sends nothing, and
// Send sends it with the message.
func TestACrashLeavesOnlyOnSend(t *testing.T) {
	srv := fakesentry.New(t)
	a, m := privacyApp(t, srv, false, "ask")
	connect(t, a, m, false, "ask")
	leaveACrash(t, a, "first")
	a.checkCrashes()
	tapButton(t, overlay(t, a), "Don't send")
	if n := quiet(a, srv); n != 0 {
		t.Fatalf("%d requests after Don't send", n)
	}

	leaveACrash(t, a, "second")
	a.checkCrashes()
	d := overlay(t, a)
	snapshot(t, a, "privacy-crash")
	if !contains(texts(d), "The recorder stopped unexpectedly") {
		t.Errorf("dialog texts %v", texts(d))
	}
	tapButton(t, d, "Send")
	ev := srv.Wait(t, "event", 1)[0].JSON()
	if ex := ev["exception"].([]any)[0].(map[string]any); ex["value"] != "second" {
		t.Errorf("sent %v", ex)
	}
	if a.win.Canvas().Overlays().Top() != nil {
		t.Error("the dialog stayed")
	}
}

// Don't ask again with Send makes crash reports leave without asking.
func TestDontAskAgainRemembersTheAnswer(t *testing.T) {
	srv := fakesentry.New(t)
	a, m := privacyApp(t, srv, false, "ask")
	connect(t, a, m, false, "ask")
	leaveACrash(t, a, "boom")
	a.checkCrashes()
	d := overlay(t, a)
	again := checkBoxes(d, "Don't ask again")
	if len(again) != 1 {
		t.Fatal("no Don't ask again box")
	}
	again[0].SetChecked(true)
	tapButton(t, d, "Don't send")
	if got := a.privacy(crashesKey); got != "never" {
		t.Errorf("crash reports %v after Don't ask again and Don't send", got)
	}
}

// The diagnostics offer to send the checks only when one fails, and send
// them on the click.
func TestSendToTheDevelopers(t *testing.T) {
	srv := fakesentry.New(t)
	a, m := privacyApp(t, srv, false, "ask")
	connect(t, a, m, false, "ask")
	a.showTab(tabLogs)
	a.render()
	if b := buttons(a.win.Content(), "Send to the developers"); len(b) != 0 {
		t.Fatal("a Send button while every check passes")
	}
	in := info()
	in.Checks = append(in.Checks, proto.Check{Name: "ffmpeg found", Detail: "not on PATH"})
	m.SetInfo(in)
	a.render()
	snapshot(t, a, "privacy-diagnostics")
	tapButton(t, a.win.Content(), "Send to the developers")
	snapshot(t, a, "privacy-send-checks")
	tapButton(t, overlay(t, a), "Send")
	ev := srv.Wait(t, "event", 1)[0].JSON()
	if ev["message"] != "stuck: ffmpeg found" {
		t.Errorf("sent %v", ev["message"])
	}
}

// The first-run notice offers anonymous stats, unticked; ticking it turns
// them on.
func TestFirstRunOffersAnonymousStatsUnticked(t *testing.T) {
	srv := fakesentry.New(t)
	a, _ := privacyApp(t, srv, false, "ask")
	a.fa.Preferences().SetBool(firstRunKey, false)
	a.fa.Preferences().SetBool(privacySeenKey, false)
	closed := false
	a.maybeShowFirstRun(func() { closed = true })
	d := overlay(t, a)
	snapshot(t, a, "privacy-first-run")
	box := checkBoxes(d, statsLabel)
	if len(box) != 1 {
		t.Fatalf("the notice has %d stats boxes", len(box))
	}
	if box[0].Checked {
		t.Fatal("anonymous stats are ticked by default")
	}
	box[0].SetChecked(true)
	tapButton(t, d, "OK")
	if !closed || !a.tel.Stats() {
		t.Fatalf("closed %v, stats on %v", closed, a.tel.Stats())
	}
	if !a.fa.Preferences().Bool(privacySeenKey) {
		t.Error("the choice would be offered again")
	}
}

// Anonymous stats follow the setting, and with them on, the recorder's
// problems leave with their breadcrumbs, a full disk excepted.
func TestStatsFollowTheSetting(t *testing.T) {
	srv := fakesentry.New(t)
	a, m := privacyApp(t, srv, false, "ask")
	connect(t, a, m, false, "ask")
	if a.tel.Stats() {
		t.Fatal("stats on with the setting off")
	}
	a.showTab(tabSettings)
	a.render()
	set := a.screens[tabSettings].(*settingsScreen)
	set.search.SetText("privacy")
	if all := texts(set.root); !contains(all, statsLabel) || !contains(all, "Crash reports") {
		t.Errorf("the Privacy section shows %v", all)
	}
	snapshot(t, a, "privacy-settings")
	set.search.SetText("")
	m.Apply(event(t, proto.EventSettingsChanged, proto.SettingsValues{Values: map[string]any{statsKey: true}}))
	a.render()
	if !a.tel.Stats() {
		t.Fatal("stats off with the setting on")
	}
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
	if !strings.Contains(strings.Join(trail, " "), "recording") {
		t.Errorf("breadcrumbs %v", trail)
	}
	m.Apply(event(t, proto.EventSettingsChanged, proto.SettingsValues{Values: map[string]any{statsKey: false}}))
	a.render()
	if a.tel.Stats() {
		t.Fatal("stats still on")
	}
}
