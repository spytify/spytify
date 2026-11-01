// Package ui is the spytify desktop client: one window with tabs, a single
// Record / Stop control in the app bar, and a tray menu. It holds no
// recording state of its own; every screen is drawn from the model, which
// only changes when the core sends a result or an event.
package ui

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	fynetooltip "github.com/dweymouth/fyne-tooltip"

	"github.com/spytify/spytify/internal/settings"
	"github.com/spytify/spytify/internal/telemetry"
	"github.com/spytify/spytify/internal/ui/link"
	"github.com/spytify/spytify/internal/ui/model"
	"github.com/spytify/spytify/pkg/proto"
)

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
	// tabKeys is the tab bar's stop in the Tab order.
	tabKeys *tabKeys
	center  *fyne.Container
	offline *container.Scroll
	player  *playerCard

	dot     *widget.Label
	status  *widget.Label
	output  *widget.RichText
	update  *button
	donate  *tipButton
	action  *litButton
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
	// (watchTrayHost). quitApp quits, replaced by tests.
	tray       bool
	noTrayHost bool
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
	// masks are the widgets the window's picture covers (picture.go), true
	// for those covered whatever they show; unpictured counts the dialogs
	// open that it could not cover, an error's message or the folder
	// picker, in Fyne's own widgets: no picture is taken meanwhile.
	masks      map[fyne.CanvasObject]bool
	unpictured int
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

	// focusSeen is the control followFocus last saw focused.
	focusSeen fyne.Focusable
}

// New builds the window; call ShowAndRun to start.
func New(fa fyne.App, l *link.Link, m *model.Model, opts Options) *App {
	a := &App{fa: fa, m: m, link: l, opts: opts, tel: opts.Telemetry, launch: launchTrace{start: time.Now()}, masks: map[fyne.CanvasObject]bool{}}
	a.tel.SetScreenshot(a.screenshot)
	a.applyTheme("system")
	a.covers = newCovers(a.scheduleRender)
	a.win = fa.NewWindow("Spytify")
	a.win.SetIcon(Logo)
	a.win.Resize(fyne.NewSize(920, 640))

	a.screens = []screen{newRecordScreen(a), newLibraryScreen(a), newSettingsScreen(a), newLogsScreen(a), newAboutScreen(a)}
	// Record and Library run their cards and lists to the window's sides,
	// Logs and About their plain colour; Settings keeps its cards on the
	// page, off them, and its scroll bar at the window's edge.
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
		a.noteTab(a.tabs.SelectedIndex())
		a.renderCenter(a.last)
	}

	a.dot = widget.NewLabel("●")
	a.status = widget.NewLabel("")
	// Secondary, but readable in both themes, unlike a low-importance label.
	a.output = widget.NewRichText(&widget.TextSegment{Style: widget.RichTextStyle{ColorName: theme.ColorNamePlaceHolder, Inline: true}})
	a.update = newIconButton("", newReleasesIcon, nil)
	// No fill: the badge's own green says it, as the heart's pink does.
	a.update.Importance = widget.LowImportance
	a.update.Hide()
	a.donate = newTipButton("", heartIcon, func() { a.openURL(donateURL) })
	a.donate.Importance = widget.LowImportance
	a.donate.SetToolTip("Donate")
	a.action = newLitButton("Record", theme.MediaRecordIcon(), widget.HighImportance, a.toggleRecording)
	// The wordmark: the logo with the name in its green.
	mark := canvas.NewImageFromResource(Wordmark)
	mark.FillMode = canvas.ImageFillContain
	mark.SetMinSize(fyne.NewSize(26*622/204, 26))
	brand := container.NewCenter(mark)
	pills := container.NewHBox(widget.NewSeparator(), a.dot, a.status, widget.NewSeparator(), a.output)
	a.bar = container.NewBorder(nil, nil, container.NewHBox(brand, pills), container.NewHBox(a.update, a.donate, a.action.lit))

	a.banner = widget.NewLabel("")
	a.banner.Wrapping = fyne.TextWrapWord
	a.banner.Importance = widget.DangerImportance
	a.banner.Hide()
	a.maskPaths(a.banner)

	start := newLitButton("Start the recorder", theme.MediaPlayIcon(), widget.HighImportance, a.StartDaemon)
	a.player = newPlayerCard(a)
	intro := widget.NewLabel("Spytify records through spytifyd, which runs on its own so recording goes on with this window closed. It starts with the app; choose the player it records if none is set.")
	intro.Wrapping = fyne.TextWrapWord
	a.offlineErr = widget.NewLabel("")
	a.offlineErr.Wrapping = fyne.TextWrapWord
	a.offlineErr.Importance = widget.DangerImportance
	a.maskPaths(a.offlineErr)
	pad := theme.Padding()
	a.offline = container.NewVScroll(container.New(layout.NewCustomPaddedLayout(pad, pad*2, pad*2, pad*2), container.NewVBox(
		heading("The recorder is not running"),
		a.offlineErr,
		intro,
		container.NewHBox(start.lit),
		caption(opts.Socket),
		widget.NewSeparator(),
		newCard(sectionTitle("Player"), indent(a.player.box)),
	)))
	a.starting = opts.AutoStart

	a.tabKeys = newTabKeys(a.tabs)
	a.tabsView = withPlainTabBar(a.tabs, a.tabKeys)
	a.center = container.NewStack(a.tabsView)
	// The window has no padding of its own, so the bars' plain colour goes
	// to its edges; the page's grey is the window's background.
	top := plain(container.New(layout.NewCustomPaddedLayout(pad, 0, pad, pad), container.NewVBox(container.NewPadded(a.bar), a.banner)))
	body := inReadingOrder(top, nil, nil, nil, a.center)
	a.win.SetPadded(false)
	a.win.SetContent(fynetooltip.AddWindowToolTipLayer(body, a.win.Canvas()))
	// With nothing focused, Escape comes to the window.
	a.win.Canvas().SetOnTypedKey(func(e *fyne.KeyEvent) {
		if e.Name == fyne.KeyEscape {
			escape(a.win.Canvas())
		}
	})

	if !opts.ManualRender {
		onReopen(a.Reopen)
		a.watchFocus()
	}
	_, a.tray = fa.(desktop.App)
	if a.tray && !opts.ManualRender {
		watchTrayHost(func(present bool) { fyne.Do(func() { a.setTrayHost(present) }) })
	}
	// The toolkit starts a tray with its loop and never after, macOS's
	// menu bar item included: the icon is there from launch, and
	// Minimize to tray only says whether closing hides the window to it.
	if a.tray {
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
	m.OnEvent(a.feedTracker)
	m.OnEvent(a.recheck)
	m.OnEvent(a.notify)
	if !opts.ManualRender {
		go a.clockAds()
	}
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
	ask(a.win, "Quit Spytify?", "Recording stops, and the track being recorded is not kept.", func(ok bool) {
		if ok {
			a.quitApp()
		}
	})
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
		// The home folder in it leaves as ~ all the same.
		if settings.IsDefaultOutputDir(dir) {
			dir = ""
		}
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

// clockAds moves the progress once a second while an ad plays: the
// recorder sends level events, which move it, only while it records.
func (a *App) clockAds() {
	for range time.Tick(time.Second) {
		if c, session, _ := a.m.Live(); c != nil && session == proto.StateAd {
			a.scheduleLevel()
		}
	}
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
	d := showError(a.win, err)
	a.unpictured++
	d.SetOnClosed(func() { a.unpictured-- })
}
