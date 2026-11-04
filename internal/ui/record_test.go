package ui

import (
	"fmt"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/spytify/spytify/internal/ui/lang"
	"github.com/spytify/spytify/internal/ui/model"
	"github.com/spytify/spytify/pkg/proto"
)

func TestRecordScreenShowsTheSession(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	feedSession(t, m)
	a.render()
	all := texts(a.win.Content())
	for _, want := range []string{lang.Recording, "Midnight City", "M83 · Hurry Up, We're Dreaming", "Reunion", lang.Saved, fmt.Sprintf(lang.SkippedFor, lang.ReasonDuplicate),
		fmt.Sprintf(lang.SessionTotals, 2, 1, 0, ""), lang.Stop, fmt.Sprintf(lang.PlayerRunning, "Player"), "FLAC", lang.Skip} {
		if !contains(all, want) {
			t.Errorf("%q not on screen", want)
		}
	}
	// The rows' tags were found online: their chips still say Saved.
	if contains(all, "Tagged") {
		t.Error("a saved track's chip says Tagged")
	}
	// The app bar's Donate is a heart, as in v1; About has the worded one.
	if contains(all, lang.DonateHeart) || !a.donate.Visible() || a.donate.Icon != heartIcon || a.donate.ToolTip() != lang.Donate {
		t.Error("the app bar's Donate is not a heart alone")
	}
	if a.tabs.Items[tabRecord].Text != fmt.Sprintf(lang.RecordCount, 2) {
		t.Errorf("Record tab title %q", a.tabs.Items[tabRecord].Text)
	}
	r := a.screens[tabRecord].(*recordScreen)
	if r.clear.Disabled() {
		t.Error("Clear list is disabled with rows in the list")
	}
	if a.action.Text != lang.Stop || a.action.Importance != widget.DangerImportance {
		t.Errorf("app bar action %q", a.action.Text)
	}
	snapshot(t, a, "record")
	// The picture carries art: the card's cover, blurred behind it too, and
	// one on every row.
	pictures := slices.DeleteFunc(images(a.win.Content()), func(s string) bool { return !strings.HasPrefix(s, "picture ") })
	if len(pictures) != 5 {
		t.Errorf("%d pictures on the Record screen, want the card's cover and its glow, and three rows': %v", len(pictures), images(a.win.Content()))
	}

	test.Tap(r.clear)
	a.render()
	if s := m.Snapshot(); len(s.Rows) != 0 || s.Current == nil {
		t.Errorf("after Clear list: %d rows, current %v", len(s.Rows), s.Current)
	}
	if !r.clear.Disabled() || !contains(texts(a.win.Content()), lang.NoFilesYet) {
		t.Error("the cleared list does not say it is empty")
	}
}

// A device above 48 kHz is written at a lower rate, and the card says so in
// a badge of its own, after the stream's rate and channels.
func TestTheRecordCardSaysTheFileIsAtALowerRate(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	feedSession(t, m)
	a.render()
	r := a.screens[tabRecord].(*recordScreen)
	// badges is what the card's badges shown say, left to right.
	badges := func() []string {
		var out []string
		for _, b := range []*badge{r.state, r.rate, r.channels, r.fileRate} {
			if b.Visible() {
				out = append(out, b.text)
			}
		}
		return out
	}
	// No format known, no badge for it.
	if got, want := badges(), []string{lang.Recording}; !slices.Equal(got, want) {
		t.Errorf("card badges %q, want %q", got, want)
	}

	m.Apply(event(t, proto.EventTrackFinished, proto.TrackFinishedEvent{ID: "s-004", Path: "/Users/listener/Music/Spytify/M83/Midnight City.flac", DurationMs: 243000, Bytes: 31_400_000, Tagged: true, TaggedBy: "lastfm"}))
	m.Apply(event(t, proto.EventTrackStarted, proto.TrackStartedEvent{Track: proto.Track{ID: "s-005", Artist: "M83", Title: "Outro", Album: "Hurry Up, We're Dreaming", LengthMs: 247000},
		Source: &proto.AudioSource{SampleRate: 96000, Channels: 2}}))
	a.render()
	if got, want := badges(), []string{lang.Recording, "96 kHz", lang.Stereo, "48 kHz file"}; !slices.Equal(got, want) {
		t.Errorf("card badges %q, want %q", got, want)
	}
	if all := texts(r.card); !contains(all, "Outro") || !contains(all, "M83 · Hurry Up, We're Dreaming") || !contains(all, "0:00 / 4:07") {
		t.Errorf("the card does not show the track and its time: %q", all)
	}
	snapshot(t, a, "record-96khz-layout")

	// At the narrowest the window goes, everything on the card stays on it.
	a.win.Resize(a.win.Content().MinSize())
	a.render()
	panel := r.card.Objects[0]
	d := fyne.CurrentApp().Driver()
	box := d.AbsolutePositionForObject(panel)
	walk(panel, func(o fyne.CanvasObject) bool {
		if !o.Visible() {
			return false
		}
		p, s := d.AbsolutePositionForObject(o), o.Size()
		if p.X < box.X || p.X+s.Width > box.X+panel.Size().Width {
			t.Errorf("%T at x %v to %v, out of the card's %v to %v", o, p.X, p.X+s.Width, box.X, box.X+panel.Size().Width)
		}
		return true
	})
	snapshot(t, a, "record-96khz-narrow")
}

