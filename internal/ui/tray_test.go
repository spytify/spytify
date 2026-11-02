package ui

import (
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2"

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
