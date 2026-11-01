package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/spytify/spytify/internal/encode"
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
	hint       *widget.Label
	attention  *fyne.Container
	openPlayer *litButton
	diagnose   *litButton
	session    *fyne.Container

	card  *fyne.Container
	cover *canvas.Image
	// track is the state, the title, the artist and album, then the
	// format, one line each.
	track    *widget.RichText
	progress *bar
	elapsed  *widget.RichText
	level    *bar

	waitRow *fyne.Container
	waiting *widget.Label
	// launch and play are what a waiting session needs from the player:
	// to run, then to play.
	launch  *litButton
	play    *litButton
	restart *button
	// blocked leads to the diagnostics once a session ended for want of
	// an output folder.
	blocked *litButton

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
	clear  *button
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
	r.hint = widget.NewLabelWithStyle("A track that is already playing when you press Record is skipped: recording starts with the next one, or replay it from the start.",
		fyne.TextAlignCenter, fyne.TextStyle{})
	r.hint.Wrapping = fyne.TextWrapWord
	r.hint.SizeName = theme.SizeNameCaptionText
	r.diagnose = newLitButton("Open diagnostics", theme.WarningIcon(), widget.WarningImportance, a.showDiagnostics)
	diagnose := r.diagnose
	r.openPlayer = newLitButton("Open the player", theme.MediaPlayIcon(), widget.HighImportance, a.launchPlayer)
	r.attention = container.NewCenter(container.NewHBox(r.openPlayer.lit, diagnose.lit))
	r.idle = container.NewCenter(container.NewGridWrap(fyne.NewSize(460, 280), container.NewVBox(
		container.NewCenter(icon),
		r.idleTitle,
		r.idleText, r.hint, r.attention,
	)))

	r.cover = canvas.NewImageFromResource(theme.MediaMusicIcon())
	r.cover.FillMode = canvas.ImageFillContain
	r.cover.SetMinSize(fyne.NewSquareSize(liveCover))
	// One block, as a list row's: stacked widgets would each add their
	// padding between the lines.
	r.track = widget.NewRichText(
		&widget.TextSegment{Style: widget.RichTextStyle{ColorName: theme.ColorNameError, SizeName: theme.SizeNameCaptionText, TextStyle: fyne.TextStyle{Bold: true}}},
		&widget.TextSegment{Style: widget.RichTextStyle{ColorName: theme.ColorNameForeground, SizeName: theme.SizeNameSubHeadingText, TextStyle: fyne.TextStyle{Bold: true}}},
		&widget.TextSegment{Style: widget.RichTextStyle{ColorName: theme.ColorNamePlaceHolder, SizeName: theme.SizeNameCaptionText}},
	)
	r.track.Truncation = fyne.TextTruncateEllipsis
	a.maskPaths(r.track)
	r.progress = newBar(theme.ColorNamePrimary)
	r.elapsed = caption("0:00 / 0:00")
	r.level = newBar(theme.ColorNameSuccess)
	skip := newIconButton("Skip", theme.MediaSkipNextIcon(), func() {
		a.noteAction("skip")
		a.do(proto.MethodPlayerControl, proto.PlayerControlParams{Cmd: proto.CmdNext}, nil)
	})
	// Length and Level are a pair of rows as close as two lines of text,
	// their bars starting and ending at one x.
	meters := container.NewBorder(nil, nil, nil, tight(r.elapsed), container.New(layout.NewFormLayout(),
		tight(caption("Length")), r.progress,
		tight(caption("Level")), r.level))
	info := container.NewVBox(r.track, meters)
	// The track being recorded is on a card of its own colour, apart from
	// the list.
	pad := theme.Padding()
	live := container.NewBorder(nil, nil, r.cover, container.NewCenter(skip), container.NewVBox(layout.NewSpacer(), info, layout.NewSpacer()))
	// Rounder than the list's card: on a box this big, the same radius looks
	// square.
	r.card = inset(newPanel(colorLive, colorCardLine, 16, fyne.NewSize(pad*2, pad*2), live))

	r.waiting = widget.NewLabel("")
	r.launch = newLitButton("Open the player", theme.MediaPlayIcon(), widget.HighImportance, a.launchPlayer)
	r.play = newLitButton("Play", theme.MediaPlayIcon(), widget.HighImportance, func() {
		a.noteAction("play in the player")
		a.do(proto.MethodPlayerControl, proto.PlayerControlParams{Cmd: proto.CmdPlay}, nil)
	})
	// A track already playing when the session starts is not recorded;
	// playing it again from 0:00 makes it the next track.
	r.restart = newIconButton("Record this track from the start", theme.MediaReplayIcon(), func() {
		a.noteAction("record this track from the start")
		a.do(proto.MethodPlayerControl, proto.PlayerControlParams{Cmd: proto.CmdRestart}, nil)
	})
	r.blocked = newLitButton("Open diagnostics", theme.WarningIcon(), widget.WarningImportance, a.showDiagnostics)
	r.waitRow = inset(container.NewHBox(r.waiting, layout.NewSpacer(), r.launch.lit, r.play.lit, r.restart, r.blocked.lit))

	r.list = widget.NewList(
		func() int { return len(r.rows) },
		func() fyne.CanvasObject {
			chip := caption("Skipped: duplicate")
			keep := newIconButton("Keep anyway", theme.DocumentSaveIcon(), nil)
			keep.Hide()
			reveal := newIconButton("", theme.SearchIcon(), nil)
			reveal.Importance = widget.LowImportance
			play := newIconButton("", theme.MediaPlayIcon(), nil)
			play.Importance = widget.LowImportance
			// The title, the artist, then the length, format and size.
			return trackRow(3, container.NewHBox(container.NewCenter(chip), container.NewCenter(keep), container.NewCenter(reveal), container.NewCenter(play)))
		},
		func(id widget.ListItemID, o fyne.CanvasObject) {
			if id >= len(r.rows) {
				return
			}
			row := r.rows[id]
			// Border layout: center first, then the leading and trailing parts.
			box := o.(*fyne.Container)
			right := box.Objects[2].(*fyne.Container)
			setTrackRow(box, a.covers.get(row.CoverURL).row, row.Title, row.Artist, rowDetails(row))
			chip := right.Objects[0].(*fyne.Container).Objects[0].(*widget.RichText)
			seg := chip.Segments[0].(*widget.TextSegment)
			if c := chipColor(row); seg.Text != row.StatusText() || seg.Style.ColorName != c {
				seg.Text, seg.Style.ColorName = row.StatusText(), c
				chip.Refresh()
			}
			keepBtn := right.Objects[1].(*fyne.Container).Objects[0].(*button)
			revealBtn := right.Objects[2].(*fyne.Container).Objects[0].(*button)
			playBtn := right.Objects[3].(*fyne.Container).Objects[0].(*button)
			if row.Keepable {
				id := row.ID
				keepBtn.OnTapped = func() { a.keep(id) }
				keepBtn.Show()
			} else {
				keepBtn.Hide()
			}
			if row.Path != "" && row.Status == model.Saved {
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
	openFolder := newIconButton("Open folder", theme.FolderOpenIcon(), func() {
		a.noteAction("open folder")
		if r.folder != "" {
			_ = openPath(r.folder)
		}
	})
	openFolder.Importance = widget.LowImportance
	r.clear = newIconButton("Clear list", theme.ContentClearIcon(), func() {
		a.noteAction("clear list")
		a.m.ClearRows()
	})
	r.clear.Importance = widget.LowImportance
	footer := container.NewBorder(nil, nil, nil, container.NewHBox(r.clear, openFolder), r.totals)
	r.noRows = emptyState("No files yet", widget.NewLabel("Each track becomes a file here when it ends."))
	r.listCard = listCard(r.list)
	r.session = inReadingOrder(container.NewVBox(r.card, r.waitRow), inset(footer), nil, nil,
		container.NewStack(inset(r.listCard), r.noRows))
	r.root = container.NewStack(r.idle, r.session)
	return r
}

// rowDetails is a finished track's length, format and size, without the
// parts not known: "3:56 · FLAC · 29.6 MB".
func rowDetails(r model.Row) string {
	var parts []string
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
	case model.Saved:
		return colorSaved
	case model.Skipped:
		return colorSkipped
	}
	return theme.ColorNamePlaceHolder
}

// keep saves a take cut short after all, the part that played: the row
// shows Saving until its file is made, or skipped again if the take is
// gone.
func (a *App) keep(id string) {
	a.noteAction("keep anyway")
	a.m.Keeping(id, true)
	a.calls.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := a.link.Do(ctx, proto.MethodTrackKeep, proto.TrackKeepParams{ID: id}, nil); err != nil {
			fyne.Do(func() {
				a.m.Keeping(id, false)
				a.showError(err)
			})
		}
	})
}

