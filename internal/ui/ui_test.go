package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	ttwidget "github.com/dweymouth/fyne-tooltip/widget"

	"github.com/spytify/spytify/internal/bus"
	"github.com/spytify/spytify/internal/settings"
	"github.com/spytify/spytify/internal/ui/link"
	"github.com/spytify/spytify/internal/ui/model"
	"github.com/spytify/spytify/pkg/proto"
)

func event(t *testing.T, method string, payload any) proto.Event {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return proto.Event{Method: method, Params: raw}
}

func defaults() map[string]any {
	values := map[string]any{}
	for _, d := range settings.Table {
		values[d.Key] = d.Default
		// As JSON brings them.
		if n, ok := d.Default.(int64); ok {
			values[d.Key] = float64(n)
		}
	}
	values["output.dir"] = "/Users/listener/Music/Spytify"
	values["encode.format"] = "flac"
	return values
}

func info() proto.CoreInfoResult {
	return proto.CoreInfoResult{
		Version: "v2.0.0-alpha", Protocol: proto.Protocol, Platform: "darwin/arm64",
		Target: proto.TargetInfo{Process: "Player", BundleID: "org.example.player"},
		Checks: []proto.Check{
			{Name: "target configured", OK: true, Detail: "SPYTIFY_TARGET_PROCESS=Player"},
			{Name: "player running", OK: true, Detail: "pid 4242"},
			{Name: "ffmpeg found", OK: true, Detail: "ffmpeg version 9.0.2"},
			{Name: "capture helper", OK: true, Detail: "/Applications/Spytify.app/Contents/MacOS/spytify-tap"},
		},
		Warnings: []string{"turn crossfade and gapless playback off in the player"},
	}
}

// outputGone is info with the output folder out of reach, its USB drive
// unplugged.
func outputGone() proto.CoreInfoResult {
	i := info()
	i.Checks = append(i.Checks, proto.Check{Name: proto.CheckOutputWritable, OK: false, Detail: `mkdir L:\Music: The system cannot find the path specified.`})
	return i
}

// newTestApp builds the window over a model the test feeds directly.
func newTestApp(t *testing.T, addr string) (*App, *model.Model, *link.Link) {
	t.Helper()
	fa := test.NewTempApp(t)
	fa.Preferences().SetBool(firstRunKey, true)
	m := model.New()
	l := link.New(addr, m)
	a := New(fa, l, m, Options{StateDir: t.TempDir(), Socket: addr, ManualRender: true, NoUpdateCheck: true})
	t.Cleanup(a.calls.Wait)
	a.win.Resize(fyne.NewSize(920, 620))
	return a, m, l
}

// generatedCover is testdata/covers/cover-NN.jpg, drawn by gencovers, as the
// file URL the fake player hands out, so the pictures carry art as a real
// player's tracks do.
func generatedCover(t *testing.T, n int) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "..", "testdata", "covers", fmt.Sprintf("cover-%02d.jpg", n)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return (&url.URL{Scheme: "file", Path: p}).String()
}

func feedSession(t *testing.T, m *model.Model) {
	m.SetConn(model.Connected, "")
	m.SetInfo(info())
	m.SetSettings(defaults())
	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateWaiting}))
	m.Apply(event(t, proto.EventTargetState, proto.TargetStateEvent{Running: true, PID: 4242}))
	for i, title := range []string{"Intro", "Reunion", "Wait"} {
		id := "s-00" + string(rune('1'+i))
		m.Apply(event(t, proto.EventTrackStarted, proto.TrackStartedEvent{Track: proto.Track{ID: id, Artist: "M83", Title: title, Album: "Hurry Up, We're Dreaming", LengthMs: 134000 + int64(i)*20000, CoverURL: generatedCover(t, i+1)}}))
		switch i {
		case 2:
			m.Apply(event(t, proto.EventTrackSkipped, proto.TrackSkippedEvent{ID: id, Reason: proto.SkipDuplicate}))
		default:
			m.Apply(event(t, proto.EventTrackFinished, proto.TrackFinishedEvent{ID: id, Path: "/Users/listener/Music/Spytify/M83/" + title + ".flac", DurationMs: 134000 + int64(i)*102000, Bytes: 18_200_000 + int64(i)*12_800_000, Tagged: true, TaggedBy: "lastfm"}))
		}
	}
	m.Apply(event(t, proto.EventTrackStarted, proto.TrackStartedEvent{Track: proto.Track{ID: "s-004", Artist: "M83", Title: "Midnight City", Album: "Hurry Up, We're Dreaming", LengthMs: 243000, CoverURL: generatedCover(t, 4)}}))
	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateRecording}))
	m.Apply(event(t, proto.EventLevel, proto.LevelEvent{PeakDBFS: -3, RMSDBFS: -14}))
	m.Apply(event(t, proto.EventLog, proto.LogEvent{Level: "info", Msg: "session 20261023-120000 started", TS: time.Now().Format(time.RFC3339Nano)}))
	m.Apply(event(t, proto.EventLog, proto.LogEvent{Level: "error", Msg: "\"Wait\": already recorded", TS: time.Now().Format(time.RFC3339Nano)}))
}

// coversIn draws again until the covers the window asked for are in: they
// load off the UI thread, and the tests draw by hand.
func coversIn(t *testing.T, a *App) {
	t.Helper()
	state := func() (asked, loading bool) {
		a.covers.mu.Lock()
		defer a.covers.mu.Unlock()
		return a.covers.done > 0 || len(a.covers.pending) > 0, len(a.covers.pending) > 0
	}
	if asked, _ := state(); !asked {
		return
	}
	for {
		eventually(t, "the covers never loaded", func() bool { _, loading := state(); return !loading })
		a.render()
		if _, loading := state(); !loading {
			return
		}
	}
}

// snapshot saves the window as PNG when SPYTIFY_UI_SNAPSHOTS names a folder,
// once the covers it shows are in.
func snapshot(t *testing.T, a *App, name string) {
	t.Helper()
	coversIn(t, a)
	dir := os.Getenv("SPYTIFY_UI_SNAPSHOTS")
	if dir == "" {
		return
	}
	img := a.win.Canvas().Capture()
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

// texts collects every label and button text under o.
func texts(o fyne.CanvasObject) []string {
	var out []string
	var walk func(fyne.CanvasObject)
	walk = func(o fyne.CanvasObject) {
		if o == nil || !o.Visible() {
			return
		}
		if b, ok := asButton(o); ok {
			out = append(out, b.Text)
		}
		switch w := o.(type) {
		case *widget.Label:
			out = append(out, w.Text)
		case *widget.RichText:
			for _, seg := range w.Segments {
				if ts, ok := seg.(*widget.TextSegment); ok {
					out = append(out, ts.Text)
				}
			}
		case *canvas.Text:
			out = append(out, w.Text)
		case *widget.Hyperlink:
			out = append(out, w.Text)
		case *widget.Card:
			out = append(out, w.Title)
			walk(w.Content)
			return
		case *fyne.Container:
			for _, c := range w.Objects {
				walk(c)
			}
			return
		}
		if w, ok := o.(fyne.Widget); ok {
			r := test.WidgetRenderer(w)
			for _, c := range r.Objects() {
				walk(c)
			}
		}
	}
	walk(o)
	return out
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if strings.Contains(s, want) {
			return true
		}
	}
	return false
}