// A player paused mid-track keeps the track's card, its badge saying so
// and its meter out, with the info bar above it offering to play.
func TestAPausedTrackHasTheInfoBarAboveItsCard(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	feedSession(t, m)
	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StatePaused}))
	a.render()
	r := a.screens[tabRecord].(*recordScreen)
	if !r.card.Visible() || r.state.text != lang.StatePaused || r.meter.lit != [2]float32{} {
		t.Errorf("paused card: shown %v, badge %q, meter lit %v", r.card.Visible(), r.state.text, r.meter.lit)
	}
	if !r.waitRow.Visible() || r.waiting.String() != fmt.Sprintf(lang.NothingRecordedPaused, "Player") || !r.play.Visible() || r.restart.Visible() {
		t.Errorf("info bar shown %v, says %q, Play %v, restart %v", r.waitRow.Visible(), r.waiting.String(), r.play.Visible(), r.restart.Visible())
	}
	if d := fyne.CurrentApp().Driver(); d.AbsolutePositionForObject(r.waitRow).Y >= d.AbsolutePositionForObject(r.card).Y {
		t.Error("the info bar is not above the card")
	}
	snapshot(t, a, "record-paused")
	light := defaults()
	light["app.theme"] = "light"
	m.SetSettings(light)
	a.render()
	snapshot(t, a, "record-paused-light")

	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateRecording}))
	a.render()
	if r.waitRow.Visible() || r.state.text != lang.Recording || r.meter.lit == [2]float32{} {
		t.Errorf("playing again: info bar %v, badge %q, meter lit %v", r.waitRow.Visible(), r.state.text, r.meter.lit)
	}
}

// Stop pressed mid-track: the track plays to its end and is recorded to it,
// its badge saying so and its meter moving. A stop now drops the take, and
// the meter goes out.
func TestTheMeterMovesWhileAStopWaitsForTheTrack(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	feedSession(t, m)
	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateStopping}))
	m.Apply(event(t, proto.EventLevel, proto.LevelEvent{RMSDBFS: -12}))
	a.render()
	r := a.screens[tabRecord].(*recordScreen)
	if r.state.text != lang.StateStopping || r.meter.lit == [2]float32{} {
		t.Errorf("stop pending: badge %q, meter lit %v", r.state.text, r.meter.lit)
	}
	snapshot(t, a, "record-stopping")

	m.Apply(event(t, proto.EventTrackSkipped, proto.TrackSkippedEvent{ID: "s-004", Reason: proto.SkipIncomplete}))
	a.render()
	if r.meter.lit != [2]float32{} {
		t.Errorf("take dropped: meter lit %v", r.meter.lit)
	}
}

