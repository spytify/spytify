//go:build windows

package meta

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/spytify/spytify/internal/target"
)

// The poller loads the media session API in Windows PowerShell and reports a
// player that is not running: no session, no error.
func TestSMTCPollerRunsOnWindows(t *testing.T) {
	var mu sync.Mutex
	var logs []string
	m := NewSMTC()
	m.Logf = func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		logs = append(logs, fmt.Sprintf(format, args...))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	events, err := m.Watch(ctx, target.Target{ProcessNames: []string{"spytify-no-such-player"}})
	if err != nil {
		t.Fatal(err)
	}
	var got []Event
	for len(got) < 2 {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatalf("the poller stopped after %+v", got)
			}
			got = append(got, ev)
		case <-ctx.Done():
			t.Fatalf("no poll in 60 s; got %+v", got)
		}
	}
	if got[0].Kind != TrackChanged || !got[0].Track.Empty() || got[1].Kind != StatusChanged || got[1].Status != Stopped {
		t.Fatalf("first events = %+v", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(logs) > 0 {
		t.Fatalf("poller errors: %v", logs)
	}
}
