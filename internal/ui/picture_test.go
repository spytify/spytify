package ui

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/spytify/spytify/internal/settings"
	"github.com/spytify/spytify/internal/telemetry/fakesentry"
	"github.com/spytify/spytify/internal/ui/lang"
	"github.com/spytify/spytify/pkg/proto"
)

// covered says the picture is blurred over the part of obj on screen: each
// row of it one colour, which no text shown leaves.
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
		row := color.NRGBAModel.Convert(img.At(shown.Min.X, y))
		for x := shown.Min.X; x < shown.Max.X; x++ {
			if color.NRGBAModel.Convert(img.At(x, y)) != row {
				return false
			}
		}
	}
	return true
}

// Two texts with the same ink on each row, in another order, blur the same:
// what is left cannot be matched against a guess at the text.
func TestTheBlurKeepsNothingOfTheText(t *testing.T) {
	box := image.Rect(10, 4, 50, 16)
	write := func(xs ...int) *image.NRGBA {
		img := image.NewNRGBA(image.Rect(0, 0, 60, 20))
		draw.Draw(img, img.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
		for _, x := range append(xs, 5) {
			draw.Draw(img, image.Rect(x, 8, x+1, 12), image.NewUniform(color.Black), image.Point{}, draw.Src)
		}
		return img
	}
	one, other := write(12, 13, 20), write(41, 30, 40)
	blur(one, box)
	blur(other, box)
	if !bytes.Equal(one.Pix, other.Pix) {
		t.Error("two texts blur apart")
	}
	for y := box.Min.Y; y < box.Max.Y; y++ {
		for x := box.Min.X; x < box.Max.X; x++ {
			if one.NRGBAAt(x, y) != one.NRGBAAt(box.Min.X, y) {
				t.Fatalf("row %d keeps a glyph at %d", y, x)
			}
		}
	}
	if one.NRGBAAt(box.Min.X, 10) == (color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}) {
		t.Error("the text's band is gone, not blurred")
	}
	if one.NRGBAAt(5, 10) != (color.NRGBA{A: 0xff}) {
		t.Error("the blur goes past its box")
	}
}

// The blur takes the folder in a line and leaves the rest: each pixel the
// folder draws is under it, as Fyne lays out a log line's label and a
// check's detail.
func TestTheBlurCoversTheFolderOnly(t *testing.T) {
	test.NewTempApp(t)
	fyne.CurrentApp().Settings().SetTheme(spytifyTheme{})
	sep := string(filepath.Separator)
	short := filepath.Join(sep, "Users", "listener", "Mixtapes")
	long := filepath.Join(sep, "Users", "listener", strings.Repeat("Mixtapes from the attic ", 6))
	label, cut := widget.NewLabel(""), widget.NewLabel("")
	cut.Truncation = fyne.TextTruncateEllipsis
	detail := widget.NewRichText(&widget.TextSegment{Style: widget.RichTextStyle{SizeName: theme.SizeNameCaptionText, Inline: true}})
	w := test.NewWindow(container.NewVBox(label, detail, cut))
	defer w.Close()
	w.Resize(fyne.NewSize(700, 200))
	for _, c := range []struct {
		name   string
		obj    fyne.CanvasObject
		set    func(string)
		folder string
	}{
		{"label", label, label.SetText, short},
		{"rich text", detail, func(s string) {
			detail.Segments[0].(*widget.TextSegment).Text = s
			detail.Refresh()
		}, short},
		// Past the cut, the blur goes to the label's end.
		{"cut short", cut, cut.SetText, long},
	} {
		t.Run(c.name, func(t *testing.T) {
			shot := func(text string) image.Image {
				c.set(text)
				return w.Canvas().Capture()
			}
			saved, withFolder := shot("saved "), shot("saved "+c.folder)
			full := shot("saved " + c.folder + filepath.Join(sep, "M83", "Intro.flac"))
			private := func(s string) [][2]int {
				if i := strings.Index(s, c.folder); i >= 0 {
					return [][2]int{{i, i + len(c.folder)}}
				}
				return nil
			}
			at := fyne.CurrentApp().Driver().AbsolutePositionForObject(c.obj)
			boxes := privateBoxes(w.Canvas(), c.obj, at, private)
			if len(boxes) != 1 {
				t.Fatalf("%d boxes, want the folder's", len(boxes))
			}
			box, bg := boxes[0], color.NRGBAModel.Convert(saved.At(0, 0))
			// Each drawing of a line shades its letters a little apart:
			// what tells is where a letter is and was not.
			for _, p := range ink(withFolder, bg, saved) {
				if !p.In(box) {
					t.Fatalf("the folder draws at %v, out of the blur %v", p, box)
				}
			}
			for _, p := range ink(saved, bg, nil) {
				if p.In(box) {
					t.Fatalf("saved is under the blur %v at %v", box, p)
				}
			}
			rest := slices.ContainsFunc(ink(full, bg, withFolder), func(p image.Point) bool { return !p.In(box) })
			if rest != (c.folder == short) {
				t.Errorf("the rest of the line shows %v under the blur %v", rest, box)
			}
		})
	}
}

