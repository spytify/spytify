package ui

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/spytify/spytify/internal/settings"
	"github.com/spytify/spytify/internal/ui/model"
	"github.com/spytify/spytify/pkg/proto"
)

// recordScreen is the session: the track being recorded on top, every file
// of the session below it. Starting and stopping is the app bar's button.
type recordScreen struct {
	app  *App
	root *fyne.Container

	idle       *fyne.Container
	idleTitle  *widget.Label
	idleText   *widget.Label
	attention  *fyne.Container
	openPlayer *widget.Button
	diagnose   *widget.Button
	session    *fyne.Container

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

	rows     []model.Row
	list     *widget.List
	listCard fyne.CanvasObject
	// shape is which parts the screen last showed; it is laid out again
	// only when that changes.
	shape string
	// shown is what the list last drew; refreshing it on every level event
	// would clear the row the pointer hovers.
	shown  string
	noRows fyne.CanvasObject
	clear  *widget.Button
	totals *widget.RichText
	folder string
}

func newRecordScreen(a *App) *recordScreen {
	r := &recordScreen{app: a}

	icon := canvas.NewImageFromResource(Logo)
	icon.FillMode = canvas.ImageFillContain
	icon.SetMinSize(fyne.NewSize(72, 72))
	r.idleTitle = widget.NewLabelWithStyle("", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	r.idleText = widget.NewLabelWithStyle("", fyne.TextAlignCenter, fyne.TextStyle{})
	r.idleText.Wrapping = fyne.TextWrapWord
	hint := widget.NewLabelWithStyle("A track that is already playing when you press Record is skipped: recording starts with the next one, or replay it from the start.",
		fyne.TextAlignCenter, fyne.TextStyle{})
	hint.Wrapping = fyne.TextWrapWord
	hint.SizeName = theme.SizeNameCaptionText
	r.diagnose = widget.NewButtonWithIcon("Open diagnostics", theme.WarningIcon(), func() { a.showTab(tabLogs) })
	r.diagnose.Importance = widget.WarningImportance
	diagnose := r.diagnose
	r.openPlayer = widget.NewButtonWithIcon("Open the player", theme.MediaPlayIcon(), a.launchPlayer)
	r.openPlayer.Importance = widget.HighImportance
	r.attention = container.NewCenter(container.NewHBox(r.openPlayer, diagnose))
	r.idle = container.NewCenter(container.NewGridWrap(fyne.NewSize(460, 280), container.NewVBox(
		container.NewCenter(icon),
		r.idleTitle,
		r.idleText, hint, r.attention,
	)))

	r.cover = canvas.NewImageFromResource(theme.MediaMusicIcon())
	r.cover.FillMode = canvas.ImageFillContain
	r.cover.SetMinSize(fyne.NewSquareSize(liveCover))
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
		a.noteAction("skip")
		a.do(proto.MethodPlayerControl, proto.PlayerControlParams{Cmd: proto.CmdNext}, nil)
	})
	info := container.NewVBox(r.state, r.title, r.subtitle,
		container.NewBorder(nil, nil, nil, r.elapsed, r.progress),
		container.NewBorder(nil, nil, caption("Level"), nil, r.level))
	// The track being recorded is on a card of its own colour, apart from
	// the list.
	pad := theme.Padding()
	live := container.NewBorder(nil, nil, r.cover, container.NewCenter(skip), container.NewVBox(layout.NewSpacer(), info, layout.NewSpacer()))
	r.card = inset(newPanel(colorLive, "", 10, fyne.NewSize(pad*2, pad*2), live))

	r.waiting = widget.NewLabel("")
	// A track already playing when the session starts is not recorded;
	// playing it again from 0:00 makes it the next track.
	r.restart = widget.NewButtonWithIcon("Record this track from the start", theme.MediaReplayIcon(), func() {
		a.noteAction("record this track from the start")
		a.do(proto.MethodPlayerControl, proto.PlayerControlParams{Cmd: proto.CmdRestart}, nil)
	})
	r.waitRow = inset(container.NewHBox(r.waiting, layout.NewSpacer(), r.restart))

	r.list = widget.NewList(
		func() int { return len(r.rows) },
		func() fyne.CanvasObject {
			img := canvas.NewImageFromResource(theme.MediaMusicIcon())
			img.FillMode = canvas.ImageFillContain
			img.SetMinSize(fyne.NewSquareSize(rowCover))
			chip := caption("Skipped: duplicate")
			reveal := widget.NewButtonWithIcon("", theme.SearchIcon(), nil)
			reveal.Importance = widget.LowImportance
			play := widget.NewButtonWithIcon("", theme.MediaPlayIcon(), nil)
			play.Importance = widget.LowImportance
			return container.NewBorder(nil, nil, img, container.NewHBox(container.NewCenter(chip), container.NewCenter(reveal), container.NewCenter(play)),
				rowText())
		},
		func(id widget.ListItemID, o fyne.CanvasObject) {
			if id >= len(r.rows) {
				return
			}
			row := r.rows[id]
			// Border layout: center first, then the leading and trailing parts.
			box := o.(*fyne.Container)
			text := box.Objects[0].(*widget.RichText)
			img := box.Objects[1].(*canvas.Image)
			right := box.Objects[2].(*fyne.Container)
			setRowText(text, row.Title, rowSubtitle(row))
			setCover(img, a.covers.get(row.CoverURL).row)
			chip := right.Objects[0].(*fyne.Container).Objects[0].(*widget.RichText)
			seg := chip.Segments[0].(*widget.TextSegment)
			if c := chipColor(row); seg.Text != row.StatusText() || seg.Style.ColorName != c {
				seg.Text, seg.Style.ColorName = row.StatusText(), c
				chip.Refresh()
			}
			revealBtn := right.Objects[1].(*fyne.Container).Objects[0].(*widget.Button)
			playBtn := right.Objects[2].(*fyne.Container).Objects[0].(*widget.Button)
			if row.Path != "" && (row.Status == model.Saved || row.Status == model.Tagged) {
				path := row.Path
				revealBtn.OnTapped = func() { a.noteAction("show a track in its folder"); _ = reveal(path) }
				playBtn.OnTapped = func() { a.noteAction("play a track"); _ = openPath(path) }
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
		a.noteAction("open folder")
		if r.folder != "" {
			_ = openPath(r.folder)
		}
	})
	openFolder.Importance = widget.LowImportance
	r.clear = widget.NewButtonWithIcon("Clear list", theme.ContentClearIcon(), func() {
		a.noteAction("clear list")
		a.m.ClearRows()
	})
	r.clear.Importance = widget.LowImportance
	footer := container.NewBorder(nil, nil, nil, container.NewHBox(r.clear, openFolder), r.totals)
	r.noRows = emptyState("No files yet", widget.NewLabel("Each track becomes a file here when it ends."))
	r.listCard = listCard(r.list)
	r.session = container.NewBorder(container.NewVBox(r.card, r.waitRow), inset(footer), nil, nil,
		container.NewStack(inset(r.listCard), r.noRows))
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

// chipColor is the status in a soft tone: the row's title is what matters.
func chipColor(r model.Row) fyne.ThemeColorName {
	switch r.Status {
	case model.Tagged, model.Saved:
		return colorSaved
	case model.Skipped:
		return colorSkipped
	}
	return theme.ColorNamePlaceHolder
}

func (r *recordScreen) content() fyne.CanvasObject { return r.root }

func (r *recordScreen) render(s model.State) {
	r.folder, _ = s.Settings["output.dir"].(string)
	r.folder = settings.ExpandHome(r.folder)
	defer r.relayout()
	if !s.Recording() && s.Current == nil && len(s.Rows) == 0 {
		// Until the recorder answers it is starting, with nothing to offer.
		starting := !s.HaveInfo
		setLabel(r.idleTitle, map[bool]string{true: startingText, false: "Ready to record"}[starting])
		show(r.idleText, !starting)
		if starting {
			show(r.attention, false)
			r.idle.Show()
			r.session.Hide()
			return
		}
		player := s.Info.Target.Process
		if player == "" {
			player = "your player"
		}
		setLabel(r.idleText, fmt.Sprintf("Press Record, then play music in %s. Each track becomes its own file in %s.", player, r.folder))
		// A player that is not running is shown in the app bar and offered
		// to open here; the diagnostics are for anything else.
		down := !playerRunning(s)
		r.openPlayer.SetText("Open " + player)
		show(r.openPlayer, down)
		show(r.diagnose, failingChecks(s.Info.Checks) > 0)
		show(r.attention, r.openPlayer.Visible() || r.diagnose.Visible())
		r.idle.Show()
		r.session.Hide()
		return
	}
	r.idle.Hide()
	r.session.Show()

	if c := s.Current; c != nil {
		r.card.Show()
		r.waitRow.Hide()
		setLabel(r.state, stateText(s.Session, c))
		setLabel(r.title, c.Title)
		sub := c.Artist
		if c.Album != "" {
			sub += " · " + c.Album
		}
		if src := sourceText(c.SourceRate, c.SourceChannels); src != "" {
			sub += " · " + src
		}
		setCaption(r.subtitle, sub)
		setCover(r.cover, r.app.covers.get(c.CoverURL).live)
	} else {
		r.card.Hide()
		setLabel(r.waiting, waitingText(s.Session))
		show(r.restart, s.Session == proto.StateWaiting)
		r.waitRow.Show()
	}
	r.renderLive(s.Current, s.Session, s.Level)

	r.rows = s.Rows
	if shown := fmt.Sprint(r.rows, r.app.covers.count()); shown != r.shown {
		r.shown = shown
		r.listCard.Refresh()
	}
	show(r.noRows, len(r.rows) == 0)
	show(r.listCard, len(r.rows) > 0)
	if len(r.rows) == 0 {
		r.clear.Disable()
	} else {
		r.clear.Enable()
	}
	t := s.Totals()
	setCaption(r.totals, fmt.Sprintf("%d saved · %d skipped · %d ads · %s", t.Saved, t.Skipped, t.Ads, fmtBytes(t.Bytes)))
}

// renderLive moves the progress, the elapsed time and the meter, which the
// level events refresh ten times a second without a full render.
func (r *recordScreen) renderLive(c *model.Row, session proto.SessionState, level proto.LevelEvent) {
	if c == nil {
		return
	}
	elapsed := r.app.m.Elapsed(*c)
	if c.LengthMs > 0 {
		r.progress.SetValue(float64(elapsed.Milliseconds()) / float64(c.LengthMs))
		setCaption(r.elapsed, fmtDuration(elapsed.Milliseconds())+" / "+fmtDuration(c.LengthMs))
	} else {
		r.progress.SetValue(0)
		setCaption(r.elapsed, fmtDuration(elapsed.Milliseconds()))
	}
	v := (level.RMSDBFS + 60) / 60
	if v < 0 || session != proto.StateRecording {
		v = 0
	}
	r.level.SetValue(v)
}

// relayout lays the screen out again when a part appeared or went away.
func (r *recordScreen) relayout() {
	shape := fmt.Sprint(r.idle.Visible(), r.attention.Visible(), r.openPlayer.Visible(), r.diagnose.Visible(), r.card.Visible(), r.waitRow.Visible(), r.restart.Visible(), r.noRows.Visible())
	if shape != r.shape {
		r.shape = shape
		r.root.Refresh()
	}
}

func show(o fyne.CanvasObject, visible bool) {
	if visible {
		o.Show()
	} else {
		o.Hide()
	}
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

// sourceText is the captured stream: "44.1 kHz stereo".
func sourceText(rate, channels int) string {
	if rate <= 0 {
		return ""
	}
	khz := strconv.FormatFloat(float64(rate)/1000, 'f', -1, 64) + " kHz"
	switch channels {
	case 1:
		return khz + " mono"
	case 2:
		return khz + " stereo"
	}
	return fmt.Sprintf("%s %d channels", khz, channels)
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