func (r *recordScreen) content() fyne.CanvasObject { return r.root }

func (r *recordScreen) render(s model.State) {
	r.folder, _ = s.Settings[settings.OutputDir.String()].(string)
	r.folder = settings.ExpandHome(r.folder)
	defer r.relayout()
	player := s.Info.Target.Process
	if player == "" {
		player = "your player"
	}
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
		title, text := "Ready to record", fmt.Sprintf("Press Record, then play music in %s. Each track becomes its own file in %s.", player, r.folder)
		blocked := checkFails(s.Info.Checks, proto.CheckOutputWritable)
		if blocked {
			title, text = "Record is off", fmt.Sprintf("Spytify cannot write to the output folder, %s. The diagnostics say why.", r.folder)
		}
		setLabel(r.idleTitle, title)
		setLabel(r.idleText, text)
		show(r.hint, !blocked)
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
		sub := c.Artist
		if c.Album != "" {
			sub += " · " + c.Album
		}
		lines := []string{stateText(s.Session, c), c.Title, sub}
		if src := sourceText(c.SourceRate, c.SourceChannels); src != "" {
			lines = append(lines, src)
		}
		setRowText(r.track, lines...)
		setCover(r.cover, r.app.covers.get(c.CoverURL).live)
	} else {
		r.card.Hide()
		// A session the output folder ended, its drive unplugged: Record
		// stays off until the folder is back or another one is chosen.
		blocked := s.Session == proto.StateIdle && checkFails(s.Info.Checks, proto.CheckOutputWritable)
		// Nothing is recorded until the player plays, whatever the list
		// holds: between sessions or while one waits, the row says so and
		// offers the one next step, to open the player or to play. Waiting
		// with the player running is also a track that started before the
		// session, which only a restart records; the session does not tell
		// it from a stopped player.
		waits := s.Session == proto.StateIdle || s.Session == proto.StateWaiting || s.Session == proto.StatePaused
		down := waits && !blocked && !playerRunning(s)
		paused := s.Session == proto.StatePaused && !down
		text := waitingText(s.Session)
		switch {
		case blocked:
			text = "Session ended: Spytify cannot write to the output folder, so Record is off."
		case down:
			text = fmt.Sprintf("Nothing is recorded while %s is not running.", player)
		case paused:
			text = fmt.Sprintf("Nothing is recorded while %s is paused: press play there.", player)
		}
		setLabel(r.waiting, text)
		r.launch.SetText("Open " + player)
		show(r.launch, down)
		show(r.play, paused)
		show(r.restart, s.Session == proto.StateWaiting && !down)
		show(r.blocked, blocked)
		r.waitRow.Show()
	}
	r.renderLive(s.Current, s.Session, s.Level)

	r.rows = s.Rows
	if shown := fmt.Sprint(r.rows, r.app.covers.loads()); shown != r.shown {
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
	shape := fmt.Sprint(r.idle.Visible(), r.hint.Visible(), r.attention.Visible(), r.openPlayer.Visible(), r.diagnose.Visible(), r.card.Visible(), len(r.track.Segments), r.waitRow.Visible(), r.launch.Visible(), r.play.Visible(), r.restart.Visible(), r.blocked.Visible(), r.noRows.Visible())
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

// sourceText is the captured stream, "44.1 kHz stereo", and the file's rate
// when the encoder writes it at a lower one: "96 kHz stereo, 48 kHz file".
func sourceText(rate, channels int) string {
	if rate <= 0 {
		return ""
	}
	khz := func(rate int) string { return strconv.FormatFloat(float64(rate)/1000, 'f', -1, 64) + " kHz" }
	text := khz(rate)
	switch channels {
	case 1:
		text += " mono"
	case 2:
		text += " stereo"
	default:
		text = fmt.Sprintf("%s %d channels", text, channels)
	}
	// Not an arrow: the app's font has none, and the symbol font Fyne
	// falls back to draws the space after it far too wide.
	if file := encode.FileRate(rate); file != rate {
		text += ", " + khz(file) + " file"
	}
	return text
}

func waitingText(st proto.SessionState) string {
	switch st {
	case proto.StateIdle:
		return "Session ended."
	case proto.StateStopping:
		return "Finishing the last files…"
	}
	return "Waiting for the next track to start."
}
