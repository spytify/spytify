// Package ui is the spytify desktop client: one window with tabs, a single
// Record / Stop control in the app bar, and a tray menu. It holds no
// recording state of its own; every screen is drawn from the model, which
// only changes when the core sends a result or an event.
package ui

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"net/url"
	"os"
	"runtime"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	fynetooltip "github.com/dweymouth/fyne-tooltip"
	ttwidget "github.com/dweymouth/fyne-tooltip/widget"

	"github.com/spytify/spytify/internal/service"
	"github.com/spytify/spytify/internal/settings"
	"github.com/spytify/spytify/internal/telemetry"
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

// Wordmark is the logo with the name beside it, for the app bar and the About screen.
var Wordmark = fyne.NewStaticResource("logo-wordmark.png", wordmarkPNG)

//go:embed assets/open-in-new.svg
var openInNewSVG []byte

// openInNewIcon marks a button that opens a page in the browser: Material
// Design's open_in_new, like Fyne's own icons, in the button's colour.
var openInNewIcon = theme.NewThemedResource(fyne.NewStaticResource("open-in-new.svg", openInNewSVG))

//go:embed assets/heart.png
var heartPNG []byte

// heartIcon is the app bar's way to Donate: v1's heart, from its icons, in
// its pink in both themes.
var heartIcon = fyne.NewStaticResource("heart.png", heartPNG)

//go:embed assets/new-releases.png
var newReleasesPNG []byte

// newReleasesIcon marks a newer release: v1's badge, from its icons, in its
// green with the mark cut out.
var newReleasesIcon = fyne.NewStaticResource("new-releases.png", newReleasesPNG)

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
	// AutoStart says the app starts spytifyd itself, a player being set:
	// until it answers, the tabs show that it is starting rather than the
	// offline screen asking for it.
	AutoStart bool
	// Telemetry sends what the user agrees to send; nil sends nothing.
	Telemetry *telemetry.Client
	// Privacy are the privacy.* settings read at launch, used until the
	// recorder answers.
	Privacy map[string]any
	// MinimizeToTray is app.minimize_to_tray read at launch: its tray is
	// set up before the toolkit's loop runs, the only time Linux starts
	// one.
	MinimizeToTray bool
	// SoftwareOpenGL says the window is drawn with Mesa's OpenGL, no
	// graphics driver offering any (package opengl).
	SoftwareOpenGL bool
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
	// tabsView is the tabs with the plain colour behind their bar.
	tabsView *fyne.Container
	center   *fyne.Container
	offline  fyne.CanvasObject
	player   *playerCard

	dot     *widget.Label
	status  *widget.Label
	output  *widget.RichText
	update  *widget.Button
	donate  *ttwidget.Button
	action  *widget.Button
	banner  *widget.Label
	bar     *fyne.Container
	covers  *covers
	checked bool
	// themeMode is the app.theme applied: system, light or dark.
	themeMode string
	// outputDir is the output.dir the reports leave out.
	outputDir string
	// replaced is set once a recorder of another build was restarted.
	replaced bool
	// tray says the platform has a system tray (menu bar on macOS);
	// noTrayHost, that this Linux desktop draws no tray icons
	// (watchTrayHost); trayLate, that the tray was set up once the
	// toolkit's loop ran, which Linux never shows (lateTrayShows);
	// running, that the loop runs. quitApp quits, replaced by tests.
	tray       bool
	noTrayHost bool
	trayLate   bool
	running    bool
	quitApp    func()
	// fyneStop is Fyne's stop hook, which ShowAndRun runs (see New).
	fyneStop func()
	// trayMenu is the tray's menu, whose trayAction is the app bar's
	// Record or Stop.
	trayMenu   *fyne.Menu
	trayAction *fyne.MenuItem
	// notes is what the notifications sent while the window is hidden in
	// the tray keep from one event to the next.
	notes notices
	// starting is set while spytifyd is being started; startErr is why
	// the last start failed.
	starting   bool
	startErr   string
	offlineErr *widget.Label

	// tel is the Sentry bug tracker; launch times the way to a recorder
	// that answers; checksNoted are the checks last left as a breadcrumb.
	tel          *telemetry.Client
	launch       launchTrace
	shown        bool
	wasConnected bool
	checksNoted  string
	diagNoted    bool
	// reports are the reports on their way to the bug tracker, and calls
	// the requests on their way to the recorder, which tests wait for:
	// one still going must not touch the next test's window.
	reports sync.WaitGroup
	calls   sync.WaitGroup
	// heard is the link goroutine's: the first sound.
	heard bool

	renderQueue throttle
	levelQueue  throttle
	barKey      string
	last        model.State
}

