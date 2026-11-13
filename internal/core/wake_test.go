package core

import (
	"testing"
	"time"
)

// A ticker's ticks a minute or more apart, by the wall clock, mean the
// computer slept between them: the second is when it woke. The first tick
// and ticks on time say nothing.
func TestWakesSeeTheComputerSleep(t *testing.T) {
	var w wakes
	t0 := time.Date(2026, 11, 12, 1, 0, 0, 0, time.UTC)
	for i, c := range []struct {
		at    time.Time
		slept time.Duration
	}{
		{t0, 0},
		{t0.Add(2 * time.Second), 0},
		{t0.Add(62 * time.Second), 0},
		{t0.Add(8 * time.Hour), 8*time.Hour - 62*time.Second},
		{t0.Add(8*time.Hour + 2*time.Second), 0},
	} {
		if got := w.tick(c.at); got != c.slept {
			t.Errorf("tick %d: slept %s, want %s", i, got, c.slept)
		}
	}
	if d, ok := w.since(t0.Add(8*time.Hour + 30*time.Second)); !ok || d != 30*time.Second {
		t.Errorf("since the wake: %s %v, want 30s true", d, ok)
	}
	var never wakes
	never.tick(t0)
	if _, ok := never.since(t0); ok {
		t.Error("a computer that never slept woke")
	}
}