func TestRecordScreenShowsTheSession(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	feedSession(t, m)
	a.render()
	all := texts(a.win.Content())
	for _, want := range []string{"Recording", "Midnight City", "M83 · Hurry Up, We're Dreaming", "Reunion", "Saved", "Skipped: duplicate",
		"2 saved · 1 skipped · 0 ads", "Stop", "Player is running", "FLAC", "Skip"} {
		if !contains(all, want) {
			t.Errorf("%q not on screen", want)
		}
	}
	// The rows' tags were found online: their chips still say Saved.
	if contains(all, "Tagged") {
		t.Error("a saved track's chip says Tagged")
	}
	// The app bar's Donate is a heart, as in v1; About has the worded one.
	if contains(all, "♥ Donate") || !a.donate.Visible() || a.donate.Icon != heartIcon || a.donate.ToolTip() != "Donate" {
		t.Error("the app bar's Donate is not a heart alone")
	}
	if a.tabs.Items[tabRecord].Text != "Record · 2" {
		t.Errorf("Record tab title %q", a.tabs.Items[tabRecord].Text)
	}
	r := a.screens[tabRecord].(*recordScreen)
	if r.clear.Disabled() {
		t.Error("Clear list is disabled with rows in the list")
	}
	if a.action.Text != "Stop" || a.action.Importance != widget.DangerImportance {
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
	if !r.clear.Disabled() || !contains(texts(a.win.Content()), "No files yet") {
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
	if got, want := badges(), []string{"Recording"}; !slices.Equal(got, want) {
		t.Errorf("card badges %q, want %q", got, want)
	}

	m.Apply(event(t, proto.EventTrackFinished, proto.TrackFinishedEvent{ID: "s-004", Path: "/Users/listener/Music/Spytify/M83/Midnight City.flac", DurationMs: 243000, Bytes: 31_400_000, Tagged: true, TaggedBy: "lastfm"}))
	m.Apply(event(t, proto.EventTrackStarted, proto.TrackStartedEvent{Track: proto.Track{ID: "s-005", Artist: "M83", Title: "Outro", Album: "Hurry Up, We're Dreaming", LengthMs: 247000},
		Source: &proto.AudioSource{SampleRate: 96000, Channels: 2}}))
	a.render()
	if got, want := badges(), []string{"Recording", "96 kHz", "Stereo", "48 kHz file"}; !slices.Equal(got, want) {
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
	var inside func(fyne.CanvasObject)
	inside = func(o fyne.CanvasObject) {
		if !o.Visible() {
			return
		}
		p, s := d.AbsolutePositionForObject(o), o.Size()
		if p.X < box.X || p.X+s.Width > box.X+panel.Size().Width {
			t.Errorf("%T at x %v to %v, out of the card's %v to %v", o, p.X, p.X+s.Width, box.X, box.X+panel.Size().Width)
		}
		switch w := o.(type) {
		case *fyne.Container:
			for _, c := range w.Objects {
				inside(c)
			}
		case fyne.Widget:
			for _, c := range test.WidgetRenderer(w).Objects() {
				inside(c)
			}
		}
	}
	inside(panel)
	snapshot(t, a, "record-96khz-narrow")
}

// A player paused mid-track keeps the track's card, its badge saying so,
// with the info bar above it offering to play.
func TestAPausedTrackHasTheInfoBarAboveItsCard(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	feedSession(t, m)
	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StatePaused}))
	a.render()
	r := a.screens[tabRecord].(*recordScreen)
	if !r.card.Visible() || r.state.text != "Paused" || r.meter.Visible() {
		t.Errorf("paused card: shown %v, badge %q, meter shown %v", r.card.Visible(), r.state.text, r.meter.Visible())
	}
	if !r.waitRow.Visible() || !strings.HasSuffix(r.waiting.String(), "is paused: press play there.") || !r.play.Visible() || r.restart.Visible() {
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
	if r.waitRow.Visible() || r.state.text != "Recording" || !r.meter.Visible() {
		t.Errorf("playing again: info bar %v, badge %q, meter %v", r.waitRow.Visible(), r.state.text, r.meter.Visible())
	}
}

// drawnRow is a track's row in a list as drawn: the row, and its lines of
// text top to bottom.
type drawnRow struct {
	row   *fyne.Container
	text  *widget.RichText
	lines []*canvas.Text
}

// drawnRows is the rows of a list of tracks as drawn, by their whole title.
func drawnRows(t *testing.T, list fyne.CanvasObject) map[string]drawnRow {
	t.Helper()
	out := map[string]drawnRow{}
	var walk func(fyne.CanvasObject)
	walk = func(o fyne.CanvasObject) {
		if !o.Visible() {
			return
		}
		switch w := o.(type) {
		case *fyne.Container:
			// A track's row: its text, centred in the middle, then its
			// cover and the parts after its text.
			if len(w.Objects) == 3 {
				if box, ok := w.Objects[0].(*fyne.Container); ok && len(box.Objects) == 3 {
					if rt, ok := box.Objects[1].(*widget.RichText); ok {
						row := drawnRow{row: w, text: rt}
						y := float32(-1)
						for _, o := range test.WidgetRenderer(rt).Objects() {
							if txt, ok := o.(*canvas.Text); ok && txt.Text != "" {
								if txt.Position().Y <= y {
									t.Errorf("%q is not on a line of its own", txt.Text)
								}
								y = txt.Position().Y
								row.lines = append(row.lines, txt)
							}
						}
						out[rt.Segments[0].(*widget.TextSegment).Text] = row
						return
					}
				}
			}
			for _, c := range w.Objects {
				walk(c)
			}
		case fyne.Widget:
			for _, c := range test.WidgetRenderer(w).Objects() {
				walk(c)
			}
		}
	}
	walk(list)
	return out
}

// words is a row's lines as text.
func words(lines []*canvas.Text) []string {
	var out []string
	for _, txt := range lines {
		out = append(out, txt.Text)
	}
	return out
}

// insideRows checks that every row's text stays in its box, and that the
// parts after it stay after it, in the row.
func insideRows(t *testing.T, rows map[string]drawnRow) {
	t.Helper()
	d := fyne.CurrentApp().Driver()
	for title, row := range rows {
		after := row.row.Objects[2]
		box, end := d.AbsolutePositionForObject(row.text), d.AbsolutePositionForObject(row.row).X+row.row.Size().Width
		right := box.X + row.text.Size().Width
		for _, txt := range row.lines {
			if x := box.X + txt.Position().X; x < box.X || x+fyne.MeasureText(txt.Text, txt.TextSize, txt.TextStyle).Width > right {
				t.Errorf("%s row: %q is past its text's edges", title, txt.Text)
			}
		}
		if x := d.AbsolutePositionForObject(after).X; x < right || x+after.Size().Width > end {
			t.Errorf("%s row: the parts after the text at x %v to %v, the text ending at %v and the row at %v", title, x, x+after.Size().Width, right, end)
		}
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
		"Reunion":      {"Reunion", "M83", "3:56 · FLAC · 29.6 MB"},
		"Wait":         {"Wait", "M83", "2:54"},
		"Intermission": {"Intermission", "1:02 · FLAC · 36.3 MB"},
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
		"Splendor": {"Splendor", longArtist, "5:13 · FLAC · 36.3 MB"},
		longTitle:  {longTitle, "M83", "4:07 · FLAC · 36.3 MB"},
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

// A Library row is a file's title over its artists, as the player lists a
// track: as its tags say once read, as its name says until then and for a
// file without tags. At the narrowest the window goes, a long line ends in an
// ellipsis and leaves the buttons in the row.
func TestALibraryRowHasTheTitleThenTheArtists(t *testing.T) {
	ff, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	longTitle := "Outro (Extended Mix, Live in a Very Large Concert Hall With the Full Orchestra and the Choir, Second Night)"
	longArtist := "M83 with The Hurry Up Orchestra, The Choir of the Night, The Second Night Strings, The Brass of the Rain and The Children's Voices"
	tagged := func(name, title, artist string) string {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(ff, "-v", "error", "-nostdin", "-f", "lavfi", "-t", "0.5", "-i", "anullsrc", "-metadata", "title="+title, "-metadata", "artist="+artist, path)
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("make %s: %v: %s", name, err, b)
		}
		return path
	}
	untagged := func(name, data string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	// Newest first: the Library's order.
	files := []string{
		tagged("Outro.flac", longTitle, longArtist),
		tagged(filepath.Join("Hurry Up, We're Dreaming", "03 Reunion.flac"), "Reunion", "M83"),
		untagged("Splendor.wav", "RIFF"),
		untagged("M83 - Wait.mp3", ""),
		untagged("Intermission.mp3", ""),
	}
	untagged("Splendor.wav.json", `{"title": "Splendor", "performers": ["M83", "The Hurry Up Orchestra"]}`)
	for i, path := range files {
		at := time.Now().Add(-time.Duration(i) * time.Minute)
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
	}
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	values := defaults()
	values["output.dir"] = dir
	m.SetConn(model.Connected, "")
	m.SetInfo(info())
	m.SetSettings(values)
	lib := a.screens[tabLibrary].(*libraryScreen)
	// Until the tags are read, the names say what they can. Taking the
	// reads' slots holds them back.
	for range cap(lib.tagged.slots) {
		lib.tagged.slots <- struct{}{}
	}
	a.showTab(tabLibrary)
	a.render()
	all := drawnRows(t, lib.list)
	for title, want := range map[string][]string{
		"Outro":        {"Outro"},
		"03 Reunion":   {"03 Reunion"},
		"Splendor":     {"Splendor"},
		"Wait":         {"Wait", "M83"},
		"Intermission": {"Intermission"},
	} {
		if got := words(all[title].lines); !slices.Equal(got, want) {
			t.Errorf("before the tags, %s row %q, want %q", title, got, want)
		}
	}

	for range cap(lib.tagged.slots) {
		<-lib.tagged.slots
	}
	eventually(t, "the tags are never read", func() bool {
		lib.tagged.mu.Lock()
		defer lib.tagged.mu.Unlock()
		return len(lib.tagged.read) == len(files)
	})
	lib.list.Refresh()
	all = drawnRows(t, lib.list)
	for title, want := range map[string][]string{
		"Reunion":      {"Reunion", "M83"},
		"Splendor":     {"Splendor", "M83, The Hurry Up Orchestra"},
		"Wait":         {"Wait", "M83"},
		"Intermission": {"Intermission"},
	} {
		if got := words(all[title].lines); !slices.Equal(got, want) {
			t.Errorf("%s row %q, want %q", title, got, want)
		}
	}
	// The same look as the Record list's rows: the title in its style, the
	// artists in the caption's, rows as tall, a lone title in the middle.
	if lines := all["Reunion"].lines; len(lines) == 2 {
		if title, artist := lines[0], lines[1]; !title.TextStyle.Bold || title.TextSize <= artist.TextSize || artist.TextStyle.Bold || artist.Color == title.Color {
			t.Errorf("the artists are drawn like the title: %+v, %+v", title, artist)
		}
	}
	for title, row := range all {
		if h := all["Reunion"].row.Size().Height; row.row.Size().Height != h {
			t.Errorf("%s row is %v tall, the others %v", title, row.row.Size().Height, h)
		}
	}
	if full, short := all["Reunion"].text, all["Intermission"].text; full.Position().Y != 0 || short.Position().Y < (full.Size().Height-short.Size().Height)/2-0.5 {
		t.Errorf("the row with no artist has its text at y %v, the others at %v", short.Position().Y, full.Position().Y)
	}
	// The pictures show a listener's folder, not this test's.
	setCaption(lib.count, pluralFiles(len(files), len(files), "/Users/listener/Music/Spytify"))
	snapshot(t, a, "library-rows")

	a.win.Resize(a.win.Content().MinSize())
	a.render()
	all = drawnRows(t, lib.list)
	if got := words(all[longTitle].lines); len(got) != 2 || !strings.HasPrefix(got[0], "Outro (Extended Mix, ") || !strings.HasSuffix(got[0], "…") ||
		!strings.HasPrefix(got[1], "M83 with The") || !strings.HasSuffix(got[1], "…") {
		t.Errorf("a long title and long artists do not end in an ellipsis: %q", got)
	}
	insideRows(t, all)
	snapshot(t, a, "library-rows-narrow")
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
	if !contains(all, "Skipped: incomplete") || !contains(all, "Keep anyway") {
		t.Errorf("no Keep anyway on the take cut short: %v", all)
	}
	snapshot(t, a, "record-keep")

	m.Apply(event(t, proto.EventTrackKeepable, proto.TrackKeepableEvent{ID: "s-004"}))
	a.render()
	if contains(texts(a.win.Content()), "Keep anyway") {
		t.Error("Keep anyway stays once the take is let go")
	}
}

func TestWaitingOffersToRestartTheTrack(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	m.SetConn(model.Connected, "")
	m.SetInfo(info())
	m.SetSettings(defaults())
	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateWaiting}))
	a.render()
	all := texts(a.win.Content())
	if !contains(all, "Waiting for the next track to start.") || !contains(all, "Record this track from the start") {
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
			if b, _ := asButton(o); o.Visible() != (o == want) {
				t.Errorf("%q shown %v", b.Text, o.Visible())
			}
		}
	}

	// The player was closed since: Record is off, the list stays, and the
	// row offers to open the player.
	m.Apply(event(t, proto.EventTargetState, proto.TargetStateEvent{Running: false}))
	offers("Nothing is recorded while Player is not running.", r.launch)
	if r.launch.Text != "Open Player" || !a.action.Disabled() || len(r.rows) != 1 {
		t.Errorf("the button reads %q, Record disabled %v, %d rows", r.launch.Text, a.action.Disabled(), len(r.rows))
	}
	snapshot(t, a, "waiting-player-closed")
	test.Tap(r.launch)
	waitCall(t, core, proto.MethodPlayerLaunch)
	m.Apply(event(t, proto.EventTargetState, proto.TargetStateEvent{Running: true, PID: 4242}))
	offers("Session ended.", nil)

	// It quits during a session.
	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateWaiting}))
	m.Apply(event(t, proto.EventTargetState, proto.TargetStateEvent{Running: false}))
	offers("Nothing is recorded while Player is not running.", r.launch)

	// It opens again, paused.
	m.Apply(event(t, proto.EventTargetState, proto.TargetStateEvent{Running: true, PID: 4243}))
	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StatePaused}))
	offers("Nothing is recorded while Player is paused: press play there.", r.play)
	snapshot(t, a, "waiting-player-paused")
	test.Tap(r.play)
	deadline := time.Now().Add(5 * time.Second)
	for {
		core.mu.Lock()
		controls := slices.Clone(core.controls)
		core.mu.Unlock()
		if slices.Equal(controls, []string{proto.CmdPlay}) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("player.control sent %q, want play", controls)
		}
		time.Sleep(20 * time.Millisecond)
	}

	// It plays the track it was on, which started before the session.
	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateWaiting}))
	offers("Waiting for the next track to start.", r.restart)
}

