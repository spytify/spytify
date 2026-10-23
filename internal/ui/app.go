// Package ui is the spytify desktop client: one window with tabs, a single
// Record / Stop control in the app bar, and a tray menu. It holds no
// recording state of its own; every screen is drawn from the model, which
// only changes when the core sends a result or an event.
package ui

import (
	"context"
	_ "embed"
	"fmt"
	"net/url"
	"os"
	"runtime"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/spytify/spytify/internal/service"
	"github.com/spytify/spytify/internal/ui/link"
	"github.com/spytify/spytify/internal/ui/model"
	"github.com/spytify/spytify/pkg/proto"
)

//go:embed assets/logo.png
var logoPNG []byte

// Logo is the app and tray icon.
var Logo = fyne.NewStaticResource("logo.png", logoPNG)

//go:embed assets/logo-wordmark.png
var wordmarkPNG []byte

// Wordmark is the logo with the name beside it, for the About screen.
var Wordmark = fyne.NewStaticResource("logo-wordmark.png", wordmarkPNG)

// Options are what the UI needs from its environment.
type Options struct {
	// StateDir holds the daemon's log file.
	StateDir string
	// Socket is shown when the daemon cannot be reached.
	Socket string
	// ManualRender leaves redrawing to the caller. Tests set it: the
	// headless driver runs fyne.Do on the calling goroutine, so a timed
	// redraw would race with the test.
	ManualRender bool
	// NoUpdateCheck skips the GitHub release check (tests).
	NoUpdateCheck bool
}

type screen interface {
	content() fyne.CanvasObject
	render(s model.State)
}

const (
	tabRecord = iota
	tabLibrary
	tabSettings
	tabLogs
	tabAbout
)

// App is the window and its screens.
type App struct {
	fa   fyne.App
	win  fyne.Window
	m    *model.Model
	link *link.Link
	opts Options

	screens []screen
	tabs    *container.AppTabs
	center  *fyne.Container
	offline fyne.CanvasObject
	player  *playerCard

	dot     *widget.Label
	status  *widget.Label
	output  *widget.RichText
	update  *widget.Button
	action  *widget.Button
	banner  *widget.Label
	bar     *fyne.Container
	covers  *covers
	checked bool
	// themeMode is the app.theme applied: system, light or dark.
	themeMode string
	// replaced is set once a recorder of another build was restarted.
	replaced bool

	pending     sync.Mutex
	queued      bool
	levelQueued bool
	barKey      string
	last        model.State
}

