package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	ttwidget "github.com/dweymouth/fyne-tooltip/widget"

	"github.com/spytify/spytify/internal/ui/model"
	"github.com/spytify/spytify/pkg/proto"
)

var logLevels = []string{"error", "info", "debug"}

// logsScreen is the v1 console, filtered by level, under the diagnostics:
// the checks core.info runs, folded away while they all pass.
type logsScreen struct {
	app   *App
	root  fyne.CanvasObject
	level *widget.CheckGroup
	list  *widget.List
	lines []proto.LogEvent
	all   []proto.LogEvent
	path  string

	diag      *widget.Accordion
	checks    *fyne.Container
	lastState string
	// sent is the state of the checks last sent to the developers.
	sent string
}

func newLogsScreen(a *App) *logsScreen {
	l := &logsScreen{app: a, path: filepath.Join(a.opts.StateDir, "spytifyd.log")}
	l.checks = container.NewVBox()
	refresh := widget.NewButtonWithIcon("Run the checks again", theme.ViewRefreshIcon(), func() {
		a.noteAction("run the checks again")
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			_ = a.link.RefreshInfo(ctx)
		}()
	})
	refresh.Importance = widget.LowImportance
	l.diag = widget.NewAccordion(widget.NewAccordionItem("Diagnostics", container.NewVBox(l.checks, container.NewHBox(refresh))))

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
	tools := container.NewBorder(nil, nil, container.NewHBox(widget.NewLabel("Show"), l.level), container.NewHBox(copyAll, open))
	l.root = container.NewBorder(container.NewVBox(l.diag, widget.NewSeparator(), tools), nil, nil, nil, l.list)
	return l
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
	title := fmt.Sprintf("Diagnostics · all %d checks pass", len(s.Info.Checks))
	if failing > 0 {
		title = fmt.Sprintf("Diagnostics · %d of %d checks need attention", failing, len(s.Info.Checks))
	}
	state := fmt.Sprint(title, s.Info.Checks, s.Info.Warnings)
	// The send button follows the bug tracker's switch too.
	on, _ := l.app.privacy(trackerKey).(bool)
	redraw := fmt.Sprint(state, on)
	if redraw == l.lastState {
		return
	}
	l.lastState = redraw
	var rows []fyne.CanvasObject
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
		rows = append(rows, container.NewBorder(nil, nil, container.NewHBox(widget.NewIcon(icon), widget.NewLabel(checkLabel(c))), fix, clipped(c.Detail)))
	}
	for _, w := range s.Info.Warnings {
		rows = append(rows, container.NewBorder(nil, nil, widget.NewIcon(theme.ErrorIcon()), nil, wrapped(w)))
	}
	// Stuck on a check: the user may send it to the developers, once for
	// these results, unless they opted out of the bug tracker.
	if failing > 0 && l.app.tel.Available() {
		send := ttwidget.NewButtonWithIcon("Send to the developers", theme.MailSendIcon(), nil)
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
		switch {
		case !on:
			send.Disable()
			send.SetToolTip(optedOut)
		case l.sent == state:
			done()
		}
		rows = append(rows, container.NewHBox(send))
	}
	l.checks.Objects = rows
	l.checks.Refresh()
	l.diag.Items[0].Title = title
	// Fold the checks away while they all pass; open them when one fails.
	if failing > 0 {
		l.diag.Open(0)
	} else {
		l.diag.Close(0)
	}
	l.diag.Refresh()
	// The diagnostics changed height: lay the log out below them again.
	l.root.Refresh()
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