// A finished track's row has its title, its artist, then its length, format
// and size, each on a line of its own and in a style of its own; a track with
// no artist has two lines, centred in a row as tall as the others. At the
// narrowest the window goes, a long line ends in an ellipsis and leaves the
// chip and the buttons in the row.
func TestARecordRowHasTheTitleTheArtistThenTheDetails(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	feedSession(t, m)
	playing := proto.Track{ID: "s-004", Title: "Midnight City", LengthMs: 243000}
	// play saves the track playing, then plays the tracks one after another,
	// saving all but the last.
	play := func(tracks ...proto.Track) {
		for _, tr := range tracks {
			m.Apply(event(t, proto.EventTrackFinished, proto.TrackFinishedEvent{ID: playing.ID, Path: "/Users/listener/Music/Spytify/" + playing.Title + ".flac", DurationMs: playing.LengthMs, Bytes: 38_100_000}))
			m.Apply(event(t, proto.EventTrackStarted, proto.TrackStartedEvent{Track: tr}))
			playing = tr
		}
		a.render()
	}
	play(proto.Track{ID: "s-005", Title: "Intermission", LengthMs: 62000},
		proto.Track{ID: "s-006", Artist: "M83", Title: "Raconte-moi une histoire", Album: "Hurry Up, We're Dreaming", LengthMs: 270000})
	r := a.screens[tabRecord].(*recordScreen)

	all := drawnRows(t, r.list)
	for title, want := range map[string][]string{
		"Reunion":      {"Reunion", "M83", "3:56 · FLAC · 31.0 MB"},
		"Wait":         {"Wait", "M83", "2:54"},
		"Intermission": {"Intermission", "1:02 · FLAC · 38.1 MB"},
	} {
		if got := words(all[title].lines); !slices.Equal(got, want) {
			t.Errorf("%s row %q, want %q", title, got, want)
		}
	}
	// The title keeps its style; the artist and the details share theirs.
	if lines := all["Reunion"].lines; len(lines) == 3 {
		title, artist, details := lines[0], lines[1], lines[2]
		if !title.TextStyle.Bold || title.TextSize <= artist.TextSize || title.Color == artist.Color {
			t.Errorf("the artist is drawn like the title: %+v, %+v", title, artist)
		}
		if artist.TextStyle != details.TextStyle || artist.TextSize != details.TextSize || artist.Color != details.Color {
			t.Errorf("the artist is not in the details' style: %+v, %+v", artist, details)
		}
	}
	// Every row is as tall; the one with no artist has its two lines in the
	// middle of it.
	for title, row := range all {
		if h := all["Reunion"].row.Size().Height; row.row.Size().Height != h {
			t.Errorf("%s row is %v tall, the others %v", title, row.row.Size().Height, h)
		}
	}
	if full, short := all["Reunion"].text, all["Intermission"].text; full.Position().Y != 0 || short.Position().Y < (full.Size().Height-short.Size().Height)/2-0.5 {
		t.Errorf("the row with no artist has its text at y %v, the others at %v", short.Position().Y, full.Position().Y)
	}
	snapshot(t, a, "history-rows")

	longArtist := "M83 with The Hurry Up Orchestra, The Choir of the Night, The Second Night Strings and The Brass of the Rain"
	longTitle := "Outro (Extended Mix, Live in a Very Large Concert Hall With the Full Orchestra and the Choir)"
	play(proto.Track{ID: "s-007", Artist: longArtist, Title: "Splendor", LengthMs: 313000},
		proto.Track{ID: "s-008", Artist: "M83", Title: longTitle, LengthMs: 247000},
		proto.Track{ID: "s-009", Artist: "M83", Title: "Year One, One UFO", Album: "Hurry Up, We're Dreaming", LengthMs: 194000})
	all = drawnRows(t, r.list)
	for title, want := range map[string][]string{
		"Splendor": {"Splendor", longArtist, "5:13 · FLAC · 38.1 MB"},
		longTitle:  {longTitle, "M83", "4:07 · FLAC · 38.1 MB"},
	} {
		if got := words(all[title].lines); !slices.Equal(got, want) {
			t.Errorf("%s row %q, want %q", title, got, want)
		}
	}
	a.win.Resize(a.win.Content().MinSize())
	a.render()
	all = drawnRows(t, r.list)
	if got := words(all["Splendor"].lines); len(got) != 3 || got[0] != "Splendor" || !strings.HasPrefix(got[1], "M83 with The") || !strings.HasSuffix(got[1], "…") {
		t.Errorf("a long artist does not end in an ellipsis: %q", got)
	}
	if got := words(all[longTitle].lines); len(got) != 3 || !strings.HasPrefix(got[0], "Outro (Extended Mix, ") || !strings.HasSuffix(got[0], "…") || got[1] != "M83" {
		t.Errorf("a long title does not end in an ellipsis: %q", got)
	}
	insideRows(t, all)
	snapshot(t, a, "history-rows-narrow")
}

// The last take cut short offers Keep anyway on its row, and only it.
func TestRecordScreenOffersKeepAnyway(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	feedSession(t, m)
	m.Apply(event(t, proto.EventTrackStarted, proto.TrackStartedEvent{Track: proto.Track{ID: "s-004", Artist: "M83", Title: "Outro", Album: "Hurry Up, We're Dreaming", LengthMs: 247000, CoverURL: generatedCover(t, 5)}}))
	m.Apply(event(t, proto.EventTrackSkipped, proto.TrackSkippedEvent{ID: "s-004", Reason: proto.SkipIncomplete}))
	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateIdle}))
	m.Apply(event(t, proto.EventTrackKeepable, proto.TrackKeepableEvent{ID: "s-004", Keepable: true}))
	a.render()
	r := a.screens[tabRecord].(*recordScreen)
	r.list.ScrollToBottom()
	all := texts(a.win.Content())
	if !contains(all, fmt.Sprintf(lang.SkippedFor, lang.ReasonIncomplete)) || !contains(all, lang.KeepAnyway) {
		t.Errorf("no Keep anyway on the take cut short: %v", all)
	}
	// Keep anyway looks like the screen's other buttons: the save icon
	// before its words, and no outline around them.
	i := slices.IndexFunc(r.rows, func(row model.Row) bool { return row.ID == "s-004" })
	// The row's track, over an ad break's pill and an ad's line.
	track := rowAt(r.list, i).content.(*fyne.Container).Objects[0].(*fyne.Container)
	right := track.Objects[2].(*fyne.Container)
	if keep, ok := right.Objects[1].(*fyne.Container).Objects[0].(*rowButton); !ok {
		t.Errorf("Keep anyway is a %T", right.Objects[1].(*fyne.Container).Objects[0])
	} else if keep.Icon != theme.DocumentSaveIcon() || keep.Importance != widget.LowImportance {
		t.Errorf("Keep anyway has the icon %v, importance %v", keep.Icon, keep.Importance)
	}
	snapshot(t, a, "record-keep")

	m.Apply(event(t, proto.EventTrackKeepable, proto.TrackKeepableEvent{ID: "s-004"}))
	a.render()
	if contains(texts(a.win.Content()), lang.KeepAnyway) {
		t.Error("Keep anyway stays once the take is let go")
	}
}

