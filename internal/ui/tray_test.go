package ui

import (
	"bytes"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/software"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"

	"github.com/spytify/spytify/internal/ui/model"
)

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

// QA found the macOS menu bar showing the logo in colour among icons in
// one colour. There the tray icon is a template, which macOS tints only if
// Fyne gets a themed resource whose SVG it can colour; Windows and Linux
// keep the logo in colour.
func TestTheMenuBarIconIsATemplateOnMacOS(t *testing.T) {
	test.NewTempApp(t)
	for _, goos := range []string{"windows", "linux"} {
		if trayIcon(goos) != Logo {
			t.Fatalf("%s tray icon is %s, want the logo in colour", goos, trayIcon(goos).Name())
		}
	}
	themed, ok := trayIcon("darwin").(*theme.ThemedResource)
	if !ok {
		t.Fatalf("macOS tray icon is a %T, want a *theme.ThemedResource", trayIcon("darwin"))
	}
	if bytes.Equal(themed.Content(), traySVG) {
		t.Fatal("Fyne could not colour tray.svg and fell back to its bytes")
	}

	// The coloured SVG keeps the logo's holes: the brow and the eye are
	// cut out of the disc, and the iris sits in the eye.
	img := canvas.NewImageFromResource(themed)
	c := software.NewTransparentCanvas()
	c.SetPadded(false)
	c.SetContent(img)
	c.Resize(fyne.NewSquareSize(256))
	shot := c.Capture()
	for _, p := range []struct {
		name   string
		x, y   int
		opaque bool
	}{
		{"disc", 128, 20, true},
		{"brow", 128, 79, false},
		{"eye", 60, 150, false},
		{"iris", 126, 154, true},
	} {
		if _, _, _, a := shot.At(p.x, p.y).RGBA(); p.opaque && a < 0xf000 || !p.opaque && a > 0x0fff {
			t.Errorf("%s at (%d,%d) has alpha %#x, want opaque %v", p.name, p.x, p.y, a, p.opaque)
		}
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
