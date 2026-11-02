package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"image/png"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	ttwidget "github.com/dweymouth/fyne-tooltip/widget"

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
	m.Apply(event(t, proto.EventLevel, proto.LevelEvent{PeakDBFS: -3, RMSDBFS: -14.4,
		Channels: []proto.ChannelLevel{{PeakDBFS: -3, RMSDBFS: -13}, {PeakDBFS: -4.5, RMSDBFS: -16.5}}}))
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
	// silent is a capture that heard only silence: the player heard check
	// fails.
	silent bool
}

func (c *recordingCore) setNoOutput(gone bool) {
	c.mu.Lock()
	c.noOutput = gone
	c.mu.Unlock()
}

func (c *recordingCore) setSilent(silent bool) {
	c.mu.Lock()
	c.silent = silent
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
	noOutput, silent := c.noOutput, c.silent
	c.mu.Unlock()
	switch method {
	case proto.MethodCoreInfo:
		if noOutput {
			return outputGone(), nil
		}
		if silent {
			i := info()
			i.Checks = append(i.Checks, proto.Check{Name: proto.CheckPlayerHeard, Detail: "only silence from the capture while the player played"})
			return i, nil
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
	case *rowButton:
		return &b.Button, true
	case *ttwidget.Button:
		return &b.Button, true
	case *tipButton:
		return &b.Button.Button, true
	case *litButton:
		return &b.Button.Button, true
	case *rowLitButton:
		return &b.Button.Button, true
	}
	return nil, false
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

// eventually waits for cond, set by the link's goroutine.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); !cond(); time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal(what)
		}
	}
}
