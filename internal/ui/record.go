package ui

import (
	"context"
	"fmt"
	"image/color"
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
	glow  *canvas.Image
	cover *canvas.Image
	// state says whether the track is recorded, with the level while it is;
	// rate, channels and fileRate are the stream's format.
	state                    *badge
	recDot, pausedIcon       fyne.CanvasObject
	meter                    *levelMeter
	rate, channels, fileRate *badge
	// track is the title, then the artist and album.
	track    *widget.RichText
	progress *bar
	elapsed  *widget.RichText

	waitRow *fyne.Container
	waiting *widget.RichText
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
	r.cover.CornerRadius = 6
	r.cover.SetMinSize(fyne.NewSquareSize(liveCover))
	// The card's background is its cover blurred: a few pixels of it,
	// stretched smoothly.
	r.glow = canvas.NewImageFromImage(noCoverGlow)
	r.glow.FillMode = canvas.ImageFillStretch
	r.glow.ScaleMode = canvas.ImageScaleSmooth
	r.recDot = container.NewGridWrap(fyne.NewSquareSize(8), canvas.NewCircle(color.NRGBA{R: 0xba, G: 0x1a, B: 0x1a, A: 0xff}))
	r.pausedIcon = container.NewGridWrap(fyne.NewSquareSize(14), widget.NewIcon(theme.MediaPauseIcon()))
	r.meter = newLevelMeter(colorOnRecBadge)
	r.state = newBadge("", colorRecBadge, "", colorOnRecBadge)
	r.state.lead = container.NewStack(r.recDot, r.pausedIcon)
	r.state.tail = r.meter
	r.rate = newBadge("", "", colorMediaLine, theme.ColorNameForeground)
	r.channels = newBadge("", "", colorMediaLine, theme.ColorNameForeground)
	r.fileRate = newBadge("", "", colorMediaLine, theme.ColorNameForeground)
	// One block for the title and the artist: stacked widgets would each add
	// their padding between the lines.
	r.track = widget.NewRichText(
		&widget.TextSegment{Style: widget.RichTextStyle{ColorName: theme.ColorNameForeground, SizeName: theme.SizeNameHeadingText, TextStyle: fyne.TextStyle{Bold: true}}},
		&widget.TextSegment{Style: widget.RichTextStyle{ColorName: theme.ColorNamePlaceHolder, SizeName: theme.SizeNameText}},
	)
	r.track.Truncation = fyne.TextTruncateEllipsis
	r.elapsed = caption("0:00 / 0:00")
	r.progress = newBar(theme.ColorNamePrimary)
	skip := newIconButton("", theme.MediaSkipNextIcon(), func() {
		a.noteAction("skip")
		a.do(proto.MethodPlayerControl, proto.PlayerControlParams{Cmd: proto.CmdNext}, nil)
	})
	pad := theme.Padding()
	skipBox := container.NewVBox(layout.NewSpacer(),
		container.NewCenter(container.NewGridWrap(fyne.NewSquareSize(48), skip)), container.NewCenter(caption("Skip")),
		layout.NewSpacer())
	info := container.NewVBox(layout.NewSpacer(), container.NewHBox(r.state, r.rate, r.channels, r.fileRate), r.track, r.elapsed, layout.NewSpacer())
	body := container.NewBorder(nil, nil, r.cover, skipBox, container.New(layout.NewCustomPaddedLayout(0, 0, pad*3, pad*2), info))
	// The track being recorded is on its cover's colours, from one side of
	// the window to the other, its progress along the bottom edge.
	media := container.NewStack(r.glow, container.NewBorder(nil, r.progress, nil, nil,
		container.New(layout.NewCustomPaddedLayout(pad*3, pad*3, pad*3, pad*4), body)))
	r.card = container.NewStack(container.NewThemeOverride(media, mediaTheme{}))

	r.waiting = widget.NewRichText(&widget.TextSegment{Style: widget.RichTextStyle{ColorName: colorInfoText, Inline: true}})
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
	// What the session waits for is an outlined info alert, as MUI draws
	// one in the app's green: an outline and icon around green text, the
	// page showing through, the next step on the right.
	infoIcon := container.NewCenter(widget.NewIcon(theme.NewColoredResource(theme.InfoIcon(), theme.ColorNamePrimary)))
	alert := container.NewBorder(nil, nil, infoIcon, container.NewHBox(r.launch.lit, r.play.lit, r.restart, r.blocked.lit),
		container.NewVBox(layout.NewSpacer(), r.waiting, layout.NewSpacer()))
	r.waitRow = inset(newPanel("", theme.ColorNamePrimary, 8, fyne.NewSize(pad*2, pad*1.5), alert))

	r.list = widget.NewList(
		func() int { return len(r.rows) },
		func() fyne.CanvasObject {
			status := newBadge("Skipped: duplicate", colorSkippedBadge, "", colorOnSkippedBadge)
			keep := container.NewCenter(outlined(newButton("Keep anyway", nil)))
			keep.Hide()
			reveal := newIconButton("", theme.SearchIcon(), nil)
			reveal.Importance = widget.LowImportance
			play := newIconButton("", theme.MediaPlayIcon(), nil)
			play.Importance = widget.LowImportance
			// The title, the artist, then the length, format and size.
			return trackRow(3, container.NewHBox(container.NewCenter(status), keep, container.NewCenter(reveal), container.NewCenter(play)))
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
			fill, ink := badgeColors(row)
			right.Objects[0].(*fyne.Container).Objects[0].(*badge).set(row.StatusText(), fill, ink)
			keep := right.Objects[1].(*fyne.Container)
			keepBtn := keep.Objects[0].(*panel).content.(*button)
			revealBtn := right.Objects[2].(*fyne.Container).Objects[0].(*button)
			playBtn := right.Objects[3].(*fyne.Container).Objects[0].(*button)
			if row.Keepable {
				id := row.ID
				keepBtn.OnTapped = func() { a.keep(id) }
				keep.Show()
			} else {
				keep.Hide()
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
			// The badge's text changed width: lay the row out again.
			box.Refresh()
		},
	)

	r.totals = caption("")
	r.totals.Segments[0].(*widget.TextSegment).Style.ColorName = colorLabel
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
	r.noRows = emptyState("No files yet", widget.NewLabel("Each track becomes a file here when it ends."))
	r.listCard = listCard(r.list)
	// The info bar over the track being recorded, the session's files under
	// them and its totals at the bottom.
	r.session = inReadingOrder(container.NewVBox(r.waitRow, r.card), footerBar(r.totals, container.NewHBox(r.clear, openFolder)), nil, nil,
		container.NewStack(r.listCard, r.noRows))
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

// badgeColors is a row's status badge: green for a saved file, sand for a
// skipped one, grey while it is on its way.
func badgeColors(r model.Row) (fill, ink fyne.ThemeColorName) {
	switch r.Status {
	case model.Saved:
		return colorSavedBadge, colorOnSavedBadge
	case model.Skipped:
		return colorSkippedBadge, colorOnSkippedBadge
	}
	return theme.ColorNameInputBackground, theme.ColorNamePlaceHolder
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

	c := s.Current
	// A session the output folder ended, its drive unplugged: Record stays
	// off until the folder is back or another one is chosen.
	blocked := c == nil && s.Session == proto.StateIdle && checkFails(s.Info.Checks, proto.CheckOutputWritable)
	// Nothing is recorded until the player plays, whatever the list holds:
	// between sessions or while one waits, the info bar says so and offers
	// the one next step, to open the player or to play; over a track the
	// player paused, it offers to play. Waiting with the player running is
	// also a track that started before the session, which only a restart
	// records; the session does not tell it from a stopped player.
	waits := s.Session == proto.StateIdle || s.Session == proto.StateWaiting || s.Session == proto.StatePaused
	down := c == nil && waits && !blocked && !playerRunning(s)
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
	setCaption(r.waiting, text)
	r.launch.SetText("Open " + player)
	show(r.launch, down)
	show(r.play, paused)
	show(r.restart, c == nil && s.Session == proto.StateWaiting && !down)
	show(r.blocked, blocked)
	show(r.waitRow, c == nil || paused)
	show(r.card, c != nil)
	if c != nil {
		r.renderCard(c, s.Session)
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

// renderCard shows the track being recorded: its cover, blurred behind the
// card too, its title, whether it is recorded, and the stream's format.
func (r *recordScreen) renderCard(c *model.Row, session proto.SessionState) {
	sub := c.Artist
	if c.Album != "" {
		sub += " · " + c.Album
	}
	setRowText(r.track, c.Title, sub)
	text, recording := stateText(session, c)
	fill, ink := colorMediaBadge, theme.ColorNameForeground
	switch {
	case recording:
		fill, ink = colorRecBadge, colorOnRecBadge
	case c.Status == model.Skipped:
		fill, ink = colorSkippedBadge, colorOnSkippedBadge
	}
	r.state.set(text, fill, ink)
	pausedIcon := session == proto.StatePaused && c.Status != model.Skipped
	show(r.recDot, recording)
	show(r.pausedIcon, pausedIcon)
	show(r.state.lead, recording || pausedIcon)
	show(r.meter, recording)
	parts := sourceParts(c.SourceRate, c.SourceChannels)
	for i, b := range []*badge{r.rate, r.channels, r.fileRate} {
		show(b, i < len(parts))
		if i < len(parts) {
			b.set(parts[i], "", theme.ColorNameForeground)
		}
	}
	cover := r.app.covers.get(c.CoverURL)
	setCover(r.cover, cover.live)
	g := cover.glow
	if g == nil {
		g = noCoverGlow
	}
	if r.glow.Image != g {
		r.glow.Image = g
		r.glow.Refresh()
	}
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
	r.meter.SetLevel(v)
}

// relayout lays the screen out again when a part appeared or went away, or
// a badge on the card changed width.
func (r *recordScreen) relayout() {
	shape := fmt.Sprint(r.idle.Visible(), r.hint.Visible(), r.attention.Visible(), r.openPlayer.Visible(), r.diagnose.Visible(), r.card.Visible(),
		r.state.text, r.state.lead.Visible(), r.rate.text, r.channels.text, r.fileRate.Visible(), r.fileRate.text,
		r.waitRow.Visible(), r.launch.Visible(), r.play.Visible(), r.restart.Visible(), r.blocked.Visible(), r.noRows.Visible())
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

// stateText is what the card's badge says of the track, and whether it is
// recorded: the red badge, with the level.
func stateText(st proto.SessionState, c *model.Row) (text string, recording bool) {
	if c.Status == model.Skipped {
		return "Playing, not recorded: " + strings.TrimPrefix(c.StatusText(), "Skipped: "), false
	}
	switch st {
	case proto.StatePaused:
		return "Paused", false
	case proto.StateStopping:
		return "Recording · stops after this track", true
	case proto.StateAd:
		return "Advert, not recorded", false
	}
	return "Recording", true
}

// sourceParts is the captured stream, a badge each: "44.1 kHz", "Stereo",
// then the file's rate when the encoder writes it at a lower one: "48 kHz
// file". None while the stream is not known.
func sourceParts(rate, channels int) []string {
	if rate <= 0 {
		return nil
	}
	khz := func(rate int) string { return strconv.FormatFloat(float64(rate)/1000, 'f', -1, 64) + " kHz" }
	parts := []string{khz(rate)}
	switch channels {
	case 1:
		parts = append(parts, "Mono")
	case 2:
		parts = append(parts, "Stereo")
	default:
		parts = append(parts, fmt.Sprintf("%d channels", channels))
	}
	if file := encode.FileRate(rate); file != rate {
		parts = append(parts, khz(file)+" file")
	}
	return parts
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
