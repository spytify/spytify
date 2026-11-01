package telemetry

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/spytify/spytify/internal/telemetry/fakesentry"
	"github.com/spytify/spytify/pkg/proto"
)

// screenshotOf is the picture a report sent as Sentry's screenshot, or
// nil.
func screenshotOf(t *testing.T, srv *fakesentry.Server) image.Image {
	t.Helper()
	var img image.Image
	for _, it := range srv.Items("attachment") {
		if it.Header["filename"] != "screenshot.png" {
			continue
		}
		if it.Header["content_type"] != "image/png" {
			t.Errorf("screenshot of type %v", it.Header["content_type"])
		}
		if img != nil {
			t.Error("two screenshots")
		}
		var err error
		if img, err = png.Decode(bytes.NewReader(it.Payload)); err != nil {
			t.Fatal(err)
		}
	}
	return img
}

// A problem goes with the window's picture; no picture, or one that
// fails, leaves the report without one, never leaves no report.
func TestProblemScreenshot(t *testing.T) {
	for name, take := range map[string]func() image.Image{
		"picture": func() image.Image { return image.NewNRGBA(image.Rect(0, 0, 4, 3)) },
		"none":    func() image.Image { return nil },
		"panics":  func() image.Image { panic("no window") },
		"broken":  func() image.Image { return (*image.NRGBA)(nil) },
	} {
		t.Run(name, func(t *testing.T) {
			srv := fakesentry.New(t)
			c := newClient(t, srv, Options{ProblemSampleRate: 1})
			c.SetEnabled(true)
			c.SetScreenshot(take)
			c.Problem("encode", "ffmpeg exited with 1")
			ev := srv.Wait(t, "event", 1)[0].JSON()
			if ev["message"] != "blocked: encode" {
				t.Errorf("sent %v", ev["message"])
			}
			img := screenshotOf(t, srv)
			if name != "picture" {
				if img != nil {
					t.Error("a screenshot with no picture")
				}
				return
			}
			if img == nil || img.Bounds().Dx() != 4 || img.Bounds().Dy() != 3 {
				t.Errorf("screenshot %v, want the 4×3 picture", img)
			}
		})
	}
}

// The checks go with the window's picture when the dialog had one, and
// without when it had none.
func TestSendChecksScreenshot(t *testing.T) {
	srv := fakesentry.New(t)
	c := newClient(t, srv, Options{})
	c.SetEnabled(true)
	c.SetScreenshot(func() image.Image { panic("only problems take their own picture") })
	r := Checks{Failing: []proto.Check{{Name: "ffmpeg found", Detail: "not on PATH"}}}
	if err := c.SendChecks(r, ""); err != nil {
		t.Fatal(err)
	}
	srv.Wait(t, "event", 1)
	if screenshotOf(t, srv) != nil {
		t.Error("a screenshot the dialog did not show")
	}
	r.Screenshot = image.NewNRGBA(image.Rect(0, 0, 4, 3))
	if err := c.SendChecks(r, ""); err != nil {
		t.Fatal(err)
	}
	srv.Wait(t, "event", 2)
	if screenshotOf(t, srv) == nil {
		t.Error("the dialog's picture did not leave")
	}
}

// Private says what the scrubbing would change: the window's picture
// covers the same texts.
func TestPrivate(t *testing.T) {
	c := New(Options{})
	home, _ := os.UserHomeDir()
	c.SetOutputDir(filepath.Join(string(filepath.Separator), "Volumes", "Mixtapes"))
	for text, want := range map[string]bool{
		filepath.Join(home, "Music", "x.flac"):                                true,
		filepath.Join(string(filepath.Separator), "Volumes", "Mixtapes", "a"): true,
		"M83 - Midnight City.flac":                                            false,
		"":                                                                    false,
	} {
		if got := c.Private(text); got != want {
			t.Errorf("Private(%q) = %v, want %v", text, got, want)
		}
	}
	var none *Client
	if none.Private(home) {
		t.Error("a nil client finds a text private")
	}
}