// New builds the window; call ShowAndRun to start.
func New(fa fyne.App, l *link.Link, m *model.Model, opts Options) *App {
	a := &App{fa: fa, m: m, link: l, opts: opts}
	a.applyTheme("system")
	a.covers = newCovers(a.scheduleRender)
	a.win = fa.NewWindow("Spytify")
	a.win.SetIcon(Logo)
	a.win.Resize(fyne.NewSize(920, 640))

	a.screens = []screen{newRecordScreen(a), newLibraryScreen(a), newSettingsScreen(a), newLogsScreen(a), newAboutScreen(a)}
	a.tabs = container.NewAppTabs(
		container.NewTabItemWithIcon("Record", theme.MediaRecordIcon(), a.screens[tabRecord].content()),
		container.NewTabItemWithIcon("Library", theme.FolderIcon(), a.screens[tabLibrary].content()),
		container.NewTabItemWithIcon("Settings", theme.SettingsIcon(), a.screens[tabSettings].content()),
		container.NewTabItemWithIcon("Logs", theme.ListIcon(), a.screens[tabLogs].content()),
		container.NewTabItemWithIcon("About", theme.InfoIcon(), a.screens[tabAbout].content()),
	)
	a.tabs.SetTabLocation(container.TabLocationTop)
	a.tabs.OnSelected = func(*container.TabItem) {
		// Files can change behind the app's back: read the folder again.
		if l, ok := a.screens[a.tabs.SelectedIndex()].(*libraryScreen); ok {
			l.scan()
		}
		a.renderCenter(a.last)
	}

	a.dot = widget.NewLabel("●")
	a.status = widget.NewLabel("")
	// Secondary, but readable in both themes, unlike a low-importance label.
	a.output = widget.NewRichText(&widget.TextSegment{Style: widget.RichTextStyle{ColorName: theme.ColorNamePlaceHolder, Inline: true}})
	a.update = widget.NewButtonWithIcon("", theme.DownloadIcon(), nil)
	a.update.Importance = widget.SuccessImportance
	a.update.Hide()
	a.action = widget.NewButtonWithIcon("Record", theme.MediaRecordIcon(), a.toggleRecording)
	a.action.Importance = widget.HighImportance
	brand := container.NewHBox(widget.NewIcon(Logo), widget.NewLabelWithStyle("Spytify", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))
	pills := container.NewHBox(widget.NewSeparator(), a.dot, a.status, widget.NewSeparator(), a.output)
	a.bar = container.NewBorder(nil, nil, container.NewHBox(brand, pills), container.NewHBox(a.update, a.action))

	a.banner = widget.NewLabel("")
	a.banner.Wrapping = fyne.TextWrapWord
	a.banner.Importance = widget.DangerImportance
	a.banner.Hide()

	start := widget.NewButtonWithIcon("Start spytifyd", theme.MediaPlayIcon(), a.StartDaemon)
	start.Importance = widget.HighImportance
	a.player = newPlayerCard(a)
	intro := widget.NewLabel("The recorder runs on its own, so it keeps recording when this window is closed. Choose the player it records, then start it.")
	intro.Wrapping = fyne.TextWrapWord
	a.offline = container.NewVScroll(container.NewPadded(container.NewVBox(
		heading("spytifyd is not running"),
		intro,
		container.NewHBox(start),
		caption(opts.Socket),
		widget.NewSeparator(),
		a.player.form,
	)))

	a.center = container.NewStack(a.tabs)
	body := container.NewBorder(container.NewVBox(container.NewPadded(a.bar), a.banner), nil, nil, nil, a.center)
	a.win.SetContent(body)

	a.setupTray()
	a.win.SetCloseIntercept(func() {
		if _, tray := a.fa.(desktop.App); tray && a.last.Settings["app.minimize_to_tray"] == true {
			a.win.Hide()
			return
		}
		a.quit()
	})
	// Every way out (this window, the tray's Quit, Cmd+Q) ends here.
	fa.Lifecycle().SetOnStopped(a.stopDaemon)
	m.OnChange(a.scheduleRender)
	m.OnLevel(a.scheduleLevel)
	a.render()
	return a
}

// Window is the main window.
func (a *App) Window() fyne.Window { return a.win }

// ShowAndRun shows the window and runs the toolkit's loop.
func (a *App) ShowAndRun() {
	a.win.Show()
	a.maybeShowFirstRun()
	a.fa.Run()
}

// showTab switches to a tab, from a button elsewhere in the window.
// playerRunning is the latest word on the player: the target.state events,
// or core.info's check until the first one.
func playerRunning(s model.State) bool {
	return (s.TargetKnown && s.Target.Running) || (!s.TargetKnown && checkOK(s.Info.Checks, "player running"))
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// launchPlayer asks the recorder to open the player.
func (a *App) launchPlayer() {
	a.do(proto.MethodPlayerLaunch, nil, nil)
}

// replaceOtherBuild restarts a recorder of another build, once and only
// while it records nothing: an app and a daemon of different builds do not
// agree on their settings and features.
func (a *App) replaceOtherBuild(s model.State) {
	if a.replaced || !s.HaveInfo || s.Info.Version == proto.Version || s.Recording() || a.opts.ManualRender {
		return
	}
	a.replaced = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if a.link.Do(ctx, proto.MethodCoreShutdown, nil, nil) == nil {
			time.Sleep(time.Second)
		}
		if err := a.link.StartDaemon(); err != nil {
			fyne.Do(func() { a.showError(err) })
		}
	}()
}

// quit closes Spytify, the recorder included; a recording in progress is
// confirmed first, since its current track is not kept.
func (a *App) quit() {
	if !a.last.Recording() {
		a.fa.Quit()
		return
	}
	dialog.ShowConfirm("Quit Spytify?", "Recording stops, and the track being recorded is not kept.", func(ok bool) {
		if ok {
			a.fa.Quit()
		}
	}, a.win)
}