// ink is where img draws on bg and but does not draw at all: a letter's
// pixels, told from the background by more than its edges' shading.
func ink(img image.Image, bg color.Color, but image.Image) []image.Point {
	from := func(img image.Image, x, y int) int {
		a, b := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA), bg.(color.NRGBA)
		d := func(u, v uint8) int { return max(int(u)-int(v), int(v)-int(u)) }
		return d(a.R, b.R) + d(a.G, b.G) + d(a.B, b.B)
	}
	var at []image.Point
	r := img.Bounds()
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if from(img, x, y) > 96 && (but == nil || from(but, x, y) < 24) {
				at = append(at, image.Pt(x, y))
			}
		}
	}
	return at
}

// blurs says the picture changes some of obj from the window as it shows.
func blurs(a *App, img image.Image, obj fyne.CanvasObject) bool {
	c := a.win.Canvas()
	box := pixelBox(c, a.fa.Driver().AbsolutePositionForObject(obj), obj.Size()).Intersect(img.Bounds())
	shot := c.Capture()
	for y := box.Min.Y; y < box.Max.Y; y++ {
		for x := box.Min.X; x < box.Max.X; x++ {
			if color.NRGBAModel.Convert(img.At(x, y)) != color.NRGBAModel.Convert(shot.At(x, y)) {
				return true
			}
		}
	}
	return false
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
		if _, ok := asButton(o); !ok {
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
	// The folder's part of a line is blurred, the rest shows.
	if l := labels(content, saved); len(l) != 1 || !blurs(a, img, l[0]) || covered(a, img, l[0]) {
		t.Errorf("the line naming the output folder is not blurred in part (%d lines)", len(l))
	}
	if l := labels(content, started); len(l) != 1 || blurs(a, img, l[0]) {
		t.Errorf("the line naming no folder is blurred (%d lines)", len(l))
	}

	in := info()
	detail := "mkdir " + out + ": permission denied"
	in.Checks = append(in.Checks, proto.Check{Name: proto.CheckOutputWritable, Detail: detail})
	m.SetInfo(in)
	a.render()
	d := diagnostics(t, a)
	img = a.picture()
	if rt := richTexts(d, detail); len(rt) != 1 || !blurs(a, img, rt[0]) || covered(a, img, rt[0]) {
		t.Errorf("the check naming the output folder is not blurred in part (%d)", len(rt))
	}
	if rt := richTexts(d, "ffmpeg version 9.0.2"); len(rt) != 1 || blurs(a, img, rt[0]) {
		t.Errorf("the check naming no folder is blurred (%d)", len(rt))
	}
}

// The Record screen's text before a session and the Library's when it is
// empty name the output folder.
func TestThePictureCoversTheFolderTheScreensName(t *testing.T) {
	srv := fakesentry.New(t)
	a, m := privacyApp(t, srv, true)
	connect(t, a, m, true)
	a.win.Resize(fyne.NewSize(920, 900))
	out := defaults()["output.dir"].(string)

	rec := a.screens[tabRecord].(*recordScreen)
	a.showTab(tabRecord)
	a.render()
	if !strings.Contains(rec.idleText.Text, out) {
		t.Fatalf("the Record screen says %q", rec.idleText.Text)
	}
	if img := a.picture(); img == nil || !blurs(a, img, rec.idleText) {
		t.Error("the Record screen's text naming the output folder shows")
	}

	lib := a.screens[tabLibrary].(*libraryScreen)
	a.showTab(tabLibrary)
	a.render()
	lib.scan()
	if !lib.empty.Visible() || !strings.Contains(lib.hint.Text, out) {
		t.Fatalf("the empty Library says %q", lib.hint.Text)
	}
	if img := a.picture(); img == nil || !blurs(a, img, lib.hint) {
		t.Error("the empty Library's text naming the output folder shows")
	}
}

// The default output folders name no one: a path in one is blurred up to
// the home folder, and the rest shows. A folder the user chose is blurred
// whole, in the home folder or out of it.
func TestTheDefaultFolderShowsPastTheHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home folder:", err)
	}
	srv := fakesentry.New(t)
	a, m := privacyApp(t, srv, true)
	connect(t, a, m, true)
	library := filepath.Join(string(filepath.Separator), "Library", "Spytify")
	for dir, hidden := range map[string]string{
		"~/Spytify":       home,
		"~/Music/Spytify": home,
		"~/Mixtapes":      filepath.Join(home, "Mixtapes"),
		library:           library,
	} {
		values := defaults()
		values["output.dir"] = dir
		m.SetSettings(values)
		a.render()
		path := filepath.Join(settings.ExpandHome(dir), "M83", "Intro.flac")
		if got, want := a.tel.Private(path), [][2]int{{0, len(hidden)}}; !slices.Equal(got, want) {
			t.Errorf("output folder %s: %q hides %v, want %v", dir, path, got, want)
		}
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
	tapButton(t, overlay(t, a), lang.OK)
	if a.picture() == nil {
		t.Error("no picture once the error closed")
	}

	for _, o := range s.fields["output.dir"].input.(*fyne.Container).Objects {
		if b, ok := asButton(o); ok {
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

// Send to the developers shows the window's picture it sends, which a
// tick box leaves out; with none, such as with the window hidden in the
// tray, it shows and sends none, and has no tick box.
func TestSendToTheDevelopersShowsThePicture(t *testing.T) {
	for _, c := range []struct {
		name           string
		hidden, untick bool
	}{{"sent", false, false}, {"unticked", false, true}, {"hidden", true, false}} {
		t.Run(c.name, func(t *testing.T) {
			srv := fakesentry.New(t)
			a, m := privacyApp(t, srv, true)
			connect(t, a, m, true)
			a.showTab(tabLogs)
			failCheck(a, m)
			diagnostics(t, a)
			if c.hidden {
				a.hideToTray()
			}
			tapButton(t, overlay(t, a), lang.SendToDevelopers)
			d := overlay(t, a)
			for _, f := range visible[*fold](d) {
				f.setOpen(true)
			}
			keep := checkBoxes(d, lang.SendPicture)
			if len(keep) != 1 && !c.hidden || len(keep) != 0 && c.hidden {
				t.Fatalf("%d tick boxes for the picture", len(keep))
			}
			if c.untick {
				test.Tap(keep[0])
			}
			want := !c.hidden && !c.untick
			var pictures int
			for _, name := range images(d) {
				if strings.HasPrefix(name, "picture ") {
					pictures++
				}
			}
			asked := strings.Join(texts(d), " ")
			asks := strings.Contains(asked, lang.SendChecksPicture)
			if (pictures == 1) != want || pictures > 1 || asks != want {
				t.Errorf("%d pictures shown, asked %q; want the picture %v", pictures, asked, want)
			}
			tapButton(t, d, lang.Send)
			srv.Wait(t, "event", 1)
			if sent := sentScreenshot(t, srv) != nil; sent != want {
				t.Errorf("screenshot sent %v, want %v", sent, want)
			}
		})
	}
}
