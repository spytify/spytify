package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/spytify/spytify/internal/ui/model"
	"github.com/spytify/spytify/pkg/proto"
)

var logLevels = []string{"error", "info", "debug"}

// logsScreen is the v1 console, filtered by level, with the diagnostics a
// button away: the checks core.info runs, in a dialog over the window.
type logsScreen struct {
	app   *App
	root  fyne.CanvasObject
	level *widget.CheckGroup
	list  *widget.List
	lines []proto.LogEvent
	all   []proto.LogEvent
	path  string

	// diagnose opens diag, and turns to a warning while a check fails.
	// open is whether diag shows: its checks then follow the model from
	// any tab.
	diagnose *widget.Button
	diag     *widget.PopUp
	open     bool
	// summary heads the checks, which scroll past checksMaxHeight, above
	// actions: Run the checks again, and Send to the developers when a
	// check fails.
	summary      *widget.Label
	checks       *fyne.Container
	checksScroll *container.Scroll
	actions      *fyne.Container
	refresh      *widget.Button
	lastState    string
	// sent is the state of the checks last sent to the developers.
	sent string
}

func newLogsScreen(a *App) *logsScreen {
	l := &logsScreen{app: a, path: filepath.Join(a.opts.StateDir, "spytifyd.log")}
	l.summary = widget.NewLabel("")
	l.checks = container.NewVBox()
	l.checksScroll = container.NewVScroll(l.checks)
	l.refresh = widget.NewButtonWithIcon("Run the checks again", theme.ViewRefreshIcon(), func() {
		a.noteAction("run the checks again")
		go a.refreshChecks()
	})
	l.refresh.Importance = widget.LowImportance
	l.actions = container.NewHBox(l.refresh)
	// A dialog of Fyne's stays where it first showed as the checks under it
	// grow, past the window's edge: this pop-up looks like one, and is
	// centred again.
	title := widget.NewLabelWithStyle("Diagnostics", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	done := widget.NewButton("Close", l.hideDiagnostics)
	pad := theme.Padding()
	body := container.NewBorder(container.NewVBox(title, l.summary), container.NewBorder(nil, nil, l.actions, done), nil, nil, l.checksScroll)
	l.diag = widget.NewModalPopUp(container.New(layout.NewCustomPaddedLayout(pad*2, pad*2, pad*2, pad*2), body), a.win.Canvas())
	l.diagnose = widget.NewButtonWithIcon("Diagnostics", theme.ConfirmIcon(), a.showDiagnostics)
	l.diagnose.Importance = widget.LowImportance

	l.level = widget.NewCheckGroup(logLevels, func([]string) { l.filter() })
	l.level.Horizontal = true
	l.list = widget.NewList(
		func() int { return len(l.lines) },
		func() fyne.CanvasObject {
			msg := widget.NewLabel("message")
			msg.Wrapping = fyne.TextWrapWord
			prefix := widget.NewLabelWithStyle("00:00:00 info ", fyne.TextAlignLeading, fyne.TextStyle{Monospace: true})
			prefix.SizeName = theme.SizeNameCaptionText
			return container.NewBorder(nil, nil, prefix, nil, msg)
		},
		func(id widget.ListItemID, o fyne.CanvasObject) {
			if id >= len(l.lines) {
				return
			}
			line := l.lines[id]
			box := o.(*fyne.Container)
			ts := line.TS
			if t, err := time.Parse(time.RFC3339Nano, line.TS); err == nil {
				ts = t.Format("15:04:05")
			}
			prefix := box.Objects[1].(*widget.Label)
			prefix.SetText(ts + " " + padLevel(line.Level))
			if line.Level == "error" {
				prefix.Importance = widget.DangerImportance
			} else {
				prefix.Importance = widget.MediumImportance
			}
			prefix.Refresh()
			box.Objects[0].(*widget.Label).SetText(line.Msg)
			l.list.SetItemHeight(id, box.MinSize().Height)
		},
	)
	// SetSelected runs the callback, which needs the list.
	l.level.SetSelected(logLevels)
	copyAll := widget.NewButtonWithIcon("Copy", theme.ContentCopyIcon(), func() {
		a.noteAction("copy the log")
		var b strings.Builder
		for _, line := range l.lines {
			b.WriteString(line.TS + " " + line.Level + " " + line.Msg + "\n")
		}
		a.fa.Clipboard().SetContent(b.String())
	})
	copyAll.Importance = widget.LowImportance
	open := widget.NewButtonWithIcon("Open log file", theme.DocumentIcon(), func() {
		a.noteAction("open the log file")
		_ = openPath(l.path)
	})
	open.Importance = widget.LowImportance
	tools := container.NewBorder(nil, nil, container.NewHBox(widget.NewLabel("Show"), l.level), container.NewHBox(copyAll, open, l.diagnose))
	l.root = plain(margin(container.NewBorder(tools, nil, nil, nil, l.list)))
	return l
}

// checksMaxHeight is about seven checks: more scroll. diagWidth leaves a
// check's detail room beside its name and the button that fixes it.
const (
	checksMaxHeight = 300
	diagWidth       = 640
)

// showDiagnostics opens the checks over the window, from any tab, scrolled
// to the one that fails. Opening them is counted once a launch, with the
// checks that fail then.
func (a *App) showDiagnostics() {
	a.noteAction("open diagnostics")
	if !a.diagNoted && a.tel.Enabled() {
		a.diagNoted = true
		a.tel.Log("diagnostics", map[string]any{"failing": failingNames(a.last.Info.Checks)})
	}
	l := a.screens[tabLogs].(*logsScreen)
	l.renderChecks(a.last)
	l.open = true
	l.diag.Show()
	l.fitDiagnostics()
	l.checksScroll.ScrollToBottom()
}

func (l *logsScreen) hideDiagnostics() {
	l.open = false
	l.diag.Hide()
}

// fitDiagnostics sizes the dialog to its checks, centred in the window: the
// overlay centres it only when it shows.
func (l *logsScreen) fitDiagnostics() {
	size := fyne.NewSize(diagWidth, l.diag.MinSize().Height)
	l.diag.Resize(size)
	c := l.diag.Canvas.Size()
	l.diag.Move(fyne.NewPos((c.Width-size.Width)/2, (c.Height-size.Height)/2))
}

func padLevel(s string) string {
	for len(s) < 5 {
		s += " "
	}
	return s
}

func (l *logsScreen) content() fyne.CanvasObject { return l.root }

func (l *logsScreen) render(s model.State) {
	l.renderChecks(s)
	l.all = s.Logs
	l.filter()
}

func (l *logsScreen) renderChecks(s model.State) {
	failing := failingChecks(s.Info.Checks)
	summary := fmt.Sprintf("All %d checks pass.", len(s.Info.Checks))
	if failing > 0 {
		summary = fmt.Sprintf("%d of %d checks need attention.", failing, len(s.Info.Checks))
	}
	state := fmt.Sprint(summary, s.Info.Checks, s.Info.Warnings)
	// The send button follows the bug tracker's switch too.
	on, _ := l.app.privacy(trackerKey).(bool)
	redraw := fmt.Sprint(state, on)
	if redraw == l.lastState {
		return
	}
	l.lastState = redraw
	// What needs attention comes last, the list scrolled to it: the checks
	// that pass, the advice, then the checks that fail.
	var failed, advice, passed []fyne.CanvasObject
	for _, c := range s.Info.Checks {
		icon := theme.ConfirmIcon()
		if !c.OK {
			icon = theme.WarningIcon()
		}
		var fix fyne.CanvasObject
		if c.Name == "player running" && !c.OK {
			open := widget.NewButtonWithIcon("Open "+s.Info.Target.Process, theme.MediaPlayIcon(), l.app.launchPlayer)
			open.Importance = widget.HighImportance
			fix = open
		}
		if c.Name == proto.CheckOutputWritable && !c.OK {
			choose := widget.NewButtonWithIcon("Choose another folder", theme.FolderOpenIcon(), func() {
				l.hideDiagnostics()
				l.app.showOutputSetting()
			})
			choose.Importance = widget.HighImportance
			fix = choose
		}
		row := container.NewBorder(nil, nil, container.NewHBox(widget.NewIcon(icon), widget.NewLabel(checkLabel(c))), fix, clipped(c.Detail))
		if c.OK {
			passed = append(passed, row)
		} else {
			failed = append(failed, row)
		}
	}
	for _, w := range s.Info.Warnings {
		advice = append(advice, container.NewBorder(nil, nil, widget.NewIcon(theme.InfoIcon()), nil, wrapped(w)))
	}
	rows := append(append(passed, advice...), failed...)
	// Stuck on a check: the user may send it to the developers, once for
	// these results, unless they opted out of the bug tracker.
	if failing > 0 && l.app.tel.Available() {
		send := widget.NewButtonWithIcon("Send to the developers", theme.MailSendIcon(), nil)
		done := func() {
			l.sent = state
			send.SetText("Sent")
			send.Disable()
		}
		send.OnTapped = func() {
			l.app.sendChecks(s, func(ok bool) {
				if !ok {
					send.SetText("Not sent, try again")
					return
				}
				done()
			})
		}
		l.actions.Objects = []fyne.CanvasObject{l.refresh, send}
		switch {
		case !on:
			// No tooltip shows in a pop-up: the reason is written out.
			send.Disable()
			l.actions.Objects = append(l.actions.Objects, container.NewCenter(caption(optedOut)))
		case l.sent == state:
			done()
		}
	} else {
		l.actions.Objects = []fyne.CanvasObject{l.refresh}
	}
	l.actions.Refresh()
	l.checks.Objects = lines(rows)
	l.checks.Refresh()
	// The checks take what they need up to checksMaxHeight, then scroll:
	// the dialog stays inside the window.
	l.checksScroll.SetMinSize(fyne.NewSize(0, min(l.checks.MinSize().Height, checksMaxHeight)))
	l.summary.SetText(summary)
	if l.open {
		l.fitDiagnostics()
	}
	l.diagnose.Icon, l.diagnose.Importance = theme.ConfirmIcon(), widget.LowImportance
	if failing > 0 {
		l.diagnose.Icon, l.diagnose.Importance = theme.WarningIcon(), widget.WarningImportance
	}
	l.diagnose.Refresh()
	l.checksScroll.ScrollToBottom()
}

func (l *logsScreen) filter() {
	shown := map[string]bool{}
	for _, lv := range l.level.Selected {
		shown[lv] = true
	}
	var lines []proto.LogEvent
	for i := len(l.all) - 1; i >= 0; i-- {
		if shown[l.all[i].Level] {
			lines = append(lines, l.all[i])
		}
	}
	l.lines = lines
	l.list.Refresh()
}

// checkLabel names a check as it stands: a failed "ffmpeg found" reads
// "ffmpeg not found". Code keeps finding checks by their names.
func checkLabel(c proto.Check) string {
	if c.OK {
		return c.Name
	}
	for _, word := range []string{"found", "running", "configured", "writable"} {
		if strings.HasSuffix(c.Name, " "+word) {
			return strings.TrimSuffix(c.Name, word) + "not " + word
		}
	}
	return c.Name
}
