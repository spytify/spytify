package core

import (
	"sync/atomic"
	"testing"

	"github.com/spytify/spytify/internal/capture"
	"github.com/spytify/spytify/internal/fake"
	"github.com/spytify/spytify/internal/meta"
	"github.com/spytify/spytify/internal/target"
	"github.com/spytify/spytify/pkg/proto"
)

type countingBackends struct {
	fakeBackends
	controls *atomic.Int32
}

type countingNP struct {
	*fake.Backend
	controls *atomic.Int32
}

func (c countingNP) Control(cmd string) error {
	if cmd == proto.CmdNext {
		c.controls.Add(1)
	}
	return c.Backend.Control(cmd)
}

func (c countingBackends) New(target.Target) (capture.Capture, meta.NowPlaying, error) {
	return c.b, countingNP{c.b, c.controls}, nil
}

func TestSkipDuplicateForward(t *testing.T) {
	tl := loadTimeline(t, "duplicate")
	s := newScenario(t, tl, map[string]any{"recorder.duplicates": "skip", "recorder.skip_duplicates_forward": true})
	var controls atomic.Int32
	s.core.backends = countingBackends{fakeBackends: s.core.backends.(fakeBackends), controls: &controls}
	runWith(t, s, SessionOptions{EndOnStop: true})
	saved, skipped := outcome(t, s)
	if len(saved) != 1 || skipped["Hollow Moon"] != proto.SkipDuplicate {
		t.Fatalf("saved %v skipped %v", saved, skipped)
	}
	if controls.Load() != 1 {
		t.Fatalf("player.control next sent %d times", controls.Load())
	}
}

func TestMuteAdsSilencesThePlayerDuringTheAd(t *testing.T) {
	tl := loadTimeline(t, "ads")
	tl.Items[1].LengthMs = 8000
	tl.Expect = nil
	s := newScenario(t, tl, map[string]any{"recorder.mute_ads": true})
	if err := s.player.SetVolume(0.6); err != nil {
		t.Fatal(err)
	}
	volumeAt := map[string]float64{}
	probe := func(name string) func() {
		return func() { volumeAt[name], _ = s.player.Volume() }
	}
	// Item 0 ends at 6.5 s, the ad at 15 s, the last item at 20.3 s.
	s.player.At(3000, probe("song"))
	s.player.At(11000, probe("ad"))
	s.player.At(18000, probe("after"))
	runWith(t, s, SessionOptions{EndOnStop: true})
	if volumeAt["ad"] != 0 {
		t.Errorf("volume during the ad = %v, want 0", volumeAt["ad"])
	}
	// The session pins the player at full volume and restores 60% at the end.
	if volumeAt["song"] != 1 || volumeAt["after"] != 1 {
		t.Errorf("volume around the ad = %v", volumeAt)
	}
	if v, _ := s.player.Volume(); v != 0.6 {
		t.Errorf("volume after the session = %v, want 0.6", v)
	}
}
