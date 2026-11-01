package ui

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/spytify/spytify/internal/telemetry/fakesentry"
	"github.com/spytify/spytify/pkg/proto"
)

// covered says the picture is the mask's colour all over the part of obj
// on screen.
func covered(a *App, img image.Image, obj fyne.CanvasObject) bool {
	c := a.win.Canvas()
	at := a.fa.Driver().AbsolutePositionForObject(obj)
	x0, y0 := c.PixelCoordinateForPosition(at)
	x1, y1 := c.PixelCoordinateForPosition(at.Add(obj.Size()))
	shown := image.Rect(x0, y0, x1, y1).Intersect(img.Bounds())
	if at.IsZero() || shown.Empty() {
		return false
	}
	for y := shown.Min.Y; y < shown.Max.Y; y++ {
		for x := shown.Min.X; x < shown.Max.X; x++ {
			if color.NRGBAModel.Convert(img.At(x, y)) != maskColor {
				return false
			}
		}
	}
	return true
}

// visible are the visible objects of type T under o.
func visible[T fyne.CanvasObject](o fyne.CanvasObject) []T {
	var out []T
	var walk func(fyne.CanvasObject)
	walk = func(o fyne.CanvasObject) {
		if o == nil || !o.Visible() {
			return
		}
		if w, ok := o.(T); ok {
			out = append(out, w)
		}
		switch w := o.(type) {
		case *fyne.Container:
			for _, child := range w.Objects {
				walk(child)
			}
		case fyne.Widget:
			for _, child := range test.WidgetRenderer(w).Objects() {
				walk(child)
			}
		}
	}
	walk(o)
	return out
}

// richTexts are the visible rich texts that read text.
func richTexts(o fyne.CanvasObject, text string) []*widget.RichText {
	var out []*widget.RichText
	for _, rt := range visible[*widget.RichText](o) {
		if rt.String() == text {
			out = append(out, rt)
		}
	}
	return out
}

// outputField is the output folder's text field in Settings.
func outputField(t *testing.T, a *App) fyne.CanvasObject {
	t.Helper()
	for _, o := range a.screens[tabSettings].(*settingsScreen).fields["output.dir"].input.(*fyne.Container).Objects {
		if _, ok := o.(*widget.Button); !ok {
			return o
		}
	}
	t.Fatal("no output folder field")
	return nil
}

// sentScreenshot is the picture that reached Sentry as the screenshot, or
// nil.
func sentScreenshot(t *testing.T, srv *fakesentry.Server) image.Image {
	t.Helper()
	for _, it := range srv.Items("attachment") {
		if it.Header["filename"] == "screenshot.png" {
			img, err := png.Decode(bytes.NewReader(it.Payload))
			if err != nil {
				t.Fatal(err)
			}
			return img
		}
	}
	return nil
}

// The window's picture for a report covers the output folder's field,
// whatever it shows, and the log lines and check details that name the
// user's folders; the rest shows.
func TestThePictureCoversThePaths(t *testing.T) {
	srv := fakesentry.New(t)
	a, m := privacyApp(t, srv, true)
	connect(t, a, m, true)
	out := defaults()["output.dir"].(string)
	saved := "saved " + filepath.Join(out, "M83", "Intro.flac")
	started := "session 20261023-120000 started"
	for _, msg := range []string{saved, started} {
		m.Apply(event(t, proto.EventLog, proto.LogEvent{Level: "info", Msg: msg, TS: time.Now().Format(time.RFC3339Nano)}))
	}

	// Tall enough for the output folder's field.
	a.win.Resize(fyne.NewSize(920, 900))
	a.showTab(tabSettings)
	a.render()
	img := a.picture()
	if img == nil {
		t.Fatal("no picture of the window")
	}
	if !covered(a, img, outputField(t, a)) {
		t.Error("the output folder's field shows")
	}

	a.showTab(tabLogs)
	a.render()
	img = a.picture()
	content := a.win.Canvas().Content()
	if l := labels(content, saved); len(l) != 1 || !covered(a, img, l[0]) {
		t.Errorf("the line naming the output folder shows (%d lines)", len(l))
	}
	if l := labels(content, started); len(l) != 1 || covered(a, img, l[0]) {
		t.Errorf("the line naming no folder is covered (%d lines)", len(l))
	}

	in := info()
	detail := "mkdir " + out + ": permission denied"
	in.Checks = append(in.Checks, proto.Check{Name: proto.CheckOutputWritable, Detail: detail})
	m.SetInfo(in)
	a.render()
	d := diagnostics(t, a)
	img = a.picture()
	if rt := richTexts(d, detail); len(rt) != 1 || !covered(a, img, rt[0]) {
		t.Errorf("the check naming the output folder shows (%d)", len(rt))
	}
	if rt := richTexts(d, "ffmpeg version 9.0.2"); len(rt) != 1 || covered(a, img, rt[0]) {
		t.Errorf("the check naming no folder is covered (%d)", len(rt))
	}
}