func TestIdleRecordScreenIsJustAMessage(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	m.SetConn(model.Connected, "")
	m.SetInfo(info())
	m.SetSettings(defaults())
	a.render()
	all := texts(a.win.Content())
	for _, want := range []string{"Ready to record", "Press Record, then play music in Player."} {
		if !contains(all, want) {
			t.Errorf("%q not on the idle screen", want)
		}
	}
	for _, unwanted := range []string{"pid 4242", "capture helper", "Open diagnostics"} {
		if contains(all, unwanted) {
			t.Errorf("%q is on the idle Record screen; the checks live in Logs", unwanted)
		}
	}
	if n := len(buttons(a.win.Content(), "Record")); n != 1 {
		t.Errorf("%d Record buttons; want the app bar's only", n)
	}
	snapshot(t, a, "idle")

	// A player that is not running is offered to open, not diagnosed.
	down := info()
	down.Checks[1].OK = false
	m.SetInfo(down)
	a.render()
	if all := texts(a.win.Content()); !contains(all, "Open Player") || contains(all, "Open diagnostics") {
		t.Errorf("player not running: %v", all)
	}
	// It starts: the check follows the target's state, and the offer goes.
	m.Apply(event(t, proto.EventTargetState, proto.TargetStateEvent{Running: true, PID: 99}))
	a.render()
	if all := texts(a.win.Content()); contains(all, "Open Player") || !checkOK(m.Snapshot().Info.Checks, "player running") {
		t.Errorf("player started: %v", all)
	}
	// Any other failing check sends the user to the diagnostics.
	failing := info()
	failing.Checks[2].OK = false
	m.SetInfo(failing)
	a.render()
	if !contains(texts(a.win.Content()), "Open diagnostics") {
		t.Error("no way to the diagnostics while a check fails")
	}
}