// stopDaemon shuts spytifyd down as the app quits, unless it runs as a
// login service (spytify-cli service install), which keeps it alive.
func (a *App) stopDaemon() {
	if daemonIsService() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = a.link.Do(ctx, proto.MethodCoreShutdown, nil, nil)
}

// daemonIsService reports whether spytifyd is installed as a login service.
func daemonIsService() bool {
	daemon, err := link.FindDaemon()
	if err != nil {
		return false
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	p, err := service.For(runtime.GOOS, daemon, home, os.Getuid())
	return err == nil && service.Installed(p)
}

// applyTheme switches to the Spytify theme in the given app.theme mode.
func (a *App) applyTheme(mode string) {
	a.themeMode = mode
	a.fa.Settings().SetTheme(spytifyTheme{mode: mode})
}

func (a *App) showTab(i int) {
	a.tabs.SelectIndex(i)
	a.renderCenter(a.last)
}

// scheduleRender coalesces bursts of events (levels arrive at 10 Hz) into
// at most ten redraws a second, on the toolkit's goroutine.
func (a *App) scheduleRender() {
	if a.opts.ManualRender {
		return
	}
	a.pending.Lock()
	if a.queued {
		a.pending.Unlock()
		return
	}
	a.queued = true
	a.pending.Unlock()
	time.AfterFunc(100*time.Millisecond, func() {
		a.pending.Lock()
		a.queued = false
		a.pending.Unlock()
		fyne.Do(a.render)
	})
}

func (a *App) render() {
	s := a.m.Snapshot()
	a.last = s
	connected := s.Conn == model.Connected
	running := playerRunning(s)
	switch {
	case !connected:
		a.dot.Importance = widget.LowImportance
		a.status.SetText("Not connected")
	case !s.HaveInfo:
		a.dot.Importance = widget.LowImportance
		a.status.SetText("Connecting…")
	case running:
		a.dot.Importance = widget.SuccessImportance
		a.status.SetText(s.Info.Target.Process + " is running")
	default:
		a.dot.Importance = widget.WarningImportance
		a.status.SetText(s.Info.Target.Process + " is not running")
	}
	setCaption(a.output, outputText(s.Settings))
	switch {
	case !connected:
		a.action.Disable()
		a.setAction("Record", theme.MediaRecordIcon(), widget.HighImportance)
	case s.Session == proto.StateStopping:
		a.action.Enable()
		a.setAction("Stop now", theme.MediaStopIcon(), widget.DangerImportance)
	case s.Recording():
		a.action.Enable()
		a.setAction("Stop", theme.MediaStopIcon(), widget.DangerImportance)
	default:
		a.action.Enable()
		a.setAction("Record", theme.MediaRecordIcon(), widget.HighImportance)
	}
	if s.LastError != nil && s.LastError.Msg != "" && s.Recording() {
		a.banner.SetText(s.LastError.Msg)
		a.banner.Show()
	} else {
		a.banner.Hide()
	}
	record := "Record"
	if n := s.Totals().Saved; s.Recording() && n > 0 {
		record = fmt.Sprintf("Record · %d", n)
	}
	if connected {
		a.replaceOtherBuild(s)
	}
	if mode, _ := s.Settings["app.theme"].(string); mode != "" && mode != a.themeMode {
		a.applyTheme(mode)
	}
	if a.tabs.Items[tabRecord].Text != record {
		a.tabs.Items[tabRecord].Text = record
		a.tabs.Refresh()
	}
	if connected && !a.checked && !a.opts.NoUpdateCheck {
		if v, ok := s.Settings["app.check_updates"].(bool); ok {
			a.checked = true
			if v {
				go a.checkUpdate()
			}
		}
	}
	// Texts changed width: lay the bar out again, only then.
	if key := fmt.Sprint(a.dot.Importance, a.status.Text, a.output.String(), a.action.Text, a.update.Visible()); key != a.barKey {
		a.barKey = key
		a.dot.Refresh()
		a.bar.Refresh()
	}
	a.renderCenter(s)
}

func (a *App) setAction(text string, icon fyne.Resource, imp widget.Importance) {
	if a.action.Text == text && a.action.Importance == imp {
		return
	}
	a.action.SetText(text)
	a.action.SetIcon(icon)
	a.action.Importance = imp
	a.action.Refresh()
}

func (a *App) renderCenter(s model.State) {
	if s.Conn != model.Connected && !s.HaveInfo {
		if len(a.center.Objects) != 1 || a.center.Objects[0] != a.offline {
			a.center.Objects = []fyne.CanvasObject{a.offline}
			a.center.Refresh()
		}
		return
	}
	if len(a.center.Objects) != 1 || a.center.Objects[0] != a.tabs {
		a.center.Objects = []fyne.CanvasObject{a.tabs}
		a.center.Refresh()
	}
	a.screens[a.tabs.SelectedIndex()].render(s)
}

// scheduleLevel redraws the meter and the progress, at most every 100 ms.
func (a *App) scheduleLevel() {
	if a.opts.ManualRender {
		return
	}
	a.pending.Lock()
	if a.levelQueued {
		a.pending.Unlock()
		return
	}
	a.levelQueued = true
	a.pending.Unlock()
	time.AfterFunc(100*time.Millisecond, func() {
		a.pending.Lock()
		a.levelQueued = false
		a.pending.Unlock()
		fyne.Do(a.renderLevel)
	})
}

func (a *App) renderLevel() {
	if a.tabs.SelectedIndex() != tabRecord {
		return
	}
	a.screens[tabRecord].(*recordScreen).renderLive(a.m.Live())
}

func (a *App) checkUpdate() {
	tag, page, err := latestRelease(context.Background())
	if err != nil || tag == "" || !newer(tag, proto.Version) {
		return
	}
	fyne.Do(func() {
		a.update.SetText("Update to " + tag)
		a.update.OnTapped = func() { a.openURL(page) }
		a.update.Show()
		a.bar.Refresh()
	})
}

func (a *App) openURL(raw string) {
	if u, err := url.Parse(raw); err == nil {
		_ = a.fa.OpenURL(u)
	}
}

func checkOK(checks []proto.Check, name string) bool {
	for _, c := range checks {
		if c.Name == name {
			return c.OK
		}
	}
	return false
}

func failingChecks(checks []proto.Check) int {
	n := 0
	for _, c := range checks {
		if !c.OK {
			n++
		}
	}
	return n
}

func outputText(settings map[string]any) string {
	format, _ := settings["encode.format"].(string)
	switch format {
	case "":
		return ""
	case "mp3", "aac":
		return fmt.Sprintf("%s %v kbps", upper(format), number(settings["encode.bitrate_kbps"]))
	}
	return upper(format)
}

func upper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32
		}
	}
	return string(b)
}

