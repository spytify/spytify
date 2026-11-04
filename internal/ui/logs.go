package ui

import (
	"cmp"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/spytify/spytify/internal/ui/lang"
	"github.com/spytify/spytify/internal/ui/model"
	"github.com/spytify/spytify/pkg/proto"
)

// logLevels are the log filter's choices, levelOf the level each shows.
var (
	logLevels = []string{lang.LevelError, lang.LevelInfo, lang.LevelDebug}
	levelOf   = map[string]proto.LogLevel{lang.LevelError: proto.LogError, lang.LevelInfo: proto.LogInfo, lang.LevelDebug: proto.LogDebug}
)

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
	diagnose *litButton
	diag     *widget.PopUp
	open     bool
	// summary heads the checks, which scroll past checksMaxHeight, above
	// actions: Run the checks again, and Send to the developers when a
	// check fails.
	summary      *widget.Label
	checks       *fyne.Container
	checkList    *checkList
	checksScroll *container.Scroll
	actions      *fyne.Container
	refresh      *button
	lastState    string
	// sent is the state of the checks last sent to the developers.
	sent string
}

func newLogsScreen(a *App) *logsScreen {
	l := &logsScreen{app: a, path: filepath.Join(a.opts.StateDir, "spytifyd.log")}
	l.summary = widget.NewLabel("")
	l.checks = container.NewVBox()
	a.maskPaths(l.checks)
	l.checkList = newCheckList(l.checks)
	l.checksScroll = l.checkList.scroll
	l.refresh = newIconButton(lang.RunChecksAgain, theme.ViewRefreshIcon(), func() {
		a.noteAction("run the checks again")
		go a.refreshChecks()
	})
	l.refresh.Importance = widget.LowImportance
	l.actions = container.NewHBox(l.refresh)
	// A dialog of Fyne's stays where it first showed as the checks under it
	// grow, past the window's edge: this pop-up looks like one, and is
	// centred again.
	title := widget.NewLabelWithStyle(lang.Diagnostics, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	// Its buttons have no background but their focus, as Run the checks
	// again: the checks stand out.
	done := newButton(lang.Close, l.hideDiagnostics)
	done.Importance = widget.LowImportance
	// The checks go from one side to the other, their scroll bar at the
	// dialog's edge, as the lists' is at the window's.
	pad := theme.Padding()
	top := container.New(layout.NewCustomPaddedLayout(pad*2, 0, pad*2, pad*2), container.NewVBox(title, l.summary))
	bottom := container.New(layout.NewCustomPaddedLayout(0, pad*2, pad*2, pad*2), container.NewBorder(nil, nil, l.actions, done))
	l.diag = widget.NewModalPopUp(container.NewBorder(top, bottom, nil, nil, l.checkList), a.win.Canvas())
	l.diagnose = newLitButton(lang.Diagnostics, theme.ConfirmIcon(), widget.LowImportance, a.showDiagnostics)

	l.level = widget.NewCheckGroup(logLevels, func([]string) { l.filter() })
	l.level.Horizontal = true
	l.list = widget.NewList(
		func() int { return len(l.lines) },
		func() fyne.CanvasObject {
			msg := widget.NewLabel("message")
			msg.Wrapping = fyne.TextWrapWord
			a.maskPaths(msg)
			prefix := widget.NewLabelWithStyle("00:00:00 info ", fyne.TextAlignLeading, fyne.TextStyle{Monospace: true})
			prefix.SizeName = theme.SizeNameCaptionText
			// Off the window's sides, in line with the tools above: the
			// list goes to them, its scroll bar at the edge.
			side := 2 * theme.Padding()
			return container.New(layout.NewCustomPaddedLayout(0, 0, side, side), container.NewBorder(nil, nil, prefix, nil, msg))
		},
		func(id widget.ListItemID, o fyne.CanvasObject) {
			if id >= len(l.lines) {
				return
			}
			line := l.lines[id]
			box := o.(*fyne.Container).Objects[0].(*fyne.Container)
			ts := line.TS
			if t, err := time.Parse(time.RFC3339Nano, line.TS); err == nil {
				ts = t.Format("15:04:05")
			}
			prefix := box.Objects[1].(*widget.Label)
			prefix.SetText(ts + " " + padLevel(string(line.Level)))
			if line.Level == proto.LogError {
				prefix.Importance = widget.DangerImportance
			} else {
				prefix.Importance = widget.MediumImportance
			}
			prefix.Refresh()
			box.Objects[0].(*widget.Label).SetText(line.Msg)
			l.list.SetItemHeight(id, o.MinSize().Height)
		},
	)
	// SetSelected runs the callback, which needs the list.
	l.level.SetSelected(logLevels)
	copyAll := newIconButton(lang.Copy, theme.ContentCopyIcon(), func() {
		a.noteAction("copy the log")
		var b strings.Builder
		for _, line := range l.lines {
			b.WriteString(line.TS + " " + string(line.Level) + " " + line.Msg + "\n")
		}
		a.fa.Clipboard().SetContent(b.String())
	})
	copyAll.Importance = widget.LowImportance
	open := newIconButton(lang.OpenLogFile, theme.DocumentIcon(), func() {
		a.noteAction("open the log file")
		_ = openPath(l.path)
	})
	open.Importance = widget.LowImportance
	tools := container.NewBorder(nil, nil, container.NewHBox(widget.NewLabel(lang.Show), l.level), container.NewHBox(copyAll, open, l.diagnose.lit))
	l.root = plain(inReadingOrder(inset(tools), nil, nil, nil, l.list))
	return l
}

// checksMaxHeight is about seven checks: more scroll. diagWidth leaves a
// passing check's detail room beside its name, and a failed check's reason
// few lines under it.
const (
	checksMaxHeight = 300
	diagWidth       = 800
)

// showDiagnostics opens the checks over the window, from any tab, scrolled
// to the one that fails. Opening them is counted once a launch, with the
// checks that fail then.
func (a *App) showDiagnostics() {
	a.noteAction("open diagnostics")
	if !a.diagNoted && a.tel.Enabled() {
		a.diagNoted = true
		a.tel.Log("diagnostics", map[string]any{"failing": failingNames(a.diagChecks(a.last))})
	}
	l := a.screens[tabLogs].(*logsScreen)
	l.renderChecks(a.last)
	l.open = true
	l.diag.Show()
	l.fitDiagnostics()
	l.checksScroll.ScrollToBottom()
	a.win.Canvas().Focus(l.checkList)
	closesOnEscape(a.win, l.hideDiagnostics)
}

func (l *logsScreen) hideDiagnostics() {
	l.open = false
	l.diag.Hide()
}

// fitDiagnostics sizes the dialog to its checks, centred in the window: the
// overlay centres it only when it shows.
func (l *logsScreen) fitDiagnostics() {
	c := l.diag.Canvas.Size()
	width := max(min(diagWidth, c.Width-8*theme.Padding()), l.diag.MinSize().Width)
	// Laid out at its width first: a failed check's reason wraps to it, and
	// only then do the checks know their height. They take it up to
	// checksMaxHeight, then scroll: the dialog stays inside the window.
	l.diag.Resize(fyne.NewSize(width, l.diag.MinSize().Height))
	l.checksScroll.SetMinSize(fyne.NewSize(0, min(l.checks.MinSize().Height, checksMaxHeight)))
	size := fyne.NewSize(width, l.diag.MinSize().Height)
	l.diag.Resize(size)
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

// checkDaemon is the name of the app's own check, model.CheckRecorder, for
// the bug tracker.
const checkDaemon = "spytifyd running"

// diagChecks are the checks the diagnostics show: the app's own, then
// spytifyd's. While it is gone its checks are left out, its last answers
// being no longer true.
func (a *App) diagChecks(s model.State) []proto.Check {
	if s.Conn == model.Connected {
		return append([]proto.Check{{ID: model.CheckRecorder, Name: checkDaemon, OK: true, Detail: s.Info.Version}}, s.Info.Checks...)
	}
	detail := cmp.Or(a.startErr, s.ConnError)
	if a.starting {
		detail = lang.StartingRecorder
	}
	return []proto.Check{{ID: model.CheckRecorder, Name: checkDaemon, Detail: detail}}
}

func (l *logsScreen) renderChecks(s model.State) {
	checks := l.app.diagChecks(s)
	failing := failingChecks(checks)
	// Advice that fails reads with the advice, out of the count.
	counted := len(checks) - len(adviceNames(checks))
	summary := fmt.Sprintf(lang.ChecksPass, counted)
	warnings := s.Info.Warnings
	switch {
	case s.Conn != model.Connected:
		summary, warnings = lang.NothingElseChecked, nil
	case failing > 0:
		summary = fmt.Sprintf(lang.ChecksNeedAttention, failing, counted)
	}
	state := fmt.Sprint(summary, checks, warnings)
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
	for _, c := range checks {
		if !c.OK && c.Advice {
			// The name and its icon stay on the first line of the detail.
			name := container.NewVBox(container.NewHBox(widget.NewIcon(theme.InfoIcon()), widget.NewLabel(model.CheckName(c))))
			advice = append(advice, container.NewBorder(nil, nil, name, nil, wrapped(model.CheckDetail(c, runtime.GOOS))))
			continue
		}
		icon := theme.ConfirmIcon()
		if !c.OK {
			icon = theme.WarningIcon()
		}
		var fix fyne.CanvasObject
		if c.ID == model.CheckRecorder && !c.OK && !l.app.starting {
			start := newRowLitButton(lang.StartRecorder, theme.MediaPlayIcon(), widget.HighImportance, l.app.StartDaemon)
			fix = start.lit
		}
		if c.ID == proto.CheckPlayerRunning && !c.OK {
			open := newRowLitButton(fmt.Sprintf(lang.Open, s.Info.Target.Process), theme.MediaPlayIcon(), widget.HighImportance, l.app.launchPlayer)
			fix = open.lit
		}
		if c.ID == proto.CheckOutputWritable && !c.OK {
			choose := newRowLitButton(lang.ChooseAnotherFolder, theme.FolderOpenIcon(), widget.HighImportance, func() {
				l.hideDiagnostics()
				l.app.showOutputSetting()
			})
			fix = choose.lit
		}
		name := container.NewHBox(widget.NewIcon(icon), widget.NewLabel(model.CheckName(c)))
		detail := model.CheckDetail(c, runtime.GOOS)
		var row fyne.CanvasObject = container.NewBorder(nil, nil, name, fix, clipped(detail))
		if !c.OK && detail != "" {
			// Why it failed goes under the name, whole and in line with it:
			// beside it, cut short, it lost the reason.
			reason := container.New(layout.NewCustomPaddedLayout(0, 0, theme.IconInlineSize()+theme.Padding(), 0), wrapped(detail))
			row = container.New(layout.NewCustomPaddedVBoxLayout(0), container.NewBorder(nil, nil, name, fix), reason)
		}
		if c.OK {
			passed = append(passed, row)
		} else {
			failed = append(failed, row)
		}
	}
	for _, w := range warnings {
		advice = append(advice, container.NewBorder(nil, nil, widget.NewIcon(theme.InfoIcon()), nil, wrapped(w)))
	}
	rows := append(append(passed, advice...), failed...)
	// Stuck on a check: the user may send it to the developers, once for
	// these results, unless they opted out of the bug tracker.
	if failing > 0 && l.app.tel.Available() {
		send := newIconButton(lang.SendToDevelopers, theme.MailSendIcon(), nil)
		send.Importance = widget.LowImportance
		done := func() {
			l.sent = state
			send.SetText(lang.Sent)
			send.Disable()
		}
		send.OnTapped = func() {
			l.app.sendChecks(s, func(ok bool) {
				if !ok {
					send.SetText(lang.NotSent)
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
			l.actions.Objects = append(l.actions.Objects, container.NewCenter(caption(lang.TrackerOptedOut)))
		case l.sent == state:
			done()
		}
	} else {
		l.actions.Objects = []fyne.CanvasObject{l.refresh}
	}
	l.actions.Refresh()
	l.checkList.setRows(rows)
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
	shown := map[proto.LogLevel]bool{}
	for _, lv := range l.level.Selected {
		shown[levelOf[lv]] = true
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