func TestOfflineScreen(t *testing.T) {
	a, m, _ := newTestApp(t, "/tmp/spytify.sock")
	m.SetConn(model.Disconnected, "spytifyd is not running")
	a.render()
	all := texts(a.win.Content())
	for _, want := range []string{"The recorder is not running", "spytifyd is not running", "Start the recorder", "/tmp/spytify.sock", "Player process"} {
		if !contains(all, want) {
			t.Errorf("%q not on the offline screen", want)
		}
	}
	if !a.action.Disabled() {
		t.Error("Record is enabled without a daemon")
	}
	snapshot(t, a, "offline")
}

func TestOtherScreensRender(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	feedSession(t, m)
	a.render()
	for i, name := range map[int]string{tabLibrary: "library", tabSettings: "settings", tabLogs: "logs", tabAbout: "about"} {
		a.showTab(i)
		a.render()
		snapshot(t, a, name)
	}

	a.showTab(tabSettings)
	a.render()
	all := texts(a.win.Content())
	for _, want := range []string{"App", "Minimize to tray", "Output", "Output folder", "Group by folders", "Encoding", "Output format",
		"Recording", "Normalize volume", "Ads", "Mute ads", "Advanced", "Reset"} {
		if !contains(all, want) {
			t.Errorf("%q not on the settings screen", want)
		}
	}
	// Advanced settings are folded, and no field carries a caption.
	for _, unwanted := range []string{"File name template", "Player process", "Embed album cover", "output.dir", "Log level", "Language"} {
		if contains(all, unwanted) {
			t.Errorf("%q shows on the settings screen", unwanted)
		}
	}
	if g := groups(); g[0] != "App" {
		t.Errorf("sections %v: App is not first", g)
	}
	s := a.screens[tabSettings].(*settingsScreen)
	s.search.SetText("cover")
	a.render()
	if all := texts(a.win.Content()); !contains(all, "Embed album cover") || contains(all, "Output folder") {
		t.Errorf("search for cover: %v", all)
	}
	s.search.SetText("player")
	a.render()
	if all := texts(a.win.Content()); !contains(all, "Player process") || contains(all, "Restart spytifyd") {
		t.Errorf("search for player: %v", all)
	}
	s.search.SetText("")

	a.showTab(tabLogs)
	a.render()
	all = texts(a.win.Content())
	for _, want := range []string{"Diagnostics", "already recorded", "session 20261023-120000 started"} {
		if !contains(all, want) {
			t.Errorf("%q not on the logs screen", want)
		}
	}
	tapButton(t, a.win.Content(), "Diagnostics")
	if d := texts(overlay(t, a)); !contains(d, "All 4 checks pass.") || !contains(d, "ffmpeg found") {
		t.Errorf("the diagnostics: %v", d)
	}
	snapshot(t, a, "logs-diagnostics")
	tapButton(t, overlay(t, a), "Close")

	a.showTab(tabAbout)
	a.render()
	all = texts(a.win.Content())
	for _, want := range []string{"recorder v2.0.0-alpha on darwin/arm64", "Source code on GitHub", "Project page", "Check for updates", "♥ Donate"} {
		if !contains(all, want) {
			t.Errorf("%q not on the about screen", want)
		}
	}
}

func TestLibraryRescansWhenShown(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	dir := t.TempDir()
	values := defaults()
	values["output.dir"] = dir
	m.SetConn(model.Connected, "")
	m.SetInfo(info())
	m.SetSettings(values)
	a.render()
	lib := a.screens[tabLibrary].(*libraryScreen)

	a.showTab(tabLibrary)
	a.render()
	if len(lib.shown) != 0 {
		t.Fatalf("%d files in an empty folder", len(lib.shown))
	}
	// A file lands while another tab is open; coming back lists it.
	a.showTab(tabRecord)
	if err := os.WriteFile(filepath.Join(dir, "Artist - Title.flac"), []byte("fLaC"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.showTab(tabLibrary)
	a.render()
	if len(lib.shown) != 1 || !slices.Equal(words(drawnRows(t, lib.list)["Title"].lines), []string{"Title", "Artist"}) {
		t.Errorf("library after coming back: %d files", len(lib.shown))
	}
}

func TestThemeFollowsTheSetting(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	values := defaults()
	values["app.theme"] = "light"
	m.SetConn(model.Connected, "")
	m.SetInfo(info())
	m.SetSettings(values)
	a.render()
	th := a.fa.Settings().Theme()
	// The green replaces the blue, whatever variant the OS asks for.
	if got := th.Color(theme.ColorNamePrimary, theme.VariantDark); got != greenLight {
		t.Errorf("light theme primary = %v", got)
	}
	values["app.theme"] = "dark"
	m.SetSettings(values)
	a.render()
	if got := a.fa.Settings().Theme().Color(theme.ColorNamePrimary, theme.VariantLight); got != greenDark {
		t.Errorf("dark theme primary = %v", got)
	}
	values["app.theme"] = "system"
	m.SetSettings(values)
	a.render()
	if got := a.fa.Settings().Theme().Color(theme.ColorNamePrimary, theme.VariantLight); got != greenLight {
		t.Errorf("system theme on a light OS = %v", got)
	}
}

func TestNewerVersion(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"v2.0.0", "v2.0.0-alpha+f5dd96e", true},
		{"v2.0.1", "v2.0.0", true},
		{"v2.0.0", "v2.0.0", false},
		{"v2.0.0-beta", "v2.0.0-alpha", true},
		{"v2.0.0-alpha", "v2.0.0-beta", false},
		{"v2.0.0-alpha.10", "v2.0.0-alpha.9", true},
		{"v2.0.0-alpha.1", "v2.0.0-alpha", true},
		{"v2.0.0-alpha", "v2.0.0-alpha+f5dd96e", false},
		{"1.12", "v2.0.0-alpha", false},
		{"", "v2.0.0", false},
		// A development build is after the tag it was made from (one
		// patch on), so it is never offered that tag, nor the release the
		// tag led to; the next patch release is newer.
		{"v2.0.0-alpha.1", "v2.0.1-dev.12", false},
		{"v2.0.0", "v2.0.1-dev.1", false},
		{"v2.0.1", "v2.0.1-dev.12", true},
		{"v2.0.0-alpha.1", "v0.0.0-dev.0", true},
	}
	for _, c := range cases {
		if got := newer(c.latest, c.current); got != c.want {
			t.Errorf("newer(%q, %q) = %v", c.latest, c.current, got)
		}
	}
}

// recordingCore is a fake daemon that remembers what the UI asked.
type recordingCore struct {
	mu    sync.Mutex
	calls []string
	sets  []map[string]any
	stops []proto.SessionStopParams
	// controls are the player.control commands, in order.
	controls []string
	// noOutput is an output folder that cannot be written to: its check
	// fails and no session starts.
	noOutput bool
}

