// Package ui is the spytify desktop client: one window with a sidebar, a
// Record / Stop control in the title bar, and a tray menu. It holds no
// recording state of its own; every screen is drawn from the model, which
// only changes when the core sends a result or an event.
package ui

import (
	"context"
	_ "embed"
	"fmt"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/spytify/spytify/internal/ui/link"
	"github.com/spytify/spytify/internal/ui/model"
	"github.com/spytify/spytify/pkg/proto"
)

//go:embed assets/logo.png
var logoPNG []byte

// Logo is the app and tray icon.
var Logo = fyne.NewStaticResource("logo.png", logoPNG)

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
}

type screen interface {
	content() fyne.CanvasObject
	render(s model.State)
}

// App is the window and its screens.
type App struct {
	fa   fyne.App
	win  fyne.Window
	m    *model.Model
	link *link.Link
	opts Options

	screens []screen
	names   []string
	icons   []fyne.Resource
	current int

	sidebar       *widget.List
	center        *fyne.Container
	offline       fyne.CanvasObject
	offlinePlayer *playerCard
	status        *widget.Label
	output        *widget.Label
	action        *widget.Button
	banner        *widget.Label
	bar           *fyne.Container
	covers        *covers

	pending sync.Mutex
	queued  bool
	last    model.State
}

// New builds the window; call ShowAndRun to start.
func New(fa fyne.App, l *link.Link, m *model.Model, opts Options) *App {
	a := &App{fa: fa, m: m, link: l, opts: opts}
	a.covers = newCovers(a.scheduleRender)
	a.win = fa.NewWindow("Spytify")
	a.win.SetIcon(Logo)
	a.win.Resize(fyne.NewSize(920, 620))

	rec := newRecordScreen(a)
	a.screens = []screen{rec, newLibraryScreen(a), newSettingsScreen(a), newLogsScreen(a), newAboutScreen(a)}
	a.names = []string{"Record", "Library", "Settings", "Logs", "About"}
	a.icons = []fyne.Resource{theme.MediaRecordIcon(), theme.FolderIcon(), theme.SettingsIcon(), theme.ListIcon(), theme.InfoIcon()}

	a.sidebar = widget.NewList(
		func() int { return len(a.names) },
		func() fyne.CanvasObject {
			// The template's texts size the column: the longest name and a count.
			return container.NewHBox(widget.NewIcon(theme.MediaRecordIcon()), widget.NewLabel("Settings"), widget.NewLabel("● 999"))
		},
		func(id widget.ListItemID, o fyne.CanvasObject) {
			box := o.(*fyne.Container)
			box.Objects[0].(*widget.Icon).SetResource(a.icons[id])
			box.Objects[1].(*widget.Label).SetText(a.names[id])
			extra := box.Objects[2].(*widget.Label)
			extra.SetText("")
			if id == 0 && a.last.Recording() {
				extra.SetText(fmt.Sprintf("● %d", a.last.Totals().Saved))
				extra.Importance = widget.DangerImportance
			}
			extra.Refresh()
		},
	)
	a.sidebar.OnSelected = func(id widget.ListItemID) { a.show(int(id)) }

	a.status = widget.NewLabel("")
	a.output = widget.NewLabel("")
	a.action = widget.NewButtonWithIcon("Record", theme.MediaRecordIcon(), a.toggleRecording)
	a.action.Importance = widget.HighImportance
	title := widget.NewLabelWithStyle("Spytify", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	a.bar = container.NewBorder(nil, nil, container.NewHBox(widget.NewIcon(Logo), title), a.action, container.NewHBox(a.status, widget.NewSeparator(), a.output))
	a.banner = widget.NewLabel("")
	a.banner.Wrapping = fyne.TextWrapWord
	a.banner.Importance = widget.DangerImportance
	a.banner.Hide()

	start := widget.NewButtonWithIcon("Start spytifyd", theme.MediaPlayIcon(), a.StartDaemon)
	start.Importance = widget.HighImportance
	a.offlinePlayer = newPlayerCard(a)
	a.offline = container.NewVScroll(container.NewVBox(
		widget.NewLabelWithStyle("spytifyd is not running", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewLabelWithStyle("The recorder runs on its own, so it keeps recording when this window is closed.", fyne.TextAlignCenter, fyne.TextStyle{}),
		container.NewCenter(start),
		widget.NewLabelWithStyle(opts.Socket, fyne.TextAlignCenter, fyne.TextStyle{Monospace: true}),
		a.offlinePlayer.card,
	))

	a.center = container.NewStack(a.screens[0].content())
	body := container.NewBorder(container.NewVBox(a.bar, a.banner, widget.NewSeparator()), nil,
		container.NewBorder(nil, nil, nil, widget.NewSeparator(), a.sidebar), nil, a.center)
	a.win.SetContent(body)
	a.sidebar.Select(0)

	a.setupTray()
	a.win.SetCloseIntercept(func() {
		if _, tray := a.fa.(desktop.App); tray && a.last.Settings["app.minimize_to_tray"] == true {
			a.win.Hide()
			return
		}
		a.fa.Quit()
	})
	m.OnChange(a.scheduleRender)
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

func (a *App) show(i int) {
	a.current = i
	a.center.Objects = []fyne.CanvasObject{a.screens[i].content()}
	a.renderCenter(a.last)
	a.center.Refresh()
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
	switch {
	case !connected:
		a.status.SetText("Not connected")
	case !s.HaveInfo:
		a.status.SetText("Connecting…")
	case (s.TargetKnown && s.Target.Running) || (!s.TargetKnown && checkOK(s.Info.Checks, "player running")):
		a.status.SetText("● " + s.Info.Target.Process + " running")
	default:
		a.status.SetText("○ " + s.Info.Target.Process + " not running")
	}
	a.output.SetText(outputText(s.Settings))
	switch {
	case !connected:
		a.action.Disable()
	case s.Session == proto.StateStopping:
		a.action.Enable()
		a.action.SetText("Stop now")
		a.action.SetIcon(theme.MediaStopIcon())
	case s.Recording():
		a.action.Enable()
		a.action.SetText("Stop")
		a.action.SetIcon(theme.MediaStopIcon())
	default:
		a.action.Enable()
		a.action.SetText("Record")
		a.action.SetIcon(theme.MediaRecordIcon())
	}
	if s.LastError != nil && s.LastError.Msg != "" {
		a.banner.SetText(s.LastError.Msg)
		a.banner.Show()
	} else {
		a.banner.Hide()
	}
	a.sidebar.Refresh()
	// Texts changed width: lay the bar out again.
	a.bar.Refresh()
	a.renderCenter(s)
}

func (a *App) renderCenter(s model.State) {
	if s.Conn != model.Connected && !s.HaveInfo {
		a.center.Objects = []fyne.CanvasObject{a.offline}
		a.center.Refresh()
		return
	}
	view := a.screens[a.current].content()
	if len(a.center.Objects) != 1 || a.center.Objects[0] != view {
		a.center.Objects = []fyne.CanvasObject{view}
		a.center.Refresh()
	}
	a.screens[a.current].render(s)
	// Rows and checklists changed size: lay the screen out again.
	a.center.Refresh()
}

func checkOK(checks []proto.Check, name string) bool {
	for _, c := range checks {
		if c.Name == name {
			return c.OK
		}
	}
	return false
}

func outputText(settings map[string]any) string {
	format, _ := settings["encode.format"].(string)
	switch format {
	case "":
		return ""
	case "mp3", "aac":
		return fmt.Sprintf("%s %v kbps", upper(format), settings["encode.bitrate_kbps"])
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
