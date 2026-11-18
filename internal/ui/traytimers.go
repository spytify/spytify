package ui

import (
	"sync"
	"time"
)

// trayState says whether the window is hidden in the tray, for the timers
// that run off the toolkit's goroutine.
type trayState struct {
	mu     sync.Mutex
	hidden bool
	// back is closed when the window shows again.
	back chan struct{}
}

func (t *trayState) set(hidden bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if hidden == t.hidden {
		return
	}
	t.hidden = hidden
	if hidden {
		t.back = make(chan struct{})
	} else {
		close(t.back)
	}
}

// hiddenUntil reports whether the window is hidden in the tray, and if so
// a channel closed when it shows again.
func (t *trayState) hiddenUntil() (bool, <-chan struct{}) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.hidden, t.back
}

// everyShown calls fn every d while the window shows, for as long as the
// app runs. While it is hidden in the tray the ticker stops, so the app
// wakes for nothing there (#851).
func (a *App) everyShown(d time.Duration, fn func()) {
	t := time.NewTicker(d)
	for range t.C {
		if hidden, back := a.inTray.hiddenUntil(); hidden {
			t.Stop()
			<-back
			t.Reset(d)
			continue
		}
		fn()
	}
}