func (c *recordingCore) setNoOutput(gone bool) {
	c.mu.Lock()
	c.noOutput = gone
	c.mu.Unlock()
}

func (c *recordingCore) handle(ctx context.Context, method string, params json.RawMessage) (any, error) {
	c.mu.Lock()
	c.calls = append(c.calls, method)
	if method == proto.MethodSettingsSet {
		var p proto.SettingsSetParams
		_ = json.Unmarshal(params, &p)
		c.sets = append(c.sets, p.Values)
	}
	if method == proto.MethodSessionStop {
		var p proto.SessionStopParams
		_ = json.Unmarshal(params, &p)
		c.stops = append(c.stops, p)
	}
	if method == proto.MethodPlayerControl {
		var p proto.PlayerControlParams
		_ = json.Unmarshal(params, &p)
		c.controls = append(c.controls, p.Cmd)
	}
	noOutput := c.noOutput
	c.mu.Unlock()
	switch method {
	case proto.MethodCoreInfo:
		if noOutput {
			return outputGone(), nil
		}
		return info(), nil
	case proto.MethodSessionStart:
		if noOutput {
			return nil, proto.Errorf(proto.CodeOutputDir, "the output folder cannot be written to: mkdir L:\\Music: The system cannot find the path specified.")
		}
	case proto.MethodSettingsGet:
		return proto.SettingsValues{Values: defaults()}, nil
	case proto.MethodLogTail:
		return proto.LogTailResult{}, nil
	case proto.MethodEventsRecent:
		return proto.EventsRecentResult{}, nil
	case proto.MethodSessionStatus:
		return proto.SessionStatusResult{State: proto.StateIdle}, nil
	case proto.MethodSettingsSet:
		return proto.SettingsSetResult{Applied: []string{"x"}, Rejected: map[string]string{}}, nil
	}
	return proto.Empty{}, nil
}

func (c *recordingCore) called(method string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, m := range c.calls {
		if m == method {
			return true
		}
	}
	return false
}

func TestButtonsSendActions(t *testing.T) {
	dir, err := os.MkdirTemp("", "spyui")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	core := &recordingCore{}
	srv := bus.NewServer(core.handle, nil)
	sock := filepath.Join(dir, "s.sock")
	if err := srv.ListenUnix(sock); err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	a, m, l := newTestApp(t, sock)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go l.Run(ctx)
	deadline := time.Now().Add(5 * time.Second)
	for m.Snapshot().Conn != model.Connected {
		if time.Now().After(deadline) {
			t.Fatal("UI did not connect")
		}
		time.Sleep(20 * time.Millisecond)
	}
	a.render()

	test.Tap(a.action)
	waitCall(t, core, proto.MethodSessionStart)

	a.showTab(tabSettings)
	a.render()
	s := a.screens[tabSettings].(*settingsScreen)
	sw, ok := s.fields["output.underscores"].input.(*toggle)
	if !ok {
		t.Fatal("no switch for output.underscores")
	}
	test.Tap(sw)
	waitCall(t, core, proto.MethodSettingsSet)
	core.mu.Lock()
	last := core.sets[len(core.sets)-1]
	core.mu.Unlock()
	if last["output.underscores"] != true {
		t.Fatalf("settings.set sent %v", last)
	}

	// A text setting applies when it loses focus, once, if it changed.
	e := s.fields["output.template"].input.(*applyEntry)
	e.FocusLost()
	e.SetText("{title}")
	e.FocusLost()
	e.FocusLost()
	waitSets(t, core, 2)
	core.mu.Lock()
	sets := append([]map[string]any(nil), core.sets...)
	core.mu.Unlock()
	if len(sets) != 2 || sets[1]["output.template"] != "{title}" {
		t.Fatalf("settings.set calls %v", sets)
	}

	// A choice shows its words and applies its value.
	sel := s.fields["output.prefix"].input.(*widget.Select)
	sel.SetSelected("Album track number (04)")
	waitSets(t, core, 3)
	core.mu.Lock()
	last = core.sets[len(core.sets)-1]
	core.mu.Unlock()
	if last["output.prefix"] != "album_track" {
		t.Fatalf("choosing Album track number sent %v", last)
	}

	// Quitting the app stops the recorder too.
	a.stopDaemon()
	waitCall(t, core, proto.MethodCoreShutdown)
}

// Fyne 2.8.2 can skip its stop hook at quit, hang or panic
// (fyne-io/fyne#6634): the app runs it, and stops the recorder, once the
// loop has returned.
func TestQuittingStopsTheRecorderOnceTheLoopHasReturned(t *testing.T) {
	dir, err := os.MkdirTemp("", "spyui")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	core := &recordingCore{}
	srv := bus.NewServer(core.handle, nil)
	sock := filepath.Join(dir, "s.sock")
	if err := srv.ListenUnix(sock); err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	fa := test.NewTempApp(t)
	fa.Preferences().SetBool(firstRunKey, true)
	hook := fa.Lifecycle().(fyneStopHook)
	saved := 0
	hook.SetOnStoppedHookExecuted(func() { saved++ })
	m := model.New()
	l := link.New(sock, m)
	a := New(fa, l, m, Options{StateDir: t.TempDir(), Socket: sock, ManualRender: true, NoUpdateCheck: true})
	t.Cleanup(a.calls.Wait)
	if hook.OnStopped() != nil {
		t.Fatal("Fyne is left a stop hook to run at quit")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go l.Run(ctx)
	deadline := time.Now().Add(5 * time.Second)
	for m.Snapshot().Conn != model.Connected {
		if time.Now().After(deadline) {
			t.Fatal("UI did not connect")
		}
		time.Sleep(20 * time.Millisecond)
	}

	// The test driver's loop returns at once, as Fyne's does at quit.
	a.ShowAndRun()
	waitCall(t, core, proto.MethodCoreShutdown)
	if saved != 1 {
		t.Errorf("Fyne's stop hook ran %d times, want once", saved)
	}
}

func TestQuitWhileRecordingAsksFirst(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	feedSession(t, m)
	a.render()
	a.quit()
	top := a.win.Canvas().Overlays().Top()
	if top == nil || !contains(texts(top), "Quit Spytify?") {
		t.Fatal("quitting during a recording did not ask")
	}
}

func waitSets(t *testing.T, c *recordingCore, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		c.mu.Lock()
		got := len(c.sets)
		c.mu.Unlock()
		if got >= n {
			time.Sleep(100 * time.Millisecond)
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d settings.set calls, want %d", got, n)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func waitCall(t *testing.T, c *recordingCore, method string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !c.called(method) {
		if time.Now().After(deadline) {
			t.Fatalf("%s was not sent; calls: %v", method, c.calls)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func buttons(o fyne.CanvasObject, text string) []*widget.Button {
	var out []*widget.Button
	for _, o := range buttonObjects(o, text) {
		b, _ := asButton(o)
		out = append(out, b)
	}
	return out
}

// buttonObjects are the buttons showing text under o as the window holds
// them, the app's own included, to find where they are drawn.
func buttonObjects(o fyne.CanvasObject, text string) []fyne.CanvasObject {
	var out []fyne.CanvasObject
	var walk func(fyne.CanvasObject)
	walk = func(o fyne.CanvasObject) {
		if o == nil || !o.Visible() {
			return
		}
		if b, ok := asButton(o); ok {
			if b.Text == text {
				out = append(out, o)
			}
			return
		}
		switch w := o.(type) {
		case *fyne.Container:
			for _, c := range w.Objects {
				walk(c)
			}
			return
		}
		if w, ok := o.(fyne.Widget); ok {
			for _, c := range test.WidgetRenderer(w).Objects() {
				walk(c)
			}
		}
	}
	walk(o)
	return out
}

// asButton is the widget.Button of a button, Fyne's or one of the app's
// built on it, to read and tap as Fyne's.
func asButton(o fyne.CanvasObject) (*widget.Button, bool) {
	switch b := o.(type) {
	case *widget.Button:
		return b, true
	case *button:
		return &b.Button, true
	case *ttwidget.Button:
		return &b.Button, true
	case *tipButton:
		return &b.Button.Button, true
	case *litButton:
		return &b.Button, true
	}
	return nil, false
}

func TestLatestReleaseCountsPreReleases(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `[{"tag_name":"v2.1.0","html_url":"https://x/draft","draft":true},
			{"tag_name":"v2.0.0-alpha.2","html_url":"https://x/a2","draft":false,"prerelease":true},
			{"tag_name":"v2.0.0-alpha.1","html_url":"https://x/a1","draft":false,"prerelease":true}]`)
	}))
	defer srv.Close()
	defer func(u string) { releasesURL = u }(releasesURL)
	releasesURL = srv.URL
	tag, page, err := latestRelease(context.Background())
	if err != nil || tag != "v2.0.0-alpha.2" || page != "https://x/a2" {
		t.Fatalf("latest = %q %q %v", tag, page, err)
	}
	if !newer(tag, "v2.0.0-alpha.1+f5dd96e") {
		t.Fatal("alpha.2 is not newer than alpha.1")
	}
}