// New builds the window; call ShowAndRun to start.
func New(fa fyne.App, l *link.Link, m *model.Model, opts Options) *App {
	a := &App{fa: fa, m: m, link: l, opts: opts, tel: opts.Telemetry, launch: launchTrace{start: time.Now()}}
	a.applyTheme("system")
	a.covers = newCovers(a.scheduleRender)
	a.win = fa.NewWindow("Spytify")
	a.win.SetIcon(Logo)
	a.win.Resize(fyne.NewSize(920, 640))

	a.screens = []screen{newRecordScreen(a), newLibraryScreen(a), newSettingsScreen(a), newLogsScreen(a), newAboutScreen(a)}
	// Logs and About have the plain colour to the window's sides; the other
	// screens are on the page, off them.
	a.tabs = container.NewAppTabs(
		container.NewTabItemWithIcon("Record", theme.MediaRecordIcon(), margin(a.screens[tabRecord].content())),
		container.NewTabItemWithIcon("Library", theme.FolderIcon(), margin(a.screens[tabLibrary].content())),
		container.NewTabItemWithIcon("Settings", theme.SettingsIcon(), margin(a.screens[tabSettings].content())),
		container.NewTabItemWithIcon("Logs", theme.ListIcon(), a.screens[tabLogs].content()),
		container.NewTabItemWithIcon("About", theme.InfoIcon(), a.screens[tabAbout].content()),
	)
	a.tabs.SetTabLocation(container.TabLocationTop)
	a.tabs.OnSelected = func(*container.TabItem) {
		// Files can change behind the app's back: read the folder again.
		if l, ok := a.screens[a.tabs.SelectedIndex()].(*libraryScreen); ok {
			l.scan()
		}
		a.noteTab(a.tabs.SelectedIndex())
		a.renderCenter(a.last)
	}

	a.dot = widget.NewLabel("●")
	a.status = widget.NewLabel("")
	// Secondary, but readable in both themes, unlike a low-importance label.
	a.output = widget.NewRichText(&widget.TextSegment{Style: widget.RichTextStyle{ColorName: theme.ColorNamePlaceHolder, Inline: true}})
	a.update = widget.NewButtonWithIcon("", newReleasesIcon, nil)
	// No fill: the badge's own green says it, as the heart's pink does.
	a.update.Importance = widget.LowImportance
	a.update.Hide()
	a.donate = ttwidget.NewButtonWithIcon("", heartIcon, func() { a.openURL(donateURL) })
	a.donate.Importance = widget.LowImportance
	a.donate.SetToolTip("Donate")
	a.action = widget.NewButtonWithIcon("Record", theme.MediaRecordIcon(), a.toggleRecording)
	a.action.Importance = widget.HighImportance
	// The wordmark: the logo with the name in its green.
	mark := canvas.NewImageFromResource(Wordmark)
	mark.FillMode = canvas.ImageFillContain
	mark.SetMinSize(fyne.NewSize(26*622/204, 26))
	brand := container.NewCenter(mark)
	pills := container.NewHBox(widget.NewSeparator(), a.dot, a.status, widget.NewSeparator(), a.output)
	a.bar = container.NewBorder(nil, nil, container.NewHBox(brand, pills), container.NewHBox(a.update, a.donate, a.action))

	a.banner = widget.NewLabel("")
	a.banner.Wrapping = fyne.TextWrapWord
	a.banner.Importance = widget.DangerImportance
	a.banner.Hide()

	start := widget.NewButtonWithIcon("Start the recorder", theme.MediaPlayIcon(), a.StartDaemon)
	start.Importance = widget.HighImportance
	a.player = newPlayerCard(a)
	intro := widget.NewLabel("Spytify records through spytifyd, which runs on its own so recording goes on with this window closed. It starts with the app; choose the player it records if none is set.")
	intro.Wrapping = fyne.TextWrapWord
	a.offlineErr = widget.NewLabel("")
	a.offlineErr.Wrapping = fyne.TextWrapWord
	a.offlineErr.Importance = widget.DangerImportance
	pad := theme.Padding()
	a.offline = container.NewVScroll(container.New(layout.NewCustomPaddedLayout(pad, pad*2, pad*2, pad*2), container.NewVBox(
		heading("The recorder is not running"),
		a.offlineErr,
		intro,
		container.NewHBox(start),
		caption(opts.Socket),
		widget.NewSeparator(),
		newCard(sectionTitle("Player"), indent(a.player.box)),
	)))
	a.starting = opts.AutoStart

	a.tabsView = withPlainTabBar(a.tabs)
	a.center = container.NewStack(a.tabsView)
	// The window has no padding of its own, so the bars' plain colour goes
	// to its edges; the page's grey is the window's background.
	top := plain(container.New(layout.NewCustomPaddedLayout(pad, 0, pad, pad), container.NewVBox(container.NewPadded(a.bar), a.banner)))
	body := container.NewBorder(top, nil, nil, nil, a.center)
	a.win.SetPadded(false)
	a.win.SetContent(fynetooltip.AddWindowToolTipLayer(body, a.win.Canvas()))

	if !opts.ManualRender {
		onReopen(a.Reopen)
	}
	_, a.tray = fa.(desktop.App)
	if a.tray && !opts.ManualRender {
		watchTrayHost(func(present bool) { fyne.Do(func() { a.setTrayHost(present) }) })
	}
	if a.tray && opts.MinimizeToTray {
		a.setupTray()
	}
	a.quitApp = fa.Quit
	a.win.SetCloseIntercept(a.closeWindow)
	// Fyne 2.8.2 sends its stop hook, which saves the preferences, through
	// a queue it closes at the same moment: at quit, the hook can be skipped,
	// hang the app or panic (fyne-io/fyne#6634). ShowAndRun runs it once
	// the loop has returned.
	if hook, ok := fa.Lifecycle().(fyneStopHook); ok {
		a.fyneStop = hook.OnStopped()
		hook.SetOnStoppedHookExecuted(nil)
	}
	m.OnChange(a.scheduleRender)
	m.OnLevel(a.scheduleLevel)
	m.OnEvent(a.onEvent)
	m.OnEvent(a.recheck)
	m.OnEvent(a.notify)
	a.render()
	return a
}

