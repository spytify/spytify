package ui

import (
	"errors"
	"fmt"
	"image/color"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	ttwidget "github.com/dweymouth/fyne-tooltip/widget"

	"github.com/spytify/spytify/internal/settings"
	"github.com/spytify/spytify/internal/telemetry"
	"github.com/spytify/spytify/internal/telemetry/fakesentry"
	"github.com/spytify/spytify/internal/ui/model"
	"github.com/spytify/spytify/pkg/proto"
)

// The keys go where Fyne 2.8.2's window sends them
// (internal/driver/glfw/window.go): Tab and Shift+Tab move the focus, unless
// the field focused types tabs; every other key goes to the control
// focused.

func pressTab(c fyne.Canvas) {
	if !typesTab(c) {
		c.FocusNext()
	}
}

func pressShiftTab(c fyne.Canvas) {
	if !typesTab(c) {
		c.FocusPrevious()
	}
}

// typesTab hands Tab to the field focused if it types tabs.
func typesTab(c fyne.Canvas) bool {
	f, ok := c.Focused().(fyne.Tabbable)
	if ok && f.AcceptsTab() {
		c.Focused().TypedKey(&fyne.KeyEvent{Name: fyne.KeyTab})
	}
	return ok && f.AcceptsTab()
}

func pressKey(c fyne.Canvas, key fyne.KeyName) {
	if f := c.Focused(); f != nil {
		f.TypedKey(&fyne.KeyEvent{Name: key})
	}
}

// pressSpace is the space bar: the key, then the character it types, which
// goes to whatever has the focus by then.
func pressSpace(c fyne.Canvas) {
	pressKey(c, fyne.KeySpace)
	if f := c.Focused(); f != nil {
		f.TypedRune(' ')
	}
}

// focusName names a control as the test reads it: its text, its tooltip,
// what its icon does, or the setting it changes.
func focusName(a *App, f fyne.Focusable) string {
	setting := func(o fyne.CanvasObject) string {
		for key, fl := range a.screens[tabSettings].(*settingsScreen).fields {
			if fl.input == o || inContainers(fl.input, o) {
				return key
			}
		}
		return ""
	}
	icon := func(b *widget.Button) string {
		if b.Text != "" || b.Icon == nil {
			return b.Text
		}
		switch b.Icon.Name() {
		case theme.SearchIcon().Name():
			return "Show in folder"
		case theme.MediaPlayIcon().Name():
			return "Play"
		case theme.ViewRefreshIcon().Name():
			return "Rescan"
		case theme.FolderOpenIcon().Name():
			return "Choose folder"
		}
		return b.Icon.Name()
	}
	switch w := f.(type) {
	case *ttwidget.Button:
		if w.Text == "" {
			return w.ToolTip()
		}
		return w.Text
	case *tipButton:
		if w.Text == "" {
			return w.ToolTip()
		}
		return w.Text
	case *widget.Button:
		return icon(w)
	case *button:
		return icon(&w.Button)
	case *litButton:
		return icon(&w.Button)
	case *toggle:
		return setting(w)
	case *widget.Select:
		if key := setting(w); key != "" {
			return key
		}
		return "Sort"
	case *checkSelect:
		return "Filter"
	case *widget.Check:
		return w.Text
	case *widget.Hyperlink:
		return w.Text
	case *widget.List:
		return "list"
	}
	if key := setting(f.(fyne.CanvasObject)); key != "" {
		return key
	}
	// An entry, Fyne's or one of the app's built on it, by its placeholder.
	if v := reflect.ValueOf(f).Elem(); v.Kind() == reflect.Struct {
		if p := v.FieldByName("PlaceHolder"); p.IsValid() && p.Kind() == reflect.String {
			return p.String()
		}
	}
	return fmt.Sprintf("%T", f)
}

// tabOrder is the controls Tab goes through, from nothing back round to the
// first.
func tabOrder(t *testing.T, a *App) []string {
	t.Helper()
	c := a.win.Canvas()
	c.Unfocus()
	var names []string
	var first fyne.Focusable
	for range 200 {
		pressTab(c)
		f := c.Focused()
		if f == nil || f == first {
			break
		}
		if first == nil {
			first = f
		}
		names = append(names, focusName(a, f))
	}
	c.Unfocus()
	return names
}