func TestANewerReleaseShowsTheBadge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `[{"tag_name":"v9.0.0","html_url":"https://x/v9","draft":false}]`)
	}))
	defer srv.Close()
	defer func(u string) { releasesURL = u }(releasesURL)
	releasesURL = srv.URL
	defer func(v string) { proto.Version = v }(proto.Version)
	proto.Version = "v2.0.0"
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	feedSession(t, m)
	a.render()
	if a.update.Visible() {
		t.Fatal("Update shows before any release was found")
	}
	a.checkUpdate()
	if !a.update.Visible() || a.update.Icon != newReleasesIcon || a.update.Text != "Update to v9.0.0" {
		t.Fatalf("Update: visible %v, text %q", a.update.Visible(), a.update.Text)
	}
	snapshot(t, a, "update")
	light := defaults()
	light["app.theme"] = "light"
	m.SetSettings(light)
	a.render()
	snapshot(t, a, "update-light")
}

// A development build, local or from CI, does not look for releases.
func TestADevelopmentBuildShowsNoBadge(t *testing.T) {
	var asked atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		asked.Store(true)
		fmt.Fprint(w, `[{"tag_name":"v9.0.0","html_url":"https://x/v9","draft":false}]`)
	}))
	defer srv.Close()
	defer func(u string) { releasesURL = u }(releasesURL)
	releasesURL = srv.URL
	for _, v := range []string{"v2.0.1-dev.12", "v0.0.0-dev"} {
		func() {
			defer func(old string) { proto.Version = old }(proto.Version)
			proto.Version = v
			a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
			feedSession(t, m)
			a.checkUpdate()
			if a.update.Visible() || asked.Load() {
				t.Errorf("%s: Update visible %v, releases asked %v", v, a.update.Visible(), asked.Load())
			}
		}()
	}
}