// Window is the main window.
func (a *App) Window() fyne.Window { return a.win }

// ShowAndRun shows the window and runs the toolkit's loop.
func (a *App) ShowAndRun() {
	a.win.Show()
	a.maybeShowFirstRun(func() {
		a.shown = true
		a.renderPrivacy(a.last)
		a.tel.Step("launched")
		a.tel.Breadcrumb("app", "launched", nil)
		a.checkCrashes()
	})
	a.running = true
	a.fa.Run()
	// Every way out (this window, the tray's Quit, Cmd+Q, a signal) ends
	// here.
	a.stopDaemon()
	if a.fyneStop != nil {
		a.fyneStop()
	}
}

// fyneStopHook is the part of Fyne's lifecycle that holds its own stop hook.
type fyneStopHook interface {
	OnStopped() func()
	SetOnStoppedHookExecuted(func())
}

// showTab switches to a tab, from a button elsewhere in the window.
// playerRunning is the latest word on the player: the target.state events,
// or core.info's check until the first one.
func playerRunning(s model.State) bool {
	return (s.TargetKnown && s.Target.Running) || (!s.TargetKnown && checkOK(s.Info.Checks, "player running"))
}

// launchPlayer asks the recorder to open the player.
func (a *App) launchPlayer() {
	a.noteAction("open the player")
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
	a.calls.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if a.link.Do(ctx, proto.MethodCoreShutdown, nil, nil) == nil {
			time.Sleep(time.Second)
		}
		if err := a.link.StartDaemon(); err != nil {
			fyne.Do(func() { a.showError(err) })
		}
	})
}