// A take that held only silence, as when the player is muted in its own
// volume, says so on its row, under the alert that the player is not
// heard, with nothing to keep.
func TestASilentTakeSaysSo(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	feedSession(t, m)
	m.Apply(event(t, proto.EventError, proto.ErrorEvent{Code: proto.ErrorCaptureSilent, Msg: "only silence from the capture for 10s while the player plays"}))
	m.Apply(event(t, proto.EventTrackSkipped, proto.TrackSkippedEvent{ID: "s-004", Reason: proto.SkipSilent}))
	m.Apply(event(t, proto.EventTrackStarted, proto.TrackStartedEvent{Track: proto.Track{ID: "s-005", Artist: "M83", Title: "Outro", Album: "Hurry Up, We're Dreaming", LengthMs: 247000, CoverURL: generatedCover(t, 5)}}))
	m.Apply(event(t, proto.EventLevel, proto.LevelEvent{PeakDBFS: -120, RMSDBFS: -120,
		Channels: []proto.ChannelLevel{{PeakDBFS: -120, RMSDBFS: -120}, {PeakDBFS: -120, RMSDBFS: -120}}}))
	a.render()
	all := texts(a.win.Content())
	if !contains(all, fmt.Sprintf(lang.SkippedFor, lang.ReasonSilent)) || contains(all, lang.KeepAnyway) {
		t.Errorf("the silent take's row: %v", all)
	}
	snapshot(t, a, "record-silent-take")
}

// A take saved with the player's fades around a pause in it says so in the
// list.
func TestRecordScreenSaysATakeHasAPauseFade(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	feedSession(t, m)
	// s-004 is the track playing.
	m.Apply(event(t, proto.EventTrackFinished, proto.TrackFinishedEvent{ID: "s-004", Path: "/Users/listener/Music/Spytify/M83/Midnight City (pause fade).flac", DurationMs: 243000, Bytes: 31_400_000, Tagged: true, TaggedBy: "lastfm", PauseFade: true}))
	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateIdle}))
	a.render()
	a.screens[tabRecord].(*recordScreen).list.ScrollToBottom()
	if all := texts(a.win.Content()); !contains(all, lang.SavedPauseFade) {
		t.Errorf("the take does not say it has a pause fade: %v", all)
	}
	snapshot(t, a, "record-pause-fade")
}

// A track skipped as already recorded shows and plays the file it has,
// which the recorder names; its details stay its length.
func TestAnAlreadyRecordedRowShowsAndPlaysItsFile(t *testing.T) {
	var revealed, played []string
	defer func(r, o func(string) error) { reveal, openPath = r, o }(reveal, openPath)
	reveal = func(p string) error { revealed = append(revealed, p); return nil }
	openPath = func(p string) error { played = append(played, p); return nil }

	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	feedSession(t, m)
	r := a.screens[tabRecord].(*recordScreen)
	a.render()
	if n := len(iconButtons(r.list, theme.SearchIcon())); n != 2 {
		t.Fatalf("%d magnifiers before the recorder names Wait's file, want the 2 saved tracks'", n)
	}
	file := "/Users/listener/Music/Spytify/M83/Wait.flac"
	m.Apply(event(t, proto.EventTrackSkipped, proto.TrackSkippedEvent{ID: "s-003", Reason: proto.SkipDuplicate, Path: file}))
	a.render()
	if got := words(drawnRows(t, r.list)["Wait"].lines); !slices.Equal(got, []string{"Wait", "M83", "2:54"}) {
		t.Errorf("Wait's row reads %q", got)
	}
	search, play := iconButtons(r.list, theme.SearchIcon()), iconButtons(r.list, theme.MediaPlayIcon())
	if len(search) != 3 || len(play) != 3 {
		t.Fatalf("%d magnifiers and %d play buttons, want 3 of each", len(search), len(play))
	}
	for i := range search {
		test.Tap(search[i])
		test.Tap(play[i])
	}
	if !slices.Contains(revealed, file) || !slices.Contains(played, file) {
		t.Errorf("revealed %v and played %v, want %s in both", revealed, played, file)
	}
	snapshot(t, a, "record-already-recorded")
	light := defaults()
	light["app.theme"] = "light"
	m.SetSettings(light)
	a.render()
	snapshot(t, a, "record-already-recorded-light")
}

func TestWaitingOffersToRestartTheTrack(t *testing.T) {
	a, m := connectedTestApp(t, defaults())
	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateWaiting}))
	a.render()
	all := texts(a.win.Content())
	if !contains(all, lang.WaitingNextTrack) || !contains(all, lang.RecordFromStart) {
		t.Fatalf("waiting state: %v", all)
	}
	snapshot(t, a, "waiting")
}