func TestLibraryShowsEmbeddedCovers(t *testing.T) {
	ff, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	var picture bytes.Buffer
	red := color.NRGBA{R: 0xff, A: 0xff}
	pic := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	draw.Draw(pic, pic.Bounds(), image.NewUniform(red), image.Point{}, draw.Src)
	if err := png.Encode(&picture, pic); err != nil {
		t.Fatal(err)
	}
	cover := filepath.Join(t.TempDir(), "cover.png")
	if err := os.WriteFile(cover, picture.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	track := filepath.Join(dir, "Artist - Song.flac")
	cmd := exec.Command(ff, "-v", "error", "-nostdin", "-f", "lavfi", "-t", "0.5", "-i", "anullsrc", "-i", cover,
		"-map", "0:a", "-map", "1:v", "-c:a", "flac", "-c:v", "copy", "-disposition:v", "attached_pic", track)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("make the FLAC: %v: %s", err, b)
	}
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	values := defaults()
	values["output.dir"] = dir
	m.SetConn(model.Connected, "")
	m.SetInfo(info())
	m.SetSettings(values)
	a.showTab(tabLibrary)
	a.render()
	lib := a.screens[tabLibrary].(*libraryScreen)
	if len(lib.shown) != 1 {
		t.Fatalf("%d files listed", len(lib.shown))
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if got := lib.cover(lib.shown[0]).row; got != nil {
			if got.Bounds().Dx() != 8 || color.NRGBAModel.Convert(got.At(4, 4)) != red {
				t.Fatalf("the row shows another picture: %v, %v at its middle", got.Bounds(), got.At(4, 4))
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the cover never loaded")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// The library builds rows only for the files in view, however many there
// are: a row, and its cover, cost nothing until it scrolls in.
func TestTheLibraryDrawsOnlyTheRowsInView(t *testing.T) {
	dir := t.TempDir()
	for i := range 400 {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("Artist - Song %03d.mp3", i)), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// No ffmpeg: the empty files have no cover to look for.
	t.Setenv("SPYTIFY_FFMPEG", filepath.Join(dir, "none"))
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	values := defaults()
	values["output.dir"] = dir
	m.SetConn(model.Connected, "")
	m.SetInfo(info())
	m.SetSettings(values)
	a.showTab(tabLibrary)
	a.render()
	lib := a.screens[tabLibrary].(*libraryScreen)
	if len(lib.shown) != 400 {
		t.Fatalf("%d files listed", len(lib.shown))
	}
	drawn := map[widget.ListItemID]bool{}
	update := lib.list.UpdateItem
	lib.list.UpdateItem = func(id widget.ListItemID, o fyne.CanvasObject) {
		drawn[id] = true
		update(id, o)
	}
	// About eight rows fit in the window.
	lib.list.Refresh()
	if len(drawn) == 0 || len(drawn) > 12 {
		t.Errorf("drew rows %v of 400", slices.Sorted(maps.Keys(drawn)))
	}
	clear(drawn)
	lib.list.ScrollTo(200)
	if len(drawn) == 0 || len(drawn) > 12 || !drawn[200] {
		t.Errorf("scrolled to the 200th file, drew rows %v", slices.Sorted(maps.Keys(drawn)))
	}
}

// Hover and selection are drawn over buttons: an opaque colour would hide
// a red Stop under the pointer.
func TestHoverAndSelectionAreTranslucent(t *testing.T) {
	th := spytifyTheme{mode: "system"}
	for _, v := range []fyne.ThemeVariant{theme.VariantLight, theme.VariantDark} {
		for _, name := range []fyne.ThemeColorName{theme.ColorNameHover, theme.ColorNameSelection} {
			if _, _, _, a := th.Color(name, v).RGBA(); a >= 0xffff {
				t.Errorf("%s is opaque in variant %d", name, v)
			}
		}
	}
}

func TestSourceParts(t *testing.T) {
	for _, c := range []struct {
		rate, ch int
		want     []string
	}{{44100, 2, []string{"44.1 kHz", "Stereo"}}, {48000, 2, []string{"48 kHz", "Stereo"}}, {48000, 1, []string{"48 kHz", "Mono"}},
		{96000, 2, []string{"96 kHz", "Stereo", "48 kHz file"}}, {88200, 2, []string{"88.2 kHz", "Stereo", "44.1 kHz file"}},
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
		{"saved", model.Row{Title: "Reunion", Artist: "M83", Status: model.Saved, Path: flac, DurationMs: 236000, Bytes: 31_000_000}, []string{"Reunion", "M83", "3:56 · FLAC · 29.6 MB"}},
		{"skipped", model.Row{Title: "Wait", Artist: "M83", Status: model.Skipped, LengthMs: 174000}, []string{"Wait", "M83", "2:54"}},
		{"no artist", model.Row{Title: "Reunion", Status: model.Saved, Path: flac, DurationMs: 236000, Bytes: 31_000_000}, []string{"Reunion", "3:56 · FLAC · 29.6 MB"}},
		{"title alone", model.Row{Title: "Intermission", Status: model.Skipped}, []string{"Intermission"}},
		{"no size", model.Row{Title: "Reunion", Artist: "M83", Status: model.Saved, Path: flac, DurationMs: 236000}, []string{"Reunion", "M83", "3:56 · FLAC"}},
	} {
		setTrackRow(row, nil, c.row.Title, c.row.Artist, rowDetails(c.row))
		if got := texts(); !slices.Equal(got, c.want) {
			t.Errorf("%s row %q, want %q", c.name, got, c.want)
		}
	}
}

func TestAFailedCheckReadsAsWhatIsWrong(t *testing.T) {
	for name, want := range map[string]string{
		"ffmpeg found": "ffmpeg not found", "player running": "player not running",
		"output folder writable": "output folder not writable", "capture helper": "capture helper",
	} {
		if got := checkLabel(proto.Check{Name: name}); got != want {
			t.Errorf("checkLabel(%q) = %q, want %q", name, got, want)
		}
		if got := checkLabel(proto.Check{Name: name, OK: true}); got != name {
			t.Errorf("a passing %q reads %q", name, got)
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
	m.Apply(event(t, proto.EventLevel, proto.LevelEvent{RMSDBFS: -3}))
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

// QA found closing the window on macOS left the app running with Minimize
// to tray off. Closing quits unless the setting is on and there is a tray,
// which a Linux desktop with no tray host does not draw.
func TestClosingTheWindowQuitsUnlessMinimizedToTray(t *testing.T) {
	for _, c := range []struct {
		name               string
		tray, noHost, mini bool
		quits              bool
	}{
		{"setting on, tray", true, false, true, false},
		{"setting off, tray", true, false, false, true},
		{"setting on, no tray", false, false, true, true},
		{"setting on, tray with no host", true, true, true, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
			values := defaults()
			values["app.minimize_to_tray"] = c.mini
			m.SetConn(model.Connected, "")
			m.SetSettings(values)
			a.render()
			quits := 0
			a.tray, a.noTrayHost, a.quitApp = c.tray, c.noHost, func() { quits++ }
			a.closeWindow()
			if (quits == 1) != c.quits {
				t.Fatalf("quit %d times, want quitting %v", quits, c.quits)
			}
		})
	}
}

// spyWindow is the window, telling whether it is shown and was brought to
// the front.
type spyWindow struct {
	fyne.Window
	shown, focused bool
}

func (w *spyWindow) Show()         { w.shown = true; w.Window.Show() }
func (w *spyWindow) Hide()         { w.shown, w.focused = false, false; w.Window.Hide() }
func (w *spyWindow) RequestFocus() { w.focused = true; w.Window.RequestFocus() }

// On Windows, opening Spytify again with its window hidden to the tray
// did not bring the window back. The second launch asks the running app
// to reopen (cmd/spytify-ui), which shows the window, in front.
func TestReopeningShowsTheWindowHiddenToTheTray(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	values := defaults()
	values["app.minimize_to_tray"] = true
	m.SetConn(model.Connected, "")
	m.SetSettings(values)
	a.render()
	win := &spyWindow{Window: a.win}
	a.win = win
	win.Show()
	a.tray, a.quitApp = true, func() { t.Error("closing quit with Minimize to tray on") }
	a.closeWindow()
	if win.shown {
		t.Fatal("the window is still shown after closing to the tray")
	}
	a.Reopen()
	if !win.shown || !win.focused {
		t.Fatalf("reopened: shown %v, in front %v; want both", win.shown, win.focused)
	}
}

// With a player set, the app starts the recorder itself: the tabs show,
// saying so, instead of the offline screen, which only a start that failed
// shows, with the reason.
func TestTheAppStartsTheRecorderItself(t *testing.T) {
	fa := test.NewTempApp(t)
	fa.Preferences().SetBool(firstRunKey, true)
	m := model.New()
	l := link.New(filepath.Join(t.TempDir(), "absent.sock"), m)
	a := New(fa, l, m, Options{StateDir: t.TempDir(), ManualRender: true, NoUpdateCheck: true, AutoStart: true})
	m.SetConn(model.Disconnected, "spytifyd is not running")
	a.render()
	snapshot(t, a, "starting")
	all := texts(a.win.Content())
	if !contains(all, "Starting the recorder…") || contains(all, "Start the recorder") || contains(all, "The recorder is not running") {
		t.Fatalf("starting: %v", all)
	}
	if a.center.Objects[0] != a.tabsView || a.tabs.SelectedIndex() != tabRecord {
		t.Fatal("the Record tab is not shown while the recorder starts")
	}
	for i := range a.tabs.Items {
		a.showTab(i)
		a.render()
	}
	a.showTab(tabRecord)
	a.startFailed(link.ErrInArchive)
	all = texts(a.win.Content())
	for _, want := range []string{"The recorder is not running", link.ErrInArchive.Error(), "Start the recorder"} {
		if !contains(all, want) {
			t.Errorf("%q not shown after a failed start: %v", want, all)
		}
	}
	m.SetConn(model.Connected, "")
	m.SetInfo(info())
	a.render()
	if contains(texts(a.win.Content()), "Starting the recorder…") {
		t.Error("still starting once connected")
	}
}

// Record needs the player: with it closed the button is off, and the app
// bar says why. A recording under way keeps its Stop.
func TestRecordWaitsForThePlayer(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	m.SetConn(model.Connected, "")
	m.SetInfo(info())
	m.SetSettings(defaults())
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
	if a.action.Disabled() || a.action.Text != "Stop" {
		t.Fatalf("player quit while recording: %q disabled %v", a.action.Text, a.action.Disabled())
	}
}

// The diagnostics are a dialog behind a button of the Logs screen, which
// turns to a warning while a check fails. Opened from another tab, the
// dialog follows the checks there, and closing it stays on that tab.
func TestTheDiagnosticsAreADialog(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	m.SetConn(model.Connected, "")
	m.SetInfo(info())
	m.SetSettings(defaults())
	a.showTab(tabLogs)
	a.render()
	b := a.screens[tabLogs].(*logsScreen).diagnose
	if b.Importance != widget.LowImportance {
		t.Errorf("every check passes: the button's importance is %v", b.Importance)
	}
	failCheck(a, m)
	if b.Importance != widget.WarningImportance {
		t.Errorf("a check fails: the button's importance is %v", b.Importance)
	}
	snapshot(t, a, "logs-check-fails")

	a.showTab(tabRecord)
	m.SetInfo(info())
	a.render()
	d := diagnostics(t, a)
	if all := texts(d); !contains(all, "All 4 checks pass.") {
		t.Fatalf("opened from Record: %v", all)
	}
	failCheck(a, m)
	if all := texts(d); !contains(all, "1 of 5 checks need attention.") || !contains(all, "ffmpeg not found") {
		t.Errorf("a check fails under the dialog: %v", all)
	}
	tapButton(t, d, "Close")
	if a.win.Canvas().Overlays().Top() != nil || a.tabs.SelectedIndex() != tabRecord {
		t.Errorf("closed: a dialog %v, tab %d", a.win.Canvas().Overlays().Top(), a.tabs.SelectedIndex())
	}
}

// A failed check's reason shows whole, on its own lines under the name:
// beside it, it was cut short with "…".
func TestAFailedChecksReasonShowsWholeUnderItsName(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	m.SetConn(model.Connected, "")
	reason := `mkdir \\nas\shared\Music\Recordings from the radio, every Saturday night since the spring of 2019\Spytify: The system cannot find the path specified.`
	in := info()
	in.Checks = append(in.Checks, proto.Check{Name: proto.CheckOutputWritable, Detail: reason})
	m.SetInfo(in)
	m.SetSettings(defaults())
	a.render()
	d := diagnostics(t, a)
	l := a.screens[tabLogs].(*logsScreen)
	if w := l.diag.Size().Width; w != diagWidth {
		t.Errorf("the dialog is %v wide, want %v", w, diagWidth)
	}

	name := labels(d, "output folder not writable")
	var detail *widget.RichText
	for _, rt := range richTextsOf(d) {
		if rt.String() == reason {
			detail = rt
		}
	}
	if len(name) != 1 || detail == nil {
		t.Fatalf("%d names, the reason found %v: %v", len(name), detail != nil, texts(d))
	}
	at := a.fa.Driver().AbsolutePositionForObject
	if n, r := at(name[0]), at(detail); r.Y < n.Y+name[0].Size().Height || r.X != n.X {
		t.Errorf("the reason at %v, the name at %v and %v tall: not under it", r, n, name[0].Size().Height)
	}
	var drawn []string
	for _, o := range test.WidgetRenderer(detail).Objects() {
		if tx, ok := o.(*canvas.Text); ok && tx.Text != "" {
			drawn = append(drawn, tx.Text)
		}
	}
	if shown := strings.Join(drawn, " "); len(drawn) < 2 || strings.Contains(shown, "…") || !slices.Equal(strings.Fields(shown), strings.Fields(reason)) {
		t.Errorf("the reason shows as %q", drawn)
	}
	// The dialog is as tall as the reason's lines: the checks do not scroll.
	if got, want := l.checksScroll.Size().Height, l.checks.MinSize().Height; got < want {
		t.Errorf("the checks show %v of their %v", got, want)
	}
	snapshot(t, a, "diagnostics-reason-under-the-name")

	// A narrower window narrows the dialog.
	a.win.Resize(fyne.NewSize(700, 640))
	l.fitDiagnostics()
	if w := l.diag.Size().Width; w >= 700 {
		t.Errorf("in a window 700 wide, the dialog is %v", w)
	}
}

// richTextsOf are the rich texts shown in o.
func richTextsOf(o fyne.CanvasObject) []*widget.RichText {
	var out []*widget.RichText
	var walk func(fyne.CanvasObject)
	walk = func(o fyne.CanvasObject) {
		if o == nil || !o.Visible() {
			return
		}
		switch w := o.(type) {
		case *widget.RichText:
			out = append(out, w)
			return
		case *fyne.Container:
			for _, child := range w.Objects {
				walk(child)
			}
			return
		}
		if w, ok := o.(fyne.Widget); ok {
			for _, child := range test.WidgetRenderer(w).Objects() {
				walk(child)
			}
		}
	}
	walk(o)
	return out
}

// A player that is off is in the app bar and offered to open; the
// diagnostics are not, even while its check still says so.
func TestNoDiagnosticsForAPlayerThatIsOff(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	m.SetConn(model.Connected, "")
	stale := info()
	stale.Checks[1].OK = false
	m.SetInfo(stale)
	m.SetSettings(defaults())
	a.render()
	all := texts(a.win.Content())
	if !contains(all, "Open Player") || contains(all, "Open diagnostics") {
		t.Errorf("player off: %v", all)
	}
	// The check can lag behind the player's state, which the app bar
	// shows: it sends nobody to the diagnostics either.
	m.Apply(event(t, proto.EventTargetState, proto.TargetStateEvent{Running: true, PID: 7}))
	m.SetInfo(stale)
	a.render()
	if all := texts(a.win.Content()); contains(all, "Open diagnostics") {
		t.Errorf("a stale player check sends to the diagnostics: %v", all)
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
	if !a.action.Disabled() || !contains(all, "Record is off") || !contains(all, "Open diagnostics") || contains(all, "Ready to record") {
		t.Fatalf("output folder gone: Record disabled %v, screen %v", a.action.Disabled(), all)
	}
	snapshot(t, a, "record-no-output")
	tapButton(t, a.win.Content(), "Open diagnostics")
	if a.tabs.SelectedIndex() != tabRecord {
		t.Fatalf("Open diagnostics shows tab %d", a.tabs.SelectedIndex())
	}
	snapshot(t, a, "diagnostics-no-output")
	tapButton(t, overlay(t, a), "Choose another folder")
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
	if !a.action.Disabled() || !contains(all, "Session ended: Spytify cannot write to the output folder, so Record is off.") || !contains(all, "Open diagnostics") {
		t.Fatalf("session ended without its folder: Record disabled %v, screen %v", a.action.Disabled(), all)
	}
	snapshot(t, a, "record-session-no-output")
	m.SetInfo(info())
	a.render()
	if a.action.Disabled() || contains(texts(a.win.Content()), "Open diagnostics") {
		t.Error("Record stays off once the folder is back")
	}
}

// eventually waits for cond, set by the link's goroutine.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); !cond(); time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal(what)
		}
	}
}

// A start the recorder refuses for want of an output folder runs the
// checks again and shows them; the folder lost during a session or
// another one chosen runs them again too.
func TestTheOutputFolderIsCheckedAgain(t *testing.T) {
	a, m, core := connectedApp(t)
	failing := func() bool { return checkFails(m.Snapshot().Info.Checks, proto.CheckOutputWritable) }
	// The drive unplugged since the checks ran.
	core.setNoOutput(true)
	test.Tap(a.action)
	// The app shows the diagnostics, then runs the checks: once the model
	// has them, the dialog is open and shows the one that fails.
	eventually(t, "the checks did not run again after the refused start", failing)
	a.render()
	if len(buttons(overlay(t, a), "Choose another folder")) != 1 || !a.action.Disabled() {
		t.Fatalf("after the refused start: diagnostics %v, Record disabled %v", texts(overlay(t, a)), a.action.Disabled())
	}
	tapButton(t, overlay(t, a), "Close")
	// Another folder chosen.
	core.setNoOutput(false)
	m.Apply(event(t, proto.EventSettingsChanged, proto.SettingsValues{Values: map[string]any{"output.dir": "/Volumes/Other"}}))
	eventually(t, "the checks did not run again for another folder", func() bool { return !failing() })
	// The folder lost during a recording.
	core.setNoOutput(true)
	m.Apply(event(t, proto.EventError, proto.ErrorEvent{Code: "output_dir", Msg: "the output folder cannot be reached, so the recording stopped"}))
	eventually(t, "the checks did not run again for a folder lost while recording", failing)
}

// The warning of an output device too poor to record from shows as soon
// as the capture says so, while the session waits for the next track, and
// goes once the device is fit again.
func TestTheLowQualityWarningShowsWhileWaiting(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	m.SetConn(model.Connected, "")
	m.SetInfo(info())
	m.SetSettings(defaults())
	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateWaiting}))
	msg := "The player plays to a device at 16 kHz mono, not fit for recording: tracks will sound like a phone call."
	m.Apply(event(t, proto.EventError, proto.ErrorEvent{Code: "capture_low_quality", Msg: msg}))
	a.render()
	if !a.banner.Visible() || a.banner.Text != msg {
		t.Fatalf("banner shown %v, text %q", a.banner.Visible(), a.banner.Text)
	}
	m.Apply(event(t, proto.EventError, proto.ErrorEvent{Code: "capture_low_quality"}))
	a.render()
	if a.banner.Visible() {
		t.Fatalf("banner still shows %q with the device fit again", a.banner.Text)
	}
}
