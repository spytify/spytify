package ui

import (
	"cmp"
	"context"
	"fmt"
	"image/color"
	"path/filepath"
	"runtime"
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
	"github.com/spytify/spytify/internal/ui/lang"
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
	// startDaemon, and startAgain in a session, start spytifyd once it
	// went: Record is off without it.
	startDaemon *litButton
	session     *fyne.Container

	card  *fyne.Container
	glow  *canvas.Image
	cover *canvas.Image
	// state says whether the track is recorded; rate, channels and
	// fileRate are the stream's format; meter is its level by channel.
	state                    *badge
	recDot, pausedIcon       fyne.CanvasObject
	meter                    *stereoMeter
	rate, channels, fileRate *badge
	// track is the title, then the artist and album.
	track    *widget.RichText
	progress *bar
	elapsed  *widget.RichText

	// alertRow is the recorder's warning while a session runs: a capture
	// too poor for music, or one that brings no audio or only silence.
	alertRow *fyne.Container
	alert    *widget.RichText
	// alertVolume puts the player's own volume back at 100% when the
	// warning says it is lower.
	alertVolume *litButton
	// alertDiagnose leads to the diagnostics, which say why and what to do
	// when the warning has a check of its own.
	alertDiagnose *litButton

	waitRow *fyne.Container
	waiting *widget.RichText
	// launch and play are what a waiting session needs from the player:
	// to run, then to play.
	launch     *litButton
	play       *litButton
	restart    *button
	startAgain *litButton
	// blocked leads to the diagnostics once a session ended for want of
	// an output folder.
	blocked *litButton
	// showPlaylist shows the playlist the session that ended wrote.
	showPlaylist *button
	playlist     string

	rows []model.Row
	// items is the list's lines: the rows, each run of ads one pill, and
	// its ads under it while open has its key. slim is the height of an ad
	// break's lines.
	items []sessionItem
	open  map[string]bool
	slim  float32
	list  *cardList
	// shape is which parts the screen last showed; it is laid out again
	// only when that changes.
	shape string
	// shown is what the list last drew; it is not refreshed on every level
	// event, ten times a second.
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
	a.maskPaths(r.idleText)
	r.hint = widget.NewLabelWithStyle(lang.AlreadyPlaying,
		fyne.TextAlignCenter, fyne.TextStyle{})
	r.hint.Wrapping = fyne.TextWrapWord
	r.hint.SizeName = theme.SizeNameCaptionText
	r.diagnose = newLitButton(lang.OpenDiagnostics, theme.WarningIcon(), widget.WarningImportance, a.showDiagnostics)
	diagnose := r.diagnose
	r.openPlayer = newLitButton(lang.OpenThePlayer, theme.MediaPlayIcon(), widget.HighImportance, a.launchPlayer)
	r.startDaemon = newLitButton(lang.StartRecorder, theme.MediaPlayIcon(), widget.HighImportance, a.StartDaemon)
	show(r.startDaemon, false)
	r.attention = container.NewCenter(container.NewHBox(r.openPlayer.lit, diagnose.lit, r.startDaemon.lit))
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
	r.meter = newStereoMeter()
	r.state = newBadge("", colorRecBadge, "", colorOnRecBadge)
	r.state.lead = container.NewStack(r.recDot, r.pausedIcon)
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
	// The meter and Skip side by side, their captions on one line: Skip is
	// centred on the meter's height, and each bar over its L or R.
	skipPad := float32(meterHeight-48) / 2
	skipBox := container.NewVBox(layout.NewSpacer(),
		container.NewCenter(container.New(layout.NewCustomPaddedLayout(skipPad, skipPad, 0, 0), container.NewGridWrap(fyne.NewSquareSize(48), skip))),
		container.NewCenter(caption(lang.Skip)),
		layout.NewSpacer())
	meterBox := container.NewVBox(layout.NewSpacer(), r.meter,
		container.NewGridWithColumns(2, container.NewCenter(caption(lang.MeterLeft)), container.NewCenter(caption(lang.MeterRight))),
		layout.NewSpacer())
	controls := container.NewHBox(container.New(layout.NewCustomPaddedLayout(0, 0, 0, pad*2), meterBox), skipBox)
	info := container.NewVBox(layout.NewSpacer(), container.NewHBox(r.state, r.rate, r.channels, r.fileRate), r.track, r.elapsed, layout.NewSpacer())
	body := container.NewBorder(nil, nil, r.cover, controls, container.New(layout.NewCustomPaddedLayout(0, 0, pad*3, pad*2), info))
	// The track being recorded is on its cover's colours, from one side of
	// the window to the other, its progress along the bottom edge.
	media := container.NewStack(r.glow, container.NewBorder(nil, r.progress, nil, nil,
		container.New(layout.NewCustomPaddedLayout(pad*3, pad*3, pad*3, pad*4), body)))
	r.card = container.NewStack(container.NewThemeOverride(media, mediaTheme{}))

	r.waiting = widget.NewRichText(&widget.TextSegment{Style: widget.RichTextStyle{ColorName: colorInfoText, Inline: true}})
	r.launch = newLitButton(lang.OpenThePlayer, theme.MediaPlayIcon(), widget.HighImportance, a.launchPlayer)
	r.play = newLitButton(lang.Play, theme.MediaPlayIcon(), widget.HighImportance, func() {
		a.noteAction("play in the player")
		a.do(proto.MethodPlayerControl, proto.PlayerControlParams{Cmd: proto.CmdPlay}, nil)
	})
	// A track already playing when the session starts is not recorded;
	// playing it again from 0:00 makes it the next track.
	r.restart = newIconButton(lang.RecordFromStart, theme.MediaReplayIcon(), func() {
		a.noteAction("record this track from the start")
		a.do(proto.MethodPlayerControl, proto.PlayerControlParams{Cmd: proto.CmdRestart}, nil)
	})
	r.blocked = newLitButton(lang.OpenDiagnostics, theme.WarningIcon(), widget.WarningImportance, a.showDiagnostics)
	r.startAgain = newLitButton(lang.StartRecorder, theme.MediaPlayIcon(), widget.HighImportance, a.StartDaemon)
	// Until spytifyd goes: the screen's width counts every button shown.
	show(r.startAgain, false)
	r.showPlaylist = newIconButton(lang.ShowPlaylist, theme.SearchIcon(), func() {
		a.noteAction("show the playlist")
		if r.playlist != "" {
			_ = reveal(r.playlist)
		}
	})
	show(r.showPlaylist, false)
	// What the session waits for is an outlined info alert, as MUI draws
	// one in the app's green: an outline and icon around green text, the
	// page showing through, the next step on the right. Its corners are
	// square, as the cards'.
	infoIcon := container.NewCenter(widget.NewIcon(theme.NewColoredResource(theme.InfoIcon(), theme.ColorNamePrimary)))
	alert := container.NewBorder(nil, nil, infoIcon, container.NewHBox(r.launch.lit, r.play.lit, r.restart, r.showPlaylist, r.blocked.lit, r.startAgain.lit),
		container.NewVBox(layout.NewSpacer(), r.waiting, layout.NewSpacer()))
	r.waitRow = inset(newPanel("", theme.ColorNamePrimary, 0, fyne.NewSize(pad*2, pad*1.5), alert))
	// The recorder's warning is the same alert in the error red, its text
	// on as many lines as it takes.
	r.alert = widget.NewRichText(&widget.TextSegment{Style: widget.RichTextStyle{ColorName: colorErrorText, Inline: true}})
	r.alert.Wrapping = fyne.TextWrapWord
	a.maskPaths(r.alert)
	r.alertDiagnose = newLitButton(lang.OpenDiagnostics, theme.WarningIcon(), widget.WarningImportance, a.showDiagnostics)
	errorIcon := container.NewCenter(widget.NewIcon(theme.NewColoredResource(theme.ErrorIcon(), theme.ColorNameError)))
	r.alertVolume = newLitButton(lang.SetFullVolume, theme.VolumeUpIcon(), widget.WarningImportance, func() {
		a.noteAction("set the player's volume to 100%")
		a.do(proto.MethodPlayerControl, proto.PlayerControlParams{Cmd: proto.CmdFullVolume}, nil)
	})
	r.alertRow = inset(newPanel("", theme.ColorNameError, 0, fyne.NewSize(pad*2, pad*1.5),
		container.NewBorder(nil, nil, errorIcon, container.NewCenter(container.NewHBox(r.alertVolume.lit, r.alertDiagnose.lit)), container.NewVBox(layout.NewSpacer(), r.alert, layout.NewSpacer()))))

	r.list = newCardList(
		func() int { return len(r.items) },
		func() fyne.CanvasObject {
			status := newBadge(fmt.Sprintf(lang.SkippedFor, lang.ReasonDuplicate), colorSkippedBadge, "", colorOnSkippedBadge)
			keep := container.NewCenter(newRowButton(lang.KeepAnyway, theme.DocumentSaveIcon()))
			keep.Hide()
			reveal := newRowButton("", theme.SearchIcon())
			play := newRowButton("", theme.MediaPlayIcon())
			play.main = true
			// The title, the artist, then the length, format and size; or an
			// ad break's pill, or one of its ads.
			track := trackRow(3, container.NewHBox(container.NewCenter(status), keep, container.NewCenter(reveal), container.NewCenter(play)))
			brk, ad := newAdBreak(), newAdLine()
			brk.Hide()
			ad.Hide()
			return container.NewStack(track, brk, ad)
		},
		func(id widget.ListItemID, o fyne.CanvasObject) {
			if id >= len(r.items) {
				return
			}
			item := r.items[id]
			parts := o.(*fyne.Container).Objects
			// Border layout: center first, then the leading and trailing parts.
			box, brk, ad := parts[0].(*fyne.Container), parts[1].(*fyne.Container), parts[2].(*fyne.Container)
			show(box, item.kind == itemTrack)
			show(brk, item.kind == itemBreak)
			show(ad, item.kind == itemAd)
			switch item.kind {
			case itemBreak:
				key := item.brk
				setAdBreak(brk, fmt.Sprintf(lang.AdBreak, fmtDuration(item.length)), r.open[key], func() { r.toggle(key) })
				// The pill's text changed width: lay the row out again.
				brk.Refresh()
				return
			case itemAd:
				setAdLine(ad, adName(item.n, item.row.Title), fmtDuration(item.length))
				ad.Refresh()
				return
			}
			row := item.row
			right := box.Objects[2].(*fyne.Container)
			setTrackRow(box, a.covers.get(row.CoverURL).row, row.Title, row.Artist, rowDetails(row))
			fill, ink := badgeColors(row)
			right.Objects[0].(*fyne.Container).Objects[0].(*badge).set(row.StatusText(), fill, ink)
			keep := right.Objects[1].(*fyne.Container)
			keepBtn := keep.Objects[0].(*rowButton)
			revealBtn := right.Objects[2].(*fyne.Container).Objects[0].(*rowButton)
			playBtn := right.Objects[3].(*fyne.Container).Objects[0].(*rowButton)
			if row.Keepable {
				id := row.ID
				keepBtn.OnTapped = func() { a.keep(id) }
				keep.Show()
			} else {
				keep.Hide()
			}
			// A track already recorded has its file from before.
			if row.Path != "" {
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

	r.list.height = func(id widget.ListItemID) float32 {
		if id < len(r.items) && r.items[id].kind != itemTrack {
			return r.slimHeight()
		}
		return 0
	}
	// An ad break, open or not, is one blue block between its tracks.
	r.list.band = func(id widget.ListItemID) fyne.ThemeColorName {
		if id < len(r.items) && r.items[id].kind != itemTrack {
			return colorAdBand
		}
		return ""
	}

	r.totals = caption("")
	r.totals.Segments[0].(*widget.TextSegment).Style.ColorName = colorLabel
	openFolder := newIconButton(lang.OpenFolder, theme.FolderOpenIcon(), func() {
		a.noteAction("open folder")
		if r.folder != "" {
			_ = openPath(r.folder)
		}
	})
	openFolder.Importance = widget.LowImportance
	r.clear = newIconButton(lang.ClearList, theme.ContentClearIcon(), func() {
		a.noteAction("clear list")
		a.m.ClearRows()
	})
	r.clear.Importance = widget.LowImportance
	r.noRows = emptyState(lang.NoFilesYet, widget.NewLabel(lang.NoFilesYetText))
	// The recorder's warning and the info bar over the track being
	// recorded, the session's files under them and its totals at the
	// bottom.
	r.session = inReadingOrder(container.NewVBox(r.alertRow, r.waitRow, r.card), footerBar(r.totals, container.NewHBox(r.clear, openFolder)), nil, nil,
		container.NewStack(r.list, r.noRows))
	r.root = container.NewStack(r.idle, r.session)
	return r
}

// rowDetails is a finished track's length, format and size, without the
// parts not known: "3:56 · FLAC · 29.6 MB".
func rowDetails(r model.Row) string {
	var parts []string
	if ms := rowLength(r); ms > 0 {
		parts = append(parts, fmtDuration(ms))
	}
	if r.Path != "" && r.Status == model.Saved {
		parts = append(parts, strings.ToUpper(strings.TrimPrefix(filepath.Ext(r.Path), ".")))
	}
	if r.Bytes > 0 {
		parts = append(parts, fmtBytes(r.Bytes))
	}
	return strings.Join(parts, lang.Separator)
}

// rowLength is how long a row's file plays, or its track when it has none.
func rowLength(r model.Row) int64 {
	if r.DurationMs > 0 {
		return r.DurationMs
	}
	return r.LengthMs
}

// toggle opens an ad break's pill, or closes it.
func (r *recordScreen) toggle(key string) {
	r.app.noteAction("open or close an ad break")
	if r.open == nil {
		r.open = map[string]bool{}
	}
	r.open[key] = !r.open[key]
	r.items = sessionItems(r.rows, r.open)
	r.list.Refresh()
}

// slimHeight is an ad break's lines': as tall as an ad's line, the pill
// fitting in it.
func (r *recordScreen) slimHeight() float32 {
	if r.slim == 0 {
		r.slim = max(newAdLine().MinSize().Height, newAdBreak().MinSize().Height)
	}
	return r.slim
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
	player := playerName(s.Info)
	// spytifyd went after it answered: Record is off until it runs again,
	// and what it last said of the session and the player no longer holds.
	gone := s.Conn != model.Connected && !r.app.starting
	if !s.Recording() && s.Current == nil && len(s.Rows) == 0 {
		// Until the recorder answers it is starting, with nothing to offer.
		starting := !s.HaveInfo || (s.Conn != model.Connected && r.app.starting)
		setLabel(r.idleTitle, map[bool]string{true: lang.StartingRecorder, false: lang.ReadyToRecord}[starting])
		show(r.idleText, !starting)
		if starting {
			show(r.attention, false)
			r.idle.Show()
			r.session.Hide()
			return
		}
		show(r.startDaemon, gone)
		if gone {
			setLabel(r.idleTitle, lang.RecordIsOff)
			setLabel(r.idleText, cmp.Or(r.app.startErr, lang.RecordNeedsRecorder))
			show(r.hint, false)
			show(r.openPlayer, false)
			show(r.diagnose, false)
			show(r.attention, true)
			r.idle.Show()
			r.session.Hide()
			return
		}
		folder := settings.ShortHome(r.folder)
		title, text := lang.ReadyToRecord, fmt.Sprintf(lang.ReadyText, player, folder)
		blocked := checkFails(s.Info.Checks, proto.CheckOutputWritable)
		if blocked {
			title, text = lang.RecordIsOff, fmt.Sprintf(lang.RecordIsOffText, folder)
		}
		setLabel(r.idleTitle, title)
		setLabel(r.idleText, text)
		show(r.hint, !blocked)
		// A player that is not running is shown in the app bar and offered
		// to open here; the diagnostics are for anything else.
		down := !playerRunning(s)
		r.openPlayer.SetText(fmt.Sprintf(lang.OpenPlayer, player))
		show(r.openPlayer, down)
		show(r.diagnose, failingChecks(s.Info.Checks) > 0)
		show(r.attention, r.openPlayer.Visible() || r.diagnose.Visible())
		r.idle.Show()
		r.session.Hide()
		return
	}
	r.idle.Hide()
	r.session.Show()

	// Only silence is said in a line: why, and what to do, is the player
	// heard check's in the diagnostics. A recorder gone no longer warns.
	warns := !gone && s.LastError != nil && s.LastError.Msg != "" && s.Recording()
	silent := warns && s.LastError.Code == proto.ErrorCaptureSilent
	switch {
	case silent:
		setCaption(r.alert, fmt.Sprintf(lang.OnlySilenceHeard, player))
	case warns:
		setCaption(r.alert, model.WarningText(*s.LastError, runtime.GOOS))
	}
	show(r.alertRow, warns)
	show(r.alertDiagnose, silent)
	show(r.alertVolume, warns && s.LastError.Code == proto.ErrorPlayerVolumeLow)

	c := s.Current
	if gone {
		c = nil
	}
	// A session the output folder ended, its drive unplugged: Record stays
	// off until the folder is back or another one is chosen.
	blocked := !gone && c == nil && s.Session == proto.StateIdle && checkFails(s.Info.Checks, proto.CheckOutputWritable)
	// Nothing is recorded until the player plays, whatever the list holds:
	// between sessions or while one waits, the info bar says so and offers
	// the one next step, to open the player or to play; over a track the
	// player paused, it offers to play. Waiting with the player running is
	// also a track that started before the session, which only a restart
	// records; the session does not tell it from a stopped player.
	waits := !gone && (s.Session == proto.StateIdle || s.Session == proto.StateWaiting || s.Session == proto.StatePaused)
	down := c == nil && waits && !blocked && !playerRunning(s)
	paused := waits && s.Session == proto.StatePaused && !down
	// The session ended with a playlist: the alert names it.
	ended := !gone && s.Session == proto.StateIdle && s.Playlist != ""
	text := waitingText(s.Session)
	switch {
	case gone:
		text = fmt.Sprintf(lang.NothingRecordedNotRunning, "spytifyd")
	case blocked:
		text = lang.SessionEndedBlocked
	case down:
		text = fmt.Sprintf(lang.NothingRecordedNotRunning, player)
	case paused:
		text = fmt.Sprintf(lang.NothingRecordedPaused, player)
	case ended:
		text = fmt.Sprintf(lang.SessionEndedPlaylist, filepath.Join(filepath.Base(filepath.Dir(s.Playlist)), filepath.Base(s.Playlist)))
	}
	setCaption(r.waiting, text)
	r.launch.SetText(fmt.Sprintf(lang.OpenPlayer, player))
	show(r.launch, down)
	show(r.play, paused)
	show(r.restart, waits && c == nil && s.Session == proto.StateWaiting && !down)
	show(r.blocked, blocked)
	show(r.startAgain, gone)
	r.playlist = s.Playlist
	show(r.showPlaylist, ended && !blocked && !down)
	show(r.waitRow, c == nil || paused)
	show(r.card, c != nil)
	if c != nil {
		r.renderCard(c, s.Session)
	}
	r.renderLive(s.Current, s.Session, s.Level)

	r.rows = s.Rows
	if shown := fmt.Sprint(r.rows, r.app.covers.loads()); shown != r.shown {
		r.shown = shown
		r.items = sessionItems(r.rows, r.open)
		r.list.Refresh()
	}
	show(r.noRows, len(r.rows) == 0)
	show(r.list, len(r.rows) > 0)
	if len(r.rows) == 0 {
		r.clear.Disable()
	} else {
		r.clear.Enable()
	}
	t := s.Totals()
	setCaption(r.totals, fmt.Sprintf(lang.SessionTotals, t.Saved, t.Skipped, t.Ads, fmtBytes(t.Bytes)))
}

// renderCard shows the track being recorded: its cover, blurred behind the
// card too, its title, whether it is recorded, and the stream's format.
func (r *recordScreen) renderCard(c *model.Row, session proto.SessionState) {
	sub := c.Artist
	if c.Album != "" {
		sub += lang.Separator + c.Album
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
	// The meter moves only while recording, the track a stop waits for
	// included: paused, stopped or with the take dropped, it is out.
	if session == proto.StateRecording || session == proto.StateStopping && c.Status != model.Skipped {
		r.meter.SetLevel(level)
	} else {
		r.meter.Clear()
	}
}

// relayout lays the screen out again when a part appeared or went away, or
// a badge on the card changed width.
func (r *recordScreen) relayout() {
	shape := fmt.Sprint(r.idle.Visible(), r.hint.Visible(), r.attention.Visible(), r.openPlayer.Visible(), r.diagnose.Visible(), r.startDaemon.Visible(), r.card.Visible(),
		r.state.text, r.state.lead.Visible(), r.rate.text, r.channels.text, r.fileRate.Visible(), r.fileRate.text,
		r.alertRow.Visible(), r.alert.String(), r.alertDiagnose.Visible(), r.alertVolume.Visible(), r.waitRow.Visible(), r.launch.Visible(), r.play.Visible(), r.restart.Visible(), r.blocked.Visible(), r.startAgain.Visible(), r.noRows.Visible())
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
		why := lang.Skipped
		if c.Reason != "" {
			why = model.ReasonText(c.Reason)
		}
		return fmt.Sprintf(lang.StateNotRecorded, why), false
	}
	switch st {
	case proto.StatePaused:
		return lang.StatePaused, false
	case proto.StateStopping:
		return lang.StateStopping, true
	case proto.StateAd:
		return lang.StateAdvert, false
	}
	return lang.Recording, true
}

// sourceParts is the captured stream, a badge each: "44.1 kHz", "Stereo",
// then the file's rate when the encoder writes it at a lower one: "48 kHz
// file". None while the stream is not known.
func sourceParts(rate, channels int) []string {
	if rate <= 0 {
		return nil
	}
	khz := func(rate int) string {
		return fmt.Sprintf(lang.KHz, strconv.FormatFloat(float64(rate)/1000, 'f', -1, 64))
	}
	parts := []string{khz(rate)}
	switch channels {
	case 1:
		parts = append(parts, lang.Mono)
	case 2:
		parts = append(parts, lang.Stereo)
	default:
		parts = append(parts, fmt.Sprintf(lang.Channels, channels))
	}
	if file := encode.FileRate(rate); file != rate {
		parts = append(parts, fmt.Sprintf(lang.FileRate, khz(file)))
	}
	return parts
}

func waitingText(st proto.SessionState) string {
	switch st {
	case proto.StateIdle:
		return lang.SessionEnded
	case proto.StateStopping:
		return lang.FinishingLastFiles
	}
	return lang.WaitingNextTrack
}