// The Record screen says to start the player even with tracks in the list:
// it offers to open the player while it is not running, then to play while
// a session waits on it paused, one action at a time.
func TestWaitingSaysToStartThePlayer(t *testing.T) {
	a, m, core := connectedApp(t)
	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateRecording}))
	m.Apply(event(t, proto.EventTrackStarted, proto.TrackStartedEvent{Track: proto.Track{ID: "s-001", Artist: "M83", Title: "Midnight City", Album: "Hurry Up, We're Dreaming", LengthMs: 243000, CoverURL: generatedCover(t, 4)}}))
	m.Apply(event(t, proto.EventTrackFinished, proto.TrackFinishedEvent{ID: "s-001", Path: "/Users/listener/Music/Spytify/M83/Midnight City.flac", DurationMs: 243000, Bytes: 31_400_000, Tagged: true}))
	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateIdle}))
	r := a.screens[tabRecord].(*recordScreen)
	// offers is the wait row's text and the one button it shows.
	offers := func(text string, want fyne.CanvasObject) {
		t.Helper()
		a.render()
		if !r.waitRow.Visible() || r.waiting.String() != text {
			t.Errorf("the wait row says %q, want %q", r.waiting.String(), text)
		}
		for _, o := range []fyne.CanvasObject{r.launch, r.play, r.restart, r.blocked} {
			if b, _ := as[*widget.Button](o); o.Visible() != (o == want) {
				t.Errorf("%q shown %v", b.Text, o.Visible())
			}
		}
	}

	// The player was closed since: Record is off, the list stays, and the
	// row offers to open the player.
	m.Apply(event(t, proto.EventTargetState, proto.TargetStateEvent{Running: false}))
	offers(fmt.Sprintf(lang.NothingRecordedNotRunning, "Player"), r.launch)
	if r.launch.Text != fmt.Sprintf(lang.OpenPlayer, "Player") || !a.action.Disabled() || len(r.rows) != 1 {
		t.Errorf("the button reads %q, Record disabled %v, %d rows", r.launch.Text, a.action.Disabled(), len(r.rows))
	}
	snapshot(t, a, "waiting-player-closed")
	test.Tap(r.launch)
	waitCall(t, core, proto.MethodPlayerLaunch)
	m.Apply(event(t, proto.EventTargetState, proto.TargetStateEvent{Running: true, PID: 4242}))
	offers(lang.SessionEnded, nil)

	// It quits during a session.
	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateWaiting}))
	m.Apply(event(t, proto.EventTargetState, proto.TargetStateEvent{Running: false}))
	offers(fmt.Sprintf(lang.NothingRecordedNotRunning, "Player"), r.launch)

	// It opens again, paused.
	m.Apply(event(t, proto.EventTargetState, proto.TargetStateEvent{Running: true, PID: 4243}))
	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StatePaused}))
	offers(fmt.Sprintf(lang.NothingRecordedPaused, "Player"), r.play)
	snapshot(t, a, "waiting-player-paused")
	test.Tap(r.play)
	eventually(t, "player.control did not send play alone", func() bool {
		core.mu.Lock()
		defer core.mu.Unlock()
		return slices.Equal(core.controls, []proto.PlayerCmd{proto.CmdPlay})
	})

	// It plays the track it was on, which started before the session.
	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateWaiting}))
	offers(lang.WaitingNextTrack, r.restart)
}

func TestIdleRecordScreenIsJustAMessage(t *testing.T) {
	a, m := connectedTestApp(t, defaults())
	all := texts(a.win.Content())
	for _, want := range []string{lang.ReadyToRecord, fmt.Sprintf(lang.ReadyText, "Player", defaults()["output.dir"])} {
		if !contains(all, want) {
			t.Errorf("%q not on the idle screen", want)
		}
	}
	for _, unwanted := range []string{"pid 4242", "capture helper", lang.OpenDiagnostics} {
		if contains(all, unwanted) {
			t.Errorf("%q is on the idle Record screen; the checks live in Logs", unwanted)
		}
	}
	if n := len(buttons(a.win.Content(), lang.Record)); n != 1 {
		t.Errorf("%d Record buttons; want the app bar's only", n)
	}
	snapshot(t, a, "idle")

	// A player that is not running is offered to open, not diagnosed.
	down := info()
	down.Checks[1].OK = false
	m.SetInfo(down)
	a.render()
	if all := texts(a.win.Content()); !contains(all, fmt.Sprintf(lang.OpenPlayer, "Player")) || contains(all, lang.OpenDiagnostics) {
		t.Errorf("player not running: %v", all)
	}
	// It starts: the check follows the target's state, and the offer goes.
	m.Apply(event(t, proto.EventTargetState, proto.TargetStateEvent{Running: true, PID: 99}))
	a.render()
	if all := texts(a.win.Content()); contains(all, fmt.Sprintf(lang.OpenPlayer, "Player")) || !checkOK(m.Snapshot().Info.Checks, proto.CheckPlayerRunning) {
		t.Errorf("player started: %v", all)
	}
	// Any other failing check sends the user to the diagnostics.
	failing := info()
	failing.Checks[2].OK = false
	m.SetInfo(failing)
	a.render()
	if !contains(texts(a.win.Content()), lang.OpenDiagnostics) {
		t.Error("no way to the diagnostics while a check fails")
	}
}