// No picture is taken while the window is hidden in the tray, or under an
// error or the folder picker: those dialogs are Fyne's, which the picture
// cannot cover.
func TestNoPictureItCannotCover(t *testing.T) {
	a, m, s := settingsApp(t)
	v := defaults()
	v["output.dir"] = t.TempDir()
	m.SetSettings(v)
	a.render()
	if a.picture() == nil {
		t.Fatal("no picture of the window")
	}
	a.hideToTray()
	if a.picture() != nil {
		t.Error("a picture of the window hidden in the tray")
	}
	a.showWindow()

	home, _ := os.UserHomeDir()
	a.showError(errors.New("open " + filepath.Join(home, "x.flac") + ": permission denied"))
	if a.picture() != nil {
		t.Error("a picture under an error")
	}
	tapButton(t, overlay(t, a), "OK")
	if a.picture() == nil {
		t.Error("no picture once the error closed")
	}

	for _, o := range s.fields["output.dir"].input.(*fyne.Container).Objects {
		if b, ok := o.(*widget.Button); ok {
			test.Tap(b)
		}
	}
	if a.picture() != nil {
		t.Error("a picture under the folder picker")
	}
	tapButton(t, overlay(t, a), "Cancel")
	if a.picture() == nil {
		t.Error("no picture once the folder picker closed")
	}
}

// A problem's report goes with the window's picture.
func TestAProblemGoesWithThePicture(t *testing.T) {
	srv := fakesentry.New(t)
	a, m := privacyApp(t, srv, true)
	connect(t, a, m, true)
	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateRecording}))
	m.Apply(event(t, proto.EventError, proto.ErrorEvent{Code: "encode", Msg: "ffmpeg exited with 1"}))
	srv.Wait(t, "event", 1)
	img := sentScreenshot(t, srv)
	w, h := a.win.Canvas().PixelCoordinateForPosition(fyne.NewPos(a.win.Canvas().Size().Width, a.win.Canvas().Size().Height))
	if img == nil || img.Bounds().Dx() != w || img.Bounds().Dy() != h {
		t.Errorf("screenshot %v, want the window's %d×%d", img, w, h)
	}
}

// A recorder that did not start is reported from the toolkit's goroutine,
// whose queue the picture waits on: the report leaves from another, with
// the picture.
func TestARecorderThatDidNotStartGoesWithThePicture(t *testing.T) {
	srv := fakesentry.New(t)
	a, m := privacyApp(t, srv, true)
	connect(t, a, m, true)
	a.launchFailed(errors.New("spytifyd started but does not answer; see its log in Logs"))
	a.reports.Wait()
	ev := srv.Wait(t, "event", 1)[0].JSON()
	if ev["message"] != "blocked: recorder_start" {
		t.Errorf("sent %v", ev["message"])
	}
	if sentScreenshot(t, srv) == nil {
		t.Error("no screenshot")
	}
}

// Send to the developers shows the window's picture it sends; with none,
// such as with the window hidden in the tray, it shows and sends none.
func TestSendToTheDevelopersShowsThePicture(t *testing.T) {
	for name, hidden := range map[string]bool{"shown": false, "hidden": true} {
		t.Run(name, func(t *testing.T) {
			sendWithPicture(t, hidden)
		})
	}
}

func sendWithPicture(t *testing.T, hidden bool) {
	{
		srv := fakesentry.New(t)
		a, m := privacyApp(t, srv, true)
		connect(t, a, m, true)
		a.showTab(tabLogs)
		failCheck(a, m)
		diagnostics(t, a)
		if hidden {
			a.hideToTray()
		}
		tapButton(t, overlay(t, a), "Send to the developers")
		d := overlay(t, a)
		for _, acc := range visible[*widget.Accordion](d) {
			acc.OpenAll()
		}
		var pictures int
		for _, name := range images(d) {
			if strings.HasPrefix(name, "picture ") {
				pictures++
			}
		}
		asked := strings.Join(texts(d), " ")
		if hidden {
			if pictures != 0 || strings.Contains(asked, "picture") {
				t.Errorf("hidden: %d pictures shown, asked %q", pictures, asked)
			}
		} else {
			if pictures != 1 || !strings.Contains(asked, "a picture of the window") {
				t.Errorf("%d pictures shown, asked %q", pictures, asked)
			}
		}
		tapButton(t, d, "Send")
		srv.Wait(t, "event", 1)
		if sent := sentScreenshot(t, srv); (sent != nil) == hidden {
			t.Errorf("hidden %v: screenshot sent %v", hidden, sent != nil)
		}
	}
}
