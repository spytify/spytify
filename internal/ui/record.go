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

// recordScreen is the session: the track being recorded on top, every file
// of the session below it. Starting and stopping is the app bar's button.
type recordScreen struct {
	app  *App
	root *fyne.Container

	idle      *fyne.Container
	idleText  *widget.Label
	attention *fyne.Container
	session   *fyne.Container

	card     *fyne.Container
	cover    *canvas.Image
	state    *widget.Label
	title    *widget.Label
	subtitle *widget.RichText
	progress *bar
	elapsed  *widget.RichText
	level    *bar

	waitRow *fyne.Container
	waiting *widget.Label
	restart *widget.Button

	rows   []model.Row
	list   *widget.List
	noRows fyne.CanvasObject
	totals *widget.RichText
	folder string
}

func newRecordScreen(a *App) *recordScreen {
	r := &recordScreen{app: a}

	icon := canvas.NewImageFromResource(Logo)
	icon.FillMode = canvas.ImageFillContain
	icon.SetMinSize(fyne.NewSize(72, 72))
	r.idleText = widget.NewLabelWithStyle("", fyne.TextAlignCenter, fyne.TextStyle{})
	r.idleText.Wrapping = fyne.TextWrapWord
	hint := widget.NewLabelWithStyle("A track that is already playing when you press Record is skipped: recording starts with the next one, or replay it from the start.",
		fyne.TextAlignCenter, fyne.TextStyle{})
	hint.Wrapping = fyne.TextWrapWord
	hint.SizeName = theme.SizeNameCaptionText
	diagnose := widget.NewButtonWithIcon("Open diagnostics", theme.WarningIcon(), func() { a.showTab(tabLogs) })
	diagnose.Importance = widget.WarningImportance
	r.attention = container.NewCenter(diagnose)
	r.idle = container.NewCenter(container.NewGridWrap(fyne.NewSize(460, 280), container.NewVBox(
		container.NewCenter(icon),
		widget.NewLabelWithStyle("Ready to record", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		r.idleText, hint, r.attention,
	)))

	r.cover = canvas.NewImageFromResource(theme.MediaMusicIcon())
	r.cover.FillMode = canvas.ImageFillContain
	r.cover.SetMinSize(fyne.NewSize(96, 96))
	r.state = widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	r.state.SizeName = theme.SizeNameCaptionText
	r.state.Importance = widget.DangerImportance
	r.title = widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	r.title.SizeName = theme.SizeNameSubHeadingText
	r.title.Truncation = fyne.TextTruncateEllipsis
	r.subtitle = clipped("")
	r.progress = newBar(theme.ColorNamePrimary)
	r.elapsed = caption("0:00 / 0:00")
	r.level = newBar(theme.ColorNameSuccess)
	skip := widget.NewButtonWithIcon("Skip", theme.MediaSkipNextIcon(), func() {
		a.do(proto.MethodPlayerControl, proto.PlayerControlParams{Cmd: proto.CmdNext}, nil)
	})
	info := container.NewVBox(r.state, r.title, r.subtitle,
		container.NewBorder(nil, nil, nil, r.elapsed, r.progress),
		container.NewBorder(nil, nil, caption("Level"), nil, r.level))
	r.card = container.NewPadded(container.NewBorder(nil, nil, r.cover, container.NewCenter(skip), container.NewVBox(layout.NewSpacer(), info, layout.NewSpacer())))

	r.waiting = widget.NewLabel("")
	// A track already playing when the session starts is not recorded;
	// playing it again from 0:00 makes it the next track.
	r.restart = widget.NewButtonWithIcon("Record this track from the start", theme.MediaReplayIcon(), func() {
		a.do(proto.MethodPlayerControl, proto.PlayerControlParams{Cmd: proto.CmdRestart}, nil)
	})
	r.waitRow = container.NewPadded(container.NewHBox(r.waiting, layout.NewSpacer(), r.restart))

	r.list = widget.NewList(
		func() int { return len(r.rows) },
		func() fyne.CanvasObject {
			img := canvas.NewImageFromResource(theme.MediaMusicIcon())
			img.FillMode = canvas.ImageFillContain
			img.SetMinSize(fyne.NewSize(40, 40))
			title := widget.NewLabelWithStyle("Title", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
			title.Truncation = fyne.TextTruncateEllipsis
			chip := widget.NewLabel("Skipped: duplicate")
			chip.SizeName = theme.SizeNameCaptionText
			reveal := widget.NewButtonWithIcon("", theme.SearchIcon(), nil)
			reveal.Importance = widget.LowImportance
			play := widget.NewButtonWithIcon("", theme.MediaPlayIcon(), nil)
			play.Importance = widget.LowImportance
			return container.NewBorder(nil, nil, img, container.NewHBox(container.NewCenter(chip), container.NewCenter(reveal), container.NewCenter(play)),
				container.NewVBox(title, clipped("Artist")))
		},
		func(id widget.ListItemID, o fyne.CanvasObject) {
			if id >= len(r.rows) {
				return
			}
			row := r.rows[id]
			// Border layout: center first, then the leading and trailing parts.
			box := o.(*fyne.Container)
			text := box.Objects[0].(*fyne.Container)
			img := box.Objects[1].(*canvas.Image)
			right := box.Objects[2].(*fyne.Container)
			text.Objects[0].(*widget.Label).SetText(row.Title)
			setCaption(text.Objects[1].(*widget.RichText), rowSubtitle(row))
			if res := a.covers.get(row.CoverURL); res != nil {
				img.Resource = res
			} else {
				img.Resource = theme.MediaMusicIcon()
			}
			img.Refresh()
			chip := right.Objects[0].(*fyne.Container).Objects[0].(*widget.Label)
			chip.SetText(row.StatusText())
			chip.Importance = chipImportance(row)
			chip.Refresh()
			revealBtn := right.Objects[1].(*fyne.Container).Objects[0].(*widget.Button)
			playBtn := right.Objects[2].(*fyne.Container).Objects[0].(*widget.Button)
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

	r.totals = caption("")
	openFolder := widget.NewButtonWithIcon("Open folder", theme.FolderOpenIcon(), func() {
		if r.folder != "" {
			_ = openPath(r.folder)
		}
	})
	openFolder.Importance = widget.LowImportance
	footer := container.NewBorder(nil, nil, nil, openFolder, r.totals)
	r.noRows = emptyState("No files yet", widget.NewLabel("Each track becomes a file here when it ends."))
	r.session = container.NewBorder(container.NewVBox(r.card, r.waitRow, widget.NewSeparator()), container.NewVBox(widget.NewSeparator(), footer), nil, nil,
		container.NewStack(r.list, r.noRows))
	r.root = container.NewStack(r.idle, r.session)
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
		player := s.Info.Target.Process
		if player == "" {
			player = "your player"
		}
		r.idleText.SetText(fmt.Sprintf("Press Record, then play music in %s. Each track becomes its own file in %s.", player, r.folder))
		if failingChecks(s.Info.Checks) > 0 {
			r.attention.Show()
		} else {
			r.attention.Hide()
		}
		r.idle.Show()
		r.session.Hide()
		return
	}
	r.idle.Hide()
	r.session.Show()

	if c := s.Current; c != nil {
		r.card.Show()
		r.waitRow.Hide()
		r.state.SetText(stateText(s.Session, c))
		r.title.SetText(c.Title)
		sub := c.Artist
		if c.Album != "" {
			sub += " · " + c.Album
		}
		setCaption(r.subtitle, sub)
		elapsed := r.app.m.Elapsed(*c)
		if c.LengthMs > 0 {
			r.progress.SetValue(float64(elapsed.Milliseconds()) / float64(c.LengthMs))
			setCaption(r.elapsed, fmtDuration(elapsed.Milliseconds())+" / "+fmtDuration(c.LengthMs))
		} else {
			r.progress.SetValue(0)
			setCaption(r.elapsed, fmtDuration(elapsed.Milliseconds()))
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
	} else {
		r.card.Hide()
		r.waiting.SetText(waitingText(s.Session))
		if s.Session == proto.StateWaiting {
			r.restart.Show()
		} else {
			r.restart.Hide()
		}
		r.waitRow.Show()
	}

	r.rows = s.Rows
	r.list.Refresh()
	if len(r.rows) == 0 {
		r.noRows.Show()
	} else {
		r.noRows.Hide()
	}
	t := s.Totals()
	setCaption(r.totals, fmt.Sprintf("%d saved · %d skipped · %d ads · %s", t.Saved, t.Skipped, t.Ads, fmtBytes(t.Bytes)))
}

func stateText(st proto.SessionState, c *model.Row) string {
	if c.Status == model.Skipped {
		return "PLAYING, NOT RECORDED: " + strings.ToUpper(strings.TrimPrefix(c.StatusText(), "Skipped: "))
	}
	switch st {
	case proto.StatePaused:
		return "PAUSED"
	case proto.StateStopping:
		return "RECORDING · STOPS AFTER THIS TRACK"
	case proto.StateAd:
		return "ADVERT, NOT RECORDED"
	}
	return "● RECORDING"
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