func TestSourceParts(t *testing.T) {
	for _, c := range []struct {
		rate, ch int
		want     []string
	}{{44100, 2, []string{"44.1 kHz", lang.Stereo}}, {48000, 2, []string{"48 kHz", lang.Stereo}}, {48000, 1, []string{"48 kHz", lang.Mono}},
		{96000, 2, []string{"96 kHz", lang.Stereo, "48 kHz file"}}, {88200, 2, []string{"88.2 kHz", lang.Stereo, "44.1 kHz file"}},
		{96000, 6, []string{"96 kHz", "6 channels", "48 kHz file"}}, {0, 2, nil}} {
		if got := sourceParts(c.rate, c.ch); !slices.Equal(got, c.want) {
			t.Errorf("sourceParts(%d, %d) = %q, want %q", c.rate, c.ch, got, c.want)
		}
	}
}

// A finished track's row is its title, its artist, then its length, format
// and size without the parts not known; with no artist, its line is left out.
func TestTrackText(t *testing.T) {
	test.NewTempApp(t)
	row := trackRow(3, nil)
	// texts is the row's lines, checking that those under the title are in
	// their style even after a row with the title alone.
	texts := func() []string {
		var out []string
		for i, seg := range row.Objects[0].(*fyne.Container).Objects[1].(*widget.RichText).Segments {
			if i > 0 && seg.(*widget.TextSegment).Style != rowLine {
				t.Errorf("%q is not in the style of the lines under the title", seg.Textual())
			}
			out = append(out, seg.Textual())
		}
		return out
	}
	flac := "/Users/listener/Music/Spytify/M83/Reunion.flac"
	for _, c := range []struct {
		name string
		row  model.Row
		want []string
	}{
		{"saved", model.Row{Title: "Reunion", Artist: "M83", Status: model.Saved, Path: flac, DurationMs: 236000, Bytes: 31_000_000}, []string{"Reunion", "M83", "3:56 · FLAC · 31.0 MB"}},
		{"skipped", model.Row{Title: "Wait", Artist: "M83", Status: model.Skipped, LengthMs: 174000}, []string{"Wait", "M83", "2:54"}},
		{"no artist", model.Row{Title: "Reunion", Status: model.Saved, Path: flac, DurationMs: 236000, Bytes: 31_000_000}, []string{"Reunion", "3:56 · FLAC · 31.0 MB"}},
		{"title alone", model.Row{Title: "Intermission", Status: model.Skipped}, []string{"Intermission"}},
		{"no size", model.Row{Title: "Reunion", Artist: "M83", Status: model.Saved, Path: flac, DurationMs: 236000}, []string{"Reunion", "M83", "3:56 · FLAC"}},
	} {
		setTrackRow(row, nil, c.row.Title, c.row.Artist, rowDetails(c.row))
		if got := texts(); !slices.Equal(got, c.want) {
			t.Errorf("%s row %q, want %q", c.name, got, c.want)
		}
	}
}

// Under a dialog the meter stays still: moving it there repaints the whole
// dimmed window, two thirds of a core with the first-run dialog open.
func TestTheMeterWaitsUnderADialog(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	feedSession(t, m)
	a.render()
	meter := a.screens[tabRecord].(*recordScreen).meter
	before := meter.lit
	// Louder: the bars rise at once.
	m.Apply(event(t, proto.EventLevel, proto.LevelEvent{RMSDBFS: -6}))
	d := dialog.NewInformation("A dialog", "covering the meter", a.win)
	d.Show()
	a.renderLevel()
	if meter.lit != before {
		t.Fatalf("the meter moved under a dialog: %v to %v bars", before, meter.lit)
	}
	d.Hide()
	a.renderLevel()
	if meter.lit == before {
		t.Fatal("the meter did not move once the dialog closed")
	}
}

// Record needs the player: with it closed the button is off, and the app
// bar says why. A recording under way keeps its Stop.
func TestRecordWaitsForThePlayer(t *testing.T) {
	a, m := connectedTestApp(t, defaults())
	m.Apply(event(t, proto.EventTargetState, proto.TargetStateEvent{Running: false}))
	a.render()
	if !a.action.Disabled() || !strings.Contains(a.status.Text, "is not running") {
		t.Fatalf("player closed: Record disabled %v, status %q", a.action.Disabled(), a.status.Text)
	}
	m.Apply(event(t, proto.EventTargetState, proto.TargetStateEvent{Running: true, PID: 7}))
	a.render()
	if a.action.Disabled() {
		t.Fatal("Record stays disabled once the player runs")
	}
	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateRecording}))
	m.Apply(event(t, proto.EventTargetState, proto.TargetStateEvent{Running: false}))
	a.render()
	if a.action.Disabled() || a.action.Text != lang.Stop {
		t.Fatalf("player quit while recording: %q disabled %v", a.action.Text, a.action.Disabled())
	}
}

