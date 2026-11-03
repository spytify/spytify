package ui

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/software"

	"github.com/spytify/spytify/pkg/proto"
)

// TestTheReadmeCapture draws assets/recording-light.png and
// assets/recording-dark.png, the pictures of the README and of the site's
// Overview: a FLAC session 1:33 into its fifth track, three files saved, one
// skipped as already recorded, and no ads, in a macOS window.
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

	// Twice the window's size, as a Retina screen draws it.
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
		f, err := os.Create(filepath.Join("..", "..", "assets", "recording-"+look+".png"))
		if err != nil {
			t.Fatal(err)
		}
		if err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(f, inWindow(t, a.win.Canvas().Capture(), look)); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
	if s := m.Snapshot(); s.Current == nil || m.Elapsed(*s.Current) != 93*time.Second {
		t.Error("the current track is not 1:33 in")
	}
}

// inWindow puts a capture in the macOS window of testdata/window: two
// pictures of an empty Spytify window of the same size, taken on a Retina
// screen with the look's appearance, one black inside and one white. Their
// difference is how much of each pixel the content covers, so the rounded
// corners, the window's edge and its shadow stay as macOS drew them.
func inWindow(t *testing.T, content image.Image, look string) image.Image {
	t.Helper()
	black, white := readPNG(t, look+"-black.png"), readPNG(t, look+"-white.png")
	cover := func(x, y int) uint32 {
		_, k, _, _ := black.At(x, y).RGBA()
		_, w, _, _ := white.At(x, y).RGBA()
		return max(w, k) - k
	}
	var box image.Rectangle
	b := black.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if cover(x, y) > 0x8000 {
				box = box.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	if box.Size() != content.Bounds().Size() {
		t.Fatalf("the window holds %v, the capture is %v", box.Size(), content.Bounds().Size())
	}
	out := image.NewRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			p := color.RGBA64Model.Convert(black.At(x, y)).(color.RGBA64)
			if c := cover(x, y); c > 0 && image.Pt(x, y).In(box) {
				r, g, bl, _ := content.At(x-box.Min.X, y-box.Min.Y).RGBA()
				add := func(v uint16, by uint32) uint16 { return uint16(min(uint32(v)+c*by/0xffff, uint32(p.A))) }
				p.R, p.G, p.B = add(p.R, r), add(p.G, g), add(p.B, bl)
			}
			out.SetRGBA64(x, y, p)
		}
	}
	return out
}

func readPNG(t *testing.T, name string) image.Image {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "window", name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	return img
}
