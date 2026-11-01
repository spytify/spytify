package ui

import (
	"sync/atomic"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"
)

// A burst of changes gets one redraw, throttleDelay after its first call,
// and the next change after that redraw gets another.
func TestABurstOfChangesGetsOneRedraw(t *testing.T) {
	test.NewTempApp(t)
	var q throttle
	runs := make(chan time.Time, 20)
	redraw := func() { runs <- time.Now() }

	start := time.Now()
	for range 20 {
		q.schedule(redraw)
	}
	if waited := nextRun(t, runs).Sub(start); waited < throttleDelay {
		t.Errorf("the redraw ran after %v, before %v", waited, throttleDelay)
	}
	noRun(t, runs)

	q.schedule(redraw)
	nextRun(t, runs)
	noRun(t, runs)
}

// A change that comes while the redraw draws gets a redraw of its own: the
// wait ends before the redraw runs.
func TestAChangeDuringARedrawGetsItsOwn(t *testing.T) {
	test.NewTempApp(t)
	var q throttle
	var calls atomic.Int32
	runs := make(chan time.Time, 2)
	var redraw func()
	redraw = func() {
		if calls.Add(1) == 1 {
			q.schedule(redraw)
		}
		runs <- time.Now()
	}

	q.schedule(redraw)
	nextRun(t, runs)
	nextRun(t, runs)
}

// The window's redraw and the meter's don't drop each other: each has its
// own throttle.
func TestTwoThrottlesDontDropEachOther(t *testing.T) {
	test.NewTempApp(t)
	var render, level throttle
	runs := make(chan time.Time, 2)
	redraw := func() { runs <- time.Now() }

	render.schedule(redraw)
	level.schedule(redraw)
	nextRun(t, runs)
	nextRun(t, runs)
}

func nextRun(t *testing.T, runs <-chan time.Time) time.Time {
	t.Helper()
	select {
	case at := <-runs:
		return at
	case <-time.After(time.Second):
		t.Fatal("the redraw never ran")
		return time.Time{}
	}
}

func noRun(t *testing.T, runs <-chan time.Time) {
	t.Helper()
	select {
	case <-runs:
		t.Fatal("the redraw ran once too often")
	case <-time.After(2 * throttleDelay):
	}
}
