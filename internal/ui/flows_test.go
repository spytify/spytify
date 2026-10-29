package ui

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/spytify/spytify/internal/bus"
	"github.com/spytify/spytify/internal/ui/link"
	"github.com/spytify/spytify/internal/ui/model"
	"github.com/spytify/spytify/pkg/proto"
)

// The user flows QA walked through on macOS, with the operating system's
// parts (Finder, the media player, the menu bar) replaced by fakes.

// connectedApp is the app talking to a recordingCore over a real socket.
func connectedApp(t *testing.T) (*App, *model.Model, *recordingCore) {
	t.Helper()
	// A short folder: Unix socket paths have a length limit.
	dir, err := os.MkdirTemp("", "spyui")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	core := &recordingCore{}
	srv := bus.NewServer(core.handle, nil)
	sock := filepath.Join(dir, "s.sock")
	if err := srv.ListenUnix(sock); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })
	// Cancelled once newTestApp waited for the app's requests: they end
	// with the core's answer, not with the link going away.
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	a, m, l := newTestApp(t, sock)
	go l.Run(ctx)
	deadline := time.Now().Add(5 * time.Second)
	for m.Snapshot().Conn != model.Connected {
		if time.Now().After(deadline) {
			t.Fatal("the app did not connect")
		}
		time.Sleep(20 * time.Millisecond)
	}
	a.render()
	return a, m, core
}

func waitStops(t *testing.T, c *recordingCore, n int) []proto.SessionStopParams {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		c.mu.Lock()
		stops := append([]proto.SessionStopParams(nil), c.stops...)
		c.mu.Unlock()
		if len(stops) >= n {
			return stops
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d session.stop calls, want %d", len(stops), n)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Stop lets the track playing finish; pressed again, as Stop now, it ends
// at once and the track is discarded (the core's side is
// TestGracefulStopFinishesCurrentTrack and TestStopNowDiscardsCurrentTrack).
func TestStopThenStopNow(t *testing.T) {
	a, m, core := connectedApp(t)
	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateRecording}))
	a.render()
	if a.action.Text != "Stop" {
		t.Fatalf("while recording the button reads %q", a.action.Text)
	}
	test.Tap(a.action)
	if stops := waitStops(t, core, 1); stops[0].Now {
		t.Fatal("the first Stop ended the session at once")
	}
	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateStopping}))
	a.render()
	if a.action.Text != "Stop now" || a.action.Disabled() {
		t.Fatalf("while stopping the button reads %q (disabled %v)", a.action.Text, a.action.Disabled())
	}
	test.Tap(a.action)
	if stops := waitStops(t, core, 2); !stops[1].Now {
		t.Fatal("Stop now did not end the session at once")
	}
}

// recorded sets up a session whose one track was saved, with a cover, to a
// file in the output folder.
func recorded(t *testing.T) (a *App, m *model.Model, file, coverURL string) {
	t.Helper()
	a, m, _ = newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	out := t.TempDir()
	file = filepath.Join(out, "M83 - Midnight City.flac")
	if err := os.WriteFile(file, []byte("fLaC"), 0o644); err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for i := range img.Pix {
		img.Pix[i] = 0x80
	}
	img.Set(0, 0, color.RGBA{R: 0xff, A: 0xff})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	cover := filepath.Join(t.TempDir(), "cover.png")
	if err := os.WriteFile(cover, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	coverURL = "file://" + filepath.ToSlash(cover)
	values := defaults()
	values["output.dir"] = out
	m.SetConn(model.Connected, "")
	m.SetInfo(info())
	m.SetSettings(values)
	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateRecording}))
	m.Apply(event(t, proto.EventTrackStarted, proto.TrackStartedEvent{Track: proto.Track{
		ID: "s-001", Artist: "M83", Title: "Midnight City", Album: "Hurry Up, We're Dreaming", LengthMs: 243000, CoverURL: coverURL}}))
	m.Apply(event(t, proto.EventTrackFinished, proto.TrackFinishedEvent{ID: "s-001", Path: file, DurationMs: 243000, Bytes: 4}))
	a.render()
	// The cover loads on its own goroutine; draw again once it is in.
	deadline := time.Now().Add(5 * time.Second)
	for a.covers.loads() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the cover never loaded")
		}
		time.Sleep(20 * time.Millisecond)
	}
	a.render()
	return a, m, file, coverURL
}

