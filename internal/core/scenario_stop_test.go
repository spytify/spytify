package core

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spytify/spytify/internal/fake"
	"github.com/spytify/spytify/pkg/proto"
)

// onTrackStart runs fn the first time a track with title starts.
func onTrackStart(r *recorder, title string, fn func()) {
	var once atomic.Bool
	r.onEvent = func(ev proto.Event) {
		var t proto.TrackStartedEvent
		if ev.Method == proto.EventTrackStarted && ev.Decode(&t) == nil && t.Title == title && once.CompareAndSwap(false, true) {
			fn()
		}
	}
}

func states(s *scenario) []proto.SessionState {
	var out []proto.SessionState
	for _, ev := range s.events.all() {
		var st proto.SessionStateEvent
		if ev.Method == proto.EventSessionState && ev.Decode(&st) == nil {
			out = append(out, st.State)
		}
	}
	return out
}

func TestGracefulStopFinishesCurrentTrack(t *testing.T) {
	tl := loadTimeline(t, "basic")
	// The stop, sent as the track starts, lets it finish.
	s := newScenario(t, tl, nil)
	onTrackStart(s.events, "Paper Lanterns", func() { s.core.StopSession(false) })
	runWith(t, s, SessionOptions{EndOnStop: true})
	saved, skipped := outcome(t, s)
	if len(saved) != 2 || saved[0] != "Midnight Ferry" || saved[1] != "Paper Lanterns" || len(skipped) != 0 {
		t.Fatalf("saved %v skipped %v\n%s", saved, skipped, s.logs)
	}
	sawStopping := false
	for _, st := range states(s) {
		sawStopping = sawStopping || st == proto.StateStopping
	}
	if !sawStopping {
		t.Fatalf("no stopping state: %v", states(s))
	}
	if problems := fake.Verify(tl, []fake.Expect{{Item: 0, Saved: true}, {Item: 1, Saved: true}}, 2, s.out, s.events.all()); len(problems) > 0 {
		t.Fatal(problems)
	}
}

func TestStopNowDiscardsCurrentTrack(t *testing.T) {
	tl := loadTimeline(t, "basic")
	s := newScenario(t, tl, nil)
	onTrackStart(s.events, "Paper Lanterns", func() { s.core.StopSession(true) })
	runWith(t, s, SessionOptions{EndOnStop: true})
	saved, skipped := outcome(t, s)
	if len(saved) != 1 || saved[0] != "Midnight Ferry" || skipped["Paper Lanterns"] != proto.SkipIncomplete {
		t.Fatalf("saved %v skipped %v", saved, skipped)
	}
	if files := fake.AudioFiles(s.out); len(files) != 1 {
		t.Fatalf("files %v", files)
	}
}

// With recorder.stop_with_player, a session started as the app and the CLI
// start one ends when the player stops, not at its pauses; turned on
// mid-session, it applies to that session. Off, the session outlives the
// stop: here, until the fake player's stream ends.
func TestTheSessionEndsWhenThePlayerStops(t *testing.T) {
	for _, c := range []struct {
		name       string
		on, midway bool
	}{{"on", true, false}, {"off", false, false}, {"turned on mid-session", false, true}} {
		t.Run(c.name, func(t *testing.T) {
			tl := loadTimeline(t, "pause-resume")
			s := newScenario(t, tl, map[string]any{"recorder.stop_with_player": c.on})
			if c.midway {
				s.player.At(2000, func() {
					if _, err := s.core.SetSettings(map[string]any{"recorder.stop_with_player": true}); err != nil {
						t.Error(err)
					}
				})
			}
			if _, err := s.core.Handle(context.Background(), proto.MethodSessionStart, nil); err != nil {
				t.Fatal(err)
			}
			done := make(chan struct{})
			go func() { s.core.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(time.Minute):
				t.Fatalf("session did not end; log:\n%s", s.logs)
			}
			if saved, skipped := outcome(t, s); len(saved) != len(tl.Items) {
				t.Errorf("saved %v skipped %v, want every track", saved, skipped)
			}
			ends := c.on || c.midway
			if outlived := strings.Contains(s.logs.String(), "capture stream ended"); outlived == ends {
				t.Errorf("the session outlived the stop: %v, want %v\n%s", outlived, !ends, s.logs)
			}
		})
	}
}