// quit closes Spytify, the recorder included; a recording in progress is
// confirmed first, since its current track is not kept.
func (a *App) quit() {
	if !a.last.Recording() {
		a.quitApp()
		return
	}
	dialog.ShowConfirm("Quit Spytify?", "Recording stops, and the track being recorded is not kept.", func(ok bool) {
		if ok {
			a.quitApp()
		}
	}, a.win)
}

// closeWindow answers the window's close button: with Minimize to tray on
// and a tray to go to, the window hides there and the recording goes on;
// otherwise the app quits, which stops the recorder too.
func (a *App) closeWindow() {
	if a.hasTray() && a.last.Settings[settings.AppMinimizeToTray.String()] == true {
		a.hideToTray()
		return
	}
	a.quit()
}

// hasTray says whether the tray icon shows, or would: the platform has a
// tray and, on Linux, the desktop draws it and the toolkit started it.
func (a *App) hasTray() bool { return a.tray && !a.noTrayHost && !a.trayLate }

// trayNote is what Minimize to tray says under its label: why closing
// quits even with it on.
func (a *App) trayNote(s model.State) string {
	switch {
	case a.noTrayHost:
		return noTrayNote
	case a.trayLate && s.Settings[settings.AppMinimizeToTray.String()] == true:
		return lateTrayNote
	}
	return ""
}