// A track recorded to its end goes to the session's history with its
// cover, and to the Library.
func TestASavedTrackGoesToTheHistoryWithItsCoverAndToTheLibrary(t *testing.T) {
	a, _, file, coverURL := recorded(t)
	content := a.win.Content()
	if all := texts(content); !contains(all, "Midnight City") || !contains(all, "Saved") {
		t.Fatalf("history: %v", all)
	}
	if row := a.covers.get(coverURL).row; row == nil || !contains(images(content), picture(row)) {
		t.Errorf("the history row does not show the cover: %v", images(content))
	}
	a.showTab(tabLibrary)
	a.render()
	lib := a.screens[tabLibrary].(*libraryScreen)
	if len(lib.shown) != 1 || lib.shown[0].path != file {
		t.Fatalf("library: %+v", lib.shown)
	}
}

// Clear list empties the session's history; the Library keeps every file.
func TestClearListEmptiesTheHistoryButNotTheLibrary(t *testing.T) {
	a, m, file, _ := recorded(t)
	clear := buttons(a.win.Content(), "Clear list")
	if len(clear) != 1 {
		t.Fatalf("%d Clear list buttons", len(clear))
	}
	test.Tap(clear[0])
	a.render()
	if rows := m.Snapshot().Rows; len(rows) != 0 {
		t.Fatalf("%d rows after Clear list", len(rows))
	}
	if contains(texts(a.win.Content()), "Midnight City") {
		t.Error("the cleared track is still listed")
	}
	a.showTab(tabLibrary)
	a.render()
	lib := a.screens[tabLibrary].(*libraryScreen)
	if len(lib.shown) != 1 || lib.shown[0].path != file {
		t.Fatalf("library after Clear list: %+v", lib.shown)
	}
}

// The magnifier shows the file in Finder or Explorer, the play button opens
// it in the media player; both are faked here.
func TestRowButtonsRevealAndPlayTheFile(t *testing.T) {
	var revealed, played []string
	defer func(r, o func(string) error) { reveal, openPath = r, o }(reveal, openPath)
	reveal = func(p string) error { revealed = append(revealed, p); return nil }
	openPath = func(p string) error { played = append(played, p); return nil }

	a, _, file, _ := recorded(t)
	for _, tab := range []int{tabRecord, tabLibrary} {
		a.showTab(tab)
		a.render()
		content := a.win.Content()
		search, play := iconButtons(content, theme.SearchIcon()), iconButtons(content, theme.MediaPlayIcon())
		if len(search) != 1 || len(play) != 1 {
			t.Fatalf("tab %d: %d magnifiers and %d play buttons", tab, len(search), len(play))
		}
		test.Tap(search[0])
		test.Tap(play[0])
	}
	if len(revealed) != 2 || revealed[0] != file || revealed[1] != file {
		t.Errorf("revealed %v, want %s twice", revealed, file)
	}
	if len(played) != 2 || played[0] != file || played[1] != file {
		t.Errorf("played %v, want %s twice", played, file)
	}
}

// trayApp is a test app with a system tray (the menu bar on macOS).
type trayApp struct {
	fyne.App
	menu *fyne.Menu
}

func (t *trayApp) SetSystemTrayMenu(m *fyne.Menu)    { t.menu = m }
func (t *trayApp) SetSystemTrayIcon(fyne.Resource)   {}
func (t *trayApp) SetSystemTrayWindow(w fyne.Window) {}