// An output folder that cannot be written to turns Record off. The Record
// screen says so and leads to the diagnostics, whose check offers another
// folder; after a session, the list says so too.
func TestRecordIsOffWithoutAnOutputFolder(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	m.SetConn(model.Connected, "")
	m.SetInfo(outputGone())
	m.SetSettings(defaults())
	a.render()
	all := texts(a.win.Content())
	if !a.action.Disabled() || !contains(all, lang.RecordIsOff) || !contains(all, lang.OpenDiagnostics) || contains(all, lang.ReadyToRecord) {
		t.Fatalf("output folder gone: Record disabled %v, screen %v", a.action.Disabled(), all)
	}
	snapshot(t, a, "record-no-output")
	tapButton(t, a.win.Content(), lang.OpenDiagnostics)
	if a.tabs.SelectedIndex() != tabRecord {
		t.Fatalf("Open diagnostics shows tab %d", a.tabs.SelectedIndex())
	}
	snapshot(t, a, "diagnostics-no-output")
	tapButton(t, overlay(t, a), lang.ChooseAnotherFolder)
	set := a.screens[tabSettings].(*settingsScreen)
	if a.tabs.SelectedIndex() != tabSettings || set.search.Text != "Output folder" {
		t.Fatalf("Choose another folder shows tab %d, searching %q", a.tabs.SelectedIndex(), set.search.Text)
	}
	if a.win.Canvas().Overlays().Top() != nil {
		t.Error("the diagnostics stay over the output folder's setting")
	}
	set.search.SetText("")

	// Lost during a session that saved a track.
	a.showTab(tabRecord)
	m.SetInfo(info())
	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateRecording}))
	m.Apply(event(t, proto.EventTrackStarted, proto.TrackStartedEvent{Track: proto.Track{ID: "s-1", Artist: "Neon Harbor", Title: "Midnight Ferry", LengthMs: 200000, CoverURL: generatedCover(t, 6)}}))
	m.Apply(event(t, proto.EventTrackFinished, proto.TrackFinishedEvent{ID: "s-1", Path: "/x.flac"}))
	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateIdle}))
	m.SetInfo(outputGone())
	a.render()
	all = texts(a.win.Content())
	if !a.action.Disabled() || !contains(all, lang.SessionEndedBlocked) || !contains(all, lang.OpenDiagnostics) {
		t.Fatalf("session ended without its folder: Record disabled %v, screen %v", a.action.Disabled(), all)
	}
	snapshot(t, a, "record-session-no-output")
	m.SetInfo(info())
	a.render()
	if a.action.Disabled() || contains(texts(a.win.Content()), lang.OpenDiagnostics) {
		t.Error("Record stays off once the folder is back")
	}
}

// The warning of an output device too poor to record from shows as soon
// as the capture says so, while the session waits for the next track, and
// goes once the device is fit again.
func TestTheLowQualityWarningShowsWhileWaiting(t *testing.T) {
	a, m := connectedTestApp(t, defaults())
	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateWaiting}))
	low := proto.ErrorEvent{Code: proto.ErrorCaptureLowQuality, Msg: "the capture is s16le 16000 Hz 1ch: tracks will sound like a phone call", Source: &proto.AudioSource{SampleRate: 16000, Channels: 1}}
	msg := model.WarningText(low, runtime.GOOS)
	m.Apply(event(t, proto.EventError, low))
	a.render()
	r := a.screens[tabRecord].(*recordScreen)
	if !r.alertRow.Visible() || r.alert.String() != msg {
		t.Fatalf("alert shown %v, text %q", r.alertRow.Visible(), r.alert.String())
	}
	m.Apply(event(t, proto.EventError, proto.ErrorEvent{Code: proto.ErrorCaptureLowQuality}))
	a.render()
	if r.alertRow.Visible() {
		t.Fatalf("alert still shows %q with the device fit again", r.alert.String())
	}
}

