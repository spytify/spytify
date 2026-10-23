//go:build darwin

package meta

import (
	"context"
	"testing"
	"time"

	"github.com/spytify/spytify/internal/target"
)

// The poll script runs for real, against a player that is not installed;
// that path sends no Apple events, so no permission prompt appears.
func TestPollScriptWithAbsentPlayer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	j := NewJXA()
	j.Interval = 100 * time.Millisecond
	events, err := j.Watch(ctx, target.Target{ProcessNames: []string{"absent"}, BundleID: "org.spytify.absent"})
	if err != nil {
		t.Fatal(err)
	}
	var got []Event
	for len(got) < 2 {
		select {
		case e := <-events:
			got = append(got, e)
		case <-ctx.Done():
			t.Fatalf("poll script produced %d events", len(got))
		}
	}
	if got[0].Kind != TrackChanged || !got[0].Track.Empty() || got[1].Kind != StatusChanged || got[1].Status != Stopped {
		t.Fatalf("events = %+v", got)
	}
	if err := j.Control("next"); err != ErrNoPlayer {
		t.Fatalf("Control on an absent player: %v", err)
	}
}