// setTrayHost takes watchTrayHost's word: with a tray host, closing hides
// to the tray, whose icon registers with it by itself; with none, closing
// quits and Settings says why.
func (a *App) setTrayHost(present bool) {
	if a.noTrayHost == !present {
		return
	}
	a.noTrayHost = !present
	a.render()
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

// showOutputSetting opens Settings on the output folder.
func (a *App) showOutputSetting() {
	a.noteAction("choose another folder")
	a.showTab(tabSettings)
	if set, ok := a.screens[tabSettings].(*settingsScreen); ok {
		set.search.SetText("Output folder")
	}
}

// scheduleRender coalesces bursts of events (levels arrive at 10 Hz) into
// at most ten redraws a second, on the toolkit's goroutine.
func (a *App) scheduleRender() {
	if a.opts.ManualRender {
		return
	}
	a.renderQueue.schedule(a.render)
}

func (a *App) render() {
	s := a.m.Snapshot()
	a.last = s
	connected := s.Conn == model.Connected
	running := playerRunning(s)
	switch {
	case !connected && a.starting:
		a.dot.Importance = widget.LowImportance
		a.status.SetText(startingText)
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
	case s.HaveInfo && !running:
		// Nothing would play: the app bar says the player is not running,
		// and the Record screen offers to open it.
		a.action.Disable()
		a.setAction("Record", theme.MediaRecordIcon(), widget.HighImportance)
	case s.HaveInfo && checkFails(s.Info.Checks, proto.CheckOutputWritable):
		// Nothing could be saved: the Record screen says so and leads to
		// the diagnostics.
		a.action.Disable()
		a.setAction("Record", theme.MediaRecordIcon(), widget.HighImportance)
	default:
		a.action.Enable()
		a.setAction("Record", theme.MediaRecordIcon(), widget.HighImportance)
	}
	// The tray icon comes with Minimize to tray; the toolkit cannot take
	// it away, so turned off, it goes when Spytify restarts. On Linux,
	// turned on, it comes then too: see setupTray.
	if a.hasTray() && a.trayMenu == nil && s.Settings[settings.AppMinimizeToTray.String()] == true {
		a.setupTray()
	}
	a.syncTray()
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
	if mode, _ := s.Settings[settings.AppTheme.String()].(string); mode != "" && mode != a.themeMode {
		a.applyTheme(mode)
	}
	if dir, _ := s.Settings[settings.OutputDir.String()].(string); dir != "" && dir != a.outputDir {
		a.outputDir = dir
		a.tel.SetOutputDir(settings.ExpandHome(dir))
	}
	if a.tabs.Items[tabRecord].Text != record {
		a.tabs.Items[tabRecord].Text = record
		a.tabs.Refresh()
	}
	if connected && !a.checked && !a.opts.NoUpdateCheck {
		if v, ok := s.Settings[settings.AppCheckUpdates.String()].(bool); ok {
			a.checked = true
			if v {
				go a.checkUpdate()
			}
		}
	}
	a.renderPrivacy(s)
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
	if s.Conn == model.Connected {
		a.starting, a.startErr = false, ""
	}
	// The offline screen asks for a player or tells why the recorder did
	// not start; one being started shows the tabs.
	if s.Conn != model.Connected && !s.HaveInfo && !a.starting {
		reason := a.startErr
		if reason == "" {
			reason = s.ConnError
		}
		setLabel(a.offlineErr, reason)
		show(a.offlineErr, reason != "")
		if len(a.center.Objects) != 1 || a.center.Objects[0] != a.offline {
			a.center.Objects = []fyne.CanvasObject{a.offline}
			a.center.Refresh()
		}
		return
	}
	if len(a.center.Objects) != 1 || a.center.Objects[0] != a.tabsView {
		a.center.Objects = []fyne.CanvasObject{a.tabsView}
		a.center.Refresh()
	}
	a.screens[a.tabs.SelectedIndex()].render(s)
	// The diagnostics' dialog shows over any tab: its checks follow too.
	if logs := a.screens[tabLogs].(*logsScreen); logs.open && a.tabs.SelectedIndex() != tabLogs {
		logs.renderChecks(s)
	}
}

// scheduleLevel redraws the meter and the progress, at most every 100 ms.
func (a *App) scheduleLevel() {
	if a.opts.ManualRender {
		return
	}
	a.levelQueue.schedule(a.renderLevel)
}

func (a *App) renderLevel() {
	if a.tabs.SelectedIndex() != tabRecord {
		return
	}
	// Under a dialog the meter is hidden, and moving it repaints the whole
	// dimmed window: two thirds of a core with the first-run dialog open.
	if a.win.Canvas().Overlays().Top() != nil {
		return
	}
	a.screens[tabRecord].(*recordScreen).renderLive(a.m.Live())
}

func (a *App) checkUpdate() {
	if devBuild(proto.Version) {
		return
	}
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
	a.noteAction("open " + raw)
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

// failingChecks counts the checks that need attention.
func failingChecks(checks []proto.Check) int {
	n := 0
	for _, c := range checks {
		if needsAttention(c) {
			n++
		}
	}
	return n
}

// needsAttention says whether a check fails in a way the user must fix: not
// the player's, which the app bar already shows, nor advice, which only
// lowers quality.
func needsAttention(c proto.Check) bool {
	return !c.OK && !c.Advice && c.Name != "player running"
}

// checkFails says whether the check named name is there and fails: a
// recorder of an earlier build may not have it.
func checkFails(checks []proto.Check, name string) bool {
	for _, c := range checks {
		if c.Name == name {
			return !c.OK
		}
	}
	return false
}

func outputText(values map[string]any) string {
	format, _ := values[settings.EncodeFormat.String()].(string)
	switch format {
	case "":
		return ""
	case "mp3", "aac":
		return fmt.Sprintf("%s %v kbps", upper(format), number(values[settings.EncodeBitrateKbps.String()]))
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
	// A session stopped from here ends with no notification.
	a.notes.stopAsked = s.Recording()
	a.calls.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var err error
		switch {
		case s.Session == proto.StateStopping:
			a.noteAction("stop now")
			err = a.link.Do(ctx, proto.MethodSessionStop, proto.SessionStopParams{Now: true}, nil)
		case s.Recording():
			a.noteAction("stop")
			err = a.link.Do(ctx, proto.MethodSessionStop, proto.SessionStopParams{}, nil)
		default:
			a.noteAction("record")
			err = a.link.Do(ctx, proto.MethodSessionStart, proto.SessionStartParams{}, nil)
		}
		var perr *proto.Error
		if errors.As(err, &perr) && perr.Code == proto.CodeOutputDir {
			// The output folder went since the checks ran: the
			// diagnostics show them, run again.
			fyne.Do(a.showDiagnostics)
			a.refreshChecks()
			return
		}
		if err != nil {
			fyne.Do(func() { a.showError(err) })
		}
	})
}

// refreshChecks runs the recorder's checks again.
func (a *App) refreshChecks() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = a.link.RefreshInfo(ctx)
}

