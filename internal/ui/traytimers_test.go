package ui

import (
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// The app's timers stop while the window is hidden in the tray, and start
// again once it shows.
func TestTheTimersStopInTheTray(t *testing.T) {
	a, _, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	var n atomic.Int32
	go a.everyShown(5*time.Millisecond, func() { n.Add(1) })
	eventually(t, "the timer never ran", func() bool { return n.Load() > 3 })
	a.hideToTray()
	// A tick already on its way lands.
	time.Sleep(20 * time.Millisecond)
	before := n.Load()
	time.Sleep(200 * time.Millisecond)
	if got := n.Load() - before; got != 0 {
		t.Fatalf("the timer ran %d times in the tray", got)
	}
	a.showWindow()
	eventually(t, "the timer did not start again", func() bool { return n.Load() > before+3 })
	// The timer waits in the tray for the rest of the test run.
	a.hideToTray()
}
