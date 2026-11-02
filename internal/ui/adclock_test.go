package ui

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/spytify/spytify/pkg/proto"
)

// While an ad plays the recorder sends no level event, so the clock moves
// its Length bar: it counts up to the ad's end instead of stopping at 0:00
// (#645). Recording, the level events move it; waiting, nothing plays.
func TestTheClockMovesAnAdsLengthBar(t *testing.T) {
	a, m, _ := newTestApp(t, filepath.Join(t.TempDir(), "absent.sock"))
	if a.adPlaying() {
		t.Error("idle: the clock moves the card")
	}
	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateRecording}))
	m.Apply(event(t, proto.EventTrackStarted, proto.TrackStartedEvent{Track: proto.Track{ID: "s-001", Title: "Advert", LengthMs: 30_000}}))
	if a.adPlaying() {
		t.Error("recording: the clock moves the card, as the level events do")
	}
	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateAd}))
	if !a.adPlaying() {
		t.Fatal("an ad plays: the clock leaves its card")
	}
	a.render()
	r := a.screens[tabRecord].(*recordScreen)
	// The clock's tick, a second into the ad.
	time.Sleep(1100 * time.Millisecond)
	a.renderLevel()
	if got := r.elapsed.String(); got == "0:00 / 0:30" || r.progress.value == 0 {
		t.Errorf("a second into the ad, the card reads %q, its bar at %v", got, r.progress.value)
	}
	m.Apply(event(t, proto.EventSessionState, proto.SessionStateEvent{State: proto.StateWaiting}))
	if a.adPlaying() {
		t.Error("waiting: the clock still moves the card")
	}
}