// recheck runs the checks again when what they test may have changed:
// the output folder lost during a recording, or another one chosen. It
// runs on the link's goroutine.
func (a *App) recheck(ev proto.Event) {
	switch ev.Method {
	case proto.EventError:
		var e proto.ErrorEvent
		if ev.Decode(&e) != nil || e.Code != "output_dir" {
			return
		}
	case proto.EventSettingsChanged:
		var e proto.SettingsValues
		if ev.Decode(&e) != nil {
			return
		}
		if _, ok := e.Values[settings.OutputDir.String()]; !ok {
			return
		}
	default:
		return
	}
	go a.refreshChecks()
}

// do runs an action off the toolkit's goroutine and reports a failure.
func (a *App) do(method string, params any, then func()) {
	a.calls.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := a.link.Do(ctx, method, params, nil); err != nil {
			fyne.Do(func() { a.showError(err) })
			return
		}
		if then != nil {
			fyne.Do(then)
		}
	})
}

func (a *App) showError(err error) {
	showError(a.win, err)
}

// StartDaemon starts spytifyd and reports why when it stops at once.
func (a *App) StartDaemon() {
	fyne.Do(func() {
		a.starting, a.startErr = true, ""
		a.tel.Breadcrumb("app", "starting the recorder", nil)
		if a.launch.started.IsZero() {
			a.launch.started = time.Now()
		}
		a.render()
	})
	go func() {
		err := a.link.StartDaemon()
		if err == nil {
			// Started: it answers in a moment, or this says it did not.
			time.Sleep(startAnswerTimeout)
			err = errors.New("spytifyd started but does not answer; see its log in Logs")
			if a.m.Snapshot().Conn == model.Connected {
				return
			}
		}
		fyne.Do(func() { a.startFailed(err) })
	}()
}

// startingText is what the app bar and Record say while spytifyd starts.
const startingText = "Starting the recorder…"

// startAnswerTimeout is how long a started spytifyd has to answer.
const startAnswerTimeout = 15 * time.Second

// startFailed shows the offline screen with why the recorder did not start.
func (a *App) startFailed(err error) {
	a.starting, a.startErr = false, err.Error()
	a.launchFailed(err)
	a.render()
}

func (a *App) setupTray() {
	desk, ok := a.fa.(desktop.App)
	if !ok {
		return
	}
	// The toolkit starts Linux's tray with its loop: one set up later
	// never shows, and closing quits until Spytify restarts.
	a.trayLate = a.running && !lateTrayShows
	desk.SetSystemTrayIcon(Logo)
	// The tray's action is the app bar's, which render copies there:
	// Record, Stop, or Stop now while the last track is being saved.
	a.trayAction = fyne.NewMenuItem("Record", a.toggleRecording)
	a.trayAction.Disabled = true
	a.trayMenu = fyne.NewMenu("Spytify",
		a.trayAction,
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Open window", a.showWindow),
	)
	desk.SetSystemTrayMenu(a.trayMenu)
}

// syncTray gives the tray the app bar's action, rebuilding the tray menu
// only when it changes.
func (a *App) syncTray() {
	if a.trayMenu == nil {
		return
	}
	label, off := a.action.Text, a.action.Disabled()
	if a.trayAction.Label == label && a.trayAction.Disabled == off {
		return
	}
	a.trayAction.Label, a.trayAction.Disabled = label, off
	a.trayMenu.Refresh()
}

const trayNoticeKey = "tray-notice-seen"

// hideToTray hides the window, and on macOS the Dock icon with it; the
// session goes on. The first time, a notification says where the app is.
func (a *App) hideToTray() {
	a.win.Hide()
	a.notes.hidden = true
	if a.opts.ManualRender {
		return
	}
	showInDock(false)
	prefs := a.fa.Preferences()
	if !prefs.Bool(trayNoticeKey) {
		prefs.SetBool(trayNoticeKey, true)
		a.fa.SendNotification(fyne.NewNotification("Spytify is still running",
			"It records from the menu bar. Quit it from there, or turn off Minimize to tray in Settings."))
	}
}

func (a *App) showWindow() {
	a.notes.hidden = false
	if !a.opts.ManualRender {
		showInDock(true)
	}
	a.win.Show()
	a.win.RequestFocus()
}

// Reopen brings the window back, hidden to the tray or not: Spytify was
// opened again while it runs. It may be called from any goroutine.
func (a *App) Reopen() {
	fyne.Do(a.showWindow)
}