func wantOrder(t *testing.T, a *App, want ...string) {
	t.Helper()
	if got := tabOrder(t, a); !slices.Equal(got, want) {
		t.Fatalf("Tab goes through\n  %s\nwant\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

// tabTo presses Tab until the control named name has the focus, the
// number-th of that name.
func tabTo(t *testing.T, a *App, name string, number int) fyne.Focusable {
	t.Helper()
	c := a.win.Canvas()
	c.Unfocus()
	seen := 0
	for range 200 {
		pressTab(c)
		f := c.Focused()
		if f == nil {
			break
		}
		if focusName(a, f) == name {
			if seen++; seen == number {
				return f
			}
		}
	}
	t.Fatalf("Tab never reaches %q number %d", name, number)
	return nil
}

// urlApp keeps the pages the app asked the browser to open.
type urlApp struct {
	fyne.App
	opened []string
}

func (u *urlApp) OpenURL(link *url.URL) error {
	u.opened = append(u.opened, link.String())
	return nil
}

// fakeOS replaces the file manager and the media player for the test.
func fakeOS(t *testing.T) (revealed, opened *[]string) {
	t.Helper()
	r, o := reveal, openPath
	t.Cleanup(func() { reveal, openPath = r, o })
	revealed, opened = &[]string{}, &[]string{}
	reveal = func(p string) error { *revealed = append(*revealed, p); return nil }
	openPath = func(p string) error { *opened = append(*opened, p); return nil }
	return revealed, opened
}

// Record: the app bar, then the track being recorded, its list and the
// list's buttons, top to bottom; each does from the keyboard what a click
// does.
func TestTheKeyboardWorksTheRecordScreen(t *testing.T) {
	revealed, opened := fakeOS(t)
	a, m, core := connectedApp(t)
	c := a.win.Canvas()
	wantOrder(t, a, "Donate", "Record")
	tabTo(t, a, "Record", 1)
	pressSpace(c)
	waitCall(t, core, proto.MethodSessionStart)

	// With the player closed, Record is off and the screen offers to open it.
	m.Apply(event(t, proto.EventTargetState, proto.TargetStateEvent{Running: false}))
	a.render()
	wantOrder(t, a, "Donate", "Open Player")
	tabTo(t, a, "Open Player", 1)
	pressSpace(c)
	waitCall(t, core, proto.MethodPlayerLaunch)

	// A session: two files saved, one skipped, whose buttons are off.
	feedSession(t, m)
	a.render()
	wantOrder(t, a, "Donate", "Stop", "Skip", "list", "Show in folder", "Play", "Show in folder", "Play", "Clear list", "Open folder")
	tabTo(t, a, "Skip", 1)
	pressSpace(c)
	waitCall(t, core, proto.MethodPlayerControl)
	for n := range 2 {
		tabTo(t, a, "Show in folder", n+1)
		pressSpace(c)
		tabTo(t, a, "Play", n+1)
		pressSpace(c)
	}
	saved := []string{"/Users/listener/Music/Spytify/M83/Intro.flac", "/Users/listener/Music/Spytify/M83/Reunion.flac"}
	slices.Sort(*revealed)
	if !slices.Equal(*revealed, saved) || !slices.Equal(slices.Sorted(slices.Values(*opened)), saved) {
		t.Errorf("the rows' buttons showed %v and played %v, want %v", *revealed, *opened, saved)
	}
	tabTo(t, a, "Open folder", 1)
	pressSpace(c)
	if last := (*opened)[len(*opened)-1]; last != "/Users/listener/Music/Spytify" {
		t.Errorf("Open folder opened %s", last)
	}
	tabTo(t, a, "Stop", 1)
	pressSpace(c)
	waitStops(t, core, 1)
	tabTo(t, a, "Clear list", 1)
	pressSpace(c)
	a.render()
	if rows := m.Snapshot().Rows; len(rows) != 0 {
		t.Errorf("%d rows after Clear list", len(rows))
	}
}

// Library: the search, the sort, the filter and the rescan above the files,
// then each file's buttons.
func TestTheKeyboardWorksTheLibrary(t *testing.T) {
	revealed, opened := fakeOS(t)
	a, _, file, _ := recorded(t)
	a.showTab(tabLibrary)
	a.render()
	c := a.win.Canvas()
	lib := a.screens[tabLibrary].(*libraryScreen)
	wantOrder(t, a, "Donate", "Stop", "Search artist, title or album", "Sort", "Filter", "Rescan", "list", "Show in folder", "Play")

	test.Type(tabTo(t, a, "Search artist, title or album", 1), "nothing like it")
	if len(lib.shown) != 0 {
		t.Errorf("%d files match a search for nothing", len(lib.shown))
	}
	lib.search.SetText("")

	// Space opens the choices; the arrows and Enter choose, Escape keeps.
	tabTo(t, a, "Sort", 1)
	pressSpace(c)
	pressKey(c, fyne.KeyDown)
	pressKey(c, fyne.KeyDown)
	pressKey(c, fyne.KeyReturn)
	if lib.sortBy.Selected != "Name" {
		t.Errorf("sorted by %q, want Name", lib.sortBy.Selected)
	}
	tabTo(t, a, "Sort", 1)
	pressSpace(c)
	pressKey(c, fyne.KeyEscape)
	if c.Overlays().Top() != nil || lib.sortBy.Selected != "Name" {
		t.Errorf("Escape: choices still shown %v, sorted by %q", c.Overlays().Top() != nil, lib.sortBy.Selected)
	}

	// A file that came in meanwhile shows once rescanned.
	other := filepath.Join(filepath.Dir(file), "M83 - Outro.flac")
	if err := os.WriteFile(other, []byte("fLaC"), 0o644); err != nil {
		t.Fatal(err)
	}
	tabTo(t, a, "Rescan", 1)
	pressSpace(c)
	if len(lib.shown) != 2 {
		t.Fatalf("%d files after the rescan", len(lib.shown))
	}

	// Sorted by name, Midnight City comes first.
	tabTo(t, a, "Show in folder", 1)
	pressSpace(c)
	tabTo(t, a, "Play", 1)
	pressSpace(c)
	if !slices.Equal(*revealed, []string{file}) || !slices.Equal(*opened, []string{file}) {
		t.Errorf("showed %v and played %v, want %s", *revealed, *opened, file)
	}
}

// settingsOrder is the Settings screen's Tab order: each card's Reset, its
// settings in the table's order but those turned off, then its Advanced
// fold.
func settingsOrder(s *settingsScreen) []string {
	want := []string{"Donate", "Record", "Search settings"}
	for _, g := range groups() {
		var main []string
		advanced := g == "App"
		for _, d := range settings.Table {
			off, _ := s.fields[d.Key].input.(fyne.Disableable)
			switch {
			case d.Group != g:
			case d.Advanced:
				advanced = true
			case off != nil && off.Disabled():
			default:
				main = append(main, d.Key)
				if d.Kind == settings.KindPath {
					main = append(main, "Choose folder")
				}
			}
		}
		want = append(want, "Reset")
		want = append(want, main...)
		if advanced {
			want = append(want, "Advanced")
		}
	}
	return want
}

// Settings: the search, then each card from top to bottom. A switch flips
// with Space, a list of choices opens, Reset and Advanced work, and the
// screen scrolls to what Tab focused.
func TestTheKeyboardWorksSettings(t *testing.T) {
	a, _, core := connectedApp(t)
	a.showTab(tabSettings)
	a.render()
	c := a.win.Canvas()
	s := a.screens[tabSettings].(*settingsScreen)
	wantOrder(t, a, settingsOrder(s)...)

	sw := tabTo(t, a, "app.minimize_to_tray", 1)
	pressSpace(c)
	waitSets(t, core, 1)
	core.mu.Lock()
	sent := core.sets[0]
	core.mu.Unlock()
	if sent["app.minimize_to_tray"] != true || !sw.(*toggle).on {
		t.Errorf("Space on Minimize to tray sent %v", sent)
	}

	tabTo(t, a, "app.theme", 1)
	pressSpace(c)
	if c.Overlays().Top() == nil {
		t.Fatal("Space opened no choices")
	}
	pressKey(c, fyne.KeyEscape)
	if c.Overlays().Top() != nil {
		t.Fatal("Escape left the choices open")
	}

	tabTo(t, a, "Reset", 1)
	pressSpace(c)
	waitCall(t, core, proto.MethodSettingsReset)

	// The folder picker opens with the focus in it.
	tabTo(t, a, "Choose folder", 1)
	pressSpace(c)
	if picker := c.Overlays().Top(); picker == nil || c.Focused() == nil {
		t.Fatal("the folder picker opened without the focus")
	} else {
		c.Overlays().Remove(picker)
	}

	// The App card's Advanced fold holds the player: open, Tab goes in.
	tabTo(t, a, "Advanced", 1)
	pressSpace(c)
	if !s.advanced["App"].open {
		t.Fatal("Space did not open Advanced")
	}
	// The window lays the screen out again before it next draws.
	relayout(s.scroll)
	order := tabOrder(t, a)
	if i := slices.Index(order, "Advanced"); i < 0 || order[i+1] != "process name, or a comma list" {
		t.Errorf("after the open Advanced, Tab goes to %v", order[i+1:])
	}

	// The last setting is below the fold: Shift+Tab from nothing goes there
	// and the screen follows.
	c.Unfocus()
	a.followFocus()
	pressShiftTab(c)
	a.followFocus()
	last := c.Focused().(fyne.CanvasObject)
	if s.scroll.Offset.Y <= 0 || !inSight(a, s.scroll, last) {
		t.Fatalf("the last setting, %s, is out of sight at offset %v", focusName(a, c.Focused()), s.scroll.Offset)
	}
	first := tabTo(t, a, "Reset", 1).(fyne.CanvasObject)
	a.followFocus()
	if !inSight(a, s.scroll, first) {
		t.Fatalf("the first card's Reset is out of sight at offset %v", s.scroll.Offset)
	}
}

// Space presses a button and leaves the focus on it, for Tab to go on from
// there: Fyne's button gave the focus to nothing, so opening or closing an
// Advanced fold, or pressing Reset, sent the next Tab back to Donate.
func TestSpaceLeavesTheFocusOnTheButtonItPresses(t *testing.T) {
	a, _, core := connectedApp(t)
	a.showTab(tabSettings)
	a.render()
	c := a.win.Canvas()
	s := a.screens[tabSettings].(*settingsScreen)
	closed := settingsOrder(s)
	after := closed[slices.Index(closed, "Advanced")+1]

	adv := tabTo(t, a, "Advanced", 1)
	pressSpace(c)
	if !s.advanced["App"].open || c.Focused() != adv {
		t.Fatalf("Space on Advanced: open %v, the focus on %v", s.advanced["App"].open, c.Focused())
	}
	pressTab(c)
	if got := focusName(a, c.Focused()); got != "process name, or a comma list" {
		t.Errorf("Tab after opening Advanced went to %q, want the fold's first field", got)
	}
	pressShiftTab(c)
	pressSpace(c)
	if s.advanced["App"].open || c.Focused() != adv {
		t.Fatalf("Space on the open Advanced: open %v, the focus on %v", s.advanced["App"].open, c.Focused())
	}
	pressTab(c)
	if got := focusName(a, c.Focused()); got != after {
		t.Errorf("Tab after closing Advanced went to %q, want %q", got, after)
	}

	reset := tabTo(t, a, "Reset", 1)
	pressSpace(c)
	waitCall(t, core, proto.MethodSettingsReset)
	if c.Focused() != reset {
		t.Errorf("Space on Reset left the focus on %v", c.Focused())
	}
}

// inSight says whether o shows whole in the part of s on screen.
func inSight(a *App, s *container.Scroll, o fyne.CanvasObject) bool {
	d := a.fa.Driver()
	y := d.AbsolutePositionForObject(o).Y - d.AbsolutePositionForObject(s).Y
	return y >= 0 && y+o.Size().Height <= s.Size().Height
}

// A switch shows the focus with a ring around it, apart from its track, so
// that it shows on a switch that is on, whose track is the focus's green.
func TestTheSwitchShowsItsFocus(t *testing.T) {
	for _, mode := range []string{"light", "dark"} {
		t.Run(mode, func(t *testing.T) {
			a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
			values := defaults()
			values["app.theme"] = mode
			m.SetConn(model.Connected, "")
			m.SetInfo(info())
			m.SetSettings(values)
			a.showTab(tabSettings)
			a.render()
			c := a.win.Canvas()
			for _, key := range []string{"app.check_updates", "app.minimize_to_tray"} {
				sw := tabTo(t, a, key, 1).(*toggle)
				r := test.WidgetRenderer(sw).(*toggleRenderer)
				if r.ring.StrokeColor != theme.Color(theme.ColorNameFocus) || r.ring.StrokeWidth == 0 {
					t.Errorf("%s focused, on %v: ring %v", key, sw.on, r.ring.StrokeColor)
				}
				if key == "app.check_updates" {
					snapshot(t, a, "keyboard-switch-focus-"+mode)
				}
				c.Unfocus()
				if r.ring.StrokeColor != color.Transparent {
					t.Errorf("%s unfocused: ring %v", key, r.ring.StrokeColor)
				}
			}
		})
	}
}

// Logs: the diagnostics, then the log's level boxes and buttons, then the
// log.
func TestTheKeyboardWorksTheLogs(t *testing.T) {
	_, opened := fakeOS(t)
	a, m, core := connectedApp(t)
	m.Apply(event(t, proto.EventLog, proto.LogEvent{Level: "info", Msg: "session started", TS: "2026-10-31T12:00:00Z"}))
	a.showTab(tabLogs)
	a.render()
	c := a.win.Canvas()
	l := a.screens[tabLogs].(*logsScreen)
	wantOrder(t, a, "Donate", "Record", "error", "info", "debug", "Copy", "Open log file", "Diagnostics", "list")

	// Diagnostics opens its dialog with the focus on its first control.
	tabTo(t, a, "Diagnostics", 1)
	pressSpace(c)
	if got := focusName(a, c.Focused()); got != "Run the checks again" {
		t.Fatalf("the diagnostics open with %q focused", got)
	}
	core.mu.Lock()
	core.calls = nil
	core.mu.Unlock()
	pressSpace(c)
	waitCall(t, core, proto.MethodCoreInfo)
	for range 5 {
		if f := c.Focused(); f != nil && focusName(a, f) == "Close" {
			break
		}
		pressTab(c)
	}
	pressSpace(c)
	if c.Overlays().Top() != nil {
		t.Fatal("Close left the diagnostics open")
	}

	tabTo(t, a, "info", 1)
	pressSpace(c)
	if slices.Contains(l.level.Selected, "info") || len(l.lines) != 0 {
		t.Errorf("Space on info: shown %v, %d lines", l.level.Selected, len(l.lines))
	}
	pressSpace(c)
	tabTo(t, a, "Copy", 1)
	pressSpace(c)
	if got := a.fa.Clipboard().Content(); !strings.Contains(got, "session started") {
		t.Errorf("Copy put %q on the clipboard", got)
	}
	tabTo(t, a, "Open log file", 1)
	pressSpace(c)
	if !slices.Equal(*opened, []string{l.path}) {
		t.Errorf("Open log file opened %v", *opened)
	}
}

// About: the links, then the buttons, left to right.
func TestTheKeyboardWorksAbout(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	browser := &urlApp{App: a.fa}
	a.fa = browser
	m.SetConn(model.Connected, "")
	m.SetInfo(info())
	m.SetSettings(defaults())
	a.showTab(tabAbout)
	a.render()
	c := a.win.Canvas()
	wantOrder(t, a, "Donate", "Record", "Source code on GitHub", "Project page", "Check for updates", "♥ Donate")
	// Check for updates asks GitHub: not pressed here.
	for _, name := range []string{"Donate", "Source code on GitHub", "Project page", "♥ Donate"} {
		tabTo(t, a, name, 1)
		pressSpace(c)
	}
	if want := []string{donateURL, sourceURL, siteURL, donateURL}; !slices.Equal(browser.opened, want) {
		t.Errorf("opened %v, want %v", browser.opened, want)
	}
}

// Offline: the app bar, Start the recorder, then the player's fields; the
// screen scrolls to them in a short window.
func TestTheKeyboardWorksTheOfflineScreen(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	m.SetConn(model.Disconnected, "spytifyd is not running")
	a.render()
	order := tabOrder(t, a)
	if len(order) < 4 || !slices.Equal(order[:3], []string{"Donate", "Start the recorder", "process name, or a comma list"}) ||
		order[len(order)-1] != "command that starts the player (optional)" {
		t.Fatalf("Tab goes through %v", order)
	}
	// Start the recorder would start spytifyd: not pressed here.
	a.win.Resize(fyne.NewSize(920, 320))
	c := a.win.Canvas()
	c.Unfocus()
	pressShiftTab(c)
	a.followFocus()
	if launch := c.Focused().(fyne.CanvasObject); a.offline.Offset.Y <= 0 || !inSight(a, a.offline, launch) {
		t.Fatalf("the launch command is out of sight at offset %v", a.offline.Offset)
	}
}

// A dialog opens with the focus on its first control, Tab goes round its
// controls, and Space presses them.
func TestDialogsTakeTheFocus(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	feedSession(t, m)
	a.render()
	c := a.win.Canvas()
	quit := false
	a.quitApp = func() { quit = true }

	a.quit()
	if got := focusName(a, c.Focused()); got != "No" {
		t.Fatalf("Quit Spytify? opens with %q focused", got)
	}
	pressSpace(c)
	if quit || c.Overlays().Top() != nil {
		t.Fatalf("No: quit %v, dialog left %v", quit, c.Overlays().Top() != nil)
	}
	a.quit()
	pressTab(c)
	if got := focusName(a, c.Focused()); got != "Yes" {
		t.Fatalf("Tab from No went to %q", got)
	}
	pressSpace(c)
	if !quit {
		t.Fatal("Yes did not quit")
	}

	a.showError(errors.New("boom"))
	if got := focusName(a, c.Focused()); got != "OK" {
		t.Fatalf("an error opens with %q focused", got)
	}
	pressSpace(c)
	if c.Overlays().Top() != nil {
		t.Fatal("OK left the error shown")
	}
}

// The first-run notice opens on the bug tracker's box; Space unticks it, and
// the keyboard reaches OK.
func TestTheFirstRunNoticeTakesTheKeyboard(t *testing.T) {
	srv := fakesentry.New(t)
	a, _ := privacyApp(t, srv, true)
	a.shown = false
	a.fa.Preferences().SetBool(firstRunKey, false)
	a.fa.Preferences().SetBool(privacySeenKey, false)
	closed := false
	a.maybeShowFirstRun(func() { closed = true })
	c := a.win.Canvas()
	if got := focusName(a, c.Focused()); got != trackerLabel {
		t.Fatalf("the notice opens with %q focused", got)
	}
	pressSpace(c)
	pressTab(c)
	pressTab(c)
	if got := focusName(a, c.Focused()); got != "OK" {
		t.Fatalf("two Tabs on, %q is focused", got)
	}
	pressSpace(c)
	if !closed || a.privacy(trackerKey) != false {
		t.Fatalf("closed %v, bug tracker %v", closed, a.privacy(trackerKey))
	}
}

// The crash dialog opens on its fold, which Space opens, then its buttons.
func TestTheCrashDialogTakesTheKeyboard(t *testing.T) {
	srv := fakesentry.New(t)
	a, m := privacyApp(t, srv, false)
	connect(t, a, m, false)
	a.tel = telemetry.New(telemetry.Options{Prefs: a.fa.Preferences()})
	leaveACrash(t, a, "boom")
	a.checkCrashes()
	c := a.win.Canvas()
	d := overlay(t, a)
	if got := focusName(a, c.Focused()); got != "Error and stack trace" {
		t.Fatalf("the crash dialog opens with %q focused", got)
	}
	pressSpace(c)
	if !contains(texts(d), "panic: boom") {
		t.Error("Space did not unfold the trace")
	}
	wantOrder(t, a, "Error and stack trace", "Close", "Copy", "Report on GitHub")
	tabTo(t, a, "Close", 1)
	pressSpace(c)
	if c.Overlays().Top() != nil {
		t.Fatal("Close left the dialog shown")
	}
}

// Send to the developers: Tab gets out of the comment, which kept it to
// type a tab, and on to Send.
func TestTabLeavesTheComment(t *testing.T) {
	srv := fakesentry.New(t)
	a, m := privacyApp(t, srv, true)
	connect(t, a, m, true)
	failCheck(a, m)
	a.showTab(tabLogs)
	a.render()
	c := a.win.Canvas()
	tabTo(t, a, "Diagnostics", 1)
	pressSpace(c)
	tabTo(t, a, "Send to the developers", 1)
	pressSpace(c)
	if got := focusName(a, c.Focused()); got != "What will be sent" {
		t.Fatalf("the dialog opens with %q focused", got)
	}
	pressTab(c)
	comment := c.Focused()
	test.Type(comment, "stuck")
	pressTab(c)
	if got := focusName(a, c.Focused()); got != "Cancel" || strings.Contains(comment.(*commentEntry).Text, "\t") {
		t.Fatalf("Tab from the comment: %q focused, comment %q", got, comment.(*commentEntry).Text)
	}
	pressShiftTab(c)
	if c.Focused() != comment {
		t.Fatal("Shift+Tab does not come back to the comment")
	}
	pressTab(c)
	pressTab(c)
	if got := focusName(a, c.Focused()); got != "Send" {
		t.Fatalf("two Tabs from the comment: %q", got)
	}
	pressSpace(c)
	srv.Wait(t, "event", 1)
}
