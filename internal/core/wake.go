package core

import (
	"sync/atomic"
	"time"
)

// sleptGap is how far the wall clock may move between two ticks of a
// ticker before the computer is taken to have slept: the ticker stands
// still while it sleeps, the wall clock does not.
const sleptGap = time.Minute

// wakes keeps when the computer last woke from sleep, as a ticker sees it.
// tick runs on the ticker's goroutine; since is safe from any.
type wakes struct {
	prev time.Time
	// last is when it last woke, in Unix nanoseconds, 0 before any wake.
	last atomic.Int64
}

// tick takes the time of a tick and returns how long the computer slept
// before it, 0 when it did not.
func (w *wakes) tick(now time.Time) time.Duration {
	now = now.Round(0)
	prev := w.prev
	w.prev = now
	if prev.IsZero() {
		return 0
	}
	if gap := now.Sub(prev); gap > sleptGap {
		w.last.Store(now.UnixNano())
		return gap
	}
	return 0
}

// since is how long ago the computer last woke, and false when it has not
// slept since spytifyd started.
func (w *wakes) since(now time.Time) (time.Duration, bool) {
	last := w.last.Load()
	if last == 0 {
		return 0, false
	}
	return now.Round(0).Sub(time.Unix(0, last)), true
}