// With Minimize to tray on, closing the window keeps Spytify in the tray,
// whose menu brings the window back or records; quitting from the tray is
// the toolkit's Quit item, which ends where every quit does (the recorder
// is stopped: see TestButtonsSendActions). With it off, closing quits:
// TestClosingTheWindowQuitsUnlessMinimizedToTray.
func TestClosingToTheTrayKeepsSpytifyThere(t *testing.T) {
	tray := &trayApp{App: test.NewTempApp(t)}
	tray.Preferences().SetBool(firstRunKey, true)
	m := model.New()
	l := link.New(filepath.Join(t.TempDir(), "absent.sock"), m)
	a := New(tray, l, m, Options{StateDir: t.TempDir(), ManualRender: true, NoUpdateCheck: true})
	m.SetConn(model.Connected, "")
	m.SetInfo(info())
	values := defaults()
	values["app.minimize_to_tray"] = true
	m.SetSettings(values)
	a.render()
	quits := 0
	a.quitApp = func() { quits++ }
	if !a.tray || tray.menu == nil {
		t.Fatal("no tray menu was set")
	}
	a.closeWindow()
	if quits != 0 {
		t.Fatal("closing quit with Minimize to tray on")
	}
	var labels []string
	var open *fyne.MenuItem
	for _, it := range tray.menu.Items {
		labels = append(labels, it.Label)
		if it.Label == "Open window" {
			open = it
		}
	}
	if len(labels) != 3 || labels[0] != "Record" || labels[2] != "Open window" {
		t.Errorf("tray menu %v, want Record, a separator and Open window", labels)
	}
	// The tray's action follows the session, as the app bar's does.
	action := tray.menu.Items[0]
	for _, step := range []struct {
		state proto.SessionState
		want  string
	}{{proto.StateIdle, "Record"}, {proto.StateRecording, "Stop"}, {proto.StateStopping, "Stop now"}, {proto.StateIdle, "Record"}} {
		m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: step.state}))
		a.render()
		if action.Label != step.want || action.Disabled || action.Label != a.action.Text {
			t.Errorf("%s: tray action %q (disabled %v), app bar %q; want %q", step.state, action.Label, action.Disabled, a.action.Text, step.want)
		}
	}
	m.SetConn(model.Disconnected, "")
	a.render()
	if !action.Disabled {
		t.Error("the tray's action is enabled with no recorder")
	}
	if open == nil {
		t.Fatal("no way back to the window from the tray")
	}
	open.Action()
}

// With Minimize to tray off, the default, Spytify puts nothing in the tray;
// turned on, its icon and menu come at once.
func TestTheTrayIconComesWithMinimizeToTray(t *testing.T) {
	tray := &trayApp{App: test.NewTempApp(t)}
	tray.Preferences().SetBool(firstRunKey, true)
	m := model.New()
	l := link.New(filepath.Join(t.TempDir(), "absent.sock"), m)
	a := New(tray, l, m, Options{StateDir: t.TempDir(), ManualRender: true, NoUpdateCheck: true})
	m.SetConn(model.Connected, "")
	m.SetInfo(info())
	values := defaults()
	m.SetSettings(values)
	a.render()
	if tray.menu != nil {
		t.Fatal("a tray menu with Minimize to tray off")
	}
	values["app.minimize_to_tray"] = true
	m.SetSettings(values)
	a.render()
	if tray.menu == nil || tray.menu.Items[0].Label != "Record" {
		t.Fatalf("tray menu %v once Minimize to tray is on", tray.menu)
	}
}

// On a Linux desktop that draws no tray icons, closing the window quits
// even with Minimize to tray on, and Settings says why; a tray host coming
// (the AppIndicator extension turned on) brings the icon, and closing
// hides the window there again until the host goes.
func TestClosingQuitsWhereTheDesktopDrawsNoTray(t *testing.T) {
	tray := &trayApp{App: test.NewTempApp(t)}
	tray.Preferences().SetBool(firstRunKey, true)
	m := model.New()
	l := link.New(filepath.Join(t.TempDir(), "absent.sock"), m)
	a := New(tray, l, m, Options{StateDir: t.TempDir(), ManualRender: true, NoUpdateCheck: true})
	a.setTrayHost(false)
	m.SetConn(model.Connected, "")
	m.SetInfo(info())
	values := defaults()
	values["app.minimize_to_tray"] = true
	m.SetSettings(values)
	a.showTab(tabSettings)
	a.render()
	s := a.screens[tabSettings].(*settingsScreen)
	quits := 0
	a.quitApp = func() { quits++ }
	closing := func(host string, want int) {
		t.Helper()
		before := quits
		a.closeWindow()
		if quits-before != want {
			t.Errorf("%s: closing quit %d times, want %d", host, quits-before, want)
		}
	}
	if tray.menu != nil {
		t.Error("a tray menu with nothing to draw it")
	}
	if !contains(texts(s.sections), noTrayNote) {
		t.Error("Minimize to tray does not say the desktop draws no tray icons")
	}
	a.win.Resize(fyne.NewSize(920, 620))
	snapshot(t, a, "settings-no-tray")
	closing("no host", 1)

	a.setTrayHost(true)
	if tray.menu == nil {
		t.Fatal("no tray menu once a host came")
	}
	if contains(texts(s.sections), noTrayNote) {
		t.Error("Minimize to tray still says there is no tray")
	}
	closing("a host", 0)

	a.setTrayHost(false)
	closing("the host gone", 1)
}

