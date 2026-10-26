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
	// replaced is set once a recorder of another build was restarted.
	replaced bool
	// tray says the platform has a system tray (menu bar on macOS);
	// quitApp quits, replaced by tests.
	tray    bool
	quitApp func()
	// trayMenu is the tray's menu, whose trayAction is the app bar's
	// Record or Stop.
	trayMenu   *fyne.Menu
	trayAction *fyne.MenuItem
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
	// reports are the reports on their way to the bug tracker, which
	// tests wait for: one still going must not touch the next test's
	// window.
	reports sync.WaitGroup
	// heard and incomplete are the link goroutine's: the first sound, and
	// the incomplete tracks in a row.
	heard      bool
	incomplete int

	pending     sync.Mutex
	queued      bool
	levelQueued bool
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
		onReopen(func() { fyne.Do(a.showWindow) })
	}
	_, a.tray = fa.(desktop.App)
	a.quitApp = fa.Quit
	a.win.SetCloseIntercept(a.closeWindow)
	// Every way out (this window, the tray's Quit, Cmd+Q) ends here.
	fa.Lifecycle().SetOnStopped(a.stopDaemon)
	m.OnChange(a.scheduleRender)
	m.OnLevel(a.scheduleLevel)
	m.OnEvent(a.onEvent)
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
	a.fa.Run()
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
	if a.tray && a.last.Settings["app.minimize_to_tray"] == true {
		a.hideToTray()
		return
	}
	a.quit()
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
	default:
		a.action.Enable()
		a.setAction("Record", theme.MediaRecordIcon(), widget.HighImportance)
	}
	// The tray icon comes with Minimize to tray; the toolkit cannot take
	// it away, so turned off, it goes when Spytify restarts.
	if a.tray && a.trayMenu == nil && s.Settings["app.minimize_to_tray"] == true {
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

// failingChecks counts the failing checks but the player's, which the app
// bar already shows.
func failingChecks(checks []proto.Check) int {
	n := 0
	for _, c := range checks {
		if !c.OK && c.Name != "player running" {
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
			a.noteAction("stop now")
			err = a.link.Do(ctx, proto.MethodSessionStop, proto.SessionStopParams{Now: true}, nil)
		case s.Recording():
			a.noteAction("stop")
			err = a.link.Do(ctx, proto.MethodSessionStop, proto.SessionStopParams{}, nil)
		default:
			a.noteAction("record")
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
	if !a.opts.ManualRender {
		showInDock(true)
	}
	a.win.Show()
	a.win.RequestFocus()
}