// The recorder's warning shows once, on the Record tab, in an outlined
// alert in the error red over the track being recorded, as the info alert
// the session waits in: not as a line of red text over the tabs. It goes
// when the recorder withdraws it.
func TestARecorderWarningShowsInAnAlertOnTheRecordTab(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	feedSession(t, m)
	low := proto.ErrorEvent{Code: proto.ErrorCaptureLowQuality, Msg: "the capture is s16le 16000 Hz 1ch: tracks will sound like a phone call", Source: &proto.AudioSource{SampleRate: 16000, Channels: 1}}
	msg := model.WarningText(low, runtime.GOOS)
	m.Apply(event(t, proto.EventError, low))
	a.render()
	r := a.screens[tabRecord].(*recordScreen)
	if !r.alertRow.Visible() || r.alert.String() != msg || r.alertDiagnose.Visible() {
		t.Fatalf("alert shown %v, text %q, Open diagnostics %v", r.alertRow.Visible(), r.alert.String(), r.alertDiagnose.Visible())
	}
	if p := r.alertRow.Objects[0].(*panel); p.line != theme.ColorNameError || p.fill != "" {
		t.Errorf("alert outline %q, fill %q", p.line, p.fill)
	}
	shown := func(o fyne.CanvasObject) int {
		return len(slices.DeleteFunc(texts(o), func(s string) bool { return s != msg }))
	}
	if all, tab := shown(a.win.Content()), shown(r.root); all == 0 || all != tab {
		t.Errorf("the warning shows %d times, %d on the Record tab", all, tab)
	}
	if !r.card.Visible() || r.alertRow.Position().Y >= r.card.Position().Y {
		t.Errorf("alert at %v, not over the track card at %v", r.alertRow.Position(), r.card.Position())
	}
	a.showTab(tabLibrary)
	a.render()
	if contains(texts(a.win.Content()), msg) {
		t.Error("the warning shows on the Library tab")
	}
	a.showTab(tabRecord)
	// What spytifyd last said no longer holds once it is gone.
	m.SetConn(model.Disconnected, lang.RecorderClosed)
	a.render()
	if r.alertRow.Visible() {
		t.Fatalf("alert still shows %q with spytifyd gone", r.alert.String())
	}
	m.SetConn(model.Connected, "")
	m.Apply(event(t, proto.EventError, proto.ErrorEvent{Code: proto.ErrorCaptureLowQuality}))
	a.render()
	if r.alertRow.Visible() {
		t.Fatalf("alert still shows %q with the device fit again", r.alert.String())
	}
}

// spytifyd killed with no session: Record says why Record is off and offers
// to start it, rather than "Ready to record" beside a Record turned off.
func TestRecordSaysSpytifydIsGone(t *testing.T) {
	a, m := connectedTestApp(t, defaults())

	m.SetConn(model.Disconnected, lang.RecorderClosed)
	a.render()
	all := texts(a.win.Content())
	for _, want := range []string{lang.RecordIsOff, lang.RecordNeedsRecorder, lang.StartRecorder} {
		if !contains(all, want) {
			t.Errorf("%q not on Record once spytifyd is gone: %v", want, all)
		}
	}
	for _, stale := range []string{lang.ReadyToRecord, "Press Record", "Open Player"} {
		if contains(all, stale) {
			t.Errorf("%q still on Record once spytifyd is gone", stale)
		}
	}
	if !a.action.Disabled() {
		t.Error("Record is on without spytifyd")
	}
	snapshot(t, a, "record-spytifyd-gone")

	// A start that failed says why.
	a.startErr = fmt.Sprintf(lang.RecorderStopped, "listen: address already in use")
	a.render()
	if all := texts(a.win.Content()); !contains(all, a.startErr) || !contains(all, lang.StartRecorder) {
		t.Errorf("failed start: %v", all)
	}
	a.startErr = ""

	// Being started, it is no longer offered to start.
	a.starting = true
	a.render()
	if all := texts(a.win.Content()); !contains(all, lang.StartingRecorder) || contains(all, lang.StartRecorder) {
		t.Errorf("spytifyd starting: %v", all)
	}
	a.starting = false

	m.SetConn(model.Connected, "")
	a.render()
	if all := texts(a.win.Content()); !contains(all, lang.ReadyToRecord) || contains(all, lang.StartRecorder) {
		t.Errorf("spytifyd back: %v", all)
	}
}

// spytifyd killed while it records: the track it was on is no longer shown
// as being recorded, and the session's row says spytifyd is missing.
func TestRecordSaysSpytifydIsGoneDuringASession(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	feedSession(t, m)
	a.render()
	r := a.screens[tabRecord].(*recordScreen)
	if !r.card.Visible() {
		t.Fatal("recording: no card for the track")
	}

	m.SetConn(model.Disconnected, lang.RecorderClosed)
	a.render()
	if r.card.Visible() {
		t.Error("spytifyd is gone: the track still shows as being recorded")
	}
	all := texts(a.win.Content())
	for _, want := range []string{fmt.Sprintf(lang.NothingRecordedNotRunning, "spytifyd"), lang.StartRecorder, "Reunion"} {
		if !contains(all, want) {
			t.Errorf("%q not on Record once spytifyd is gone: %v", want, all)
		}
	}
	if r.restart.Visible() || r.launch.Visible() || r.play.Visible() || r.blocked.Visible() {
		t.Error("spytifyd is gone: a button for the player or the folder shows")
	}
	snapshot(t, a, "record-spytifyd-gone-session")

	m.SetConn(model.Connected, "")
	a.render()
	if !r.card.Visible() || r.waitRow.Visible() {
		t.Errorf("spytifyd back: card %v, waiting row %v", r.card.Visible(), r.waitRow.Visible())
	}
}
