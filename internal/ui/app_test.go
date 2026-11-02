package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2/test"

	"github.com/spytify/spytify/internal/bus"
	"github.com/spytify/spytify/internal/ui/lang"
	"github.com/spytify/spytify/internal/ui/link"
	"github.com/spytify/spytify/internal/ui/model"
	"github.com/spytify/spytify/pkg/proto"
)

func TestOfflineScreen(t *testing.T) {
	a, m, _ := newTestApp(t, "/tmp/spytify.sock")
	m.SetConn(model.Disconnected, "spytifyd is not running")
	a.render()
	all := texts(a.win.Content())
	for _, want := range []string{lang.RecorderNotRunning, "spytifyd is not running", lang.StartRecorder, "/tmp/spytify.sock", lang.PlayerProcess} {
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
	for _, unwanted := range []string{"File name template", lang.PlayerProcess, "Embed album cover", "output.dir", "Log level", "Language"} {
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
	if all := texts(a.win.Content()); !contains(all, lang.PlayerProcess) || contains(all, lang.RestartRecorder) {
		t.Errorf("search for player: %v", all)
	}
	s.search.SetText("")

	a.showTab(tabLogs)
	a.render()
	all = texts(a.win.Content())
	for _, want := range []string{lang.Diagnostics, "already recorded", "session 20261023-120000 started"} {
		if !contains(all, want) {
			t.Errorf("%q not on the logs screen", want)
		}
	}
	tapButton(t, a.win.Content(), lang.Diagnostics)
	if d := texts(overlay(t, a)); !contains(d, fmt.Sprintf(lang.ChecksPass, 5)) || !contains(d, "ffmpeg found") {
		t.Errorf("the diagnostics: %v", d)
	}
	snapshot(t, a, "logs-diagnostics")
	tapButton(t, overlay(t, a), lang.Close)

	a.showTab(tabAbout)
	a.render()
	all = texts(a.win.Content())
	for _, want := range []string{fmt.Sprintf(lang.Versions, proto.Version, fmt.Sprintf(lang.RecorderOn, "v2.0.0-alpha", "darwin/arm64"), proto.Protocol), lang.SourceCode, lang.ProjectPage, lang.CheckForUpdates, lang.DonateHeart} {
		if !contains(all, want) {
			t.Errorf("%q not on the about screen", want)
		}
	}
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
	eventually(t, "UI did not connect", func() bool { return m.Snapshot().Conn == model.Connected })
	a.render()

	test.Tap(a.action)
	waitCall(t, core, proto.MethodSessionStart)

	a.showTab(tabSettings)
	a.render()
	s := a.screens[tabSettings].(*settingsScreen)
	sw, ok := s.fields["output.underscores"].input.(*toggle)
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

	// A choice shows its words and applies its value.
	sel := s.fields["output.prefix"].input.(*selectField)
	sel.SetSelected("Album track number (04)")
	waitSets(t, core, 3)
	core.mu.Lock()
	last = core.sets[len(core.sets)-1]
	core.mu.Unlock()
	if last["output.prefix"] != "album_track" {
		t.Fatalf("choosing Album track number sent %v", last)
	}

	// Quitting the app stops the recorder too.
	a.stopDaemon()
	waitCall(t, core, proto.MethodCoreShutdown)
}

// Fyne 2.8.2 can skip its stop hook at quit, hang or panic
// (fyne-io/fyne#6634): the app runs it, and stops the recorder, once the
// loop has returned.
func TestQuittingStopsTheRecorderOnceTheLoopHasReturned(t *testing.T) {
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

	fa := test.NewTempApp(t)
	fa.Preferences().SetBool(firstRunKey, true)
	hook := fa.Lifecycle().(fyneStopHook)
	saved := 0
	hook.SetOnStoppedHookExecuted(func() { saved++ })
	m := model.New()
	l := link.New(sock, m)
	a := New(fa, l, m, Options{StateDir: t.TempDir(), Socket: sock, ManualRender: true, NoUpdateCheck: true})
	t.Cleanup(a.calls.Wait)
	if hook.OnStopped() != nil {
		t.Fatal("Fyne is left a stop hook to run at quit")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go l.Run(ctx)
	eventually(t, "UI did not connect", func() bool { return m.Snapshot().Conn == model.Connected })

	// The test driver's loop returns at once, as Fyne's does at quit.
	a.ShowAndRun()
	waitCall(t, core, proto.MethodCoreShutdown)
	if saved != 1 {
		t.Errorf("Fyne's stop hook ran %d times, want once", saved)
	}
}

func TestQuitWhileRecordingAsksFirst(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	feedSession(t, m)
	a.render()
	a.quit()
	top := a.win.Canvas().Overlays().Top()
	if top == nil || !contains(texts(top), lang.Quit) {
		t.Fatal("quitting during a recording did not ask")
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
	if !contains(all, lang.StartingRecorder) || contains(all, lang.StartRecorder) || contains(all, lang.RecorderNotRunning) {
		t.Fatalf("starting: %v", all)
	}
	if a.center.Objects[0] != a.tabsView || a.tabs.SelectedIndex() != tabRecord {
		t.Fatal("the Record tab is not shown while the recorder starts")
	}
	for i := range a.tabs.Items {
		a.showTab(i)
		a.render()
	}
	a.showTab(tabRecord)
	a.startFailed(link.ErrInArchive)
	all = texts(a.win.Content())
	for _, want := range []string{lang.RecorderNotRunning, link.ErrInArchive.Error(), lang.StartRecorder} {
		if !contains(all, want) {
			t.Errorf("%q not shown after a failed start: %v", want, all)
		}
	}
	m.SetConn(model.Connected, "")
	m.SetInfo(info())
	a.render()
	if contains(texts(a.win.Content()), lang.StartingRecorder) {
		t.Error("still starting once connected")
	}
}