// On at launch, Minimize to tray sets its tray up before the toolkit's
// loop runs, the only time Linux starts one. Turned on while the app runs,
// the icon shows at once on macOS and Windows; on Linux, it comes when
// Spytify restarts, closing quits until then, and Settings says so.
func TestTheTrayIconComesAtLaunchOrOnLinuxAtTheNext(t *testing.T) {
	tray := &trayApp{App: test.NewTempApp(t)}
	tray.Preferences().SetBool(firstRunKey, true)
	m := model.New()
	l := link.New(filepath.Join(t.TempDir(), "absent.sock"), m)
	a := New(tray, l, m, Options{StateDir: t.TempDir(), ManualRender: true, NoUpdateCheck: true, MinimizeToTray: true})
	if tray.menu == nil || !a.hasTray() {
		t.Fatal("no tray at launch with Minimize to tray on")
	}

	tray = &trayApp{App: test.NewTempApp(t)}
	tray.Preferences().SetBool(firstRunKey, true)
	m = model.New()
	l = link.New(filepath.Join(t.TempDir(), "absent.sock"), m)
	a = New(tray, l, m, Options{StateDir: t.TempDir(), ManualRender: true, NoUpdateCheck: true})
	a.running = true
	m.SetConn(model.Connected, "")
	m.SetInfo(info())
	values := defaults()
	values["app.minimize_to_tray"] = true
	m.SetSettings(values)
	a.showTab(tabSettings)
	a.render()
	quits := 0
	a.quitApp = func() { quits++ }
	a.closeWindow()
	s := a.screens[tabSettings].(*settingsScreen)
	noted := contains(texts(s.sections), lateTrayNote)
	if lateTrayShows {
		if tray.menu == nil || quits != 0 || noted {
			t.Errorf("turned on: tray menu %v, closing quit %d times, restart noted %v; want a menu, no quit, no note", tray.menu != nil, quits, noted)
		}
		return
	}
	if quits != 1 || !noted {
		t.Errorf("turned on, on Linux: closing quit %d times, restart noted %v; want once, noted", quits, noted)
	}
	values["app.minimize_to_tray"] = false
	m.SetSettings(values)
	a.render()
	if contains(texts(s.sections), lateTrayNote) {
		t.Error("the restart is noted with Minimize to tray off")
	}
}

// iconButtons are the visible buttons that show only icon.
func iconButtons(o fyne.CanvasObject, icon fyne.Resource) []*widget.Button {
	var out []*widget.Button
	var walk func(fyne.CanvasObject)
	walk = func(o fyne.CanvasObject) {
		if o == nil || !o.Visible() {
			return
		}
		if b, ok := o.(*widget.Button); ok {
			if b.Text == "" && b.Icon != nil && b.Icon.Name() == icon.Name() && !b.Disabled() {
				out = append(out, b)
			}
			return
		}
		if c, ok := o.(*fyne.Container); ok {
			for _, child := range c.Objects {
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

// images are the names of the visible images' resources.
func images(o fyne.CanvasObject) []string {
	var out []string
	var walk func(fyne.CanvasObject)
	walk = func(o fyne.CanvasObject) {
		if o == nil || !o.Visible() {
			return
		}
		switch w := o.(type) {
		case *canvas.Image:
			if w.Resource != nil {
				out = append(out, w.Resource.Name())
			} else if w.Image != nil {
				out = append(out, picture(w.Image))
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

// picture names an image shown from its pixels, such as a cover's thumbnail.
func picture(img image.Image) string { return fmt.Sprintf("picture %p", img) }
