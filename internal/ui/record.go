package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/spytify/spytify/internal/ui/model"
	"github.com/spytify/spytify/pkg/proto"
)

type recordScreen struct {
	app  *App
	root *fyne.Container

	empty     *fyne.Container
	checklist *fyne.Container
	session   *fyne.Container

	card     *fyne.Container
	cover    *canvas.Image
	state    *widget.Label
	title    *widget.Label
	subtitle *widget.Label
	progress *widget.ProgressBar
	elapsed  *widget.Label
	level    *widget.ProgressBar
	skip     *widget.Button
	waiting  *widget.Label

	rows   []model.Row
	list   *widget.List
	totals *widget.Label
	folder string
}

func newRecordScreen(a *App) *recordScreen {
	r := &recordScreen{app: a}

	r.checklist = container.NewVBox()
	record := widget.NewButtonWithIcon("Record", theme.MediaRecordIcon(), a.toggleRecording)
	record.Importance = widget.HighImportance
	r.empty = container.NewVBox(
		widget.NewLabelWithStyle("Ready to record", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		muted("Recording starts with the next track the player plays."),
		r.checklist,
		container.NewHBox(record),
	)

	r.cover = canvas.NewImageFromResource(theme.MediaMusicIcon())
	r.cover.FillMode = canvas.ImageFillContain
	r.cover.SetMinSize(fyne.NewSize(72, 72))
	r.state = widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	r.state.Importance = widget.DangerImportance
	r.title = widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	r.title.Truncation = fyne.TextTruncateEllipsis
	r.subtitle = muted("")
	r.subtitle.Truncation = fyne.TextTruncateEllipsis
	r.progress = widget.NewProgressBar()
	r.progress.TextFormatter = func() string { return "" }
	r.elapsed = muted("")
	r.level = widget.NewProgressBar()
	r.level.TextFormatter = func() string { return "" }
	r.skip = widget.NewButtonWithIcon("Skip", theme.MediaSkipNextIcon(), func() {
		a.do(proto.MethodPlayerControl, proto.PlayerControlParams{Cmd: proto.CmdNext}, nil)
	})
	r.waiting = muted("")
	info := container.NewVBox(r.state, r.title, r.subtitle,
		container.NewBorder(nil, nil, nil, r.elapsed, r.progress),
		container.NewBorder(nil, nil, muted("Level"), nil, r.level))
	r.card = container.NewPadded(container.NewBorder(nil, nil, r.cover, container.NewCenter(r.skip), info))

	r.list = widget.NewList(
		func() int { return len(r.rows) },
		func() fyne.CanvasObject {
			img := canvas.NewImageFromResource(theme.MediaMusicIcon())
			img.FillMode = canvas.ImageFillContain
			img.SetMinSize(fyne.NewSize(40, 40))
			title := widget.NewLabelWithStyle("Title", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
			title.Truncation = fyne.TextTruncateEllipsis
			sub := muted("Artist")
			sub.Truncation = fyne.TextTruncateEllipsis
			chip := widget.NewLabel("Tagged")
			reveal := widget.NewButtonWithIcon("", theme.SearchIcon(), nil)
			play := widget.NewButtonWithIcon("", theme.MediaPlayIcon(), nil)
			return container.NewBorder(nil, nil, img, container.NewHBox(chip, reveal, play), container.NewVBox(title, sub))
		},
		func(id widget.ListItemID, o fyne.CanvasObject) {
			if id >= len(r.rows) {
				return
			}
			row := r.rows[id]
			box := o.(*fyne.Container)
			// Border layout: center first, then the leading and trailing parts.
			text := box.Objects[0].(*fyne.Container)
			img := box.Objects[1].(*canvas.Image)
			right := box.Objects[2].(*fyne.Container)
			text.Objects[0].(*widget.Label).SetText(row.Title)
			text.Objects[1].(*widget.Label).SetText(rowSubtitle(row))
			if res := a.covers.get(row.CoverURL); res != nil {
				img.Resource = res
			} else {
				img.Resource = theme.MediaMusicIcon()
			}
			img.Refresh()
			chip := right.Objects[0].(*widget.Label)
			chip.SetText(row.StatusText())
			chip.Importance = chipImportance(row)
			chip.Refresh()
			revealBtn, playBtn := right.Objects[1].(*widget.Button), right.Objects[2].(*widget.Button)
			if row.Path != "" && (row.Status == model.Saved || row.Status == model.Tagged) {
				path := row.Path
				revealBtn.OnTapped = func() { _ = reveal(path) }
				playBtn.OnTapped = func() { _ = openPath(path) }
				revealBtn.Enable()
				playBtn.Enable()
			} else {
				revealBtn.Disable()
				playBtn.Disable()
			}
			// The chip's text changed width: lay the row out again.
			box.Refresh()
		},
	)

	r.totals = muted("")
	openFolder := widget.NewButtonWithIcon("Open folder", theme.FolderOpenIcon(), func() {
		if r.folder != "" {
			_ = openPath(r.folder)
		}
	})
	footer := container.NewBorder(nil, nil, nil, openFolder, r.totals)
	r.session = container.NewBorder(container.NewVBox(r.card, r.waiting, widget.NewSeparator()), container.NewVBox(widget.NewSeparator(), footer), nil, nil, r.list)
	r.root = container.NewStack(r.empty, r.session)
	return r
}

func rowSubtitle(r model.Row) string {
	parts := []string{r.Artist}
	if r.DurationMs > 0 {
		parts = append(parts, fmtDuration(r.DurationMs))
	} else if r.LengthMs > 0 {
		parts = append(parts, fmtDuration(r.LengthMs))
	}
	if r.Path != "" {
		parts = append(parts, strings.ToUpper(strings.TrimPrefix(filepath.Ext(r.Path), ".")))
	}
	if r.Bytes > 0 {
		parts = append(parts, fmtBytes(r.Bytes))
	}
	return strings.Join(parts, " · ")
}

func chipImportance(r model.Row) widget.Importance {
	switch r.Status {
	case model.Tagged, model.Saved:
		return widget.SuccessImportance
	case model.Skipped:
		return widget.WarningImportance
	}
	return widget.MediumImportance
}

func (r *recordScreen) content() fyne.CanvasObject { return r.root }

func (r *recordScreen) render(s model.State) {
	r.folder, _ = s.Settings["output.dir"].(string)
	r.folder = expandHome(r.folder)
	if !s.Recording() && s.Current == nil && len(s.Rows) == 0 {
		r.renderChecklist(s)
		r.empty.Show()
		r.session.Hide()
		return
	}
	r.empty.Hide()
	r.session.Show()

	if c := s.Current; c != nil {
		r.card.Show()
		r.state.SetText(stateText(s.Session, c))
		r.title.SetText(c.Title)
		sub := c.Artist
		if c.Album != "" {
			sub += " · " + c.Album
		}
		r.subtitle.SetText(sub)
		elapsed := r.app.m.Elapsed(*c)
		if c.LengthMs > 0 {
			r.progress.SetValue(float64(elapsed.Milliseconds()) / float64(c.LengthMs))
			r.elapsed.SetText(fmtDuration(elapsed.Milliseconds()) + " / " + fmtDuration(c.LengthMs))
		} else {
			r.progress.SetValue(0)
			r.elapsed.SetText(fmtDuration(elapsed.Milliseconds()))
		}
		v := (s.Level.RMSDBFS + 60) / 60
		if v < 0 || s.Session != proto.StateRecording {
			v = 0
		}
		r.level.SetValue(v)
		if res := r.app.covers.get(c.CoverURL); res != nil {
			r.cover.Resource = res
		} else {
			r.cover.Resource = theme.MediaMusicIcon()
		}
		r.cover.Refresh()
		r.waiting.Hide()
	} else {
		r.card.Hide()
		r.waiting.SetText(waitingText(s.Session))
		r.waiting.Show()
	}

	r.rows = s.Rows
	r.list.Refresh()
	t := s.Totals()
	r.totals.SetText(fmt.Sprintf("%d saved · %d skipped · %d ads · %s", t.Saved, t.Skipped, t.Ads, fmtBytes(t.Bytes)))
}

func stateText(st proto.SessionState, c *model.Row) string {
	if c.Status == model.Skipped {
		return "PLAYING, NOT RECORDED: " + strings.ToUpper(strings.TrimPrefix(c.StatusText(), "Skipped: "))
	}
	switch st {
	case proto.StatePaused:
		return "PAUSED"
	case proto.StateStopping:
		return "RECORDING, STOPPING AFTER THIS TRACK"
	case proto.StateAd:
		return "ADVERT, NOT RECORDED"
	}
	return "RECORDING"
}

func waitingText(st proto.SessionState) string {
	switch st {
	case proto.StatePaused:
		return "Paused."
	case proto.StateIdle:
		return "Session ended."
	case proto.StateStopping:
		return "Finishing the last files…"
	}
	return "Waiting for the next track to start."
}

func (r *recordScreen) renderChecklist(s model.State) {
	var rows []fyne.CanvasObject
	for _, c := range s.Info.Checks {
		icon := theme.ConfirmIcon()
		if !c.OK {
			icon = theme.ErrorIcon()
		}
		label := widget.NewLabel(c.Name)
		detail := muted(c.Detail)
		detail.Truncation = fyne.TextTruncateEllipsis
		rows = append(rows, container.NewBorder(nil, nil, container.NewHBox(widget.NewIcon(icon), label), nil, detail))
	}
	for _, w := range s.Info.Warnings {
		rows = append(rows, container.NewHBox(widget.NewIcon(theme.WarningIcon()), muted(w)))
	}
	rows = append(rows, layout.NewSpacer())
	r.checklist.Objects = rows
	r.checklist.Refresh()
}
