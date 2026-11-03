package ui

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/software"

	"github.com/spytify/spytify/pkg/proto"
)

// TestTheReadmeCapture draws LIGHT.png and DARK.png at the repository's root,
// the pictures of the README and of the site's Overview: a FLAC session 1:33
// into its fifth track, three files saved, one skipped as already recorded,
// and no ads.
func TestTheReadmeCapture(t *testing.T) {
	if os.Getenv("SPYTIFY_README_CAPTURE") == "" {
		t.Skip("only for the README's pictures: SPYTIFY_README_CAPTURE=1 go test -run TestTheReadmeCapture ./internal/ui/")
	}
	a, m := connectedTestApp(t, defaults())
	start := time.Date(2026, 11, 3, 20, 0, 0, 0, time.UTC)
	m.SetClock(func() time.Time { return start })
	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateWaiting}))
	m.Apply(event(t, proto.EventTargetState, proto.TargetStateEvent{Running: true, PID: 4242}))
	for i, tr := range []struct {
		title     string
		ms, bytes int64
		skipped   bool
	}{
		{"Intro", 134000, 18_200_000, false},
		{"Reunion", 236000, 31_000_000, false},
		{"Wait", 174000, 0, true},
		{"Midnight City", 243000, 32_600_000, false},
	} {
		id := "s-00" + string(rune('1'+i))
		path := "/Users/listener/Music/Spytify/M83/Hurry Up, We're Dreaming/" + tr.title + ".flac"
		m.Apply(event(t, proto.EventTrackStarted, proto.TrackStartedEvent{Track: proto.Track{ID: id, Artist: "M83", Title: tr.title, Album: "Hurry Up, We're Dreaming", LengthMs: tr.ms, CoverURL: generatedCover(t, i+1)}}))
		if tr.skipped {
			m.Apply(event(t, proto.EventTrackSkipped, proto.TrackSkippedEvent{ID: id, Reason: proto.SkipDuplicate, Path: path}))
			continue
		}
		m.Apply(event(t, proto.EventTrackFinished, proto.TrackFinishedEvent{ID: id, Path: path, DurationMs: tr.ms, Bytes: tr.bytes, Tagged: true, TaggedBy: "lastfm"}))
	}
	m.Apply(event(t, proto.EventTrackStarted, proto.TrackStartedEvent{Track: proto.Track{ID: "s-005", Artist: "M83", Title: "Raconte-moi une histoire", Album: "Hurry Up, We're Dreaming", LengthMs: 190000, CoverURL: generatedCover(t, 5)}}))
	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateRecording}))
	m.Apply(event(t, proto.EventLevel, proto.LevelEvent{PeakDBFS: -2.5, RMSDBFS: -13,
		Channels: []proto.ChannelLevel{{PeakDBFS: -2.5, RMSDBFS: -11}, {PeakDBFS: -6, RMSDBFS: -17.5}}}))
	m.SetClock(func() time.Time { return start.Add(93 * time.Second) })

	// Twice the window's size, for screens that draw two pixels per point.
	a.win.Canvas().(software.WindowlessCanvas).SetScale(2)
	for _, look := range []string{"light", "dark"} {
		values := defaults()
		values["app.theme"] = look
		m.SetSettings(values)
		a.render()
		coversIn(t, a)
		// The test window lays out only on a resize.
		size := a.win.Canvas().Size()
		a.win.Resize(size.Add(fyne.NewSize(1, 1)))
		a.win.Resize(size)
		writeCapture(t, a, filepath.Join("..", "..", map[string]string{"light": "LIGHT.png", "dark": "DARK.png"}[look]))
	}
	if s := m.Snapshot(); s.Current == nil || m.Elapsed(*s.Current) != 93*time.Second {
		t.Error("the current track is not 1:33 in")
	}
}

func writeCapture(t *testing.T, a *App, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, a.win.Canvas().Capture()); err != nil {
		t.Fatal(err)
	}
}
