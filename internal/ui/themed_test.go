package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/software"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

// Fyne lets go of a widget it has not drawn for a minute, and of the theme
// stamped on it: the Record card, back from a minute on another tab, had
// the app's dark text on its darkened cover in light, and the footer's
// buttons lost their colours. A themed stamps its theme again when
// refreshed, as the tab is shown again.
func TestAHiddenTabComesBackInItsColours(t *testing.T) {
	if testing.Short() {
		t.Skip("waits for Fyne's clean, 10 s apart")
	}
	// Fyne reads how long it keeps a widget when it starts: the test runs
	// again in a process that keeps them a second.
	if os.Getenv("FYNE_CACHE") == "" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestAHiddenTabComesBackInItsColours$")
		cmd.Env = append(os.Environ(), "FYNE_CACHE=1s")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		return
	}
	// A window's draw tells Fyne the time, and cleans what it has not drawn
	// lately: the first draw at once, then 10 s apart at the soonest.
	draw := software.NewCanvas().Capture
	draw()
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	feedSession(t, m)
	a.render()
	// Nothing draws this one: Fyne letting go of its theme shows that the
	// clean ran.
	control := widget.NewLabel("")
	test.WidgetRenderer(newThemed(control, mediaTheme{}))

	a.showTab(tabSettings)
	for end := time.Now().Add(11 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		draw()
	}
	a.showTab(tabRecord)
	a.render()
	a.win.Canvas().Capture()

	if control.Refresh(); !inAppTheme(control) {
		t.Fatal("Fyne kept the theme of a widget nothing drew: the test cleaned nothing")
	}
	var lost []string
	var walk func(o fyne.CanvasObject, under bool)
	walk = func(o fyne.CanvasObject, under bool) {
		switch o := o.(type) {
		case *themed:
			walk(o.o.Content, true)
		case *fyne.Container:
			for _, c := range o.Objects {
				walk(c, under)
			}
		case fyne.Widget:
			if o.Refresh(); under && inAppTheme(o) {
				lost = append(lost, fmt.Sprintf("%T", o))
			}
			for _, c := range test.WidgetRenderer(o).Objects() {
				walk(c, under)
			}
		}
	}
	// The tabs are in the window's AppTabs.
	walk(a.win.Content(), false)
	walk(a.screens[tabLogs].(*logsScreen).diag, false)
	if len(lost) > 0 {
		t.Errorf("%d widgets lost their theme: %v", len(lost), lost)
	}
}

// inAppTheme says whether a widget draws in the app's theme rather than the
// one of the themed around it.
func inAppTheme(w fyne.Widget) bool {
	th, ok := w.(interface{ Theme() fyne.Theme })
	if !ok {
		return false
	}
	_, app := th.Theme().(spytifyTheme)
	return app
}