func (a *App) toggleRecording() {
	s := a.last
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var err error
		switch {
		case s.Session == proto.StateStopping:
			err = a.link.Do(ctx, proto.MethodSessionStop, proto.SessionStopParams{Now: true}, nil)
		case s.Recording():
			err = a.link.Do(ctx, proto.MethodSessionStop, proto.SessionStopParams{}, nil)
		default:
			err = a.link.Do(ctx, proto.MethodSessionStart, proto.SessionStartParams{}, nil)
		}
		if err != nil {
			fyne.Do(func() { a.showError(err) })
		}
	}()
}

// do runs an action off the toolkit's goroutine and reports a failure.
func (a *App) do(method string, params any, then func()) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := a.link.Do(ctx, method, params, nil); err != nil {
			fyne.Do(func() { a.showError(err) })
			return
		}
		if then != nil {
			fyne.Do(then)
		}
	}()
}

func (a *App) showError(err error) {
	showError(a.win, err)
}

// StartDaemon starts spytifyd and reports why when it stops at once.
func (a *App) StartDaemon() {
	go func() {
		if err := a.link.StartDaemon(); err != nil {
			fyne.Do(func() { a.showError(err) })
		}
	}()
}

func (a *App) setupTray() {
	desk, ok := a.fa.(desktop.App)
	if !ok {
		return
	}
	desk.SetSystemTrayIcon(Logo)
	desk.SetSystemTrayMenu(fyne.NewMenu("Spytify",
		fyne.NewMenuItem("Start recording", func() { a.do(proto.MethodSessionStart, proto.SessionStartParams{}, nil) }),
		fyne.NewMenuItem("Stop", func() { a.do(proto.MethodSessionStop, proto.SessionStopParams{}, nil) }),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Open window", func() { a.win.Show(); a.win.RequestFocus() }),
	))
}
