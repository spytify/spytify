package ui

import (
	"path/filepath"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/spytify/spytify/internal/ui/model"
	"github.com/spytify/spytify/pkg/proto"
)

var levelRank = map[string]int{"error": 0, "info": 1, "debug": 2}

// logsScreen is the v1 console, filtered by level.
type logsScreen struct {
	app   *App
	root  fyne.CanvasObject
	level *widget.Select
	list  *widget.List
	lines []proto.LogEvent
	all   []proto.LogEvent
	path  string
}

func newLogsScreen(a *App) *logsScreen {
	l := &logsScreen{app: a, path: filepath.Join(a.opts.StateDir, "spytifyd.log")}
	l.level = widget.NewSelect([]string{"error", "info", "debug"}, func(string) { l.filter() })
	l.list = widget.NewList(
		func() int { return len(l.lines) },
		func() fyne.CanvasObject {
			msg := widget.NewLabel("message")
			msg.Wrapping = fyne.TextWrapWord
			return container.NewBorder(nil, nil, widget.NewLabelWithStyle("00:00:00 info ", fyne.TextAlignLeading, fyne.TextStyle{Monospace: true}), nil, msg)
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
				prefix.Importance = widget.LowImportance
			}
			prefix.Refresh()
			box.Objects[0].(*widget.Label).SetText(line.Msg)
			l.list.SetItemHeight(id, box.MinSize().Height)
		},
	)
	// SetSelected runs the callback, which needs the list.
	l.level.SetSelected("info")
	copyAll := widget.NewButtonWithIcon("Copy", theme.ContentCopyIcon(), func() {
		var b strings.Builder
		for _, line := range l.lines {
			b.WriteString(line.TS + " " + line.Level + " " + line.Msg + "\n")
		}
		a.fa.Clipboard().SetContent(b.String())
	})
	open := widget.NewButtonWithIcon("Open log file", theme.DocumentIcon(), func() { _ = openPath(l.path) })
	top := container.NewBorder(nil, nil, container.NewHBox(widget.NewLabel("Show"), l.level), container.NewHBox(copyAll, open))
	l.root = container.NewBorder(top, nil, nil, nil, l.list)
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
	l.all = s.Logs
	l.filter()
}

func (l *logsScreen) filter() {
	max := levelRank[l.level.Selected]
	var lines []proto.LogEvent
	for i := len(l.all) - 1; i >= 0; i-- {
		if levelRank[l.all[i].Level] <= max {
			lines = append(lines, l.all[i])
		}
	}
	l.lines = lines
	l.list.Refresh()
}
