package ui

import (
	"sync"
	"time"

	"fyne.io/fyne/v2"
)

// throttleDelay keeps a redraw to at most ten a second.
const throttleDelay = 100 * time.Millisecond

// throttle runs a redraw on the toolkit's goroutine, throttleDelay after
// the first of a burst of calls. The calls made while it waits are
// dropped: the redraw reads the model when it runs, so it draws what they
// would have. Its zero value is ready to use.
type throttle struct {
	mu     sync.Mutex
	queued bool
}

// schedule runs fn throttleDelay from now, unless a run is already
// waiting. The wait ends before fn runs, so a change that comes while fn
// draws gets a redraw of its own.
func (t *throttle) schedule(fn func()) {
	t.mu.Lock()
	if t.queued {
		t.mu.Unlock()
		return
	}
	t.queued = true
	t.mu.Unlock()
	time.AfterFunc(throttleDelay, func() {
		t.mu.Lock()
		t.queued = false
		t.mu.Unlock()
		fyne.Do(fn)
	})
}
